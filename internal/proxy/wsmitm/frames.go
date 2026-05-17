// Package wsmitm implements the T188 WebSocket-MITM engine for the
// Codex Responses API conversation transport
// (`wss://chatgpt.com/backend-api/codex/responses` with subprotocol
// `responses_websockets=2026-02-06`).
//
// The wire schema is derived from openai/codex Rust sources
// (`codex-rs/codex-api/src/endpoint/responses_websocket.rs`,
// `codex-rs/codex-api/src/sse/responses.rs`) read on 2026-05-16.
// We deliberately keep the parser **schema-light**: only the
// discriminator (`type` field) and the minimum fields we need to
// route a frame are interpreted. Everything else passes through
// as `json.RawMessage` so we forward bytes faithfully even when
// OpenAI adds new fields.
//
// Fail-open: malformed JSON envelope frames downgrade the session to a
// pure passthrough tunnel. Compressed/RSV frames and text frames that are
// not JSON envelopes are forwarded byte-equal without degrading because
// they are valid WSS protocol payloads but not Phase-F mutation
// candidates in their current wire shape.
package wsmitm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// FrameKind is the union of frame types we recognise from the
// derived schema. Strings come from the Codex `kind` field
// (`#[serde(rename = "type")]`).
type FrameKind string

const (
	// Client-to-server frames.
	FrameKindRequest FrameKind = "request"
	FrameKindPing    FrameKind = "ping"

	// Server-to-client framing for the Responses-API stream events.
	// `kind` corresponds to the SSE event type the server would have
	// emitted; the websocket transport wraps the SSE payload directly.
	FrameKindResponseCreated                   FrameKind = "response.created"
	FrameKindResponseOutputItemAdded           FrameKind = "response.output_item.added"
	FrameKindResponseOutputItemDone            FrameKind = "response.output_item.done"
	FrameKindResponseOutputTextDelta           FrameKind = "response.output_text.delta"
	FrameKindResponseReasoningSummaryPartAdded FrameKind = "response.reasoning_summary_part.added"
	FrameKindResponseReasoningSummaryTextDelta FrameKind = "response.reasoning_summary_text.delta"
	FrameKindResponseReasoningTextDelta        FrameKind = "response.reasoning_text.delta"
	FrameKindResponseCustomToolCallInputDelta  FrameKind = "response.custom_tool_call_input.delta"
	FrameKindResponseFunctionCallArgsDelta     FrameKind = "response.function_call_arguments.delta"
	FrameKindResponseCompleted                 FrameKind = "response.completed"
	FrameKindResponseIncomplete                FrameKind = "response.incomplete"
	FrameKindResponseFailed                    FrameKind = "response.failed"

	// Codex-specific server frames.
	FrameKindCodexRateLimits  FrameKind = "codex.rate_limits"
	FrameKindResponseMetadata FrameKind = "response.metadata"

	// Control / error / unknown frames.
	FrameKindError   FrameKind = "error"
	FrameKindPong    FrameKind = "pong"
	FrameKindUnknown FrameKind = ""
)

// KnownFrameKinds returns the closed set of frame types the parser
// can dispatch deterministically. Frames with `type` strings not in
// this list are reported as Unknown by Parse, which is the signal
// for fail-open passthrough.
var KnownFrameKinds = []FrameKind{
	FrameKindRequest,
	FrameKindPing,
	FrameKindResponseCreated,
	FrameKindResponseOutputItemAdded,
	FrameKindResponseOutputItemDone,
	FrameKindResponseOutputTextDelta,
	FrameKindResponseReasoningSummaryPartAdded,
	FrameKindResponseReasoningSummaryTextDelta,
	FrameKindResponseReasoningTextDelta,
	FrameKindResponseCustomToolCallInputDelta,
	FrameKindResponseFunctionCallArgsDelta,
	FrameKindResponseCompleted,
	FrameKindResponseIncomplete,
	FrameKindResponseFailed,
	FrameKindCodexRateLimits,
	FrameKindResponseMetadata,
	FrameKindError,
	FrameKindPong,
}

// IsKnown reports whether `k` is in the closed set.
func (k FrameKind) IsKnown() bool {
	if k == FrameKindUnknown {
		return false
	}
	for _, known := range KnownFrameKinds {
		if known == k {
			return true
		}
	}
	return false
}

// IsTextDelta reports whether the frame carries a streamed text
// fragment that the streamcut cutter should observe.
func (k FrameKind) IsTextDelta() bool {
	switch k {
	case FrameKindResponseOutputTextDelta,
		FrameKindResponseReasoningTextDelta,
		FrameKindResponseReasoningSummaryTextDelta,
		FrameKindResponseFunctionCallArgsDelta,
		FrameKindResponseCustomToolCallInputDelta:
		return true
	}
	return false
}

// IsTerminal reports whether the frame signals end-of-response, after
// which the engine should run repdet over accumulated output and
// finalise telemetry.
func (k FrameKind) IsTerminal() bool {
	switch k {
	case FrameKindResponseCompleted, FrameKindResponseIncomplete, FrameKindResponseFailed:
		return true
	}
	return false
}

// IsControl reports whether the frame is a control-plane keepalive
// (ping/pong). Control frames pass through untouched.
func (k FrameKind) IsControl() bool {
	return k == FrameKindPing || k == FrameKindPong
}

// Envelope is the parsed shape of a single wire frame. Raw holds the
// original bytes so the engine can re-emit the frame byte-equal when
// no mutation is needed. Fields beyond `kind` come from the Codex
// `ResponsesStreamEvent` struct - all optional, all passed through as
// raw JSON.
type Envelope struct {
	Kind     FrameKind       `json:"type"`
	Headers  json.RawMessage `json:"headers,omitempty"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
	Request  json.RawMessage `json:"request,omitempty"`
	Response json.RawMessage `json:"response,omitempty"`
	Item     json.RawMessage `json:"item,omitempty"`
	Body     json.RawMessage `json:"body,omitempty"`
	ItemID   string          `json:"item_id,omitempty"`
	CallID   string          `json:"call_id,omitempty"`
	Delta    string          `json:"delta,omitempty"`

	SequenceNumber *int64 `json:"sequence_number,omitempty"`
	SummaryIndex   *int64 `json:"summary_index,omitempty"`
	ContentIndex   *int64 `json:"content_index,omitempty"`

	// Raw holds the unmodified wire bytes. Used when forwarding
	// without mutation so byte-equality is preserved.
	Raw json.RawMessage `json:"-"`

	// Fields preserves the full JSON object, including schema fields
	// Slimference does not understand yet. Mutation code updates the
	// typed fields above and Marshal merges them back into Fields
	// instead of dropping unknown Codex fields on re-encode.
	Fields map[string]json.RawMessage `json:"-"`
}

// ErrEmpty is returned when Parse is handed an empty byte slice.
var ErrEmpty = errors.New("wsmitm: empty frame")

// ErrMalformed is returned when the bytes are not valid JSON.
var ErrMalformed = errors.New("wsmitm: malformed JSON")

// Parse decodes one WebSocket text-frame payload. The byte slice
// must be a complete JSON object (the WebSocket framing layer
// guarantees this for `Text` opcodes). Returns ErrMalformed when the
// payload isn't valid JSON - callers handle that by downgrading the
// session to pure passthrough.
//
// Unknown `kind` values do NOT produce errors: the returned Envelope
// has `Kind = FrameKindUnknown`. Callers inspect IsKnown / IsTextDelta
// etc to decide what to do.
func Parse(data []byte) (Envelope, error) {
	if len(data) == 0 {
		return Envelope{}, ErrEmpty
	}
	if !looksLikeJSONObject(data) {
		return Envelope{}, ErrMalformed
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return Envelope{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return Envelope{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	env.Raw = append([]byte(nil), data...)
	env.Fields = cloneRawFields(fields)
	if !env.Kind.IsKnown() {
		env.Kind = FrameKindUnknown
	}
	return env, nil
}

// looksLikeJSONObject does a fast prefix-check before invoking
// json.Unmarshal so completely-bogus inputs short-circuit cheaply.
func looksLikeJSONObject(data []byte) bool {
	for _, b := range data {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		case '{':
			return true
		default:
			return false
		}
	}
	return false
}

// Sequence pulls the `sequence_number` field out of the envelope or
// returns 0 if not present. The Responses-API streams events with
// monotonically-increasing sequence numbers, used by the engine to
// detect dropped/reordered frames.
func (e Envelope) Sequence() int64 {
	if e.SequenceNumber == nil {
		return 0
	}
	return *e.SequenceNumber
}

// Marshal returns the envelope re-encoded as JSON. When Raw is
// available and the envelope hasn't been mutated, the caller should
// prefer Raw for byte-equal forwarding; Marshal exists for cases
// where Phase F handlers modified a field.
func (e Envelope) Marshal() ([]byte, error) {
	fields := cloneRawFields(e.Fields)
	if fields == nil {
		fields = make(map[string]json.RawMessage, 12)
	}
	if e.Kind != FrameKindUnknown {
		setStringJSON(fields, "type", string(e.Kind), true)
	}
	setRawJSON(fields, "headers", e.Headers)
	setRawJSON(fields, "metadata", e.Metadata)
	setRawJSON(fields, "request", e.Request)
	setRawJSON(fields, "response", e.Response)
	setRawJSON(fields, "item", e.Item)
	setRawJSON(fields, "body", e.Body)
	_, hadItemID := fields["item_id"]
	setStringJSON(fields, "item_id", e.ItemID, hadItemID)
	_, hadCallID := fields["call_id"]
	setStringJSON(fields, "call_id", e.CallID, hadCallID)
	_, hadDelta := fields["delta"]
	setStringJSON(fields, "delta", e.Delta, hadDelta)
	setIntPtrJSON(fields, "sequence_number", e.SequenceNumber)
	setIntPtrJSON(fields, "summary_index", e.SummaryIndex)
	setIntPtrJSON(fields, "content_index", e.ContentIndex)
	return json.Marshal(fields)
}

func cloneRawFields(in map[string]json.RawMessage) map[string]json.RawMessage {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]json.RawMessage, len(in))
	for k, v := range in {
		out[k] = append(json.RawMessage(nil), v...)
	}
	return out
}

func setRawJSON(fields map[string]json.RawMessage, key string, raw json.RawMessage) {
	if len(raw) == 0 {
		delete(fields, key)
		return
	}
	fields[key] = append(json.RawMessage(nil), raw...)
}

func setStringJSON(fields map[string]json.RawMessage, key, value string, keepEmpty bool) {
	if value == "" && !keepEmpty {
		delete(fields, key)
		return
	}
	encoded, _ := json.Marshal(value)
	fields[key] = encoded
}

func setIntPtrJSON(fields map[string]json.RawMessage, key string, value *int64) {
	if value == nil {
		delete(fields, key)
		return
	}
	encoded, _ := json.Marshal(*value)
	fields[key] = encoded
}

// String renders an envelope summary for log lines. Includes kind +
// item id + call id + sequence so a trail of frames in the daemon
// log is greppable.
func (e Envelope) String() string {
	var b strings.Builder
	b.WriteString(string(e.Kind))
	if e.SequenceNumber != nil {
		fmt.Fprintf(&b, " seq=%d", *e.SequenceNumber)
	}
	if e.ItemID != "" {
		fmt.Fprintf(&b, " item=%s", e.ItemID)
	}
	if e.CallID != "" {
		fmt.Fprintf(&b, " call=%s", e.CallID)
	}
	return b.String()
}

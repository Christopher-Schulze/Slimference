// Package streamcut watches an in-flight SSE response and signals
// the proxy when the model has slipped into trailing commentary
// ("Hope this helps!", "Let me know if…"). The proxy then closes
// the upstream connection - stopping further token generation - and
// emits a synthetic terminator to the client. Already-streamed bytes
// stay on the client's screen; the saving comes from preventing the
// model from continuing past the commentary opener.
//
// This package is a safety net for cases the API-level stop_sequences
// (see internal/outstop) miss: providers cap stop arrays at four, and
// the model may invent unlisted closing phrases.
package streamcut

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/outstop"
)

// Cutter inspects SSE deltas as they flow upstream→client and decides
// when the response has begun trailing commentary. A single Cutter is
// scoped to one HTTP response stream; it is not safe for concurrent
// use across requests.
type Cutter struct {
	provider      string
	buf           strings.Builder
	fired         bool
	minBeforeFire int
	// tailWindow caps the lookback when checking for pattern matches.
	// A match older than this is treated as residue from earlier
	// content, not as evidence the model just opened commentary.
	tailWindow int
	phrases    []string

	// holdback (T184) queues recent text-delta lines so that the
	// trailing-commentary opener never reaches the client. When the
	// cutter fires, the queue is dropped (those bytes are never
	// emitted). Set holdbackLines via NewCutterWithHoldback.
	holdback      [][]byte
	holdbackLines int
}

// NewCutter builds a Cutter for the given upstream provider ("anthropic",
// "openai", or "codex_chatgpt"). Other providers receive a no-op cutter
// (Observe always returns false) so the proxy can call it unconditionally.
func NewCutter(provider string) *Cutter {
	return &Cutter{
		provider:      provider,
		minBeforeFire: 80,
		tailWindow:    96,
		phrases:       outstop.Phrases(),
	}
}

// NewCutterWithHoldback builds a Cutter that delays forwarding text
// deltas by `holdbackLines` SSE lines. When the cutter fires the
// queued lines are dropped instead of forwarded, so the
// trailing-commentary opener never reaches the client. Set
// holdbackLines=0 for the legacy passthrough behaviour.
func NewCutterWithHoldback(provider string, holdbackLines int) *Cutter {
	c := NewCutter(provider)
	if holdbackLines > 0 {
		c.holdbackLines = holdbackLines
	}
	return c
}

// Observe consumes one raw SSE line from the upstream response. Returns
// true the first time a commentary pattern is detected; on every
// subsequent call after firing it also returns true so callers can short
// circuit. Non-text events (event:, empty lines, message_start, usage
// envelopes) are accumulated only for their textual content - control
// frames are ignored.
func (c *Cutter) Observe(line []byte) bool {
	if c.fired {
		return true
	}
	text := extractDeltaText(c.provider, line)
	if text == "" {
		return false
	}
	c.buf.WriteString(text)
	if c.buf.Len() < c.minBeforeFire {
		return false
	}
	if c.matchTail() {
		c.fired = true
		return true
	}
	return false
}

// Fired reports whether the cutter has already triggered. Useful for
// callers that want to know without re-feeding a line.
func (c *Cutter) Fired() bool { return c.fired }

// Forward consumes one upstream SSE line and decides what to emit
// downstream. Behaviour:
//   - For non-text events (event:, message_start, control frames),
//     emit immediately so the client sees the protocol-required envelope.
//   - For text-delta events, queue the line. Once the queue exceeds
//     holdbackLines, the oldest queued line is returned for emission.
//   - When the trailing-commentary matcher fires, the queue is dropped
//     and the synthetic provider terminator is returned with
//     terminate=true so the caller closes the upstream connection.
//
// holdbackLines==0 collapses to legacy passthrough: every line is
// returned immediately, the matcher still fires, and on fire the
// caller emits the terminator after the current line.
func (c *Cutter) Forward(line []byte) (emit []byte, terminate bool) {
	if c.fired {
		return nil, true
	}
	text := extractDeltaText(c.provider, line)
	if text == "" {
		return line, false
	}
	c.buf.WriteString(text)
	if c.buf.Len() >= c.minBeforeFire && c.matchTail() {
		c.fired = true
		c.holdback = nil
		return c.SyntheticTerminator(), true
	}
	if c.holdbackLines <= 0 {
		return line, false
	}
	copied := make([]byte, len(line))
	copy(copied, line)
	c.holdback = append(c.holdback, copied)
	if len(c.holdback) <= c.holdbackLines {
		return nil, false
	}
	out := c.holdback[0]
	c.holdback = c.holdback[1:]
	return out, false
}

// Flush returns any text-delta lines still queued in the holdback
// after the upstream stream ends naturally without firing the cutter.
// Callers must emit each returned line in order so the client sees
// the full natural response.
func (c *Cutter) Flush() [][]byte {
	if c.fired {
		c.holdback = nil
		return nil
	}
	out := c.holdback
	c.holdback = nil
	return out
}

// SyntheticTerminator returns the SSE bytes a caller should emit after
// the cutter fires so the client sees a clean end-of-stream matching
// the upstream protocol. Returns nil for unknown providers.
func (c *Cutter) SyntheticTerminator() []byte {
	switch c.provider {
	case "anthropic":
		// Anthropic streams a sequence of events; emit the last
		// two (message_delta with stop_reason and message_stop) so
		// downstream SDKs treat the turn as complete.
		return []byte("event: message_delta\n" +
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"stop_sequence\"}}\n\n" +
			"event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n\n")
	case "openai", "codex_chatgpt":
		return []byte("data: [DONE]\n\n")
	}
	return nil
}

// matchTail looks for any registered phrase within the last tailWindow
// bytes of accumulated text. A match older than the window is ignored:
// the model has moved past it and is no longer in commentary mode.
func (c *Cutter) matchTail() bool {
	s := c.buf.String()
	start := 0
	if len(s) > c.tailWindow {
		start = len(s) - c.tailWindow
	}
	tail := s[start:]
	for _, p := range c.phrases {
		if strings.Contains(tail, p) {
			return true
		}
	}
	return false
}

// extractDeltaText pulls the visible text delta out of a single SSE
// line for the given provider. Returns "" for control frames, empty
// lines, malformed JSON, or non-text deltas. The caller does not see
// distinction between "no text" and "parse failed" - both safely
// return "" and the cutter accumulator is left untouched.
func extractDeltaText(provider string, line []byte) string {
	if !bytes.HasPrefix(line, []byte("data: ")) {
		return ""
	}
	data := line[6:]
	if bytes.Equal(data, []byte("[DONE]")) {
		return ""
	}
	switch provider {
	case "anthropic":
		return extractAnthropicDelta(data)
	case "openai", "codex_chatgpt":
		return extractOpenAIDelta(data)
	}
	return ""
}

func extractAnthropicDelta(data []byte) string {
	var ev struct {
		Type  string `json:"type"`
		Delta *struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta,omitempty"`
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return ""
	}
	if ev.Type == "content_block_delta" && ev.Delta != nil && ev.Delta.Type == "text_delta" {
		return ev.Delta.Text
	}
	return ""
}

func extractOpenAIDelta(data []byte) string {
	// Chat Completions: choices[0].delta.content
	var chunk struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
		// Responses API: top-level delta as plain string
		Type  string `json:"type,omitempty"`
		Delta string `json:"delta,omitempty"`
	}
	if err := json.Unmarshal(data, &chunk); err != nil {
		return ""
	}
	if chunk.Type == "response.output_text.delta" && chunk.Delta != "" {
		return chunk.Delta
	}
	for _, c := range chunk.Choices {
		if c.Delta.Content != "" {
			return c.Delta.Content
		}
	}
	return ""
}

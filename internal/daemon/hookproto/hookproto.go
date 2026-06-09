// Package hookproto defines the wire schema between the fail-open sidecar
// (cmd/slimference-sidecar) and the Slimference engine (internal/daemon
// hookserver). The protocol is newline-delimited JSON over a Unix domain
// socket; one connection carries one or more request/response pairs.
//
// encoding/json only (docs/spec.md "Document authority"). No third-party
// codecs.
//
// Versioning: every Envelope carries Version. Bumping is additive: a
// newer sidecar may speak Version 2 to an older engine, which must
// respond with ErrUnsupportedVersion so the sidecar degrades to direct
// passthrough rather than mis-parsing.
package hookproto

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// CurrentVersion pins the protocol revision shipped with this Slimference
// build. Bump when the schema changes incompatibly.
const CurrentVersion = 1

// Op identifies the RPC operation carried by an Envelope. Keep the set
// minimal: each new op is an extension surface we have to keep
// compatible.
type Op string

const (
	// OpPing is a cheap health probe. Response carries Engine status.
	OpPing Op = "ping"

	// OpForwardRequest hands the engine a full HTTP request shape so the
	// engine can apply Layer 1..4 compaction and return a (possibly
	// mutated) request the sidecar then forwards upstream. On any engine
	// error the sidecar must fall back to forwarding the original
	// request verbatim.
	OpForwardRequest Op = "forward_request"
)

// Envelope is the wire form of every request and response. Exactly one
// of Request or Response is populated per frame.
type Envelope struct {
	Version  int       `json:"version"`
	Op       Op        `json:"op"`
	ID       string    `json:"id,omitempty"`
	Request  *Request  `json:"request,omitempty"`
	Response *Response `json:"response,omitempty"`
}

// Request carries the operation payload from sidecar to engine.
type Request struct {
	// Ping has no payload.
	Ping *PingRequest `json:"ping,omitempty"`

	// ForwardRequest carries the HTTP request shape.
	ForwardRequest *ForwardRequest `json:"forward_request,omitempty"`
}

// Response carries the operation result from engine to sidecar.
type Response struct {
	// Error is non-empty when the engine refused the operation. The
	// sidecar treats any non-empty Error as a signal to degrade to
	// direct passthrough for this request.
	Error string `json:"error,omitempty"`

	Ping           *PingResponse           `json:"ping,omitempty"`
	ForwardRequest *ForwardRequestResponse `json:"forward_request,omitempty"`
}

// PingRequest is intentionally empty. Reserved for future capability
// negotiation.
type PingRequest struct{}

// PingResponse describes the engine's state at probe time.
type PingResponse struct {
	Healthy       bool   `json:"healthy"`
	Version       string `json:"version,omitempty"`
	UptimeSeconds int64  `json:"uptime_seconds,omitempty"`
	LastError     string `json:"last_error,omitempty"`
}

// ForwardRequest is the request shape the engine receives so it can run
// compaction. The sidecar uses it to ask the engine "may I mutate this
// before forwarding?". The engine returns ForwardRequestResponse with
// either a mutated body or PassThrough=true.
type ForwardRequest struct {
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Headers map[string][]string `json:"headers"`
	Body    []byte              `json:"body"`
	// SourceUA is the client User-Agent header, lifted out for cheap
	// inspection without re-parsing Headers on the engine side.
	SourceUA string `json:"source_ua,omitempty"`
}

// ForwardRequestResponse instructs the sidecar what to forward upstream.
type ForwardRequestResponse struct {
	// PassThrough=true means "forward the original request unchanged".
	// All other fields are ignored in that case.
	PassThrough bool `json:"pass_through"`

	// Method/URL/Headers/Body, when PassThrough is false, replace the
	// original request before the sidecar forwards. Method/URL empty
	// means "keep original".
	Method  string              `json:"method,omitempty"`
	URL     string              `json:"url,omitempty"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    []byte              `json:"body,omitempty"`
}

// ErrUnsupportedVersion is what the engine must return when it sees a
// Version it does not understand. Treated by the sidecar as a fail-open
// trigger.
var ErrUnsupportedVersion = errors.New("hookproto: unsupported version")

// NewEnvelope is a convenience constructor that stamps the current
// version and op into an outgoing Envelope.
func NewEnvelope(op Op, id string) Envelope {
	return Envelope{Version: CurrentVersion, Op: op, ID: id}
}

// Encode writes one Envelope as a single JSON line followed by '\n'.
// Callers that share a writer must serialize their writes.
//
// json.Marshal cannot fail for Envelope: every field is a
// JSON-representable plain type, []byte (base64-encoded), or
// map[string][]string. If a future schema change introduces a field
// whose marshal can fail (channels, funcs, NaN floats), reinstate the
// marshal-error wrap.
func Encode(w io.Writer, env Envelope) error {
	buf, _ := json.Marshal(env)
	buf = append(buf, '\n')
	if _, err := w.Write(buf); err != nil {
		return fmt.Errorf("hookproto: write: %w", err)
	}
	return nil
}

// Decode reads one '\n'-terminated JSON line from r and unmarshals it
// into an Envelope. EOF is returned as io.EOF (unwrapped) so callers can
// detect clean shutdown.
func Decode(r *bufio.Reader) (Envelope, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		if errors.Is(err, io.EOF) && len(line) == 0 {
			return Envelope{}, io.EOF
		}
		if !errors.Is(err, io.EOF) {
			return Envelope{}, fmt.Errorf("hookproto: read: %w", err)
		}
		// Trailing line without newline at EOF: try to decode it.
	}
	var env Envelope
	if err := json.Unmarshal(trimNewline(line), &env); err != nil {
		return Envelope{}, fmt.Errorf("hookproto: unmarshal: %w", err)
	}
	if env.Version == 0 {
		return env, fmt.Errorf("hookproto: missing version")
	}
	if env.Version > CurrentVersion {
		return env, ErrUnsupportedVersion
	}
	return env, nil
}

func trimNewline(b []byte) []byte {
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	if len(b) > 0 && b[len(b)-1] == '\r' {
		b = b[:len(b)-1]
	}
	return b
}

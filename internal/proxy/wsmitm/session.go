package wsmitm

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/slimference/slimference/internal/wscompact"
)

// Direction names the half of a bridged session.
type Direction string

const (
	DirClientToServer Direction = "c2s"
	DirServerToClient Direction = "s2c"
)

// FrameHandler is invoked once per parsed JSON frame. The handler
// may mutate the Envelope (e.g. inject stop_sequences, run repdet,
// etc.). When the returned `replace` flag is true the bridge re-
// encodes the envelope from its current Go-struct state; when false
// the bridge forwards the original bytes byte-equal.
//
// Returning a non-nil error closes the session.
type FrameHandler func(ctx context.Context, dir Direction, env *Envelope) (replace bool, err error)

// Session bridges a client WebSocket to an upstream WebSocket while
// running each text frame through the Phase F pipeline via the
// FrameHandler. Non-text frames (ping/pong/close/binary) pass through
// untouched - only JSON text frames carry conversation events.
//
// Fail-open: any parse / handler error counts a `degraded` event
// and falls through to byte-copy mode for the rest of the session.
// The Codex client retries against the real upstream if our session
// ends abruptly.
type Session struct {
	// Client is the local end of the bridge (Codex CLI / Desktop).
	// Frames read from here go through ClientHandler then onto Upstream.
	Client io.ReadWriter
	// Upstream is the real chatgpt.com end of the bridge.
	// Frames read from here go through UpstreamHandler then onto Client.
	Upstream io.ReadWriter
	// ClientHandler runs on every frame travelling Client → Upstream.
	// Optional; nil = byte-equal forwarding.
	ClientHandler FrameHandler
	// UpstreamHandler runs on every frame travelling Upstream → Client.
	// Optional; nil = byte-equal forwarding.
	UpstreamHandler FrameHandler
	// Telemetry counters.
	counters SessionCounters
}

// SessionCounters tracks per-session frame and degradation events.
type SessionCounters struct {
	C2SFrames       atomic.Int64
	S2CFrames       atomic.Int64
	C2SBytes        atomic.Int64
	S2CBytes        atomic.Int64
	ParseFailures   atomic.Int64
	Degraded        atomic.Bool
	FramesReencoded atomic.Int64
	FramesForwarded atomic.Int64
}

// Snapshot returns a value-copy of the counters for telemetry.
func (s *Session) Snapshot() SessionTelemetry {
	return SessionTelemetry{
		C2SFrames:       s.counters.C2SFrames.Load(),
		S2CFrames:       s.counters.S2CFrames.Load(),
		C2SBytes:        s.counters.C2SBytes.Load(),
		S2CBytes:        s.counters.S2CBytes.Load(),
		ParseFailures:   s.counters.ParseFailures.Load(),
		Degraded:        s.counters.Degraded.Load(),
		FramesReencoded: s.counters.FramesReencoded.Load(),
		FramesForwarded: s.counters.FramesForwarded.Load(),
	}
}

// SessionTelemetry is the read-only view of session counters.
type SessionTelemetry struct {
	C2SFrames       int64 `json:"c2s_frames"`
	S2CFrames       int64 `json:"s2c_frames"`
	C2SBytes        int64 `json:"c2s_bytes"`
	S2CBytes        int64 `json:"s2c_bytes"`
	ParseFailures   int64 `json:"parse_failures"`
	Degraded        bool  `json:"degraded"`
	FramesReencoded int64 `json:"frames_reencoded"`
	FramesForwarded int64 `json:"frames_forwarded"`
}

// ErrSessionClosed is returned by Serve when either end-of-stream
// occurs normally. Distinct from genuine errors so the caller can
// branch.
var ErrSessionClosed = errors.New("wsmitm: session closed by peer")

// Serve runs the bidirectional pump until either side closes or ctx
// is cancelled. Returns as soon as the first direction terminates;
// the other direction's goroutine will exit on its next Read once
// the caller closes the corresponding side of the connection.
//
// **Caller contract**: callers MUST close both Client and Upstream
// after Serve returns to release the remaining goroutine. Production
// callers wrap Serve in a function that closes both via `defer`.
// Tests close their in-memory pipes explicitly.
func (s *Session) Serve(ctx context.Context) error {
	if s.Client == nil || s.Upstream == nil {
		return errors.New("wsmitm: Session.Client and .Upstream must be non-nil")
	}
	derived, cancel := context.WithCancel(ctx)
	defer cancel()

	errs := make(chan error, 2)
	go func() {
		errs <- s.pump(derived, DirClientToServer, s.Client, s.Upstream, s.ClientHandler)
	}()
	go func() {
		errs <- s.pump(derived, DirServerToClient, s.Upstream, s.Client, s.UpstreamHandler)
	}()

	// Whichever direction finishes first determines the session's
	// outcome. The second pump will return on its own once its
	// underlying Reader closes (caller's responsibility).
	first := <-errs

	if first == nil || errors.Is(first, ErrSessionClosed) ||
		errors.Is(first, io.EOF) || errors.Is(first, context.Canceled) {
		return nil
	}
	return first
}

// pump runs one direction of the bridge. Returns on first error or
// EOF.
func (s *Session) pump(ctx context.Context, dir Direction, src io.Reader,
	dst io.Writer, handler FrameHandler) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		frame, err := wscompact.ReadFrame(src)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return ErrSessionClosed
			}
			return fmt.Errorf("read frame: %w", err)
		}
		s.countFrame(dir, len(frame.Raw))

		// Pass-through for frames that Phase-F cannot safely mutate in
		// their current wire shape. RSV frames are usually compressed
		// by negotiated WebSocket extensions (e.g. permessage-deflate);
		// they need extension-aware decode/re-encode before mutation.
		if frame.Opcode != byte(wscompact.OpcodeText) || frame.RSV ||
			s.counters.Degraded.Load() || handler == nil || len(frame.Payload) == 0 {
			if _, err := dst.Write(frame.Raw); err != nil {
				return fmt.Errorf("write frame raw: %w", err)
			}
			s.counters.FramesForwarded.Add(1)
			continue
		}

		if !looksLikeJSONObject(frame.Payload) {
			if _, err := dst.Write(frame.Raw); err != nil {
				return fmt.Errorf("write non-envelope text frame raw: %w", err)
			}
			s.counters.FramesForwarded.Add(1)
			continue
		}

		// Parse the JSON envelope. On malformed object-shaped JSON we
		// degrade the rest of the session to byte-copy mode rather than
		// block traffic on a schema change. Non-object text frames were
		// forwarded above because Codex may use them as legal protocol
		// sentinels that Phase-F cannot mutate.
		env, err := Parse(frame.Payload)
		if err != nil {
			s.counters.ParseFailures.Add(1)
			s.counters.Degraded.Store(true)
			if _, werr := dst.Write(frame.Raw); werr != nil {
				return fmt.Errorf("write frame raw after parse failure: %w", werr)
			}
			s.counters.FramesForwarded.Add(1)
			continue
		}

		replace, herr := handler(ctx, dir, &env)
		if herr != nil {
			return fmt.Errorf("handler: %w", herr)
		}

		if !replace {
			// Handler observed but did not mutate; forward original bytes.
			if _, err := dst.Write(frame.Raw); err != nil {
				return fmt.Errorf("write frame raw: %w", err)
			}
			s.counters.FramesForwarded.Add(1)
			continue
		}

		// Re-encode the envelope and write a fresh frame. The bridge
		// always emits server→client without a mask (per RFC) and
		// client→server WITHOUT mask (we are speaking for the client
		// to the upstream after TLS termination - upstream sees us as
		// the client, so we must mask. But because we have already
		// terminated TLS on the client side and accepted unmasked
		// frames from Codex, masking on the outbound direction is the
		// caller's call: the Codex client originally masked, so we
		// preserve that by re-using the original mask shape).
		newPayload, err := env.Marshal()
		if err != nil {
			return fmt.Errorf("re-marshal envelope: %w", err)
		}
		var maskKey []byte
		if frame.Masked {
			// Reuse the frame's original mask key when we have it.
			// ReadFrame strips the mask from the Payload but doesn't
			// retain the key separately; we reconstruct a fresh
			// random key. RFC 6455 §5.3 requires per-frame fresh keys.
			maskKey = freshMaskKey()
		}
		n, err := wscompact.WriteFrame(dst, frame.Fin,
			wscompact.WSOpcode(frame.Opcode), maskKey, newPayload)
		if err != nil {
			return fmt.Errorf("write re-encoded frame: %w", err)
		}
		_ = n
		s.counters.FramesReencoded.Add(1)
	}
}

func (s *Session) countFrame(dir Direction, n int) {
	switch dir {
	case DirClientToServer:
		s.counters.C2SFrames.Add(1)
		s.counters.C2SBytes.Add(int64(n))
	case DirServerToClient:
		s.counters.S2CFrames.Add(1)
		s.counters.S2CBytes.Add(int64(n))
	}
}

// maskKeySource is overrideable by tests. Production uses crypto/rand
// to satisfy RFC 6455 §5.3 (fresh, unpredictable mask per frame).
var maskKeySource = func(buf []byte) (int, error) { return rand.Read(buf) }

func freshMaskKey() []byte {
	buf := make([]byte, 4)
	_, _ = maskKeySource(buf)
	return buf
}

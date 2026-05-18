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

var (
	compressedMessagePayloadLimitBytes = 16 << 20
	inflatedMessagePayloadLimitBytes   = 64 << 20
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
	// Extensions carries the negotiated WebSocket extension profile for
	// this session. Zero-value means extension passthrough only.
	Extensions wscompact.WSExtensionProfile
	// Telemetry counters.
	counters SessionCounters
}

// SessionCounters tracks per-session frame and degradation events.
type SessionCounters struct {
	C2SFrames                   atomic.Int64
	S2CFrames                   atomic.Int64
	C2SBytes                    atomic.Int64
	S2CBytes                    atomic.Int64
	ParseFailures               atomic.Int64
	Degraded                    atomic.Bool
	FramesReencoded             atomic.Int64
	FramesForwarded             atomic.Int64
	CompressedMessagesInspected atomic.Int64
	CompressedMessagesMutated   atomic.Int64
	CompressedMessagesBypassed  atomic.Int64
	CompressionErrors           atomic.Int64
}

// Snapshot returns a value-copy of the counters for telemetry.
func (s *Session) Snapshot() SessionTelemetry {
	return SessionTelemetry{
		C2SFrames:                   s.counters.C2SFrames.Load(),
		S2CFrames:                   s.counters.S2CFrames.Load(),
		C2SBytes:                    s.counters.C2SBytes.Load(),
		S2CBytes:                    s.counters.S2CBytes.Load(),
		ParseFailures:               s.counters.ParseFailures.Load(),
		Degraded:                    s.counters.Degraded.Load(),
		FramesReencoded:             s.counters.FramesReencoded.Load(),
		FramesForwarded:             s.counters.FramesForwarded.Load(),
		CompressedMessagesInspected: s.counters.CompressedMessagesInspected.Load(),
		CompressedMessagesMutated:   s.counters.CompressedMessagesMutated.Load(),
		CompressedMessagesBypassed:  s.counters.CompressedMessagesBypassed.Load(),
		CompressionErrors:           s.counters.CompressionErrors.Load(),
	}
}

// SessionTelemetry is the read-only view of session counters.
type SessionTelemetry struct {
	C2SFrames                   int64 `json:"c2s_frames"`
	S2CFrames                   int64 `json:"s2c_frames"`
	C2SBytes                    int64 `json:"c2s_bytes"`
	S2CBytes                    int64 `json:"s2c_bytes"`
	ParseFailures               int64 `json:"parse_failures"`
	Degraded                    bool  `json:"degraded"`
	FramesReencoded             int64 `json:"frames_reencoded"`
	FramesForwarded             int64 `json:"frames_forwarded"`
	CompressedMessagesInspected int64 `json:"compressed_messages_inspected"`
	CompressedMessagesMutated   int64 `json:"compressed_messages_mutated"`
	CompressedMessagesBypassed  int64 `json:"compressed_messages_bypassed"`
	CompressionErrors           int64 `json:"compression_errors"`
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
	compressed := newCompressedMessageState(s.Extensions, dir)
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

		if compressed.pending() || isDataContinuation(frame) {
			if err := s.handleContinuationFrame(ctx, compressed, frame, dst, handler); err != nil {
				return err
			}
			continue
		}

		if compressed.canHandle(frame) {
			if err := s.handleCompressedFrame(ctx, compressed, frame, dst, handler); err != nil {
				return err
			}
			continue
		}

		// Pass-through for frames that Phase-F cannot safely mutate in
		// their current wire shape. RSV frames are usually compressed
		// by negotiated WebSocket extensions (e.g. permessage-deflate);
		// they need extension-aware decode/re-encode before mutation.
		if frame.Opcode != byte(wscompact.OpcodeText) || frame.RSV ||
			s.counters.Degraded.Load() || handler == nil || len(frame.Payload) == 0 {
			if frame.Opcode == byte(wscompact.OpcodeText) && frame.RSV1 && !frame.RSV2 && !frame.RSV3 {
				s.counters.CompressedMessagesBypassed.Add(1)
			}
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

		// Re-encode the envelope and preserve the original mask shape:
		// client-origin frames stay masked when forwarded to upstream;
		// server-origin frames stay unmasked when forwarded to Codex.
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

func (s *Session) handleContinuationFrame(ctx context.Context, compressed *compressedMessageState,
	frame wscompact.Frame, dst io.Writer, handler FrameHandler) error {
	if !compressed.pending() || frame.Opcode != byte(wscompact.OpcodeContinuation) {
		if _, err := dst.Write(frame.Raw); err != nil {
			return fmt.Errorf("write frame raw: %w", err)
		}
		s.counters.FramesForwarded.Add(1)
		return nil
	}
	compressed.add(frame)
	if compressed.payloadTooLarge() {
		compressed.blocked = true
		s.counters.CompressionErrors.Add(1)
		s.counters.CompressedMessagesBypassed.Add(1)
		return s.forwardFrames(dst, compressed.take(), "write compressed frame raw after size cap")
	}
	if !frame.Fin {
		return nil
	}
	return s.finishCompressedMessage(ctx, compressed, dst, handler)
}

func (s *Session) handleCompressedFrame(ctx context.Context, compressed *compressedMessageState,
	frame wscompact.Frame, dst io.Writer, handler FrameHandler) error {
	compressed.start(frame)
	if compressed.payloadTooLarge() {
		compressed.blocked = true
		s.counters.CompressionErrors.Add(1)
		s.counters.CompressedMessagesBypassed.Add(1)
		return s.forwardFrames(dst, compressed.take(), "write compressed frame raw after size cap")
	}
	if !frame.Fin {
		return nil
	}
	return s.finishCompressedMessage(ctx, compressed, dst, handler)
}

func (s *Session) finishCompressedMessage(ctx context.Context, compressed *compressedMessageState,
	dst io.Writer, handler FrameHandler) error {
	frames := compressed.take()
	payload := joinPayloads(frames)
	plain, err := compressed.inflate.InflateWithLimit(payload, inflatedMessagePayloadLimitBytes)
	if err != nil {
		compressed.blocked = true
		s.counters.CompressionErrors.Add(1)
		return s.forwardFrames(dst, frames, "write compressed frame raw after inflate failure")
	}
	s.counters.CompressedMessagesInspected.Add(1)
	if handler == nil || len(plain) == 0 || !looksLikeJSONObject(plain) {
		if err := compressed.deflate.Observe(plain); err != nil {
			compressed.blocked = true
			s.counters.CompressionErrors.Add(1)
		}
		return s.forwardFrames(dst, frames, "write compressed non-envelope frame raw")
	}
	env, err := Parse(plain)
	if err != nil {
		s.counters.ParseFailures.Add(1)
		s.counters.Degraded.Store(true)
		return s.forwardFrames(dst, frames, "write compressed frame raw after parse failure")
	}
	replace, herr := handler(ctx, compressed.dir, &env)
	if herr != nil {
		return fmt.Errorf("handler: %w", herr)
	}
	if !replace {
		if err := compressed.deflate.Observe(plain); err != nil {
			compressed.blocked = true
			s.counters.CompressionErrors.Add(1)
		}
		return s.forwardFrames(dst, frames, "write compressed frame raw")
	}
	newPayload, err := env.Marshal()
	if err != nil {
		return fmt.Errorf("re-marshal compressed envelope: %w", err)
	}
	wirePayload, err := compressed.deflate.Deflate(newPayload)
	if err != nil {
		compressed.blocked = true
		s.counters.CompressionErrors.Add(1)
		return s.forwardFrames(dst, frames, "write compressed frame raw after deflate failure")
	}
	written, err := writeCompressedDataFrames(dst, frames, wirePayload)
	if err != nil {
		return fmt.Errorf("write re-encoded compressed frame: %w", err)
	}
	s.counters.CompressedMessagesMutated.Add(1)
	s.counters.FramesReencoded.Add(int64(written))
	return nil
}

func (s *Session) forwardFrames(dst io.Writer, frames []wscompact.Frame, context string) error {
	for _, frame := range frames {
		if _, err := dst.Write(frame.Raw); err != nil {
			return fmt.Errorf("%s: %w", context, err)
		}
		s.counters.FramesForwarded.Add(1)
	}
	return nil
}

type compressedMessageState struct {
	dir          Direction
	enabled      bool
	blocked      bool
	inflate      *wscompact.InflateContext
	deflate      *wscompact.DeflateContext
	fragments    []wscompact.Frame
	payloadBytes int
}

func newCompressedMessageState(profile wscompact.WSExtensionProfile, dir Direction) *compressedMessageState {
	if !profile.Supported || !profile.PermessageDeflate {
		return &compressedMessageState{dir: dir}
	}
	noContextTakeover := profile.ClientNoContextTakeover
	if dir == DirServerToClient {
		noContextTakeover = profile.ServerNoContextTakeover
	}
	return &compressedMessageState{
		dir:     dir,
		enabled: true,
		inflate: wscompact.NewInflateContext(noContextTakeover),
		deflate: wscompact.NewDeflateContext(noContextTakeover),
	}
}

func (s *compressedMessageState) canHandle(frame wscompact.Frame) bool {
	return s != nil && s.enabled && !s.blocked &&
		frame.Opcode == byte(wscompact.OpcodeText) && frame.RSV1 && !frame.RSV2 && !frame.RSV3
}

func (s *compressedMessageState) pending() bool {
	return s != nil && len(s.fragments) > 0
}

func (s *compressedMessageState) start(frame wscompact.Frame) {
	s.fragments = append(s.fragments[:0], frame)
	s.payloadBytes = len(frame.Payload)
}

func (s *compressedMessageState) add(frame wscompact.Frame) {
	s.fragments = append(s.fragments, frame)
	s.payloadBytes += len(frame.Payload)
}

func (s *compressedMessageState) take() []wscompact.Frame {
	out := append([]wscompact.Frame(nil), s.fragments...)
	s.fragments = s.fragments[:0]
	s.payloadBytes = 0
	return out
}

func (s *compressedMessageState) payloadTooLarge() bool {
	return compressedMessagePayloadLimitBytes > 0 && s.payloadBytes > compressedMessagePayloadLimitBytes
}

func isDataContinuation(frame wscompact.Frame) bool {
	return frame.Opcode == byte(wscompact.OpcodeContinuation)
}

func joinPayloads(frames []wscompact.Frame) []byte {
	var total int
	for _, frame := range frames {
		total += len(frame.Payload)
	}
	out := make([]byte, 0, total)
	for _, frame := range frames {
		out = append(out, frame.Payload...)
	}
	return out
}

func writeCompressedDataFrames(dst io.Writer, original []wscompact.Frame, payload []byte) (int, error) {
	if len(original) <= 1 {
		_, err := writeCompressedFrame(dst, original[0], true,
			wscompact.OpcodeText, payload, true)
		if err != nil {
			return 0, err
		}
		return 1, nil
	}
	offset := 0
	writtenFrames := 0
	for i, frame := range original {
		remainingFrames := len(original) - i
		remainingBytes := len(payload) - offset
		size := 0
		if remainingFrames == 1 {
			size = remainingBytes
		} else if remainingBytes > 0 {
			size = len(frame.Payload)
			if size > remainingBytes {
				size = remainingBytes
			}
		}
		chunk := payload[offset : offset+size]
		offset += size
		opcode := wscompact.OpcodeContinuation
		if i == 0 {
			opcode = wscompact.OpcodeText
		}
		n, err := writeCompressedFrame(dst, frame, i == len(original)-1,
			opcode, chunk, i == 0)
		if err != nil {
			return writtenFrames, err
		}
		_ = n
		writtenFrames++
	}
	return writtenFrames, nil
}

func writeCompressedFrame(dst io.Writer, original wscompact.Frame, fin bool,
	opcode wscompact.WSOpcode, payload []byte, rsv1 bool) (int, error) {
	var maskKey []byte
	if original.Masked {
		maskKey = freshMaskKey()
	}
	return wscompact.WriteFrameWithOptions(dst, wscompact.WriteFrameOptions{
		Fin:     fin,
		Opcode:  opcode,
		MaskKey: maskKey,
		Payload: payload,
		RSV1:    rsv1,
	})
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

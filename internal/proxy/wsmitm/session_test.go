package wsmitm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/wscompact"
)

// duplexBuffer connects two pipe ends; each side reads from one
// buffer and writes to the other. Used to fake the client/upstream
// I/O without real TCP.
type duplexBuffer struct {
	in  *blockingPipe // bytes the OWNER reads from
	out *blockingPipe // bytes the OWNER writes to
}

func (d *duplexBuffer) Read(p []byte) (int, error)  { return d.in.Read(p) }
func (d *duplexBuffer) Write(p []byte) (int, error) { return d.out.Write(p) }

func newDuplexPair() (*duplexBuffer, *duplexBuffer) {
	a := newBlockingPipe()
	b := newBlockingPipe()
	left := &duplexBuffer{in: a, out: b}
	right := &duplexBuffer{in: b, out: a}
	return left, right
}

// blockingPipe is an in-memory pipe with Close() semantics that
// returns io.EOF on subsequent reads after close.
type blockingPipe struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    bytes.Buffer
	closed bool
}

func newBlockingPipe() *blockingPipe {
	p := &blockingPipe{}
	p.cond = sync.NewCond(&p.mu)
	return p
}

func (p *blockingPipe) Read(out []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for p.buf.Len() == 0 && !p.closed {
		p.cond.Wait()
	}
	if p.buf.Len() == 0 {
		return 0, io.EOF
	}
	return p.buf.Read(out)
}

func (p *blockingPipe) Write(in []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0, io.ErrClosedPipe
	}
	n, err := p.buf.Write(in)
	p.cond.Broadcast()
	return n, err
}

func (p *blockingPipe) Close() {
	p.mu.Lock()
	p.closed = true
	p.cond.Broadcast()
	p.mu.Unlock()
}

func writeTextFrame(t *testing.T, w io.Writer, payload string) {
	t.Helper()
	if _, err := wscompact.WriteFrame(w, true, wscompact.OpcodeText, nil, []byte(payload)); err != nil {
		t.Fatal(err)
	}
}

func compressedTextFrame(t *testing.T, deflater *wscompact.DeflateContext, payload string) []byte {
	t.Helper()
	compressed, err := deflater.Deflate([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := wscompact.WriteFrameWithOptions(&out, wscompact.WriteFrameOptions{
		Fin:     true,
		Opcode:  wscompact.OpcodeText,
		Payload: compressed,
		RSV1:    true,
	}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func testDeflateProfile(noContext bool) wscompact.WSExtensionProfile {
	return wscompact.WSExtensionProfile{
		PermessageDeflate:       true,
		Supported:               true,
		ClientNoContextTakeover: noContext,
		ServerNoContextTakeover: noContext,
	}
}

// readOneTextFrame reads a single text frame from r and returns its
// payload + Opcode. Helper for assertions.
func readOneTextFrame(t *testing.T, r io.Reader) (string, byte) {
	t.Helper()
	f, err := wscompact.ReadFrame(r)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	return string(f.Payload), f.Opcode
}

func TestSessionServeRejectsNilEnds(t *testing.T) {
	s := &Session{}
	if err := s.Serve(context.Background()); err == nil {
		t.Errorf("expected error on nil endpoints")
	}
}

func TestSessionForwardsTextFrameWithoutMutation(t *testing.T) {
	client, clientPeer := newDuplexPair()
	upstream, upstreamPeer := newDuplexPair()

	session := &Session{
		Client: client, Upstream: upstream,
		// No handlers - everything byte-equal forwarded.
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- session.Serve(ctx) }()

	writeTextFrame(t, clientPeer, `{"type":"request","data":1}`)
	gotPayload, op := readOneTextFrame(t, upstreamPeer)
	if op != byte(wscompact.OpcodeText) {
		t.Errorf("opcode=%d", op)
	}
	if gotPayload != `{"type":"request","data":1}` {
		t.Errorf("payload mutated: %q", gotPayload)
	}

	cancel()
	closeAll(client, clientPeer, upstream, upstreamPeer)
	<-done

	snap := session.Snapshot()
	if snap.C2SFrames != 1 {
		t.Errorf("C2S=%d", snap.C2SFrames)
	}
	if snap.FramesForwarded < 1 {
		t.Errorf("expected at least 1 forwarded, got %d", snap.FramesForwarded)
	}
}

func TestSessionClientHandlerReplacesPayload(t *testing.T) {
	client, clientPeer := newDuplexPair()
	upstream, upstreamPeer := newDuplexPair()

	session := &Session{
		Client: client, Upstream: upstream,
		ClientHandler: func(_ context.Context, _ Direction, env *Envelope) (bool, error) {
			// Replace ItemID to prove the re-encode path fires.
			env.ItemID = "rewritten"
			return true, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- session.Serve(ctx) }()

	writeTextFrame(t, clientPeer, `{"type":"request","item_id":"original"}`)
	got, _ := readOneTextFrame(t, upstreamPeer)
	if got == `{"type":"request","item_id":"original"}` {
		t.Errorf("payload not rewritten: %q", got)
	}
	if !contains(got, "rewritten") {
		t.Errorf("rewritten id not in output: %q", got)
	}

	cancel()
	closeAll(client, clientPeer, upstream, upstreamPeer)
	<-done

	if session.Snapshot().FramesReencoded != 1 {
		t.Errorf("FramesReencoded should be 1, got %d", session.Snapshot().FramesReencoded)
	}
}

func TestSessionUpstreamHandlerSeesS2CDirection(t *testing.T) {
	client, clientPeer := newDuplexPair()
	upstream, upstreamPeer := newDuplexPair()

	seen := make(chan Direction, 5)
	session := &Session{
		Client: client, Upstream: upstream,
		UpstreamHandler: func(_ context.Context, dir Direction, _ *Envelope) (bool, error) {
			seen <- dir
			return false, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- session.Serve(ctx) }()

	writeTextFrame(t, upstreamPeer, `{"type":"response.created"}`)
	got := waitDir(t, seen)
	if got != DirServerToClient {
		t.Errorf("expected s2c, got %s", got)
	}

	cancel()
	closeAll(client, clientPeer, upstream, upstreamPeer)
	<-done
}

func TestSessionForwardsNonTextFramesByteEqual(t *testing.T) {
	client, clientPeer := newDuplexPair()
	upstream, upstreamPeer := newDuplexPair()

	calls := atomic.Int32{}
	session := &Session{
		Client: client, Upstream: upstream,
		ClientHandler: func(context.Context, Direction, *Envelope) (bool, error) {
			calls.Add(1)
			return true, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- session.Serve(ctx) }()

	// Ping frame - control frame, should pass through without handler call.
	if _, err := wscompact.WriteFrame(clientPeer, true, wscompact.OpcodePing, nil, nil); err != nil {
		t.Fatal(err)
	}
	gotFrame, err := wscompact.ReadFrame(upstreamPeer)
	if err != nil {
		t.Fatal(err)
	}
	if gotFrame.Opcode != byte(wscompact.OpcodePing) {
		t.Errorf("opcode=%d want ping", gotFrame.Opcode)
	}
	if calls.Load() != 0 {
		t.Errorf("handler called %d times for ping frame", calls.Load())
	}

	cancel()
	closeAll(client, clientPeer, upstream, upstreamPeer)
	<-done
}

func TestSessionForwardsNonEnvelopeTextWithoutDegrading(t *testing.T) {
	client, clientPeer := newDuplexPair()
	upstream, upstreamPeer := newDuplexPair()

	calls := atomic.Int32{}
	session := &Session{
		Client: client, Upstream: upstream,
		ClientHandler: func(context.Context, Direction, *Envelope) (bool, error) {
			calls.Add(1)
			return true, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- session.Serve(ctx) }()

	// Codex may send legal text frames that are not JSON envelopes
	// (sentinels, extension payloads, etc.). They are not Phase-F
	// mutation candidates and must not degrade the whole session.
	writeTextFrame(t, clientPeer, `not json at all`)
	got, _ := readOneTextFrame(t, upstreamPeer)
	if got != `not json at all` {
		t.Errorf("non-envelope frame mutated: %q", got)
	}

	writeTextFrame(t, clientPeer, `{"type":"request"}`)
	_, _ = readOneTextFrame(t, upstreamPeer)

	cancel()
	closeAll(client, clientPeer, upstream, upstreamPeer)
	<-done

	snap := session.Snapshot()
	if snap.Degraded {
		t.Errorf("Degraded=true after non-envelope text")
	}
	if snap.ParseFailures != 0 {
		t.Errorf("ParseFailures=%d", snap.ParseFailures)
	}
	if calls.Load() != 1 {
		t.Errorf("handler calls=%d want 1", calls.Load())
	}
}

func TestSessionForwardsRSVTextWithoutDegrading(t *testing.T) {
	client, clientPeer := newDuplexPair()
	upstream, upstreamPeer := newDuplexPair()

	calls := atomic.Int32{}
	session := &Session{
		Client: client, Upstream: upstream,
		ClientHandler: func(context.Context, Direction, *Envelope) (bool, error) {
			calls.Add(1)
			return true, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- session.Serve(ctx) }()

	// FIN + RSV1 + text, payload "xyz". This models a permessage-
	// deflate frame without requiring a full extension implementation.
	if _, err := clientPeer.Write([]byte{0xc1, 0x03, 'x', 'y', 'z'}); err != nil {
		t.Fatal(err)
	}
	got, err := wscompact.ReadFrame(upstreamPeer)
	if err != nil {
		t.Fatal(err)
	}
	if !got.RSV {
		t.Fatalf("RSV bit was not preserved in raw forward: %+v", got)
	}
	if string(got.Payload) != "xyz" {
		t.Fatalf("payload changed: %q", got.Payload)
	}

	writeTextFrame(t, clientPeer, `{"type":"request"}`)
	_, _ = readOneTextFrame(t, upstreamPeer)

	cancel()
	closeAll(client, clientPeer, upstream, upstreamPeer)
	<-done

	snap := session.Snapshot()
	if snap.Degraded {
		t.Errorf("Degraded=true after RSV text")
	}
	if snap.ParseFailures != 0 {
		t.Errorf("ParseFailures=%d", snap.ParseFailures)
	}
	if snap.CompressedMessagesBypassed != 1 {
		t.Errorf("CompressedMessagesBypassed=%d want 1", snap.CompressedMessagesBypassed)
	}
	if snap.CompressedMessagesInspected != 0 {
		t.Errorf("CompressedMessagesInspected=%d want 0", snap.CompressedMessagesInspected)
	}
	if calls.Load() != 1 {
		t.Errorf("handler calls=%d want 1", calls.Load())
	}
}

func TestSessionDecompressesAndMutatesNoContextTakeover(t *testing.T) {
	client, clientPeer := newDuplexPair()
	upstream, upstreamPeer := newDuplexPair()

	session := &Session{
		Client:     client,
		Upstream:   upstream,
		Extensions: testDeflateProfile(true),
		ClientHandler: func(_ context.Context, _ Direction, env *Envelope) (bool, error) {
			env.ItemID = "rewritten"
			return true, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- session.Serve(ctx) }()

	sourceDeflater := wscompact.NewDeflateContext(true)
	raw := compressedTextFrame(t, sourceDeflater, `{"type":"request","item_id":"original"}`)
	if _, err := clientPeer.Write(raw); err != nil {
		t.Fatal(err)
	}

	out, err := wscompact.ReadFrame(upstreamPeer)
	if err != nil {
		t.Fatal(err)
	}
	if !out.RSV1 {
		t.Fatalf("mutated compressed frame lost RSV1: %+v", out)
	}
	destinationInflater := wscompact.NewInflateContext(true)
	plain, err := destinationInflater.Inflate(out.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(plain), `"item_id":"rewritten"`) {
		t.Fatalf("mutation missing from decompressed payload: %s", plain)
	}

	cancel()
	closeAll(client, clientPeer, upstream, upstreamPeer)
	<-done

	snap := session.Snapshot()
	if got := snap.FramesReencoded; got != 1 {
		t.Fatalf("FramesReencoded=%d want 1", got)
	}
	if snap.CompressedMessagesInspected != 1 || snap.CompressedMessagesMutated != 1 || snap.CompressionErrors != 0 {
		t.Fatalf("unexpected compressed telemetry: %+v", snap)
	}
}

func TestSessionCompressedReplaceFalseKeepsContextForLaterMutation(t *testing.T) {
	client, clientPeer := newDuplexPair()
	upstream, upstreamPeer := newDuplexPair()

	session := &Session{
		Client:     client,
		Upstream:   upstream,
		Extensions: testDeflateProfile(false),
		ClientHandler: func(_ context.Context, _ Direction, env *Envelope) (bool, error) {
			if env.ItemID == "first" {
				return false, nil
			}
			env.ItemID = "second-mutated"
			return true, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- session.Serve(ctx) }()

	sourceDeflater := wscompact.NewDeflateContext(false)
	firstRaw := compressedTextFrame(t, sourceDeflater, `{"type":"request","item_id":"first","body":"shared-history-shared-history-shared-history"}`)
	secondRaw := compressedTextFrame(t, sourceDeflater, `{"type":"request","item_id":"second","body":"shared-history-tail"}`)

	if _, err := clientPeer.Write(firstRaw); err != nil {
		t.Fatal(err)
	}
	forwardedFirst := make([]byte, len(firstRaw))
	if _, err := io.ReadFull(upstreamPeer, forwardedFirst); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(forwardedFirst, firstRaw) {
		t.Fatalf("replace=false frame was not byte-equal")
	}

	firstFrame, err := wscompact.ReadFrame(bytes.NewReader(forwardedFirst))
	if err != nil {
		t.Fatal(err)
	}
	destinationInflater := wscompact.NewInflateContext(false)
	if _, err := destinationInflater.Inflate(firstFrame.Payload); err != nil {
		t.Fatal(err)
	}

	if _, err := clientPeer.Write(secondRaw); err != nil {
		t.Fatal(err)
	}
	secondFrame, err := wscompact.ReadFrame(upstreamPeer)
	if err != nil {
		t.Fatal(err)
	}
	plainSecond, err := destinationInflater.Inflate(secondFrame.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(plainSecond), `"item_id":"second-mutated"`) {
		t.Fatalf("mutated context payload missing: %s", plainSecond)
	}

	cancel()
	closeAll(client, clientPeer, upstream, upstreamPeer)
	<-done

	snap := session.Snapshot()
	if snap.ParseFailures != 0 || snap.Degraded {
		t.Fatalf("unexpected parser degradation: %+v", snap)
	}
	if snap.FramesReencoded != 1 {
		t.Fatalf("FramesReencoded=%d want 1", snap.FramesReencoded)
	}
	if snap.CompressedMessagesInspected != 2 || snap.CompressedMessagesMutated != 1 || snap.CompressionErrors != 0 {
		t.Fatalf("unexpected compressed telemetry: %+v", snap)
	}
}

func TestSessionCompressedNonObjectForwardedWithoutDegrading(t *testing.T) {
	client, clientPeer := newDuplexPair()
	upstream, upstreamPeer := newDuplexPair()

	calls := atomic.Int32{}
	session := &Session{
		Client:     client,
		Upstream:   upstream,
		Extensions: testDeflateProfile(true),
		ClientHandler: func(context.Context, Direction, *Envelope) (bool, error) {
			calls.Add(1)
			return true, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- session.Serve(ctx) }()

	sourceDeflater := wscompact.NewDeflateContext(true)
	raw := compressedTextFrame(t, sourceDeflater, `not an envelope`)
	if _, err := clientPeer.Write(raw); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(raw))
	if _, err := io.ReadFull(upstreamPeer, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("compressed non-object text was not byte-equal")
	}

	cancel()
	closeAll(client, clientPeer, upstream, upstreamPeer)
	<-done

	snap := session.Snapshot()
	if snap.Degraded || snap.ParseFailures != 0 || calls.Load() != 0 {
		t.Fatalf("unexpected non-object handling: snap=%+v calls=%d", snap, calls.Load())
	}
	if snap.CompressedMessagesInspected != 1 || snap.CompressedMessagesMutated != 0 || snap.CompressionErrors != 0 {
		t.Fatalf("unexpected compressed telemetry: %+v", snap)
	}
}

func TestSessionCompressedPayloadLimitBypassesAndBlocks(t *testing.T) {
	oldCompressedLimit := compressedMessagePayloadLimitBytes
	compressedMessagePayloadLimitBytes = 4
	t.Cleanup(func() { compressedMessagePayloadLimitBytes = oldCompressedLimit })

	client, clientPeer := newDuplexPair()
	upstream, upstreamPeer := newDuplexPair()

	calls := atomic.Int32{}
	session := &Session{
		Client:     client,
		Upstream:   upstream,
		Extensions: testDeflateProfile(true),
		ClientHandler: func(context.Context, Direction, *Envelope) (bool, error) {
			calls.Add(1)
			return true, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- session.Serve(ctx) }()

	sourceDeflater := wscompact.NewDeflateContext(true)
	raw := compressedTextFrame(t, sourceDeflater, `{"type":"request","item_id":"too-large"}`)
	if _, err := clientPeer.Write(raw); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(raw))
	if _, err := io.ReadFull(upstreamPeer, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("oversized compressed frame was not byte-equal")
	}

	second := compressedTextFrame(t, sourceDeflater, `{"type":"request","item_id":"blocked"}`)
	if _, err := clientPeer.Write(second); err != nil {
		t.Fatal(err)
	}
	gotSecond := make([]byte, len(second))
	if _, err := io.ReadFull(upstreamPeer, gotSecond); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotSecond, second) {
		t.Fatal("blocked compressed direction did not stay byte-equal")
	}

	cancel()
	closeAll(client, clientPeer, upstream, upstreamPeer)
	<-done

	snap := session.Snapshot()
	if snap.Degraded || snap.ParseFailures != 0 || calls.Load() != 0 {
		t.Fatalf("oversized compressed message should fail open: snap=%+v calls=%d", snap, calls.Load())
	}
	if snap.CompressionErrors != 1 || snap.CompressedMessagesInspected != 0 || snap.CompressedMessagesMutated != 0 {
		t.Fatalf("unexpected oversized compressed telemetry: %+v", snap)
	}
	if snap.CompressedMessagesBypassed != 2 {
		t.Fatalf("CompressedMessagesBypassed=%d want 2", snap.CompressedMessagesBypassed)
	}
}

func TestSessionInflatedPayloadLimitBypassesAndBlocks(t *testing.T) {
	oldInflatedLimit := inflatedMessagePayloadLimitBytes
	inflatedMessagePayloadLimitBytes = 8
	t.Cleanup(func() { inflatedMessagePayloadLimitBytes = oldInflatedLimit })

	client, clientPeer := newDuplexPair()
	upstream, upstreamPeer := newDuplexPair()

	calls := atomic.Int32{}
	session := &Session{
		Client:     client,
		Upstream:   upstream,
		Extensions: testDeflateProfile(true),
		ClientHandler: func(context.Context, Direction, *Envelope) (bool, error) {
			calls.Add(1)
			return true, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- session.Serve(ctx) }()

	sourceDeflater := wscompact.NewDeflateContext(true)
	raw := compressedTextFrame(t, sourceDeflater, `{"type":"request","item_id":"inflated-too-large"}`)
	if _, err := clientPeer.Write(raw); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(raw))
	if _, err := io.ReadFull(upstreamPeer, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("over-limit inflated frame was not byte-equal")
	}

	cancel()
	closeAll(client, clientPeer, upstream, upstreamPeer)
	<-done

	snap := session.Snapshot()
	if snap.Degraded || snap.ParseFailures != 0 || calls.Load() != 0 {
		t.Fatalf("inflated limit should fail open: snap=%+v calls=%d", snap, calls.Load())
	}
	if snap.CompressionErrors != 1 || snap.CompressedMessagesInspected != 0 || snap.CompressedMessagesMutated != 0 {
		t.Fatalf("unexpected inflated-limit telemetry: %+v", snap)
	}
}

func TestSessionFragmentedCompressedMessageWithInterleavedPing(t *testing.T) {
	client, clientPeer := newDuplexPair()
	upstream, upstreamPeer := newDuplexPair()

	session := &Session{
		Client:     client,
		Upstream:   upstream,
		Extensions: testDeflateProfile(true),
		ClientHandler: func(_ context.Context, _ Direction, env *Envelope) (bool, error) {
			env.ItemID = "fragment-mutated"
			return true, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- session.Serve(ctx) }()

	sourceDeflater := wscompact.NewDeflateContext(true)
	compressed, err := sourceDeflater.Deflate([]byte(`{"type":"request","item_id":"fragment-original"}`))
	if err != nil {
		t.Fatal(err)
	}
	cut := len(compressed) / 2
	if _, err := wscompact.WriteFrameWithOptions(clientPeer, wscompact.WriteFrameOptions{
		Fin:     false,
		Opcode:  wscompact.OpcodeText,
		Payload: compressed[:cut],
		RSV1:    true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := wscompact.WriteFrame(clientPeer, true, wscompact.OpcodePing, nil, []byte("p")); err != nil {
		t.Fatal(err)
	}
	if _, err := wscompact.WriteFrame(clientPeer, true, wscompact.OpcodeContinuation, nil, compressed[cut:]); err != nil {
		t.Fatal(err)
	}

	ping, err := wscompact.ReadFrame(upstreamPeer)
	if err != nil {
		t.Fatal(err)
	}
	if ping.Opcode != byte(wscompact.OpcodePing) {
		t.Fatalf("interleaved control frame not forwarded first: %+v", ping)
	}
	first, err := wscompact.ReadFrame(upstreamPeer)
	if err != nil {
		t.Fatal(err)
	}
	second, err := wscompact.ReadFrame(upstreamPeer)
	if err != nil {
		t.Fatal(err)
	}
	if !first.RSV1 || first.Fin || second.Opcode != byte(wscompact.OpcodeContinuation) || !second.Fin {
		t.Fatalf("fragment shape not preserved enough: first=%+v second=%+v", first, second)
	}
	destinationInflater := wscompact.NewInflateContext(true)
	plain, err := destinationInflater.Inflate(append(append([]byte(nil), first.Payload...), second.Payload...))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(plain), `"item_id":"fragment-mutated"`) {
		t.Fatalf("fragment mutation missing: %s", plain)
	}

	cancel()
	closeAll(client, clientPeer, upstream, upstreamPeer)
	<-done

	snap := session.Snapshot()
	if snap.CompressedMessagesInspected != 1 || snap.CompressedMessagesMutated != 1 || snap.FramesReencoded != 2 {
		t.Fatalf("unexpected fragmented compressed telemetry: %+v", snap)
	}
}

func TestSessionDecompressedMalformedObjectJSONDegrades(t *testing.T) {
	client, clientPeer := newDuplexPair()
	upstream, upstreamPeer := newDuplexPair()

	session := &Session{
		Client:     client,
		Upstream:   upstream,
		Extensions: testDeflateProfile(true),
		ClientHandler: func(context.Context, Direction, *Envelope) (bool, error) {
			return true, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- session.Serve(ctx) }()

	sourceDeflater := wscompact.NewDeflateContext(true)
	raw := compressedTextFrame(t, sourceDeflater, `{"type":`)
	if _, err := clientPeer.Write(raw); err != nil {
		t.Fatal(err)
	}
	forwarded := make([]byte, len(raw))
	if _, err := io.ReadFull(upstreamPeer, forwarded); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(forwarded, raw) {
		t.Fatal("malformed compressed frame not forwarded byte-equal")
	}

	cancel()
	closeAll(client, clientPeer, upstream, upstreamPeer)
	<-done

	snap := session.Snapshot()
	if !snap.Degraded || snap.ParseFailures != 1 {
		t.Fatalf("expected compressed malformed degrade: %+v", snap)
	}
	if snap.CompressedMessagesInspected != 1 || snap.CompressionErrors != 0 {
		t.Fatalf("unexpected compressed malformed telemetry: %+v", snap)
	}
}

func TestSessionDegradesOnMalformedEnvelopeObject(t *testing.T) {
	client, clientPeer := newDuplexPair()
	upstream, upstreamPeer := newDuplexPair()

	calls := atomic.Int32{}
	session := &Session{
		Client: client, Upstream: upstream,
		ClientHandler: func(context.Context, Direction, *Envelope) (bool, error) {
			calls.Add(1)
			return true, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- session.Serve(ctx) }()

	writeTextFrame(t, clientPeer, `{"type":`)
	got, _ := readOneTextFrame(t, upstreamPeer)
	if got != `{"type":` {
		t.Errorf("malformed envelope mutated: %q", got)
	}

	// Subsequent frames should now skip the handler (degraded mode).
	writeTextFrame(t, clientPeer, `{"type":"request"}`)
	_, _ = readOneTextFrame(t, upstreamPeer)

	cancel()
	closeAll(client, clientPeer, upstream, upstreamPeer)
	<-done

	snap := session.Snapshot()
	if !snap.Degraded {
		t.Errorf("expected Degraded=true")
	}
	if snap.ParseFailures != 1 {
		t.Errorf("ParseFailures=%d", snap.ParseFailures)
	}
	if calls.Load() != 0 {
		t.Errorf("handler should not have been called after malformed envelope")
	}
}

func TestSessionHandlerErrorClosesSession(t *testing.T) {
	client, clientPeer := newDuplexPair()
	upstream, upstreamPeer := newDuplexPair()

	session := &Session{
		Client: client, Upstream: upstream,
		ClientHandler: func(context.Context, Direction, *Envelope) (bool, error) {
			return false, fmt.Errorf("boom")
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- session.Serve(ctx) }()

	writeTextFrame(t, clientPeer, `{"type":"request"}`)

	select {
	case err := <-done:
		if err == nil || !contains(err.Error(), "handler") {
			t.Errorf("expected handler error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("session did not close on handler error")
	}
	closeAll(client, clientPeer, upstream, upstreamPeer)
}

func TestSessionContextCancelEnds(t *testing.T) {
	client, clientPeer := newDuplexPair()
	upstream, upstreamPeer := newDuplexPair()
	session := &Session{Client: client, Upstream: upstream}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- session.Serve(ctx) }()
	cancel()
	closeAll(client, clientPeer, upstream, upstreamPeer)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("session did not exit after context cancel")
	}
}

func TestSessionEmptyTextFrameForwarded(t *testing.T) {
	client, clientPeer := newDuplexPair()
	upstream, upstreamPeer := newDuplexPair()
	calls := atomic.Int32{}
	session := &Session{
		Client: client, Upstream: upstream,
		ClientHandler: func(context.Context, Direction, *Envelope) (bool, error) {
			calls.Add(1)
			return true, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- session.Serve(ctx) }()

	// Zero-length text frame - handler should NOT fire (we skip
	// empty payloads to avoid parsing empty bytes).
	writeTextFrame(t, clientPeer, "")
	_, _ = wscompact.ReadFrame(upstreamPeer)

	cancel()
	closeAll(client, clientPeer, upstream, upstreamPeer)
	<-done

	if calls.Load() != 0 {
		t.Errorf("handler fired on empty payload: %d", calls.Load())
	}
}

func TestSessionEOFEndsCleanly(t *testing.T) {
	client, clientPeer := newDuplexPair()
	upstream, upstreamPeer := newDuplexPair()
	session := &Session{Client: client, Upstream: upstream}
	done := make(chan error, 1)
	go func() { done <- session.Serve(context.Background()) }()

	// Closing both client peer and upstream peer pipes ends both
	// directions.
	closeAll(client, clientPeer, upstream, upstreamPeer)
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("clean EOF should yield nil, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("session did not close on EOF")
	}
}

func TestSessionSnapshotZeroByDefault(t *testing.T) {
	s := &Session{}
	if snap := s.Snapshot(); snap.C2SFrames != 0 || snap.Degraded {
		t.Errorf("fresh session not zero: %+v", snap)
	}
}

func TestFreshMaskKeyIsFourBytes(t *testing.T) {
	if k := freshMaskKey(); len(k) != 4 {
		t.Errorf("mask key len=%d", len(k))
	}
}

func TestFreshMaskKeyDeterministicWithOverride(t *testing.T) {
	old := maskKeySource
	defer func() { maskKeySource = old }()
	maskKeySource = func(buf []byte) (int, error) {
		for i := range buf {
			buf[i] = 0xab
		}
		return len(buf), nil
	}
	k := freshMaskKey()
	if !bytes.Equal(k, []byte{0xab, 0xab, 0xab, 0xab}) {
		t.Errorf("override not honoured: %x", k)
	}
}

func TestSessionMaskedReencode(t *testing.T) {
	// Client sends a masked frame; bridge re-encodes and re-masks.
	client, clientPeer := newDuplexPair()
	upstream, upstreamPeer := newDuplexPair()

	old := maskKeySource
	defer func() { maskKeySource = old }()
	maskKeySource = func(buf []byte) (int, error) {
		for i := range buf {
			buf[i] = 0x11
		}
		return 4, nil
	}

	session := &Session{
		Client: client, Upstream: upstream,
		ClientHandler: func(_ context.Context, _ Direction, env *Envelope) (bool, error) {
			env.ItemID = "x"
			return true, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- session.Serve(ctx) }()

	mask := []byte{0xaa, 0xbb, 0xcc, 0xdd}
	if _, err := wscompact.WriteFrame(clientPeer, true, wscompact.OpcodeText,
		mask, []byte(`{"type":"request"}`)); err != nil {
		t.Fatal(err)
	}
	out, err := wscompact.ReadFrame(upstreamPeer)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Masked {
		t.Errorf("re-encoded frame lost mask flag")
	}
	if !contains(string(out.Payload), `"item_id":"x"`) {
		t.Errorf("mutation not visible after re-encode: %q", out.Payload)
	}

	cancel()
	closeAll(client, clientPeer, upstream, upstreamPeer)
	<-done
}

func TestSessionReadFrameErrorPropagates(t *testing.T) {
	// Closed client pipe → ReadFrame returns EOF on next read, which
	// is ErrSessionClosed (normal). Use a corrupt frame instead.
	r := bytes.NewReader([]byte{0x80}) // truncated header
	client := &fakeRW{r: r}
	upstream, upstreamPeer := newDuplexPair()
	session := &Session{Client: client, Upstream: upstream}
	done := make(chan error, 1)
	go func() { done <- session.Serve(context.Background()) }()

	select {
	case err := <-done:
		if err == nil {
			t.Errorf("expected read error to propagate")
		}
	case <-time.After(time.Second):
		t.Fatal("session did not exit on read error")
	}
	closeAll(upstream, upstreamPeer)
}

func TestSessionWriteFrameErrorPropagates(t *testing.T) {
	// Forwards return write error on dst.Write of frame.Raw.
	// Upstream Read blocks (never returns EOF) so only the C2S
	// direction can error - the test then sees that error.
	client, clientPeer := newDuplexPair()
	blockPipe := newBlockingPipe()
	upstream := &fakeRW{
		r: blockPipe,
		w: failingDst{err: errors.New("write fail")},
	}
	t.Cleanup(blockPipe.Close)
	session := &Session{Client: client, Upstream: upstream}
	done := make(chan error, 1)
	go func() { done <- session.Serve(context.Background()) }()

	writeTextFrame(t, clientPeer, `{"type":"request"}`)

	select {
	case err := <-done:
		if err == nil {
			t.Errorf("expected write error to propagate")
		}
	case <-time.After(time.Second):
		t.Fatal("session did not exit on write error")
	}
	closeAll(client, clientPeer)
}

func TestSessionParseFailureWriteErrorPropagates(t *testing.T) {
	client, clientPeer := newDuplexPair()
	blockPipe := newBlockingPipe()
	upstream := &fakeRW{
		r: blockPipe,
		w: failingDst{err: errors.New("write parse fail")},
	}
	t.Cleanup(blockPipe.Close)
	session := &Session{
		Client: client, Upstream: upstream,
		ClientHandler: func(context.Context, Direction, *Envelope) (bool, error) {
			return true, nil
		},
	}
	done := make(chan error, 1)
	go func() { done <- session.Serve(context.Background()) }()

	writeTextFrame(t, clientPeer, `{"type":`)
	select {
	case err := <-done:
		if err == nil || !contains(err.Error(), "parse failure") {
			t.Fatalf("expected parse-failure write error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("session did not exit on parse-failure write error")
	}
	closeAll(client, clientPeer)
}

func TestSessionNoReplaceWriteErrorPropagates(t *testing.T) {
	client, clientPeer := newDuplexPair()
	blockPipe := newBlockingPipe()
	upstream := &fakeRW{
		r: blockPipe,
		w: failingDst{err: errors.New("write raw fail")},
	}
	t.Cleanup(blockPipe.Close)
	session := &Session{
		Client: client, Upstream: upstream,
		ClientHandler: func(context.Context, Direction, *Envelope) (bool, error) {
			return false, nil
		},
	}
	done := make(chan error, 1)
	go func() { done <- session.Serve(context.Background()) }()

	writeTextFrame(t, clientPeer, `{"type":"request"}`)
	select {
	case err := <-done:
		if err == nil || !contains(err.Error(), "write frame raw") {
			t.Fatalf("expected raw write error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("session did not exit on raw write error")
	}
	closeAll(client, clientPeer)
}

func TestSessionMarshalAndReencodedWriteErrorsPropagate(t *testing.T) {
	t.Run("marshal", func(t *testing.T) {
		client, clientPeer := newDuplexPair()
		upstream, upstreamPeer := newDuplexPair()
		session := &Session{
			Client: client, Upstream: upstream,
			ClientHandler: func(_ context.Context, _ Direction, env *Envelope) (bool, error) {
				env.Fields["bad"] = []byte(`{`)
				return true, nil
			},
		}
		done := make(chan error, 1)
		go func() { done <- session.Serve(context.Background()) }()
		writeTextFrame(t, clientPeer, `{"type":"request"}`)
		select {
		case err := <-done:
			if err == nil || !contains(err.Error(), "re-marshal") {
				t.Fatalf("expected marshal error, got %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("session did not exit on marshal error")
		}
		closeAll(client, clientPeer, upstream, upstreamPeer)
	})

	t.Run("write", func(t *testing.T) {
		client, clientPeer := newDuplexPair()
		blockPipe := newBlockingPipe()
		upstream := &fakeRW{
			r: blockPipe,
			w: failingDst{err: errors.New("write reencoded fail")},
		}
		t.Cleanup(blockPipe.Close)
		session := &Session{
			Client: client, Upstream: upstream,
			ClientHandler: func(_ context.Context, _ Direction, env *Envelope) (bool, error) {
				env.ItemID = "changed"
				return true, nil
			},
		}
		done := make(chan error, 1)
		go func() { done <- session.Serve(context.Background()) }()
		writeTextFrame(t, clientPeer, `{"type":"request"}`)
		select {
		case err := <-done:
			if err == nil || !contains(err.Error(), "re-encoded") {
				t.Fatalf("expected re-encoded write error, got %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("session did not exit on re-encoded write error")
		}
		closeAll(client, clientPeer)
	})
}

// fakeRW combines an optional Reader and Writer.
type fakeRW struct {
	r io.Reader
	w io.Writer
}

func (f *fakeRW) Read(p []byte) (int, error) {
	if f.r == nil {
		return 0, io.EOF
	}
	return f.r.Read(p)
}
func (f *fakeRW) Write(p []byte) (int, error) {
	if f.w == nil {
		return len(p), nil
	}
	return f.w.Write(p)
}

type failingDst struct{ err error }

func (d failingDst) Write(p []byte) (int, error) { return 0, d.err }

// ---- helpers ----

func contains(haystack, needle string) bool {
	return bytes.Contains([]byte(haystack), []byte(needle))
}

func waitDir(t *testing.T, ch <-chan Direction) Direction {
	t.Helper()
	select {
	case d := <-ch:
		return d
	case <-time.After(time.Second):
		t.Fatal("direction not signalled")
	}
	return ""
}

// closeAll closes anything that has a Close method - duplexBuffers
// don't, but their underlying blockingPipes do.
func closeAll(ends ...*duplexBuffer) {
	for _, e := range ends {
		if e == nil {
			continue
		}
		e.in.Close()
		e.out.Close()
	}
}

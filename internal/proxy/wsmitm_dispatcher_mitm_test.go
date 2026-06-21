package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/sniroute"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
	"github.com/Christopher-Schulze/Slimference/internal/wscompact"
)

// wsFrameBytes returns RFC 6455 WS-framed bytes carrying payload as a
// text opcode, unmasked.
func wsFrameBytes(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if _, err := wscompact.WriteFrame(&buf, true, wscompact.OpcodeText, nil, payload); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	return buf.Bytes()
}

// wsmitm session reads newline-delimited JSON envelopes from each
// side. To exercise the dispatcher's MITMConversation path we feed
// real JSON envelopes through.

func TestMITMConversationRoutesThroughWSMITMSession(t *testing.T) {
	upstreamRemote, upstreamLocal := newPipe()

	d := &PhaseFDispatcher{
		UpstreamDial: func(_ context.Context, _ string) (net.Conn, error) {
			return upstreamLocal, nil
		},
	}
	clientRemote, clientLocal := newPipe()

	var bridgeErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		bridgeErr = d.Handle(context.Background(), sniroute.MITMConversation,
			sniroute.Request{SNI: "chatgpt.com"}, clientLocal)
	})

	// Send one JSON envelope wrapped in a WS text frame.
	raw := mustMarshal(map[string]string{"type": string(wsmitm.FrameKindRequest)})
	frameBytes := wsFrameBytes(t, raw)
	go func() {
		_, _ = clientRemote.Write(frameBytes)
	}()

	// Wait until the C2S pump has forwarded the frame to upstream
	// BEFORE we tear down the pipes. This avoids the race where the
	// S2C pump's EOF causes Serve() to return before C2S processes
	// the queued frame.
	if err := upstreamRemote.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	buf := make([]byte, len(frameBytes))
	if _, err := io.ReadFull(upstreamRemote, buf); err != nil {
		t.Fatalf("upstream read: %v", err)
	}

	// Now tear down: closing the upstream signals EOF to S2C; closing
	// client signals EOF to C2S (already done above via Write goroutine
	// exit, but explicit close releases the pipe properly).
	_ = upstreamRemote.Close()
	_ = clientRemote.Close()
	_ = clientLocal.Close()
	wg.Wait()
	_ = bridgeErr

	snap := d.Snapshot()
	if snap.MITMBridged != 1 {
		t.Errorf("MITMBridged=%d, want 1", snap.MITMBridged)
	}
	if snap.WSMITMC2SFrames < 1 {
		t.Errorf("WSMITMC2SFrames=%d, want >= 1", snap.WSMITMC2SFrames)
	}
}

func TestMITMConversationUsesRuntimeWSSCapture(t *testing.T) {
	upstreamRemote, upstreamLocal := newPipe()
	p := New(config.Defaults())
	capturePath := filepath.Join(t.TempDir(), "frames.jsonl")
	if _, err := p.SetWSSABCapture(capturePath, time.Hour); err != nil {
		t.Fatalf("set runtime capture: %v", err)
	}
	d := &PhaseFDispatcher{
		Proxy: p,
		UpstreamDial: func(_ context.Context, _ string) (net.Conn, error) {
			return upstreamLocal, nil
		},
	}
	clientRemote, clientLocal := newPipe()

	var wg sync.WaitGroup
	wg.Go(func() {
		_ = d.Handle(context.Background(), sniroute.MITMConversation,
			sniroute.Request{SNI: "chatgpt.com"}, clientLocal)
	})

	frameBytes := wsFrameBytes(t, mustMarshal(map[string]string{"type": string(wsmitm.FrameKindRequest)}))
	go func() { _, _ = clientRemote.Write(frameBytes) }()
	if err := upstreamRemote.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	buf := make([]byte, len(frameBytes))
	if _, err := io.ReadFull(upstreamRemote, buf); err != nil {
		t.Fatalf("upstream read: %v", err)
	}

	_ = upstreamRemote.Close()
	_ = clientRemote.Close()
	_ = clientLocal.Close()
	wg.Wait()

	data, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("runtime capture file missing: %v", err)
	}
	if !strings.Contains(string(data), `"direction":"c2s"`) || !strings.Contains(string(data), `"type":"request"`) {
		t.Fatalf("runtime capture did not record the WSS frame: %s", data)
	}
}

func TestMITMConversationParseFailureDegrades(t *testing.T) {
	upstreamRemote, upstreamLocal := newPipe()
	d := &PhaseFDispatcher{
		UpstreamDial: func(_ context.Context, _ string) (net.Conn, error) {
			return upstreamLocal, nil
		},
	}
	clientRemote, clientLocal := newPipe()

	var wg sync.WaitGroup
	wg.Go(func() {
		_ = d.Handle(context.Background(), sniroute.MITMConversation,
			sniroute.Request{SNI: "chatgpt.com"}, clientLocal)
	})

	// Send WS-framed garbage JSON (the frame is valid; payload is not).
	frameBytes := wsFrameBytes(t, []byte("garbage not json"))
	go func() { _, _ = clientRemote.Write(frameBytes) }()

	// Wait for upstream forwarding to prove the c2s pump ran.
	if err := upstreamRemote.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	buf := make([]byte, len(frameBytes))
	if _, err := io.ReadFull(upstreamRemote, buf); err != nil {
		t.Fatalf("upstream read: %v", err)
	}

	_ = upstreamRemote.Close()
	_ = clientRemote.Close()
	_ = clientLocal.Close()
	wg.Wait()

	snap := d.Snapshot()
	if snap.MITMBridged != 1 {
		t.Errorf("MITMBridged=%d, want 1", snap.MITMBridged)
	}
	if snap.WSMITMC2SFrames < 1 {
		t.Errorf("expected at least one frame counted, got %+v", snap)
	}
}

func TestDispatcherInitialHTTPUpgradeReroutesToWSMITM(t *testing.T) {
	upstreamRemote, upstreamLocal := newPipe()
	d := &PhaseFDispatcher{
		Resolver: sniroute.New(nil),
		UpstreamDial: func(_ context.Context, _ string) (net.Conn, error) {
			return upstreamLocal, nil
		},
	}
	clientRemote, clientLocal := newPipe()

	var bridgeErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		bridgeErr = d.Handle(context.Background(), sniroute.PassthroughTLS,
			sniroute.Request{SNI: "chatgpt.com"}, clientLocal)
	})

	reqHeader := "GET /backend-api/codex/responses HTTP/1.1\r\n" +
		"Host: chatgpt.com\r\n" +
		"Connection: Upgrade\r\n" +
		"Upgrade: websocket\r\n" +
		"Sec-WebSocket-Protocol: responses_websockets=2026-02-06\r\n" +
		"User-Agent: codex_cli_rs/0.130.0\r\n\r\n"
	go func() { _, _ = clientRemote.Write([]byte(reqHeader)) }()

	if err := upstreamRemote.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("set upstream deadline: %v", err)
	}
	gotReq, err := readHTTPHeader(upstreamRemote, initialHTTPHeaderLimit)
	if err != nil {
		t.Fatalf("upstream header read: %v", err)
	}
	if string(gotReq) != reqHeader {
		t.Fatalf("request header changed:\ngot  %q\nwant %q", gotReq, reqHeader)
	}

	respHeader := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Protocol: responses_websockets=2026-02-06\r\n\r\n"
	go func() { _, _ = upstreamRemote.Write([]byte(respHeader)) }()

	if err := clientRemote.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("set client deadline: %v", err)
	}
	gotResp, err := readHTTPHeader(clientRemote, initialHTTPHeaderLimit)
	if err != nil {
		t.Fatalf("client header read: %v", err)
	}
	if string(gotResp) != respHeader {
		t.Fatalf("response header changed:\ngot  %q\nwant %q", gotResp, respHeader)
	}

	raw := mustMarshal(map[string]string{"type": string(wsmitm.FrameKindRequest)})
	frameBytes := wsFrameBytes(t, raw)
	go func() { _, _ = clientRemote.Write(frameBytes) }()

	if err := upstreamRemote.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("set frame deadline: %v", err)
	}
	buf := make([]byte, len(frameBytes))
	if _, err := io.ReadFull(upstreamRemote, buf); err != nil {
		t.Fatalf("upstream frame read: %v", err)
	}
	if !bytes.Equal(buf, frameBytes) {
		t.Fatalf("frame changed: got %q want %q", buf, frameBytes)
	}

	_ = upstreamRemote.Close()
	_ = clientRemote.Close()
	_ = clientLocal.Close()
	wg.Wait()
	_ = bridgeErr

	snap := d.Snapshot()
	if snap.MITMBridged != 1 {
		t.Fatalf("MITMBridged=%d, want 1 (snap=%+v)", snap.MITMBridged, snap)
	}
	if snap.WSMITMC2SFrames < 1 {
		t.Fatalf("WSMITMC2SFrames=%d, want >=1 (snap=%+v)", snap.WSMITMC2SFrames, snap)
	}
}

func TestWSPhaseFAdapterWithoutProxyFallsBackToByteEqual(t *testing.T) {
	d := &PhaseFDispatcher{}
	h := d.newWSPhaseFAdapter()
	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":  "gpt-5-codex",
			"input":  []map[string]any{{"type": "message", "role": "user", "content": "hello"}},
			"stream": true,
		},
	})
	replace, err := h.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Errorf("fallback returned err: %v", err)
	}
	if replace {
		t.Error("fallback replace=true (should be false)")
	}
}

func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

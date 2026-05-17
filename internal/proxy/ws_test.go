package proxy

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/wscompact"
)

func TestIsWebSocketUpgrade_Positive(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest("GET", "/ws", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "Upgrade")
	if !IsWebSocketUpgrade(r) {
		t.Fatal("upgrade headers must be recognised")
	}
}

func TestIsWebSocketUpgrade_MixedCaseConnection(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest("GET", "/ws", nil)
	r.Header.Set("Upgrade", "WebSocket")
	r.Header.Set("Connection", "keep-alive, Upgrade")
	if !IsWebSocketUpgrade(r) {
		t.Fatal("comma-separated Connection list must be parsed")
	}
}

func TestIsWebSocketUpgrade_NoUpgradeHeader(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest("GET", "/ws", nil)
	if IsWebSocketUpgrade(r) {
		t.Fatal("missing Upgrade must yield false")
	}
}

func TestIsWebSocketUpgrade_WrongConnection(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest("GET", "/ws", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "keep-alive")
	if IsWebSocketUpgrade(r) {
		t.Fatal("Connection without Upgrade must yield false")
	}
}

func TestWebSocketTunnel_AudioBypassBuiltIn(t *testing.T) {
	t.Parallel()
	wt := &WebSocketTunnel{}
	for _, p := range []string{"/v1/realtime", "/realtime/connect", "/api/webrtc/sdp"} {
		if !wt.IsAudioBypassPath(p) {
			t.Errorf("expected built-in bypass for %q", p)
		}
	}
	if wt.IsAudioBypassPath("/v1/responses") {
		t.Error("non-audio path must not bypass")
	}
}

func TestWebSocketTunnel_AudioBypassExtra(t *testing.T) {
	t.Parallel()
	wt := &WebSocketTunnel{BypassPaths: []string{"/voice"}}
	if !wt.IsAudioBypassPath("/voice/stream") {
		t.Fatal("operator-supplied pattern must bypass")
	}
	wt2 := &WebSocketTunnel{BypassPaths: []string{""}}
	if wt2.IsAudioBypassPath("anything") {
		t.Fatal("empty pattern must not match")
	}
}

func TestWebSocketTunnel_ServeUpgradeNoDialer(t *testing.T) {
	t.Parallel()
	wt := &WebSocketTunnel{}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	r := httptest.NewRequest("GET", "/ws", nil)
	wt.ServeUpgrade(a, r, "api.openai.com")
	// No dialer means immediate return; nothing to assert beyond no
	// panic.
}

func TestWebSocketTunnel_ServeUpgradeDialFailure(t *testing.T) {
	t.Parallel()
	wt := &WebSocketTunnel{
		Dialer: func(host, port string) (net.Conn, error) {
			return nil, errors.New("simulated dial failure")
		},
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	r := httptest.NewRequest("GET", "/ws", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "Upgrade")
	go func() {
		// Drain client side so the 502 write succeeds.
		_, _ = io.Copy(io.Discard, a)
	}()
	wt.ServeUpgrade(b, r, "api.openai.com")
}

func TestWebSocketTunnel_UpstreamRejectsUpgrade(t *testing.T) {
	t.Parallel()
	// Upstream stub that responds 403 to the upgrade.
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	go func() {
		// Read the request, respond with 403.
		br := bufio.NewReader(server)
		_, _ = http.ReadRequest(br)
		_, _ = server.Write([]byte("HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n"))
		// Hold the conn open so client read does not race close.
		time.Sleep(50 * time.Millisecond)
	}()
	wt := &WebSocketTunnel{
		Dialer: func(host, port string) (net.Conn, error) { return client, nil },
	}
	clientToTunnel, tunnelToClient := net.Pipe()
	defer clientToTunnel.Close()
	defer tunnelToClient.Close()
	r := httptest.NewRequest("GET", "/ws", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "Upgrade")
	go func() {
		wt.ServeUpgrade(tunnelToClient, r, "api.openai.com")
	}()
	resp, err := http.ReadResponse(bufio.NewReader(clientToTunnel), r)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != 403 {
		t.Fatalf("expected upstream 403 forwarded, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestWebSocketTunnel_BufferedBytesAfterUpgradeFlushed(t *testing.T) {
	t.Parallel()
	// Upstream sends 101 + an immediate WebSocket frame in the same
	// write so bufio.Reader buffers extra bytes past the 101. The
	// tunnel must flush those buffered bytes before entering pipeBytes.
	upA, upB := net.Pipe()
	defer upA.Close()
	defer upB.Close()
	go func() {
		br := bufio.NewReader(upA)
		_, _ = http.ReadRequest(br)
		// Single write: response + extra frame
		_, _ = upA.Write([]byte(
			"HTTP/1.1 101 Switching Protocols\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n" +
				"\r\n" +
				"FRAME"))
		// Echo the rest.
		_, _ = io.Copy(upA, upA)
	}()
	wt := &WebSocketTunnel{
		Dialer: func(host, port string) (net.Conn, error) { return upB, nil },
	}
	clientA, clientB := net.Pipe()
	defer clientA.Close()
	defer clientB.Close()
	r := httptest.NewRequest("GET", "/ws", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "Upgrade")
	go func() {
		wt.ServeUpgrade(clientB, r, "api.openai.com")
	}()
	br := bufio.NewReader(clientA)
	resp, err := http.ReadResponse(br, r)
	if err != nil {
		t.Fatalf("read 101: %v", err)
	}
	if resp.StatusCode != 101 {
		t.Fatalf("expected 101, got %d", resp.StatusCode)
	}
	resp.Body.Close()
	// Read the buffered "FRAME".
	buf := make([]byte, 5)
	if _, err := io.ReadFull(br, buf); err != nil {
		t.Fatalf("read buffered frame: %v", err)
	}
	if string(buf) != "FRAME" {
		t.Fatalf("unexpected buffered bytes %q", buf)
	}
	clientA.Close()
}

func TestWebSocketTunnel_FrameBridgeReadsBufferedBytes(t *testing.T) {
	t.Parallel()
	upstreamA, upstreamB := net.Pipe()
	defer upstreamA.Close()
	defer upstreamB.Close()
	go func() {
		br := bufio.NewReader(upstreamA)
		_, _ = http.ReadRequest(br)
		_, _ = upstreamA.Write([]byte(
			"HTTP/1.1 101 Switching Protocols\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n\r\n" +
				"FRAME"))
	}()
	seen := make(chan string, 1)
	wt := &WebSocketTunnel{
		Dialer: func(host, port string) (net.Conn, error) { return upstreamB, nil },
		FrameBridge: func(ctx context.Context, client, upstream net.Conn) error {
			buf := make([]byte, 5)
			_, err := io.ReadFull(upstream, buf)
			if err != nil {
				return err
			}
			seen <- string(buf)
			return nil
		},
	}
	clientA, clientB := net.Pipe()
	defer clientA.Close()
	defer clientB.Close()
	r := httptest.NewRequest("GET", "/backend-api/codex/responses", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "Upgrade")
	go wt.ServeUpgrade(clientB, r, "chatgpt.com")
	resp, err := http.ReadResponse(bufio.NewReader(clientA), r)
	if err != nil {
		t.Fatalf("read 101: %v", err)
	}
	resp.Body.Close()
	select {
	case got := <-seen:
		if got != "FRAME" {
			t.Fatalf("buffered bytes = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("frame bridge did not receive buffered bytes")
	}
}

func TestWebSocketTunnel_AudioBypassSkipsFrameBridge(t *testing.T) {
	t.Parallel()
	upstreamA, upstreamB := net.Pipe()
	defer upstreamA.Close()
	defer upstreamB.Close()
	go func() {
		br := bufio.NewReader(upstreamA)
		_, _ = http.ReadRequest(br)
		_, _ = upstreamA.Write([]byte(
			"HTTP/1.1 101 Switching Protocols\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n\r\n"))
		_, _ = io.Copy(upstreamA, upstreamA)
	}()
	bridgeCalled := make(chan struct{}, 1)
	wt := &WebSocketTunnel{
		Dialer: func(host, port string) (net.Conn, error) { return upstreamB, nil },
		FrameBridge: func(ctx context.Context, client, upstream net.Conn) error {
			bridgeCalled <- struct{}{}
			return nil
		},
	}
	clientA, clientB := net.Pipe()
	defer clientA.Close()
	defer clientB.Close()
	r := httptest.NewRequest("GET", "/v1/realtime", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "Upgrade")
	go wt.ServeUpgrade(clientB, r, "api.openai.com")
	br := bufio.NewReader(clientA)
	resp, err := http.ReadResponse(br, r)
	if err != nil {
		t.Fatalf("read 101: %v", err)
	}
	resp.Body.Close()
	if _, err := clientA.Write([]byte("PING")); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(br, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != "PING" {
		t.Fatalf("echo = %q", buf)
	}
	select {
	case <-bridgeCalled:
		t.Fatal("audio bypass must not enter frame bridge")
	default:
	}
}

func TestWebSocketTunnel_BidirectionalPipe(t *testing.T) {
	t.Parallel()
	// Upstream stub: accept upgrade, echo bytes.
	upstreamA, upstreamB := net.Pipe()
	defer upstreamA.Close()
	defer upstreamB.Close()
	go func() {
		br := bufio.NewReader(upstreamA)
		_, _ = http.ReadRequest(br)
		_, _ = upstreamA.Write([]byte(
			"HTTP/1.1 101 Switching Protocols\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n\r\n"))
		// Echo
		_, _ = io.Copy(upstreamA, upstreamA)
	}()
	wt := &WebSocketTunnel{
		Dialer: func(host, port string) (net.Conn, error) { return upstreamB, nil },
	}
	clientA, clientB := net.Pipe()
	defer clientA.Close()
	defer clientB.Close()
	r := httptest.NewRequest("GET", "/ws", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "Upgrade")
	done := make(chan struct{})
	go func() {
		wt.ServeUpgrade(clientB, r, "api.openai.com")
		close(done)
	}()
	br := bufio.NewReader(clientA)
	resp, err := http.ReadResponse(br, r)
	if err != nil {
		t.Fatalf("read 101: %v", err)
	}
	if resp.StatusCode != 101 {
		t.Fatalf("expected 101, got %d", resp.StatusCode)
	}
	resp.Body.Close()
	// Send a frame, receive echo.
	if _, err := clientA.Write([]byte("HELO")); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(br, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != "HELO" {
		t.Fatalf("unexpected echo %q", buf)
	}
	clientA.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServeUpgrade did not return after client close")
	}
}

func TestWebSocketTunnel_InspectorSeesFrameShapes(t *testing.T) {
	t.Parallel()
	upstreamA, upstreamB := net.Pipe()
	defer upstreamA.Close()
	defer upstreamB.Close()
	go func() {
		br := bufio.NewReader(upstreamA)
		_, _ = http.ReadRequest(br)
		_, _ = upstreamA.Write([]byte(
			"HTTP/1.1 101 Switching Protocols\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n\r\n"))
		buf := make([]byte, 18)
		_, _ = io.ReadFull(br, buf)
		_, _ = upstreamA.Write([]byte{0x81, 0x0e})
		_, _ = upstreamA.Write([]byte(`{"type":"ack"}`))
	}()
	summaries := make(chan wscompact.FrameSummary, 4)
	wt := &WebSocketTunnel{
		Dialer: func(host, port string) (net.Conn, error) { return upstreamB, nil },
		Inspector: wscompact.InspectorFunc(func(summary wscompact.FrameSummary) {
			summaries <- summary
		}),
	}
	clientA, clientB := net.Pipe()
	defer clientA.Close()
	defer clientB.Close()
	r := httptest.NewRequest("GET", "/ws", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "Upgrade")
	go wt.ServeUpgrade(clientB, r, "api.openai.com")
	br := bufio.NewReader(clientA)
	resp, err := http.ReadResponse(br, r)
	if err != nil {
		t.Fatalf("read 101: %v", err)
	}
	resp.Body.Close()
	_, _ = clientA.Write([]byte{0x81, 0x10})
	_, _ = clientA.Write([]byte(`{"type":"hello"}`))
	buf := make([]byte, 16)
	if _, err := io.ReadFull(br, buf); err != nil {
		t.Fatalf("read upstream ack frame: %v", err)
	}
	clientA.Close()
	var seenClient, seenServer bool
	deadline := time.After(2 * time.Second)
	for !(seenClient && seenServer) {
		select {
		case summary := <-summaries:
			if summary.Direction == wscompact.DirectionClientToServer && summary.MessageType == "hello" {
				seenClient = true
			}
			if summary.Direction == wscompact.DirectionServerToClient && summary.MessageType == "ack" {
				seenServer = true
			}
		case <-deadline:
			t.Fatalf("missing inspected summaries client=%t server=%t", seenClient, seenServer)
		}
	}
}

func TestWebSocketTunnel_ForwardRequestFailureLogged(t *testing.T) {
	t.Parallel()
	// Use a closed conn as upstream so the request write fails.
	upstreamA, upstreamB := net.Pipe()
	upstreamA.Close()
	upstreamB.Close()
	wt := &WebSocketTunnel{
		Dialer: func(host, port string) (new net.Conn, err error) { return upstreamB, nil },
		Logger: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	clientA, clientB := net.Pipe()
	defer clientA.Close()
	defer clientB.Close()
	r := httptest.NewRequest("GET", "/ws", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "Upgrade")
	go func() {
		// Drain the 502 written by ServeUpgrade.
		_, _ = io.Copy(io.Discard, clientA)
	}()
	wt.ServeUpgrade(clientB, r, "api.openai.com")
}

func TestWebSocketTunnel_ReadResponseFailureLogged(t *testing.T) {
	t.Parallel()
	// Upstream that consumes the request but returns garbage.
	upA, upB := net.Pipe()
	defer upA.Close()
	defer upB.Close()
	go func() {
		br := bufio.NewReader(upA)
		_, _ = http.ReadRequest(br)
		_, _ = upA.Write([]byte("not a valid http response\r\n"))
		upA.Close()
	}()
	wt := &WebSocketTunnel{
		Dialer: func(host, port string) (net.Conn, error) { return upB, nil },
	}
	clientA, clientB := net.Pipe()
	defer clientA.Close()
	defer clientB.Close()
	r := httptest.NewRequest("GET", "/ws", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "Upgrade")
	go func() {
		_, _ = io.Copy(io.Discard, clientA)
	}()
	wt.ServeUpgrade(clientB, r, "api.openai.com")
}

func TestWebSocketTunnel_LogfFallbackToDefault(t *testing.T) {
	t.Parallel()
	wt := &WebSocketTunnel{}
	wt.logf("test message", "key", "value")
}

func TestWriteBadGateway(t *testing.T) {
	t.Parallel()
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go func() {
		_ = writeBadGateway(b)
		b.Close()
	}()
	resp, err := http.ReadResponse(bufio.NewReader(a), nil)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.StatusCode != 502 {
		t.Fatalf("expected 502, got %d", resp.StatusCode)
	}
}

func TestDefaultWebSocketDialer_TLSFailureSurfaces(t *testing.T) {
	t.Parallel()
	// Dial a port we know nothing is listening on. Expected: error.
	_, err := DefaultWebSocketDialer("127.0.0.1", "1")
	if err == nil {
		t.Fatal("expected dial failure to a closed port")
	}
}

func TestForwardResponse_BasicWrite(t *testing.T) {
	t.Parallel()
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	resp := &http.Response{
		Status:     "200 OK",
		StatusCode: 200,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1, ProtoMinor: 1,
		Header:        http.Header{"Content-Length": []string{"0"}},
		Body:          http.NoBody,
		ContentLength: 0,
	}
	go func() {
		_ = forwardResponse(b, resp)
		b.Close()
	}()
	out, _ := io.ReadAll(a)
	if !strings.Contains(string(out), "200 OK") {
		t.Fatalf("expected 200 OK in response, got %q", out)
	}
}

func TestWriteRequest_PreservesPath(t *testing.T) {
	t.Parallel()
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	r := httptest.NewRequest("GET", "/v1/realtime?session=abc", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Sec-WebSocket-Key", "test123==")
	go func() {
		_ = writeRequest(b, r, "api.openai.com")
		b.Close()
	}()
	out, _ := io.ReadAll(a)
	if !strings.Contains(string(out), "/v1/realtime?session=abc") {
		t.Fatalf("expected path in request line, got %q", out)
	}
	if !strings.Contains(strings.ToLower(string(out)), "sec-websocket-key: test123==") {
		t.Fatalf("expected Sec-WebSocket-Key forwarded, got %q", out)
	}
}

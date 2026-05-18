package proxy

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
)

func TestRawScopedWSSListenerInterceptsBeforeNetHTTP(t *testing.T) {
	t.Parallel()
	client, server := net.Pipe()
	defer client.Close()
	seenHeader := make(chan string, 1)
	intercepted := make(chan string, 1)
	tunnel := &WebSocketTunnel{
		Dialer: func(string, string) (net.Conn, error) {
			upClient, upServer := net.Pipe()
			go func() {
				defer upServer.Close()
				header, err := readHTTPHeader(upServer, initialHTTPHeaderLimit)
				if err != nil {
					seenHeader <- "read-error:" + err.Error()
					return
				}
				seenHeader <- string(header)
				_, _ = io.WriteString(upServer, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
			}()
			return upClient, nil
		},
		FrameBridge: func(context.Context, net.Conn, net.Conn, WebSocketBridgeOptions) error {
			return nil
		},
	}
	listener := &rawScopedWSSListener{
		Tunnel:       tunnel,
		UpstreamHost: "chatgpt.com",
		OnIntercept: func(path string, header []byte) {
			intercepted <- path
		},
	}
	go func() {
		_, _ = io.WriteString(client,
			"GET /backend-api/codex/responses HTTP/1.1\r\n"+
				"uSeR-aGeNt: codex_cli_rs/0.130.0\r\n"+
				"X-Custom-One: alpha\r\n"+
				"Host: 127.0.0.1:8990\r\n"+
				"Sec-WebSocket-Protocol: other, responses_websockets=2026-02-06\r\n"+
				"Upgrade: websocket\r\n"+
				"Connection: keep-alive, Upgrade\r\n"+
				"Sec-WebSocket-Key: raw-key\r\n"+
				"\r\n")
		resp, err := http.ReadResponse(bufio.NewReader(client), nil)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	handled, replay := listener.maybeHandle(server)
	if !handled || replay != nil {
		t.Fatalf("expected raw WSS interception, handled=%v replay=%T", handled, replay)
	}
	select {
	case got := <-intercepted:
		if got != "/backend-api/codex/responses" {
			t.Fatalf("intercepted path = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("raw intercept callback did not fire")
	}
	select {
	case got := <-seenHeader:
		wantOrder := []string{
			"GET /backend-api/codex/responses HTTP/1.1",
			"uSeR-aGeNt: codex_cli_rs/0.130.0",
			"X-Custom-One: alpha",
			"Host: chatgpt.com",
			"Sec-WebSocket-Protocol: other, responses_websockets=2026-02-06",
			"Upgrade: websocket",
			"Connection: keep-alive, Upgrade",
			"Sec-WebSocket-Key: raw-key",
		}
		last := -1
		for _, want := range wantOrder {
			idx := strings.Index(got, want)
			if idx <= last {
				t.Fatalf("raw upstream header lost order/casing %q after %d:\n%s", want, last, got)
			}
			last = idx
		}
	case <-time.After(2 * time.Second):
		t.Fatal("upstream did not receive raw header")
	}
}

func TestRawScopedWSSListenerFallsBackWithPrefetchedHeader(t *testing.T) {
	t.Parallel()
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	raw := "GET /health HTTP/1.1\r\nHost: 127.0.0.1:8990\r\n\r\n"
	go func() {
		_, _ = io.WriteString(client, raw)
	}()
	listener := &rawScopedWSSListener{Tunnel: &WebSocketTunnel{}, UpstreamHost: "chatgpt.com"}
	handled, replay := listener.maybeHandle(server)
	if handled || replay == nil {
		t.Fatalf("expected fallback replay, handled=%v replay=%T", handled, replay)
	}
	buf := make([]byte, len(raw))
	if _, err := io.ReadFull(replay, buf); err != nil {
		t.Fatalf("read replay: %v", err)
	}
	if string(buf) != raw {
		t.Fatalf("prefetched header changed:\n got %q\nwant %q", string(buf), raw)
	}
}

func TestIsRawScopedCodexWSSRequiresExactConversationPath(t *testing.T) {
	t.Parallel()
	base := parsedHTTPRequestHeader{
		method:      "GET",
		path:        "/backend-api/codex/responses",
		subprotocol: "responses_websockets=2026-02-06",
		websocket:   true,
	}
	if !isRawScopedCodexWSS(base) {
		t.Fatal("exact conversation path should match")
	}
	base.path = "/backend-api/codex/responses?model=gpt-5"
	if !isRawScopedCodexWSS(base) {
		t.Fatal("conversation path with query should match")
	}
	base.path = "/backend-api/codex/responses-extra"
	if isRawScopedCodexWSS(base) {
		t.Fatal("prefix collision must not match raw WSS gate")
	}
}

func TestProxyStartUsesRawScopedWSSListener(t *testing.T) {
	cfg := config.Defaults()
	cfg.Proxy.ListenPort = 0
	p := New(cfg)
	seenHeader := make(chan string, 1)
	p.webSocketTunnel.Dialer = func(string, string) (net.Conn, error) {
		upClient, upServer := net.Pipe()
		go func() {
			defer upServer.Close()
			header, err := readHTTPHeader(upServer, initialHTTPHeaderLimit)
			if err != nil {
				seenHeader <- "read-error:" + err.Error()
				return
			}
			seenHeader <- string(header)
			_, _ = io.WriteString(upServer, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		}()
		return upClient, nil
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.Start()
	}()
	for i := 0; i < 100 && !p.HasListener(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if !p.HasListener() {
		t.Fatal("proxy listener did not start")
	}
	conn, err := net.Dial("tcp", p.ListenAddr())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(conn,
		"GET /backend-api/codex/responses HTTP/1.1\r\n"+
			"X-Before-Host: kept\r\n"+
			"Host: 127.0.0.1:8990\r\n"+
			"Sec-WebSocket-Protocol: responses_websockets=2026-02-06\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	_ = conn.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	select {
	case got := <-seenHeader:
		if !strings.Contains(got, "X-Before-Host: kept\r\nHost: chatgpt.com\r\nSec-WebSocket-Protocol") {
			t.Fatalf("raw listener did not preserve order through Start:\n%s", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("upstream did not receive raw Start header")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	select {
	case err := <-errCh:
		if err != nil && !strings.Contains(err.Error(), "Server closed") {
			t.Fatalf("Start returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after shutdown")
	}
}

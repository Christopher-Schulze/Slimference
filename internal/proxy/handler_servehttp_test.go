package proxy

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

var errReadBodyTest = errors.New("read body failed")

type alwaysFailBody struct{}

func (alwaysFailBody) Read([]byte) (int, error) { return 0, errReadBodyTest }

func TestServeHTTP_passthroughGET(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"upstream":true}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = upstream.URL
	p := New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %s", res.StatusCode, rec.Body.String())
	}
}

func TestServeHTTP_DirectCodexWebSocketUpgradeTunnels(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	p := New(cfg)
	seenPath := make(chan string, 1)
	p.webSocketTunnel = &WebSocketTunnel{
		Dialer: func(string, string) (net.Conn, error) {
			client, upstream := net.Pipe()
			go func() {
				defer upstream.Close()
				req, err := http.ReadRequest(bufio.NewReader(upstream))
				if err != nil {
					seenPath <- "read-error:" + err.Error()
					return
				}
				seenPath <- req.URL.Path
				_, _ = io.WriteString(upstream, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
			}()
			return client, nil
		},
	}
	srv := httptest.NewServer(p)
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = io.WriteString(conn, "GET /backend-api/codex/responses HTTP/1.1\r\nHost: 127.0.0.1:8990\r\nUser-Agent: codex/0.130.0\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if got := <-seenPath; got != "/backend-api/codex/responses" {
		t.Fatalf("upstream path = %q", got)
	}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 || summaries[0].RouteMode != "websocket_tunnel" {
		t.Fatalf("missing websocket flight summary: %#v", summaries)
	}
}

func TestServeHTTP_passthroughProviderDisabled(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `[]`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	p := New(cfg)
	p.SetProviderEnabled(types.Anthropic, false)

	body := `{"model":"claude","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
}

func TestServeHTTP_UnknownAnthropicVersionDowngradesPipeline(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Proxy.AnthropicVersions = []string{"2023-06-01"}
	cfg.Proxy.AnthropicUnknownBehavior = "passthrough"
	p := New(cfg)

	body := `{"model":"claude","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2099-01-01")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if AnthropicUnknownVersionCount() == 0 {
		t.Fatal("unknown version counter did not increment")
	}
}

func TestServeHTTP_readBodyFailedCompressible(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.Body = io.NopCloser(alwaysFailBody{})
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", res.StatusCode, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "read body") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

func TestServeHTTP_readBodyFailedPassthrough(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	req := httptest.NewRequest(http.MethodPost, "/v1/models", nil)
	req.Body = io.NopCloser(alwaysFailBody{})
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", res.StatusCode, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "read body") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

func TestServeHTTP_readBodyTooLargeCompressible(t *testing.T) {
	t.Parallel()

	p := New(config.Defaults())
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.Body = io.NopCloser(&repeatingBody{remaining: maxRequestBodySize + 1})
	req.ContentLength = maxRequestBodySize + 1
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d: %s", res.StatusCode, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "request body too large") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

func TestServeHTTP_readBodyTooLargePassthrough(t *testing.T) {
	t.Parallel()

	p := New(config.Defaults())
	req := httptest.NewRequest(http.MethodPost, "/v1/models", nil)
	req.Body = io.NopCloser(&repeatingBody{remaining: maxRequestBodySize + 1})
	req.ContentLength = maxRequestBodySize + 1
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d: %s", res.StatusCode, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "request body too large") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

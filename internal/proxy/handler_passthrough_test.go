package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tokenproxy/tokenproxy/internal/config"
	"github.com/tokenproxy/tokenproxy/internal/types"
)

func TestHandlePassthrough_upstreamConnectionFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = srv.URL
	p := New(cfg)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	p.handlePassthrough(rec, r, types.OpenAI, nil)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", res.StatusCode, rec.Body.String())
	}
}

func TestHandlePassthrough_invalidUpstreamURL(t *testing.T) {
	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = ":"
	p := New(cfg)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	p.handlePassthrough(rec, r, types.OpenAI, nil)

	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "build request failed") {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestHandlePassthrough_usesDefaultClientWhenNil(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = upstream.URL
	p := New(cfg)
	p.httpClients[types.OpenAI] = nil

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	p.handlePassthrough(rec, r, types.OpenAI, nil)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
}

func TestHandlePassthrough_nonStreamJSONBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Up", "1")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"x":1}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = upstream.URL
	p := New(cfg)

	body := []byte(`{"model":"gpt-4","messages":[]}`)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/some-route", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Client", "test")
	p.handlePassthrough(rec, r, types.OpenAI, body)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status=%d", res.StatusCode)
	}
	b, _ := io.ReadAll(res.Body)
	if string(b) != `{"x":1}` {
		t.Fatalf("body=%q", b)
	}
	if res.Header.Get("X-Up") != "1" {
		t.Fatalf("headers not copied")
	}
}

// TestHandlePassthrough_headerSkipBranch covers the "Host"/"Content-Length"/"Connection" skip
// in handlePassthrough (lines 377-378 in handler.go). We explicitly set those headers on the
// request so the switch-continue branch is exercised.
func TestHandlePassthrough_headerSkipBranch(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = upstream.URL
	p := New(cfg)

	body := []byte(`{"model":"gpt-4","messages":[]}`)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/models", strings.NewReader(string(body)))
	// These headers hit the switch-continue branch in handlePassthrough.
	r.Header.Set("Host", "example.com")
	r.Header.Set("Content-Length", "30")
	r.Header.Set("Connection", "keep-alive")
	r.Header.Set("X-Custom", "forwarded")
	p.handlePassthrough(rec, r, types.OpenAI, body)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.StatusCode, rec.Body.String())
	}
}

func TestHandlePassthrough_streamingSSE(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"x\":1}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = upstream.URL
	p := New(cfg)

	body := []byte(`{"stream":true,"max_tokens":10}`)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/other", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	p.handlePassthrough(rec, r, types.OpenAI, body)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d: %s", res.StatusCode, rec.Body.String())
	}
	out, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(out), "data:") {
		t.Fatalf("expected SSE line in body, got %q", out)
	}
}

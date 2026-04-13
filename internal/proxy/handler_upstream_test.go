package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

func newUpstreamTestProxy(t *testing.T, srv *httptest.Server) *Proxy {
	t.Helper()
	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = srv.URL
	p := New(cfg)
	p.httpClients[types.Anthropic] = srv.Client()
	return p
}

func anthropicUpstreamReq(t *testing.T, body []byte) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	r.URL.RawQuery = ""
	return r.WithContext(context.WithValue(r.Context(), origBodyKey{}, body))
}

func TestDoUpstreamRequest_contextOverflowRetriesWithOriginalBody(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"context_length_exceeded"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	}))
	defer srv.Close()

	orig := []byte(`{"model":"claude-3-haiku-20240307","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`)
	p := newUpstreamTestProxy(t, srv)
	r := anthropicUpstreamReq(t, orig)

	resp, err := p.doUpstreamRequest(r, types.Anthropic, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if string(b) != `{"id":"ok"}` {
		t.Fatalf("body=%q", b)
	}
	if n != 2 {
		t.Fatalf("upstream calls=%d want 2", n)
	}
}

func TestDoUpstreamRequest_aggRetrySucceeds(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`context_length_exceeded`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"tier":"aggressive_ok"}`))
	}))
	defer srv.Close()

	orig := []byte(`{"model":"claude-3-haiku-20240307","max_tokens":256,"messages":[{"role":"user","content":"hello world repeated text for compression path"}]}`)
	msgs, _, err := extractMessages(types.Anthropic, orig)
	if err != nil {
		t.Fatal(err)
	}
	stash := pipelineStash{messages: msgs, origBody: orig, provider: types.Anthropic}
	r0 := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	ctx := context.WithValue(r0.Context(), pipelineStashKey{}, stash)
	ctx = context.WithValue(ctx, origBodyKey{}, orig)
	r := r0.WithContext(ctx)

	p := newUpstreamTestProxy(t, srv)
	resp, err := p.doUpstreamRequest(r, types.Anthropic, []byte(`{"model":"x","messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(b, []byte("aggressive_ok")) {
		t.Fatalf("body=%q", b)
	}
	if n != 2 {
		t.Fatalf("upstream calls=%d want 2", n)
	}
}

func TestDoUpstreamRequest_aggRetryFailsThenOriginalSucceeds(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		switch n {
		case 1, 2:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`context_length_exceeded`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer srv.Close()

	orig := []byte(`{"model":"claude-3-haiku-20240307","max_tokens":256,"messages":[{"role":"user","content":"hello world repeated text for compression path"}]}`)
	msgs, _, err := extractMessages(types.Anthropic, orig)
	if err != nil {
		t.Fatal(err)
	}
	stash := pipelineStash{messages: msgs, origBody: orig, provider: types.Anthropic}
	r0 := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	ctx := context.WithValue(r0.Context(), pipelineStashKey{}, stash)
	ctx = context.WithValue(ctx, origBodyKey{}, orig)
	r := r0.WithContext(ctx)

	p := newUpstreamTestProxy(t, srv)
	resp, err := p.doUpstreamRequest(r, types.Anthropic, []byte(`{"model":"x","messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if n != 3 {
		t.Fatalf("upstream calls=%d want 3 (400, 400 agg, 200 original)", n)
	}
}

func TestDoUpstreamRequest_nonOverflow400PreservesBody(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"not a context length issue"}`))
	}))
	defer srv.Close()

	p := newUpstreamTestProxy(t, srv)
	r := anthropicUpstreamReq(t, []byte(`{}`))

	resp, err := p.doUpstreamRequest(r, types.Anthropic, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if n != 1 {
		t.Fatalf("calls=%d want 1", n)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("not a context length issue")) {
		t.Fatalf("body=%q", b)
	}
}

func TestDoUpstreamRequest_firstDoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("server should not be reached")
	}))
	srv.Close()

	p := newUpstreamTestProxy(t, srv)
	r := anthropicUpstreamReq(t, []byte(`{}`))

	_, err := p.doUpstreamRequest(r, types.Anthropic, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error from closed server")
	}
}

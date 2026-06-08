package proxy

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/caching"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

func TestCtxReader_ContextCanceledBeforeRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cr := &ctxReader{ctx: ctx, r: strings.NewReader("hello")}
	_, err := cr.Read(make([]byte, 8))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestProxy_GetProviderHealth_NoMonitor(t *testing.T) {
	p := New(config.Defaults())
	p.healthMon = nil

	info := p.GetProviderHealth(types.Anthropic)
	if info.Status != types.ProviderHealthIdle {
		t.Fatalf("expected idle without monitor, got %v", info.Status)
	}
}

func TestHealthMonitor_idleAfterStaleActivity(t *testing.T) {
	h := newHealthMonitor()
	h.record(types.Anthropic, true)
	h.mu.Lock()
	h.results[types.Anthropic].lastSuccess = time.Now().Add(-6 * time.Minute)
	h.mu.Unlock()

	info := h.getStatus(types.Anthropic)
	if info.Status != types.ProviderHealthIdle {
		t.Fatalf("expected idle after stale activity, got %v", info.Status)
	}
}

func TestProxy_HasListener(t *testing.T) {
	p := New(config.Defaults())
	if p.HasListener() {
		t.Fatal("expected false without bound listener")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	p.listenerMu.Lock()
	p.listener = ln
	p.listenerMu.Unlock()
	if !p.HasListener() {
		t.Fatal("expected true with bound listener")
	}
}

func fillAnalyticsQueue(p *Proxy) {
	for range cap(p.analyticsQueue) {
		p.analyticsQueue <- types.AnalyticsEvent{Type: types.EventRequestProcessed}
	}
}

func TestExtractSessionIDCodexHTTPUsesTurnMetadata(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","previous_response_id":"resp_prev","client_metadata":{"x-codex-turn-metadata":"{\"thread_id\":\"019ea6ca-5279-7200-868e-2efda5e6731d\",\"source\":\"cli\"}"},"input":[]}`)
	got := extractSessionID(types.CodexChatGPT, body, http.Header{})
	if got != "codex-wss:019ea6ca-5279-7200-868e-2efda5e6731d" {
		t.Fatalf("session id=%q", got)
	}
}

func TestServeHTTP_AnalyticsQueueFullBranches(t *testing.T) {
	t.Run("cache hit", func(t *testing.T) {
		p := New(config.Defaults())
		fillAnalyticsQueue(p)
		body := []byte(`{"model":"claude","temperature":0,"messages":[{"role":"user","content":"cache"}]}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
		sessionID := extractSessionID(types.Anthropic, body, req.Header)
		key := p.responseCache.ComputeRequestKeyWithRoute(types.Anthropic, p.responseCacheEffectiveRouteKey(req, sessionID), body, req.Header)
		p.responseCache.Set(key, &caching.CacheEntry{
			Response:    []byte(`{"ok":true}`),
			Headers:     map[string][]string{"Content-Type": {"application/json"}},
			StatusCode:  http.StatusOK,
			CreatedAt:   time.Now(),
			TokensSaved: 1,
		})
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("cache-hit status = %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("secret redaction", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"id":"ok","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
		}))
		defer upstream.Close()

		cfg := config.Defaults()
		cfg.Upstream.Anthropic.BaseURL = upstream.URL
		cfg.Compression.Layer1Enabled = false
		cfg.Compression.Layer2Enabled = false
		cfg.Secrets.Mode = "redact"

		p := New(cfg)
		fillAnalyticsQueue(p)
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude","messages":[{"role":"user","content":"key AKIAIOSFODNN7EXAMPLE"}]}`))
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("secret status = %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("upstream error and request processed", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Upstream.Anthropic.BaseURL = "http://127.0.0.1:1"
		cfg.Compression.Layer1Enabled = false
		cfg.Compression.Layer2Enabled = false
		cfg.Secrets.Mode = "off"

		p := New(cfg)
		fillAnalyticsQueue(p)
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude","messages":[{"role":"user","content":"x"}]}`))
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("upstream error status = %d body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestDoUpstreamRequest_ContextDoneAndOverflowAnalyticsQueueFull(t *testing.T) {
	t.Run("rate limit context done", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer srv.Close()

		cfg := config.Defaults()
		cfg.Upstream.Anthropic.BaseURL = srv.URL
		p := New(cfg)
		p.httpClients[types.Anthropic] = srv.Client()
		fillAnalyticsQueue(p)

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()
		r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(context.WithValue(ctx, origBodyKey{}, []byte(`{}`)))

		_, err := p.doUpstreamRequest(r, types.Anthropic, []byte(`{}`))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	})

	t.Run("overflow retry analytics queue full", func(t *testing.T) {
		var calls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls++
			if calls == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `context_length_exceeded`)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"ok":true}`)
		}))
		defer srv.Close()

		cfg := config.Defaults()
		cfg.Upstream.Anthropic.BaseURL = srv.URL
		p := New(cfg)
		p.httpClients[types.Anthropic] = srv.Client()
		fillAnalyticsQueue(p)

		orig := []byte(`{"model":"claude","messages":[{"role":"user","content":"hello"}]}`)
		r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(context.WithValue(context.Background(), origBodyKey{}, orig))

		resp, err := p.doUpstreamRequest(r, types.Anthropic, []byte(`{"model":"claude","messages":[]}`))
		if err != nil {
			t.Fatalf("overflow retry failed: %v", err)
		}
		_ = resp.Body.Close()
		if calls != 2 {
			t.Fatalf("expected overflow retry path, got %d calls", calls)
		}
	})
}

func TestHandleCompressibleRequest_ReconstructBodyError(t *testing.T) {
	p := New(config.Defaults())
	orig := reconstructBodyFn
	reconstructBodyFn = func(types.Provider, []byte, []types.Message) ([]byte, error) {
		return nil, errors.New("rebuild boom")
	}
	defer func() {
		reconstructBodyFn = orig
	}()

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude","messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected reconstruct error 500, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDoUpstreamRequest_OverflowFallbackBuildError(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `context_length_exceeded`)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = srv.URL
	p := New(cfg)
	p.httpClients[types.Anthropic] = srv.Client()

	origNewRequest := newRequestWithContextFn
	callCount := 0
	newRequestWithContextFn = func(ctx context.Context, method string, url string, body io.Reader) (*http.Request, error) {
		callCount++
		if callCount >= 2 {
			return nil, errors.New("new request boom")
		}
		return http.NewRequestWithContext(ctx, method, url, body)
	}
	defer func() {
		newRequestWithContextFn = origNewRequest
	}()

	orig := []byte(`{"model":"claude","messages":[{"role":"user","content":"hello"}]}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(context.WithValue(context.Background(), origBodyKey{}, orig))

	_, err := p.doUpstreamRequest(r, types.Anthropic, []byte(`{"model":"claude","messages":[]}`))
	if err == nil || !strings.Contains(err.Error(), "build overflow fallback request") {
		t.Fatalf("expected overflow fallback build error, got %v", err)
	}
}

func TestDoUpstreamRequest_ContextAlreadyCanceled(t *testing.T) {
	p := New(config.Defaults())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
	_, err := p.doUpstreamRequest(r, types.Anthropic, []byte(`{}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected immediate context cancellation, got %v", err)
	}
}

func TestParseRetryAfter_FutureDateCapped(t *testing.T) {
	header := time.Now().Add(2 * time.Minute).UTC().Format(http.TimeFormat)
	got := parseRetryAfter(header)
	if got != 30*time.Second {
		t.Fatalf("expected 30s cap for future http-date, got %v", got)
	}
}

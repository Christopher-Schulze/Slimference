package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

func TestEnqueueCompressionJob(t *testing.T) {
	queue := make(chan types.CompressJob, 1)
	enqueueCompressionJob(queue, types.CompressJob{SessionID: "first"})
	if got := <-queue; got.SessionID != "first" {
		t.Fatalf("first job=%+v", got)
	}
	queue <- types.CompressJob{SessionID: "kept"}
	enqueueCompressionJob(queue, types.CompressJob{SessionID: "dropped"})
	if got := <-queue; got.SessionID != "kept" {
		t.Fatalf("full queue should keep original job, got %+v", got)
	}
}

func TestInjectOpenAIPromptCache(t *testing.T) {
	cfg := config.Defaults()
	cfg.Proxy.OpenAIPromptCache.Enabled = true
	cfg.Proxy.OpenAIPromptCache.PromptCacheKeyStrategy = "model_session"
	cfg.Proxy.OpenAIPromptCache.Retention = "24h"
	cfg.Proxy.OpenAIPromptCache.MinTokens = 100
	p := New(cfg)

	body := []byte(`{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`)
	out, decision := p.injectOpenAIPromptCache(types.OpenAI, body, "gpt-5", 200, "sess-1")
	if !decision.Applied || decision.Key == "" || decision.Retention != "24h" {
		t.Fatalf("unexpected decision: %+v body=%s", decision, out)
	}
	text := string(out)
	if !strings.Contains(text, `"prompt_cache_key"`) || !strings.Contains(text, `"prompt_cache_retention":"24h"`) {
		t.Fatalf("cache fields missing: %s", text)
	}
}

func TestInjectOpenAIPromptCacheDecisionBranches(t *testing.T) {
	baseBody := []byte(`{"model":"gpt-5","messages":[]}`)

	t.Run("disabled", func(t *testing.T) {
		cfg := config.Defaults()
		p := New(cfg)
		out, decision := p.injectOpenAIPromptCache(types.OpenAI, baseBody, "gpt-5", 200, "sess")
		if decision.Applied || decision.Reason != "disabled" || string(out) != string(baseBody) {
			t.Fatalf("decision=%+v body=%s", decision, out)
		}
	})
	t.Run("below_min_tokens", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Proxy.OpenAIPromptCache.Enabled = true
		cfg.Proxy.OpenAIPromptCache.MinTokens = 1000
		p := New(cfg)
		_, decision := p.injectOpenAIPromptCache(types.OpenAI, baseBody, "gpt-5", 10, "sess")
		if decision.Reason != "below_min_tokens" {
			t.Fatalf("decision=%+v", decision)
		}
	})
	t.Run("invalid_json", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Proxy.OpenAIPromptCache.Enabled = true
		cfg.Proxy.OpenAIPromptCache.MinTokens = 0
		p := New(cfg)
		out, decision := p.injectOpenAIPromptCache(types.OpenAI, []byte(`{`), "gpt-5", 10, "sess")
		if decision.Reason != "invalid_json" || string(out) != `{` {
			t.Fatalf("decision=%+v body=%s", decision, out)
		}
	})
	t.Run("rate_limited_no_change", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Proxy.OpenAIPromptCache.Enabled = true
		cfg.Proxy.OpenAIPromptCache.PromptCacheKeyStrategy = "static"
		cfg.Proxy.OpenAIPromptCache.StaticPromptCacheKey = "stable"
		cfg.Proxy.OpenAIPromptCache.MinTokens = 0
		cfg.Proxy.OpenAIPromptCache.MaxRequestsPerKeyPerMinute = 1
		p := New(cfg)
		_, first := p.injectOpenAIPromptCache(types.OpenAI, baseBody, "gpt-5", 10, "sess")
		_, second := p.injectOpenAIPromptCache(types.OpenAI, baseBody, "gpt-5", 10, "sess")
		if !first.Applied || second.Applied || second.Reason != "rate_limited" {
			t.Fatalf("first=%+v second=%+v", first, second)
		}
	})
}

func TestBuildOpenAIPromptCacheKeyBranches(t *testing.T) {
	cfg := config.Defaults().Proxy.OpenAIPromptCache
	cfg.PromptCacheKeyStrategy = ""
	if got := buildOpenAIPromptCacheKey(cfg, "gpt-5", "sess-raw-secret"); got == "" || strings.Contains(got, "sess-raw-secret") {
		t.Fatalf("default/session key not hashed: %q", got)
	}
	cfg.PromptCacheKeyStrategy = "off"
	if got := buildOpenAIPromptCacheKey(cfg, "gpt-5", "sess"); got != "" {
		t.Fatalf("off key=%q", got)
	}
	cfg.PromptCacheKeyStrategy = "static"
	cfg.StaticPromptCacheKey = "  fixed-key  "
	if got := buildOpenAIPromptCacheKey(cfg, "gpt-5", "sess"); got != "fixed-key" {
		t.Fatalf("static key=%q", got)
	}
	cfg.PromptCacheKeyStrategy = "unknown"
	if got := buildOpenAIPromptCacheKey(cfg, "gpt-5", "sess"); got != "" {
		t.Fatalf("unknown key=%q", got)
	}
	if got := hashedPromptCacheKey("session", "   "); got != "" {
		t.Fatalf("empty hash key=%q", got)
	}
}

func TestInjectOpenAIPromptCache_IdempotentAndScoped(t *testing.T) {
	cfg := config.Defaults()
	cfg.Proxy.OpenAIPromptCache.Enabled = true
	cfg.Proxy.OpenAIPromptCache.PromptCacheKeyStrategy = "session"
	cfg.Proxy.OpenAIPromptCache.Retention = "in_memory"
	cfg.Proxy.OpenAIPromptCache.MinTokens = 0
	p := New(cfg)

	body := []byte(`{"model":"gpt-5","prompt_cache_key":"caller","prompt_cache_retention":"in_memory","messages":[]}`)
	out, decision := p.injectOpenAIPromptCache(types.OpenAI, body, "gpt-5", 100, "sess-1")
	if decision.Applied || string(out) != string(body) {
		t.Fatalf("caller-owned fields must be preserved, decision=%+v body=%s", decision, out)
	}
	if out, decision := p.injectOpenAIPromptCache(types.CodexChatGPT, []byte(`{"messages":[]}`), "gpt-5-codex", 100, "sess-1"); decision.Applied || string(out) != `{"messages":[]}` {
		t.Fatalf("codex backend must stay untouched, decision=%+v body=%s", decision, out)
	}
}

func TestOpenAIPromptCacheRateLimit(t *testing.T) {
	cfg := config.Defaults()
	p := New(cfg)
	if !p.allowOpenAIPromptCacheKey("", 1, time.Unix(1, 0)) {
		t.Fatal("empty key should bypass limiter")
	}
	if !p.allowOpenAIPromptCacheKey("k0", 0, time.Unix(1, 0)) {
		t.Fatal("zero limit should disable limiter")
	}
	now := time.Unix(1000, 0)
	if !p.allowOpenAIPromptCacheKey("k", 1, now) {
		t.Fatal("first request should pass")
	}
	if p.allowOpenAIPromptCacheKey("k", 1, now.Add(10*time.Second)) {
		t.Fatal("second request inside the same minute should be rate-limited")
	}
	if !p.allowOpenAIPromptCacheKey("k", 1, now.Add(61*time.Second)) {
		t.Fatal("new minute should reset rate bucket")
	}
	if !p.allowOpenAIPromptCacheKey("k2", 2, now) {
		t.Fatal("first k2 request should pass")
	}
	if !p.allowOpenAIPromptCacheKey("k2", 2, now.Add(10*time.Second)) {
		t.Fatal("second k2 request should increment and pass")
	}
}

func TestPromptCacheUnsupportedPeekRestoresBody(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(strings.NewReader(`{"error":"unknown parameter prompt_cache_key"}`)),
	}
	if !peekPromptCacheUnsupportedError(resp) {
		t.Fatal("expected prompt-cache unsupported signal")
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "prompt_cache_key") {
		t.Fatalf("body was not restored: %s", body)
	}
	if peekPromptCacheUnsupportedError(nil) {
		t.Fatal("nil response should not match")
	}
	if peekPromptCacheUnsupportedError(&http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}) {
		t.Fatal("2xx response should not match")
	}
}

func TestResolveOpenAIPromptCacheRetention(t *testing.T) {
	if got := resolveOpenAIPromptCacheRetention("24h", "gpt-5.1-codex"); got != "24h" {
		t.Fatalf("gpt-5.1-codex retention=%q", got)
	}
	if got := resolveOpenAIPromptCacheRetention("24h", "gpt-4o"); got != "" {
		t.Fatalf("unsupported model retention=%q", got)
	}
	if got := resolveOpenAIPromptCacheRetention("in_memory", "gpt-4o"); got != "in_memory" {
		t.Fatalf("in_memory retention=%q", got)
	}
	if got := resolveOpenAIPromptCacheRetention("auto", "gpt-5"); got != "" {
		t.Fatalf("auto retention should defer to provider default, got %q", got)
	}
}

func TestExtractOpenAICacheUsageFromBodyEmpty(t *testing.T) {
	if got := extractOpenAICacheUsageFromBody(nil); got != (cacheUsage{}) {
		t.Fatalf("empty body usage=%+v", got)
	}
}

func TestServeHTTP_OpenAIPromptCacheInjectionRetryReappliesServerState(t *testing.T) {
	var bodies []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(buf))
		w.Header().Set("Content-Type", "application/json")
		if len(bodies) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"unknown parameter prompt_cache_key"}}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"resp_new","choices":[{"message":{"role":"assistant","content":"ok"}}],"model":"gpt-5"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Secrets.Mode = "off"
	cfg.Proxy.ServerStateEnabled = true
	cfg.Proxy.OpenAIPromptCache.Enabled = true
	cfg.Proxy.OpenAIPromptCache.PromptCacheKeyStrategy = "static"
	cfg.Proxy.OpenAIPromptCache.StaticPromptCacheKey = "pc-key"
	cfg.Proxy.OpenAIPromptCache.MinTokens = 0
	p := New(cfg)
	p.serverState.Set("conv-cache", "resp_old")

	body := `{"model":"gpt-5","metadata":{"conversation_id":"conv-cache"},"messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(bodies) != 2 {
		t.Fatalf("expected rejected request + retry, got %d bodies", len(bodies))
	}
	if !strings.Contains(bodies[0], `"prompt_cache_key":"pc-key"`) || !strings.Contains(bodies[0], `"previous_response_id":"resp_old"`) {
		t.Fatalf("first body missing cache key or previous response id: %s", bodies[0])
	}
	if strings.Contains(bodies[1], "prompt_cache_key") || !strings.Contains(bodies[1], `"previous_response_id":"resp_old"`) {
		t.Fatalf("retry must remove cache hints and preserve server state: %s", bodies[1])
	}
}

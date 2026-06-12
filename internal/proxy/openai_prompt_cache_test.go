package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func TestInjectOpenAIPromptCache(t *testing.T) {
	cfg := config.Defaults()
	cfg.Proxy.OpenAIPromptCache.Enabled = true
	cfg.Proxy.OpenAIPromptCache.PromptCacheKeyStrategy = "model_session"
	cfg.Proxy.OpenAIPromptCache.Retention = "24h"
	cfg.Proxy.OpenAIPromptCache.MinTokens = 100
	p := New(cfg)

	body := []byte(`{"model":"gpt-5","messages":[{"role":"system","content":"` + strings.Repeat("stable ", 100) + `"},{"role":"user","content":"old"},{"role":"assistant","content":"ok"},{"role":"user","content":"hi"}]}`)
	out, decision := p.injectOpenAIPromptCache(types.OpenAI, body, "gpt-5", 200, "sess-1")
	if !decision.Applied || decision.Key == "" || decision.Retention != "24h" || decision.StablePrefixTokens == 0 || decision.StablePrefixHash == "" {
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
		cfg.Proxy.OpenAIPromptCache.Enabled = false
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
		_, decision := p.injectOpenAIPromptCache(types.OpenAI, []byte(`{"messages":[{"role":"system","content":"tiny"},{"role":"user","content":"old"},{"role":"assistant","content":"ok"},{"role":"user","content":"hi"}]}`), "gpt-5", 2000, "sess")
		if decision.Reason != "stable_prefix_below_min_tokens" {
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
	t.Run("unsupported_provider", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Proxy.OpenAIPromptCache.Enabled = true
		p := New(cfg)
		out, decision := p.injectOpenAIPromptCache(types.Anthropic, baseBody, "claude", 200, "sess")
		if decision.Applied || decision.Reason != "unsupported_provider" || string(out) != string(baseBody) {
			t.Fatalf("decision=%+v body=%s", decision, out)
		}
	})
	t.Run("no_stable_prefix", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Proxy.OpenAIPromptCache.Enabled = true
		p := New(cfg)
		out, decision := p.injectOpenAIPromptCache(types.OpenAI, []byte(`{}`), "gpt-5", 200, "sess")
		if decision.Applied || decision.Reason != "no_stable_prefix" || string(out) != `{}` {
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
		body := []byte(`{"model":"gpt-5","messages":[{"role":"system","content":"stable prefix"},{"role":"user","content":"old"},{"role":"assistant","content":"ok"},{"role":"user","content":"latest"}]}`)
		_, first := p.injectOpenAIPromptCache(types.OpenAI, body, "gpt-5", 10, "sess")
		_, second := p.injectOpenAIPromptCache(types.OpenAI, body, "gpt-5", 10, "sess")
		if !first.Applied || second.Applied || second.Reason != "rate_limited" {
			t.Fatalf("first=%+v second=%+v", first, second)
		}
	})
}

func TestBuildOpenAIPromptCacheKeyBranches(t *testing.T) {
	cfg := config.Defaults().Proxy.OpenAIPromptCache
	cfg.PromptCacheKeyStrategy = ""
	if got := buildOpenAIPromptCacheKey(cfg, "gpt-5", "sess-raw-secret", "prefix-hash"); got == "" || !strings.Contains(got, "model_stable_prefix") || strings.Contains(got, "sess-raw-secret") || strings.Contains(got, "prefix-hash") {
		t.Fatalf("default/model-stable key not hashed: %q", got)
	}
	cfg.PromptCacheKeyStrategy = "off"
	if got := buildOpenAIPromptCacheKey(cfg, "gpt-5", "sess", "prefix-hash"); got != "" {
		t.Fatalf("off key=%q", got)
	}
	cfg.PromptCacheKeyStrategy = "static"
	cfg.StaticPromptCacheKey = "  fixed-key  "
	if got := buildOpenAIPromptCacheKey(cfg, "gpt-5", "sess", "prefix-hash"); got != "fixed-key" {
		t.Fatalf("static key=%q", got)
	}
	cfg.PromptCacheKeyStrategy = "stable_prefix"
	stableA := buildOpenAIPromptCacheKey(cfg, "gpt-5", "sess-a", "prefix-hash")
	stableB := buildOpenAIPromptCacheKey(cfg, "gpt-5", "sess-b", "prefix-hash")
	if stableA == "" || stableA != stableB || !strings.Contains(stableA, "stable_prefix") {
		t.Fatalf("stable_prefix should reuse key across sessions: a=%q b=%q", stableA, stableB)
	}
	cfg.PromptCacheKeyStrategy = "model_stable_prefix"
	modelA := buildOpenAIPromptCacheKey(cfg, "gpt-5", "sess-a", "prefix-hash")
	modelB := buildOpenAIPromptCacheKey(cfg, "gpt-5", "sess-b", "prefix-hash")
	modelChanged := buildOpenAIPromptCacheKey(cfg, "gpt-5.1", "sess-a", "prefix-hash")
	prefixChanged := buildOpenAIPromptCacheKey(cfg, "gpt-5", "sess-a", "other-prefix")
	if modelA == "" || modelA != modelB || modelA == modelChanged || modelA == prefixChanged {
		t.Fatalf("model_stable_prefix key mismatch: a=%q b=%q model=%q prefix=%q", modelA, modelB, modelChanged, prefixChanged)
	}
	cfg.PromptCacheKeyStrategy = "session"
	sessionA := buildOpenAIPromptCacheKey(cfg, "gpt-5", "sess-a", "prefix-hash")
	sessionB := buildOpenAIPromptCacheKey(cfg, "gpt-5", "sess-b", "prefix-hash")
	if sessionA == "" || sessionA == sessionB {
		t.Fatalf("session keys must remain session-scoped: a=%q b=%q", sessionA, sessionB)
	}
	cfg.PromptCacheKeyStrategy = "unknown"
	if got := buildOpenAIPromptCacheKey(cfg, "gpt-5", "sess", "prefix-hash"); got != "" {
		t.Fatalf("unknown key=%q", got)
	}
	if got := hashedPromptCacheKey("session", "   "); got != "" {
		t.Fatalf("empty hash key=%q", got)
	}
}

func TestOpenAIStablePrefixPlannerKeyRotation(t *testing.T) {
	cfg := config.Defaults()
	cfg.Proxy.OpenAIPromptCache.Enabled = true
	cfg.Proxy.OpenAIPromptCache.PromptCacheKeyStrategy = "model_session"
	cfg.Proxy.OpenAIPromptCache.Retention = "off"
	cfg.Proxy.OpenAIPromptCache.MinTokens = 10
	p := New(cfg)

	body := func(stable, latest, tools string) []byte {
		return []byte(`{"model":"gpt-5","tools":[{"type":"function","function":{"name":"` + tools + `"}}],"messages":[{"role":"system","content":"` + stable + `"},{"role":"user","content":"old question"},{"role":"assistant","content":"old answer"},{"role":"user","content":"` + latest + `"}]}`)
	}
	_, first := p.injectOpenAIPromptCache(types.OpenAI, body(strings.Repeat("stable ", 30), "latest A", "read_file"), "gpt-5", 1000, "sess")
	_, latestChanged := p.injectOpenAIPromptCache(types.OpenAI, body(strings.Repeat("stable ", 30), "latest B", "read_file"), "gpt-5", 1000, "sess")
	_, stableChanged := p.injectOpenAIPromptCache(types.OpenAI, body(strings.Repeat("different ", 30), "latest A", "read_file"), "gpt-5", 1000, "sess")
	_, toolChanged := p.injectOpenAIPromptCache(types.OpenAI, body(strings.Repeat("stable ", 30), "latest A", "write_file"), "gpt-5", 1000, "sess")

	if !first.Applied || !latestChanged.Applied || !stableChanged.Applied || !toolChanged.Applied {
		t.Fatalf("expected all multi-turn plans to apply: first=%+v latest=%+v stable=%+v tool=%+v", first, latestChanged, stableChanged, toolChanged)
	}
	if first.Key != latestChanged.Key {
		t.Fatalf("latest user turn must not rotate key: first=%q latest=%q", first.Key, latestChanged.Key)
	}
	if first.Key == stableChanged.Key {
		t.Fatalf("stable prefix change must rotate key: %q", first.Key)
	}
	if first.Key == toolChanged.Key {
		t.Fatalf("tool schema change must rotate key: %q", first.Key)
	}
}

func TestOpenAIStablePrefixPlannerOneTurnDoesNotUseTotalTokens(t *testing.T) {
	cfg := config.Defaults()
	cfg.Proxy.OpenAIPromptCache.Enabled = true
	cfg.Proxy.OpenAIPromptCache.MinTokens = 10
	p := New(cfg)

	body := []byte(`{"model":"gpt-5","messages":[{"role":"user","content":"` + strings.Repeat("huge latest ", 100) + `"}]}`)
	out, decision := p.injectOpenAIPromptCache(types.OpenAI, body, "gpt-5", 2000, "sess")
	if decision.Applied || decision.Reason != "no_stable_prefix" || string(out) != string(body) {
		t.Fatalf("one-turn request should not get cache hints from total tokens: decision=%+v body=%s", decision, out)
	}
}

func TestPlanOpenAIStablePrefixBranches(t *testing.T) {
	if plan := planOpenAIStablePrefix([]byte(`{`)); plan.Detected || plan.Reason != "invalid_json" {
		t.Fatalf("invalid plan=%+v", plan)
	}
	body := []byte(`{
		"instructions":"keep stable",
		"system":"system text",
		"developer":{"role":"developer","content":"dev text"},
		"tools":[{"type":"function","function":{"name":"read_file"}}],
		"input":[{"role":"system","content":"old"},{"role":"user","content":"latest"}],
		"messages":[{"role":"system","content":"old"},{"role":"assistant","content":"ok"},{"role":"user","content":"latest"}]
	}`)
	plan := planOpenAIStablePrefix(body)
	if !plan.Detected || plan.Hash == "" || plan.Tokens == 0 || plan.Reason != "planned" {
		t.Fatalf("plan=%+v", plan)
	}
	if prefix, ok := stablePrefixArray(json.RawMessage(`[{"role":"user","content":"only"}]`)); ok || prefix != nil {
		t.Fatalf("single latest user should not produce prefix: %+v ok=%v", prefix, ok)
	}
	if prefix, ok := stablePrefixArray(json.RawMessage(`[{"role":"assistant","content":"no user"}]`)); ok || prefix != nil {
		t.Fatalf("no user should not produce prefix: %+v ok=%v", prefix, ok)
	}
	if prefix, ok := stablePrefixArray(json.RawMessage(`{"not":"array"}`)); ok || prefix != nil {
		t.Fatalf("non-array should not produce prefix: %+v ok=%v", prefix, ok)
	}
	if role := rawMessageRole(json.RawMessage(`{`)); role != "" {
		t.Fatalf("invalid role raw=%q", role)
	}
	if got := string(compactRawJSON(json.RawMessage(`{`))); got != "{" {
		t.Fatalf("invalid compact fallback=%q", got)
	}
	if got := mustMarshalRawArray([]json.RawMessage{json.RawMessage(`{`)}); got != nil {
		t.Fatalf("invalid raw array marshal should return nil: %s", got)
	}
}

func TestInjectOpenAIPromptCache_IdempotentAndScoped(t *testing.T) {
	cfg := config.Defaults()
	cfg.Proxy.OpenAIPromptCache.Enabled = true
	cfg.Proxy.OpenAIPromptCache.PromptCacheKeyStrategy = "session"
	cfg.Proxy.OpenAIPromptCache.Retention = "in_memory"
	cfg.Proxy.OpenAIPromptCache.MinTokens = 0
	p := New(cfg)

	body := []byte(`{"model":"gpt-5","prompt_cache_key":"caller","prompt_cache_retention":"in_memory","messages":[{"role":"system","content":"stable"},{"role":"user","content":"old"},{"role":"assistant","content":"ok"},{"role":"user","content":"latest"}]}`)
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

func TestOpenAIPromptCacheRejectedCooldown(t *testing.T) {
	cfg := config.Defaults()
	cfg.Proxy.OpenAIPromptCache.Enabled = true
	cfg.Proxy.OpenAIPromptCache.MinTokens = 0
	p := New(cfg)
	now := time.Unix(2000, 0)

	if p.openAIPromptCacheRejected(types.OpenAI, "gpt-5", now) {
		t.Fatal("unexpected rejection before mark")
	}
	p.markOpenAIPromptCacheRejected(types.OpenAI, "gpt-5", openAIPromptCacheUnsupportedFields{Key: true}, now)
	if !p.openAIPromptCacheRejected(types.OpenAI, "gpt-5", now.Add(time.Minute)) {
		t.Fatal("expected rejection cooldown")
	}
	if p.openAIPromptCacheRejected(types.OpenAI, "gpt-5", now.Add(openAIPromptCacheRejectTTL+time.Second)) {
		t.Fatal("cooldown should expire")
	}

	body := []byte(`{"model":"gpt-5","messages":[{"role":"system","content":"stable prefix"},{"role":"user","content":"old"},{"role":"assistant","content":"ok"},{"role":"user","content":"latest"}]}`)
	p.markOpenAIPromptCacheRejected(types.OpenAI, "gpt-5", openAIPromptCacheUnsupportedFields{Key: true}, time.Now())
	out, decision := p.injectOpenAIPromptCache(types.OpenAI, body, "gpt-5", 100, "sess")
	if decision.Applied || decision.Reason != "prompt_cache_key_rejected_cooldown" || string(out) != string(body) {
		t.Fatalf("decision=%+v body=%s", decision, out)
	}
}

func TestOpenAIPromptCacheFieldRejectedCooldownIsPrecise(t *testing.T) {
	body := []byte(`{"model":"gpt-5","messages":[{"role":"system","content":"stable prefix"},{"role":"user","content":"old"},{"role":"assistant","content":"ok"},{"role":"user","content":"latest"}]}`)

	cfg := config.Defaults()
	cfg.Proxy.OpenAIPromptCache.Enabled = true
	cfg.Proxy.OpenAIPromptCache.PromptCacheKeyStrategy = "static"
	cfg.Proxy.OpenAIPromptCache.StaticPromptCacheKey = "still-usable-key"
	cfg.Proxy.OpenAIPromptCache.Retention = "in_memory"
	cfg.Proxy.OpenAIPromptCache.MinTokens = 0
	keyProxy := New(cfg)
	keyProxy.markOpenAIPromptCacheRejected(types.OpenAI, "gpt-5", openAIPromptCacheUnsupportedFields{Key: true}, time.Now())
	out, decision := keyProxy.injectOpenAIPromptCache(types.OpenAI, body, "gpt-5", 2000, "sess")
	if !decision.Applied || decision.Key != "" || decision.Retention != "in_memory" || strings.Contains(string(out), "prompt_cache_key") || !strings.Contains(string(out), `"prompt_cache_retention":"in_memory"`) {
		t.Fatalf("key cooldown should keep retention usable: decision=%+v body=%s", decision, out)
	}

	retentionProxy := New(cfg)
	retentionProxy.markOpenAIPromptCacheRejected(types.OpenAI, "gpt-5", openAIPromptCacheUnsupportedFields{Retention: true}, time.Now())
	out, decision = retentionProxy.injectOpenAIPromptCache(types.OpenAI, body, "gpt-5", 2000, "sess")
	if !decision.Applied || decision.Key != "still-usable-key" || decision.Retention != "" || !strings.Contains(string(out), `"prompt_cache_key":"still-usable-key"`) || strings.Contains(string(out), "prompt_cache_retention") {
		t.Fatalf("retention cooldown should keep key usable: decision=%+v body=%s", decision, out)
	}
}

func TestOpenAIPromptCacheNegativeNetCooldown(t *testing.T) {
	cfg := config.Defaults()
	cfg.Proxy.OpenAIPromptCache.Enabled = true
	cfg.Proxy.OpenAIPromptCache.PromptCacheKeyStrategy = "static"
	cfg.Proxy.OpenAIPromptCache.StaticPromptCacheKey = "lossy-key"
	cfg.Proxy.OpenAIPromptCache.MinTokens = 0
	cfg.Proxy.OpenAIPromptCache.Retention = "off"
	p := New(cfg)
	body := []byte(`{"model":"gpt-5","messages":[{"role":"system","content":"stable prefix for cache"},{"role":"user","content":"old"},{"role":"assistant","content":"ok"},{"role":"user","content":"latest"}]}`)

	now := time.Now()
	p.observeOpenAIPromptCacheNet(types.OpenAI, "gpt-5", openAIPromptCacheDecision{
		Applied: true,
		Key:     "lossy-key",
	}, cacheUsage{CreateTokens: openAIPromptCacheNetMinLossTokens}, now)
	if p.openAIPromptCacheKeyRejected(types.OpenAI, "gpt-5", "lossy-key", now.Add(time.Second)) {
		t.Fatal("one create-only warmup should not reject the key")
	}
	for i := 1; i < openAIPromptCacheNetMinNegativeSamples; i++ {
		p.observeOpenAIPromptCacheNet(types.OpenAI, "gpt-5", openAIPromptCacheDecision{
			Applied: true,
			Key:     "lossy-key",
		}, cacheUsage{CreateTokens: openAIPromptCacheNetMinLossTokens}, now.Add(time.Duration(i)*time.Second))
	}

	out, decision := p.injectOpenAIPromptCache(types.OpenAI, body, "gpt-5", 2000, "sess")
	if decision.Applied || decision.Reason != "negative_net_cooldown" || strings.Contains(string(out), "prompt_cache_key") {
		t.Fatalf("expected negative-net cooldown: decision=%+v body=%s", decision, out)
	}
	if !p.openAIPromptCacheKeyRejected(types.OpenAI, "gpt-5", "lossy-key", now.Add(time.Minute)) {
		t.Fatal("key should be rejected during cooldown")
	}
	if p.openAIPromptCacheKeyRejected(types.OpenAI, "gpt-5", "lossy-key", now.Add(openAIPromptCacheRejectTTL+5*time.Second)) {
		t.Fatal("key rejection should expire")
	}
	p.config.Proxy.OpenAIPromptCache.StaticPromptCacheKey = "healthy-key"
	out, decision = p.injectOpenAIPromptCache(types.OpenAI, body, "gpt-5", 2000, "sess")
	if !decision.Applied || decision.Key != "healthy-key" || !strings.Contains(string(out), `"prompt_cache_key"`) {
		t.Fatalf("other keys should keep working: decision=%+v body=%s", decision, out)
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
	retentionResp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(strings.NewReader(`{"error":"unsupported prompt_cache_retention"}`)),
	}
	fields, ok := peekPromptCacheUnsupportedFields(retentionResp)
	if !ok || fields.Key || !fields.Retention {
		t.Fatalf("expected retention-only unsupported fields: ok=%v fields=%+v", ok, fields)
	}
	genericResp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(strings.NewReader(`{"error":"prompt_cache is not supported"}`)),
	}
	fields, ok = peekPromptCacheUnsupportedFields(genericResp)
	if !ok || !fields.Key || !fields.Retention {
		t.Fatalf("generic prompt_cache rejection should cool down both fields: ok=%v fields=%+v", ok, fields)
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
	cfg.Secrets.Mode = "off"
	cfg.Proxy.ServerStateEnabled = true
	cfg.Proxy.OpenAIPromptCache.Enabled = true
	cfg.Proxy.OpenAIPromptCache.PromptCacheKeyStrategy = "static"
	cfg.Proxy.OpenAIPromptCache.StaticPromptCacheKey = "pc-key"
	cfg.Proxy.OpenAIPromptCache.Retention = "in_memory"
	cfg.Proxy.OpenAIPromptCache.MinTokens = 0
	p := New(cfg)
	p.serverState.Set("conv-cache", "resp_old")

	body := `{"model":"gpt-5","metadata":{"conversation_id":"conv-cache"},"messages":[{"role":"system","content":"stable prefix"},{"role":"user","content":"old"},{"role":"assistant","content":"ok"},{"role":"user","content":"hello"}]}`
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
	if !strings.Contains(bodies[0], `"prompt_cache_key":"pc-key"`) || !strings.Contains(bodies[0], `"prompt_cache_retention":"in_memory"`) || !strings.Contains(bodies[0], `"previous_response_id":"resp_old"`) {
		t.Fatalf("first body missing cache key or previous response id: %s", bodies[0])
	}
	if strings.Contains(bodies[1], "prompt_cache_key") || strings.Contains(bodies[1], "prompt_cache_retention") || !strings.Contains(bodies[1], `"previous_response_id":"resp_old"`) {
		t.Fatalf("retry must remove cache hints and preserve server state: %s", bodies[1])
	}
	if !p.openAIPromptCacheRejected(types.OpenAI, "gpt-5", time.Now()) {
		t.Fatal("rejected cache hints should activate field cooldown")
	}
	if p.openAIPromptCacheFieldRejected(types.OpenAI, "gpt-5", openAIPromptCacheFieldRetention, time.Now()) {
		t.Fatal("prompt_cache_key rejection must not cool down retention")
	}
	out, decision := p.injectOpenAIPromptCache(types.OpenAI, []byte(body), "gpt-5", 2000, "sess")
	if !decision.Applied || decision.Key != "" || decision.Retention != "in_memory" || strings.Contains(string(out), "prompt_cache_key") || !strings.Contains(string(out), `"prompt_cache_retention":"in_memory"`) {
		t.Fatalf("future requests should keep retention while key is cooling down: decision=%+v body=%s", decision, out)
	}
}

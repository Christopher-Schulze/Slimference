package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/compactsignal"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/summarization"
	"github.com/slimference/slimference/internal/types"
)

// TestServeHTTP_compressibleAnthropic exercises handleCompressibleRequest → upstream round-trip.
func TestServeHTTP_compressibleAnthropic(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Secrets.Mode = "off"

	p := New(cfg)

	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":256,"messages":[{"role":"user","content":"Hello world"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %s", res.StatusCode, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"text":"ok"`) {
		t.Fatalf("upstream body not forwarded: %s", rec.Body.String())
	}
}

func TestServeHTTP_AdaptiveWindowAndLayer2CacheApplied(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = true
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.SlidingWindow = 5
	cfg.Compression.Tuning.AdaptiveWindowEnabled = true
	cfg.Compression.Tuning.AdaptiveWindowMin = 3
	cfg.Compression.Tuning.AdaptiveWindowMax = 3
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	p.layer2.GetSessionCache().Store("anthropic:trace-window", &summarization.CachedSummary{
		Summary:          "prior context",
		CoveredRange:     [2]int{0, 2},
		OriginalTokens:   100,
		CompressedTokens: 20,
		CreatedAt:        time.Now(),
	})

	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":256,"messages":[` +
		`{"role":"user","content":"m1"},` +
		`{"role":"assistant","content":"m2"},` +
		`{"role":"user","content":"m3"},` +
		`{"role":"assistant","content":"m4"},` +
		`{"role":"user","content":"m5"},` +
		`{"role":"assistant","content":"m6"},` +
		`{"role":"user","content":"m7"}` +
		`]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-trace-id", "trace-window")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %s", res.StatusCode, rec.Body.String())
	}
}

func TestServeHTTP_compressibleOpenAI(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":"done"}}],"model":"gpt-4"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = upstream.URL
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Secrets.Mode = "off"

	p := New(cfg)

	body := `{"model":"gpt-4","messages":[{"role":"user","content":"ping"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "done") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

func TestServeHTTP_compressibleStreamingAnthropic(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":12}}\n")
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"stream"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "message_delta") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

func TestServeHTTP_compressibleStreamingOpenAI(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `data: {"usage":{"completion_tokens":9},"choices":[]}`+"\n")
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	body := `{"model":"gpt-4","stream":true,"messages":[{"role":"user","content":"stream"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "completion_tokens") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

func TestServeHTTP_compressibleMalformedJSON(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Secrets.Mode = "off"
	p := New(cfg)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{not-json`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", res.StatusCode, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "parse request") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

// TestServeHTTP_layer3CacheHit verifies identical compressible requests hit the response cache (upstream once).
func TestServeHTTP_layer3CacheHit(t *testing.T) {
	t.Parallel()
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"cached","type":"message","role":"assistant","content":[{"type":"text","text":"hit"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = true
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"messages":[{"role":"user","content":"cache probe"}]}`
	post := func() string {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		res := rec.Result()
		t.Cleanup(func() { _ = res.Body.Close() })
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
		}
		return rec.Body.String()
	}

	out1 := post()
	out2 := post()
	if upstreamCalls.Load() != 1 {
		t.Fatalf("upstream calls: want 1, got %d", upstreamCalls.Load())
	}
	if out1 != out2 {
		t.Fatalf("responses differ:\n1=%q\n2=%q", out1, out2)
	}
	if !strings.Contains(out1, `"text":"hit"`) {
		t.Fatalf("body: %s", out1)
	}
}

func TestServeHTTP_compressibleUpstreamUnreachable(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = "http://127.0.0.1:1"
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Secrets.Mode = "off"
	p := New(cfg)

	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":10,"messages":[{"role":"user","content":"x"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("want 502, got %d: %s", res.StatusCode, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "upstream") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

func TestServeHTTP_compressibleOpenAIUpstreamUnreachable(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = "http://127.0.0.1:1"
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Secrets.Mode = "off"
	p := New(cfg)

	body := `{"model":"gpt-4","messages":[{"role":"user","content":"x"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("want 502, got %d: %s", res.StatusCode, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "upstream") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

// TestServeHTTP_compressibleSecretsBlock verifies secrets.Mode=block returns 400 before upstream.
func TestServeHTTP_compressibleSecretsBlock(t *testing.T) {
	t.Parallel()
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Secrets.Mode = "block"

	p := New(cfg)
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"messages":[{"role":"user","content":"key AKIAIOSFODNN7EXAMPLE"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", res.StatusCode, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("body: %s", rec.Body.String())
	}
	if upstreamCalls.Load() != 0 {
		t.Fatalf("upstream should not be called, got %d calls", upstreamCalls.Load())
	}
}

// TestServeHTTP_compressibleSecretsRedact verifies default redact mode forwards a scrubbed body upstream.
func TestServeHTTP_compressibleSecretsRedact(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upBody, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s := string(upBody)
		if strings.Contains(s, "AKIAIOSFODNN7EXAMPLE") {
			http.Error(w, "raw secret leaked", http.StatusBadRequest)
			return
		}
		if !strings.Contains(s, "[REDACTED") {
			http.Error(w, "expected redaction marker in forwarded body", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Secrets.Mode = "redact"

	p := New(cfg)
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"messages":[{"role":"user","content":"key AKIAIOSFODNN7EXAMPLE"}]}`
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

// TestServeHTTP_compressibleSecretsWarn verifies warn mode forwards the original secret text upstream (no redaction).
func TestServeHTTP_compressibleSecretsWarn(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upBody, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !strings.Contains(string(upBody), "AKIAIOSFODNN7EXAMPLE") {
			http.Error(w, "warn mode should forward request without redacting", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"w1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Secrets.Mode = "warn"

	p := New(cfg)
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"messages":[{"role":"user","content":"key AKIAIOSFODNN7EXAMPLE"}]}`
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

// TestServeHTTP_compressibleSecretsUnknownMode behaves like warn (detector default).
func TestServeHTTP_compressibleSecretsUnknownMode(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upBody, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !strings.Contains(string(upBody), "AKIAIOSFODNN7EXAMPLE") {
			http.Error(w, "unknown mode should behave like warn (no redaction)", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"u1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Secrets.Mode = "not-a-valid-mode"

	p := New(cfg)
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"messages":[{"role":"user","content":"key AKIAIOSFODNN7EXAMPLE"}]}`
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

// anthropicLongConversationJSON returns a valid Anthropic request with nPairs user/assistant turns (2*nPairs messages).
func anthropicLongConversationJSON(nPairs int) string {
	var b strings.Builder
	b.WriteString(`{"model":"claude-3-5-sonnet-20241022","max_tokens":1024,"messages":[`)
	for i := 0; i < nPairs; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"role":"user","content":"`)
		b.WriteString(strings.Repeat("u", 24))
		b.WriteString(`"},{"role":"assistant","content":"`)
		b.WriteString(strings.Repeat("a", 24))
		b.WriteString(`"}`)
	}
	b.WriteString(`]}`)
	return b.String()
}

// anthropicConversationWithToolResults builds a valid Anthropic request JSON with nExchanges
// user/assistant turn pairs followed by a user turn containing a tool_result with prettyJSON content.
// This is used to push messages outside the sliding window so Layer 1 compresses them.
func anthropicConversationWithToolResults(nExchanges int, prettyJSON string) string {
	type block struct {
		Type      string `json:"type"`
		ToolUseID string `json:"tool_use_id,omitempty"`
		Content   string `json:"content,omitempty"`
		Text      string `json:"text,omitempty"`
	}
	type message struct {
		Role    string  `json:"role"`
		Content []block `json:"content"`
	}
	type request struct {
		Model     string    `json:"model"`
		MaxTokens int       `json:"max_tokens"`
		Messages  []message `json:"messages"`
	}

	msgs := make([]message, 0, nExchanges*2+1)
	// Old exchanges that will fall outside the sliding window.
	for i := 0; i < nExchanges; i++ {
		msgs = append(msgs,
			message{Role: "user", Content: []block{{Type: "tool_result", ToolUseID: "tu_old", Content: prettyJSON}}},
			message{Role: "assistant", Content: []block{{Type: "text", Text: "ok"}}},
		)
	}
	// Final user turn (inside window, not compressed).
	msgs = append(msgs, message{Role: "user", Content: []block{{Type: "text", Text: "final"}}})

	req := request{
		Model:     "claude-3-5-sonnet-20241022",
		MaxTokens: 64,
		Messages:  msgs,
	}
	b, _ := json.Marshal(req)
	return string(b)
}

// TestServeHTTP_layer1Applied covers handler.go lines 108-118 (layer1 applied, TokensSaved > 0).
// Layer1 only compresses messages outside the sliding window. We create a conversation with
// nExchanges > SlidingWindow, each turn having a tool_result with compressible JSON content.
func TestServeHTTP_layer1Applied(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = true
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Secrets.Mode = "off"
	// SlidingWindow=5 by default; use 7 exchanges to ensure messages fall outside the window.
	// prettyJSON has enough whitespace to exceed the 10% savings threshold.
	prettyJSON := `{ "key1": "value1",  "key2": "value2",  "key3": "value3",  "key4": "value4",  "key5": "value5",  "key6": "value6",  "key7": "value7",  "key8": "value8" }`

	p := New(cfg)
	body := anthropicConversationWithToolResults(7, prettyJSON)
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

// TestServeHTTP_layer2Applied covers handler.go lines 123-128 (layer2 applied, tokensSaved > 0).
// Pre-populates the Layer2 cache with a summary so ApplyToMessages returns applied=true.
func TestServeHTTP_layer2Applied(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"ok","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = true
	cfg.Compression.Layer3Enabled = false
	cfg.Secrets.Mode = "off"

	p := New(cfg)

	// A conversation with 8 user turns so the sliding window (5) leaves 3 messages outside.
	// Pre-seed the summary cache to cover messages 0-4 (indices 0,1,2,3,4 = 5 messages).
	// The conversation has 8 exchanges (16 messages) + final user = 17, so end=4 < 17.
	// ApplyToMessages returns applied=true.
	cache := p.layer2.GetCache()
	cache.Store(&summarization.CachedSummary{
		Summary:          "Summary of old messages.",
		CoveredRange:     [2]int{0, 4},
		OriginalTokens:   1000,
		CompressedTokens: 100,
		CompressionRatio: 0.1,
		CreatedAt:        time.Now(),
	})

	body := anthropicLongConversationJSON(8) // 8 exchanges = 16 messages + final user = 17
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

// TestServeHTTP_layer2CompressQueueEnqueue covers handler.go line 200 (successful compressQueue enqueue).
// Uses a long conversation (> SlidingWindow exchanges) with layer2 enabled and an empty queue,
// so ShouldTriggerCompression returns true and the job is enqueued successfully.
func TestServeHTTP_layer2CompressQueueEnqueue(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"ok","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.Layer2Enabled = true
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	// Queue starts empty; long conversation triggers ShouldTriggerCompression -> enqueue succeeds (line 200).
	body := anthropicLongConversationJSON(10)
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

// TestServeHTTP_promptCacheBreakpointsInjected verifies L1.6: after Layer 1 compression,
// Anthropic prompt cache breakpoints (cache_control: {type: "ephemeral"}) are injected into
// the stable prefix messages before the request is forwarded upstream.
func TestServeHTTP_promptCacheBreakpointsInjected(t *testing.T) {
	t.Parallel()

	var capturedBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = true
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.SlidingWindow = 5
	cfg.Secrets.Mode = "off"

	// Build a conversation: 7 user/assistant exchanges + 1 final user message = 15 messages total.
	// With SlidingWindow=5, userIdx=[0,2,4,6,8,10,12,14], stableBoundary=userIdx[8-5]=userIdx[3]=6.
	// Messages 0..5 form the stable prefix (3 user + 3 assistant).
	// User messages have 1500 chars each → 3 * 1500 = 4500 chars > 4096 (minStablePrefixTokens*charsPerToken).
	largeContent := strings.Repeat("x", 1500)
	body := anthropicConversationForCacheTest(7, largeContent)

	p := New(cfg)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}

	// Parse the upstream request and check for cache_control injection.
	// Content can be a string (simple text) or an array (with cache_control), so use RawMessage.
	var upstreamReq struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(capturedBody, &upstreamReq); err != nil {
		t.Fatalf("parse upstream body: %v\nbody: %s", err, capturedBody)
	}

	var found int
	for _, msg := range upstreamReq.Messages {
		// Content may be a string or an array of blocks.
		var blocks []struct {
			CacheControl *struct {
				Type string `json:"type"`
			} `json:"cache_control,omitempty"`
		}
		if err := json.Unmarshal(msg.Content, &blocks); err != nil {
			continue // string content - no cache_control possible
		}
		for _, blk := range blocks {
			if blk.CacheControl != nil && blk.CacheControl.Type == "ephemeral" {
				found++
			}
		}
	}
	if found == 0 {
		t.Fatalf("no cache_control:ephemeral breakpoints found in upstream request; messages=%d", len(upstreamReq.Messages))
	}
}

// anthropicConversationForCacheTest builds an Anthropic request with nExchanges user/assistant
// turns, each with largeContent as the user message body, plus a final short user turn.
// The large stable prefix ensures prompt cache breakpoint injection triggers.
func anthropicConversationForCacheTest(nExchanges int, largeContent string) string {
	type block struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type message struct {
		Role    string  `json:"role"`
		Content []block `json:"content"`
	}
	type request struct {
		Model     string    `json:"model"`
		MaxTokens int       `json:"max_tokens"`
		Messages  []message `json:"messages"`
	}

	msgs := make([]message, 0, nExchanges*2+1)
	for i := 0; i < nExchanges; i++ {
		msgs = append(msgs,
			message{Role: "user", Content: []block{{Type: "text", Text: largeContent}}},
			message{Role: "assistant", Content: []block{{Type: "text", Text: "ok"}}},
		)
	}
	msgs = append(msgs, message{Role: "user", Content: []block{{Type: "text", Text: "what next?"}}})

	req := request{Model: "claude-3-5-sonnet-20241022", MaxTokens: 64, Messages: msgs}
	b, _ := json.Marshal(req)
	return string(b)
}

// TestServeHTTP_compressQueueFullSkipsEnqueue fills the async Layer 2 queue so the next enqueue hits select default.
func TestServeHTTP_compressQueueFullSkipsEnqueue(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"ok","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.Layer2Enabled = true
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	dummy := types.CompressJob{
		Messages: []types.Message{
			{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "x"}}},
		},
		Timestamp: time.Now(),
	}
	for range 4 {
		p.compressQueue <- dummy
	}

	body := anthropicLongConversationJSON(10)
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

func TestServeHTTPPrecompactSignalShrinksWindow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"ok","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.SlidingWindow = 4
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Secrets.Mode = "off"
	p := New(cfg)
	if err := p.compactSignals.WriteMarker(compactsignal.PhasePre, "anthropic:trace-pre", "", "test"); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(anthropicLongConversationJSON(10)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-trace-id", "trace-pre")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestServeHTTPAnthropicPromptCacheHotBranch(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"ok","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.SlidingWindow = 1
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Secrets.Mode = "off"
	p := New(cfg)
	body := anthropicLongConversationJSON(4)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("anthropic-trace-id", "trace-hot")
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("run %d status %d body %s", i, rec.Code, rec.Body.String())
		}
	}
}

package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// TestServeHTTP_serverStateCaptureAndReuse exercises the full T78 happy
// path: first OpenAI request stores the response id; second request
// (same conversation_id) is rewritten to use previous_response_id so
// the upstream sees a single-message body instead of the full history.
func TestServeHTTP_serverStateCaptureAndReuse(t *testing.T) {
	t.Parallel()
	var captured atomic.Value // []byte
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		captured.Store(buf)
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"resp_xyz","choices":[{"message":{"role":"assistant","content":"ok"}}],"model":"gpt-4"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Secrets.Mode = "off"
	cfg.Proxy.ServerStateEnabled = true

	p := New(cfg)

	body := `{"model":"gpt-4","metadata":{"conversation_id":"conv-T78"},"messages":[{"role":"user","content":"first"}]}`
	post := func(b string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		res := rec.Result()
		t.Cleanup(func() { _ = res.Body.Close() })
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
		}
	}

	post(body)
	if calls.Load() != 1 {
		t.Fatalf("first call count: %d", calls.Load())
	}
	first := string(captured.Load().([]byte))
	if strings.Contains(first, "previous_response_id") {
		t.Fatalf("first request must not carry previous_response_id: %s", first)
	}
	if p.serverState.Get("conv-T78") != "resp_xyz" {
		t.Fatalf("response id not captured: %q", p.serverState.Get("conv-T78"))
	}

	// Second turn with full history: the rewrite must collapse it to
	// the last user turn + previous_response_id.
	body2 := `{"model":"gpt-4","metadata":{"conversation_id":"conv-T78"},"messages":[{"role":"user","content":"first"},{"role":"assistant","content":"ok"},{"role":"user","content":"second"}]}`
	post(body2)
	if calls.Load() != 2 {
		t.Fatalf("second call count: %d", calls.Load())
	}
	second := string(captured.Load().([]byte))
	if !strings.Contains(second, `"previous_response_id":"resp_xyz"`) {
		t.Fatalf("second request must include previous_response_id: %s", second)
	}
	if strings.Contains(second, `"first"`) || strings.Contains(second, `"ok"`) {
		t.Fatalf("second request must drop history: %s", second)
	}
	if !strings.Contains(second, `"second"`) {
		t.Fatalf("second request must keep last user turn: %s", second)
	}
	if p.serverState.SkipTotal() != 1 {
		t.Fatalf("skip counter: %d", p.serverState.SkipTotal())
	}
}

// TestServeHTTP_serverStateRecoveryOnUnknownPreviousID exercises the
// T78 recovery branch: upstream rejects the rewritten request with
// "previous_response_id not found", the proxy forgets the anchor and
// retries with the full body.
func TestServeHTTP_serverStateRecoveryOnUnknownPreviousID(t *testing.T) {
	t.Parallel()
	var bodies []string
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		n := calls.Add(1)
		bodies = append(bodies, string(buf))
		if n == 1 {
			// Reject the rewritten request.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"message":"previous_response_id not found","type":"invalid_request_error"}}`)
			return
		}
		// Recovery retry succeeds.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"resp_new","choices":[{"message":{"role":"assistant","content":"ok"}}],"model":"gpt-4"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Secrets.Mode = "off"
	cfg.Proxy.ServerStateEnabled = true

	p := New(cfg)
	// Pre-seed the store with a stale anchor.
	p.serverState.Set("conv-rec", "resp_stale")

	body := `{"model":"gpt-4","metadata":{"conversation_id":"conv-rec"},"messages":[{"role":"user","content":"first"},{"role":"assistant","content":"ok"},{"role":"user","content":"second"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected recovery retry to succeed, got %d: %s", res.StatusCode, rec.Body.String())
	}
	if calls.Load() != 2 {
		t.Fatalf("expected exactly 2 upstream calls (rejected + retry), got %d", calls.Load())
	}
	if !strings.Contains(bodies[0], `"previous_response_id":"resp_stale"`) {
		t.Fatalf("first attempt must have been rewritten: %s", bodies[0])
	}
	if !strings.Contains(bodies[1], `"first"`) || !strings.Contains(bodies[1], `"second"`) {
		t.Fatalf("retry must resend full body: %s", bodies[1])
	}
	if strings.Contains(bodies[1], "previous_response_id") {
		t.Fatalf("retry must not include previous_response_id: %s", bodies[1])
	}
	if p.serverState.RecoverTotal() != 1 {
		t.Fatalf("recover counter: %d", p.serverState.RecoverTotal())
	}
	if p.serverState.Get("conv-rec") != "resp_new" {
		t.Fatalf("response id from recovery retry not captured: %q", p.serverState.Get("conv-rec"))
	}
}

// TestServeHTTP_serverStateDisabledByFlag verifies the flag really
// gates the behaviour: with ServerStateEnabled=false the upstream sees
// the full body and the store stays empty, even though the default is
// now on.
func TestServeHTTP_serverStateDisabledByFlag(t *testing.T) {
	t.Parallel()
	var captured atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		captured.Store(buf)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"resp_off","choices":[{"message":{"role":"assistant","content":"ok"}}],"model":"gpt-4"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Secrets.Mode = "off"
	cfg.Proxy.ServerStateEnabled = false

	p := New(cfg)
	p.serverState.Set("conv-X", "resp_should_be_unused")

	body := `{"model":"gpt-4","metadata":{"conversation_id":"conv-X"},"messages":[{"role":"user","content":"first"},{"role":"user","content":"second"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	got := string(captured.Load().([]byte))
	if strings.Contains(got, "previous_response_id") {
		t.Fatalf("flag off must not rewrite: %s", got)
	}
	if p.serverState.SkipTotal() != 0 {
		t.Fatalf("skip total must stay zero: %d", p.serverState.SkipTotal())
	}
}

// TestServeHTTP_serverStateAnthropicNoRegression verifies Anthropic
// requests are untouched even when the flag is on (capability map says
// SupportsResponseID=false for Anthropic).
func TestServeHTTP_serverStateAnthropicNoRegression(t *testing.T) {
	t.Parallel()
	var captured atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		captured.Store(buf)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Secrets.Mode = "off"
	cfg.Proxy.ServerStateEnabled = true

	p := New(cfg)
	p.serverState.Set("any", "resp_anthropic")

	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	got := string(captured.Load().([]byte))
	if strings.Contains(got, "previous_response_id") {
		t.Fatalf("anthropic must never carry previous_response_id: %s", got)
	}
}

// TestServerStateEnabledByDefault verifies the §3.4 handbrake removal:
// config.Defaults() must return ServerStateEnabled=true so the L1 lever
// is active without manual configuration. The fail-open path (tested
// above) makes default-on safe.
func TestServerStateEnabledByDefault(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	if !cfg.Proxy.ServerStateEnabled {
		t.Fatal("ServerStateEnabled must default to true (AGENTS.md §3.4: no default-off without a proven drawdown vector)")
	}
}

func TestServeHTTP_serverStateL1SavingsMeasured(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"resp_l1","choices":[{"message":{"role":"assistant","content":"ok"}}],"model":"gpt-4","usage":{"prompt_tokens":1000,"completion_tokens":50}}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Secrets.Mode = "off"
	cfg.Proxy.ServerStateEnabled = true

	p := New(cfg)

	// First request: stores response ID.
	body1 := `{"model":"gpt-4","metadata":{"conversation_id":"conv-L1-test"},"messages":[{"role":"user","content":"first turn with enough text to be meaningful"}]}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body1))
	rec1 := httptest.NewRecorder()
	p.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request status %d", rec1.Code)
	}

	// Drain first analytics event.
	<-p.analyticsQueue

	// Second request: full history, should be rewritten with previous_response_id.
	body2 := `{"model":"gpt-4","metadata":{"conversation_id":"conv-L1-test"},"messages":[{"role":"user","content":"first turn with enough text to be meaningful"},{"role":"assistant","content":"ok"},{"role":"user","content":"second turn also with enough text to be meaningful for testing"}]}`
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body2))
	rec2 := httptest.NewRecorder()
	p.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second request status %d", rec2.Code)
	}

	// Read the analytics event for the second request.
	select {
	case ev := <-p.analyticsQueue:
		if ev.Type != types.EventRequestProcessed {
			t.Fatalf("expected EventRequestProcessed, got %v", ev.Type)
		}
		if ev.InputTokensOrig <= 0 {
			t.Fatalf("InputTokensOrig = %d, must be > 0", ev.InputTokensOrig)
		}
		// L1 savings: InputTokensComp should be much smaller than InputTokensOrig
		// because the history was replaced with previous_response_id.
		if ev.InputTokensComp >= ev.InputTokensOrig {
			t.Fatalf("L1 savings not measured: orig=%d comp=%d (comp must be < orig when server-state continuation is used)", ev.InputTokensOrig, ev.InputTokensComp)
		}
		saved := ev.InputTokensOrig - ev.InputTokensComp
		if saved <= 0 {
			t.Fatalf("L1 saved = %d, must be > 0", saved)
		}
	default:
		t.Fatal("no analytics event received for second request")
	}
}

package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
)

func TestServeHTTP_OutputReduceInjectsBeforeUpstream(t *testing.T) {
	t.Parallel()
	var captured []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"output_tokens":12}}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.OutputReduce.Enabled = true
	cfg.Compression.OutputReduce.MinInputTokens = 1
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"messages":[{"role":"user","content":"` + strings.Repeat("please edit tersely ", 40) + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if !strings.Contains(string(captured), "#slimference-output-rules") {
		t.Fatalf("directive not injected: %s", captured)
	}
	snap := p.outputReduce.Snapshot()
	if snap.InjectedTurns != 1 || snap.InputOverheadTokens == 0 || snap.OutputTokensObserved == 0 {
		t.Fatalf("output-reduce snapshot: %+v", snap)
	}
}

func TestServeHTTP_OutputReduceSkipsBelowMinTokens(t *testing.T) {
	t.Parallel()
	var captured []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}]}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.OutputReduce.Enabled = true
	cfg.Compression.OutputReduce.MinInputTokens = 10_000
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if strings.Contains(string(captured), "#slimference-output-rules") {
		t.Fatalf("directive injected below min: %s", captured)
	}
	snap := p.outputReduce.Snapshot()
	if snap.InjectedTurns != 0 || snap.SkippedTurns != 1 || snap.LastReason != "below_min_tokens" {
		t.Fatalf("output-reduce snapshot: %+v", snap)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(captured, &root); err != nil {
		t.Fatal(err)
	}
	if _, ok := root["system"]; ok {
		t.Fatalf("unexpected system field: %s", captured)
	}
}

func TestServeHTTP_OutputReduceInjectionErrorFallsBackToOriginal(t *testing.T) {
	t.Parallel()
	var captured []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}]}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.OutputReduce.Enabled = true
	cfg.Compression.OutputReduce.MinInputTokens = 1
	cfg.Compression.OutputReduce.CustomDirectivePath = "/definitely/missing/slimference-output-rules.md"
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"messages":[{"role":"user","content":"` + strings.Repeat("please edit tersely ", 40) + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if strings.Contains(string(captured), "#slimference-output-rules") {
		t.Fatalf("directive injected after custom directive read error: %s", captured)
	}
	snap := p.outputReduce.Snapshot()
	if snap.InjectedTurns != 0 || snap.SkippedTurns != 1 || snap.LastReason != "error" {
		t.Fatalf("output-reduce snapshot after injection error: %+v", snap)
	}
}

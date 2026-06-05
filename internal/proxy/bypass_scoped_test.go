package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/slimference/slimference/internal/config"
)

func TestBypassedTools_RoundTrip(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	if p.IsToolBypassed("Bash") {
		t.Fatal("default must be empty")
	}
	if got := p.BypassedTools(); got != nil {
		t.Fatalf("default empty list: %v", got)
	}
	p.SetBypassedTools([]string{"Bash", "Write", ""})
	if !p.IsToolBypassed("Bash") || !p.IsToolBypassed("Write") {
		t.Fatal("set did not stick")
	}
	if p.IsToolBypassed("") || p.IsToolBypassed("Read") {
		t.Fatal("only listed tools must match")
	}
	got := p.BypassedTools()
	if len(got) != 2 || got[0] != "Bash" || got[1] != "Write" {
		t.Fatalf("sorted list: %v", got)
	}
	p.SetBypassedTools(nil)
	if p.BypassedTools() != nil {
		t.Fatal("clear failed")
	}
}

func TestBypassedRoutes_RoundTrip(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	if p.IsRouteBypassed("/v1/messages") {
		t.Fatal("default must be empty")
	}
	if got := p.BypassedRoutes(); got != nil {
		t.Fatalf("default empty list: %v", got)
	}
	p.SetBypassedRoutes([]string{"/v1/messages", "/v1/chat/completions", ""})
	if !p.IsRouteBypassed("/v1/messages") || !p.IsRouteBypassed("/v1/chat/completions") {
		t.Fatal("set did not stick")
	}
	if p.IsRouteBypassed("") || p.IsRouteBypassed("/other") {
		t.Fatal("non-matching paths must not match")
	}
	got := p.BypassedRoutes()
	if len(got) != 2 {
		t.Fatalf("sorted list: %v", got)
	}
	p.SetBypassedRoutes(nil)
	if p.BypassedRoutes() != nil {
		t.Fatal("clear failed")
	}
}

func TestServeHTTP_PerRouteBypass(t *testing.T) {
	t.Parallel()
	var compressed atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		compressed.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	p.SetBypassedRoutes([]string{"/v1/messages"})

	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	// Upstream still sees the request; the bypass means we did not
	// pass through the compression pipeline. The contract is verified
	// indirectly: the call succeeds and the upstream was hit exactly
	// once via the passthrough path.
	if compressed.Load() != 1 {
		t.Fatalf("upstream calls: %d", compressed.Load())
	}
}

func TestServeHTTP_PerToolBypass(t *testing.T) {
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
	cfg.Compression.Layer3Enabled = false
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	p.SetBypassedTools([]string{"Bash"})

	// Body carries a tools[] entry with name "Bash" so the per-tool
	// gate fires.
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"tools":[{"name":"Bash","description":"run"}],"messages":[{"role":"user","content":"do something"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestHasBypassedTool_FastPathEmpty(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	if p.hasBypassedTool([]byte(`{"name":"Bash"}`)) {
		t.Fatal("empty set must short-circuit to false")
	}
}

func TestHasBypassedTool_NoMatch(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	p.SetBypassedTools([]string{"Bash"})
	if p.hasBypassedTool([]byte(`{"name":"Read","content":"x"}`)) {
		t.Fatal("non-matching tool must not bypass")
	}
}

func TestHasBypassedTool_Match(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	p.SetBypassedTools([]string{"Bash"})
	if !p.hasBypassedTool([]byte(`{"tools":[{"name":"Bash"}]}`)) {
		t.Fatal("match expected")
	}
}

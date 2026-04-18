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

// TestServeHTTP_stageACacheHitSkipsCompressionPipeline verifies the T20
// two-stage cache: the second identical request resolves via the Stage A
// pointer and does not run Layer 1 or Layer 2 at all.
func TestServeHTTP_stageACacheHitSkipsCompressionPipeline(t *testing.T) {
	t.Parallel()

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = true
	cfg.Compression.Layer2Enabled = true
	cfg.Compression.Layer3Enabled = true
	cfg.Secrets.Mode = "off"

	p := New(cfg)

	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"},{"role":"user","content":"again"}]}`

	// Cold: upstream is called, Stage B entry stored, Stage A pointer registered.
	req1 := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	p.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request status=%d body=%s", rec1.Code, rec1.Body.String())
	}
	if upstreamCalls.Load() != 1 {
		t.Fatalf("want 1 upstream call after first request, got %d", upstreamCalls.Load())
	}

	// Hot: identical request must be served from cache without hitting upstream.
	req2 := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	p.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second request status=%d", rec2.Code)
	}
	if upstreamCalls.Load() != 1 {
		t.Fatalf("Stage A hit must skip upstream, got %d total upstream calls", upstreamCalls.Load())
	}
	if rec2.Body.String() != rec1.Body.String() {
		t.Fatalf("response body mismatch between cold and hot:\ncold=%s\nhot=%s", rec1.Body.String(), rec2.Body.String())
	}
}

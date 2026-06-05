package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/caching"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/summarization"
	"github.com/slimference/slimference/internal/toolprune"
	"github.com/slimference/slimference/internal/types"
)

func TestIsContextOverflow(t *testing.T) {
	t.Parallel()
	if !isContextOverflow([]byte(`{"type":"error","error":{"type":"context_length_exceeded"}}`)) {
		t.Fatal("want true for context_length_exceeded")
	}
	if !isContextOverflow([]byte(`prompt too long for this model`)) {
		t.Fatal("want true for prompt too long")
	}
	if !isContextOverflow([]byte(`maximum context length exceeded`)) {
		t.Fatal("want true for maximum context length")
	}
	if isContextOverflow([]byte(`ok`)) {
		t.Fatal("want false")
	}
}

func TestBuildAggressiveCompressedBody_Anthropic(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	p := New(cfg)
	body := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "hello"},
			{"role": "assistant", "content": "hi"},
			{"role": "user", "content": "again"}
		]
	}`)
	msgs, _, err := extractMessages(types.Anthropic, body)
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.buildAggressiveCompressedBodyContext(context.Background(), pipelineStash{
		messages: msgs,
		origBody: body,
		provider: types.Anthropic,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("empty body")
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(out, &probe); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

func TestHealthHandler(t *testing.T) {
	t.Parallel()

	p := New(config.Defaults())
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	p.healthHandler(w, req)

	resp := w.Result()
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body struct {
		Status            string          `json:"status"`
		Service           string          `json:"service"`
		Version           string          `json:"version"`
		PID               int             `json:"pid"`
		RSSBytes          int64           `json:"rss_bytes"`
		UptimeSec         int64           `json:"uptime_sec"`
		CPUUserSeconds    float64         `json:"cpu_user_seconds"`
		CPUSystemSeconds  float64         `json:"cpu_system_seconds"`
		CPUPercent        float64         `json:"cpu_percent"`
		CPUWindowPercent  float64         `json:"cpu_window_percent"`
		CPUWindowSeconds  float64         `json:"cpu_window_seconds"`
		DiskReadOps       int64           `json:"disk_read_ops"`
		DiskWriteOps      int64           `json:"disk_write_ops"`
		DiskReadOpsDelta  int64           `json:"disk_read_ops_delta"`
		DiskWriteOpsDelta int64           `json:"disk_write_ops_delta"`
		StateBytes        int64           `json:"state_bytes"`
		Layers            map[string]bool `json:"layers"`
		Providers         map[string]bool `json:"providers"`
		QueueDepth        map[string]int  `json:"queue_depth"`
		CacheEntries      int             `json:"cache_entries"`
		MiniMaxConfigured bool            `json:"minimax_configured"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
	if body.Service != "slimference" {
		t.Errorf("service = %q, want slimference", body.Service)
	}
	if body.PID <= 0 {
		t.Errorf("pid = %d, want positive process id", body.PID)
	}
	if body.RSSBytes < 0 || body.UptimeSec < 0 || body.CPUUserSeconds < 0 || body.CPUSystemSeconds < 0 ||
		body.CPUPercent < 0 || body.CPUWindowPercent < 0 || body.CPUWindowSeconds < 0 || body.DiskReadOps < 0 ||
		body.DiskWriteOps < 0 || body.DiskReadOpsDelta < 0 || body.DiskWriteOpsDelta < 0 || body.StateBytes < 0 {
		t.Errorf("resource fields rss=%d uptime=%d user=%f system=%f cpu=%f window_cpu=%f window_seconds=%f read_ops=%d write_ops=%d read_delta=%d write_delta=%d state=%d",
			body.RSSBytes, body.UptimeSec, body.CPUUserSeconds, body.CPUSystemSeconds, body.CPUPercent,
			body.CPUWindowPercent, body.CPUWindowSeconds, body.DiskReadOps, body.DiskWriteOps, body.DiskReadOpsDelta,
			body.DiskWriteOpsDelta, body.StateBytes)
	}
	// 2026-05-15: Slimference ships deterministic-only by default.
	// L1 + L3 are on; L2 model-facing summary replacement remains
	// opt-in. Supersedes T129's default-on policy.
	if !body.Layers["1"] || body.Layers["2"] || !body.Layers["3"] {
		t.Errorf("layers = %v, want L1=true L2=false L3=true (deterministic-only defaults)", body.Layers)
	}
	// Both providers should be enabled.
	if !body.Providers["anthropic"] || !body.Providers["openai"] {
		t.Errorf("providers = %v, want all true", body.Providers)
	}
	// Queue depth fields must be present.
	if _, ok := body.QueueDepth["compress"]; !ok {
		t.Error("queue_depth.compress missing")
	}
	if _, ok := body.QueueDepth["analytics"]; !ok {
		t.Error("queue_depth.analytics missing")
	}
}

// TestBuildAggressiveCompressedBody_minRatioClamp verifies the TargetRatio is clamped to MinRatio
// when MinRatio > 0.10 (the cfg.Summary.MinRatio > cfg.Summary.TargetRatio branch).
func TestBuildAggressiveCompressedBody_minRatioClamp(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	// Force MinRatio above the hardcoded 0.10 target so the clamp branch fires.
	cfg.Compression.Summary.MinRatio = 0.50
	p := New(cfg)

	body := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "hello world"},
			{"role": "assistant", "content": "response here"}
		]
	}`)
	msgs, _, err := extractMessages(types.Anthropic, body)
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.buildAggressiveCompressedBodyContext(context.Background(), pipelineStash{
		messages: msgs,
		origBody: body,
		provider: types.Anthropic,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("empty body")
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(out, &probe); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

// TestBuildAggressiveCompressedBody_contextCancelled ensures the ctx.Err()
// early-return branch fires when the caller's context is already dead.
func TestBuildAggressiveCompressedBody_contextCancelled(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.buildAggressiveCompressedBodyContext(ctx, pipelineStash{
		messages: nil,
		origBody: []byte(`{}`),
		provider: types.Anthropic,
	})
	if err == nil {
		t.Fatal("want error from cancelled context, got nil")
	}
}

// TestBuildAggressiveCompressedBody_appliesCachedSummary verifies the
// read-only Layer 2 apply branch when a cached summary covers the messages.
func TestBuildAggressiveCompressedBody_appliesCachedSummary(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Compression.Summary.AllowModelFacingReplacement = true
	p := New(cfg)
	// Seed an existing Layer 2 summary that covers indices 0..1.
	const sessionID = "session-trusted"
	p.layer2.GetCache().GetInner().Store(sessionID, &summarization.CachedSummary{
		Summary:          "stashed summary",
		CoveredRange:     [2]int{0, 1},
		OriginalTokens:   20,
		CompressedTokens: 5,
		CreatedAt:        time.Now(),
	})

	body := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"max_tokens": 64,
		"messages": [
			{"role": "user", "content": "first"},
			{"role": "assistant", "content": "second"},
			{"role": "user", "content": "tail"}
		]
	}`)
	msgs, _, err := extractMessages(types.Anthropic, body)
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.buildAggressiveCompressedBodyContext(context.Background(), pipelineStash{
		messages:  msgs,
		origBody:  body,
		provider:  types.Anthropic,
		sessionID: sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "Conversation summary covering messages") {
		t.Fatalf("expected synthetic summary injected into forwarded body; got %s", out)
	}
}

// TestBuildAggressiveCompressedBody_enqueuesAsyncJob verifies that when
// ShouldTriggerCompression is true, the overflow recover path enqueues a
// fresh async Layer 2 job instead of calling MiniMax synchronously.
func TestBuildAggressiveCompressedBody_enqueuesAsyncJob(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	cfg := config.Defaults()
	cfg.Compression.Layer2Enabled = true
	cfg.Compression.Summary.MinRatio = 0.01
	cfg.Compression.MinMessagesForCompression = 3
	cfg.Compression.SlidingWindow = 2
	cfg.Compression.MinTokensForLayer2 = 0
	p := New(cfg)

	// Build a stash with enough messages to pass ShouldTriggerCompression.
	stash := pipelineStash{
		messages: []types.Message{
			{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "one"}}},
			{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "two"}}},
			{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "three"}}},
			{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "four"}}},
			{Index: 4, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "five"}}},
			{Index: 5, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "six"}}},
			{Index: 6, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "seven"}}},
		},
		origBody: []byte(`{"model":"claude-3-5-sonnet-20241022","max_tokens":32,"messages":[]}`),
		provider: types.Anthropic,
	}

	_, err := p.buildAggressiveCompressedBodyContext(context.Background(), stash)
	if err != nil {
		t.Fatal(err)
	}
	// At least one job must have landed on the compression queue.
	select {
	case <-p.compressQueue:
		// ok
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected async compression job to be enqueued on overflow recover")
	}
}

// TestAggressiveTuningHelpers_defaults asserts the tuning fallbacks fire when
// the legacy config did not set them (T22).
func TestAggressiveTuningHelpers_defaults(t *testing.T) {
	t.Parallel()
	if got := aggressiveSlidingWindow(0); got != 2 {
		t.Errorf("aggressiveSlidingWindow(0) = %d, want 2", got)
	}
	if got := aggressiveSlidingWindow(-5); got != 2 {
		t.Errorf("aggressiveSlidingWindow(-5) = %d, want 2", got)
	}
	if got := aggressiveSlidingWindow(3); got != 3 {
		t.Errorf("aggressiveSlidingWindow(3) = %d, want 3", got)
	}
	if got := aggressiveTargetRatio(0); got != 0.10 {
		t.Errorf("aggressiveTargetRatio(0) = %v, want 0.10", got)
	}
	if got := aggressiveTargetRatio(-0.2); got != 0.10 {
		t.Errorf("aggressiveTargetRatio(-0.2) = %v, want 0.10", got)
	}
	if got := aggressiveTargetRatio(0.25); got != 0.25 {
		t.Errorf("aggressiveTargetRatio(0.25) = %v, want 0.25", got)
	}
}

// TestBuildAggressiveCompressedBody_asyncQueueFullFallsThrough verifies the
// default case of the non-blocking select fires when the queue is already
// full - the recover path must still return successfully.
func TestBuildAggressiveCompressedBody_asyncQueueFullFallsThrough(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	cfg := config.Defaults()
	cfg.Compression.Summary.MinRatio = 0.01
	cfg.Compression.MinMessagesForCompression = 3
	cfg.Compression.SlidingWindow = 2
	cfg.Compression.MinTokensForLayer2 = 0
	p := New(cfg)

	// Fill the compression queue so the default branch has to fire.
	for i := 0; i < cap(p.compressQueue); i++ {
		p.compressQueue <- types.CompressJob{Timestamp: time.Now()}
	}

	stash := pipelineStash{
		messages: []types.Message{
			{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "one"}}},
			{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "two"}}},
			{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "three"}}},
			{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "four"}}},
			{Index: 4, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "five"}}},
			{Index: 5, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "six"}}},
			{Index: 6, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "seven"}}},
		},
		origBody: []byte(`{"model":"claude-3-5-sonnet-20241022","max_tokens":32,"messages":[]}`),
		provider: types.Anthropic,
	}

	if _, err := p.buildAggressiveCompressedBodyContext(context.Background(), stash); err != nil {
		t.Fatalf("recover path must still succeed with full queue: %v", err)
	}
}

// TestDoUpstreamRequest_invalidURL covers the http.NewRequestWithContext error path
// when the upstream URL is malformed (line 251-253 in handler.go).
func TestDoUpstreamRequest_invalidURL(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = "://bad-url"
	p := New(cfg)

	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	_, err := p.doUpstreamRequest(r, types.Anthropic, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

// TestDoUpstreamRequest_nilClientFallback covers the nil httpClient branch
// which falls back to http.DefaultClient (line 269-271 in handler.go).
func TestDoUpstreamRequest_nilClientFallback(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	p := New(cfg)
	p.httpClients[types.Anthropic] = nil // force nil client -> DefaultClient fallback

	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	r = r.WithContext(context.WithValue(r.Context(), origBodyKey{}, []byte(`{}`)))
	resp, err := p.doUpstreamRequest(r, types.Anthropic, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

// TestHandleCompressibleRequest_origTokensZero covers the compressionRatio=1.0 branch
// that fires when origTokens == 0 (line 212-214 in handler.go), reached with empty messages.
func TestHandleCompressibleRequest_origTokensZero(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"m0","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"model":"claude","stop_reason":"end_turn"}`))
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Secrets.Mode = "off"
	p := New(cfg)

	// Empty messages array -> origTokens == 0 -> compressionRatio = 1.0 branch
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"messages":[]}`
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

// TestProxy_Shutdown_canceledContext covers the ctx.Done() (shutdown timeout) branch in Shutdown
// (line 530-531 in handler.go). The already-canceled context is passed to Shutdown so that the
// worker-wait select immediately picks ctx.Done() instead of <-done.
func TestProxy_Shutdown_canceledContext(t *testing.T) {
	cfg := config.Defaults()
	cfg.Proxy.ListenPort = 0
	p := New(cfg)
	errCh := make(chan error, 1)
	go func() { errCh <- p.Start() }()
	// Give the server time to start and workers to register in wg.
	time.Sleep(150 * time.Millisecond)

	// Already-canceled context: ctx.Done() fires for the worker-wait select (line 530-531).
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = p.Shutdown(ctx)
}

// TestProxy_Shutdown_serverShutdownError covers the server.Shutdown error branch (lines 515-517).
// We create a long-running SSE connection then call Shutdown with an immediate-timeout context
// so server.Shutdown has to wait for the active connection and the context cancels first.
func TestProxy_Shutdown_serverShutdownError(t *testing.T) {
	// Upstream that holds the connection open for 2 seconds.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Hold connection open.
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Proxy.ListenPort = 0
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	errCh := make(chan error, 1)
	go func() { errCh <- p.Start() }()
	time.Sleep(150 * time.Millisecond)

	// Send a streaming request that will hold the connection open.
	addr := p.ListenAddr()
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		body := strings.NewReader(`{"model":"claude","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"x"}]}`)
		req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/v1/messages", body)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}()
	// Let the connection establish.
	time.Sleep(100 * time.Millisecond)

	// Shutdown with a very short timeout - server.Shutdown will fail trying to drain the active connection.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	_ = p.Shutdown(ctx)
}

// TestProxy_Shutdown_finalFlushError covers handler.go lines 537-539 (WriteSnapshot error in Shutdown).
// We close the persister's file handle then remove the log directory so that WriteSnapshot's
// rotateIfNeeded -> openFile call fails when Shutdown tries the final flush.
func TestProxy_Shutdown_finalFlushError(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Defaults()
	cfg.Proxy.ListenPort = 0
	cfg.Analytics.LogDir = tmp
	p := New(cfg)
	if p.persister == nil {
		t.Skip("persister not initialized")
	}

	errCh := make(chan error, 1)
	go func() { errCh <- p.Start() }()
	time.Sleep(100 * time.Millisecond)

	// Close persister's file handle so rotateIfNeeded forces a reopen on next WriteSnapshot.
	// Then remove the directory so reopen fails -> WriteSnapshot returns error -> lines 537-539 hit.
	p.persister.Close()
	if err := os.RemoveAll(tmp); err != nil {
		t.Skip("could not remove temp dir:", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Shutdown should complete even though final flush fails.
	_ = p.Shutdown(ctx)
}

// TestProxy_Shutdown_withPersisterAndFinalFlush covers the final analytics flush on Shutdown
// (line 535-541 in handler.go).
func TestProxy_Shutdown_withPersisterAndFinalFlush(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Defaults()
	cfg.Proxy.ListenPort = 0
	cfg.Analytics.LogDir = tmp
	p := New(cfg)

	errCh := make(chan error, 1)
	go func() { errCh <- p.Start() }()
	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}
}

// TestDoUpstreamRequest_headerSkipBranch covers the "Host"/"Content-Length"/"Connection"/"Transfer-Encoding"
// continue branches in doUpstreamRequest (lines 258-259 in handler.go).
func TestDoUpstreamRequest_headerSkipBranch(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	p := New(cfg)

	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	// These headers hit the switch-continue branch in doUpstreamRequest.
	r.Header.Set("Host", "example.com")
	r.Header.Set("Content-Length", "2")
	r.Header.Set("Connection", "keep-alive")
	r.Header.Set("Transfer-Encoding", "chunked")
	r.Header.Set("X-Custom", "forwarded")

	resp, err := p.doUpstreamRequest(r, types.Anthropic, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

// TestAnalyticsPeriodicFlush_shutdownBranch starts the goroutine and shuts it down
// to cover the shutdownCh branch in analyticsPeriodicFlush.
func TestAnalyticsPeriodicFlush_shutdownBranch(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	p.wg.Add(1)
	go p.analyticsPeriodicFlush(analyticsFlushInterval)
	// Allow the goroutine to start and select on shutdownCh.
	time.Sleep(20 * time.Millisecond)
	close(p.shutdownCh)
	p.wg.Wait()
}

// TestAnalyticsPeriodicFlush_withPersisterShutdown starts the goroutine with a persister
// configured and shuts it down via the shutdownCh branch.
func TestAnalyticsPeriodicFlush_withPersisterShutdown(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Defaults()
	cfg.Analytics.LogDir = tmp
	p := New(cfg)
	if p.persister == nil {
		t.Skip("persister not initialized")
	}
	p.wg.Add(1)
	go p.analyticsPeriodicFlush(analyticsFlushInterval)
	time.Sleep(20 * time.Millisecond)
	close(p.shutdownCh)
	p.wg.Wait()
}

// TestCacheJanitor_shutdownBranch starts the janitor goroutine and shuts it down
// via the shutdownCh branch (line 483-484 in handler.go).
func TestCacheJanitor_shutdownBranch(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	p.wg.Add(1)
	go p.cacheJanitor(cacheJanitorInterval)
	time.Sleep(20 * time.Millisecond)
	close(p.shutdownCh)
	p.wg.Wait()
}

// TestCleanupExpiredCache tests the extracted cleanupExpiredCache method directly.
func TestCleanupExpiredCache(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	// Should not panic when called directly.
	p.cleanupExpiredCache()
}

// TestFlushAnalyticsSnapshot_withPersister tests the extracted flushAnalyticsSnapshot method
// with a persister configured.
func TestFlushAnalyticsSnapshot_withPersister(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Defaults()
	cfg.Analytics.LogDir = tmp
	p := New(cfg)
	if p.persister == nil {
		t.Skip("persister not initialized")
	}
	// Should write a snapshot without error.
	p.flushAnalyticsSnapshot()
}

// TestFlushAnalyticsSnapshot_noPersister tests the extracted flushAnalyticsSnapshot method
// when persister is nil (no-op branch).
func TestFlushAnalyticsSnapshot_noPersister(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	p.persister = nil
	// Should be a no-op without panic.
	p.flushAnalyticsSnapshot()
}

// TestFlushAnalyticsSnapshot_writeError covers the slog.Warn branch inside flushAnalyticsSnapshot
// (handler.go line ~558) when persister.WriteSnapshot returns an error.
// We close the persister's file handle and remove the log dir so WriteSnapshot fails.
func TestFlushAnalyticsSnapshot_writeError(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Defaults()
	cfg.Analytics.LogDir = tmp
	p := New(cfg)
	if p.persister == nil {
		t.Skip("persister not initialized")
	}

	// Close the persister file and delete the dir so WriteSnapshot must fail.
	p.persister.Close()
	if err := os.RemoveAll(tmp); err != nil {
		t.Skip("could not remove temp dir:", err)
	}

	// Must not panic even though WriteSnapshot returns an error.
	p.flushAnalyticsSnapshot()
}

// TestCacheJanitor_tickerBranch covers the ticker.C branch in cacheJanitor
// by passing 1ms so the tick fires during the test without touching the package var.
func TestCacheJanitor_tickerBranch(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	p.wg.Add(1)
	go p.cacheJanitor(1 * time.Millisecond)
	// Let the ticker fire at least once.
	time.Sleep(30 * time.Millisecond)
	close(p.shutdownCh)
	p.wg.Wait()
}

// TestAnalyticsPeriodicFlush_tickerBranch covers the ticker.C branch in analyticsPeriodicFlush
// by passing 1ms so the tick fires during the test without touching the package var.
func TestAnalyticsPeriodicFlush_tickerBranch(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	p.wg.Add(1)
	go p.analyticsPeriodicFlush(1 * time.Millisecond)
	// Let the ticker fire at least once.
	time.Sleep(30 * time.Millisecond)
	close(p.shutdownCh)
	p.wg.Wait()
}

// TestParseRetryAfter verifies all branches of the parseRetryAfter helper (§17.3).
func TestParseRetryAfter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{"empty", "", 0},
		{"integer seconds small", "5", 5 * time.Second},
		{"integer seconds cap", "60", 30 * time.Second},
		{"integer zero", "0", 0},
		{"invalid string", "not-a-number-or-date", 0},
		{"http date future", time.Now().Add(10 * time.Second).UTC().Format(http.TimeFormat), 0},
		{"http date past", time.Now().Add(-5 * time.Second).UTC().Format(http.TimeFormat), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseRetryAfter(tt.header)
			// For future HTTP dates we just verify it's non-negative and <= 30s.
			if tt.name == "http date future" {
				if got < 0 || got > 30*time.Second {
					t.Errorf("parseRetryAfter(%q) = %v, want 0..30s", tt.header, got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.header, got, tt.want)
			}
		})
	}
}

// TestDoUpstreamRequest_rateLimitRetry verifies that the proxy retries on 429 status,
// then succeeds on a subsequent 200 response.
func TestDoUpstreamRequest_rateLimitRetry(t *testing.T) {
	t.Parallel()
	var attempt atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempt.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	p := New(cfg)

	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	resp, err := p.doUpstreamRequest(r, types.Anthropic, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d after retry, want 200", resp.StatusCode)
	}
	if attempt.Load() < 2 {
		t.Fatalf("expected at least 2 upstream calls, got %d", attempt.Load())
	}
}

// TestDoUpstreamRequest_rateLimitExhausted verifies that after maxRateLimitRetries
// the final 429 response is returned to the caller.
func TestDoUpstreamRequest_rateLimitExhausted(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	p := New(cfg)

	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	resp, err := p.doUpstreamRequest(r, types.Anthropic, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status=%d after retries exhausted, want 429", resp.StatusCode)
	}
}

// TestNew_fileWatcherError covers the error branch in New (proxy.go line ~115)
// when the file watcher cannot be initialized.
// NOT parallel: modifies the package-level newFileWatcherFunc variable.
func TestNew_fileWatcherError(t *testing.T) {
	old := newFileWatcherFunc
	newFileWatcherFunc = func(_ func(string)) (*caching.FileWatcher, error) {
		return nil, errors.New("injected watcher error")
	}
	defer func() { newFileWatcherFunc = old }()

	p := New(config.Defaults())
	// fileWatcher must be nil when init fails; Proxy must still be usable.
	if p.fileWatcher != nil {
		t.Fatal("expected fileWatcher to be nil after init error")
	}
}

func TestHandleCompressibleRequest_ToolPruneEnabled(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"done"}],"model":"claude","stop_reason":"end_turn"}`))
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.Tuning.ToolPruneEnabled = true
	cfg.Secrets.Mode = "off"
	p := New(cfg)

	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"tools":[{"name":"Read","description":"read file"},{"name":"Bash","description":"run command"}],"messages":[{"role":"user","content":[{"type":"tool_result","text":"output"}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	snap := p.toolPrune.Snapshot()
	if snap.Sessions != 1 {
		t.Fatalf("toolPrune sessions: %d want 1", snap.Sessions)
	}
}

func TestHandleCompressibleRequest_ToolPrunePrunesIdle(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]json.RawMessage
		bodyBytes, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(bodyBytes, &reqBody)
		var tools []struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(reqBody["tools"], &tools)
		toolCount := len(tools)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := fmt.Sprintf(`{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"done-%d"}],"model":"claude","stop_reason":"end_turn"}`, toolCount)
		_, _ = w.Write([]byte(resp))
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.Tuning.ToolPruneEnabled = true
	cfg.Secrets.Mode = "off"
	p := New(cfg)

	fixedSession := "fixed-toolprune-session"
	trackerSession := "anthropic:" + fixedSession
	origNewReqID := newRequestIDFn
	newRequestIDFn = func() string { return fixedSession }
	defer func() { newRequestIDFn = origNewReqID }()

	p.toolPrune = toolprune.NewUsageTracker(2)
	for i := 0; i < 3; i++ {
		p.toolPrune.ObserveTurn(trackerSession, []string{"KeepHot", "GetWeather", "SendMail"})
	}
	for i := 0; i < 5; i++ {
		p.toolPrune.ObserveTurn(trackerSession, []string{"KeepHot"})
	}

	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"tools":[{"name":"KeepHot","description":"hot tool ` + strings.Repeat("details ", 100) + `"},{"name":"GetWeather","description":"weather ` + strings.Repeat("details ", 100) + `"},{"name":"SendMail","description":"mail ` + strings.Repeat("details ", 100) + `"}],"messages":[{"role":"user","content":[{"type":"tool_result","text":"output"}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-trace-id", fixedSession)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}

	if !strings.Contains(rec.Body.String(), "done-1") {
		t.Fatalf("expected only 1 tool (KeepHot kept, cold tools pruned), got: %s", rec.Body.String())
	}
	snap := p.toolPrune.Snapshot()
	if snap.PrunedTotal == 0 {
		t.Fatal("expected pruned tools")
	}
}

func TestHandleCompressibleRequest_ToolPruneUnknownSchemaFullPasses(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]json.RawMessage
		bodyBytes, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(bodyBytes, &reqBody)
		var tools []json.RawMessage
		_ = json.Unmarshal(reqBody["tools"], &tools)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := fmt.Sprintf(`{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"done-%d"}],"model":"claude","stop_reason":"end_turn"}`, len(tools))
		_, _ = w.Write([]byte(resp))
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.Tuning.ToolPruneEnabled = true
	cfg.Secrets.Mode = "off"
	p := New(cfg)

	fixedSession := "fixed-toolprune-unknown-schema"
	trackerSession := "anthropic:" + fixedSession
	origNewReqID := newRequestIDFn
	newRequestIDFn = func() string { return fixedSession }
	defer func() { newRequestIDFn = origNewReqID }()

	p.toolPrune = toolprune.NewUsageTracker(1)
	p.toolPrune.ObserveTurn(trackerSession, []string{"KeepHot", "ColdTool"})
	p.toolPrune.ObserveTurn(trackerSession, []string{"KeepHot"})
	p.toolPrune.ObserveTurn(trackerSession, []string{"KeepHot"})

	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"tools":[{"name":"KeepHot","description":"hot"},{"name":"ColdTool","description":"cold"},{"description":"unknown schema"}],"messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK || !strings.Contains(rec.Body.String(), "done-3") {
		t.Fatalf("unknown tool schema should full-pass all tools, status=%d body=%s", res.StatusCode, rec.Body.String())
	}
	if snap := p.toolPrune.Snapshot(); snap.PrunedTotal != 0 {
		t.Fatalf("unknown tool schema must not prune, snap=%+v", snap)
	}
}

// TestHandleCompressibleRequest_T103b_ReattachOnMention covers the
// T103b reattach path: a tool that was previously cached as pruned
// is reattached when the next request mentions its name. T103b.
func TestHandleCompressibleRequest_T103b_ReattachOnMention(t *testing.T) {
	var captured []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`))
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.Tuning.ToolPruneEnabled = true
	cfg.Secrets.Mode = "off"
	p := New(cfg)
	p.toolPrune = toolprune.NewUsageTracker(1)

	// Override request id generation so the test can pre-seed the
	// tracker under a known session key.
	const fixedID = "test-reattach-session"
	const trackerID = "anthropic:test-reattach-session"
	prev := newRequestIDFn
	newRequestIDFn = func() string { return fixedID }
	t.Cleanup(func() { newRequestIDFn = prev })

	p.toolPrune.ObserveTurn(trackerID, []string{"GetWeather"})
	p.toolPrune.ObserveTurn(trackerID, []string{"Other"})
	p.toolPrune.ObserveTurn(trackerID, []string{"Other"})
	p.toolPrune.RememberPrunedDef(
		trackerID,
		"GetWeather",
		[]byte(`{"name":"GetWeather","description":"read weather"}`),
	)

	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"tools":[{"name":"Read","description":"read"}],"messages":[{"role":"user","content":"please check the weather"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-trace-id", fixedID)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if got := p.toolPrune.Snapshot().ReattachTotal; got != 1 {
		t.Fatalf("reattach counter: %d want 1", got)
	}
	if !strings.Contains(string(captured), `"GetWeather"`) {
		t.Fatalf("reattached intent tool must survive the same prune pass: %s", captured)
	}
}

func TestHandleCompressibleRequest_ToolPruneReattachUnknownSchemaFullPassesAndKeepsCache(t *testing.T) {
	var captured []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`))
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.Tuning.ToolPruneEnabled = true
	cfg.Secrets.Mode = "off"
	p := New(cfg)
	p.toolPrune = toolprune.NewUsageTracker(1)

	const fixedID = "test-reattach-unknown-schema"
	const trackerID = "anthropic:test-reattach-unknown-schema"
	prev := newRequestIDFn
	newRequestIDFn = func() string { return fixedID }
	t.Cleanup(func() { newRequestIDFn = prev })

	p.toolPrune.RememberPrunedDef(
		trackerID,
		"GetWeather",
		[]byte(`{"name":"GetWeather","description":"read weather"}`),
	)

	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"tools":[{"description":"unknown schema"}],"messages":[{"role":"user","content":"please check the weather"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-trace-id", fixedID)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if strings.Contains(string(captured), `"GetWeather"`) {
		t.Fatalf("unknown schema request must not be mutated by reattach: %s", captured)
	}
	if got := p.toolPrune.Snapshot().ReattachTotal; got != 0 {
		t.Fatalf("reattach counter=%d want 0", got)
	}
	if names := p.toolPrune.PrunedToolNames(trackerID); len(names) != 1 || names[0] != "GetWeather" {
		t.Fatalf("failed safe reattach must keep cached definition for later: %v", names)
	}
}

func TestHandleCompressibleRequest_ToolPruneAlwaysKeepsCoreTools(t *testing.T) {
	var toolCount int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		var reqBody map[string]json.RawMessage
		_ = json.Unmarshal(bodyBytes, &reqBody)
		var tools []struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(reqBody["tools"], &tools)
		atomic.StoreInt32(&toolCount, int32(len(tools)))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`))
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.Tuning.ToolPruneEnabled = true
	cfg.Secrets.Mode = "off"
	p := New(cfg)

	const fixedSession = "toolprune-always-core"
	const trackerSession = "anthropic:toolprune-always-core"
	prev := newRequestIDFn
	newRequestIDFn = func() string { return fixedSession }
	t.Cleanup(func() { newRequestIDFn = prev })

	p.toolPrune = toolprune.NewUsageTracker(1)
	p.toolPrune.ObserveTurn(trackerSession, []string{"Bash", "Read", "ColdTool"})
	p.toolPrune.ObserveTurn(trackerSession, []string{"Read"})

	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"tools":[{"name":"Bash","description":"run command ` + strings.Repeat("details ", 100) + `"},{"name":"Read","description":"read file ` + strings.Repeat("details ", 100) + `"},{"name":"ColdTool","description":"cold function ` + strings.Repeat("details ", 100) + `"}],"messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-trace-id", fixedSession)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if got := atomic.LoadInt32(&toolCount); got != 2 {
		t.Fatalf("upstream tool count = %d want 2 core tools kept", got)
	}
	snap := p.toolPrune.Snapshot()
	if snap.AlwaysKeepTotal < 2 {
		t.Fatalf("always keep telemetry missing: %+v", snap)
	}
}

func TestHandleCompressibleRequest_ToolPruneMissingToolRetryDisablesSession(t *testing.T) {
	var calls int32
	var firstHadPrunedTool atomic.Bool
	var retryHadPrunedTool atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		bodyBytes, _ := io.ReadAll(r.Body)
		var reqBody map[string]json.RawMessage
		_ = json.Unmarshal(bodyBytes, &reqBody)
		var tools []struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(reqBody["tools"], &tools)
		hasWeather := false
		for _, tool := range tools {
			if tool.Name == "GetWeather" {
				hasWeather = true
				break
			}
		}
		if call == 1 {
			firstHadPrunedTool.Store(hasWeather)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"unknown tool GetWeather"}`))
			return
		}
		retryHadPrunedTool.Store(hasWeather)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`))
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.Tuning.ToolPruneEnabled = true
	cfg.Secrets.Mode = "off"
	p := New(cfg)

	const fixedSession = "toolprune-retry-session"
	const trackerSession = "anthropic:toolprune-retry-session"
	prev := newRequestIDFn
	newRequestIDFn = func() string { return fixedSession }
	t.Cleanup(func() { newRequestIDFn = prev })

	p.toolPrune = toolprune.NewUsageTracker(1)
	p.toolPrune.ObserveTurn(trackerSession, []string{"EchoTool", "GetWeather"})
	p.toolPrune.ObserveTurn(trackerSession, []string{"EchoTool"})

	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"tools":[{"name":"EchoTool","description":"echo ` + strings.Repeat("details ", 100) + `"},{"name":"GetWeather","description":"weather ` + strings.Repeat("details ", 100) + `"}],"messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-trace-id", fixedSession)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("upstream calls = %d want 2", got)
	}
	if firstHadPrunedTool.Load() {
		t.Fatal("first request should have pruned GetWeather")
	}
	if !retryHadPrunedTool.Load() {
		t.Fatal("retry request should restore GetWeather")
	}
	snap := p.toolPrune.Snapshot()
	if snap.MissTotal != 1 || snap.RetryTotal != 1 || snap.DisabledSessions != 1 {
		t.Fatalf("retry telemetry: %+v", snap)
	}
}

// TestHandleCompressibleRequest_MidExchangeEnabled covers the T99 mid-exchange
// summary wire-in path in the handler.
func TestHandleCompressibleRequest_MidExchangeEnabled(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"done"}],"model":"claude","stop_reason":"end_turn"}`))
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.Tuning.MidExchangeEnabled = true
	cfg.Compression.Tuning.MidExchangeThresholdTokens = 100
	cfg.Secrets.Mode = "off"
	p := New(cfg)

	longOutput := strings.Repeat("x ", 500)
	body := fmt.Sprintf(`{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"messages":[{"role":"user","content":"start"},{"role":"assistant","content":[{"type":"tool_use","name":"Bash"}]},{"role":"user","content":[{"type":"tool_result","content":"%s"}]},{"role":"assistant","content":"analysis"},{"role":"user","content":[{"type":"tool_result","content":"ok"}]}]}`, longOutput)
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

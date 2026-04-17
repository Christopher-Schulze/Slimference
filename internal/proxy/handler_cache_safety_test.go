package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
)

func drainAnalyticsQueueForTest(p *Proxy) {
	for {
		select {
		case event := <-p.analyticsQueue:
			p.analytics.Record(event)
		default:
			return
		}
	}
}

func TestServeHTTP_layer3CacheHit_partitionsByAPIKey(t *testing.T) {
	t.Parallel()

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = true
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	body := `{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":"cache me"}]}`

	post := func(apiKey string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", apiKey)
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		res := rec.Result()
		t.Cleanup(func() { _ = res.Body.Close() })
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
		}
	}

	post("key-a")
	post("key-b")

	if upstreamCalls.Load() != 2 {
		t.Fatalf("upstream calls = %d, want 2 for account-partitioned cache keys", upstreamCalls.Load())
	}
}

func TestServeHTTP_layer3CacheHit_skipsExplicitStochasticRequests(t *testing.T) {
	t.Parallel()

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = true
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	body := `{"model":"claude-3-5-sonnet-20241022","temperature":0.7,"messages":[{"role":"user","content":"sample"}]}`

	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "key-a")
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		res := rec.Result()
		t.Cleanup(func() { _ = res.Body.Close() })
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
		}
	}

	if upstreamCalls.Load() != 2 {
		t.Fatalf("upstream calls = %d, want 2 when stochastic requests are not cacheable", upstreamCalls.Load())
	}
}

func TestServeHTTP_layer3CacheHit_recordsProcessedRequestMetrics(t *testing.T) {
	t.Parallel()

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = true
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	body := `{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":"cache me"}]}`

	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "key-a")
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		res := rec.Result()
		t.Cleanup(func() { _ = res.Body.Close() })
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
		}
	}

	drainAnalyticsQueueForTest(p)
	snap := p.analytics.Snapshot()

	if upstreamCalls.Load() != 1 {
		t.Fatalf("upstream calls = %d, want 1", upstreamCalls.Load())
	}
	if snap.TotalRequests != 2 {
		t.Fatalf("TotalRequests = %d, want 2 including the cache hit", snap.TotalRequests)
	}
	if snap.CacheHits != 1 {
		t.Fatalf("CacheHits = %d, want 1", snap.CacheHits)
	}
	if len(snap.PerProvider) == 0 {
		t.Fatal("expected per-provider stats after processed cache hit")
	}
}

func TestServeHTTP_layer3CacheHit_invalidatesOnWatchedDependencyChange(t *testing.T) {
	dir := t.TempDir()
	depPath := filepath.Join(dir, "cache-target.txt")
	if err := os.WriteFile(depPath, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = true
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	if p.fileWatcher == nil {
		t.Skip("fileWatcher not initialized on this platform")
	}
	defer p.fileWatcher.Close()

	body := `{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":"read ` + depPath + ` before replying"}]}`

	post := func() {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "key-a")
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		res := rec.Result()
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
		}
	}

	post()
	post()
	if upstreamCalls.Load() != 1 {
		t.Fatalf("upstream calls after warm cache = %d, want 1", upstreamCalls.Load())
	}

	if err := os.WriteFile(depPath, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(350 * time.Millisecond)

	post()
	if upstreamCalls.Load() != 2 {
		t.Fatalf("upstream calls after dependency change = %d, want 2", upstreamCalls.Load())
	}
}

func TestServeHTTP_layer3CacheHit_skipsCachingWithoutDependencyWatcher(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	depPath := filepath.Join(dir, "cache-target.txt")
	if err := os.WriteFile(depPath, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = true
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	if p.fileWatcher != nil {
		p.fileWatcher.Close()
		p.fileWatcher = nil
	}

	body := `{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":"read ` + depPath + ` before replying"}]}`

	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "key-a")
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		res := rec.Result()
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
		}
	}

	if upstreamCalls.Load() != 2 {
		t.Fatalf("upstream calls = %d, want 2 when dependency invalidation is unavailable", upstreamCalls.Load())
	}
}

func TestServeHTTP_layer3CacheHit_skipsCachingWhenDependencyWatchFails(t *testing.T) {
	t.Parallel()

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = true
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	if p.fileWatcher == nil {
		t.Skip("fileWatcher not initialized on this platform")
	}
	defer p.fileWatcher.Close()

	body := `{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":"read /nonexistent-slimference-test-dir-xyz999/cache-target.txt before replying"}]}`

	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "key-a")
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		res := rec.Result()
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
		}
	}

	if upstreamCalls.Load() != 2 {
		t.Fatalf("upstream calls = %d, want 2 when dependency watch fails", upstreamCalls.Load())
	}
}

func TestServeHTTP_layer3CacheHit_skipsCachingWhenDependencyWatchIsNotArmed(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	depDir := filepath.Join(base, "dep-extra")
	if err := os.MkdirAll(depDir, 0o755); err != nil {
		t.Fatal(err)
	}
	depPath := filepath.Join(depDir, "cache-target.txt")
	if err := os.WriteFile(depPath, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = true
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	if p.fileWatcher == nil {
		t.Skip("fileWatcher not initialized on this platform")
	}
	defer p.fileWatcher.Close()

	for i := 0; i < 50; i++ {
		dir := filepath.Join(base, "prefill-"+strconv.Itoa(i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := p.fileWatcher.Watch(dir + string(os.PathSeparator)); err != nil {
			t.Fatal(err)
		}
	}

	body := `{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":"read ` + depPath + ` before replying"}]}`

	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "key-a")
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		res := rec.Result()
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
		}
	}

	if upstreamCalls.Load() != 2 {
		t.Fatalf("upstream calls = %d, want 2 when dependency watch is not armed", upstreamCalls.Load())
	}
}

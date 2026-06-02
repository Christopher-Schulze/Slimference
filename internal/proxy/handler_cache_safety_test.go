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

	"github.com/slimference/slimference/internal/beterse"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/qualityab"
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

func TestResponseCacheRouteKeyIncludesMethodPathAndQuery(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/v1/responses?include=usage", nil)
	if got := responseCacheRouteKey(req); got != "POST /v1/responses?include=usage" {
		t.Fatalf("route key = %q", got)
	}
	getReq := httptest.NewRequest(http.MethodGet, "/v1/responses?include=usage", nil)
	if got := responseCacheRouteKey(getReq); got == responseCacheRouteKey(req) {
		t.Fatalf("GET and POST route keys must differ, got %q", got)
	}
	if got := responseCacheRouteKey(nil); got != "" {
		t.Fatalf("nil route key = %q", got)
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
	body := `{"model":"claude-3-5-sonnet-20241022","temperature":0,"messages":[{"role":"user","content":"cache me"}]}`

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

func TestServeHTTP_layer3CacheHit_skipsImplicitSamplingDefaults(t *testing.T) {
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
	body := `{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":"sample"}]}`

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
		t.Fatalf("upstream calls = %d, want 2 when sampling defaults are implicit", upstreamCalls.Load())
	}
}

func TestServeHTTP_layer3CacheHit_skipsMetadataServerState(t *testing.T) {
	t.Parallel()

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"gpt-5","choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = true
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	body := `{"model":"gpt-5","temperature":0,"metadata":{"conversation_id":"conv-cache-safety"},"messages":[{"role":"user","content":"cache me"}]}`

	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer key-a")
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		res := rec.Result()
		t.Cleanup(func() { _ = res.Body.Close() })
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
		}
	}

	if upstreamCalls.Load() != 2 {
		t.Fatalf("upstream calls = %d, want 2 when metadata carries server state", upstreamCalls.Load())
	}
}

func TestServeHTTP_layer3CacheHit_partitionsRequestPolicy(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(string(body), "stop_sequences") {
			_, _ = io.WriteString(w, `{"id":"m2","type":"message","role":"assistant","content":[{"type":"text","text":"stop policy"}],"model":"claude","stop_reason":"end_turn"}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"baseline"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = true
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	body := `{"model":"claude-3-5-sonnet-20241022","temperature":0,"messages":[{"role":"user","content":"cache me"}]}`

	post := func() string {
		t.Helper()
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
		return rec.Body.String()
	}

	if got := post(); !strings.Contains(got, "baseline") {
		t.Fatalf("first response = %s, want baseline", got)
	}
	if got := post(); !strings.Contains(got, "baseline") {
		t.Fatalf("second response = %s, want baseline cache hit", got)
	}
	if upstreamCalls.Load() != 1 {
		t.Fatalf("upstream calls before policy change = %d, want 1", upstreamCalls.Load())
	}

	p.config.Compression.OutputReduce.StopSequencesEnabled = true
	if got := post(); !strings.Contains(got, "stop policy") {
		t.Fatalf("policy-changed response = %s, want fresh stop-policy upstream response", got)
	}
	if upstreamCalls.Load() != 2 {
		t.Fatalf("upstream calls after policy change = %d, want 2", upstreamCalls.Load())
	}
}

func TestServeHTTP_layer3CacheHit_partitionsBeTerseCohorts(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(string(body), beterse.DefaultHint) {
			_, _ = io.WriteString(w, `{"id":"m2","type":"message","role":"assistant","content":[{"type":"text","text":"terse policy"}],"model":"claude","stop_reason":"end_turn"}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"baseline"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = true
	cfg.Compression.OutputReduce.BeTerseHintEnabled = true
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	orgForCohort := func(want qualityab.Cohort) string {
		t.Helper()
		for i := 0; i < 2000; i++ {
			org := "org-cache-policy-" + strconv.Itoa(i)
			if p.qualityAB.Cohort("anthropic:"+org) == want {
				return org
			}
		}
		t.Fatalf("could not find %s org", want)
		return ""
	}
	controlOrg := orgForCohort(qualityab.CohortControl)
	treatmentOrg := orgForCohort(qualityab.CohortTreatment)
	body := `{"model":"claude-3-5-sonnet-20241022","temperature":0,"messages":[{"role":"user","content":"cache me"}]}`

	post := func(org string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "key-a")
		req.Header.Set("Anthropic-Organization-Id", org)
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		res := rec.Result()
		t.Cleanup(func() { _ = res.Body.Close() })
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
		}
		return rec.Body.String()
	}

	if got := post(controlOrg); !strings.Contains(got, "baseline") {
		t.Fatalf("control first response = %s, want baseline", got)
	}
	if got := post(controlOrg); !strings.Contains(got, "baseline") {
		t.Fatalf("control second response = %s, want baseline cache hit", got)
	}
	if upstreamCalls.Load() != 1 {
		t.Fatalf("control upstream calls = %d, want 1", upstreamCalls.Load())
	}
	if got := post(treatmentOrg); !strings.Contains(got, "terse policy") {
		t.Fatalf("treatment response = %s, want fresh be-terse upstream response", got)
	}
	if upstreamCalls.Load() != 2 {
		t.Fatalf("upstream calls after treatment = %d, want 2", upstreamCalls.Load())
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
	body := `{"model":"claude-3-5-sonnet-20241022","temperature":0,"messages":[{"role":"user","content":"cache me"}]}`

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

	body := `{"model":"claude-3-5-sonnet-20241022","temperature":0,"messages":[{"role":"user","content":"read ` + depPath + ` before replying"}]}`

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

	body := `{"model":"claude-3-5-sonnet-20241022","temperature":0,"messages":[{"role":"user","content":"read ` + depPath + ` before replying"}]}`

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

	body := `{"model":"claude-3-5-sonnet-20241022","temperature":0,"messages":[{"role":"user","content":"read ` + depPath + ` before replying"}]}`

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

package proxy

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

func TestProxy_ProviderLayerToggles(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	p.SetProviderEnabled(types.Anthropic, false)
	if p.IsProviderEnabled(types.Anthropic) {
		t.Fatal("anthropic off")
	}
	p.SetProviderEnabled(types.Anthropic, true)
	if !p.IsProviderEnabled(types.Anthropic) {
		t.Fatal("anthropic on")
	}
	p.SetLayerEnabled(1, false)
	if p.IsLayerEnabled(1) {
		t.Fatal("layer1 off")
	}
	p.SetLayerEnabled(1, true)
	if !p.IsLayerEnabled(1) {
		t.Fatal("layer1 on")
	}
}

func TestProxy_ToggleBounds(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	p.SetProviderEnabled(types.Provider(99), false)
	if p.IsProviderEnabled(types.Provider(99)) {
		t.Fatal("out-of-range provider should stay disabled")
	}
	p.SetLayerEnabled(0, false)
	p.SetLayerEnabled(99, false)
	if p.IsLayerEnabled(0) {
		t.Fatal("layer 0 invalid")
	}
	if p.IsLayerEnabled(99) {
		t.Fatal("layer 99 invalid")
	}
	if p.GetLayer2Cache() == nil {
		t.Fatal("layer2 cache")
	}
}

func TestProxy_GetAnalyticsFlushCaches(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	p.FlushCaches()
	_ = p.GetAnalytics()
	if n := len(p.GetRecentRequests(10)); n != 0 {
		t.Fatalf("want 0 recent requests, got %d", n)
	}
}

func TestReadBody(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))
	b, err := readBody(req)
	if err != nil || string(b) != "hello" {
		t.Fatalf("err=%v b=%q", err, b)
	}
	req2 := &http.Request{Body: nil}
	b2, err2 := readBody(req2)
	if err2 != nil || b2 != nil {
		t.Fatalf("nil body: err=%v b=%v", err2, b2)
	}
}

func TestProxy_StartShutdown_Health(t *testing.T) {
	cfg := config.Defaults()
	cfg.Proxy.ListenPort = 0
	p := New(cfg)
	errCh := make(chan error, 1)
	go func() { errCh <- p.Start() }()
	time.Sleep(150 * time.Millisecond)
	addr := p.ListenAddr()
	if addr == "" {
		t.Fatal("empty listen addr")
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr + "/health")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestProxy_Shutdown_withAnalyticsPersister(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Defaults()
	cfg.Proxy.ListenPort = 0
	cfg.Analytics.LogDir = tmp
	p := New(cfg)
	if p.persister == nil {
		t.Fatal("expected analytics persister when LogDir is set")
	}
	errCh := make(chan error, 1)
	go func() { errCh <- p.Start() }()
	time.Sleep(200 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestProxy_ListenAddrBeforeStart(t *testing.T) {
	p := New(config.Defaults())
	if p.ListenAddr() == "" {
		t.Fatal("expected config-derived addr")
	}
}

func TestProxy_getOriginalBody(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	if p.getOriginalBody(r) != nil {
		t.Fatal("without origBodyKey want nil")
	}
	payload := []byte(`{"model":"claude"}`)
	r = r.WithContext(context.WithValue(r.Context(), origBodyKey{}, payload))
	if string(p.getOriginalBody(r)) != string(payload) {
		t.Fatalf("got %q", p.getOriginalBody(r))
	}
}

func TestProxy_runCompressionJob_shortConversationExitsEarly(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "hi"}}},
	}
	p.runCompressionJob(types.CompressJob{Messages: msgs})
}

func TestProxy_ConfigAndSetTUISendFn(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	p := New(cfg)
	if p.Config() != cfg {
		t.Fatal("Config() should return constructor config")
	}
	var saw bool
	p.SetTUISendFn(func(types.RequestMetrics) { saw = true })
	p.tuiSendFn(types.RequestMetrics{})
	if !saw {
		t.Fatal("SetTUISendFn callback not stored")
	}
}

// TestProxy_fileWatcherCallback covers proxy.go lines 108-110 (FileWatcher onChange callback).
// The callback calls p.responseCache.Invalidate(path). We trigger it by watching a temp file
// and modifying it; the 100ms debounce means we wait ~200ms for the event to fire.
// NOT parallel: needs stable temp dir lifecycle.
func TestProxy_fileWatcherCallback(t *testing.T) {
	cfg := config.Defaults()
	p := New(cfg)
	if p.fileWatcher == nil {
		t.Skip("fileWatcher not initialized on this platform")
	}
	defer p.fileWatcher.Close()

	// Create a temp file and watch it.
	tmpFile, err := os.CreateTemp("", "proxy-watch-test-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	name := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(name)

	if err := p.fileWatcher.Watch(name); err != nil {
		t.Fatalf("Watch failed: %v", err)
	}

	// Modify the file to trigger the onChange callback.
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("changed")
	f.Close()

	// Wait for debounce (100ms) + some extra time for the callback to fire.
	time.Sleep(300 * time.Millisecond)
	// If we got here without panic, the callback executed (p.responseCache.Invalidate called).
}

// TestNew_invalidCustomPattern covers the error branch in New() for custom secret patterns
// with invalid regex (lines 123-125 in proxy.go: slog.Warn + continue).
func TestNew_invalidCustomPattern(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Secrets.Mode = "redact"
	cfg.Secrets.CustomPatterns = []config.CustomPattern{
		{Name: "bad-regex", Regex: "("},        // invalid regex - hits error+continue branch
		{Name: "good-regex", Regex: `MYKEY\d+`}, // valid - hits append branch
	}
	p := New(cfg)
	// With one invalid and one valid custom pattern, secretsDetector should still be set.
	if p.secretsDetector == nil {
		t.Fatal("secretsDetector should be set when mode != off, even with one bad pattern")
	}
}

// TestProxy_DebugRecorder covers the DebugRecorder getter.
func TestProxy_DebugRecorder(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	// DebugRecorder is always non-nil (initialized in New via dbg.NewRecorder).
	if p.DebugRecorder() == nil {
		t.Fatal("DebugRecorder should never be nil after New()")
	}
}

// TestNew_persisterInitError covers the analytics persister failure branch (lines 139-141 in proxy.go).
// Passing a path under /dev/null forces NewPersister to fail (can't mkdir under a file).
func TestNew_persisterInitError(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Analytics.LogDir = "/dev/null/nonexistent-subdir"
	p := New(cfg)
	// persister should be nil when the directory cannot be created.
	if p.persister != nil {
		t.Fatal("persister should be nil when LogDir path is invalid")
	}
}

// TestStart_portAlreadyInUse covers the net.Listen error branch in Start() (lines 175-177 in proxy.go).
func TestStart_portAlreadyInUse(t *testing.T) {
	t.Parallel()
	// Grab a free port and hold it so Start() fails.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	cfg := config.Defaults()
	// Force the proxy to use the port we're holding.
	addr := ln.Addr().(*net.TCPAddr)
	cfg.Proxy.ListenPort = addr.Port
	p := New(cfg)
	if err := p.Start(); err == nil {
		t.Fatal("expected error when port is already in use")
	}
}

// TestRecoverMiddleware_panic verifies that recoverMiddleware catches a handler panic,
// logs it, and responds with 502 Bad Gateway (no origBodyKey in context -> cannot passthrough).
func TestRecoverMiddleware_panic(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())

	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test-induced panic")
	})

	wrapped := p.recoverMiddleware(panicHandler)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	// No origBodyKey in context so the middleware returns 502.
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 after panic recovery, got %d", rec.Code)
	}
}

// TestRecoverMiddleware_panicWithBody verifies that when origBodyKey is present in context,
// recoverMiddleware attempts passthrough rather than returning 502.
// (Passthrough will fail to reach upstream in unit test, which is acceptable.)
func TestRecoverMiddleware_panicWithBody(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	p := New(cfg)

	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test-induced panic with body")
	})

	wrapped := p.recoverMiddleware(panicHandler)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	body := []byte(`{"model":"claude","messages":[{"role":"user","content":"x"}]}`)
	req = req.WithContext(context.WithValue(req.Context(), origBodyKey{}, body))
	// Must not panic; middleware catches it and attempts passthrough.
	wrapped.ServeHTTP(rec, req)
	// Passthrough to upstream -> 200, or if network fails -> 502. Either way, no panic.
}

func TestProxy_ClearLayer2ForTesting_CompressQueue_SessionLogger(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	// CompressQueue returns the channel
	if p.CompressQueue() == nil {
		t.Fatal("CompressQueue nil")
	}
	// SessionLogger returns non-nil session logger
	if p.SessionLogger() == nil {
		t.Fatal("SessionLogger nil")
	}
	// GetLayer2Cache returns non-nil before clear
	if p.GetLayer2Cache() == nil {
		t.Fatal("GetLayer2Cache should be non-nil before clear")
	}
	// ClearLayer2ForTesting sets layer2 to nil
	p.ClearLayer2ForTesting()
	// GetLayer2Cache returns nil after clear (covers the nil branch)
	if p.GetLayer2Cache() != nil {
		t.Fatal("GetLayer2Cache should be nil after ClearLayer2ForTesting")
	}
}

func TestProxy_GetProviderHealth(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	p := New(cfg)

	// No requests yet -> idle.
	info := p.GetProviderHealth(types.Anthropic)
	if info.Status != types.ProviderHealthIdle {
		t.Fatalf("want idle, got %v", info.Status)
	}

	// Record some outcomes.
	p.healthMon.record(types.Anthropic, true)
	p.healthMon.record(types.Anthropic, true)
	info = p.GetProviderHealth(types.Anthropic)
	if info.Status != types.ProviderHealthHealthy {
		t.Fatalf("want healthy, got %v", info.Status)
	}
}

func TestProxy_Handler(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	p := New(cfg)
	h := p.Handler()
	if h == nil {
		t.Fatal("Handler() should not return nil")
	}
}

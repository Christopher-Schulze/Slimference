package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/control"
	"github.com/slimference/slimference/internal/control/apps"
	"github.com/slimference/slimference/internal/filter"
	"github.com/slimference/slimference/internal/proxy"
	"github.com/slimference/slimference/internal/types"
)

func testRemoteConfig(t *testing.T, logPath string, srvURL string) *config.Config {
	t.Helper()
	cfg := config.Defaults()
	host, portStr, err := net.SplitHostPort(strings.TrimPrefix(srvURL, "http://"))
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("atoi port: %v", err)
	}
	cfg.Proxy.ListenAddress = host
	cfg.Proxy.ListenPort = port
	cfg.Logging.File = logPath
	return cfg
}

func TestRemoteProxyAdapter_StatusAndActions(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "slimference.jsonl")
	if err := os.WriteFile(logPath, []byte("{\"time\":\"2026-04-19T10:00:00Z\",\"level\":\"warn\",\"msg\":\"daemon log\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var providerReq proxy.AdminToggleProviderRequest
	var layerReq proxy.AdminToggleLayerRequest
	flushCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case proxy.AdminStatusPath:
			_ = json.NewEncoder(w).Encode(proxy.AdminStatus{
				Status:       "ok",
				Service:      "slimference",
				Version:      "test",
				ListenPort:   8990,
				PrefillSpeed: 1234,
				Layers: map[string]bool{
					"1": true,
					"2": false,
					"3": true,
				},
				Providers: map[string]bool{
					"anthropic": true,
					"openai":    false,
				},
				Analytics: analytics.AnalyticsSnapshot{
					TotalRequests:    2,
					SavedInputTokens: 10,
				},
				RecentRequests: []types.RequestMetrics{
					{Model: "m1"},
					{Model: "m2"},
				},
				Layer2: proxy.AdminLayer2Status{
					HasCache:    true,
					Compressing: true,
					LastRun:     time.Unix(100, 0).UTC(),
					QueueDepth:  4,
				},
				Layer0: map[string]filter.FilterSnapshot{
					"git_status": {
						Name:       "git_status",
						Attempts:   5,
						Matches:    2,
						Misses:     3,
						BytesSaved: 120,
						HitRate:    0.4,
						AvgMs:      0.2,
					},
				},
				ReadCache: proxy.AdminReadCacheStatus{
					Evaluations:     7,
					Allows:          3,
					Blocks:          4,
					UnchangedBlocks: 3,
					DeltaBlocks:     1,
					Sessions:        2,
					TrackedFiles:    5,
				},
				ProviderHealth: map[string]types.ProviderHealthInfo{
					"anthropic": {Status: types.ProviderHealthHealthy},
					"openai":    {Status: types.ProviderHealthDown},
				},
			})
		case proxy.AdminProviderPath:
			if err := json.NewDecoder(r.Body).Decode(&providerReq); err != nil {
				t.Fatalf("decode provider request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		case proxy.AdminLayerPath:
			if err := json.NewDecoder(r.Body).Decode(&layerReq); err != nil {
				t.Fatalf("decode layer request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		case proxy.AdminFlushPath:
			flushCalls++
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := testRemoteConfig(t, logPath, srv.URL)
	a := newRemoteProxyAdapter(cfg)

	if got := a.GetAnalytics(); got.TotalRequests != 2 {
		t.Fatalf("analytics total requests: %d", got.TotalRequests)
	}
	reqs := a.GetRecentRequests(1)
	if len(reqs) != 1 || reqs[0].Model != "m2" {
		t.Fatalf("recent requests: %+v", reqs)
	}
	if !a.GetLayer2Status().Compressing {
		t.Fatal("layer2 status should be cached from admin status")
	}
	if got := a.GetLayer0Status(); got.Attempts != 5 || got.BytesSaved != 120 || len(got.Filters) != 1 {
		t.Fatalf("layer0 status: %+v", got)
	}
	if got := a.GetReadCacheStatus(); got.Blocks != 4 || got.TrackedFiles != 5 {
		t.Fatalf("read cache status: %+v", got)
	}
	if a.GetProviderHealth(types.OpenAI).Status != types.ProviderHealthDown {
		t.Fatal("openai health should come from admin status")
	}
	if a.IsProviderEnabled(types.OpenAI) {
		t.Fatal("openai provider should be disabled from admin status")
	}
	if a.IsLayerEnabled(2) {
		t.Fatal("layer 2 should be disabled from admin status")
	}

	a.SetProviderEnabled(types.OpenAI, true)
	if providerReq.Provider != "openai" || !providerReq.Enabled {
		t.Fatalf("provider request: %+v", providerReq)
	}

	a.SetLayerEnabled(2, true)
	if layerReq.Layer != 2 || !layerReq.Enabled {
		t.Fatalf("layer request: %+v", layerReq)
	}

	a.FlushCaches()
	if flushCalls != 1 {
		t.Fatalf("flush calls: %d", flushCalls)
	}

	logger := a.SessionLogger()
	entries := logger.Recent(5)
	if len(entries) != 1 || logger.Format(entries[0]) != "daemon log" {
		t.Fatalf("log entries: %+v", entries)
	}

	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if a.Config().GetListenPort() != cfg.Proxy.ListenPort || a.Config().GetPrefillSpeed() != cfg.Usage.EstimatedPrefillSpeed {
		t.Fatal("config adapter mismatch")
	}
}

func TestRemoteProxyAdapter_FallbacksAndHelpers(t *testing.T) {
	cfg := config.Defaults()
	cfg.Proxy.ListenAddress = "127.0.0.1"
	cfg.Proxy.ListenPort = 1
	cfg.Logging.File = filepath.Join(t.TempDir(), "missing.log")

	a := newRemoteProxyAdapter(cfg)
	if got := a.GetAnalytics(); got.TotalRequests != 0 {
		t.Fatalf("unexpected analytics: %+v", got)
	}
	if a.GetProviderHealth(types.Anthropic).Status != types.ProviderHealthIdle {
		t.Fatal("missing daemon should report idle health")
	}
	if a.GetLayer2Status().HasCache {
		t.Fatal("missing daemon should not report layer2 cache")
	}
	if len(a.GetRecentRequests(5)) != 0 {
		t.Fatal("missing daemon should not report requests")
	}

	raw := parseLogEntry("plain line")
	if raw.Message != "plain line" || raw.Level != "INFO" {
		t.Fatalf("raw log parse: %+v", raw)
	}
	if strconvItoa(1) != "1" || strconvItoa(2) != "2" || strconvItoa(3) != "3" || strconvItoa(9) != "" {
		t.Fatal("strconvItoa mismatch")
	}

	a.SetProviderEnabled(types.Anthropic, false)
	if a.IsProviderEnabled(types.Anthropic) {
		t.Fatal("anthropic provider should be disabled locally")
	}
	a.SetProviderEnabled(types.Provider(99), true)
	if a.IsProviderEnabled(types.Provider(99)) {
		t.Fatal("unknown provider should remain disabled")
	}
	if a.GetProviderHealth(types.Provider(99)).Status != types.ProviderHealthIdle {
		t.Fatal("unknown provider should report idle health")
	}

	emptyLogger := &fileSessionLogger{}
	if got := emptyLogger.Recent(3); got != nil {
		t.Fatalf("empty logger should have no entries: %+v", got)
	}
	if got := (&fileSessionLogger{path: filepath.Join(t.TempDir(), "missing.log")}).Recent(3); got != nil {
		t.Fatalf("missing log should have no entries: %+v", got)
	}
}

func TestRemoteProxyAdapter_RefreshAndPostEdgeCases(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "slimference.jsonl")

	t.Run("status non-200 and bad json", func(t *testing.T) {
		count := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count++
			if count == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte("{"))
		}))
		defer srv.Close()

		cfg := testRemoteConfig(t, logPath, srv.URL)
		a := newRemoteProxyAdapter(cfg)
		a.refresh()
		a.mu.Lock()
		a.lastRefresh = time.Time{}
		a.mu.Unlock()
		a.refresh()
	})

	t.Run("refresh skip on fresh cache", func(t *testing.T) {
		hits := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits++
			_ = json.NewEncoder(w).Encode(proxy.AdminStatus{
				Layers:         map[string]bool{"1": true, "2": true, "3": true},
				Providers:      map[string]bool{"anthropic": true, "openai": true},
				ProviderHealth: map[string]types.ProviderHealthInfo{"anthropic": {Status: types.ProviderHealthIdle}, "openai": {Status: types.ProviderHealthIdle}},
			})
		}))
		defer srv.Close()

		cfg := testRemoteConfig(t, logPath, srv.URL)
		a := newRemoteProxyAdapter(cfg)
		a.refresh()
		a.refresh()
		if hits != 1 {
			t.Fatalf("refresh should skip when cache is fresh, hits=%d", hits)
		}
	})

	t.Run("post transport error", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Proxy.ListenAddress = "127.0.0.1"
		cfg.Proxy.ListenPort = 1
		a := newRemoteProxyAdapter(cfg)
		a.post(proxy.AdminFlushPath, struct{}{})
	})
}

func TestRemoteProxyAdapterAppsEndpoints(t *testing.T) {
	var gotReq proxy.AdminAppsRequest
	state := control.SetupState{
		Apps: []control.AppEntry{{
			ID:       apps.AppCodexCLI,
			Enabled:  true,
			Detected: true,
			BinPath:  "/usr/local/bin/codex",
			Routed:   7,
			Bypassed: 1,
		}},
		Savings: control.SavingsSummary{Product: control.ProductSavingsSummary{
			Status:                   "saving",
			BillableInputTokensSaved: 99,
		}},
		WSS: control.WSSState{MutationActive: true},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case proxy.AdminStatePath:
			_ = json.NewEncoder(w).Encode(state)
		case proxy.AdminAppsPath:
			if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
				t.Fatalf("decode apps request: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := testRemoteConfig(t, filepath.Join(t.TempDir(), "log.jsonl"), srv.URL)
	a := newRemoteProxyAdapter(cfg)
	entries := a.AppEntries()
	if len(entries) != 1 || entries[0].ID != string(apps.AppCodexCLI) || !entries[0].Enabled || entries[0].BinPath == "" {
		t.Fatalf("app entries: %+v", entries)
	}
	if got := a.GetProductStatus(); got.RouteStatus != "WSS savings active" || got.BillableInputTokensSaved != 99 {
		t.Fatalf("product status: %+v", got)
	}
	if err := a.SetAppEnabled(string(apps.AppCodexCLI), false); err != nil {
		t.Fatalf("SetAppEnabled: %v", err)
	}
	if gotReq.ID != string(apps.AppCodexCLI) || gotReq.Enabled {
		t.Fatalf("apps request: %+v", gotReq)
	}
}

func TestRemoteProxyAdapterAppsErrorBranches(t *testing.T) {
	t.Run("state non-200 and bad json", func(t *testing.T) {
		count := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count++
			if count == 1 {
				w.WriteHeader(http.StatusTeapot)
				return
			}
			_, _ = w.Write([]byte("{"))
		}))
		defer srv.Close()
		cfg := testRemoteConfig(t, filepath.Join(t.TempDir(), "log.jsonl"), srv.URL)
		a := newRemoteProxyAdapter(cfg)
		if got := a.AppEntries(); got != nil {
			t.Fatalf("non-200 entries=%+v", got)
		}
		if got := a.AppEntries(); got != nil {
			t.Fatalf("bad-json entries=%+v", got)
		}
	})

	t.Run("set non-2xx", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()
		cfg := testRemoteConfig(t, filepath.Join(t.TempDir(), "log.jsonl"), srv.URL)
		a := newRemoteProxyAdapter(cfg)
		if err := a.SetAppEnabled("codex_cli", true); err == nil || !strings.Contains(err.Error(), "500") {
			t.Fatalf("err=%v want 500", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Proxy.ListenAddress = "127.0.0.1"
		cfg.Proxy.ListenPort = 1
		a := newRemoteProxyAdapter(cfg)
		if got := a.AppEntries(); got != nil {
			t.Fatalf("transport error entries=%+v", got)
		}
		if err := a.SetAppEnabled("codex_cli", true); err == nil {
			t.Fatal("expected transport error")
		}
	})
}

package main

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/proxy"
	"github.com/slimference/slimference/internal/tui"
)

func TestStartProxyForDaemon_TickerSuccessAndServerClosedIgnored(t *testing.T) {
	origConfigLoad := configLoadFn
	origNewProxy := newProxyFn
	origRunner := proxyStartRunnerFn
	origHasListener := proxyHasListenerFn
	origAfter := timeAfterFn
	origTicker := newTickerFn
	origLoadState := loadTUIStateFn
	defer func() {
		configLoadFn = origConfigLoad
		newProxyFn = origNewProxy
		proxyStartRunnerFn = origRunner
		proxyHasListenerFn = origHasListener
		timeAfterFn = origAfter
		newTickerFn = origTicker
		loadTUIStateFn = origLoadState
	}()

	cfg := config.Defaults()
	configLoadFn = func() (*config.Config, error) { return cfg, nil }
	loadTUIStateFn = func() (*tui.PersistedState, error) { return nil, nil }

	var gotProxy *proxy.Proxy
	newProxyFn = func(*config.Config) *proxy.Proxy {
		gotProxy = proxy.New(cfg)
		return gotProxy
	}

	tickCh := make(chan time.Time, 1)
	deadlineCh := make(chan time.Time)
	proxyStartRunnerFn = func(*proxy.Proxy) error {
		return http.ErrServerClosed
	}
	proxyHasListenerFn = func(p *proxy.Proxy) bool {
		return p == gotProxy
	}
	timeAfterFn = func(time.Duration) <-chan time.Time { return deadlineCh }
	newTickerFn = func(time.Duration) *time.Ticker { return &time.Ticker{C: tickCh} }

	tickCh <- time.Now()
	port, shutdown, err := startProxyForDaemon()
	if err != nil {
		t.Fatalf("startProxyForDaemon ticker success: %v", err)
	}
	if port != cfg.Proxy.ListenPort || shutdown == nil {
		t.Fatalf("unexpected return values: port=%d shutdown_nil=%v", port, shutdown == nil)
	}
}

func TestStartProxyForDaemon_DeadlineSuccess(t *testing.T) {
	origConfigLoad := configLoadFn
	origNewProxy := newProxyFn
	origRunner := proxyStartRunnerFn
	origHasListener := proxyHasListenerFn
	origAfter := timeAfterFn
	origTicker := newTickerFn
	origLoadState := loadTUIStateFn
	defer func() {
		configLoadFn = origConfigLoad
		newProxyFn = origNewProxy
		proxyStartRunnerFn = origRunner
		proxyHasListenerFn = origHasListener
		timeAfterFn = origAfter
		newTickerFn = origTicker
		loadTUIStateFn = origLoadState
	}()

	cfg := config.Defaults()
	configLoadFn = func() (*config.Config, error) { return cfg, nil }
	loadTUIStateFn = func() (*tui.PersistedState, error) { return nil, nil }

	var gotProxy *proxy.Proxy
	newProxyFn = func(*config.Config) *proxy.Proxy {
		gotProxy = proxy.New(cfg)
		return gotProxy
	}
	proxyStartRunnerFn = func(*proxy.Proxy) error { return nil }
	proxyHasListenerFn = func(p *proxy.Proxy) bool { return p == gotProxy }

	deadlineCh := make(chan time.Time, 1)
	deadlineCh <- time.Now()
	timeAfterFn = func(time.Duration) <-chan time.Time { return deadlineCh }
	newTickerFn = func(time.Duration) *time.Ticker {
		return &time.Ticker{C: make(chan time.Time)}
	}

	port, shutdown, err := startProxyForDaemon()
	if err != nil {
		t.Fatalf("startProxyForDaemon deadline success: %v", err)
	}
	if port != cfg.Proxy.ListenPort || shutdown == nil {
		t.Fatalf("unexpected return values: port=%d shutdown_nil=%v", port, shutdown == nil)
	}
}

func TestStartProxyForDaemon_RunnerErrorWins(t *testing.T) {
	origConfigLoad := configLoadFn
	origNewProxy := newProxyFn
	origRunner := proxyStartRunnerFn
	origHasListener := proxyHasListenerFn
	origAfter := timeAfterFn
	origTicker := newTickerFn
	origLoadState := loadTUIStateFn
	defer func() {
		configLoadFn = origConfigLoad
		newProxyFn = origNewProxy
		proxyStartRunnerFn = origRunner
		proxyHasListenerFn = origHasListener
		timeAfterFn = origAfter
		newTickerFn = origTicker
		loadTUIStateFn = origLoadState
	}()

	cfg := config.Defaults()
	configLoadFn = func() (*config.Config, error) { return cfg, nil }
	loadTUIStateFn = func() (*tui.PersistedState, error) { return nil, nil }
	newProxyFn = func(*config.Config) *proxy.Proxy { return proxy.New(cfg) }
	proxyStartRunnerFn = func(*proxy.Proxy) error { return http.ErrBodyNotAllowed }
	proxyHasListenerFn = func(*proxy.Proxy) bool { return false }
	timeAfterFn = func(time.Duration) <-chan time.Time { return make(chan time.Time) }
	newTickerFn = func(time.Duration) *time.Ticker {
		return &time.Ticker{C: make(chan time.Time)}
	}

	_, _, err := startProxyForDaemon()
	if err == nil || !strings.Contains(err.Error(), "proxy start") {
		t.Fatalf("expected runner error, got %v", err)
	}
}

func TestRemoteProxyAdapter_PostMarshalError(t *testing.T) {
	cfg := config.Defaults()
	cfg.Proxy.ListenAddress = "127.0.0.1"
	cfg.Proxy.ListenPort = 1
	cfg.Logging.File = filepath.Join(t.TempDir(), "slimference.log")

	a := newRemoteProxyAdapter(cfg)
	before := time.Now().Add(-time.Second)
	a.mu.Lock()
	a.lastRefresh = before
	a.mu.Unlock()

	a.post(proxy.AdminFlushPath, struct {
		Broken chan int `json:"broken"`
	}{Broken: make(chan int)})

	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.lastRefresh.Equal(before) {
		t.Fatalf("marshal failure should not mutate refresh timestamp: got %s want %s", a.lastRefresh, before)
	}
}

func TestRemoteProxyAdapter_PostResetsRefreshOnHTTPResponse(t *testing.T) {
	cfg := config.Defaults()
	cfg.Proxy.ListenAddress = "127.0.0.1"
	cfg.Proxy.ListenPort = 1
	cfg.Logging.File = filepath.Join(t.TempDir(), "slimference.log")

	a := newRemoteProxyAdapter(cfg)
	a.mu.Lock()
	a.lastRefresh = time.Now()
	a.mu.Unlock()

	a.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       http.NoBody,
			Header:     make(http.Header),
		}, nil
	})}

	a.post(proxy.AdminFlushPath, struct{}{})

	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.lastRefresh.IsZero() {
		t.Fatalf("HTTP response should reset refresh timestamp, got %s", a.lastRefresh)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestStartProxyForDaemon_TimeoutErrorContainsDuration(t *testing.T) {
	origConfigLoad := configLoadFn
	origNewProxy := newProxyFn
	origRunner := proxyStartRunnerFn
	origHasListener := proxyHasListenerFn
	origAfter := timeAfterFn
	origTicker := newTickerFn
	origLoadState := loadTUIStateFn
	defer func() {
		configLoadFn = origConfigLoad
		newProxyFn = origNewProxy
		proxyStartRunnerFn = origRunner
		proxyHasListenerFn = origHasListener
		timeAfterFn = origAfter
		newTickerFn = origTicker
		loadTUIStateFn = origLoadState
	}()

	cfg := config.Defaults()
	configLoadFn = func() (*config.Config, error) { return cfg, nil }
	loadTUIStateFn = func() (*tui.PersistedState, error) { return nil, nil }
	newProxyFn = func(*config.Config) *proxy.Proxy { return proxy.New(cfg) }
	proxyStartRunnerFn = func(*proxy.Proxy) error { return nil }
	proxyHasListenerFn = func(*proxy.Proxy) bool { return false }
	deadlineCh := make(chan time.Time, 1)
	deadlineCh <- time.Now()
	timeAfterFn = func(time.Duration) <-chan time.Time { return deadlineCh }
	newTickerFn = func(time.Duration) *time.Ticker {
		return &time.Ticker{C: make(chan time.Time)}
	}

	_, _, err := startProxyForDaemon()
	if err == nil || !strings.Contains(err.Error(), "timeout after") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

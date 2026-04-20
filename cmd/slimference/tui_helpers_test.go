package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/proxy"
	"github.com/slimference/slimference/internal/tui"
	"github.com/slimference/slimference/internal/types"
)

func TestConfigAdapter_GetPrefillSpeed(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Usage.EstimatedPrefillSpeed = 777
	ca := &configAdapter{cfg: cfg}
	if ca.GetPrefillSpeed() != 777 {
		t.Fatalf("GetPrefillSpeed() = %d", ca.GetPrefillSpeed())
	}
}

func TestProxyAdapter_GetLayer2Status_layer2Cleared(t *testing.T) {
	cfg := config.Defaults()
	p := proxy.New(cfg)
	p.ClearLayer2ForTesting()
	a := newProxyAdapter(p)
	st := a.GetLayer2Status()
	if st.HasCache || st.Compressing || st.QueueDepth != 0 || !st.LastRun.IsZero() {
		t.Fatalf("got %+v", st)
	}
}

func TestSetupLogging_jsonFileAndTextStderr(t *testing.T) {
	discard := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	t.Cleanup(func() { slog.SetDefault(discard) })

	logPath := filepath.Join(t.TempDir(), "tp.log")
	cfg := config.Defaults()
	cfg.Logging.Level = "warn"
	cfg.Logging.Format = "json"
	cfg.Logging.File = logPath
	setupLogging(cfg)
	slog.Warn("setup-log-test", "k", "v")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("setup-log-test")) {
		t.Fatalf("log file: %s", data)
	}

	cfg2 := config.Defaults()
	cfg2.Logging.Level = "debug"
	cfg2.Logging.Format = "text"
	cfg2.Logging.File = ""
	setupLogging(cfg2)
}

// TestSetupLogging_errorLevel covers the "error" case in setupLogging (main.go:1204-1205).
func TestSetupLogging_errorLevel(t *testing.T) {
	cfg := config.Defaults()
	cfg.Logging.Level = "error"
	cfg.Logging.Format = "text"
	setupLogging(cfg)
}

// TestSetupLogging_debugLevel covers the "debug" case in setupLogging (main.go:1200-1201).
func TestSetupLogging_debugLevel(t *testing.T) {
	cfg := config.Defaults()
	cfg.Logging.Level = "debug"
	setupLogging(cfg)
}

func TestSetupLogging_smoke(t *testing.T) {
	cfg := config.Defaults()
	cfg.Logging.Level = "warn"
	cfg.Logging.Format = "json"
	f, err := os.CreateTemp("", "slimference-log-*.log")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	_ = f.Close()
	defer func() { _ = os.Remove(path) }()
	cfg.Logging.File = path
	setupLogging(cfg)
}

func TestProxyAdapter_smoke(t *testing.T) {
	cfg := config.Defaults()
	cfg.Proxy.ListenPort = 0
	p := proxy.New(cfg)
	a := newProxyAdapter(p)
	a.SetProviderEnabled(types.Anthropic, false)
	if a.IsProviderEnabled(types.Anthropic) {
		t.Fatal("expected anthropic disabled")
	}
	a.SetProviderEnabled(types.Anthropic, true)
	a.SetLayerEnabled(2, false)
	if a.IsLayerEnabled(2) {
		t.Fatal("expected layer 2 off")
	}
	a.FlushCaches()
	_ = a.GetAnalytics()
	_ = a.GetRecentRequests(2)
	_ = a.GetLayer2Status()
	_ = a.SessionLogger()
	_ = a.GetProviderHealth(types.Anthropic)
	_ = a.GetProviderHealth(types.OpenAI)
	if a.Config().GetListenPort() != cfg.Proxy.ListenPort {
		t.Fatal("config adapter")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestRunTUI_configError covers the config error exit in runTUI() (main.go:81-84).
func TestRunTUI_configError(t *testing.T) {
	tmp := t.TempDir()
	badCfg := filepath.Join(tmp, "bad.toml")
	if err := os.WriteFile(badCfg, []byte("this is not valid toml [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", badCfg)

	rp, cleanup := redirectStderr()
	code, exited := captureExit(runTUI)
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "config error") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestRunTUI_attachMode verifies that the default `slimference` command constructs
// a monitor proxy and forwards it into runTUIAfterStart without trying to bind a port.
func TestRunTUI_attachMode(t *testing.T) {
	t.Setenv("SLIMFERENCE_LISTEN_ADDRESS", "127.0.0.1")
	t.Setenv("SLIMFERENCE_LISTEN_PORT", "8990")
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	origNewRemote := newRemoteProxyFn
	origAfterStart := runTUIAfterStartFn
	defer func() {
		newRemoteProxyFn = origNewRemote
		runTUIAfterStartFn = origAfterStart
	}()

	stub := &testTUIProxy{}
	newRemoteProxyFn = func(cfg *config.Config) tui.ProxyInterface {
		if cfg.Proxy.ListenPort != 8990 {
			t.Fatalf("listen port = %d", cfg.Proxy.ListenPort)
		}
		return stub
	}

	called := false
	runTUIAfterStartFn = func(p tui.ProxyInterface) {
		called = true
		if p != stub {
			t.Fatal("runTUIAfterStartFn received wrong proxy interface")
		}
	}

	runTUI()

	if !called {
		t.Fatal("runTUIAfterStartFn was not called")
	}
}

// TestProgSender_send_withProg covers the case branch in progSender.send when a program
// is available in the channel: tuiSendProxyEventFn is called and prog is returned.
func TestProgSender_send_withProg(t *testing.T) {
	origSend := tuiSendProxyEventFn
	defer func() { tuiSendProxyEventFn = origSend }()
	var called bool
	tuiSendProxyEventFn = func(_ *tea.Program, _ types.RequestMetrics) { called = true }

	progCh := make(chan *tea.Program, 1)
	var prog *tea.Program // nil pointer is fine since tuiSendProxyEventFn is mocked
	progCh <- prog

	s := &progSender{ch: progCh}
	s.send(types.RequestMetrics{})

	if !called {
		t.Fatal("tuiSendProxyEventFn not called")
	}
	select {
	case got := <-progCh:
		if got != prog {
			t.Fatal("wrong prog returned to channel")
		}
	default:
		t.Fatal("prog not returned to channel after send")
	}
}

// TestProgSender_send_empty covers the default branch in progSender.send when the channel
// holds no program (send is a no-op).
func TestProgSender_send_empty(t *testing.T) {
	origSend := tuiSendProxyEventFn
	defer func() { tuiSendProxyEventFn = origSend }()
	var called bool
	tuiSendProxyEventFn = func(_ *tea.Program, _ types.RequestMetrics) { called = true }

	s := &progSender{ch: make(chan *tea.Program, 1)}
	s.send(types.RequestMetrics{})

	if called {
		t.Fatal("tuiSendProxyEventFn should not be called on empty channel")
	}
}

// TestRunTUIAfterStart_tuiError covers runTUIAfterStart when program.Run() returns an error:
// signal setup, goroutine cleanup via done channel, TUI error path (exit 1).
func TestRunTUIAfterStart_tuiError(t *testing.T) {
	origRunProg := runTeaProgramFn
	origMakeSig := makeSignalChanFn
	defer func() {
		runTeaProgramFn = origRunProg
		makeSignalChanFn = origMakeSig
	}()

	sigCh := make(chan os.Signal, 1)
	makeSignalChanFn = func() chan os.Signal { return sigCh }

	runTeaProgramFn = func(_ *tea.Program) (tea.Model, error) {
		return nil, errors.New("fake TUI error")
	}

	p := &testTUIProxy{}

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		runTUIAfterStart(p)
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "TUI error") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestRunTUIAfterStart_signalPath covers the signal goroutine body in runTUIAfterStart:
// when a signal fires, Shutdown is called and exitFn(0) is invoked.
func TestRunTUIAfterStart_signalPath(t *testing.T) {
	origRunProg := runTeaProgramFn
	origMakeSig := makeSignalChanFn
	defer func() {
		runTeaProgramFn = origRunProg
		makeSignalChanFn = origMakeSig
	}()

	sigCh := make(chan os.Signal, 1)
	makeSignalChanFn = func() chan os.Signal { return sigCh }

	exitCh := make(chan int, 1)
	blockCh := make(chan struct{})
	origExit := exitFn
	exitFn = func(code int) {
		exitCh <- code
		close(blockCh)
		runtime.Goexit()
	}
	defer func() { exitFn = origExit }()

	runTeaProgramFn = func(_ *tea.Program) (tea.Model, error) {
		<-blockCh
		return nil, nil
	}

	p := &testTUIProxy{}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runTUIAfterStart(p)
	}()

	time.Sleep(10 * time.Millisecond)

	sigCh <- syscall.SIGTERM

	select {
	case code := <-exitCh:
		if code != 0 {
			t.Fatalf("want exit 0, got %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for signal handler exit")
	}

	wg.Wait()
}

// TestMakeSignalChanFn_default covers the default makeSignalChanFn closure body:
// it creates a channel and registers it with signal.Notify.
func TestMakeSignalChanFn_default(t *testing.T) {
	ch := makeSignalChanFn()
	if ch == nil {
		t.Fatal("makeSignalChanFn: expected non-nil channel")
	}
	signal.Stop(ch)
}

// TestApplyTUIFlags verifies that CLI flags correctly override config values (spec §13.3).
func TestApplyTUIFlags(t *testing.T) {
	t.Parallel()

	base := func() *config.Config {
		cfg := config.Defaults()
		cfg.Proxy.ListenPort = 8080
		cfg.Compression.SlidingWindow = 20
		cfg.Compression.Layer1Enabled = true
		cfg.Compression.Layer2Enabled = true
		cfg.Compression.Layer3Enabled = true
		cfg.Logging.Level = "info"
		return cfg
	}

	t.Run("port", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"--port", "9999"})
		if cfg.Proxy.ListenPort != 9999 {
			t.Fatalf("port = %d, want 9999", cfg.Proxy.ListenPort)
		}
	})

	t.Run("port_alias", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"-port", "7777"})
		if cfg.Proxy.ListenPort != 7777 {
			t.Fatalf("port = %d, want 7777", cfg.Proxy.ListenPort)
		}
	})

	t.Run("sliding_window", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"--sliding-window", "5"})
		if cfg.Compression.SlidingWindow != 5 {
			t.Fatalf("sliding_window = %d, want 5", cfg.Compression.SlidingWindow)
		}
	})

	t.Run("no_layer1", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"--no-layer1"})
		if cfg.Compression.Layer1Enabled {
			t.Fatal("expected Layer1Enabled=false")
		}
		if !cfg.Compression.Layer2Enabled || !cfg.Compression.Layer3Enabled {
			t.Fatal("other layers should be unaffected")
		}
	})

	t.Run("no_layer2", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"--no-layer2"})
		if cfg.Compression.Layer2Enabled {
			t.Fatal("expected Layer2Enabled=false")
		}
		if !cfg.Compression.Layer1Enabled || !cfg.Compression.Layer3Enabled {
			t.Fatal("other layers should be unaffected")
		}
	})

	t.Run("no_layer3", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"--no-layer3"})
		if cfg.Compression.Layer3Enabled {
			t.Fatal("expected Layer3Enabled=false")
		}
	})

	t.Run("log_level", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"--log-level", "debug"})
		if cfg.Logging.Level != "debug" {
			t.Fatalf("log level = %q, want debug", cfg.Logging.Level)
		}
	})

	t.Run("combined_flags", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"--port", "1234", "--no-layer2", "--log-level", "warn", "--sliding-window", "3"})
		if cfg.Proxy.ListenPort != 1234 {
			t.Fatalf("port = %d, want 1234", cfg.Proxy.ListenPort)
		}
		if cfg.Compression.Layer2Enabled {
			t.Fatal("expected Layer2Enabled=false")
		}
		if cfg.Logging.Level != "warn" {
			t.Fatalf("log level = %q, want warn", cfg.Logging.Level)
		}
		if cfg.Compression.SlidingWindow != 3 {
			t.Fatalf("sliding_window = %d, want 3", cfg.Compression.SlidingWindow)
		}
	})

	t.Run("unknown_flags_ignored", func(t *testing.T) {
		t.Parallel()
		cfg := base()

		applyTUIFlags(cfg, []string{"--unknown-flag", "value", "--another"})
		if cfg.Proxy.ListenPort != 8080 {
			t.Fatalf("port changed unexpectedly: %d", cfg.Proxy.ListenPort)
		}
	})

	t.Run("zero_port_ignored", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"--port", "0"})
		if cfg.Proxy.ListenPort != 8080 {
			t.Fatalf("port should not change to 0: %d", cfg.Proxy.ListenPort)
		}
	})

	t.Run("non_numeric_port_ignored", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"--port", "notanumber"})
		if cfg.Proxy.ListenPort != 8080 {
			t.Fatalf("port should not change for non-numeric: %d", cfg.Proxy.ListenPort)
		}
	})

	t.Run("missing_argument_flags_ignored", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"--port", "--sliding-window", "--log-level"})
		if cfg.Proxy.ListenPort != 8080 || cfg.Compression.SlidingWindow != 20 || cfg.Logging.Level != "info" {
			t.Fatalf("unexpected config after missing args: %+v", cfg)
		}
	})

	t.Run("invalid_sliding_window_ignored", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"--sliding-window", "0"})
		if cfg.Compression.SlidingWindow != 20 {
			t.Fatalf("sliding window should stay unchanged: %d", cfg.Compression.SlidingWindow)
		}
	})
}

func TestSetupLogging_FallbackTextAndInfoLevel(t *testing.T) {
	cfg := config.Defaults()
	cfg.Logging.Level = "info"
	cfg.Logging.Format = "text"
	cfg.Logging.File = filepath.Join("/no/such", "dir", "slimference.log")
	setupLogging(cfg)
}

func TestSetupLogging_FallbackJSON(t *testing.T) {
	cfg := config.Defaults()
	cfg.Logging.Level = "warn"
	cfg.Logging.Format = "json"
	cfg.Logging.File = filepath.Join("/no/such", "dir", "slimference.log")
	setupLogging(cfg)
}

func TestApplyPersistedRuntimeState(t *testing.T) {
	origLoadState := loadTUIStateFn
	defer func() { loadTUIStateFn = origLoadState }()

	cfg := config.Defaults()
	p := proxy.New(cfg)

	loadTUIStateFn = func() (*tui.PersistedState, error) {
		return &tui.PersistedState{
			ClaudeEnabled: false,
			CodexEnabled:  true,
			Layer1Enabled: false,
			Layer2Enabled: true,
			Layer3Enabled: false,
		}, nil
	}
	applyPersistedRuntimeState(p)

	if p.IsProviderEnabled(types.Anthropic) {
		t.Fatal("claude should be disabled from persisted state")
	}
	if p.IsLayerEnabled(1) || p.IsLayerEnabled(3) {
		t.Fatal("layer 1 and 3 should be disabled from persisted state")
	}

	loadTUIStateFn = func() (*tui.PersistedState, error) { return nil, errors.New("boom") }
	applyPersistedRuntimeState(p)
}

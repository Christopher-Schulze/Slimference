package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/analytics"
	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/control/apps"
	"github.com/Christopher-Schulze/Slimference/internal/daemon"
	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/filter"
	"github.com/Christopher-Schulze/Slimference/internal/proxy"
	"github.com/Christopher-Schulze/Slimference/internal/sessions"
	"github.com/Christopher-Schulze/Slimference/internal/toolarchive"
	"github.com/Christopher-Schulze/Slimference/internal/tui"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func TestFormatTokensPlain64(t *testing.T) {
	if formatTokensPlain64(500) != "500" {
		t.Fatalf("got %q", formatTokensPlain64(500))
	}
	if formatTokensPlain64(1500) != "1.5K" {
		t.Fatalf("got %q", formatTokensPlain64(1500))
	}
	if formatTokensPlain64(2_200_000) != "2.2M" {
		t.Fatalf("got %q", formatTokensPlain64(2_200_000))
	}
	if formatTokensPlain64(-3) != "0" {
		t.Fatalf("got %q", formatTokensPlain64(-3))
	}
}

func TestCodexPostToolDecisionEntriesLayerAndArchiveReplacement(t *testing.T) {
	details := filter.PostToolPayload{
		ToolName:    "Bash",
		CommandLine: "sed -n '1,200p' docs/todo.md",
	}
	originalBytes := len(strings.Repeat("raw output line\n", 400))
	compactedBytes := len(strings.Repeat("preview line\n", 120))
	contextBytes := len("compacted bash output: archive=local-archive://tool-1")

	entries := codexPostToolDecisionEntries(details, originalBytes, compactedBytes, contextBytes)
	if len(entries) != 2 {
		t.Fatalf("entries=%d want 2: %+v", len(entries), entries)
	}
	if entries[0].Layer != 1 || entries[0].SubLayer != "codex_posttool_compaction" {
		t.Fatalf("bad compaction entry: %+v", entries[0])
	}
	if entries[1].Layer != 1 || entries[1].SubLayer != "codex_archive_replacement" {
		t.Fatalf("bad replacement entry: %+v", entries[1])
	}
	var net int
	for _, entry := range entries {
		net += entry.SavedTokens
	}
	wantNet := filter.EstimateTokensFromBytes(originalBytes) - filter.EstimateTokensFromBytes(contextBytes)
	if net != wantNet {
		t.Fatalf("net=%d want %d entries=%+v", net, wantNet, entries)
	}
}

// TestIsServerClosed verifies the server closed error detection.
func TestIsServerClosed(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"server closed string", errors.New("http: Server closed"), true},
		{"wrapped server closed", fmtErrorfWrapped(http.ErrServerClosed), true},
		{"other error", errors.New("connection refused"), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isServerClosed(tt.err); got != tt.want {
				t.Errorf("isServerClosed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func fmtErrorfWrapped(err error) error {
	return errors.New("wrap: " + err.Error())
}

func TestHandleSubcommand_Version(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"version"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "slimference v") {
		t.Fatalf("stdout: %q", out)
	}
}

func TestHandleSubcommandPhaseHCommands(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "install", args: []string{"install", "--help"}},
		{name: "uninstall", args: []string{"uninstall", "--help"}},
		{name: "enable", args: []string{"enable", "--help"}},
		{name: "disable", args: []string{"disable", "--help"}},
		{name: "status", args: []string{"status", "--help"}},
		{name: "lab", args: []string{"lab", "--help"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, exited := captureExit(func() { handleSubcommand(tc.args) })
			if !exited || code != 0 {
				t.Fatalf("exit=(%d,%v), want (0,true)", code, exited)
			}
		})
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "start", args: []string{"start", "--help"}, want: "slimference start"},
		{name: "stop", args: []string{"stop", "--help"}, want: "slimference stop"},
		{name: "restart", args: []string{"restart", "--help"}, want: "slimference restart"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _ := captureRootCommandOutput(t, func() {
				code, exited := captureExit(func() { handleSubcommand(tc.args) })
				if exited {
					t.Fatalf("unexpected exit %d", code)
				}
			})
			if !strings.Contains(out, tc.want) {
				t.Fatalf("output missing %q: %q", tc.want, out)
			}
		})
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "cert-trust", args: []string{"cert-trust", "--help"}, want: "slimference cert-trust"},
		{name: "root-arm", args: []string{"root-arm", "--help"}, want: "slimference root-arm"},
		{name: "root-disarm", args: []string{"root-disarm", "--help"}, want: "slimference root-disarm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _ := captureRootCommandOutput(t, func() {
				code, exited := captureExit(func() { handleSubcommand(tc.args) })
				if exited {
					t.Fatalf("unexpected exit %d", code)
				}
			})
			if !strings.Contains(out, tc.want) {
				t.Fatalf("output missing %q: %q", tc.want, out)
			}
		})
	}
}

func TestHandleSubcommandLifecycleRejectsUnexpectedArgs(t *testing.T) {
	for _, command := range []string{"start", "stop", "restart"} {
		t.Run(command, func(t *testing.T) {
			code, exited := captureExit(func() { handleSubcommand([]string{command, "--wat"}) })
			if !exited || code != 2 {
				t.Fatalf("exit=(%d,%v), want (2,true)", code, exited)
			}
		})
	}
}

func TestProxyAdapterAppEntriesAndSetAppEnabled(t *testing.T) {
	p := proxy.New(config.Defaults())
	adapter := &proxyAdapter{p: p}
	if got := adapter.AppEntries(); got != nil {
		t.Fatalf("nil manager entries=%v", got)
	}
	if err := adapter.SetAppEnabled("codex_cli", true); err == nil {
		t.Fatal("expected error when apps manager is not wired")
	}

	manager, err := apps.NewManager(filepath.Join(t.TempDir(), "apps.toml"))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	p.SetAppsManager(manager)
	entries := adapter.AppEntries()
	if len(entries) != len(apps.KnownApps) {
		t.Fatalf("entries=%d want %d", len(entries), len(apps.KnownApps))
	}
	if err := adapter.SetAppEnabled("codex_cli", false); err != nil {
		t.Fatalf("SetAppEnabled: %v", err)
	}
	if manager.Policy().IsEnabled(apps.AppID("codex_cli")) {
		t.Fatal("codex_cli should be disabled")
	}
}

func TestServiceControlAdapterRunTransparentProxyCommandErrors(t *testing.T) {
	orig := proxyRunFn
	t.Cleanup(func() { proxyRunFn = orig })
	adapter := &serviceControlAdapter{}
	for _, tc := range []struct {
		name string
		run  func([]string, proxyEnv) int
		want string
	}{
		{
			name: "stderr",
			run: func(_ []string, env proxyEnv) int {
				fmt.Fprint(env.Stderr, "proxy stderr")
				return 2
			},
			want: "proxy stderr",
		},
		{
			name: "stdout",
			run: func(_ []string, env proxyEnv) int {
				fmt.Fprint(env.Stdout, "proxy stdout")
				return 3
			},
			want: "proxy stdout",
		},
		{
			name: "fallback",
			run: func(_ []string, _ proxyEnv) int {
				return 4
			},
			want: "proxy install failed with exit 4",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proxyRunFn = tc.run
			err := adapter.runTransparentProxyCommand("install")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want %q", err, tc.want)
			}
		})
	}
	proxyRunFn = func(_ []string, _ proxyEnv) int { return 0 }
	if err := adapter.runTransparentProxyCommand("install"); err != nil {
		t.Fatalf("success path err=%v", err)
	}
}

func testOpenFilterDBAndRecord(t *testing.T, commands ...string) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "filter.db")
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Now()
	for _, cmd := range commands {
		if err := filter.RecordFilterRun(db, cmd, "/proj", 100, 40, 60, ts); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return dbPath
}

func writeTestAnalyticsConfigToml(t *testing.T, logDir string) string {
	t.Helper()
	absLog, err := filepath.Abs(logDir)
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "tp-analytics.toml")
	content := fmt.Sprintf("[analytics]\nlog_dir = %q\n", absLog)
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

// TestHandleSubcommand_unknownExits1 verifies unknown top-level command exits 1 (subprocess).
func TestHandleSubcommand_unknownExits1(t *testing.T) {
	if os.Getenv("TP_SUB_UNKNOWN") == "1" {
		handleSubcommand([]string{"not-a-command"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_unknownExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_UNKNOWN=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestFormatTokensPlain verifies the token formatting helper.
func TestFormatTokensPlain(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{42, "42"},
		{500, "500"},
		{1000, "1.0K"},
		{1500, "1.5K"},
		{1_000_000, "1.0M"},
		{2_500_000, "2.5M"},
	}
	for _, tt := range tests {
		got := formatTokensPlain(tt.input)
		if got != tt.want {
			t.Errorf("formatTokensPlain(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// exitPanic is the sentinel type panicked by the injected exitFn.
type exitPanic struct{ code int }

// controlledExitMarker tags exitPanic as a controlled-unwind value so
// failopen.guardHook re-raises it instead of treating it as a runtime
// panic (t164 fail-open contract).
func (exitPanic) controlledExitMarker() {}

// captureExit runs fn and returns the exit code + whether exitFn was called.
// exitFn is temporarily overridden to panic with exitPanic{code}; the deferred
// recover catches it and returns the code. Any other panic is re-panicked.
func captureExit(fn func()) (code int, exited bool) {
	orig := exitFn
	exitFn = func(c int) { panic(exitPanic{c}) }
	defer func() { exitFn = orig }()

	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(exitPanic); ok {
				code = e.code
				exited = true
			} else {
				panic(r)
			}
		}
	}()

	fn()
	return -1, false
}

// redirectStderr replaces os.Stderr with a pipe and returns a reader and a cleanup
// func. The cleanup func closes the write end and restores os.Stderr. Call it after
// captureExit returns (before reading the pipe) to close the write end.
//
// Pattern for tests that use both captureExit and stderr capture:
//
//	rp, cleanup := redirectStderr()
//	code, exited := captureExit(fn)
//	cleanup()
//	var buf bytes.Buffer; io.Copy(&buf, rp)
func redirectStderr() (r *os.File, cleanup func()) {
	orig := os.Stderr
	rp, wp, _ := os.Pipe()
	os.Stderr = wp
	return rp, func() {
		_ = wp.Close()
		os.Stderr = orig
	}
}

// TestMain_noArgs covers the `runTUIFn()` branch in main() on a TTY.
func TestMain_noArgs(t *testing.T) {
	orig := runTUIFn
	defer func() { runTUIFn = orig }()
	called := false
	runTUIFn = func() { called = true }

	origTerm := termIsTerminalFn
	defer func() { termIsTerminalFn = origTerm }()
	termIsTerminalFn = func(int) bool { return true }

	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"slimference"}

	main()
	if !called {
		t.Fatal("runTUIFn was not called")
	}
}

// TestMain_noTTYEmitsHelp covers the T43 non-TTY guard: running with no args on
// a non-TTY must print help, exit 2, and never call runTUIFn.
func TestMain_noTTYEmitsHelp(t *testing.T) {
	origTerm := termIsTerminalFn
	defer func() { termIsTerminalFn = origTerm }()
	termIsTerminalFn = func(int) bool { return false }

	origRunTUI := runTUIFn
	defer func() { runTUIFn = origRunTUI }()
	runTUIFn = func() { t.Fatal("runTUIFn must not fire on non-TTY") }

	origExit := exitFn
	defer func() { exitFn = origExit }()
	var exitCode int
	exitFn = func(code int) { exitCode = code }

	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"slimference"}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()
	main()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if !strings.Contains(buf.String(), "SUBCOMMANDS") {
		t.Fatalf("help banner missing: %q", buf.String())
	}
}

// TestMain_helpFlag covers --help early dispatch.
func TestMain_helpFlag(t *testing.T) {
	origRunTUI := runTUIFn
	defer func() { runTUIFn = origRunTUI }()
	runTUIFn = func() { t.Fatal("runTUIFn must not fire on --help") }

	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"slimference", "--help"}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()
	main()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if !strings.Contains(buf.String(), "SUBCOMMANDS") {
		t.Fatalf("--help banner missing: %q", buf.String())
	}
}

// TestMain_versionFlag covers --version early dispatch.
func TestMain_versionFlag(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"slimference", "-V"}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()
	main()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if !strings.HasPrefix(buf.String(), "slimference v") {
		t.Fatalf("version banner wrong: %q", buf.String())
	}
}

// TestMain_noTuiFlag covers --no-tui early dispatch to runHeadlessFn.
func TestMain_noTuiFlag(t *testing.T) {
	origFn := runHeadlessFn
	defer func() { runHeadlessFn = origFn }()
	called := false
	runHeadlessFn = func(args []string) { called = true }

	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"slimference", "--no-tui"}

	main()
	if !called {
		t.Fatal("runHeadlessFn was not called")
	}
}

func TestMain_GlobalConfigFlagIsExtractedBeforeHeadless(t *testing.T) {
	origFn := runHeadlessFn
	origConfig := explicitConfigPath
	defer func() {
		runHeadlessFn = origFn
		explicitConfigPath = origConfig
	}()
	var gotArgs []string
	runHeadlessFn = func(args []string) { gotArgs = append([]string{}, args...) }

	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	os.Args = []string{"slimference", "--config", cfgPath, "--no-tui"}

	main()
	if explicitConfigPath != cfgPath {
		t.Fatalf("explicitConfigPath = %q, want %q", explicitConfigPath, cfgPath)
	}
	if len(gotArgs) != 1 || gotArgs[0] != "--no-tui" {
		t.Fatalf("headless args = %v", gotArgs)
	}
}

func TestHandleSubcommand_IntegrateAndBypassDispatch(t *testing.T) {
	isolateIntegrateEnv(t)
	captureIntegrate(t, func() {
		handleSubcommand([]string{"integrate", "status"})
	})

	state := false
	srv := stubBypassAdminServer(t, &state)
	defer srv.Close()
	origURL := bypassProxyURL
	bypassProxyURL = srv.URL
	defer func() { bypassProxyURL = origURL }()
	out := captureStdoutBypass(t, func() {
		handleSubcommand([]string{"bypass", "status"})
	})
	if !strings.Contains(out, "bypass: off") {
		t.Fatalf("bypass dispatch output: %q", out)
	}
}

// TestMain_withArgs covers the handleSubcommand branch in main() (main.go:71-74).
func TestMain_withArgs(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"slimference", "version"}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()
	main()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "slimference v") {
		t.Fatalf("stdout: %q", buf.String())
	}
}

type testTUIProxy struct {
	shutdownCalls int
}

func (p *testTUIProxy) SetProviderEnabled(types.Provider, bool) {}

func (p *testTUIProxy) SetLayerEnabled(int, bool) {}

func (p *testTUIProxy) IsProviderEnabled(types.Provider) bool { return true }

func (p *testTUIProxy) IsLayerEnabled(int) bool { return true }

func (p *testTUIProxy) FlushCaches() {}

func (p *testTUIProxy) GetAnalytics() analytics.AnalyticsSnapshot {
	return analytics.AnalyticsSnapshot{}
}

func (p *testTUIProxy) GetRecentRequests(int) []types.RequestMetrics { return nil }

func (p *testTUIProxy) GetRecentFlights(int) []dbg.FlightRequestSummary { return nil }

func (p *testTUIProxy) GetLayer0Status() tui.Layer0Status { return tui.Layer0Status{} }

func (p *testTUIProxy) GetReadCacheStatus() tui.ReadCacheStatus { return tui.ReadCacheStatus{} }

func (p *testTUIProxy) GetQualityStatus() tui.QualityStatus { return tui.QualityStatus{} }

func (p *testTUIProxy) GetProductStatus() tui.ProductStatus { return tui.ProductStatus{} }

func (p *testTUIProxy) GetProviderHealth(types.Provider) types.ProviderHealthInfo {
	return types.ProviderHealthInfo{Status: types.ProviderHealthIdle}
}

func (p *testTUIProxy) SessionLogger() tui.SessionLoggerInterface { return nil }

func (p *testTUIProxy) Shutdown(context.Context) error {
	p.shutdownCalls++
	return nil
}

func (p *testTUIProxy) Config() tui.ProxyConfigInterface {
	return &configAdapter{cfg: config.Defaults()}
}

// Bypass / SetBypass satisfy the T67 additions to tui.ProxyInterface.
func (p *testTUIProxy) Bypass() bool   { return false }
func (p *testTUIProxy) SetBypass(bool) {}

// AppEntries / SetAppEnabled satisfy the Phase H additions.
func (p *testTUIProxy) AppEntries() []tui.AppEntry       { return nil }
func (p *testTUIProxy) SetAppEnabled(string, bool) error { return nil }

func TestHandlePostToolCmd(t *testing.T) {
	origTerm := termIsTerminalFn
	origRead := readStdinAll
	origGetwd := osGetwd
	origConfigLoad := configLoadFn
	defer func() {
		termIsTerminalFn = origTerm
		readStdinAll = origRead
		osGetwd = origGetwd
		configLoadFn = origConfigLoad
	}()

	t.Run("usage_on_unexpected_arg", func(t *testing.T) {
		rp, cleanup := redirectStderr()
		code, exited := captureExit(func() { handlePostToolCmd([]string{"bad"}) })
		cleanup()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rp)
		if !exited || code != 1 || !strings.Contains(buf.String(), "usage: slimference posttool") {
			t.Fatalf("exited=%v code=%d stderr=%q", exited, code, buf.String())
		}
	})

	t.Run("usage_when_terminal", func(t *testing.T) {
		termIsTerminalFn = func(int) bool { return true }
		rp, cleanup := redirectStderr()
		code, exited := captureExit(func() { handlePostToolCmd(nil) })
		cleanup()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rp)
		if !exited || code != 1 || !strings.Contains(buf.String(), "usage: slimference posttool") {
			t.Fatalf("exited=%v code=%d stderr=%q", exited, code, buf.String())
		}
		termIsTerminalFn = origTerm
	})

	t.Run("stdin_read_error", func(t *testing.T) {
		termIsTerminalFn = func(int) bool { return false }
		readStdinAll = func() ([]byte, error) { return nil, errors.New("read fail") }
		rp, cleanup := redirectStderr()
		code, exited := captureExit(func() { handlePostToolCmd(nil) })
		cleanup()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rp)
		if !exited || code != 1 || !strings.Contains(buf.String(), "read stdin: read fail") {
			t.Fatalf("exited=%v code=%d stderr=%q", exited, code, buf.String())
		}
		readStdinAll = origRead
	})

	t.Run("json_parse_error_fails_open", func(t *testing.T) {
		termIsTerminalFn = func(int) bool { return false }
		readStdinAll = func() ([]byte, error) { return []byte("{"), nil }
		rp, cleanup := redirectStderr()
		code, exited := captureExit(func() { handlePostToolCmd(nil) })
		cleanup()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rp)
		if exited || buf.Len() != 0 {
			t.Fatalf("exited=%v code=%d stderr=%q", exited, code, buf.String())
		}
		readStdinAll = origRead
	})

	t.Run("missing_tool_response_skips", func(t *testing.T) {
		termIsTerminalFn = func(int) bool { return false }
		readStdinAll = func() ([]byte, error) { return []byte(`{"command":"git status"}`), nil }
		rp, cleanup := redirectStderr()
		code, exited := captureExit(func() { handlePostToolCmd([]string{"--"}) })
		cleanup()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rp)
		if exited || buf.Len() != 0 {
			t.Fatalf("exited=%v code=%d stderr=%q", exited, code, buf.String())
		}
		readStdinAll = origRead
	})

	t.Run("no_change_emits_nothing", func(t *testing.T) {
		termIsTerminalFn = func(int) bool { return false }
		readStdinAll = func() ([]byte, error) {
			return []byte(`{"command":"echo hi","tool_response":"short output"}`), nil
		}
		cfg := config.Defaults()
		cfg.Filter.PassthroughMaxChars = 500
		configLoadFn = func() (*config.Config, error) { return cfg, nil }
		osGetwd = func() (string, error) { return "/tmp", nil }
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handlePostToolCmd(nil)
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		if buf.Len() != 0 {
			t.Fatalf("expected no stdout when output unchanged, got %q", buf.String())
		}
		readStdinAll = origRead
		configLoadFn = origConfigLoad
		osGetwd = origGetwd
	})

	t.Run("no_change_compact_emits_nothing", func(t *testing.T) {
		t.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "compact")
		termIsTerminalFn = func(int) bool { return false }
		readStdinAll = func() ([]byte, error) {
			return []byte(`{"command":"echo hi","tool_response":"short output"}`), nil
		}
		cfg := config.Defaults()
		cfg.Filter.PassthroughMaxChars = 500
		cfg.Hooks.CodexPostToolMinTokens = 0
		configLoadFn = func() (*config.Config, error) { return cfg, nil }
		osGetwd = func() (string, error) { return "/tmp", nil }
		origHome := osUserHomeDir
		osUserHomeDir = func() (string, error) { return "", errors.New("home") }
		defer func() { osUserHomeDir = origHome }()
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handlePostToolCmd(nil)
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		if buf.Len() != 0 {
			t.Fatalf("expected no stdout when compact mode has no material change, got %q", buf.String())
		}
		readStdinAll = origRead
		configLoadFn = origConfigLoad
		osGetwd = origGetwd
	})

	t.Run("medium_compacted_output_auto_mode_emits_by_default", func(t *testing.T) {
		t.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "auto")
		termIsTerminalFn = func(int) bool { return false }
		var statusOutput strings.Builder
		for i := range 150 {
			fmt.Fprintf(&statusOutput, " M file_%03d.go\n", i)
		}
		payload, err := json.Marshal(map[string]string{
			"command":       "git status --short",
			"tool_response": statusOutput.String(),
		})
		if err != nil {
			t.Fatal(err)
		}
		readStdinAll = func() ([]byte, error) { return payload, nil }
		cfg := config.Defaults()
		cfg.Filter.PassthroughMaxChars = 2000
		cfg.Hooks.CodexPostToolMinTokens = 0
		configLoadFn = func() (*config.Config, error) { return cfg, nil }
		osGetwd = func() (string, error) { return "", errors.New("no wd") }
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handlePostToolCmd(nil)
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		out := buf.String()
		if !strings.Contains(out, `"continue":false`) || !strings.Contains(out, `"hookEventName":"PostToolUse"`) {
			t.Fatalf("expected default auto replacement for medium positive output, got %q", out)
		}
		readStdinAll = origRead
		configLoadFn = origConfigLoad
		osGetwd = origGetwd
	})

	t.Run("medium_generic_truncated_output_silent_by_default", func(t *testing.T) {
		t.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "auto")
		termIsTerminalFn = func(int) bool { return false }
		payload, err := json.Marshal(map[string]string{
			"command":       "git status",
			"tool_response": strings.Repeat("line\n", 300),
		})
		if err != nil {
			t.Fatal(err)
		}
		readStdinAll = func() ([]byte, error) { return payload, nil }
		cfg := config.Defaults()
		cfg.Filter.PassthroughMaxChars = 40
		cfg.Hooks.CodexPostToolMinTokens = 0
		configLoadFn = func() (*config.Config, error) { return cfg, nil }
		osGetwd = func() (string, error) { return "", errors.New("no wd") }
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handlePostToolCmd(nil)
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		if buf.Len() != 0 {
			t.Fatalf("expected medium generic truncation to stay silent, got %q", buf.String())
		}
		readStdinAll = origRead
		configLoadFn = origConfigLoad
		osGetwd = origGetwd
	})

	t.Run("large_compacted_output_auto_mode_emits_hook_json", func(t *testing.T) {
		t.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "auto")
		termIsTerminalFn = func(int) bool { return false }
		payload, err := json.Marshal(map[string]string{
			"command":       "git status",
			"tool_response": strings.Repeat("line\n", 2000),
		})
		if err != nil {
			t.Fatal(err)
		}
		readStdinAll = func() ([]byte, error) { return payload, nil }
		cfg := config.Defaults()
		cfg.Filter.PassthroughMaxChars = 40
		cfg.Hooks.CodexPostToolMinTokens = 0
		configLoadFn = func() (*config.Config, error) { return cfg, nil }
		osGetwd = func() (string, error) { return "", errors.New("no wd") }
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handlePostToolCmd(nil)
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		out := buf.String()
		if !strings.Contains(out, `"continue":false`) || !strings.Contains(out, `"hookEventName":"PostToolUse"`) {
			t.Fatalf("expected auto replacement for high-savings output, got %q", out)
		}
		readStdinAll = origRead
		configLoadFn = origConfigLoad
		osGetwd = origGetwd
	})

	t.Run("large_compacted_output_silent_mode_emits_nothing", func(t *testing.T) {
		t.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "silent")
		termIsTerminalFn = func(int) bool { return false }
		payload, err := json.Marshal(map[string]string{
			"command":       "git status",
			"tool_response": strings.Repeat("line\n", 2000),
		})
		if err != nil {
			t.Fatal(err)
		}
		readStdinAll = func() ([]byte, error) { return payload, nil }
		cfg := config.Defaults()
		cfg.Filter.PassthroughMaxChars = 40
		cfg.Hooks.CodexPostToolMinTokens = 0
		configLoadFn = func() (*config.Config, error) { return cfg, nil }
		osGetwd = func() (string, error) { return "", errors.New("no wd") }
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handlePostToolCmd(nil)
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		if buf.Len() != 0 {
			t.Fatalf("expected explicit silent mode to suppress replacement, got %q", buf.String())
		}
		readStdinAll = origRead
		configLoadFn = origConfigLoad
		osGetwd = origGetwd
	})

	t.Run("compacted_output_emits_hook_json", func(t *testing.T) {
		t.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "compact")
		termIsTerminalFn = func(int) bool { return false }
		payload, err := json.Marshal(map[string]string{
			"command":       "git status",
			"tool_response": strings.Repeat("line\n", 300),
		})
		if err != nil {
			t.Fatal(err)
		}
		readStdinAll = func() ([]byte, error) {
			return payload, nil
		}
		cfg := config.Defaults()
		cfg.Filter.PassthroughMaxChars = 40
		cfg.Hooks.CodexPostToolMinTokens = 0
		configLoadFn = func() (*config.Config, error) { return cfg, nil }
		osGetwd = func() (string, error) { return "", errors.New("no wd") }
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handlePostToolCmd(nil)
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		out := buf.String()
		if !strings.Contains(out, `"continue":false`) || !strings.Contains(out, `"hookEventName":"PostToolUse"`) || !strings.Contains(out, `Bash output for \"git status\" was compacted locally`) || !strings.Contains(out, `[output truncated to 40 characters]`) {
			t.Fatalf("unexpected stdout: %q", out)
		}
		readStdinAll = origRead
		configLoadFn = origConfigLoad
		osGetwd = origGetwd
	})

	t.Run("compacted_output_without_command_uses_generic_context", func(t *testing.T) {
		t.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "compact")
		termIsTerminalFn = func(int) bool { return false }
		payload, err := json.Marshal(map[string]string{
			"tool_response": strings.Repeat("line\n", 300),
		})
		if err != nil {
			t.Fatal(err)
		}
		readStdinAll = func() ([]byte, error) { return payload, nil }
		cfg := config.Defaults()
		cfg.Filter.PassthroughMaxChars = 40
		cfg.Hooks.CodexPostToolMinTokens = 0
		configLoadFn = func() (*config.Config, error) { return cfg, nil }
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handlePostToolCmd([]string{"--"})
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		out := buf.String()
		if !strings.Contains(out, `Bash output was compacted locally.`) || strings.Contains(out, `for \"`) {
			t.Fatalf("unexpected stdout: %q", out)
		}
		readStdinAll = origRead
		configLoadFn = origConfigLoad
	})

	t.Run("encode_error_exits", func(t *testing.T) {
		t.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "compact")
		termIsTerminalFn = func(int) bool { return false }
		payload, err := json.Marshal(map[string]string{
			"command":       "git status",
			"tool_response": strings.Repeat("line\n", 300),
		})
		if err != nil {
			t.Fatal(err)
		}
		readStdinAll = func() ([]byte, error) { return payload, nil }
		cfg := config.Defaults()
		cfg.Filter.PassthroughMaxChars = 40
		cfg.Hooks.CodexPostToolMinTokens = 0
		configLoadFn = func() (*config.Config, error) { return cfg, nil }
		osGetwd = func() (string, error) { return "/tmp", nil }

		oldStdout := os.Stdout
		oldStderr := os.Stderr
		rp, wp, _ := os.Pipe()
		_ = wp.Close()
		errR, errW, _ := os.Pipe()
		os.Stdout = wp
		os.Stderr = errW
		code, exited := captureExit(func() { handlePostToolCmd(nil) })
		_ = errW.Close()
		os.Stdout = oldStdout
		os.Stderr = oldStderr
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, errR)
		_ = rp.Close()
		if !exited || code != 1 || !strings.Contains(buf.String(), "encode posttool output") {
			t.Fatalf("exited=%v code=%d stderr=%q", exited, code, buf.String())
		}
		readStdinAll = origRead
		configLoadFn = origConfigLoad
		osGetwd = origGetwd
	})
}

func TestPostToolWatchdogHelpers(t *testing.T) {
	stop := startCodexPostToolWatchdog(nil, 0)
	stop()

	origExit := exitFn
	defer func() { exitFn = origExit }()
	exited := make(chan int, 1)
	exitFn = func(code int) { exited <- code }

	stop = startCodexPostToolWatchdog([]byte(`{"session_id":"wd"}`), time.Millisecond)
	defer stop()
	select {
	case code := <-exited:
		if code != 0 {
			t.Fatalf("exit code=%d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("watchdog did not fire")
	}
}

func TestHandleClaudePostToolCmd(t *testing.T) {
	origTerm := termIsTerminalFn
	origRead := readStdinAll
	origGetwd := osGetwd
	origConfigLoad := configLoadFn
	defer func() {
		termIsTerminalFn = origTerm
		readStdinAll = origRead
		osGetwd = origGetwd
		configLoadFn = origConfigLoad
	}()

	t.Run("default_off_reads_nothing_and_emits_nothing", func(t *testing.T) {
		t.Setenv("SLIMFERENCE_CLAUDE_HOOK_MODE", "")
		readCalled := false
		readStdinAll = func() ([]byte, error) {
			readCalled = true
			return []byte(`{}`), nil
		}
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handleClaudePostToolCmd(nil)
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		if readCalled || buf.Len() != 0 {
			t.Fatalf("default-off should not read or emit, read=%v stdout=%q", readCalled, buf.String())
		}
		readStdinAll = origRead
	})

	t.Run("max_updates_tool_output", func(t *testing.T) {
		t.Setenv("SLIMFERENCE_CLAUDE_HOOK_MODE", "max")
		termIsTerminalFn = func(int) bool { return false }
		payload, err := json.Marshal(map[string]string{
			"tool_name":     "Bash",
			"command":       "go test ./...",
			"tool_response": strings.Repeat("very noisy output line\n", 100),
		})
		if err != nil {
			t.Fatal(err)
		}
		readStdinAll = func() ([]byte, error) { return payload, nil }
		cfg := config.Defaults()
		cfg.Filter.PassthroughMaxChars = 80
		cfg.Hooks.CodexPostToolMinTokens = 0
		configLoadFn = func() (*config.Config, error) { return cfg, nil }
		osGetwd = func() (string, error) { return "/tmp", nil }

		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handleClaudePostToolCmd([]string{"--"})
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		out := buf.String()
		if !strings.Contains(out, `"updatedToolOutput"`) || !strings.Contains(out, `"hookEventName":"PostToolUse"`) {
			t.Fatalf("expected Claude updatedToolOutput JSON, got %q", out)
		}
		if !strings.Contains(out, `"stdout"`) || !strings.Contains(out, `"interrupted":false`) || !strings.Contains(out, `"isImage":false`) {
			t.Fatalf("expected Claude Bash output shape, got %q", out)
		}
		if strings.Contains(out, strings.Repeat("very noisy output line\n", 10)) {
			t.Fatalf("expected compacted output, got %q", out)
		}
		readStdinAll = origRead
		configLoadFn = origConfigLoad
		osGetwd = origGetwd
		termIsTerminalFn = origTerm
	})
}

func TestHandleClaudePostToolCmdFailOpenAndSkipBranches(t *testing.T) {
	origTerm := termIsTerminalFn
	origRead := readStdinAll
	origGetwd := osGetwd
	origConfigLoad := configLoadFn
	origStdout := os.Stdout
	t.Cleanup(func() {
		termIsTerminalFn = origTerm
		readStdinAll = origRead
		osGetwd = origGetwd
		configLoadFn = origConfigLoad
		os.Stdout = origStdout
	})

	runNoOutput := func(t *testing.T, payload []byte, cfg *config.Config) {
		t.Helper()
		t.Setenv("SLIMFERENCE_CLAUDE_HOOK_MODE", "max")
		termIsTerminalFn = func(int) bool { return false }
		readStdinAll = func() ([]byte, error) { return payload, nil }
		if cfg == nil {
			cfg = config.Defaults()
		}
		configLoadFn = func() (*config.Config, error) { return cfg, nil }
		osGetwd = func() (string, error) { return "", errors.New("cwd unavailable") }

		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Stdout = w
		handleClaudePostToolCmd(nil)
		_ = w.Close()
		os.Stdout = origStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		if buf.Len() != 0 {
			t.Fatalf("expected no stdout, got %q", buf.String())
		}
	}

	t.Run("invalid_arg_exits_before_mode_check", func(t *testing.T) {
		rp, cleanup := redirectStderr()
		code, exited := captureExit(func() { handleClaudePostToolCmd([]string{"--bad"}) })
		cleanup()
		var stderr bytes.Buffer
		_, _ = io.Copy(&stderr, rp)
		if !exited || code != 1 || !strings.Contains(stderr.String(), "usage: slimference claudeposttool") {
			t.Fatalf("exit=(%d,%v) stderr=%q", code, exited, stderr.String())
		}
	})

	t.Run("terminal_exits", func(t *testing.T) {
		t.Setenv("SLIMFERENCE_CLAUDE_HOOK_MODE", "max")
		termIsTerminalFn = func(int) bool { return true }
		rp, cleanup := redirectStderr()
		code, exited := captureExit(func() { handleClaudePostToolCmd(nil) })
		cleanup()
		var stderr bytes.Buffer
		_, _ = io.Copy(&stderr, rp)
		if !exited || code != 1 || !strings.Contains(stderr.String(), "pipe Claude PostToolUse") {
			t.Fatalf("exit=(%d,%v) stderr=%q", code, exited, stderr.String())
		}
	})

	t.Run("read_error_exits", func(t *testing.T) {
		t.Setenv("SLIMFERENCE_CLAUDE_HOOK_MODE", "max")
		termIsTerminalFn = func(int) bool { return false }
		readStdinAll = func() ([]byte, error) { return nil, errors.New("stdin boom") }
		rp, cleanup := redirectStderr()
		code, exited := captureExit(func() { handleClaudePostToolCmd(nil) })
		cleanup()
		var stderr bytes.Buffer
		_, _ = io.Copy(&stderr, rp)
		if !exited || code != 1 || !strings.Contains(stderr.String(), "stdin boom") {
			t.Fatalf("exit=(%d,%v) stderr=%q", code, exited, stderr.String())
		}
	})

	t.Run("parse_fail_open", func(t *testing.T) {
		runNoOutput(t, []byte(`{`), nil)
	})

	t.Run("skip_no_response", func(t *testing.T) {
		runNoOutput(t, []byte(`{"session_id":"s","tool_name":"Bash"}`), nil)
	})

	t.Run("skip_tiny", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Hooks.CodexPostToolMinTokens = 1000
		runNoOutput(t, []byte(`{"session_id":"s","tool_name":"Bash","tool_response":"tiny"}`), cfg)
	})

	t.Run("passthrough_when_unchanged", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Hooks.CodexPostToolMinTokens = 0
		cfg.Filter.PassthroughMaxChars = 10_000
		runNoOutput(t, []byte(`{"session_id":"s","tool_name":"Bash","tool_response":"already short"}`), cfg)
	})

	t.Run("cross_tool_dedup_replaces_output", func(t *testing.T) {
		home := t.TempDir()
		prevHome := osUserHomeDir
		osUserHomeDir = func() (string, error) { return home, nil }
		t.Cleanup(func() { osUserHomeDir = prevHome })
		observeSessionStartTurnState("sess-claude-dedup")
		paths := []string{
			"a.go", "b.go", "cmd/main.go", "internal/proxy/x.go", "internal/proxy/y.go",
			"internal/hooks/a.go", "internal/hooks/b.go", "docs/install.md", "docs/todo.md", "README.md",
		}
		statusText := " M " + strings.Join(paths, "\n?? ") + "\n"
		diffText := strings.Join(paths, "\n") + "\n"

		cfg := config.Defaults()
		cfg.Hooks.CodexPostToolMinTokens = 0
		cfg.Filter.PassthroughMaxChars = 10_000
		firstPayload, err := json.Marshal(map[string]string{
			"session_id":    "sess-claude-dedup",
			"tool_name":     "Bash",
			"command":       "git status --short",
			"cwd":           "/repo",
			"tool_response": statusText,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("SLIMFERENCE_CLAUDE_HOOK_MODE", "max")
		termIsTerminalFn = func(int) bool { return false }
		readStdinAll = func() ([]byte, error) { return firstPayload, nil }
		configLoadFn = func() (*config.Config, error) { return cfg, nil }
		osGetwd = func() (string, error) { return "/repo", nil }
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Stdout = w
		handleClaudePostToolCmd(nil)
		_ = w.Close()
		os.Stdout = origStdout
		_, _ = io.Copy(io.Discard, r)

		termIsTerminalFn = func(int) bool { return false }
		secondPayload, err := json.Marshal(map[string]string{
			"session_id":    "sess-claude-dedup",
			"tool_name":     "Bash",
			"command":       "git diff --name-only",
			"cwd":           "/repo",
			"tool_response": diffText,
		})
		if err != nil {
			t.Fatal(err)
		}
		readStdinAll = func() ([]byte, error) {
			return secondPayload, nil
		}
		configLoadFn = func() (*config.Config, error) { return cfg, nil }
		osGetwd = func() (string, error) { return "/repo", nil }
		r, w, err = os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Stdout = w
		handleClaudePostToolCmd(nil)
		_ = w.Close()
		os.Stdout = origStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		out := buf.String()
		if !strings.Contains(out, "git paths already shown") || !strings.Contains(out, "updatedToolOutput") {
			t.Fatalf("expected cross-tool dedup replacement, got %q", out)
		}
	})

	t.Run("encode_error_exits", func(t *testing.T) {
		t.Setenv("SLIMFERENCE_CLAUDE_HOOK_MODE", "max")
		termIsTerminalFn = func(int) bool { return false }
		payload, err := json.Marshal(map[string]string{
			"tool_name":     "Bash",
			"command":       "go test ./...",
			"tool_response": strings.Repeat("very noisy output line\n", 100),
		})
		if err != nil {
			t.Fatal(err)
		}
		readStdinAll = func() ([]byte, error) { return payload, nil }
		cfg := config.Defaults()
		cfg.Filter.PassthroughMaxChars = 80
		cfg.Hooks.CodexPostToolMinTokens = 0
		configLoadFn = func() (*config.Config, error) { return cfg, nil }
		osGetwd = func() (string, error) { return "/tmp", nil }

		_, closedOut, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		_ = closedOut.Close()
		os.Stdout = closedOut
		rp, cleanup := redirectStderr()
		code, exited := captureExit(func() { handleClaudePostToolCmd(nil) })
		cleanup()
		os.Stdout = origStdout
		var stderr bytes.Buffer
		_, _ = io.Copy(&stderr, rp)
		if !exited || code != 1 || !strings.Contains(stderr.String(), "encode claudeposttool output") {
			t.Fatalf("exit=(%d,%v) stderr=%q", code, exited, stderr.String())
		}
	})
}

func TestHandleSubcommandClaudePostToolIsNotExposed(t *testing.T) {
	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { handleSubcommand([]string{"claudeposttool"}) })
	cleanup()
	var stderr bytes.Buffer
	_, _ = io.Copy(&stderr, rp)
	if !exited || code != 1 || !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("exit=(%d,%v) stderr=%q", code, exited, stderr.String())
	}
}

func TestPostToolReplacementDecisionHelpers(t *testing.T) {
	if postToolBelowMinTokens("abc", 0) {
		t.Fatal("disabled min-token skip must be false")
	}
	if !postToolBelowMinTokens(strings.Repeat("a", 300), 100) {
		t.Fatal("long low-token text should still skip below min tokens")
	}
	if postToolBelowMinTokens(strings.Repeat("word ", 1000), 100) {
		t.Fatal("large token body must not skip below min tokens")
	}

	t.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "compact")
	if !codexPostToolShouldEmitReplacement(true, 100, 100, "") {
		t.Fatal("compact mode must emit changed replacement")
	}
	if codexPostToolShouldEmitReplacement(false, 1000, 1, "") {
		t.Fatal("unchanged output must not emit replacement")
	}
	if codexPostToolShouldEmitReplacement(true, 1000, 0, "") {
		t.Fatal("empty final output must not emit replacement")
	}

	t.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "auto")
	if codexPostToolShouldEmitReplacement(true, 100, 100, "summary") {
		t.Fatal("auto mode must reject non-saving replacement")
	}
	compactThresholds := codexPostToolAutoThresholdsForContext("[git status] 20 paths")
	truncatedThresholds := codexPostToolAutoThresholdsForContext("[output truncated to 40 characters]")
	if codexPostToolAutoReplacementWorthIt(1000, 900, compactThresholds) {
		t.Fatal("small savings must not pass auto replacement threshold")
	}
	if codexPostToolAutoReplacementWorthIt(4000, 3300, compactThresholds) {
		t.Fatal("savings pct threshold must reject weak compaction")
	}
	if codexPostToolAutoReplacementWorthIt(700, 1, compactThresholds) {
		t.Fatal("small original token estimate must not pass auto replacement threshold")
	}
	if !codexPostToolAutoReplacementWorthIt(900, 500, compactThresholds) {
		t.Fatal("medium positive auto replacement must pass lowered threshold")
	}
	if codexPostToolShouldEmitReplacement(true, 900, 700, "summary") {
		t.Fatal("auto mode must reject weak final-context savings")
	}
	if !codexPostToolShouldEmitReplacement(true, 900, 500, "summary") {
		t.Fatal("auto mode must accept net-positive final context")
	}
	if codexPostToolShouldEmitReplacement(true, 1600, 700, "[output truncated to 40 characters]") {
		t.Fatal("auto mode must keep conservative threshold for generic truncation")
	}
	if !codexPostToolAutoReplacementWorthIt(8000, 3000, truncatedThresholds) {
		t.Fatal("large generic truncation must still pass conservative auto threshold")
	}
}

func TestCodexPostToolArchiveContext(t *testing.T) {
	withPreview := codexPostToolArchiveContext(toolarchive.Entry{
		ID:      "tool-ctx",
		URI:     "local-archive://tool-ctx",
		Command: "go test ./...",
		Preview: strings.Repeat("x", codexPostToolContextPreviewChars+20),
	})
	for _, want := range []string{
		`Bash output for "go test ./..." compacted by Slimference.`,
		"Raw archive: local-archive://tool-ctx",
		"Archive ID: tool-ctx",
		"archived preview",
	} {
		if !strings.Contains(withPreview, want) {
			t.Fatalf("archive context missing %q in %q", want, withPreview)
		}
	}

	withoutPreview := codexPostToolArchiveContext(toolarchive.Entry{
		ID:  "tool-no-preview",
		URI: "local-archive://tool-no-preview",
	})
	if !strings.Contains(withoutPreview, "Bash output compacted by Slimference.") ||
		strings.Contains(withoutPreview, "Preview:") {
		t.Fatalf("archive context without preview=%q", withoutPreview)
	}
}

func TestCodexHookModeDefaultsAuto(t *testing.T) {
	t.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "")
	if got := codexHookMode(); got != "auto" {
		t.Fatalf("default hook mode=%q", got)
	}
	t.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "nonsense")
	if got := codexHookMode(); got != "auto" {
		t.Fatalf("unknown hook mode=%q", got)
	}
}

func TestHandleSubcommand_PostToolDispatch(t *testing.T) {
	origTerm := termIsTerminalFn
	origRead := readStdinAll
	origConfigLoad := configLoadFn
	defer func() {
		termIsTerminalFn = origTerm
		readStdinAll = origRead
		configLoadFn = origConfigLoad
	}()

	termIsTerminalFn = func(int) bool { return false }
	t.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "compact")
	payload, err := json.Marshal(map[string]string{
		"command":       "git status",
		"tool_response": strings.Repeat("x", 500),
	})
	if err != nil {
		t.Fatal(err)
	}
	readStdinAll = func() ([]byte, error) {
		return payload, nil
	}
	cfg := config.Defaults()
	cfg.Filter.PassthroughMaxChars = 20
	cfg.Hooks.CodexPostToolMinTokens = 0
	configLoadFn = func() (*config.Config, error) { return cfg, nil }

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"posttool"})
	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), `"hookEventName":"PostToolUse"`) {
		t.Fatalf("unexpected stdout: %q", buf.String())
	}
}

func TestHandlePostToolCmdRecordsFlight(t *testing.T) {
	origTerm := termIsTerminalFn
	origRead := readStdinAll
	origConfigLoad := configLoadFn
	origGetwd := osGetwd
	origHome := osUserHomeDir
	defer func() {
		termIsTerminalFn = origTerm
		readStdinAll = origRead
		configLoadFn = origConfigLoad
		osGetwd = origGetwd
		osUserHomeDir = origHome
	}()

	tmp := t.TempDir()
	osUserHomeDir = func() (string, error) { return tmp, nil }
	decisionsPath := filepath.Join(tmp, "decisions.jsonl")
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", decisionsPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(tmp, "missing.toml"))
	termIsTerminalFn = func(int) bool { return false }
	readStdinAll = func() ([]byte, error) {
		return []byte(`{"session_id":"sess-hook","tool_name":"Bash","command":"echo hi","tool_response":"short output"}`), nil
	}
	cfg := config.Defaults()
	cfg.Filter.PassthroughMaxChars = 500
	cfg.Hooks.CodexPostToolMinTokens = 0
	configLoadFn = func() (*config.Config, error) { return cfg, nil }
	osGetwd = func() (string, error) { return tmp, nil }

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handlePostToolCmd(nil)
	_ = w.Close()
	os.Stdout = oldStdout
	_, _ = io.Copy(io.Discard, r)

	data, err := os.ReadFile(decisionsPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"source":"hook_post"`) ||
		!strings.Contains(text, `"session_id":"sess-hook"`) ||
		!strings.Contains(text, `"route_mode":"hook"`) ||
		!strings.Contains(text, `"flight"`) {
		t.Fatalf("flight log missing hook fields: %s", text)
	}
}

func TestHandlePostToolCmdArchivesSilentlyByDefault(t *testing.T) {
	origTerm := termIsTerminalFn
	origRead := readStdinAll
	origConfigLoad := configLoadFn
	origGetwd := osGetwd
	origHome := osUserHomeDir
	defer func() {
		termIsTerminalFn = origTerm
		readStdinAll = origRead
		configLoadFn = origConfigLoad
		osGetwd = origGetwd
		osUserHomeDir = origHome
	}()

	home := t.TempDir()
	osUserHomeDir = func() (string, error) { return home, nil }
	osGetwd = func() (string, error) { return home, nil }
	termIsTerminalFn = func(int) bool { return false }
	payload, err := json.Marshal(map[string]string{
		"session_id":    "sess-silent-archive",
		"tool_name":     "Bash",
		"tool_use_id":   "tool-silent-archive",
		"command":       "go test ./...",
		"tool_response": strings.Repeat("line\n", 200),
	})
	if err != nil {
		t.Fatal(err)
	}
	readStdinAll = func() ([]byte, error) { return payload, nil }
	cfg := config.Defaults()
	cfg.Filter.PassthroughMaxChars = 40
	cfg.Hooks.CodexPostToolMinTokens = 0
	configLoadFn = func() (*config.Config, error) { return cfg, nil }

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handlePostToolCmd(nil)
	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if buf.Len() != 0 {
		t.Fatalf("expected silent archive by default, got %q", buf.String())
	}
	if _, err := os.Stat(filepath.Join(toolarchive.DefaultDir(home), "entries", "tool-silent-archive.json")); err != nil {
		t.Fatalf("archive metadata missing: %v", err)
	}
}

func TestHookTurnStateHelpers(t *testing.T) {
	origHome := osUserHomeDir
	defer func() { osUserHomeDir = origHome }()
	home := t.TempDir()
	osUserHomeDir = func() (string, error) { return home, nil }

	if got := hookPathCandidates("/repo", "src/main.go"); len(got) != 2 || got[0] != "src/main.go" || got[1] != "/repo/src/main.go" {
		t.Fatalf("path candidates=%v", got)
	}
	if got := hookPathCandidates("/repo", " "); got != nil {
		t.Fatalf("blank candidates=%v", got)
	}
	if got := firstNonEmpty("", "  ", "x"); got != "x" {
		t.Fatalf("firstNonEmpty=%q", got)
	}
	if got := firstNonEmpty("", " "); got != "" {
		t.Fatalf("firstNonEmpty blank=%q", got)
	}

	observeSessionStartTurnState("sess-helper")
	observePostToolTurnState("/repo", filter.PostToolPayload{
		SessionID:    "sess-helper",
		ToolName:     "apply_patch",
		CommandLine:  "*** Update File: src/main.go",
		CWD:          "/repo",
		FilePaths:    []string{"src/main.go"},
		ToolUseID:    "tool-1",
		ToolResponse: "ok",
	})
	ctx := hookFileReadContext("/repo", filter.PostToolPayload{
		SessionID:    "sess-helper",
		ToolName:     "Bash",
		CommandLine:  "cat src/main.go",
		ToolResponse: "package main\n",
	})
	if !ctx.RecentlyEdited {
		t.Fatal("expected recently edited context")
	}
	ctx = hookFileReadContext("/repo", filter.PostToolPayload{
		SessionID:    "sess-helper",
		ToolName:     "Bash",
		CommandLine:  "cat src/other.go",
		ToolResponse: "package main\n",
	})
	if ctx.RecentlyEdited {
		t.Fatal("unseen file should not be recently edited")
	}
	observePostToolTurnState("/wrong", filter.PostToolPayload{
		SessionID:    "sess-helper",
		ToolName:     "Edit",
		CommandLine:  "apply_patch",
		CWD:          "/actual",
		FilePaths:    []string{"src/cwd.go"},
		ToolResponse: "ok",
	})
	ctx = hookFileReadContext("/wrong", filter.PostToolPayload{
		SessionID:    "sess-helper",
		ToolName:     "Bash",
		CommandLine:  "cat src/cwd.go",
		CWD:          "/actual",
		ToolResponse: "package main\n",
	})
	if !ctx.RecentlyEdited {
		t.Fatal("hook cwd should take precedence over process cwd")
	}
	observePostToolTurnState("/repo", filter.PostToolPayload{
		SessionID:    "sess-helper",
		ToolName:     "Bash",
		CommandLine:  "cat src/other.go",
		ToolResponse: "package main\n",
	})
	observeUserPromptTurnState("sess-helper")
	if got := currentHookTurnID(sessions.DefaultHookStateDir(home), "sess-helper"); got != "turn-2" {
		t.Fatalf("current hook turn id=%q", got)
	}
	observeStopTurnState("sess-helper")

	osUserHomeDir = func() (string, error) { return "", errors.New("home") }
	ctx = hookFileReadContext("/repo", filter.PostToolPayload{SessionID: "sess-helper", CommandLine: "cat src/main.go"})
	if ctx.RecentlyEdited {
		t.Fatal("home error should degrade to scan context")
	}
	observeSessionStartTurnState("ignored")
	observeUserPromptTurnState("ignored")
	observeStopTurnState("ignored")
	observePostToolTurnState("/repo", filter.PostToolPayload{SessionID: "ignored", ToolName: "Edit", FilePaths: []string{"x.go"}})
	dirAsFile := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(dirAsFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := currentHookTurnID(dirAsFile, "ignored"); got != "" {
		t.Fatalf("currentHookTurnID error path=%q", got)
	}

	if postToolLooksLikeEdit(filter.PostToolPayload{ToolName: "Bash", CommandLine: "cat main.go"}) {
		t.Fatal("cat must not look like edit")
	}
	if !postToolLooksLikeEdit(filter.PostToolPayload{CommandLine: "*** Add File: x.go"}) {
		t.Fatal("patch command must look like edit")
	}
}

func TestPostToolCrossToolDedup(t *testing.T) {
	origHome := osUserHomeDir
	defer func() { osUserHomeDir = origHome }()
	home := t.TempDir()
	osUserHomeDir = func() (string, error) { return home, nil }
	observeSessionStartTurnState("sess-git")

	status := filter.PostToolPayload{
		SessionID:    "sess-git",
		CommandLine:  "git status --short",
		CWD:          "/repo",
		ToolResponse: " M a.go\n?? b.go\n",
	}
	if out, ok := applyPostToolCrossToolDedup("/wrong", status); ok || len(out) != 0 {
		t.Fatalf("status should observe only: ok=%v out=%q", ok, out)
	}
	diff := filter.PostToolPayload{
		SessionID:    "sess-git",
		CommandLine:  "git diff --name-only",
		CWD:          "/repo",
		ToolResponse: "b.go\na.go\n",
	}
	out, ok := applyPostToolCrossToolDedup("/wrong", diff)
	if !ok || !strings.Contains(string(out), "2 git paths already shown") || !strings.Contains(string(out), "git status --short") {
		t.Fatalf("expected crosstool marker: ok=%v out=%q", ok, out)
	}
	otherCWD := diff
	otherCWD.CWD = "/other"
	if out, ok := applyPostToolCrossToolDedup("/wrong", otherCWD); ok || len(out) != 0 {
		t.Fatalf("different cwd should not elide: ok=%v out=%q", ok, out)
	}
	if out, ok := applyPostToolCrossToolDedup("/repo", filter.PostToolPayload{CommandLine: "git diff --name-only", ToolResponse: "a.go\n"}); ok || len(out) != 0 {
		t.Fatalf("missing session should passthrough: ok=%v out=%q", ok, out)
	}
	if out, ok := applyPostToolCrossToolDedup("/repo", filter.PostToolPayload{SessionID: "sess-git", CommandLine: "> out.txt", ToolResponse: "a.go\n"}); ok || len(out) != 0 {
		t.Fatalf("empty argv should passthrough: ok=%v out=%q", ok, out)
	}
	osUserHomeDir = func() (string, error) { return "", errors.New("home") }
	if out, ok := applyPostToolCrossToolDedup("/repo", diff); ok || len(out) != 0 {
		t.Fatalf("home error should passthrough: ok=%v out=%q", ok, out)
	}
	if got := gitPathListForPostTool([]string{"git", "diff", "--name-status"}, "M\ta.go\n"); got != nil {
		t.Fatalf("name-status must not produce name-only paths: %v", got)
	}
}

func TestHandlePostToolCmdCrossToolDedup(t *testing.T) {
	origTerm := termIsTerminalFn
	origRead := readStdinAll
	origConfigLoad := configLoadFn
	origGetwd := osGetwd
	origHome := osUserHomeDir
	defer func() {
		termIsTerminalFn = origTerm
		readStdinAll = origRead
		configLoadFn = origConfigLoad
		osGetwd = origGetwd
		osUserHomeDir = origHome
	}()

	home := t.TempDir()
	osUserHomeDir = func() (string, error) { return home, nil }
	osGetwd = func() (string, error) { return "/repo", nil }
	termIsTerminalFn = func(int) bool { return false }
	t.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "compact")
	cfg := config.Defaults()
	cfg.Filter.PassthroughMaxChars = 500
	cfg.Hooks.CodexPostToolMinTokens = 0
	configLoadFn = func() (*config.Config, error) { return cfg, nil }
	observeSessionStartTurnState("sess-hook-git")

	run := func(payload []byte) string {
		t.Helper()
		readStdinAll = func() ([]byte, error) { return payload, nil }
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handlePostToolCmd(nil)
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		return buf.String()
	}
	statusPayload := []byte(`{"session_id":"sess-hook-git","tool_name":"Bash","cwd":"/repo","command":"git status --short","tool_response":" M a.go\n?? b.go\n"}`)
	_ = run(statusPayload)
	diffPayload := []byte(`{"session_id":"sess-hook-git","tool_name":"Bash","cwd":"/repo","command":"git diff --name-only","tool_response":"b.go\na.go\n"}`)
	out := run(diffPayload)
	if !strings.Contains(out, "2 git paths already shown") || !strings.Contains(out, `"continue":false`) {
		t.Fatalf("posttool crosstool output=%q", out)
	}
}

func TestHandleCodexHookCmd(t *testing.T) {
	origTerm := termIsTerminalFn
	origRead := readStdinAll
	origHome := osUserHomeDir
	defer func() {
		termIsTerminalFn = origTerm
		readStdinAll = origRead
		osUserHomeDir = origHome
	}()
	termIsTerminalFn = func(int) bool { return false }
	home := t.TempDir()
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	t.Run("session_start_silent_only_in_silent_mode", func(t *testing.T) {
		t.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "silent")
		readStdinAll = func() ([]byte, error) {
			return []byte(`{"session_id":"s1","hook_event_name":"SessionStart","source":"startup"}`), nil
		}
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handleCodexHookCmd([]string{"session-start"})
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		if buf.Len() != 0 {
			t.Fatalf("silent mode must emit no output, got %q", buf.String())
		}
	})

	t.Run("session_start_auto_is_silent", func(t *testing.T) {
		// Default mode (auto) keeps normal Codex session starts silent.
		// Explicit compact/aggressive/debug modes own context injection.
		t.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "auto")
		readStdinAll = func() ([]byte, error) {
			return []byte(`{"session_id":"s1","hook_event_name":"SessionStart","source":"startup"}`), nil
		}
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handleCodexHookCmd([]string{"session-start"})
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		if buf.Len() != 0 {
			t.Fatalf("auto-mode session-start must be silent, got %q", buf.String())
		}
	})

	t.Run("session_start_compact_full_awareness", func(t *testing.T) {
		t.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "compact")
		readStdinAll = func() ([]byte, error) {
			return []byte(`{"session_id":"sc","hook_event_name":"SessionStart","source":"startup"}`), nil
		}
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handleCodexHookCmd([]string{"session-start"})
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		out := buf.String()
		if !strings.Contains(out, "Slimference is compacting tool outputs") {
			t.Fatalf("compact-mode awareness missing, got %q", out)
		}
	})

	t.Run("session_start_aggressive_full_awareness", func(t *testing.T) {
		t.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "aggressive")
		readStdinAll = func() ([]byte, error) {
			return []byte(`{"session_id":"sa","hook_event_name":"SessionStart","source":"startup"}`), nil
		}
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handleCodexHookCmd([]string{"session-start"})
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		out := buf.String()
		if !strings.Contains(out, "aggressively compacting") {
			t.Fatalf("aggressive-mode awareness missing, got %q", out)
		}
	})

	t.Run("session_start_debug_adds_context", func(t *testing.T) {
		t.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "debug")
		readStdinAll = func() ([]byte, error) {
			return []byte(`{"session_id":"s1","hook_event_name":"SessionStart","source":"startup"}`), nil
		}
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handleCodexHookCmd([]string{"session-start"})
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		out := buf.String()
		if !strings.Contains(out, `"hookEventName":"SessionStart"`) || !strings.Contains(out, "Local hook debug mode is active") {
			t.Fatalf("unexpected session hook output: %q", out)
		}
	})

	t.Run("permission_request_denies_destructive_command", func(t *testing.T) {
		readStdinAll = func() ([]byte, error) {
			return []byte(`{"session_id":"s2","hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"rm -rf /"}}`), nil
		}
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handleCodexHookCmd([]string{"permission-request"})
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		out := buf.String()
		if !strings.Contains(out, `"hookEventName":"PermissionRequest"`) || !strings.Contains(out, `"behavior":"deny"`) {
			t.Fatalf("unexpected permission hook output: %q", out)
		}
	})

	t.Run("permission_request_allows_safe_command", func(t *testing.T) {
		readStdinAll = func() ([]byte, error) {
			return []byte(`{"session_id":"s3","hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"git status"}}`), nil
		}
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handleCodexHookCmd([]string{"permission-request"})
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		if !strings.Contains(buf.String(), `"behavior":"allow"`) {
			t.Fatalf("unexpected allow output: %q", buf.String())
		}
	})

	t.Run("user_prompt_submit_no_output", func(t *testing.T) {
		readStdinAll = func() ([]byte, error) {
			return []byte(`{"session_id":"s4","hook_event_name":"UserPromptSubmit","prompt":"hi"}`), nil
		}
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handleCodexHookCmd([]string{"user-prompt-submit"})
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		if buf.Len() != 0 {
			t.Fatalf("expected no output, got %q", buf.String())
		}
	})

	t.Run("posttool_timeout_records_flight", func(t *testing.T) {
		decisionsPath := filepath.Join(t.TempDir(), "decisions.jsonl")
		t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", decisionsPath)
		readStdinAll = func() ([]byte, error) {
			return []byte(`{"session_id":"s-timeout","tool_name":"Bash","hook_event_name":"PostToolUse"}`), nil
		}
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handleCodexHookCmd([]string{"posttool-timeout"})
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		if buf.Len() != 0 {
			t.Fatalf("expected no output, got %q", buf.String())
		}
		data, err := os.ReadFile(decisionsPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"bypass_reason":"timeout_fail_open"`) || !strings.Contains(string(data), `"session_id":"s-timeout"`) {
			t.Fatalf("timeout flight missing: %s", data)
		}
	})

	t.Run("stop_emits_valid_json", func(t *testing.T) {
		readStdinAll = func() ([]byte, error) {
			return []byte(`{"session_id":"s5","hook_event_name":"Stop","turn_id":"t1"}`), nil
		}
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handleCodexHookCmd([]string{"stop"})
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		if !strings.Contains(buf.String(), `"continue":true`) {
			t.Fatalf("unexpected stop output: %q", buf.String())
		}
	})

	t.Run("pre_compact_writes_marker", func(t *testing.T) {
		readStdinAll = func() ([]byte, error) {
			return []byte(`{"session_id":"sc1","turn_id":"t9","hook_event_name":"PreCompact","trigger":"auto","cwd":"/x","model":"o4","transcript_path":null}`), nil
		}
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handleCodexHookCmd([]string{"pre-compact"})
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		if !strings.Contains(buf.String(), `"continue":true`) {
			t.Fatalf("pre-compact output should be continue:true, got %q", buf.String())
		}
		marker := filepath.Join(home, ".slimference", "run", "compact", "pre", "sc1.json")
		data, err := os.ReadFile(marker)
		if err != nil {
			t.Fatalf("pre-compact marker not written: %v", err)
		}
		got := string(data)
		for _, want := range []string{`"phase":"pre"`, `"session_id":"sc1"`, `"turn_id":"t9"`, `"trigger":"auto"`} {
			if !strings.Contains(got, want) {
				t.Fatalf("marker missing %q in %q", want, got)
			}
		}
	})

	t.Run("post_compact_writes_marker_and_records_trigger", func(t *testing.T) {
		readStdinAll = func() ([]byte, error) {
			return []byte(`{"session_id":"sc2","turn_id":"t10","hook_event_name":"PostCompact","trigger":"manual","cwd":"/x","model":"o4","transcript_path":null}`), nil
		}
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handleCodexHookCmd([]string{"post-compact"})
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		if !strings.Contains(buf.String(), `"continue":true`) {
			t.Fatalf("post-compact output should be continue:true, got %q", buf.String())
		}
		marker := filepath.Join(home, ".slimference", "run", "compact", "post", "sc2.json")
		data, err := os.ReadFile(marker)
		if err != nil {
			t.Fatalf("post-compact marker not written: %v", err)
		}
		got := string(data)
		for _, want := range []string{`"phase":"post"`, `"session_id":"sc2"`, `"trigger":"manual"`} {
			if !strings.Contains(got, want) {
				t.Fatalf("marker missing %q in %q", want, got)
			}
		}
	})

	t.Run("pre_compact_with_empty_session_skips_marker", func(t *testing.T) {
		readStdinAll = func() ([]byte, error) {
			return []byte(`{"hook_event_name":"PreCompact","trigger":"auto"}`), nil
		}
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handleCodexHookCmd([]string{"pre-compact"})
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		// Hook must still emit continue:true; marker silently skipped.
		if !strings.Contains(buf.String(), `"continue":true`) {
			t.Fatalf("empty-session pre-compact must still emit continue:true, got %q", buf.String())
		}
	})

	t.Run("pre_compact_unknown_trigger_defaults", func(t *testing.T) {
		readStdinAll = func() ([]byte, error) {
			return []byte(`{"session_id":"sc3","turn_id":"t11"}`), nil
		}
		oldStdout := os.Stdout
		_, w, _ := os.Pipe()
		os.Stdout = w
		handleCodexHookCmd([]string{"pre-compact"})
		_ = w.Close()
		os.Stdout = oldStdout
		marker := filepath.Join(home, ".slimference", "run", "compact", "pre", "sc3.json")
		data, err := os.ReadFile(marker)
		if err != nil {
			t.Fatalf("read marker: %v", err)
		}
		if !strings.Contains(string(data), `"trigger":"unknown"`) {
			t.Fatalf("expected trigger:unknown fallback, got %s", string(data))
		}
	})
}

func TestHandleCodexHookCmd_Edges(t *testing.T) {
	origTerm := termIsTerminalFn
	origRead := readStdinAll
	defer func() {
		termIsTerminalFn = origTerm
		readStdinAll = origRead
	}()

	tests := []struct {
		name string
		args []string
		term bool
		read func() ([]byte, error)
		want string
	}{
		{
			name: "missing_event",
			args: nil,
			want: "usage: slimference codexhook",
		},
		{
			name: "terminal_stdin",
			args: []string{"stop"},
			term: true,
			want: "usage: slimference codexhook",
		},
		{
			name: "read_error",
			args: []string{"stop"},
			read: func() ([]byte, error) { return nil, errors.New("read boom") },
			want: "read stdin: read boom",
		},
		{
			name: "unknown_event",
			args: []string{"future"},
			read: func() ([]byte, error) { return []byte(`{}`), nil },
			want: "unknown codexhook event: future",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			termIsTerminalFn = func(int) bool { return tt.term }
			readStdinAll = tt.read
			if readStdinAll == nil {
				readStdinAll = func() ([]byte, error) { return []byte(`{}`), nil }
			}
			rp, cleanup := redirectStderr()
			code, exited := captureExit(func() { handleCodexHookCmd(tt.args) })
			cleanup()
			var buf bytes.Buffer
			_, _ = io.Copy(&buf, rp)
			if !exited || code != 1 || !strings.Contains(buf.String(), tt.want) {
				t.Fatalf("exited=%v code=%d stderr=%q want %q", exited, code, buf.String(), tt.want)
			}
		})
	}
}

func TestCodexHookJSONExtractionAndNoCommand(t *testing.T) {
	if got := extractJSONText([]byte(`not-json`), "session_id"); got != "" {
		t.Fatalf("invalid json extracted %q", got)
	}
	payload := []byte(`{"outer":[{"inner":{"conversation_id":"c-nested"}}]}`)
	if got := extractJSONText(payload, "session_id", "conversation_id"); got != "c-nested" {
		t.Fatalf("nested extraction=%q", got)
	}

	origRead := readStdinAll
	origTerm := termIsTerminalFn
	defer func() {
		readStdinAll = origRead
		termIsTerminalFn = origTerm
	}()
	termIsTerminalFn = func(int) bool { return false }
	readStdinAll = func() ([]byte, error) {
		return []byte(`{"session_id":"s-no-command","tool_name":"Bash"}`), nil
	}
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleCodexHookCmd([]string{"permission-request"})
	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if buf.Len() != 0 {
		t.Fatalf("no-command permission hook should emit no output, got %q", buf.String())
	}
}

func TestCodexHookEncodeErrorsAndFlightBranches(t *testing.T) {
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	decisionsPath := filepath.Join(t.TempDir(), "decisions.jsonl")
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", decisionsPath)

	recordHookFlight("test_hook", "sess", "Tool", "expanded", 1, 100, []int{1}, errors.New("hook boom"))
	data, err := os.ReadFile(decisionsPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "hook boom") || !strings.Contains(text, `"saved":0`) {
		t.Fatalf("flight branches missing: %s", text)
	}
	recordHookFlight("test_hook_zero", "sess", "Tool", "zero", 12, 0, nil, nil)

	tests := []struct {
		name string
		fn   func()
	}{
		{"session_start", func() {
			oldMode, hadMode := os.LookupEnv("SLIMFERENCE_CODEX_HOOK_MODE")
			_ = os.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "debug")
			defer func() {
				if hadMode {
					_ = os.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", oldMode)
				} else {
					_ = os.Unsetenv("SLIMFERENCE_CODEX_HOOK_MODE")
				}
			}()
			handleCodexSessionStartHook([]byte(`{"session_id":"s"}`))
		}},
		{"permission_deny", func() {
			handleCodexPermissionRequestHook([]byte(`{"tool_input":{"command":"rm -rf /"}}`))
		}},
		{"permission_allow", func() {
			handleCodexPermissionRequestHook([]byte(`{"tool_input":{"command":"git status"}}`))
		}},
		{"stop", func() { handleCodexStopHook([]byte(`{"session_id":"s"}`)) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origStdout := os.Stdout
			_, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			_ = w.Close()
			os.Stdout = w
			rp, cleanup := redirectStderr()
			code, exited := captureExit(tt.fn)
			cleanup()
			os.Stdout = origStdout
			var buf bytes.Buffer
			_, _ = io.Copy(&buf, rp)
			if !exited || code != 1 || !strings.Contains(buf.String(), "encode codexhook output") {
				t.Fatalf("exited=%v code=%d stderr=%q", exited, code, buf.String())
			}
		})
	}
}

func TestRecordHookFlight_NoDecisionsLogNoop(t *testing.T) {
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", "")

	recordHookFlight("test_hook_no_log", "sess", "Tool", "noop", 12, 6, nil, nil)
}

func TestHandleSubcommand_CodexHookDispatch(t *testing.T) {
	origTerm := termIsTerminalFn
	origRead := readStdinAll
	defer func() {
		termIsTerminalFn = origTerm
		readStdinAll = origRead
	}()
	// silent mode keeps the legacy zero-output session-start behavior so
	// dispatch tests can verify the routing without the awareness preamble
	// interfering. Active modes (auto/compact/aggressive) inject the
	// preamble — covered by TestHandleCodexHookCmd subtests.
	t.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "silent")
	termIsTerminalFn = func(int) bool { return false }
	readStdinAll = func() ([]byte, error) {
		return []byte(`{"session_id":"s","source":"startup"}`), nil
	}
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"codexhook", "session-start"})
	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if buf.Len() != 0 {
		t.Fatalf("codexhook session-start must be silent in silent mode, output=%q", buf.String())
	}
}

func TestNotifyInterceptReceived(t *testing.T) {
	ch := make(chan struct{}, 1)
	notifyInterceptReceived(ch)
	select {
	case <-ch:
	default:
		t.Fatal("expected first notify")
	}
	ch <- struct{}{}
	notifyInterceptReceived(ch)
	select {
	case <-ch:
	default:
		t.Fatal("channel should still contain original value after default branch")
	}
}

func TestHandleSubcommand_DaemonAndServiceDispatch(t *testing.T) {
	origIsRunning := daemonIsRunningFn
	origRunDaemon := daemonRunFn
	origStop := daemonStopFn
	origInstall := daemonInstallLaunchdFn
	origUninstall := daemonUninstallFn
	origFormatStatus := daemonFormatStatusFn
	origExecutable := osExecutable
	origStartProcess := osStartProcess
	defer func() {
		daemonIsRunningFn = origIsRunning
		daemonRunFn = origRunDaemon
		daemonStopFn = origStop
		daemonInstallLaunchdFn = origInstall
		daemonUninstallFn = origUninstall
		daemonFormatStatusFn = origFormatStatus
		osExecutable = origExecutable
		osStartProcess = origStartProcess
	}()

	daemonRunFn = func(func() (int, func(context.Context) error, error)) error { return nil }
	started := false
	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		if started {
			return true, &daemon.PIDFile{PID: 99, Port: 8990}, nil
		}
		return false, nil, nil
	}
	daemonStopFn = func() error {
		started = false
		return nil
	}
	daemonInstallLaunchdFn = func(string) error { return nil }
	daemonUninstallFn = func() error { return nil }
	daemonFormatStatusFn = func() ([]byte, error) { return []byte(`{"running":false}`), nil }
	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	osStartProcess = func(string, []string, *os.ProcAttr) (*os.Process, error) {
		started = true
		return os.FindProcess(os.Getpid())
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"daemon"})
	handleSubcommand([]string{"start"})
	handleSubcommand([]string{"stop"})
	handleSubcommand([]string{"restart"})
	handleSubcommand([]string{"service", "install"})
	handleSubcommand([]string{"service", "uninstall"})
	handleSubcommand([]string{"service", "status"})
	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Slimference daemon started.") || !strings.Contains(out, "Service installed.") || !strings.Contains(out, `"running":false`) {
		t.Fatalf("unexpected stdout: %q", out)
	}
}

func TestHandleDaemonStartStopRestartAndServiceCommands(t *testing.T) {
	origIsRunning := daemonIsRunningFn
	origRunDaemon := daemonRunFn
	origStop := daemonStopFn
	origInstall := daemonInstallLaunchdFn
	origUninstall := daemonUninstallFn
	origFormatStatus := daemonFormatStatusFn
	origExecutable := osExecutable
	origStartProcess := osStartProcess
	origStartDetached := startDetachedDaemonFn
	defer func() {
		daemonIsRunningFn = origIsRunning
		daemonRunFn = origRunDaemon
		daemonStopFn = origStop
		daemonInstallLaunchdFn = origInstall
		daemonUninstallFn = origUninstall
		daemonFormatStatusFn = origFormatStatus
		osExecutable = origExecutable
		osStartProcess = origStartProcess
		startDetachedDaemonFn = origStartDetached
	}()

	rp, cleanup := redirectStderr()
	daemonRunFn = func(func() (int, func(context.Context) error, error)) error {
		return errors.New("daemon fail")
	}
	code, exited := captureExit(func() { handleDaemonCmd(nil) })
	cleanup()
	var errBuf bytes.Buffer
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "daemon fail") {
		t.Fatalf("handleDaemonCmd: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	daemonStarted := false
	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		if daemonStarted {
			return true, &daemon.PIDFile{PID: 99, Port: 8990}, nil
		}
		return false, nil, nil
	}
	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	startCalls := 0
	startDetachedDaemonFn = func(binary string) error {
		startCalls++
		daemonStarted = true
		if binary != "/tmp/slimference" {
			t.Fatalf("unexpected start binary: %q", binary)
		}
		return nil
	}
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleStartCmd()
	_ = w.Close()
	os.Stdout = oldStdout
	if startCalls != 1 {
		t.Fatalf("expected one start call, got %d", startCalls)
	}

	daemonStopCalls := 0
	daemonStopFn = func() error {
		daemonStopCalls++
		daemonStarted = false
		return nil
	}
	handleStopCmd()
	restartChecks := 0
	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		restartChecks++
		if restartChecks == 1 {
			return true, &daemon.PIDFile{PID: 1, Port: 2}, nil
		}
		if daemonStarted {
			return true, &daemon.PIDFile{PID: 99, Port: 8990}, nil
		}
		return false, nil, nil
	}
	handleRestartCmd()
	if daemonStopCalls != 2 {
		t.Fatalf("expected stop to be called twice, got %d", daemonStopCalls)
	}

	daemonInstallLaunchdFn = func(binary string) error {
		if binary != "/tmp/slimference" {
			t.Fatalf("unexpected install binary: %q", binary)
		}
		return nil
	}
	daemonUninstallFn = func() error { return nil }
	daemonFormatStatusFn = func() ([]byte, error) { return []byte(`{"running":true}`), nil }
	handleServiceCmd([]string{"install"})
	handleServiceCmd([]string{"uninstall"})
	oldStdout = os.Stdout
	r, w, _ = os.Pipe()
	os.Stdout = w
	handleServiceCmd([]string{"status"})
	_ = w.Close()
	os.Stdout = oldStdout
	var outBuf bytes.Buffer
	_, _ = io.Copy(&outBuf, r)
	if !strings.Contains(outBuf.String(), `"running":true`) {
		t.Fatalf("status stdout: %q", outBuf.String())
	}
}

func TestHandleServiceStartStopRestartAndLogsAliases(t *testing.T) {
	origIsRunning := daemonIsRunningFn
	origStop := daemonStopFn
	origExecutable := osExecutable
	origStartDetached := startDetachedDaemonFn
	origReadRecent := daemonReadRecentLogLinesFn
	defer func() {
		daemonIsRunningFn = origIsRunning
		daemonStopFn = origStop
		osExecutable = origExecutable
		startDetachedDaemonFn = origStartDetached
		daemonReadRecentLogLinesFn = origReadRecent
	}()

	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	started := false
	startCalls := 0
	startDetachedDaemonFn = func(binary string) error {
		startCalls++
		started = true
		if binary != "/tmp/slimference" {
			t.Fatalf("unexpected start binary: %q", binary)
		}
		return nil
	}
	stopCalls := 0
	daemonStopFn = func() error {
		stopCalls++
		started = false
		return nil
	}
	checks := 0
	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		if started {
			return true, &daemon.PIDFile{PID: 99, Port: 8990}, nil
		}
		checks++
		switch checks {
		case 1: // service start
			return false, nil, nil
		case 2: // service restart checks whether it should stop first
			return true, &daemon.PIDFile{PID: 7, Port: 8990}, nil
		default: // service restart delegates to start
			return false, nil, nil
		}
	}
	daemonReadRecentLogLinesFn = func(path string, n int, since time.Time) ([]string, error) {
		if n != 200 || !since.IsZero() {
			t.Fatalf("unexpected log args n=%d since=%v", n, since)
		}
		return []string{"service-log-line"}, nil
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleServiceCmd([]string{"start"})
	handleServiceCmd([]string{"stop"})
	handleServiceCmd([]string{"restart"})
	handleServiceCmd([]string{"logs", "--stream=stdout"})
	_ = w.Close()
	os.Stdout = oldStdout

	var out bytes.Buffer
	_, _ = io.Copy(&out, r)
	if startCalls != 2 || stopCalls != 2 {
		t.Fatalf("startCalls=%d stopCalls=%d output=%q", startCalls, stopCalls, out.String())
	}
	if !strings.Contains(out.String(), "Slimference daemon started.") || !strings.Contains(out.String(), "service-log-line") {
		t.Fatalf("unexpected service alias output: %q", out.String())
	}
}

func TestHandleStartStopRestartAndServiceCommandErrors(t *testing.T) {
	origIsRunning := daemonIsRunningFn
	origStop := daemonStopFn
	origInstall := daemonInstallLaunchdFn
	origUninstall := daemonUninstallFn
	origFormatStatus := daemonFormatStatusFn
	origExecutable := osExecutable
	origStartDetached := startDetachedDaemonFn
	defer func() {
		daemonIsRunningFn = origIsRunning
		daemonStopFn = origStop
		daemonInstallLaunchdFn = origInstall
		daemonUninstallFn = origUninstall
		daemonFormatStatusFn = origFormatStatus
		osExecutable = origExecutable
		startDetachedDaemonFn = origStartDetached
	}()

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		return false, nil, errors.New("check fail")
	}
	rp, cleanup := redirectStderr()
	code, exited := captureExit(handleStartCmd)
	cleanup()
	var errBuf bytes.Buffer
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "check daemon") {
		t.Fatalf("handleStartCmd check error: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		return true, &daemon.PIDFile{PID: 9, Port: 8990}, nil
	}
	rp, cleanup = redirectStderr()
	code, exited = captureExit(handleStartCmd)
	cleanup()
	errBuf.Reset()
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "already running") {
		t.Fatalf("handleStartCmd already running: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) { return false, nil, nil }
	osExecutable = func() (string, error) { return "", errors.New("no executable") }
	rp, cleanup = redirectStderr()
	code, exited = captureExit(handleStartCmd)
	cleanup()
	errBuf.Reset()
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "executable") {
		t.Fatalf("handleStartCmd executable: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	startDetachedDaemonFn = func(string) error {
		return errors.New("spawn fail")
	}
	rp, cleanup = redirectStderr()
	code, exited = captureExit(handleStartCmd)
	cleanup()
	errBuf.Reset()
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "start daemon") {
		t.Fatalf("handleStartCmd spawn: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	daemonStopFn = func() error { return errors.New("stop fail") }
	rp, cleanup = redirectStderr()
	code, exited = captureExit(handleStopCmd)
	cleanup()
	errBuf.Reset()
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "stop fail") {
		t.Fatalf("handleStopCmd: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		return true, &daemon.PIDFile{PID: 1, Port: 2}, nil
	}
	rp, cleanup = redirectStderr()
	code, exited = captureExit(handleRestartCmd)
	cleanup()
	errBuf.Reset()
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "stop: stop fail") {
		t.Fatalf("handleRestartCmd: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() { handleServiceCmd(nil) })
	cleanup()
	errBuf.Reset()
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "usage: slimference service") {
		t.Fatalf("handleServiceCmd usage: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	osExecutable = func() (string, error) { return "", errors.New("no executable") }
	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() { handleServiceCmd([]string{"install"}) })
	cleanup()
	errBuf.Reset()
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "executable") {
		t.Fatalf("handleServiceCmd install executable: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	daemonInstallLaunchdFn = func(string) error { return errors.New("install fail") }
	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() { handleServiceCmd([]string{"install"}) })
	cleanup()
	errBuf.Reset()
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "install fail") {
		t.Fatalf("handleServiceCmd install fail: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	daemonUninstallFn = func() error { return errors.New("uninstall fail") }
	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() { handleServiceCmd([]string{"uninstall"}) })
	cleanup()
	errBuf.Reset()
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "uninstall fail") {
		t.Fatalf("handleServiceCmd uninstall fail: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	daemonFormatStatusFn = func() ([]byte, error) { return nil, errors.New("status fail") }
	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() { handleServiceCmd([]string{"status"}) })
	cleanup()
	errBuf.Reset()
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "status fail") {
		t.Fatalf("handleServiceCmd status fail: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() { handleServiceCmd([]string{"nope"}) })
	cleanup()
	errBuf.Reset()
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "unknown service command") {
		t.Fatalf("handleServiceCmd unknown: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}
}

func TestHandleStartWaitBranches(t *testing.T) {
	origIsRunning := daemonIsRunningFn
	origExecutable := osExecutable
	origStartDetached := startDetachedDaemonFn
	origAfter := timeAfterFn
	origTicker := newTickerFn
	defer func() {
		daemonIsRunningFn = origIsRunning
		osExecutable = origExecutable
		startDetachedDaemonFn = origStartDetached
		timeAfterFn = origAfter
		newTickerFn = origTicker
	}()

	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	startDetachedDaemonFn = func(string) error { return nil }
	timeAfterFn = func(time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}
	newTickerFn = func(time.Duration) *time.Ticker { return time.NewTicker(time.Hour) }
	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) { return false, nil, nil }
	rp, cleanup := redirectStderr()
	code, exited := captureExit(handleStartCmd)
	cleanup()
	var errBuf bytes.Buffer
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "check `slimference service logs") {
		t.Fatalf("handleStartCmd wait timeout: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	timeAfterFn = func(time.Duration) <-chan time.Time { return make(chan time.Time) }
	newTickerFn = func(time.Duration) *time.Ticker { return time.NewTicker(time.Millisecond) }
	checks := 0
	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		checks++
		if checks <= 2 {
			return false, nil, nil
		}
		return true, nil, nil
	}
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleStartCmd()
	_ = w.Close()
	os.Stdout = oldStdout
	var out bytes.Buffer
	_, _ = io.Copy(&out, r)
	if !strings.Contains(out.String(), "Slimference daemon started.") {
		t.Fatalf("generic start output=%q", out.String())
	}

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) { return false, nil, errors.New("check later") }
	if _, err := waitForDaemonStarted(time.Second, time.Millisecond); err == nil || !strings.Contains(err.Error(), "check daemon") {
		t.Fatalf("expected check daemon error, got %v", err)
	}
}

func TestServiceControlAdapterStartDaemonWaitError(t *testing.T) {
	origIsRunning := daemonIsRunningFn
	origExecutable := osExecutable
	origStartDetached := startDetachedDaemonFn
	origAfter := timeAfterFn
	origTicker := newTickerFn
	defer func() {
		daemonIsRunningFn = origIsRunning
		osExecutable = origExecutable
		startDetachedDaemonFn = origStartDetached
		timeAfterFn = origAfter
		newTickerFn = origTicker
	}()

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) { return false, nil, nil }
	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	startDetachedDaemonFn = func(string) error { return nil }
	timeAfterFn = func(time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}
	newTickerFn = func(time.Duration) *time.Ticker { return time.NewTicker(time.Hour) }
	err := (&serviceControlAdapter{}).StartDaemon()
	if err == nil || !strings.Contains(err.Error(), "timeout after") {
		t.Fatalf("expected wait error, got %v", err)
	}
}

func TestStartProxyForDaemon(t *testing.T) {
	stubReloadPIDWriter(t)
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

	configLoadFn = func() (*config.Config, error) {
		return nil, errors.New("config failed")
	}
	if _, _, err := startProxyForDaemon(); err == nil || !strings.Contains(err.Error(), "config load") {
		t.Fatalf("expected config load error, got %v", err)
	}

	cfg := config.Defaults()
	configLoadFn = func() (*config.Config, error) { return cfg, nil }
	loadTUIStateFn = func() (*tui.PersistedState, error) { return nil, nil }
	newProxyFn = func(*config.Config) *proxy.Proxy { return proxy.New(cfg) }
	proxyStartRunnerFn = func(*proxy.Proxy) error { return errors.New("proxy failed") }
	proxyHasListenerFn = func(*proxy.Proxy) bool { return false }
	timeAfterFn = func(time.Duration) <-chan time.Time {
		ch := make(chan time.Time)
		go func() { ch <- time.Now() }()
		return ch
	}
	newTickerFn = func(time.Duration) *time.Ticker { return time.NewTicker(time.Hour) }
	_, _, err := startProxyForDaemon()
	if err == nil || !strings.Contains(err.Error(), "proxy start") {
		t.Fatalf("expected proxy start error, got %v", err)
	}

	cfg.Proxy.ListenPort = 7777
	configLoadFn = func() (*config.Config, error) { return cfg, nil }
	var gotProxy *proxy.Proxy
	newProxyFn = func(got *config.Config) *proxy.Proxy {
		if got != cfg {
			t.Fatal("newProxyFn should receive loaded config")
		}
		gotProxy = proxy.New(got)
		return gotProxy
	}
	proxyStartRunnerFn = func(*proxy.Proxy) error { return nil }
	proxyHasListenerFn = func(p *proxy.Proxy) bool { return p == gotProxy }
	port, shutdown, err := startProxyForDaemon()
	if err != nil {
		t.Fatalf("startProxyForDaemon success: %v", err)
	}
	if port != cfg.Proxy.ListenPort || shutdown == nil {
		t.Fatalf("unexpected return values: port=%d shutdown_nil=%v", port, shutdown == nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	proxyHasListenerFn = func(*proxy.Proxy) bool { return false }
	_, _, err = startProxyForDaemon()
	if err == nil || !strings.Contains(err.Error(), "timeout after") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestStartDetachedDaemon(t *testing.T) {
	origMkdirAll := osMkdirAll
	origOpenFile := osOpenFile
	origStartProcess := osStartProcess
	origEnviron := osEnvironFn
	origStdout := daemonStdoutLogPathFn
	origStderr := daemonStderrLogPathFn
	defer func() {
		osMkdirAll = origMkdirAll
		osOpenFile = origOpenFile
		osStartProcess = origStartProcess
		osEnvironFn = origEnviron
		daemonStdoutLogPathFn = origStdout
		daemonStderrLogPathFn = origStderr
	}()

	tmp := t.TempDir()
	daemonStdoutLogPathFn = func() string { return filepath.Join(tmp, "daemon.stdout.log") }
	daemonStderrLogPathFn = func() string { return filepath.Join(tmp, "daemon.stderr.log") }

	osMkdirAll = func(string, os.FileMode) error { return errors.New("mkdir fail") }
	if err := startDetachedDaemon("/tmp/slimference"); err == nil || !strings.Contains(err.Error(), "create log dir") {
		t.Fatalf("expected mkdir failure, got %v", err)
	}

	osMkdirAll = func(string, os.FileMode) error { return nil }
	openCalls := 0
	osOpenFile = func(name string, flag int, perm os.FileMode) (*os.File, error) {
		openCalls++
		if openCalls == 1 {
			return nil, errors.New("stdin fail")
		}
		return os.OpenFile(name, flag|os.O_CREATE, perm)
	}
	if err := startDetachedDaemon("/tmp/slimference"); err == nil || !strings.Contains(err.Error(), "open stdin") {
		t.Fatalf("expected stdin failure, got %v", err)
	}

	openCalls = 0
	osOpenFile = func(name string, flag int, perm os.FileMode) (*os.File, error) {
		openCalls++
		if strings.Contains(name, "stdout") {
			return nil, errors.New("stdout fail")
		}
		return os.OpenFile(name, flag|os.O_CREATE, perm)
	}
	if err := startDetachedDaemon("/tmp/slimference"); err == nil || !strings.Contains(err.Error(), "open stdout log") {
		t.Fatalf("expected stdout failure, got %v", err)
	}

	openCalls = 0
	osOpenFile = func(name string, flag int, perm os.FileMode) (*os.File, error) {
		openCalls++
		if strings.Contains(name, "stderr") {
			return nil, errors.New("stderr fail")
		}
		return os.OpenFile(name, flag|os.O_CREATE, perm)
	}
	if err := startDetachedDaemon("/tmp/slimference"); err == nil || !strings.Contains(err.Error(), "open stderr log") {
		t.Fatalf("expected stderr failure, got %v", err)
	}

	osOpenFile = os.OpenFile
	osEnvironFn = func() []string { return []string{"SLIMFERENCE_WSS_AB_CAPTURE=/tmp/frames.jsonl"} }
	started := false
	osStartProcess = func(name string, argv []string, attr *os.ProcAttr) (*os.Process, error) {
		started = true
		if name != "/tmp/slimference" || len(argv) != 2 || argv[1] != "daemon" || attr == nil || attr.Sys == nil {
			t.Fatalf("unexpected daemon spawn: name=%q argv=%v attr=%#v", name, argv, attr)
		}
		if len(attr.Env) != 1 || attr.Env[0] != "SLIMFERENCE_WSS_AB_CAPTURE=/tmp/frames.jsonl" {
			t.Fatalf("daemon env not propagated: %#v", attr.Env)
		}
		return os.FindProcess(os.Getpid())
	}
	if err := startDetachedDaemon("/tmp/slimference"); err != nil {
		t.Fatalf("startDetachedDaemon: %v", err)
	}
	if !started {
		t.Fatal("expected daemon spawn to run")
	}

	osStartProcess = func(string, []string, *os.ProcAttr) (*os.Process, error) {
		return nil, errors.New("spawn fail")
	}
	if err := startDetachedDaemon("/tmp/slimference"); err == nil || !strings.Contains(err.Error(), "spawn fail") {
		t.Fatalf("expected spawn failure, got %v", err)
	}
}

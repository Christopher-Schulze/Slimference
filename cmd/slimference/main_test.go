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

	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/daemon"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/filter"
	"github.com/slimference/slimference/internal/proxy"
	"github.com/slimference/slimference/internal/summarization"
	"github.com/slimference/slimference/internal/tui"
	"github.com/slimference/slimference/internal/types"
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

func TestRunTUI_Layer2PolicyError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	origConfigLoad := configLoadFn
	origAfterStart := runTUIAfterStartFn
	origStdin := os.Stdin
	defer func() {
		configLoadFn = origConfigLoad
		runTUIAfterStartFn = origAfterStart
		os.Stdin = origStdin
	}()

	cfg := config.Defaults()
	cfg.Compression.Layer2Enabled = true
	configLoadFn = func() (*config.Config, error) { return cfg, nil }
	runTUIAfterStartFn = func(tui.ProxyInterface) {
		t.Fatal("runTUIAfterStartFn must not run after policy error")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	_ = w.Close()
	os.Stdin = r

	rp, cleanup := redirectStderr()
	code, exited := captureExit(runTUI)
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "layer2 policy") {
		t.Fatalf("exited=%v code=%d stderr=%q", exited, code, buf.String())
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

// TestGetLayer2Status_withCache covers the cs != nil branch (main.go:1275-1277) by using
// a proxy whose Layer2 cache has a stored summary.
func TestGetLayer2Status_withCache(t *testing.T) {
	cfg := config.Defaults()
	p := proxy.New(cfg)
	cache := p.GetLayer2Cache()
	if cache == nil {
		t.Skip("no layer2 cache available")
	}
	cache.Store(&summarization.CachedSummary{
		Summary:   "test",
		CreatedAt: time.Now(),
	})
	a := newProxyAdapter(p)
	st := a.GetLayer2Status()
	if !st.HasCache {
		t.Fatalf("expected HasCache=true, got %+v", st)
	}
	if st.LastRun.IsZero() {
		t.Fatalf("expected non-zero LastRun, got %+v", st)
	}
}

// exitPanic is the sentinel type panicked by the injected exitFn.
type exitPanic struct{ code int }

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

func (p *testTUIProxy) GetLayer2Status() tui.Layer2Status { return tui.Layer2Status{} }

func (p *testTUIProxy) GetReadCacheStatus() tui.ReadCacheStatus { return tui.ReadCacheStatus{} }

func (p *testTUIProxy) GetQualityStatus() tui.QualityStatus { return tui.QualityStatus{} }

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

	t.Run("json_parse_error", func(t *testing.T) {
		termIsTerminalFn = func(int) bool { return false }
		readStdinAll = func() ([]byte, error) { return []byte("{"), nil }
		rp, cleanup := redirectStderr()
		code, exited := captureExit(func() { handlePostToolCmd(nil) })
		cleanup()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rp)
		if !exited || code != 1 || !strings.Contains(buf.String(), "filter: JSON") {
			t.Fatalf("exited=%v code=%d stderr=%q", exited, code, buf.String())
		}
		readStdinAll = origRead
	})

	t.Run("missing_tool_response_error", func(t *testing.T) {
		termIsTerminalFn = func(int) bool { return false }
		readStdinAll = func() ([]byte, error) { return []byte(`{"command":"git status"}`), nil }
		rp, cleanup := redirectStderr()
		code, exited := captureExit(func() { handlePostToolCmd([]string{"--"}) })
		cleanup()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rp)
		if !exited || code != 1 || !strings.Contains(buf.String(), `no string field "tool_response"`) {
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

	t.Run("compacted_output_emits_hook_json", func(t *testing.T) {
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

	if postToolLooksLikeEdit(filter.PostToolPayload{ToolName: "Bash", CommandLine: "cat main.go"}) {
		t.Fatal("cat must not look like edit")
	}
	if !postToolLooksLikeEdit(filter.PostToolPayload{CommandLine: "*** Add File: x.go"}) {
		t.Fatal("patch command must look like edit")
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

	t.Run("session_start_adds_context", func(t *testing.T) {
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
		if !strings.Contains(out, `"hookEventName":"SessionStart"`) || !strings.Contains(out, "Slimference hooks are active") {
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
		{"session_start", func() { handleCodexSessionStartHook([]byte(`{"session_id":"s"}`)) }},
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
	if !strings.Contains(buf.String(), `"hookEventName":"SessionStart"`) {
		t.Fatalf("codexhook subcommand output=%q", buf.String())
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
	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) { return false, nil, nil }
	daemonStopFn = func() error { return nil }
	daemonInstallLaunchdFn = func(string) error { return nil }
	daemonUninstallFn = func() error { return nil }
	daemonFormatStatusFn = func() ([]byte, error) { return []byte(`{"running":false}`), nil }
	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	osStartProcess = func(string, []string, *os.ProcAttr) (*os.Process, error) {
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

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		return false, nil, nil
	}
	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	startCalls := 0
	startDetachedDaemonFn = func(binary string) error {
		startCalls++
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
		return nil
	}
	handleStopCmd()
	restartChecks := 0
	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		restartChecks++
		if restartChecks == 1 {
			return true, &daemon.PIDFile{PID: 1, Port: 2}, nil
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
	startCalls := 0
	startDetachedDaemonFn = func(binary string) error {
		startCalls++
		if binary != "/tmp/slimference" {
			t.Fatalf("unexpected start binary: %q", binary)
		}
		return nil
	}
	stopCalls := 0
	daemonStopFn = func() error {
		stopCalls++
		return nil
	}
	checks := 0
	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
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

func TestStartProxyForDaemon(t *testing.T) {
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
	origStdout := daemonStdoutLogPathFn
	origStderr := daemonStderrLogPathFn
	defer func() {
		osMkdirAll = origMkdirAll
		osOpenFile = origOpenFile
		osStartProcess = origStartProcess
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
	started := false
	osStartProcess = func(name string, argv []string, attr *os.ProcAttr) (*os.Process, error) {
		started = true
		if name != "/tmp/slimference" || len(argv) != 2 || argv[1] != "daemon" || attr == nil || attr.Sys == nil {
			t.Fatalf("unexpected daemon spawn: name=%q argv=%v attr=%#v", name, argv, attr)
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

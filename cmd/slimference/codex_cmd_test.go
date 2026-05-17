package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/codexroute"
)

func withCodexCmdStubs(t *testing.T) {
	t.Helper()
	oldHome := codexRouteHomeFn
	oldEnable := codexRouteEnableFn
	oldDisable := codexRouteDisableFn
	oldInspect := codexRouteInspectFn
	oldHealth := codexRouteHealthFn
	oldProxyRun := codexProxyRunFn
	t.Cleanup(func() {
		codexRouteHomeFn = oldHome
		codexRouteEnableFn = oldEnable
		codexRouteDisableFn = oldDisable
		codexRouteInspectFn = oldInspect
		codexRouteHealthFn = oldHealth
		codexProxyRunFn = oldProxyRun
	})
}

func TestCodexCmdRunUsesProxiedWhenDaemonHealthy(t *testing.T) {
	withCodexCmdStubs(t)
	codexRouteHealthFn = func(host, port string) error { return nil }
	var got []string
	codexProxyRunFn = func(args []string, env proxyEnv) int {
		got = append([]string(nil), args...)
		return 0
	}
	p, _, errBuf := newTestPrinter()
	rc := runCodexCmd([]string{"run", "--", "exec", "hi"}, p)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errBuf.String())
	}
	joined := strings.Join(got, "\x00")
	for _, want := range []string{"run", "codex", "--proxied", "--host=127.0.0.1", "--port=8990", "exec", "hi"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("proxy args missing %q in %#v", want, got)
		}
	}
}

func TestCodexCmdRunFallsBackDirectWhenDaemonDown(t *testing.T) {
	withCodexCmdStubs(t)
	codexRouteHealthFn = func(host, port string) error { return errors.New("dial refused") }
	var got []string
	codexProxyRunFn = func(args []string, env proxyEnv) int {
		got = append([]string(nil), args...)
		return 0
	}
	p, _, errBuf := newTestPrinter()
	rc := runCodexCmd([]string{"run", "exec", "hi"}, p)
	if rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(strings.Join(got, "\x00"), "--direct") {
		t.Fatalf("expected direct fallback args, got %#v", got)
	}
	if !strings.Contains(errBuf.String(), "falling back to direct Codex") {
		t.Fatalf("missing fallback warning: %q", errBuf.String())
	}
}

func TestCodexCmdEnableDisableStatus(t *testing.T) {
	withCodexCmdStubs(t)
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("model = \"gpt-5\"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	codexRouteHomeFn = func() (string, error) { return home, nil }
	codexRouteHealthFn = func(host, port string) error { return nil }

	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"enable"}, p); rc != 0 {
		t.Fatalf("enable rc=%d stdout=%s stderr=%s", rc, out.String(), errBuf.String())
	}
	if !strings.Contains(out.String(), "Codex route enabled") ||
		!strings.Contains(out.String(), "ChatGPT.app stay direct") {
		t.Fatalf("bad enable output: %q", out.String())
	}

	out.Reset()
	errBuf.Reset()
	if rc := runCodexCmd([]string{"status", "--json"}, p); rc != 0 {
		t.Fatalf("status rc=%d stderr=%s", rc, errBuf.String())
	}
	var got struct {
		Route  codexroute.Status `json:"route"`
		Daemon struct {
			Reachable bool `json:"reachable"`
		} `json:"daemon"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode status: %v\n%s", err, out.String())
	}
	if !got.Route.Complete || !got.Daemon.Reachable {
		t.Fatalf("bad status: %+v", got)
	}

	out.Reset()
	errBuf.Reset()
	if rc := runCodexCmd([]string{"disable"}, p); rc != 0 {
		t.Fatalf("disable rc=%d stderr=%s", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "Codex route disabled") {
		t.Fatalf("bad disable output: %q", out.String())
	}
}

func TestCodexCmdEnableMissingConfigIsNotSuccess(t *testing.T) {
	withCodexCmdStubs(t)
	home := t.TempDir()
	codexRouteHomeFn = func() (string, error) { return home, nil }
	p, _, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"enable"}, p); rc != 1 {
		t.Fatalf("enable rc=%d stderr=%s", rc, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "does not exist") ||
		!strings.Contains(errBuf.String(), "No files changed") {
		t.Fatalf("bad missing-config output: %q", errBuf.String())
	}
}

func TestCodexCmdDryRunAndErrors(t *testing.T) {
	withCodexCmdStubs(t)
	codexRouteHomeFn = func() (string, error) { return "/tmp/home", nil }
	codexRouteHealthFn = func(host, port string) error { return errors.New("down") }
	codexProxyRunFn = func(args []string, env proxyEnv) int { return 0 }
	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd(nil, p); rc != 0 || !strings.Contains(out.String(), "usage: slimference codex") {
		t.Fatalf("help rc=%d out=%q", rc, out.String())
	}
	out.Reset()
	if rc := runCodexCmd([]string{"enable", "--dry-run", "--host=::1"}, p); rc != 0 {
		t.Fatalf("dry-run rc=%d stderr=%s", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "http://[::1]:8990/backend-api/codex") {
		t.Fatalf("dry-run missing block: %q", out.String())
	}
	out.Reset()
	errBuf.Reset()
	if rc := runCodexCmd([]string{"bogus"}, p); rc != 2 || !strings.Contains(errBuf.String(), "unknown subcommand") {
		t.Fatalf("unknown rc=%d stderr=%q", rc, errBuf.String())
	}
	errBuf.Reset()
	if rc := runCodexCmd([]string{"status", "--bogus"}, p); rc != 2 || !strings.Contains(errBuf.String(), "unknown flag") {
		t.Fatalf("bad flag rc=%d stderr=%q", rc, errBuf.String())
	}
}

func TestCodexCmdHelpAndErrorBranches(t *testing.T) {
	withCodexCmdStubs(t)
	p, out, errBuf := newTestPrinter()
	for _, args := range [][]string{
		{"--help"},
		{"help"},
		{"run", "--help"},
		{"enable", "--help"},
		{"disable", "--help"},
		{"status", "--help"},
	} {
		out.Reset()
		errBuf.Reset()
		if rc := runCodexCmd(args, p); rc != 0 || !strings.Contains(out.String(), "usage: slimference") {
			t.Fatalf("%v rc=%d out=%q err=%q", args, rc, out.String(), errBuf.String())
		}
	}

	out.Reset()
	errBuf.Reset()
	if rc := runCodexCmd([]string{"enable", "--bogus"}, p); rc != 2 || !strings.Contains(errBuf.String(), "unknown flag") {
		t.Fatalf("enable bad flag rc=%d err=%q", rc, errBuf.String())
	}

	codexRouteHomeFn = func() (string, error) { return "", errors.New("no home") }
	for _, args := range [][]string{{"enable"}, {"disable"}, {"status"}} {
		errBuf.Reset()
		if rc := runCodexCmd(args, p); rc != 1 || !strings.Contains(errBuf.String(), "HOME unresolved") {
			t.Fatalf("%v rc=%d err=%q", args, rc, errBuf.String())
		}
	}

	codexRouteHomeFn = func() (string, error) { return "/tmp/home", nil }
	codexRouteEnableFn = func(string, string) (codexroute.Event, error) {
		return codexroute.Event{}, errors.New("enable failed")
	}
	errBuf.Reset()
	if rc := runCodexCmd([]string{"enable"}, p); rc != 1 || !strings.Contains(errBuf.String(), "enable failed") {
		t.Fatalf("enable error rc=%d err=%q", rc, errBuf.String())
	}

	codexRouteDisableFn = func(string) (codexroute.Event, error) {
		return codexroute.Event{}, errors.New("disable failed")
	}
	errBuf.Reset()
	if rc := runCodexCmd([]string{"disable"}, p); rc != 1 || !strings.Contains(errBuf.String(), "disable failed") {
		t.Fatalf("disable error rc=%d err=%q", rc, errBuf.String())
	}

	codexRouteInspectFn = func(string, string) (codexroute.Status, error) {
		return codexroute.Status{}, errors.New("inspect failed")
	}
	errBuf.Reset()
	if rc := runCodexCmd([]string{"status"}, p); rc != 1 || !strings.Contains(errBuf.String(), "inspect failed") {
		t.Fatalf("status error rc=%d err=%q", rc, errBuf.String())
	}

	out.Reset()
	if rc := runCodexCmd([]string{"disable", "--dry-run"}, p); rc != 0 ||
		!strings.Contains(out.String(), "would remove scoped Codex route") {
		t.Fatalf("disable dry-run rc=%d out=%q", rc, out.String())
	}
}

func TestCodexStatusHumanBranches(t *testing.T) {
	withCodexCmdStubs(t)
	codexRouteHomeFn = func() (string, error) { return "/tmp/home", nil }
	p, out, _ := newTestPrinter()

	codexRouteInspectFn = func(string, string) (codexroute.Status, error) {
		return codexroute.Status{Exists: true, Enabled: true, Complete: true, BaseURL: "http://127.0.0.1:8990/backend-api/codex"}, nil
	}
	codexRouteHealthFn = func(string, string) error { return nil }
	if rc := runCodexCmd([]string{"status"}, p); rc != 0 ||
		!strings.Contains(out.String(), "route is ready") {
		t.Fatalf("ready status rc=%d out=%q", rc, out.String())
	}

	out.Reset()
	codexRouteHealthFn = func(string, string) error { return errors.New("down") }
	if rc := runCodexCmd([]string{"status"}, p); rc != 0 ||
		!strings.Contains(out.String(), "daemon is unreachable") {
		t.Fatalf("down status rc=%d out=%q", rc, out.String())
	}

	out.Reset()
	codexRouteInspectFn = func(string, string) (codexroute.Status, error) {
		return codexroute.Status{
			Exists:     true,
			Enabled:    false,
			Complete:   false,
			Conflict:   "top-level model_provider already set",
			LegacyKeys: true,
			BaseURL:    "http://127.0.0.1:8990/backend-api/codex",
		}, nil
	}
	if rc := runCodexCmd([]string{"status"}, p); rc != 0 ||
		!strings.Contains(out.String(), "Route is disabled") ||
		!strings.Contains(out.String(), "Conflict top-level model_provider") ||
		!strings.Contains(out.String(), "Legacy") {
		t.Fatalf("disabled status rc=%d out=%q", rc, out.String())
	}
}

func TestHandleCodexCmdUsesExitFn(t *testing.T) {
	withCodexCmdStubs(t)
	oldExit := exitFn
	t.Cleanup(func() { exitFn = oldExit })
	got := -1
	exitFn = func(code int) { got = code }
	handleCodexCmd([]string{"--help"})
	if got != 0 {
		t.Fatalf("exit code=%d", got)
	}
}

func TestCodexProxyEnvCarriesPrinter(t *testing.T) {
	p := installPrinter{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	env := codexProxyEnv(p)
	if env.Stdout != p.Out || env.Stderr != p.Err || env.Stdin == nil {
		t.Fatalf("bad proxy env")
	}
	if env.LoadCA == nil || env.HealthCheck == nil || env.RunCommand == nil {
		t.Fatalf("missing proxy env dependencies")
	}
}

func TestServiceControlAdapterCodexRoute(t *testing.T) {
	withCodexCmdStubs(t)
	oldEnable := tuiCodexRouteEnableCmdFn
	oldDisable := tuiCodexRouteDisableCmdFn
	oldHealth := tuiCodexRouteHealthCheckFn
	oldHome := osUserHomeDir
	t.Cleanup(func() {
		tuiCodexRouteEnableCmdFn = oldEnable
		tuiCodexRouteDisableCmdFn = oldDisable
		tuiCodexRouteHealthCheckFn = oldHealth
		osUserHomeDir = oldHome
	})

	enableCalled := false
	disableCalled := false
	tuiCodexRouteEnableCmdFn = func(args []string, p installPrinter) int {
		enableCalled = true
		return 0
	}
	tuiCodexRouteDisableCmdFn = func(args []string, p installPrinter) int {
		disableCalled = true
		return 0
	}
	osUserHomeDir = func() (string, error) { return "/tmp/home", nil }
	codexRouteInspectFn = func(home, proxyURL string) (codexroute.Status, error) {
		return codexroute.Status{Exists: true, Enabled: true, Complete: true}, nil
	}
	tuiCodexRouteHealthCheckFn = func(host, port string) error { return nil }

	adapter := &serviceControlAdapter{}
	if err := adapter.EnableCodexRoute(); err != nil {
		t.Fatalf("EnableCodexRoute: %v", err)
	}
	if err := adapter.DisableCodexRoute(); err != nil {
		t.Fatalf("DisableCodexRoute: %v", err)
	}
	if !enableCalled || !disableCalled {
		t.Fatalf("route commands not called: enable=%v disable=%v", enableCalled, disableCalled)
	}
	status := adapter.CodexRouteStatus()
	if !status.Exists || !status.Enabled || !status.Complete || !status.DaemonReachable {
		t.Fatalf("bad route status: %+v", status)
	}
}

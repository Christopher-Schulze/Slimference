package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/integrate"
)

func captureIntegrate(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	oldO, oldE := os.Stdout, os.Stderr
	ro, wo, _ := os.Pipe()
	re, we, _ := os.Pipe()
	os.Stdout = wo
	os.Stderr = we
	defer func() { os.Stdout = oldO; os.Stderr = oldE }()
	fn()
	_ = wo.Close()
	_ = we.Close()
	var bo, be bytes.Buffer
	_, _ = io.Copy(&bo, ro)
	_, _ = io.Copy(&be, re)
	return bo.String(), be.String()
}

// isolateIntegrateEnv sets HOME to a temp dir and stubs out the hook
// installers so integrate tests do not touch the user's real files.
func isolateIntegrateEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("SLIMFERENCE_CONFIG", "")
	t.Setenv("SHELL", "/bin/zsh")

	origClaude := installClaudeHookFn
	origCodex := installCodexHookFn
	origRemoveClaude := removeClaudeHookFn
	origRemoveCodex := removeCodexHookFn
	installClaudeHookFn = func(string, string) error { return nil }
	installCodexHookFn = func(string, string) error { return nil }
	removeClaudeHookFn = func(string) error { return nil }
	removeCodexHookFn = func(string) error { return nil }
	t.Cleanup(func() {
		installClaudeHookFn = origClaude
		installCodexHookFn = origCodex
		removeClaudeHookFn = origRemoveClaude
		removeCodexHookFn = origRemoveCodex
	})
	return home
}

func prepareCodexConfig(t *testing.T, home string) string {
	t.Helper()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("model = \"gpt-5.2\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHandleIntegrateCmd_MissingVerbExits1(t *testing.T) {
	origExit := exitFn
	defer func() { exitFn = origExit }()
	var code int
	exitFn = func(c int) { code = c; panic(exitSentinel{}) }
	defer func() { _ = recover() }()
	handleIntegrateCmd(nil)
	if code != 1 {
		t.Fatalf("exit = %d", code)
	}
}

func TestHandleIntegrateCmd_UnknownVerbExits1(t *testing.T) {
	origExit := exitFn
	defer func() { exitFn = origExit }()
	var code int
	exitFn = func(c int) { code = c; panic(exitSentinel{}) }
	defer func() { _ = recover() }()
	handleIntegrateCmd([]string{"weird"})
	if code != 1 {
		t.Fatalf("exit = %d", code)
	}
}

func TestHandleIntegrateCmd_StatusHumanOutput(t *testing.T) {
	isolateIntegrateEnv(t)
	stdout, _ := captureIntegrate(t, func() {
		handleIntegrateCmd([]string{"status"})
	})
	if !strings.Contains(stdout, "Slimference Integration - Status") {
		t.Fatalf("missing banner: %q", stdout)
	}
	if !strings.Contains(stdout, "Claude Code:") {
		t.Fatalf("missing claude line: %q", stdout)
	}
	if !strings.Contains(stdout, "Codex:") {
		t.Fatalf("missing codex line: %q", stdout)
	}
}

func TestHandleIntegrateCmd_StatusJSON(t *testing.T) {
	isolateIntegrateEnv(t)
	stdout, _ := captureIntegrate(t, func() {
		handleIntegrateCmd([]string{"status", "--json"})
	})
	var rep integrate.Report
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("expected valid JSON, got %q (err=%v)", stdout, err)
	}
}

func TestHandleIntegrateCmd_InstallDryRunNoWrites(t *testing.T) {
	home := isolateIntegrateEnv(t)
	stdout, _ := captureIntegrate(t, func() {
		handleIntegrateCmd([]string{"install", "--dry-run"})
	})
	if !strings.Contains(stdout, "DRY_RUN_wrote_block") {
		t.Fatalf("dry-run marker missing: %q", stdout)
	}
	// Ensure no rc file was actually created.
	if _, err := os.Stat(filepath.Join(home, ".zshrc")); err == nil {
		t.Fatal("dry-run wrote to .zshrc")
	}
}

func TestHandleIntegrateCmd_InstallRemoveRoundTrip(t *testing.T) {
	home := isolateIntegrateEnv(t)
	cfgPath := prepareCodexConfig(t, home)
	captureIntegrate(t, func() {
		handleIntegrateCmd([]string{"install", "--client=codex", "--no-hook"})
	})
	content, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(content), "openai_base_url") ||
		!strings.Contains(string(content), "chatgpt_base_url") {
		t.Fatalf("codex config missing proxy URLs: %s", content)
	}
	captureIntegrate(t, func() {
		handleIntegrateCmd([]string{"remove", "--client=codex", "--no-hook"})
	})
	content, _ = os.ReadFile(cfgPath)
	if strings.Contains(string(content), ">>> slimference integration >>>") {
		t.Fatalf("remove left fence: %s", content)
	}
}

func TestHandleIntegrateCmd_ClaudeClientParkedNoWrites(t *testing.T) {
	home := isolateIntegrateEnv(t)
	_, stderr := captureIntegrate(t, func() {
		handleIntegrateCmd([]string{"install", "--client=claude"})
	})
	if !strings.Contains(stderr, "Claude Code is parked") {
		t.Fatalf("missing parked warning: %q", stderr)
	}
	if _, err := os.Stat(filepath.Join(home, ".zshrc")); !os.IsNotExist(err) {
		t.Fatalf("Claude install path must not write .zshrc, stat err=%v", err)
	}
}

func TestParseIntegrateFlags_ClientSeparated(t *testing.T) {
	opts, _, err := parseIntegrateFlags([]string{"--client", "codex", "--dry-run"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Client != "codex" || !opts.DryRun {
		t.Fatalf("unexpected opts: %+v", opts)
	}
}

func TestParseIntegrateFlags_ClientEqualsForm(t *testing.T) {
	opts, _, err := parseIntegrateFlags([]string{"--client=claude"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Client != "claude" {
		t.Fatalf("client = %q, want claude", opts.Client)
	}
}

func TestParseIntegrateFlags_InvalidClientErrors(t *testing.T) {
	if _, _, err := parseIntegrateFlags([]string{"--client", "bogus"}); err == nil {
		t.Fatal("expected error on bogus client")
	}
	if _, _, err := parseIntegrateFlags([]string{"--client"}); err == nil {
		t.Fatal("expected error on missing value")
	}
	if _, _, err := parseIntegrateFlags([]string{"--client=weird"}); err == nil {
		t.Fatal("expected error on equals form invalid value")
	}
}

func TestParseIntegrateFlags_ProxyURLAndJSONAndNoHook(t *testing.T) {
	opts, extra, err := parseIntegrateFlags([]string{
		"--proxy-url", "http://localhost:9000",
		"--json",
		"--no-hook",
		"--force",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.ProxyURL != "http://localhost:9000" || !opts.Force {
		t.Fatalf("opts = %+v", opts)
	}
	if !extra.JSON || extra.InstallHook {
		t.Fatalf("extra = %+v", extra)
	}
}

func TestParseIntegrateFlags_ProxyURLEqualsAndMissingValue(t *testing.T) {
	opts, _, err := parseIntegrateFlags([]string{"--proxy-url=http://x"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.ProxyURL != "http://x" {
		t.Fatalf("url = %q", opts.ProxyURL)
	}
	if _, _, err := parseIntegrateFlags([]string{"--proxy-url"}); err == nil {
		t.Fatal("expected error on trailing --proxy-url")
	}
}

func TestParseIntegrateFlags_UnknownFlagErrors(t *testing.T) {
	if _, _, err := parseIntegrateFlags([]string{"--weird"}); err == nil {
		t.Fatal("expected error on unknown flag")
	}
}

func TestHandleIntegrateCmd_BadFlagExits1(t *testing.T) {
	origExit := exitFn
	defer func() { exitFn = origExit }()
	var code int
	exitFn = func(c int) { code = c; panic(exitSentinel{}) }
	defer func() { _ = recover() }()
	handleIntegrateCmd([]string{"status", "--weird"})
	if code != 1 {
		t.Fatalf("exit = %d", code)
	}
}

func TestHandleIntegrateCmd_EmergencyOffRunsRemove(t *testing.T) {
	home := isolateIntegrateEnv(t)
	cfgPath := prepareCodexConfig(t, home)
	// Pre-wire so emergency-off has something to strip.
	opts := integrate.Options{HomeDir: home, Client: "codex"}
	integrate.Install(opts)
	// Stub the daemon stop + uninstall to isolate from real launchctl.
	origStop := daemonStopFn
	origUninstall := daemonUninstallFn
	defer func() {
		daemonStopFn = origStop
		daemonUninstallFn = origUninstall
	}()
	daemonStopFn = func() error { return nil }
	daemonUninstallFn = func() error { return nil }

	captureIntegrate(t, func() {
		handleIntegrateCmd([]string{"emergency-off"})
	})

	content, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(content), ">>> slimference integration >>>") {
		t.Fatalf("emergency-off left fence: %s", content)
	}
}

func TestHandleIntegrateCmd_JSONRemove(t *testing.T) {
	isolateIntegrateEnv(t)
	stdout, _ := captureIntegrate(t, func() {
		handleIntegrateCmd([]string{"remove", "--json", "--no-hook"})
	})
	var rep integrate.Report
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("bad JSON: %q (err=%v)", stdout, err)
	}
}

// TestRunIntegrateInstall_HookErrorsCaptured covers the branch where hook
// installers return a non-nil error - those errors should surface in the
// Report.Errors list, not crash.
func TestRunIntegrateInstall_HookErrorsCaptured(t *testing.T) {
	home := isolateIntegrateEnv(t)
	prepareCodexConfig(t, home)
	origClaude := installClaudeHookFn
	origCodex := installCodexHookFn
	t.Cleanup(func() {
		installClaudeHookFn = origClaude
		installCodexHookFn = origCodex
	})
	installClaudeHookFn = func(string, string) error { return &errString{"claude-broken"} }
	installCodexHookFn = func(string, string) error { return &errString{"codex-broken"} }

	stdout, stderr := captureIntegrate(t, func() {
		handleIntegrateCmd([]string{"install", "--client=all"})
	})
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); err != nil {
		t.Fatal("install skipped codex config on hook error")
	}
	if strings.Contains(stderr, "claude-broken") {
		t.Fatalf("Claude hook must not run while parked: %q", stderr)
	}
	if !strings.Contains(stderr, "codex-broken") {
		t.Fatalf("stderr missing codex error: %q", stderr)
	}
	_ = stdout // stdout carries the report; errors go to stderr
}

// TestRunIntegrateInstall_JSONOutputHookErrors also surfaces errors through
// the JSON path.
func TestRunIntegrateInstall_JSONOutputHookErrors(t *testing.T) {
	isolateIntegrateEnv(t)
	origCodex := installCodexHookFn
	t.Cleanup(func() { installCodexHookFn = origCodex })
	installCodexHookFn = func(string, string) error { return &errString{"codex-fail"} }
	stdout, _ := captureIntegrate(t, func() {
		handleIntegrateCmd([]string{"install", "--client=codex", "--json"})
	})
	if !strings.Contains(stdout, "codex-fail") {
		t.Fatalf("JSON missing error: %q", stdout)
	}
}

// TestRunIntegrateRemove_JSONOutput verifies the JSON branch of remove.
func TestRunIntegrateRemove_JSONOutput(t *testing.T) {
	isolateIntegrateEnv(t)
	stdout, _ := captureIntegrate(t, func() {
		handleIntegrateCmd([]string{"remove", "--client=codex", "--json"})
	})
	var rep integrate.Report
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("bad JSON: %q", stdout)
	}
}

// TestRunIntegrateRemove_HookErrorsCaptured ensures hook removal failures
// are not fatal.
func TestRunIntegrateRemove_HookErrorsCaptured(t *testing.T) {
	isolateIntegrateEnv(t)
	origClaude := removeClaudeHookFn
	origCodex := removeCodexHookFn
	t.Cleanup(func() {
		removeClaudeHookFn = origClaude
		removeCodexHookFn = origCodex
	})
	removeClaudeHookFn = func(string) error { return &errString{"rm-claude-boom"} }
	removeCodexHookFn = func(string) error { return &errString{"rm-codex-boom"} }

	_, stderr := captureIntegrate(t, func() {
		handleIntegrateCmd([]string{"remove", "--client=all"})
	})
	if strings.Contains(stderr, "rm-claude-boom") {
		t.Fatalf("Claude hook remove must not run while parked: %q", stderr)
	}
	if !strings.Contains(stderr, "rm-codex-boom") {
		t.Fatalf("stderr missing errors: %q", stderr)
	}
}

// TestRunIntegrateEmergencyOff_DaemonStopErrorCaptured covers the daemon-
// stop + launchd-uninstall error paths.
func TestRunIntegrateEmergencyOff_DaemonStopErrorCaptured(t *testing.T) {
	isolateIntegrateEnv(t)
	origStop := daemonStopFn
	origUninstall := daemonUninstallFn
	defer func() {
		daemonStopFn = origStop
		daemonUninstallFn = origUninstall
	}()
	daemonStopFn = func() error { return &errString{"stop-failed"} }
	daemonUninstallFn = func() error { return &errString{"uninstall-failed"} }

	_, stderr := captureIntegrate(t, func() {
		handleIntegrateCmd([]string{"emergency-off"})
	})
	if !strings.Contains(stderr, "stop-failed") {
		t.Fatalf("stderr missing stop error: %q", stderr)
	}
	if !strings.Contains(stderr, "uninstall-failed") {
		t.Fatalf("stderr missing uninstall error: %q", stderr)
	}
}

func TestRunIntegrateEmergencyOff_HookErrorsCaptured(t *testing.T) {
	isolateIntegrateEnv(t)
	origClaude := removeClaudeHookFn
	origCodex := removeCodexHookFn
	removeClaudeHookFn = func(string) error { return &errString{"emergency-rm-claude"} }
	removeCodexHookFn = func(string) error { return &errString{"emergency-rm-codex"} }
	origStop := daemonStopFn
	origUninstall := daemonUninstallFn
	defer func() {
		removeClaudeHookFn = origClaude
		removeCodexHookFn = origCodex
		daemonStopFn = origStop
		daemonUninstallFn = origUninstall
	}()
	daemonStopFn = func() error { return nil }
	daemonUninstallFn = func() error { return nil }

	_, stderr := captureIntegrate(t, func() {
		handleIntegrateCmd([]string{"emergency-off"})
	})
	if strings.Contains(stderr, "emergency-rm-claude") {
		t.Fatalf("Claude hook remove must not run while parked: %q", stderr)
	}
	if !strings.Contains(stderr, "emergency-rm-codex") {
		t.Fatalf("stderr missing hook errors: %q", stderr)
	}
}

// TestRunIntegrateEmergencyOff_CallsUninstallOnSuccess ensures that when
// both daemon-stop and launchd-uninstall succeed, the emergency-off path
// actually fires both.
func TestRunIntegrateEmergencyOff_CallsUninstallOnSuccess(t *testing.T) {
	isolateIntegrateEnv(t)
	origStop := daemonStopFn
	origUninstall := daemonUninstallFn
	defer func() {
		daemonStopFn = origStop
		daemonUninstallFn = origUninstall
	}()
	var stopCalled, uninstallCalled bool
	daemonStopFn = func() error { stopCalled = true; return nil }
	daemonUninstallFn = func() error { uninstallCalled = true; return nil }

	captureIntegrate(t, func() {
		handleIntegrateCmd([]string{"emergency-off"})
	})
	if !stopCalled || !uninstallCalled {
		t.Fatalf("stopCalled=%v uninstallCalled=%v", stopCalled, uninstallCalled)
	}
}

// TestRunIntegrateEmergencyOff_JSON covers the JSON output path.
func TestRunIntegrateEmergencyOff_JSON(t *testing.T) {
	isolateIntegrateEnv(t)
	origStop := daemonStopFn
	origUninstall := daemonUninstallFn
	defer func() {
		daemonStopFn = origStop
		daemonUninstallFn = origUninstall
	}()
	daemonStopFn = func() error { return nil }
	daemonUninstallFn = func() error { return nil }

	stdout, _ := captureIntegrate(t, func() {
		handleIntegrateCmd([]string{"emergency-off", "--json"})
	})
	var rep integrate.Report
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("bad JSON: %q", stdout)
	}
}

// errString is a tiny stand-in so we do not pull errors.New into the
// compile-time graph of the test-support block.
type errString struct{ s string }

func (e *errString) Error() string { return e.s }

func TestEmitJSON_Roundtrip(t *testing.T) {
	stdout, _ := captureIntegrate(t, func() {
		emitJSON(map[string]int{"x": 1})
	})
	if !strings.Contains(stdout, `"x"`) {
		t.Fatalf("emit JSON missing key: %q", stdout)
	}
}

func TestRenderIntegrateReport_IncludesKeys(t *testing.T) {
	rep := integrate.Report{
		Claude: integrate.ClientStatus{Name: "claude", State: integrate.ClientInstalled,
			BinaryPath: "/bin/claude",
			Details:    []string{"details line"}},
		Codex: integrate.ClientStatus{Name: "codex", State: integrate.ClientFullyWired},
		Daemon: integrate.DaemonStatus{Running: true, PID: 4242, Health: "ok",
			Details: []string{"daemon detail"}},
		Writes: []integrate.WriteEvent{{Path: "/tmp/x", Action: "wrote_block"}},
		Errors: []string{"oh no"},
	}
	stdout, stderr := captureIntegrate(t, func() {
		renderIntegrateReport("Test", rep)
	})
	for _, s := range []string{"Claude Code:", "Codex:", "Daemon:", "running (pid 4242)",
		"details line", "daemon detail", "Writes:", "wrote_block"} {
		if !strings.Contains(stdout, s) {
			t.Errorf("stdout missing %q:\n%s", s, stdout)
		}
	}
	if !strings.Contains(stderr, "oh no") {
		t.Fatalf("stderr missing error: %q", stderr)
	}
}

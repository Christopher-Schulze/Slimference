package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/control"
	"github.com/slimference/slimference/internal/control/reversibility"
	"github.com/slimference/slimference/internal/install"
	"github.com/slimference/slimference/internal/proxy"
)

func newTestPrinter() (installPrinter, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	err := &bytes.Buffer{}
	return installPrinter{Out: out, Err: err}, out, err
}

func TestParseInstallFlagsAccepted(t *testing.T) {
	f, err := parseInstallFlags([]string{
		"--dry-run", "--json", "--no-hooks", "--no-autostart", "extra",
		"--with-claude", "--with-keychain", "--no-keychain", "--keep-ca", "--system", "--preflight", "--config=/tmp/x",
		"--binary=/usr/local/bin/slimference",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !f.dryRun || !f.json || !f.noHooks || !f.withClaude || !f.noAutoStart || !f.withKeychain || !f.noKeychain ||
		!f.keepCA || !f.systemScope || f.configPath != "/tmp/x" || f.binaryPath != "/usr/local/bin/slimference" ||
		len(f.rest) != 1 || f.rest[0] != "extra" {
		t.Errorf("flags not all set: %+v", f)
	}
	if !f.preflight {
		t.Errorf("preflight flag not set: %+v", f)
	}
}

func TestParseInstallFlagsRejectsUnknown(t *testing.T) {
	if _, err := parseInstallFlags([]string{"--bogus"}); err == nil {
		t.Fatal("expected error on unknown flag")
	}
}

func TestRunInstallCmdHelp(t *testing.T) {
	p, out, _ := newTestPrinter()
	rc := runInstallCmd([]string{"--help"}, p)
	if rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(out.String(), "slimference install") {
		t.Errorf("help text missing: %q", out.String())
	}
}

func TestRunInstallCmdDryRunJSON(t *testing.T) {
	home := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prev })
	xdg := filepath.Join(home, ".config")
	t.Setenv("XDG_CONFIG_HOME", xdg)

	p, out, _ := newTestPrinter()
	rc := runInstallCmd([]string{"--dry-run", "--json", "--no-keychain", "--no-autostart", "--no-hooks"}, p)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, out.String())
	}
	var got struct {
		Verb  string            `json:"verb"`
		Order []string          `json:"order"`
		State map[string]string `json:"state"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if got.Verb != "install" {
		t.Errorf("verb=%q", got.Verb)
	}
	if len(got.Order) != 1 || got.Order[0] != "ca.generate" {
		t.Errorf("order=%v", got.Order)
	}
}

func TestRunInstallCmdDryRunHuman(t *testing.T) {
	home := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prev })
	xdg := filepath.Join(home, ".config")
	t.Setenv("XDG_CONFIG_HOME", xdg)

	p, out, _ := newTestPrinter()
	rc := runInstallCmd([]string{"--dry-run", "--no-keychain", "--no-autostart", "--no-hooks"}, p)
	if rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(out.String(), "ca.generate") {
		t.Errorf("dry-run output missing step: %q", out.String())
	}
}

func TestRunInstallCmdPassesBinaryOverride(t *testing.T) {
	prevPlan := installPlanFn
	t.Cleanup(func() { installPlanFn = prevPlan })

	var got install.Options
	installPlanFn = func(opts install.Options) (*reversibility.Plan, error) {
		got = opts
		return reversibility.NewPlan(installCmdFakeStep{name: "ok"}), nil
	}

	p, _, errBuf := newTestPrinter()
	rc := runInstallCmd([]string{"--dry-run", "--binary=/stable/slimference"}, p)
	if rc != 0 {
		t.Fatalf("rc=%d err=%s", rc, errBuf.String())
	}
	if got.BinaryPath != "/stable/slimference" {
		t.Fatalf("BinaryPath=%q want /stable/slimference", got.BinaryPath)
	}
}

func TestRunInstallCmdDefaultSkipsKeychain(t *testing.T) {
	home := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prev })
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	p, out, errBuf := newTestPrinter()
	rc := runInstallCmd([]string{"--dry-run", "--json", "--no-autostart", "--no-hooks"}, p)
	if rc != 0 {
		t.Fatalf("rc=%d out=%s err=%s", rc, out.String(), errBuf.String())
	}
	var got struct {
		Order []string `json:"order"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	for _, name := range got.Order {
		if name == "ca.keychain" {
			t.Fatalf("default install must not include Keychain trust: %v", got.Order)
		}
	}
}

func TestRunInstallCmdWithKeychainOptIn(t *testing.T) {
	home := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prev })
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	p, out, errBuf := newTestPrinter()
	rc := runInstallCmd([]string{"--dry-run", "--json", "--with-keychain", "--no-autostart", "--no-hooks"}, p)
	if rc != 0 {
		t.Fatalf("rc=%d out=%s err=%s", rc, out.String(), errBuf.String())
	}
	var got struct {
		Order []string `json:"order"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	want := []string{"ca.generate", "ca.keychain"}
	if len(got.Order) != len(want) {
		t.Fatalf("order=%v want %v", got.Order, want)
	}
	for i := range want {
		if got.Order[i] != want[i] {
			t.Fatalf("order=%v want %v", got.Order, want)
		}
	}
}

func TestRunInstallCmdApplyMinimalPlan(t *testing.T) {
	home := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prev })
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	p, out, errBuf := newTestPrinter()
	rc := runInstallCmd([]string{"--no-keychain", "--no-autostart", "--no-hooks"}, p)
	if rc != 0 {
		t.Fatalf("rc=%d stdout=%s stderr=%s", rc, out.String(), errBuf.String())
	}
	if !strings.Contains(out.String(), "Install complete") {
		t.Fatalf("install output missing completion: %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".slimference", "ca", "root.crt")); err != nil {
		t.Fatalf("CA cert not generated: %v", err)
	}
}

func TestRunInstallCmdSystemScopeAndApplyFailure(t *testing.T) {
	p, out, errBuf := newTestPrinter()
	if rc := runInstallCmd([]string{"--dry-run", "--json", "--system", "--no-keychain", "--no-autostart", "--no-hooks"}, p); rc != 0 {
		t.Fatalf("system dry-run rc=%d out=%s err=%s", rc, out.String(), errBuf.String())
	}

	homeFile := filepath.Join(t.TempDir(), "home-file")
	if err := os.WriteFile(homeFile, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("write home file: %v", err)
	}
	t.Setenv("HOME", homeFile)
	out.Reset()
	errBuf.Reset()
	if rc := runInstallCmd([]string{"--no-keychain", "--no-autostart", "--no-hooks"}, p); rc != 3 {
		t.Fatalf("apply failure rc=%d out=%s err=%s", rc, out.String(), errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "Install failed") {
		t.Fatalf("missing rollback hint: %q", errBuf.String())
	}
}

func TestRunInstallAndUninstallPlanErrors(t *testing.T) {
	prevPlan := installPlanFn
	t.Cleanup(func() { installPlanFn = prevPlan })
	installPlanFn = func(install.Options) (*reversibility.Plan, error) {
		return nil, errors.New("plan boom")
	}

	p, _, errBuf := newTestPrinter()
	if rc := runInstallCmd(nil, p); rc != 1 {
		t.Fatalf("install plan error rc=%d", rc)
	}
	if !strings.Contains(errBuf.String(), "plan boom") {
		t.Fatalf("install stderr missing plan error: %q", errBuf.String())
	}

	errBuf.Reset()
	if rc := runUninstallCmd(nil, p); rc != 1 {
		t.Fatalf("uninstall plan error rc=%d", rc)
	}
	if !strings.Contains(errBuf.String(), "plan boom") {
		t.Fatalf("uninstall stderr missing plan error: %q", errBuf.String())
	}
}

func TestHandleInstallCmdExitsWithRunCode(t *testing.T) {
	code, exited := captureExit(func() { handleInstallCmd([]string{"--bogus"}) })
	if !exited || code != 2 {
		t.Fatalf("exit=(%d,%v), want (2,true)", code, exited)
	}
}

func TestRunUninstallCmdHelp(t *testing.T) {
	p, out, _ := newTestPrinter()
	if rc := runUninstallCmd([]string{"--help"}, p); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(out.String(), "slimference uninstall") {
		t.Error("uninstall help missing")
	}
}

func TestRunUninstallCmdDryRun(t *testing.T) {
	home := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prev })

	p, _, _ := newTestPrinter()
	if rc := runUninstallCmd([]string{"--dry-run", "--no-keychain", "--no-autostart", "--no-hooks"}, p); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
}

func TestRunUninstallCmdPassesBinaryOverride(t *testing.T) {
	prevPlan := installPlanFn
	t.Cleanup(func() { installPlanFn = prevPlan })

	var got install.Options
	installPlanFn = func(opts install.Options) (*reversibility.Plan, error) {
		got = opts
		return reversibility.NewPlan(installCmdFakeStep{name: "ok"}), nil
	}

	p, _, errBuf := newTestPrinter()
	rc := runUninstallCmd([]string{"--dry-run", "--binary=/stable/slimference"}, p)
	if rc != 0 {
		t.Fatalf("rc=%d err=%s", rc, errBuf.String())
	}
	if got.BinaryPath != "/stable/slimference" {
		t.Fatalf("BinaryPath=%q want /stable/slimference", got.BinaryPath)
	}
}

func TestRunUninstallCmdReverseMinimalPlan(t *testing.T) {
	home := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prev })
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	p, out, errBuf := newTestPrinter()
	if rc := runInstallCmd([]string{"--no-keychain", "--no-autostart", "--no-hooks"}, p); rc != 0 {
		t.Fatalf("install rc=%d stdout=%s stderr=%s", rc, out.String(), errBuf.String())
	}
	out.Reset()
	errBuf.Reset()
	if rc := runUninstallCmd([]string{"--no-keychain", "--no-autostart", "--no-hooks"}, p); rc != 0 {
		t.Fatalf("uninstall rc=%d stdout=%s stderr=%s", rc, out.String(), errBuf.String())
	}
	if !strings.Contains(out.String(), "Uninstall complete") {
		t.Fatalf("uninstall output missing completion: %q", out.String())
	}
}

type installCmdFakeStep struct {
	name       string
	applyFn    func(context.Context) error
	applyErr   error
	reverseFn  func(context.Context) error
	reverseErr error
}

func (s installCmdFakeStep) Name() string { return s.name }

func (s installCmdFakeStep) Apply(ctx context.Context) error {
	if s.applyFn != nil {
		return s.applyFn(ctx)
	}
	return s.applyErr
}

func (s installCmdFakeStep) Reverse(ctx context.Context) error {
	if s.reverseFn != nil {
		return s.reverseFn(ctx)
	}
	return s.reverseErr
}

func (s installCmdFakeStep) Inspect(context.Context) reversibility.StepState {
	return reversibility.StatePresent
}

func TestRunUninstallCmdReverseError(t *testing.T) {
	prevPlan := installPlanFn
	t.Cleanup(func() { installPlanFn = prevPlan })
	installPlanFn = func(install.Options) (*reversibility.Plan, error) {
		return reversibility.NewPlan(installCmdFakeStep{name: "bad", reverseErr: errors.New("reverse boom")}), nil
	}

	p, _, errBuf := newTestPrinter()
	if rc := runUninstallCmd(nil, p); rc != 3 {
		t.Fatalf("reverse error rc=%d", rc)
	}
	if !strings.Contains(errBuf.String(), "reverse boom") {
		t.Fatalf("stderr missing reverse error: %q", errBuf.String())
	}
}

func TestRunUninstallCmdSystemScope(t *testing.T) {
	p, _, errBuf := newTestPrinter()
	if rc := runUninstallCmd([]string{"--dry-run", "--system", "--no-keychain", "--no-autostart", "--no-hooks"}, p); rc != 0 {
		t.Fatalf("rc=%d err=%s", rc, errBuf.String())
	}
}

func TestHandleUninstallCmdExitsWithRunCode(t *testing.T) {
	code, exited := captureExit(func() { handleUninstallCmd([]string{"--bogus"}) })
	if !exited || code != 2 {
		t.Fatalf("exit=(%d,%v), want (2,true)", code, exited)
	}
}

// TestRunUninstallCmdHonoursSkipFlags is a regression test for the
// bug where uninstall ignored --no-autostart and --no-hooks, causing
// Reverse to attempt to clean up Steps the user had skipped on install.
func TestRunUninstallCmdHonoursSkipFlags(t *testing.T) {
	home := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prev })
	xdg := filepath.Join(home, ".config")
	t.Setenv("XDG_CONFIG_HOME", xdg)

	p, out, _ := newTestPrinter()
	rc := runUninstallCmd([]string{
		"--dry-run", "--json",
		"--no-keychain", "--no-autostart", "--no-hooks",
	}, p)
	if rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	var got struct {
		Verb  string   `json:"verb"`
		Order []string `json:"order"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// With all skips, only ca.generate should remain.
	if len(got.Order) != 1 || got.Order[0] != "ca.generate" {
		t.Errorf("order=%v want [ca.generate]", got.Order)
	}
}

func TestRunEnableWritesScopedCodexRoute(t *testing.T) {
	withCodexCmdStubs(t)
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfgPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.WriteFile(cfgPath, []byte("model = \"gpt-5\"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	codexRouteHomeFn = func() (string, error) { return home, nil }

	p, out, _ := newTestPrinter()
	rc := runEnableCmd(nil, p)
	if rc != 0 {
		t.Fatalf("rc=%d out=%s", rc, out.String())
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if !strings.Contains(string(data), "slimference-codex") ||
		!strings.Contains(string(data), "base_url = \"http://127.0.0.1:8990/backend-api/codex\"") {
		t.Errorf("config wrong: %q", string(data))
	}
	if !strings.Contains(out.String(), "Codex route enabled") ||
		!strings.Contains(out.String(), "Browser ChatGPT and ChatGPT.app stay direct") {
		t.Errorf("output missing scoped route: %q", out.String())
	}
}

func TestRunEnableHelpAndUnknownFlag(t *testing.T) {
	p, out, errBuf := newTestPrinter()
	if rc := runEnableCmd([]string{"--help"}, p); rc != 0 {
		t.Fatalf("help rc=%d", rc)
	}
	if !strings.Contains(out.String(), "slimference codex enable") {
		t.Fatalf("help output mismatch: %q", out.String())
	}
	if rc := runEnableCmd([]string{"--bogus"}, p); rc != 2 {
		t.Fatalf("unknown flag rc=%d want 2", rc)
	}
	if !strings.Contains(errBuf.String(), "unknown flag") {
		t.Fatalf("unknown flag error missing: %q", errBuf.String())
	}
}

func TestRunLabEnablePatchErrorAndSignalSentBranch(t *testing.T) {
	p, _, errBuf := newTestPrinter()
	if rc := runLabEnableCmd([]string{"--config=" + t.TempDir()}, p); rc != 1 {
		t.Fatalf("directory config should fail rc=%d", rc)
	}
	if !strings.Contains(errBuf.String(), "read:") {
		t.Fatalf("missing patch error: %q", errBuf.String())
	}

	prevSignal := signalPIDFn
	var signaledPID int
	signalPIDFn = func(pid int, sig os.Signal) error {
		signaledPID = pid
		return nil
	}
	t.Cleanup(func() { signalPIDFn = prevSignal })

	home := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prev })
	runDir := filepath.Join(home, ".slimference", "run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "daemon.pid"), []byte(strconv.Itoa(4242)), 0o600); err != nil {
		t.Fatalf("write pid: %v", err)
	}
	p, out, errBuf := newTestPrinter()
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if rc := runLabEnableCmd([]string{"--config=" + cfg}, p); rc != 0 {
		t.Fatalf("enable rc=%d out=%s err=%s", rc, out.String(), errBuf.String())
	}
	if !strings.Contains(out.String(), "Daemon SIGHUP sent") {
		t.Fatalf("missing sent branch: %q", out.String())
	}
	if signaledPID != 4242 {
		t.Fatalf("signaled pid=%d, want 4242", signaledPID)
	}
}

func TestRunLabEnableCannotResolveConfigPath(t *testing.T) {
	prev := enableDisableConfigPathFn
	enableDisableConfigPathFn = func() string { return "" }
	t.Cleanup(func() { enableDisableConfigPathFn = prev })

	p, _, errBuf := newTestPrinter()
	if rc := runLabEnableCmd(nil, p); rc != 1 {
		t.Fatalf("empty config path rc=%d", rc)
	}
	if !strings.Contains(errBuf.String(), "cannot resolve config path") {
		t.Fatalf("missing config path error: %q", errBuf.String())
	}
}

func TestRunLabEnableReportsSignalError(t *testing.T) {
	prevSignal := signalPIDFn
	signalPIDFn = func(int, os.Signal) error { return errors.New("signal denied") }
	t.Cleanup(func() { signalPIDFn = prevSignal })

	home := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prev })
	runDir := filepath.Join(home, ".slimference", "run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "daemon.pid"), []byte("777"), 0o600); err != nil {
		t.Fatalf("write pid: %v", err)
	}

	p, out, errBuf := newTestPrinter()
	if rc := runLabEnableCmd([]string{"--config=" + filepath.Join(t.TempDir(), "config.toml")}, p); rc != 0 {
		t.Fatalf("enable rc=%d out=%s err=%s", rc, out.String(), errBuf.String())
	}
	if !strings.Contains(out.String(), "signal denied") {
		t.Fatalf("signal error branch missing: %q", out.String())
	}
}

func TestRunLabCmdHelpAndEnableHelp(t *testing.T) {
	p, out, errBuf := newTestPrinter()
	if rc := runLabCmd([]string{"--help"}, p); rc != 0 {
		t.Fatalf("lab help rc=%d", rc)
	}
	if !strings.Contains(out.String(), "slimference lab") {
		t.Fatalf("lab help missing: %q", out.String())
	}
	out.Reset()
	if rc := runLabCmd([]string{"enable", "--help"}, p); rc != 0 {
		t.Fatalf("lab enable help rc=%d err=%s", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "slimference lab enable | disable") {
		t.Fatalf("lab enable help missing: %q", out.String())
	}
	out.Reset()
	errBuf.Reset()
	if rc := runLabCmd([]string{"wat"}, p); rc != 2 {
		t.Fatalf("unknown lab rc=%d", rc)
	}
	if !strings.Contains(errBuf.String(), "unknown subcommand") {
		t.Fatalf("unknown lab error missing: %q", errBuf.String())
	}
}

func TestRunLabCmdDelegatesGlobalLabSubcommandHelp(t *testing.T) {
	for _, args := range [][]string{
		{"cert-trust", "--help"},
		{"root-arm", "--help"},
		{"root-disarm", "--help"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			if rc := runLabCmd(args, defaultInstallPrinter()); rc != 0 {
				t.Fatalf("runLabCmd(%v) rc=%d", args, rc)
			}
		})
	}
}

func TestHandleEnableDisableCmdExitCodes(t *testing.T) {
	code, exited := captureExit(func() { handleEnableCmd([]string{"--bogus"}) })
	if !exited || code != 2 {
		t.Fatalf("enable exit=(%d,%v), want (2,true)", code, exited)
	}
	code, exited = captureExit(func() { handleDisableCmd([]string{"--bogus"}) })
	if !exited || code != 2 {
		t.Fatalf("disable exit=(%d,%v), want (2,true)", code, exited)
	}
}

func TestRunDisableWritesConfig(t *testing.T) {
	home := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prev })
	xdg := filepath.Join(home, ".config")
	t.Setenv("XDG_CONFIG_HOME", xdg)

	// Pre-enable first
	if rc := runLabEnableCmd(nil, installPrinter{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}); rc != 0 {
		t.Fatalf("pre-enable: rc=%d", rc)
	}

	p, out, _ := newTestPrinter()
	rc := runLabDisableCmd(nil, p)
	if rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	data, _ := os.ReadFile(filepath.Join(xdg, "slimference", "config.toml"))
	if !strings.Contains(string(data), "sni_peek_mode = false") {
		t.Errorf("config not disabled: %q", string(data))
	}
	if !strings.Contains(out.String(), "DISABLED") {
		t.Errorf("output missing DISABLED: %q", out.String())
	}
}

func TestRunEnableDisableRoundTrip(t *testing.T) {
	home := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prev })
	xdg := filepath.Join(home, ".config")
	t.Setenv("XDG_CONFIG_HOME", xdg)

	pNull := installPrinter{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	if rc := runLabEnableCmd(nil, pNull); rc != 0 {
		t.Fatalf("enable rc=%d", rc)
	}
	if rc := runLabDisableCmd(nil, pNull); rc != 0 {
		t.Fatalf("disable rc=%d", rc)
	}
	data, _ := os.ReadFile(filepath.Join(xdg, "slimference", "config.toml"))
	if !strings.Contains(string(data), "sni_peek_mode = false") {
		t.Errorf("final state wrong: %q", string(data))
	}
}

func TestRunStatusCmdNoDaemonExitsNonZero(t *testing.T) {
	home := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prev })
	cfgPath := filepath.Join(home, "config.toml")
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)

	// Point config at a closed port.
	_ = os.WriteFile(cfgPath, []byte("[proxy]\nlisten_port = 1\n"), 0o600)

	p, _, errBuf := newTestPrinter()
	rc := runStatusCmd(nil, p)
	if rc != 1 {
		t.Fatalf("rc=%d want 1", rc)
	}
	if !strings.Contains(errBuf.String(), "daemon not running") {
		t.Errorf("err missing hint: %q", errBuf.String())
	}
}

func TestRunStatusCmdHelp(t *testing.T) {
	p, out, _ := newTestPrinter()
	if rc := runStatusCmd([]string{"--help"}, p); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(out.String(), "status") {
		t.Error("status help missing")
	}
}

func TestRunStatusCmdJSONFromDaemon(t *testing.T) {
	state := control.SetupState{
		CA:     control.CAState{Installed: true, InKeychain: true, Fingerprint: "fp"},
		Daemon: control.DaemonState{Running: true, PID: 99, HealthOK: true},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != proxy.AdminStatePath {
			t.Fatalf("path=%q want %q", r.URL.Path, proxy.AdminStatePath)
		}
		_ = json.NewEncoder(w).Encode(state)
	}))
	defer srv.Close()
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("split hostport: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)
	if err := os.WriteFile(cfgPath, []byte("[proxy]\nlisten_port = "+strconv.Itoa(port)+"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	p, out, errBuf := newTestPrinter()
	if rc := runStatusCmd([]string{"--json"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errBuf.String())
	}
	var got control.SetupState
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode status JSON: %v\n%s", err, out.String())
	}
	if got.Daemon.PID != 99 || got.CA.Fingerprint != "fp" {
		t.Fatalf("unexpected state: %+v", got)
	}
}

func TestRunStatusCmdHumanFromDaemon(t *testing.T) {
	state := control.SetupState{
		CA:     control.CAState{Installed: true, InKeychain: true, Fingerprint: "fp"},
		Daemon: control.DaemonState{Running: true, PID: 100, HealthOK: true},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(state)
	}))
	defer srv.Close()
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("split hostport: %v", err)
	}
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)
	if err := os.WriteFile(cfgPath, []byte("[proxy]\nlisten_port = "+portText+"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	p, out, errBuf := newTestPrinter()
	if rc := runStatusCmd(nil, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "Slimference status") {
		t.Fatalf("human status missing: %q", out.String())
	}
}

func TestRunStatusCmdPreflightFromDaemon(t *testing.T) {
	prevPreflight := preflightUpstreamResolutionFn
	preflightUpstreamResolutionFn = func(_ context.Context, hosts []string) []proxy.UpstreamResolutionCheck {
		if len(hosts) != 2 || hosts[0] != "chatgpt.com" || hosts[1] != "api.openai.com" {
			t.Fatalf("hosts=%v", hosts)
		}
		return []proxy.UpstreamResolutionCheck{
			{Host: "chatgpt.com", OK: true, IP: "203.0.113.10"},
			{Host: "api.openai.com", OK: false, Error: "doh blocked"},
		}
	}
	t.Cleanup(func() { preflightUpstreamResolutionFn = prevPreflight })

	state := control.SetupState{
		CA:     control.CAState{Installed: true, InKeychain: true, Fingerprint: "fp"},
		Daemon: control.DaemonState{Running: true, PID: 100, HealthOK: true},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(state)
	}))
	defer srv.Close()
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("split hostport: %v", err)
	}
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)
	if err := os.WriteFile(cfgPath, []byte("[proxy]\nlisten_port = "+portText+"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	p, out, errBuf := newTestPrinter()
	if rc := runStatusCmd([]string{"--preflight"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errBuf.String())
	}
	text := out.String()
	if !strings.Contains(text, "Preflight") ||
		!strings.Contains(text, "chatgpt.com") ||
		!strings.Contains(text, "doh blocked") ||
		!strings.Contains(text, "do not live-arm") {
		t.Fatalf("preflight output missing detail: %q", text)
	}
}

func TestHandleStatusCmdExitsWithRunCode(t *testing.T) {
	code, exited := captureExit(func() { handleStatusCmd([]string{"--bogus"}) })
	if !exited || code != 2 {
		t.Fatalf("status exit=(%d,%v), want (2,true)", code, exited)
	}
}

func TestRenderApplyAndReverseResultsIncludeErrors(t *testing.T) {
	p, out, errBuf := newTestPrinter()
	renderApplyResult(p, reversibility.ApplyResult{
		Applied:    []string{"one"},
		RolledBack: []string{"one"},
		Err:        os.ErrPermission,
	})
	if !strings.Contains(out.String(), "one") || !strings.Contains(out.String(), "rolled back") {
		t.Fatalf("apply output missing step detail: %q", out.String())
	}
	if !strings.Contains(errBuf.String(), os.ErrPermission.Error()) {
		t.Fatalf("apply stderr missing error: %q", errBuf.String())
	}

	out.Reset()
	errBuf.Reset()
	renderReverseResult(p, reversibility.ReverseResult{
		Reversed: []string{"one"},
		Errors:   []error{os.ErrPermission},
	})
	if !strings.Contains(out.String(), "one reverted") {
		t.Fatalf("reverse output missing step detail: %q", out.String())
	}
	if !strings.Contains(errBuf.String(), os.ErrPermission.Error()) {
		t.Fatalf("reverse stderr missing error: %q", errBuf.String())
	}
}

func TestRenderStatusHuman(t *testing.T) {
	p, out, _ := newTestPrinter()
	state := control.SetupState{
		CA:     control.CAState{Installed: true, InKeychain: true, Fingerprint: "abc"},
		Daemon: control.DaemonState{Running: true, PID: 4242, HealthOK: true},
	}
	renderStatus(p, state)
	if !strings.Contains(out.String(), "DISARMED") {
		t.Errorf("expected DISARMED hint: %q", out.String())
	}
	if !strings.Contains(out.String(), "pid=4242") {
		t.Errorf("PID missing: %q", out.String())
	}
}

func TestRenderStatusCAMaterialIsNotKeychainGated(t *testing.T) {
	p, out, _ := newTestPrinter()
	renderStatus(p, control.SetupState{
		CA: control.CAState{Installed: true, InKeychain: false, Fingerprint: "abc"},
	})
	text := out.String()
	if !strings.Contains(text, "CA       ✓ installed=true in_keychain=false") {
		t.Fatalf("CA material should be green without Keychain trust: %q", text)
	}
}

func TestRenderStatusIncludesApps(t *testing.T) {
	p, out, _ := newTestPrinter()
	renderStatus(p, control.SetupState{
		Apps: []control.AppEntry{{
			ID:       "codex_cli",
			Enabled:  true,
			Detected: true,
			Routed:   3,
			Bypassed: 1,
		}},
	})
	s := out.String()
	if !strings.Contains(s, "codex_cli") || !strings.Contains(s, "routed=3") || !strings.Contains(s, "bypassed=1") {
		t.Fatalf("apps row missing: %q", s)
	}
}

func TestRenderStatusArmed(t *testing.T) {
	p, out, _ := newTestPrinter()
	state := control.SetupState{
		Listener:     control.ListenerState{BoundOnSNIPeek: true},
		NetworkRedir: control.NetworkState{HostsActive: true, HostsEntries: []string{"a"}},
	}
	renderStatus(p, state)
	if !strings.Contains(out.String(), "ARMED") {
		t.Errorf("expected ARMED: %q", out.String())
	}
}

func TestRenderStatusRoutingActiveWithoutSNIWarns(t *testing.T) {
	p, out, _ := newTestPrinter()
	state := control.SetupState{
		NetworkRedir: control.NetworkState{HostsActive: true, HostsEntries: []string{"a"}},
	}
	renderStatus(p, state)
	if !strings.Contains(out.String(), "ROUTING ACTIVE") ||
		!strings.Contains(out.String(), "root-disarm") {
		t.Errorf("expected recovery warning: %q", out.String())
	}
}

func TestRenderStatusPreflightBlock(t *testing.T) {
	p, out, _ := newTestPrinter()
	renderStatus(p, control.SetupState{
		Preflight: control.PreflightState{DoH: []control.DoHPreflightEntry{{
			Host: "chatgpt.com",
			OK:   true,
			IP:   "203.0.113.10",
		}}},
	})
	if !strings.Contains(out.String(), "Preflight") ||
		!strings.Contains(out.String(), "203.0.113.10") {
		t.Fatalf("preflight block missing: %q", out.String())
	}
}

func TestEnableDisableConfigPathDefaultsToXDG(t *testing.T) {
	xdg := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	want := filepath.Join(xdg, "slimference", "config.toml")
	if got := enableDisableConfigPath(); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestEnableDisableConfigPathEnvWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.toml")
	t.Setenv("SLIMFERENCE_CONFIG", path)
	if got := enableDisableConfigPath(); got != path {
		t.Fatalf("got %q want env path", got)
	}
}

// Compile-time sanity: ensure we can construct a context with timeout
// the way fetchSetupState does, without dragging unnecessary imports.
func TestContextWithTimeoutSmoke(t *testing.T) {
	_, cancel := context.WithTimeout(context.Background(), 10)
	cancel()
}

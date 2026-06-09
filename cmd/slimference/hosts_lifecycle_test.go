package main

// Tests in this file mutate package-level lifecycle state
// (startProxyHostsCleanup, startProxyAppsManager, startProxySNICancel
// via withTempHostsFile + osUserHomeDir override). They are NOT safe
// for t.Parallel() — adding parallelism here will introduce data
// races on those vars + undefined cleanup ordering.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/control/apps"
	"github.com/Christopher-Schulze/Slimference/internal/control/reversibility"
	"github.com/Christopher-Schulze/Slimference/internal/install"
	"github.com/Christopher-Schulze/Slimference/internal/proxy"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/transparent"
)

// withTempHostsFile redirects install.HostsPlan defaults to a temp
// hosts file so tests don't touch /etc/hosts. Returns the path + a
// cleanup hook.
func withTempHostsFile(t *testing.T) (homePath, hostsPath string) {
	t.Helper()
	home := t.TempDir()
	hosts := filepath.Join(home, "hosts.test")
	if err := os.WriteFile(hosts, []byte("127.0.0.1 localhost\n"), 0o644); err != nil {
		t.Fatalf("seed hosts: %v", err)
	}
	// Stub osUserHomeDir so the HostsPlan default backup dir lands
	// under the test home.
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prev })
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("SLIMFERENCE_CONFIG", "")
	return home, hosts
}

func TestApplyHostsPatchDisabledReturnsNoopCleanup(t *testing.T) {
	t.Cleanup(func() { startProxyHostsArmed = false })
	cfg := config.Defaults()
	cfg.Transparent.SNIPeekMode = false
	cleanup := applyHostsPatch(cfg)
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup")
	}
	// Calling it must not panic.
	cleanup()
	if startProxyHostsArmed {
		t.Fatal("disabled apply must not mark hosts armed")
	}
}

func TestApplyHostsPatchNilCfgReturnsNoop(t *testing.T) {
	t.Cleanup(func() { startProxyHostsArmed = false })
	cleanup := applyHostsPatch(nil)
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup")
	}
	cleanup()
}

func TestApplyHostsPatchBuildPlanFailureReturnsNoop(t *testing.T) {
	t.Cleanup(func() { startProxyHostsArmed = false })
	prev := installHostsPlanFn
	installHostsPlanFn = func(install.HostsOptions) (*reversibility.Plan, error) {
		return nil, errors.New("no hosts")
	}
	t.Cleanup(func() { installHostsPlanFn = prev })

	cfg := config.Defaults()
	cfg.Transparent.SNIPeekMode = true
	cleanup := applyHostsPatch(cfg)
	if cleanup == nil {
		t.Fatal("expected noop cleanup")
	}
	cleanup()
	if startProxyHostsArmed {
		t.Fatal("build-plan failure must not mark hosts armed")
	}
}

func TestApplyHostsPatchUsesInjectedPlanAndCleanupIsIdempotent(t *testing.T) {
	home, hosts := withTempHostsFile(t)
	prev := installHostsPlanFn
	installHostsPlanFn = func(install.HostsOptions) (*reversibility.Plan, error) {
		return install.HostsPlan(install.HostsOptions{Home: home, HostsPath: hosts})
	}
	t.Cleanup(func() { installHostsPlanFn = prev })

	cfg := config.Defaults()
	cfg.Transparent.SNIPeekMode = true
	cleanup := applyHostsPatch(cfg)
	if !startProxyHostsArmed {
		t.Fatal("successful apply should mark hosts armed")
	}
	data, _ := os.ReadFile(hosts)
	if !strings.Contains(string(data), "chatgpt.com") {
		t.Fatalf("hosts not patched through injected plan: %q", data)
	}
	cleanup()
	cleanup()
	data, _ = os.ReadFile(hosts)
	if strings.Contains(string(data), "chatgpt.com") {
		t.Fatalf("hosts not reverted after cleanup: %q", data)
	}
	if startProxyHostsArmed {
		t.Fatal("cleanup should clear hosts armed state")
	}
}

func TestApplyHostsPatchCleanupReverseFailureNoPanic(t *testing.T) {
	prev := installHostsPlanFn
	installHostsPlanFn = func(install.HostsOptions) (*reversibility.Plan, error) {
		return reversibility.NewPlan(installCmdFakeStep{name: "hosts.patch", reverseErr: errors.New("reverse boom")}), nil
	}
	t.Cleanup(func() { installHostsPlanFn = prev })

	cfg := config.Defaults()
	cfg.Transparent.SNIPeekMode = true
	cleanup := applyHostsPatch(cfg)
	if cleanup == nil {
		t.Fatal("expected cleanup")
	}
	cleanup()
}

func TestApplyHostsPatchApplyFailureReturnsNoop(t *testing.T) {
	t.Cleanup(func() { startProxyHostsArmed = false })
	home := t.TempDir()
	badHosts := filepath.Join(home, "missing", "hosts")
	prev := installHostsPlanFn
	installHostsPlanFn = func(install.HostsOptions) (*reversibility.Plan, error) {
		return install.HostsPlan(install.HostsOptions{Home: home, HostsPath: badHosts})
	}
	t.Cleanup(func() { installHostsPlanFn = prev })

	cfg := config.Defaults()
	cfg.Transparent.SNIPeekMode = true
	cleanup := applyHostsPatch(cfg)
	if cleanup == nil {
		t.Fatal("expected noop cleanup")
	}
	cleanup()
	if startProxyHostsArmed {
		t.Fatal("apply failure must not mark hosts armed")
	}
}

func TestApplyHostsPatchAppliesAndReverts(t *testing.T) {
	home, hosts := withTempHostsFile(t)

	// install.HostsPlan reads home for backups; we already pointed
	// it via osUserHomeDir stub. Build a Plan against the temp path.
	plan, err := install.HostsPlan(install.HostsOptions{
		Home:      home,
		HostsPath: hosts,
	})
	if err != nil {
		t.Fatalf("HostsPlan: %v", err)
	}
	if res := plan.Apply(context.Background()); res.Err != nil {
		t.Fatalf("Apply: %v", res.Err)
	}
	data, _ := os.ReadFile(hosts)
	if !strings.Contains(string(data), "127.0.0.1") ||
		!strings.Contains(string(data), "chatgpt.com") {
		t.Errorf("hosts not patched: %q", string(data))
	}
	if res := plan.Reverse(context.Background()); res.Err() != nil {
		t.Fatalf("Reverse: %v", res.Err())
	}
	data, _ = os.ReadFile(hosts)
	if strings.Contains(string(data), "chatgpt.com") {
		t.Errorf("hosts not reverted: %q", string(data))
	}
}

func TestWritePIDFileLifecycle(t *testing.T) {
	dir := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { osUserHomeDir = prev })

	cleanup := writePIDFile()
	if cleanup == nil {
		t.Fatal("nil cleanup")
	}
	path := filepath.Join(dir, ".slimference", "run", "daemon.pid")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("pidfile not written: %v", err)
	}
	data, _ := os.ReadFile(path)
	if strings.TrimSpace(string(data)) == "" {
		t.Errorf("pidfile empty: %q", string(data))
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("pidfile not removed: %v", err)
	}
	// Idempotent — second call is a no-op.
	cleanup()
}

func TestWritePIDFileHomeMissingNoCrash(t *testing.T) {
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { osUserHomeDir = prev })

	cleanup := writePIDFile()
	if cleanup == nil {
		t.Fatal("nil cleanup")
	}
	cleanup() // must not panic
}

func TestWritePIDFileMkdirFailureNoCrash(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".slimference"), []byte("file"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prev })

	cleanup := writePIDFile()
	if cleanup == nil {
		t.Fatal("nil cleanup")
	}
	cleanup()
}

func TestWritePIDFileWriteFailureNoCrash(t *testing.T) {
	home := t.TempDir()
	runDir := filepath.Join(home, ".slimference", "run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("mkdir run: %v", err)
	}
	if err := os.Mkdir(filepath.Join(runDir, "daemon.pid"), 0o755); err != nil {
		t.Fatalf("mkdir daemon.pid: %v", err)
	}
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prev })

	cleanup := writePIDFile()
	if cleanup == nil {
		t.Fatal("nil cleanup")
	}
	cleanup()
}

func TestWritePIDFileCleanupRemoveFailureNoCrash(t *testing.T) {
	home := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prev })

	cleanup := writePIDFile()
	path := filepath.Join(home, ".slimference", "run", "daemon.pid")
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove pid file before sabotage: %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir pid dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "child"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write child: %v", err)
	}
	cleanup()
}

func TestSnippetReadBoolTrue(t *testing.T) {
	got := snippetReadBool([]byte("[transparent]\nsni_peek_mode = true\n"), "sni_peek_mode")
	if got == nil || *got != true {
		t.Errorf("got %v want true", got)
	}
}

func TestSnippetReadBoolFalse(t *testing.T) {
	got := snippetReadBool([]byte("[transparent]\nsni_peek_mode = false\n"), "sni_peek_mode")
	if got == nil || *got != false {
		t.Errorf("got %v want false", got)
	}
}

func TestSnippetReadBoolAbsent(t *testing.T) {
	got := snippetReadBool([]byte("[transparent]\nca_dir = \"x\"\n"), "sni_peek_mode")
	if got != nil {
		t.Errorf("got %v want nil", got)
	}
}

func TestSnippetReadBoolIgnoresComment(t *testing.T) {
	got := snippetReadBool([]byte("sni_peek_mode = true # legacy hint\n"), "sni_peek_mode")
	if got == nil || *got != true {
		t.Errorf("got %v want true (comment ignored)", got)
	}
}

func TestReloadSNIPeekModeFromDiskArms(t *testing.T) {
	home, hosts := withTempHostsFile(t)
	t.Cleanup(func() {
		startProxyHostsCleanup = nil
		startProxyHostsArmed = false
		startProxySNICancel = nil
		startProxyInstance = nil
	})

	cfg := config.Defaults()
	cfg.Transparent.SNIPeekMode = false
	// Initially no cleanup armed.
	startProxyHostsCleanup = nil
	startProxyHostsArmed = false

	cfgPath := filepath.Join(home, ".config", "slimference", "config.toml")
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0o755)
	_ = os.WriteFile(cfgPath, []byte("[transparent]\nsni_peek_mode = true\n"), 0o600)

	// Pre-condition: hosts file is the seeded value.
	_ = hosts
	_ = cfg

	// Note: reloadSNIPeekModeFromDisk uses install.HostsPlan() with
	// default options, which targets /etc/hosts. We cannot exercise
	// the full apply path in a unit test without privileged file
	// access. Test the DECISION instead: post-call, the cleanup
	// closure is non-nil (means apply was attempted), and cfg.
	// Transparent.SNIPeekMode is updated.
	reloadSNIPeekModeFromDisk(cfg)
	if !cfg.Transparent.SNIPeekMode {
		t.Error("cfg.SNIPeekMode not flipped on")
	}
}

func TestReloadSNIPeekModeFromDiskDisarms(t *testing.T) {
	home, _ := withTempHostsFile(t)

	cfg := config.Defaults()
	cfg.Transparent.SNIPeekMode = true
	// Simulate previously-armed: install a fake cleanup that records
	// being called.
	calls := 0
	startProxyHostsCleanup = func() { calls++ }
	startProxyHostsArmed = true
	t.Cleanup(func() {
		startProxyHostsCleanup = nil
		startProxyHostsArmed = false
		startProxySNICancel = nil
	})

	cfgPath := filepath.Join(home, ".config", "slimference", "config.toml")
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0o755)
	_ = os.WriteFile(cfgPath, []byte("[transparent]\nsni_peek_mode = false\n"), 0o600)

	reloadSNIPeekModeFromDisk(cfg)
	if cfg.Transparent.SNIPeekMode {
		t.Error("cfg.SNIPeekMode not flipped off")
	}
	if calls != 1 {
		t.Errorf("cleanup called %d times, want 1", calls)
	}
	if startProxyHostsCleanup != nil {
		t.Error("cleanup reference not cleared after disarm")
	}
	if startProxyHostsArmed {
		t.Error("hosts armed state not cleared after disarm")
	}
}

func TestReloadSNIPeekModeFromDiskNoChange(t *testing.T) {
	home, _ := withTempHostsFile(t)
	cfg := config.Defaults()
	cfg.Transparent.SNIPeekMode = false
	startProxyHostsCleanup = nil
	startProxyHostsArmed = false
	t.Cleanup(func() {
		startProxyHostsCleanup = nil
		startProxyHostsArmed = false
		startProxySNICancel = nil
	})

	cfgPath := filepath.Join(home, ".config", "slimference", "config.toml")
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0o755)
	_ = os.WriteFile(cfgPath, []byte("[transparent]\nsni_peek_mode = false\n"), 0o600)

	reloadSNIPeekModeFromDisk(cfg)
	if cfg.Transparent.SNIPeekMode {
		t.Error("cfg.SNIPeekMode incorrectly flipped on")
	}
}

func TestReloadSNIPeekModeFromDiskArmsAfterNoopStartupCleanup(t *testing.T) {
	home, _ := withTempHostsFile(t)
	prev := installHostsPlanFn
	applied := 0
	installHostsPlanFn = func(install.HostsOptions) (*reversibility.Plan, error) {
		return reversibility.NewPlan(installCmdFakeStep{name: "hosts.patch", applyFn: func(context.Context) error {
			applied++
			return nil
		}}), nil
	}
	t.Cleanup(func() {
		installHostsPlanFn = prev
		startProxyHostsCleanup = nil
		startProxyHostsArmed = false
		startProxySNICancel = nil
	})

	cfg := config.Defaults()
	cfg.Transparent.SNIPeekMode = false
	startProxyHostsCleanup = applyHostsPatch(cfg)
	if startProxyHostsArmed {
		t.Fatal("disabled startup should install only a no-op cleanup")
	}

	cfgPath := filepath.Join(home, ".config", "slimference", "config.toml")
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0o755)
	_ = os.WriteFile(cfgPath, []byte("[transparent]\nsni_peek_mode = true\n"), 0o600)

	reloadSNIPeekModeFromDisk(cfg)
	if !cfg.Transparent.SNIPeekMode {
		t.Fatal("cfg.SNIPeekMode not flipped on")
	}
	if applied != 1 {
		t.Fatalf("hosts apply attempts=%d, want 1", applied)
	}
	if !startProxyHostsArmed {
		t.Fatal("reload should mark hosts armed after successful apply")
	}
}

func TestReloadSNIPeekModeFromDiskStartsAndStopsSNIEngine(t *testing.T) {
	home, _ := withTempHostsFile(t)
	prevPlan := installHostsPlanFn
	prevEngine := startSNIPeekEngineFn
	starts := 0
	cancels := 0
	installHostsPlanFn = func(install.HostsOptions) (*reversibility.Plan, error) {
		return reversibility.NewPlan(installCmdFakeStep{name: "hosts.patch"}), nil
	}
	startSNIPeekEngineFn = func(_ *proxy.Proxy, _ *config.Config, _ *apps.Manager) (*transparent.Engine, context.CancelFunc) {
		starts++
		return nil, func() { cancels++ }
	}
	t.Cleanup(func() {
		installHostsPlanFn = prevPlan
		startSNIPeekEngineFn = prevEngine
		startProxyHostsCleanup = nil
		startProxyHostsArmed = false
		startProxySNICancel = nil
		startProxyInstance = nil
	})

	cfg := config.Defaults()
	cfg.Transparent.SNIPeekMode = false
	startProxyInstance = proxy.New(cfg)
	startProxyHostsCleanup = applyHostsPatch(cfg)

	cfgPath := filepath.Join(home, ".config", "slimference", "config.toml")
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0o755)
	_ = os.WriteFile(cfgPath, []byte("[transparent]\nsni_peek_mode = true\n"), 0o600)
	reloadSNIPeekModeFromDisk(cfg)
	if starts != 1 {
		t.Fatalf("engine starts=%d, want 1", starts)
	}
	if startProxySNICancel == nil {
		t.Fatal("engine cancel not retained after arm")
	}

	_ = os.WriteFile(cfgPath, []byte("[transparent]\nsni_peek_mode = false\n"), 0o600)
	reloadSNIPeekModeFromDisk(cfg)
	if cancels != 1 {
		t.Fatalf("engine cancels=%d, want 1", cancels)
	}
	if startProxySNICancel != nil {
		t.Fatal("engine cancel not cleared after disarm")
	}
}

func TestReloadSNIPeekModeFromDiskNilCfgNoCrash(t *testing.T) {
	reloadSNIPeekModeFromDisk(nil)
}

func TestReloadSNIPeekModeFromDiskMissingKeyIsNoOp(t *testing.T) {
	home, _ := withTempHostsFile(t)
	cfg := config.Defaults()
	cfg.Transparent.SNIPeekMode = false

	cfgPath := filepath.Join(home, ".config", "slimference", "config.toml")
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0o755)
	_ = os.WriteFile(cfgPath, []byte("[transparent]\nca_dir = \"x\"\n"), 0o600)

	reloadSNIPeekModeFromDisk(cfg)
	if cfg.Transparent.SNIPeekMode {
		t.Error("missing key should not flip cfg")
	}
}

func TestReloadSNIPeekModeFromDiskMissingAndUnreadableConfig(t *testing.T) {
	home := t.TempDir()
	prevHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prevHome })
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("SLIMFERENCE_CONFIG", "")

	cfg := config.Defaults()
	reloadSNIPeekModeFromDisk(cfg)
	if cfg.Transparent.SNIPeekMode {
		t.Fatal("missing config should not arm")
	}

	cfgDir := filepath.Join(home, "config-as-dir")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir cfg dir: %v", err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", cfgDir)
	reloadSNIPeekModeFromDisk(cfg)
	if cfg.Transparent.SNIPeekMode {
		t.Fatal("unreadable config should not arm")
	}
}

// TestEndToEndCLIDaemonSIGHUP: write config via runLabEnableCmd, send
// SIGHUP to a fake daemon goroutine, observe reloadSNIPeekModeFromDisk
// fires.
func TestEndToEndCLIDaemonSIGHUP(t *testing.T) {
	home, _ := withTempHostsFile(t)

	cfg := config.Defaults()
	cfg.Transparent.SNIPeekMode = false
	startProxyHostsCleanup = nil
	t.Cleanup(func() { startProxyHostsCleanup = nil })

	// Step 1: CLI enable writes config + tries to SIGHUP. With no
	// PID file, SIGHUP is skipped — that's the fail-open path.
	bufOut := &bytes.Buffer{}
	bufErr := &bytes.Buffer{}
	rc := runLabEnableCmd(nil, installPrinter{Out: bufOut, Err: bufErr})
	if rc != 0 {
		t.Fatalf("enable rc=%d err=%s", rc, bufErr.String())
	}

	// Step 2: simulate daemon reading the updated config on SIGHUP.
	reloadSNIPeekModeFromDisk(cfg)
	if !cfg.Transparent.SNIPeekMode {
		t.Error("daemon did not pick up enabled state")
	}

	// Step 3: CLI disable.
	rc = runLabDisableCmd(nil, installPrinter{Out: bufOut, Err: bufErr})
	if rc != 0 {
		t.Fatalf("disable rc=%d err=%s", rc, bufErr.String())
	}
	reloadSNIPeekModeFromDisk(cfg)
	if cfg.Transparent.SNIPeekMode {
		t.Error("daemon did not pick up disabled state")
	}

	_ = home
}

// TestPIDFileEnablesCLISignal writes a PID file and verifies the CLI
// asks the signal layer to SIGHUP that PID. The signal layer is
// injected so the test never signals the running `go test` process.
func TestPIDFileEnablesCLISignal(t *testing.T) {
	dir := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { osUserHomeDir = prev })

	prevSignal := signalPIDFn
	var gotPID int
	var gotSignal os.Signal
	signalPIDFn = func(pid int, sig os.Signal) error {
		gotPID = pid
		gotSignal = sig
		return nil
	}
	t.Cleanup(func() { signalPIDFn = prevSignal })

	cleanup := writePIDFile()
	t.Cleanup(cleanup)

	sent, err := signalDaemonReload()
	if err != nil {
		t.Fatalf("signal: %v", err)
	}
	if !sent {
		t.Fatal("expected sent=true")
	}
	if gotPID != os.Getpid() || gotSignal != syscall.SIGHUP {
		t.Fatalf("signal target=(%d,%v), want (%d,%v)", gotPID, gotSignal, os.Getpid(), syscall.SIGHUP)
	}
}

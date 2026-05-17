package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/control/apps"
)

// TestRunHeadlessSIGHUPReloadsApps drives runHeadless with both SIGHUP
// and SIGTERM channels. The first signal is a SIGHUP that should
// rewrite the in-memory policy, the second is SIGTERM that ends the
// loop. We assert that after exit, the manager reflects the updated
// on-disk policy.
func TestRunHeadlessSIGHUPReloadsApps(t *testing.T) {
	dir := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { osUserHomeDir = prev })

	// Manager wired into startProxyAppsManager so the signal handler
	// sees a non-nil target.
	appsPath := filepath.Join(dir, ".slimference", "apps.toml")
	if err := os.MkdirAll(filepath.Dir(appsPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	m, err := apps.NewManager(appsPath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prevMgr := startProxyAppsManager
	startProxyAppsManager = m
	t.Cleanup(func() { startProxyAppsManager = prevMgr })

	// Pre-conditions
	if !m.Policy().IsEnabled(apps.AppCodexCLI) {
		t.Fatal("precondition: codex_cli should be enabled in default policy")
	}

	// Stub the runtime so runHeadless does not actually open a port.
	code := new(int)
	*code = -1
	origExit := exitFn
	exitFn = func(c int) { *code = c; panic(exitSentinel{}) }
	t.Cleanup(func() { exitFn = origExit })

	origCfg := configLoadFn
	configLoadFn = func() (*config.Config, error) {
		cfg, _, err := config.LoadWithOptions(config.LoadOptions{})
		return cfg, err
	}
	t.Cleanup(func() { configLoadFn = origCfg })

	origStart := startProxyFn
	startProxyFn = func(cfg *config.Config) (func(ctx context.Context) error, error) {
		return func(ctx context.Context) error { return nil }, nil
	}
	t.Cleanup(func() { startProxyFn = origStart })

	// Drive both signal channels. The first Notify call (line ~58 in
	// headless.go) installs sigCh — answer that with SIGTERM after
	// the HUP has had time to take effect. The second Notify call
	// (line ~62) installs hupCh — answer that immediately with SIGHUP.
	origNotify := signalNotifyFn
	origStopFn := signalStopFn
	signalStopFn = func(c chan<- os.Signal) {}
	t.Cleanup(func() {
		signalNotifyFn = origNotify
		signalStopFn = origStopFn
	})

	var calls int
	var mu sync.Mutex
	tomlOut := "schema_version = 1\n\n[apps.codex_cli]\nenabled = false\n\n[apps.codex_desktop_app]\nenabled = true\n\n[apps.claude_code]\nenabled = false\n"

	signalNotifyFn = func(c chan<- os.Signal, sig ...os.Signal) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		switch n {
		case 1:
			// sigCh - wait long enough for the HUP path to run
			go func() {
				time.Sleep(120 * time.Millisecond)
				c <- syscall.SIGTERM
			}()
		case 2:
			// hupCh - rewrite the toml then trigger reload immediately
			go func() {
				_ = os.WriteFile(appsPath, []byte(tomlOut), 0o600)
				time.Sleep(20 * time.Millisecond)
				c <- syscall.SIGHUP
			}()
		}
	}

	defer recoverExit(t)
	runHeadless(nil)

	if *code != 0 {
		t.Fatalf("exit code = %d, want 0", *code)
	}
	if m.Policy().IsEnabled(apps.AppCodexCLI) {
		t.Fatal("expected codex_cli disabled after SIGHUP reload")
	}
}

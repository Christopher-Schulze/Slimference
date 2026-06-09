package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/control"
	"github.com/Christopher-Schulze/Slimference/internal/control/apps"
	"github.com/Christopher-Schulze/Slimference/internal/proxy"
)

func TestPhaseGAppsPathHomeMissing(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SLIMFERENCE_CONFIG", "")
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { osUserHomeDir = prev })

	if got := phaseGAppsPath(); got != "" {
		t.Fatalf("expected empty path when HOME missing, got %q", got)
	}
}

func TestPhaseGAppsPathHomePresent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SLIMFERENCE_CONFIG", "")
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { osUserHomeDir = prev })

	got := phaseGAppsPath()
	want := filepath.Join(dir, ".config", "slimference", "apps.toml")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPhaseGAppsPathHonorsXDG(t *testing.T) {
	xdg := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	got := phaseGAppsPath()
	want := filepath.Join(xdg, "slimference", "apps.toml")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPhaseGAppsPathHonorsSlimferenceConfigDir(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "custom.toml")
	t.Setenv("SLIMFERENCE_CONFIG", cfg)
	got := phaseGAppsPath()
	want := filepath.Join(dir, "apps.toml")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestWirePhaseGNilProxyReturnsNil(t *testing.T) {
	if got := wirePhaseG(nil, config.Defaults()); got != nil {
		t.Fatalf("expected nil for nil proxy, got %v", got)
	}
}

func TestWirePhaseGInstallsManagerAndProvider(t *testing.T) {
	dir := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { osUserHomeDir = prev })

	p := proxy.New(config.Defaults())
	m := wirePhaseG(p, config.Defaults())
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if p.AppsManager() != m {
		t.Fatal("manager not installed on proxy")
	}

	// /admin/state should now succeed.
	req := httptest.NewRequest(http.MethodGet, proxy.AdminStatePath, nil)
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from /admin/state, got %d: %s", rec.Code, rec.Body.String())
	}
	var state control.SetupState
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode SetupState: %v", err)
	}
	if len(state.Apps) != len(apps.KnownApps) {
		t.Errorf("expected %d apps, got %d", len(apps.KnownApps), len(state.Apps))
	}
}

func TestWirePhaseGManagerInitFailureFallsBack(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "apps.toml"), []byte("not = [valid"), 0o600); err != nil {
		t.Fatalf("write bad apps toml: %v", err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(dir, "config.toml"))
	p := proxy.New(config.Defaults())
	if got := wirePhaseG(p, config.Defaults()); got != nil {
		t.Fatalf("expected nil manager on corrupt apps.toml, got %v", got)
	}
	if p.AppsManager() != nil {
		t.Fatal("proxy should not retain manager after init failure")
	}
}

func TestBuildProbesFieldsPopulated(t *testing.T) {
	dir := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { osUserHomeDir = prev })

	p := proxy.New(config.Defaults())
	m, err := apps.NewManager(filepath.Join(dir, "apps.toml"))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	probes := buildProbes(p, m, config.Defaults())
	if probes.CA == nil {
		t.Error("CA probe missing")
	}
	if probes.Daemon == nil {
		t.Error("Daemon probe missing")
	}
	if probes.Listener == nil {
		t.Error("Listener probe missing")
	}
	if probes.NetworkRedir == nil {
		t.Error("NetworkRedir probe missing")
	}
	if probes.Apps == nil {
		t.Error("Apps probe missing")
	}
	if probes.CodexRoute == nil {
		t.Error("CodexRoute probe missing")
	}
	if probes.Savings == nil {
		t.Error("Savings probe missing")
	}
	if probes.Indist == nil {
		t.Error("Indist probe missing")
	}
}

func TestBuildProbesNilProxyReturnsEmpty(t *testing.T) {
	probes := buildProbes(nil, nil, config.Defaults())
	if probes == nil {
		t.Fatal("expected non-nil probes struct")
	}
	if probes.CA != nil || probes.Apps != nil {
		t.Errorf("expected empty probes for nil proxy")
	}
}

func TestReloadAppsManagerNilSafe(t *testing.T) {
	reloadAppsManager(nil) // must not panic
}

func TestReloadAppsManagerReadsUpdatedToml(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apps.toml")
	m, err := apps.NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	// Default policy enables codex_cli.
	if !m.Policy().IsEnabled(apps.AppCodexCLI) {
		t.Fatal("precondition: codex_cli should be enabled by default")
	}
	// Externally mutate the file: write a policy that disables it.
	tomlOut := "schema_version = 1\n\n[apps.codex_cli]\nenabled = false\n\n[apps.codex_desktop_app]\nenabled = true\n\n[apps.claude_code]\nenabled = false\n"
	if err := os.WriteFile(path, []byte(tomlOut), 0o600); err != nil {
		t.Fatalf("write toml: %v", err)
	}
	reloadAppsManager(m)
	if m.Policy().IsEnabled(apps.AppCodexCLI) {
		t.Fatal("after reload codex_cli should be disabled")
	}
}

func TestReloadAppsManagerMalformedTomlIsNonFatal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apps.toml")
	m, err := apps.NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := os.WriteFile(path, []byte("schema_version = [broken"), 0o600); err != nil {
		t.Fatalf("write bad toml: %v", err)
	}
	reloadAppsManager(m)
	if !m.Policy().IsEnabled(apps.AppCodexCLI) {
		t.Fatal("malformed reload should keep previous policy")
	}
}

func TestEnsureSlimDataDirIdempotent(t *testing.T) {
	dir := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { osUserHomeDir = prev })

	first := ensureSlimDataDir()
	if first == "" {
		t.Fatal("expected non-empty path")
	}
	info, err := os.Stat(first)
	if err != nil || !info.IsDir() {
		t.Fatalf("dir not created: %v", err)
	}
	// Second call must not error.
	second := ensureSlimDataDir()
	if second != first {
		t.Errorf("expected stable path, got %q vs %q", first, second)
	}
}

func TestEnsureSlimDataDirHomeMissing(t *testing.T) {
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { osUserHomeDir = prev })
	if got := ensureSlimDataDir(); got != "" {
		t.Fatalf("expected empty path on missing HOME, got %q", got)
	}
}

func TestEnsureSlimDataDirMkdirFailureStillReturnsPath(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "home-as-file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return blocker, nil }
	t.Cleanup(func() { osUserHomeDir = prev })
	want := filepath.Join(blocker, ".slimference")
	if got := ensureSlimDataDir(); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestWirePhaseGStateProviderRespectsCancelledContext(t *testing.T) {
	dir := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { osUserHomeDir = prev })

	p := proxy.New(config.Defaults())
	wirePhaseG(p, config.Defaults())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, proxy.AdminStatePath, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)
	// Probes still run (no per-probe ctx check in HostsFile/FileCA),
	// so a cancelled ctx is fine.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

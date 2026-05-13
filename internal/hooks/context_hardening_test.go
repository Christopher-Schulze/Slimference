package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallCodex_hooksJSONWriteErrorAfterPreflight(t *testing.T) {
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}
	if err := os.Chmod(codexDir, 0o555); err != nil {
		t.Fatalf("chmod codex dir: %v", err)
	}
	defer func() {
		_ = os.Chmod(codexDir, 0o755)
	}()

	err := InstallCodex(home, "slimference")
	if err == nil || !strings.Contains(err.Error(), "write hooks.json") {
		t.Fatalf("expected hooks.json write error, got %v", err)
	}
}

func TestInstallCodex_reportsFeatureFlagWriteErrorAfterHooksJSON(t *testing.T) {
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}

	configPath := filepath.Join(codexDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("# existing user config\n"), 0o444); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}
	defer func() {
		_ = os.Chmod(configPath, 0o644)
	}()

	err := InstallCodex(home, "slimference")
	if err == nil || !strings.Contains(err.Error(), "enable codex hooks feature") {
		t.Fatalf("expected codex_hooks feature write error, got %v", err)
	}
}

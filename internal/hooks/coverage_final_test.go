package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallClaude_ReadHookWriteErrorAndSettingsReadError(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	hookDir := filepath.Join(home, ".claude", "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatal(err)
	}
	readScriptPath := filepath.Join(hookDir, "slimference-read-cache.sh")
	if err := os.Mkdir(readScriptPath, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := InstallClaude(home, "slimference"); err == nil {
		t.Fatal("expected read hook install failure")
	}

	if err := os.RemoveAll(readScriptPath); err != nil {
		t.Fatal(err)
	}
	if err := InstallClaude(home, "slimference"); err != nil {
		t.Fatal(err)
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	if err := os.Remove(settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(settingsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if claudeHookInstalled(home) {
		t.Fatal("settings read errors must not count as installed")
	}
}

func TestInstallCodex_ReadHookWriteErrorAndMissingReadScript(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	hooksDir := filepath.Join(home, ".slimference", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	readScriptPath := CodexReadHookScriptPath(home)
	if err := os.Mkdir(readScriptPath, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := InstallCodex(home, "slimference"); err == nil {
		t.Fatal("expected codex read hook install failure")
	}

	if err := os.RemoveAll(readScriptPath); err != nil {
		t.Fatal(err)
	}
	if err := InstallCodex(home, "slimference"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(CodexReadHookScriptPath(home)); err != nil {
		t.Fatal(err)
	}
	if codexCoherentInstall(home) {
		t.Fatal("missing codex read hook must make install incoherent")
	}
}

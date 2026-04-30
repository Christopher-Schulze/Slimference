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

// TestValidateCodexConfig_ReadError covers the non-IsNotExist read error branch
// in validateCodexConfig (codex.go:239).
func TestValidateCodexConfig_ReadError(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Make config.toml a directory so ReadFile returns an error that is NOT ErrNotExist.
	if err := os.Mkdir(filepath.Join(codexDir, "config.toml"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := validateCodexConfig(filepath.Join(codexDir, "config.toml"))
	if err == nil {
		t.Fatal("expected read error for directory-as-file")
	}
}

// TestUnpatchCodexConfig_NotExists covers the IsNotExist early-return branch
// in unpatchCodexConfig (codex.go:361-363).
func TestUnpatchCodexConfig_NotExists(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	// config.toml does not exist -> should return nil without error.
	err := unpatchCodexConfig(home)
	if err != nil {
		t.Fatalf("expected nil for missing config, got %v", err)
	}
}

// TestCodexStatusInstalled_CoherentTrue covers the codexCoherentInstall==true
// early-return branch in codexStatusInstalled (verify.go:52-54).
func TestCodexStatusInstalled_CoherentTrue(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := InstallCodex(home, "slimference"); err != nil {
		t.Fatalf("install codex: %v", err)
	}
	// InstallCodex does not write config.toml; a coherent install requires
	// both openai_base_url and chatgpt_base_url pointing at the proxy.
	config := "openai_base_url = \"http://127.0.0.1:8990/v1\"\nchatgpt_base_url = \"http://127.0.0.1:8990/v1\"\n"
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if !codexStatusInstalled(home) {
		t.Fatal("expected codexStatusInstalled true for coherent install")
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

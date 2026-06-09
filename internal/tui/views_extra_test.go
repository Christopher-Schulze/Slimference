package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderSetupView_NoServiceControlAllChecksReady(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfgPath := filepath.Join(home, ".slimference", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	pidPath := filepath.Join(home, ".slimference", "slimference.pid")
	if err := os.WriteFile(pidPath, []byte("1234"), 0o644); err != nil {
		t.Fatalf("write pid: %v", err)
	}

	plistPath := filepath.Join(home, "Library", "LaunchAgents", "com.slimference.daemon.plist")
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		t.Fatalf("mkdir plist dir: %v", err)
	}
	if err := os.WriteFile(plistPath, []byte("plist"), 0o644); err != nil {
		t.Fatalf("write plist: %v", err)
	}

	model := NewModel(newMockProxy())
	model.width = 100
	model.height = 30
	model.hookStatus = HookStatus{Claude: true, Codex: true}

	output := model.renderSetupView()
	if !strings.Contains(output, "ALL SET") {
		t.Fatalf("expected ready header, got: %s", output)
	}
	if !strings.Contains(output, "Daemon running") {
		t.Fatalf("expected daemon-running checklist entry, got: %s", output)
	}
	if strings.Contains(output, "✗") {
		t.Fatalf("expected all fallback checks to pass, got: %s", output)
	}
}

func TestRenderSetupView_NoServiceControlStoppedFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(home, ".slimference", "missing.toml"))

	model := NewModel(newMockProxy())
	model.width = 100
	model.height = 30

	output := model.renderSetupView()
	if !strings.Contains(output, "Daemon not running") {
		t.Fatalf("expected stopped fallback status, got: %s", output)
	}
}

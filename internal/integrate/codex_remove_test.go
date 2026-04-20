package integrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveCodexBlock_MissingFileIsIdempotent(t *testing.T) {
	home := t.TempDir()
	evt, err := RemoveCodexBlock(home)
	if err != nil {
		t.Fatal(err)
	}
	if evt.Action != "skipped_idempotent" {
		t.Fatalf("action = %q, want skipped_idempotent", evt.Action)
	}
}

func TestRemoveCodexBlock_NoFenceIsIdempotent(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	cfg := filepath.Join(home, ".codex", "config.toml")
	_ = os.WriteFile(cfg, []byte(`model = "gpt-5.4"`+"\n"), 0o644)

	evt, err := RemoveCodexBlock(home)
	if err != nil {
		t.Fatal(err)
	}
	if evt.Action != "skipped_idempotent" {
		t.Fatalf("action = %q", evt.Action)
	}
}

func TestRemoveCodexBlock_StripsExistingFence(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	// Install first to create a fenced config.
	if _, err := WriteCodexBlock(home, ProxyURL); err != nil {
		t.Fatal(err)
	}
	evt, err := RemoveCodexBlock(home)
	if err != nil {
		t.Fatal(err)
	}
	if evt.Action != "removed_block" {
		t.Fatalf("action = %q, want removed_block", evt.Action)
	}
	content, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if strings.Contains(string(content), markerStart) {
		t.Fatalf("fence still present: %s", content)
	}
}

func TestRemoveCodexBlock_PreservesSurroundingContent(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	cfg := filepath.Join(home, ".codex", "config.toml")
	content := `model = "gpt-5.4"
approval_policy = "never"

[projects."/work"]
trust_level = "trusted"
`
	_ = os.WriteFile(cfg, []byte(content), 0o644)

	// Install then remove - surrounding content must survive.
	if _, err := WriteCodexBlock(home, ProxyURL); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveCodexBlock(home); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(cfg)
	if !strings.Contains(string(got), `model = "gpt-5.4"`) {
		t.Fatal("user content lost")
	}
	if !strings.Contains(string(got), `[projects."/work"]`) {
		t.Fatal("table header lost")
	}
	if strings.Contains(string(got), markerStart) {
		t.Fatal("fence still present")
	}
}

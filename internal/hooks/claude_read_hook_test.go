package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeReadHookScript_customCommand(t *testing.T) {
	t.Parallel()

	script := ClaudeReadHookScript("/opt/bin/slimference")
	if !strings.Contains(script, "/opt/bin/slimference") {
		t.Fatalf("script:\n%s", script)
	}
	if !strings.Contains(script, "readhook") {
		t.Fatalf("script:\n%s", script)
	}
}

func TestInstallClaude_WiresReadMatcher(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := InstallClaude(home, "slimference"); err != nil {
		t.Fatal(err)
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"matcher": "Read"`) {
		t.Fatalf("expected Read matcher in settings: %s", text)
	}
	if !strings.Contains(text, "slimference-read-cache.sh") {
		t.Fatalf("expected read-cache hook script in settings: %s", text)
	}
}

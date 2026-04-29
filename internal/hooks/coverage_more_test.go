package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallAndVerifyEdgeCoverage(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := InstallClaude(home, "slimference"); err != nil {
		t.Fatal(err)
	}
	if !claudeHookInstalled(home) {
		t.Fatal("claude hook should be installed")
	}

	if err := InstallCodex(home, "slimference"); err != nil {
		t.Fatal(err)
	}
	if !InspectCodexHooks(home).Complete() {
		t.Fatal("codex hooks should be complete")
	}

	if err := os.Remove(CodexHookScriptPath(home)); err != nil {
		t.Fatal(err)
	}
	if InspectCodexHooks(home).Complete() {
		t.Fatal("codex hooks should become incomplete when post script is removed")
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{`), 0o644); err != nil {
		t.Fatal(err)
	}
	if claudeHookInstalled(home) {
		t.Fatal("broken claude settings should not count as installed")
	}
}

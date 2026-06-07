package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexDesktopMenubarScriptUsesPatchFreeStatusItem(t *testing.T) {
	script := codexDesktopMenubarScript(codexDesktopMenubarTitle, codexDesktopMenubarTooltip)
	for _, want := range []string{
		"NSStatusBar.systemStatusBar.statusItemWithLength",
		"item.button.title = \"● SF\"",
		"item.button.toolTip = \"Slimference Codex App active\"",
		"Slimference active",
		"Codex App through Slimference",
		"Hide indicator",
		"app.run();",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
	for _, forbidden := range []string{
		"model/list",
		"defaultServiceTier",
		"serviceTiers",
		"Codex.app/Contents",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("script must stay patch-free; found %q in:\n%s", forbidden, script)
		}
	}
}

func TestCodexDesktopMenubarScriptPathUsesSlimferenceRunDir(t *testing.T) {
	oldHome := codexDesktopMenubarHomeFn
	t.Cleanup(func() { codexDesktopMenubarHomeFn = oldHome })
	home := t.TempDir()
	codexDesktopMenubarHomeFn = func() (string, error) { return home, nil }
	got, err := codexDesktopMenubarScriptPath()
	if err != nil {
		t.Fatalf("script path: %v", err)
	}
	want := filepath.Join(home, ".slimference", "run", codexDesktopMenubarScriptName)
	if got != want {
		t.Fatalf("script path=%q want %q", got, want)
	}
}

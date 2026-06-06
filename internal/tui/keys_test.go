package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
)

// TestDefaultKeyMap_AllBindingsPresent verifies that all key bindings are configured.
func TestDefaultKeyMap_AllBindingsPresent(t *testing.T) {
	t.Parallel()
	km := DefaultKeyMap()

	bindings := []struct {
		name string
		b    interface{ Help() key.Help }
	}{
		{"ToggleClaude", km.ToggleClaude},
		{"ToggleCodex", km.ToggleCodex},
		{"ToggleLayer1", km.ToggleLayer1},
		{"ToggleLayer2", km.ToggleLayer2},
		{"PrevView", km.PrevView},
		{"NextView", km.NextView},
		{"CursorUp", km.CursorUp},
		{"CursorDown", km.CursorDown},
		{"Execute", km.Execute},
		{"ViewStats", km.ViewStats},
		{"ViewDebug", km.ViewDebug},
		{"FlushCaches", km.FlushCaches},
		{"Quit", km.Quit},
	}

	for _, bb := range bindings {
		h := bb.b.Help()
		if h.Key == "" {
			t.Errorf("%s binding has empty key", bb.name)
		}
		if h.Desc == "" {
			t.Errorf("%s binding has empty description", bb.name)
		}
	}
}

// TestFooterHelp_ContainsAllKeys verifies that the footer help string includes all shortcuts.
func TestFooterHelp_ContainsAllKeys(t *testing.T) {
	t.Parallel()
	km := DefaultKeyMap()
	help := km.footerHelp()

	expectedKeys := []string{"[←/→]", "[↑/↓]", "[enter]", "[s]", "[d]", "[i]", "[b]", "[q]"}
	for _, key := range expectedKeys {
		if !strings.Contains(help, key) {
			t.Errorf("footerHelp() missing key %q in: %s", key, help)
		}
	}
}

// TestFooterHelp_NonEmpty verifies the footer help is not empty.
func TestFooterHelp_NonEmpty(t *testing.T) {
	t.Parallel()
	km := DefaultKeyMap()
	help := km.footerHelp()
	if help == "" {
		t.Error("footerHelp() returned empty string")
	}
}

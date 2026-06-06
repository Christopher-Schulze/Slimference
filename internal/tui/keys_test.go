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

// TestFooterHelp_Hidden verifies that the visible footer legend stays hidden.
func TestFooterHelp_Hidden(t *testing.T) {
	t.Parallel()
	km := DefaultKeyMap()
	help := km.footerHelp()

	if help != "" {
		t.Fatalf("footerHelp()=%q, want hidden footer", help)
	}
}

// TestKeybindingsMarkdown_StillDocumentsKeys verifies that removing the visible
// footer does not erase the generated keybinding documentation source.
func TestKeybindingsMarkdown_StillDocumentsKeys(t *testing.T) {
	t.Parallel()
	km := DefaultKeyMap()
	doc := km.RenderKeybindingsMarkdown()
	for _, key := range []string{"up, k", "enter", "b, B", "q, ctrl+c"} {
		if !strings.Contains(doc, key) {
			t.Fatalf("RenderKeybindingsMarkdown() missing %q in:\n%s", key, doc)
		}
	}
}

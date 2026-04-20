package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// keybindingsDocPath returns the canonical path to the committed
// docs/tui-keybindings.md file. Resolved relative to this source file so the
// test does not depend on the working directory.
func keybindingsDocPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// internal/tui/keybindings_doc_test.go -> <repo>/docs/tui-keybindings.md
	return filepath.Join(filepath.Dir(file), "..", "..", "docs", "tui-keybindings.md")
}

// TestT64_KeybindingsDocInSync asserts that docs/tui-keybindings.md matches
// the output of DefaultKeyMap().RenderKeybindingsMarkdown(). This is how we
// prevent drift between the canonical bindings (keys.go) and the user-facing
// documentation.
//
// Regenerate the file by setting GO_GEN_TUI_DOCS=1 and re-running this test:
//
//	GO_GEN_TUI_DOCS=1 go test -run TestT64_KeybindingsDocInSync ./internal/tui/...
func TestT64_KeybindingsDocInSync(t *testing.T) {
	want := DefaultKeyMap().RenderKeybindingsMarkdown()
	path := keybindingsDocPath(t)

	if os.Getenv("GO_GEN_TUI_DOCS") == "1" {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatalf("write keybindings doc: %v", err)
		}
		t.Logf("wrote %s", path)
		return
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read keybindings doc (does %s exist?): %v", path, err)
	}
	if string(got) != want {
		t.Fatalf(`keybindings doc out of sync with keys.go.

To regenerate run:
    GO_GEN_TUI_DOCS=1 go test -run TestT64_KeybindingsDocInSync ./internal/tui/...

first 400 bytes diff:
--- got ---
%s
--- want ---
%s`, firstN(string(got), 400), firstN(want, 400))
	}
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

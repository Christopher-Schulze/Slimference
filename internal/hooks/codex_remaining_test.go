package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallCodexPreAndPostCompactWriteErrors(t *testing.T) {
	for _, tc := range []struct {
		name      string
		blockPath func(string) string
		want      string
	}{
		{name: "precompact", blockPath: CodexPreCompactHookScriptPath, want: "write pre-compact hook script"},
		{name: "postcompact", blockPath: CodexPostCompactHookScriptPath, want: "write post-compact hook script"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
				t.Fatalf("mkdir codex: %v", err)
			}
			if err := os.MkdirAll(tc.blockPath(home), 0o755); err != nil {
				t.Fatalf("mkdir block path: %v", err)
			}
			err := InstallCodex(home, "slimference")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("InstallCodex err=%v want %q", err, tc.want)
			}
		})
	}
}

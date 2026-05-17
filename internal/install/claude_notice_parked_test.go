package install

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeNoticeShapeRemainsParkedReference(t *testing.T) {
	home := t.TempDir()
	n := claudeNotice(home, "test-version")
	if n == nil {
		t.Fatal("nil notice")
	}
	if n.Path != filepath.Join(home, ".claude", "SLIMFERENCE.md") {
		t.Fatalf("path=%q", n.Path)
	}
	if n.StepName != "notice.claude" || n.AppName != "Claude Code" {
		t.Fatalf("unexpected metadata: step=%q app=%q", n.StepName, n.AppName)
	}
	if n.Version != "test-version" {
		t.Fatalf("version=%q", n.Version)
	}
	for _, want := range []string{"settings.json", "slimference-rewrite.sh"} {
		if !strings.Contains(n.Body, want) {
			t.Fatalf("notice body missing %q: %s", want, n.Body)
		}
	}
}

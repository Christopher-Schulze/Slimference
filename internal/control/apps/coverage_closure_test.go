package apps

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetEnabledWriteErrorFromInvalidParentPath(t *testing.T) {
	dir := t.TempDir()
	notDir := filepath.Join(dir, "not-dir")
	if err := os.WriteFile(notDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager("")
	if err != nil {
		t.Fatal(err)
	}
	m.path = filepath.Join(notDir, "apps.toml")
	if err := m.SetEnabled(AppCodexCLI, false); err == nil {
		t.Fatal("expected write error through invalid parent path")
	}
}

func TestNormalizePolicyNilEnabledMap(t *testing.T) {
	p := normalizePolicy(Policy{})
	if p.Enabled == nil {
		t.Fatal("Enabled map must be initialized")
	}
	if p.SchemaVersion != 1 {
		t.Fatalf("SchemaVersion=%d, want 1", p.SchemaVersion)
	}
	for _, id := range KnownApps {
		if _, ok := p.Enabled[id]; !ok {
			t.Fatalf("missing known app %q", id)
		}
	}
	if p.Enabled[AppClaudeCode] {
		t.Fatal("Claude Code must stay forced off")
	}
}

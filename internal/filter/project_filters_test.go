package filter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProjectDenyPatterns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sub := filepath.Join(dir, "proj")
	if err := os.MkdirAll(filepath.Join(sub, ".tokenproxy"), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, ".tokenproxy", "filters.toml")
	content := `deny_patterns = ['^git\s+push\s+--force']
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	got := LoadProjectDenyPatterns(sub)
	want := `^git\s+push\s+--force`
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %#v want %q", got, want)
	}
	if LoadProjectDenyPatterns(t.TempDir()) != nil {
		t.Fatal("missing file should return nil")
	}
}

// TestProjectFiltersPath_empty covers the wd=="" early return.
func TestProjectFiltersPath_empty(t *testing.T) {
	t.Parallel()
	if p := ProjectFiltersPath(""); p != "" {
		t.Fatalf("empty wd: got %q", p)
	}
}

// TestLoadProjectDenyPatterns_emptyWd covers the empty wd path through ProjectFiltersPath.
func TestLoadProjectDenyPatterns_emptyWd(t *testing.T) {
	t.Parallel()
	if got := LoadProjectDenyPatterns(""); got != nil {
		t.Fatalf("empty wd should return nil, got %v", got)
	}
}

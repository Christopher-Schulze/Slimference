package filter

import (
	"strings"
	"testing"
)

func TestTryCompactLs(t *testing.T) {
	t.Parallel()
	out, ok := TryCompactLs([]string{"ls", "-la"}, []byte(""))
	if !ok || string(out) != "[ls] empty\n" {
		t.Fatalf("ok=%v %q", ok, out)
	}
	if _, ok := TryCompactLs([]string{"dir"}, []byte("")); ok {
		t.Fatal("dir not ls")
	}
	out2, ok := TryCompactTree([]string{"tree", "-a", "d"}, []byte(""))
	if !ok || string(out2) != "[tree] empty\n" {
		t.Fatalf("tree: %q", out2)
	}
}

func TestTryCompactFs_guards(t *testing.T) {
	t.Parallel()
	// len < 1 for ls
	if _, ok := TryCompactLs([]string{}, []byte("")); ok {
		t.Fatal("ls: len<1")
	}
	// short ls output (≤10 entries) should pass through
	if _, ok := TryCompactLs([]string{"ls"}, []byte("file.txt\n")); ok {
		t.Fatal("ls: short output should pass through")
	}
	// len < 1 for tree
	if _, ok := TryCompactTree([]string{}, []byte("")); ok {
		t.Fatal("tree: len<1")
	}
	// tree output without summary line should pass through
	if _, ok := TryCompactTree([]string{"tree"}, []byte(".\n")); ok {
		t.Fatal("tree: no summary → pass through")
	}
}

func TestTryCompactLs_manyEntries(t *testing.T) {
	t.Parallel()
	// Build ls -la output with 20 file entries (> 10 threshold)
	var sb strings.Builder
	sb.WriteString("total 80\n")
	for i := 0; i < 20; i++ {
		sb.WriteString("drwxr-xr-x  2 user group 4096 Jan 01 00:00 subdir\n")
	}
	input := sb.String()
	out, ok := TryCompactLs([]string{"ls", "-la"}, []byte(input))
	if !ok {
		t.Fatalf("expected compaction for %d entries, got pass-through", 20)
	}
	s := string(out)
	if !strings.Contains(s, "[ls] 20 entries") {
		t.Errorf("want entry count, got: %q", s)
	}
	if len(s) >= len(input) {
		t.Errorf("compact should be shorter: %d vs %d", len(s), len(input))
	}
}

func TestTryCompactTree_withSummary(t *testing.T) {
	t.Parallel()
	input := ".\n├── src\n│   ├── main.go\n│   └── config.go\n├── go.mod\n└── README.md\n\n2 directories, 4 files\n"
	out, ok := TryCompactTree([]string{"tree"}, []byte(input))
	if !ok {
		t.Fatalf("expected tree compaction, got pass-through")
	}
	s := string(out)
	if s != "[tree] 2 directories, 4 files\n" {
		t.Errorf("want summary line, got: %q", s)
	}
	if len(s) >= len(input) {
		t.Errorf("compact should be shorter: %d vs %d", len(s), len(input))
	}
}

// TestCompactLsOutput_emptyEntries covers the len(entries)==0 path (line 42-44):
// input with only "total" lines produces no real entries → returns "[ls] empty\n".
func TestCompactLsOutput_emptyEntries(t *testing.T) {
	t.Parallel()
	got := compactLsOutput("total 8\ntotal 16\n")
	if got != "[ls] empty\n" {
		t.Errorf("only-total lines: want '[ls] empty\\n', got %q", got)
	}
}

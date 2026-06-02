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
	var sb strings.Builder
	sb.WriteString("total 80\n")
	for i := 0; i < 20; i++ {
		sb.WriteString("drwxr-xr-x  2 user group 4096 Jan 01 00:00 subdir")
		sb.WriteString(string(rune('a' + i%26)))
		sb.WriteString("\n")
	}
	input := sb.String()
	if _, ok := TryCompactLs([]string{"ls", "-la"}, []byte(input)); ok {
		t.Fatal("non-empty ls output must full-pass; filenames are model evidence")
	}
}

func TestTryCompactTree_withSummary(t *testing.T) {
	t.Parallel()
	input := ".\n├── src\n│   ├── main.go\n│   └── config.go\n├── go.mod\n└── README.md\n\n2 directories, 4 files\n"
	if _, ok := TryCompactTree([]string{"tree"}, []byte(input)); ok {
		t.Fatal("non-empty tree output must full-pass; hierarchy is model evidence")
	}
}

func TestTryCompactLs_onlyTotalLinesAreEmpty(t *testing.T) {
	t.Parallel()
	out, ok := TryCompactLs([]string{"ls", "-la"}, []byte("total 8\ntotal 16\n"))
	if !ok || string(out) != "[ls] empty\n" {
		t.Errorf("only-total lines: want '[ls] empty\\n', got ok=%v out=%q", ok, out)
	}
}

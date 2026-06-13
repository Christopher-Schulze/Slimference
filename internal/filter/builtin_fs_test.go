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

func TestTryCompactPathListOutputRipgrepFiles(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 40; i++ {
		sb.WriteString("src/generated/deep/package/file_")
		if i < 10 {
			sb.WriteByte('0')
		}
		sb.WriteString(string(rune('0' + i/10)))
		sb.WriteString(string(rune('0' + i%10)))
		sb.WriteString(".go\n")
	}
	out, ok := TryCompactPathListOutput([]string{"rg", "--files", "-g", "*.go", "src"}, []byte(sb.String()))
	if !ok {
		t.Fatal("rg --files path list should compact")
	}
	text := string(out)
	if !strings.Contains(text, "[rg --files paths]") ||
		!strings.Contains(text, "src/generated/deep/package/") ||
		!strings.Contains(text, "file_39.go") {
		t.Fatalf("unexpected rg --files compaction: %q", text)
	}
	if len(text) >= sb.Len() {
		t.Fatalf("path-list compaction should save bytes: out=%d in=%d", len(text), sb.Len())
	}
	if !PathListOutputReducerEligibleFromCommandLine(`rg --files --hidden -g '*.go' src`) {
		t.Fatal("direct rg --files command should be path-list eligible")
	}
	if !PathListOutputReducerEligibleFromCommandLine(`cd /repo/app && rg --files --hidden -g '*.go' src`) {
		t.Fatal("cd-wrapped rg --files command should be path-list eligible")
	}
}

func TestTryCompactPathListOutputRipgrepFilesFailOpen(t *testing.T) {
	t.Parallel()
	listOutput := []byte(strings.Repeat("src/a.go\nsrc/b.go\n", 8))
	if _, ok := TryCompactPathListOutput([]string{"rg", "-l", "needle"}, listOutput); ok {
		t.Fatal("rg -l search result lists must stay out of path-list reducer")
	}
	if _, ok := TryCompactPathListOutput([]string{"rg", "--files", "--json"}, listOutput); ok {
		t.Fatal("rg --files with unsupported output flags must fail open")
	}
	if _, ok := TryCompactPathListOutput([]string{"rg", "--files", "--null"}, []byte("src/a.go\x00src/b.go\x00")); ok {
		t.Fatal("NUL path-list output must fail open")
	}
}

func TestCompactCapturedOutputWithContextCDWrappedPathList(t *testing.T) {
	t.Parallel()

	var input strings.Builder
	for i := 0; i < 40; i++ {
		input.WriteString("src/generated/deep/package/file_")
		if i < 10 {
			input.WriteByte('0')
		}
		input.WriteString(string(rune('0' + i/10)))
		input.WriteString(string(rune('0' + i%10)))
		input.WriteString(".go\n")
	}
	out, changed := CompactCapturedOutputWithContext("", `cd /repo/app && rg --files -g '*.go' src`, input.String(), 0, FileReadContext{Mode: "scan"})
	if !changed {
		t.Fatal("cd-wrapped rg --files output should compact")
	}
	text := string(out)
	if !strings.Contains(text, "[rg --files paths]") || !strings.Contains(text, "src/generated/deep/package/") {
		t.Fatalf("unexpected cd-wrapped path-list compaction: %q", text)
	}
}

func TestPathListOutputParserEdges(t *testing.T) {
	t.Parallel()

	eligible := [][]string{
		{"ripgrep", "--files", "--glob=*.go", "src"},
		{"rg", "--files", "-g*.go", "src"},
		{"rg", "--files", "--type", "go", "--max-depth", "2", "--sort=path"},
		{"rg", "--files", "-Tvendor", "--sortr", "path"},
		{"rg", "--files", "--"},
	}
	for _, argv := range eligible {
		if !pathListOutputEligibleArgv(argv) {
			t.Fatalf("argv should be path-list eligible: %#v", argv)
		}
	}

	ineligible := [][]string{
		nil,
		{"fd", "--files"},
		{"rg", "--"},
		{"rg", "--files", "--glob="},
		{"rg", "--files", "--max-depth"},
		{"rg", "--files", "-g"},
		{"rg", "--files", "-z"},
		{"rg", "--files", ""},
	}
	for _, argv := range ineligible {
		if pathListOutputEligibleArgv(argv) {
			t.Fatalf("argv should fail open: %#v", argv)
		}
	}

	if got := pathListOutputLabel([]string{"fd", "."}); got != "paths" {
		t.Fatalf("fallback path-list label = %q", got)
	}
	if PathListOutputReducerEligibleFromCommandLine("go test ./...") {
		t.Fatal("non-path-list command must not be path-list eligible")
	}
	if normalized := NormalizePathListCommandLine("", ""); normalized != "" {
		t.Fatalf("empty path-list command normalized to %q", normalized)
	}
	if normalized := NormalizePathListCommandLine("cd repo && rg --files", ""); normalized != "" {
		t.Fatalf("relative cd wrapper must fail open, got %q", normalized)
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

func TestTryCompactWc(t *testing.T) {
	t.Parallel()
	out, ok := TryCompactWc([]string{"wc"}, []byte("      30      96     978 scripts/find_duplicate_attrs.py\n"))
	if !ok || string(out) != "scripts/find_duplicate_attrs.py: 30L 96W 978B\n" {
		t.Fatalf("full wc: ok=%v out=%q", ok, out)
	}

	lines, ok := TryCompactWc([]string{"wc", "-l"}, []byte("      300 src/generated/very/long/path/main.go\n"))
	if !ok || string(lines) != "src/generated/very/long/path/main.go: 300L\n" {
		t.Fatalf("wc -l: ok=%v out=%q", ok, lines)
	}

	multi, ok := TryCompactWc([]string{"wc", "-lw"}, []byte("      30      96 src/main.rs\n      50     120 src/lib.rs\n      80     216 total\n"))
	if !ok {
		t.Fatal("multi-file wc should compact")
	}
	want := "[wc prefix=src/]\nmain.rs: 30L 96W\nlib.rs: 50L 120W\ntotal: 80L 216W\n"
	if string(multi) != want {
		t.Fatalf("multi-file wc = %q, want %q", multi, want)
	}
}

func TestTryCompactWcFailOpen(t *testing.T) {
	t.Parallel()
	if _, ok := TryCompactWc([]string{"wc", "--files0-from=list"}, []byte("1 file\n")); ok {
		t.Fatal("unsupported wc flag must fail open")
	}
	if _, ok := TryCompactWc([]string{"wc", "-l"}, []byte("not a count\n")); ok {
		t.Fatal("unparseable wc output must fail open")
	}
	if _, ok := TryCompactWc([]string{"wc", "-l"}, []byte("123abc\n")); ok {
		t.Fatal("wc count without a separator must fail open")
	}
	if _, ok := TryCompactWc([]string{"cat"}, []byte("1 file\n")); ok {
		t.Fatal("non-wc command must fail open")
	}
}

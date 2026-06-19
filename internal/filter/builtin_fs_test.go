package filter

import (
	"fmt"
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
	if !PathListOutputReducerEligibleArgv([]string{"rg", "--files", "--hidden", "-g", "*.go", "src"}) {
		t.Fatal("rg --files argv should be path-list eligible")
	}
}

func TestTryCompactPlainPathListOutput(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&sb, "src/generated/deep/package/file_%02d.go\n", i)
	}
	out, ok := TryCompactPlainPathListOutput([]byte(sb.String()))
	if !ok {
		t.Fatal("plain path list should compact")
	}
	text := string(out)
	if !strings.Contains(text, "[plain paths]") ||
		!strings.Contains(text, "src/generated/deep/package/") ||
		!strings.Contains(text, "file_39.go") {
		t.Fatalf("unexpected plain path-list compaction: %q", text)
	}
	if len(text) >= sb.Len() {
		t.Fatalf("plain path-list compaction should save bytes: out=%d in=%d", len(text), sb.Len())
	}
}

func TestTryCompactPlainPathListOutputFailOpen(t *testing.T) {
	t.Parallel()
	searchOutput := strings.Repeat("src/a.go:10:needle context\n", 12)
	if _, ok := TryCompactPlainPathListOutput([]byte(searchOutput)); ok {
		t.Fatal("search-style output must not compact as a plain path list")
	}
	diagnostic := strings.Repeat("warning: generated file skipped\n", 12)
	if _, ok := TryCompactPlainPathListOutput([]byte(diagnostic)); ok {
		t.Fatal("diagnostic prose must not compact as a plain path list")
	}
	withSpaces := strings.Repeat("src/generated file.go\n", 12)
	if _, ok := TryCompactPlainPathListOutput([]byte(withSpaces)); ok {
		t.Fatal("space-containing ambiguous paths must fail open without command metadata")
	}
	withLeadingSpace := strings.Repeat(" src/generated/file.go\n", 12)
	if _, ok := TryCompactPlainPathListOutput([]byte(withLeadingSpace)); ok {
		t.Fatal("whitespace-padded paths must fail open without command metadata")
	}
}

func TestTryCompactPathListOutputFdPathLists(t *testing.T) {
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
	out, ok := TryCompactPathListOutput([]string{"fd", ".go", "src"}, []byte(sb.String()))
	if !ok {
		t.Fatal("fd path list should compact")
	}
	text := string(out)
	if !strings.Contains(text, "[fd paths]") ||
		!strings.Contains(text, "src/generated/deep/package/") ||
		!strings.Contains(text, "file_39.go") {
		t.Fatalf("unexpected fd path-list compaction: %q", text)
	}
	if len(text) >= sb.Len() {
		t.Fatalf("fd path-list compaction should save bytes: out=%d in=%d", len(text), sb.Len())
	}
	if !PathListOutputReducerEligibleFromCommandLine(`fd .go src`) {
		t.Fatal("direct fd command should be path-list eligible")
	}
	if !PathListOutputReducerEligibleFromCommandLine(`cd /repo/app && fd --extension go src`) {
		t.Fatal("cd-wrapped fd command should be path-list eligible")
	}
}

func TestTryCompactPathListOutputFindPathLists(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 40; i++ {
		sb.WriteString(".reconc/audit/")
		if i < 10 {
			sb.WriteByte('0')
		}
		sb.WriteString(string(rune('0' + i/10)))
		sb.WriteString(string(rune('0' + i%10)))
		sb.WriteString(".jsonl\n")
	}
	out, ok := TryCompactPathListOutput([]string{"find", ".reconc", "-maxdepth", "4", "-type", "f"}, []byte(sb.String()))
	if !ok {
		t.Fatal("bounded find path list should compact")
	}
	text := string(out)
	if !strings.Contains(text, "[find paths]") ||
		!strings.Contains(text, ".reconc/audit/") ||
		!strings.Contains(text, "39.jsonl") {
		t.Fatalf("unexpected find path-list compaction: %q", text)
	}
	if len(text) >= sb.Len() {
		t.Fatalf("find path-list compaction should save bytes: out=%d in=%d", len(text), sb.Len())
	}
	if !PathListOutputReducerEligibleFromCommandLine(`find .reconc -maxdepth 4 -type f`) {
		t.Fatal("bounded find command should be path-list eligible")
	}
	if !PathListOutputReducerEligibleFromCommandLine(`cd /repo/app && find .reconc -maxdepth 4 -type f`) {
		t.Fatal("cd-wrapped bounded find command should be path-list eligible")
	}
}

func TestTryCompactPathListOutputRipgrepFilesRootEntries(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for _, path := range []string{"README.md", "AGENTS.md", "go.mod", "SECURITY.md"} {
		sb.WriteString(path)
		sb.WriteByte('\n')
	}
	for i := 0; i < 40; i++ {
		sb.WriteString("internal/proxy/generated/deep/path/file_")
		if i < 10 {
			sb.WriteByte('0')
		}
		sb.WriteString(string(rune('0' + i/10)))
		sb.WriteString(string(rune('0' + i%10)))
		sb.WriteString(".go\n")
	}

	out, ok := TryCompactPathListOutput([]string{"rg", "--files"}, []byte(sb.String()))
	if !ok {
		t.Fatal("rg --files root path list should compact")
	}
	text := string(out)
	if !strings.Contains(text, "[rg --files paths]\n./\n  README.md\n") ||
		!strings.Contains(text, "internal/proxy/generated/deep/path/") ||
		!strings.Contains(text, "file_39.go") {
		t.Fatalf("unexpected root path-list compaction: %q", text)
	}
	if len(text) >= sb.Len() {
		t.Fatalf("root path-list compaction should save bytes: out=%d in=%d", len(text), sb.Len())
	}

	var diagnostic strings.Builder
	for i := 0; i < 12; i++ {
		if i == 3 {
			diagnostic.WriteString("warning: ambiguous path\n")
			continue
		}
		diagnostic.WriteString("internal/proxy/generated/deep/path/file_")
		diagnostic.WriteString(string(rune('0' + i/10)))
		diagnostic.WriteString(string(rune('0' + i%10)))
		diagnostic.WriteString(".go\n")
	}
	if _, ok := TryCompactPathListOutput([]string{"rg", "--files"}, []byte(diagnostic.String())); ok {
		t.Fatal("root-level diagnostic path-list line must fail open")
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
		{"fd", ".go", "src"},
		{"fdfind", "--extension", "go", "src"},
		{"fd", "-ego", "--max-depth=2", "src"},
		{"fd", "--hidden", "--type=file", "--exclude", "vendor", "src"},
		{"fd", "-HI", ".go", "src"},
		{"fd", "-ug", "--color", "never", ".go", "src"},
		{"fd", "-cnever", ".go", "src"},
		{"fd", "--", "-literal-pattern", "src"},
		{"find", ".reconc", "-maxdepth", "4", "-type", "f"},
		{"find", "internal", "-mindepth", "0", "-maxdepth", "2", "-name", "*.go", "-print"},
		{"find", ".", "-maxdepth", "0"},
	}
	for _, argv := range eligible {
		if !pathListOutputEligibleArgv(argv) {
			t.Fatalf("argv should be path-list eligible: %#v", argv)
		}
	}

	ineligible := [][]string{
		nil,
		{"fd", "--exec", "cat", "{}"},
		{"fd", "-x", "cat", "{}"},
		{"fd", "--print0", ".go"},
		{"fd", "--list-details", ".go"},
		{"fd", "--json", ".go"},
		{"fd", "-X", "cat", "{}"},
		{"fd", "-Hx", ".go"},
		{"fd", "-Q", ".go"},
		{"fd", "--extension"},
		{"fd", ""},
		{"find", ".reconc", "-type", "f"},
		{"find", ".reconc", "-maxdepth", "7", "-type", "f"},
		{"find", ".reconc", "-maxdepth"},
		{"find", ""},
		{"find", ".reconc", "-maxdepth", "x"},
		{"find", ".reconc", "-maxdepth", "999999999999999999999999999999"},
		{"find", ".reconc", "-maxdepth", "4", "-name"},
		{"find", ".reconc", "-maxdepth", "4", "-unknown"},
		{"find", ".reconc", "-maxdepth", "4", "-exec", "cat", "{}", ";"},
		{"find", ".reconc", "-maxdepth", "4", "-print0"},
		{"find", ".reconc", "-maxdepth", "4", "-printf", "%p\n"},
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

	if got := pathListOutputLabel([]string{"fd", "."}); got != "fd" {
		t.Fatalf("fd path-list label = %q", got)
	}
	if got := pathListOutputLabel([]string{"find", ".", "-maxdepth", "1"}); got != "find" {
		t.Fatalf("find path-list label = %q", got)
	}
	if findPathListBoundedDepthArg("") {
		t.Fatal("empty find depth must fail open")
	}
	if got := pathListOutputLabel([]string{"go", "test"}); got != "paths" {
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

	longFlags, ok := TryCompactWc([]string{"wc", "--lines", "--words", "--chars", "--bytes", "--max-line-length"}, []byte("      3      9     27     31     12 notes/report.txt\n"))
	if !ok || string(longFlags) != "notes/report.txt: 3L 9W 27Ch 31B 12Max\n" {
		t.Fatalf("wc long flags: ok=%v out=%q", ok, longFlags)
	}

	separator, ok := TryCompactWc([]string{"wc", "-l", "--", "-leading-name.txt"}, []byte("      7 -leading-name.txt\n"))
	if !ok || string(separator) != "-leading-name.txt: 7L\n" {
		t.Fatalf("wc separator path: ok=%v out=%q", ok, separator)
	}

	noPath, ok := TryCompactWc([]string{"wc", "-l"}, []byte("      42\n"))
	if !ok || string(noPath) != "42L\n" {
		t.Fatalf("wc no-path row: ok=%v out=%q", ok, noPath)
	}

	noCommonPrefix, ok := TryCompactWc([]string{"wc", "-l"}, []byte("      12 src/main.go\n      13 docs/spec.md\n      25 total\n"))
	if !ok {
		t.Fatal("multi-file wc without common prefix should compact")
	}
	wantNoPrefix := "src/main.go: 12L\ndocs/spec.md: 13L\ntotal: 25L\n"
	if string(noCommonPrefix) != wantNoPrefix {
		t.Fatalf("multi-file wc without prefix = %q, want %q", noCommonPrefix, wantNoPrefix)
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

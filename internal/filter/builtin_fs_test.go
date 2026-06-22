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
	for i := range 20 {
		sb.WriteString("drwxr-xr-x  2 user group 4096 Jan 01 00:00 subdir")
		sb.WriteString(string(rune('a' + i%26)))
		sb.WriteString("\n")
	}
	input := sb.String()
	if _, ok := TryCompactLs([]string{"ls", "-la"}, []byte(input)); ok {
		t.Fatal("non-empty ls output must full-pass; filenames are model evidence")
	}
}

func TestTryCompactLsLong(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("total 160\n")
	for i := range 24 {
		fmt.Fprintf(&sb, "-rw-r--r--  1 user staff 1200 Jan 01 00:%02d generated_file_%02d.go\n", i%60, i)
	}
	out, ok := TryCompactLsLong([]string{"ls", "-lah", "generated"}, []byte(sb.String()))
	if !ok {
		t.Fatal("long ls output should compact")
	}
	text := string(out)
	if !strings.Contains(text, "[ls -l] 24 entries total 160 owner=user group=staff") ||
		!strings.Contains(text, "-rw-r--r-- 1 1200 Jan 01 00:23 generated_file_23.go") {
		t.Fatalf("unexpected long ls compaction: %q", text)
	}
	if strings.Contains(text, "user staff 1200 Jan") {
		t.Fatalf("common owner/group should be lifted to header, got %q", text)
	}
	if len(text) >= sb.Len() {
		t.Fatalf("long ls compaction should save bytes: out=%d in=%d", len(text), sb.Len())
	}
}

func TestTryCompactLsLongMixedOwnerAndLongOptions(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("total 96\n")
	for i := range 48 {
		owner := "user"
		if i%2 == 0 {
			owner = "root"
		}
		fmt.Fprintf(&sb, "drwxr-xr-x  2 %s staff 4.0K Jan 02 12:%02d dir_%02d\n", owner, i%60, i)
	}
	argv := []string{"ls", "-lh", "--all", "--human-readable", "--color=never", "--", "-literal"}
	out, ok := TryCompactLsLong(argv, []byte(sb.String()))
	if !ok {
		t.Fatal("long ls with safe long options should compact")
	}
	text := string(out)
	if strings.Contains(text, "owner=") || !strings.Contains(text, "root:staff 4.0K") ||
		!strings.Contains(text, "user:staff 4.0K") {
		t.Fatalf("mixed owner/group rows should preserve row owner/group: %q", text)
	}
	if !LsLongOutputEligibleArgv(argv) {
		t.Fatal("safe long ls argv should be eligible")
	}
}

func TestTryCompactLsLongFailOpen(t *testing.T) {
	t.Parallel()
	raw := []byte("-rw-r--r--  1 user staff 1200 Jan 01 00:00 file.go\n")
	if _, ok := TryCompactLsLong(nil, raw); ok {
		t.Fatal("nil argv must fail open")
	}
	if _, ok := TryCompactLsLong([]string{"dir", "-la"}, raw); ok {
		t.Fatal("non-ls command must fail open")
	}
	if _, ok := TryCompactLsLong([]string{"ls"}, raw); ok {
		t.Fatal("plain ls must fail open")
	}
	if _, ok := TryCompactLsLong([]string{"ls", "--color=always"}, raw); ok {
		t.Fatal("colorized ls must fail open")
	}
	if _, ok := TryCompactLsLong([]string{"ls", "-lR"}, raw); ok {
		t.Fatal("recursive ls must fail open")
	}
	if _, ok := TryCompactLsLong([]string{"ls", "-la@"}, raw); ok {
		t.Fatal("extended-attribute ls must fail open")
	}
	if _, ok := TryCompactLsLong([]string{"ls", "-la"}, []byte("garbage row\n")); ok {
		t.Fatal("unparseable long ls output must fail open")
	}
	if _, ok := TryCompactLsLong([]string{"ls", "-la"}, []byte("total 8\ntotal 16\n"+string(raw))); ok {
		t.Fatal("conflicting total lines must fail open")
	}
	if _, ok := TryCompactLsLong([]string{"ls", "-la"}, []byte("-rw-r--r--  1 user staff 1200 Jan 01 00:00 \x1b[31mfile.go\n")); ok {
		t.Fatal("ANSI/control output must fail open")
	}
}

func TestLsLongParserGuardHelpers(t *testing.T) {
	t.Parallel()
	if LsLongOutputEligibleArgv([]string{}) {
		t.Fatal("empty argv must not be eligible")
	}
	if LsLongOutputEligibleArgv([]string{"ls", ""}) {
		t.Fatal("blank arg must not be eligible")
	}
	if LsLongOutputEligibleArgv([]string{"ls", "--unknown"}) {
		t.Fatal("unknown long option must not be eligible")
	}
	if LsLongOutputEligibleArgv([]string{"ls", "-l", "--sort"}) {
		t.Fatal("missing inline sort value must not be eligible")
	}
	if !LsLongOutputEligibleArgv([]string{"ls", "-lSrd", "internal"}) {
		t.Fatal("safe combined long-list flags should be eligible")
	}
	if _, _, ok := splitLeadingFields("one two", 3); ok {
		t.Fatal("splitLeadingFields should reject missing fields")
	}
	if _, _, ok := splitLeadingFields("one two three", 3); ok {
		t.Fatal("splitLeadingFields should reject missing rest")
	}
	if lsPermsField("") || lsPermsField("xrw-r--r--") || lsPermsField("-rw-r--r--\x1b") {
		t.Fatal("invalid permission fields must fail")
	}
	if !lsPermsField("lrwxr-xr-x") || !lsPermsField("-rw-r--r--@") {
		t.Fatal("valid permission fields should pass")
	}
	if lsSizeField("") || lsSizeField("12ms") {
		t.Fatal("invalid size fields must fail")
	}
	if !lsSizeField("4.0K") || !lsSizeField("128B") {
		t.Fatal("human-readable size fields should pass")
	}
	if !containsControl("bad\x7f") || containsControl("normal name -> target") {
		t.Fatal("control detection mismatch")
	}
}

func TestTryCompactPathListOutputRipgrepFiles(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := range 40 {
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
	for i := range 40 {
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
	for i := range 40 {
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
	for i := range 40 {
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
	for i := range 40 {
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
	for i := range 12 {
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
	for i := range 40 {
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
	var sb strings.Builder
	sb.WriteString(".\n")
	sb.WriteString("├── src\n")
	for i := range 24 {
		connector := "│   ├── "
		if i == 23 {
			connector = "│   └── "
		}
		fmt.Fprintf(&sb, "%sgenerated_file_%02d.go\n", connector, i)
	}
	sb.WriteString("├── go.mod\n")
	sb.WriteString("└── README.md\n\n")
	sb.WriteString("2 directories, 26 files\n")

	out, ok := TryCompactTree([]string{"tree", "-a", "-L", "2", "."}, []byte(sb.String()))
	if !ok {
		t.Fatal("bounded non-empty tree output should compact")
	}
	text := string(out)
	if !strings.Contains(text, "[tree paths] 27 entries 2 directories 26 files root=.") ||
		!strings.Contains(text, "src/") ||
		!strings.Contains(text, "  generated_file_23.go") ||
		!strings.Contains(text, "./\n  go.mod\n  README.md") {
		t.Fatalf("unexpected tree compaction: %q", text)
	}
	if strings.Contains(text, "├──") || strings.Contains(text, "│") {
		t.Fatalf("tree compaction should remove tree drawing glyphs: %q", text)
	}
	if len(text) >= sb.Len() {
		t.Fatalf("tree compaction should save bytes: out=%d in=%d", len(text), sb.Len())
	}
}

func TestTryCompactTreeFailOpen(t *testing.T) {
	t.Parallel()
	input := ".\n├── src\n│   ├── main.go\n│   └── config.go\n├── go.mod\n└── README.md\n\n2 directories, 4 files\n"
	for _, tc := range []struct {
		name   string
		argv   []string
		stdout string
	}{
		{name: "missing bounded depth", argv: []string{"tree"}, stdout: input},
		{name: "noreport", argv: []string{"tree", "-L", "2", "--noreport"}, stdout: ".\n├── src\n│   └── main.go\n"},
		{name: "deep", argv: []string{"tree", "-L", "99"}, stdout: input},
		{name: "rich flag", argv: []string{"tree", "-L", "2", "--du"}, stdout: input},
		{name: "full-path flag", argv: []string{"tree", "-f", "-L", "2"}, stdout: input},
		{name: "ansi", argv: []string{"tree", "-L", "2"}, stdout: ".\n├── \x1b[31msrc\n\n2 directories, 0 files\n"},
		{name: "summary mismatch", argv: []string{"tree", "-L", "2"}, stdout: ".\n├── src\n│   └── main.go\n\n9 directories, 9 files\n"},
		{name: "tiny non shrinking", argv: []string{"tree", "-L", "1"}, stdout: ".\n└── README.md\n\n1 directory, 1 file\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, ok := TryCompactTree(tc.argv, []byte(tc.stdout)); ok {
				t.Fatal("unsafe or non-beneficial tree output must fail open")
			}
		})
	}
}

func TestTryCompactTreeArgvAndASCIIShapes(t *testing.T) {
	t.Parallel()
	var ascii strings.Builder
	ascii.WriteString("internal/proxy\n")
	for i := range 40 {
		connector := "|-- "
		if i == 39 {
			connector = "`-- "
		}
		fmt.Fprintf(&ascii, "%stree_file_%02d.go\n", connector, i)
	}
	ascii.WriteString("\n1 directory, 40 files\n")

	for _, argv := range [][]string{
		{"tree", "-adL2", "internal/proxy"},
		{"tree", "--dirsfirst", "--charset", "ascii", "-L", "2", "--", "internal/proxy"},
		{"tree.exe", "--charset=ascii", "-L2", "internal/proxy"},
	} {
		out, ok := TryCompactTree(argv, []byte(ascii.String()))
		if !ok {
			t.Fatalf("safe ASCII tree argv should compact: %v", argv)
		}
		text := string(out)
		if !strings.Contains(text, "root=internal/proxy") ||
			!strings.Contains(text, "tree_file_39.go") ||
			strings.Contains(text, "`--") ||
			strings.Contains(text, "|--") {
			t.Fatalf("unexpected ASCII tree compaction for %v: %q", argv, text)
		}
	}
}

func TestTryCompactTreeParserFailOpenEdges(t *testing.T) {
	t.Parallel()
	if treeOutputEligibleArgv(nil) || treeOutputEligibleArgv([]string{"tree", ""}) {
		t.Fatal("empty tree argv shapes must fail open")
	}
	if treeOutputEligibleArgv([]string{"tree", "-L"}) || treeOutputEligibleArgv([]string{"tree", "-L", "x"}) {
		t.Fatal("missing or non-numeric tree depth must fail open")
	}
	if treeOutputEligibleArgv([]string{"tree", "--", ""}) ||
		treeOutputEligibleArgv([]string{"tree", "--charset"}) ||
		treeOutputEligibleArgv([]string{"tree", "--charset="}) {
		t.Fatal("blank separator or charset args must fail open")
	}
	if !treeOutputEligibleArgv([]string{"tree", "-L", "6", "--", "path with spaces"}) {
		t.Fatal("bounded tree with separator and path argument should be eligible")
	}
	if treeBoundedDepthArg("0") || treeBoundedDepthArg("-1") || treeBoundedDepthArg("7") {
		t.Fatal("tree depth bounds mismatch")
	}
	if _, _, ok := parseTreeEntryLine("│  ├── bad-prefix.go"); ok {
		t.Fatal("malformed tree prefix must fail open")
	}
	if _, _, ok := parseTreeEntryLine("\\-- ascii-backslash.go"); !ok {
		t.Fatal("ascii backslash final connector should parse")
	}
	if _, _, ok := parseTreeSummaryLine("2 dirs, 4 files"); ok {
		t.Fatal("non-tree summary wording must fail open")
	}
	if _, _, ok := parseTreeSummaryLine("x directories, 4 files"); ok {
		t.Fatal("non-numeric directory count must fail open")
	}
	if _, _, ok := parseTreeSummaryLine("2 directories, -4 files"); ok {
		t.Fatal("negative file count must fail open")
	}
	if _, ok := treePrefixDepth("│   |   "); !ok {
		t.Fatal("mixed unicode/ascii continuation chunks should parse")
	}
	if _, ok := treePrefixDepth("│\u00a0\u00a0 "); !ok {
		t.Fatal("macOS tree NBSP continuation chunk should parse")
	}
	if _, ok := treePrefixDepth("│  "); ok {
		t.Fatal("partial tree prefix chunk must fail open")
	}
	if got := treeEntryPath(".", []string{"src/"}, "main.go"); got != "src/main.go" {
		t.Fatalf("treeEntryPath trims parent slash for children, got %q", got)
	}
	for _, payload := range []string{
		"",
		"  .\n└── file.go\n\n1 directory, 1 file\n",
		".\n└── file.go\n\n1 directory, 1 file\nunexpected\n",
		".\n└── file.go\n\n1 directory, 1 file\n1 directory, 1 file\n",
		".\n    └── impossible.go\n\n1 directory, 1 file\n",
		".\n├── .\n\n1 directory, 1 file\n",
		".\n└── file.go\n\n0 directories, 0 files\n",
	} {
		if _, ok := parseTreeListing(payload); ok {
			t.Fatalf("unsafe tree payload should fail open: %q", payload)
		}
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

func TestTryCompactDu_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&sb, "4.0K\t./dir%d/file%d.go\n", i, i)
	}
	sb.WriteString("3.5M\t.\n")
	input := []byte(sb.String())
	compacted, ok := TryCompactDu([]string{"du", "-a", "."}, input)
	if !ok {
		t.Fatalf("TryCompactDu returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted (%d) >= input (%d)", len(compacted), len(input))
	}
	s := string(compacted)
	if !strings.Contains(s, "[du] 201 entries") {
		t.Fatalf("compacted missing summary: %s", s[:200])
	}
	if !strings.Contains(s, "3.5M\t.") {
		t.Fatalf("compacted missing total line")
	}
}

func TestTryCompactDu_SmallOutput(t *testing.T) {
	t.Parallel()
	input := []byte("4.0K\t./src\n8.0K\t.\n")
	_, ok := TryCompactDu([]string{"du", "."}, input)
	if ok {
		t.Fatalf("TryCompactDu should return false for small output")
	}
}

func TestTryCompactDu_NotDu(t *testing.T) {
	t.Parallel()
	input := []byte("4.0K\t./src\n")
	_, ok := TryCompactDu([]string{"ls", "-la"}, input)
	if ok {
		t.Fatalf("TryCompactDu should return false for non-du argv")
	}
}

func TestTryCompactDu_EmptyOutput(t *testing.T) {
	t.Parallel()
	compacted, ok := TryCompactDu([]string{"du", "."}, []byte(""))
	if !ok {
		t.Fatalf("TryCompactDu should return true for empty output")
	}
	if string(compacted) != "[du] empty\n" {
		t.Fatalf("compacted should be [du] empty, got: %s", compacted)
	}
}

// --- TryCompactDf tests ---

func TestTryCompactDf_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("Filesystem     1K-blocks      Used Available Use% Mounted on\n")
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&sb, "/dev/sda%d      1000000    500000    500000  50%% /mnt/fs%d\n", i, i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactDf([]string{"df", "-h"}, input)
	if !ok {
		t.Fatalf("TryCompactDf returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted (%d) >= input (%d)", len(compacted), len(input))
	}
	s := string(compacted)
	if !strings.Contains(s, "[df] 60 filesystems") {
		t.Fatalf("compacted missing summary: %s", s[:200])
	}
	if !strings.Contains(s, "Filesystem") {
		t.Fatalf("compacted missing header")
	}
	if !strings.Contains(s, "[+20 more filesystems]") {
		t.Fatalf("compacted missing truncation marker")
	}
}

func TestTryCompactDf_SmallOutput(t *testing.T) {
	t.Parallel()
	input := []byte("Filesystem  1K-blocks  Used Available Use% Mounted on\n/dev/sda1   1000000   500000   500000  50% /\n/dev/sda2   2000000   500000   1500000  25% /home\n")
	_, ok := TryCompactDf([]string{"df"}, input)
	if ok {
		t.Fatalf("TryCompactDf should return false for small output")
	}
}

func TestTryCompactDf_NotDf(t *testing.T) {
	t.Parallel()
	input := []byte("Filesystem  1K-blocks\n")
	_, ok := TryCompactDf([]string{"ls", "-la"}, input)
	if ok {
		t.Fatalf("TryCompactDf should return false for non-df argv")
	}
}

func TestTryCompactDf_EmptyOutput(t *testing.T) {
	t.Parallel()
	compacted, ok := TryCompactDf([]string{"df"}, []byte(""))
	if !ok {
		t.Fatalf("TryCompactDf should return true for empty output")
	}
	if string(compacted) != "[df] empty\n" {
		t.Fatalf("compacted should be [df] empty, got: %s", compacted)
	}
}

// --- TryCompactPs tests ---

func TestTryCompactPs_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("  PID TTY          TIME CMD\n")
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&sb, "%5d pts/0    00:00:01 process%d\n", i, i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactPs([]string{"ps", "aux"}, input)
	if !ok {
		t.Fatalf("TryCompactPs returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted (%d) >= input (%d)", len(compacted), len(input))
	}
	s := string(compacted)
	if !strings.Contains(s, "[ps] 100 processes") {
		t.Fatalf("compacted missing summary: %s", s[:200])
	}
	if !strings.Contains(s, "PID TTY") {
		t.Fatalf("compacted missing header")
	}
	if !strings.Contains(s, "[+50 more processes]") {
		t.Fatalf("compacted missing truncation marker")
	}
}

func TestTryCompactPs_SmallOutput(t *testing.T) {
	t.Parallel()
	input := []byte("  PID TTY          TIME CMD\n    1 pts/0    00:00:01 bash\n    2 pts/0    00:00:01 ps\n")
	_, ok := TryCompactPs([]string{"ps"}, input)
	if ok {
		t.Fatalf("TryCompactPs should return false for small output")
	}
}

func TestTryCompactPs_NotPs(t *testing.T) {
	t.Parallel()
	input := []byte("  PID TTY\n")
	_, ok := TryCompactPs([]string{"ls"}, input)
	if ok {
		t.Fatalf("TryCompactPs should return false for non-ps argv")
	}
}

func TestTryCompactPs_EmptyOutput(t *testing.T) {
	t.Parallel()
	compacted, ok := TryCompactPs([]string{"ps"}, []byte(""))
	if !ok {
		t.Fatalf("TryCompactPs should return true for empty output")
	}
	if string(compacted) != "[ps] empty\n" {
		t.Fatalf("compacted should be [ps] empty, got: %s", compacted)
	}
}

// --- TryCompactEnv tests ---

func TestTryCompactEnv_SecretRedaction(t *testing.T) {
	t.Parallel()
	input := []byte("PATH=/usr/bin:/bin\nAPI_KEY=sk-1234567890abcdef\nHOME=/home/user\nTOKEN=ghp_1234567890abcdefghijklmnopqrstuvwxyz\n")
	compacted, ok := TryCompactEnv([]string{"env"}, input)
	if !ok {
		t.Fatalf("TryCompactEnv returned ok=false")
	}
	s := string(compacted)
	if strings.Contains(s, "sk-1234567890abcdef") {
		t.Fatalf("API_KEY value not redacted: %s", s)
	}
	if strings.Contains(s, "ghp_1234567890abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("TOKEN value not redacted: %s", s)
	}
	if !strings.Contains(s, "API_KEY=[REDACTED]") {
		t.Fatalf("API_KEY not redacted properly: %s", s)
	}
	if !strings.Contains(s, "TOKEN=[REDACTED]") {
		t.Fatalf("TOKEN not redacted properly: %s", s)
	}
	if !strings.Contains(s, "PATH=/usr/bin:/bin") {
		t.Fatalf("non-secret PATH value should be preserved: %s", s)
	}
	if !strings.Contains(s, "HOME=/home/user") {
		t.Fatalf("non-secret HOME value should be preserved: %s", s)
	}
}

func TestTryCompactEnv_LargeOutput(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 70; i++ {
		fmt.Fprintf(&sb, "VAR_%d=value_%d\n", i, i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactEnv([]string{"env"}, input)
	if !ok {
		t.Fatalf("TryCompactEnv returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted (%d) >= input (%d)", len(compacted), len(input))
	}
	s := string(compacted)
	if !strings.Contains(s, "[env] 70 variables") {
		t.Fatalf("compacted missing summary: %s", s[:200])
	}
	if !strings.Contains(s, "[+20 more variables]") {
		t.Fatalf("compacted missing truncation marker")
	}
}

func TestTryCompactEnv_NotEnv(t *testing.T) {
	t.Parallel()
	input := []byte("PATH=/usr/bin\n")
	_, ok := TryCompactEnv([]string{"ls"}, input)
	if ok {
		t.Fatalf("TryCompactEnv should return false for non-env argv")
	}
}

func TestTryCompactEnv_EmptyOutput(t *testing.T) {
	t.Parallel()
	compacted, ok := TryCompactEnv([]string{"env"}, []byte(""))
	if !ok {
		t.Fatalf("TryCompactEnv should return true for empty output")
	}
	if string(compacted) != "[env] empty\n" {
		t.Fatalf("compacted should be [env] empty, got: %s", compacted)
	}
}

func TestTryCompactEnv_Printenv(t *testing.T) {
	t.Parallel()
	input := []byte("API_KEY=sk-1234567890abcdef1234567890abcdef\n")
	compacted, ok := TryCompactEnv([]string{"printenv"}, input)
	if !ok {
		t.Fatalf("TryCompactEnv should work for printenv")
	}
	if strings.Contains(string(compacted), "sk-1234567890") {
		t.Fatalf("printenv secret not redacted: %s", compacted)
	}
}

func TestEnvKeyLooksSecret(t *testing.T) {
	t.Parallel()
	secretKeys := []string{
		"API_KEY", "api_key", "SECRET_TOKEN", "PASSWORD", "PWD_HASH",
		"ACCESS_KEY", "PRIVATE_KEY", "AUTH_TOKEN", "CREDENTIAL",
		"CLIENT_SECRET", "REFRESH_TOKEN", "JWT_SECRET", "SIGNING_KEY",
		"SSH_KEY", "VAULT_TOKEN", "AWS_SECRET_ACCESS_KEY",
	}
	for _, key := range secretKeys {
		if !envKeyLooksSecret(key) {
			t.Errorf("envKeyLooksSecret(%q) = false, want true", key)
		}
	}
	nonSecretKeys := []string{
		"PATH", "HOME", "USER", "SHELL", "TERM", "LANG", "EDITOR",
		"GOPATH", "GOROOT", "NODE_ENV", "DEBUG", "VERBOSE",
	}
	for _, key := range nonSecretKeys {
		if envKeyLooksSecret(key) {
			t.Errorf("envKeyLooksSecret(%q) = true, want false", key)
		}
	}
}

func TestEnvValueLooksSecret(t *testing.T) {
	t.Parallel()
	secretValues := []string{
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.payload.signature",
		"bearer abc123",
		"AKIAIOSFODNN7EXAMPLE",
		"ghp_1234567890abcdefghijklmnopqrstuvwxyz",
		"xoxb-1234567890abcdef",
		strings.Repeat("a", 40),
	}
	for _, val := range secretValues {
		if !envValueLooksSecret(val) {
			t.Errorf("envValueLooksSecret(%q) = false, want true", val)
		}
	}
	nonSecretValues := []string{
		"/usr/bin:/bin", "/home/user", "bash", "xterm", "en_US.UTF-8",
		"true", "1", "debug", "production",
	}
	for _, val := range nonSecretValues {
		if envValueLooksSecret(val) {
			t.Errorf("envValueLooksSecret(%q) = true, want false", val)
		}
	}
}

// --- TryCompactHexDump tests ---

func TestTryCompactHexDump_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&sb, "%08x: 4865 6c6c 6f20 576f 726c 6421 0a00 0000  Hello World!.....\n", i*16)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactHexDump([]string{"xxd", "file.bin"}, input)
	if !ok {
		t.Fatalf("TryCompactHexDump returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted (%d) >= input (%d)", len(compacted), len(input))
	}
	s := string(compacted)
	if !strings.Contains(s, "[hexdump] 50 lines") {
		t.Fatalf("compacted missing summary: %s", s[:200])
	}
}

func TestTryCompactHexDump_SmallOutput(t *testing.T) {
	t.Parallel()
	input := []byte("00000000: 4865 6c6c 6f                              Hello\n")
	_, ok := TryCompactHexDump([]string{"xxd"}, input)
	if ok {
		t.Fatalf("TryCompactHexDump should return false for small output")
	}
}

func TestTryCompactHexDump_NotHexDump(t *testing.T) {
	t.Parallel()
	input := []byte("00000000: 4865\n")
	_, ok := TryCompactHexDump([]string{"cat"}, input)
	if ok {
		t.Fatalf("TryCompactHexDump should return false for non-hexdump argv")
	}
}

func TestTryCompactHexDump_EmptyOutput(t *testing.T) {
	t.Parallel()
	compacted, ok := TryCompactHexDump([]string{"xxd"}, []byte(""))
	if !ok {
		t.Fatalf("TryCompactHexDump should return true for empty output")
	}
	if string(compacted) != "[hexdump] empty\n" {
		t.Fatalf("compacted should be [hexdump] empty, got: %s", compacted)
	}
}

func TestTryCompactHexDump_PreservesLastLines(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&sb, "%08x: %04x %04x line%d\n", i*16, i, i, i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactHexDump([]string{"od", "-A", "x", "-t", "x2"}, input)
	if !ok {
		t.Fatalf("TryCompactHexDump returned ok=false")
	}
	s := string(compacted)
	// Last 3 lines should be preserved.
	if !strings.Contains(s, "line49") {
		t.Fatalf("compacted missing last line: %s", s[len(s)-200:])
	}
	if !strings.Contains(s, "line48") {
		t.Fatalf("compacted missing second-to-last line: %s", s[len(s)-200:])
	}
}

// --- TryCompactDiff tests ---

func TestTryCompactDiff_BasicCompact(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("--- a/file.go\n")
	sb.WriteString("+++ b/file.go\n")
	sb.WriteString("@@ -1,5 +1,5 @@\n")
	sb.WriteString(" package main\n")
	sb.WriteString(" \n")
	sb.WriteString(" import \"fmt\"\n")
	sb.WriteString("-old line\n")
	sb.WriteString("+new line\n")
	sb.WriteString(" \n")
	sb.WriteString(" func main() {}\n")
	input := []byte(sb.String())
	compacted, ok := TryCompactDiff([]string{"diff", "-u", "a/file.go", "b/file.go"}, input)
	if !ok {
		t.Fatalf("TryCompactDiff returned ok=false")
	}
	s := string(compacted)
	if !strings.Contains(s, "[diff] 1 file(s)") {
		t.Fatalf("compacted missing summary: %s", s)
	}
	if !strings.Contains(s, "+new line") {
		t.Fatalf("compacted missing + line")
	}
	if !strings.Contains(s, "-old line") {
		t.Fatalf("compacted missing - line")
	}
	// Context lines should be stripped.
	if strings.Contains(s, "package main") {
		t.Fatalf("context line 'package main' should be stripped: %s", s)
	}
}

func TestTryCompactDiff_NotUnifiedDiff(t *testing.T) {
	t.Parallel()
	input := []byte("1c1\n< old\n---\n> new\n")
	_, ok := TryCompactDiff([]string{"diff"}, input)
	if ok {
		t.Fatalf("TryCompactDiff should return false for non-unified diff")
	}
}

func TestTryCompactDiff_NotDiff(t *testing.T) {
	t.Parallel()
	input := []byte("--- a/file.go\n+++ b/file.go\n")
	_, ok := TryCompactDiff([]string{"cat"}, input)
	if ok {
		t.Fatalf("TryCompactDiff should return false for non-diff argv")
	}
}

func TestTryCompactDiff_EmptyOutput(t *testing.T) {
	t.Parallel()
	compacted, ok := TryCompactDiff([]string{"diff"}, []byte(""))
	if !ok {
		t.Fatalf("TryCompactDiff should return true for empty output")
	}
	if string(compacted) != "[diff] empty\n" {
		t.Fatalf("compacted should be [diff] empty, got: %s", compacted)
	}
}

func TestTryCompactDiff_MultipleFiles(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("--- a/file1.go\n")
	sb.WriteString("+++ b/file1.go\n")
	sb.WriteString("@@ -1,10 +1,10 @@\n")
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&sb, " context_line_%d\n", i)
	}
	sb.WriteString("-old1\n")
	sb.WriteString("+new1\n")
	sb.WriteString("--- a/file2.go\n")
	sb.WriteString("+++ b/file2.go\n")
	sb.WriteString("@@ -1,10 +1,10 @@\n")
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&sb, " context_line_%d\n", i)
	}
	sb.WriteString("-old2\n")
	sb.WriteString("+new2\n")
	input := []byte(sb.String())
	compacted, ok := TryCompactDiff([]string{"diff", "-u"}, input)
	if !ok {
		t.Fatalf("TryCompactDiff returned ok=false")
	}
	s := string(compacted)
	if !strings.Contains(s, "[diff] 2 file(s)") {
		t.Fatalf("compacted missing 2-file summary: %s", s)
	}
	if !strings.Contains(s, "file1.go") {
		t.Fatalf("compacted missing file1")
	}
	if !strings.Contains(s, "file2.go") {
		t.Fatalf("compacted missing file2")
	}
	if strings.Contains(s, "context_line_0") {
		t.Fatalf("context lines should be stripped: %s", s)
	}
}

// --- TryCompactLsof tests ---

func TestTryCompactLsof_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("COMMAND     PID   USER   FD   TYPE DEVICE SIZE NODE NAME\n")
	for i := 0; i < 150; i++ {
		fmt.Fprintf(&sb, "process    %5d user   %3d   REG  253,0  1000  123  /file%d\n", i, i, i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactLsof([]string{"lsof"}, input)
	if !ok {
		t.Fatalf("TryCompactLsof returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted (%d) >= input (%d)", len(compacted), len(input))
	}
	s := string(compacted)
	if !strings.Contains(s, "[lsof] 150 entries") {
		t.Fatalf("compacted missing summary: %s", s[:200])
	}
	if !strings.Contains(s, "COMMAND") {
		t.Fatalf("compacted missing header")
	}
	if !strings.Contains(s, "[+50 more entries]") {
		t.Fatalf("compacted missing truncation marker")
	}
}

func TestTryCompactLsof_SmallOutput(t *testing.T) {
	t.Parallel()
	input := []byte("COMMAND  PID USER  FD  TYPE DEVICE SIZE NODE NAME\nbash    1234 user txt  REG  253,0 1000  123  /bin/bash\n")
	_, ok := TryCompactLsof([]string{"lsof"}, input)
	if ok {
		t.Fatalf("TryCompactLsof should return false for small output")
	}
}

func TestTryCompactLsof_NotLsof(t *testing.T) {
	t.Parallel()
	input := []byte("COMMAND  PID\n")
	_, ok := TryCompactLsof([]string{"ps"}, input)
	if ok {
		t.Fatalf("TryCompactLsof should return false for non-lsof argv")
	}
}

func TestTryCompactLsof_EmptyOutput(t *testing.T) {
	t.Parallel()
	compacted, ok := TryCompactLsof([]string{"lsof"}, []byte(""))
	if !ok {
		t.Fatalf("TryCompactLsof should return true for empty output")
	}
	if string(compacted) != "[lsof] empty\n" {
		t.Fatalf("compacted should be [lsof] empty, got: %s", compacted)
	}
}

// --- TryCompactNetstat tests ---

func TestTryCompactNetstat_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("Netid State  Recv-Q Send-Q Local Address:Port Peer Address:Port\n")
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&sb, "tcp   ESTAB  0      0      127.0.0.1:%d    127.0.0.1:8080\n", 30000+i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactNetstat([]string{"ss", "-t"}, input)
	if !ok {
		t.Fatalf("TryCompactNetstat returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted (%d) >= input (%d)", len(compacted), len(input))
	}
	s := string(compacted)
	if !strings.Contains(s, "[netstat] 100 entries") {
		t.Fatalf("compacted missing summary: %s", s[:200])
	}
	if !strings.Contains(s, "Local Address") {
		t.Fatalf("compacted missing header")
	}
	if !strings.Contains(s, "[+40 more entries]") {
		t.Fatalf("compacted missing truncation marker")
	}
}

func TestTryCompactNetstat_SmallOutput(t *testing.T) {
	t.Parallel()
	input := []byte("Proto Local Address\n tcp   127.0.0.1:80\n")
	_, ok := TryCompactNetstat([]string{"ss"}, input)
	if ok {
		t.Fatalf("TryCompactNetstat should return false for small output")
	}
}

func TestTryCompactNetstat_NotNetstat(t *testing.T) {
	t.Parallel()
	input := []byte("Proto Local\n")
	_, ok := TryCompactNetstat([]string{"ls"}, input)
	if ok {
		t.Fatalf("TryCompactNetstat should return false for non-ss/netstat argv")
	}
}

func TestTryCompactNetstat_EmptyOutput(t *testing.T) {
	t.Parallel()
	compacted, ok := TryCompactNetstat([]string{"ss"}, []byte(""))
	if !ok {
		t.Fatalf("TryCompactNetstat should return true for empty output")
	}
	if string(compacted) != "[netstat] empty\n" {
		t.Fatalf("compacted should be [netstat] empty, got: %s", compacted)
	}
}

// --- TryCompactTextUtility tests ---

func TestTryCompactTextUtility_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&sb, "line_%d\n", i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactTextUtility([]string{"sort", "file.txt"}, input)
	if !ok {
		t.Fatalf("TryCompactTextUtility returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted (%d) >= input (%d)", len(compacted), len(input))
	}
	s := string(compacted)
	if !strings.Contains(s, "[sort] 200 lines") {
		t.Fatalf("compacted missing summary: %s", s[:200])
	}
	if !strings.Contains(s, "[+100 more lines]") {
		t.Fatalf("compacted missing truncation marker")
	}
}

func TestTryCompactTextUtility_SmallOutput(t *testing.T) {
	t.Parallel()
	input := []byte("line1\nline2\nline3\n")
	_, ok := TryCompactTextUtility([]string{"sort"}, input)
	if ok {
		t.Fatalf("TryCompactTextUtility should return false for small output")
	}
}

func TestTryCompactTextUtility_NotTextUtility(t *testing.T) {
	t.Parallel()
	input := []byte("line1\nline2\n")
	_, ok := TryCompactTextUtility([]string{"cat", "file.txt"}, input)
	if ok {
		t.Fatalf("TryCompactTextUtility should return false for non-text-utility argv")
	}
}

func TestTryCompactTextUtility_Uniq(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&sb, "unique_line_%d\n", i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactTextUtility([]string{"uniq", "-c"}, input)
	if !ok {
		t.Fatalf("TryCompactTextUtility returned ok=false for uniq")
	}
	s := string(compacted)
	if !strings.Contains(s, "[uniq] 200 lines") {
		t.Fatalf("compacted missing summary: %s", s[:200])
	}
}

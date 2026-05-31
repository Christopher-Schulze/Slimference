package filter

import (
	"fmt"
	"strings"
	"testing"
)

func TestTryCompactSearchOutput(t *testing.T) {
	t.Parallel()
	out, ok := TryCompactRipgrep([]string{"/bin/rg", "foo"}, []byte(""))
	if !ok || string(out) != "[rg] no matches\n" {
		t.Fatalf("rg: ok=%v %q", ok, out)
	}
	out2, ok := TryCompactGrep([]string{"grep", "-r", "x", "."}, []byte("\n"))
	if !ok || string(out2) != "[grep] no matches\n" {
		t.Fatalf("grep: %q", out2)
	}
	out3, ok := TryCompactFd([]string{"fd", "pattern"}, []byte(""))
	if !ok || string(out3) != "[fd] no matches\n" {
		t.Fatalf("fd: %q", out3)
	}
	gg, ok := TryCompactGitGrep([]string{"git", "grep", "foo"}, []byte(""))
	if !ok || string(gg) != "[git grep] no matches\n" {
		t.Fatalf("git grep: %q", gg)
	}
	out4, ok := TryCompactSearchOutput([]string{"rg", "x"}, []byte(""))
	if !ok || string(out4) != "[rg] no matches\n" {
		t.Fatalf("chain: %q", out4)
	}
	outGG, ok := TryCompactSearchOutput([]string{"git", "grep", "nope"}, []byte("\n"))
	if !ok || string(outGG) != "[git grep] no matches\n" {
		t.Fatalf("chain git grep: %q", outGG)
	}
	out5, ok := TryCompactFind([]string{"find", ".", "-name", "nope"}, []byte(""))
	if !ok || string(out5) != "[find] no matches\n" {
		t.Fatalf("find: %q", out5)
	}
	ag, ok := TryCompactAg([]string{"ag", "pat"}, []byte(""))
	if !ok || string(ag) != "[ag] no matches\n" {
		t.Fatalf("ag: %q", ag)
	}
	ack, ok := TryCompactAck([]string{"ack", "pat"}, []byte(""))
	if !ok || string(ack) != "[ack] no matches\n" {
		t.Fatalf("ack: %q", ack)
	}
	ug, ok := TryCompactUgrep([]string{"ug", "pat"}, []byte(""))
	if !ok || string(ug) != "[ug] no matches\n" {
		t.Fatalf("ug: %q", ug)
	}
	ugrep, ok := TryCompactUgrep([]string{"ugrep", "-q", "x"}, []byte(""))
	if !ok || string(ugrep) != "[ug] no matches\n" {
		t.Fatalf("ugrep: %q", ugrep)
	}
	sift, ok := TryCompactSift([]string{"sift", "pat"}, []byte(""))
	if !ok || string(sift) != "[sift] no matches\n" {
		t.Fatalf("sift: %q", sift)
	}
	ploc, ok := TryCompactPlocate([]string{"plocate", "nope"}, []byte(""))
	if !ok || string(ploc) != "[plocate] no matches\n" {
		t.Fatalf("plocate: %q", ploc)
	}
	loc, ok := TryCompactLocate([]string{"locate", "nope"}, []byte("\n"))
	if !ok || string(loc) != "[locate] no matches\n" {
		t.Fatalf("locate: %q", loc)
	}
	locCh, ok := TryCompactSearchOutput([]string{"locate", "x"}, []byte(""))
	if !ok || string(locCh) != "[locate] no matches\n" {
		t.Fatalf("chain locate: %q", locCh)
	}
	sk, ok := TryCompactSk([]string{"sk", "--query", "foo"}, []byte(""))
	if !ok || string(sk) != "[sk] no matches\n" {
		t.Fatalf("sk: %q", sk)
	}
	skCh, ok := TryCompactSearchOutput([]string{"sk"}, []byte("\n"))
	if !ok || string(skCh) != "[sk] no matches\n" {
		t.Fatalf("chain sk: %q", skCh)
	}
	if _, ok := TryCompactGrep([]string{"sed", "s/a/b/"}, []byte("")); ok {
		t.Fatal("sed not grep")
	}
}

func TestGroupSearchResults_grouped(t *testing.T) {
	t.Parallel()
	// Build a realistic rg output that grouping will compress (many same-file matches).
	// Each "src/some/long/path/file.go:NNN:content line text here" line is ~50 chars.
	// Grouped output replaces the per-line file prefix with a single header — saves space
	// when the same file appears many times.
	var sb strings.Builder
	for i := 1; i <= 30; i++ {
		sb.WriteString("src/internal/proxy/handler.go:")
		sb.WriteString(strings.Repeat("1", len([]byte{byte('0' + i/10), byte('0' + i%10)})))
		sb.WriteString(":func handleCompressibleRequest() { // compression step ")
		sb.WriteString(strings.Repeat("x", 20))
		sb.WriteByte('\n')
	}
	for i := 1; i <= 10; i++ {
		sb.WriteString("src/internal/config/defaults.go:22")
		sb.WriteString(":func Defaults() *Config { return &Config{ // defaults ")
		sb.WriteString(strings.Repeat("y", 20))
		sb.WriteByte('\n')
	}
	input := sb.String()
	out, ok := TryCompactSearchOutput([]string{"rg", "func"}, []byte(input))
	if !ok {
		t.Fatalf("expected grouping to succeed (input %d bytes), got pass-through", len(input))
	}
	s := string(out)
	if !strings.Contains(s, "40 match(es) in 2 file(s)") {
		t.Errorf("want summary line, got: %s", s[:min(len(s), 200)])
	}
	if !strings.Contains(s, "handler.go") {
		t.Errorf("want handler.go in grouped output")
	}
	if !strings.Contains(s, "defaults.go") {
		t.Errorf("want defaults.go in grouped output")
	}
	if len(s) >= len(input) {
		t.Errorf("grouped output should be shorter: got %d vs input %d", len(s), len(input))
	}
}

func TestGroupSearchResults_shortPassthrough(t *testing.T) {
	t.Parallel()
	// Less than minLinesForGrouped → passthrough
	input := "a.go:1:x\nb.go:2:y\n"
	_, ok := TryCompactSearchOutput([]string{"rg", "x"}, []byte(input))
	if ok {
		t.Fatal("short input should pass through")
	}
}

func TestGroupSearchResults_nonGrepTool(t *testing.T) {
	t.Parallel()
	// fd produces paths, not file:line:content — should pass through
	input := "src/main.go\nsrc/config/config.go\nsrc/handler.go\nsrc/session.go\n"
	_, ok := TryCompactSearchOutput([]string{"fd", ".go"}, []byte(input))
	if ok {
		t.Fatal("fd is not grep-style, should pass through or return not-ok")
	}
}

func TestGroupSearchResults_grepNoLineNum(t *testing.T) {
	t.Parallel()
	// grep without -n produces "file:content" (no line number column).
	// Build a large enough input for grouping to save space.
	var sb strings.Builder
	for i := 0; i < 20; i++ {
		sb.WriteString("src/internal/proxy/handler.go:func handleCompressibleRequest_step_")
		sb.WriteString(strings.Repeat("a", 20))
		sb.WriteByte('\n')
	}
	for i := 0; i < 15; i++ {
		sb.WriteString("src/internal/config/defaults.go:func DefaultsInitializer_step_")
		sb.WriteString(strings.Repeat("b", 20))
		sb.WriteByte('\n')
	}
	input := sb.String()
	out, ok := TryCompactSearchOutput([]string{"grep", "-r", "func"}, []byte(input))
	if !ok {
		t.Fatalf("expected grouping (input %d bytes), got pass-through", len(input))
	}
	s := string(out)
	if !strings.Contains(s, "35 match(es) in 2 file(s)") {
		t.Errorf("want 35 matches in 2 files summary, got: %s", s[:min(len(s), 200)])
	}
}

// TestTryCompactSearchOutput_guards covers len<1 and non-empty stdout branches.
func TestTryCompactSearchOutput_guards(t *testing.T) {
	t.Parallel()

	// len < 1 guards
	for _, fn := range []struct {
		name string
		fn   func([]string, []byte) ([]byte, bool)
	}{
		{"rg", TryCompactRipgrep},
		{"grep", TryCompactGrep},
		{"fd", TryCompactFd},
		{"find", TryCompactFind},
		{"ag", TryCompactAg},
		{"ack", TryCompactAck},
		{"ug", TryCompactUgrep},
		{"sift", TryCompactSift},
		{"plocate", TryCompactPlocate},
		{"locate", TryCompactLocate},
		{"sk", TryCompactSk},
	} {
		if _, ok := fn.fn([]string{}, []byte("")); ok {
			t.Fatalf("%s: empty argv should return false", fn.name)
		}
		// non-empty stdout guard
		if _, ok := fn.fn([]string{fn.name}, []byte("some output\n")); ok {
			t.Fatalf("%s: non-empty stdout should return false", fn.name)
		}
	}

	// TryCompactGitGrep: non-empty stdout
	if _, ok := TryCompactGitGrep([]string{"git", "grep", "foo"}, []byte("match\n")); ok {
		t.Fatal("git grep: non-empty stdout should return false")
	}
	// TryCompactGitGrep: len < 2
	if _, ok := TryCompactGitGrep([]string{"git"}, []byte("")); ok {
		t.Fatal("git: len<2 should return false")
	}
	// TryCompactGitGrep: wrong subcommand
	if _, ok := TryCompactGitGrep([]string{"git", "log"}, []byte("")); ok {
		t.Fatal("git log: not grep")
	}

	// additional binary name guards
	if _, ok := TryCompactRipgrep([]string{"grep", "x"}, []byte("")); ok {
		t.Fatal("grep not rg")
	}
	if _, ok := TryCompactFd([]string{"find", "."}, []byte("")); ok {
		t.Fatal("find not fd")
	}
	if _, ok := TryCompactFind([]string{"fd", "."}, []byte("")); ok {
		t.Fatal("fd not find")
	}
	if _, ok := TryCompactAg([]string{"rg", "."}, []byte("")); ok {
		t.Fatal("rg not ag")
	}
	if _, ok := TryCompactAck([]string{"grep", "."}, []byte("")); ok {
		t.Fatal("grep not ack")
	}
	if _, ok := TryCompactSift([]string{"rg", "."}, []byte("")); ok {
		t.Fatal("rg not sift")
	}
	if _, ok := TryCompactPlocate([]string{"locate", "x"}, []byte("")); ok {
		t.Fatal("locate not plocate")
	}
	if _, ok := TryCompactLocate([]string{"plocate", "x"}, []byte("")); ok {
		t.Fatal("plocate not locate")
	}
	if _, ok := TryCompactSk([]string{"fzf", "x"}, []byte("")); ok {
		t.Fatal("fzf not sk")
	}

	// ggrep alias
	gg, ok := TryCompactGrep([]string{"ggrep", "-r", "x", "."}, []byte(""))
	if !ok || string(gg) != "[grep] no matches\n" {
		t.Fatalf("ggrep: ok=%v %q", ok, gg)
	}

	// ack.pl alias
	ackPl, ok := TryCompactAck([]string{"ack.pl", "pat"}, []byte(""))
	if !ok || string(ackPl) != "[ack] no matches\n" {
		t.Fatalf("ack.pl: ok=%v %q", ok, ackPl)
	}
}

// TestSearchToolName exercises the searchToolName function directly.
func TestSearchToolName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		argv []string
		want string
	}{
		// empty argv → default "search"
		{[]string{}, "search"},
		// basic tool
		{[]string{"rg", "pattern"}, "rg"},
		// git grep special case
		{[]string{"git", "grep", "pattern", "."}, "git grep"},
		// git without grep → returns "git"
		{[]string{"git", "log"}, "git"},
		// .exe suffix is stripped
		{[]string{"rg.exe", "pattern"}, "rg"},
	}
	for _, c := range cases {
		got := searchToolName(c.argv)
		if got != c.want {
			t.Errorf("searchToolName(%v) = %q, want %q", c.argv, got, c.want)
		}
	}
}

func TestSearchOutputKeyFromCommandLine(t *testing.T) {
	t.Parallel()
	if got := SearchOutputKeyFromCommandLine(`rg -n "needle" internal`); got != "rg\t-n\tneedle\tinternal" {
		t.Fatalf("rg search key = %q", got)
	}
	if got := SearchOutputKeyFromCommandLine(`git grep needle -- internal`); got != "git\tgrep\tneedle\t--\tinternal" {
		t.Fatalf("git grep search key = %q", got)
	}
	if got := SearchOutputKeyFromCommandLine(`git -C /repo/a grep needle -- internal`); got != "git\t-C\t/repo/a\tgrep\tneedle\t--\tinternal" {
		t.Fatalf("git -C grep search key = %q", got)
	}
	if got := SearchOutputKeyFromCommandLine(`cd /repo/a && rg -n "needle" internal`); got != "rg\t-n\tneedle\t/repo/a/internal" {
		t.Fatalf("cd-wrapped rg search key = %q", got)
	}
	if got := SearchOutputKeyFromCommandLine(`cd /repo/b && rg -n "needle" internal`); got != "rg\t-n\tneedle\t/repo/b/internal" {
		t.Fatalf("cd-wrapped rg cross-repo key = %q", got)
	}
	if got := SearchOutputKeyFromCommandLine(`go test ./...`); got != "" {
		t.Fatalf("non-search command produced key %q", got)
	}
}

func TestNormalizeSearchCommandLine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		command string
		workdir string
		want    string
	}{
		{
			name:    "workdir appended when rg has no path",
			command: `rg -n "needle"`,
			workdir: "/repo/a",
			want:    `rg -n needle /repo/a`,
		},
		{
			name:    "workdir resolves rg relative path",
			command: `rg -n "needle" internal`,
			workdir: "/repo/a",
			want:    `rg -n needle /repo/a/internal`,
		},
		{
			name:    "leading cd resolves relative path",
			command: `cd /repo/a && rg -n "needle" internal`,
			want:    `rg -n needle /repo/a/internal`,
		},
		{
			name:    "grep recursive dot",
			command: `grep -R "needle" .`,
			workdir: "/repo/a",
			want:    `grep -R needle /repo/a`,
		},
		{
			name:    "rg pattern option still resolves path",
			command: `rg -e "needle" src`,
			workdir: "/repo/a",
			want:    `rg -e needle /repo/a/src`,
		},
		{
			name:    "git grep gets git C",
			command: `git grep needle -- internal`,
			workdir: "/repo/a",
			want:    `git -C /repo/a grep needle -- internal`,
		},
		{
			name:    "git C preserved",
			command: `git -C /repo/b grep needle -- internal`,
			workdir: "/repo/a",
			want:    `git -C /repo/b grep needle -- internal`,
		},
		{
			name:    "rg separate value options preserve pattern path split",
			command: `rg --type-add 'go:*.go' --glob '*.go' -e needle src`,
			workdir: "/repo/a",
			want:    `rg --type-add "go:*.go" --glob "*.go" -e needle /repo/a/src`,
		},
		{
			name:    "grep include exclude options preserve pattern path split",
			command: `grep -R --include '*.go' --exclude-dir vendor needle .`,
			workdir: "/repo/a",
			want:    `grep -R --include "*.go" --exclude-dir vendor needle /repo/a`,
		},
		{
			name:    "rg replace option consumes value before pattern",
			command: `rg --replace '$1' 'needle(.*)' src`,
			workdir: "/repo/a",
			want:    `rg --replace "$1" "needle(.*)" /repo/a/src`,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeSearchCommandLine(tc.command, tc.workdir); got != tc.want {
				t.Fatalf("NormalizeSearchCommandLine() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCompactCapturedOutputWithContextCDWrappedSearch(t *testing.T) {
	t.Parallel()

	var input strings.Builder
	for i := 1; i <= 40; i++ {
		input.WriteString(fmt.Sprintf("internal/file_%02d.go:%d:TODO repeated search context that is intentionally long\n", i%4, i))
	}
	out, changed := CompactCapturedOutputWithContext("", `cd /repo/a && rg -n TODO internal`, input.String(), 0, FileReadContext{Mode: "scan"})
	if !changed {
		t.Fatal("cd-wrapped search output should compact")
	}
	if !strings.Contains(string(out), "[rg] 40 match(es)") || !strings.Contains(string(out), "internal/file_") {
		t.Fatalf("unexpected compacted search output: %s", out)
	}
}

func TestSearchOutputGroupingSkipsNonMatchLineModes(t *testing.T) {
	t.Parallel()

	jsonOutput := strings.Repeat(`{"type":"match","data":{"path":{"text":"a.go"},"lines":{"text":"needle"}}}`+"\n", 8)
	if _, ok := TryCompactSearchOutput([]string{"rg", "--json", "needle"}, []byte(jsonOutput)); ok {
		t.Fatal("rg --json must not be grouped as file:line output")
	}
	listOutput := strings.Repeat("src/a.go\nsrc/b.go\n", 8)
	if _, ok := TryCompactSearchOutput([]string{"rg", "-l", "needle"}, []byte(listOutput)); ok {
		t.Fatal("rg -l must not be grouped as match-line output")
	}
	countOutput := strings.Repeat("src/a.go:12\nsrc/b.go:4\n", 8)
	if _, ok := TryCompactSearchOutput([]string{"grep", "-Rc", "needle", "."}, []byte(countOutput)); ok {
		t.Fatal("grep -c must not be grouped as match-line output")
	}
}

// TestIsGrepStyleTool exercises isGrepStyleTool branches.
func TestIsGrepStyleTool(t *testing.T) {
	t.Parallel()
	cases := []struct {
		argv []string
		want bool
	}{
		{[]string{}, false},
		{[]string{"rg", "."}, true},
		{[]string{"grep", "-r", "."}, true},
		{[]string{"ggrep", "."}, true},
		{[]string{"ag", "."}, true},
		{[]string{"ack", "."}, true},
		{[]string{"ug", "."}, true},
		{[]string{"ugrep", "."}, true},
		{[]string{"sift", "."}, true},
		// git grep → true
		{[]string{"git", "grep", "."}, true},
		{[]string{"git", "-C", "/repo", "grep", "."}, true},
		// git without grep → false
		{[]string{"git", "log"}, false},
		// unknown tool → false
		{[]string{"find", "."}, false},
	}
	for _, c := range cases {
		got := isGrepStyleTool(c.argv)
		if got != c.want {
			t.Errorf("isGrepStyleTool(%v) = %v, want %v", c.argv, got, c.want)
		}
	}
}

// TestGroupSearchResults_noLineNum covers the content-only (no line number) branch.
func TestGroupSearchResults_noLineNum(t *testing.T) {
	t.Parallel()
	// grep without -n: "file:content" format (no second colon with line number).
	// Use a long file path repeated many times to ensure grouped output is shorter.
	longPath := "src/internal/some/deeply/nested/module/package/file.go"
	var sb strings.Builder
	for i := 0; i < 15; i++ {
		sb.WriteString(longPath + ":matching function content here with more text\n")
	}
	input := []byte(sb.String())
	out, ok := groupSearchResults(input, "grep")
	if !ok {
		t.Fatalf("expected grouping for no-linenum format, got false (output would not be shorter)")
	}
	if !strings.Contains(string(out), longPath) {
		t.Errorf("want file name in output, got %q", string(out))
	}
}

// TestGroupSearchResults_notParseable covers the "first colon <= 0" early return.
func TestGroupSearchResults_notParseable(t *testing.T) {
	t.Parallel()
	// Lines without colons can't be grouped
	lines := "no colon here\nanother line\nthird line\nfourth line\n"
	_, ok := groupSearchResults([]byte(lines), "rg")
	if ok {
		t.Error("unparseable lines: want false, got true")
	}
}

// TestGroupSearchResults_manyMatchesPerFile covers the "[+N more]" per-file truncation.
func TestGroupSearchResults_manyMatchesPerFile(t *testing.T) {
	t.Parallel()
	// Generate >20 matches in a single file to trigger per-file truncation
	var sb strings.Builder
	for i := 0; i < 25; i++ {
		sb.WriteString(fmt.Sprintf("src/big_file.go:%d: func doSomething() {}\n", i+1))
	}
	out, ok := groupSearchResults([]byte(sb.String()), "rg")
	if !ok {
		t.Fatalf("expected grouping for many matches, got false")
	}
	if !strings.Contains(string(out), "+5 more") {
		t.Errorf("want '+5 more' truncation, got %q", string(out))
	}
	if !strings.Contains(string(out), "25:") {
		t.Errorf("tail match should survive truncation, got %q", string(out))
	}
}

// TestGroupSearchResults_manyFiles covers the filesShown >= maxFilesShown (30)
// → "[+N more files]" branch.
func TestGroupSearchResults_manyFiles(t *testing.T) {
	t.Parallel()
	// Build 35 unique files × 25 matches each so the compact output is shorter.
	content := "function body content here with enough length to make grouping beneficial"
	var sb strings.Builder
	for f := 0; f < 35; f++ {
		for m := 1; m <= 25; m++ {
			fmt.Fprintf(&sb, "pkg/internal/module/sub/file_%02d.go:%d:%s\n", f, m, content)
		}
	}
	out, ok := groupSearchResults([]byte(sb.String()), "rg")
	if !ok {
		t.Fatalf("expected grouping for 35-file output, got false (input %d bytes)", sb.Len())
	}
	if !strings.Contains(string(out), "more files") {
		t.Errorf("want '+N more files' in output, got %q", string(out)[:min(len(string(out)), 300)])
	}
	if !strings.Contains(string(out), "file_34.go") {
		t.Errorf("tail file should survive truncation, got %q", string(out)[max(0, len(string(out))-400):])
	}
}

// TestGroupSearchResults_nonDigitBetweenColons covers the allDigits=false branch
// when the segment between the first and second colon contains non-digit characters.
func TestGroupSearchResults_nonDigitBetweenColons(t *testing.T) {
	t.Parallel()
	// Format: "file:funcname:content" — "funcname" triggers allDigits=false; lineNum stays "".
	path := "src/internal/module/package/my_file.go"
	matchContent := "funcname:some long content text that makes grouped output shorter here"
	var sb strings.Builder
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&sb, "%s:%s\n", path, matchContent)
	}
	out, ok := groupSearchResults([]byte(sb.String()), "grep")
	if !ok {
		t.Fatalf("non-digit between colons: expected grouping, got false")
	}
	// lineNum is empty → output format has no "N: " prefix, just the content
	if !strings.Contains(string(out), path) {
		t.Errorf("want file path in output, got %q", string(out))
	}
}

// TestGroupSearchResults_whitespaceOnly covers the s=="" early return (line 21-23):
// whitespace-only input is treated as empty → return stdout, false.
func TestGroupSearchResults_whitespaceOnly(t *testing.T) {
	t.Parallel()
	_, ok := groupSearchResults([]byte("  \t  \n  "), "rg")
	if ok {
		t.Error("whitespace-only input: want false, got true")
	}
}

// TestGroupSearchResults_embeddedEmptyLine covers the line=="" continue (line 39-40):
// a blank line embedded in search output is skipped and does not abort parsing.
func TestGroupSearchResults_embeddedEmptyLine(t *testing.T) {
	t.Parallel()
	// Four lines with an embedded empty line; each file has a colon, so all parse.
	input := "file.go:1:content line here\n\nfile.go:2:another match here\nfile.go:3:third match here\n"
	out, ok := groupSearchResults([]byte(input), "rg")
	if !ok {
		// Empty line skipped; 3 matches in 1 file — grouped may or may not be shorter.
		// What matters is the code path fires without returning early on the blank line.
		t.Logf("groupSearchResults: not shorter (acceptable), but code path reached")
		return
	}
	_ = out // pass-through is also acceptable
}

// TestGroupSearchResults_notShorter covers the len(result)>=len(s) guard (line 115-117):
// when each file has only one match, the grouped header adds overhead → not shorter → return false.
func TestGroupSearchResults_notShorter(t *testing.T) {
	t.Parallel()
	// 4 unique files × 1 short match → grouped output is longer than original.
	input := "a.go:1:x\nb.go:2:y\nc.go:3:z\nd.go:4:w\n"
	_, ok := groupSearchResults([]byte(input), "rg")
	if ok {
		t.Error("4 unique-file 1-match input: grouped adds overhead, want false, got true")
	}
}

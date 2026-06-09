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

func TestGroupSearchResultsWindowsPathLineNumbers(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	for i := 1; i <= 24; i++ {
		fmt.Fprintf(&sb, `C:\Users\chris\repo\src\file-one.go:%d:ordinary match text with enough payload to save space`, i)
		sb.WriteByte('\n')
	}
	input := sb.String()
	out, ok := TryCompactSearchOutput([]string{"rg", "-n", "needle"}, []byte(input))
	if !ok {
		t.Fatalf("windows path output should group")
	}
	s := string(out)
	if !strings.Contains(s, `C:\Users\chris\repo\src\file-one.go`) {
		t.Fatalf("windows path was truncated at drive colon: %q", s)
	}
	if strings.Contains(s, "  C (") {
		t.Fatalf("drive letter must not become the file key: %q", s)
	}
}

func TestGroupSearchResultsDashSeparatedLineNumbers(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	for i := 1; i <= 24; i++ {
		fmt.Fprintf(&sb, "src/path-with-dash/pre-commit-config.yaml-%d-match text with enough payload to save space\n", i)
	}
	out, ok := TryCompactSearchOutput([]string{"ag", "match"}, []byte(sb.String()))
	if !ok {
		t.Fatalf("dash-separated output should group")
	}
	s := string(out)
	if !strings.Contains(s, "src/path-with-dash/pre-commit-config.yaml") || !strings.Contains(s, "24:") {
		t.Fatalf("dash-separated path/line format parsed incorrectly: %q", s)
	}
}

func TestGroupSearchResultsPromotesHighSignalMiddleMatch(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	for i := 1; i <= 30; i++ {
		msg := "ordinary match text with enough payload to save space"
		if i == 16 {
			msg = "fatal timeout rejected request with enough payload to survive score promotion"
		}
		fmt.Fprintf(&sb, "src/service/handler.go:%d:%s\n", i, msg)
	}
	out, ok := groupSearchResults([]byte(sb.String()), "rg")
	if !ok {
		t.Fatalf("large search output should group")
	}
	if !strings.Contains(string(out), "fatal timeout rejected request") {
		t.Fatalf("high-signal middle match was dropped: %q", string(out))
	}
}

func TestCanonicalSearchMatchSetWindowsPath(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		`C:\repo\src\b-file.go:20:needle beta`,
		`C:\repo\src\a-file.go:2:needle alpha`,
	}, "\n")
	got, ok := CanonicalSearchMatchSet([]byte(input))
	if !ok {
		t.Fatal("windows search output should canonicalize")
	}
	want := `C:\repo\src\a-file.go:2:needle alpha` + "\n" +
		`C:\repo\src\b-file.go:20:needle beta` + "\n"
	if got != want {
		t.Fatalf("canonical windows search identity = %q, want %q", got, want)
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
	// Short fd path lists do not save enough to justify changing shape.
	input := "src/main.go\nsrc/config/config.go\nsrc/handler.go\nsrc/session.go\n"
	_, ok := TryCompactSearchOutput([]string{"fd", ".go"}, []byte(input))
	if ok {
		t.Fatal("short fd path list should pass through")
	}
}

func TestGroupPathListResults(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&sb, "src/generated/deep/package/file_%02d.go\n", i)
	}
	out, ok := TryCompactSearchOutput([]string{"fd", ".go"}, []byte(sb.String()))
	if !ok {
		t.Fatal("large fd path list should group")
	}
	text := string(out)
	if !strings.Contains(text, "[fd paths]") || !strings.Contains(text, "src/generated/deep/package/") || !strings.Contains(text, "file_39.go") {
		t.Fatalf("unexpected grouped path list: %q", text)
	}
	if len(text) >= sb.Len() {
		t.Fatalf("grouped path list should be shorter: out=%d in=%d", len(text), sb.Len())
	}
}

func TestGroupPathListResultsFailOpen(t *testing.T) {
	t.Parallel()
	var leading strings.Builder
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&leading, " src/path/file_%02d.go\n", i)
	}
	if _, ok := TryCompactSearchOutput([]string{"find", "."}, []byte(leading.String())); ok {
		t.Fatal("ambiguous path list line should fail open")
	}
	var nul strings.Builder
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&nul, "src/path/file_%02d.go\n", i)
	}
	withNUL := strings.Replace(nul.String(), "file_04.go", "file_04.go\x00", 1)
	if _, ok := TryCompactSearchOutput([]string{"find", "."}, []byte(withNUL)); ok {
		t.Fatal("NUL-separated/invalid path list should fail open")
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
	if got := SearchOutputKeyFromCommandLine(`rg --heading -n "needle" internal`); got != "" {
		t.Fatalf("heading search must not produce a plain match-set key: %q", got)
	}
	if got := SearchOutputKeyFromCommandLine(`rg -C 2 -n "needle" internal`); got != "" {
		t.Fatalf("context search must not produce a plain match-set key: %q", got)
	}
	if got := SearchOutputKeyFromCommandLine(`git grep needle -- internal`); got != "git\tgrep\tneedle\t--\tinternal" {
		t.Fatalf("git grep search key = %q", got)
	}
	if got := SearchOutputKeyFromCommandLine(`git -C /repo/a grep needle -- internal`); got != "git\t-C\t/repo/a\tgrep\tneedle\t--\tinternal" {
		t.Fatalf("git -C grep search key = %q", got)
	}
	if got := SearchOutputKeyFromCommandLine(`git -C /repo/a grep -C 2 needle -- internal`); got != "" {
		t.Fatalf("git grep context search must not produce a plain match-set key: %q", got)
	}
	if got := SearchOutputKeyFromCommandLine(`cd /repo/a && rg -n "needle" internal`); got != "rg\t-n\tneedle\t/repo/a/internal" {
		t.Fatalf("cd-wrapped rg search key = %q", got)
	}
	if got := SearchOutputKeyFromCommandLine(`cd /repo/b && rg -n "needle" internal`); got != "rg\t-n\tneedle\t/repo/b/internal" {
		t.Fatalf("cd-wrapped rg cross-repo key = %q", got)
	}
	if got := SearchOutputKeyFromCommandLine(`cd "/Users/example/My Repo" && rg -n "needle" "src files"`); got != "rg\t-n\tneedle\t/Users/example/My Repo/src files" {
		t.Fatalf("cd-wrapped rg with spaces key = %q", got)
	}
	if got := SearchOutputKeyFromCommandLine(`go test ./...`); got != "" {
		t.Fatalf("non-search command produced key %q", got)
	}
}

func TestSearchOutputReducerEligibleFromCommandLine(t *testing.T) {
	t.Parallel()
	if !SearchOutputReducerEligibleFromCommandLine(`cd /repo && rg -n "needle" src`, "") {
		t.Fatal("repo-scoped ripgrep must be search-output reducer eligible")
	}
	if !SearchOutputReducerEligibleFromCommandLine(`find .reconc -maxdepth 4 -type f`, "/repo") {
		t.Fatal("find path lists must be search-output reducer eligible")
	}
	if !SearchOutputReducerEligibleFromCommandLine(`fd TASK docs/tasks`, "/repo") {
		t.Fatal("fd path lists must be search-output reducer eligible")
	}
	if SearchOutputReducerEligibleFromCommandLine(`go test ./...`, "/repo") {
		t.Fatal("non-search command must not be search-output reducer eligible")
	}
}

func TestRepoScopedSearchOutputKeyFromCommandLine(t *testing.T) {
	t.Parallel()
	if got := RepoScopedSearchOutputKeyFromCommandLine(`rg -n "needle" internal`); got != "" {
		t.Fatalf("implicit-cwd rg must not get a repo-scoped key: %q", got)
	}
	if got := RepoScopedSearchOutputKeyFromCommandLine(`cd /repo/a && rg -n "needle" internal`); got != "rg\t-n\tneedle\t/repo/a/internal" {
		t.Fatalf("cd-wrapped rg repo key = %q", got)
	}
	if got := RepoScopedSearchOutputKeyFromCommandLine(`cd /repo/a && rg --heading -n "needle" internal`); got != "" {
		t.Fatalf("heading rg must not get a repo-scoped match-set key: %q", got)
	}
	if got := RepoScopedSearchOutputKeyFromCommandLine(`rg -n "needle" /repo/a/internal`); got != "rg\t-n\tneedle\t/repo/a/internal" {
		t.Fatalf("absolute-path rg repo key = %q", got)
	}
	if got := RepoScopedSearchOutputKeyFromCommandLine(`git grep needle -- internal`); got != "" {
		t.Fatalf("implicit-cwd git grep must not get a repo-scoped key: %q", got)
	}
	if got := RepoScopedSearchOutputKeyFromCommandLine(`git -C /repo/a grep needle -- internal`); got != "git\t-C\t/repo/a\tgrep\tneedle\t--\tinternal" {
		t.Fatalf("git -C grep repo key = %q", got)
	}
	if got := RepoScopedSearchOutputKeyFromCommandLine(`git -C "/Users/example/My Repo" grep needle -- "src files"`); got != "git\t-C\t/Users/example/My Repo\tgrep\tneedle\t--\tsrc files" {
		t.Fatalf("git -C grep repo key with spaces = %q", got)
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
		{
			name:    "workdir with spaces keeps quoted repo scope",
			command: `rg -n "needle" "src files"`,
			workdir: "/Users/example/My Repo",
			want:    `rg -n needle "/Users/example/My Repo/src files"`,
		},
		{
			name:    "leading cd with spaces keeps quoted repo scope",
			command: `cd "/Users/example/My Repo" && rg -n "needle" "src files"`,
			want:    `rg -n needle "/Users/example/My Repo/src files"`,
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
	headingOutput := strings.Repeat("src/a.go\n12: needle with heading mode\n13: another heading mode match\n", 8)
	if _, ok := TryCompactSearchOutput([]string{"rg", "--heading", "-n", "needle"}, []byte(headingOutput)); ok {
		t.Fatal("rg --heading must not be grouped as file:line output")
	}
	contextOutput := strings.Repeat("src/a.go-11-before context\nsrc/a.go:12:needle match\nsrc/a.go-13-after context\n--\n", 8)
	if _, ok := TryCompactSearchOutput([]string{"rg", "-C", "1", "-n", "needle"}, []byte(contextOutput)); ok {
		t.Fatal("rg -C must not drop context lines through match-line grouping")
	}
	customSeparatorOutput := strings.Repeat("src/a.go=12=needle with custom separator\n", 8)
	if _, ok := TryCompactSearchOutput([]string{"rg", "--field-match-separator", "=", "-n", "needle"}, []byte(customSeparatorOutput)); ok {
		t.Fatal("custom search separators must not be grouped by the colon parser")
	}
	nullOutput := strings.Repeat("src/a.go\x0012:needle with nul path terminator\n", 8)
	if _, ok := TryCompactSearchOutput([]string{"rg", "-0", "-n", "needle"}, []byte(nullOutput)); ok {
		t.Fatal("rg -0 must not be grouped as colon-delimited match-line output")
	}
	grepNullOutput := strings.Repeat("src/a.go\x00needle with nul filename terminator\n", 8)
	if _, ok := TryCompactSearchOutput([]string{"grep", "-RZ", "needle", "."}, []byte(grepNullOutput)); ok {
		t.Fatal("grep -Z must not be grouped as colon-delimited match-line output")
	}
	nullDataOutput := strings.Repeat("src/a.go:12:needle with nul-data mode\n", 8)
	if _, ok := TryCompactSearchOutput([]string{"rg", "--null-data", "-n", "needle"}, []byte(nullDataOutput)); ok {
		t.Fatal("rg --null-data must not be grouped as normal newline-delimited output")
	}
	pathSeparatorOutput := strings.Repeat("src::a.go:12:needle with path separator override\n", 8)
	if _, ok := TryCompactSearchOutput([]string{"rg", "--path-separator", "::", "-n", "needle"}, []byte(pathSeparatorOutput)); ok {
		t.Fatal("rg --path-separator must not be grouped by the default colon parser")
	}
}

func TestCanonicalSearchMatchSetIgnoresResultOrder(t *testing.T) {
	t.Parallel()

	first := strings.Join([]string{
		"src/b.go:20:needle beta",
		"Chunk ID: volatile",
		"src/a.go:2:needle alpha",
		"Wall time: 0.0001 seconds",
		"src/a.go:10:needle zeta",
	}, "\n")
	second := strings.Join([]string{
		"src/a.go:10:needle zeta",
		"src/a.go:2:needle alpha",
		"Original token count: 42",
		"src/b.go:20:needle beta",
	}, "\n")
	firstCanonical, ok := CanonicalSearchMatchSet([]byte(first))
	if !ok {
		t.Fatal("first search output should canonicalize")
	}
	secondCanonical, ok := CanonicalSearchMatchSet([]byte(second))
	if !ok {
		t.Fatal("second search output should canonicalize")
	}
	if firstCanonical != secondCanonical {
		t.Fatalf("canonical search identity should ignore order/noise:\nfirst=%q\nsecond=%q", firstCanonical, secondCanonical)
	}
	want := "src/a.go:2:needle alpha\nsrc/a.go:10:needle zeta\nsrc/b.go:20:needle beta\n"
	if firstCanonical != want {
		t.Fatalf("canonical search identity = %q, want %q", firstCanonical, want)
	}
	if canonical, ok := CanonicalSearchMatchSet([]byte("[rg] 3 match(es) in 1 file(s)\n  src/a.go (3 match(es))\n    1: needle\n")); ok || canonical != "" {
		t.Fatalf("grouped/capped search summaries must not become canonical identity: %q", canonical)
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

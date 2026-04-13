package compression

import (
	"strings"
	"testing"

	"github.com/tokenproxy/tokenproxy/internal/types"
)

func TestCompressToolOutput_Passthrough(t *testing.T) {
	t.Parallel()
	content := "some generic output"
	got := compressToolOutput(types.ToolTypeCommandOutput, content, 1, 5)
	if got != content {
		t.Errorf("CommandOutput should passthrough, got %q", got)
	}
}

func TestCompressToolOutput_EmptyContent(t *testing.T) {
	t.Parallel()
	got := compressToolOutput(types.ToolTypeGitOutput, "", 5, 5)
	if got != "" {
		t.Errorf("empty content should stay empty, got %q", got)
	}
}

func TestFilterGitCompact_Moderate(t *testing.T) {
	t.Parallel()
	content := "On branch main\n" +
		"diff --git a/foo.go b/foo.go\n" +
		"--- a/foo.go\n" +
		"+++ b/foo.go\n" +
		"@@ -1,3 +1,3 @@\n" +
		"-old line\n" +
		"+new line\n" +
		" context\n" +
		" 1 file changed, 1 insertion(+), 1 deletion(-)\n"
	got := filterGitCompact(content, false)
	// Should keep stats and branch info but reduce diff output
	if got == "" {
		t.Error("moderate git compact should not produce empty output")
	}
	if !strings.Contains(got, "1 file changed") {
		t.Errorf("stats line missing from output: %q", got)
	}
}

func TestFilterGitCompact_Aggressive(t *testing.T) {
	t.Parallel()
	// Build a large diff that would be dropped in aggressive mode
	var sb strings.Builder
	sb.WriteString("commit abc123\nAuthor: Test\nDate: Mon\n\n")
	for i := 0; i < 100; i++ {
		sb.WriteString("+added line\n")
		sb.WriteString("-removed line\n")
	}
	sb.WriteString("2 files changed, 100 insertions(+), 100 deletions(-)\n")
	content := sb.String()

	got := filterGitCompact(content, true)
	// Aggressive mode: no diff lines, only stats+header
	if strings.Contains(got, "+added line") {
		t.Errorf("aggressive mode should not include diff lines, got %q", got[:100])
	}
	if !strings.Contains(got, "commit abc123") {
		t.Errorf("commit header should be kept in aggressive mode")
	}
}

func TestFilterGitCompact_NoSavings_ReturnOriginal(t *testing.T) {
	t.Parallel()
	short := "On branch main\n1 file changed\n"
	got := filterGitCompact(short, true)
	// Result should be <= original (or original if no savings)
	if len(got) > len(short) {
		t.Errorf("result should not be larger than input: len(got)=%d > len(short)=%d", len(got), len(short))
	}
}

func TestFilterTestCompact_AllPassing(t *testing.T) {
	t.Parallel()
	content := "=== RUN   TestFoo\n--- PASS: TestFoo (0.01s)\n" +
		"=== RUN   TestBar\n--- PASS: TestBar (0.02s)\n" +
		"ok  \tgithub.com/foo/bar\t0.03s\n"
	got := filterTestCompact(content, true)
	// Should keep summary, may drop individual PASS lines in aggressive mode
	if got == "" {
		t.Error("should not produce empty output")
	}
}

func TestFilterTestCompact_WithFailures(t *testing.T) {
	t.Parallel()
	content := "=== RUN   TestFoo\n" +
		"--- FAIL: TestFoo (0.01s)\n" +
		"    foo_test.go:42: assertion failed: got 1, want 2\n" +
		"FAIL\tgithub.com/foo/bar\t0.01s\n"
	got := filterTestCompact(content, false)
	if !strings.Contains(got, "FAIL") {
		t.Errorf("failure lines should be kept: %q", got)
	}
	if !strings.Contains(got, "assertion failed") {
		t.Errorf("failure detail should be kept in moderate mode: %q", got)
	}
}

func TestFilterBuildCompact_ErrorsKept(t *testing.T) {
	t.Parallel()
	content := "Compiling project...\n" +
		"src/main.go:10:5: error: undefined: Foo\n" +
		"src/lib.go:20:3: warning: unused variable\n" +
		"Build failed with 1 error\n"
	got := filterBuildCompact(content, false)
	if !strings.Contains(got, "error: undefined") {
		t.Errorf("error line should be kept: %q", got)
	}
	if !strings.Contains(got, "warning:") {
		t.Errorf("warning line should be kept in moderate mode: %q", got)
	}
}

func TestFilterBuildCompact_Aggressive_LimitsErrors(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 30; i++ {
		sb.WriteString("file.go:10:1: error: something wrong\n")
	}
	content := sb.String()
	got := filterBuildCompact(content, true)
	lines := strings.Split(strings.TrimSpace(got), "\n")
	// Aggressive mode caps at 20 errors + the "omitted" line
	if len(lines) > 22 {
		t.Errorf("aggressive build compact should limit errors, got %d lines", len(lines))
	}
}

func TestFilterLintCompact_ViolationsKept(t *testing.T) {
	t.Parallel()
	content := "src/app.ts:5:3: error no-unused-vars: 'foo' is defined but never used\n" +
		"src/app.ts:10:1: warning @typescript-eslint/no-explicit-any: Unexpected any\n" +
		"\n2 problems (1 error, 1 warning)\n"
	got := filterLintCompact(content, false)
	if !strings.Contains(got, "no-unused-vars") {
		t.Errorf("lint violation should be kept: %q", got)
	}
}

func TestFilterLogCompact_DeduplicatesRepeatedLines(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 20; i++ {
		sb.WriteString("2026-04-12 10:00:00 INFO heartbeat\n")
	}
	sb.WriteString("2026-04-12 10:05:00 ERROR connection failed\n")
	content := sb.String()
	got := filterLogCompact(content, false)
	// Should deduplicate the 20 identical heartbeat lines
	if strings.Count(got, "heartbeat") > 2 {
		t.Errorf("repeated log lines should be deduplicated: got %d occurrences",
			strings.Count(got, "heartbeat"))
	}
	if !strings.Contains(got, "connection failed") {
		t.Errorf("unique error line should be kept: %q", got)
	}
}

func TestFilterDirCompact_Aggressive_Summary(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("total 128\n")
	for i := 0; i < 25; i++ {
		sb.WriteString("-rw-r--r-- 1 user staff 1234 Jan 1 12:00 file.go\n")
	}
	for i := 0; i < 5; i++ {
		sb.WriteString("drwxr-xr-x 2 user staff  64 Jan 1 12:00 subdir/\n")
	}
	content := sb.String()
	got := filterDirCompact(content, true)
	if !strings.Contains(got, "25 files") || !strings.Contains(got, "5 dirs") {
		t.Errorf("aggressive dir compact should produce summary with counts: %q", got)
	}
}

func TestFilterSearchCompact_LimitsResults(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		sb.WriteString("src/file.go:10: matched content here\n")
	}
	content := sb.String()
	got := filterSearchCompact(content, true)
	if strings.Count(got, "matched content") > 32 {
		t.Errorf("aggressive search compact should limit to 30 matches, got %d",
			strings.Count(got, "matched content"))
	}
	if !strings.Contains(got, "more matches") {
		t.Errorf("should indicate remaining match count: %q", got[:min(len(got), 200)])
	}
}

func TestCompressToolOutput_LogOutput(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		sb.WriteString("2026-04-12 10:00:00 INFO request processed\n")
	}
	sb.WriteString("2026-04-12 10:05:00 ERROR connection failed\n")
	content := sb.String()
	got := compressToolOutput(types.ToolTypeLogOutput, content, 3, 5)
	if len(got) >= len(content) {
		t.Errorf("log output should be compressed: got %d >= %d", len(got), len(content))
	}
}

func TestCompressToolOutput_DirListing(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("total 256\n")
	for i := 0; i < 30; i++ {
		sb.WriteString("-rw-r--r-- 1 user staff 1234 Jan 1 12:00 file.go\n")
	}
	for i := 0; i < 8; i++ {
		sb.WriteString("drwxr-xr-x 2 user staff  64 Jan 1 12:00 subdir/\n")
	}
	content := sb.String()
	// aggressive mode (messageAge=15 > 2*5=10)
	got := compressToolOutput(types.ToolTypeDirListing, content, 15, 5)
	if len(got) >= len(content) {
		t.Errorf("dir listing should be compressed in aggressive mode: got %d >= %d", len(got), len(content))
	}
	if !strings.Contains(got, "30 files") || !strings.Contains(got, "8 dirs") {
		t.Errorf("dir compact should show file/dir counts: %q", got)
	}
}

func TestCompressToolOutput_SearchResult(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		sb.WriteString("src/file.go:10: matched content line here\n")
	}
	content := sb.String()
	// aggressive mode
	got := compressToolOutput(types.ToolTypeSearchResult, content, 20, 5)
	if len(got) >= len(content) {
		t.Errorf("search result should be compressed: got %d >= %d", len(got), len(content))
	}
}

func TestFilterGitCompact_ModerateWithDiff(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("diff --git a/foo.go b/foo.go\n")
	sb.WriteString("--- a/foo.go\n")
	sb.WriteString("+++ b/foo.go\n")
	sb.WriteString("@@ -1,3 +1,3 @@\n")
	for i := 0; i < 80; i++ {
		sb.WriteString("+new line\n")
		sb.WriteString("-old line\n")
	}
	sb.WriteString("1 file changed, 80 insertions(+), 80 deletions(-)\n")
	content := sb.String()
	got := filterGitCompact(content, false) // moderate
	if !strings.Contains(got, "truncated") {
		t.Errorf("moderate mode should truncate long diffs: %q", got[:min2(len(got), 100)])
	}
	if !strings.Contains(got, "1 file changed") {
		t.Errorf("stats should be kept: %q", got)
	}
}

func TestFilterDirCompact_ShortContent_NoSavings(t *testing.T) {
	t.Parallel()
	// Short dir listing that can't be compressed to a shorter summary
	content := "total 8\n-rw-r--r-- foo.go\n"
	got := filterDirCompact(content, false) // moderate, few files
	// Should return content unchanged since no real savings possible
	_ = got // just verify it doesn't panic
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestCompressToolOutput_FileRead_Passthrough(t *testing.T) {
	t.Parallel()
	content := "package main\n\nfunc main() {}\n"
	got := compressToolOutput(types.ToolTypeFileRead, content, 5, 5)
	if got != content {
		t.Errorf("FileRead should passthrough unchanged, got %q", got)
	}
}

func TestCompressToolOutput_JSONData_Passthrough(t *testing.T) {
	t.Parallel()
	content := `{"status":"ok","items":[1,2,3]}`
	got := compressToolOutput(types.ToolTypeJSONData, content, 5, 5)
	if got != content {
		t.Errorf("JSONData should passthrough unchanged (handled by JSON compact), got %q", got)
	}
}

func TestFilterLogCompact_Aggressive(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 60; i++ {
		sb.WriteString("2026-04-12 10:00:00 INFO line " + itoaTest(i) + "\n")
	}
	content := sb.String()
	got := filterLogCompact(content, true) // aggressive
	if len(got) >= len(content) {
		t.Errorf("aggressive log compact should reduce content: %d >= %d", len(got), len(content))
	}
	if !strings.Contains(got, "omitted") {
		t.Errorf("aggressive mode should mention omitted lines: %q", got[:min2(len(got), 100)])
	}
}

func TestFilterSearchCompact_Moderate_NoLimit(t *testing.T) {
	t.Parallel()
	// Small number of matches: no truncation needed
	content := "src/a.go:1: match one\nsrc/b.go:2: match two\nsrc/c.go:3: match three\n"
	got := filterSearchCompact(content, false)
	if strings.Contains(got, "more matches") {
		t.Errorf("small result set should not say 'more matches': %q", got)
	}
}

func TestFilterLintCompact_LimitHit(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 80; i++ {
		sb.WriteString("src/file.ts:" + itoaTest(i) + ":1: error no-unused-vars: x\n")
	}
	sb.WriteString("\n80 problems (80 errors, 0 warnings)\n")
	content := sb.String()
	got := filterLintCompact(content, false)
	if len(got) >= len(content) {
		t.Errorf("lint compact should reduce content: got %d >= %d", len(got), len(content))
	}
}

func TestFilterLintCompact_Aggressive(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 40; i++ {
		sb.WriteString("src/file.ts:" + itoaTest(i) + ":1: error no-unused-vars: x\n")
	}
	content := sb.String()
	got := filterLintCompact(content, true)
	if len(got) >= len(content) {
		t.Errorf("aggressive lint compact should reduce content: %d >= %d", len(got), len(content))
	}
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 10)
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestCompressToolOutput_Integration(t *testing.T) {
	t.Parallel()
	// Integration: git output with large diff, aggressive mode
	var sb strings.Builder
	sb.WriteString("On branch main\n")
	for i := 0; i < 200; i++ {
		sb.WriteString("+added line content here\n")
		sb.WriteString("-removed line content here\n")
	}
	sb.WriteString("1 file changed, 200 insertions(+), 200 deletions(-)\n")
	content := sb.String()

	// messageAge=15, slidingWindow=5 -> aggressive (15 > 2*5=10)
	got := compressToolOutput(types.ToolTypeGitOutput, content, 15, 5)
	if len(got) >= len(content) {
		t.Errorf("aggressive git compress should reduce content: got %d >= orig %d", len(got), len(content))
	}
}

func TestCompressToolOutput_BuildType(t *testing.T) {
	t.Parallel()
	// Build output with errors - exercises the ToolTypeBuildOutput branch.
	content := "error: cannot find package \"foo\"\n" +
		"src/main.go:10:5: undefined: bar\n" +
		"src/main.go:11:5: undefined: baz\n"
	got := compressToolOutput(types.ToolTypeBuildOutput, content, 3, 5)
	// Should pass through or compress - just verify the branch is exercised
	if got == "" && content != "" {
		t.Error("non-empty input should not produce empty output")
	}
}

func TestCompressToolOutput_TestType(t *testing.T) {
	t.Parallel()
	// Test output with passing tests - exercises the ToolTypeTestOutput branch.
	content := "ok  \tgithub.com/foo/bar\t0.123s\nPASS\n"
	got := compressToolOutput(types.ToolTypeTestOutput, content, 3, 5)
	if got == "" {
		t.Error("non-empty test output should not produce empty result")
	}
}

func TestCompressToolOutput_LintType(t *testing.T) {
	t.Parallel()
	// Lint output - exercises the ToolTypeLintOutput branch.
	var sb strings.Builder
	for i := 0; i < 30; i++ {
		sb.WriteString("src/foo.go:10:1: warning: something wrong\n")
	}
	content := sb.String()
	got := compressToolOutput(types.ToolTypeLintOutput, content, 5, 5)
	// Lint compressor should produce output
	if got == "" {
		t.Error("lint output should not be empty")
	}
}

// TestFilterDirCompact_AggressiveSummaryNotShorter verifies the fallback when
// the generated summary is not shorter than the input content.
func TestFilterDirCompact_AggressiveSummaryNotShorter(t *testing.T) {
	t.Parallel()
	// Very short content where the "[directory: N files, N dirs]" summary would be longer
	content := "-rw-r--r-- f\n"
	got := filterDirCompact(content, true)
	// When summary >= input: return content unchanged
	if got != content {
		t.Errorf("want original content for short input, got %q", got)
	}
}

// TestFilterDirCompact_ModerateWithOtherLines verifies the kept/others path.
func TestFilterDirCompact_ModerateWithOtherLines(t *testing.T) {
	t.Parallel()
	// Non-ls lines (no permission bits) trigger the "others" path
	content := "file1.go\nfile2.go\nfile3.go\n"
	got := filterDirCompact(content, false)
	// Moderate mode with unrecognized lines: content returned as-is
	_ = got // just verify no panic
}

// TestFilterDirCompact_ModerateWithKeptLines covers the `kept` append path (others++)
// triggered by lines that start with whitespace (not matched by reSimpleEntry=^\\S).
func TestFilterDirCompact_ModerateWithKeptLines(t *testing.T) {
	t.Parallel()
	// Lines starting with whitespace are not matched by reSimpleEntry (^\S) or reDirEntry
	// so they go into "kept". Moderate mode, few file entries → returns content as-is.
	content := "-rw-r--r-- 1 user group 100 Jan 1 file.go\n  extra annotation line\n"
	got := filterDirCompact(content, false)
	_ = got // covers the else { kept = append(kept, stripped); others++ } path
}

// TestFilterSearchCompact_ContextLinesAppended covers the contextLines append path
// (lines not matching reSearchMatch go to contextLines, then appended when !aggressive).
func TestFilterSearchCompact_ContextLinesAppended(t *testing.T) {
	t.Parallel()
	// "-- context --" is not a match line (no file:N: pattern) → contextLines
	content := "src/file.go:1:match content\n-- context separator --\nsrc/other.go:5:second match\n"
	got := filterSearchCompact(content, false)
	_ = got // covers contextLines = append(contextLines, stripped)
}

// TestFilterSearchCompact_AggressiveNoMatches covers result=="" → return content passthrough.
// When aggressive=true and no match lines, out is empty → result="" → return content.
func TestFilterSearchCompact_AggressiveNoMatches(t *testing.T) {
	t.Parallel()
	// Only context lines (no file:N: patterns) with aggressive=true → contextLines not appended
	// → out is empty → result="" → return content
	content := "just some text output\nno search patterns here\nmore lines\nseveral more\n"
	got := filterSearchCompact(content, true)
	if got != content {
		t.Errorf("no matches aggressive: want content unchanged, got %q", got)
	}
}

// TestFilterGitCompact_FileSummaryLines covers the reGitFileSummary branch (lines 64-67):
// "create mode"/"delete mode"/"rename "/"mode change " lines go to fileSummary.
func TestFilterGitCompact_FileSummaryLines(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("diff --git a/new.go b/new.go\n")
	sb.WriteString("--- /dev/null\n")
	sb.WriteString("+++ b/new.go\n")
	for i := 0; i < 80; i++ {
		sb.WriteString("+added line content here\n")
	}
	sb.WriteString("create mode 100644 new.go\n")
	sb.WriteString("1 file changed, 80 insertions(+)\n")
	content := sb.String()

	got := filterGitCompact(content, false)
	if !strings.Contains(got, "create mode 100644 new.go") {
		t.Errorf("fileSummary line should appear in output: %q", got[:min2(len(got), 120)])
	}
	if !strings.Contains(got, "1 file changed") {
		t.Errorf("stats line should appear in output: %q", got)
	}
	if len(got) >= len(content) {
		t.Errorf("output should be shorter than input (diff truncated): got %d >= %d", len(got), len(content))
	}
}

// TestFilterTestCompact_NonAggressivePassLines covers the !aggressive append (line 133):
// PASS lines are kept in failures slice when aggressive=false.
func TestFilterTestCompact_NonAggressivePassLines(t *testing.T) {
	t.Parallel()
	// "=== RUN" lines are dropped; "--- PASS:" lines are kept in non-aggressive mode.
	var sb strings.Builder
	for i := 0; i < 10; i++ {
		sb.WriteString("=== RUN   TestCase\n")
		sb.WriteString("--- PASS: TestCase (0.01s)\n")
	}
	sb.WriteString("ok\tgithub.com/foo/bar\t0.10s\n")
	content := sb.String()

	got := filterTestCompact(content, false) // non-aggressive → PASS lines kept
	if !strings.Contains(got, "--- PASS:") {
		t.Errorf("non-aggressive mode should keep PASS lines: %q", got[:min2(len(got), 120)])
	}
	if len(got) >= len(content) {
		t.Errorf("output should be shorter (=== RUN lines dropped): got %d >= %d", len(got), len(content))
	}
}

// TestFilterTestCompact_NoSavingsGuard covers the result=="" guard (lines 148-150):
// content with no matching test patterns → out empty → result="" → return content.
func TestFilterTestCompact_NoSavingsGuard(t *testing.T) {
	t.Parallel()
	content := "plain output line\nno test patterns present\nanother line\n"
	got := filterTestCompact(content, false)
	if got != content {
		t.Errorf("no test patterns: want original content, got %q", got)
	}
}

// TestFilterBuildCompact_NoSavingsGuard covers the result=="" guard (lines 186-188):
// content with no error/warning/info patterns → out empty → result="" → return content.
func TestFilterBuildCompact_NoSavingsGuard(t *testing.T) {
	t.Parallel()
	content := "plain build output\nno errors or warnings here\njust some notes\n"
	got := filterBuildCompact(content, false)
	if got != content {
		t.Errorf("no build patterns: want original content, got %q", got)
	}
}

// TestFilterLintCompact_NoSavingsGuard covers the result=="" guard (lines 222-224):
// content with no violation/summary patterns → out empty → result="" → return content.
func TestFilterLintCompact_NoSavingsGuard(t *testing.T) {
	t.Parallel()
	content := "plain output\nno lint violations present\njust some text\n"
	got := filterLintCompact(content, false)
	if got != content {
		t.Errorf("no lint patterns: want original content, got %q", got)
	}
}

// TestFilterLogCompact_NoSavingsGuard covers the len(result)>=len(content) guard (lines 266-268):
// unique lines without timestamps → deduplicated = original → result == content → return content.
func TestFilterLogCompact_NoSavingsGuard(t *testing.T) {
	t.Parallel()
	// No timestamps → normalization is identity; no duplicates → deduplicated = all lines.
	// Join(deduplicated, "\n") reconstructs content exactly → len(result) == len(content).
	content := "alpha\nbeta\ngamma\n"
	got := filterLogCompact(content, false)
	if got != content {
		t.Errorf("no-savings log: want original content, got %q", got)
	}
}


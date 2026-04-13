package summarization

import (
	"strings"
	"testing"

	"github.com/tokenproxy/tokenproxy/internal/types"
)

func TestClassifyPriority_AnchorAlwaysHigh(t *testing.T) {
	t.Parallel()
	// Anchor messages should always be HIGH regardless of tool type
	p := ClassifyPriority(types.ToolTypeDirListing, "total 5\n", true)
	if p != types.PriorityHigh {
		t.Errorf("anchor message should always be HIGH, got %v", p)
	}
}

func TestClassifyPriority_TestFailure_High(t *testing.T) {
	t.Parallel()
	p := ClassifyPriority(types.ToolTypeTestOutput, "--- FAIL: TestFoo (0.01s)\nFAIL", false)
	if p != types.PriorityHigh {
		t.Errorf("test failure should be HIGH, got %v", p)
	}
}

func TestClassifyPriority_TestPass_Low(t *testing.T) {
	t.Parallel()
	p := ClassifyPriority(types.ToolTypeTestOutput, "ok  \tgithub.com/foo/bar\t0.01s\n42 passed, 0 failed", false)
	if p != types.PriorityLow {
		t.Errorf("all-passing test should be LOW, got %v", p)
	}
}

func TestClassifyPriority_BuildError_High(t *testing.T) {
	t.Parallel()
	p := ClassifyPriority(types.ToolTypeBuildOutput, "src/main.go:10:5: error: undefined: Foo", false)
	if p != types.PriorityHigh {
		t.Errorf("build error should be HIGH, got %v", p)
	}
}

func TestClassifyPriority_CleanBuild_Low(t *testing.T) {
	t.Parallel()
	p := ClassifyPriority(types.ToolTypeBuildOutput, "Compiled successfully. 0 errors.", false)
	if p != types.PriorityLow {
		t.Errorf("clean build should be LOW, got %v", p)
	}
}

func TestClassifyPriority_FileRead_Medium(t *testing.T) {
	t.Parallel()
	p := ClassifyPriority(types.ToolTypeFileRead, "package main\n\nfunc main() {}", false)
	if p != types.PriorityMedium {
		t.Errorf("file read should be MEDIUM, got %v", p)
	}
}

func TestClassifyPriority_SearchResult_Medium(t *testing.T) {
	t.Parallel()
	p := ClassifyPriority(types.ToolTypeSearchResult, "src/foo.go:10: match", false)
	if p != types.PriorityMedium {
		t.Errorf("search result should be MEDIUM, got %v", p)
	}
}

func TestClassifyPriority_GitDiff_Medium(t *testing.T) {
	t.Parallel()
	p := ClassifyPriority(types.ToolTypeGitOutput, "diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go", false)
	if p != types.PriorityMedium {
		t.Errorf("git diff should be MEDIUM, got %v", p)
	}
}

func TestClassifyPriority_DirListing_Low(t *testing.T) {
	t.Parallel()
	p := ClassifyPriority(types.ToolTypeDirListing, "total 8\n-rw-r--r-- foo.go", false)
	if p != types.PriorityLow {
		t.Errorf("dir listing should be LOW, got %v", p)
	}
}

func TestClassifyPriority_LogError_High(t *testing.T) {
	t.Parallel()
	p := ClassifyPriority(types.ToolTypeLogOutput, "2026-01-01 10:00:00 ERROR database connection failed", false)
	if p != types.PriorityHigh {
		t.Errorf("log with ERROR should be HIGH, got %v", p)
	}
}

func TestClassifyPriority_LogInfo_Low(t *testing.T) {
	t.Parallel()
	p := ClassifyPriority(types.ToolTypeLogOutput, "2026-01-01 10:00:00 INFO server started", false)
	if p != types.PriorityLow {
		t.Errorf("info-only log should be LOW, got %v", p)
	}
}

func TestPriorityLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		p    types.ToolResultPriority
		want string
	}{
		{types.PriorityHigh, "HIGH"},
		{types.PriorityMedium, "MEDIUM"},
		{types.PriorityLow, "LOW"},
	}
	for _, tc := range tests {
		got := PriorityLabel(tc.p)
		if got != tc.want {
			t.Errorf("PriorityLabel(%d) = %q, want %q", tc.p, got, tc.want)
		}
	}
}

func TestSummarizationHint_ContainsPriorityLabels(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{
			Index: 0,
			Role:  "user",
			Content: []types.ContentBlock{
				{Type: "tool_result", ToolName: "Bash",
					Text: "--- FAIL: TestFoo (0.01s)\nFAIL", ToolResultID: "x"},
			},
		},
		{
			Index: 1,
			Role:  "user",
			Content: []types.ContentBlock{
				{Type: "tool_result", ToolName: "ls",
					Text: "total 8\ndrwxr-xr-x 2 user staff 64 foo/", ToolResultID: "y"},
			},
		},
	}

	hint := SummarizationHint(msgs)
	if !strings.Contains(hint, "HIGH") || !strings.Contains(hint, "LOW") {
		t.Errorf("hint should contain HIGH and LOW labels, got: %q", hint)
	}
}

func TestSummarizationHint_Empty(t *testing.T) {
	t.Parallel()
	hint := SummarizationHint(nil)
	if hint != "" {
		t.Errorf("nil messages should produce empty hint, got %q", hint)
	}
}

func TestClassifyPriority_LintErrors_Medium(t *testing.T) {
	t.Parallel()
	p := ClassifyPriority(types.ToolTypeLintOutput, "src/app.ts:5:1: error no-unused-vars: foo", false)
	if p != types.PriorityMedium {
		t.Errorf("lint with errors should be MEDIUM, got %v", p)
	}
}

func TestClassifyPriority_LintClean_Low(t *testing.T) {
	t.Parallel()
	p := ClassifyPriority(types.ToolTypeLintOutput, "No problems found.", false)
	if p != types.PriorityLow {
		t.Errorf("clean lint should be LOW, got %v", p)
	}
}

func TestClassifyPriority_JSONData_Medium(t *testing.T) {
	t.Parallel()
	p := ClassifyPriority(types.ToolTypeJSONData, `{"status":"ok"}`, false)
	if p != types.PriorityMedium {
		t.Errorf("JSON data should be MEDIUM, got %v", p)
	}
}

func TestClassifyPriority_CommandOutput_Low(t *testing.T) {
	t.Parallel()
	p := ClassifyPriority(types.ToolTypeCommandOutput, "some generic output", false)
	if p != types.PriorityLow {
		t.Errorf("generic command output should be LOW, got %v", p)
	}
}

func TestClassifyByContent_GitBranch(t *testing.T) {
	t.Parallel()
	result := classifyByContent("On branch main\nnothing to commit")
	if result != types.ToolTypeGitOutput {
		t.Errorf("git branch content should be GitOutput, got %d", result)
	}
}

func TestClassifyByContent_BuildOutput(t *testing.T) {
	t.Parallel()
	result := classifyByContent("src/main.go:10:1: error: undefined")
	if result != types.ToolTypeBuildOutput {
		t.Errorf("build error should be BuildOutput, got %d", result)
	}
}

func TestClassifyByContent_EmptyIsCommandOutput(t *testing.T) {
	t.Parallel()
	result := classifyByContent("")
	if result != types.ToolTypeCommandOutput {
		t.Errorf("empty should be CommandOutput, got %d", result)
	}
}

func TestClassifyFromBlock_KnownToolName(t *testing.T) {
	t.Parallel()
	block := types.ContentBlock{Type: "tool_result", ToolName: "Read", Text: "content"}
	result := classifyFromBlock(block)
	if result != types.ToolTypeFileRead {
		t.Errorf("Read tool should be FileRead, got %d", result)
	}
}

func TestPriorityLabel_Unknown(t *testing.T) {
	t.Parallel()
	// Unknown priority should return MEDIUM
	got := PriorityLabel(types.ToolResultPriority(99))
	if got != "MEDIUM" {
		t.Errorf("unknown priority should return MEDIUM, got %q", got)
	}
}

func TestClassifyPriority_GitStatus_Low(t *testing.T) {
	t.Parallel()
	// Git status without diff content -> LOW
	p := ClassifyPriority(types.ToolTypeGitOutput, "On branch main\nnothing to commit", false)
	if p != types.PriorityLow {
		t.Errorf("git status without diff should be LOW, got %v", p)
	}
}

func TestClassifyByContent_TestOutput(t *testing.T) {
	t.Parallel()
	result := classifyByContent("PASS\nok  \tgithub.com/foo/bar\t0.123s\n")
	if result != types.ToolTypeTestOutput {
		t.Errorf("test output should be TestOutput, got %d", result)
	}
}

func TestClassifyByContent_GitDiff(t *testing.T) {
	t.Parallel()
	result := classifyByContent("diff --git a/foo.go b/foo.go\n@@ -1,3 +1,4 @@\n")
	if result != types.ToolTypeGitOutput {
		t.Errorf("git diff should be GitOutput, got %d", result)
	}
}

func TestClassifyByContent_DirListing(t *testing.T) {
	t.Parallel()
	// ls -l style output with permission bits
	result := classifyByContent("-rw-r--r-- 1 user group 1234 Jan 1 foo.go\ntotal 8\n")
	if result != types.ToolTypeDirListing {
		t.Errorf("dir listing should be DirListing, got %d", result)
	}
}

func TestClassifyByContent_GenericCommandOutput(t *testing.T) {
	t.Parallel()
	result := classifyByContent("Hello World\nsome output here\n")
	if result != types.ToolTypeCommandOutput {
		t.Errorf("generic output should be CommandOutput, got %d", result)
	}
}

func TestClassifyFromBlock_GrepTool(t *testing.T) {
	t.Parallel()
	block := types.ContentBlock{Type: "tool_result", ToolName: "grep", Text: "match"}
	result := classifyFromBlock(block)
	if result != types.ToolTypeSearchResult {
		t.Errorf("grep tool should be SearchResult, got %d", result)
	}
}

func TestClassifyFromBlock_LsTool(t *testing.T) {
	t.Parallel()
	block := types.ContentBlock{Type: "tool_result", ToolName: "ls", Text: "file1.go\nfile2.go\n"}
	result := classifyFromBlock(block)
	if result != types.ToolTypeDirListing {
		t.Errorf("ls tool should be DirListing, got %d", result)
	}
}

func TestClassifyFromBlock_FallbackToContent(t *testing.T) {
	t.Parallel()
	// Unknown tool name → falls back to content-based classification
	block := types.ContentBlock{Type: "tool_result", ToolName: "unknown-tool", Text: "diff --git a/x b/x\n"}
	result := classifyFromBlock(block)
	if result != types.ToolTypeGitOutput {
		t.Errorf("content-based fallback should detect GitOutput, got %d", result)
	}
}

func TestClassifyPriority_UnknownType_Medium(t *testing.T) {
	t.Parallel()
	p := ClassifyPriority(types.ToolTypeUnknown, "some content", false)
	if p != types.PriorityMedium {
		t.Errorf("unknown tool type should be MEDIUM, got %v", p)
	}
}

// TestSummarizationHint_MultipleHighAndLow covers:
// - line 88-89: non-tool_result block triggers the "continue" path
// - line 107-109: second HIGH index appends ", " separator
// - line 117-119: second LOW index appends ", " separator
func TestSummarizationHint_MultipleHighAndLow(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{
			Index: 0, Role: "user",
			Content: []types.ContentBlock{
				// Non-tool_result block: triggers the "continue" guard (line 88-89).
				{Type: "text", Text: "plain text block not counted"},
				// tool_result with test failure -> HIGH priority
				{Type: "tool_result", ToolName: "Bash",
					Text: "--- FAIL: TestFoo (0.01s)\nFAIL", ToolResultID: "a"},
			},
		},
		{
			Index: 1, Role: "user",
			Content: []types.ContentBlock{
				// Second HIGH-priority result -> triggers separator in highIdxs loop (line 107-109).
				{Type: "tool_result", ToolName: "Bash",
					Text: "--- FAIL: TestBar (0.01s)\nFAIL", ToolResultID: "b"},
			},
		},
		{
			Index: 2, Role: "user",
			Content: []types.ContentBlock{
				// LOW-priority: dir listing
				{Type: "tool_result", ToolName: "ls",
					Text: "total 8\ndrwxr-xr-x 2 user staff 64 foo/", ToolResultID: "c"},
			},
		},
		{
			Index: 3, Role: "user",
			Content: []types.ContentBlock{
				// Second LOW-priority result -> triggers separator in lowIdxs loop (line 117-119).
				{Type: "tool_result", ToolName: "ls",
					Text: "total 4\n-rw-r--r-- 1 user staff 0 bar.go", ToolResultID: "d"},
			},
		},
	}

	hint := SummarizationHint(msgs)
	if !strings.Contains(hint, "HIGH") {
		t.Errorf("hint should contain HIGH label, got: %q", hint)
	}
	if !strings.Contains(hint, "LOW") {
		t.Errorf("hint should contain LOW label, got: %q", hint)
	}
	// With 2 high-priority message indices (0 and 1), the separator ", " should appear
	// between them in the HIGH section.
	if !strings.Contains(hint, "0, 1") {
		t.Errorf("hint should contain '0, 1' for multiple HIGH indices, got: %q", hint)
	}
	// With 2 low-priority message indices (2 and 3), separator between them.
	if !strings.Contains(hint, "2, 3") {
		t.Errorf("hint should contain '2, 3' for multiple LOW indices, got: %q", hint)
	}
}

package compression

import (
	"testing"

	"github.com/slimference/slimference/internal/types"
)

func TestClassifyToolResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		toolName string
		content  string
		want     types.ToolResultType
	}{
		{
			name:     "Read tool name -> FileRead",
			toolName: "Read",
			content:  "package main\n\nfunc main() {}",
			want:     types.ToolTypeFileRead,
		},
		{
			name:     "View tool name -> FileRead",
			toolName: "View",
			content:  "some file content",
			want:     types.ToolTypeFileRead,
		},
		{
			name:     "Grep tool name -> SearchResult",
			toolName: "Grep",
			content:  "file.go:10: match",
			want:     types.ToolTypeSearchResult,
		},
		{
			name:     "Glob tool name -> SearchResult",
			toolName: "Glob",
			content:  "src/main.go\nsrc/lib.go",
			want:     types.ToolTypeSearchResult,
		},
		{
			name:     "ls tool name -> DirListing",
			toolName: "ls",
			content:  "total 8\ndrwxr-xr-x 2 user group 64 Jan 1 12:00 .",
			want:     types.ToolTypeDirListing,
		},
		{
			name:     "git branch content -> GitOutput",
			toolName: "Bash",
			content:  "On branch main\nYour branch is up to date",
			want:     types.ToolTypeGitOutput,
		},
		{
			name:     "git diff content -> GitOutput",
			toolName: "Bash",
			content:  "diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go",
			want:     types.ToolTypeGitOutput,
		},
		{
			name:     "go test pass -> TestOutput",
			toolName: "Bash",
			content:  "ok  \tgithub.com/foo/bar\t0.123s",
			want:     types.ToolTypeTestOutput,
		},
		{
			name:     "go test fail -> TestOutput",
			toolName: "Bash",
			content:  "--- FAIL: TestFoo (0.01s)\nFAIL\tgithub.com/foo/bar",
			want:     types.ToolTypeTestOutput,
		},
		{
			name:     "go compiler error -> BuildOutput",
			toolName: "Bash",
			content:  "internal/foo/bar.go:42:10: undefined: SomeFunc",
			want:     types.ToolTypeBuildOutput,
		},
		{
			name:     "valid JSON object -> JSONData",
			toolName: "Bash",
			content:  `{"status":"ok","code":200}`,
			want:     types.ToolTypeJSONData,
		},
		{
			name:     "valid JSON array -> JSONData",
			toolName: "Bash",
			content:  `[{"id":1},{"id":2}]`,
			want:     types.ToolTypeJSONData,
		},
		{
			name:     "directory listing -> DirListing",
			toolName: "Bash",
			content:  "total 48\ndrwxr-xr-x 5 user staff 160 Jan 1 12:00 .\n-rw-r--r-- 1 user staff 234 Jan 1 12:00 README.md",
			want:     types.ToolTypeDirListing,
		},
		{
			name:     "timestamp log -> LogOutput",
			toolName: "Bash",
			content:  "2026-04-12 10:01:00 INFO server started\n2026-04-12 10:01:01 DEBUG connection established",
			want:     types.ToolTypeLogOutput,
		},
		{
			name:     "empty content -> CommandOutput",
			toolName: "Bash",
			content:  "",
			want:     types.ToolTypeCommandOutput,
		},
		{
			name:     "generic output -> CommandOutput",
			toolName: "Bash",
			content:  "Hello, World!",
			want:     types.ToolTypeCommandOutput,
		},
		{
			name:     "invalid JSON not classified as JSON",
			toolName: "Bash",
			content:  `{"incomplete":`,
			want:     types.ToolTypeCommandOutput,
		},
		{
			// Matches reClassifyLintCode (no-unused-vars) but not git/test/build/dir/log patterns.
			name:     "lint rule code -> LintOutput",
			toolName: "Bash",
			content:  "variable 'x' is never used no-unused-vars\nvariable 'y' is never used no-unused-vars",
			want:     types.ToolTypeLintOutput,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifyToolResult(tc.toolName, tc.content)
			if got != tc.want {
				snip := tc.content
			if len(snip) > 40 {
				snip = snip[:40]
			}
			t.Errorf("classifyToolResult(%q, %q) = %d, want %d",
					tc.toolName, snip, got, tc.want)
			}
		})
	}
}


package compression

import (
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/types"
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

func TestClassifyToolResultWithInput_UsesCodexShellCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		toolName  string
		toolInput string
		content   string
		want      types.ToolResultType
	}{
		{
			name:      "git status short command",
			toolName:  "shell",
			toolInput: `{"command":"git status --short"}`,
			content:   " M internal/proxy/provider.go\n?? tmp.txt\n",
			want:      types.ToolTypeGitOutput,
		},
		{
			name:      "go test command",
			toolName:  "exec_command",
			toolInput: `{"cmd":"go test ./internal/proxy"}`,
			content:   "ok  github.com/Christopher-Schulze/Slimference/internal/proxy  0.041s\nPASS\n",
			want:      types.ToolTypeTestOutput,
		},
		{
			name:      "ripgrep command",
			toolName:  "shell",
			toolInput: `{"command":"rg -n TODO internal"}`,
			content:   "internal/a.go:10:TODO\ninternal/b.go:20:TODO\n",
			want:      types.ToolTypeSearchResult,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifyToolResultWithInput(tc.toolName, tc.toolInput, tc.content)
			if got != tc.want {
				t.Fatalf("classifyToolResultWithInput()=%d want %d", got, tc.want)
			}
		})
	}
}

func TestClassifyToolInput_Branches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  types.ToolResultType
	}{
		{name: "empty", input: "", want: types.ToolTypeUnknown},
		{name: "invalid json", input: "{", want: types.ToolTypeUnknown},
		{name: "missing command", input: `{"path":"x"}`, want: types.ToolTypeUnknown},
		{name: "non-string command", input: `{"command":123}`, want: types.ToolTypeUnknown},
		{name: "blank command", input: `{"command":"   "}`, want: types.ToolTypeUnknown},
		{name: "absolute git show", input: `{"command":"/usr/bin/git show HEAD"}`, want: types.ToolTypeGitOutput},
		{name: "git unknown", input: `{"command":"git config --list"}`, want: types.ToolTypeUnknown},
		{name: "go build", input: `{"command":"go build ./..."}`, want: types.ToolTypeBuildOutput},
		{name: "go vet", input: `{"command":"go vet ./..."}`, want: types.ToolTypeBuildOutput},
		{name: "cargo check", input: `{"command":"cargo check --all-targets"}`, want: types.ToolTypeBuildOutput},
		{name: "dotnet test", input: `{"command":"dotnet test"}`, want: types.ToolTypeTestOutput},
		{name: "gradlew build", input: `{"command":"./gradlew build"}`, want: types.ToolTypeBuildOutput},
		{name: "mvn test", input: `{"command":"mvn test"}`, want: types.ToolTypeTestOutput},
		{name: "swift build", input: `{"command":"swift build"}`, want: types.ToolTypeBuildOutput},
		{name: "mix compile", input: `{"command":"mix compile"}`, want: types.ToolTypeBuildOutput},
		{name: "bun test", input: `{"command":"bun test"}`, want: types.ToolTypeTestOutput},
		{name: "npx tsc", input: `{"command":"npx tsc --noEmit"}`, want: types.ToolTypeBuildOutput},
		{name: "npm run build", input: `{"command":"npm run build"}`, want: types.ToolTypeBuildOutput},
		{name: "pnpm exec eslint", input: `{"command":"pnpm exec eslint ."}`, want: types.ToolTypeLintOutput},
		{name: "pnpm lint", input: `{"command":"pnpm lint"}`, want: types.ToolTypeLintOutput},
		{name: "yarn unknown", input: `{"command":"yarn why react"}`, want: types.ToolTypeUnknown},
		{name: "tsc", input: `{"command":"tsc --noEmit"}`, want: types.ToolTypeBuildOutput},
		{name: "xcodebuild", input: `{"command":"xcodebuild test"}`, want: types.ToolTypeBuildOutput},
		{name: "eslint", input: `{"command":"eslint ."}`, want: types.ToolTypeLintOutput},
		{name: "basedpyright", input: `{"command":"basedpyright ."}`, want: types.ToolTypeLintOutput},
		{name: "shellcheck", input: `{"command":"shellcheck script.sh"}`, want: types.ToolTypeLintOutput},
		{name: "fd search", input: `{"command":"fd '.*go$' internal"}`, want: types.ToolTypeSearchResult},
		{name: "ls", input: `{"command":"ls -la"}`, want: types.ToolTypeDirListing},
		{name: "du", input: `{"command":"du -sh internal"}`, want: types.ToolTypeDirListing},
		{name: "journalctl", input: `{"command":"journalctl -u slimference"}`, want: types.ToolTypeLogOutput},
		{name: "unknown head", input: `{"command":"echo hello"}`, want: types.ToolTypeUnknown},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyToolInput(tc.input); got != tc.want {
				t.Fatalf("classifyToolInput(%s)=%d want %d", tc.input, got, tc.want)
			}
		})
	}
}

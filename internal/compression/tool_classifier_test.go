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
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyToolInput(tc.input); got != tc.want {
				t.Fatalf("classifyToolInput(%s)=%d want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestClassifyBuildTestCommand(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		head   string
		fields []string
		want   types.ToolResultType
	}{
		// < 2 fields.
		{"pytest_no_args", "pytest", []string{"pytest"}, types.ToolTypeTestOutput},
		{"jest_no_args", "jest", []string{"jest"}, types.ToolTypeTestOutput},
		{"vitest_no_args", "vitest", []string{"vitest"}, types.ToolTypeTestOutput},
		{"playwright_no_args", "playwright", []string{"playwright"}, types.ToolTypeTestOutput},
		{"rspec_no_args", "rspec", []string{"rspec"}, types.ToolTypeTestOutput},
		{"rake_no_args", "rake", []string{"rake"}, types.ToolTypeTestOutput},
		{"unknown_no_args", "unknown", []string{"unknown"}, types.ToolTypeUnknown},
		// >= 2 fields.
		{"pytest_with_args", "pytest", []string{"pytest", "-x"}, types.ToolTypeTestOutput},
		{"jest_with_args", "jest", []string{"jest", "--coverage"}, types.ToolTypeTestOutput},
		{"vitest_with_args", "vitest", []string{"vitest", "run"}, types.ToolTypeTestOutput},
		{"playwright_with_args", "playwright", []string{"playwright", "test"}, types.ToolTypeTestOutput},
		{"rspec_with_args", "rspec", []string{"rspec", "./spec"}, types.ToolTypeTestOutput},
		{"rake_test", "rake", []string{"rake", "test"}, types.ToolTypeTestOutput},
		{"rake_spec", "rake", []string{"rake", "spec"}, types.ToolTypeTestOutput},
		{"rake_other", "rake", []string{"rake", "db:migrate"}, types.ToolTypeUnknown},
		{"go_test", "go", []string{"go", "test"}, types.ToolTypeTestOutput},
		{"go_build", "go", []string{"go", "build"}, types.ToolTypeBuildOutput},
		{"go_vet", "go", []string{"go", "vet"}, types.ToolTypeBuildOutput},
		{"go_generate", "go", []string{"go", "generate"}, types.ToolTypeBuildOutput},
		{"go_other", "go", []string{"go", "run"}, types.ToolTypeUnknown},
		{"cargo_test", "cargo", []string{"cargo", "test"}, types.ToolTypeTestOutput},
		{"cargo_nextest", "cargo", []string{"cargo", "nextest"}, types.ToolTypeTestOutput},
		{"cargo_build", "cargo", []string{"cargo", "build"}, types.ToolTypeBuildOutput},
		{"cargo_check", "cargo", []string{"cargo", "check"}, types.ToolTypeBuildOutput},
		{"cargo_clippy", "cargo", []string{"cargo", "clippy"}, types.ToolTypeBuildOutput},
		{"cargo_other", "cargo", []string{"cargo", "run"}, types.ToolTypeUnknown},
		{"dotnet_test", "dotnet", []string{"dotnet", "test"}, types.ToolTypeTestOutput},
		{"dotnet_build", "dotnet", []string{"dotnet", "build"}, types.ToolTypeBuildOutput},
		{"dotnet_publish", "dotnet", []string{"dotnet", "publish"}, types.ToolTypeBuildOutput},
		{"dotnet_restore", "dotnet", []string{"dotnet", "restore"}, types.ToolTypeBuildOutput},
		{"dotnet_other", "dotnet", []string{"dotnet", "run"}, types.ToolTypeUnknown},
		{"gradle_test", "gradle", []string{"gradle", "test"}, types.ToolTypeTestOutput},
		{"gradle_build", "gradle", []string{"gradle", "build"}, types.ToolTypeBuildOutput},
		{"gradle_compile", "gradle", []string{"gradle", "compileJava"}, types.ToolTypeBuildOutput},
		{"gradle_assemble", "gradle", []string{"gradle", "assemble"}, types.ToolTypeBuildOutput},
		{"gradle_bootrun", "gradle", []string{"gradle", "bootRun"}, types.ToolTypeBuildOutput},
		{"gradle_other", "gradle", []string{"gradle", "clean"}, types.ToolTypeUnknown},
		{"gradlew_test", "gradlew", []string{"gradlew", "test"}, types.ToolTypeTestOutput},
		{"mvn_test", "mvn", []string{"mvn", "test"}, types.ToolTypeTestOutput},
		{"mvn_compile", "mvn", []string{"mvn", "compile"}, types.ToolTypeBuildOutput},
		{"mvn_package", "mvn", []string{"mvn", "package"}, types.ToolTypeBuildOutput},
		{"mvn_install", "mvn", []string{"mvn", "install"}, types.ToolTypeBuildOutput},
		{"mvn_verify", "mvn", []string{"mvn", "verify"}, types.ToolTypeBuildOutput},
		{"mvn_spring_boot_run", "mvn", []string{"mvn", "spring-boot:run"}, types.ToolTypeBuildOutput},
		{"mvn_other", "mvn", []string{"mvn", "clean"}, types.ToolTypeUnknown},
		{"swift_test", "swift", []string{"swift", "test"}, types.ToolTypeTestOutput},
		{"swift_build", "swift", []string{"swift", "build"}, types.ToolTypeBuildOutput},
		{"swift_package", "swift", []string{"swift", "package"}, types.ToolTypeBuildOutput},
		{"swift_other", "swift", []string{"swift", "run"}, types.ToolTypeUnknown},
		{"mix_test", "mix", []string{"mix", "test"}, types.ToolTypeTestOutput},
		{"mix_compile", "mix", []string{"mix", "compile"}, types.ToolTypeBuildOutput},
		{"mix_format", "mix", []string{"mix", "format"}, types.ToolTypeBuildOutput},
		{"mix_deps_get", "mix", []string{"mix", "deps.get"}, types.ToolTypeBuildOutput},
		{"mix_other", "mix", []string{"mix", "phx.server"}, types.ToolTypeUnknown},
		{"unknown_with_args", "unknown", []string{"unknown", "arg"}, types.ToolTypeUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyBuildTestCommand(tc.head, tc.fields); got != tc.want {
				t.Fatalf("classifyBuildTestCommand(%q, %v) = %d, want %d", tc.head, tc.fields, got, tc.want)
			}
		})
	}
}

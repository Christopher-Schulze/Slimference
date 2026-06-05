package compression

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/slimference/slimference/internal/types"
)

// classifyToolResult classifies a tool_result content block by type.
// Uses tool_name first (most reliable), then content pattern matching.
func classifyToolResult(toolName string, content string) types.ToolResultType {
	lowerName := strings.ToLower(toolName)

	// tool_name-based classification (highest reliability)
	switch lowerName {
	case "read", "view", "readfile", "cat":
		return types.ToolTypeFileRead
	case "grep", "glob", "search", "find", "ls", "list":
		if lowerName == "ls" || lowerName == "list" {
			return types.ToolTypeDirListing
		}
		return types.ToolTypeSearchResult
	}

	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return types.ToolTypeCommandOutput
	}

	// JSON detection first (before other pattern checks that might false-match)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		var js interface{}
		if json.Unmarshal([]byte(trimmed), &js) == nil {
			return types.ToolTypeJSONData
		}
	}

	// Git output detection
	if reClassifyGitBranch.MatchString(trimmed) || reClassifyGitDiff.MatchString(content) {
		return types.ToolTypeGitOutput
	}

	// Test output detection
	if reClassifyTestOutput.MatchString(content) {
		return types.ToolTypeTestOutput
	}

	// Build output detection (error: with file:line pattern)
	if reClassifyBuildError.MatchString(content) {
		return types.ToolTypeBuildOutput
	}

	// Lint output detection (rule codes like E0001, no-unused-vars, @rule/name)
	if reClassifyLintCode.MatchString(content) {
		return types.ToolTypeLintOutput
	}

	// Directory listing detection (file permissions or total block count)
	if reClassifyDirListing.MatchString(trimmed) {
		return types.ToolTypeDirListing
	}

	// Log output detection (timestamps)
	if reClassifyLogTimestamp.MatchString(trimmed) {
		return types.ToolTypeLogOutput
	}

	return types.ToolTypeCommandOutput
}

func classifyToolResultWithInput(toolName string, toolInput string, content string) types.ToolResultType {
	if toolType := classifyToolInput(toolInput); toolType != types.ToolTypeUnknown {
		return toolType
	}
	return classifyToolResult(toolName, content)
}

func classifyToolInput(toolInput string) types.ToolResultType {
	if strings.TrimSpace(toolInput) == "" {
		return types.ToolTypeUnknown
	}
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(toolInput), &raw); err != nil {
		return types.ToolTypeUnknown
	}
	cmd := ""
	for _, key := range []string{"command", "cmd"} {
		if value, ok := raw[key].(string); ok {
			cmd = value
			break
		}
	}
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return types.ToolTypeUnknown
	}
	fields := strings.Fields(cmd)
	head := strings.ToLower(fields[0])
	if strings.Contains(head, "/") {
		parts := strings.Split(head, "/")
		head = strings.ToLower(parts[len(parts)-1])
	}
	switch head {
	case "git":
		if len(fields) > 1 {
			switch fields[1] {
			case "status", "diff", "log", "show":
				return types.ToolTypeGitOutput
			}
		}
	case "go", "cargo", "dotnet", "gradle", "gradlew", "mvn", "swift", "mix", "pytest", "jest", "vitest", "playwright", "rspec", "rake":
		if class := classifyBuildTestCommand(head, fields); class != types.ToolTypeUnknown {
			return class
		}
	case "bun", "npm", "npx", "pnpm", "pnpx", "yarn":
		return classifyJavaScriptPackageCommand(fields)
	case "tsc", "webpack", "vite", "cmake", "make", "gcc", "g++", "xcodebuild", "trunk", "pio", "turbo", "nx":
		return types.ToolTypeBuildOutput
	case "eslint", "ruff", "mypy", "pyright", "basedpyright", "clippy", "golangci", "golangci-lint", "staticcheck", "biome", "hadolint", "markdownlint", "oxlint", "rubocop", "shellcheck", "ty", "yamllint":
		return types.ToolTypeLintOutput
	case "rg", "grep", "ag", "ack", "find", "fd", "ug", "ugrep", "sift", "locate", "plocate":
		return types.ToolTypeSearchResult
	case "ls", "tree", "df", "du", "ps", "stat":
		return types.ToolTypeDirListing
	case "journalctl", "systemctl", "fail2ban-client", "iptables", "ping":
		return types.ToolTypeLogOutput
	}
	return types.ToolTypeUnknown
}

func classifyBuildTestCommand(head string, fields []string) types.ToolResultType {
	if len(fields) < 2 {
		switch head {
		case "pytest", "jest", "vitest", "playwright", "rspec", "rake":
			return types.ToolTypeTestOutput
		}
		return types.ToolTypeUnknown
	}
	sub := strings.ToLower(fields[1])
	switch head {
	case "pytest", "jest", "vitest", "playwright", "rspec":
		return types.ToolTypeTestOutput
	case "rake":
		if sub == "test" || strings.HasPrefix(sub, "spec") {
			return types.ToolTypeTestOutput
		}
	case "go":
		if sub == "test" {
			return types.ToolTypeTestOutput
		}
		if sub == "build" || sub == "vet" || sub == "generate" {
			return types.ToolTypeBuildOutput
		}
	case "cargo":
		if sub == "test" || sub == "nextest" {
			return types.ToolTypeTestOutput
		}
		if sub == "build" || sub == "check" || sub == "clippy" {
			return types.ToolTypeBuildOutput
		}
	case "dotnet":
		if sub == "test" {
			return types.ToolTypeTestOutput
		}
		if sub == "build" || sub == "publish" || sub == "restore" {
			return types.ToolTypeBuildOutput
		}
	case "gradle", "gradlew":
		if strings.Contains(sub, "test") {
			return types.ToolTypeTestOutput
		}
		if strings.Contains(sub, "build") || strings.Contains(sub, "compile") || strings.Contains(sub, "assemble") || strings.Contains(sub, "bootrun") {
			return types.ToolTypeBuildOutput
		}
	case "mvn":
		if sub == "test" {
			return types.ToolTypeTestOutput
		}
		if sub == "compile" || sub == "package" || sub == "install" || sub == "verify" || sub == "spring-boot:run" {
			return types.ToolTypeBuildOutput
		}
	case "swift":
		if sub == "test" {
			return types.ToolTypeTestOutput
		}
		if sub == "build" || sub == "package" {
			return types.ToolTypeBuildOutput
		}
	case "mix":
		if sub == "test" {
			return types.ToolTypeTestOutput
		}
		if sub == "compile" || sub == "format" || sub == "deps.get" {
			return types.ToolTypeBuildOutput
		}
	}
	return types.ToolTypeUnknown
}

func classifyJavaScriptPackageCommand(fields []string) types.ToolResultType {
	if len(fields) < 2 {
		return types.ToolTypeUnknown
	}
	head := strings.ToLower(fields[0])
	args := fields[1:]
	if (head == "pnpm" && args[0] == "exec" && len(args) > 1) ||
		(head == "yarn" && args[0] == "dlx" && len(args) > 1) {
		args = args[1:]
	}
	if args[0] == "run" && len(args) > 1 {
		args = args[1:]
	}
	switch strings.ToLower(args[0]) {
	case "test", "vitest", "jest", "playwright":
		return types.ToolTypeTestOutput
	case "build", "compile", "typecheck", "check", "tsc", "vite", "webpack":
		return types.ToolTypeBuildOutput
	case "lint", "eslint", "biome", "oxlint":
		return types.ToolTypeLintOutput
	}
	return types.ToolTypeUnknown
}

var (
	// Git patterns
	reClassifyGitBranch = regexp.MustCompile(
		`(?m)^(On branch|HEAD detached|nothing to commit|Changes (?:not staged|to be committed)|Your branch)`)
	// Use specific git-only markers: diff --git (unique), commit+hash, Author/Date (log format)
	// Avoid --- and +++ which are too generic and match test output like "--- FAIL:"
	reClassifyGitDiff = regexp.MustCompile(
		`(?m)^(diff --git\s|commit [0-9a-f]{6,40}\s*$|Author:\s|Date:\s)`)

	// Test patterns: go test, jest, cargo test, pytest
	reClassifyTestOutput = regexp.MustCompile(
		`(?m)(^(?:PASS|FAIL|ok\s+\S)|` +
			`\d+\s+(?:passed|failed|skipped)|` +
			`^---\s+(?:PASS|FAIL):|` +
			`^test result:|` +
			`^\s*\d+\s+tests?\s+(?:passed|failed))`)

	// Build error patterns: go compiler, rustc, tsc, gcc
	reClassifyBuildError = regexp.MustCompile(
		`(?m)([a-zA-Z0-9_./\\]+\.(?:go|rs|ts|js|cpp|c|java):\d+:\d+:\s|` +
			`error\[E\d+\]|` +
			`error TS\d+|` +
			`^\s*\^\s*error:)`)

	// Lint rule code patterns: ESLint, Clippy, pylint, etc.
	reClassifyLintCode = regexp.MustCompile(
		`(?m)(no-[a-z][\w-]{2,}|` +
			`[A-Z]\d{3,4}\b|` +
			`@[\w-]+/[\w-]+\s+error|` +
			`warning\[clippy::)`)

	// Directory listing: ls -la style output
	reClassifyDirListing = regexp.MustCompile(
		`(?m)^(total \d+$|[-dlcbsp][rwx-]{9}\s+\d+\s+\w+)`)

	// Log timestamp patterns: ISO 8601, syslog, apache
	reClassifyLogTimestamp = regexp.MustCompile(
		`(?m)\d{4}[-/]\d{2}[-/]\d{2}[T ]\d{2}:\d{2}:\d{2}`)
)

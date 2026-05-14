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
	head := fields[0]
	if strings.Contains(head, "/") {
		parts := strings.Split(head, "/")
		head = parts[len(parts)-1]
	}
	switch head {
	case "git":
		if len(fields) > 1 {
			switch fields[1] {
			case "status", "diff", "log", "show":
				return types.ToolTypeGitOutput
			}
		}
	case "go", "cargo", "pytest", "jest", "vitest", "playwright", "rspec":
		if (len(fields) > 1 && fields[1] == "test") || head == "pytest" || head == "jest" || head == "vitest" || head == "playwright" || head == "rspec" {
			return types.ToolTypeTestOutput
		}
		if len(fields) > 1 && (fields[1] == "build" || fields[1] == "vet") {
			return types.ToolTypeBuildOutput
		}
	case "bun", "npm", "pnpm", "yarn":
		return classifyJavaScriptPackageCommand(fields)
	case "tsc", "webpack", "vite", "cmake", "make":
		return types.ToolTypeBuildOutput
	case "eslint", "ruff", "mypy", "pyright", "clippy", "golangci-lint", "staticcheck":
		return types.ToolTypeLintOutput
	case "rg", "grep", "ag", "ack", "find":
		return types.ToolTypeSearchResult
	case "ls", "tree":
		return types.ToolTypeDirListing
	}
	return types.ToolTypeUnknown
}

func classifyJavaScriptPackageCommand(fields []string) types.ToolResultType {
	if len(fields) < 2 {
		return types.ToolTypeUnknown
	}
	args := fields[1:]
	if args[0] == "run" && len(args) > 1 {
		args = args[1:]
	}
	switch args[0] {
	case "test", "vitest", "jest", "playwright":
		return types.ToolTypeTestOutput
	case "build", "compile", "typecheck", "check":
		return types.ToolTypeBuildOutput
	case "lint", "eslint":
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

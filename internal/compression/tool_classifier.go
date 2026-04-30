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

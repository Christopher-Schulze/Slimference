package toolprune

import "strings"

var defaultAlwaysKeepTokens = []string{
	"apply_patch",
	"bash",
	"browser",
	"edit",
	"exec",
	"glob",
	"grep",
	"list",
	"ls",
	"mcp__",
	"multiedit",
	"multi_edit",
	"open",
	"patch",
	"permission",
	"plan",
	"read",
	"rg",
	"safety",
	"search",
	"shell",
	"terminal",
	"update_plan",
	"view",
	"write",
}

// AlwaysKeepSet returns a map for names that must never be pruned. The
// default class keeps shell, edit, read, safety, browser, and MCP tools
// attached. Extra names are exact case-sensitive tool names supplied by config.
func AlwaysKeepSet(extra []string) map[string]bool {
	out := make(map[string]bool, len(extra))
	for _, name := range extra {
		name = strings.TrimSpace(name)
		if name != "" {
			out[name] = true
		}
	}
	return out
}

// IsDefaultAlwaysKeep reports whether a tool belongs to the conservative
// always-on safety class.
func IsDefaultAlwaysKeep(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	for _, token := range defaultAlwaysKeepTokens {
		if strings.Contains(n, token) {
			return true
		}
	}
	return false
}

// LooksLikeMissingToolError detects provider 4xx responses that are very
// likely caused by pruning a needed tool definition.
func LooksLikeMissingToolError(statusCode int, body []byte) bool {
	if statusCode < 400 || statusCode >= 500 || len(body) == 0 {
		return false
	}
	lower := strings.ToLower(string(body))
	switch {
	case strings.Contains(lower, "missing tool"):
		return true
	case strings.Contains(lower, "unknown tool"):
		return true
	case strings.Contains(lower, "tool not found"):
		return true
	case strings.Contains(lower, "no such tool"):
		return true
	case strings.Contains(lower, "tool is not available"):
		return true
	case strings.Contains(lower, "tool was not provided"):
		return true
	case strings.Contains(lower, "not found in tools"):
		return true
	case strings.Contains(lower, "not among the tools"):
		return true
	case strings.Contains(lower, "not a valid tool"):
		return true
	case strings.Contains(lower, "tool_use") && strings.Contains(lower, "not found"):
		return true
	default:
		return false
	}
}

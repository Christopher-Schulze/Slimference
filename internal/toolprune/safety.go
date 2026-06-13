package toolprune

import "strings"

var defaultAlwaysKeepTokens = []string{
	"apply_patch",
	"bash",
	"browser",
	"create_goal",
	"edit",
	"exec",
	"get_goal",
	"glob",
	"grep",
	"image",
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
	"request_plugin_install",
	"request_user_input",
	"rg",
	"safety",
	"search",
	"shell",
	"terminal",
	"update_goal",
	"update_plan",
	"view",
	"write",
}

// AlwaysKeepSet returns a map for names that must never be pruned. The
// default class keeps shell, edit, read, safety, browser, MCP, and Codex
// control tools attached. Extra names are exact tool names supplied by config,
// matched case-insensitively because provider/tool casing is not a capability
// boundary.
func AlwaysKeepSet(extra []string) map[string]bool {
	out := make(map[string]bool, len(extra))
	for _, name := range extra {
		name = strings.ToLower(strings.TrimSpace(name))
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
	case strings.Contains(lower, "no tool named"):
		return true
	case strings.Contains(lower, "no such tool"):
		return true
	case strings.Contains(lower, "tool is not available"):
		return true
	case strings.Contains(lower, "tool was not provided"):
		return true
	case strings.Contains(lower, "tool") && strings.Contains(lower, "does not exist"):
		return true
	case strings.Contains(lower, "tool") && strings.Contains(lower, "not in the list of available"):
		return true
	case strings.Contains(lower, "not found in tools"):
		return true
	case strings.Contains(lower, "not among the tools"):
		return true
	case strings.Contains(lower, "not a valid tool"):
		return true
	case strings.Contains(lower, "tool_use") && strings.Contains(lower, "not found"):
		return true
	case strings.Contains(lower, "function not found"):
		return true
	case strings.Contains(lower, "no function named"):
		return true
	case strings.Contains(lower, "not a valid function"):
		return true
	case strings.Contains(lower, "function") && strings.Contains(lower, "does not exist"):
		return true
	case strings.Contains(lower, "function_call") && strings.Contains(lower, "not found"):
		return true
	default:
		return false
	}
}

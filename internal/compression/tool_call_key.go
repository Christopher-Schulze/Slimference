package compression

import (
	"encoding/json"
	"strings"

	"github.com/slimference/slimference/internal/types"
)

// ExtractToolCallKey returns a stable string key identifying a tool_result
// block for delta encoding and cross-message deduplication. The key prefers
// an explicit file path (suitable for Read/Edit/Write results) and falls
// back to "<tool_name>|<topic>" where topic is the first structured argument
// or the first non-empty line of the raw text. Returns "" when no stable
// identity can be derived.
//
// T29: this generalises delta encoding across tool calls without a
// filepath (e.g. `git status`, `grep <pattern>`, `ls <dir>`) so repeated
// invocations against the same target can be delta-encoded.
func ExtractToolCallKey(block types.ContentBlock) string {
	if fp := extractFilepathFromToolResult(block); fp != "" {
		return "file:" + fp
	}
	name := strings.ToLower(strings.TrimSpace(block.ToolName))
	if name == "" {
		return ""
	}
	topic := extractToolCallTopic(block)
	if topic == "" {
		return "tool:" + name
	}
	return "tool:" + name + "|" + topic
}

// extractToolCallTopic picks a stable short signature from the tool
// invocation's structured arguments (first positional string arg, first
// well-known keyword like "command"/"pattern"/"url") or from the leading
// line of the textual output. The topic is truncated to 64 runes.
func extractToolCallTopic(block types.ContentBlock) string {
	if topic := topicFromToolInput(block.ToolInput); topic != "" {
		return truncateRunes(topic, 64)
	}
	trimmed := strings.TrimSpace(block.Text)
	if trimmed == "" {
		return ""
	}
	if nl := strings.IndexByte(trimmed, '\n'); nl >= 0 {
		trimmed = trimmed[:nl]
	}
	return truncateRunes(trimmed, 64)
}

// topicFromToolInput tries to parse ToolInput as JSON and extract a
// human-stable field. Returns "" when no usable signature is found.
func topicFromToolInput(toolInput string) string {
	if toolInput == "" {
		return ""
	}
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(toolInput), &raw); err != nil {
		return ""
	}
	// Well-known keys preferred order.
	for _, key := range []string{"command", "cmd", "pattern", "query", "url", "target", "name"} {
		if v, ok := raw[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// truncateRunes returns s limited to n runes (never breaks a multibyte
// character in half).
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

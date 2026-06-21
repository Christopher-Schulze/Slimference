package compression

import (
	"regexp"
	"strings"
)

// rePreFilteredMarker matches compact output markers produced by Layer 0 built-in filters.
// These patterns appear at the start of a tool_result that was already processed by
// "slimference filter" before entering the conversation.
//
// When a tool_result is pre-filtered, Layer 1 skips comment stripping, JSON compact,
// and structure extraction (which would be redundant or could mangle the compact format).
// Dedup, delta, success short-circuit, and repeated collapse still run.
var rePreFilteredMarker = regexp.MustCompile(
	// [git status] clean / [git diff] empty / [git log] empty / [git X] ok, up to date
	`(?i)^\[git\s+\w+\]` +
		// [×N] duplicate-collapse marker
		`|^\[×\d+\]` +
		// TOML filter [ok] / [ok: ...] / [no matches] / [N matches]
		`|^\[ok\]|^\[ok:` +
		`|\[\d+ match` +
		// [full output: path/to/tee] tee recovery hint
		`|\[full output:` +
		// compact git status: "N paths (staged:N worktree:N untracked:N)"
		`|\d+ paths \(staged:\d+` +
		// compact build / test: "[build] ok" / "[test] pass" style
		`|^\[build\]|^\[test\]` +
		// compact search: "[search] N results" / "[grep] N matches"
		`|^\[search\]|^\[grep\]`,
)

// isPreFiltered returns true when content was already processed by a Layer 0 filter
// and should bypass redundant Layer 1 sub-layers (JSON compact, comment strip,
// structure extraction). The function is intentionally conservative: it only fires
// on the unambiguous marker patterns written by built-in filters.
func isPreFiltered(content string) bool {
	if content == "" {
		return false
	}
	// Fast path: all current markers are ASCII and appear on the first line.
	firstLine := content
	if before, _, ok := strings.Cut(content, "\n"); ok {
		firstLine = before
	}
	return rePreFilteredMarker.MatchString(firstLine)
}

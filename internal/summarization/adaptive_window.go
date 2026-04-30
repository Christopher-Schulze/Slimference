package summarization

import (
	"math"
	"strings"

	"github.com/slimference/slimference/internal/types"
)

// AdaptiveWindowSize returns a dynamically adjusted sliding window size based on
// session complexity. The window expands for complex sessions (many files, diverse
// tools, high anchor density) and contracts for simple ones.
//
// BaseWindow is config.SlidingWindow (default 5).
// Range: [max(3, baseWindow-2), baseWindow+2].
func AdaptiveWindowSize(messages []types.Message, baseWindow int) int {
	if len(messages) < baseWindow+2 {
		return baseWindow // not enough messages to adapt
	}

	recentStart := len(messages) - 10
	if recentStart < 0 {
		recentStart = 0
	}
	recentMsgs := messages[recentStart:]

	score := computeComplexityScore(recentMsgs)
	adjusted := baseWindow + int(math.Round(score*4)) - 2

	minWindow := baseWindow - 2
	if minWindow < 3 {
		minWindow = 3
	}

	if adjusted < minWindow {
		return minWindow
	}
	return adjusted
}

// computeComplexityScore returns a 0.0-1.0 complexity score for the given messages.
//
// Score = 0.3 * normalize(UniqueFilePaths, 1, 15)
//   - 0.4 * AnchorDensity
//   - 0.3 * normalize(ToolCallDiversity, 1, 8)
func computeComplexityScore(msgs []types.Message) float64 {
	if len(msgs) == 0 {
		return 0.5
	}

	uniqueFiles := countUniqueFilePaths(msgs)
	anchorFraction := anchorDensity(msgs)
	toolDiversity := countToolDiversity(msgs)

	fileScore := normalizeLinear(float64(uniqueFiles), 1, 15)
	toolScore := normalizeLinear(float64(toolDiversity), 1, 8)

	return 0.3*fileScore + 0.4*anchorFraction + 0.3*toolScore
}

// normalizeLinear maps v onto [0,1] using a linear scale clamped to [lo, hi].
func normalizeLinear(v, lo, hi float64) float64 {
	if hi <= lo {
		return 0
	}
	n := (v - lo) / (hi - lo)
	if n < 0 {
		return 0
	}
	if n > 1 {
		return 1
	}
	return n
}

// countUniqueFilePaths counts distinct file paths referenced in tool_use/tool_result blocks.
func countUniqueFilePaths(msgs []types.Message) int {
	seen := make(map[string]bool)
	for _, msg := range msgs {
		for _, block := range msg.Content {
			if path := extractBlockFilePath(block); path != "" {
				seen[path] = true
			}
		}
	}
	return len(seen)
}

// anchorDensity returns the fraction of messages that qualify as anchor messages.
func anchorDensity(msgs []types.Message) float64 {
	if len(msgs) == 0 {
		return 0
	}
	d := NewAnchorDetector()
	anchors := 0
	for _, msg := range msgs {
		if d.IsAnchor(msg, msgs) {
			anchors++
		}
	}
	return float64(anchors) / float64(len(msgs))
}

// countToolDiversity counts distinct tool names used across messages.
func countToolDiversity(msgs []types.Message) int {
	seen := make(map[string]bool)
	for _, msg := range msgs {
		for _, block := range msg.Content {
			if block.Type == "tool_use" && block.ToolName != "" {
				seen[strings.ToLower(block.ToolName)] = true
			}
		}
	}
	return len(seen)
}

// extractBlockFilePath extracts a file path from a tool_use or tool_result block.
func extractBlockFilePath(block types.ContentBlock) string {
	if block.ToolInput == "" {
		return ""
	}
	// Minimal JSON path extraction matching the compression package's logic
	input := block.ToolInput
	for _, key := range []string{`"path"`, `"file_path"`, `"filename"`, `"filepath"`, `"file"`} {
		idx := strings.Index(input, key)
		if idx < 0 {
			continue
		}
		rest := input[idx+len(key):]
		// Find the colon and the value
		colonIdx := strings.Index(rest, ":")
		if colonIdx < 0 {
			continue
		}
		rest = strings.TrimSpace(rest[colonIdx+1:])
		if len(rest) == 0 || rest[0] != '"' {
			continue
		}
		end := strings.Index(rest[1:], `"`)
		if end < 0 {
			continue
		}
		p := rest[1 : end+1]
		if p != "" {
			return p
		}
	}
	return ""
}

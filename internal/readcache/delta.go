package readcache

import (
	"fmt"
	"strings"
)

const maxDeltaLines = 8

func buildDeltaSummary(path string, oldContent string, newContent string) string {
	oldLines := trimmedLines(oldContent)
	newLines := trimmedLines(newContent)

	added := distinctDiff(newLines, oldLines)
	removed := distinctDiff(oldLines, newLines)
	if len(added) == 0 && len(removed) == 0 {
		return ""
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("Slimference delta for %s:", path))
	for _, line := range added[:limit(len(added), maxDeltaLines)] {
		parts = append(parts, "+ "+line)
	}
	for _, line := range removed[:limit(len(removed), maxDeltaLines)] {
		parts = append(parts, "- "+line)
	}
	if len(added) > maxDeltaLines || len(removed) > maxDeltaLines {
		parts = append(parts, "(delta truncated)")
	}
	return strings.Join(parts, "\n")
}

func trimmedLines(content string) []string {
	raw := strings.Split(content, "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lines = append(lines, trimmed)
	}
	return lines
}

func distinctDiff(a []string, b []string) []string {
	seen := make(map[string]struct{}, len(b))
	for _, line := range b {
		seen[line] = struct{}{}
	}

	out := make([]string, 0, len(a))
	emitted := make(map[string]struct{}, len(a))
	for _, line := range a {
		if _, ok := seen[line]; ok {
			continue
		}
		if _, ok := emitted[line]; ok {
			continue
		}
		emitted[line] = struct{}{}
		out = append(out, line)
	}
	return out
}

func limit(n int, max int) int {
	if n < max {
		return n
	}
	return max
}

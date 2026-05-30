package readcache

import (
	"fmt"
	"strings"
)

const deltaContextLines = 3

func buildDeltaSummary(path string, oldContent string, newContent string) string {
	if oldContent == newContent {
		return ""
	}
	diff := buildPositionAwareDelta(oldContent, newContent, deltaContextLines)
	if diff == "" {
		return ""
	}
	return fmt.Sprintf("Slimference delta for %s:\n%s", path, diff)
}

func buildPositionAwareDelta(oldContent, newContent string, contextLines int) string {
	oldLines := splitDeltaLines(oldContent)
	newLines := splitDeltaLines(newContent)
	prefix := commonPrefixLines(oldLines, newLines)
	suffix := commonSuffixLines(oldLines, newLines, prefix)
	oldChangeEnd := len(oldLines) - suffix
	newChangeEnd := len(newLines) - suffix
	if prefix == oldChangeEnd && prefix == newChangeEnd {
		return ""
	}

	hunkStartOld := prefix - contextLines
	if hunkStartOld < 0 {
		hunkStartOld = 0
	}
	hunkStartNew := prefix - contextLines
	if hunkStartNew < 0 {
		hunkStartNew = 0
	}
	hunkEndOld := oldChangeEnd + contextLines
	if hunkEndOld > len(oldLines) {
		hunkEndOld = len(oldLines)
	}
	hunkEndNew := newChangeEnd + contextLines
	if hunkEndNew > len(newLines) {
		hunkEndNew = len(newLines)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", hunkStartOld+1, hunkEndOld-hunkStartOld, hunkStartNew+1, hunkEndNew-hunkStartNew)
	writeLine := func(prefix, line string) {
		b.WriteString(prefix)
		b.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			b.WriteByte('\n')
		}
	}
	for _, line := range oldLines[hunkStartOld:prefix] {
		writeLine(" ", line)
	}
	for _, line := range oldLines[prefix:oldChangeEnd] {
		writeLine("-", line)
	}
	for _, line := range newLines[prefix:newChangeEnd] {
		writeLine("+", line)
	}
	for _, line := range newLines[newChangeEnd:hunkEndNew] {
		writeLine(" ", line)
	}
	return strings.TrimRight(b.String(), "\n")
}

func splitDeltaLines(content string) []string {
	lines := strings.SplitAfter(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func commonPrefixLines(a, b []string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return i
}

func commonSuffixLines(a, b []string, prefix int) int {
	maxA := len(a) - prefix
	maxB := len(b) - prefix
	n := maxA
	if maxB < n {
		n = maxB
	}
	i := 0
	for i < n && a[len(a)-1-i] == b[len(b)-1-i] {
		i++
	}
	return i
}

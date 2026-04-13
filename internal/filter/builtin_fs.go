package filter

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var reTreeSummary = regexp.MustCompile(`\d+ director`)

// TryCompactLs summarizes empty stdout from `ls`; non-empty → entry count (F11).
func TryCompactLs(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "ls" && b != "ls.exe" {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[ls] empty\n"), true
	}
	compact := compactLsOutput(s)
	if compact == "" || len(compact) >= len(s) {
		return stdout, false
	}
	return []byte(compact), true
}

// compactLsOutput counts entries and returns a summary if output is large enough.
func compactLsOutput(s string) string {
	lines := strings.Split(s, "\n")
	var entries []string
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t != "" && t != "total 0" && !strings.HasPrefix(t, "total ") {
			entries = append(entries, t)
		}
	}
	if len(entries) == 0 {
		return "[ls] empty\n"
	}
	if len(entries) <= 10 {
		return "" // short enough, pass through
	}
	return fmt.Sprintf("[ls] %d entries\n", len(entries))
}

// TryCompactTree summarizes empty stdout from `tree`; non-empty → summary line (F11).
func TryCompactTree(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "tree" && b != "tree.exe" {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[tree] empty\n"), true
	}
	compact := compactTreeOutput(s)
	if compact == "" || len(compact) >= len(s) {
		return stdout, false
	}
	return []byte(compact), true
}

// compactTreeOutput extracts the trailing summary line from tree output (e.g. "3 directories, 12 files").
func compactTreeOutput(s string) string {
	lines := strings.Split(s, "\n")
	// Find summary line: last non-empty line containing "director"
	var summaryLine string
	for i := len(lines) - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if t != "" && reTreeSummary.MatchString(t) {
			summaryLine = t
			break
		}
	}
	if summaryLine == "" {
		return ""
	}
	return fmt.Sprintf("[tree] %s\n", summaryLine)
}

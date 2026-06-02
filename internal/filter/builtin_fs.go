package filter

import (
	"path/filepath"
	"strings"
)

// TryCompactLs summarizes empty stdout from `ls`. Non-empty directory listings
// full-pass because file names are the evidence the model asked for.
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
	if lsOnlyTotalLines(s) {
		return []byte("[ls] empty\n"), true
	}
	return stdout, false
}

// TryCompactTree summarizes empty stdout from `tree`. Non-empty tree output
// full-passes because path names and hierarchy are model-relevant evidence.
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
	return stdout, false
}

func lsOnlyTotalLines(s string) bool {
	lines := strings.Split(s, "\n")
	sawLine := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		sawLine = true
		if trimmed != "total 0" && !strings.HasPrefix(trimmed, "total ") {
			return false
		}
	}
	return sawLine
}

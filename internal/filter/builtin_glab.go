package filter

import (
	"fmt"
	"path/filepath"
	"strings"
)

var glabListSubcommands = map[string]bool{
	"mr": true, "issue": true, "pipeline": true, "ci": true, "incident": true,
	"registry": true, "job": true, "label": true, "milestone": true, "release": true,
	"variable": true, "deploy-key": true, "snippet": true, "branch": true, "repo": true,
	"schedule": true, "runner": true, "token": true, "cluster": true,
}

const glabListMaxRows = 15

// TryCompactGlabList summarizes `glab … list` output (F18-style): empty → "empty"; many rows → count + preview.
func TryCompactGlabList(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 3 {
		return stdout, false
	}
	if filepath.Base(argv[0]) != "glab" {
		return stdout, false
	}
	if argv[2] != "list" {
		return stdout, false
	}
	sub := argv[1]
	if !glabListSubcommands[sub] {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte(fmt.Sprintf("[glab %s list] empty\n", sub)), true
	}
	// Non-empty: count rows and truncate if large.
	lines := strings.Split(s, "\n")
	var rows []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			rows = append(rows, l)
		}
	}
	if len(rows) <= glabListMaxRows {
		return stdout, false
	}
	header := fmt.Sprintf("[glab %s list] %d items\n", sub, len(rows))
	preview := strings.Join(rows[:glabListMaxRows], "\n")
	more := len(rows) - glabListMaxRows
	out := fmt.Sprintf("%s%s\n... +%d more\n", header, preview, more)
	if len(out) >= len(s) {
		return stdout, false
	}
	return []byte(out), true
}

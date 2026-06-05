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

// TryCompactGlabList summarizes `glab … list` output (F18-style): empty →
// "empty"; many rows compact only when diagnostic attention rows are present.
// Healthy non-empty lists are model evidence and must full-pass.
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
	// Non-empty: preserve healthy lists. For large diagnostic lists, keep the
	// attention rows and a count.
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
	out := compactCLIListRows(fmt.Sprintf("glab %s list", sub), rows, glabListMaxRows)
	if out == "" {
		return stdout, false
	}
	if len(out) >= len(s) {
		return stdout, false
	}
	return []byte(out), true
}

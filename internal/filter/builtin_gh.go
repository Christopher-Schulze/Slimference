package filter

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ghListSubcommands is the set of gh subcommands that support "list".
var ghListSubcommands = map[string]bool{
	"pr": true, "issue": true, "run": true, "release": true, "workflow": true,
	"alias": true, "gist": true, "label": true, "cache": true, "secret": true,
	"repo": true, "codespace": true, "extension": true, "variable": true,
	"ruleset": true, "project": true, "gpg-key": true, "ssh-key": true,
	"org": true, "milestone": true, "auth": true, "config": true,
	"team": true, "sponsor": true, "agent-task": true,
}

const ghListMaxRows = 15

// TryCompactGhList summarizes `gh … list` output (F18): empty → "empty"; many rows → count + first N rows.
func TryCompactGhList(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 3 {
		return stdout, false
	}
	if filepath.Base(argv[0]) != "gh" {
		return stdout, false
	}
	if argv[2] != "list" {
		return stdout, false
	}
	sub := argv[1]
	if !ghListSubcommands[sub] {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte(fmt.Sprintf("[gh %s list] empty\n", sub)), true
	}
	// Non-empty: count rows and truncate if large.
	lines := strings.Split(s, "\n")
	var rows []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			rows = append(rows, l)
		}
	}
	if len(rows) <= ghListMaxRows {
		return stdout, false // short enough
	}
	header := fmt.Sprintf("[gh %s list] %d items\n", sub, len(rows))
	preview := strings.Join(rows[:ghListMaxRows], "\n")
	more := len(rows) - ghListMaxRows
	out := fmt.Sprintf("%s%s\n... +%d more\n", header, preview, more)
	if len(out) >= len(s) {
		return stdout, false
	}
	return []byte(out), true
}

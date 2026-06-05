package filter

import (
	"fmt"
	"path/filepath"
	"sort"
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

// TryCompactGhList summarizes `gh … list` output (F18): empty → "empty"; many
// rows compact only when diagnostic attention rows are present. Healthy
// non-empty lists are model evidence and must full-pass.
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
	// Non-empty: preserve healthy lists. For large diagnostic lists, keep the
	// attention rows and a count.
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
	out := compactCLIListRows(fmt.Sprintf("gh %s list", sub), rows, ghListMaxRows)
	if out == "" {
		return stdout, false
	}
	if len(out) >= len(s) {
		return stdout, false
	}
	return []byte(out), true
}

func compactCLIListRows(label string, rows []string, maxRows int) string {
	if maxRows <= 0 || len(rows) <= maxRows {
		return ""
	}
	selected := make(map[int]struct{}, maxRows)
	attention := 0
	for i, row := range rows {
		if !cliListRowNeedsAttention(row) {
			continue
		}
		attention++
		if len(selected) < maxRows {
			selected[i] = struct{}{}
		}
	}
	if attention == 0 {
		return ""
	}
	for i := 0; i < len(rows) && len(selected) < maxRows; i++ {
		selected[i] = struct{}{}
	}
	indexes := make([]int, 0, len(selected))
	for i := range selected {
		indexes = append(indexes, i)
	}
	sort.Ints(indexes)
	var b strings.Builder
	if attention > 0 {
		fmt.Fprintf(&b, "[%s] %d items, %d attention row(s)\n", label, len(rows), attention)
	} else {
		fmt.Fprintf(&b, "[%s] %d items\n", label, len(rows))
	}
	for _, i := range indexes {
		b.WriteString(rows[i])
		b.WriteByte('\n')
	}
	if omitted := len(rows) - len(indexes); omitted > 0 {
		fmt.Fprintf(&b, "... +%d more\n", omitted)
	}
	return b.String()
}

func cliListRowNeedsAttention(row string) bool {
	lower := strings.ToLower(row)
	for _, token := range []string{
		"blocked",
		"cancelled",
		"canceled",
		"conflict",
		"critical",
		"error",
		"fail",
		"red",
		"security",
		"timed out",
		"timeout",
		"unhealthy",
		"vulnerab",
	} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

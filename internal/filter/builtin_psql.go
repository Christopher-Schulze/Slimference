package filter

import (
	"path/filepath"
	"regexp"
	"strings"
)

// rePsqlSeparator matches psql ASCII table separator lines like "----+------+----".
var rePsqlSeparator = regexp.MustCompile(`^[-+]+$`)

// TryCompactPsql compacts psql ASCII table output (F19): strips separator lines and trims column padding.
func TryCompactPsql(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "psql" && b != "psql.exe" {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[psql] ok\n"), true
	}
	compact := compactPsqlOutput(s)
	if compact == "" || len(compact) >= len(s) {
		return stdout, false
	}
	return []byte(compact), true
}

// compactPsqlOutput removes ASCII table borders and normalizes column spacing.
func compactPsqlOutput(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Skip separator lines like "----+------+-------".
		if rePsqlSeparator.MatchString(trimmed) {
			continue
		}
		// For table rows with |, trim cell padding.
		if strings.Contains(line, "|") {
			cols := strings.Split(line, "|")
			trimmed := make([]string, len(cols))
			for i, c := range cols {
				trimmed[i] = strings.TrimSpace(c)
			}
			// Drop empty leading/trailing empty columns from the border style " val | val ".
			start, end := 0, len(trimmed)-1
			for start <= end && trimmed[start] == "" {
				start++
			}
			for end >= start && trimmed[end] == "" {
				end--
			}
			if start <= end {
				out = append(out, strings.Join(trimmed[start:end+1], " | "))
			}
			continue
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

package filter

import (
	"fmt"
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

type wcColumn string

const (
	wcColumnLines         wcColumn = "L"
	wcColumnWords         wcColumn = "W"
	wcColumnChars         wcColumn = "Ch"
	wcColumnBytes         wcColumn = "B"
	wcColumnMaxLineLength wcColumn = "Max"
)

type wcRow struct {
	Counts []string
	Path   string
	Total  bool
}

// TryCompactWc compacts deterministic `wc` output while preserving every count,
// the requested units, file names, and total rows. Unknown flags or ambiguous
// rows fail open to the original output.
func TryCompactWc(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "wc" && b != "wc.exe" {
		return stdout, false
	}
	columns, ok := wcColumnsFromArgv(argv[1:])
	if !ok {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return stdout, false
	}
	rows, ok := parseWcRows(s, columns)
	if !ok {
		return stdout, false
	}
	out := formatWcRows(rows, columns)
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out), true
}

func wcColumnsFromArgv(args []string) ([]wcColumn, bool) {
	selected := map[wcColumn]bool{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			continue
		}
		if strings.HasPrefix(arg, "--") {
			switch {
			case arg == "--lines":
				selected[wcColumnLines] = true
			case arg == "--words":
				selected[wcColumnWords] = true
			case arg == "--chars":
				selected[wcColumnChars] = true
			case arg == "--bytes":
				selected[wcColumnBytes] = true
			case arg == "--max-line-length":
				selected[wcColumnMaxLineLength] = true
			default:
				return nil, false
			}
			continue
		}
		for _, ch := range arg[1:] {
			switch ch {
			case 'l':
				selected[wcColumnLines] = true
			case 'w':
				selected[wcColumnWords] = true
			case 'm':
				selected[wcColumnChars] = true
			case 'c':
				selected[wcColumnBytes] = true
			case 'L':
				selected[wcColumnMaxLineLength] = true
			default:
				return nil, false
			}
		}
	}
	if len(selected) == 0 {
		return []wcColumn{wcColumnLines, wcColumnWords, wcColumnBytes}, true
	}
	order := []wcColumn{wcColumnLines, wcColumnWords, wcColumnChars, wcColumnBytes, wcColumnMaxLineLength}
	columns := make([]wcColumn, 0, len(selected))
	for _, column := range order {
		if selected[column] {
			columns = append(columns, column)
		}
	}
	return columns, len(columns) > 0
}

func parseWcRows(s string, columns []wcColumn) ([]wcRow, bool) {
	lines := strings.Split(s, "\n")
	rows := make([]wcRow, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" {
			continue
		}
		counts, rest, ok := parseLeadingWcCounts(line, len(columns))
		if !ok {
			return nil, false
		}
		rows = append(rows, wcRow{
			Counts: counts,
			Path:   rest,
			Total:  rest == "total",
		})
	}
	if len(rows) == 0 {
		return nil, false
	}
	if len(rows) > 1 {
		for _, row := range rows {
			if row.Path == "" {
				return nil, false
			}
		}
	}
	return rows, true
}

func parseLeadingWcCounts(line string, n int) ([]string, string, bool) {
	counts := make([]string, 0, n)
	i := 0
	for len(counts) < n {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		start := i
		for i < len(line) && line[i] >= '0' && line[i] <= '9' {
			i++
		}
		if start == i {
			return nil, "", false
		}
		if i < len(line) && line[i] != ' ' && line[i] != '\t' {
			return nil, "", false
		}
		counts = append(counts, line[start:i])
	}
	return counts, strings.TrimSpace(line[i:]), true
}

func formatWcRows(rows []wcRow, columns []wcColumn) string {
	if len(rows) == 1 {
		return formatWcRow(rows[0], columns, "") + "\n"
	}
	paths := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Path != "" && !row.Total {
			paths = append(paths, row.Path)
		}
	}
	prefix := commonDirectoryPrefix(paths)
	var sb strings.Builder
	if prefix != "" {
		sb.WriteString("[wc prefix=")
		sb.WriteString(prefix)
		sb.WriteString("]\n")
	}
	for _, row := range rows {
		sb.WriteString(formatWcRow(row, columns, prefix))
		sb.WriteByte('\n')
	}
	return sb.String()
}

func formatWcRow(row wcRow, columns []wcColumn, prefix string) string {
	counts := make([]string, 0, len(row.Counts))
	for i, count := range row.Counts {
		counts = append(counts, count+string(columns[i]))
	}
	countPart := strings.Join(counts, " ")
	if row.Path == "" {
		return countPart
	}
	if row.Total {
		return "total: " + countPart
	}
	path := strings.TrimPrefix(row.Path, prefix)
	if path == "" {
		path = row.Path
	}
	return fmt.Sprintf("%s: %s", path, countPart)
}

func commonDirectoryPrefix(paths []string) string {
	if len(paths) <= 1 {
		return ""
	}
	first := paths[0]
	pos := strings.LastIndex(first, "/")
	if pos < 0 {
		return ""
	}
	candidate := first[:pos+1]
	for candidate != "" {
		allMatch := true
		for _, path := range paths[1:] {
			if !strings.HasPrefix(path, candidate) {
				allMatch = false
				break
			}
		}
		if allMatch {
			return candidate
		}
		trimmed := strings.TrimSuffix(candidate, "/")
		next := strings.LastIndex(trimmed, "/")
		if next < 0 {
			return ""
		}
		candidate = trimmed[:next+1]
	}
	return ""
}

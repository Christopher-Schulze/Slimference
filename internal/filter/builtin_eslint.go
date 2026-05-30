package filter

import (
	"encoding/json"
	"fmt"
	"strings"
)

// isEslintJSONArgv reports whether argv is an eslint invocation requesting the
// native JSON formatter (eslint ... --format json / -f json / --format=json).
func isEslintJSONArgv(argv []string) bool {
	hasEslint := false
	for _, a := range argv {
		base := a
		if i := strings.LastIndexByte(base, '/'); i >= 0 {
			base = base[i+1:]
		}
		if base == "eslint" || base == "eslint.js" || strings.HasSuffix(base, "-eslint") {
			hasEslint = true
			break
		}
	}
	if !hasEslint {
		return false
	}
	for i, a := range argv {
		switch {
		case a == "--format=json", a == "-f=json":
			return true
		case (a == "--format" || a == "-f") && i+1 < len(argv) && argv[i+1] == "json":
			return true
		}
	}
	return false
}

// TryCompactEslintJSON compacts `eslint --format json` output (a top-level array
// of files, each carrying messages). Clean runs collapse to a one-line summary;
// runs with problems keep error-severity messages first, then warnings, capped,
// as `file:line:col severity [rule] message`. Faithful: every error/warning count
// is reported and errors are never dropped before warnings.
func TryCompactEslintJSON(argv []string, stdout []byte) ([]byte, bool) {
	if !isEslintJSONArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" || s[0] != '[' {
		return stdout, false
	}
	type message struct {
		RuleID   string `json:"ruleId"`
		Severity int    `json:"severity"`
		Message  string `json:"message"`
		Line     int    `json:"line"`
		Column   int    `json:"column"`
	}
	type file struct {
		FilePath     string    `json:"filePath"`
		Messages     []message `json:"messages"`
		ErrorCount   int       `json:"errorCount"`
		WarningCount int       `json:"warningCount"`
	}
	var files []file
	if err := json.Unmarshal([]byte(s), &files); err != nil {
		return stdout, false
	}

	totalErr, totalWarn := 0, 0
	for _, f := range files {
		totalErr += f.ErrorCount
		totalWarn += f.WarningCount
	}
	if totalErr == 0 && totalWarn == 0 {
		out := fmt.Sprintf("[eslint] clean (%d file(s))\n", len(files))
		if len(out) >= len(stdout) {
			return stdout, false
		}
		return []byte(out), true
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[eslint] %d error(s), %d warning(s) in %d file(s)\n", totalErr, totalWarn, len(files))
	const maxRows = 20
	rows := 0
	emit := func(wantSeverity int) bool {
		for _, f := range files {
			for _, m := range f.Messages {
				if m.Severity != wantSeverity {
					continue
				}
				if rows >= maxRows {
					return false
				}
				sev := "warning"
				if m.Severity == 2 {
					sev = "error"
				}
				rule := m.RuleID
				if rule == "" {
					rule = "-"
				}
				fmt.Fprintf(&b, "  %s:%d:%d %s [%s] %s\n", f.FilePath, m.Line, m.Column, sev, rule, truncateEslintMessage(m.Message))
				rows++
			}
		}
		return true
	}
	if emit(2) { // errors first
		emit(1) // then warnings
	}
	if totalErr+totalWarn > rows {
		fmt.Fprintf(&b, "  ... +%d more problem(s)\n", totalErr+totalWarn-rows)
	}
	out := b.String()
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out), true
}

func truncateEslintMessage(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	const limit = 160
	if len(s) > limit {
		return s[:limit] + "..."
	}
	return s
}

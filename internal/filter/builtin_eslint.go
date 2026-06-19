package filter

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// isEslintJSONArgv reports whether argv is an eslint invocation requesting the
// native JSON formatter (eslint ... --format json / -f json / --format=json).
func isEslintJSONArgv(argv []string) bool {
	hasEslint := false
	for _, a := range argv {
		base := strings.ToLower(a)
		if i := strings.LastIndexAny(base, `/\`); i >= 0 {
			base = base[i+1:]
		}
		switch {
		case base == "eslint", base == "eslint.js", base == "eslint.cmd", base == "eslint.exe":
			hasEslint = true
		case strings.HasSuffix(base, "-eslint"), strings.HasSuffix(base, "-eslint.js"),
			strings.HasSuffix(base, "-eslint.cmd"), strings.HasSuffix(base, "-eslint.exe"):
			hasEslint = true
		}
		if hasEslint {
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
	type problemRow struct {
		filePath string
		message  message
	}
	collect := func(wantSeverity int) []problemRow {
		rows := make([]problemRow, 0)
		for _, f := range files {
			for _, m := range f.Messages {
				if m.Severity != wantSeverity {
					continue
				}
				rows = append(rows, problemRow{filePath: f.FilePath, message: m})
			}
		}
		return rows
	}
	emitRows := func(problemRows []problemRow, limit int) int {
		emitted := 0
		for _, idx := range cappedEvidenceIndexes(len(problemRows), limit, 4) {
			row := problemRows[idx]
			m := row.message
			sev := "warning"
			if m.Severity == 2 {
				sev = "error"
			}
			rule := m.RuleID
			if rule == "" {
				rule = "-"
			}
			fmt.Fprintf(&b, "  %s:%d:%d %s [%s] %s\n", row.filePath, m.Line, m.Column, sev, rule, truncateEslintMessage(m.Message))
			emitted++
		}
		return emitted
	}
	rows := 0
	errorRows := collect(2)
	rows += emitRows(errorRows, maxRows)
	if rows < maxRows {
		rows += emitRows(collect(1), maxRows-rows)
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

// TryCompactEslintStylish compacts parser-proven ESLint stylish findings while
// preserving every file, line, column, severity, message, rule ID, summary
// count, and fixable-count line. Clean stylish output is empty and stays handled
// by TryCompactEslint.
func TryCompactEslintStylish(argv []string, stdout []byte) ([]byte, bool) {
	if !isEslintStylishArgv(argv) {
		return stdout, false
	}
	out, ok := compactEslintStylishFindings(string(stdout))
	if !ok || len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out), true
}

func isEslintStylishArgv(argv []string) bool {
	if !commandMatchesAny(argv, "eslint") {
		return false
	}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		switch {
		case arg == "--format" || arg == "-f":
			if i+1 >= len(argv) || argv[i+1] != "stylish" {
				return false
			}
			i++
		case strings.HasPrefix(arg, "--format="):
			if strings.TrimPrefix(arg, "--format=") != "stylish" {
				return false
			}
		case strings.HasPrefix(arg, "-f="):
			if strings.TrimPrefix(arg, "-f=") != "stylish" {
				return false
			}
		}
	}
	return true
}

type eslintStylishFinding struct {
	file     string
	line     int
	column   int
	severity string
	message  string
	rule     string
}

func compactEslintStylishFindings(stdout string) (string, bool) {
	if strings.TrimSpace(stdout) == "" || strings.ContainsRune(stdout, '\x1b') || strings.Contains(stdout, "\r") {
		return "", false
	}
	lines := strings.Split(stdout, "\n")
	currentFile := ""
	seenFiles := make(map[string]struct{})
	findings := make([]eslintStylishFinding, 0)
	summarySeen := false
	summaryTotal, summaryErrors, summaryWarnings := 0, 0, 0
	fixableLine := ""

	for _, raw := range lines {
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "✖ ") {
			if summarySeen {
				return "", false
			}
			total, errors, warnings, ok := parseEslintStylishSummary(trimmed)
			if !ok {
				return "", false
			}
			summarySeen = true
			summaryTotal, summaryErrors, summaryWarnings = total, errors, warnings
			continue
		}
		if summarySeen {
			errors, warnings, ok := parseEslintStylishFixableLine(trimmed)
			if !ok || fixableLine != "" || errors > summaryErrors || warnings > summaryWarnings {
				return "", false
			}
			fixableLine = trimmed
			continue
		}
		if finding, ok := parseEslintStylishFindingLine(trimmed, currentFile); ok {
			findings = append(findings, finding)
			continue
		}
		if eslintStylishFileLineOK(trimmed) {
			currentFile = trimmed
			seenFiles[currentFile] = struct{}{}
			continue
		}
		return "", false
	}

	if !summarySeen || summaryTotal == 0 || len(findings) != summaryTotal {
		return "", false
	}
	errors, warnings := 0, 0
	for _, finding := range findings {
		switch finding.severity {
		case "error":
			errors++
		case "warning":
			warnings++
		default:
			return "", false
		}
	}
	if errors != summaryErrors || warnings != summaryWarnings || summaryTotal != summaryErrors+summaryWarnings {
		return "", false
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[eslint] FINDINGS (%d %s: %d %s, %d %s in %d %s)\n",
		summaryTotal, pluralWord(summaryTotal, "problem", "problems"),
		summaryErrors, pluralWord(summaryErrors, "error", "errors"),
		summaryWarnings, pluralWord(summaryWarnings, "warning", "warnings"),
		len(seenFiles), pluralWord(len(seenFiles), "file", "files"))
	lastFile := ""
	for _, finding := range findings {
		if finding.file != lastFile {
			b.WriteString(finding.file)
			b.WriteByte('\n')
			lastFile = finding.file
		}
		fmt.Fprintf(&b, "  %d:%d %s [%s] %s\n",
			finding.line, finding.column, finding.severity, finding.rule, finding.message)
	}
	if fixableLine != "" {
		b.WriteString(fixableLine)
		b.WriteByte('\n')
	}
	return b.String(), true
}

func parseEslintStylishFindingLine(line string, currentFile string) (eslintStylishFinding, bool) {
	if currentFile == "" {
		return eslintStylishFinding{}, false
	}
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return eslintStylishFinding{}, false
	}
	lineNo, column, ok := parseEslintStylishLocation(fields[0])
	if !ok {
		return eslintStylishFinding{}, false
	}
	severity := fields[1]
	if severity != "error" && severity != "warning" {
		return eslintStylishFinding{}, false
	}
	rule := fields[len(fields)-1]
	if !eslintRuleIDOK(rule) {
		return eslintStylishFinding{}, false
	}
	message := strings.TrimSpace(strings.Join(fields[2:len(fields)-1], " "))
	if message == "" || sourceKeywordLine(message) || outputLineLooksLikeSourceContext(message) {
		return eslintStylishFinding{}, false
	}
	return eslintStylishFinding{
		file:     currentFile,
		line:     lineNo,
		column:   column,
		severity: severity,
		message:  message,
		rule:     rule,
	}, true
}

func parseEslintStylishLocation(raw string) (int, int, bool) {
	lineText, columnText, ok := strings.Cut(raw, ":")
	if !ok {
		return 0, 0, false
	}
	lineNo, err := strconv.Atoi(lineText)
	if err != nil || lineNo <= 0 {
		return 0, 0, false
	}
	column, err := strconv.Atoi(columnText)
	if err != nil || column <= 0 {
		return 0, 0, false
	}
	return lineNo, column, true
}

func parseEslintStylishSummary(line string) (int, int, int, bool) {
	fields := strings.Fields(line)
	if len(fields) != 7 || fields[0] != "✖" {
		return 0, 0, 0, false
	}
	total, err := strconv.Atoi(fields[1])
	if err != nil || total <= 0 || !eslintPluralOK(total, fields[2], "problem", "problems") {
		return 0, 0, 0, false
	}
	errorCount, err := strconv.Atoi(strings.TrimPrefix(fields[3], "("))
	if err != nil || errorCount < 0 || !eslintPluralOK(errorCount, strings.TrimSuffix(fields[4], ","), "error", "errors") {
		return 0, 0, 0, false
	}
	warningCount, err := strconv.Atoi(fields[5])
	if err != nil || warningCount < 0 || !eslintPluralOK(warningCount, strings.TrimSuffix(fields[6], ")"), "warning", "warnings") {
		return 0, 0, 0, false
	}
	return total, errorCount, warningCount, total == errorCount+warningCount
}

func parseEslintStylishFixableLine(line string) (int, int, bool) {
	fields := strings.Fields(line)
	if len(fields) != 11 ||
		fields[2] != "and" ||
		fields[5] != "potentially" ||
		fields[6] != "fixable" ||
		fields[7] != "with" ||
		fields[8] != "the" ||
		fields[9] != "`--fix`" ||
		fields[10] != "option." {
		return 0, 0, false
	}
	errors, err := strconv.Atoi(fields[0])
	if err != nil || errors < 0 || !eslintPluralOK(errors, fields[1], "error", "errors") {
		return 0, 0, false
	}
	warnings, err := strconv.Atoi(fields[3])
	if err != nil || warnings < 0 || !eslintPluralOK(warnings, fields[4], "warning", "warnings") {
		return 0, 0, false
	}
	return errors, warnings, true
}

func eslintStylishFileLineOK(line string) bool {
	if line == "" || strings.ContainsRune(line, '\x1b') || sourceKeywordLine(line) || outputLineLooksLikeSourceContext(line) {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(line))
	for _, ext := range []string{".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts", ".vue", ".svelte", ".astro"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func eslintRuleIDOK(rule string) bool {
	if rule == "" || len(rule) > 140 {
		return false
	}
	for _, r := range rule {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '@' || r == '/' || r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}

func eslintPluralOK(count int, got, singular, plural string) bool {
	if count == 1 {
		return got == singular
	}
	return got == plural
}

func truncateEslintMessage(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	const limit = 160
	if len(s) > limit {
		return s[:limit] + "..."
	}
	return s
}

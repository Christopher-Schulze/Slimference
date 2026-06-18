package filter

import (
	"regexp"
	"strings"
)

var reCargoError = regexp.MustCompile(`^error(?:\[[A-Z]\d+\])?:\s`)
var reCargoWarning = regexp.MustCompile(`^warning(?:\[[A-Z]\d+\])?:\s`)
var reCargoSpan = regexp.MustCompile(`^\s*--> `)
var reCargoTestFail = regexp.MustCompile(`\sFAILED$`)
var reCargoPanic = regexp.MustCompile(`^thread '.*' panicked at`)

func parseCargoErrors(stdout string) (string, bool, bool) {
	return parseCargoErrorsWithLabel(stdout, "cargo build")
}

func parseCargoErrorsForArgv(argv []string, stdout string) (string, bool, bool) {
	return parseCargoErrorsWithLabel(stdout, cargoDiagnosticLabel(argv))
}

func cargoDiagnosticLabel(argv []string) string {
	switch {
	case isCargoCheckArgv(argv):
		return "cargo check"
	case len(argv) >= 2 && isCargoBin(argv[0]) && argv[1] == "clippy":
		return "cargo clippy"
	case isCargoBuildArgv(argv):
		return "cargo build"
	default:
		return "cargo build"
	}
}

func parseCargoErrorsWithLabel(stdout string, label string) (string, bool, bool) {
	if stdout == "" {
		return "", false, false
	}
	lines := strings.Split(stdout, "\n")
	type errEntry struct {
		lineNo  int
		content string
	}
	var errs []errEntry

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if reCargoError.MatchString(trimmed) {
			errs = append(errs, errEntry{i, trimmed})
		} else if reCargoPanic.MatchString(trimmed) {
			errs = append(errs, errEntry{i, trimmed})
		}
	}

	if len(errs) == 0 {
		if detectBuildSuccess(stdout) {
			if buildOutputHasNonZeroWarning(stdout) {
				return "", false, false
			}
			return "[" + label + "] ok\n", false, true
		}
		return "", false, false
	}

	kept := map[int]bool{}
	for _, e := range errs {
		for j := e.lineNo; j < len(lines); j++ {
			kept[j] = true
			if j > e.lineNo && strings.TrimSpace(lines[j]) == "" {
				break
			}
		}
	}

	var out strings.Builder
	out.WriteString("[")
	out.WriteString(label)
	out.WriteString("] FAILED\n")
	for i, line := range lines {
		if kept[i] {
			out.WriteString(strings.TrimRight(line, "\r"))
			out.WriteByte('\n')
		}
	}
	result := out.String()
	if len(result) >= len(stdout) {
		return "", false, false
	}
	return result, true, true
}

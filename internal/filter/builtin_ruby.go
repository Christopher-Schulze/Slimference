package filter

import (
	"path/filepath"
	"strings"
)

func isRakeArgv(argv []string) bool {
	if len(argv) < 1 {
		return false
	}
	return strings.ToLower(filepath.Base(argv[0])) == "rake"
}

// TryCompactRake summarizes empty stdout from `rake` / `npx|pnpm exec|yarn … rake` (F21 partial).
func TryCompactRake(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if isRakeArgv(argv) {
		return []byte("[rake] ok\n"), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && isRakeArgv(rest) {
		return []byte("[rake] ok\n"), true
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && isRakeArgv(argv[2:]) {
		return []byte("[rake] ok\n"), true
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && isRakeArgv(argv[1:]) {
		return []byte("[rake] ok\n"), true
	}
	return stdout, false
}

func isRspecDirectOrBundleExec(argv []string) bool {
	if len(argv) < 1 {
		return false
	}
	if len(argv) >= 3 {
		b0 := strings.ToLower(filepath.Base(argv[0]))
		b2 := strings.ToLower(filepath.Base(argv[2]))
		if b0 == "bundle" && argv[1] == "exec" && (b2 == "rspec" || b2 == "rspec.cmd") {
			return true
		}
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	return b == "rspec" || b == "rspec.cmd"
}

// TryCompactRspec summarizes rspec output (F21): empty → ok; non-empty → failure focus.
func TryCompactRspec(argv []string, stdout []byte) ([]byte, bool) {
	isRspec := isRspecDirectOrBundleExec(argv)
	if !isRspec {
		if rest, ok := npxArgvSuffix(argv); ok && isRspecDirectOrBundleExec(rest) {
			isRspec = true
		}
	}
	if !isRspec {
		b0 := strings.ToLower(filepath.Base(argv[0]))
		if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" &&
			isRspecDirectOrBundleExec(argv[2:]) {
			isRspec = true
		}
		if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") &&
			isRspecDirectOrBundleExec(argv[1:]) {
			isRspec = true
		}
	}
	if !isRspec {
		return stdout, false
	}

	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[rspec] ok\n"), true
	}

	compact := compactRspecOutput(s)
	if compact == "" || len(compact) >= len(s) {
		return stdout, false
	}
	return []byte(compact), true
}

// compactRspecOutput extracts failure details and the summary line from rspec output.
func compactRspecOutput(s string) string {
	lines := strings.Split(s, "\n")

	// Find "Failures:" section and summary line.
	var failures []string
	var summaryLine string
	inFailures := false

	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")

		// Summary line: "X examples, Y failures" or "X examples, Y failure"
		if strings.Contains(line, "example") && (strings.Contains(line, "failure") || strings.Contains(line, "passed")) {
			summaryLine = strings.TrimSpace(line)
		}

		if strings.TrimSpace(line) == "Failures:" {
			inFailures = true
			continue
		}
		// Stop at "Finished in" or empty sections after failures.
		if inFailures && strings.HasPrefix(strings.TrimSpace(line), "Finished in") {
			inFailures = false
			continue
		}
		if inFailures {
			failures = append(failures, line)
		}
	}

	// Trim trailing empty lines from failures.
	for len(failures) > 0 && strings.TrimSpace(failures[len(failures)-1]) == "" {
		failures = failures[:len(failures)-1]
	}

	if summaryLine == "" && len(failures) == 0 {
		return ""
	}

	var sb strings.Builder
	if len(failures) > 0 {
		sb.WriteString("Failures:\n")
		for _, f := range failures {
			sb.WriteString(f)
			sb.WriteByte('\n')
		}
	}
	if summaryLine != "" {
		sb.WriteString(summaryLine)
		sb.WriteByte('\n')
	}

	// Success detection: no failures and all passed.
	// "0 failures" contains "failure", so check for "0 failure" explicitly.
	if len(failures) == 0 && summaryLine != "" &&
		(strings.Contains(summaryLine, "0 failure") || !strings.Contains(strings.ToLower(summaryLine), "failure")) {
		return "[rspec] ok (" + summaryLine + ")\n"
	}

	return sb.String()
}

// TryCompactRubyOutput chains rake / rspec empty-success summaries (rubocop → lint.go).
func TryCompactRubyOutput(argv []string, stdout []byte) ([]byte, bool) {
	if out, ok := TryCompactRake(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactRspec(argv, stdout); ok {
		return out, true
	}
	return stdout, false
}

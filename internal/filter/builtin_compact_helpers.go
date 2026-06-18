package filter

import (
	"fmt"
	"path/filepath"
	"strings"
)

// isSingleBinarySubcmdArgv checks argv[0]==bin (optionally argv[1]==sub) across direct/npx/pnpm exec/yarn wrappers.
// sub="" means no subcommand required (just the binary name).
func isSingleBinarySubcmdArgv(argv []string, bin, sub string) bool {
	if len(argv) < 1 {
		return false
	}
	return isSingleBinarySubcmdDirect(argv, bin, sub)
}

func isSingleBinarySubcmdDirect(argv []string, bin, sub string) bool {
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 1 {
			return false
		}
		return isSingleBinarySubcmdDirect(rest, bin, sub)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isSingleBinarySubcmdDirect(argv[2:], bin, sub)
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isSingleBinarySubcmdDirect(argv[1:], bin, sub)
	}
	if b0 != bin && b0 != bin+".exe" && b0 != bin+".cmd" {
		return false
	}
	if sub == "" {
		return true
	}
	return len(argv) >= 2 && argv[1] == sub
}

// detectBuildSuccess returns true when build output contains common success markers.
func detectBuildSuccess(s string) bool {
	low := strings.ToLower(s)
	return strings.Contains(low, "build succeeded") ||
		strings.Contains(low, "built successfully") ||
		strings.Contains(low, "compiled successfully") ||
		strings.Contains(low, "compilation successful") ||
		strings.Contains(low, "build successful") ||
		strings.Contains(low, "successfully built") ||
		strings.Contains(low, "finished successfully") ||
		strings.Contains(low, "build complete") ||
		strings.Contains(low, "all builds passed") ||
		strings.Contains(low, "build ok") ||
		strings.Contains(low, "0 errors, 0 warnings") ||
		strings.Contains(low, "compiled with 0 errors")
}

// extractBuildErrors compacts non-empty build output.
// On warning-free success: returns "[label] ok\n"; success with warnings fails open.
// On failure: returns "[label] FAILED\n<error lines>\n" if shorter than input.
func extractBuildErrors(s, label string) (string, bool) {
	if detectBuildSuccess(s) {
		if buildOutputHasNonZeroWarning(s) {
			return "", false
		}
		return fmt.Sprintf("[%s] ok\n", label), true
	}
	var errLines []string
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		tl := strings.ToLower(t)
		if strings.Contains(tl, "error") || strings.Contains(tl, "failed") ||
			strings.Contains(tl, "fatal") || strings.Contains(tl, "cannot") ||
			strings.Contains(tl, "undefined") || strings.Contains(tl, "unresolved") ||
			strings.Contains(tl, "aborting") {
			errLines = append(errLines, t)
		}
	}
	if len(errLines) == 0 {
		return "", false
	}
	out := fmt.Sprintf("[%s] FAILED\n%s\n", label, strings.Join(errLines, "\n"))
	if len(out) >= len(s) {
		return "", false
	}
	return out, true
}

func buildOutputHasNonZeroWarning(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		tl := strings.ToLower(t)
		if !strings.Contains(tl, "warning") {
			continue
		}
		if strings.Contains(tl, "0 warning") || strings.Contains(tl, "0 warnings") {
			continue
		}
		return true
	}
	return false
}

// extractTestFailures compacts non-empty test output.
// On all-pass: returns "[label] ok (summary)\n".
// On failures: returns "[label] FAILED\n<fail lines>\n" if shorter than input.
func extractTestFailures(s, label string) (string, bool) {
	low := strings.ToLower(s)

	// Detect success across ecosystems:
	// go test: last line "ok  \t..." or "--- PASS"
	// cargo test: "test result: ok."
	// pytest: "N passed"
	// jest/vitest: "Tests: N passed"
	isAllPass := (strings.Contains(low, "test result: ok") ||
		strings.Contains(low, "all tests passed") ||
		strings.Contains(low, "all passed") ||
		(strings.Contains(low, "passed") && !strings.Contains(low, "failed") && !strings.Contains(low, "fail")))

	// go test: "PASS\n" as a line
	if !isAllPass {
		for _, line := range strings.Split(s, "\n") {
			if strings.TrimSpace(line) == "PASS" {
				isAllPass = true
				break
			}
			// "ok  \tpkg\t0.123s"
			if strings.HasPrefix(line, "ok  \t") || strings.HasPrefix(line, "ok \t") {
				isAllPass = true
				break
			}
		}
	}

	if isAllPass {
		// Find a summary line with counts for the label
		for _, line := range strings.Split(s, "\n") {
			t := strings.TrimSpace(line)
			tl := strings.ToLower(t)
			if t == "" {
				continue
			}
			if strings.Contains(tl, "passed") && (strings.Contains(t, "ms") || strings.Contains(t, "s ") ||
				strings.Contains(t, "0.") || strings.Contains(t, "second")) {
				return fmt.Sprintf("[%s] ok (%s)\n", label, t), true
			}
		}
		return fmt.Sprintf("[%s] ok\n", label), true
	}

	// Failure: collect FAIL/FAILED lines
	var failLines []string
	if strings.Contains(label, "go test") {
		failLines = extractGoTestFailureLines(s)
	}
	if len(failLines) == 0 {
		for _, line := range strings.Split(s, "\n") {
			t := strings.TrimSpace(line)
			if t == "" {
				continue
			}
			// go test: "--- FAIL: TestName", "FAIL\t<pkg>"
			// cargo test: "test name ... FAILED"
			// pytest: "FAILED tests/test_foo.py::test_bar"
			// jest: "● description"
			if strings.HasPrefix(t, "--- FAIL") || strings.HasPrefix(t, "FAIL\t") ||
				strings.HasSuffix(t, " FAILED") || strings.HasPrefix(t, "FAILED ") ||
				strings.HasPrefix(t, "● ") {
				failLines = append(failLines, t)
			}
		}
	}
	if len(failLines) == 0 {
		return "", false
	}
	out := fmt.Sprintf("[%s] FAILED\n%s\n", label, strings.Join(failLines, "\n"))
	if len(out) >= len(s) {
		return "", false
	}
	return out, true
}

func extractGoTestFailureLines(s string) []string {
	var failLines []string
	var currentTest []string
	inFailedBlock := false
	add := func(line string) {
		line = strings.TrimSpace(line)
		if line != "" {
			failLines = append(failLines, line)
		}
	}
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		switch {
		case strings.HasPrefix(t, "=== RUN"):
			currentTest = []string{t}
			inFailedBlock = false
		case strings.HasPrefix(t, "--- PASS:"):
			currentTest = nil
			inFailedBlock = false
		case strings.HasPrefix(t, "--- FAIL:"):
			start := 0
			if len(currentTest) > 12 {
				start = len(currentTest) - 12
			}
			for _, kept := range currentTest[start:] {
				add(kept)
			}
			add(t)
			currentTest = nil
			inFailedBlock = true
		case strings.HasPrefix(t, "FAIL\t"):
			add(t)
			inFailedBlock = false
		case inFailedBlock:
			add(t)
		case currentTest != nil:
			currentTest = append(currentTest, t)
		}
	}
	if len(failLines) <= 80 {
		return failLines
	}
	out := append([]string(nil), failLines[:60]...)
	out = append(out, fmt.Sprintf("... +%d more go-test failure line(s) omitted", len(failLines)-80))
	out = append(out, failLines[len(failLines)-20:]...)
	return out
}

const maxLintLines = 60

// truncateLintViolations trims large lint output to maxLintLines non-empty lines.
// Returns ("", false) if output is already short enough.
func truncateLintViolations(s, label string) (string, bool) {
	lines := strings.Split(s, "\n")
	var kept []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			kept = append(kept, l)
		}
	}
	if len(kept) <= maxLintLines {
		return "", false
	}
	out := truncateViolationsPreservingErrors(kept, maxLintLines)
	if len(out) >= len(s) {
		return "", false
	}
	return out, true
}

// truncateViolationsPreservingErrors keeps up to maxLines violations, prioritising
// error-severity rows over warnings/notes (in original order) so a truncated lint
// report never hides an error behind less important warnings. Remaining budget is
// filled with head rows; the selection is emitted in original order.
func truncateViolationsPreservingErrors(kept []string, maxLines int) string {
	if len(kept) <= maxLines || maxLines <= 0 {
		return strings.Join(kept, "\n")
	}
	selected := make(map[int]struct{}, maxLines)
	for i, line := range kept {
		if len(selected) >= maxLines {
			break
		}
		if strings.Contains(strings.ToLower(line), "error") {
			selected[i] = struct{}{}
		}
	}
	for i := 0; i < len(kept) && len(selected) < maxLines; i++ {
		selected[i] = struct{}{}
	}
	ordered := make([]string, 0, len(selected))
	for i, line := range kept {
		if _, ok := selected[i]; ok {
			ordered = append(ordered, line)
		}
	}
	dropped := len(kept) - len(ordered)
	out := strings.Join(ordered, "\n")
	if dropped > 0 {
		out += fmt.Sprintf("\n... +%d more violation(s) (kept errors)\n", dropped)
	}
	return out
}

func cappedEvidenceIndexes(total, budget, tail int) []int {
	if total <= 0 || budget <= 0 {
		return nil
	}
	if total <= budget {
		out := make([]int, total)
		for i := range out {
			out[i] = i
		}
		return out
	}
	if tail < 0 {
		tail = 0
	}
	if tail > budget/2 {
		tail = budget / 2
	}
	head := budget - tail
	out := make([]int, 0, budget)
	for i := 0; i < head; i++ {
		out = append(out, i)
	}
	for i := total - tail; i < total; i++ {
		if i >= head {
			out = append(out, i)
		}
	}
	return out
}

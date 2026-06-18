package filter

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// TryCompactGoTestJSON replaces a successful `go test -json` NDJSON stream with one line (F22 partial).
// TryCompactGoTestJSON compacts `go test -json` output (F22):
// all-pass → "[go test -json] ok"; failures → only failed test events + summary.
func TryCompactGoTestJSON(argv []string, stdout []byte) ([]byte, bool) {
	if !isGoTestJSONArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return stdout, false
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 2 {
		return stdout, false
	}

	type testEvent struct {
		Action  string `json:"Action"`
		Package string `json:"Package"`
		Test    string `json:"Test"`
		Output  string `json:"Output"`
	}

	var events []testEvent
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev testEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return stdout, false // malformed: pass through
		}
		events = append(events, ev)
	}

	// Collect pass/fail stats and failure output.
	passed, failed := 0, 0
	var failLines []string
	for _, ev := range events {
		switch ev.Action {
		case "pass":
			if ev.Test != "" {
				passed++
			}
		case "fail":
			if ev.Test != "" {
				failed++
				failLines = append(failLines, fmt.Sprintf("FAIL %s/%s", ev.Package, ev.Test))
			} else {
				failLines = append(failLines, fmt.Sprintf("FAIL %s", ev.Package))
			}
		case "output":
			if ev.Test != "" && strings.Contains(strings.ToLower(ev.Output), "fail") {
				t := strings.TrimRight(ev.Output, "\n")
				if strings.TrimSpace(t) != "" {
					failLines = append(failLines, "  "+t)
				}
			}
		}
	}

	if failed == 0 {
		out := []byte("[go test -json] ok\n")
		if len(out) >= len(stdout) {
			return stdout, false
		}
		return out, true
	}

	// Build compact failure output.
	summary := fmt.Sprintf("[go test -json] %d passed, %d failed\n", passed, failed)
	var sb strings.Builder
	sb.WriteString(summary)
	for _, l := range failLines {
		sb.WriteString(l)
		sb.WriteByte('\n')
	}
	return []byte(sb.String()), true
}

func isGoTestJSONArgv(argv []string) bool {
	if !isGoTestArgv(argv) {
		return false
	}
	for _, a := range argv[1:] {
		if a == "-json" {
			return true
		}
	}
	return false
}

func isGoBinary(name string) bool {
	b := strings.ToLower(filepath.Base(name))
	return b == "go" || b == "go.exe"
}

// TryCompactGoTest collapses empty stdout from `go test …` / `npx|pnpm exec|yarn … go test …` (F08 partial — failure extraction TBD).
// TryCompactGoTest compacts go test output. Empty all-pass stdout becomes a
// single ok marker. Verbose all-pass output (-v) drops only the per-test
// RUN/PASS/PAUSE/CONT noise lines and keeps every other line verbatim
// (package ok rows, coverage, skips, t.Log output) plus the exact passed
// count, so no evidence beyond the redundant pass roll-call is lost. Any
// failure marker fails open to the full transcript.
func TryCompactGoTest(argv []string, stdout []byte) ([]byte, bool) {
	if !isGoTestArgv(argv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[go test] ok\n"), true
	}
	return compactGoTestVerbosePass(stdout)
}

func compactGoTestVerbosePass(stdout []byte) ([]byte, bool) {
	s := string(stdout)
	for _, marker := range []string{"--- FAIL", "\nFAIL", "FAIL\t", "panic:", "DATA RACE", "--- TIMEOUT", "build failed"} {
		if strings.Contains(s, marker) || strings.HasPrefix(s, "FAIL") {
			return stdout, false
		}
	}
	lines := strings.Split(s, "\n")
	kept := make([]string, 0, len(lines))
	passed := 0
	dropped := 0
	sawRun := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "=== RUN"):
			sawRun = true
			dropped++
		case strings.HasPrefix(t, "--- PASS:"):
			passed++
			dropped++
		case strings.HasPrefix(t, "=== PAUSE"), strings.HasPrefix(t, "=== CONT"), t == "PASS":
			dropped++
		default:
			kept = append(kept, line)
		}
	}
	if !sawRun || passed == 0 {
		return stdout, false
	}
	out := fmt.Sprintf("[go test] ok - %d passed, per-test PASS lines elided\n", passed) +
		strings.TrimLeft(strings.Join(kept, "\n"), "\n")
	if len(out) >= len(s) {
		return stdout, false
	}
	return []byte(out), true
}

func isGoTestArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isGoBinary(argv[0]) {
		for _, a := range argv[1:] {
			if a == "test" {
				return true
			}
		}
		return false
	}
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 2 {
			return false
		}
		return isGoTestArgv(rest)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isGoTestArgv(argv[2:])
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isGoTestArgv(argv[1:])
	}
	return false
}

func isCargoBin(name string) bool {
	b := strings.ToLower(filepath.Base(name))
	return b == "cargo" || b == "cargo.exe"
}

func isCargoTestArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isCargoBin(argv[0]) {
		return argv[1] == "test"
	}
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 2 {
			return false
		}
		return isCargoTestArgv(rest)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isCargoTestArgv(argv[2:])
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isCargoTestArgv(argv[1:])
	}
	return false
}

// TryCompactCargoTest collapses empty stdout from `cargo test …` / `npx|pnpm exec|yarn … cargo test …` (F08 partial).
func TryCompactCargoTest(argv []string, stdout []byte) ([]byte, bool) {
	if !isCargoTestArgv(argv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[cargo test] ok\n"), true
	}
	return compactCargoTestVerbosePass(stdout)
}

// compactCargoTestVerbosePass drops only the per-test "test name ... ok"
// roll-call from an all-pass cargo test transcript and keeps every other
// line verbatim (compile lines, running headers, "test result: ok"
// summaries, ignored/bench rows) plus the exact passed count. Any failure
// marker fails open to the full transcript.
func compactCargoTestVerbosePass(stdout []byte) ([]byte, bool) {
	s := string(stdout)
	low := strings.ToLower(s)
	for _, marker := range []string{"failed", "panicked", "error[", "error:", "warning: unused"} {
		if strings.Contains(low, marker) && !strings.Contains(low, "0 failed") {
			return stdout, false
		}
	}
	if strings.Contains(low, "failed") && !allCargoFailureCountsZero(s) {
		return stdout, false
	}
	lines := strings.Split(s, "\n")
	kept := make([]string, 0, len(lines))
	passed := 0
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "test ") && strings.HasSuffix(t, "... ok") {
			passed++
			continue
		}
		kept = append(kept, line)
	}
	if passed == 0 {
		return stdout, false
	}
	out := fmt.Sprintf("[cargo test] ok - %d passed, per-test ok lines elided\n", passed) +
		strings.TrimLeft(strings.Join(kept, "\n"), "\n")
	if len(out) >= len(s) {
		return stdout, false
	}
	return []byte(out), true
}

// allCargoFailureCountsZero accepts "test result: ok. N passed; 0 failed"
// summary rows while rejecting any non-zero failure count.
func allCargoFailureCountsZero(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "test result:") {
			continue
		}
		if !strings.HasPrefix(t, "test result: ok.") || !strings.Contains(t, " 0 failed") {
			return false
		}
	}
	return strings.Contains(s, "test result: ok.")
}

func isCargoNextestRunArgv(argv []string) bool {
	if len(argv) < 3 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isCargoBin(argv[0]) && argv[1] == "nextest" && argv[2] == "run" {
		return true
	}
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 3 {
			return false
		}
		return isCargoNextestRunArgv(rest)
	}
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isCargoNextestRunArgv(argv[2:])
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isCargoNextestRunArgv(argv[1:])
	}
	return false
}

// TryCompactCargoNextest summarizes `cargo nextest run` / `npx|pnpm exec|yarn … cargo nextest run`.
func TryCompactCargoNextest(argv []string, stdout []byte) ([]byte, bool) {
	if !isCargoNextestRunArgv(argv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[cargo nextest run] ok\n"), true
	}
	return compactCargoNextestAllPass(stdout)
}

func compactCargoNextestAllPass(stdout []byte) ([]byte, bool) {
	s := string(stdout)
	low := strings.ToLower(s)
	for _, marker := range []string{"failure", "error", "panicked", "panic", "leak", "timed out", "cancelled", "warning", "deprecated"} {
		if strings.Contains(low, marker) {
			return stdout, false
		}
	}
	if strings.Contains(low, "failed") && !strings.Contains(low, "0 failed") {
		return stdout, false
	}
	lines := strings.Split(s, "\n")
	kept := make([]string, 0, len(lines))
	passed := 0
	hasSummary := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "PASS ") || strings.HasPrefix(t, "PASS [") {
			passed++
			continue
		}
		tl := strings.ToLower(t)
		if strings.HasPrefix(t, "Summary [") && strings.Contains(tl, " passed") {
			hasSummary = true
		}
		kept = append(kept, line)
	}
	if passed == 0 || !hasSummary {
		return stdout, false
	}
	out := fmt.Sprintf("[cargo nextest run] ok - %d passed, per-test PASS lines elided\n", passed) +
		strings.TrimLeft(strings.Join(kept, "\n"), "\n")
	if len(out) >= len(s) {
		return stdout, false
	}
	return []byte(out), true
}

func isCargoLlvmCovArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isCargoBin(argv[0]) && argv[1] == "llvm-cov" {
		return true
	}
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 2 {
			return false
		}
		return isCargoLlvmCovArgv(rest)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isCargoLlvmCovArgv(argv[2:])
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isCargoLlvmCovArgv(argv[1:])
	}
	return false
}

// TryCompactCargoLlvmCov summarizes empty stdout from `cargo llvm-cov` / `npx|pnpm exec|yarn … cargo llvm-cov` (F08 partial).
func TryCompactCargoLlvmCov(argv []string, stdout []byte) ([]byte, bool) {
	if !isCargoLlvmCovArgv(argv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[cargo llvm-cov] ok\n"), true
}

// TryCompactGinkgo summarizes successful `ginkgo` / `npx|pnpm exec|yarn … ginkgo` output (F08 partial).
func TryCompactGinkgo(argv []string, stdout []byte) ([]byte, bool) {
	if !isSingleBinarySubcmdArgv(argv, "ginkgo", "") {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[ginkgo] ok\n"), true
	}
	return compactGinkgoAllPass(stdout)
}

func compactGinkgoAllPass(stdout []byte) ([]byte, bool) {
	s := string(stdout)
	lines := strings.Split(s, "\n")
	kept := make([]string, 0, len(lines))
	willRun := 0
	ranSpecs := 0
	successPassed := 0
	bulletCount := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if n, ok := ginkgoWillRunCount(trimmed); ok {
			willRun = n
			kept = append(kept, line)
			continue
		}
		if n, ok := ginkgoRanSpecsCount(trimmed); ok {
			ranSpecs = n
			kept = append(kept, line)
			continue
		}
		if n, ok := ginkgoSuccessPassedCount(trimmed); ok {
			successPassed = n
			kept = append(kept, line)
			continue
		}
		lower := strings.ToLower(trimmed)
		if ginkgoLineHasUnsafeMarker(trimmed, lower) {
			return stdout, false
		}
		if count, ok := ginkgoBulletProgressCount(trimmed); ok {
			bulletCount += count
			continue
		}
		kept = append(kept, line)
	}
	if successPassed <= 0 || willRun != successPassed || ranSpecs != successPassed || bulletCount != successPassed {
		return stdout, false
	}
	out := fmt.Sprintf("[ginkgo] ok - %d passed, progress line elided\n", successPassed) +
		strings.TrimLeft(strings.Join(kept, "\n"), "\n")
	if len(out) >= len(s) {
		return stdout, false
	}
	return []byte(out), true
}

func ginkgoWillRunCount(line string) (int, bool) {
	fields := strings.Fields(line)
	if len(fields) < 6 || fields[0] != "Will" || fields[1] != "run" || fields[3] != "of" ||
		!strings.EqualFold(fields[5], "specs") || !asciiDecimal(fields[2]) || fields[2] != fields[4] {
		return 0, false
	}
	return parsePositiveASCIIInt(fields[2])
}

func ginkgoRanSpecsCount(line string) (int, bool) {
	fields := strings.Fields(line)
	if len(fields) < 7 || fields[0] != "Ran" || fields[2] != "of" ||
		!strings.EqualFold(fields[4], "Specs") || !asciiDecimal(fields[1]) || fields[1] != fields[3] {
		return 0, false
	}
	return parsePositiveASCIIInt(fields[1])
}

func ginkgoSuccessPassedCount(line string) (int, bool) {
	fields := strings.Fields(line)
	if len(fields) < 13 || fields[0] != "SUCCESS!" || fields[1] != "--" ||
		fields[3] != "Passed" || fields[4] != "|" ||
		fields[6] != "Failed" || fields[7] != "|" ||
		fields[9] != "Pending" || fields[10] != "|" ||
		fields[12] != "Skipped" ||
		!asciiDecimal(fields[2]) || fields[5] != "0" || fields[8] != "0" || fields[11] != "0" {
		return 0, false
	}
	return parsePositiveASCIIInt(fields[2])
}

func ginkgoLineHasUnsafeMarker(trimmed, lower string) bool {
	return strings.HasPrefix(trimmed, "FAIL!") ||
		strings.HasPrefix(trimmed, "Failure") ||
		strings.Contains(lower, "failed") ||
		strings.Contains(lower, "panic") ||
		strings.Contains(lower, "error") ||
		strings.Contains(lower, "timed out") ||
		strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "interrupted") ||
		strings.Contains(lower, "pending") ||
		strings.Contains(lower, "skipped")
}

func ginkgoBulletProgressCount(line string) (int, bool) {
	if line == "" {
		return 0, false
	}
	count := 0
	for _, r := range line {
		if r != '\u2022' {
			return 0, false
		}
		count++
	}
	return count, count > 0
}

// TryCompactCtest summarizes `ctest` / `npx|pnpm exec|yarn ... ctest`.
func TryCompactCtest(argv []string, stdout []byte) ([]byte, bool) {
	if !isSingleBinarySubcmdArgv(argv, "ctest", "") {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[ctest] ok\n"), true
	}
	return compactCtestAllPass(stdout)
}

func compactCtestAllPass(stdout []byte) ([]byte, bool) {
	s := string(stdout)
	low := strings.ToLower(s)
	if !strings.Contains(low, "tests passed") || !strings.Contains(low, "0 tests failed") {
		return stdout, false
	}
	for _, marker := range []string{"errors while running ctest", "the following tests failed", "timeout", "not run"} {
		if strings.Contains(low, marker) {
			return stdout, false
		}
	}
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		tl := strings.ToLower(t)
		if strings.Contains(tl, "tests passed") && strings.Contains(tl, "0 tests failed") {
			out := fmt.Sprintf("[ctest] ok (%s)\n", t)
			if len(out) >= len(s) {
				return stdout, false
			}
			return []byte(out), true
		}
	}
	return stdout, false
}

// TryCompactPytest summarizes empty stdout from `pytest` / `py.test` / `python -m pytest` / `npx|pnpm exec|yarn … pytest` (F08 partial).
func TryCompactPytest(argv []string, stdout []byte) ([]byte, bool) {
	if !isPytestArgv(argv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[pytest] ok\n"), true
	}
	return compactPytestVerbosePass(stdout, "pytest")
}

// compactPytestVerbosePass drops only the per-test "path::name PASSED [ N%]"
// roll-call from an all-pass pytest -v transcript and keeps every other line
// verbatim (session header, warnings summary, the final "N passed" row) plus
// the exact passed count. Any failure/error marker fails open.
func compactPytestVerbosePass(stdout []byte, label string) ([]byte, bool) {
	s := string(stdout)
	low := strings.ToLower(s)
	for _, marker := range []string{"failed", " error", "error ", "errors", "traceback"} {
		if strings.Contains(low, marker) {
			return stdout, false
		}
	}
	if !strings.Contains(low, " passed") {
		return stdout, false
	}
	lines := strings.Split(s, "\n")
	kept := make([]string, 0, len(lines))
	passed := 0
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.Contains(t, "::") && (strings.HasSuffix(t, " PASSED") || strings.Contains(t, " PASSED ")) {
			passed++
			continue
		}
		kept = append(kept, line)
	}
	if passed == 0 {
		return stdout, false
	}
	out := fmt.Sprintf("[%s] ok - %d passed, per-test PASSED lines elided\n", label, passed) +
		strings.TrimLeft(strings.Join(kept, "\n"), "\n")
	if len(out) >= len(s) {
		return stdout, false
	}
	return []byte(out), true
}

func compactPytestWrapperOutput(stdout []byte, label string) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[" + label + "] ok\n"), true
	}
	return compactPytestVerbosePass(stdout, label)
}

// TryCompactPhpunit summarizes `phpunit` / `npx|pnpm exec|yarn ... phpunit`.
func TryCompactPhpunit(argv []string, stdout []byte) ([]byte, bool) {
	if !isPhpunitArgv(argv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[phpunit] ok\n"), true
	}
	return compactPhpunitAllPass(stdout)
}

func isPhpunitArgv(argv []string) bool {
	return isSingleBinarySubcmdArgv(argv, "phpunit", "") ||
		isSingleBinarySubcmdArgv(argv, "phpunit.phar", "")
}

func compactPhpunitAllPass(stdout []byte) ([]byte, bool) {
	s := string(stdout)
	low := strings.ToLower(s)
	for _, marker := range []string{"failures!", "errors!", "warning", "risky", "skipped", "incomplete", "deprecation", "there was", "there were"} {
		if strings.Contains(low, marker) {
			return stdout, false
		}
	}
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "OK (") && strings.HasSuffix(t, ")") {
			summary := strings.TrimSuffix(strings.TrimPrefix(t, "OK ("), ")")
			out := fmt.Sprintf("[phpunit] ok (%s)\n", summary)
			if len(out) >= len(s) {
				return stdout, false
			}
			return []byte(out), true
		}
	}
	return stdout, false
}

// TryCompactVitest summarizes empty stdout from `vitest` / `npx vitest` / `pnpm exec vitest` / `yarn vitest` (F08 partial).
func TryCompactVitest(argv []string, stdout []byte) ([]byte, bool) {
	if !isVitestCompactArgv(argv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[vitest] ok\n"), true
	}
	return compactJSTestVerbosePass(stdout, "vitest")
}

func isVitestCompactArgv(argv []string) bool {
	if len(argv) < 1 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	switch {
	case b == "vitest" || b == "vitest.cmd":
		return true
	case npxMatches(argv, "vitest"):
		return true
	case len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "vitest":
		return true
	case len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "vitest":
		return true
	default:
		return false
	}
}

// compactJSTestVerbosePass drops only the per-test pass roll-call lines
// (checkmark rows) from an all-pass jest/vitest transcript and keeps every
// other line verbatim (PASS file rows, the "Tests: N passed" summary, timing,
// warnings) plus the exact dropped count. Any failure marker fails open.
func compactJSTestVerbosePass(stdout []byte, label string) ([]byte, bool) {
	s := string(stdout)
	low := strings.ToLower(s)
	for _, marker := range []string{"fail", " error", "error:", "\u2715", "\u2716", "\u00d7"} {
		if strings.Contains(low, marker) {
			return stdout, false
		}
	}
	if !strings.Contains(low, "passed") {
		return stdout, false
	}
	lines := strings.Split(s, "\n")
	kept := make([]string, 0, len(lines))
	passed := 0
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "\u2713 ") || strings.HasPrefix(t, "\u2714 ") {
			passed++
			continue
		}
		kept = append(kept, line)
	}
	if passed == 0 {
		return stdout, false
	}
	out := fmt.Sprintf("[%s] ok - %d passed, per-test check lines elided\n", label, passed) +
		strings.TrimLeft(strings.Join(kept, "\n"), "\n")
	if len(out) >= len(s) {
		return stdout, false
	}
	return []byte(out), true
}

// TryCompactKarma summarizes empty stdout from `karma start` / `npx|pnpm exec|yarn … karma start` (F08 partial).
func TryCompactKarma(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 2 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if (b == "karma" || b == "karma.cmd") && argv[1] == "start" {
		return []byte("[karma] ok\n"), true
	}
	if npxMatches(argv, "karma", "start") {
		return []byte("[karma] ok\n"), true
	}
	if len(argv) >= 4 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "karma" && argv[3] == "start" {
		return []byte("[karma] ok\n"), true
	}
	if len(argv) >= 3 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "karma" && argv[2] == "start" {
		return []byte("[karma] ok\n"), true
	}
	return stdout, false
}

// TryCompactJest summarizes empty stdout from `jest` / `npx|pnpm exec|yarn … jest` (F08 partial).
func TryCompactJest(argv []string, stdout []byte) ([]byte, bool) {
	if !isJestCompactArgv(argv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[jest] ok\n"), true
	}
	return compactJSTestVerbosePass(stdout, "jest")
}

func isJestCompactArgv(argv []string) bool {
	if len(argv) < 1 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "jest" || b == "jest.cmd" {
		return true
	}
	if npxMatches(argv, "jest") {
		return true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "jest" {
		return true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "jest" {
		return true
	}
	return false
}

// TryCompactMocha summarizes successful `mocha` / `npx|pnpm exec|yarn … mocha` output (F08 partial).
func TryCompactMocha(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	matches := false
	if b == "mocha" || b == "mocha.cmd" {
		matches = true
	} else if npxMatches(argv, "mocha") {
		matches = true
	} else if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "mocha" {
		matches = true
	} else if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "mocha" {
		matches = true
	}
	if !matches {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[mocha] ok\n"), true
	}
	return compactMochaAllPass(stdout)
}

func compactMochaAllPass(stdout []byte) ([]byte, bool) {
	s := string(stdout)
	low := strings.ToLower(s)
	for _, marker := range []string{"failed", "failing", "failure", "error", "exception", "uncaught", "timeout", "pending", "skipped", "warning", "deprecated", "\u2716", "\u00d7"} {
		if strings.Contains(low, marker) {
			return stdout, false
		}
	}
	summaryCount := 0
	summaryLine := ""
	lines := strings.Split(s, "\n")
	for _, line := range lines {
		count, ok := mochaPassingSummaryCount(strings.TrimSpace(line))
		if ok {
			summaryCount = count
			summaryLine = strings.TrimSpace(line)
		}
	}
	if summaryCount <= 0 {
		return stdout, false
	}
	kept := make([]string, 0, len(lines))
	passed := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "\u2714 ") || strings.HasPrefix(trimmed, "\u2713 ") || strings.HasPrefix(trimmed, "\u221a ") {
			passed++
			continue
		}
		kept = append(kept, line)
	}
	if passed != summaryCount {
		return stdout, false
	}
	out := fmt.Sprintf("[mocha] ok - %d passed, per-test pass lines elided\n", passed) +
		strings.TrimLeft(strings.Join(kept, "\n"), "\n")
	if !strings.Contains(out, summaryLine) || len(out) >= len(s) {
		return stdout, false
	}
	return []byte(out), true
}

func mochaPassingSummaryCount(line string) (int, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[1] != "passing" || !asciiDecimal(fields[0]) {
		return 0, false
	}
	count := 0
	for _, r := range fields[0] {
		count = count*10 + int(r-'0')
	}
	return count, count > 0
}

// TryCompactAva summarizes successful `ava` / `npx|pnpm exec|yarn … ava` output (F08 partial).
func TryCompactAva(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	matches := false
	if b == "ava" || b == "ava.cmd" {
		matches = true
	} else if npxMatches(argv, "ava") {
		matches = true
	} else if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "ava" {
		matches = true
	} else if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "ava" {
		matches = true
	}
	if !matches {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[ava] ok\n"), true
	}
	return compactAvaAllPass(stdout)
}

func compactAvaAllPass(stdout []byte) ([]byte, bool) {
	s := string(stdout)
	low := strings.ToLower(s)
	for _, marker := range []string{"failed", "failing", "failure", "error", "exception", "uncaught", "rejected", "timeout", "timed out", "skipped", "todo", "warning", "deprecated", "not ok", "\u2716", "\u00d7"} {
		if strings.Contains(low, marker) {
			return stdout, false
		}
	}
	summaryCount := 0
	summaryLine := ""
	lines := strings.Split(s, "\n")
	for _, line := range lines {
		count, ok := avaPassedSummaryCount(strings.TrimSpace(line))
		if ok {
			summaryCount = count
			summaryLine = strings.TrimSpace(line)
		}
	}
	if summaryCount <= 0 {
		return stdout, false
	}
	kept := make([]string, 0, len(lines))
	passed := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "\u2714 ") || strings.HasPrefix(trimmed, "\u2713 ") {
			passed++
			continue
		}
		kept = append(kept, line)
	}
	if passed != summaryCount {
		return stdout, false
	}
	out := fmt.Sprintf("[ava] ok - %d passed, per-test pass lines elided\n", passed) +
		strings.TrimLeft(strings.Join(kept, "\n"), "\n")
	if !strings.Contains(out, summaryLine) || len(out) >= len(s) {
		return stdout, false
	}
	return []byte(out), true
}

func avaPassedSummaryCount(line string) (int, bool) {
	fields := strings.Fields(line)
	if len(fields) < 3 || !asciiDecimal(fields[0]) || fields[len(fields)-1] != "passed" {
		return 0, false
	}
	if fields[1] != "test" && fields[1] != "tests" {
		return 0, false
	}
	count := 0
	for _, r := range fields[0] {
		count = count*10 + int(r-'0')
	}
	return count, count > 0
}

func asciiDecimal(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// TryCompactTap summarizes successful `tap` / `npx|pnpm exec|yarn … tap` (Node-TAP) output (F08 partial).
func TryCompactTap(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	matches := false
	if b == "tap" || b == "tap.cmd" {
		matches = true
	} else if npxMatches(argv, "tap") {
		matches = true
	} else if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "tap" {
		matches = true
	} else if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "tap" {
		matches = true
	}
	if !matches {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[tap] ok\n"), true
	}
	return compactTapAllPass(stdout)
}

func compactTapAllPass(stdout []byte) ([]byte, bool) {
	s := string(stdout)
	low := strings.ToLower(s)
	for _, marker := range []string{"not ok", "failed", "failing", "failure", "error", "exception", "uncaught", "timeout", "timed out", "skipped", "todo", "warning", "deprecated"} {
		if strings.Contains(low, marker) {
			return stdout, false
		}
	}
	lines := strings.Split(s, "\n")
	planCount := 0
	testsCount := 0
	passCount := 0
	failCountSeen := false
	kept := make([]string, 0, len(lines))
	passed := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "ok "):
			n, ok := tapOKLineNumber(trimmed)
			if !ok || n != passed+1 {
				return stdout, false
			}
			passed++
			continue
		case strings.HasPrefix(trimmed, "1.."):
			n, ok := parsePositiveASCIIInt(strings.TrimPrefix(trimmed, "1.."))
			if !ok {
				return stdout, false
			}
			planCount = n
		case strings.HasPrefix(trimmed, "# tests "):
			n, ok := parsePositiveASCIIInt(strings.TrimSpace(strings.TrimPrefix(trimmed, "# tests ")))
			if !ok {
				return stdout, false
			}
			testsCount = n
		case strings.HasPrefix(trimmed, "# pass "):
			n, ok := parsePositiveASCIIInt(strings.TrimSpace(strings.TrimPrefix(trimmed, "# pass ")))
			if !ok {
				return stdout, false
			}
			passCount = n
		case strings.HasPrefix(trimmed, "# fail "):
			n, ok := parseNonNegativeASCIIInt(strings.TrimSpace(strings.TrimPrefix(trimmed, "# fail ")))
			if !ok || n != 0 {
				return stdout, false
			}
			failCountSeen = true
		}
		kept = append(kept, line)
	}
	if passed <= 0 || planCount != passed || testsCount != passed || passCount != passed || !failCountSeen {
		return stdout, false
	}
	out := fmt.Sprintf("[tap] ok - %d passed, per-test ok lines elided\n", passed) +
		strings.TrimLeft(strings.Join(kept, "\n"), "\n")
	if len(out) >= len(s) {
		return stdout, false
	}
	return []byte(out), true
}

func tapOKLineNumber(line string) (int, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0, false
	}
	return parsePositiveASCIIInt(fields[1])
}

func parsePositiveASCIIInt(s string) (int, bool) {
	n, ok := parseNonNegativeASCIIInt(s)
	return n, ok && n > 0
}

func parseNonNegativeASCIIInt(s string) (int, bool) {
	if !asciiDecimal(s) {
		return 0, false
	}
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n, true
}

// TryCompactPlaywrightTest summarizes successful `playwright test` / `npx|pnpm exec|yarn … playwright test` output (F08 partial).
func TryCompactPlaywrightTest(argv []string, stdout []byte) ([]byte, bool) {
	if !isPlaywrightTestArgv(argv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[playwright test] ok\n"), true
	}
	return compactPlaywrightAllPass(stdout)
}

func isPlaywrightTestArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if (b == "playwright" || b == "playwright.cmd") && argv[1] == "test" {
		return true
	}
	if npxMatches(argv, "playwright", "test") {
		return true
	}
	if len(argv) >= 4 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && strings.EqualFold(filepath.Base(argv[2]), "playwright") && argv[3] == "test" {
		return true
	}
	if len(argv) >= 3 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && strings.EqualFold(filepath.Base(argv[1]), "playwright") && argv[2] == "test" {
		return true
	}
	return false
}

func compactPlaywrightAllPass(stdout []byte) ([]byte, bool) {
	s := string(stdout)
	lines := strings.Split(s, "\n")
	summaryCount := 0
	summaryLine := ""
	kept := make([]string, 0, len(lines))
	passed := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if playwrightLineHasUnsafeMarker(trimmed, lower) {
			return stdout, false
		}
		if count, ok := playwrightPassedSummaryCount(trimmed); ok {
			summaryCount = count
			summaryLine = trimmed
		}
		if strings.HasPrefix(trimmed, "\u2713 ") || strings.HasPrefix(trimmed, "\u2714 ") {
			passed++
			continue
		}
		kept = append(kept, line)
	}
	if passed <= 0 || summaryCount != passed {
		return stdout, false
	}
	out := fmt.Sprintf("[playwright test] ok - %d passed, per-test pass lines elided\n", passed) +
		strings.TrimLeft(strings.Join(kept, "\n"), "\n")
	if !strings.Contains(out, summaryLine) || len(out) >= len(s) {
		return stdout, false
	}
	return []byte(out), true
}

func playwrightLineHasUnsafeMarker(trimmed, lower string) bool {
	if strings.HasPrefix(trimmed, "\u2718 ") || strings.HasPrefix(trimmed, "\u2716 ") || strings.HasPrefix(trimmed, "\u00d7 ") {
		return true
	}
	return strings.Contains(lower, " failed") ||
		strings.Contains(lower, "failure") ||
		strings.Contains(lower, "error:") ||
		strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "timed out") ||
		strings.Contains(lower, "skipped") ||
		strings.Contains(lower, "flaky") ||
		strings.Contains(lower, "warning:") ||
		strings.Contains(lower, "deprecated")
}

func playwrightPassedSummaryCount(line string) (int, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[1] != "passed" || !asciiDecimal(fields[0]) {
		return 0, false
	}
	n := 0
	for _, r := range fields[0] {
		n = n*10 + int(r-'0')
	}
	return n, n > 0
}

// TryCompactCypressRun summarizes empty stdout from `cypress run` / `npx|pnpm exec|yarn … cypress run` (F08 partial).
func TryCompactCypressRun(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 2 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if (b == "cypress" || b == "cypress.cmd") && argv[1] == "run" {
		return []byte("[cypress run] ok\n"), true
	}
	if npxMatches(argv, "cypress", "run") {
		return []byte("[cypress run] ok\n"), true
	}
	if len(argv) >= 4 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "cypress" && argv[3] == "run" {
		return []byte("[cypress run] ok\n"), true
	}
	if len(argv) >= 3 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "cypress" && argv[2] == "run" {
		return []byte("[cypress run] ok\n"), true
	}
	return stdout, false
}

// TryCompactWdioRun summarizes empty stdout from `wdio run …` / `npx|pnpm exec|yarn … wdio run` (WebdriverIO) (F08 partial).
func TryCompactWdioRun(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 2 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if (b == "wdio" || b == "wdio.cmd") && argv[1] == "run" {
		return []byte("[wdio run] ok\n"), true
	}
	if npxMatches(argv, "wdio", "run") {
		return []byte("[wdio run] ok\n"), true
	}
	if len(argv) >= 4 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "wdio" && argv[3] == "run" {
		return []byte("[wdio run] ok\n"), true
	}
	if len(argv) >= 3 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "wdio" && argv[2] == "run" {
		return []byte("[wdio run] ok\n"), true
	}
	return stdout, false
}

// TryCompactNpmRunTest summarizes empty stdout from `npm run test` (F08 partial).
func TryCompactNpmRunTest(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 3 {
		return stdout, false
	}
	if strings.ToLower(filepath.Base(argv[0])) != "npm" {
		return stdout, false
	}
	if argv[1] != "run" || argv[2] != "test" {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[npm run test] ok\n"), true
}

// TryCompactPnpmTest summarizes empty stdout from `pnpm test` (F08 partial).
func TryCompactPnpmTest(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 2 {
		return stdout, false
	}
	if filepath.Base(argv[0]) != "pnpm" || argv[1] != "test" {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[pnpm test] ok\n"), true
}

// TryCompactYarnTest summarizes empty stdout from `yarn test` (F08 partial).
func TryCompactYarnTest(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 2 {
		return stdout, false
	}
	if filepath.Base(argv[0]) != "yarn" || argv[1] != "test" {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[yarn test] ok\n"), true
}

// TryCompactBunTest summarizes successful `bun test` / `npx|pnpm exec|yarn … bun test` output (F08 partial).
func TryCompactBunTest(argv []string, stdout []byte) ([]byte, bool) {
	if !isBunTestArgv(argv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[bun test] ok\n"), true
	}
	return compactBunTestAllPass(stdout)
}

func isBunTestArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if (b == "bun" || b == "bun.exe") && argv[1] == "test" {
		return true
	}
	if npxMatches(argv, "bun", "test") {
		return true
	}
	if len(argv) >= 4 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "bun" && argv[3] == "test" {
		return true
	}
	if len(argv) >= 3 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "bun" && argv[2] == "test" {
		return true
	}
	return false
}

func compactBunTestAllPass(stdout []byte) ([]byte, bool) {
	s := string(stdout)
	lines := strings.Split(s, "\n")
	kept := make([]string, 0, len(lines))
	passRows := 0
	passSummary := 0
	passSummarySeen := false
	failSummarySeen := false
	ranTests := 0
	ranSummary := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if bunLineHasUnsafeMarker(trimmed, lower) {
			return stdout, false
		}
		if n, ok := bunSummaryCount(trimmed, "pass"); ok {
			passSummary = n
			passSummarySeen = true
		}
		if n, ok := bunSummaryCount(trimmed, "fail"); ok {
			if n != 0 {
				return stdout, false
			}
			failSummarySeen = true
		}
		if n, ok := bunRanSummaryCount(trimmed); ok {
			ranTests = n
			ranSummary = trimmed
		}
		if strings.HasPrefix(trimmed, "(pass) ") {
			passRows++
			continue
		}
		kept = append(kept, line)
	}
	if passRows <= 0 || !passSummarySeen || !failSummarySeen || ranTests <= 0 ||
		passRows != passSummary || passRows != ranTests {
		return stdout, false
	}
	out := fmt.Sprintf("[bun test] ok - %d passed, per-test pass lines elided\n", passRows) +
		strings.TrimLeft(strings.Join(kept, "\n"), "\n")
	if !strings.Contains(out, ranSummary) || len(out) >= len(s) {
		return stdout, false
	}
	return []byte(out), true
}

func bunLineHasUnsafeMarker(trimmed, lower string) bool {
	if strings.HasPrefix(trimmed, "(fail)") {
		return true
	}
	return strings.Contains(lower, "failed") ||
		strings.Contains(lower, "failure") ||
		strings.Contains(lower, "error:") ||
		strings.Contains(lower, "exception") ||
		strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "timed out") ||
		strings.Contains(lower, "warning:") ||
		strings.Contains(lower, "deprecated") ||
		strings.Contains(lower, " skip")
}

func bunSummaryCount(line, word string) (int, bool) {
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[1] != word || !asciiDecimal(fields[0]) {
		return 0, false
	}
	n := 0
	for _, r := range fields[0] {
		n = n*10 + int(r-'0')
	}
	return n, true
}

func bunRanSummaryCount(line string) (int, bool) {
	fields := strings.Fields(line)
	if len(fields) < 6 || fields[0] != "Ran" || fields[2] != "tests" || fields[3] != "across" || !asciiDecimal(fields[1]) {
		return 0, false
	}
	n := 0
	for _, r := range fields[1] {
		n = n*10 + int(r-'0')
	}
	return n, n > 0
}

// TryCompactNxTest summarizes empty stdout from `nx test …` / `npx|pnpm exec|yarn … nx test` (F08 partial).
func TryCompactNxTest(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 2 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if (b == "nx" || b == "nx.cmd") && argv[1] == "test" {
		return []byte("[nx test] ok\n"), true
	}
	if npxMatches(argv, "nx", "test") {
		return []byte("[nx test] ok\n"), true
	}
	if len(argv) >= 4 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "nx" && argv[3] == "test" {
		return []byte("[nx test] ok\n"), true
	}
	if len(argv) >= 3 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "nx" && argv[2] == "test" {
		return []byte("[nx test] ok\n"), true
	}
	return stdout, false
}

// TryCompactTurboTest summarizes empty stdout from `turbo test` / `turbo run test` / `npx|pnpm exec|yarn … turbo … test` (F08 partial).
func TryCompactTurboTest(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 2 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "turbo" || b == "turbo.cmd" {
		okTurbo := false
		switch {
		case argv[1] == "test":
			okTurbo = true
		case argv[1] == "run" && len(argv) >= 3 && argv[2] == "test":
			okTurbo = true
		}
		if okTurbo {
			return []byte("[turbo test] ok\n"), true
		}
		return stdout, false
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 1 && strings.EqualFold(filepath.Base(rest[0]), "turbo") {
		if len(rest) >= 2 && rest[1] == "test" {
			return []byte("[turbo test] ok\n"), true
		}
		if len(rest) >= 3 && rest[1] == "run" && rest[2] == "test" {
			return []byte("[turbo test] ok\n"), true
		}
	}
	if len(argv) >= 4 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "turbo" {
		if argv[3] == "test" {
			return []byte("[turbo test] ok\n"), true
		}
		if argv[3] == "run" && len(argv) >= 5 && argv[4] == "test" {
			return []byte("[turbo test] ok\n"), true
		}
	}
	if len(argv) >= 3 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "turbo" {
		if argv[2] == "test" {
			return []byte("[turbo test] ok\n"), true
		}
		if argv[2] == "run" && len(argv) >= 4 && argv[3] == "test" {
			return []byte("[turbo test] ok\n"), true
		}
	}
	return stdout, false
}

// TryCompactPythonUnittest summarizes successful `python -m unittest` / `npx|pnpm exec|yarn … python … -m unittest` output (F08 partial).
func TryCompactPythonUnittest(argv []string, stdout []byte) ([]byte, bool) {
	if !isPythonUnittestArgv(argv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[python -m unittest] ok\n"), true
	}
	return compactPythonUnittestAllPass(stdout)
}

func compactPythonUnittestAllPass(stdout []byte) ([]byte, bool) {
	s := string(stdout)
	lower := strings.ToLower(s)
	for _, marker := range []string{
		"failed",
		"failure",
		"error",
		"traceback",
		"exception",
		"warning",
		"deprecated",
		"cancelled",
		"timed out",
	} {
		if strings.Contains(lower, marker) {
			return stdout, false
		}
	}

	ranLine := ""
	okLine := ""
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		lowerLine := strings.ToLower(trimmed)
		if strings.HasPrefix(lowerLine, "ran ") && strings.Contains(lowerLine, " test") {
			ranLine = trimmed
			continue
		}
		if trimmed == "OK" || (strings.HasPrefix(trimmed, "OK (") && strings.HasSuffix(trimmed, ")")) {
			okLine = trimmed
		}
	}
	if ranLine == "" || okLine == "" {
		return stdout, false
	}

	out := fmt.Sprintf("[python -m unittest] ok (%s; %s)\n", ranLine, okLine)
	if len(out) >= len(s) {
		return stdout, false
	}
	return []byte(out), true
}

func isPythonUnittestArgv(argv []string) bool {
	if len(argv) < 3 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 3 {
			return false
		}
		return isPythonUnittestArgv(rest)
	}
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isPythonUnittestArgv(argv[2:])
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isPythonUnittestArgv(argv[1:])
	}
	b := b0
	switch b {
	case "python", "python3", "python2", "py", "py.exe", "python.exe", "python3.exe", "python2.exe":
	default:
		return false
	}
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == "-m" && argv[i+1] == "unittest" {
			return true
		}
	}
	return false
}

func isRailsTestArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 2 {
			return false
		}
		return isRailsTestArgv(rest)
	}
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isRailsTestArgv(argv[2:])
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isRailsTestArgv(argv[1:])
	}
	if (b0 == "rails" || b0 == "rails.cmd") && argv[1] == "test" {
		return true
	}
	if len(argv) >= 4 {
		b2 := strings.ToLower(filepath.Base(argv[2]))
		if b0 == "bundle" && argv[1] == "exec" && (b2 == "rails" || b2 == "rails.cmd") && argv[3] == "test" {
			return true
		}
	}
	return false
}

// TryCompactRailsTest summarizes empty stdout from `rails test` / `bundle exec rails test` / `npx|pnpm exec|yarn … rails test|bundle exec rails test` (F08 partial).
func TryCompactRailsTest(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !isRailsTestArgv(argv) {
		return stdout, false
	}
	return []byte("[rails test] ok\n"), true
}

func isDartTestArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if (b0 == "dart" || b0 == "dart.exe") && argv[1] == "test" {
		return true
	}
	if len(argv) >= 3 && (b0 == "fvm" || b0 == "fvm.exe") && argv[1] == "dart" && argv[2] == "test" {
		return true
	}
	if npxMatches(argv, "dart", "test") {
		return true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 3 {
		r0 := strings.ToLower(filepath.Base(rest[0]))
		if (r0 == "fvm" || r0 == "fvm.exe") && rest[1] == "dart" && rest[2] == "test" {
			return true
		}
	}
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && strings.EqualFold(filepath.Base(argv[2]), "dart") && argv[3] == "test" {
		return true
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && strings.EqualFold(filepath.Base(argv[1]), "dart") && argv[2] == "test" {
		return true
	}
	if len(argv) >= 5 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		r2 := strings.ToLower(filepath.Base(argv[2]))
		if (r2 == "fvm" || r2 == "fvm.exe") && argv[3] == "dart" && argv[4] == "test" {
			return true
		}
	}
	if len(argv) >= 4 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		r1 := strings.ToLower(filepath.Base(argv[1]))
		if (r1 == "fvm" || r1 == "fvm.exe") && argv[2] == "dart" && argv[3] == "test" {
			return true
		}
	}
	return false
}

// TryCompactDartTest summarizes `dart test` / `fvm dart test` /
// `npx|pnpm exec|yarn ... dart test`. Empty stdout is the normal quiet
// success case. Non-empty all-pass output is compacted only when the final
// Dart test summary is explicit and no warning/failure signal appears.
func TryCompactDartTest(argv []string, stdout []byte) ([]byte, bool) {
	if !isDartTestArgv(argv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[dart test] ok\n"), true
	}
	return compactDartFlutterAllPass(stdout, "dart test")
}

func isFlutterTestArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if (b0 == "flutter" || b0 == "flutter.bat" || b0 == "flutter.cmd") && argv[1] == "test" {
		return true
	}
	if len(argv) >= 3 && (b0 == "fvm" || b0 == "fvm.exe") && argv[1] == "flutter" && argv[2] == "test" {
		return true
	}
	if npxMatches(argv, "flutter", "test") {
		return true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 3 {
		r0 := strings.ToLower(filepath.Base(rest[0]))
		if (r0 == "fvm" || r0 == "fvm.exe") && rest[1] == "flutter" && rest[2] == "test" {
			return true
		}
	}
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && strings.EqualFold(filepath.Base(argv[2]), "flutter") && argv[3] == "test" {
		return true
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && strings.EqualFold(filepath.Base(argv[1]), "flutter") && argv[2] == "test" {
		return true
	}
	if len(argv) >= 5 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		r2 := strings.ToLower(filepath.Base(argv[2]))
		if (r2 == "fvm" || r2 == "fvm.exe") && argv[3] == "flutter" && argv[4] == "test" {
			return true
		}
	}
	if len(argv) >= 4 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		r1 := strings.ToLower(filepath.Base(argv[1]))
		if (r1 == "fvm" || r1 == "fvm.exe") && argv[2] == "flutter" && argv[3] == "test" {
			return true
		}
	}
	return false
}

// TryCompactFlutterTest summarizes `flutter test` / `fvm flutter test` /
// `npx|pnpm exec|yarn ... flutter test`. Non-empty output uses the same
// strict all-pass parser as Dart because Flutter delegates to package:test.
func TryCompactFlutterTest(argv []string, stdout []byte) ([]byte, bool) {
	if !isFlutterTestArgv(argv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[flutter test] ok\n"), true
	}
	return compactDartFlutterAllPass(stdout, "flutter test")
}

func compactDartFlutterAllPass(stdout []byte, label string) ([]byte, bool) {
	s := string(stdout)
	low := strings.ToLower(s)
	for _, marker := range []string{"some tests failed", "failed to load", "no tests ran", "exception", "error", "warning", "deprecated", "timed out", "unhandled"} {
		if strings.Contains(low, marker) {
			return stdout, false
		}
	}
	summary := ""
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if strings.Contains(strings.ToLower(t), "all tests passed!") {
			summary = t
		}
	}
	if summary == "" {
		return stdout, false
	}
	out := fmt.Sprintf("[%s] ok (%s)\n", label, summary)
	if len(out) >= len(s) {
		return stdout, false
	}
	return []byte(out), true
}

// TryCompactElmTest summarizes empty stdout from `elm-test` / `npx|pnpm exec|yarn … elm-test` (F08 partial).
func TryCompactElmTest(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "elm-test" || b == "elm-test.cmd" {
		return []byte("[elm-test] ok\n"), true
	}
	if npxMatches(argv, "elm-test") {
		return []byte("[elm-test] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "elm-test" {
		return []byte("[elm-test] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "elm-test" {
		return []byte("[elm-test] ok\n"), true
	}
	return stdout, false
}

func isDenoBin(name string) bool {
	b := strings.ToLower(filepath.Base(name))
	return b == "deno" || b == "deno.exe" || b == "deno.cmd"
}

func isDenoTestArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isDenoBin(argv[0]) && argv[1] == "test" {
		return true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 2 && isDenoBin(rest[0]) && rest[1] == "test" {
		return true
	}
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && isDenoBin(argv[2]) && argv[3] == "test" {
		return true
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && isDenoBin(argv[1]) && argv[2] == "test" {
		return true
	}
	return false
}

// TryCompactDenoTest summarizes `deno test` / `npx ... deno test` /
// `pnpm exec|yarn ... deno test`. Non-empty output compacts only the
// canonical all-pass summary, preserving any warning/failure transcript.
func TryCompactDenoTest(argv []string, stdout []byte) ([]byte, bool) {
	if !isDenoTestArgv(argv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[deno test] ok\n"), true
	}
	return compactDenoTestAllPass(stdout)
}

func compactDenoTestAllPass(stdout []byte) ([]byte, bool) {
	s := string(stdout)
	low := strings.ToLower(s)
	for _, marker := range []string{"failure", "error", "uncaught", "panicked", "leak", "warning", "deprecated", "not ok"} {
		if strings.Contains(low, marker) {
			return stdout, false
		}
	}
	if strings.Contains(low, "failed") && !strings.Contains(low, "0 failed") {
		return stdout, false
	}
	summary := ""
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		lowLine := strings.ToLower(t)
		if strings.HasPrefix(lowLine, "ok |") && strings.Contains(lowLine, " passed") && strings.Contains(lowLine, "0 failed") {
			summary = t
		}
	}
	if summary == "" {
		return stdout, false
	}
	out := fmt.Sprintf("[deno test] ok (%s)\n", summary)
	if len(out) >= len(s) {
		return stdout, false
	}
	return []byte(out), true
}

func isGradleTestBin(name string) bool {
	b := strings.ToLower(filepath.Base(name))
	return b == "gradle" || b == "gradle.bat" || b == "gradlew" || b == "gradlew.bat"
}

// TryCompactGradleTest summarizes `gradle test` / `gradlew test` /
// `npx|pnpm exec|yarn ... gradle|gradlew ...` when `test` appears as a task token.
func TryCompactGradleTest(argv []string, stdout []byte) ([]byte, bool) {
	if !isGradleTestArgv(argv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[gradle test] ok\n"), true
	}
	return compactGradleTestAllPass(stdout)
}

func isGradleTestArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	if !argvHasExactToken(argv, "test") {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isGradleTestBin(argv[0]) {
		return true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 1 && isGradleTestBin(rest[0]) {
		return true
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && isGradleTestBin(argv[2]) {
		return true
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && isGradleTestBin(argv[1]) {
		return true
	}
	return false
}

func compactGradleTestAllPass(stdout []byte) ([]byte, bool) {
	s := string(stdout)
	low := strings.ToLower(s)
	if !strings.Contains(low, "build successful") {
		return stdout, false
	}
	for _, marker := range []string{"failed", "failure", "exception", "error", "warning", "deprecated", "problems report"} {
		if strings.Contains(low, marker) {
			return stdout, false
		}
	}
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "BUILD SUCCESSFUL") {
			out := fmt.Sprintf("[gradle test] ok (%s)\n", t)
			if len(out) >= len(s) {
				return stdout, false
			}
			return []byte(out), true
		}
	}
	return stdout, false
}

func isSbtTestBin(name string) bool {
	b := strings.ToLower(filepath.Base(name))
	return b == "sbt" || b == "sbt.bat"
}

// TryCompactSbtTest summarizes empty stdout from `sbt … test` / `npx|pnpm exec|yarn … sbt …` when `test` appears as a task token (F08 partial).
func TryCompactSbtTest(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 2 {
		return stdout, false
	}
	if !argvHasExactToken(argv, "test") {
		return stdout, false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isSbtTestBin(argv[0]) {
		return []byte("[sbt test] ok\n"), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 1 && isSbtTestBin(rest[0]) {
		return []byte("[sbt test] ok\n"), true
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && isSbtTestBin(argv[2]) {
		return []byte("[sbt test] ok\n"), true
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && isSbtTestBin(argv[1]) {
		return []byte("[sbt test] ok\n"), true
	}
	return stdout, false
}

// TryCompactMillTest summarizes empty stdout from `mill test` / `npx|pnpm exec|yarn … mill test` (F08 partial).
func TryCompactMillTest(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 2 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if (b == "mill" || b == "mill.bat") && argv[1] == "test" {
		return []byte("[mill test] ok\n"), true
	}
	if npxMatches(argv, "mill", "test") {
		return []byte("[mill test] ok\n"), true
	}
	if len(argv) >= 4 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "mill" && argv[3] == "test" {
		return []byte("[mill test] ok\n"), true
	}
	if len(argv) >= 3 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "mill" && argv[2] == "test" {
		return []byte("[mill test] ok\n"), true
	}
	return stdout, false
}

// TryCompactHatchTest summarizes `hatch test` / `npx|pnpm exec|yarn ... hatch test`.
func TryCompactHatchTest(argv []string, stdout []byte) ([]byte, bool) {
	if !isHatchTestArgv(argv) {
		return stdout, false
	}
	return compactPytestWrapperOutput(stdout, "hatch test")
}

func isHatchTestArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if (b == "hatch" || b == "hatch.exe") && argv[1] == "test" {
		return true
	}
	if npxMatches(argv, "hatch", "test") {
		return true
	}
	if len(argv) >= 4 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "hatch" && argv[3] == "test" {
		return true
	}
	if len(argv) >= 3 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "hatch" && argv[2] == "test" {
		return true
	}
	return false
}

func isUvRunPytestArgv(argv []string) bool {
	if len(argv) < 3 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "uv" && b != "uv.exe" {
		return false
	}
	if argv[1] != "run" {
		return false
	}
	if argv[2] == "pytest" {
		return true
	}
	if len(argv) >= 5 && argv[3] == "-m" && argv[4] == "pytest" {
		py := strings.ToLower(filepath.Base(argv[2]))
		return py == "python" || py == "python3" || py == "python.exe" || py == "python3.exe"
	}
	return false
}

// TryCompactUvRunPytest summarizes `uv run pytest` / `uv run python -m pytest` / `npx|pnpm exec|yarn ... uv run ... pytest`.
func TryCompactUvRunPytest(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	if isUvRunPytestArgv(argv) {
		return compactPytestWrapperOutput(stdout, "uv run pytest")
	}
	if rest, ok := npxArgvSuffix(argv); ok && isUvRunPytestArgv(rest) {
		return compactPytestWrapperOutput(stdout, "uv run pytest")
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if len(argv) >= 5 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		if isUvRunPytestArgv(argv[2:]) {
			return compactPytestWrapperOutput(stdout, "uv run pytest")
		}
	}
	if len(argv) >= 4 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		if isUvRunPytestArgv(argv[1:]) {
			return compactPytestWrapperOutput(stdout, "uv run pytest")
		}
	}
	return stdout, false
}

func isPoetryRunPytestArgv(argv []string) bool {
	if len(argv) < 3 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "poetry" && b != "poetry.exe" {
		return false
	}
	if argv[1] != "run" {
		return false
	}
	if argv[2] == "pytest" {
		return true
	}
	if len(argv) >= 5 && argv[3] == "-m" && argv[4] == "pytest" {
		py := strings.ToLower(filepath.Base(argv[2]))
		return py == "python" || py == "python3" || py == "python.exe" || py == "python3.exe"
	}
	return false
}

// TryCompactPoetryRunPytest summarizes `poetry run pytest` / `poetry run python -m pytest` / `npx|pnpm exec|yarn ... poetry run ... pytest`.
func TryCompactPoetryRunPytest(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	if isPoetryRunPytestArgv(argv) {
		return compactPytestWrapperOutput(stdout, "poetry run pytest")
	}
	if rest, ok := npxArgvSuffix(argv); ok && isPoetryRunPytestArgv(rest) {
		return compactPytestWrapperOutput(stdout, "poetry run pytest")
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if len(argv) >= 5 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		if isPoetryRunPytestArgv(argv[2:]) {
			return compactPytestWrapperOutput(stdout, "poetry run pytest")
		}
	}
	if len(argv) >= 4 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		if isPoetryRunPytestArgv(argv[1:]) {
			return compactPytestWrapperOutput(stdout, "poetry run pytest")
		}
	}
	return stdout, false
}

// TryCompactNoxTest summarizes `nox -s test` / `nox --session=test`.
func TryCompactNoxTest(argv []string, stdout []byte) ([]byte, bool) {
	if !isNoxTestSessionArgv(argv) {
		return stdout, false
	}
	return compactPytestWrapperOutput(stdout, "nox test")
}

func argvHasNoxTestSessionFlags(args []string) bool {
	for i := 0; i < len(args)-1; i++ {
		switch args[i] {
		case "-s", "--session":
			if args[i+1] == "test" {
				return true
			}
		}
	}
	for _, a := range args {
		if strings.HasPrefix(a, "--session=") && strings.TrimPrefix(a, "--session=") == "test" {
			return true
		}
	}
	return false
}

func isNoxTestSessionArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 1 {
			return false
		}
		if bn := strings.ToLower(filepath.Base(rest[0])); bn != "nox" && bn != "nox.exe" {
			return false
		}
		return argvHasNoxTestSessionFlags(rest[1:])
	}
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		if bn := strings.ToLower(filepath.Base(argv[2])); bn == "nox" || bn == "nox.exe" {
			return argvHasNoxTestSessionFlags(argv[3:])
		}
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		if bn := strings.ToLower(filepath.Base(argv[1])); bn == "nox" || bn == "nox.exe" {
			return argvHasNoxTestSessionFlags(argv[2:])
		}
	}
	if b0 != "nox" && b0 != "nox.exe" {
		return false
	}
	return argvHasNoxTestSessionFlags(argv[1:])
}

func argvHasExactToken(argv []string, token string) bool {
	for _, a := range argv[1:] {
		if a == token {
			return true
		}
	}
	return false
}

// TryCompactTestOutput chains common test runners with empty-success stdout.
func TryCompactTestOutput(argv []string, stdout []byte) ([]byte, bool) {
	if out, ok := TryCompactGoTestJSON(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactGoTest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactCargoTest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactCargoNextest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactCargoLlvmCov(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactGinkgo(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactCtest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactPytest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactUvRunPytest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactPoetryRunPytest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactHatchTest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactNoxTest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactPythonUnittest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactPhpunit(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactRailsTest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactGradleTest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactSbtTest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactMillTest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactVitest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactKarma(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactJest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactMocha(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactAva(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactTap(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactPlaywrightTest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactDartTest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactFlutterTest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactElmTest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactDenoTest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactCypressRun(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactWdioRun(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactNxTest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactTurboTest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactBunTest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactNpmRunTest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactPnpmTest(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactYarnTest(argv, stdout); ok {
		return out, true
	}
	// Fallback: for recognized test tools with non-empty output, extract failures or detect success.
	if label := testToolLabel(argv); label != "" {
		s := strings.TrimSpace(string(stdout))
		if s != "" {
			if out, ok := extractTestFailures(s, label); ok {
				return []byte(out), true
			}
		}
	}
	return stdout, false
}

// testToolLabel returns the compact label for argv if it is a recognized test command, else "".
func testToolLabel(argv []string) string {
	switch {
	case isGoTestArgv(argv) || isGoTestJSONArgv(argv):
		return "go test"
	case isCargoTestArgv(argv):
		return "cargo test"
	case isCargoNextestRunArgv(argv):
		return "cargo nextest"
	case isCargoLlvmCovArgv(argv):
		return "cargo llvm-cov"
	case isPythonUnittestArgv(argv):
		return "python unittest"
	case isRailsTestArgv(argv):
		return "rails test"
	case isDartTestArgv(argv):
		return "dart test"
	case isFlutterTestArgv(argv):
		return "flutter test"
	case isDenoTestArgv(argv):
		return "deno test"
	case isNoxTestSessionArgv(argv):
		return "nox test"
	case isUvRunPytestArgv(argv):
		return "uv run pytest"
	case isPoetryRunPytestArgv(argv):
		return "poetry run pytest"
	}
	type binSub struct{ bin, sub, label string }
	tools := []binSub{
		{"ginkgo", "", "ginkgo"},
		{"ctest", "", "ctest"},
		{"pytest", "", "pytest"},
		{"py.test", "", "pytest"},
		{"phpunit", "", "phpunit"},
		{"phpunit.phar", "", "phpunit"},
		{"vitest", "", "vitest"},
		{"karma", "start", "karma"},
		{"jest", "", "jest"},
		{"mocha", "", "mocha"},
		{"ava", "", "ava"},
		{"tap", "", "tap"},
		{"playwright", "test", "playwright test"},
		{"cypress", "run", "cypress run"},
		{"wdio", "run", "wdio run"},
		{"bun", "test", "bun test"},
		{"elm-test", "", "elm-test"},
	}
	for _, t := range tools {
		if isSingleBinarySubcmdArgv(argv, t.bin, t.sub) {
			return t.label
		}
	}
	// nx test / turbo test
	if isSingleBinarySubcmdArgv(argv, "nx", "test") {
		return "nx test"
	}
	if isSingleBinarySubcmdArgv(argv, "turbo", "test") {
		return "turbo test"
	}
	// npm/pnpm/yarn run test
	if len(argv) >= 3 {
		b0 := strings.ToLower(filepath.Base(argv[0]))
		if b0 == "npm" && argv[1] == "run" && argv[2] == "test" {
			return "npm run test"
		}
		if (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "run" && argv[2] == "test" {
			return "pnpm run test"
		}
		if (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && argv[1] == "run" && argv[2] == "test" {
			return "yarn run test"
		}
	}
	return ""
}

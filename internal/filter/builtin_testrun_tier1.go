package filter

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Tier-1 parsers for JSON-emitting test runners. Each parser is strict:
// if the input does not match the expected JSON shape exactly, it
// returns (stdout, false) so the pipeline falls through to Tier-2
// (regex via embedded TOML) and finally Tier-3 (truncate). This
// fail-clean discipline means we surface "5 tests, 0 failed" instead of the
// multi-kilobyte raw stream that even regex compaction cannot trim.

// TryCompactVitestJSON compacts `vitest --reporter=json` and
// `jest --json` output (identical reporter schema). The schema is
// stable across major Jest/Vitest versions: numTotal*, numPassed*,
// numFailed* counts at the top level, plus per-suite testResults.
//
// Success → "[vitest --reporter=json] N tests passed in M suite(s)\n"
// Failure → "[vitest --reporter=json] FAILED X/N tests\n<top-N failure messages>"
func TryCompactVitestJSON(argv []string, stdout []byte) ([]byte, bool) {
	if !isJestVitestJSONArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" || s[0] != '{' {
		return stdout, false
	}
	type assertion struct {
		Status          string   `json:"status"`
		FullName        string   `json:"fullName"`
		Title           string   `json:"title"`
		FailureMessages []string `json:"failureMessages"`
	}
	type suite struct {
		Name             string      `json:"name"`
		Status           string      `json:"status"`
		AssertionResults []assertion `json:"assertionResults"`
	}
	type report struct {
		NumTotalTestSuites  int     `json:"numTotalTestSuites"`
		NumPassedTestSuites int     `json:"numPassedTestSuites"`
		NumFailedTestSuites int     `json:"numFailedTestSuites"`
		NumTotalTests       int     `json:"numTotalTests"`
		NumPassedTests      int     `json:"numPassedTests"`
		NumFailedTests      int     `json:"numFailedTests"`
		TestResults         []suite `json:"testResults"`
	}
	var r report
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return stdout, false
	}
	// Require at least the count fields to be present; total=0 is
	// suspicious for a real run, but allow it.
	if r.NumTotalTests == 0 && r.NumTotalTestSuites == 0 {
		return stdout, false
	}
	label := vitestLabelFromArgv(argv)
	if r.NumFailedTests == 0 && r.NumFailedTestSuites == 0 {
		return fmt.Appendf(nil, "[%s] %d tests passed in %d suite(s)\n", label, r.NumTotalTests, r.NumTotalTestSuites), true
	}
	var out strings.Builder
	fmt.Fprintf(&out, "[%s] FAILED %d/%d tests in %d/%d suite(s)\n", label, r.NumFailedTests, r.NumTotalTests, r.NumFailedTestSuites, r.NumTotalTestSuites)
	const maxFailures = 5
	type vitestFailure struct {
		name     string
		messages []string
	}
	failures := make([]vitestFailure, 0, r.NumFailedTests)
	for _, suite := range r.TestResults {
		for _, a := range suite.AssertionResults {
			if a.Status != "failed" {
				continue
			}
			name := a.FullName
			if name == "" {
				name = a.Title
			}
			failures = append(failures, vitestFailure{name: name, messages: a.FailureMessages})
		}
	}
	previous := -1
	for _, idx := range cappedEvidenceIndexes(len(failures), maxFailures, 2) {
		if previous >= 0 && idx > previous+1 {
			fmt.Fprintf(&out, "  ... +%d more failure(s)\n", idx-previous-1)
		}
		failure := failures[idx]
		fmt.Fprintf(&out, "  FAIL %s\n", failure.name)
		for _, msg := range failure.messages {
			for _, line := range firstLines(msg, 3) {
				fmt.Fprintf(&out, "    %s\n", line)
			}
		}
		previous = idx
	}
	if len(failures) > 0 && previous < len(failures)-1 {
		fmt.Fprintf(&out, "  ... +%d more failure(s)\n", len(failures)-previous-1)
	}
	return []byte(out.String()), true
}

// TryCompactPytestJSON compacts `pytest --json-report` stdout (the
// pytest-json-report plugin's --json-report-file=- mode). Schema:
// {"summary":{"passed":N,"failed":M,...},"tests":[{"nodeid","outcome","longrepr"}]}.
func TryCompactPytestJSON(argv []string, stdout []byte) ([]byte, bool) {
	if !isPytestJSONArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" || s[0] != '{' {
		return stdout, false
	}
	type test struct {
		NodeID   string `json:"nodeid"`
		Outcome  string `json:"outcome"`
		LongRepr string `json:"longrepr"`
	}
	type summary struct {
		Passed  int     `json:"passed"`
		Failed  int     `json:"failed"`
		Error   int     `json:"error"`
		Skipped int     `json:"skipped"`
		Total   int     `json:"total"`
		Time    float64 `json:"duration"`
	}
	type report struct {
		Summary summary `json:"summary"`
		Tests   []test  `json:"tests"`
	}
	var r report
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return stdout, false
	}
	if r.Summary.Total == 0 && len(r.Tests) == 0 {
		return stdout, false
	}
	totalFail := r.Summary.Failed + r.Summary.Error
	if totalFail == 0 {
		return fmt.Appendf(nil, "[pytest --json-report] %d tests passed in %.2fs\n", r.Summary.Total, r.Summary.Time), true
	}
	var out strings.Builder
	fmt.Fprintf(&out, "[pytest --json-report] FAILED %d/%d tests\n", totalFail, r.Summary.Total)
	const maxFailures = 5
	failures := make([]test, 0, totalFail)
	for _, tc := range r.Tests {
		if tc.Outcome != "failed" && tc.Outcome != "error" {
			continue
		}
		failures = append(failures, tc)
	}
	previous := -1
	for _, idx := range cappedEvidenceIndexes(len(failures), maxFailures, 2) {
		if previous >= 0 && idx > previous+1 {
			fmt.Fprintf(&out, "  ... +%d more failure(s)\n", idx-previous-1)
		}
		tc := failures[idx]
		fmt.Fprintf(&out, "  FAIL %s\n", tc.NodeID)
		if tc.LongRepr != "" {
			for _, line := range firstLines(tc.LongRepr, 3) {
				fmt.Fprintf(&out, "    %s\n", line)
			}
		}
		previous = idx
	}
	if len(failures) > 0 && previous < len(failures)-1 {
		fmt.Fprintf(&out, "  ... +%d more failure(s)\n", len(failures)-previous-1)
	}
	return []byte(out.String()), true
}

// TryCompactCargoTestJSON compacts `cargo test -- --format json -Z
// unstable-options` (the JSON test reporter) or the libtest JSON output
// from `cargo test -- --format json`. Schema: NDJSON with
// {"type":"suite|test","event":"started|ok|failed|...","..."}.
func TryCompactCargoTestJSON(argv []string, stdout []byte) ([]byte, bool) {
	if !isCargoTestJSONArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return stdout, false
	}
	lines := strings.Split(s, "\n")
	type event struct {
		Type   string `json:"type"`
		Event  string `json:"event"`
		Name   string `json:"name"`
		Passed int    `json:"passed"`
		Failed int    `json:"failed"`
		Stdout string `json:"stdout"`
	}
	var suiteFinal event
	var failedTests []event
	parsed := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line[0] != '{' {
			continue
		}
		var ev event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		parsed++
		if ev.Type == "suite" && (ev.Event == "ok" || ev.Event == "failed") {
			suiteFinal = ev
		}
		if ev.Type == "test" && ev.Event == "failed" {
			failedTests = append(failedTests, ev)
		}
	}
	if parsed == 0 {
		return stdout, false
	}
	if suiteFinal.Event == "ok" || (suiteFinal.Event == "" && len(failedTests) == 0) {
		passed := suiteFinal.Passed
		failed := suiteFinal.Failed
		return fmt.Appendf(nil, "[cargo test --format json] ok %d passed, %d failed\n", passed, failed), true
	}
	var out strings.Builder
	fmt.Fprintf(&out, "[cargo test --format json] FAILED %d failed, %d passed\n", suiteFinal.Failed, suiteFinal.Passed)
	const maxFailures = 5
	previous := -1
	for _, idx := range cappedEvidenceIndexes(len(failedTests), maxFailures, 2) {
		if previous >= 0 && idx > previous+1 {
			fmt.Fprintf(&out, "  ... +%d more failure(s)\n", idx-previous-1)
		}
		ft := failedTests[idx]
		fmt.Fprintf(&out, "  FAIL %s\n", ft.Name)
		if ft.Stdout != "" {
			for _, line := range firstLines(ft.Stdout, 3) {
				fmt.Fprintf(&out, "    %s\n", line)
			}
		}
		previous = idx
	}
	if len(failedTests) > 0 && previous < len(failedTests)-1 {
		fmt.Fprintf(&out, "  ... +%d more failure(s)\n", len(failedTests)-previous-1)
	}
	return []byte(out.String()), true
}

// ---- argv matchers ----

func isJestVitestJSONArgv(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	base := basenameLower(argv[0])
	known := map[string]bool{
		"vitest": true, "jest": true,
		"npx": true, "pnpm": true, "yarn": true, "bun": true,
	}
	if !known[base] && !strings.HasSuffix(argv[0], "vitest") && !strings.HasSuffix(argv[0], "jest") {
		return false
	}
	joined := strings.Join(argv, " ")
	if base == "npx" || base == "pnpm" || base == "yarn" || base == "bun" {
		if !strings.Contains(joined, "vitest") && !strings.Contains(joined, "jest") {
			return false
		}
	}
	return strings.Contains(joined, "--reporter=json") ||
		strings.Contains(joined, "--reporter json") ||
		strings.Contains(joined, "--json") ||
		strings.Contains(joined, "-r json")
}

func vitestLabelFromArgv(argv []string) string {
	joined := strings.Join(argv, " ")
	if strings.Contains(joined, "vitest") {
		return "vitest --reporter=json"
	}
	if strings.Contains(joined, "jest") {
		return "jest --json"
	}
	return "test json"
}

func isPytestJSONArgv(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	base := basenameLower(argv[0])
	if base != "pytest" && base != "py.test" && !strings.HasSuffix(argv[0], "pytest") {
		return false
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "--json-report") {
		return false
	}
	// stdout-mode requires --json-report-file=- ; otherwise the report
	// is written to a file and stdout still has the usual text.
	return strings.Contains(joined, "--json-report-file=-") ||
		strings.Contains(joined, "--json-report-file -") ||
		strings.Contains(joined, "--json-report-file=/dev/stdout")
}

func isCargoTestJSONArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	base := basenameLower(argv[0])
	if base != "cargo" {
		return false
	}
	sub := strings.ToLower(argv[1])
	if sub != "test" && sub != "nextest" {
		return false
	}
	joined := strings.Join(argv, " ")
	// libtest JSON: cargo test -- --format json
	// cargo nextest also supports --message-format json
	return strings.Contains(joined, "--format json") ||
		strings.Contains(joined, "--format=json") ||
		strings.Contains(joined, "--message-format json") ||
		strings.Contains(joined, "--message-format=json")
}

// firstLines returns up to n non-empty lines from s after trimming each
// for whitespace. Empty lines and pure-whitespace lines are dropped
// before the truncation count is applied — n counts content lines, not
// raw splits — so a payload like "\n\nreal\nthing\n" with n=3 returns
// ["real","thing"] rather than ["real"].
func firstLines(s string, n int) []string {
	if n <= 0 {
		return nil
	}
	out := make([]string, 0, n)
	for l := range strings.SplitSeq(s, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		out = append(out, l)
		if len(out) >= n {
			break
		}
	}
	return out
}

func basenameLower(p string) string {
	idx := strings.LastIndexAny(p, "/\\")
	if idx >= 0 {
		p = p[idx+1:]
	}
	return strings.ToLower(p)
}

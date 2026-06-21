package evidence

import (
	"slices"
	"strings"
	"testing"
)

func TestAnalyzeClassifiesCoreContent(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		text string
		want ContentClass
	}{
		{name: "json", text: `{"ok":true,"items":[1,2]}`, want: ContentJSON},
		{name: "diff", argv: []string{"git", "diff"}, text: "diff --git a/a.go b/a.go\n@@ -1 +1 @@\n-old\n+new\n", want: ContentDiff},
		{name: "search", argv: []string{"rg", "TODO"}, text: "a.go:10:TODO one\nb.go:20:TODO two\n", want: ContentSearch},
		{name: "windows search", text: `C:\repo\src\a-file.go:10:TODO one` + "\n" + `C:\repo\src\b-file.go:20:TODO two`, want: ContentSearch},
		{name: "stacktrace", text: "Traceback (most recent call last):\n  File \"a.py\", line 1\nValueError: bad\n", want: ContentStacktrace},
		{name: "test", argv: []string{"go", "test"}, text: "--- FAIL: TestX (0.00s)\nFAIL\n", want: ContentTest},
		{name: "log", text: "2026-06-08 error failed\n2026-06-08 warn degraded\n", want: ContentLog},
		{name: "code", text: "package main\n\nfunc main() {}\n", want: ContentCode},
		{name: "unknown empty no argv", text: "", want: ContentPlain},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Analyze(tc.argv, []byte(tc.text))
			if got.ContentClass != tc.want {
				t.Fatalf("class=%s want %s signals=%v", got.ContentClass, tc.want, got.Signals)
			}
		})
	}
}

func TestAnalyzeBoundsLargeContent(t *testing.T) {
	jsonBody := []byte(`{"items":[`)
	for len(jsonBody) < analyzeMaxBytes*4 {
		jsonBody = append(jsonBody, []byte(`{"k":"v"},`)...)
	}
	jsonBody = append(jsonBody, []byte(`{"k":"v"}]}`)...)
	if got := Analyze(nil, jsonBody); got.ContentClass != ContentJSON {
		t.Fatalf("large JSON classified as %s, want %s", got.ContentClass, ContentJSON)
	}

	logLine := "2026-06-08 error failed\n2026-06-08 warn degraded\n"
	logBody := []byte(strings.Repeat(logLine, analyzeMaxBytes/len(logLine)*3))
	if got := Analyze(nil, logBody); got.ContentClass != ContentLog {
		t.Fatalf("large log classified as %s, want %s", got.ContentClass, ContentLog)
	}

	small := []byte(`{"ok":true}`)
	if got := Analyze(nil, small); got.ContentClass != ContentJSON {
		t.Fatalf("small JSON classified as %s, want %s", got.ContentClass, ContentJSON)
	}
	notJSON := []byte("{ this is not json at all")
	if got := Analyze(nil, notJSON); got.ContentClass == ContentJSON {
		t.Fatal("small invalid JSON must not classify as JSON")
	}
}

func TestAnalyzeSignalsEvidence(t *testing.T) {
	text := "panic: failed hard\nsame line\nsame line\nsame line\nfile.go:12: warning\nTODO fix auth secret\nexit status 1\n" +
		"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\n"
	got := Analyze([]string{"go", "test"}, []byte(text))
	for _, signal := range []Signal{SignalErrorKeyword, SignalWarning, SignalImportant, SignalSecurity, SignalDedupe, SignalOutlier, SignalPath, SignalCount, SignalExitStatus, SignalFirstLast, SignalRecency} {
		if !hasSignal(got.Signals, signal) {
			t.Fatalf("missing signal %s in %v", signal, got.Signals)
		}
	}
}

func TestAnalyzeExtendedErrorSignals(t *testing.T) {
	for _, text := range []string{
		"request abort after timeout",
		"connection rejected by server",
		"critical crash in worker",
	} {
		got := Analyze([]string{"tail", "app.log"}, []byte(text))
		if !hasSignal(got.Signals, SignalErrorKeyword) {
			t.Fatalf("extended error signal missing for %q: %v", text, got.Signals)
		}
	}
	got := Analyze(nil, []byte("token budget reached but no sensitive material"))
	if hasSignal(got.Signals, SignalSecurity) {
		t.Fatalf("token must not be treated as security signal: %v", got.Signals)
	}
}

func TestDecisionFromObservationAccountsPositiveAndNegative(t *testing.T) {
	analysis := Analysis{ContentClass: ContentLog, Signals: []Signal{SignalErrorKeyword}}
	positive := DecisionFromObservation(0, "log_output", SafetyDiagnosticPriority, ActionApplied, "matched", analysis, []string{"error line"}, "fail-open", 100, 40)
	if positive.SavedTokens != 60 || positive.AddedTokens != 0 || positive.NetTokens != 60 {
		t.Fatalf("bad positive accounting: %+v", positive)
	}
	negative := DecisionFromObservation(3, "output_reduce_directive", SafetyFullPass, ActionSkipped, "negative_net", analysis, nil, "", 10, 14)
	if negative.SavedTokens != 0 || negative.AddedTokens != 4 || negative.NetTokens != -4 {
		t.Fatalf("bad negative accounting: %+v", negative)
	}
}

func TestRedactDecisionClonesSlices(t *testing.T) {
	in := BlockDecision{
		Mechanism:            "search_output",
		ContentClass:         ContentSearch,
		Signals:              []Signal{SignalPath},
		PreservedEvidence:    []string{"file"},
		FootprintScoreBucket: "high",
	}
	out := RedactDecision(in)
	out.Signals[0] = SignalWarning
	out.PreservedEvidence[0] = "changed"
	if in.Signals[0] != SignalPath || in.PreservedEvidence[0] != "file" {
		t.Fatalf("redaction should clone slices: in=%+v out=%+v", in, out)
	}
	if out.FootprintScoreBucket != "high" {
		t.Fatalf("redaction should preserve footprint bucket: %+v", out)
	}
}

func hasSignal(signals []Signal, want Signal) bool {
	return slices.Contains(signals, want)
}

func TestCommandContains(t *testing.T) {
	t.Parallel()
	if !commandContains([]string{"git", "status"}, "status") {
		t.Fatal("commandContains should find 'status'")
	}
	if !commandContains([]string{"git", "STATUS"}, "status") {
		t.Fatal("commandContains should be case-insensitive")
	}
	if !commandContains([]string{"git", "  status  "}, "status") {
		t.Fatal("commandContains should trim whitespace")
	}
	if commandContains([]string{"git", "log"}, "status") {
		t.Fatal("commandContains should not find 'status' in git log")
	}
	if commandContains([]string{}, "status") {
		t.Fatal("commandContains should return false for empty argv")
	}
}

func TestCloneSignals(t *testing.T) {
	t.Parallel()
	// Empty input -> nil.
	if got := cloneSignals(nil); got != nil {
		t.Fatalf("cloneSignals(nil) = %v, want nil", got)
	}
	if got := cloneSignals([]Signal{}); got != nil {
		t.Fatalf("cloneSignals([]) = %v, want nil", got)
	}
	// Non-empty input -> cloned slice.
	in := []Signal{SignalPath, SignalWarning}
	got := cloneSignals(in)
	if len(got) != 2 || got[0] != SignalPath || got[1] != SignalWarning {
		t.Fatalf("cloneSignals = %v, want %v", got, in)
	}
	// Modifying the clone should not affect the original.
	got[0] = SignalErrorKeyword
	if in[0] != SignalPath {
		t.Fatal("modifying clone should not affect original")
	}
}

func TestCloneStrings(t *testing.T) {
	t.Parallel()
	// Empty input -> nil.
	if got := cloneStrings(nil); got != nil {
		t.Fatalf("cloneStrings(nil) = %v, want nil", got)
	}
	if got := cloneStrings([]string{}); got != nil {
		t.Fatalf("cloneStrings([]) = %v, want nil", got)
	}
	// Non-empty input -> cloned slice.
	in := []string{"a", "b"}
	got := cloneStrings(in)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("cloneStrings = %v, want %v", got, in)
	}
}

package evidence

import "testing"

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

func TestAnalyzeSignalsEvidence(t *testing.T) {
	text := "panic: failed hard\nsame line\nsame line\nsame line\nfile.go:12: warning\nexit status 1\n" +
		"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\n"
	got := Analyze([]string{"go", "test"}, []byte(text))
	for _, signal := range []Signal{SignalErrorKeyword, SignalWarning, SignalDedupe, SignalOutlier, SignalPath, SignalCount, SignalExitStatus, SignalFirstLast, SignalRecency} {
		if !hasSignal(got.Signals, signal) {
			t.Fatalf("missing signal %s in %v", signal, got.Signals)
		}
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
		Mechanism:         "search_output",
		ContentClass:      ContentSearch,
		Signals:           []Signal{SignalPath},
		PreservedEvidence: []string{"file"},
	}
	out := RedactDecision(in)
	out.Signals[0] = SignalWarning
	out.PreservedEvidence[0] = "changed"
	if in.Signals[0] != SignalPath || in.PreservedEvidence[0] != "file" {
		t.Fatalf("redaction should clone slices: in=%+v out=%+v", in, out)
	}
}

func hasSignal(signals []Signal, want Signal) bool {
	for _, signal := range signals {
		if signal == want {
			return true
		}
	}
	return false
}

package filter

import (
	"strings"
	"testing"
)

func TestTryCompactSARIF_ZeroResults(t *testing.T) {
	in := `{"$schema":"https://json.schemastore.org/sarif-2.1.0.json","version":"2.1.0","runs":[{"tool":{"driver":{"name":"clippy"}},"results":[]}]}`
	out, ok := TryCompactSARIF([]string{"clippy", "--format", "sarif"}, []byte(in))
	if !ok {
		t.Fatalf("expected match")
	}
	if string(out) != "[sarif: clippy] 0 results\n" {
		t.Fatalf("unexpected: %q", string(out))
	}
}

func TestTryCompactSARIF_SingleErrorWithLocation(t *testing.T) {
	in := `{
		"$schema":"https://json.schemastore.org/sarif-2.1.0.json",
		"version":"2.1.0",
		"runs":[{
			"tool":{"driver":{"name":"eslint","version":"9.0.0"}},
			"results":[
				{"ruleId":"no-unused-vars","level":"error","message":{"text":"'x' is defined but never used."},
					"locations":[{"physicalLocation":{"artifactLocation":{"uri":"src/foo.ts"},"region":{"startLine":12,"startColumn":7}}}]}
			]
		}]
	}`
	out, ok := TryCompactSARIF([]string{"eslint", "--format=sarif"}, []byte(in))
	if !ok {
		t.Fatalf("expected match")
	}
	s := string(out)
	if !strings.Contains(s, "[sarif: eslint]") {
		t.Fatalf("missing tool label: %q", s)
	}
	if !strings.Contains(s, "1 result(s)") || !strings.Contains(s, "1 error") {
		t.Fatalf("missing counters: %q", s)
	}
	if !strings.Contains(s, "src/foo.ts:12:7 error [no-unused-vars] 'x' is defined but never used.") {
		t.Fatalf("missing detail line: %q", s)
	}
}

func TestTryCompactSARIF_MultipleToolsAndLevels(t *testing.T) {
	in := `{
		"version":"2.1.0",
		"runs":[
			{"tool":{"driver":{"name":"clippy"}},"results":[
				{"ruleId":"E1","level":"error","message":{"text":"boom"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"a.rs"},"region":{"startLine":1}}}]},
				{"ruleId":"W1","level":"warning","message":{"text":"meh"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"b.rs"}}}]}
			]},
			{"tool":{"driver":{"name":"hadolint"}},"results":[
				{"ruleId":"DL3008","level":"note","message":{"text":"pin version"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"Dockerfile"},"region":{"startLine":4}}}]}
			]}
		]
	}`
	out, ok := TryCompactSARIF([]string{"some-runner"}, []byte(in))
	if !ok {
		t.Fatalf("expected match")
	}
	s := string(out)
	if !strings.Contains(s, "clippy+hadolint") {
		t.Fatalf("expected combined tool label, got %q", s)
	}
	if !strings.Contains(s, "3 result(s)") {
		t.Fatalf("expected 3 results, got %q", s)
	}
	if !strings.Contains(s, "1 error") || !strings.Contains(s, "1 warning") || !strings.Contains(s, "1 note") {
		t.Fatalf("expected level counts, got %q", s)
	}
}

func TestTryCompactSARIF_DuplicateToolNamesAreCollapsed(t *testing.T) {
	in := `{
		"version":"2.1.0",
		"runs":[
			{"tool":{"driver":{"name":"eslint"}},"results":[{"level":"warning","message":{"text":"one"},"locations":[]}]},
			{"tool":{"driver":{"name":"eslint"}},"results":[{"level":"error","message":{"text":"two"},"locations":[]}]}
		]
	}`
	out, ok := TryCompactSARIF(nil, []byte(in))
	if !ok {
		t.Fatalf("expected match")
	}
	s := string(out)
	if strings.Count(s, "[sarif: eslint]") != 1 || strings.Contains(s, "eslint+eslint") {
		t.Fatalf("duplicate tool name not collapsed: %q", s)
	}
}

func TestTryCompactSARIF_TruncatesAfterMax(t *testing.T) {
	results := make([]string, 15)
	for i := range results {
		results[i] = `{"ruleId":"R","level":"warning","message":{"text":"w"},"locations":[]}`
	}
	in := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"x"}},"results":[` + strings.Join(results, ",") + `]}]}`
	out, ok := TryCompactSARIF(nil, []byte(in))
	if !ok {
		t.Fatalf("expected match")
	}
	if !strings.Contains(string(out), "+5 more") {
		t.Fatalf("expected truncation marker, got %q", string(out))
	}
}

func TestTryCompactSARIF_ErrorPastCapSurvives(t *testing.T) {
	results := make([]string, 14)
	for i := range results {
		results[i] = `{"ruleId":"W","level":"warning","message":{"text":"warning"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"warn.go"}}}]}`
	}
	results = append(results, `{"ruleId":"E_LATE","level":"error","message":{"text":"late critical error"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"late.go"},"region":{"startLine":42}}}]}`)
	in := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"lint"}},"results":[` + strings.Join(results, ",") + `]}]}`
	out, ok := TryCompactSARIF(nil, []byte(in))
	if !ok {
		t.Fatalf("expected match")
	}
	s := string(out)
	if !strings.Contains(s, "late.go:42 error [E_LATE] late critical error") {
		t.Fatalf("late error dropped: %q", s)
	}
	if strings.Count(s, "warn.go warning [W] warning") >= 10 {
		t.Fatalf("warnings should not crowd out the late error: %q", s)
	}
}

func TestTryCompactSARIF_NotSARIF_PassthroughTextRejected(t *testing.T) {
	cases := [][]byte{
		[]byte(""),
		[]byte("not json at all"),
		[]byte(`{"foo":"bar"}`),
		[]byte(`{"runs":"oops"}`),               // runs is wrong type
		[]byte(`{"version":"2.1.0","runs":[]}`), // version but no runs entries
		[]byte(`{"$schema":"https://...","version":"2.1.0","runs":[{}]}`), // empty run
	}
	for i, c := range cases {
		_, ok := TryCompactSARIF(nil, c)
		if ok && i < 5 {
			t.Fatalf("case %d unexpectedly matched: %q", i, string(c))
		}
	}
}

func TestTryCompactSARIF_TruncatesLongMessages(t *testing.T) {
	longMsg := strings.Repeat("verbose-detail ", 30) // > 160 chars
	in := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"t"}},"results":[{"ruleId":"R","level":"warning","message":{"text":"` + longMsg + `"},"locations":[]}]}]}`
	out, ok := TryCompactSARIF(nil, []byte(in))
	if !ok {
		t.Fatalf("expected match")
	}
	if !strings.Contains(string(out), "...") {
		t.Fatalf("expected ellipsis on long message, got %q", string(out))
	}
}

func TestTryCompactSARIF_NoLocationHandled(t *testing.T) {
	in := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"t"}},"results":[{"ruleId":"R","level":"warning","message":{"text":"m"},"locations":[]}]}]}`
	out, ok := TryCompactSARIF(nil, []byte(in))
	if !ok {
		t.Fatalf("expected match")
	}
	if !strings.Contains(string(out), "<no-location>") {
		t.Fatalf("expected no-location placeholder, got %q", string(out))
	}
}

func TestTryCompactSARIF_DefaultsToolNameLevelRule(t *testing.T) {
	in := `{"version":"2.1.0","runs":[{"tool":{"driver":{}},"results":[{"locations":[]}]}]}`
	out, ok := TryCompactSARIF(nil, []byte(in))
	if !ok {
		t.Fatalf("expected match")
	}
	s := string(out)
	// Tool defaults to "sarif"; level defaults to warning; rule "?"; message "(no message)".
	if !strings.Contains(s, "[sarif: sarif]") || !strings.Contains(s, "warning") || !strings.Contains(s, "[?]") || !strings.Contains(s, "(no message)") {
		t.Fatalf("default fallbacks missing in %q", s)
	}
}

func TestTryCompactSARIF_IgnoresNonJSONStart(t *testing.T) {
	in := `   prefix
{"version":"2.1.0","runs":[]}`
	_, ok := TryCompactSARIF(nil, []byte(in))
	if ok {
		t.Fatalf("must not match content that doesn't start with {")
	}
}

func TestIsSARIFArgv(t *testing.T) {
	cases := map[string]bool{
		"clippy --format sarif":               true,
		"eslint --format=sarif":               true,
		"ruff check --output-format sarif":    true,
		"golangci-lint --output-format=sarif": true,
		"foo -f sarif":                        true,
		"eslint --format json":                false,
		"":                                    false,
	}
	for cmd, want := range cases {
		got := isSARIFArgv(strings.Fields(cmd))
		if got != want {
			t.Errorf("isSARIFArgv(%q) = %v, want %v", cmd, got, want)
		}
	}
}

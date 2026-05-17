package filter

import (
	"strings"
	"testing"
)

func TestTryCompactVitestJSON_AllPass(t *testing.T) {
	in := `{"numTotalTestSuites":2,"numPassedTestSuites":2,"numFailedTestSuites":0,"numTotalTests":7,"numPassedTests":7,"numFailedTests":0,"testResults":[]}`
	out, ok := TryCompactVitestJSON([]string{"vitest", "run", "--reporter=json"}, []byte(in))
	if !ok {
		t.Fatalf("expected match")
	}
	if string(out) != "[vitest --reporter=json] 7 tests passed in 2 suite(s)\n" {
		t.Fatalf("unexpected: %q", string(out))
	}
}

func TestTryCompactVitestJSON_FailureExtractsTopFailures(t *testing.T) {
	in := `{
		"numTotalTestSuites":1,"numFailedTestSuites":1,"numPassedTestSuites":0,
		"numTotalTests":3,"numFailedTests":2,"numPassedTests":1,
		"testResults":[{
			"name":"src/foo.test.ts","status":"failed",
			"assertionResults":[
				{"status":"failed","fullName":"foo > bar","failureMessages":["AssertionError: expected 1 to equal 2","  at line 12"]},
				{"status":"failed","fullName":"foo > baz","failureMessages":["TypeError: undefined"]},
				{"status":"passed","fullName":"foo > qux"}
			]
		}]
	}`
	out, ok := TryCompactVitestJSON([]string{"vitest", "run", "--reporter=json"}, []byte(in))
	if !ok {
		t.Fatalf("expected match")
	}
	s := string(out)
	if !strings.Contains(s, "FAILED 2/3") {
		t.Fatalf("missing summary: %q", s)
	}
	if !strings.Contains(s, "FAIL foo > bar") || !strings.Contains(s, "FAIL foo > baz") {
		t.Fatalf("missing failure names: %q", s)
	}
	if !strings.Contains(s, "AssertionError: expected 1 to equal 2") {
		t.Fatalf("missing failure message: %q", s)
	}
}

func TestTryCompactVitestJSON_TruncatesAfterMaxFailures(t *testing.T) {
	suite := `{"name":"x.test.ts","status":"failed","assertionResults":[`
	for i := 0; i < 8; i++ {
		if i > 0 {
			suite += ","
		}
		suite += `{"status":"failed","fullName":"t` + string(rune('0'+i)) + `","failureMessages":["boom"]}`
	}
	suite += `]}`
	in := `{"numTotalTests":8,"numFailedTests":8,"numTotalTestSuites":1,"numFailedTestSuites":1,"testResults":[` + suite + `]}`
	out, ok := TryCompactVitestJSON([]string{"vitest", "run", "--reporter=json"}, []byte(in))
	if !ok {
		t.Fatalf("expected match")
	}
	if !strings.Contains(string(out), "+3 more failure") {
		t.Fatalf("expected truncation marker, got %q", string(out))
	}
}

func TestTryCompactVitestJSON_NoMatchOnWrongArgv(t *testing.T) {
	_, ok := TryCompactVitestJSON([]string{"echo", "hello"}, []byte(`{"numTotalTests":1}`))
	if ok {
		t.Fatalf("must not match echo")
	}
}

func TestTryCompactVitestJSON_NoMatchOnNonJSONBody(t *testing.T) {
	_, ok := TryCompactVitestJSON([]string{"vitest", "--reporter=json"}, []byte("PASS\n"))
	if ok {
		t.Fatalf("must not match non-JSON body")
	}
}

func TestTryCompactVitestJSON_JestArgvLabel(t *testing.T) {
	in := `{"numTotalTestSuites":1,"numTotalTests":1,"numPassedTests":1}`
	out, ok := TryCompactVitestJSON([]string{"jest", "--json"}, []byte(in))
	if !ok {
		t.Fatalf("expected match")
	}
	if !strings.Contains(string(out), "[jest --json]") {
		t.Fatalf("expected jest label, got %q", string(out))
	}
}

func TestTryCompactVitestJSON_NpxWrapped(t *testing.T) {
	in := `{"numTotalTestSuites":1,"numTotalTests":1,"numPassedTests":1}`
	_, ok := TryCompactVitestJSON([]string{"npx", "vitest", "--reporter=json"}, []byte(in))
	if !ok {
		t.Fatalf("expected npx-wrapped vitest to match")
	}
}

func TestTryCompactVitestJSON_NpxWithoutTestRunner(t *testing.T) {
	in := `{"numTotalTestSuites":1,"numTotalTests":1,"numPassedTests":1}`
	_, ok := TryCompactVitestJSON([]string{"npx", "tsc", "--reporter=json"}, []byte(in))
	if ok {
		t.Fatalf("npx tsc must not match vitest parser")
	}
}

func TestTryCompactVitestJSON_EmptyTotalSkips(t *testing.T) {
	in := `{"numTotalTestSuites":0,"numTotalTests":0}`
	_, ok := TryCompactVitestJSON([]string{"vitest", "--reporter=json"}, []byte(in))
	if ok {
		t.Fatalf("empty total should not match")
	}
}

func TestTryCompactVitestJSON_InvalidJSONAndTitleFallback(t *testing.T) {
	if _, ok := TryCompactVitestJSON(nil, []byte(`{"numTotalTests":1}`)); ok {
		t.Fatal("empty argv must not match")
	}
	if _, ok := TryCompactVitestJSON([]string{"custom-vitest", "-r", "json"}, []byte(`{not-json`)); ok {
		t.Fatal("invalid JSON must not match")
	}
	in := `{"numTotalTestSuites":1,"numFailedTestSuites":1,"numTotalTests":1,"numFailedTests":1,"testResults":[{"assertionResults":[{"status":"failed","title":"title fallback","failureMessages":["boom"]}]}]}`
	out, ok := TryCompactVitestJSON([]string{"custom-vitest", "-r", "json"}, []byte(in))
	if !ok {
		t.Fatal("custom vitest suffix should match")
	}
	s := string(out)
	if !strings.Contains(s, "[vitest --reporter=json]") || !strings.Contains(s, "FAIL title fallback") {
		t.Fatalf("unexpected custom-label/title output: %q", s)
	}
}

func TestTryCompactPytestJSON_AllPass(t *testing.T) {
	in := `{"summary":{"passed":5,"failed":0,"total":5,"duration":1.23},"tests":[]}`
	out, ok := TryCompactPytestJSON([]string{"pytest", "--json-report", "--json-report-file=-"}, []byte(in))
	if !ok {
		t.Fatalf("expected match")
	}
	if !strings.Contains(string(out), "5 tests passed in 1.23s") {
		t.Fatalf("unexpected: %q", string(out))
	}
}

func TestTryCompactPytestJSON_Failures(t *testing.T) {
	in := `{"summary":{"passed":1,"failed":2,"total":3,"duration":0.5},"tests":[
		{"nodeid":"test_a.py::test_x","outcome":"failed","longrepr":"assert 1 == 2\nat test_a.py:5"},
		{"nodeid":"test_a.py::test_y","outcome":"failed","longrepr":"boom"},
		{"nodeid":"test_a.py::test_z","outcome":"passed"}
	]}`
	out, ok := TryCompactPytestJSON([]string{"pytest", "--json-report", "--json-report-file=-"}, []byte(in))
	if !ok {
		t.Fatalf("expected match")
	}
	if !strings.Contains(string(out), "FAILED 2/3") ||
		!strings.Contains(string(out), "FAIL test_a.py::test_x") ||
		!strings.Contains(string(out), "assert 1 == 2") {
		t.Fatalf("unexpected: %q", string(out))
	}
}

func TestTryCompactPytestJSON_RequiresStdoutFile(t *testing.T) {
	in := `{"summary":{"total":1,"passed":1}}`
	_, ok := TryCompactPytestJSON([]string{"pytest", "--json-report"}, []byte(in))
	if ok {
		t.Fatalf("must not match without --json-report-file=-")
	}
}

func TestTryCompactPytestJSON_NotPytest(t *testing.T) {
	_, ok := TryCompactPytestJSON([]string{"echo"}, []byte(`{"summary":{}}`))
	if ok {
		t.Fatalf("must not match echo")
	}
}

func TestTryCompactPytestJSON_Edges(t *testing.T) {
	if _, ok := TryCompactPytestJSON(nil, []byte(`{"summary":{"total":1}}`)); ok {
		t.Fatal("empty argv must not match")
	}
	if _, ok := TryCompactPytestJSON([]string{"/tmp/pytest", "--json-report", "--json-report-file=-"}, []byte("PASS")); ok {
		t.Fatal("non-JSON body must not match")
	}
	if _, ok := TryCompactPytestJSON([]string{"pytest", "--json-report", "--json-report-file=-"}, []byte(`{not-json`)); ok {
		t.Fatal("invalid JSON must not match")
	}
	if _, ok := TryCompactPytestJSON([]string{"py.test", "--json-report", "--json-report-file=/dev/stdout"}, []byte(`{"summary":{"total":0},"tests":[]}`)); ok {
		t.Fatal("empty pytest report must not match")
	}

	tests := make([]string, 0, 7)
	for i := 0; i < 7; i++ {
		outcome := "failed"
		if i == 6 {
			outcome = "error"
		}
		tests = append(tests, `{"nodeid":"test_mod.py::test_`+string(rune('a'+i))+`","outcome":"`+outcome+`","longrepr":"line1\nline2\nline3\nline4"}`)
	}
	in := `{"summary":{"failed":6,"error":1,"total":7},"tests":[` + strings.Join(tests, ",") + `]}`
	out, ok := TryCompactPytestJSON([]string{"pytest", "--json-report", "--json-report-file", "-"}, []byte(in))
	if !ok {
		t.Fatal("pytest failure report should compact")
	}
	if !strings.Contains(string(out), "+2 more failure") || strings.Contains(string(out), "line4") {
		t.Fatalf("pytest truncation/firstLines failed: %q", out)
	}
}

func TestTryCompactCargoTestJSON_AllPass(t *testing.T) {
	in := strings.Join([]string{
		`{"type":"suite","event":"started","test_count":3}`,
		`{"type":"test","event":"ok","name":"a"}`,
		`{"type":"test","event":"ok","name":"b"}`,
		`{"type":"test","event":"ok","name":"c"}`,
		`{"type":"suite","event":"ok","passed":3,"failed":0}`,
	}, "\n")
	out, ok := TryCompactCargoTestJSON([]string{"cargo", "test", "--", "--format", "json"}, []byte(in))
	if !ok {
		t.Fatalf("expected match")
	}
	if !strings.Contains(string(out), "ok 3 passed, 0 failed") {
		t.Fatalf("unexpected: %q", string(out))
	}
}

func TestTryCompactCargoTestJSON_Failures(t *testing.T) {
	in := strings.Join([]string{
		`{"type":"suite","event":"started","test_count":3}`,
		`{"type":"test","event":"ok","name":"a"}`,
		`{"type":"test","event":"failed","name":"b","stdout":"assert failed at line 5"}`,
		`{"type":"test","event":"failed","name":"c"}`,
		`{"type":"suite","event":"failed","passed":1,"failed":2}`,
	}, "\n")
	out, ok := TryCompactCargoTestJSON([]string{"cargo", "test", "--", "--format", "json"}, []byte(in))
	if !ok {
		t.Fatalf("expected match")
	}
	if !strings.Contains(string(out), "FAILED 2 failed, 1 passed") ||
		!strings.Contains(string(out), "FAIL b") ||
		!strings.Contains(string(out), "FAIL c") ||
		!strings.Contains(string(out), "assert failed at line 5") {
		t.Fatalf("unexpected: %q", string(out))
	}
}

func TestTryCompactCargoTestJSON_NotCargo(t *testing.T) {
	_, ok := TryCompactCargoTestJSON([]string{"go", "test", "--format", "json"}, []byte(""))
	if ok {
		t.Fatalf("must not match non-cargo")
	}
}

func TestTryCompactCargoTestJSON_RequiresJSONFlag(t *testing.T) {
	_, ok := TryCompactCargoTestJSON([]string{"cargo", "test"}, []byte(""))
	if ok {
		t.Fatalf("must not match without --format json")
	}
}

func TestTryCompactCargoTestJSON_NoParseable(t *testing.T) {
	in := "PASS\nFAIL\n"
	_, ok := TryCompactCargoTestJSON([]string{"cargo", "test", "--", "--format", "json"}, []byte(in))
	if ok {
		t.Fatalf("must not match unparseable stream")
	}
}

func TestTryCompactCargoTestJSON_Edges(t *testing.T) {
	if _, ok := TryCompactCargoTestJSON([]string{"cargo"}, []byte(`{}`)); ok {
		t.Fatal("short cargo argv must not match")
	}
	if _, ok := TryCompactCargoTestJSON([]string{"cargo", "build", "--message-format=json"}, []byte(`{}`)); ok {
		t.Fatal("non-test cargo subcommand must not match")
	}
	if _, ok := TryCompactCargoTestJSON([]string{"cargo", "test", "--", "--format=json"}, nil); ok {
		t.Fatal("empty cargo JSON output must not match")
	}

	lines := []string{"not-json", `{"type":"test","event":"failed","name":"a"}`}
	for i := 0; i < 7; i++ {
		lines = append(lines, `{"type":"test","event":"failed","name":"f`+string(rune('0'+i))+`","stdout":"one\ntwo\nthree\nfour"}`)
	}
	lines = append(lines, `{"type":"suite","event":"failed","passed":1,"failed":8}`)
	out, ok := TryCompactCargoTestJSON([]string{"cargo", "nextest", "--message-format", "json"}, []byte(strings.Join(lines, "\n")))
	if !ok {
		t.Fatal("cargo failed JSON should compact")
	}
	if !strings.Contains(string(out), "+3 more failure") || strings.Contains(string(out), "four") {
		t.Fatalf("cargo truncation/firstLines failed: %q", out)
	}
}

func TestTryCompactCargoTestJSON_NextestSupported(t *testing.T) {
	in := `{"type":"suite","event":"ok","passed":1,"failed":0}`
	_, ok := TryCompactCargoTestJSON([]string{"cargo", "nextest", "run", "--message-format=json"}, []byte(in))
	if !ok {
		t.Fatalf("nextest with --message-format=json should match")
	}
}

func TestFirstLines_HandlesShortInput(t *testing.T) {
	got := firstLines("only-one", 5)
	if len(got) != 1 || got[0] != "only-one" {
		t.Fatalf("unexpected: %v", got)
	}
}

func TestFirstLines_TrimsEmpty(t *testing.T) {
	got := firstLines("\n\nreal\nthing\n", 3)
	if len(got) != 2 || got[0] != "real" || got[1] != "thing" {
		t.Fatalf("unexpected: %v", got)
	}
}

func TestFirstLines_ZeroN(t *testing.T) {
	if got := firstLines("anything", 0); got != nil {
		t.Fatalf("expected nil for n=0, got %v", got)
	}
}

func TestBasenameLower(t *testing.T) {
	cases := map[string]string{
		"vitest":                "vitest",
		"/usr/local/bin/Vitest": "vitest",
		`C:\Tools\Vitest.exe`:   "vitest.exe",
		"./pytest":              "pytest",
	}
	for in, want := range cases {
		if got := basenameLower(in); got != want {
			t.Fatalf("basenameLower(%q) = %q, want %q", in, got, want)
		}
	}
}

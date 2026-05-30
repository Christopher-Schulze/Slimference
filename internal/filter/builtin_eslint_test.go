package filter

import (
	"fmt"
	"strings"
	"testing"
)

func TestTryCompactEslintJSON_clean(t *testing.T) {
	t.Parallel()
	in := `[{"filePath":"/src/a.js","messages":[],"errorCount":0,"warningCount":0},` +
		`{"filePath":"/src/b.js","messages":[],"errorCount":0,"warningCount":0}]`
	out, ok := TryCompactEslintJSON([]string{"eslint", "--format", "json", "src/"}, []byte(in))
	if !ok {
		t.Fatalf("expected compaction")
	}
	if got := string(out); got != "[eslint] clean (2 file(s))\n" {
		t.Fatalf("clean summary = %q", got)
	}
}

func TestTryCompactEslintJSON_errorsAndWarnings(t *testing.T) {
	t.Parallel()
	in := `[{"filePath":"/src/a.js","messages":[` +
		`{"ruleId":"no-unused-vars","severity":2,"message":"'x' is defined but never used.","line":3,"column":7},` +
		`{"ruleId":"semi","severity":1,"message":"Missing semicolon.","line":5,"column":12}` +
		`],"errorCount":1,"warningCount":1}]`
	out, ok := TryCompactEslintJSON([]string{"eslint", "-f", "json"}, []byte(in))
	if !ok {
		t.Fatalf("expected compaction")
	}
	got := string(out)
	if !strings.Contains(got, "[eslint] 1 error(s), 1 warning(s) in 1 file(s)") {
		t.Fatalf("summary missing: %q", got)
	}
	if !strings.Contains(got, "/src/a.js:3:7 error [no-unused-vars]") {
		t.Fatalf("error row missing: %q", got)
	}
	if !strings.Contains(got, "/src/a.js:5:12 warning [semi]") {
		t.Fatalf("warning row missing: %q", got)
	}
	// Errors must come before warnings in the output.
	if strings.Index(got, "error [no-unused-vars]") > strings.Index(got, "warning [semi]") {
		t.Fatalf("error should precede warning: %q", got)
	}
}

func TestTryCompactEslintJSON_errorSurvivesPastWarningCap(t *testing.T) {
	t.Parallel()
	var msgs strings.Builder
	for i := 0; i < 25; i++ {
		msgs.WriteString(fmt.Sprintf(`{"ruleId":"semi","severity":1,"message":"Missing semicolon.","line":%d,"column":1},`, i+1))
	}
	// The single error sits AFTER 25 warnings; it must still be emitted because
	// errors are selected before warnings, ahead of the row cap.
	msgs.WriteString(`{"ruleId":"no-undef","severity":2,"message":"'criticalSymbol' is not defined.","line":99,"column":3}`)
	in := `[{"filePath":"/src/big.js","messages":[` + msgs.String() + `],"errorCount":1,"warningCount":25}]`
	out, ok := TryCompactEslintJSON([]string{"eslint", "--format=json"}, []byte(in))
	if !ok {
		t.Fatalf("expected compaction")
	}
	got := string(out)
	if !strings.Contains(got, "criticalSymbol") {
		t.Fatalf("error past warning cap was dropped: %q", got[:min(len(got), 300)])
	}
	if !strings.Contains(got, "more problem(s)") {
		t.Fatalf("expected truncation notice: %q", got)
	}
}

func TestTryCompactEslintJSON_passThrough(t *testing.T) {
	t.Parallel()
	// Not eslint argv.
	if _, ok := TryCompactEslintJSON([]string{"jest", "--json"}, []byte("[]")); ok {
		t.Fatal("non-eslint argv should pass through")
	}
	// eslint but no json format.
	if _, ok := TryCompactEslintJSON([]string{"eslint", "src/"}, []byte("[]")); ok {
		t.Fatal("eslint without json format should pass through")
	}
	// eslint json argv but non-array / invalid payload.
	if _, ok := TryCompactEslintJSON([]string{"eslint", "--format", "json"}, []byte("not json")); ok {
		t.Fatal("invalid payload should pass through")
	}
	if _, ok := TryCompactEslintJSON([]string{"eslint", "--format", "json"}, []byte(`[{bad`)); ok {
		t.Fatal("malformed json should pass through")
	}
}

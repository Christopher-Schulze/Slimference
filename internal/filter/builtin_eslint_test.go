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

func TestTryCompactEslintJSON_windowsShimArgv(t *testing.T) {
	t.Parallel()
	in := `[{"filePath":"C:\\src\\a.js","messages":[],"errorCount":0,"warningCount":0}]`
	out, ok := TryCompactEslintJSON([]string{`C:\repo\node_modules\.bin\eslint.cmd`, "-f=json"}, []byte(in))
	if !ok {
		t.Fatalf("expected compaction for eslint.cmd")
	}
	if got := string(out); got != "[eslint] clean (1 file(s))\n" {
		t.Fatalf("clean summary = %q", got)
	}
}

func TestTryCompactEslintJSON_errorSurvivesPastWarningCap(t *testing.T) {
	t.Parallel()
	var msgs strings.Builder
	for i := range 25 {
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

func TestTryCompactEslintJSON_lateErrorSurvivesWithinErrorCap(t *testing.T) {
	t.Parallel()
	var msgs strings.Builder
	for i := range 26 {
		if i > 0 {
			msgs.WriteString(",")
		}
		msgs.WriteString(fmt.Sprintf(`{"ruleId":"rule-%02d","severity":2,"message":"error-%02d","line":%d,"column":1}`, i, i, i+1))
	}
	in := `[{"filePath":"/src/errors.js","messages":[` + msgs.String() + `],"errorCount":26,"warningCount":0}]`
	out, ok := TryCompactEslintJSON([]string{"eslint", "--format=json"}, []byte(in))
	if !ok {
		t.Fatalf("expected compaction")
	}
	got := string(out)
	if !strings.Contains(got, "error-25") {
		t.Fatalf("late same-priority error was dropped: %q", got)
	}
	if strings.Contains(got, "error-18") {
		t.Fatalf("middle same-priority error should be capped before tail evidence: %q", got)
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

func TestTryCompactEslintStylishFindings(t *testing.T) {
	t.Parallel()

	input := eslintStylishFixture("/repo/src/app.js", 50, true)
	out, ok := TryCompactEslintStylish([]string{"eslint", "src", "--format", "stylish"}, []byte(input))
	if !ok {
		t.Fatal("expected ESLint stylish findings to compact")
	}
	got := string(out)
	for _, want := range []string{
		"[eslint] FINDINGS (100 problems: 50 errors, 50 warnings in 1 file)",
		"/repo/src/app.js",
		"2:1 warning [no-console] Unexpected console statement",
		"2:20 error [eqeqeq] Expected '===' and instead saw '=='",
		"1 error and 0 warnings potentially fixable with the `--fix` option.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("compact ESLint stylish output missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "\n\n/repo/src/app.js\n") {
		t.Fatalf("file heading should be normalized into finding rows: %q", got)
	}
}

func TestTryCompactEslintStylishWarningOnly(t *testing.T) {
	t.Parallel()

	var input strings.Builder
	for range 30 {
		input.WriteString("\nsrc/app.js\n")
		input.WriteString("  1:1  warning  Unexpected console statement  no-console\n")
	}
	input.WriteString("\n✖ 30 problems (0 errors, 30 warnings)\n")
	out, ok := TryCompactEslintStylish([]string{"npx", "--yes", "eslint", "src"}, []byte(input.String()))
	if !ok {
		t.Fatal("expected warning-only stylish findings to compact")
	}
	got := string(out)
	if !strings.Contains(got, "[eslint] FINDINGS (30 problems: 0 errors, 30 warnings in 1 file)") ||
		!strings.Contains(got, "src/app.js") ||
		!strings.Contains(got, "1:1 warning [no-console] Unexpected console statement") {
		t.Fatalf("warning-only findings not preserved: %q", got)
	}
}

func TestTryCompactEslintStylishFailsOpen(t *testing.T) {
	t.Parallel()

	base := strings.Join([]string{
		"",
		"src/app.js",
		"  1:1   warning  Unexpected console statement         no-console",
		"  1:20  error    Expected '===' and instead saw '=='  eqeqeq",
		"",
		"✖ 2 problems (1 error, 1 warning)",
		"",
	}, "\n")

	tests := []struct {
		name   string
		argv   []string
		stdout string
	}{
		{
			name:   "explicit json formatter",
			argv:   []string{"eslint", "--format", "json", "src"},
			stdout: base,
		},
		{
			name:   "mismatched summary",
			argv:   []string{"eslint", "src"},
			stdout: strings.Replace(base, "✖ 2 problems (1 error, 1 warning)", "✖ 3 problems (2 errors, 1 warning)", 1),
		},
		{
			name: "codeframe formatter",
			argv: []string{"eslint", "src"},
			stdout: strings.Join([]string{
				"src/app.js",
				"  1:1  error  Unexpected console statement  no-console",
				"  1 | console.log('x')",
				"    | ^^^^^^^^^^^",
				"✖ 1 problem (1 error, 0 warnings)",
				"",
			}, "\n"),
		},
		{
			name:   "ansi color",
			argv:   []string{"eslint", "src"},
			stdout: "\x1b[31msrc/app.js\x1b[0m\n  1:1  error  bad  no-console\n\n✖ 1 problem (1 error, 0 warnings)\n",
		},
		{
			name:   "tiny non shrinking",
			argv:   []string{"eslint", "src"},
			stdout: "src/a.js\n  1:1  error  bad  x\n\n✖ 1 problem (1 error, 0 warnings)\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if out, ok := TryCompactEslintStylish(tt.argv, []byte(tt.stdout)); ok {
				t.Fatalf("expected fail-open, got %q", out)
			}
		})
	}
}

func TestEslintStylishParserHelpers(t *testing.T) {
	t.Parallel()

	if !isEslintStylishArgv([]string{"eslint", "-f=stylish", "src"}) {
		t.Fatal("explicit -f=stylish should be accepted")
	}
	if !isEslintStylishArgv([]string{"pnpm", "exec", "eslint", "--format", "stylish", "src"}) {
		t.Fatal("pnpm exec eslint --format stylish should be accepted")
	}
	for _, argv := range [][]string{
		{"jest", "--format", "stylish"},
		{"eslint", "--format", "json"},
		{"eslint", "-f", "unix"},
		{"eslint", "--format"},
	} {
		if isEslintStylishArgv(argv) {
			t.Fatalf("argv should not be stylish-safe: %v", argv)
		}
	}

	if line, col, ok := parseEslintStylishLocation("12:7"); !ok || line != 12 || col != 7 {
		t.Fatalf("parse location = %d:%d ok=%v", line, col, ok)
	}
	for _, loc := range []string{"0:1", "1:0", "1", "a:1", "1:b"} {
		if _, _, ok := parseEslintStylishLocation(loc); ok {
			t.Fatalf("location should fail: %q", loc)
		}
	}

	if total, errors, warnings, ok := parseEslintStylishSummary("✖ 1 problem (1 error, 0 warnings)"); !ok || total != 1 || errors != 1 || warnings != 0 {
		t.Fatalf("singular summary parsed wrong: total=%d errors=%d warnings=%d ok=%v", total, errors, warnings, ok)
	}
	for _, summary := range []string{
		"✖ 2 problem (1 error, 1 warning)",
		"✖ 2 problems (2 errors, 1 warning)",
		"2 problems (1 error, 1 warning)",
	} {
		if _, _, _, ok := parseEslintStylishSummary(summary); ok {
			t.Fatalf("summary should fail: %q", summary)
		}
	}

	if errors, warnings, ok := parseEslintStylishFixableLine("1 error and 2 warnings potentially fixable with the `--fix` option."); !ok || errors != 1 || warnings != 2 {
		t.Fatalf("fixable parsed wrong: errors=%d warnings=%d ok=%v", errors, warnings, ok)
	}
	for _, line := range []string{
		"1 errors and 0 warnings potentially fixable with the `--fix` option.",
		"1 error and 0 warnings fixable with the `--fix` option.",
		"x error and 0 warnings potentially fixable with the `--fix` option.",
	} {
		if _, _, ok := parseEslintStylishFixableLine(line); ok {
			t.Fatalf("fixable line should fail: %q", line)
		}
	}

	for _, path := range []string{"src/app.mjs", "src/App.vue", "src/Page.astro"} {
		if !eslintStylishFileLineOK(path) {
			t.Fatalf("expected ESLint file path accepted: %q", path)
		}
	}
	for _, path := range []string{"const x = 1", "src/app.txt", "src/app.js:1:1"} {
		if eslintStylishFileLineOK(path) {
			t.Fatalf("expected ESLint file path rejected: %q", path)
		}
	}

	if !eslintRuleIDOK("@typescript-eslint/no-unused-vars") {
		t.Fatal("scoped ESLint rule ID should be accepted")
	}
	for _, rule := range []string{"", "bad rule", "bad:rule"} {
		if eslintRuleIDOK(rule) {
			t.Fatalf("rule ID should fail: %q", rule)
		}
	}

	if finding, ok := parseEslintStylishFindingLine("3:9 error Unexpected any @typescript-eslint/no-explicit-any", "src/app.ts"); !ok ||
		finding.file != "src/app.ts" || finding.line != 3 || finding.column != 9 || finding.rule != "@typescript-eslint/no-explicit-any" {
		t.Fatalf("finding parsed wrong: %+v ok=%v", finding, ok)
	}
	for _, row := range []struct {
		line string
		file string
	}{
		{line: "3:9 info Unexpected any rule", file: "src/app.ts"},
		{line: "3:9 error Unexpected any bad:rule", file: "src/app.ts"},
		{line: "3:9 error import value no-restricted-syntax", file: "src/app.ts"},
		{line: "3:9 error Unexpected any rule", file: ""},
	} {
		if finding, ok := parseEslintStylishFindingLine(row.line, row.file); ok {
			t.Fatalf("finding should fail: %+v from row=%+v", finding, row)
		}
	}
}

func eslintStylishFixture(path string, repeats int, fixable bool) string {
	var b strings.Builder
	for range repeats {
		b.WriteString("\n")
		b.WriteString(path)
		b.WriteString("\n")
		b.WriteString("  2:1   warning  Unexpected console statement         no-console\n")
		b.WriteString("  2:20  error    Expected '===' and instead saw '=='  eqeqeq\n")
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "✖ %d problems (%d errors, %d warnings)\n", repeats*2, repeats, repeats)
	if fixable {
		b.WriteString("  1 error and 0 warnings potentially fixable with the `--fix` option.\n")
	}
	return b.String()
}

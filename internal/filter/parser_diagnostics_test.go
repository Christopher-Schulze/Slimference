package filter

import (
	"strings"
	"testing"
)

func TestParseTypeScriptDiagnostics(t *testing.T) {
	t.Parallel()
	stdout := paddedDiagnosticOutput(`src/App.tsx(12,7): error TS2322: Type 'string' is not assignable to type 'number'.
src/routes/+page.ts:3:11 - error TS2304: Cannot find name 'loadData'.
Found 2 errors in 2 files.`)
	got, hadFailures, ok := parseTypeScriptDiagnostics(stdout)
	if !ok || !hadFailures {
		t.Fatal("expected TypeScript diagnostics")
	}
	for _, want := range []string{"[typescript] FAILED", "TS2322", "Found 2 errors"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestParseZigDiagnostics(t *testing.T) {
	t.Parallel()
	stdout := paddedDiagnosticOutput(`src/main.zig:8:17: error: expected type 'u8', found 'u16'
    const x: u8 = y;
                ^
error: the following command failed with 1 compilation errors:`)
	got, hadFailures, ok := parseZigDiagnostics(stdout)
	if !ok || !hadFailures {
		t.Fatal("expected Zig diagnostics")
	}
	if !strings.Contains(got, "src/main.zig:8:17") || !strings.Contains(got, "compilation errors") {
		t.Fatalf("missing Zig details: %q", got)
	}
}

func TestParseSvelteDiagnostics(t *testing.T) {
	t.Parallel()
	stdout := paddedDiagnosticOutput(`src/routes/+page.svelte:14:5
Error: Type 'string' is not assignable to type 'number'. (ts)

====================================
svelte-check found 1 error and 0 warnings in 1 file`)
	got, hadFailures, ok := parseSvelteDiagnostics(stdout)
	if !ok || !hadFailures {
		t.Fatal("expected Svelte diagnostics")
	}
	if !strings.Contains(got, "svelte-check found 1 error") {
		t.Fatalf("missing Svelte summary: %q", got)
	}
}

func TestParseFrontendDiagnostics(t *testing.T) {
	t.Parallel()
	stdout := paddedDiagnosticOutput(`src/App.test.tsx:22:9: error: expect(received).toBe(expected)
Error: page.goto: net::ERR_CONNECTION_REFUSED at http://127.0.0.1:3000
Test failed. 1 failed, 2 passed.`)
	got, hadFailures, ok := parseFrontendDiagnostics(stdout)
	if !ok || !hadFailures {
		t.Fatal("expected frontend diagnostics")
	}
	for _, want := range []string{"[frontend] FAILED", "App.test.tsx", "ERR_CONNECTION_REFUSED", "1 failed"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestParsePythonDiagnostics(t *testing.T) {
	t.Parallel()
	stdout := paddedDiagnosticOutput(`src/app.py:3:1: F401 'os' imported but unused
src/main.py:10: error: Argument 1 has incompatible type "str"; expected "int"  [arg-type]
/Users/me/project/pkg/service.py:22:7 - error: "missing_name" is not defined
FAILED tests/test_service.py::test_handles_missing_name - AssertionError: expected 1 got 2
Found 2 errors in 2 files.`)
	got, hadFailures, ok := parsePythonDiagnostics(stdout)
	if !ok || !hadFailures {
		t.Fatal("expected Python diagnostics")
	}
	for _, want := range []string{"[python] FAILED", "F401", "[arg-type]", "missing_name", "test_handles_missing_name", "Found 2 errors"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestParseSQLDiagnostics(t *testing.T) {
	t.Parallel()
	stdout := paddedDiagnosticOutput(`== [migrations/001_init.sql] FAIL
L:   4 | P:  13 | LT01 | Expected single whitespace between naked identifier and comma.
All Finished!`)
	got, hadFailures, ok := parseSQLDiagnostics(stdout)
	if !ok || !hadFailures {
		t.Fatal("expected SQL diagnostics")
	}
	if !strings.Contains(got, "LT01") || !strings.Contains(got, "FAIL") {
		t.Fatalf("missing SQL details: %q", got)
	}
}

func TestParseSQLClientDiagnostics(t *testing.T) {
	t.Parallel()
	stdout := diagnosticOutputWithNeutralPadding(`ERROR 1064 (42000) at line 1: You have an error in your SQL syntax
Parse error near line 2: no such table: users
Prisma schema loaded from prisma/schema.prisma
Error: P3006 Migration failed to apply cleanly to the shadow database`)
	got, hadFailures, ok := parseSQLDiagnostics(stdout)
	if !ok || !hadFailures {
		t.Fatal("expected SQL client diagnostics")
	}
	for _, want := range []string{"[sql] FAILED", "ERROR 1064", "no such table", "P3006"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestParseMarkdownDiagnostics(t *testing.T) {
	t.Parallel()
	stdout := paddedDiagnosticOutput(`docs/readme.md:12: MD013/line-length Line length [Expected: 80; Actual: 142]
docs/guide.md:4: MD041/first-line-heading First line in a file should be a top-level heading
2 problems`)
	got, hadFailures, ok := parseMarkdownDiagnostics(stdout)
	if !ok || !hadFailures {
		t.Fatal("expected Markdown diagnostics")
	}
	if !strings.Contains(got, "MD013") || !strings.Contains(got, "2 problems") {
		t.Fatalf("missing Markdown details: %q", got)
	}
}

func TestParsePracticalEcosystemDiagnostics(t *testing.T) {
	t.Parallel()
	stdout := paddedDiagnosticOutput(`lib/main.dart:7:10: Error: The getter 'foo' isn't defined for the type 'App'.
sources/App.swift:9:13: warning: initialization of immutable value was never used
2 issues found.`)
	got, hadFailures, ok := parsePracticalEcosystemDiagnostics(stdout)
	if !ok || !hadFailures {
		t.Fatal("expected practical ecosystem diagnostics")
	}
	if !strings.Contains(got, "lib/main.dart") || !strings.Contains(got, "sources/App.swift") {
		t.Fatalf("missing ecosystem details: %q", got)
	}
}

func TestParseDiagnosticRows_Success(t *testing.T) {
	t.Parallel()
	got, hadFailures, ok := parseDiagnosticRows("typescript", "Build succeeded.\n0 errors\n"+strings.Repeat("progress\n", 20))
	if !ok || hadFailures {
		t.Fatalf("expected success compaction, got=%q hadFailures=%v ok=%v", got, hadFailures, ok)
	}
	if !strings.Contains(got, "[typescript] ok") {
		t.Fatalf("unexpected success output: %q", got)
	}
}

func TestParseDiagnosticRows_NoMatchAndTooShort(t *testing.T) {
	t.Parallel()
	if got, _, ok := parseDiagnosticRows("typescript", "plain output\n"); ok {
		t.Fatalf("unexpected parser match: %q", got)
	}
	if got, _, ok := parseDiagnosticRows("typescript", "x.ts:1:1: error TS1: x\n"); ok {
		t.Fatalf("short output should not compact: %q", got)
	}
	if got, _, ok := parseDiagnosticRows("typescript", "0 errors\n"); ok {
		t.Fatalf("short success should not compact: %q", got)
	}
	if got, _, ok := parseDiagnosticRows("very-long-label", "build ok"); ok {
		t.Fatalf("short build success should not compact: %q", got)
	}
	if got, _, ok := parseDiagnosticRows("typescript", "plain output\nBuild finished without diagnostics\n"); ok {
		t.Fatalf("non-diagnostic output should not compact: %q", got)
	}
	if isDiagnosticSummary(strings.Repeat("error ", 60)) {
		t.Fatal("very long summary-like lines should be ignored")
	}
}

func TestCommandMatchesAnyWrappers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		argv []string
		want bool
	}{
		{[]string{"tsc"}, true},
		{[]string{"npx", "-y", "tsc"}, true},
		{[]string{"pnpm", "exec", "tsc"}, true},
		{[]string{"npm", "exec", "--", "tsc"}, true},
		{[]string{"npm", "exec", "--"}, false},
		{[]string{"yarn", "svelte-check"}, true},
		{[]string{"bun", "x", "markdownlint"}, true},
		{[]string{"bun", "run", "markdownlint"}, true},
		{[]string{"node", "script.js"}, false},
		{nil, false},
	}
	for _, tt := range tests {
		got := commandMatchesAny(tt.argv, "tsc", "svelte-check", "markdownlint")
		if got != tt.want {
			t.Fatalf("commandMatchesAny(%v)=%v want %v", tt.argv, got, tt.want)
		}
	}
}

func TestFrontendDiagnosticArgv(t *testing.T) {
	t.Parallel()
	tests := []struct {
		argv []string
		want bool
	}{
		{[]string{"next", "build"}, true},
		{[]string{"pnpm", "exec", "vite", "build"}, true},
		{[]string{"yarn", "playwright", "test"}, true},
		{[]string{"bun", "run", "vitest"}, true},
		{[]string{"bun", "test"}, true},
		{[]string{"bun", "build"}, true},
		{[]string{"bun", "install"}, false},
		{[]string{"node", "server.js"}, false},
		{[]string{}, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(strings.Join(tt.argv, " "), func(t *testing.T) {
			t.Parallel()
			if got := isFrontendDiagnosticArgv(tt.argv); got != tt.want {
				t.Fatalf("isFrontendDiagnosticArgv(%v)=%v want %v", tt.argv, got, tt.want)
			}
		})
	}
}

func TestPythonDiagnosticArgv(t *testing.T) {
	t.Parallel()
	tests := []struct {
		argv []string
		want bool
	}{
		{[]string{"ruff", "check", "."}, true},
		{[]string{"python", "-m", "ruff", "check", "."}, true},
		{[]string{"pylint", "pkg"}, true},
		{[]string{"flake8", "pkg"}, true},
		{[]string{"mypy", "src"}, true},
		{[]string{"python3", "-m", "mypy", "src"}, true},
		{[]string{"pyright", "src"}, true},
		{[]string{"basedpyright", "src"}, true},
		{[]string{"pytest", "-q"}, true},
		{[]string{"npx", "pytest", "-q"}, true},
		{[]string{"npx", "-y"}, false},
		{[]string{"pnpm", "exec", "pytest", "-q"}, true},
		{[]string{"yarn", "py.test", "-q"}, true},
		{[]string{"python3", "-m", "pytest"}, true},
		{[]string{"python3", "-m", "notpytest"}, false},
		{[]string{"uv", "run", "pytest"}, true},
		{[]string{"poetry", "run", "python", "-m", "pytest"}, true},
		{[]string{"python", "-m", "unittest"}, true},
		{[]string{"python", "script.py"}, false},
		{[]string{"pip", "install", "x"}, false},
		{[]string{}, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(strings.Join(tt.argv, " "), func(t *testing.T) {
			t.Parallel()
			if got := isPythonDiagnosticArgv(tt.argv); got != tt.want {
				t.Fatalf("isPythonDiagnosticArgv(%v)=%v want %v", tt.argv, got, tt.want)
			}
		})
	}
}

func TestSQLDiagnosticArgv(t *testing.T) {
	t.Parallel()
	tests := []struct {
		argv []string
		want bool
	}{
		{[]string{"sqlfluff", "lint", "."}, true},
		{[]string{"python", "-m", "sqlfluff", "lint", "."}, true},
		{[]string{"psql", "-c", "select 1"}, true},
		{[]string{"sqlite3", "db.sqlite", "select 1"}, true},
		{[]string{"mysql", "-e", "select 1"}, true},
		{[]string{"mariadb", "-e", "select 1"}, true},
		{[]string{"npm", "exec", "--", "prisma", "migrate", "dev"}, true},
		{[]string{"pnpm", "exec", "prisma", "migrate", "dev"}, true},
		{[]string{"npm", "x", "drizzle-kit", "push"}, true},
		{[]string{"bun", "x", "drizzle-kit", "push"}, true},
		{[]string{"python", "script.py"}, false},
		{[]string{}, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(strings.Join(tt.argv, " "), func(t *testing.T) {
			t.Parallel()
			if got := isSQLDiagnosticArgv(tt.argv); got != tt.want {
				t.Fatalf("isSQLDiagnosticArgv(%v)=%v want %v", tt.argv, got, tt.want)
			}
		})
	}
}

func TestT124_ParseFailuresDispatchesNewParsers(t *testing.T) {
	t.Parallel()
	stdout := paddedDiagnosticOutput(`src/App.tsx(12,7): error TS2322: Type mismatch.
Found 1 error.`)
	got, ok := ParseFailures([]string{"pnpm", "exec", "tsc", "--noEmit"}, stdout)
	if !ok {
		t.Fatal("expected tsc dispatch")
	}
	if !strings.Contains(got, "[typescript] FAILED") {
		t.Fatalf("unexpected dispatch output: %q", got)
	}

	frontendStdout := paddedDiagnosticOutput(`src/App.test.tsx:22:9: error: expect(received).toBe(expected)
1 failed`)
	got, ok = ParseFailures([]string{"bun", "test"}, frontendStdout)
	if !ok {
		t.Fatal("expected bun test dispatch")
	}
	if !strings.Contains(got, "[frontend] FAILED") {
		t.Fatalf("unexpected frontend dispatch output: %q", got)
	}

	pythonStdout := paddedDiagnosticOutput(`src/app.py:3:1: F401 'os' imported but unused
Found 1 error.`)
	got, ok = ParseFailures([]string{"ruff", "check", "."}, pythonStdout)
	if !ok {
		t.Fatal("expected ruff dispatch")
	}
	if !strings.Contains(got, "[python] FAILED") {
		t.Fatalf("unexpected python dispatch output: %q", got)
	}

	sqlStdout := diagnosticOutputWithNeutralPadding(`ERROR 1064 (42000) at line 1: You have an error in your SQL syntax`)
	got, ok = ParseFailures([]string{"mysql", "-e", "select * from"}, sqlStdout)
	if !ok {
		t.Fatal("expected mysql dispatch")
	}
	if !strings.Contains(got, "[sql] FAILED") {
		t.Fatalf("unexpected sql dispatch output: %q", got)
	}
}

func TestPythonDiagnosticsIntegratedIntoLintAndTestFilters(t *testing.T) {
	t.Parallel()
	lintStdout := diagnosticOutputWithNeutralPadding(`src/app.py:3:1: F401 'os' imported but unused
Found 1 error.`)
	lintOut, ok := TryCompactLintOutput([]string{"ruff", "check", "."}, []byte(lintStdout))
	if !ok {
		t.Fatal("expected ruff lint output to compact through shared diagnostics")
	}
	if !strings.Contains(string(lintOut), "[python] FAILED") || strings.Contains(string(lintOut), "neutral padding") {
		t.Fatalf("unexpected lint compact output: %q", lintOut)
	}

	testStdout := diagnosticOutputWithNeutralPadding(`FAILED tests/test_service.py::test_handles_missing_name - AssertionError: expected 1 got 2
1 failed, 3 passed.`)
	testOut, ok := TryCompactTestOutput([]string{"pytest", "-q"}, []byte(testStdout))
	if !ok {
		t.Fatal("expected pytest output to compact through existing test fallback")
	}
	if !strings.Contains(string(testOut), "[pytest] FAILED") || strings.Contains(string(testOut), "neutral padding") {
		t.Fatalf("unexpected test compact output: %q", testOut)
	}
}

func paddedDiagnosticOutput(core string) string {
	var sb strings.Builder
	sb.WriteString(core)
	sb.WriteByte('\n')
	for i := 0; i < 20; i++ {
		sb.WriteString("progress line that should be removed from compact diagnostics\n")
	}
	return sb.String()
}

func diagnosticOutputWithNeutralPadding(core string) string {
	var sb strings.Builder
	sb.WriteString(core)
	sb.WriteByte('\n')
	for i := 0; i < 20; i++ {
		sb.WriteString("neutral padding line that should be removed from compact output\n")
	}
	return sb.String()
}

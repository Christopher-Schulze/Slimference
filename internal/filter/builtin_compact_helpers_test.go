package filter

import (
	"strings"
	"testing"
)

// TestExtractTestFailures covers the main branches of extractTestFailures.
func TestExtractTestFailures(t *testing.T) {
	t.Parallel()

	// All-pass: "all tests passed" → ok (no "failed"/"fail" in string)
	passOutput := "all tests passed\nTests ran in 1.2s\n"
	out, ok := extractTestFailures(passOutput, "pytest")
	if !ok {
		t.Fatalf("expected compact for all-pass, got false")
	}
	if !strings.Contains(out, "[pytest] ok") {
		t.Errorf("all-pass: want ok label, got %q", out)
	}

	// PASS line (go test style)
	goPassOutput := "PASS\nok  \tgithub.com/foo/bar\t0.123s\n"
	out2, ok2 := extractTestFailures(goPassOutput, "go test")
	if !ok2 {
		t.Fatalf("expected compact for go pass, got false")
	}
	if !strings.Contains(out2, "[go test] ok") {
		t.Errorf("go pass: want ok label, got %q", out2)
	}

	// All-pass with timing summary line → includes timing (needs "ms" or "0." or "second" or "s ")
	timingOutput := "30 passed in 0.345s\n"
	out3, ok3 := extractTestFailures(timingOutput, "pytest")
	if !ok3 {
		t.Fatalf("expected compact for timing output")
	}
	// Should contain the timing line
	if !strings.Contains(out3, "0.345s") {
		t.Errorf("timing pass: want timing in output, got %q", out3)
	}

	// Failure: "--- FAIL: TestFoo" (go test style)
	// Generate enough failure lines so the compact is shorter than the input
	var failLines strings.Builder
	failLines.WriteString("running tests...\n")
	for i := 0; i < 5; i++ {
		failLines.WriteString("--- FAIL: TestSomething (0.01s)\n")
		failLines.WriteString("    Error: assertion failed at line 42\n")
		failLines.WriteString("    Expected: foo\n")
		failLines.WriteString("    Got:      bar\n")
		failLines.WriteString("    Stack trace: frame 1\n")
		failLines.WriteString("    Stack trace: frame 2\n")
	}
	failLines.WriteString("FAIL\tgithub.com/foo/bar\t0.05s\n")
	out4, ok4 := extractTestFailures(failLines.String(), "go test")
	if !ok4 {
		t.Fatalf("expected compact for failures")
	}
	if !strings.Contains(out4, "[go test] FAILED") {
		t.Errorf("failure: want FAILED header, got %q", out4)
	}
	if !strings.Contains(out4, "Error: assertion failed at line 42") {
		t.Errorf("failure: want assertion detail preserved, got %q", out4)
	}

	// Failure: "FAILED tests/test_foo.py::test_bar" (pytest style)
	pytestFail := strings.Repeat("FAILED tests/test_foo.py::test_bar\nsome long error context here\n", 5)
	out5, ok5 := extractTestFailures(pytestFail, "pytest")
	if !ok5 {
		t.Fatalf("expected compact for pytest failure")
	}
	if !strings.Contains(out5, "FAILED") {
		t.Errorf("pytest fail: want FAILED in output, got %q", out5)
	}

	// Jest style: "● test description"
	jestFail := strings.Repeat("● my test description\n  some long error output\n  more context\n", 5)
	out6, ok6 := extractTestFailures(jestFail, "jest")
	if !ok6 {
		t.Fatalf("expected compact for jest failure")
	}
	if !strings.Contains(out6, "[jest] FAILED") {
		t.Errorf("jest fail: want FAILED header, got %q", out6)
	}

	// No recognizable failure lines → return "", false
	unknownOutput := "some generic output\nno test patterns here\n"
	out7, ok7 := extractTestFailures(unknownOutput, "tool")
	if ok7 {
		t.Errorf("unknown output: want false, got true with %q", out7)
	}
}

// TestTruncateLintViolations covers the main branches of truncateLintViolations.
func TestTruncateLintViolations(t *testing.T) {
	t.Parallel()

	// Short output (≤60 lines) → no truncation
	var shortSb strings.Builder
	for i := 0; i < 10; i++ {
		shortSb.WriteString("src/app.ts:5:1: error no-unused-vars\n")
	}
	out, ok := truncateLintViolations(shortSb.String(), "eslint")
	if ok {
		t.Errorf("short output: want false (no truncation), got true with %q", out)
	}

	// Long output (>60 lines) → truncate to 60 + "+N more" suffix
	var longSb strings.Builder
	for i := 0; i < 80; i++ {
		longSb.WriteString("src/app.ts:5:1: error no-unused-vars: variable 'x' is defined but never used\n")
	}
	inputStr := longSb.String()
	out2, ok2 := truncateLintViolations(inputStr, "eslint")
	if !ok2 {
		t.Fatalf("long output: want true (truncation), got false")
	}
	if !strings.Contains(out2, "+20 more") {
		t.Errorf("want '+20 more', got %q", out2)
	}
	if len(out2) >= len(inputStr) {
		t.Errorf("truncated output should be shorter: %d vs %d", len(out2), len(inputStr))
	}
}

// TestIsSingleBinarySubcmdArgvExtra covers additional branches of isSingleBinarySubcmdArgv.
func TestIsSingleBinarySubcmdArgvExtra(t *testing.T) {
	t.Parallel()
	// Direct binary with no required subcommand matches
	if !isSingleBinarySubcmdArgv([]string{"golangci-lint", "run"}, "golangci-lint", "") {
		t.Error("direct binary without subcommand constraint should match")
	}
	// Direct binary with required subcommand that matches
	if !isSingleBinarySubcmdArgv([]string{"buf", "lint"}, "buf", "lint") {
		t.Error("direct binary with matching subcommand should match")
	}
	// Direct binary with required subcommand that does not match
	if isSingleBinarySubcmdArgv([]string{"buf", "build"}, "buf", "lint") {
		t.Error("direct binary with wrong subcommand should not match")
	}
	// Wrong binary name → false
	if isSingleBinarySubcmdArgv([]string{"other-tool", "lint"}, "buf", "lint") {
		t.Error("wrong binary should not match")
	}
	// npx wrapper → resolves inner binary
	if !isSingleBinarySubcmdArgv([]string{"npx", "golangci-lint", "run"}, "golangci-lint", "") {
		t.Error("npx wrapper should match")
	}
	// pnpm exec wrapper
	if !isSingleBinarySubcmdArgv([]string{"pnpm", "exec", "buf", "lint"}, "buf", "lint") {
		t.Error("pnpm exec wrapper should match")
	}
	// yarn wrapper
	if !isSingleBinarySubcmdArgv([]string{"yarn", "buf", "lint"}, "buf", "lint") {
		t.Error("yarn wrapper should match")
	}
}

// TestIsSingleBinarySubcmdDirect_npxNoArgs covers the !ok || len(rest)<1 guard (line 22-24):
// when npx has no further args, npxArgvSuffix returns !ok → return false.
func TestIsSingleBinarySubcmdDirect_npxNoArgs(t *testing.T) {
	t.Parallel()
	// ["npx"] alone - npxArgvSuffix finds no binary after the npx token
	got := isSingleBinarySubcmdDirect([]string{"npx"}, "buf", "lint")
	if got {
		t.Error("npx with no args: want false, got true")
	}
}

// TestExtractTestFailures_okSingleTab covers the "ok \t" (single space+tab) prefix path (line 114-116).
func TestExtractTestFailures_okSingleTab(t *testing.T) {
	t.Parallel()
	// "ok \t" has single space before tab — distinct from "ok  \t" (double space)
	input := "ok \tgithub.com/foo/bar\t0.123s\n"
	out, ok := extractTestFailures(input, "go test")
	if !ok {
		t.Fatalf("ok-single-tab: expected compact, got false")
	}
	if !strings.Contains(out, "[go test] ok") {
		t.Errorf("ok-single-tab: want ok label, got %q", out)
	}
}

// TestExtractTestFailures_compactNotShorter covers the len(out) >= len(s) guard (line 158-160):
// single short failure line where the compact "[label] FAILED\nLINE\n" is longer than original.
func TestExtractTestFailures_compactNotShorter(t *testing.T) {
	t.Parallel()
	// "FAILED x::test_y\n" = 18 chars; compact "[pytest] FAILED\nFAILED x::test_y\n" = 34 chars
	input := "FAILED x::test_y\n"
	_, ok := extractTestFailures(input, "pytest")
	if ok {
		t.Error("compact >= original: want false, got true")
	}
}

// TestTruncateLintViolations_compactNotShorter covers the len(out) >= len(s) guard (line 181-183):
// 61 very short non-empty lines where the truncated output + "+1 more" suffix exceeds original.
func TestTruncateLintViolations_compactNotShorter(t *testing.T) {
	t.Parallel()
	// 61 lines of "x" separated by newlines → original = 122 chars;
	// truncated 60 + suffix "\n... +1 more violation(s)\n" ≈ 145 chars > 122.
	input := strings.Repeat("x\n", 61)
	_, ok := truncateLintViolations(input, "eslint")
	if ok {
		t.Error("compact >= original for short lines: want false, got true")
	}
}

// TestTruncateLintViolations_KeepsErrorsOverWarnings proves error-severity rows
// survive truncation even when they sit past the head budget behind warnings.
func TestTruncateLintViolations_KeepsErrorsOverWarnings(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 65; i++ {
		sb.WriteString("src/a.go:1:1: warning: unused import (govet)\n")
	}
	sb.WriteString("src/b.go:9:9: error: undefined: criticalSymbol (typecheck)\n")
	out, ok := truncateLintViolations(sb.String(), "golangci-lint")
	if !ok {
		t.Fatalf("expected truncation")
	}
	if !strings.Contains(out, "undefined: criticalSymbol") {
		t.Fatalf("error violation past head budget was dropped: %q", out[:min(len(out), 240)])
	}
}

// TestExtractBuildErrors covers edge cases in extractBuildErrors.
func TestExtractBuildErrors(t *testing.T) {
	t.Parallel()

	// Build success → ok
	successOutput := "Build successful. Compiled with 0 errors."
	out, ok := extractBuildErrors(successOutput, "go build")
	if !ok {
		t.Fatalf("success: expected compact, got false")
	}
	if out != "[go build] ok\n" {
		t.Errorf("success: want '[go build] ok\\n', got %q", out)
	}

	// No error lines found → false
	genericOutput := "Compiling...\nLinking...\nDone.\n"
	out2, ok2 := extractBuildErrors(genericOutput, "go build")
	if ok2 {
		t.Errorf("no error lines: want false, got true with %q", out2)
	}

	// Error lines found but compact output not shorter → false
	// Single short error line
	shortErr := "error: undefined\n"
	out3, ok3 := extractBuildErrors(shortErr, "go build")
	if ok3 {
		t.Errorf("short error: want false (not shorter), got true with %q", out3)
	}
}

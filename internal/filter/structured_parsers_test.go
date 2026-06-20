package filter

import (
	"strings"
	"testing"
)

func TestParseGoErrors_SingleError(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("# example.com/pkg\n")
	sb.WriteString("./main.go:10:3: undefined: foo\n")
	for i := 0; i < 10; i++ {
		sb.WriteString("padding line with enough content to make output longer\n")
	}
	compact, hadFailures, ok := parseGoErrors(sb.String())
	if !ok || !hadFailures {
		t.Fatal("expected parse match with failures")
	}
	if !strings.Contains(compact, "main.go:10:3") {
		t.Fatalf("missing error line: %q", compact)
	}
}

func TestParseGoErrors_MultipleErrors(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("# example.com/pkg\n")
	sb.WriteString("./main.go:10:3: undefined: foo\n")
	sb.WriteString("./main.go:15:5: cannot use x as int\n")
	sb.WriteString("./util.go:22:1: missing return\n")
	for i := 0; i < 10; i++ {
		sb.WriteString("padding line that makes output significantly longer\n")
	}
	compact, hadFailures, ok := parseGoErrors(sb.String())
	if !ok || !hadFailures {
		t.Fatal("expected failures")
	}
	if !strings.Contains(compact, "main.go:10") || !strings.Contains(compact, "util.go:22") {
		t.Fatalf("missing errors: %q", compact)
	}
}

func TestParseGoErrors_DeduplicatesConsecutiveDiagnostics(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 80; i++ {
		sb.WriteString("internal/app/app.go:10:5: fmt.Printf call needs 1 arg but has 2 args\n")
	}
	compact, hadFailures, ok := parseGoErrorsForArgv([]string{"go", "vet", "./..."}, sb.String())
	if !ok || !hadFailures {
		t.Fatal("expected go vet diagnostic compaction")
	}
	if !strings.Contains(compact, "[go vet] FAILED") ||
		!strings.Contains(compact, "fmt.Printf call needs 1 arg but has 2 args [x80]") {
		t.Fatalf("missing deduped go vet diagnostic: %q", compact)
	}
	if strings.Count(compact, "fmt.Printf call needs") != 1 {
		t.Fatalf("duplicate diagnostic was not folded: %q", compact)
	}
}

func TestParseGoErrors_LabelsGoVetAfterGlobalOptions(t *testing.T) {
	t.Parallel()
	stdout := "internal/app/app.go:10:5: fmt.Printf call needs 1 arg but has 2 args\n" +
		strings.Repeat("padding line that makes output significantly longer\n", 10)
	compact, ok := ParseFailures([]string{"go", "-C=/repo", "vet", "./..."}, stdout)
	if !ok {
		t.Fatal("expected go vet global-option diagnostic compaction")
	}
	if !strings.Contains(compact, "[go vet] FAILED") {
		t.Fatalf("wrong go diagnostic label: %q", compact)
	}
}

func TestGoDiagnosticCommandLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		argv []string
		want string
	}{
		{[]string{"go", "build", "./..."}, "go build"},
		{[]string{"go", "-C", "/repo", "vet", "./..."}, "go vet"},
		{[]string{"go", "-mod=mod", "test", "./..."}, "go test"},
		{[]string{"python", "build"}, "go build"},
	}
	for _, tt := range tests {
		if got := goDiagnosticCommandLabel(tt.argv); got != tt.want {
			t.Fatalf("goDiagnosticCommandLabel(%v)=%q want %q", tt.argv, got, tt.want)
		}
	}
}

func TestParseGoErrors_Success(t *testing.T) {
	t.Parallel()
	result, hadFailures, ok := parseGoErrors("")
	if ok {
		t.Fatal("empty should not match")
	}
	_ = result
	_ = hadFailures
}

func TestParseGoErrors_BuildSucceeded(t *testing.T) {
	t.Parallel()
	compact, hadFailures, ok := parseGoErrors("Build succeeded.\n0 Error(s)\n")
	if !ok {
		t.Fatal("should match")
	}
	if hadFailures {
		t.Fatal("should not have failures")
	}
	if !strings.Contains(compact, "ok") {
		t.Fatalf("expected ok: %q", compact)
	}
}

func TestParseGoErrors_NoErrors(t *testing.T) {
	t.Parallel()
	compact, _, ok := parseGoErrors("some random output\nno errors here\n")
	if ok {
		t.Fatalf("no go errors, should not match: %q", compact)
	}
}

func TestParseGoErrors_TestFailure(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("=== RUN   TestFoo\n")
	sb.WriteString("--- FAIL: TestFoo (0.00s)\n")
	sb.WriteString("    main_test.go:12: expected 1 got 2\n")
	sb.WriteString("FAIL\n")
	for i := 0; i < 10; i++ {
		sb.WriteString("padding line that makes output significantly longer\n")
	}
	compact, hadFailures, ok := parseGoErrors(sb.String())
	if !ok || !hadFailures {
		t.Fatal("expected test failure")
	}
	if !strings.Contains(compact, "FAIL: TestFoo") {
		t.Fatalf("missing test failure: %q", compact)
	}
}

func TestParseGoErrors_Panic(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("=== RUN   TestBar\n")
	sb.WriteString("panic: runtime error: index out of range [1] with length 1\n")
	sb.WriteString("goroutine 1 [running]:\n")
	for i := 0; i < 10; i++ {
		sb.WriteString("padding line that makes output significantly longer\n")
	}
	compact, hadFailures, ok := parseGoErrors(sb.String())
	if !ok || !hadFailures {
		t.Fatal("expected panic failure")
	}
	if !strings.Contains(compact, "panic:") {
		t.Fatalf("missing panic: %q", compact)
	}
}

func TestParseGoErrors_OutputNotShorter(t *testing.T) {
	t.Parallel()
	stdout := "./main.go:1:1: x\n"
	_, _, ok := parseGoErrors(stdout)
	if ok {
		t.Fatal("short output should not compact (not shorter)")
	}
}

func TestParseCargoErrors_SingleError(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("error[E0425]: cannot find value `foo` in this scope\n")
	sb.WriteString(" --> src/main.rs:4:5\n")
	sb.WriteString("  |\n")
	sb.WriteString("4 |     foo\n")
	sb.WriteString("  |     ^^^ not found in this scope\n")
	sb.WriteString("\n")
	for i := 0; i < 10; i++ {
		sb.WriteString("padding line that makes output significantly longer\n")
	}
	compact, hadFailures, ok := parseCargoErrors(sb.String())
	if !ok || !hadFailures {
		t.Fatal("expected cargo error")
	}
	if !strings.Contains(compact, "error[E0425]") {
		t.Fatalf("missing error: %q", compact)
	}
}

func TestParseCargoErrors_Panic(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("thread 'main' panicked at 'index out of bounds'\n")
	sb.WriteString("stack backtrace:\n")
	sb.WriteString("\n")
	for i := 0; i < 10; i++ {
		sb.WriteString("padding line that makes output significantly longer\n")
	}
	compact, hadFailures, ok := parseCargoErrors(sb.String())
	if !ok || !hadFailures {
		t.Fatal("expected cargo panic")
	}
	if !strings.Contains(compact, "panicked") {
		t.Fatalf("missing panic: %q", compact)
	}
}

func TestParseCargoErrors_OutputNotShorter(t *testing.T) {
	_, _, ok := parseCargoErrors("error: short\n")
	if ok {
		t.Fatal("short output should not compact")
	}
}

func TestParseCargoErrors_Warnings(t *testing.T) {
	t.Parallel()
	stdout := `warning: unused variable: ` + "`x`" + `
 --> src/main.rs:2:5
  |
2 |     let x = 1;
  |     ^^^^^^
`
	result, _, ok := parseCargoErrors(stdout)
	if ok {
		t.Fatalf("warnings only should not trigger failure extraction: %q", result)
	}
}

func TestParseCargoErrors_Success(t *testing.T) {
	t.Parallel()
	result, hadFailures, ok := parseCargoErrors("   Compiling foo v0.1.0\n    Finished dev [unoptimized + debuginfo] target(s)\n     Running unittests\nbuild succeeded\n")
	if !ok {
		t.Fatal("should match on success detection")
	}
	if hadFailures {
		t.Fatal("should not have failures")
	}
	_ = result
}

func TestParseCargoErrors_Empty(t *testing.T) {
	t.Parallel()
	_, _, ok := parseCargoErrors("")
	if ok {
		t.Fatal("empty should not match")
	}
}

func TestParseGccClangErrors_SingleError(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("main.c:10:5: error: 'foo' undeclared (first use in this function)\n")
	sb.WriteString("   10 |     foo;\n")
	sb.WriteString("      |     ^~~\n")
	for i := 0; i < 10; i++ {
		sb.WriteString("padding line that makes output significantly longer\n")
	}
	compact, hadFailures, ok := parseGccClangErrors(sb.String())
	if !ok || !hadFailures {
		t.Fatal("expected gcc error")
	}
	if !strings.Contains(compact, "main.c:10:5") {
		t.Fatalf("missing error: %q", compact)
	}
}

func TestParseGccClangErrors_FatalError(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("main.c:5:1: fatal error: stdio.h: No such file or directory\n")
	sb.WriteString("    5 | #include <stdio.h>\n")
	sb.WriteString("      |   ^~~~~~~~~~~~~\n")
	sb.WriteString("compilation terminated.\n")
	for i := 0; i < 10; i++ {
		sb.WriteString("padding line that makes output significantly longer\n")
	}
	compact, hadFailures, ok := parseGccClangErrors(sb.String())
	if !ok || !hadFailures {
		t.Fatal("expected fatal error")
	}
	if !strings.Contains(compact, "fatal error") {
		t.Fatalf("missing fatal: %q", compact)
	}
}

func TestParseGccClangErrors_Success(t *testing.T) {
	t.Parallel()
	result, hadFailures, ok := parseGccClangErrors("Build succeeded.\n0 errors\n")
	if !ok {
		t.Fatal("should match on success")
	}
	if hadFailures {
		t.Fatal("should not have failures")
	}
	_ = result
}

func TestParseGccClangErrors_NoErrors(t *testing.T) {
	t.Parallel()
	_, _, ok := parseGccClangErrors("some output\nno errors\n")
	if ok {
		t.Fatal("no errors should not match")
	}
}

func TestParseGccClangErrors_Empty(t *testing.T) {
	_, _, ok := parseGccClangErrors("")
	if ok {
		t.Fatal("empty should not match")
	}
}

func TestParseGccClangErrors_BuildSucceeded(t *testing.T) {
	result, hf, ok := parseGccClangErrors("Build succeeded.\n0 Error(s)\n")
	if !ok {
		t.Fatalf("should match: ok=%v", ok)
	}
	if hf {
		t.Fatal("should not have failures")
	}
	if !strings.Contains(result, "ok") {
		t.Fatalf("expected ok: %q", result)
	}
	_ = result
	_ = hf
	_ = ok
}

func TestBuildSuccessWithWarningsFailsOpenAcrossParsers(t *testing.T) {
	t.Parallel()
	stdout := strings.Join([]string{
		"# github.com/example/project",
		"warning: generated binding is deprecated",
		"Build succeeded with 0 errors and 1 warning.",
		strings.Repeat("padding line with neutral build output\n", 8),
	}, "\n")
	tests := []struct {
		name  string
		parse func(string) (string, bool, bool)
	}{
		{name: "go", parse: parseGoErrors},
		{name: "cargo", parse: parseCargoErrors},
		{name: "gcc", parse: parseGccClangErrors},
		{name: "diagnostic rows", parse: func(s string) (string, bool, bool) {
			return parseDiagnosticRows("frontend", s)
		}},
	}
	for _, tt := range tests {
		got, hadFailures, ok := tt.parse(stdout)
		if ok || hadFailures {
			t.Fatalf("%s parser compacted success-with-warning output: got=%q hadFailures=%v ok=%v", tt.name, got, hadFailures, ok)
		}
	}
	if got, ok := ParseFailures([]string{"go", "build", "./..."}, stdout); ok {
		t.Fatalf("ParseFailures compacted go build success-with-warning output: %q", got)
	}
}

func TestParseGccClangErrors_OutputNotShorter(t *testing.T) {
	t.Parallel()
	stdout := "main.c:1:1: error: x\n"
	_, _, ok := parseGccClangErrors(stdout)
	if ok {
		t.Fatal("short output should not compact")
	}
}

func TestParseFailures_DispatchesCorrectly(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("./main.go:10:3: undefined: foo\n")
	for i := 0; i < 10; i++ {
		sb.WriteString("padding line that makes output significantly longer\n")
	}
	compact, ok := ParseFailures([]string{"go", "build"}, sb.String())
	if !ok {
		t.Fatal("go build should dispatch to go parser")
	}
	if !strings.Contains(compact, "main.go:10") {
		t.Fatalf("missing error: %q", compact)
	}
}

func TestParseFailures_NoMatch(t *testing.T) {
	t.Parallel()
	_, ok := ParseFailures([]string{"python", "script.py"}, "some output")
	if ok {
		t.Fatal("unknown tool should not match")
	}
}

func TestIsGoBuildOrVetArgv(t *testing.T) {
	t.Parallel()
	tests := []struct {
		argv []string
		want bool
	}{
		{[]string{"go", "build"}, true},
		{[]string{"go", "vet"}, true},
		{[]string{"go", "test"}, true},
		{[]string{"go", "-C", "/repo", "build"}, true},
		{[]string{"go", "-C=/repo", "vet"}, true},
		{[]string{"go", "-mod=mod", "test"}, true},
		{[]string{"go", "-C"}, false},
		{[]string{"go", "run"}, false},
		{[]string{"go"}, false},
		{[]string{"python", "build"}, false},
	}
	for _, tt := range tests {
		got := isGoBuildOrVetArgv(tt.argv)
		if got != tt.want {
			t.Errorf("isGoBuildOrVetArgv(%v) = %v, want %v", tt.argv, got, tt.want)
		}
	}
}

func TestIsCargoBuildOrCheckArgv(t *testing.T) {
	t.Parallel()
	tests := []struct {
		argv []string
		want bool
	}{
		{[]string{"cargo", "build"}, true},
		{[]string{"cargo", "check"}, true},
		{[]string{"cargo", "clippy"}, true},
		{[]string{"cargo", "run"}, false},
		{[]string{"cargo"}, false},
	}
	for _, tt := range tests {
		got := isCargoBuildOrCheckArgv(tt.argv)
		if got != tt.want {
			t.Errorf("isCargoBuildOrCheckArgv(%v) = %v, want %v", tt.argv, got, tt.want)
		}
	}
}

func TestParseFailures_CargoCheckLabelAndDiagnosticBlock(t *testing.T) {
	t.Parallel()
	stdout := strings.Join([]string{
		"    Checking demo v0.1.0 (/tmp/demo)",
		"     Running `CARGO=/toolchain/bin/cargo /toolchain/bin/rustc --crate-name demo src/main.rs`",
		"error[E0308]: mismatched types",
		" --> src/main.rs:2:22",
		"  |",
		"2 |     let value: i32 = \"not an integer\";",
		"  |                ---   ^^^^^^^^^^^^^^^^ expected `i32`, found `&str`",
		"  |                |",
		"  |                expected due to this",
		"",
		"error: could not compile `demo` (bin \"demo\") due to 1 previous error",
		"",
		"Caused by:",
		"  process didn't exit successfully: `/toolchain/bin/rustc --crate-name demo src/main.rs` (exit status: 1)",
	}, "\n")
	compact, ok := ParseFailures([]string{"cargo", "check", "-vv"}, stdout)
	if !ok {
		t.Fatal("expected cargo check failure compaction")
	}
	for _, want := range []string{"[cargo check] FAILED", "error[E0308]", "let value: i32", "expected due to this", "could not compile"} {
		if !strings.Contains(compact, want) {
			t.Fatalf("compact missing %q: %q", want, compact)
		}
	}
	if strings.Contains(compact, "Running `CARGO=") {
		t.Fatalf("compact kept neutral verbose command noise: %q", compact)
	}
}

func TestIsGccClangArgv(t *testing.T) {
	t.Parallel()
	tests := []struct {
		argv []string
		want bool
	}{
		{[]string{"gcc"}, true},
		{[]string{"g++"}, true},
		{[]string{"clang"}, true},
		{[]string{"clang++"}, true},
		{[]string{"cc"}, true},
		{[]string{"python"}, false},
		{[]string{}, false},
	}
	for _, tt := range tests {
		got := isGccClangArgv(tt.argv)
		if got != tt.want {
			t.Errorf("isGccClangArgv(%v) = %v, want %v", tt.argv, got, tt.want)
		}
	}
}

func TestBuildOutputFallback_MakeFailure(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("make[1]: *** [target] Error 1\n")
	sb.WriteString("make: *** [all] Error 2\n")
	for i := 0; i < 10; i++ {
		sb.WriteString("padding line that makes output significantly longer\n")
	}
	out, ok := TryCompactBuildOutput([]string{"make"}, []byte(sb.String()))
	if !ok {
		t.Fatal("expected fallback to extract errors")
	}
	if !strings.Contains(string(out), "FAILED") {
		t.Fatalf("expected FAILED: %q", out)
	}
}

func TestBuildOutputFallback_MakeSuccess(t *testing.T) {
	t.Parallel()
	out, ok := TryCompactBuildOutput([]string{"make"}, []byte("Build succeeded.\n0 errors\n"))
	if !ok {
		t.Fatal("expected build success detection")
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("expected ok: %q", out)
	}
}

func TestFalsePositiveTraps(t *testing.T) {
	t.Parallel()
	traps := []string{
		"Successfully resolved errors: 0\n",
		"Test 'test_undefined_handling' passed\n",
		"Aborting on first failure: false\n",
		"error_count = 0, failed = false\n",
	}
	for _, trap := range traps {
		_, _, ok := parseGoErrors(trap)
		if ok {
			t.Errorf("go parser false-positive on: %q", trap)
		}
		_, _, ok = parseGccClangErrors(trap)
		if ok {
			t.Errorf("gcc parser false-positive on: %q", trap)
		}
	}
}

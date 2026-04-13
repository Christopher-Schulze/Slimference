package compression

import (
	"strings"
	"testing"
)

func TestMaybeSuccessShortCircuit_BuildOK(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("x", 100) + "\nBUILD SUCCESSFUL in 2s\n"
	out, ok := MaybeSuccessShortCircuit(text)
	if !ok {
		t.Fatal("expected short-circuit for build success")
	}
	if !strings.HasPrefix(out, "[ok]") {
		t.Fatalf("unexpected: %q", out)
	}
}

func TestMaybeSuccessShortCircuit_HasError(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("x", 100) + "\nBUILD SUCCESSFUL\nerror: something failed\n"
	out, ok := MaybeSuccessShortCircuit(text)
	if ok {
		t.Fatal("should not short-circuit when error present")
	}
	if out != text {
		t.Fatal("text should be unchanged")
	}
}

func TestMaybeSuccessShortCircuit_TooShort(t *testing.T) {
	t.Parallel()
	out, ok := MaybeSuccessShortCircuit("short")
	if ok || out != "short" {
		t.Fatalf("want unchanged short text, ok=%v out=%q", ok, out)
	}
}

func TestMaybeSuccessShortCircuit_TestsOK(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("x", 100) + "\nAll tests passed\n"
	out, ok := MaybeSuccessShortCircuit(text)
	if !ok || !strings.Contains(out, "tests passed") {
		t.Fatalf("ok=%v out=%q", ok, out)
	}
}

func TestMaybeSuccessShortCircuit_LintOK(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("x", 100) + "\nNo issues found\n"
	out, ok := MaybeSuccessShortCircuit(text)
	if !ok || !strings.Contains(out, "Lint clean") {
		t.Fatalf("ok=%v out=%q", ok, out)
	}
}

func TestMaybeSuccessShortCircuit_NoSignal(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("y", 100) + "\nunrelated verbose log line\n"
	out, ok := MaybeSuccessShortCircuit(text)
	if ok || out != text {
		t.Fatalf("ok=%v out=%q", ok, out)
	}
}

func TestMaybeSuccessShortCircuit_ErrorColon(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("z", 100) + "\n Error: build failed\n"
	out, ok := MaybeSuccessShortCircuit(text)
	if ok || out != text {
		t.Fatalf("ok=%v out=%q", ok, out)
	}
}

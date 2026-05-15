package filter

import (
	"context"
	"runtime"
	"testing"
)

func TestRunCommand_Echo(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("echo semantics differ on Windows")
	}
	out, errOut, code, runErr := RunCommand(context.Background(), t.TempDir(), []string{"echo", "hello"})
	if runErr != nil {
		t.Fatal(runErr)
	}
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	if string(out) != "hello\n" && string(out) != "hello\r\n" {
		t.Fatalf("stdout %q stderr %q", out, errOut)
	}
}

func TestRunCommand_UnknownBinary(t *testing.T) {
	t.Parallel()
	_, _, code, err := RunCommand(context.Background(), t.TempDir(), []string{"/nonexistent/slimference-binary-xyz"})
	if err == nil {
		t.Fatal("expected error")
	}
	if code != -1 {
		t.Fatalf("code %d", code)
	}
}

func TestRunCommand_emptyArgv(t *testing.T) {
	t.Parallel()
	_, _, code, err := RunCommand(context.Background(), t.TempDir(), []string{})
	if err == nil || code != -1 {
		t.Fatalf("empty argv: expected error, got code=%d err=%v", code, err)
	}
}

func TestRunCommand_exitCode(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("false semantics differ on Windows")
	}
	_, _, code, err := RunCommand(context.Background(), t.TempDir(), []string{"false"})
	if err != nil || code == 0 {
		t.Fatalf("false: expected non-zero exit, got code=%d err=%v", code, err)
	}
}

func TestEstimateTokensFromBytes(t *testing.T) {
	t.Parallel()
	if EstimateTokensFromBytes(0) != 0 {
		t.Fatal()
	}
	if EstimateTokensFromBytes(3) != 1 {
		t.Fatal()
	}
	if EstimateTokensFromBytes(40) != 10 {
		t.Fatal()
	}
}

func TestEstimateTokensFromTextAndSlices(t *testing.T) {
	t.Parallel()
	if EstimateTokensFromText("") != 0 {
		t.Fatal("empty text should count as zero")
	}
	short := EstimateTokensFromText("hello world")
	long := EstimateTokensFromText("hello world with several more words for tokenizer accuracy")
	if short <= 0 || long <= short {
		t.Fatalf("token estimates not monotonic: short=%d long=%d", short, long)
	}
	if got := estimateTokensFromByteSlices([]byte("hello"), nil, []byte("world")); got <= 0 {
		t.Fatalf("slice estimate=%d", got)
	}
}

func TestEstimateTokensFromText_fallbackWhenTokenizerUnavailable(t *testing.T) {
	got := estimateTokensFromText("abcdefghijkl", func(string) int { return 0 })
	if got != 3 {
		t.Fatalf("fallback estimate=%d, want 3", got)
	}
}

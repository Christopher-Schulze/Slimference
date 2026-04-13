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
	_, _, code, err := RunCommand(context.Background(), t.TempDir(), []string{"/nonexistent/tokenproxy-binary-xyz"})
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

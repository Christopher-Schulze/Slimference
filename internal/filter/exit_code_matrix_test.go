package filter

import (
	"context"
	"runtime"
	"testing"
)

// TestT63_CleanRunExit0 locks in the "Clean run -> 0" row of the matrix.
func TestT63_CleanRunExit0(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only exit semantics")
	}
	pr := RunPipeline(context.Background(), "", []string{"true"}, 0)
	if pr.Err != nil {
		t.Fatalf("unexpected start error: %v", pr.Err)
	}
	if pr.Code != 0 {
		t.Fatalf("exit code = %d, want 0", pr.Code)
	}
}

// TestT63_ChildNonZeroPropagated locks in "Child non-zero, filter ok -> N".
// Slimference must never swallow a non-zero exit into 0.
func TestT63_ChildNonZeroPropagated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only exit semantics")
	}
	cases := []int{1, 2, 7, 42, 127}
	for _, want := range cases {
		t.Run("", func(t *testing.T) {
			pr := RunPipeline(context.Background(), "",
				[]string{"sh", "-c", "exit " + itoaFilter(want)}, 0)
			if pr.Err != nil {
				t.Fatalf("start error: %v", pr.Err)
			}
			if pr.Code != want {
				t.Fatalf("exit = %d, want %d", pr.Code, want)
			}
			// RawStdout / RawStderr may be nil when the child emitted
			// nothing - that is fine, Tee recovery can still be a no-op.
			// The contract we care about here is exit-code propagation.
		})
	}
}

// TestT63_StartFailureReturnsErrCodeMinusOne pins the "Start failure" row.
func TestT63_StartFailureReturnsErrCodeMinusOne(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only exit semantics")
	}
	pr := RunPipeline(context.Background(), "",
		[]string{"/definitely/not/a/real/binary-xyzzy"}, 0)
	if pr.Err == nil {
		t.Fatal("expected start error, got nil")
	}
	if pr.Code != -1 {
		t.Fatalf("code = %d, want -1 on start failure", pr.Code)
	}
}

// TestT63_EmptyArgvHandled - defensive: Slimference should not panic on an
// empty command line. (Guards the argv0 := "" branch in pipeline.go.)
func TestT63_EmptyArgvHandled(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic on empty argv: %v", r)
		}
	}()
	_ = RunPipeline(context.Background(), "", nil, 0)
}

// itoaFilter - tiny local helper so the test file stays self-contained
// without pulling strconv into this already-slim file.
func itoaFilter(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

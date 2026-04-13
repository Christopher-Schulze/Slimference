package util

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestGo_RunsFunction verifies that Go launches the function in a goroutine.
func TestGo_RunsFunction(t *testing.T) {
	t.Parallel()
	var ran atomic.Bool
	ctx := context.Background()

	Go(ctx, "test", func() {
		ran.Store(true)
	})

	// Wait briefly for the goroutine to execute.
	time.Sleep(50 * time.Millisecond)

	if !ran.Load() {
		t.Error("function was not executed")
	}
}

// TestGo_CancelledContext verifies that Go does not start when context is cancelled.
func TestGo_CancelledContext(t *testing.T) {
	t.Parallel()
	var ran atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	Go(ctx, "test", func() {
		ran.Store(true)
	})

	time.Sleep(50 * time.Millisecond)

	if ran.Load() {
		t.Error("function should not have been started with cancelled context")
	}
}

// TestGo_PanicRecovery verifies that a panic inside the goroutine is recovered and does not crash the process.
func TestGo_PanicRecovery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// This should not panic the test process.
	Go(ctx, "panic-test", func() {
		panic("test panic")
	})

	time.Sleep(50 * time.Millisecond)
	// If we got here, the panic was recovered.
}

// TestGoWithRestart_RunsFunction verifies that GoWithRestart executes the function.
func TestGoWithRestart_RunsFunction(t *testing.T) {
	t.Parallel()
	var ran atomic.Bool
	ctx := context.Background()

	GoWithRestart(ctx, "test", 10*time.Millisecond, func() error {
		ran.Store(true)
		return nil // clean exit, no restart
	})

	time.Sleep(50 * time.Millisecond)

	if !ran.Load() {
		t.Error("function was not executed")
	}
}

// TestGoWithRestart_RestartsOnError verifies that GoWithRestart restarts after an error.
func TestGoWithRestart_RestartsOnError(t *testing.T) {
	t.Parallel()
	var count atomic.Int32
	ctx := context.Background()

	GoWithRestart(ctx, "restart-test", 10*time.Millisecond, func() error {
		c := count.Add(1)
		if c < 3 {
			return errTest{"transient error"}
		}
		return nil // stop after 3 attempts
	})

	time.Sleep(200 * time.Millisecond)

	got := count.Load()
	if got < 3 {
		t.Errorf("function executed %d times, want >= 3 (with restarts)", got)
	}
}

// TestGoWithRestart_CancelledContext verifies that GoWithRestart does not start on cancelled context.
func TestGoWithRestart_CancelledContext(t *testing.T) {
	t.Parallel()
	var ran atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	GoWithRestart(ctx, "test", 10*time.Millisecond, func() error {
		ran.Store(true)
		return nil
	})

	time.Sleep(50 * time.Millisecond)

	if ran.Load() {
		t.Error("function should not have been started with cancelled context")
	}
}

// TestGoWithRestart_PanicRecovery verifies that a panic is treated as an error and triggers restart.
func TestGoWithRestart_PanicRecovery(t *testing.T) {
	t.Parallel()
	var count atomic.Int32
	ctx := context.Background()

	GoWithRestart(ctx, "panic-restart", 10*time.Millisecond, func() error {
		c := count.Add(1)
		if c < 2 {
			panic("panic on first attempt")
		}
		return nil
	})

	time.Sleep(200 * time.Millisecond)

	got := count.Load()
	if got < 2 {
		t.Errorf("function executed %d times, want >= 2 (panic should trigger restart)", got)
	}
}

// TestDrainChannel verifies that DrainChannel removes all buffered items.
func TestDrainChannel(t *testing.T) {
	t.Parallel()
	ch := make(chan int, 5)
	for i := 0; i < 5; i++ {
		ch <- i
	}

	DrainChannel(ch)

	if len(ch) != 0 {
		t.Errorf("channel should be empty after DrainChannel, has %d items", len(ch))
	}
}

// TestDrainChannel_Empty verifies that DrainChannel on an empty channel returns immediately.
func TestDrainChannel_Empty(t *testing.T) {
	t.Parallel()
	ch := make(chan int, 5)

	done := make(chan struct{})
	go func() {
		DrainChannel(ch)
		close(done)
	}()

	select {
	case <-done:
		// ok, returned immediately
	case <-time.After(time.Second):
		t.Error("DrainChannel blocked on empty channel")
	}
}

// TestRunCatching_NoError verifies runCatching returns nil when fn succeeds.
func TestRunCatching_NoError(t *testing.T) {
	err := runCatching(func() error { return nil })
	if err != nil {
		t.Errorf("runCatching returned %v, want nil", err)
	}
}

// TestRunCatching_Error verifies runCatching returns the error from fn.
func TestRunCatching_Error(t *testing.T) {
	want := errTest{"some error"}
	got := runCatching(func() error { return want })
	if got.Error() != want.Error() {
		t.Errorf("runCatching returned %v, want %v", got, want)
	}
}

// TestRunCatching_PanicString verifies that a string panic is converted to an error.
func TestRunCatching_PanicString(t *testing.T) {
	got := runCatching(func() error {
		panic("panic value")
	})
	if got == nil {
		t.Fatal("runCatching should return an error for panic")
	}
	if got.Error() != "panic: panic value" {
		t.Errorf("runCatching error = %q, want 'panic: panic value'", got.Error())
	}
}

// TestRunCatching_PanicNonString verifies that a non-string panic is converted to an error.
func TestRunCatching_PanicNonString(t *testing.T) {
	got := runCatching(func() error {
		panic(42)
	})
	if got == nil {
		t.Fatal("runCatching should return an error for panic")
	}
	if got.Error() != "panic: non-string panic value" {
		t.Errorf("runCatching error = %q, want 'panic: non-string panic value'", got.Error())
	}
}

// TestRunCatching_PanicBytes verifies that a []byte panic is handled.
func TestRunCatching_PanicBytes(t *testing.T) {
	got := runCatching(func() error {
		panic([]byte("byte panic"))
	})
	if got == nil {
		t.Fatal("runCatching should return an error for panic")
	}
	if got.Error() != "panic: byte panic" {
		t.Errorf("runCatching error = %q, want 'panic: byte panic'", got.Error())
	}
}

// TestFormatAny verifies the formatAny helper.
func TestFormatAny(t *testing.T) {
	tests := []struct {
		input any
		want  string
	}{
		{"hello", "hello"},
		{[]byte("bytes"), "bytes"},
		{42, "non-string panic value"},
	}
	for _, tt := range tests {
		got := formatAny(tt.input)
		if got != tt.want {
			t.Errorf("formatAny(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestRunCatching_PanicError verifies that a panic value implementing error is wrapped correctly.
func TestRunCatching_PanicError(t *testing.T) {
	errPanic := errTest{"error panic value"}
	got := runCatching(func() error {
		panic(errPanic)
	})
	if got == nil {
		t.Fatal("runCatching should return an error for error panic")
	}
	if got.Error() != errPanic.Error() {
		t.Errorf("runCatching error = %q, want %q", got.Error(), errPanic.Error())
	}
}

// TestGoWithRestart_CancelDuringDelay verifies context cancellation during the restart delay.
func TestGoWithRestart_CancelDuringDelay(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	var count atomic.Int32

	GoWithRestart(ctx, "cancel-delay-test", 500*time.Millisecond, func() error {
		n := count.Add(1)
		if n == 1 {
			// Cancel immediately so the restart delay select hits ctx.Done()
			cancel()
			return errTest{"trigger restart"}
		}
		return nil
	})

	// Give goroutine time to process cancel
	time.Sleep(100 * time.Millisecond)

	got := count.Load()
	if got > 1 {
		t.Errorf("function ran %d times after cancel, want 1", got)
	}
}

// errTest is a simple test error type.
type errTest struct{ msg string }

func (e errTest) Error() string { return e.msg }

package main

import (
	"os"
	"testing"
)

func TestRun_AllStepsSucceedReturnsZero(t *testing.T) {
	steps := []step{
		{label: "echo 1", cmd: "true", args: nil},
		{label: "echo 2", cmd: "true", args: nil},
	}
	code := run(steps, devNull(t), devNull(t))
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
}

func TestRun_OneStepFailsReturnsOne(t *testing.T) {
	steps := []step{
		{label: "ok", cmd: "true", args: nil},
		{label: "fail", cmd: "false", args: nil},
		{label: "never", cmd: "true", args: nil},
	}
	code := run(steps, devNull(t), devNull(t))
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestRun_ModuleRootFailsReturnsTwo(t *testing.T) {
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir("/"); err != nil {
		t.Skipf("cannot chdir to /: %v", err)
	}
	code := run(defaultSteps(), devNull(t), devNull(t))
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestRun_EmptyStepsReturnsZero(t *testing.T) {
	code := run(nil, devNull(t), devNull(t))
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
}

func devNull(t *testing.T) *os.File {
	t.Helper()
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open /dev/null: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

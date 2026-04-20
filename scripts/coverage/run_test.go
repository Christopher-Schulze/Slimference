package main

import (
	"io"
	"os"
	"testing"
)

// TestRun_BadFlag returns exit 2 on unknown flag.
func TestRun_BadFlag(t *testing.T) {
	code := run([]string{"--not-a-flag"}, devNull(t), devNull(t))
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

// TestRun_ValidFlagsParses covers the flag-parse success path plus
// findModuleRoot success. Since we cannot actually `go test` from inside
// a test (no network / sandbox), we do not assert exit=0; we just verify
// that the early-parse path did not return exit=2.
func TestRun_ValidFlagsPasses(t *testing.T) {
	code := run([]string{"-min=0", "-keep=false"}, devNull(t), devNull(t))
	if code == 2 {
		t.Fatalf("flag parse failed unexpectedly (exit 2)")
	}
	// Expect 0 or 1 depending on in-process `go test` outcome.
}

// TestFindModuleRoot_HitsGoMod exercises the loop-until-found branch.
func TestFindModuleRoot_HitsGoMod(t *testing.T) {
	root, err := findModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root + "/go.mod"); err != nil {
		t.Fatalf("resolved root has no go.mod: %v", err)
	}
}

// TestFindModuleRoot_NoGoModError — switch cwd to / (no go.mod anywhere up).
func TestFindModuleRoot_NoGoModError(t *testing.T) {
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir("/"); err != nil {
		t.Skipf("cannot chdir to /: %v", err)
	}
	_, err := findModuleRoot()
	if err == nil {
		t.Fatal("expected error when no go.mod is reachable")
	}
}

// TestParseTotalPercent_Empty returns false.
func TestParseTotalPercent_Empty(t *testing.T) {
	_, ok := parseTotalPercent("")
	if ok {
		t.Fatal("empty output should not parse")
	}
}

// TestParseTotalPercent_Malformed returns false.
func TestParseTotalPercent_Malformed(t *testing.T) {
	_, ok := parseTotalPercent("not a coverage report\n")
	if ok {
		t.Fatal("malformed output should not parse")
	}
}

// devNull returns an *os.File pointing at /dev/null so test stdout / stderr
// capture does not pollute the test runner output.
func devNull(t *testing.T) *os.File {
	t.Helper()
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open devnull: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

var _ = io.Discard // ensure import anchor if needed in future expansion

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultSteps_CoverageGateUsesRealMinFlag(t *testing.T) {
	t.Parallel()

	steps := defaultSteps()
	if len(steps) != 4 {
		t.Fatalf("unexpected step count: %d", len(steps))
	}

	want := []string{"run", "./scripts/coverage", "-min=100"}
	if !reflect.DeepEqual(steps[3].args, want) {
		t.Fatalf("coverage gate args: got %v want %v", steps[3].args, want)
	}
}

func TestFindModuleRoot(t *testing.T) {
	t.Parallel()

	root, err := findModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("go.mod in %s: %v", root, err)
	}
}

func TestFindModuleRoot_NoGoMod(t *testing.T) {
	tmp := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	if _, err := findModuleRoot(); err == nil {
		t.Fatal("expected error when no go.mod in ancestors")
	}
}

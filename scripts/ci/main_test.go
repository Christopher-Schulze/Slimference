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
	if len(steps) != 5 {
		t.Fatalf("unexpected step count: %d", len(steps))
	}

	wantCoverage := []string{"run", "./scripts/coverage", "-min=100"}
	if !reflect.DeepEqual(steps[3].args, wantCoverage) {
		t.Fatalf("coverage gate args: got %v want %v", steps[3].args, wantCoverage)
	}

	wantCodexGate := []string{"run", "./scripts/benchmarks", "codex-smoke-gate", "tests/fixtures/codex"}
	if !reflect.DeepEqual(steps[4].args, wantCodexGate) {
		t.Fatalf("codex smoke gate args: got %v want %v", steps[4].args, wantCodexGate)
	}
	if steps[4].label != "codex smoke gate" {
		t.Fatalf("codex smoke gate label: got %q want %q", steps[4].label, "codex smoke gate")
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

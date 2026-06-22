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
	if len(steps) != 8 {
		t.Fatalf("unexpected step count: %d", len(steps))
	}

	if steps[0].label != "gofmt" || steps[0].cmd != "internal:gofmt-check" {
		t.Fatalf("gofmt step: %+v", steps[0])
	}
	wantVet := []string{"vet", "./..."}
	if !reflect.DeepEqual(steps[1].args, wantVet) {
		t.Fatalf("go vet args: got %v want %v", steps[1].args, wantVet)
	}

	wantCoverage := []string{"run", "./scripts/coverage", "-min=94.5"}
	if !reflect.DeepEqual(steps[4].args, wantCoverage) {
		t.Fatalf("coverage gate args: got %v want %v", steps[4].args, wantCoverage)
	}

	wantCodexGate := []string{"run", "./scripts/benchmarks", "codex-smoke-gate", "tests/fixtures/codex"}
	if !reflect.DeepEqual(steps[5].args, wantCodexGate) {
		t.Fatalf("codex smoke gate args: got %v want %v", steps[5].args, wantCodexGate)
	}
	if steps[5].label != "codex smoke gate" {
		t.Fatalf("codex smoke gate label: got %q want %q", steps[5].label, "codex smoke gate")
	}
	wantCorpusGate := []string{
		"run", "./scripts/benchmarks", "benchmark-corpus", "tests/fixtures/live_corpus",
		"--check",
		"--promotion-check",
		"--maxx-check",
		"--real-local-min-ratio=0.5730",
		"--real-local-min-saved=7330000",
	}
	if !reflect.DeepEqual(steps[6].args, wantCorpusGate) {
		t.Fatalf("live corpus gate args: got %v want %v", steps[6].args, wantCorpusGate)
	}
	if steps[6].label != "live corpus gate" {
		t.Fatalf("live corpus gate label: got %q want %q", steps[6].label, "live corpus gate")
	}
	wantLeafAudit := []string{"run", "./scripts/utils", "leaf-audit", "--check", "--max-empty-only-pct=20", "--root=."}
	if !reflect.DeepEqual(steps[7].args, wantLeafAudit) {
		t.Fatalf("leaf audit args: got %v want %v", steps[7].args, wantLeafAudit)
	}
	if steps[7].label != "leaf audit gate" {
		t.Fatalf("leaf audit label: got %q want %q", steps[7].label, "leaf audit gate")
	}
}

// TestRunGofmtCheck_Clean verifies the gofmt step passes on the
// current checkout (which it must - the gate would block CI itself).
func TestRunGofmtCheck_Clean(t *testing.T) {
	t.Parallel()
	root, err := findModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	if err := runGofmtCheck(root, os.Stdout); err != nil {
		t.Fatalf("gofmt check should be clean on the checkout: %v", err)
	}
}

// TestRunGofmtCheck_Drift writes a file with bad indentation into a
// scratch tree and verifies runGofmtCheck flags it.
func TestRunGofmtCheck_Drift(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	cmdDir := filepath.Join(tmp, "cmd", "x")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Spaces instead of tabs - gofmt rewrites to tabs.
	body := "package main\n\nfunc main() {\n  println(\"hi\")\n}\n"
	if err := os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"internal/keep", "scripts/keep"} {
		if err := os.MkdirAll(filepath.Join(tmp, d), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmp, d, "doc.go"), []byte("package keep\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := runGofmtCheck(tmp, os.Stdout); err == nil {
		t.Fatal("expected drift to be flagged")
	}
}

// TestRunGofmtCheck_GofmtRunFails covers the path where the gofmt
// invocation itself errors out (e.g. missing path).
func TestRunGofmtCheck_GofmtRunFails(t *testing.T) {
	t.Parallel()
	if err := runGofmtCheck("/nonexistent-root", os.Stdout); err == nil {
		t.Fatal("expected error when root does not exist")
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

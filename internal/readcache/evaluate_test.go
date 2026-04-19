package readcache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvaluate_FirstReadAllowsAndStores(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	decision, err := Evaluate(dir, Request{SessionID: "s1", FilePath: file})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Type != DecisionAllow {
		t.Fatalf("expected allow, got %#v", decision)
	}
}

func TestEvaluate_UnchangedReadBlocks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := Request{SessionID: "s1", FilePath: file}
	if _, err := Evaluate(dir, req); err != nil {
		t.Fatal(err)
	}
	decision, err := Evaluate(dir, req)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionBlock || !strings.Contains(decision.Reason, "already in context") {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestEvaluate_ChangedFullReadBlocksWithDelta(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	before := "package main\n" + strings.Repeat("func a() {}\n", 40)
	if err := os.WriteFile(file, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}

	req := Request{SessionID: "s1", FilePath: file}
	if _, err := Evaluate(dir, req); err != nil {
		t.Fatal(err)
	}

	after := "package main\n" + strings.Repeat("func a() {}\n", 40) + "func b() {}\n"
	if err := os.WriteFile(file, []byte(after), 0o644); err != nil {
		t.Fatal(err)
	}

	decision, err := Evaluate(dir, req)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionBlock || !strings.Contains(decision.Reason, "Slimference delta") {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestEvaluate_ChangedRangeAllows(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := Request{SessionID: "s1", FilePath: file, Offset: 5, Limit: 10}
	if _, err := Evaluate(dir, req); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(file, []byte("package main\nvar x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	decision, err := Evaluate(dir, req)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionAllow {
		t.Fatalf("expected allow, got %#v", decision)
	}
}

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCorpusFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMeasureStructureAccuracy_GoHappy(t *testing.T) {
	t.Parallel()
	content := "package demo\n\nfunc A() {}\ntype B struct{}\n"
	row := measureStructureAccuracy("a.go", "go", content)
	if row.InputLen == 0 {
		t.Fatalf("len zero")
	}
	if row.TotalDecls != 2 {
		t.Fatalf("decls: %d", row.TotalDecls)
	}
}

func TestMeasureStructureAccuracy_EmptyContent(t *testing.T) {
	t.Parallel()
	row := measureStructureAccuracy("x", "go", "")
	if row.InputLen != 0 {
		t.Fatal("empty must yield len 0")
	}
	if row.TotalDecls != 0 {
		t.Fatal("empty must have no decls")
	}
}

func TestMeasureStructureAccuracyDir_WalksCorpus(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeCorpusFile(t, dir, "a.go", "package x\nfunc F() {}\n")
	writeCorpusFile(t, dir, "b.py", "def g():\n    pass\n")
	writeCorpusFile(t, dir, "c.unknown", "ignored")

	rows, err := measureStructureAccuracyDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (unknown ext skipped), got %d", len(rows))
	}
}

func TestMeasureStructureAccuracyDir_BadRoot(t *testing.T) {
	t.Parallel()
	if _, err := measureStructureAccuracyDir("/definitely-not-there-xyz"); err == nil {
		t.Fatal("expected walk error")
	}
}

func TestCountSurvivedDeclarations_PerLanguage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		lang, original, summary string
		wantTotal, wantSurvived int
	}{
		{"go", "func A()\nfunc B()\n", "func A()", 2, 1},
		{"python", "def a():\nclass B:\n", "def a():", 2, 1},
		{"rust", "fn x()\nstruct Y", "fn x()\nstruct Y", 2, 2},
		{"typescript", "function a()\nclass B", "function a()", 2, 1},
		{"javascript", "function a()\n", "function a()", 1, 1},
		{"ruby", "def a\n", "def a", 1, 1},
		{"java", "class A\n", "class A", 1, 1},
		{"c", "function c()\n", "function c()", 1, 1},
	}
	for _, tc := range cases {
		survived, total := countSurvivedDeclarations(tc.original, tc.summary, tc.lang)
		if total != tc.wantTotal || survived != tc.wantSurvived {
			t.Errorf("%s: got (survived=%d total=%d), want (survived=%d total=%d)", tc.lang, survived, total, tc.wantSurvived, tc.wantTotal)
		}
	}
}

func TestFormatStructureAccuracyReport_Empty(t *testing.T) {
	t.Parallel()
	if got := formatStructureAccuracyReport(nil); !strings.Contains(got, "No source files") {
		t.Fatalf("got %s", got)
	}
}

func TestFormatStructureAccuracyReport_RendersTable(t *testing.T) {
	t.Parallel()
	rows := []structureAccuracyRow{
		{Path: "short.go", Language: "go", InputLen: 100, SummaryLen: 60, Changed: true, SurvivedN: 3, TotalDecls: 4},
		{Path: "unchanged.py", Language: "python", InputLen: 40, SummaryLen: 40, Changed: false, SurvivedN: 2, TotalDecls: 2},
	}
	out := formatStructureAccuracyReport(rows)
	for _, need := range []string{"path", "short.go", "unchanged.py", "structured", "unchanged", "size_ratio"} {
		if !strings.Contains(out, need) {
			t.Fatalf("missing %q in:\n%s", need, out)
		}
	}
}

func TestFormatStructureAccuracyReport_LongPathTruncated(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a/", 40) + "x.go"
	rows := []structureAccuracyRow{{Path: long, Language: "go", InputLen: 10, SummaryLen: 10}}
	out := formatStructureAccuracyReport(rows)
	if !strings.Contains(out, "...") {
		t.Fatalf("expected truncation: %s", out)
	}
}

func TestTruncateLeft(t *testing.T) {
	t.Parallel()
	if got := truncateLeft("short", 10); got != "short" {
		t.Fatalf("got %q", got)
	}
	if got := truncateLeft("abcdefghij", 5); got != "...ij" {
		t.Fatalf("got %q", got)
	}
}

func TestRunStructureAccuracy_Usage(t *testing.T) {
	t.Parallel()
	var out, errOut bytes.Buffer
	if code := runStructureAccuracy(nil, &out, &errOut); code != 2 {
		t.Fatalf("exit: %d", code)
	}
	if !strings.Contains(errOut.String(), "Usage:") {
		t.Fatalf("usage hint: %s", errOut.String())
	}
}

func TestRunStructureAccuracy_MissingDir(t *testing.T) {
	t.Parallel()
	var out, errOut bytes.Buffer
	if code := runStructureAccuracy([]string{filepath.Join(t.TempDir(), "nope")}, &out, &errOut); code != 1 {
		t.Fatalf("exit: %d", code)
	}
}

func TestRunStructureAccuracy_PathIsFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f := writeCorpusFile(t, dir, "x.go", "package x")
	var out, errOut bytes.Buffer
	if code := runStructureAccuracy([]string{f}, &out, &errOut); code != 1 {
		t.Fatalf("file as arg must exit 1, got %d", code)
	}
}

func TestRunStructureAccuracy_Happy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeCorpusFile(t, dir, "x.go", "package x\nfunc A() {}\n")
	var out, errOut bytes.Buffer
	if code := runStructureAccuracy([]string{dir}, &out, &errOut); code != 0 {
		t.Fatalf("exit: %d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "x.go") {
		t.Fatalf("output missing file name: %s", out.String())
	}
}

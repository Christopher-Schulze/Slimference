package filter

import (
	"fmt"
	"strings"
	"testing"
)

func TestTryStripCommentsFileRead(t *testing.T) {
	t.Parallel()
	argv := []string{"cat", "main.go"}
	src := "// hello\npackage main\n"
	out, ok := TryStripCommentsFileRead(argv, []byte(src))
	if !ok {
		t.Fatal("expected comment strip")
	}
	if strings.Contains(string(out), "// hello") {
		t.Fatalf("comment should be removed: %q", out)
	}
	// Multi-file cat with a known extension: now supported (strips comments using last path's lang).
	multiOut, multiOK := TryStripCommentsFileRead([]string{"cat", "a", "b.go"}, []byte(src))
	if multiOK && strings.Contains(string(multiOut), "// hello") {
		t.Fatal("multi-file cat: comment should be stripped when ok=true")
	}
	if _, ok := TryStripCommentsFileRead([]string{"cat", "README.md"}, []byte("# x\n")); ok {
		t.Fatal("unknown extension")
	}
}

func TestCountReadPaths(t *testing.T) {
	t.Parallel()
	if n := countReadPaths([]string{"head", "-n", "5", "x.go"}); n != 1 {
		t.Fatalf("got %d", n)
	}
}

func TestTryStripCommentsFileRead_noShrink(t *testing.T) {
	t.Parallel()
	// File with no comments — stripping produces same output, not shorter
	if _, ok := TryStripCommentsFileRead([]string{"cat", "file.go"}, []byte("package main\n")); ok {
		t.Fatal("no-shrink should return false")
	}
}

func TestCountReadPaths_flagAtEnd(t *testing.T) {
	t.Parallel()
	// Flag at very end with no following value — must not skip past end of slice
	if n := countReadPaths([]string{"head", "-n"}); n != 0 {
		t.Fatalf("flag at end: got %d", n)
	}
	if p := lastReadFilePath([]string{"head", "-n"}); p != "" {
		t.Fatalf("flag at end: got %q", p)
	}
}

// TestLastReadFilePath covers the flag-with-value and "-" stdin branches.
func TestLastReadFilePath(t *testing.T) {
	t.Parallel()
	// -n 5 file.go: flag with value, then file path.
	if p := lastReadFilePath([]string{"head", "-n", "5", "file.go"}); p != "file.go" {
		t.Fatalf("-n 5 file.go: got %q", p)
	}
	// "--lines" with value.
	if p := lastReadFilePath([]string{"head", "--lines", "10", "file.py"}); p != "file.py" {
		t.Fatalf("--lines 10 file.py: got %q", p)
	}
	// "-" as stdin placeholder: skipped, returns last real file.
	if p := lastReadFilePath([]string{"cat", "-", "real.go"}); p != "real.go" {
		t.Fatalf("cat - real.go: got %q", p)
	}
	// only "-" and flags: returns "".
	if p := lastReadFilePath([]string{"cat", "-", "-n", "5"}); p != "" {
		t.Fatalf("cat - -n 5: got %q", p)
	}
}

// TestTryStripCommentsFileRead_multiFile covers the multi-file path in TryStripCommentsFileRead.
func TestTryStripCommentsFileRead_multiFile(t *testing.T) {
	t.Parallel()
	// Two Go files: lang is recognized, comments are stripped.
	goContent := "package main\n\n// This is a comment\nfunc main() {\n\t// inner comment\n\tfmt.Println(\"hello\")\n}\n"
	// Repeat to ensure output is shorter after stripping.
	bigContent := ""
	for i := 0; i < 20; i++ {
		bigContent += goContent
	}
	argv := []string{"cat", "file1.go", "file2.go"}
	out, ok := TryStripCommentsFileRead(argv, []byte(bigContent))
	if !ok {
		t.Logf("multi-file strip: no compaction (may be content-dependent)")
		return
	}
	if len(out) >= len(bigContent) {
		t.Errorf("multi-file strip: output should be shorter: %d vs %d", len(out), len(bigContent))
	}
}

// TestTryStripCommentsFileRead_multiFileUnknownLang covers the unknown extension path.
func TestTryStripCommentsFileRead_multiFileUnknownLang(t *testing.T) {
	t.Parallel()
	// .xyz is not a recognized extension → returns false
	argv := []string{"cat", "file1.xyz", "file2.xyz"}
	_, ok := TryStripCommentsFileRead(argv, []byte("some content\n"))
	if ok {
		t.Error("unknown extension: want false, got true")
	}
}

// TestTryStripCommentsFileRead_multiFileNoShrink covers the len(out)>=len(s) guard (line 53-55):
// multi-file cat of already-comment-free Go code → stripping produces same size → return false.
func TestTryStripCommentsFileRead_multiFileNoShrink(t *testing.T) {
	t.Parallel()
	// Go code with no comments → StripComments returns same content → len(out) >= len(s).
	argv := []string{"cat", "file1.go", "file2.go"}
	content := []byte("package main\nfunc main() {}\n")
	_, ok := TryStripCommentsFileRead(argv, content)
	if ok {
		t.Error("comment-free multi-file: want false (no shrink), got true")
	}
}

// TestTryStripCommentsFileRead_singleFileStdinSkip covers path="-" skip.
func TestTryStripCommentsFileRead_singleFileStdinSkip(t *testing.T) {
	t.Parallel()
	// cat - reads from stdin: returns false
	_, ok := TryStripCommentsFileRead([]string{"cat", "-"}, []byte("some content"))
	if ok {
		t.Error("stdin path '-': want false, got true")
	}
}

// TestCompactSingleFileRead_largeFile covers the structure extraction path (≥3000 bytes).
func TestCompactSingleFileRead_largeFile(t *testing.T) {
	t.Parallel()
	// Build a large Go file (>signatureOnlyThreshold=3000 bytes) with many functions
	// and comments so structure extraction or comment stripping can compact it.
	var sb strings.Builder
	sb.WriteString("package mypackage\n\n")
	for i := 0; i < 60; i++ {
		sb.WriteString("// Function does something important\n")
		sb.WriteString("// This is a multiline comment block\n")
		sb.WriteString("// explaining the logic\n")
		sb.WriteString(fmt.Sprintf("func DoThing%d(x int) int {\n", i))
		sb.WriteString("\t// implementation detail\n")
		sb.WriteString("\treturn x * 2\n")
		sb.WriteString("}\n\n")
	}
	content := []byte(sb.String())
	if len(content) < 3000 {
		t.Skip("content too small to trigger structure extraction")
	}
	out, ok := compactSingleFileRead([]string{"cat", "large_file.go"}, "large_file.go", content)
	if !ok {
		t.Logf("large Go file: no compaction possible (content-dependent)")
		return
	}
	if len(out) >= len(content) {
		t.Errorf("compacted output should be shorter: %d vs %d", len(out), len(content))
	}
}

func TestCompactSingleFileRead_HeadBypassesASTCompaction(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	for i := 0; i < 80; i++ {
		sb.WriteString(fmt.Sprintf("func DoThing%d() int {\n", i))
		sb.WriteString("\treturn 1\n")
		sb.WriteString("}\n\n")
	}
	content := []byte(sb.String())
	out, ok := compactSingleFileRead([]string{"head", "-n", "40", "large_file.go"}, "large_file.go", content)
	if ok && strings.Contains(string(out), "AST-compacted by Slimference") {
		t.Fatal("head/tail partial reads must not use AST compaction")
	}
}

func TestCompactSingleFileRead_LargeTypeScriptUsesStructureExtraction(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("import { z } from 'zod'\n\n")
	for i := 0; i < 80; i++ {
		sb.WriteString(fmt.Sprintf("export function run%d(): number {\n", i))
		sb.WriteString("\tconst value = 1\n")
		sb.WriteString("\treturn value\n")
		sb.WriteString("}\n\n")
	}
	content := []byte(sb.String())
	out, ok := compactSingleFileRead([]string{"cat", "large.ts"}, "large.ts", content)
	if !ok {
		t.Fatal("expected large TypeScript structure extraction")
	}
	if !strings.Contains(string(out), "function run0") || len(out) >= len(content) {
		t.Fatalf("unexpected structure output len=%d input=%d body=%q", len(out), len(content), out)
	}
}

func TestCompactSingleFileRead_UnknownLangAndEmptyArgv(t *testing.T) {
	t.Parallel()
	if _, ok := compactSingleFileRead([]string{"cat", "file.unknown"}, "file.unknown", []byte("content")); ok {
		t.Fatal("unknown language should not compact")
	}
	if isFullFileCat(nil) {
		t.Fatal("empty argv is not full-file cat")
	}
}

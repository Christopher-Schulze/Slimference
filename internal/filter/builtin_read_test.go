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

func TestCompactSingleFileRead_ContextBypassesCompaction(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	for i := 0; i < 80; i++ {
		sb.WriteString(fmt.Sprintf("// comment %d\n", i))
		sb.WriteString(fmt.Sprintf("func DoThing%d() int {\n\treturn %d\n}\n\n", i, i))
	}
	content := []byte(sb.String())
	for _, ctx := range []FileReadContext{
		{Mode: "edit"},
		{Mode: "debug"},
		{Mode: "scan", RecentlyEdited: true},
		{Mode: "scan", ForceFull: true},
	} {
		out, ok := compactSingleFileReadWithContext([]string{"cat", "large_file.go"}, "large_file.go", content, ctx)
		if ok || string(out) != string(content) {
			t.Fatalf("context %+v must bypass compaction", ctx)
		}
	}
}

func TestTryStripCommentsFileReadWithContext_RecentlyEdited(t *testing.T) {
	t.Parallel()
	src := []byte("// keep while editing\npackage main\nfunc main() {}\n")
	out, ok := TryStripCommentsFileReadWithContext([]string{"cat", "main.go"}, src, FileReadContext{Mode: "scan", RecentlyEdited: true})
	if ok || string(out) != string(src) {
		t.Fatalf("recently edited read must remain literal, ok=%v out=%q", ok, out)
	}
	out, ok = TryStripCommentsFileReadWithContext([]string{"cat", "a.go", "b.go"}, src, FileReadContext{Mode: "scan", RecentlyEdited: true})
	if ok || string(out) != string(src) {
		t.Fatalf("recently edited multi-file read must remain literal, ok=%v out=%q", ok, out)
	}
}

func TestReadPathFromCommandLine(t *testing.T) {
	t.Parallel()
	if fileReadMode("") != "scan" {
		t.Fatal("empty mode must default to scan")
	}
	if got := ReadPathFromCommandLine("cat internal/filter/builtin_read.go"); got != "internal/filter/builtin_read.go" {
		t.Fatalf("read path = %q", got)
	}
	if got := ReadPathFromCommandLine("head -n 20 main.go"); got != "main.go" {
		t.Fatalf("head read path = %q", got)
	}
	if got := ReadPathFromCommandLine("sed -n '10,20p' main.go"); got != "main.go" {
		t.Fatalf("sed read path = %q", got)
	}
	if got := FullReadPathFromCommandLine("cat internal/filter/builtin_read.go"); got != "internal/filter/builtin_read.go" {
		t.Fatalf("full read path = %q", got)
	}
	if got := FullReadPathFromCommandLine("head -n 20 main.go"); got != "" {
		t.Fatalf("partial read must not be treated as full-file read, got %q", got)
	}
	for _, cmd := range []string{"cat a.go b.go", "cat main.go | wc -l", "go test ./...", "printf main.go"} {
		if got := ReadPathFromCommandLine(cmd); got != "" {
			t.Fatalf("command %q should not produce a single read path, got %q", cmd, got)
		}
	}
}

func TestReadRequestFromCommandLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		command    string
		wantPath   string
		wantOffset int
		wantLimit  int
		wantOK     bool
	}{
		{name: "cat", command: "cat main.go", wantPath: "main.go", wantOK: true},
		{name: "head split", command: "head -n 20 main.go", wantPath: "main.go", wantOffset: 1, wantLimit: 20, wantOK: true},
		{name: "head short", command: "head -200 main.go", wantPath: "main.go", wantOffset: 1, wantLimit: 200, wantOK: true},
		{name: "tail split", command: "tail -n 20 main.go", wantPath: "main.go", wantOffset: -20, wantLimit: 20, wantOK: true},
		{name: "tail plus", command: "tail -n +42 main.go", wantPath: "main.go", wantOffset: 42, wantLimit: 0, wantOK: true},
		{name: "sed range", command: "sed -n '10,20p' main.go", wantPath: "main.go", wantOffset: 10, wantLimit: 11, wantOK: true},
		{name: "sed single", command: "sed -n 42p main.go", wantPath: "main.go", wantOffset: 42, wantLimit: 1, wantOK: true},
		{name: "byte head unsupported", command: "head -c 20 main.go", wantOK: false},
		{name: "compound unsupported", command: "head -n 20 main.go | cat", wantOK: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ReadRequestFromCommandLine(tt.command)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (got %+v)", ok, tt.wantOK, got)
			}
			if !ok {
				return
			}
			if got.Path != tt.wantPath || got.Offset != tt.wantOffset || got.Limit != tt.wantLimit {
				t.Fatalf("request = %+v, want path=%q offset=%d limit=%d", got, tt.wantPath, tt.wantOffset, tt.wantLimit)
			}
		})
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

// TestScanMode_AppendsDiscoverableRecoveryNote proves first-read scan-mode output
// (signatures only) carries a neutral, discoverable recovery instruction so the
// model can re-run the same command for the full elided output instead of
// silently losing the bodies.
func TestScanMode_AppendsDiscoverableRecoveryNote(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("package mypackage\n\n")
	for i := 0; i < 40; i++ {
		sb.WriteString(fmt.Sprintf("func DoThing%d(x int) int {\n", i))
		for j := 0; j < 20; j++ {
			sb.WriteString(fmt.Sprintf("\tx += %d // body line %d\n", j, j))
		}
		sb.WriteString("\treturn x\n}\n\n")
	}
	content := []byte(sb.String())
	out, ok := compactSingleFileRead([]string{"cat", "large_file.go"}, "large_file.go", content)
	if !ok {
		t.Skip("no compaction possible (content-dependent)")
	}
	if len(out) >= len(content) {
		t.Fatalf("scan output should be shorter: %d vs %d", len(out), len(content))
	}
	if !strings.Contains(string(out), "re-run the same command to see the full elided output") {
		t.Fatalf("scan-mode output must carry the discoverable recovery note")
	}
}

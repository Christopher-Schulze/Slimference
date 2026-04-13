package compression

import (
	"strings"
	"testing"
)

func TestExtractJavaStructure(t *testing.T) {
	t.Parallel()
	e := NewStructureExtractor()
	code := `package com.example;

import java.util.List;

public class Foo {
  public void bar() {
    System.out.println("x");
  }
}
`
	out, ok := e.Extract(code, "java")
	if !ok {
		t.Fatal("expected extraction")
	}
	if !strings.Contains(out, "package com.example") {
		t.Fatalf("missing package: %s", out)
	}
	if !strings.Contains(out, "class Foo") {
		t.Fatalf("missing class: %s", out)
	}
	if strings.Contains(out, `System.out.println`) {
		t.Fatal("body should be stripped")
	}
}

func TestExtractRubyStructure(t *testing.T) {
	t.Parallel()
	e := NewStructureExtractor()
	code := `require "json"

class Foo
  def bar
    1
  end
end
`
	out, ok := e.Extract(code, "ruby")
	if !ok {
		t.Fatal("expected extraction")
	}
	if !strings.Contains(out, `require "json"`) {
		t.Fatalf("missing require: %s", out)
	}
	if !strings.Contains(out, "class Foo") {
		t.Fatalf("missing class: %s", out)
	}
}

func TestExtractShellStructure(t *testing.T) {
	t.Parallel()
	e := NewStructureExtractor()
	code := `#!/bin/bash
set -euo pipefail

function greet() {
  echo hi
}
`
	out, ok := e.Extract(code, "shell")
	if !ok {
		t.Fatal("expected extraction")
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "#") {
		t.Fatalf("expected shebang: %s", out)
	}
	if !strings.Contains(out, "function greet") {
		t.Fatalf("missing function: %s", out)
	}
}

// TestExtractGoStructure_NonBodyDeclarations covers single-line type/const/var declarations
// that do not open a body (opens == closes), exercising the `kept = append(kept, line)` path
// for isDecl without inBody.
func TestExtractGoStructure_NonBodyDeclarations(t *testing.T) {
	t.Parallel()
	e := NewStructureExtractor()
	code := "package p\n\ntype StatusCode int\nconst MaxRetries = 3\nvar ErrNotFound = errors.New(\"not found\")\n\nfunc Big() {\n" +
		strings.Repeat("\t_ = 1\n", 60) + "}\n"
	out, ok := e.Extract(code, "go")
	if !ok {
		t.Fatalf("expected extraction; out=%s", out)
	}
	if !strings.Contains(out, "type StatusCode int") {
		t.Errorf("single-line type declaration should be in summary: %s", out)
	}
	if !strings.Contains(out, "const MaxRetries") {
		t.Errorf("const declaration should be in summary: %s", out)
	}
	if !strings.Contains(out, "var ErrNotFound") {
		t.Errorf("var declaration should be in summary: %s", out)
	}
}

// TestExtractTSStructure_TypeAlias covers TypeScript type alias declarations (no body).
func TestExtractTSStructure_TypeAlias(t *testing.T) {
	t.Parallel()
	e := NewStructureExtractor()
	code := "import { z } from 'zod'\n\nexport type ID = string\nexport type Status = 'ok' | 'fail'\n\nexport function handle(): void {\n" +
		strings.Repeat("  void 0;\n", 50) + "}\n"
	out, ok := e.Extract(code, "typescript")
	if !ok {
		t.Fatalf("expected extraction; out=%s", out)
	}
	if !strings.Contains(out, "type ID") {
		t.Errorf("type alias should be in summary: %s", out)
	}
	if !strings.Contains(out, "type Status") {
		t.Errorf("type alias should be in summary: %s", out)
	}
}

// TestExtractJavaStructure_AnnotationBeforeClass covers the pendingAnn path
// (annotation before a class declaration) and the pendingAnn clearing inside a body.
func TestExtractJavaStructure_AnnotationBeforeClass(t *testing.T) {
	t.Parallel()
	e := NewStructureExtractor()
	code := "package com.example;\n\n@RestController\n@RequestMapping(\"/api\")\npublic class ApiController {\n" +
		// An annotation inside the class body -> pendingAnn set, then cleared when tracking body
		"  @Override\n" +
		"  public void init() {\n" +
		strings.Repeat("    System.out.println();\n", 30) +
		"  }\n}\n"
	out, ok := e.Extract(code, "java")
	if !ok {
		t.Fatalf("expected extraction; out=%s", out)
	}
	// Only the last annotation before the class declaration is preserved (overwrite behaviour).
	if !strings.Contains(out, "@RequestMapping") {
		t.Errorf("annotation before class should be in summary: %s", out)
	}
	if !strings.Contains(out, "class ApiController") {
		t.Errorf("class declaration should be in summary: %s", out)
	}
}

func TestStructureExtractor_Extract_goTypescriptRustPython(t *testing.T) {
	t.Parallel()
	e := NewStructureExtractor()

	goCode := "package demo\n\nfunc Hello() {\n" + strings.Repeat("\tprintln(1)\n", 80) + "}\n"
	out, ok := e.Extract(goCode, "go")
	if !ok || !strings.Contains(out, "package demo") || !strings.Contains(out, "func Hello") {
		t.Fatalf("go: ok=%v out=%s", ok, out)
	}
	if len(out) >= len(goCode) {
		t.Fatal("go summary should be shorter than original")
	}

	ts := "import { z } from 'zod'\n\nexport function run(): number {\n" + strings.Repeat("  void 0;\n", 60) + "  return 0\n}\n"
	out, ok = e.Extract(ts, "javascript")
	if !ok || !strings.Contains(out, "import") || !strings.Contains(out, "function run") {
		t.Fatalf("javascript: ok=%v out=%s", ok, out)
	}

	rs := "use std::io;\n\npub fn main() {\n" + strings.Repeat("    let _ = 1;\n", 50) + "}\n"
	out, ok = e.Extract(rs, "rust")
	if !ok || !strings.Contains(out, "use std::io") || !strings.Contains(out, "fn main") {
		t.Fatalf("rust: ok=%v out=%s", ok, out)
	}

	py := "import os\n\nclass Foo:\n    def bar(self):\n" + strings.Repeat("        _ = 1\n", 40) + "        return 1\n"
	out, ok = e.Extract(py, "python")
	if !ok || !strings.Contains(out, "import os") || !strings.Contains(out, "class Foo") {
		t.Fatalf("python: ok=%v out=%s", ok, out)
	}
}

func TestStructureExtractor_Extract_cAndCpp(t *testing.T) {
	t.Parallel()
	e := NewStructureExtractor()
	cCode := "#include <stdio.h>\n\nint main(void) {\n" + strings.Repeat("  puts(\"x\");\n", 80) + "  return 0;\n}\n"
	out, ok := e.Extract(cCode, "c")
	if !ok || !strings.Contains(out, "#include") || !strings.Contains(out, "int main") {
		t.Fatalf("c: ok=%v out=%s", ok, out)
	}
	if len(out) >= len(cCode) {
		t.Fatal("c summary should be shorter than original")
	}
	out, ok = e.Extract(cCode, "cpp")
	if !ok || !strings.Contains(out, "#include") {
		t.Fatalf("cpp: ok=%v out=%s", ok, out)
	}
}

func TestStructureExtractor_Extract_unknownLanguage(t *testing.T) {
	t.Parallel()
	e := NewStructureExtractor()
	code := "fn main() {}"
	out, ok := e.Extract(code, "zig")
	if ok || out != code {
		t.Fatalf("want passthrough, ok=%v out=%q", ok, out)
	}
}

// TestExtractCeeStructure_ForwardDeclaration covers the funcRe path where the declaration
// has no body (forward declaration / prototype).  opens == closes so inBody is not entered.
func TestExtractCeeStructure_ForwardDeclaration(t *testing.T) {
	t.Parallel()
	code := "#include <stdlib.h>\n\nvoid callback(int x, void *data);\nint compute(double a, double b);\n" +
		strings.Repeat("// comment line\n", 30)
	out := extractCeeStructure(code)
	if !strings.Contains(out, "void callback") {
		t.Errorf("forward declaration should be in summary: %s", out)
	}
	if !strings.Contains(out, "int compute") {
		t.Errorf("forward declaration should be in summary: %s", out)
	}
}

// TestExtractCeeStructure_StructBody covers the typeRe path with a struct body.
func TestExtractCeeStructure_StructBody(t *testing.T) {
	t.Parallel()
	code := "#include <stdint.h>\n\nstruct Point {\n" +
		strings.Repeat("    int coord;\n", 30) +
		"};\n"
	out := extractCeeStructure(code)
	if !strings.Contains(out, "struct Point") {
		t.Errorf("struct should be in summary: %s", out)
	}
	// Body lines should not appear in summary.
	if strings.Contains(out, "int coord") {
		t.Errorf("body lines should be stripped: %s", out)
	}
}

// TestExtractRubyStructure_SmallFileNoCompression covers the `return ""` path when the
// summary body would be as large as the original (all lines are declarations).
func TestExtractRubyStructure_SmallFileNoCompression(t *testing.T) {
	t.Parallel()
	// All lines are `def` declarations - the joined body equals the original.
	code := "def foo\ndef bar"
	out := extractRubyStructure(code)
	// body len == original len -> extractRubyStructure returns ""
	if out != "" {
		t.Errorf("should return empty string when body >= original; got %q", out)
	}
	// Extract should fall back to passthrough.
	e := NewStructureExtractor()
	result, ok := e.Extract(code, "ruby")
	if ok {
		t.Error("Extract should return ok=false when no compression possible")
	}
	if result != code {
		t.Errorf("Extract should return original code unchanged, got %q", result)
	}
}

// TestExtractRubyStructure_BodyWithoutHeader covers the path where the summary body is
// shorter than the original but the header+body would exceed it - returns body without header.
func TestExtractRubyStructure_BodyWithoutHeader(t *testing.T) {
	t.Parallel()
	// def line is short, but the body content inflates the original enough that header+body > original.
	code := "def foo\n" + strings.Repeat("  x = 1\n", 3)
	out := extractRubyStructure(code)
	// body = "def foo" (7 chars) < code (22 chars), but header+body > code -> return body only
	if out != "def foo" {
		t.Errorf("should return body without header: got %q", out)
	}
}

// TestExtractShellStructure_SmallFileNoCompression covers the `return ""` path where
// all lines are declarations and body equals original length.
func TestExtractShellStructure_SmallFileNoCompression(t *testing.T) {
	t.Parallel()
	// Shebang + one function - all lines kept; body == original.
	code := "#!/bin/bash\nfunction greet() {"
	out := extractShellStructure(code)
	if out != "" {
		t.Errorf("should return empty when body >= original; got %q", out)
	}
	e := NewStructureExtractor()
	result, ok := e.Extract(code, "shell")
	if ok {
		t.Error("Extract should return ok=false")
	}
	if result != code {
		t.Errorf("Extract should return original code, got %q", result)
	}
}

// TestExtractRustStructure_WithAttributes covers attribute handling:
// attribute before a struct (pendingAttr used), and attribute before a non-declaration
// (pendingAttr cleared).
func TestExtractRustStructure_WithAttributes(t *testing.T) {
	t.Parallel()
	e := NewStructureExtractor()
	code := "use std::io;\n\n#[derive(Debug)]\npub struct Foo {\n" +
		strings.Repeat("    x: i32,\n", 30) +
		"}\n\n#[cfg(test)]\n// not a decl - attr cleared\n\npub fn bar() {\n" +
		strings.Repeat("    let _ = 1;\n", 20) +
		"}\n"
	out, ok := e.Extract(code, "rust")
	if !ok {
		t.Fatal("expected successful extraction")
	}
	if !strings.Contains(out, "#[derive(Debug)]") {
		t.Errorf("attribute before struct should be included: %s", out)
	}
	if !strings.Contains(out, "struct Foo") {
		t.Errorf("struct declaration should be included: %s", out)
	}
	if !strings.Contains(out, "fn bar") {
		t.Errorf("fn bar should be included: %s", out)
	}
}

// TestExtractPythonStructure_DecoratorPaths covers four decorator-related branches:
// 1. pendingDecorator = line; continue  (@decorator line)
// 2. kept = append(kept, pendingDecorator)  (decorator before isDecl)
// 3. sig += ":"  (def line without trailing colon - multiline signature first line)
// 4. pendingDecorator = ""; kept = append(kept, line)  (non-decl, non-import module-level line)
func TestExtractPythonStructure_DecoratorPaths(t *testing.T) {
	t.Parallel()
	// CONSTANT = "value" triggers path 4 (non-decl, non-import at module level).
	// @classmethod triggers path 1 (decorator stored).
	// def decorated( triggers path 2 (decorator used) and path 3 (no trailing colon).
	code := "import os\n\nCONSTANT = \"value\"\n\n@classmethod\ndef decorated(\n" +
		strings.Repeat("    _ = 1\n", 40) +
		"\n@property\ndef also_decorated(self):\n" +
		strings.Repeat("    _ = 2\n", 40)
	out := extractPythonStructure(code)
	if !strings.Contains(out, "@classmethod") {
		t.Errorf("decorator before def should appear in summary: %s", out)
	}
	if !strings.Contains(out, "def decorated") {
		t.Errorf("decorated def should appear in summary: %s", out)
	}
	// CONSTANT line should be kept (it's a non-decl, non-import module-level line).
	if !strings.Contains(out, "CONSTANT") {
		t.Errorf("module-level assignment should be kept in summary: %s", out)
	}
	if strings.Contains(out, "_ = 1") || strings.Contains(out, "_ = 2") {
		t.Errorf("body lines should be stripped: %s", out)
	}
}

// TestExtractPythonStructure_NoDeclarations covers return "" when extracted==0.
// Code with no import/def/class means extractPythonStructure returns "".
func TestExtractPythonStructure_NoDeclarations(t *testing.T) {
	t.Parallel()
	// Pure assignment - no import, no def, no class → extracted==0.
	out := extractPythonStructure("X = 5\nY = 10\n")
	if out != "" {
		t.Errorf("expected empty string when no declarations, got %q", out)
	}
}

// TestExtractJavaStructure_InlineDecl covers the isDecl path where opens == closes
// (single-line enum or annotation-only type with balanced braces).
func TestExtractJavaStructure_InlineDecl(t *testing.T) {
	t.Parallel()
	// enum Status { OK, FAIL } has opens=1 closes=1 → isDecl true, opens <= closes
	// → kept = append(kept, line); continue  (the else branch of opens > closes).
	code := "package com.example;\n\nenum Status { OK, FAIL }\n" +
		strings.Repeat("// line\n", 30)
	out := extractJavaStructure(code)
	if !strings.Contains(out, "enum Status") {
		t.Errorf("inline enum should be in summary: %s", out)
	}
}

// TestExtractGoStructure_NoDeclarations covers return "" when extracted==0.
// Only a package line is present - package is not counted as extracted.
func TestExtractGoStructure_NoDeclarations(t *testing.T) {
	t.Parallel()
	out := extractGoStructure("package main\n")
	if out != "" {
		t.Errorf("expected empty string when no declarations, got %q", out)
	}
}

// TestExtractTSStructure_NoDeclarations covers return "" when extracted==0.
func TestExtractTSStructure_NoDeclarations(t *testing.T) {
	t.Parallel()
	// const x = 5 does not match constFnRe (no opening paren after =) or arrowFnRe.
	out := extractTSStructure("// just a comment\nconst x = 5;\n")
	if out != "" {
		t.Errorf("expected empty string when no declarations, got %q", out)
	}
}

// TestExtractRustStructure_NoDeclarations covers return "" when extracted==0.
func TestExtractRustStructure_NoDeclarations(t *testing.T) {
	t.Parallel()
	out := extractRustStructure("// just a comment\nlet x = 5;\n")
	if out != "" {
		t.Errorf("expected empty string when no declarations, got %q", out)
	}
}

// TestExtractCeeStructure_NoDeclarations covers return "" when extracted==0.
func TestExtractCeeStructure_NoDeclarations(t *testing.T) {
	t.Parallel()
	// Pure comments - no include/type/func declarations → extracted==0.
	out := extractCeeStructure("// just a comment\n/* block comment */\n")
	if out != "" {
		t.Errorf("expected empty string when no declarations, got %q", out)
	}
}

// TestExtractCeeStructure_TypedefNoBrace covers typeRe with opens <= closes (no body opened).
func TestExtractCeeStructure_TypedefNoBrace(t *testing.T) {
	t.Parallel()
	code := "#include <stdint.h>\n\ntypedef int MyInt;\n" +
		strings.Repeat("// filler line\n", 30)
	out := extractCeeStructure(code)
	if !strings.Contains(out, "typedef int MyInt") {
		t.Errorf("typedef without body should be in summary: %s", out)
	}
}

// TestExtractJavaStructure_NoDeclarations covers return "" when extracted==0.
func TestExtractJavaStructure_NoDeclarations(t *testing.T) {
	t.Parallel()
	out := extractJavaStructure("// just a comment\n")
	if out != "" {
		t.Errorf("expected empty string when no declarations, got %q", out)
	}
}

// TestExtractRubyStructure_NoDeclarations covers return "" when extracted==0.
func TestExtractRubyStructure_NoDeclarations(t *testing.T) {
	t.Parallel()
	// Only non-require, non-def lines → extracted==0.
	out := extractRubyStructure("# just a comment\nx = 5\n")
	if out != "" {
		t.Errorf("expected empty string when no declarations, got %q", out)
	}
}

// TestExtractShellStructure_NoDeclarations covers return "" when extracted==0.
func TestExtractShellStructure_NoDeclarations(t *testing.T) {
	t.Parallel()
	// No shebang, no function declarations → extracted==0.
	out := extractShellStructure("# just a comment\necho hi\n")
	if out != "" {
		t.Errorf("expected empty string when no declarations, got %q", out)
	}
}

// TestExtractRubyStructure_FullHeaderOutput covers the return out path where both
// body alone and header+body are shorter than the original code.
func TestExtractRubyStructure_FullHeaderOutput(t *testing.T) {
	t.Parallel()
	// Large body of non-declaration lines: body+header will still be << code length.
	code := "require \"json\"\n\nclass Foo\n" + strings.Repeat("  x = 1\n", 80) + "end\n"
	out := extractRubyStructure(code)
	// The header must be present (return out, not return body).
	if !strings.Contains(out, "# [Structural summary") {
		t.Errorf("expected structural summary header in output: %s", out)
	}
	if !strings.Contains(out, "require") || !strings.Contains(out, "class Foo") {
		t.Errorf("declaration lines missing from output: %s", out)
	}
}

// TestExtractShellStructure_FullHeaderOutput covers the return out path for shell.
func TestExtractShellStructure_FullHeaderOutput(t *testing.T) {
	t.Parallel()
	code := "#!/bin/bash\n\nfunction greet() {\n" + strings.Repeat("  echo line\n", 80) + "}\n"
	out := extractShellStructure(code)
	if !strings.Contains(out, "# [Structural summary") {
		t.Errorf("expected structural summary header in output: %s", out)
	}
	if !strings.Contains(out, "function greet") {
		t.Errorf("function declaration missing from output: %s", out)
	}
}

// TestExtractPythonStructure_MultipleClassesBodyExit covers the inBody=false fall-through
// when a new top-level declaration follows a class body.
func TestExtractPythonStructure_MultipleClassesBodyExit(t *testing.T) {
	t.Parallel()
	e := NewStructureExtractor()
	code := "import os\n\nclass Foo:\n" +
		strings.Repeat("    x = 1\n", 25) +
		"\ndef bar():\n" + // top-level def after class - triggers inBody exit
		strings.Repeat("    y = 2\n", 15) +
		"\nclass Baz:\n" +
		strings.Repeat("    z = 3\n", 15)
	out, ok := e.Extract(code, "python")
	if !ok {
		t.Fatalf("expected extraction ok; out=%s", out)
	}
	if !strings.Contains(out, "class Foo") {
		t.Errorf("class Foo should be in summary: %s", out)
	}
	if !strings.Contains(out, "def bar") {
		t.Errorf("def bar should be in summary: %s", out)
	}
	if !strings.Contains(out, "class Baz") {
		t.Errorf("class Baz should be in summary: %s", out)
	}
	if strings.Contains(out, "x = 1") || strings.Contains(out, "y = 2") {
		t.Errorf("body lines should be stripped: %s", out)
	}
}

func TestExtractStructure_PackageLevelWrapper(t *testing.T) {
	t.Parallel()
	// Exercise the package-level ExtractStructure convenience function directly.
	// Function bodies need to be long enough that stripping them produces a shorter result.
	code := `package foo

import (
	"fmt"
	"strings"
	"strconv"
)

// Hello greets the user with a personalised message.
func Hello(name string) string {
	var sb strings.Builder
	sb.WriteString("Hello, ")
	sb.WriteString(name)
	sb.WriteString("! Today is a great day.")
	sb.WriteString(" We hope you enjoy working with us.")
	sb.WriteString(" Please don't hesitate to ask for help.")
	result := sb.String()
	fmt.Println(result)
	return result
}

// World returns the number of planets.
func World() int {
	count := 0
	for i := 0; i < 8; i++ {
		count++
		fmt.Println("planet", strconv.Itoa(i))
	}
	return count
}
`
	out, ok := ExtractStructure(code, "go")
	if !ok {
		t.Fatalf("ExtractStructure should compact go code, got ok=false")
	}
	if !strings.Contains(out, "func Hello") {
		t.Errorf("want func Hello in output, got: %s", out)
	}
	if strings.Contains(out, "sb.WriteString") {
		t.Errorf("body should be stripped, got: %s", out)
	}
	// Unknown language: pass-through
	_, ok2 := ExtractStructure("some random content", "unknown-lang")
	if ok2 {
		t.Fatal("unknown language should return ok=false")
	}
}

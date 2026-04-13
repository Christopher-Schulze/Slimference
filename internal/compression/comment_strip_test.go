package compression

import (
	"strings"
	"testing"
)

// TestStripComments exercises stripComments across languages and edge cases.
// TestStripCStyleComments_multiLineBlock covers the case where a /* */ block comment spans
// three or more lines, exercising the `if end == -1 { continue }` branch inside the loop.
func TestStripCStyleComments_multiLineBlock(t *testing.T) {
	t.Parallel()
	code := "int x = 1;\n/* line 1\n   line 2\n   line 3 */\nint y = 2;\n"
	got := StripComments(code, "go")
	if strings.Contains(got, "line 1") || strings.Contains(got, "line 2") || strings.Contains(got, "line 3") {
		t.Errorf("multi-line block comment lines should be stripped: %q", got)
	}
	if !strings.Contains(got, "int x") || !strings.Contains(got, "int y") {
		t.Errorf("code lines around comment should be preserved: %q", got)
	}
}

func TestStripComments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		lang         string
		input        string
		wantContains string
		wantAbsent   string
		wantLen      string // "shorter", "same", "any"
	}{
		{
			name: "Go code with // comments",
			lang: "go",
			input: `package main

// This is a comment
func main() {
	// another comment
	x := 1 // inline comment
}`,
			wantAbsent: "// This is a comment",
			wantLen:    "shorter",
		},
		{
			name: "Go code with block comment",
			lang: "go",
			input: `package main

/* This is a
   multi-line block comment */
func main() {}`,
			wantAbsent: "This is a",
			wantLen:    "shorter",
		},
		{
			name: "Python code with # comments",
			lang: "python",
			input: `# top-level comment
def foo():
    # inner comment
    return 42  # inline`,
			wantAbsent: "# top-level comment",
			wantLen:    "shorter",
		},
		{
			name: "Python triple-quoted docstring stripped",
			lang: "python",
			input: `def foo():
    """doc
    line2"""
    return 1`,
			wantAbsent: "doc",
			wantLen:    "shorter",
		},
		{
			name: "string containing // preserved",
			lang: "go",
			input: `package main

func example() string {
	return "https://example.com/path"
}`,
			wantContains: "https://example.com/path",
			wantLen:      "any",
		},
		{
			name: "multiple blank lines normalized to one",
			lang: "go",
			input: `func a() {}



func b() {}`,
			wantLen: "shorter",
		},
		{
			name: "C strip //",
			lang: "c",
			input: `int main(void) {
	// comment
	return 0;
}`,
			wantAbsent: "// comment",
			wantLen:    "shorter",
		},
		{
			name: "Ruby strip #",
			lang: "ruby",
			input: `# frozen_string_literal: true
def foo
  # inner
  42
end`,
			wantAbsent: "# frozen_string_literal",
			wantLen:    "shorter",
		},
		{
			name: "unknown language returns input",
			lang: "brainfuck",
			input: `+++++++>
<------
// definitely not stripped`,
			wantContains: "// definitely not stripped",
			wantLen:      "same",
		},
		{
			name: "HTML strip <!-- -->",
			lang: "html",
			input: `<div>
<!-- sidebar -->
<p>hi</p>
</div>`,
			wantAbsent: "sidebar",
			wantLen:    "shorter",
		},
		{
			name: "CSS strip /* */",
			lang: "css",
			input: `.x { color: red; }
/* legacy */
.y { margin: 0; }`,
			wantAbsent: "legacy",
			wantLen:    "shorter",
		},
		{
			name: "YAML hash in quoted string preserved",
			lang: "yaml",
			input: `k: "foo#bar"
# real comment`,
			wantContains: "foo#bar",
			wantAbsent:   "real comment",
			wantLen:      "shorter",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := StripComments(tc.input, tc.lang)

			switch tc.wantLen {
			case "shorter":
				if len(got) >= len(tc.input) {
					t.Errorf("expected output shorter than input (%d), got len %d\noutput: %q", len(tc.input), len(got), got)
				}
			case "same":
				if got != tc.input {
					t.Errorf("expected output unchanged, got %q", truncate(got, 120))
				}
			}

			if tc.wantContains != "" && !strings.Contains(got, tc.wantContains) {
				t.Errorf("output missing expected %q\noutput: %q", tc.wantContains, truncate(got, 200))
			}
			if tc.wantAbsent != "" && strings.Contains(got, tc.wantAbsent) {
				t.Errorf("output should not contain %q\noutput: %q", tc.wantAbsent, truncate(got, 200))
			}
		})
	}
}

// TestStripComments_MultipleBlankLinesNormalized verifies that >1 consecutive blank lines
// become exactly 1 blank line after stripping.
func TestStripComments_MultipleBlankLinesNormalized(t *testing.T) {
	t.Parallel()

	input := "func a() {}\n\n\n\n\nfunc b() {}"
	got := StripComments(input, "go")
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("three or more consecutive newlines present in output: %q", got)
	}
}

func TestLanguageFromPath(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"pkg/foo.go":          "go",
		"app.tsx":             "typescript",
		"x.jsx":               "javascript",
		"b.mjs":               "javascript",
		"lib.rs":              "rust",
		"a.py":                "python",
		"style.scss":          "css",
		"page.html":           "html",
		"legacy.htm":          "html",
		"cfg.yaml":            "yaml",
		"deploy.yml":          "yaml",
		"config.toml":         "toml",
		"src/main.cpp":        "cpp",
		"Bean.java":           "java",
		"file.unknownext":     "",
	}
	for path, want := range tests {
		if got := LanguageFromPath(path); got != want {
			t.Errorf("%q: got %q want %q", path, got, want)
		}
	}
}

func TestNormalizeBlankLines(t *testing.T) {
	t.Parallel()
	out := normalizeBlankLines("line1\n\n\n\nline2")
	if strings.Contains(out, "\n\n\n") {
		t.Fatalf("collapsed: %q", out)
	}
}

func TestStripComments_emptyLangPassthrough(t *testing.T) {
	t.Parallel()
	in := "// drop\nx"
	if got := StripComments(in, ""); got != in {
		t.Fatalf("empty lang: got %q want %q", got, in)
	}
}

func TestStripComments_unknownLangPassthrough(t *testing.T) {
	t.Parallel()
	in := "// keep\nx"
	if got := StripComments(in, "unknown-language-xyz"); got != in {
		t.Fatalf("unknown lang: got %q want %q", got, in)
	}
}

// TestLanguageFromPath_extended covers extensions not exercised by TestLanguageFromPath.
func TestLanguageFromPath_extended(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"index.ts":    "typescript",
		"utils.js":    "javascript",
		"mod.cjs":     "javascript",
		"lib.c":       "c",
		"header.h":    "c",
		"module.cc":   "cpp",
		"impl.cxx":    "cpp",
		"api.hpp":     "cpp",
		"types.hxx":   "cpp",
		"model.rb":    "ruby",
		"run.sh":      "shell",
		"run.bash":    "shell",
		"profile.zsh": "shell",
		"main.css":    "css",
		"theme.sass":  "css",
	}
	for path, want := range tests {
		path, want := path, want
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			if got := LanguageFromPath(path); got != want {
				t.Errorf("got %q want %q", got, want)
			}
		})
	}
}

// TestStripCStyleLine_inlineBlockComment verifies that /* ... */ on a single line is removed
// and code after the block comment is kept.
func TestStripCStyleLine_inlineBlockComment(t *testing.T) {
	t.Parallel()
	var inBlock bool
	line := `code /* inline comment */ more_code`
	got := stripCStyleLine(line, &inBlock)
	if strings.Contains(got, "inline comment") {
		t.Errorf("inline block comment should be stripped: %q", got)
	}
	if !strings.Contains(got, "more_code") {
		t.Errorf("code after inline block comment should be kept: %q", got)
	}
	if inBlock {
		t.Error("inBlock should be false after same-line block comment")
	}
}

// TestStripCStyleLine_escapeInString verifies that \" inside a string is handled and the
// real // comment after the closing quote is stripped.
func TestStripCStyleLine_escapeInString(t *testing.T) {
	t.Parallel()
	var inBlock bool
	// The \" is an escaped quote inside the string; the // after the closing " is a real comment.
	line := `x := "foo\"bar" // strip me`
	got := stripCStyleLine(line, &inBlock)
	if strings.Contains(got, "strip me") {
		t.Errorf("// comment after string should be stripped: %q", got)
	}
	if !strings.Contains(got, `foo\"bar`) {
		t.Errorf("escaped content in string must be preserved: %q", got)
	}
}

// TestStripHTMLComments_unclosed verifies that an unclosed <!-- does not panic and
// preserves the rest of the content as-is.
func TestStripHTMLComments_unclosed(t *testing.T) {
	t.Parallel()
	input := `<div><!-- unclosed comment`
	got := stripHTMLComments(input)
	if !strings.Contains(got, "<!--") {
		t.Errorf("unclosed HTML comment should be preserved verbatim: %q", got)
	}
}

// TestStripCSSComments_unclosed verifies that an unclosed /* does not panic.
func TestStripCSSComments_unclosed(t *testing.T) {
	t.Parallel()
	input := `.x { color: red; } /* unclosed`
	got := stripCSSComments(input)
	if !strings.Contains(got, "/*") {
		t.Errorf("unclosed CSS comment should be preserved verbatim: %q", got)
	}
}

// TestStripTripleQuotes_oddCount verifies that an odd number of triple-quote delimiters
// leaves the input unchanged (conservative fallback).
func TestStripTripleQuotes_oddCount(t *testing.T) {
	t.Parallel()
	input := `x = """only one opening`
	got := stripTripleQuotes(input, `"""`)
	if got != input {
		t.Errorf("odd-count triple-quote: got %q want original unchanged", got)
	}
}

// TestStripHashLine_escapeInString verifies that a backslash-escaped character inside a
// string does not confuse the comment scanner, and the real # after the string IS stripped.
func TestStripHashLine_escapeInString(t *testing.T) {
	t.Parallel()
	// \" inside the string triggers the escape path; the # after the closing quote is a comment.
	line := `x = "foo\"bar"  # real comment`
	got := stripHashLine(line)
	if strings.Contains(got, "real comment") {
		t.Errorf("comment after string should be stripped: %q", got)
	}
	if !strings.Contains(got, `foo\"bar`) {
		t.Errorf("escaped content inside string must be preserved: %q", got)
	}
}

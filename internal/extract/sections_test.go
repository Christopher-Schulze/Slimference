package extract

import (
	"strings"
	"testing"
)

func TestParseSections_PlainProse(t *testing.T) {
	in := "This is a plain paragraph. It has two sentences."
	got := parseSections(in)
	if len(got) != 1 || got[0].Kind != SectionProse {
		t.Fatalf("expected 1 prose section, got %+v", got)
	}
}

func TestParseSections_FencedCode(t *testing.T) {
	in := "before\n\n```go\nfunc x() {}\n```\n\nafter\n"
	got := parseSections(in)
	if len(got) < 3 {
		t.Fatalf("expected at least 3 sections, got %d (%+v)", len(got), got)
	}
	var foundCode bool
	for _, s := range got {
		if s.Kind == SectionCode && strings.Contains(s.Content, "func x()") {
			foundCode = true
		}
	}
	if !foundCode {
		t.Fatalf("expected SectionCode containing func x(), got %+v", got)
	}
}

func TestParseSections_TildeFence(t *testing.T) {
	in := "~~~\nraw\n~~~\n"
	got := parseSections(in)
	if len(got) != 1 || got[0].Kind != SectionCode {
		t.Fatalf("expected 1 code section, got %+v", got)
	}
}

func TestParseSections_UnterminatedCodeFence(t *testing.T) {
	in := "```\nhanging\nmore\n"
	got := parseSections(in)
	if len(got) != 1 || got[0].Kind != SectionCode {
		t.Fatalf("unterminated fence should still produce code section, got %+v", got)
	}
}

func TestParseSections_IndentedCode(t *testing.T) {
	in := "intro paragraph here.\n\n    code line\n    another\n\ntrailing\n"
	got := parseSections(in)
	var found bool
	for _, s := range got {
		if s.Kind == SectionCode && strings.Contains(s.Content, "code line") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected indented code section, got %+v", got)
	}
}

func TestParseSections_Header(t *testing.T) {
	in := "# Title\n\ncontent\n"
	got := parseSections(in)
	if got[0].Kind != SectionHeader {
		t.Fatalf("expected first section header, got %+v", got)
	}
}

func TestParseSections_AllHeaderDepths(t *testing.T) {
	for _, h := range []string{"#", "##", "###", "####", "#####", "######"} {
		in := h + " H\n"
		got := parseSections(in)
		if got[0].Kind != SectionHeader {
			t.Fatalf("header depth %q not recognised", h)
		}
	}
}

func TestParseSections_NotAHeaderTooManyHashes(t *testing.T) {
	got := parseSections("####### nope\n")
	if got[0].Kind == SectionHeader {
		t.Fatalf("7+ hashes must not parse as header")
	}
}

func TestParseSections_ListBulleted(t *testing.T) {
	in := "- one\n- two\n- three\n"
	got := parseSections(in)
	if got[0].Kind != SectionList {
		t.Fatalf("expected list, got %+v", got)
	}
}

func TestParseSections_ListNumbered(t *testing.T) {
	in := "1. one\n2. two\n3. three\n"
	got := parseSections(in)
	if got[0].Kind != SectionList {
		t.Fatalf("expected numbered list, got %+v", got)
	}
}

func TestParseSections_ListPlus(t *testing.T) {
	in := "+ one\n+ two\n"
	got := parseSections(in)
	if got[0].Kind != SectionList {
		t.Fatalf("+ bullets must parse as list")
	}
}

func TestParseSections_BlankLineSeparator(t *testing.T) {
	in := "para1\n\n\npara2\n"
	got := parseSections(in)
	var blanks int
	for _, s := range got {
		if s.Kind == SectionBlank {
			blanks++
		}
	}
	if blanks == 0 {
		t.Fatalf("expected at least one blank section, got %+v", got)
	}
}

func TestParseSections_CRLF(t *testing.T) {
	in := "line1\r\nline2\r\n"
	got := parseSections(in)
	if len(got) == 0 || got[0].Kind != SectionProse {
		t.Fatalf("CRLF must canonicalise to LF, got %+v", got)
	}
}

func TestParseSections_NoTrailingNewlinePreserved(t *testing.T) {
	in := "no trailing"
	got := parseSections(in)
	if len(got) != 1 || strings.HasSuffix(got[0].Content, "\n") {
		t.Fatalf("trailing newline preservation violated: %q", got[0].Content)
	}
}

func TestParseSections_LossyConcatPreservesBytes(t *testing.T) {
	// Inputs WITH trailing newline should be byte-identical after
	// parseSections+concat (the parser is lossless on newline-terminated
	// inputs).
	in := "# A\n\nparagraph.\n\n```go\nx := 1\n```\n\n- list\n- item\n"
	got := parseSections(in)
	var rebuilt strings.Builder
	for _, s := range got {
		rebuilt.WriteString(s.Content)
	}
	if rebuilt.String() != in {
		t.Fatalf("lossless round-trip failed\nwant: %q\ngot:  %q", in, rebuilt.String())
	}
}

func TestParseSections_ListContinuationStaysInList(t *testing.T) {
	in := "- item\n  continued line\n  more\n"
	got := parseSections(in)
	// The list section should swallow the indented continuation lines
	// rather than starting an indented-code section.
	codeCount := 0
	for _, s := range got {
		if s.Kind == SectionCode {
			codeCount++
		}
	}
	if codeCount > 0 {
		t.Fatalf("list continuation lines must not become code sections, got %+v", got)
	}
}

func TestParseSections_EmptyInput(t *testing.T) {
	got := parseSections("")
	// "".Split("\n") yields [""] → one blank section.
	if len(got) != 1 || got[0].Kind != SectionBlank {
		t.Fatalf("empty input should yield one blank section, got %+v", got)
	}
}

func TestCodeFence(t *testing.T) {
	if codeFence("```go") != "```" {
		t.Fatalf("``` fence not detected")
	}
	if codeFence("~~~go") != "~~~" {
		t.Fatalf("~~~ fence not detected")
	}
	if codeFence("    code") != "" {
		t.Fatalf("indented line must not be fence")
	}
}

func TestIsIndentedCodeLine(t *testing.T) {
	cases := map[string]bool{
		"\tcode":         true,
		"    code":       true,
		"  short":        false,
		"":               false,
		"   3 spaces":    false,
		"\t\tdouble tab": true,
	}
	for in, want := range cases {
		if got := isIndentedCodeLine(in); got != want {
			t.Errorf("isIndentedCodeLine(%q)=%v want %v", in, got, want)
		}
	}
}

func TestIsHeaderLine(t *testing.T) {
	cases := map[string]bool{
		"":         false,
		"# h":      true,
		"## h":     true,
		"###### h": true,
		"#######":  false, // too many
		"#nospace": false,
		"#":        false, // nothing after
		"#\th":     true,  // tab after # is allowed
		"text":     false,
	}
	for in, want := range cases {
		if got := isHeaderLine(in); got != want {
			t.Errorf("isHeaderLine(%q)=%v want %v", in, got, want)
		}
	}
}

func TestIsListLine(t *testing.T) {
	cases := map[string]bool{
		"- item":     true,
		"  * item":   true,
		"+ item":     true,
		"1. one":     true,
		"42. forty":  true,
		"   3. ok":   true,
		"hello":      false,
		"":           false,
		"-no-space":  false,
		"1.no-space": false,
	}
	for in, want := range cases {
		if got := isListLine(in); got != want {
			t.Errorf("isListLine(%q)=%v want %v", in, got, want)
		}
	}
}

func TestIsListContinuation(t *testing.T) {
	cases := map[string]bool{
		"  cont":     true,
		"\tcont":     true,
		"":           false,
		"   ":        false,
		"plain text": false,
		"\t  mixed":  true,
	}
	for in, want := range cases {
		if got := isListContinuation(in); got != want {
			t.Errorf("isListContinuation(%q)=%v want %v", in, got, want)
		}
	}
}

func TestHasOpenList(t *testing.T) {
	if hasOpenList(nil) {
		t.Fatalf("nil sections has no open list")
	}
	if hasOpenList([]Section{{Kind: SectionProse}}) {
		t.Fatalf("prose-tail has no open list")
	}
	if !hasOpenList([]Section{{Kind: SectionList}}) {
		t.Fatalf("list-tail must report open list")
	}
}

package extract

import (
	"strings"
	"testing"
)

func TestSplitSentences_Simple(t *testing.T) {
	got := splitSentences("First sentence. Second one. Third!")
	if len(got) != 3 {
		t.Fatalf("expected 3 sentences, got %d: %v", len(got), got)
	}
}

func TestSplitSentences_HandlesAbbreviations(t *testing.T) {
	in := "I use Go, Python, etc. for daily work. The other tools, e.g. Rust, are less common."
	got := splitSentences(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 sentences (etc./e.g. should not split), got %d: %v", len(got), got)
	}
}

func TestSplitSentences_HandlesDecimal(t *testing.T) {
	in := "Pi is 3.14159 approximately. Then we move on."
	got := splitSentences(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 sentences (decimal must not split), got %d: %v", len(got), got)
	}
}

func TestSplitSentences_DoubleNewlineBoundary(t *testing.T) {
	got := splitSentences("Paragraph one\n\nParagraph two")
	if len(got) != 2 {
		t.Fatalf("expected paragraph boundary split, got %v", got)
	}
}

func TestSplitSentences_QuestionAndExclamation(t *testing.T) {
	in := "What? Yes! Indeed."
	got := splitSentences(in)
	if len(got) != 3 {
		t.Fatalf("expected 3 sentences, got %v", got)
	}
}

func TestSplitSentences_EmptyInput(t *testing.T) {
	got := splitSentences("")
	if got != nil {
		t.Fatalf("empty input should yield nil, got %v", got)
	}
}

func TestSplitSentences_WhitespaceOnly(t *testing.T) {
	got := splitSentences("    \n\t   ")
	if got != nil {
		t.Fatalf("whitespace-only should yield nil, got %v", got)
	}
}

func TestSplitSentences_DropsEmpty(t *testing.T) {
	in := ".  .  ."
	got := splitSentences(in)
	for _, s := range got {
		if strings.TrimSpace(s) == "" {
			t.Fatalf("empty sentence in output: %v", got)
		}
	}
}

func TestSplitSentences_EllipsisNotMultiSplit(t *testing.T) {
	// "Hold on... wait." should not produce 4 sentences.
	got := splitSentences("Hold on... wait.")
	if len(got) > 2 {
		t.Fatalf("ellipsis produced too many splits: %v", got)
	}
}

func TestTokenizeWords_DropsPunctuation(t *testing.T) {
	got := tokenizeWords("Hello, World! Test-case 123.")
	want := []string{"hello", "world", "test", "case", "123"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("token %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestTokenizeWords_Empty(t *testing.T) {
	if got := tokenizeWords(""); len(got) != 0 {
		t.Fatalf("empty input should yield empty slice, got %v", got)
	}
}

func TestTokenizeWords_Unicode(t *testing.T) {
	got := tokenizeWords("café naïve")
	if len(got) != 2 || got[0] != "café" || got[1] != "naïve" {
		t.Fatalf("unicode tokens lost: %v", got)
	}
}

func TestPreviousWord(t *testing.T) {
	cases := map[string]string{
		"hello world etc": "etc",
		"some text":       "text",
		"":                "",
		"   ":             "",
		"i.e.":            "i.e",
	}
	for in, want := range cases {
		if got := previousWord(in); got != want {
			t.Errorf("previousWord(%q) = %q, want %q", in, got, want)
		}
	}
}

package extract

import (
	"strings"
	"testing"
)

func TestDefaultConfig_HasReasonableDefaults(t *testing.T) {
	c := DefaultConfig()
	if c.TargetRatio <= 0 || c.TargetRatio > 1 {
		t.Fatalf("TargetRatio default out of range: %v", c.TargetRatio)
	}
	if c.MinSentences < 1 {
		t.Fatalf("MinSentences default must be ≥1")
	}
	if !c.PreserveCodeBlocks || !c.PreserveHeaders || !c.PreserveLists {
		t.Fatalf("preservation defaults must be true")
	}
}

func TestNew_FallsBackToDefaultsForInvalidConfig(t *testing.T) {
	c := New(Config{}) // all zero values
	if c.cfg.TargetRatio == 0 {
		t.Fatalf("zero TargetRatio must fall back to default, got %v", c.cfg.TargetRatio)
	}
	if c.cfg.MinSentences < 1 {
		t.Fatalf("zero MinSentences must fall back to ≥1")
	}
	if !c.cfg.PreserveCodeBlocks {
		t.Fatalf("zero-config must default PreserveCodeBlocks=true")
	}
}

func TestNew_AcceptsExplicitOptOut(t *testing.T) {
	// Explicit opt-out of code preservation with non-default ratio.
	c := New(Config{TargetRatio: 0.5, MinSentences: 2, PreserveCodeBlocks: false})
	if c.cfg.PreserveCodeBlocks {
		t.Fatalf("explicit PreserveCodeBlocks=false should not be flipped")
	}
}

func TestCompactor_PreservesCodeBlock(t *testing.T) {
	c := New(DefaultConfig())
	in := "Some intro prose here. It has multiple sentences. Here's another idea.\n\n```go\nfunc x() { return 1 }\n```\n\nAfter the code."
	out := c.Compact(in)
	if !strings.Contains(out, "func x() { return 1 }") {
		t.Fatalf("code block dropped: %q", out)
	}
}

func TestCompactor_PreservesHeaders(t *testing.T) {
	c := New(DefaultConfig())
	in := "# Section\n\nSome prose. More prose. Even more prose. Final prose.\n"
	out := c.Compact(in)
	if !strings.Contains(out, "# Section") {
		t.Fatalf("header dropped: %q", out)
	}
}

func TestCompactor_PreservesLists(t *testing.T) {
	c := New(DefaultConfig())
	in := "Intro.\n\n- one\n- two\n- three\n\nOutro.\n"
	out := c.Compact(in)
	for _, item := range []string{"- one", "- two", "- three"} {
		if !strings.Contains(out, item) {
			t.Fatalf("list item %q dropped: %q", item, out)
		}
	}
}

func TestCompactor_ProseShrinksByTargetRatio(t *testing.T) {
	c := New(Config{TargetRatio: 0.3, MinSentences: 1})
	in := "First sentence. Second sentence. Third sentence. Fourth sentence. Fifth sentence. Sixth sentence. Seventh sentence. Eighth sentence."
	out := c.Compact(in)
	// At 0.3 of 8 sentences, we keep ~2 sentences. Definitely less
	// than 8.
	gotN := strings.Count(out, ".")
	if gotN >= 8 {
		t.Fatalf("expected fewer than 8 sentences after compaction, got %d in %q", gotN, out)
	}
	if gotN < 1 {
		t.Fatalf("MinSentences=1 violated, got %d", gotN)
	}
}

func TestCompactor_ShortInputPassesThrough(t *testing.T) {
	c := New(DefaultConfig())
	in := "Only one sentence here."
	out := c.Compact(in)
	if out != in {
		t.Fatalf("short input must pass through verbatim\nwant: %q\ngot:  %q", in, out)
	}
}

func TestCompactor_Deterministic(t *testing.T) {
	c := New(DefaultConfig())
	in := "Sentence one is here. Sentence two follows. Sentence three concludes. Plus extra. Many more. Some content."
	a := c.Compact(in)
	b := c.Compact(in)
	if a != b {
		t.Fatalf("non-deterministic output:\n a=%q\n b=%q", a, b)
	}
}

func TestCompactor_KeepsOriginalOrder(t *testing.T) {
	c := New(Config{TargetRatio: 0.5, MinSentences: 1})
	in := "First sentence about apples. Second sentence about bananas. Third sentence about cherries. Fourth sentence about dates."
	out := c.Compact(in)
	// Verify that any preserved sentences appear in their original
	// relative order. We check by finding the relative positions of
	// the fruit names in the output.
	order := []string{"apples", "bananas", "cherries", "dates"}
	last := -1
	for _, fruit := range order {
		idx := strings.Index(out, fruit)
		if idx < 0 {
			continue // dropped — fine
		}
		if idx <= last {
			t.Fatalf("order inversion at %q: idx=%d last=%d in %q", fruit, idx, last, out)
		}
		last = idx
	}
}

func TestCompactor_WithCorpusBoostsCorpusTerms(t *testing.T) {
	// Set up a corpus where "buildkit" is rare. The compactor should
	// favor sentences mentioning that rare term.
	corpus := []string{
		"the system is running smoothly",
		"all tests passed last night",
		"deployment was completed without errors",
	}
	c := New(Config{TargetRatio: 0.5, MinSentences: 1})
	in := "Common boilerplate about the project. Today we shipped buildkit support, which is the rare term. More boilerplate around deployment."
	out := c.CompactWithCorpus(in, corpus)
	if !strings.Contains(out, "buildkit") {
		t.Fatalf("expected the rare-term sentence to survive, got %q", out)
	}
}

func TestCompactor_PositionScore(t *testing.T) {
	// pos=0 of 5 should be 1.0; pos=4 of 5 should be 1.0; pos=2 of 5
	// should be 0.0.
	cases := []struct {
		pos, total int
		min, max   float64
	}{
		{0, 5, 0.99, 1.01},
		{4, 5, 0.99, 1.01},
		{2, 5, -0.01, 0.01},
		{0, 1, 0.99, 1.01}, // single-sentence edge
	}
	for _, c := range cases {
		got := positionScore(c.pos, c.total)
		if got < c.min || got > c.max {
			t.Errorf("positionScore(%d,%d)=%v not in [%v,%v]", c.pos, c.total, got, c.min, c.max)
		}
	}
}

func TestCompactor_LengthScore(t *testing.T) {
	cases := []struct {
		s     string
		minOK float64
	}{
		{strings.Repeat("a", 5), 0.0},   // very short: low
		{strings.Repeat("a", 100), 0.9}, // sweet spot: high
		{strings.Repeat("a", 600), 0.0}, // very long: low (clamped to 0.2)
		{strings.Repeat("a", 50), 0.5},  // mid-short
		{strings.Repeat("a", 250), 0.6}, // mid-long
	}
	for _, c := range cases {
		got := lengthScore(c.s)
		if got < c.minOK-0.05 {
			t.Errorf("lengthScore(len=%d)=%v expected ≥%v", len(c.s), got, c.minOK)
		}
	}
}

func TestCompactor_MinSentencesFloorRaisesKeep(t *testing.T) {
	// TargetRatio=0.05 * 10 sentences = 0 sentences. MinSentences=3
	// must raise the count to 3.
	c := New(Config{TargetRatio: 0.05, MinSentences: 3})
	in := strings.Repeat("Sentence here. ", 10)
	out := c.Compact(in)
	count := strings.Count(out, ".")
	if count < 3 {
		t.Fatalf("MinSentences=3 floor violated, got %d in %q", count, out)
	}
	if count >= 10 {
		t.Fatalf("expected compression below 10, got %d", count)
	}
}

func TestCompactor_ProsePreservedIfAtMinSentences(t *testing.T) {
	c := New(Config{TargetRatio: 0.1, MinSentences: 1})
	in := "Only one sentence."
	out := c.Compact(in)
	if out != in {
		t.Fatalf("single-sentence prose must pass through, got %q", out)
	}
}

func TestCompactor_HandlesEmptyInput(t *testing.T) {
	c := New(DefaultConfig())
	if out := c.Compact(""); out != "" {
		t.Fatalf("empty input must compact to empty, got %q", out)
	}
}

func TestCompactor_BlankSectionsPreserved(t *testing.T) {
	c := New(DefaultConfig())
	in := "Para1 here.\n\nPara2 there.\n"
	out := c.Compact(in)
	if !strings.Contains(out, "\n\n") {
		t.Fatalf("blank-line separators dropped: %q", out)
	}
}

func TestCompactor_HighRatioKeepsEverything(t *testing.T) {
	c := New(Config{TargetRatio: 1.0, MinSentences: 1})
	in := "Sentence one. Sentence two. Sentence three. Sentence four."
	out := c.Compact(in)
	if !strings.Contains(out, "Sentence one") ||
		!strings.Contains(out, "Sentence two") ||
		!strings.Contains(out, "Sentence three") ||
		!strings.Contains(out, "Sentence four") {
		t.Fatalf("ratio=1.0 must keep all sentences, got %q", out)
	}
}

func TestCompactor_DropPreservation(t *testing.T) {
	c := New(Config{
		TargetRatio: 0.5, MinSentences: 1,
		PreserveCodeBlocks: false, PreserveHeaders: false, PreserveLists: false,
	})
	in := "# Header\n\n```code```\n\n- item\n\nProse here. Long enough. More prose. Final prose."
	out := c.Compact(in)
	if strings.Contains(out, "# Header") {
		t.Fatalf("header should be dropped under PreserveHeaders=false: %q", out)
	}
	if strings.Contains(out, "```code```") {
		t.Fatalf("code should be dropped under PreserveCodeBlocks=false: %q", out)
	}
	if strings.Contains(out, "- item") {
		t.Fatalf("list should be dropped under PreserveLists=false: %q", out)
	}
}

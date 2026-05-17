package repdet

import (
	"strings"
	"testing"
)

func makeBlock(seed string, size int) string {
	var b strings.Builder
	for b.Len() < size {
		b.WriteString(seed)
	}
	return b.String()[:size]
}

func TestAddBlockShortDropped(t *testing.T) {
	idx := NewIndex()
	idx.AddBlock("short.go", 1, 1, "tiny")
	if len(idx.Blocks()) != 0 {
		t.Errorf("short block should be dropped, got %d blocks", len(idx.Blocks()))
	}
}

func TestAddBlockRegistered(t *testing.T) {
	idx := NewIndex()
	idx.AddBlock("foo.go", 1, 20, makeBlock("FooBarBaz ", 500))
	if len(idx.Blocks()) != 1 {
		t.Errorf("block not registered: %d", len(idx.Blocks()))
	}
}

func TestFindMatchesEmptyIndex(t *testing.T) {
	idx := NewIndex()
	if got := idx.FindMatches("anything goes here"); got != nil {
		t.Errorf("empty index returned matches: %v", got)
	}
}

func TestFindMatchesTextTooShort(t *testing.T) {
	idx := NewIndex()
	idx.AddBlock("x.go", 1, 1, makeBlock("X", 500))
	if got := idx.FindMatches("short"); got != nil {
		t.Errorf("short text should return nil, got %v", got)
	}
}

func TestFindAndRewriteExactRepeat(t *testing.T) {
	// 400-char block, echoed verbatim mid-stream.
	block := makeBlock("Function body line. ", 400)
	idx := NewIndex()
	idx.AddBlock("src/foo.go", 10, 30, block)

	out := "Here is your answer.\n" + block + "\nDone."
	rewritten, matches := idx.Rewrite(out)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match got %d (rewritten=%q)", len(matches), rewritten)
	}
	if !strings.Contains(rewritten, "[unchanged: src/foo.go:L10-30]") {
		t.Errorf("marker missing in: %q", rewritten)
	}
	if strings.Contains(rewritten, block) {
		t.Errorf("echoed block not replaced")
	}
	if matches[0].Length < MinMatch {
		t.Errorf("match length %d below MinMatch %d", matches[0].Length, MinMatch)
	}
}

func TestRewriteNoMatchPassthrough(t *testing.T) {
	idx := NewIndex()
	idx.AddBlock("a.go", 1, 5, makeBlock("AlphaBetaGamma ", 500))
	text := "totally unrelated content here, never matches the block contents at all"
	out, matches := idx.Rewrite(text)
	if out != text {
		t.Errorf("text mutated unexpectedly")
	}
	if matches != nil {
		t.Errorf("expected nil matches, got %v", matches)
	}
}

func TestShortRepeatDoesNotFire(t *testing.T) {
	idx := NewIndex()
	block := makeBlock("Hello world ", 500)
	idx.AddBlock("b.go", 1, 5, block)
	// Echo only a 150-char window: <MinMatch=200 means we should
	// not fire.
	short := block[:150]
	text := "prefix " + short + " suffix"
	_, matches := idx.Rewrite(text)
	if len(matches) != 0 {
		t.Errorf("short echo fired despite below MinMatch threshold: %v", matches)
	}
}

func TestNoLineRangeMarker(t *testing.T) {
	idx := NewIndex()
	block := makeBlock("Z ", 500)
	idx.AddBlock("noline", 0, 0, block)
	text := "prefix " + block + " suffix"
	out, _ := idx.Rewrite(text)
	if !strings.Contains(out, "[unchanged: noline]") {
		t.Errorf("marker without line range malformed: %q", out)
	}
}

func TestMultipleMatchesNonOverlapping(t *testing.T) {
	idx := NewIndex()
	block1 := makeBlock("FirstBlockContent ", 300)
	block2 := makeBlock("SecondBlockMaterial ", 300)
	idx.AddBlock("first.go", 1, 5, block1)
	idx.AddBlock("second.go", 10, 14, block2)
	text := "intro " + block1 + " middle " + block2 + " end"
	out, matches := idx.Rewrite(text)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches got %d (out=%q)", len(matches), out)
	}
	if !strings.Contains(out, "[unchanged: first.go:L1-5]") {
		t.Errorf("first marker missing: %q", out)
	}
	if !strings.Contains(out, "[unchanged: second.go:L10-14]") {
		t.Errorf("second marker missing: %q", out)
	}
}

func TestExtendLeftAndRight(t *testing.T) {
	// The match starts mid-block and extends both directions before
	// running out. Exercises both ls and rs extension paths.
	idx := NewIndex()
	block := makeBlock("ExtendableContent ", 500)
	idx.AddBlock("ext.go", 1, 10, block)
	// Echo the middle 300 chars of the block.
	echo := block[100:400]
	text := strings.Repeat("X", 50) + echo + strings.Repeat("Y", 50)
	_, matches := idx.Rewrite(text)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match got %d", len(matches))
	}
}

func TestRollingHashConsistency(t *testing.T) {
	s := makeBlock("ConsistencyCheck ", 200)
	// Compute hash of s[5:5+WindowSize] both ways - directly and by
	// rolling from s[0:WindowSize].
	direct := hashWindow(s[5 : 5+WindowSize])
	rolling := hashWindow(s[:WindowSize])
	for i := 1; i <= 5; i++ {
		rolling = rollAdvance(rolling, s[i-1], s[i+WindowSize-1])
	}
	if direct != rolling {
		t.Errorf("rolling=%d != direct=%d", rolling, direct)
	}
}

func TestExtendBestPicksLongest(t *testing.T) {
	// Two blocks with the same WindowSize prefix but different bodies.
	// The matcher must pick the one with the longer extension.
	idx := NewIndex()
	prefix := makeBlock("SharedPrefix__", WindowSize)
	short := prefix + "SHORT"
	long := prefix + makeBlock("LongTail ", 400)
	idx.AddBlock("short.go", 1, 1, short)
	idx.AddBlock("long.go", 1, 10, long)

	text := "prefix " + long + " suffix"
	out, matches := idx.Rewrite(text)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match got %d", len(matches))
	}
	if !strings.Contains(out, "long.go") {
		t.Errorf("longer block not chosen: %q", out)
	}
}

func TestExtendBestByteVerifyRejectsMismatch(t *testing.T) {
	// Directly exercise the byte-verify branch in extendBest: hand
	// the matcher a candidate whose WindowSize window does not match
	// the text at textPos. The candidate is silently skipped.
	idx := NewIndex()
	idx.blocks = []Block{{
		Name:     "bogus.go",
		LineFrom: 1, LineTo: 1,
		Text: makeBlock("ABC ", 500),
	}}
	idx.fingerprints = map[uint64][]position{}
	text := makeBlock("XYZ ", 500)
	// Force the candidate offset onto a block window whose bytes
	// differ from the text at position 0 - byte-verify rejects it.
	candidates := []position{{block: 0, offset: 0}}
	_, found := idx.extendBest(text, 0, candidates)
	if found {
		t.Errorf("byte-verify should have rejected mismatched window")
	}
}

func TestSkipPastMatchToEndOfText(t *testing.T) {
	// Match runs to the end of text; the post-match skip exits without
	// further scanning.
	idx := NewIndex()
	block := makeBlock("FinalBlock ", 400)
	idx.AddBlock("fin.go", 1, 4, block)
	text := "intro " + block
	_, matches := idx.Rewrite(text)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match got %d", len(matches))
	}
}

package summarization

import (
	"strings"
	"testing"
)

func TestStripCoTTags_EmptyTags(t *testing.T) {
	if got := StripCoTTags("hi <think>x</think>", nil); got != "hi <think>x</think>" {
		t.Fatalf("nil tags must be no-op: %q", got)
	}
	if got := StripCoTTags("hi", []string{}); got != "hi" {
		t.Fatalf("empty tags must be no-op: %q", got)
	}
}

func TestStripCoTTags_AllFamilies(t *testing.T) {
	ResetCoTTagCounts()
	in := "<think>a</think>\n" +
		"<thinking>b</thinking>\n" +
		"<reasoning>c</reasoning>\n" +
		"<reason>r</reason>\n" +
		"<analysis>d</analysis>\n" +
		"<scratchpad>e</scratchpad>\n" +
		"<reflection>f</reflection>\n" +
		"<plan>g</plan>\n" +
		"<chain_of_thought>h</chain_of_thought>\n" +
		"<chain-of-thought>i</chain-of-thought>\n" +
		"<inner_thought>j</inner_thought>\n" +
		"<inner_monologue>k</inner_monologue>\n" +
		"- final bullet"
	got := StripCoTTags(in, defaultCoTTags)
	for _, tag := range defaultCoTTags {
		if strings.Contains(got, "<"+tag) {
			t.Fatalf("tag %q not stripped from %q", tag, got)
		}
	}
	if !strings.Contains(got, "- final bullet") {
		t.Fatalf("final bullet missing: %q", got)
	}
	for _, tag := range defaultCoTTags {
		if CoTTagCount(tag) != 0 {
			t.Fatalf("legacy counter for %q should stay zero", tag)
		}
	}
}

func TestStripCoTTags_WithAttributesIsNoOp(t *testing.T) {
	ResetCoTTagCounts()
	in := `<think id="x">step 1</think>` + "\n- bullet"
	got := StripCoTTags(in, []string{"think"})
	if got != in {
		t.Fatalf("attributed tag should be unchanged by legacy helper: %q", got)
	}
}

func TestStripCoTTags_NestedFixedPoint(t *testing.T) {
	ResetCoTTagCounts()
	in := `<reasoning><think>inner</think> outer</reasoning>` + "\n- bullet"
	got := StripCoTTags(in, []string{"think", "reasoning"})
	if strings.Contains(got, "<think") || strings.Contains(got, "<reasoning") {
		t.Fatalf("nested not collapsed: %q", got)
	}
	if CoTTagCount("think") != 0 || CoTTagCount("reasoning") != 0 {
		t.Fatal("legacy counters should stay zero")
	}
}

func TestStripCoTTags_NoMatchKeepsOriginal(t *testing.T) {
	ResetCoTTagCounts()
	in := "- pure bullet output"
	got := StripCoTTags(in, defaultCoTTags)
	if got != in {
		t.Fatalf("non-matching content must be unchanged: %q", got)
	}
}

func TestStripCoTTags_MalformedClosingTruncatesAtOpenTag(t *testing.T) {
	ResetCoTTagCounts()
	in := "<think>step</think >\n- bullet"
	got := StripCoTTags(in, []string{"think"})
	if got != "" {
		t.Fatalf("malformed close should truncate at open tag, got %q", got)
	}
}

func TestCoTTagCount_UnknownFamily(t *testing.T) {
	ResetCoTTagCounts()
	if got := CoTTagCount("never-seen"); got != 0 {
		t.Fatalf("unknown family must return 0, got %d", got)
	}
}

func TestCoTTagCounts_ReturnsCopy(t *testing.T) {
	ResetCoTTagCounts()
	StripCoTTags("<think>x</think>", []string{"think"})
	snap := CoTTagCounts()
	snap["think"] = 999
	if CoTTagCount("think") == 999 {
		t.Fatal("CoTTagCounts must return a copy, not a reference")
	}
}

func TestResetCoTTagCounts_ClearsAll(t *testing.T) {
	ResetCoTTagCounts()
	StripCoTTags("<think>x</think><reasoning>y</reasoning>", []string{"think", "reasoning"})
	if CoTTagCount("think") != 0 {
		t.Fatal("legacy counters should stay zero before reset")
	}
	ResetCoTTagCounts()
	if CoTTagCount("think") != 0 || CoTTagCount("reasoning") != 0 {
		t.Fatal("reset did not clear counters")
	}
}

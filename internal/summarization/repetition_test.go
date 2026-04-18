package summarization

import (
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/types"
)

func toolMsg(idx int, name, text string) types.Message {
	return types.Message{
		Index: idx,
		Role:  "user",
		Content: []types.ContentBlock{
			{Type: "tool_result", ToolName: name, Text: text},
		},
	}
}

// TestBuildRepetitionIndex_singleOccurrenceHidden drops groups with only one
// observation.
func TestBuildRepetitionIndex_singleOccurrenceHidden(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		toolMsg(0, "grep", "pattern alpha"),
		toolMsg(1, "grep", "pattern beta"),
	}
	groups := BuildRepetitionIndex(msgs)
	if len(groups) != 0 {
		t.Fatalf("different topics must not merge into a group, got %v", groups)
	}
}

// TestBuildRepetitionIndex_mergesRepeats identifies two repeats.
func TestBuildRepetitionIndex_mergesRepeats(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		toolMsg(0, "grep", "pattern alpha"),
		toolMsg(1, "grep", "pattern alpha"),
		toolMsg(2, "grep", "pattern alpha"),
	}
	groups := BuildRepetitionIndex(msgs)
	if len(groups) != 1 {
		t.Fatalf("expected one group, got %d", len(groups))
	}
	if got := groups[0].indices; len(got) != 3 || got[0] != 0 || got[2] != 2 {
		t.Fatalf("indices: %v", got)
	}
}

// TestBuildRepetitionIndex_collapsesConsecutiveDuplicates merges a block
// that appears twice in one message into a single logical occurrence.
func TestBuildRepetitionIndex_collapsesConsecutiveDuplicates(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{
			Index: 0,
			Role:  "user",
			Content: []types.ContentBlock{
				{Type: "tool_result", ToolName: "grep", Text: "pattern alpha"},
				{Type: "tool_result", ToolName: "grep", Text: "pattern alpha"},
			},
		},
		toolMsg(1, "grep", "pattern alpha"),
	}
	groups := BuildRepetitionIndex(msgs)
	if len(groups) != 1 {
		t.Fatalf("expected one group, got %d", len(groups))
	}
	if got := groups[0].indices; len(got) != 2 {
		t.Fatalf("consecutive duplicates in one message must collapse, got %v", got)
	}
}

// TestBuildRepetitionIndex_ignoresNonTool blocks that are neither tool_use
// nor tool_result.
func TestBuildRepetitionIndex_ignoresNonTool(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "hi"}}},
		toolMsg(1, "grep", "pattern"),
		toolMsg(2, "grep", "pattern"),
	}
	groups := BuildRepetitionIndex(msgs)
	if len(groups) != 1 {
		t.Fatalf("expected one group, got %d", len(groups))
	}
}

// TestBuildRepetitionIndex_emptyToolKeySkipped ignores blocks with no
// discriminating signal.
func TestBuildRepetitionIndex_emptyToolKeySkipped(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: "anon"}}},
		{Index: 1, Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: "anon"}}},
	}
	groups := BuildRepetitionIndex(msgs)
	if len(groups) != 0 {
		t.Fatalf("blocks with no tool name and no tool_use_id must be ignored, got %v", groups)
	}
}

// TestBuildRepetitionIndex_fallbackToToolUseID lets a block with only a
// tool_use_id still form a key.
func TestBuildRepetitionIndex_fallbackToToolUseID(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "tool_result", ToolUseID: "call_123", Text: "xyz"}}},
		{Index: 1, Role: "user", Content: []types.ContentBlock{{Type: "tool_result", ToolUseID: "call_123", Text: "xyz"}}},
	}
	groups := BuildRepetitionIndex(msgs)
	if len(groups) != 1 {
		t.Fatalf("tool_use_id fallback must form a key, got %v", groups)
	}
}

// TestExtractTopicSignal truncates long lines and stops at the first newline.
func TestExtractTopicSignal(t *testing.T) {
	t.Parallel()
	if got := extractTopicSignal(""); got != "" {
		t.Fatalf("empty: %q", got)
	}
	long := strings.Repeat("x", 60)
	if got := extractTopicSignal(long); len(got) != 48 {
		t.Fatalf("long: len=%d", len(got))
	}
	if got := extractTopicSignal("first line\nsecond line"); got != "first line" {
		t.Fatalf("newline truncation: %q", got)
	}
}

// TestRepetitionHint_silentWithoutRepeats is empty when nothing repeats.
func TestRepetitionHint_silentWithoutRepeats(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{toolMsg(0, "grep", "alpha"), toolMsg(1, "grep", "beta")}
	if got := RepetitionHint(msgs); got != "" {
		t.Fatalf("expected empty hint, got %q", got)
	}
}

// TestRepetitionHint_containsDiscounts advertises the staircase factors.
func TestRepetitionHint_containsDiscounts(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		toolMsg(0, "grep", "alpha"),
		toolMsg(1, "grep", "alpha"),
		toolMsg(2, "grep", "alpha"),
		toolMsg(3, "grep", "alpha"),
	}
	hint := RepetitionHint(msgs)
	for _, need := range []string{"Repetition guidance:", "grep|alpha", "appears 4x", "@50%", "@25%"} {
		if !strings.Contains(hint, need) {
			t.Fatalf("missing %q in:\n%s", need, hint)
		}
	}
}

// TestRepetitionHint_orderDeterministic keeps the same groups stable across
// calls when the repetition counts tie.
func TestRepetitionHint_orderDeterministic(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		toolMsg(0, "grep", "alpha"),
		toolMsg(1, "grep", "alpha"),
		toolMsg(2, "ls", "/tmp"),
		toolMsg(3, "ls", "/tmp"),
	}
	h1 := RepetitionHint(msgs)
	h2 := RepetitionHint(msgs)
	if h1 != h2 {
		t.Fatal("hint must be deterministic across calls")
	}
	// Alphabetical tie-break on key: "grep|alpha" < "ls|/tmp".
	idxGrep := strings.Index(h1, "grep|alpha")
	idxLs := strings.Index(h1, "ls|/tmp")
	if idxGrep < 0 || idxLs < 0 || idxGrep >= idxLs {
		t.Fatalf("alphabetical ordering broken: grep=%d ls=%d\n%s", idxGrep, idxLs, h1)
	}
}

// TestRepetitionHint_longerGroupsFirst ranks larger groups first.
func TestRepetitionHint_longerGroupsFirst(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		toolMsg(0, "ls", "/tmp"),
		toolMsg(1, "ls", "/tmp"),
		toolMsg(2, "grep", "alpha"),
		toolMsg(3, "grep", "alpha"),
		toolMsg(4, "grep", "alpha"),
	}
	hint := RepetitionHint(msgs)
	idxGrep := strings.Index(hint, "grep|alpha")
	idxLs := strings.Index(hint, "ls|/tmp")
	if idxGrep >= idxLs {
		t.Fatalf("larger group must appear first: grep=%d ls=%d\n%s", idxGrep, idxLs, hint)
	}
}

// TestRepetitionFactor covers all staircase positions including the clamp.
func TestRepetitionFactor(t *testing.T) {
	t.Parallel()
	if repetitionFactor(0) != 1.0 {
		t.Fatalf("first occurrence must be 1.0")
	}
	if repetitionFactor(1) != 0.5 {
		t.Fatalf("second occurrence must be 0.5")
	}
	if repetitionFactor(2) != 0.25 {
		t.Fatalf("third occurrence must be 0.25")
	}
	if repetitionFactor(99) != 0.25 {
		t.Fatalf("out-of-range occurrence must clamp to last step")
	}
	if repetitionFactor(-1) != 1.0 {
		t.Fatalf("negative occurrence must map to 1.0")
	}
}

// TestBuildRepetitionIndex_dedupToSingleHides covers the post-dedup
// `len(uniq) < 2` skip when all entries collapse to one logical occurrence.
func TestBuildRepetitionIndex_dedupToSingleHides(t *testing.T) {
	t.Parallel()
	// Two tool_result blocks in the same message with identical content and
	// tool name collapse to one logical occurrence; the group must be hidden.
	msgs := []types.Message{
		{
			Index: 7,
			Role:  "user",
			Content: []types.ContentBlock{
				{Type: "tool_result", ToolName: "grep", Text: "alpha"},
				{Type: "tool_result", ToolName: "grep", Text: "alpha"},
			},
		},
	}
	groups := BuildRepetitionIndex(msgs)
	if len(groups) != 0 {
		t.Fatalf("expected hidden group after same-message dedup, got %v", groups)
	}
}

// TestExtractToolKey_topicless covers the branch where a block has a name
// but no extractable topic signal.
func TestExtractToolKey_topicless(t *testing.T) {
	t.Parallel()
	block := types.ContentBlock{Type: "tool_result", ToolName: "ls", Text: ""}
	if got := extractToolKey(block); got != "ls" {
		t.Fatalf("topicless block must key on name alone, got %q", got)
	}
}

// TestSummarizationHint_IncludesRepetitionSection asserts the repetition
// guidance is appended to the priority hint when applicable.
func TestSummarizationHint_IncludesRepetitionSection(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		toolMsg(0, "grep", "alpha"),
		toolMsg(1, "grep", "alpha"),
	}
	hint := SummarizationHint(msgs)
	if !strings.Contains(hint, "Repetition guidance:") {
		t.Fatalf("expected repetition section, got:\n%s", hint)
	}
}

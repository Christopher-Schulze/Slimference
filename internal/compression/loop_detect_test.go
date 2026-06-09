package compression

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func userMsg(idx int, text string) types.Message {
	return types.Message{Index: idx, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: text}}}
}

func TestDetectLoop_notEnoughMessages(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{userMsg(0, "please refactor this function to use generics")}
	if _, _, ok := DetectLoop(msgs); ok {
		t.Fatal("single message must not trigger")
	}
}

func TestDetectLoop_distinctMessages(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		userMsg(0, "refactor the http server"),
		userMsg(1, "add unit tests for new module"),
		userMsg(2, "update the changelog with release notes"),
		userMsg(3, "run the full integration suite"),
	}
	if _, _, ok := DetectLoop(msgs); ok {
		t.Fatal("distinct messages must not trigger loop")
	}
}

func TestDetectLoop_repeatedMessages(t *testing.T) {
	t.Parallel()
	// Identical core text with single-word tail keeps Jaccard >= 0.75
	// across all adjacent pairs.
	text := "please fix this compression bug and continue"
	msgs := []types.Message{
		userMsg(0, text),
		userMsg(1, text),
		userMsg(2, text+" now"),
		userMsg(3, text+" now"),
	}
	nudge, streak, ok := DetectLoop(msgs)
	if !ok {
		t.Fatal("expected loop detection")
	}
	if streak < LoopDetectionMinStreak {
		t.Fatalf("streak: %d", streak)
	}
	if !strings.Contains(nudge, LoopNudgeMarker) {
		t.Fatalf("nudge must carry marker: %s", nudge)
	}
}

func TestApplyLoopNudge_prependsToLastUser(t *testing.T) {
	t.Parallel()
	text := "make the token compression more aggressive in the hot path"
	msgs := []types.Message{
		userMsg(0, text),
		userMsg(1, text),
		userMsg(2, text+" now"),
		userMsg(3, text+" final"),
	}
	out, saved := ApplyLoopNudge(msgs)
	if saved == 0 {
		t.Fatal("expected positive savings estimate")
	}
	last := out[len(out)-1]
	if !strings.Contains(last.Content[0].Text, LoopNudgeMarker) {
		t.Fatalf("nudge not prepended: %s", last.Content[0].Text)
	}
	if !strings.Contains(last.Content[0].Text, "final") {
		t.Fatalf("original text lost: %s", last.Content[0].Text)
	}
	// Message count must be stable.
	if len(out) != len(msgs) {
		t.Fatalf("msg count changed: before=%d after=%d", len(msgs), len(out))
	}
}

func TestApplyLoopNudge_idempotent(t *testing.T) {
	t.Parallel()
	text := "compress the streaming response better and avoid buffering"
	msgs := []types.Message{
		userMsg(0, text),
		userMsg(1, text),
		userMsg(2, text+" a"),
		userMsg(3, text+" b"),
	}
	out, saved1 := ApplyLoopNudge(msgs)
	out2, saved2 := ApplyLoopNudge(out)
	if saved2 != 0 {
		t.Fatalf("second call must not double-nudge: %d", saved2)
	}
	if saved1 == 0 {
		t.Fatal("first call must nudge")
	}
	// Ensure only one marker instance.
	count := strings.Count(out2[len(out2)-1].Content[0].Text, LoopNudgeMarker)
	if count != 1 {
		t.Fatalf("expected exactly one marker, got %d", count)
	}
}

func TestApplyLoopNudge_noLoopReturnsInput(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{userMsg(0, "short"), userMsg(1, "also short")}
	out, saved := ApplyLoopNudge(msgs)
	if saved != 0 {
		t.Fatal("no loop -> no savings")
	}
	if len(out) != len(msgs) {
		t.Fatal("len changed")
	}
}

func TestApplyLoopNudge_noUserMessage(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Index: 0, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "hi"}}},
	}
	_, saved := ApplyLoopNudge(msgs)
	if saved != 0 {
		t.Fatal("no user message -> no savings")
	}
}

// TestApplyLoopNudge_fallbackLastUserNoText covers the `textIdx < 0` branch.
// Loop IS detected via earlier user text messages, but the FINAL user
// message has no text block so the nudge is prepended as a fresh block.
func TestApplyLoopNudge_fallbackLastUserNoText(t *testing.T) {
	t.Parallel()
	text := "tune the compression pipeline for better latency on large inputs"
	msgs := []types.Message{
		userMsg(0, text),
		userMsg(1, text),
		userMsg(2, text),
		userMsg(3, text),
		{
			Index: 4, Role: "user",
			Content: []types.ContentBlock{{Type: "tool_result", ToolName: "Bash", Text: "output"}},
		},
	}
	out, saved := ApplyLoopNudge(msgs)
	if saved == 0 {
		t.Fatal("expected loop detection across the first 4 user texts")
	}
	last := out[4]
	if len(last.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(last.Content))
	}
	if !strings.Contains(last.Content[0].Text, LoopNudgeMarker) {
		t.Fatalf("nudge must be the first block: %+v", last.Content)
	}
	if last.Content[1].Type != "tool_result" {
		t.Fatal("original tool_result must be preserved")
	}
}

func TestApplyLoopNudge_fallbackWhenNoTextBlock(t *testing.T) {
	t.Parallel()
	text := "make sure the layer 1 path never allocates inside the hot loop"
	// Create user messages with tool_result blocks only (no text block).
	msg := func(idx int, suffix string) types.Message {
		return types.Message{
			Index: idx,
			Role:  "user",
			Content: []types.ContentBlock{
				{Type: "tool_result", ToolName: "Read", Text: text + " " + suffix},
			},
		}
	}
	msgs := []types.Message{msg(0, "a"), msg(1, "b"), msg(2, "c"), msg(3, "d")}
	// Loop detection only inspects user "text" blocks, so the text field
	// is technically present inside tool_result; our collectUserTexts
	// only returns text blocks. So no loop is detected - this test
	// guards collectUserTexts' filter.
	_, saved := ApplyLoopNudge(msgs)
	if saved != 0 {
		t.Fatal("tool_result-only user turns must not be treated as user text")
	}
}

func TestJaccard_edges(t *testing.T) {
	t.Parallel()
	if got := jaccard(nil, nil); got != 0 {
		t.Fatal("empty")
	}
	a := wordSet("hello world")
	b := wordSet("hello everyone")
	got := jaccard(a, b)
	if got <= 0 || got >= 1 {
		t.Fatalf("got %v", got)
	}
	identical := wordSet("same same same")
	if jaccard(identical, identical) != 1.0 {
		t.Fatal("identical must be 1.0")
	}
}

func TestWordSet_tokenises(t *testing.T) {
	t.Parallel()
	got := wordSet("  Hello WORLD hello  ")
	if _, ok := got["hello"]; !ok {
		t.Fatal("lowercase")
	}
	if _, ok := got["world"]; !ok {
		t.Fatal("world")
	}
	if len(got) != 2 {
		t.Fatalf("dedup failed: %v", got)
	}
}

func TestLastUserMsgIdx(t *testing.T) {
	t.Parallel()
	if got := lastUserMsgIdx(nil); got != -1 {
		t.Fatalf("empty: %d", got)
	}
	if got := lastUserMsgIdx([]types.Message{{Role: "assistant"}}); got != -1 {
		t.Fatalf("no user: %d", got)
	}
	if got := lastUserMsgIdx([]types.Message{{Role: "user"}, {Role: "assistant"}}); got != 0 {
		t.Fatalf("got %d", got)
	}
}

func TestFormatLoopNudge(t *testing.T) {
	t.Parallel()
	s := formatLoopNudge(5)
	if !strings.Contains(s, "5 near-identical") {
		t.Fatalf("missing streak: %s", s)
	}
}

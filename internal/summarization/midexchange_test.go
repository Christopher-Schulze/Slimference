package summarization

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

var errBoom = errors.New("boom")

func TestDetectMidExchangePoint_BelowThreshold(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "hi"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Bash"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: "ok"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "done"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "next"}}},
	}
	_, ok := DetectMidExchangePoint(msgs, 10000)
	if ok {
		t.Fatal("should not detect below threshold")
	}
}

func TestDetectMidExchangePoint_AboveThreshold(t *testing.T) {
	t.Parallel()
	longOutput := strings.Repeat("x ", 5000)
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "start"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Bash"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: longOutput}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "analysis"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("x ", 5000)}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Read"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: "file content"}}},
	}
	pt, ok := DetectMidExchangePoint(msgs, 100)
	if !ok {
		t.Fatal("should detect above threshold")
	}
	if pt.Start != 4 {
		t.Fatalf("start: got %d want 4", pt.Start)
	}
	if pt.End < 5 {
		t.Fatalf("end: got %d want >= 5", pt.End)
	}
}

func TestDetectMidExchangePoint_TooFewMessages(t *testing.T) {
	t.Parallel()
	_, ok := DetectMidExchangePoint([]types.Message{}, 100)
	if ok {
		t.Fatal("empty messages should not detect")
	}
	_, ok = DetectMidExchangePoint(nil, 100)
	if ok {
		t.Fatal("nil messages should not detect")
	}
}

func TestDetectMidExchangePoint_ZeroThreshold(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "hi"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Bash"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: "ok"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "done"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "next"}}},
	}
	_, ok := DetectMidExchangePoint(msgs, 0)
	if ok {
		t.Fatal("zero threshold should not detect")
	}
}

func TestDetectMidExchangePoint_NoToolCycle(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("x ", 5000)}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "response"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("y ", 5000)}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "response2"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "next"}}},
	}
	_, ok := DetectMidExchangePoint(msgs, 100)
	if ok {
		t.Fatal("no tool cycle should not detect")
	}
}

func TestFormatMidExchangeSummary(t *testing.T) {
	t.Parallel()
	s := FormatMidExchangeSummary("the summary text", 5)
	if !strings.Contains(s, "[in-progress summary, anchor=msg #5]") {
		t.Fatalf("unexpected: %s", s)
	}
	if !strings.Contains(s, "the summary text") {
		t.Fatal("summary text missing")
	}
}

func TestHasToolUse(t *testing.T) {
	t.Parallel()
	if hasToolUse(types.Message{Content: []types.ContentBlock{{Type: "text", Text: "hi"}}}) {
		t.Fatal("text block should not be tool_use")
	}
	if !hasToolUse(types.Message{Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Bash"}}}) {
		t.Fatal("tool_use block should be detected")
	}
}

func TestHasToolResult(t *testing.T) {
	t.Parallel()
	if hasToolResult(types.Message{Content: []types.ContentBlock{{Type: "text", Text: "hi"}}}) {
		t.Fatal("text block should not be tool_result")
	}
	if !hasToolResult(types.Message{Content: []types.ContentBlock{{Type: "tool_result", Text: "ok"}}}) {
		t.Fatal("tool_result block should be detected")
	}
}

func TestApplyMidExchange_BelowThreshold(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "hi"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Bash"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: "ok"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "done"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "next"}}},
	}
	out, saved, applied := ApplyMidExchange(msgs, 10000)
	if applied {
		t.Fatal("should not apply below threshold")
	}
	if saved != 0 {
		t.Fatalf("saved = %d, want 0", saved)
	}
	if len(out) != len(msgs) {
		t.Fatalf("len = %d, want %d", len(out), len(msgs))
	}
}

func TestApplyMidExchange_AboveThreshold(t *testing.T) {
	t.Parallel()
	longOutput := strings.Repeat("x ", 5000)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "start"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Bash"}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: longOutput}}},
		{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "analysis"}}},
		{Index: 4, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("x ", 5000)}}},
		{Index: 5, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Read"}}},
		{Index: 6, Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: "file content"}}},
	}
	out, saved, applied := ApplyMidExchange(msgs, 100)
	if !applied {
		t.Fatal("should apply above threshold")
	}
	if saved <= 0 {
		t.Fatalf("saved = %d, want > 0", saved)
	}
	if len(out) >= len(msgs) {
		t.Fatalf("len = %d, want < %d", len(out), len(msgs))
	}
	// Verify the in-progress marker is present.
	found := false
	for _, msg := range out {
		for _, blk := range msg.Content {
			if strings.Contains(blk.Text, "[in-progress summary") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("missing in-progress marker in output: %+v", out)
	}
	// Verify re-indexing.
	for i, msg := range out {
		if msg.Index != i {
			t.Fatalf("msg[%d].Index = %d, want %d", i, msg.Index, i)
		}
	}
}

func TestApplyMidExchange_NoDetection(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "hi"}}},
	}
	out, _, applied := ApplyMidExchange(msgs, 100)
	if applied {
		t.Fatal("should not apply with too few messages")
	}
	if len(out) != 1 {
		t.Fatalf("len = %d, want 1", len(out))
	}
}

// TestDetectMidExchangePoint_NoUserStart covers lastExchangeStart returning -1.
func TestDetectMidExchangePoint_NoUserStart(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Bash"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: "ok"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "done"}}},
	}
	_, ok := DetectMidExchangePoint(msgs, 100)
	if ok {
		t.Fatal("should not detect when no user starts an exchange")
	}
}

// TestDetectMidExchangePoint_ExactAtThreshold covers the candidateTokens < threshold branch.
func TestDetectMidExchangePoint_ExactAtThreshold(t *testing.T) {
	t.Parallel()
	// One token = len/4 = 1 char / 4 = 0, so we need 400 chars for 100 tokens.
	text := strings.Repeat("a", 396) // 99 tokens
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "start"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Bash"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: text}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "analysis"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "next"}}},
	}
	_, ok := DetectMidExchangePoint(msgs, 100)
	if ok {
		t.Fatal("should not detect when exactly at/below threshold")
	}
}

// TestDetectMidExchangePoint_NonAssistantAfterCycle covers the branch where
// end+1 exists but is not an assistant (the "&& messages[end+1].Role == 'assistant'"
// guard is false). A system message after the tool_result achieves this while
// keeping exchangeStart at 0.
func TestDetectMidExchangePoint_NonAssistantAfterCycle(t *testing.T) {
	t.Parallel()
	longOutput := strings.Repeat("x ", 5000)
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "start"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Bash"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: longOutput}}},
		{Role: "system", Content: []types.ContentBlock{{Type: "text", Text: "hint"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "analysis"}}},
	}
	pt, ok := DetectMidExchangePoint(msgs, 100)
	if !ok {
		t.Fatal("should detect with non-assistant after cycle")
	}
	if pt.End != 2 {
		t.Fatalf("end = %d, want 2", pt.End)
	}
}

// TestDetectMidExchangePoint_ExchangeStartNegative covers the branch where
// lastExchangeStart returns -1 because no user-start message exists.
func TestDetectMidExchangePoint_ExchangeStartNegative(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Bash"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: "ok"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "done"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "extra"}}},
	}
	_, ok := DetectMidExchangePoint(msgs, 100)
	if ok {
		t.Fatal("should not detect when no exchange start exists")
	}
}

// TestDetectMidExchangePoint_TrailingAssistant covers the branch where
// a completed tool cycle is followed by an assistant message, so end++ fires.
func TestDetectMidExchangePoint_TrailingAssistant(t *testing.T) {
	t.Parallel()
	longOutput := strings.Repeat("x ", 5000)
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "start"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Bash"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: longOutput}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "analysis"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: "ok"}}},
	}
	pt, ok := DetectMidExchangePoint(msgs, 100)
	if !ok {
		t.Fatal("should detect with trailing assistant")
	}
	if pt.End < 3 {
		t.Fatalf("end = %d, want >= 3", pt.End)
	}
}

// TestDetectMidExchangePoint_BelowThresholdAfterCycle covers the branch where
// a tool cycle exists but candidateTokens < threshold.
func TestDetectMidExchangePoint_BelowThresholdAfterCycle(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "start"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Bash"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: "ok"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "done"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: "ok"}}},
	}
	_, ok := DetectMidExchangePoint(msgs, 100)
	if ok {
		t.Fatal("should not detect when tokens are below threshold")
	}
}

// TestRenderRangeForSummarization covers all branches of the helper
// (T99b live-summary plumbing).
func TestRenderRangeForSummarization(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Content: []types.ContentBlock{{Type: "text", Text: "first"}}},
		{Content: []types.ContentBlock{{Type: "text", Text: "second"}, {Type: "text", Text: "third"}}},
		{Content: []types.ContentBlock{{Type: "text", Text: ""}}},
	}
	got := renderRangeForSummarization(msgs, 0, 1)
	if !strings.Contains(got, "first") || !strings.Contains(got, "second") || !strings.Contains(got, "third") {
		t.Fatalf("rendered range: %q", got)
	}
	if renderRangeForSummarization(msgs, -1, 1) != "" {
		t.Fatal("negative start must yield empty")
	}
	if renderRangeForSummarization(msgs, 1, 0) != "" {
		t.Fatal("end < start must yield empty")
	}
	if renderRangeForSummarization(msgs, 0, 99) != "" {
		t.Fatal("end out of range must yield empty")
	}
	if renderRangeForSummarization([]types.Message{{Content: []types.ContentBlock{{Type: "text", Text: ""}}}}, 0, 0) != "" {
		t.Fatal("only-empty-text must yield empty")
	}
}

// fakeSummarizer implements Summarizer for the T99b live-path tests.
type fakeSummarizer struct {
	name       string
	configured bool
	out        string
	err        error
}

func (f *fakeSummarizer) Name() string                                                   { return f.name }
func (f *fakeSummarizer) IsConfigured() bool                                             { return f.configured }
func (f *fakeSummarizer) Summarize(_ context.Context, _ string, _, _, _ int) (string, error) { return f.out, f.err }

// TestLayer2_ApplyMidExchange_LiveSummary covers the happy path where
// the FallbackChain returns a non-empty summary that replaces the stub
// body. T99b.
func TestLayer2_ApplyMidExchange_LiveSummary(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	l := NewLayer2(&cfg)
	l.chain.SetProviders(&fakeSummarizer{
		name:       "fake",
		configured: true,
		out:        "- live summary bullet [msg:0]",
	})

	longOutput := strings.Repeat("x ", 5000)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "start"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Bash"}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: longOutput}}},
		{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "analysis"}}},
		{Index: 4, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("x ", 5000)}}},
		{Index: 5, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Read"}}},
		{Index: 6, Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: "f"}}},
	}
	out, _, applied := l.ApplyMidExchange(context.Background(), msgs, 100)
	if !applied {
		t.Fatal("live path must apply")
	}
	found := false
	for _, m := range out {
		for _, b := range m.Content {
			if strings.Contains(b.Text, "live summary bullet") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected live summary text in output: %+v", out)
	}
}

// TestLayer2_ApplyMidExchange_FallsBackOnError covers the path where
// the chain errors out, forcing the stub fallback. T99b.
func TestLayer2_ApplyMidExchange_FallsBackOnError(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	l := NewLayer2(&cfg)
	l.chain.SetProviders(&fakeSummarizer{
		name:       "fake",
		configured: true,
		err:        errBoom,
	})

	out, _, applied := l.ApplyMidExchange(context.Background(), midExchangeTestMessages(), 100)
	if !applied {
		t.Fatal("fallback path must still apply")
	}
	gotMarker := false
	for _, m := range out {
		for _, b := range m.Content {
			if strings.Contains(b.Text, "completed steps summarized") {
				gotMarker = true
			}
		}
	}
	if !gotMarker {
		t.Fatalf("expected stub fallback body in output messages")
	}
}

// TestLayer2_ApplyMidExchange_NoDetect verifies the early-out when no
// summarizable range exists.
func TestLayer2_ApplyMidExchange_NoDetect(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	l := NewLayer2(&cfg)
	msgs := []types.Message{{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "x"}}}}
	out, _, applied := l.ApplyMidExchange(context.Background(), msgs, 100)
	if applied || len(out) != 1 {
		t.Fatalf("no-detect must short-circuit, got applied=%v len=%d", applied, len(out))
	}
}

// TestLayer2_ApplyMidExchange_NilChain covers the path where Layer2
// has no chain (defensive: chain is always non-nil after NewLayer2;
// hand-build a Layer2 to exercise the branch).
func TestLayer2_ApplyMidExchange_NilChain(t *testing.T) {
	t.Parallel()
	l := &Layer2{}
	_, _, applied := l.ApplyMidExchange(context.Background(), midExchangeTestMessages(), 100)
	if !applied {
		t.Fatal("nil-chain fallback must still apply via stub")
	}
}

// midExchangeTestMessages returns a 7-message exchange that always
// fires the mid-exchange detector (last user message starts a new
// exchange; an earlier completed tool-use cycle exists with enough
// tokens to exceed the threshold). T99b.
func midExchangeTestMessages() []types.Message {
	longOutput := strings.Repeat("x ", 5000)
	return []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "start"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Bash"}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: longOutput}}},
		{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "analysis"}}},
		{Index: 4, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("x ", 5000)}}},
		{Index: 5, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Read"}}},
		{Index: 6, Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: "file content"}}},
	}
}

func blockTextAt(msgs []types.Message, idx int) string {
	if idx < 0 || idx >= len(msgs) || len(msgs[idx].Content) == 0 {
		return ""
	}
	return msgs[idx].Content[0].Text
}

// TestApplyMidExchange_Idempotent verifies T99c: applying twice does
// not re-collapse the already-summarized range.
func TestApplyMidExchange_Idempotent(t *testing.T) {
	t.Parallel()
	longOutput := strings.Repeat("x ", 5000)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "start"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Bash"}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: longOutput}}},
		{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "analysis"}}},
		{Index: 4, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("x ", 5000)}}},
		{Index: 5, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Read"}}},
		{Index: 6, Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: "file content"}}},
	}
	first, _, applied := ApplyMidExchange(msgs, 100)
	if !applied {
		t.Fatal("first apply must succeed")
	}
	second, saved2, applied2 := ApplyMidExchange(first, 100)
	if applied2 {
		t.Fatalf("second apply must NOT collapse again: saved=%d", saved2)
	}
	if len(second) != len(first) {
		t.Fatalf("second apply changed length: %d vs %d", len(second), len(first))
	}
}

// TestIsMidExchangeMarker covers the marker recognition helper.
func TestIsMidExchangeMarker(t *testing.T) {
	t.Parallel()
	marker := types.Message{Content: []types.ContentBlock{{Type: "text", Text: FormatMidExchangeSummary("x", 7)}}}
	if !IsMidExchangeMarker(marker) {
		t.Fatal("synthetic marker must be detected")
	}
	plain := types.Message{Content: []types.ContentBlock{{Type: "text", Text: "regular content"}}}
	if IsMidExchangeMarker(plain) {
		t.Fatal("plain content must not match")
	}
	nonText := types.Message{Content: []types.ContentBlock{{Type: "tool_use", Text: FormatMidExchangeSummary("x", 7)}}}
	if IsMidExchangeMarker(nonText) {
		t.Fatal("non-text block must not match even with marker text")
	}
}

// TestApplyMidExchange_SavedClamp covers the saved < 0 clamp branch.
// Uses short messages so the synthetic summary (15 tokens) is longer than
// the replaced range, forcing the clamp to zero.
func TestApplyMidExchange_SavedClamp(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "aaaa"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Bash", Text: "bbbb"}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: "cccc"}}},
		{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "dddd"}}},
		{Index: 4, Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: "eeee"}}},
	}
	_, saved, applied := ApplyMidExchange(msgs, 4)
	if !applied {
		t.Fatal("should apply")
	}
	if saved != 0 {
		t.Fatalf("saved = %d, want 0 (clamped)", saved)
	}
}

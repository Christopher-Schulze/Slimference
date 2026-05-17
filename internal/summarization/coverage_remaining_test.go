package summarization

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/extract"
	"github.com/slimference/slimference/internal/types"
)

func TestFallbackChainRemainingCancellationBranches(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := NewFallbackChain(&stubSummarizer{name: "stub", configured: true}).Summarize(ctx, "input", 0, 1, 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-loop cancellation err=%v", err)
	}

	ctx, cancel = context.WithCancel(context.Background())
	chain := NewFallbackChain(
		&cancelingErrorSummarizer{cancel: cancel, err: errors.New("provider canceled")},
		&stubSummarizer{name: "unused", configured: true, result: validLayer2Summary()},
	)
	if _, _, err := chain.Summarize(ctx, "input", 0, 1, 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("between-provider cancellation err=%v", err)
	}

	ctx, cancel = context.WithCancel(context.Background())
	chain = NewFallbackChain(
		&cancelOnConfiguredSummarizer{cancel: cancel},
		&stubSummarizer{name: "unused", configured: true, result: validLayer2Summary()},
	)
	if _, _, err := chain.Summarize(ctx, "input", 0, 1, 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("next-loop cancellation err=%v", err)
	}
}

type cancelOnConfiguredSummarizer struct {
	cancel context.CancelFunc
}

func (s *cancelOnConfiguredSummarizer) Name() string { return "cancel-on-configured" }
func (s *cancelOnConfiguredSummarizer) IsConfigured() bool {
	s.cancel()
	return false
}
func (s *cancelOnConfiguredSummarizer) Summarize(context.Context, string, int, int, int) (string, error) {
	return "", errors.New("unused")
}

func TestExtractSummarizerRemainingBudgetBranches(t *testing.T) {
	es := NewExtractSummarizer(extract.Config{TargetRatio: 1, MinSentences: 1})
	input := strings.Repeat("Sentence one keeps enough prose for the extractor. Sentence two adds more useful material. ", 80)
	out, err := es.Summarize(context.Background(), input, 0, 10, 4)
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if len(out) > int(float64(len(input))*validatorMaxOutputRatio) {
		t.Fatalf("budget branch did not clamp output: len(out)=%d len(input)=%d", len(out), len(input))
	}

	shortInput := strings.Repeat("Alpha beta gamma. ", 4)
	out, err = es.Summarize(context.Background(), shortInput, 0, 1, 1)
	if err != nil {
		t.Fatalf("short summarize: %v", err)
	}
	if len(out) > int(float64(len(shortInput))*validatorMaxOutputRatio) {
		t.Fatalf("short budget branch did not clamp output: len(out)=%d len(input)=%d", len(out), len(shortInput))
	}

	strictBudget := NewExtractSummarizer(extract.Config{TargetRatio: 1, MinSentences: 100})
	repetitiveInput := strings.Repeat("A compact sentence with enough words. ", 120)
	out, err = strictBudget.Summarize(context.Background(), repetitiveInput, 0, 1, 1)
	if err != nil {
		t.Fatalf("strict summarize: %v", err)
	}
	if len(out) > int(float64(len(repetitiveInput))*validatorMaxOutputRatio) {
		t.Fatalf("strict budget branch did not clamp output: len(out)=%d len(input)=%d", len(out), len(repetitiveInput))
	}
}

func TestLayer2RemainingTriggerAndTimeoutBranches(t *testing.T) {
	l := testLayer2ForCurrentTests()
	ctx, cancel := l.withJobTimeout(nil)
	defer cancel()
	if ctx == nil {
		t.Fatal("nil parent should produce a context")
	}

	l = testLayer2ForCurrentTests()
	l.chain.SetProviders(&stubSummarizer{name: "stub", configured: true, result: validLayer2Summary()})
	l.cache.Store(&CachedSummary{
		Summary:        validLayer2Summary(),
		CoveredRange:   [2]int{0, 0},
		OriginalTokens: 100,
		CreatedAt:      time.Now(),
	})
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "old"}}},
		{Index: 1, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "recent"}}},
	}
	if !l.ShouldTriggerCompression(msgs) {
		t.Fatal("boundary zero with fresh cache should still trigger")
	}
}

func TestLayer2RunCompressionJobRemainingValidationBranches(t *testing.T) {
	msgs := longLayer2Messages(10)

	ctx, cancel := context.WithCancel(context.Background())
	l := testLayer2ForCurrentTests()
	l.chain.SetProviders(&cancelingSequenceSummarizer{
		cancelOnCall: 1,
		cancel:       cancel,
		results:      []string{"not bullets"},
		errs:         []error{nil, errors.New("retry boom")},
	})
	l.RunCompressionJobContext(ctx, msgs)
	if cached, _ := l.sessions.GetCurrent(legacySessionID); cached != nil {
		t.Fatal("context canceled on retry error should not cache")
	}

	l = testLayer2ForCurrentTests()
	l.chain.SetProviders(&sequenceSummarizer{
		results: []string{"not bullets", "still not bullets"},
	})
	l.RunCompressionJobContext(context.Background(), msgs)
	if cached, _ := l.sessions.GetCurrent(legacySessionID); cached != nil {
		t.Fatal("retry invalid summary should not cache")
	}

	l = testLayer2ForCurrentTests()
	l.chain.SetProviders(&stubSummarizer{name: "anchor", configured: true, result: validLayer2Summary()})
	withAnchor := []types.Message{
		{Index: 0, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "apply_patch"}}},
		{Index: 1, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("alpha beta gamma delta. ", 80)}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "recent"}}},
	}
	l.RunCompressionJobContext(context.Background(), withAnchor)
	if cached, _ := l.sessions.GetCurrent(legacySessionID); cached == nil || len(cached.AnchorMessages) != 1 {
		t.Fatalf("expected cached summary with one preserved anchor, got %+v", cached)
	}

	l = testLayer2ForCurrentTests()
	l.chain.SetProviders(&stubSummarizer{name: "small", configured: true, result: "- tiny summary now\n"})
	small := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "tiny"}}},
		{Index: 1, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "recent"}}},
	}
	l.RunCompressionJobContext(context.Background(), small)
}

func TestFilterNonAnchoredRangeShiftsAnchorIndices(t *testing.T) {
	msgs := []types.Message{
		{Index: 2, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "edit_file"}}},
		{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "keep me"}}},
	}
	got := filterNonAnchoredRange(msgs, []int{2}, 2)
	if len(got) != 1 || got[0].Index != 3 {
		t.Fatalf("range filter did not shift anchor indices: %+v", got)
	}
	got = filterNonAnchoredRange(msgs, []int{0}, 0)
	if len(got) != 1 || got[0].Index != 3 {
		t.Fatalf("zero-start range should delegate to normal filter: %+v", got)
	}
}

func TestLayer2RunCompressionJobAnchorOnlyAndIncrementalBranches(t *testing.T) {
	l := testLayer2ForCurrentTests()
	l.chain.SetProviders(&stubSummarizer{name: "anchor-only", configured: true, result: validLayer2Summary()})
	anchorOnly := []types.Message{
		{Index: 0, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "apply_patch"}}},
		{Index: 1, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "yes"}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "recent"}}},
	}
	l.RunCompressionJobContext(context.Background(), anchorOnly)
	if cached, _ := l.sessions.GetCurrent(legacySessionID); cached != nil {
		t.Fatalf("anchor-only prefix should not cache: %+v", cached)
	}

	l = testLayer2ForCurrentTests()
	l.cfg.Tuning.IncrementalOverlapThreshold = 0.1
	l.cfg.Tuning.IncrementalStaircase = nil
	l.chain.SetProviders(&stubSummarizer{name: "incremental", configured: true, result: validLayer2Summary()})
	l.sessions.Store(legacySessionID, &CachedSummary{
		Summary:      validLayer2Summary(),
		CoveredRange: [2]int{0, 1},
		CreatedAt:    time.Now(),
	})
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("old alpha beta. ", 40)}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("old reply. ", 40)}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("new delta epsilon. ", 60)}}},
		{Index: 3, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "recent"}}},
	}
	l.RunCompressionJobContext(context.Background(), msgs)
	if cached, _ := l.sessions.GetCurrent(legacySessionID); cached == nil || !strings.Contains(cached.Summary, validLayer2Summary()) {
		t.Fatalf("incremental branch did not cache summary: %+v", cached)
	}

	l = testLayer2ForCurrentTests()
	l.cfg.Tuning.IncrementalOverlapThreshold = 0.1
	l.cfg.Tuning.IncrementalStaircase = nil
	l.chain.SetProviders(&stubSummarizer{name: "incremental-anchor", configured: true, result: validLayer2Summary()})
	l.sessions.Store(legacySessionID, &CachedSummary{
		Summary:      validLayer2Summary(),
		CoveredRange: [2]int{0, 1},
		CreatedAt:    time.Now(),
	})
	allDeltaAnchored := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("old alpha beta. ", 40)}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("old reply. ", 40)}}},
		{Index: 2, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "edit_file"}}},
		{Index: 3, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "recent"}}},
	}
	l.RunCompressionJobContext(context.Background(), allDeltaAnchored)
}

func TestProgressiveRemainingBranches(t *testing.T) {
	l := testLayer2ForCurrentTests()
	small := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "tiny"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "small"}}},
	}
	l.chain.SetProviders(&stubSummarizer{name: "small", configured: true, result: "- tiny summary now\n"})
	got := l.applyProgressiveTiersWithContext(context.Background(), small, []CompressionTier{{
		Name:        "small",
		MsgRange:    [2]int{0, 1},
		TargetRatio: 0.01,
	}})
	if len(got) != 1 {
		t.Fatalf("small progressive tier result=%+v", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	l = testLayer2ForCurrentTests()
	l.chain.SetProviders(&cancelingErrorSummarizer{cancel: cancel, err: errors.New("boom")})
	got = l.applyProgressiveTiersWithContext(ctx, small, []CompressionTier{{
		Name:        "cancel-error",
		MsgRange:    [2]int{0, 1},
		TargetRatio: 0.2,
	}})
	if len(got) != len(small) {
		t.Fatalf("canceled progressive error should return verbatim tail: %+v", got)
	}
}

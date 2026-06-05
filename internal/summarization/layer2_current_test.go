package summarization

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

func testLayer2ForCurrentTests() *Layer2 {
	cfg := config.Defaults().Compression
	cfg.SlidingWindow = 1
	cfg.MinMessagesForCompression = 1
	cfg.MinTokensForLayer2 = 0
	cfg.Summary.TargetRatio = 0.25
	return NewLayer2(&cfg)
}

func longLayer2Messages(n int) []types.Message {
	msgs := make([]types.Message, n)
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = types.Message{
			Index: i,
			Role:  role,
			Content: []types.ContentBlock{{
				Type: "text",
				Text: strings.Repeat("alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu. ", 40),
			}},
		}
	}
	return msgs
}

func validLayer2Summary() string {
	return strings.Repeat("- alpha beta gamma delta epsilon zeta eta theta.\n", 8)
}

func validProgressiveSummary() string {
	return strings.Repeat("- alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu.\n", 35)
}

func TestExtractSummarizerCurrentContract(t *testing.T) {
	es := NewExtractSummarizer(extractConfigFromSummary(config.Defaults().Compression.Summary))
	if es.Name() != "extract-tfidf" {
		t.Fatalf("name=%q", es.Name())
	}
	if !es.IsConfigured() {
		t.Fatal("extract summarizer should always be configured")
	}
	caps := es.Capabilities()
	if !caps.SupportsTemperatureZero || !caps.SupportsSeed || !caps.SupportsMinCompletionTokens {
		t.Fatalf("deterministic caps missing: %+v", caps)
	}
	if got, err := es.Summarize(context.Background(), "   ", 0, 0, 0); err != nil || got != "   " {
		t.Fatalf("empty summarize got %q err %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := es.Summarize(ctx, "non-empty", 0, 0, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled summarize err=%v", err)
	}
	out, err := es.Summarize(context.Background(), strings.Repeat("Sentence one. Sentence two. ", 80), 0, 5, 20)
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if !strings.Contains(out, "- ") {
		t.Fatalf("extract output should be bulletised: %q", out)
	}
}

func TestExtractSummarizerBudgetAndBulletHelpers(t *testing.T) {
	es := NewExtractSummarizer(extractConfigFromSummary(config.Defaults().Compression.Summary))
	input := strings.Repeat("The deterministic compactor keeps important prose while enforcing budgets. ", 120)
	out, err := es.Summarize(context.Background(), input, 0, 0, 1)
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if len(out) > int(float64(len(input))*validatorMaxOutputRatio) {
		t.Fatalf("summary exceeded hard budget: got %d input %d", len(out), len(input))
	}

	if got := truncateBulletsToBudget("- one\n- two\n", 100); got != "- one\n- two\n" {
		t.Fatalf("under-budget truncation changed content: %q", got)
	}
	if got := truncateBulletsToBudget("- one\n- two\n- three\n", 9); got != "- one" {
		t.Fatalf("multi-line truncation=%q", got)
	}
	if got := truncateBulletsToBudget("- very long first bullet\n", 4); got != "- v\n" {
		t.Fatalf("single-line truncation=%q", got)
	}
	if got := truncateBulletsToBudget("- too long\n", 1); got != "" {
		t.Fatalf("one-byte budget should return empty, got %q", got)
	}

	if got := bulletisePoseSentences("   "); got != "   " {
		t.Fatalf("blank bulletise changed content: %q", got)
	}
	mixed := bulletisePoseSentences("# Header\n\nExisting prose sentence. Second sentence.\n\n- existing item\n\n```go\nfunc main() {}\n```\n")
	for _, want := range []string{"# Header", "- Existing prose sentence.", "- existing item", "```go"} {
		if !strings.Contains(mixed, want) {
			t.Fatalf("bulletised output missing %q: %s", want, mixed)
		}
	}
}

func TestLayer2RunCompressionJobStoresCurrentSummary(t *testing.T) {
	l := testLayer2ForCurrentTests()
	l.chain.SetProviders(&stubSummarizer{name: "stub", configured: true, result: validLayer2Summary()})

	msgs := longLayer2Messages(8)
	l.RunCompressionJobContext(context.Background(), msgs)

	cached, rng := l.sessions.GetCurrent(legacySessionID)
	if cached == nil {
		t.Fatal("expected cached summary")
	}
	if rng[1] <= 0 {
		t.Fatalf("covered range not advanced: %+v", rng)
	}
	if cached.OriginalTokens <= cached.CompressedTokens {
		t.Fatalf("summary should save tokens: %+v", cached)
	}
	if cached.Hash == ([32]byte{}) {
		t.Fatal("cached summary hash missing")
	}
}

type sequenceSummarizer struct {
	results []string
	errs    []error
	calls   int
}

func (s *sequenceSummarizer) Name() string       { return "sequence" }
func (s *sequenceSummarizer) IsConfigured() bool { return true }
func (s *sequenceSummarizer) Summarize(_ context.Context, _ string, _, _, _ int) (string, error) {
	i := s.calls
	s.calls++
	if i < len(s.errs) && s.errs[i] != nil {
		return "", s.errs[i]
	}
	if i < len(s.results) {
		return s.results[i], nil
	}
	return validLayer2Summary(), nil
}

type cancelingSummarizer struct {
	cancel context.CancelFunc
	result string
}

func (s *cancelingSummarizer) Name() string       { return "canceling" }
func (s *cancelingSummarizer) IsConfigured() bool { return true }
func (s *cancelingSummarizer) Summarize(_ context.Context, _ string, _, _, _ int) (string, error) {
	s.cancel()
	return s.result, nil
}

type cancelingErrorSummarizer struct {
	cancel context.CancelFunc
	err    error
}

func (s *cancelingErrorSummarizer) Name() string       { return "canceling-error" }
func (s *cancelingErrorSummarizer) IsConfigured() bool { return true }
func (s *cancelingErrorSummarizer) Summarize(_ context.Context, _ string, _, _, _ int) (string, error) {
	s.cancel()
	return "", s.err
}

type cancelingSequenceSummarizer struct {
	cancelOnCall int
	cancel       context.CancelFunc
	results      []string
	errs         []error
	calls        int
}

func (s *cancelingSequenceSummarizer) Name() string       { return "canceling-sequence" }
func (s *cancelingSequenceSummarizer) IsConfigured() bool { return true }
func (s *cancelingSequenceSummarizer) Summarize(_ context.Context, _ string, _, _, _ int) (string, error) {
	i := s.calls
	s.calls++
	if i == s.cancelOnCall {
		s.cancel()
	}
	if i < len(s.errs) && s.errs[i] != nil {
		return "", s.errs[i]
	}
	if i < len(s.results) {
		return s.results[i], nil
	}
	return validLayer2Summary(), nil
}

func TestLayer2RunCompressionJobWrapperAndRetryOutcomes(t *testing.T) {
	l := testLayer2ForCurrentTests()
	seq := &sequenceSummarizer{
		results: []string{
			"not bullets",
			validLayer2Summary(),
		},
	}
	l.chain.SetProviders(seq)
	l.RunCompressionJob(longLayer2Messages(8))
	if seq.calls != 2 {
		t.Fatalf("retry calls=%d want 2", seq.calls)
	}
	if cached, _ := l.sessions.GetCurrent(legacySessionID); cached == nil {
		t.Fatal("retry success should cache summary")
	}

	l = testLayer2ForCurrentTests()
	seq = &sequenceSummarizer{
		results: []string{"not bullets"},
		errs:    []error{nil, errors.New("retry failed")},
	}
	l.chain.SetProviders(seq)
	l.RunCompressionJobContext(context.Background(), longLayer2Messages(8))
	if cached, _ := l.sessions.GetCurrent(legacySessionID); cached != nil {
		t.Fatal("retry error should not cache summary")
	}
}

func TestLayer2RunCompressionJobIncrementalBranches(t *testing.T) {
	msgs := longLayer2Messages(8)

	l := testLayer2ForCurrentTests()
	stub := &stubSummarizer{name: "stub", configured: true, result: validLayer2Summary()}
	l.chain.SetProviders(stub)
	l.sessions.Store(legacySessionID, &CachedSummary{
		Summary:          validLayer2Summary(),
		CoveredRange:     [2]int{0, 6},
		OriginalTokens:   1000,
		CompressedTokens: 200,
		CreatedAt:        time.Now(),
		Hash:             hashMessages(msgs[:7]),
	})
	l.RunCompressionJobContext(context.Background(), msgs)
	if stub.callCount != 0 {
		t.Fatalf("fully-covered incremental range should skip provider, calls=%d", stub.callCount)
	}

	l = testLayer2ForCurrentTests()
	stub = &stubSummarizer{name: "stub", configured: true, result: validLayer2Summary()}
	l.chain.SetProviders(stub)
	l.sessions.Store(legacySessionID, &CachedSummary{
		Summary:          validLayer2Summary(),
		CoveredRange:     [2]int{0, 3},
		OriginalTokens:   1000,
		CompressedTokens: 200,
		CreatedAt:        time.Now(),
		Hash:             hashMessages(msgs[:4]),
	})
	l.cfg.Tuning.IncrementalOverlapThreshold = 0.40
	l.RunCompressionJobContext(context.Background(), msgs)
	if stub.callCount != 1 {
		t.Fatalf("incremental delta should call provider once, calls=%d", stub.callCount)
	}
}

func TestLayer2RunCompressionJobSkipAndRetryBranches(t *testing.T) {
	l := testLayer2ForCurrentTests()
	l.chain.SetProviders()
	l.RunCompressionJobContext(context.Background(), longLayer2Messages(6))
	if cached, _ := l.sessions.GetCurrent(legacySessionID); cached != nil {
		t.Fatal("unconfigured chain should not cache")
	}

	l.chain.SetProviders(&stubSummarizer{name: "bad", configured: true, result: "not bullets"})
	l.RunCompressionJobContext(context.Background(), longLayer2Messages(6))
	if cached, _ := l.sessions.GetCurrent(legacySessionID); cached != nil {
		t.Fatal("invalid summary without retry success should not cache")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	l.chain.SetProviders(&stubSummarizer{name: "stub", configured: true, result: validLayer2Summary()})
	l.RunCompressionJobContext(ctx, longLayer2Messages(6))
	if cached, _ := l.sessions.GetCurrent(legacySessionID); cached != nil {
		t.Fatal("canceled context should not cache")
	}
}

func TestLayer2RunCompressionJobMoreGuardBranches(t *testing.T) {
	msgs := longLayer2Messages(12)

	l := testLayer2ForCurrentTests()
	l.cfg.MinTokensForLayer2 = 1_000_000
	l.chain.SetProviders(&stubSummarizer{name: "stub", configured: true, result: validLayer2Summary()})
	l.RunCompressionJobContext(context.Background(), msgs)
	if cached, _ := l.sessions.GetCurrent(legacySessionID); cached != nil {
		t.Fatal("token gate rejection should not cache")
	}

	l = testLayer2ForCurrentTests()
	l.chain.SetProviders(&stubSummarizer{name: "stub", configured: true, result: validLayer2Summary()})
	anchorsOnly := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "edit_file"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "apply_patch"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "recent"}}},
	}
	l.RunCompressionJobContext(context.Background(), anchorsOnly)
	if cached, _ := l.sessions.GetCurrent(legacySessionID); cached != nil {
		t.Fatal("all-anchor prefix should not cache")
	}

	l = testLayer2ForCurrentTests()
	l.chain.SetProviders(&stubSummarizer{name: "boom", configured: true, err: errors.New("boom")})
	l.RunCompressionJobContext(context.Background(), msgs)
	if cached, _ := l.sessions.GetCurrent(legacySessionID); cached != nil {
		t.Fatal("provider error should not cache")
	}

	l = testLayer2ForCurrentTests()
	repaired := strings.ReplaceAll(validLayer2Summary(), "- ", "* ")
	l.chain.SetProviders(&stubSummarizer{name: "repair", configured: true, result: repaired})
	l.RunCompressionJobContext(context.Background(), msgs)
	if cached, _ := l.sessions.GetCurrent(legacySessionID); cached == nil {
		t.Fatal("deterministic repair should make alternative bullets cacheable")
	}
}

func TestLayer2RunCompressionJobAdditionalCurrentBranches(t *testing.T) {
	msgs := longLayer2Messages(10)

	l := testLayer2ForCurrentTests()
	l.chain.SetProviders(&stubSummarizer{name: "unused", configured: true, result: validLayer2Summary()})
	anchorsOnly := []types.Message{
		{Index: 0, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "edit_file"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "apply_patch"}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "recent message kept outside prefix"}}},
	}
	l.RunCompressionJobContext(context.Background(), anchorsOnly)
	if cached, _ := l.sessions.GetCurrent(legacySessionID); cached != nil {
		t.Fatal("all-anchor compressible prefix should not cache")
	}

	l = testLayer2ForCurrentTests()
	stub := &stubSummarizer{name: "delta", configured: true, result: validLayer2Summary()}
	l.chain.SetProviders(stub)
	l.cfg.Tuning.IncrementalOverlapThreshold = 0.25
	l.sessions.Store(legacySessionID, &CachedSummary{
		Summary:          validLayer2Summary(),
		CoveredRange:     [2]int{0, 2},
		OriginalTokens:   1000,
		CompressedTokens: 100,
		CreatedAt:        time.Now(),
		Hash:             hashMessages(msgs[:3]),
	})
	l.RunCompressionJobContext(context.Background(), msgs)
	if stub.callCount != 1 {
		t.Fatalf("incremental delta should call provider once, got %d", stub.callCount)
	}
	if cached, _ := l.sessions.GetCurrent(legacySessionID); cached == nil {
		t.Fatal("incremental run should cache a fresh summary")
	}

	ctx, cancel := context.WithCancel(context.Background())
	l = testLayer2ForCurrentTests()
	l.chain.SetProviders(&cancelingErrorSummarizer{cancel: cancel, err: errors.New("canceled while summarizing")})
	l.RunCompressionJobContext(ctx, msgs)
	if cached, _ := l.sessions.GetCurrent(legacySessionID); cached != nil {
		t.Fatal("canceled provider error should not cache")
	}

	ctx, cancel = context.WithCancel(context.Background())
	l = testLayer2ForCurrentTests()
	l.chain.SetProviders(&cancelingSequenceSummarizer{
		cancelOnCall: 0,
		cancel:       cancel,
		results:      []string{"not bullets"},
	})
	l.RunCompressionJobContext(ctx, msgs)
	if cached, _ := l.sessions.GetCurrent(legacySessionID); cached != nil {
		t.Fatal("context canceled after invalid summary should not retry or cache")
	}

	ctx, cancel = context.WithCancel(context.Background())
	l = testLayer2ForCurrentTests()
	l.chain.SetProviders(&cancelingSequenceSummarizer{
		cancelOnCall: 1,
		cancel:       cancel,
		results:      []string{"not bullets", validLayer2Summary()},
	})
	l.RunCompressionJobContext(ctx, msgs)
	if cached, _ := l.sessions.GetCurrent(legacySessionID); cached != nil {
		t.Fatal("context canceled after retry summary should not cache")
	}

	ctx, cancel = context.WithCancel(context.Background())
	l = testLayer2ForCurrentTests()
	l.chain.SetProviders(&cancelingSummarizer{cancel: cancel, result: validLayer2Summary()})
	l.RunCompressionJobContext(ctx, msgs)
	if cached, _ := l.sessions.GetCurrent(legacySessionID); cached != nil {
		t.Fatal("context canceled after valid summary should not cache")
	}

	l = testLayer2ForCurrentTests()
	l.chain.SetProviders(&stubSummarizer{name: "huge", configured: true, result: "- kept summary now.\n- cap stayed bounded.\n"})
	huge := longLayer2Messages(3)
	huge[0].Content[0].Text = strings.Repeat("界", maxLayer2InputTokens+10_000)
	l.RunCompressionJobContext(context.Background(), huge)
	if cached, _ := l.sessions.GetCurrent(legacySessionID); cached == nil {
		t.Fatal("huge CJK input should still summarize after input capping")
	}
}

func TestLayer2TriggerAndFormattingHelpers(t *testing.T) {
	l := testLayer2ForCurrentTests()
	msgs := longLayer2Messages(8)
	if !l.ShouldTriggerCompression(msgs) {
		t.Fatal("expected trigger for configured deterministic provider")
	}
	if !l.ShouldTriggerCompressionWindow(msgs, 2) {
		t.Fatal("expected trigger through explicit window")
	}
	l.chain.SetProviders()
	if l.ShouldTriggerCompression(msgs) {
		t.Fatal("unconfigured provider should suppress trigger")
	}
	l.chain.SetProviders(&stubSummarizer{name: "stub", configured: true, result: validLayer2Summary()})
	l.cache.Compressing.Store(true)
	if l.ShouldTriggerCompression(msgs) {
		t.Fatal("compressing flag should suppress trigger")
	}
	l.cache.Compressing.Store(false)
	l.cfg.MinTokensForLayer2 = 1_000_000
	if l.ShouldTriggerCompression(msgs) {
		t.Fatal("token gate should suppress trigger")
	}
	l.cfg.MinTokensForLayer2 = 0
	l.cache.Store(&CachedSummary{
		Summary:        validLayer2Summary(),
		CoveredRange:   [2]int{0, 0},
		CreatedAt:      time.Now(),
		OriginalTokens: 1000,
	})
	if !l.ShouldTriggerCompression(msgs) {
		t.Fatal("zero boundary should trigger refresh")
	}

	l.cfg.Tuning.IncrementalStaircase = []config.StaircaseStep{{MsgCountLE: 10, Threshold: 0.5}}
	if got := l.incrementalOverlapThreshold(8); got != 0.5 {
		t.Fatalf("staircase threshold=%v", got)
	}
	l.cfg.Tuning.IncrementalStaircase = []config.StaircaseStep{{MsgCountLE: 10}}
	if got := l.incrementalOverlapThreshold(8); got != 0.70 {
		t.Fatalf("zero staircase fallback=%v", got)
	}
	l.cfg.Tuning.IncrementalStaircase = nil
	l.cfg.Tuning.IncrementalOverlapThreshold = 0
	if got := l.incrementalOverlapThreshold(100); got != 0.70 {
		t.Fatalf("zero scalar fallback=%v", got)
	}

	formatted := l.FormatMessagesForSummarization([]types.Message{{
		Index: 3,
		Role:  "assistant",
		Content: []types.ContentBlock{
			{Type: "text", Text: "func main() {\nreturn\n}"},
			{Type: "tool_use", ToolName: "bash", ToolInput: `{"cmd":"go test"}`},
			{Type: "tool_result", ToolResultID: "r1", Text: strings.Repeat("same\n", 3)},
		},
	}})
	for _, want := range []string{"[ASSISTANT msg 3]", "<tool_use", "<tool_result id=\"r1\">"} {
		if !strings.Contains(formatted, want) {
			t.Fatalf("formatted output missing %q: %s", want, formatted)
		}
	}
	pre := preprocessInput("dup\ndup\n" + strings.Repeat("x", 2100))
	if strings.Count(pre, "dup") != 1 || !strings.Contains(pre, "[truncated") {
		t.Fatalf("preprocess did not dedupe/truncate: %q", pre)
	}
	if estimateTokens("") != 0 || estimateTokens("hello 世界") < 3 {
		t.Fatal("estimateTokens branches not exercised as expected")
	}
	if estimateTokens("   ") != 1 || estimateTokens("こんにちはカタカナ") == 0 {
		t.Fatal("estimateTokens unicode/space branches failed")
	}
	if contentDensity(nil) != 0.5 || contentDensityText("") != 0.5 {
		t.Fatal("empty density should be neutral")
	}
	if contentDensity([]types.Message{{Content: []types.ContentBlock{{Type: "text"}}}}) != 0.5 {
		t.Fatal("zero-char density should be neutral")
	}
	if contentDensityText("<tool_result>\nsrc/main.go\nfunc run()") <= 0 {
		t.Fatal("dense text should score above zero")
	}
	denseMsgs := []types.Message{{
		Content: []types.ContentBlock{
			{Type: "tool_result", Text: strings.Repeat("tool output ", 80)},
			{Type: "text", Text: "func run() {}\n/Users/me/project/file.go"},
		},
	}}
	if contentDensity(denseMsgs) <= 0 {
		t.Fatal("dense messages should score above zero")
	}
	if !looksLikeCode("func run()") || !looksLikePath("src/foo/bar.go") {
		t.Fatal("code/path heuristics failed")
	}
	if looksLikeCode("") || looksLikePath("") {
		t.Fatal("empty code/path heuristics should be false")
	}
	if !looksLikeCode("{") || !looksLikeCode("// comment") || !looksLikePath("./x") || !looksLikePath("../x") {
		t.Fatal("code/path alternate heuristics failed")
	}
	if !shouldFenceSummarizationBlock("func run()") {
		t.Fatal("strict code block should be fenced")
	}
	if formatBlockForSummarization("plain text", false) != "plain text" ||
		formatBlockForSummarization("   ", true) != "   " ||
		!strings.HasPrefix(formatBlockForSummarization("func run()", true), "```text") {
		t.Fatal("formatBlockForSummarization branches failed")
	}
	if got := capSummarizationInput("abc", 0); got != "abc" {
		t.Fatalf("zero cap should return input: %q", got)
	}
	if got := capSummarizationInput("a\n"+strings.Repeat("b", 100), 5); strings.HasPrefix(got, "a\n") {
		t.Fatalf("cap should keep tail after first newline: %q", got)
	}
	if got := capSummarizationInput(strings.Repeat("a", 90)+"\nkeep", 5); got != "keep" {
		t.Fatalf("cap should trim to content after first newline in tail: %q", got)
	}
	if got := capSummarizationInput(strings.Repeat("x", 100), 5); strings.Contains(got, "\n") || len(got) != 20 {
		t.Fatalf("cap without newline should return raw tail: len=%d %q", len(got), got)
	}
	if got := capSummarizationInput(strings.Repeat("界", 100), 10); estimateTokens(got) > 10 || !utf8.ValidString(got) {
		t.Fatalf("CJK cap should respect estimated tokens and UTF-8: tokens=%d valid=%v len=%d", estimateTokens(got), utf8.ValidString(got), len(got))
	}
	if got := capSummarizationInput("a"+strings.Repeat("界", 20), 5); !utf8.ValidString(got) {
		t.Fatalf("byte cap should keep UTF-8 valid: %q", got)
	}
	originalHuge := strings.Repeat("界", 100)
	msgsForCap := []types.Message{{
		Index: 0,
		Role:  "user",
		Content: []types.ContentBlock{{
			Type: "text",
			Text: originalHuge,
		}},
	}}
	cappedMsgs := capMessageTextsForSummarization(msgsForCap, 10)
	if estimateTokens(cappedMsgs[0].Content[0].Text) > 10 || !utf8.ValidString(cappedMsgs[0].Content[0].Text) {
		t.Fatalf("message cap should respect estimated tokens and UTF-8: tokens=%d valid=%v", estimateTokens(cappedMsgs[0].Content[0].Text), utf8.ValidString(cappedMsgs[0].Content[0].Text))
	}
	if msgsForCap[0].Content[0].Text != originalHuge {
		t.Fatal("message cap mutated original message")
	}
	if computeAdaptiveTarget(1000, denseMsgs, 0.9) != 600 {
		t.Fatal("adaptive target cap branch failed")
	}
	if got := computeAdaptiveTargetWithDensity(4000, 20, 0.05, 0); got != 1000 {
		t.Fatalf("mid-size adaptive floor=%d", got)
	}
	l.cfg.Tuning.IncrementalOverlapThreshold = 0.82
	if got := l.incrementalOverlapThreshold(100); got != 0.82 {
		t.Fatalf("positive scalar threshold=%v", got)
	}
	if !shouldFenceSummarizationBlock("\n\nfunc run()") {
		t.Fatal("blank lines before code should still fence")
	}
	if l.jobTimeout() != 15*time.Second {
		t.Fatalf("job timeout=%v", l.jobTimeout())
	}
}

func TestLayer2BackgroundCandidateBranches(t *testing.T) {
	msgs := longLayer2Messages(12)

	l := testLayer2ForCurrentTests()
	l.chain.SetProviders()
	if got := l.ScoreBackgroundCandidateSession("s", msgs, 1); got.Reason != "provider_unconfigured" {
		t.Fatalf("reason=%q", got.Reason)
	}

	l = testLayer2ForCurrentTests()
	if got := l.ScoreBackgroundCandidateSession("s", msgs[:1], 1); got.Reason != "below_min_messages" {
		t.Fatalf("reason=%q", got.Reason)
	}

	l = testLayer2ForCurrentTests()
	l.SetCompressingSession("s", true)
	if got := l.ScoreBackgroundCandidateSession("s", msgs, 1); got.Reason != "already_compressing" {
		t.Fatalf("reason=%q", got.Reason)
	}

	l = testLayer2ForCurrentTests()
	l.cfg.MinTokensForLayer2 = 1_000_000
	if got := l.ScoreBackgroundCandidateSession("s", msgs, 1); got.Reason != "below_token_roi_gate" {
		t.Fatalf("reason=%q", got.Reason)
	}

	l = testLayer2ForCurrentTests()
	l.cfg.MinTokensForLayer2 = 100
	l.cfg.Summary.TargetRatio = 0.99
	if got := l.ScoreBackgroundCandidateSession("s", msgs, 1); got.Reason != "projected_savings_too_low" {
		t.Fatalf("reason=%q savings=%d", got.Reason, got.ProjectedSavingsTokens)
	}

	l = testLayer2ForCurrentTests()
	l.cfg.MinTokensForLayer2 = 1
	l.cfg.Summary.TargetRatio = 0.10
	if got := l.ScoreBackgroundCandidateSession("s", msgs, 1); !got.Eligible || got.Reason != "stale_or_missing_summary" {
		t.Fatalf("candidate=%+v", got)
	}

	l.sessions.Store("s", &CachedSummary{
		Summary:          validLayer2Summary(),
		CoveredRange:     [2]int{0, 2},
		OriginalTokens:   1000,
		CompressedTokens: 100,
		CreatedAt:        time.Now(),
		Hash:             hashMessages(msgs[:3]),
	})
	if got := l.ScoreBackgroundCandidateSession("s", msgs, 1); !got.Eligible || got.Reason != "coverage_below_threshold" {
		t.Fatalf("candidate=%+v", got)
	}

	l.sessions.Store("s2", &CachedSummary{
		Summary:          validLayer2Summary(),
		CoveredRange:     [2]int{0, 10},
		OriginalTokens:   1000,
		CompressedTokens: 100,
		CreatedAt:        time.Now(),
		Hash:             hashMessages(msgs[:11]),
	})
	if got := l.ScoreBackgroundCandidateSession("s2", msgs, 1); got.Eligible || got.Reason != "existing_summary_sufficient" {
		t.Fatalf("candidate=%+v", got)
	}

	if adaptiveLayer2MinTokens(0) != 0 ||
		adaptiveLayer2MinTokens(100) != adaptiveLayer2FloorTokens ||
		adaptiveLayer2MinTokens(100_000) != 55_000 {
		t.Fatal("adaptiveLayer2MinTokens branches failed")
	}
	l.cfg.MinTokensForLayer2 = 1
	if l.minProjectedLayer2Savings() != 1 {
		t.Fatal("small minProjectedLayer2Savings branch failed")
	}
	l.cfg.MinTokensForLayer2 = 4096
	if l.minProjectedLayer2Savings() != 512 {
		t.Fatal("middle minProjectedLayer2Savings branch failed")
	}
	l.cfg.MinTokensForLayer2 = 100_000
	if l.minProjectedLayer2Savings() != 2048 {
		t.Fatal("capped minProjectedLayer2Savings branch failed")
	}
}

func TestLayer2SessionAPIsAndApply(t *testing.T) {
	l := testLayer2ForCurrentTests()
	l.cfg.Summary.AllowModelFacingReplacement = true
	msgs := longLayer2Messages(8)
	if _, _, ok := l.ApplyToMessagesSession("s", msgs); ok {
		t.Fatal("missing cache should not apply")
	}
	if _, ok := l.CompressionCandidateHash(msgs, 1); !ok {
		t.Fatal("candidate hash should exist")
	}
	if _, ok := l.CompressionCandidateHash(msgs[:1], 99); ok {
		t.Fatal("oversized window should not produce candidate hash")
	}
	hash, _ := l.CompressionCandidateHash(msgs, 1)
	l.MarkCompressionCandidate("s", hash)
	if !l.IsCurrentCompressionCandidate("s", hash) {
		t.Fatal("candidate hash should match after mark")
	}
	l.RecordStaleCompressionJobSkip()
	l.SetCompressingSession("s", true)
	if !l.GetSessionCache().Compressing("s") {
		t.Fatal("session compressing flag not set")
	}
	l.SetCompressingSession("s", false)

	cached := &CachedSummary{
		Summary:          validLayer2Summary(),
		CoveredRange:     [2]int{0, 4},
		OriginalTokens:   1000,
		CompressedTokens: 100,
		CreatedAt:        time.Now(),
		Hash:             hashMessages(msgs[:5]),
	}
	l.sessions.Store("s", cached)
	out, saved, ok := l.ApplyToMessagesSession("s", msgs)
	if !ok || saved != 900 || len(out) >= len(msgs) {
		t.Fatalf("apply failed ok=%v saved=%d len=%d", ok, saved, len(out))
	}
	if !strings.Contains(out[0].Content[0].Text, "Conversation summary covering messages 0-4") {
		t.Fatalf("summary text wrong: %+v", out[0])
	}
	if got := buildSummaryText(4, []int{1, 3}, "summary"); !strings.Contains(got, "excluding anchors at 1, 3") {
		t.Fatalf("anchor summary text wrong: %q", got)
	}
	if selectAnchors(nil, nil, 1) != nil {
		t.Fatal("empty stored anchors should return nil")
	}
	l.InvalidateSession("s")
	if _, _, ok := l.ApplyToMessagesSession("s", msgs); ok {
		t.Fatal("invalidated session should not apply")
	}
	l.InvalidateAllSessions()
	_ = l.CacheStats()
}

func TestProgressiveTiersCurrentContract(t *testing.T) {
	if got := DetermineCompressionTiers(5, 5); got != nil {
		t.Fatalf("no compressible messages should return nil: %#v", got)
	}
	short := DetermineCompressionTiers(10, 2)
	if len(short) != 1 || short[0].Name != "tier-single" {
		t.Fatalf("short tier plan wrong: %#v", short)
	}
	long := DetermineCompressionTiers(45, 5)
	if len(long) != 4 || long[0].Name != "tier-1" || long[len(long)-1].Name != "window" {
		t.Fatalf("long tier plan wrong: %#v", long)
	}
	if ratioStr(0.25) != "25%" {
		t.Fatalf("ratio string wrong")
	}
}

func TestApplyProgressiveTiersCurrentBranches(t *testing.T) {
	l := testLayer2ForCurrentTests()
	msgs := longLayer2Messages(8)
	if got := l.applyProgressiveTiersWithContext(context.Background(), msgs, nil); len(got) != len(msgs) {
		t.Fatalf("nil tiers should return original length")
	}

	verbatim := l.applyProgressiveTiersWithContext(context.Background(), msgs, []CompressionTier{{
		Name:        "window",
		MsgRange:    [2]int{0, 2},
		TargetRatio: 1.0,
	}})
	if len(verbatim) != 3 || verbatim[2].Index != 2 {
		t.Fatalf("verbatim tier wrong: %#v", verbatim)
	}

	l.chain.SetProviders(&stubSummarizer{name: "stub", configured: true, result: validProgressiveSummary()})
	compressed := l.applyProgressiveTiersWithContext(context.Background(), msgs, []CompressionTier{{
		Name:        "tier-1",
		MsgRange:    [2]int{0, 5},
		TargetRatio: 0.2,
	}})
	if len(compressed) != 1 || !strings.Contains(compressed[0].Content[0].Text, "tier-1 summary") {
		t.Fatalf("expected synthetic summary: %#v", compressed)
	}

	l.chain.SetProviders(&stubSummarizer{name: "boom", configured: true, err: errors.New("boom")})
	failed := l.applyProgressiveTiersWithContext(context.Background(), msgs, []CompressionTier{{
		Name:        "tier-err",
		MsgRange:    [2]int{0, 1},
		TargetRatio: 0.2,
	}})
	if len(failed) != 2 {
		t.Fatalf("failed tier should keep verbatim: %#v", failed)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tail := l.applyProgressiveTiersWithContext(ctx, msgs, []CompressionTier{{
		Name:        "tier-canceled",
		MsgRange:    [2]int{2, 5},
		TargetRatio: 0.2,
	}})
	if len(tail) != len(msgs)-2 || tail[0].Index != 0 {
		t.Fatalf("canceled tier should append tail reindexed: %#v", tail)
	}
}

func TestApplyProgressiveTiersAdditionalBranches(t *testing.T) {
	l := testLayer2ForCurrentTests()
	msgs := longLayer2Messages(8)
	if got := l.ApplyProgressiveTiers(msgs, []CompressionTier{{
		Name:        "wrapper",
		MsgRange:    [2]int{0, 1},
		TargetRatio: 1.0,
	}}); len(got) != 2 || got[0].Index != 0 || got[1].Index != 1 {
		t.Fatalf("wrapper verbatim result wrong: %#v", got)
	}

	if got := l.applyProgressiveTiersWithContext(context.Background(), msgs, []CompressionTier{{
		Name:        "past-end",
		MsgRange:    [2]int{99, 120},
		TargetRatio: 1.0,
	}}); len(got) != 0 {
		t.Fatalf("past-end tier should break without output: %#v", got)
	}

	if got := l.applyProgressiveTiersWithContext(context.Background(), msgs, []CompressionTier{{
		Name:        "cap-end",
		MsgRange:    [2]int{6, 99},
		TargetRatio: 1.0,
	}}); len(got) != 2 || got[1].Index != 1 {
		t.Fatalf("end-capped tier wrong: %#v", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := appendVerbatimTail(nil, msgs, -3, 0); len(got) != len(msgs) || got[0].Index != 0 {
		t.Fatalf("negative tail start wrong: %#v", got)
	}
	if got := appendVerbatimTail(nil, msgs, 99, 0); len(got) != 0 {
		t.Fatalf("past-end tail should be empty: %#v", got)
	}
	if got := l.applyProgressiveTiersWithContext(ctx, msgs, []CompressionTier{{
		Name:        "ctx-done",
		MsgRange:    [2]int{99, 120},
		TargetRatio: 0.2,
	}}); len(got) != 0 {
		t.Fatalf("canceled past-end tier should return empty tail: %#v", got)
	}

	l = testLayer2ForCurrentTests()
	l.chain.SetProviders(&stubSummarizer{name: "bad", configured: true, result: "not bullets"})
	invalid := l.applyProgressiveTiersWithContext(context.Background(), msgs, []CompressionTier{{
		Name:        "invalid",
		MsgRange:    [2]int{0, 1},
		TargetRatio: 0.2,
	}})
	if len(invalid) != 2 || invalid[0].Role != msgs[0].Role {
		t.Fatalf("invalid summary should keep verbatim: %#v", invalid)
	}

	l = testLayer2ForCurrentTests()
	anchorMsgs := []types.Message{
		{Index: 0, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "edit_file"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "apply_patch"}}},
	}
	l.chain.SetProviders(&stubSummarizer{name: "unused", configured: true, result: validProgressiveSummary()})
	allAnchors := l.applyProgressiveTiersWithContext(context.Background(), anchorMsgs, []CompressionTier{{
		Name:        "all-anchors",
		MsgRange:    [2]int{0, 1},
		TargetRatio: 0.2,
	}})
	if len(allAnchors) != 2 || allAnchors[1].Index != 1 {
		t.Fatalf("all-anchor tier should stay verbatim: %#v", allAnchors)
	}

	l = testLayer2ForCurrentTests()
	withAnchor := []types.Message{
		{Index: 0, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "edit_file"}}},
		{Index: 1, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("alpha beta gamma delta. ", 80)}}},
	}
	l.chain.SetProviders(&stubSummarizer{name: "summary", configured: true, result: strings.Repeat("- alpha beta gamma delta epsilon.\n", 8)})
	anchoredSummary := l.applyProgressiveTiersWithContext(context.Background(), withAnchor, []CompressionTier{{
		Name:        "mixed-anchor",
		MsgRange:    [2]int{0, 1},
		TargetRatio: 0.2,
	}})
	if len(anchoredSummary) != 2 || anchoredSummary[0].Content[0].ToolName != "edit_file" || !strings.Contains(anchoredSummary[1].Content[0].Text, "mixed-anchor summary") {
		t.Fatalf("mixed anchor tier should preserve anchor before summary: %#v", anchoredSummary)
	}

	l = testLayer2ForCurrentTests()
	ctx2, cancel2 := context.WithCancel(context.Background())
	l.chain.SetProviders(&cancelingSummarizer{cancel: cancel2, result: validProgressiveSummary()})
	canceledAfterSummarize := l.applyProgressiveTiersWithContext(ctx2, msgs, []CompressionTier{{
		Name:        "cancel-after-summary",
		MsgRange:    [2]int{2, 4},
		TargetRatio: 0.2,
	}})
	if len(canceledAfterSummarize) != len(msgs)-2 || canceledAfterSummarize[0].Index != 0 {
		t.Fatalf("cancel after summarize should append verbatim tail: %#v", canceledAfterSummarize)
	}
}

func TestLegacyTelemetryPromptParsingAndStubs(t *testing.T) {
	body, version := parsePromptDocument("---\nversion: v42\nother: kept\n---\nhello")
	if body != "hello" || version != "v42" {
		t.Fatalf("yaml prompt parse body=%q version=%q", body, version)
	}
	body, version = parsePromptDocument("---\nname: no-version\n---\nhello")
	if body != "hello" || version != "custom" {
		t.Fatalf("yaml no-version parse body=%q version=%q", body, version)
	}
	body, version = parsePromptDocument("// version: one-line")
	if body != "" || version != "one-line" {
		t.Fatalf("single-line header parse body=%q version=%q", body, version)
	}
	body, version = parsePromptDocument("# version: hash\nbody")
	if body != "body" || version != "hash" {
		t.Fatalf("hash header parse body=%q version=%q", body, version)
	}
	body, version = parsePromptDocument("---\nunterminated")
	if body != "---\nunterminated" || version != "custom" {
		t.Fatalf("unterminated front matter should fall back, body=%q version=%q", body, version)
	}

	if ExamplePromptCount("go") != 0 || len(ExamplePromptCounts()) != 0 {
		t.Fatal("legacy prompt telemetry should be zero")
	}
	ResetExamplePromptCounts()
	if CoTTagCount("think") != 0 || len(CoTTagCounts()) != 0 {
		t.Fatal("legacy CoT telemetry should be zero")
	}
	ResetCoTTagCounts()
	RecordLineageStats("[L1]")
	marked, total := LineageMarkerCounts()
	if LineageMarkerRate() != 0 || marked != 0 || total != 0 {
		t.Fatalf("legacy lineage telemetry should be zero: marked=%d total=%d rate=%v", marked, total, LineageMarkerRate())
	}
	ResetLineageMarkerStats()
}

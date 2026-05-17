package summarization

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

func TestSessionCache_StoreGet(t *testing.T) {
	t.Parallel()
	sc := NewSessionCache(10)
	sum := &CachedSummary{Summary: "s1", CoveredRange: [2]int{0, 5}, CreatedAt: time.Now()}
	sc.Store("sess-a", sum)

	got := sc.Get("sess-a")
	if got == nil || got.Summary != "s1" {
		t.Fatalf("got %v", got)
	}
	if sc.Get("nonexistent") != nil {
		t.Fatal("expected nil for missing session")
	}
}

func TestSessionCache_GetCurrent(t *testing.T) {
	t.Parallel()
	sc := NewSessionCache(10)
	sum := &CachedSummary{Summary: "s1", CoveredRange: [2]int{0, 3}, CreatedAt: time.Now()}
	sc.Store("sess-a", sum)

	cached, r := sc.GetCurrent("sess-a")
	if cached == nil || r != [2]int{0, 3} {
		t.Fatalf("got cached=%v range=%v", cached, r)
	}
	cached, r = sc.GetCurrent("nonexistent")
	if cached != nil {
		t.Fatal("expected nil")
	}
}

func TestSessionCache_GetCurrentWithHash(t *testing.T) {
	t.Parallel()
	sc := NewSessionCache(10)
	sum := &CachedSummary{Summary: "s1", CoveredRange: [2]int{0, 3}, Hash: [32]byte{1}, CreatedAt: time.Now()}
	sc.Store("sess-a", sum)

	cached, _ := sc.GetCurrentWithHash("sess-a", [32]byte{1})
	if cached == nil {
		t.Fatal("expected hit")
	}
	cached, _ = sc.GetCurrentWithHash("sess-a", [32]byte{2})
	if cached != nil {
		t.Fatal("expected hash mismatch miss")
	}
	cached, _ = sc.GetCurrentWithHash("nonexistent", [32]byte{})
	if cached != nil {
		t.Fatal("expected nil for missing")
	}
}

func TestSessionCache_GetCurrentMatchingPrefixRejectsChangedPrefix(t *testing.T) {
	t.Parallel()
	sc := NewSessionCache(10)
	msgs := makeTestMessages(4)
	sc.Store("sess-a", &CachedSummary{
		Summary:      "s1",
		CoveredRange: [2]int{0, 1},
		Hash:         hashMessages(msgs[:2]),
		CreatedAt:    time.Now(),
	})

	changed := append([]types.Message(nil), msgs...)
	changed[1].Content = append([]types.ContentBlock(nil), changed[1].Content...)
	changed[1].Content[0].Text = "changed prefix"
	cached, _ := sc.GetCurrentMatchingPrefix("sess-a", changed)
	if cached != nil {
		t.Fatal("expected hash mismatch to block cached summary apply")
	}
	if got := sc.Stats().HashMisses; got != 1 {
		t.Fatalf("hash mismatch telemetry = %d, want 1", got)
	}
}

func TestSessionCache_CandidateHashTelemetry(t *testing.T) {
	t.Parallel()
	sc := NewSessionCache(10)
	want := [32]byte{7}
	sc.SetCandidateHash("sess-a", want)
	if !sc.CandidateHashMatches("sess-a", want) {
		t.Fatal("expected candidate hash match")
	}
	if sc.CandidateHashMatches("sess-a", [32]byte{8}) {
		t.Fatal("expected candidate hash mismatch")
	}
	sc.RecordStaleJobSkip()
	stats := sc.Stats()
	if stats.CandidateSets != 1 || stats.StaleJobSkips != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestSessionCache_CandidateHashMissingAndZero(t *testing.T) {
	t.Parallel()
	sc := NewSessionCache(10)
	if sc.CandidateHashMatches("missing", [32]byte{1}) {
		t.Fatal("missing candidate hash should not match")
	}
	sc.SetCandidateHash("sess-a", [32]byte{})
	if !sc.CandidateHashMatches("sess-a", [32]byte{}) || sc.CandidateHashMatches("sess-a", [32]byte{1}) {
		t.Fatal("set zero candidate hash should match only zero")
	}
}

func TestSessionCache_Compressing(t *testing.T) {
	t.Parallel()
	sc := NewSessionCache(10)
	if sc.Compressing("sess-a") {
		t.Fatal("initially false")
	}
	sc.SetCompressing("sess-a", true)
	if !sc.Compressing("sess-a") {
		t.Fatal("expected true")
	}
	sc.SetCompressing("sess-a", false)
	if sc.Compressing("sess-a") {
		t.Fatal("expected false")
	}
	if sc.Compressing("nonexistent") {
		t.Fatal("missing session should be false")
	}
}

func TestSessionCache_Invalidate(t *testing.T) {
	t.Parallel()
	sc := NewSessionCache(10)
	sc.Store("sess-a", &CachedSummary{Summary: "s1", CreatedAt: time.Now()})
	sc.Invalidate("sess-a")
	if sc.Get("sess-a") != nil {
		t.Fatal("expected nil after invalidate")
	}
	sc.Invalidate("nonexistent")
}

func TestSessionCache_InvalidateAll(t *testing.T) {
	t.Parallel()
	sc := NewSessionCache(10)
	sc.Store("a", &CachedSummary{Summary: "a", CreatedAt: time.Now()})
	sc.Store("b", &CachedSummary{Summary: "b", CreatedAt: time.Now()})
	sc.InvalidateAll()
	if sc.Get("a") != nil || sc.Get("b") != nil {
		t.Fatal("expected all nil")
	}
}

func TestSessionCache_IsStale(t *testing.T) {
	t.Parallel()
	sc := NewSessionCache(10)
	if !sc.IsStale("missing", time.Minute) {
		t.Fatal("missing session is stale")
	}
	sc.Store("a", &CachedSummary{CreatedAt: time.Now()})
	if sc.IsStale("a", time.Hour) {
		t.Fatal("fresh summary not stale")
	}
	sc.Store("b", &CachedSummary{CreatedAt: time.Now().Add(-2 * time.Hour)})
	if !sc.IsStale("b", time.Hour) {
		t.Fatal("old summary is stale")
	}
}

func TestSessionCache_LRUEviction(t *testing.T) {
	t.Parallel()
	sc := NewSessionCache(2)
	sc.Store("a", &CachedSummary{Summary: "a", CreatedAt: time.Now()})
	sc.Store("b", &CachedSummary{Summary: "b", CreatedAt: time.Now()})
	sc.Store("c", &CachedSummary{Summary: "c", CreatedAt: time.Now()})

	if sc.Get("a") != nil {
		t.Fatal("a should be evicted")
	}
	if sc.Get("b") == nil || sc.Get("c") == nil {
		t.Fatal("b and c should remain")
	}
}

func TestSessionCache_OverwriteDoesNotEvict(t *testing.T) {
	t.Parallel()
	sc := NewSessionCache(2)
	sc.Store("a", &CachedSummary{Summary: "a1", CreatedAt: time.Now()})
	sc.Store("a", &CachedSummary{Summary: "a2", CreatedAt: time.Now()})
	if sc.SessionCount() != 1 {
		t.Fatalf("expected 1 session, got %d", sc.SessionCount())
	}
}

func TestSessionCache_SessionCount(t *testing.T) {
	t.Parallel()
	sc := NewSessionCache(10)
	if sc.SessionCount() != 0 {
		t.Fatal("expected 0")
	}
	sc.Store("a", &CachedSummary{CreatedAt: time.Now()})
	if sc.SessionCount() != 1 {
		t.Fatalf("expected 1, got %d", sc.SessionCount())
	}
}

func TestSessionCache_Stats(t *testing.T) {
	t.Parallel()
	sc := NewSessionCache(10)
	sc.Store("a", &CachedSummary{CreatedAt: time.Now()})
	sc.Get("a")
	sc.Get("missing")

	stats := sc.Stats()
	if stats.Sessions != 1 {
		t.Fatalf("sessions=%d", stats.Sessions)
	}
	if stats.Hits != 1 {
		t.Fatalf("hits=%d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Fatalf("misses=%d", stats.Misses)
	}
}

func TestSessionCache_Demotion(t *testing.T) {
	t.Parallel()
	sc := NewSessionCache(10)
	sc.Store("a", &CachedSummary{Summary: "first", CoveredRange: [2]int{0, 3}, CreatedAt: time.Now()})
	sc.Store("a", &CachedSummary{Summary: "second", CoveredRange: [2]int{0, 5}, CreatedAt: time.Now()})

	got := sc.Get("a")
	if got == nil || got.Summary != "second" {
		t.Fatalf("expected second, got %v", got)
	}
}

func TestNewSessionCache_DefaultMax(t *testing.T) {
	t.Parallel()
	sc := NewSessionCache(0)
	if sc.maxSessions != defaultMaxSessions {
		t.Fatalf("expected default %d, got %d", defaultMaxSessions, sc.maxSessions)
	}
}

func TestSummaryCache_GetInner(t *testing.T) {
	t.Parallel()
	c := NewSummaryCache()
	if c.GetInner() == nil {
		t.Fatal("expected non-nil inner")
	}
}

func TestLayer2_ApplyToMessagesSession(t *testing.T) {
	t.Parallel()
	l := NewLayer2(testCompressionConfig())
	l.sessions.Store("s1", &CachedSummary{
		Summary:          "test summary",
		CoveredRange:     [2]int{0, 5},
		OriginalTokens:   100,
		CompressedTokens: 20,
		CreatedAt:        time.Now(),
	})
	msgs := makeTestMessages(10)
	result, saved, applied := l.ApplyToMessagesSession("s1", msgs)
	if !applied {
		t.Fatal("expected applied")
	}
	if saved != 80 {
		t.Fatalf("saved=%d", saved)
	}
	if len(result) != 5 {
		t.Fatalf("len(result)=%d", len(result))
	}
	result, _, applied = l.ApplyToMessagesSession("missing", msgs)
	if applied {
		t.Fatal("missing session should not apply")
	}

	l.sessions.Store("s-end0", &CachedSummary{
		Summary:      "zero end",
		CoveredRange: [2]int{0, 0},
		CreatedAt:    time.Now(),
	})
	_, _, applied = l.ApplyToMessagesSession("s-end0", msgs)
	if applied {
		t.Fatal("end=0 should not apply")
	}

	l.sessions.Store("s-endfull", &CachedSummary{
		Summary:      "full end",
		CoveredRange: [2]int{0, len(msgs)},
		CreatedAt:    time.Now(),
	})
	_, _, applied = l.ApplyToMessagesSession("s-endfull", msgs)
	if applied {
		t.Fatal("end=len should not apply")
	}
}

func TestLayer2_ShouldTriggerCompressionSession_Compressing(t *testing.T) {
	cfg := testCompressionConfig()
	t.Setenv("TEST_KEY", "test")
	l := NewLayer2(cfg)
	l.SetCompressingSession("s1", true)
	msgs := makeTestMessages(20)
	if l.ShouldTriggerCompressionSession("s1", msgs) {
		t.Fatal("should not trigger when compressing")
	}
}

func TestLayer2_ShouldTriggerCompressionSession_Stale(t *testing.T) {
	cfg := testCompressionConfig()
	t.Setenv("TEST_KEY", "test")
	l := NewLayer2(cfg)
	l.sessions.Store("s1", &CachedSummary{
		Summary:      "old",
		CoveredRange: [2]int{0, 1},
		CreatedAt:    time.Now().Add(-2 * time.Hour),
	})
	msgs := makeTestMessages(20)
	if !l.ShouldTriggerCompressionSession("s1", msgs) {
		t.Fatal("stale cache should trigger")
	}
}

func TestLayer2_ShouldTriggerCompressionSession_CoveredFraction(t *testing.T) {
	cfg := testCompressionConfig()
	t.Setenv("TEST_KEY", "test")
	l := NewLayer2(cfg)
	l.sessions.Store("s1", &CachedSummary{
		Summary:      "recent",
		CoveredRange: [2]int{0, 1},
		CreatedAt:    time.Now(),
	})
	msgs := makeTestMessages(20)
	if !l.ShouldTriggerCompressionSession("s1", msgs) {
		t.Fatal("low covered fraction should trigger")
	}
}

func TestLayer2_ShouldTriggerCompressionSession_FullyCovered(t *testing.T) {
	cfg := testCompressionConfig()
	t.Setenv("TEST_KEY", "test")
	l := NewLayer2(cfg)
	msgs := makeTestMessages(20)
	prefixEnd := len(msgs)
	l.sessions.Store("s1", &CachedSummary{
		Summary:      "full",
		CoveredRange: [2]int{0, prefixEnd - 2},
		CreatedAt:    time.Now(),
	})
	if l.ShouldTriggerCompressionSession("s1", msgs) {
		t.Fatal("fully covered should not trigger")
	}
}

func TestLayer2_ShouldTriggerCompressionSession_TooFewMessages(t *testing.T) {
	cfg := testCompressionConfig()
	t.Setenv("TEST_KEY", "test")
	cfg.MinMessagesForCompression = 100
	l := NewLayer2(cfg)
	msgs := makeTestMessages(5)
	if l.ShouldTriggerCompressionSession("s1", msgs) {
		t.Fatal("too few messages should not trigger")
	}
}

func TestLayer2_ShouldTriggerCompressionSession_MinTokensGate(t *testing.T) {
	cfg := testCompressionConfig()
	t.Setenv("TEST_KEY", "test")
	cfg.MinTokensForLayer2 = 999999
	l := NewLayer2(cfg)
	msgs := makeTestMessages(20)
	if l.ShouldTriggerCompressionSession("s1", msgs) {
		t.Fatal("min tokens gate should block")
	}
}

func TestLayer2_ShouldTriggerCompressionSession_AdaptiveToolOutputROI(t *testing.T) {
	cfg := testCompressionConfig()
	cfg.MinTokensForLayer2 = 15000
	cfg.MinMessagesForCompression = 3
	cfg.SlidingWindow = 2
	t.Setenv("TEST_KEY", "test")
	l := NewLayer2(cfg)
	msgs := makeToolHeavyMessages(12, strings.Repeat("tool output ", 500))
	if !l.ShouldTriggerCompressionSession("s1", msgs) {
		t.Fatal("tool-heavy prefix above adaptive floor should trigger before the fixed threshold")
	}

	recentEdit := makeToolHeavyMessages(12, strings.Repeat("tool output ", 500))
	recentEdit[len(recentEdit)-1] = types.Message{
		Index: len(recentEdit) - 1,
		Role:  "assistant",
		Content: []types.ContentBlock{{
			Type:     "tool_use",
			ToolName: "apply_patch",
		}},
	}
	if l.ShouldTriggerCompressionSession("s2", recentEdit) {
		t.Fatal("recent edit anchor should block adaptive early trigger")
	}

	prose := makeTestMessages(30)
	for i := range prose {
		prose[i].Content[0].Text = strings.Repeat("plain prose ", 150)
	}
	if l.ShouldTriggerCompressionSession("s3", prose) {
		t.Fatal("large prose below fixed threshold should not use tool-output adaptive gate")
	}
}

func TestLayer2_ScoreBackgroundCandidateSessionTelemetry(t *testing.T) {
	cfg := testCompressionConfig()
	cfg.MinTokensForLayer2 = 15000
	cfg.MinMessagesForCompression = 3
	cfg.SlidingWindow = 2
	t.Setenv("TEST_KEY", "test")
	l := NewLayer2(cfg)

	candidate := l.ScoreBackgroundCandidateSession("s1", makeToolHeavyMessages(12, strings.Repeat("tool output ", 500)), 2)
	if !candidate.Eligible {
		t.Fatalf("expected eligible tool-heavy candidate, got %+v", candidate)
	}
	if candidate.ProjectedSavingsTokens <= 0 || candidate.ToolTokenShare < adaptiveLayer2ToolTokenShareMin {
		t.Fatalf("weak candidate telemetry: %+v", candidate)
	}

	recentEdit := makeToolHeavyMessages(12, strings.Repeat("tool output ", 500))
	recentEdit[len(recentEdit)-1] = types.Message{
		Index: len(recentEdit) - 1,
		Role:  "assistant",
		Content: []types.ContentBlock{{
			Type:     "tool_use",
			ToolName: "apply_patch",
		}},
	}
	blocked := l.ScoreBackgroundCandidateSession("s2", recentEdit, 2)
	if blocked.Eligible || blocked.Reason != "recent_sensitive_anchor" {
		t.Fatalf("expected recent-sensitive-anchor block, got %+v", blocked)
	}
}

func TestLayer2_BackgroundCandidateBranchCoverage(t *testing.T) {
	cfg := testCompressionConfig()
	l := NewLayer2(cfg)
	// 2026-05-15: with ExtractSummarizer as the primary, in-process
	// deterministic provider, the chain is always configured even
	// without MiniMax credentials. So the "provider_unconfigured"
	// gate from the old MiniMax-only world no longer fires.
	// The eligible path produces a "stale_or_missing_summary" reason
	// instead — the new correct behaviour because we now have a
	// summarization engine that needs no API key.
	if got := l.ScoreBackgroundCandidateSession("s1", makeTestMessages(10), 2); got.Reason != "stale_or_missing_summary" {
		t.Fatalf("expected stale_or_missing_summary with always-on extract provider, got %+v", got)
	}

	t.Setenv("TEST_KEY_BRANCH", "test-key")
	cfg = testCompressionConfig()
	cfg.SlidingWindow = 2
	cfg.MinMessagesForCompression = 20
	l = NewLayer2(cfg)
	heavy := makeToolHeavyMessages(8, strings.Repeat("tool output ", 80))
	if got := l.ScoreBackgroundCandidateSession("s1", heavy, 2); got.Reason != "below_min_messages" {
		t.Fatalf("below-min branch mismatch: %+v", got)
	}

	cfg.MinMessagesForCompression = 3
	l = NewLayer2(cfg)
	l.SetCompressingSession("s1", true)
	if got := l.ScoreBackgroundCandidateSession("s1", heavy, 2); got.Reason != "already_compressing" {
		t.Fatalf("compressing branch mismatch: %+v", got)
	}

	cfg.MinMessagesForCompression = 0
	l = NewLayer2(cfg)
	if got := l.ScoreBackgroundCandidateSession("s2", nil, 5); got.Reason != "empty_existing_boundary" || !got.Eligible {
		t.Fatalf("empty-boundary branch mismatch: %+v", got)
	}

	cfg.MinMessagesForCompression = 3
	cfg.MinTokensForLayer2 = 100000
	l = NewLayer2(cfg)
	if got := l.ScoreBackgroundCandidateSession("s3", heavy, 2); got.Reason != "below_token_roi_gate" {
		t.Fatalf("token-gate branch mismatch: %+v", got)
	}

	cfg.MinTokensForLayer2 = 10
	cfg.Summary.TargetRatio = 0.99
	l = NewLayer2(cfg)
	if got := l.ScoreBackgroundCandidateSession("s4", heavy, 2); got.Reason != "projected_savings_too_low" {
		t.Fatalf("projected-savings branch mismatch: %+v", got)
	}

	cfg.MinTokensForLayer2 = 0
	cfg.Summary.TargetRatio = 0.2
	l = NewLayer2(cfg)
	if got := l.ScoreBackgroundCandidateSession("s5-default-window", heavy, 0); got.Reason != "stale_or_missing_summary" || !got.Eligible {
		t.Fatalf("default-window branch mismatch: %+v", got)
	}
	if got := l.ScoreBackgroundCandidateSession("s5", heavy, 2); got.Reason != "stale_or_missing_summary" || !got.Eligible {
		t.Fatalf("stale branch mismatch: %+v", got)
	}
	l.sessions.Store("s6", &CachedSummary{Summary: "fresh", CoveredRange: [2]int{0, 1}, CreatedAt: time.Now()})
	if got := l.ScoreBackgroundCandidateSession("s6", heavy, 2); got.Reason != "coverage_below_threshold" || !got.Eligible {
		t.Fatalf("coverage branch mismatch: %+v", got)
	}
	l.sessions.Store("s7", &CachedSummary{Summary: "fresh", CoveredRange: [2]int{0, 5}, CreatedAt: time.Now()})
	if got := l.ScoreBackgroundCandidateSession("s7", heavy, 2); got.Reason != "existing_summary_sufficient" || got.Eligible {
		t.Fatalf("sufficient branch mismatch: %+v", got)
	}

	cfg.MinTokensForLayer2 = 10
	l = NewLayer2(cfg)
	if l.passesLayer2TokenGate(heavy, -1, 2) {
		t.Fatal("invalid prefix should fail token gate")
	}
	cfg.MinTokensForLayer2 = 0
	l = NewLayer2(cfg)
	if !l.passesLayer2TokenGate(heavy, 2, 2) {
		t.Fatal("zero min token gate should pass")
	}
	if adaptiveLayer2MinTokens(0) != 0 || adaptiveLayer2MinTokens(1000) != adaptiveLayer2FloorTokens || adaptiveLayer2MinTokens(10000) != adaptiveLayer2FloorTokens {
		t.Fatal("adaptive min token helper mismatch")
	}
	if l.recentSensitiveAnchor(nil, 0, 1) {
		t.Fatal("empty messages should not have recent anchor")
	}

	cfg.MinTokensForLayer2 = 50000
	l = NewLayer2(cfg)
	if got := l.minProjectedLayer2Savings(); got != 2048 {
		t.Fatalf("max projected savings floor mismatch: %d", got)
	}
	l.anchor = nil
	if l.recentSensitiveAnchor(heavy, 0, 2) {
		t.Fatal("nil anchor detector should not find recent anchors")
	}
	cfg.MinTokensForLayer2 = 15000
	l = NewLayer2(cfg)
	recentAdaptive := append([]types.Message(nil), heavy...)
	recentAdaptive[len(recentAdaptive)-1] = toolUseMsg(t, len(recentAdaptive)-1, "edit_file")
	if l.adaptiveLayer2ROICandidate(recentAdaptive, len(recentAdaptive)-2, 2, adaptiveLayer2FloorTokens+3000) {
		t.Fatal("recent sensitive anchor should block adaptive ROI candidate")
	}
	if l.adaptiveLayer2ROICandidate(makeTestMessages(20), 10, 2, adaptiveLayer2FloorTokens+3000) {
		t.Fatal("low tool-token volume should block adaptive ROI candidate")
	}
	cfg.MinMessagesForCompression = 1
	cfg.MinTokensForLayer2 = 0
	l = NewLayer2(cfg)
	onePrefix := makeToolHeavyMessages(1, strings.Repeat("tool output ", 80))
	l.sessions.Store("boundary-zero", &CachedSummary{Summary: "fresh", CoveredRange: [2]int{0, 0}, CreatedAt: time.Now()})
	if got := l.ScoreBackgroundCandidateSession("boundary-zero", onePrefix, 5); got.Reason != "empty_existing_boundary" || !got.Eligible {
		t.Fatalf("boundary zero branch mismatch: %+v", got)
	}
}

func TestLayer2CandidateHashHelpers(t *testing.T) {
	t.Parallel()
	cfg := testCompressionConfig()
	cfg.SlidingWindow = 5
	l := NewLayer2(cfg)
	if _, ok := l.CompressionCandidateHash(makeTestMessages(2), 5); ok {
		t.Fatal("no compressible prefix should not hash")
	}
	_, _ = l.CompressionCandidateHash(makeTestMessages(10), 0)
	hash, ok := l.CompressionCandidateHash(makeTestMessages(10), 2)
	if !ok || hash == ([32]byte{}) {
		t.Fatalf("expected candidate hash, ok=%v hash=%x", ok, hash)
	}
	l.MarkCompressionCandidate("sess", hash)
	if !l.IsCurrentCompressionCandidate("sess", hash) {
		t.Fatal("candidate hash should match after mark")
	}
	l.RecordStaleCompressionJobSkip()
	if got := l.GetSessionCache().Stats().StaleJobSkips; got != 1 {
		t.Fatalf("stale skip telemetry=%d", got)
	}
}

func TestLayer2_ShouldTriggerCompressionSession_NoProvider(t *testing.T) {
	cfg := testCompressionConfig()
	l := NewLayer2(cfg)
	msgs := makeTestMessages(20)
	// 2026-05-15: ExtractSummarizer is always configured (no API key
	// needed). With it in the chain, compression CAN trigger even
	// without any MiniMax credentials. Previously this test asserted
	// the opposite under the MiniMax-only assumption.
	if !l.ShouldTriggerCompressionSession("s1", msgs) {
		t.Fatal("expected compression to trigger via always-on extract provider")
	}
}

func TestLayer2_ShouldTriggerCompressionSession_BoundaryZero(t *testing.T) {
	cfg := testCompressionConfig()
	t.Setenv("TEST_KEY", "test")
	cfg.MinMessagesForCompression = 0
	cfg.SlidingWindow = 1
	l := NewLayer2(cfg)
	l.sessions.Store("s1", &CachedSummary{
		Summary:      "fresh",
		CoveredRange: [2]int{0, 1},
		CreatedAt:    time.Now(),
	})
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "hi"}}},
	}
	if !l.ShouldTriggerCompressionSession("s1", msgs) {
		t.Fatal("boundaryIdx=0 should trigger")
	}
}

func TestLayer2_RunCompressionJobSession(t *testing.T) {
	cfg := testCompressionConfig()
	t.Setenv("TEST_KEY", "test")
	l := NewLayer2(cfg)
	msgs := makeTestMessages(5)
	l.RunCompressionJobSession(context.Background(), "s1", msgs)
}

func TestLayer2_SetCompressingSession(t *testing.T) {
	t.Parallel()
	l := NewLayer2(testCompressionConfig())
	l.SetCompressingSession("s1", true)
	if !l.sessions.Compressing("s1") {
		t.Fatal("expected true")
	}
	l.SetCompressingSession("s1", false)
	if l.sessions.Compressing("s1") {
		t.Fatal("expected false")
	}
}

func TestLayer2_InvalidateSession(t *testing.T) {
	t.Parallel()
	l := NewLayer2(testCompressionConfig())
	l.sessions.Store("s1", &CachedSummary{Summary: "s", CreatedAt: time.Now()})
	l.InvalidateSession("s1")
	if l.sessions.Get("s1") != nil {
		t.Fatal("expected nil after invalidate")
	}
}

func TestLayer2_InvalidateAllSessions(t *testing.T) {
	t.Parallel()
	l := NewLayer2(testCompressionConfig())
	l.sessions.Store("a", &CachedSummary{Summary: "a", CreatedAt: time.Now()})
	l.sessions.Store("b", &CachedSummary{Summary: "b", CreatedAt: time.Now()})
	l.InvalidateAllSessions()
	if l.sessions.Get("a") != nil || l.sessions.Get("b") != nil {
		t.Fatal("expected all nil")
	}
}

func TestLayer2_CacheStats(t *testing.T) {
	t.Parallel()
	l := NewLayer2(testCompressionConfig())
	l.sessions.Store("a", &CachedSummary{CreatedAt: time.Now()})
	stats := l.CacheStats()
	if stats.Sessions != 1 {
		t.Fatalf("sessions=%d", stats.Sessions)
	}
}

func TestLayer2_GetSessionCache(t *testing.T) {
	t.Parallel()
	l := NewLayer2(testCompressionConfig())
	if l.GetSessionCache() == nil {
		t.Fatal("expected non-nil")
	}
}

func TestConcurrentSessions(t *testing.T) {
	t.Parallel()
	sc := NewSessionCache(64)
	done := make(chan bool, 2)

	go func() {
		for i := 0; i < 100; i++ {
			sc.Store("a", &CachedSummary{Summary: "from-a", CreatedAt: time.Now()})
		}
		done <- true
	}()
	go func() {
		for i := 0; i < 100; i++ {
			sc.Store("b", &CachedSummary{Summary: "from-b", CreatedAt: time.Now()})
		}
		done <- true
	}()
	<-done
	<-done

	gotA := sc.Get("a")
	gotB := sc.Get("b")
	if gotA == nil || gotB == nil {
		t.Fatal("both sessions should exist")
	}
}

func makeTestMessages(n int) []types.Message {
	msgs := make([]types.Message, n)
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = types.Message{
			Index: i,
			Role:  role,
			Content: []types.ContentBlock{
				{Type: "text", Text: "message content " + string(rune('a'+i%26))},
			},
		}
	}
	return msgs
}

func makeToolHeavyMessages(n int, toolOutput string) []types.Message {
	msgs := make([]types.Message, n)
	for i := range msgs {
		msgs[i] = types.Message{
			Index: i,
			Role:  "tool",
			Content: []types.ContentBlock{
				{Type: "tool_result", Text: toolOutput},
			},
		}
	}
	return msgs
}

func testCompressionConfig() *config.CompressionConfig {
	return &config.CompressionConfig{
		SlidingWindow:             5,
		MinMessagesForCompression: 3,
		MinTokensForLayer2:        0,
	}
}

func TestLayer2_ShouldTriggerCompressionSessionWindow(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 2
	cfg.SlidingWindow = 1
	cfg.MinTokensForLayer2 = 1
	l := NewLayer2(&cfg)
	long := makeTestMessages(10)
	if !l.ShouldTriggerCompressionSessionWindow("s1", long, 1) {
		t.Fatal("should trigger with window=1")
	}
	if l.ShouldTriggerCompressionSessionWindow("s1", makeTestMessages(2), 1) {
		t.Fatal("should not trigger with too few messages")
	}
}

func TestLayer2_ShouldTriggerCompressionWindow(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 2
	cfg.SlidingWindow = 1
	cfg.MinTokensForLayer2 = 1
	l := NewLayer2(&cfg)
	long := makeTestMessages(10)
	if !l.ShouldTriggerCompressionWindow(long, 1) {
		t.Fatal("should trigger with window=1")
	}
	if l.ShouldTriggerCompressionWindow(makeTestMessages(2), 1) {
		t.Fatal("should not trigger with too few messages")
	}
}

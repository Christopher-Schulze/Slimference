package summarization

import (
	"context"
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
	cfg.MiniMax.APIKeyEnv = "TEST_KEY"
	cfg.MiniMax.BaseURL = "http://127.0.0.1:1"
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
	cfg.MiniMax.APIKeyEnv = "TEST_KEY"
	cfg.MiniMax.BaseURL = "http://127.0.0.1:1"
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
	cfg.MiniMax.APIKeyEnv = "TEST_KEY"
	cfg.MiniMax.BaseURL = "http://127.0.0.1:1"
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
	cfg.MiniMax.APIKeyEnv = "TEST_KEY"
	cfg.MiniMax.BaseURL = "http://127.0.0.1:1"
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
	cfg.MiniMax.APIKeyEnv = "TEST_KEY"
	cfg.MiniMax.BaseURL = "http://127.0.0.1:1"
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
	cfg.MiniMax.APIKeyEnv = "TEST_KEY"
	cfg.MiniMax.BaseURL = "http://127.0.0.1:1"
	t.Setenv("TEST_KEY", "test")
	cfg.MinTokensForLayer2 = 999999
	l := NewLayer2(cfg)
	msgs := makeTestMessages(20)
	if l.ShouldTriggerCompressionSession("s1", msgs) {
		t.Fatal("min tokens gate should block")
	}
}

func TestLayer2_ShouldTriggerCompressionSession_NoProvider(t *testing.T) {
	cfg := testCompressionConfig()
	cfg.MiniMax.BaseURL = ""
	cfg.MiniMax.APIKeyEnv = ""
	l := NewLayer2(cfg)
	msgs := makeTestMessages(20)
	if l.ShouldTriggerCompressionSession("s1", msgs) {
		t.Fatal("no provider should not trigger")
	}
}

func TestLayer2_ShouldTriggerCompressionSession_BoundaryZero(t *testing.T) {
	cfg := testCompressionConfig()
	cfg.MiniMax.APIKeyEnv = "TEST_KEY"
	cfg.MiniMax.BaseURL = "http://127.0.0.1:1"
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
	cfg.MiniMax.APIKeyEnv = "TEST_KEY"
	cfg.MiniMax.BaseURL = "http://127.0.0.1:1"
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

func testCompressionConfig() *config.CompressionConfig {
	return &config.CompressionConfig{
		SlidingWindow:             5,
		MinMessagesForCompression: 3,
		MinTokensForLayer2:        0,
		MiniMax: config.MiniMaxConfig{
			BaseURL:   "http://127.0.0.1:1",
			APIKeyEnv: "NONEXISTENT_KEY_FOR_TEST",
		},
	}
}

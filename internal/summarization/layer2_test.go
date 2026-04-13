package summarization

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tokenproxy/tokenproxy/internal/config"
	"github.com/tokenproxy/tokenproxy/internal/types"
)

func TestLayer2_FormatMessagesForSummarization(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	l := NewLayer2(&cfg)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "hi"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{
			{Type: "tool_use", ToolName: "bash", ToolInput: "{}"},
			{Type: "tool_result", ToolResultID: "1", Text: "out"},
		}},
	}
	out := l.FormatMessagesForSummarization(msgs)
	if out == "" || len(out) < 20 {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestLayer2_ApplyToMessages(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	l := NewLayer2(&cfg)
	base := []types.Message{
		msg(t, 0, "user", "a"),
		msg(t, 1, "assistant", "b"),
		msg(t, 2, "user", "c"),
		msg(t, 3, "assistant", "d"),
		msg(t, 4, "user", "tail"),
	}
	l.cache.Store(&CachedSummary{
		Summary:          "sum",
		CoveredRange:     [2]int{0, 2},
		OriginalTokens:   400,
		CompressedTokens: 40,
		CreatedAt:        time.Now(),
	})
	out, saved, ok := l.ApplyToMessages(base)
	if !ok || saved != 360 {
		t.Fatalf("ok=%v saved=%d", ok, saved)
	}
	if len(out) != 3 {
		t.Fatalf("want 1 synthetic + 2 tail, got %d", len(out))
	}
	if out[0].Role != "assistant" || out[1].Index != 1 {
		t.Fatalf("reindex: %#v", out)
	}
}

func TestLayer2_ApplyToMessages_noCache(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	l := NewLayer2(&cfg)
	msgs := []types.Message{msg(t, 0, "user", "x")}
	out, saved, ok := l.ApplyToMessages(msgs)
	if ok || saved != 0 || len(out) != 1 {
		t.Fatalf("ok=%v saved=%d len=%d", ok, saved, len(out))
	}
}

func TestLayer2_ShouldTriggerCompression(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 2
	cfg.SlidingWindow = 1
	l := NewLayer2(&cfg)
	short := []types.Message{msg(t, 0, "user", "a")}
	if l.ShouldTriggerCompression(short) {
		t.Fatal("short conversation")
	}
	long := make([]types.Message, 10)
	for i := range long {
		long[i] = msg(t, i, "user", "x")
	}
	if !l.ShouldTriggerCompression(long) {
		t.Fatal("expected trigger with empty cache")
	}
	l.cache.Compressing.Store(true)
	if l.ShouldTriggerCompression(long) {
		t.Fatal("should not trigger while compressing")
	}
	l.cache.Compressing.Store(false)
	// boundaryIdx=8 for this slice; covering 0–7 => 7/8 >= 0.70 → no trigger while fresh
	l.cache.Store(&CachedSummary{
		Summary:          "filled",
		CoveredRange:     [2]int{0, 7},
		CreatedAt:        time.Now(),
		OriginalTokens:   1000,
		CompressedTokens: 100,
	})
	if l.ShouldTriggerCompression(long) {
		t.Fatal("high coverage + fresh cache should not trigger")
	}
}

func TestLayer2_GetCache(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	l := NewLayer2(&cfg)
	c1 := l.GetCache()
	c2 := l.GetCache()
	if c1 == nil || c2 != c1 {
		t.Fatalf("GetCache: %p %p", c1, c2)
	}
}

func TestHashMessages(t *testing.T) {
	t.Parallel()
	a := hashMessages(nil)
	b := hashMessages([]types.Message{msg(t, 0, "user", "x")})
	if a == b {
		t.Fatal("expected different hashes for nil vs non-nil slice")
	}
	h2 := hashMessages([]types.Message{msg(t, 0, "user", "x")})
	if b != h2 {
		t.Fatal("hash not stable for same messages")
	}
}

func TestEstimateTokens(t *testing.T) {
	t.Parallel()
	if estimateTokens("") != 0 {
		t.Fatalf("empty: %d", estimateTokens(""))
	}
	if estimateTokens("abc") != 1 {
		t.Fatalf("short non-empty: %d", estimateTokens("abc"))
	}
	if n := estimateTokens(strings.Repeat("x", 8)); n != 2 {
		t.Fatalf("8 bytes: %d", n)
	}
}

// TestLayer2_ApplyToMessages_endZero covers end <= 0 early return (lines 49-51).
func TestLayer2_ApplyToMessages_endZero(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	l := NewLayer2(&cfg)
	msgs := []types.Message{msg(t, 0, "user", "x")}
	// Store a summary with CoveredRange end = 0 -> end <= 0 triggers early return.
	l.cache.Store(&CachedSummary{
		Summary:      "s",
		CoveredRange: [2]int{0, 0},
		CreatedAt:    time.Now(),
	})
	out, saved, ok := l.ApplyToMessages(msgs)
	if ok || saved != 0 || len(out) != 1 {
		t.Fatalf("end=0: ok=%v saved=%d len=%d", ok, saved, len(out))
	}
}

// TestLayer2_ApplyToMessages_endBeyondSlice covers end >= len(messages) (lines 49-51).
func TestLayer2_ApplyToMessages_endBeyondSlice(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	l := NewLayer2(&cfg)
	msgs := []types.Message{msg(t, 0, "user", "x"), msg(t, 1, "assistant", "y")}
	// end=2 >= len=2 -> early return
	l.cache.Store(&CachedSummary{
		Summary:      "s",
		CoveredRange: [2]int{0, 2},
		CreatedAt:    time.Now(),
	})
	out, saved, ok := l.ApplyToMessages(msgs)
	if ok || saved != 0 || len(out) != 2 {
		t.Fatalf("end>=len: ok=%v saved=%d len=%d", ok, saved, len(out))
	}
}

// TestLayer2_RunCompressionJob_tooFewMessages covers prefixEnd < minMsgs (lines 89-91).
func TestLayer2_RunCompressionJob_tooFewMessages(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 20
	cfg.SlidingWindow = 5
	l := NewLayer2(&cfg)
	// Only 3 messages - compression should not trigger.
	msgs := []types.Message{
		msg(t, 0, "user", "a"),
		msg(t, 1, "assistant", "b"),
		msg(t, 2, "user", "c"),
	}
	l.RunCompressionJob(msgs)
	if cur, _ := l.cache.GetCurrent(); cur != nil {
		t.Fatal("expected no cache after too-few-messages skip")
	}
}

// TestLayer2_RunCompressionJob_allAnchors covers toSummarize==0 after anchor filter (lines 100-102).
func TestLayer2_RunCompressionJob_allAnchors(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 2
	cfg.SlidingWindow = 1
	l := NewLayer2(&cfg)
	// All messages are anchors (edit tool_use messages).
	msgs := make([]types.Message, 5)
	for i := range msgs {
		msgs[i] = toolUseMsg(t, i, "edit_file")
	}
	l.RunCompressionJob(msgs)
	if cur, _ := l.cache.GetCurrent(); cur != nil {
		t.Fatal("expected no cache when all messages are anchors")
	}
}

// TestLayer2_RunCompressionJob_incrementalExtension covers the incremental path
// (lines 109-121) when coveredFraction >= 0.70 and newStart <= boundaryIdx.
func TestLayer2_RunCompressionJob_incrementalExtension(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	summaryText := strings.Repeat("S", 400)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q}}]}`, summaryText)
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 0
	cfg.SlidingWindow = 2
	cfg.MinMessagesForCompression = 4

	l := NewLayer2(&cfg)
	msgs := make([]types.Message, 20)
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = msg(t, i, role, strings.Repeat("word ", 25))
	}

	// Store a prior summary covering 0-14 out of boundaryIdx=17 -> fraction 14/17 >= 0.70.
	l.cache.Store(&CachedSummary{
		Summary:          "prior summary",
		CoveredRange:     [2]int{0, 14},
		OriginalTokens:   1000,
		CompressedTokens: 200,
		CreatedAt:        time.Now(),
	})

	l.RunCompressionJob(msgs)
	// Should have stored a new summary (incremental extension path).
	cur, _ := l.cache.GetCurrent()
	if cur == nil {
		t.Fatal("expected cached summary after incremental extension")
	}
}

// TestLayer2_RunCompressionJob_incrementalAlreadyCovered covers the "already fully covered"
// early return (lines 117-120) when newStart > boundaryIdx.
func TestLayer2_RunCompressionJob_incrementalAlreadyCovered(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.SlidingWindow = 2
	cfg.MinMessagesForCompression = 4

	l := NewLayer2(&cfg)
	msgs := make([]types.Message, 20)
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = msg(t, i, role, strings.Repeat("word ", 10))
	}

	// boundaryIdx = prefixEnd - 1. SlidingWindow=2 -> prefixEnd=18 -> boundaryIdx=17.
	// Store existing covering 0-17, so coveredFraction=17/17=1.0 >= 0.70.
	// newStart = 17+1 = 18 > 17 = boundaryIdx -> already covered.
	l.cache.Store(&CachedSummary{
		Summary:          "full coverage",
		CoveredRange:     [2]int{0, 17},
		OriginalTokens:   500,
		CompressedTokens: 100,
		CreatedAt:        time.Now(),
	})

	beforeSummary := "full coverage"
	l.RunCompressionJob(msgs)

	cur, _ := l.cache.GetCurrent()
	if cur == nil || cur.Summary != beforeSummary {
		t.Fatalf("expected unchanged cache on already-covered, got %#v", cur)
	}
}

// TestLayer2_RunCompressionJob_deltaAllAnchors covers the second toSummarize==0 (lines 125-127)
// when the incremental delta messages happen to be filtered out as anchors.
//
// How anchor index matching works in filterNonAnchored: it uses 0-based indices within the
// slice passed to it. allAnchorIndices is computed over messages[:boundaryIdx+1], so anchors
// at positions 0,1,2 appear as indices 0,1,2. When filterNonAnchored is called with the
// delta sub-slice messages[newStart:boundaryIdx+1], its positions are also 0-based, so
// anchor indices 0,1,2 filter the first three delta messages.
//
// Setup: msgs[0..2]=edit anchors (allAnchorIndices=[0,1,2]),
//        msgs[3..19]=regular alternating user/assistant.
// With SlidingWindow=2: userIdx=[4,6,8,10,12,14,16,18], prefixEnd=16, boundaryIdx=15.
// Cache covers 0-12: coveredFraction=12/15=0.80>=0.70, newStart=13.
// Delta = messages[13:16] (3 elements at sub-slice indices 0,1,2).
// filterNonAnchored(messages[13:16], [0,1,2]) -> anchored={0,1,2} -> all filtered -> empty.
func TestLayer2_RunCompressionJob_deltaAllAnchors(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.SlidingWindow = 2
	cfg.MinMessagesForCompression = 4

	l := NewLayer2(&cfg)

	// msgs[0..2] are edit anchors; msgs[3..19] are regular alternating user/assistant.
	msgs := make([]types.Message, 20)
	for i := range msgs {
		if i < 3 {
			msgs[i] = toolUseMsg(t, i, "edit_file")
		} else {
			role := "user"
			if i%2 == 1 {
				role = "assistant"
			}
			msgs[i] = msg(t, i, role, strings.Repeat("word ", 10))
		}
	}

	// Cache covers 0-12 (fraction=12/15=0.80>=0.70); newStart=13, boundaryIdx=15.
	l.cache.Store(&CachedSummary{
		Summary:          "prior",
		CoveredRange:     [2]int{0, 12},
		OriginalTokens:   400,
		CompressedTokens: 80,
		CreatedAt:        time.Now(),
	})

	l.RunCompressionJob(msgs)
	// Second toSummarize==0 fires; cache must remain "prior".
	cur, _ := l.cache.GetCurrent()
	if cur == nil || cur.Summary != "prior" {
		t.Fatalf("expected unchanged 'prior' cache, got %#v", cur)
	}
}

// TestLayer2_RunCompressionJob_validationFails covers lines 147-153 (validator rejects summary).
func TestLayer2_RunCompressionJob_validationFails(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	// Return a single-character summary that will fail validation (too short).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"x"}}]}`))
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 0
	cfg.SlidingWindow = 5
	cfg.MinMessagesForCompression = 8

	l := NewLayer2(&cfg)
	msgs := make([]types.Message, 20)
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = msg(t, i, role, strings.Repeat("word ", 100))
	}

	l.RunCompressionJob(msgs)
	if cur, _ := l.cache.GetCurrent(); cur != nil {
		t.Fatalf("expected no cache when validation fails, got %#v", cur)
	}
}

// TestLayer2_ShouldTriggerCompression_stale covers the IsStale=true path (lines 200-202).
func TestLayer2_ShouldTriggerCompression_stale(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 2
	cfg.SlidingWindow = 1
	l := NewLayer2(&cfg)

	long := make([]types.Message, 10)
	for i := range long {
		long[i] = msg(t, i, "user", "x")
	}

	// Store a stale summary (created long ago).
	l.cache.Store(&CachedSummary{
		Summary:      "stale",
		CoveredRange: [2]int{0, 7},
		CreatedAt:    time.Now().Add(-2 * time.Hour),
	})

	if !l.ShouldTriggerCompression(long) {
		t.Fatal("stale cache should trigger compression")
	}
}

// TestLayer2_ShouldTriggerCompression_boundaryIdxZero covers boundaryIdx <= 0 (lines 206-208).
// When existing != nil and fresh, but boundaryIdx <= 0 -> return true.
// Setup: two consecutive user messages [user(0), user(1)] + window=1 -> prefixEnd=userIdx[1]=1 -> boundaryIdx=0.
func TestLayer2_ShouldTriggerCompression_boundaryIdxZero(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 1
	cfg.SlidingWindow = 1
	l := NewLayer2(&cfg)

	// Two user messages back-to-back + window=1:
	// userIdx=[0,1], len=2>1, prefixEnd=userIdx[1]=1, boundaryIdx=0.
	// Store a fresh summary so IsStale returns false and existing != nil.
	l.cache.Store(&CachedSummary{
		Summary:      "fresh",
		CoveredRange: [2]int{0, 0},
		CreatedAt:    time.Now(),
	})

	msgs := []types.Message{
		msg(t, 0, "user", "first user"),
		msg(t, 1, "user", "second user"),
	}
	// boundaryIdx=0 <= 0 -> return true.
	if !l.ShouldTriggerCompression(msgs) {
		t.Fatal("boundaryIdx=0 should trigger compression")
	}
}

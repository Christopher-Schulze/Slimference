package summarization

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
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
	cfg.MinTokensForLayer2 = 1
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

// TestLayer2_incrementalOverlapThresholdDefault verifies the scalar fallback
// is used when no staircase is configured and zero means "use historical 0.70".
func TestLayer2_incrementalOverlapThresholdDefault(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.Tuning.IncrementalOverlapThreshold = 0
	cfg.Tuning.IncrementalStaircase = nil
	l := NewLayer2(&cfg)
	if got := l.incrementalOverlapThreshold(10); got != 0.70 {
		t.Fatalf("expected fallback 0.70 when tuning is 0 and staircase empty, got %v", got)
	}

	cfg.Tuning.IncrementalOverlapThreshold = 0.55
	l = NewLayer2(&cfg)
	if got := l.incrementalOverlapThreshold(10); got != 0.55 {
		t.Fatalf("expected configured 0.55, got %v", got)
	}
}

// TestLayer2_incrementalOverlapThresholdStaircase verifies the T27 staircase:
// the first step whose msg_count_le is >= the current conversation length
// wins, and small/medium/large conversations pick different thresholds.
func TestLayer2_incrementalOverlapThresholdStaircase(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.Tuning.IncrementalOverlapThreshold = 0.99 // fallback must not be reached
	cfg.Tuning.IncrementalStaircase = []config.StaircaseStep{
		{MsgCountLE: 60, Threshold: 0.70},
		{MsgCountLE: 120, Threshold: 0.55},
		{MsgCountLE: 1_000_000, Threshold: 0.40},
	}
	l := NewLayer2(&cfg)

	cases := []struct {
		msgCount int
		want     float64
	}{
		{msgCount: 10, want: 0.70},
		{msgCount: 60, want: 0.70},
		{msgCount: 90, want: 0.55},
		{msgCount: 120, want: 0.55},
		{msgCount: 500, want: 0.40},
		{msgCount: 2_000_000, want: 0.99}, // beyond last step -> scalar fallback
	}
	for _, tc := range cases {
		if got := l.incrementalOverlapThreshold(tc.msgCount); got != tc.want {
			t.Errorf("msgCount=%d: got %v, want %v", tc.msgCount, got, tc.want)
		}
	}
}

// TestLayer2_incrementalOverlapThresholdStaircaseZeroStep verifies the
// defensive fallback when a configured step has threshold 0.
func TestLayer2_incrementalOverlapThresholdStaircaseZeroStep(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.Tuning.IncrementalStaircase = []config.StaircaseStep{
		{MsgCountLE: 60, Threshold: 0},
	}
	l := NewLayer2(&cfg)
	if got := l.incrementalOverlapThreshold(10); got != 0.70 {
		t.Fatalf("zero step must fall back to 0.70, got %v", got)
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
		t.Fatalf("single word: %d", estimateTokens("abc"))
	}
	if n := estimateTokens("hello world foo"); n != 3 {
		t.Fatalf("three words: %d", n)
	}
	if n := estimateTokens("  spaces   between   words  "); n != 3 {
		t.Fatalf("spaced words: %d", n)
	}
	cjk := string(rune(0x4E16)) + string(rune(0x754C))
	if n := estimateTokens(cjk); n != 2 {
		t.Fatalf("two CJK chars = 2 tokens: %d", n)
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
	cfg.MinTokensForLayer2 = 1
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
	cfg.MinTokensForLayer2 = 1
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
	summaryText := "- " + strings.Repeat("S", 397)
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
	cfg.MinTokensForLayer2 = 1

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
	cfg.MinTokensForLayer2 = 1

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
//
//	msgs[3..19]=regular alternating user/assistant.
//
// With SlidingWindow=2: userIdx=[4,6,8,10,12,14,16,18], prefixEnd=16, boundaryIdx=15.
// Cache covers 0-12: coveredFraction=12/15=0.80>=0.70, newStart=13.
// Delta = messages[13:16] (3 elements at sub-slice indices 0,1,2).
// filterNonAnchored(messages[13:16], [0,1,2]) -> anchored={0,1,2} -> all filtered -> empty.
func TestLayer2_RunCompressionJob_deltaAllAnchors(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.SlidingWindow = 2
	cfg.MinMessagesForCompression = 4
	cfg.MinTokensForLayer2 = 1

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
	cfg.MinTokensForLayer2 = 1

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

func TestLayer2_RunCompressionJob_repairBypassesRetry(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	// Return a summary using `* ` bullets (validator rejects the format
	// because bulletCount of `- ` lines = 0). The deterministic repair
	// normalises `* ` to `- ` so the second validate passes; this test
	// asserts the second validate succeeds without an extra API round
	// trip (calls stays at 1).
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		var bullets strings.Builder
		for i := 0; i < 30; i++ {
			bullets.WriteString("* extracted fact about the corpus item number ")
			bullets.WriteString(strings.Repeat("x", 5))
			bullets.WriteString("\n")
		}
		body, err := json.Marshal(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]string{
					"role":    "assistant",
					"content": bullets.String(),
				},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 0
	cfg.SlidingWindow = 5
	cfg.MinMessagesForCompression = 8
	cfg.MinTokensForLayer2 = 1

	l := NewLayer2(&cfg)
	msgs := make([]types.Message, 20)
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		// Plain prose, no paths / functions / errors, so only the format
		// check fires before repair. Long enough that 30 short bullets
		// stay below the 40% length cap.
		msgs[i] = msg(t, i, role, strings.Repeat("word ", 200))
	}

	l.RunCompressionJob(msgs)
	if calls != 1 {
		t.Fatalf("repair should bypass retry; expected 1 upstream call, got %d", calls)
	}
}

// TestLayer2_ShouldTriggerCompression_stale covers the IsStale=true path (lines 200-202).
func TestLayer2_ShouldTriggerCompression_stale(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 2
	cfg.SlidingWindow = 1
	cfg.MinTokensForLayer2 = 1
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
func TestPreprocessInput_truncatesLongLines(t *testing.T) {
	t.Parallel()
	longLine := strings.Repeat("x", 3000)
	input := "short line\n" + longLine + "\nanother short"
	got := preprocessInput(input)
	if strings.Contains(got, longLine) {
		t.Fatal("long line should be truncated")
	}
	if !strings.Contains(got, "short line") || !strings.Contains(got, "another short") {
		t.Fatalf("short lines should be preserved, got: %q", got)
	}
	if !strings.Contains(got, "[truncated") {
		t.Fatalf("truncation marker missing, got: %q", got)
	}
}

func TestPreprocessInput_removesConsecutiveDupes(t *testing.T) {
	t.Parallel()
	input := "line A\nline A\nline A\nline B\nline B"
	got := preprocessInput(input)
	count := strings.Count(got, "line A")
	if count != 1 {
		t.Fatalf("expected 1 occurrence of 'line A' after dedup, got %d: %q", count, got)
	}
	countB := strings.Count(got, "line B")
	if countB != 1 {
		t.Fatalf("expected 1 occurrence of 'line B' after dedup, got %d: %q", countB, got)
	}
}

func TestPreprocessInput_emptyInput(t *testing.T) {
	t.Parallel()
	got := preprocessInput("")
	if got != "" {
		t.Fatalf("empty input should produce empty output, got: %q", got)
	}
}

func TestPreprocessInput_noChanges(t *testing.T) {
	t.Parallel()
	input := "line one\nline two\nline three"
	got := preprocessInput(input)
	if got != input {
		t.Fatalf("clean input should be unchanged, got: %q", got)
	}
}

func TestLayer2_ShouldTriggerCompression_boundaryIdxZero(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 1
	cfg.SlidingWindow = 1
	cfg.MinTokensForLayer2 = 1
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

func TestContentDensity_empty(t *testing.T) {
	t.Parallel()
	d := contentDensity(nil)
	if d != 0.5 {
		t.Fatalf("empty messages should return 0.5, got %f", d)
	}
}

func TestContentDensity_proseOnly(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "Hello how are you today"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "I am fine thank you"}}},
	}
	d := contentDensity(msgs)
	if d > 0.3 {
		t.Fatalf("pure prose should have low density, got %f", d)
	}
}

func TestContentDensity_codeHeavy(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Index: 0, Role: "assistant", Content: []types.ContentBlock{
			{Type: "tool_use", ToolName: "edit_file", ToolInput: `{"path":"src/main.go"}`},
		}},
		{Index: 1, Role: "user", Content: []types.ContentBlock{
			{Type: "tool_result", ToolResultID: "r1", Text: "OK"},
		}},
		{Index: 2, Role: "assistant", Content: []types.ContentBlock{
			{Type: "text", Text: "func handleLogin() error {\n\treturn nil\n}"},
		}},
	}
	d := contentDensity(msgs)
	if d < 0.3 {
		t.Fatalf("code-heavy should have high density, got %f", d)
	}
}

func TestComputeAdaptiveTarget_smallInput(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "hi"}}},
	}
	target := computeAdaptiveTarget(500, msgs, 0.20)
	if target < 100 {
		t.Fatalf("minimum target should be 100, got %d", target)
	}
	if target > 300 {
		t.Fatalf("small input should not get huge target, got %d", target)
	}
}

func TestComputeAdaptiveTarget_largeInput(t *testing.T) {
	t.Parallel()
	msgs := make([]types.Message, 50)
	for i := range msgs {
		msgs[i] = types.Message{Index: i, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("word ", 100)}}}
	}
	target := computeAdaptiveTarget(50000, msgs, 0.20)
	if target < 100 {
		t.Fatalf("target should be at least 100, got %d", target)
	}
	if target > 30000 {
		t.Fatalf("large input should respect ratio cap, got %d", target)
	}
}

func TestLooksLikeCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  bool
	}{
		{"func main() {", true},
		{"var x int", true},
		{"  if err != nil {", true},
		{"Hello, how are you?", false},
		{"", false},
		{"// comment", true},
		{"pub fn handler() -> Result {", true},
		{"normal text with no code", false},
		{"}", true},
	}
	for _, tc := range tests {
		got := looksLikeCode(tc.input)
		if got != tc.want {
			t.Errorf("looksLikeCode(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestLooksLikePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  bool
	}{
		{"src/auth/handler.go", true},
		{"./relative/path", true},
		{"../parent/dir", true},
		{"config.toml", true},
		{"just a normal sentence", false},
		{"", false},
	}
	for _, tc := range tests {
		got := looksLikePath(tc.input)
		if got != tc.want {
			t.Errorf("looksLikePath(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

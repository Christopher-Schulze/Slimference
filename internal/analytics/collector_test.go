package analytics

import (
	"sync"
	"testing"
	"time"

	"github.com/tokenproxy/tokenproxy/internal/types"
)

func TestAnalytics_RecentRequests(t *testing.T) {
	t.Parallel()
	a := NewAnalytics()
	for i := 0; i < 3; i++ {
		a.Record(makeRequestEvent(types.Anthropic, "m", 100, 80, 10, false, float64(10+i), nil))
	}
	last2 := a.RecentRequests(2)
	if len(last2) != 2 {
		t.Fatalf("RecentRequests(2): len=%d", len(last2))
	}
	if last2[1].LatencyMs != 12 {
		t.Fatalf("last event latency: %v", last2[1].LatencyMs)
	}
}

func TestRunCollector_drainsOnInputClose(t *testing.T) {
	t.Parallel()
	ch := make(chan types.AnalyticsEvent, 2)
	a := NewAnalytics()
	done := make(chan struct{})
	ch <- makeRequestEvent(types.Anthropic, "m", 50, 40, 5, false, 1, nil)
	ch <- makeRequestEvent(types.Anthropic, "m", 60, 50, 6, true, 2, nil)
	close(ch)
	RunCollector(ch, a, done)
	if a.TotalRequests != 2 {
		t.Fatalf("TotalRequests=%d", a.TotalRequests)
	}
}

func TestRunCollector_doneDrainsBuffered(t *testing.T) {
	t.Parallel()
	ch := make(chan types.AnalyticsEvent, 4)
	a := NewAnalytics()
	done := make(chan struct{})
	ch <- makeRequestEvent(types.Anthropic, "m", 10, 8, 1, false, 0, nil)
	ch <- makeRequestEvent(types.Anthropic, "m", 20, 18, 2, false, 0, nil)
	close(done)
	RunCollector(ch, a, done)
	if a.TotalRequests != 2 {
		t.Fatalf("TotalRequests=%d", a.TotalRequests)
	}
}

// TestDrainInput_withEvents covers the drainInput helper directly, exercising both
// the case-event branch (events present) and the default-return branch (empty).
func TestDrainInput_withEvents(t *testing.T) {
	t.Parallel()
	ch := make(chan types.AnalyticsEvent, 3)
	a := NewAnalytics()
	ch <- makeRequestEvent(types.Anthropic, "m", 10, 8, 1, false, 0, nil)
	ch <- makeRequestEvent(types.Anthropic, "m", 20, 18, 2, false, 0, nil)
	drainInput(ch, a)
	if a.TotalRequests != 2 {
		t.Fatalf("drainInput: TotalRequests=%d, want 2", a.TotalRequests)
	}
}

// TestDrainInput_empty covers the default-return branch of drainInput when the channel is empty.
func TestDrainInput_empty(t *testing.T) {
	t.Parallel()
	ch := make(chan types.AnalyticsEvent, 2)
	a := NewAnalytics()
	drainInput(ch, a)
	if a.TotalRequests != 0 {
		t.Fatalf("drainInput on empty: TotalRequests=%d, want 0", a.TotalRequests)
	}
}

func TestAnalyticsSnapshot_EstExtraMessagesAndAvgTTFT(t *testing.T) {
	t.Parallel()
	a := NewAnalytics()
	a.Record(makeRequestEvent(types.Anthropic, "m", 400, 100, 50, false, 0, nil))
	snap := a.Snapshot()
	if snap.EstExtraMessages(100) != 3 {
		t.Fatalf("Snapshot.EstExtraMessages: %d", snap.EstExtraMessages(100))
	}
	if snap.EstExtraMessages(0) != 300 {
		t.Fatalf("Snapshot.EstExtraMessages(0): %d", snap.EstExtraMessages(0))
	}
	// 300 saved / 1 req / 1000 prefill = 0.3
	if v := snap.AvgTTFTImprovement(1000); v < 0.29 || v > 0.31 {
		t.Fatalf("Snapshot.AvgTTFTImprovement: %v", v)
	}
	empty := AnalyticsSnapshot{}
	if empty.AvgTTFTImprovement(100) != 0 || empty.EstExtraMessages(10) != 0 {
		t.Fatalf("empty snapshot methods")
	}
}

func TestAnalytics_Record_layerSavingsAndOpenAILatency(t *testing.T) {
	t.Parallel()
	a := NewAnalytics()
	ev := makeRequestEvent(types.Anthropic, "m", 1000, 500, 100, false, 50, []int{1, 2, 3})
	a.Record(ev)
	if a.Layer1Savings == 0 || a.Layer2Savings == 0 || a.Layer3Savings == 0 {
		t.Fatalf("layer savings: L1=%d L2=%d L3=%d", a.Layer1Savings, a.Layer2Savings, a.Layer3Savings)
	}
	a2 := NewAnalytics()
	a2.Record(makeRequestEvent(types.OpenAI, "gpt", 200, 180, 20, false, 33, nil))
	if a2.LatencyOpenAIMs < 32 || a2.LatencyOpenAIMs > 34 {
		t.Fatalf("OpenAI latency avg: %v", a2.LatencyOpenAIMs)
	}
}

func TestAnalytics_Record_nonRequestEvents(t *testing.T) {
	t.Parallel()
	a := NewAnalytics()
	a.Record(types.AnalyticsEvent{Type: types.EventCacheHit, Timestamp: time.Now()})
	if a.CacheHits != 1 {
		t.Fatalf("CacheHits=%d", a.CacheHits)
	}
	a.Record(types.AnalyticsEvent{Type: types.EventCompressionComplete, Timestamp: time.Now(), LatencyMs: 100})
	if a.CompressionCalls != 1 || a.MiniMaxCalls != 1 {
		t.Fatalf("compression: calls=%d minimax=%d", a.CompressionCalls, a.MiniMaxCalls)
	}
	a.Record(types.AnalyticsEvent{Type: types.EventSecretDetected, Timestamp: time.Now(), SecretsFound: 3})
	if a.SecretsRedacted != 3 {
		t.Fatalf("SecretsRedacted=%d", a.SecretsRedacted)
	}
	a.Record(types.AnalyticsEvent{Type: types.EventErrorOccurred, Timestamp: time.Now(), Error: "boom"})
	if a.Errors != 1 {
		t.Fatalf("Errors=%d", a.Errors)
	}
	a.Record(types.AnalyticsEvent{Type: types.EventLayerToggled, Timestamp: time.Now()})
	if a.TotalRequests != 0 {
		t.Fatal("layer toggle should not increment requests")
	}
	a.Record(types.AnalyticsEvent{Type: types.EventType(99), Timestamp: time.Now()})
}

// makeRequestEvent builds an EventRequestProcessed AnalyticsEvent for tests.
func makeRequestEvent(provider types.Provider, model string, inputOrig, inputComp, output int, cacheHit bool, latency float64, layers []int) types.AnalyticsEvent {
	return types.AnalyticsEvent{
		Type:             types.EventRequestProcessed,
		Timestamp:        time.Now(),
		Provider:         provider,
		Model:            model,
		InputTokensOrig:  inputOrig,
		InputTokensComp:  inputComp,
		OutputTokens:     output,
		CompressionRatio: 0,
		Layers:           layers,
		LatencyMs:        latency,
		CacheHit:         cacheHit,
	}
}

// TestAnalytics_RecordRequest verifies that counters increment correctly after one request event.
func TestAnalytics_RecordRequest(t *testing.T) {
	t.Parallel()

	a := NewAnalytics()
	a.Record(makeRequestEvent(types.Anthropic, "claude-3-5-sonnet-20241022", 1000, 800, 200, false, 120.0, []int{1}))

	if a.TotalRequests != 1 {
		t.Errorf("TotalRequests = %d, want 1", a.TotalRequests)
	}
	if a.TotalInputTokens != 1000 {
		t.Errorf("TotalInputTokens = %d, want 1000", a.TotalInputTokens)
	}
	if a.TotalOutputTokens != 200 {
		t.Errorf("TotalOutputTokens = %d, want 200", a.TotalOutputTokens)
	}
	if a.SavedInputTokens != 200 {
		t.Errorf("SavedInputTokens = %d, want 200 (1000-800)", a.SavedInputTokens)
	}
	if a.CacheMisses != 1 {
		t.Errorf("CacheMisses = %d, want 1", a.CacheMisses)
	}
	if a.CacheHits != 0 {
		t.Errorf("CacheHits = %d, want 0", a.CacheHits)
	}
}

// TestAnalytics_CompressionRatio verifies the ratio calculation.
func TestAnalytics_CompressionRatio(t *testing.T) {
	t.Parallel()

	a := NewAnalytics()
	// 1000 original, 750 compressed -> saved 250 -> ratio 0.25
	a.Record(makeRequestEvent(types.Anthropic, "m", 1000, 750, 100, false, 0, nil))

	ratio := a.CompressionRatio()
	want := 0.25
	if ratio < want-0.001 || ratio > want+0.001 {
		t.Errorf("CompressionRatio() = %f, want %f", ratio, want)
	}
}

// TestAnalytics_CompressionRatio_ZeroRequests verifies 0 ratio with no requests.
func TestAnalytics_CompressionRatio_ZeroRequests(t *testing.T) {
	t.Parallel()

	a := NewAnalytics()
	if r := a.CompressionRatio(); r != 0 {
		t.Errorf("CompressionRatio() = %f on empty analytics, want 0", r)
	}
}

// TestAnalytics_PerProviderStats verifies per-provider breakdown is tracked correctly.
func TestAnalytics_PerProviderStats(t *testing.T) {
	t.Parallel()

	a := NewAnalytics()
	a.Record(makeRequestEvent(types.Anthropic, "m", 1000, 800, 100, false, 0, nil))
	a.Record(makeRequestEvent(types.Anthropic, "m", 500, 400, 50, false, 0, nil))
	a.Record(makeRequestEvent(types.OpenAI, "gpt-4o", 2000, 1800, 200, false, 0, nil))

	snap := a.Snapshot()
	anthropicStats, ok := snap.PerProvider[types.Anthropic]
	if !ok {
		t.Fatal("no per-provider stats for Anthropic")
	}
	if anthropicStats.Messages != 2 {
		t.Errorf("Anthropic Messages = %d, want 2", anthropicStats.Messages)
	}
	if anthropicStats.InputTokensOrig != 1500 {
		t.Errorf("Anthropic InputTokensOrig = %d, want 1500", anthropicStats.InputTokensOrig)
	}

	openAIStats, ok := snap.PerProvider[types.OpenAI]
	if !ok {
		t.Fatal("no per-provider stats for OpenAI")
	}
	if openAIStats.Messages != 1 {
		t.Errorf("OpenAI Messages = %d, want 1", openAIStats.Messages)
	}
}

// TestAnalytics_Snapshot verifies that Snapshot returns a consistent value copy.
func TestAnalytics_Snapshot(t *testing.T) {
	t.Parallel()

	a := NewAnalytics()
	a.Record(makeRequestEvent(types.Anthropic, "m", 500, 400, 100, true, 50.0, nil))

	snap := a.Snapshot()
	if snap.TotalRequests != 1 {
		t.Errorf("snap.TotalRequests = %d, want 1", snap.TotalRequests)
	}
	if snap.CacheHits != 1 {
		t.Errorf("snap.CacheHits = %d, want 1", snap.CacheHits)
	}
	if snap.TotalInputTokens != 500 {
		t.Errorf("snap.TotalInputTokens = %d, want 500", snap.TotalInputTokens)
	}
}

// TestAnalytics_EstExtraMessages verifies the estimation formula.
func TestAnalytics_EstExtraMessages(t *testing.T) {
	t.Parallel()

	a := NewAnalytics()
	// saved = 300 tokens
	a.Record(makeRequestEvent(types.Anthropic, "m", 1000, 700, 100, false, 0, nil))

	// With avg 100 tokens per request: 300 / 100 = 3
	got := a.EstExtraMessages(100)
	if got != 3 {
		t.Errorf("EstExtraMessages(100) = %d, want 3", got)
	}

	// Edge case: avgTokens <= 0 treated as 1 -> 300/1 = 300
	got = a.EstExtraMessages(0)
	if got != 300 {
		t.Errorf("EstExtraMessages(0) = %d, want 300 (denominator clamped to 1)", got)
	}
}

// TestAnalytics_AvgTTFTImprovement verifies the TTFT calculation.
func TestAnalytics_AvgTTFTImprovement(t *testing.T) {
	t.Parallel()

	a := NewAnalytics()
	// 200 tokens saved in 1 request
	a.Record(makeRequestEvent(types.Anthropic, "m", 1000, 800, 100, false, 0, nil))

	// avgSaved = 200/1 = 200 tokens; prefill 50000 t/s -> 200/50000 = 0.004s
	got := a.AvgTTFTImprovement(50000)
	want := 0.004
	if got < want-0.0001 || got > want+0.0001 {
		t.Errorf("AvgTTFTImprovement(50000) = %f, want %f", got, want)
	}

	// Zero prefill speed should return 0.
	if r := a.AvgTTFTImprovement(0); r != 0 {
		t.Errorf("AvgTTFTImprovement(0) = %f, want 0", r)
	}
}

// TestAnalytics_ConcurrentRecord verifies no data race when recording from many goroutines.
func TestAnalytics_ConcurrentRecord(t *testing.T) {
	t.Parallel()

	a := NewAnalytics()
	const goroutines = 50
	const eventsEach = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < eventsEach; j++ {
				a.Record(makeRequestEvent(types.Anthropic, "m", 100, 80, 20, false, 10.0, nil))
			}
		}()
	}
	wg.Wait()

	total := goroutines * eventsEach
	if a.TotalRequests != total {
		t.Errorf("TotalRequests = %d, want %d after concurrent records", a.TotalRequests, total)
	}
}

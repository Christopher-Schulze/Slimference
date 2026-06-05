package proxy

import (
	"sync"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

func TestAnalyticsWorker_requestProcessedFansOutToTUI(t *testing.T) {
	p := New(config.Defaults())
	ts := time.Now()
	want := types.RequestMetrics{
		Timestamp:        ts,
		Provider:         types.Anthropic,
		Model:            "claude-test",
		InputTokensOrig:  100,
		InputTokensComp:  40,
		OutputTokens:     20,
		CompressionRatio: 0.4,
		Layers:           []int{1, 2},
		LatencyMs:        12.5,
		CacheHit:         false,
	}
	var got types.RequestMetrics
	var wg sync.WaitGroup
	wg.Add(1)
	p.SetTUISendFn(func(rm types.RequestMetrics) {
		got = rm
		wg.Done()
	})

	p.wg.Add(1)
	go p.analyticsWorker()

	p.analyticsQueue <- types.AnalyticsEvent{
		Type:             types.EventRequestProcessed,
		Timestamp:        want.Timestamp,
		Provider:         want.Provider,
		Model:            want.Model,
		InputTokensOrig:  want.InputTokensOrig,
		InputTokensComp:  want.InputTokensComp,
		OutputTokens:     want.OutputTokens,
		CompressionRatio: want.CompressionRatio,
		Layers:           want.Layers,
		LatencyMs:        want.LatencyMs,
		CacheHit:         want.CacheHit,
	}

	wait := make(chan struct{})
	go func() {
		wg.Wait()
		close(wait)
	}()
	select {
	case <-wait:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for TUI callback")
	}

	if got.Provider != want.Provider || got.Model != want.Model {
		t.Fatalf("metrics mismatch: %+v vs want %+v", got, want)
	}
	if got.InputTokensOrig != want.InputTokensOrig || got.OutputTokens != want.OutputTokens {
		t.Fatalf("token fields: %+v", got)
	}

	close(p.shutdownCh)
	p.wg.Wait()
}

func TestAnalyticsWorker_nonRequestProcessedSkipsTUI(t *testing.T) {
	p := New(config.Defaults())
	var tuiCalls int
	p.SetTUISendFn(func(types.RequestMetrics) { tuiCalls++ })

	p.wg.Add(1)
	go p.analyticsWorker()

	p.analyticsQueue <- types.AnalyticsEvent{
		Type:      types.EventSecretDetected,
		Timestamp: time.Now(),
	}
	// Let worker process (no TUI for this event type).
	time.Sleep(50 * time.Millisecond)
	if tuiCalls != 0 {
		t.Fatalf("tui calls=%d want 0", tuiCalls)
	}

	close(p.shutdownCh)
	p.wg.Wait()
}

func TestAnalyticsWorker_drainsQueuedEventsOnShutdown(t *testing.T) {
	p := New(config.Defaults())
	ts := time.Now()

	p.analyticsQueue <- types.AnalyticsEvent{
		Type:             types.EventRequestProcessed,
		Timestamp:        ts,
		Provider:         types.OpenAI,
		Model:            "gpt-test",
		InputTokensOrig:  200,
		InputTokensComp:  100,
		OutputTokens:     30,
		CompressionRatio: 0.5,
		Layers:           []int{1, 3},
		LatencyMs:        4.5,
		CacheHit:         true,
	}

	close(p.shutdownCh)
	p.wg.Add(1)
	go p.analyticsWorker()
	p.wg.Wait()

	snap := p.analytics.Snapshot()
	if snap.TotalRequests != 1 {
		t.Fatalf("TotalRequests = %d, want 1", snap.TotalRequests)
	}
	if snap.CacheHits != 1 {
		t.Fatalf("CacheHits = %d, want 1", snap.CacheHits)
	}
	if n := len(p.analytics.RecentRequests(10)); n != 1 {
		t.Fatalf("recent requests = %d, want 1", n)
	}
}

func TestDrainAnalyticsQueue_processesQueuedEvents(t *testing.T) {
	p := New(config.Defaults())
	ts := time.Now()

	p.analyticsQueue <- types.AnalyticsEvent{
		Type:             types.EventRequestProcessed,
		Timestamp:        ts,
		Provider:         types.Anthropic,
		Model:            "claude-test",
		InputTokensOrig:  120,
		InputTokensComp:  60,
		OutputTokens:     20,
		CompressionRatio: 0.5,
		Layers:           []int{1, 2},
		LatencyMs:        3.2,
	}

	p.drainAnalyticsQueue()

	snap := p.analytics.Snapshot()
	if snap.TotalRequests != 1 {
		t.Fatalf("TotalRequests = %d, want 1", snap.TotalRequests)
	}
	if n := len(p.analytics.RecentRequests(10)); n != 1 {
		t.Fatalf("recent requests = %d, want 1", n)
	}
}

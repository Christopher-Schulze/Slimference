package proxy

import (
	"sync"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

func TestCompressionWorker_drainsJobThenShutdown(t *testing.T) {
	p := New(config.Defaults())
	p.wg.Add(1)
	go p.compressionWorker()

	p.compressQueue <- types.CompressJob{
		Messages: []types.Message{
			{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "x"}}},
		},
		Timestamp: time.Now(),
	}

	close(p.shutdownCh)
	p.wg.Wait()
}

// TestCompressionWorker_jobBranchCovered reliably exercises the case job := <-p.compressQueue
// branch (lines 419-420) by waiting for the analyticsQueue event that runCompressionJob emits.
func TestCompressionWorker_jobBranchCovered(t *testing.T) {
	p := New(config.Defaults())
	p.wg.Add(1)
	go p.compressionWorker()

	// Drain analyticsQueue in background so it never blocks.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-p.analyticsQueue:
				// runCompressionJob sends EventCompressionComplete - receiving it proves the job ran.
				close(p.shutdownCh)
				return
			case <-time.After(2 * time.Second):
				return
			}
		}
	}()

	p.compressQueue <- types.CompressJob{
		Messages: []types.Message{
			{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "hello world"}}},
		},
		Timestamp: time.Now(),
	}

	<-done
	p.wg.Wait()
}

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
		Type:      types.EventCompressionComplete,
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

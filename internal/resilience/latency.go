package resilience

import (
	"sync"
	"time"
)

// LatencySample holds one observation of time-to-first-token and total latency
// for a single request to a provider.
type LatencySample struct {
	Provider string
	TTFT     time.Duration
	Total    time.Duration
}

// providerLatency stores the exponential moving averages for one provider.
type providerLatency struct {
	ttft  float64 // EMA in nanoseconds
	total float64 // EMA in nanoseconds
	count int64
}

// LatencyTracker maintains per-provider exponential moving averages for TTFT and
// total latency. The decay factor alpha = 0.1 weights the last ~10 observations.
type LatencyTracker struct {
	mu      sync.Mutex
	entries map[string]*providerLatency
}

// alpha is the EMA smoothing factor. At 0.1, older observations decay by (1-0.1)^n,
// meaning ~10 observations cover roughly 95% of the weight.
const alpha = 0.1

// NewLatencyTracker returns an empty LatencyTracker ready for use.
func NewLatencyTracker() *LatencyTracker {
	return &LatencyTracker{
		entries: make(map[string]*providerLatency),
	}
}

// Record updates the exponential moving averages for the sample's provider.
// Thread-safe.
func (t *LatencyTracker) Record(sample LatencySample) {
	t.mu.Lock()
	defer t.mu.Unlock()

	e, ok := t.entries[sample.Provider]
	if !ok {
		// First observation: seed the EMA with the exact value.
		e = &providerLatency{
			ttft:  float64(sample.TTFT),
			total: float64(sample.Total),
			count: 1,
		}
		t.entries[sample.Provider] = e
		return
	}

	e.ttft = alpha*float64(sample.TTFT) + (1-alpha)*e.ttft
	e.total = alpha*float64(sample.Total) + (1-alpha)*e.total
	e.count++
}

// GetAvg returns the current EMA values for provider as time.Duration.
// Returns (0, 0) if no samples have been recorded for the provider.
func (t *LatencyTracker) GetAvg(provider string) (ttft, total time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	e, ok := t.entries[provider]
	if !ok {
		return 0, 0
	}
	return time.Duration(e.ttft), time.Duration(e.total)
}

// ProxyOverhead returns the estimated proxy processing time for provider,
// calculated as (avg total latency) - (avg TTFT).
// This approximates the time the proxy itself adds before the upstream starts responding.
// Returns 0 if no samples exist or if the result would be negative.
func (t *LatencyTracker) ProxyOverhead(provider string) time.Duration {
	ttft, total := t.GetAvg(provider)
	overhead := total - ttft
	if overhead < 0 {
		return 0
	}
	return overhead
}

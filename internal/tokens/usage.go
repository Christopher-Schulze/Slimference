package tokens

import (
	"sync"
	"time"

	"github.com/tokenproxy/tokenproxy/internal/types"
)

// ProviderUsage tracks per-provider compression statistics.
type ProviderUsage struct {
	Messages   int
	TokensSaved int
	AvgRatio   float64
}

// UsageTracker aggregates token usage and compression statistics across requests.
// All methods are goroutine-safe.
type UsageTracker struct {
	mu sync.Mutex

	SessionStart    time.Time
	MessagesSent    int
	InputTokensOrig int
	InputTokensComp int
	OutputTokens    int
	PerProvider     map[types.Provider]*ProviderUsage

	// avgTokensPerReq is the running average of compressed input tokens per request.
	avgTokensPerReq float64
	// prefillSpeed is the provider's prefill throughput in tokens/second (from config).
	prefillSpeed int
}

// UsageSnapshot is a point-in-time copy of UsageTracker state (no mutex needed).
type UsageSnapshot struct {
	SessionStart    time.Time
	MessagesSent    int
	InputTokensOrig int
	InputTokensComp int
	OutputTokens    int
	PerProvider     map[types.Provider]*ProviderUsage
	AvgTokensPerReq float64
	PrefillSpeed    int
}

// NewUsageTracker returns a UsageTracker with the given prefill speed in tokens/second.
func NewUsageTracker(prefillSpeed int) *UsageTracker {
	return &UsageTracker{
		SessionStart: time.Now(),
		PerProvider:  make(map[types.Provider]*ProviderUsage),
		prefillSpeed: prefillSpeed,
	}
}

// Record registers one completed request's token counts. Thread-safe.
func (t *UsageTracker) Record(provider types.Provider, orig, comp, output int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.MessagesSent++
	t.InputTokensOrig += orig
	t.InputTokensComp += comp
	t.OutputTokens += output

	// Update running average of compressed tokens per request using Welford's method.
	delta := float64(comp) - t.avgTokensPerReq
	t.avgTokensPerReq += delta / float64(t.MessagesSent)

	pu, ok := t.PerProvider[provider]
	if !ok {
		pu = &ProviderUsage{}
		t.PerProvider[provider] = pu
	}
	pu.Messages++
	saved := orig - comp
	if saved < 0 {
		saved = 0
	}
	pu.TokensSaved += saved

	// Update per-provider running average compression ratio using Welford's method.
	var ratio float64
	if orig > 0 {
		ratio = float64(comp) / float64(orig)
	} else {
		ratio = 1.0
	}
	delta2 := ratio - pu.AvgRatio
	pu.AvgRatio += delta2 / float64(pu.Messages)
}

// EstExtraMessages estimates how many additional requests the saved tokens would fund.
// Computed as total tokens saved divided by the average compressed tokens per request.
func (t *UsageTracker) EstExtraMessages() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	saved := t.InputTokensOrig - t.InputTokensComp
	if saved <= 0 || t.avgTokensPerReq <= 0 {
		return 0
	}
	return int(float64(saved) / t.avgTokensPerReq)
}

// AvgTTFTImprovement returns the average time-to-first-token improvement per request
// in seconds, calculated as: totalSavedTokens / prefillSpeed / requests.
func (t *UsageTracker) AvgTTFTImprovement() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.prefillSpeed <= 0 || t.MessagesSent == 0 {
		return 0
	}
	saved := t.InputTokensOrig - t.InputTokensComp
	if saved <= 0 {
		return 0
	}
	return float64(saved) / float64(t.prefillSpeed) / float64(t.MessagesSent)
}

// CompressionRatio returns the overall compression ratio (comp/orig).
// Returns 1.0 when no original tokens have been seen.
func (t *UsageTracker) CompressionRatio() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.InputTokensOrig == 0 {
		return 1.0
	}
	return float64(t.InputTokensComp) / float64(t.InputTokensOrig)
}

// Snapshot returns a point-in-time copy of the tracker state.
// The PerProvider map is deep-copied so the caller owns it safely.
func (t *UsageTracker) Snapshot() UsageSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	providerCopy := make(map[types.Provider]*ProviderUsage, len(t.PerProvider))
	for k, v := range t.PerProvider {
		copied := *v
		providerCopy[k] = &copied
	}

	return UsageSnapshot{
		SessionStart:    t.SessionStart,
		MessagesSent:    t.MessagesSent,
		InputTokensOrig: t.InputTokensOrig,
		InputTokensComp: t.InputTokensComp,
		OutputTokens:    t.OutputTokens,
		PerProvider:     providerCopy,
		AvgTokensPerReq: t.avgTokensPerReq,
		PrefillSpeed:    t.prefillSpeed,
	}
}

package summarization

import (
	"sync"
	"time"
)

// LatencyEstimator tracks an exponential-moving-average of observed Layer 2
// call durations. Used by T54 to skip L2 when a projected latency would
// blow the configured per-request budget. Thread-safe.
type LatencyEstimator struct {
	mu    sync.Mutex
	alpha float64
	// emaNs is the current EMA in nanoseconds. Kept as int64-compatible
	// float64 so sub-millisecond precision is preserved.
	emaNs float64
	// Count is the number of observations applied so far.
	Count int64
}

// NewLatencyEstimator returns a new estimator with the given EMA alpha. The
// alpha controls how quickly the EMA tracks new observations: 1.0 means
// "last observation only"; 0.0 means "never update". Values <= 0 or >= 1
// fall back to the conservative 0.2 default.
func NewLatencyEstimator(alpha float64) *LatencyEstimator {
	if alpha <= 0 || alpha >= 1 {
		alpha = 0.2
	}
	return &LatencyEstimator{alpha: alpha}
}

// Observe records a single latency sample. Negative or zero durations are
// silently ignored (they are instrumentation artefacts, not real latencies).
func (l *LatencyEstimator) Observe(d time.Duration) {
	if d <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	sample := float64(d.Nanoseconds())
	if l.Count == 0 {
		l.emaNs = sample
	} else {
		l.emaNs = l.alpha*sample + (1-l.alpha)*l.emaNs
	}
	l.Count++
}

// Projected returns the current EMA multiplied by `multiplier` as a safety
// margin. Before any observation the estimator seeds with a conservative
// 400 ms so the first request does not bypass the budget guard by accident.
func (l *LatencyEstimator) Projected(multiplier float64) time.Duration {
	if multiplier <= 0 {
		multiplier = 1
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.Count == 0 {
		return time.Duration(400.0 * float64(time.Millisecond) * multiplier)
	}
	return time.Duration(l.emaNs * multiplier)
}

// SkipReason enumerates why L2 may be skipped. Returned from ShouldRunLayer2
// so callers can log and report each reason distinctly.
type SkipReason int

const (
	// SkipReasonNone - L2 should run.
	SkipReasonNone SkipReason = iota
	// SkipReasonBelowThreshold - tokens < min_tokens_for_layer2.
	SkipReasonBelowThreshold
	// SkipReasonLatencyBudget - projected latency > budget.
	SkipReasonLatencyBudget
)

func (r SkipReason) String() string {
	switch r {
	case SkipReasonNone:
		return "run"
	case SkipReasonBelowThreshold:
		return "below_threshold"
	case SkipReasonLatencyBudget:
		return "latency_budget"
	}
	return "unknown"
}

// ShouldRunLayer2 decides whether to invoke Layer 2 for a request containing
// `tokens` tokens. The decision uses minTokens as the hard floor and
// latencyBudget+multiplier to guard against slow-MiniMax scenarios.
// budgetMs <= 0 disables the budget guard (legacy behaviour).
func ShouldRunLayer2(tokens, minTokens, budgetMs int, mult float64, est *LatencyEstimator) (bool, SkipReason) {
	if tokens < minTokens {
		return false, SkipReasonBelowThreshold
	}
	if budgetMs <= 0 || est == nil {
		return true, SkipReasonNone
	}
	projected := est.Projected(mult)
	budget := time.Duration(budgetMs) * time.Millisecond
	if projected > budget {
		return false, SkipReasonLatencyBudget
	}
	return true, SkipReasonNone
}

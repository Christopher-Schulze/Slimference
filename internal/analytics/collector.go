package analytics

import (
	"log/slog"
	"sync"
	"time"

	"github.com/slimference/slimference/internal/types"
)

// ProviderStats accumulates per-provider request statistics.
type ProviderStats struct {
	Messages         int
	InputTokensOrig  int
	InputTokensSaved int
	AvgRatio         float64
}

// Analytics holds all session-level metrics. Use NewAnalytics to construct.
type Analytics struct {
	mu sync.Mutex

	SessionStart time.Time

	TotalRequests     int
	TotalInputTokens  int // original, before compression
	TotalOutputTokens int
	SavedInputTokens  int // total original - total compressed

	Layer1Savings int
	Layer3Savings int

	CacheHits   int
	CacheMisses int

	PromptCacheReadTokens   int
	PromptCacheCreateTokens int
	PromptCacheReadRequests int

	SecretsRedacted int

	Errors           int
	AutoRetries      int // total of RateLimitRetries + OverflowRetries
	RateLimitRetries int // retries triggered by 429/529 rate-limit responses
	OverflowRetries  int // retries triggered by context-length overflow errors

	RequestLog *types.RingBuffer[types.RequestMetrics] // cap 100

	LatencyAnthropicMs float64 // running average
	LatencyOpenAIMs    float64 // running average

	perProvider map[types.Provider]*ProviderStats
}

// NewAnalytics initializes a fresh Analytics instance for a new session.
func NewAnalytics() *Analytics {
	return &Analytics{
		SessionStart: time.Now(),
		RequestLog:   types.NewRingBuffer[types.RequestMetrics](100),
		perProvider:  make(map[types.Provider]*ProviderStats),
	}
}

// Record processes one AnalyticsEvent and updates all relevant counters.
// Safe for concurrent use.
func (a *Analytics) Record(event types.AnalyticsEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch event.Type {
	case types.EventRequestProcessed:
		a.TotalRequests++
		a.TotalInputTokens += event.InputTokensOrig
		a.TotalOutputTokens += event.OutputTokens
		ps := a.providerStats(event.Provider)
		ps.Messages++
		saved := event.InputTokensOrig - event.InputTokensComp
		if saved > 0 {
			a.SavedInputTokens += saved
		}
		for _, layer := range event.Layers {
			switch layer {
			case 1:
				a.Layer1Savings += saved
			case 3:
				a.Layer3Savings += saved
			}
		}
		if event.CacheHit {
			a.CacheHits++
		} else {
			a.CacheMisses++
		}
		if event.CacheReadTokens > 0 {
			a.PromptCacheReadRequests++
			a.PromptCacheReadTokens += event.CacheReadTokens
		}
		if event.CacheCreateTokens > 0 {
			a.PromptCacheCreateTokens += event.CacheCreateTokens
		}
		// Update running average latency per provider.
		if event.LatencyMs > 0 {
			switch event.Provider {
			case types.Anthropic:
				a.LatencyAnthropicMs = updateRunningAvg(a.LatencyAnthropicMs, event.LatencyMs, ps.Messages)
			case types.OpenAI:
				a.LatencyOpenAIMs = updateRunningAvg(a.LatencyOpenAIMs, event.LatencyMs, ps.Messages)
			}
		}
		// Per-provider stats.
		ps.InputTokensOrig += event.InputTokensOrig
		if saved > 0 {
			ps.InputTokensSaved += saved
		}
		if ps.InputTokensOrig > 0 {
			ps.AvgRatio = float64(ps.InputTokensSaved) / float64(ps.InputTokensOrig)
		}
		// Ring buffer.
		a.RequestLog.Push(types.RequestMetrics{
			Timestamp:         event.Timestamp,
			Provider:          event.Provider,
			Model:             event.Model,
			InputTokensOrig:   event.InputTokensOrig,
			InputTokensComp:   event.InputTokensComp,
			OutputTokens:      event.OutputTokens,
			CompressionRatio:  event.CompressionRatio,
			Layers:            event.Layers,
			LatencyMs:         event.LatencyMs,
			CacheHit:          event.CacheHit,
			CacheReadTokens:   event.CacheReadTokens,
			CacheCreateTokens: event.CacheCreateTokens,
		})

	case types.EventCacheHit:
		a.CacheHits++

	case types.EventSecretDetected:
		a.SecretsRedacted += event.SecretsFound

	case types.EventErrorOccurred:
		a.Errors++
		if event.Error != "" {
			slog.Warn("analytics: recorded error event", slog.String("error", event.Error))
		}

	case types.EventLayerToggled:
		// No counter change; reserved for future dashboard events.

	case types.EventRateLimitRetry:
		a.AutoRetries++
		a.RateLimitRetries++

	case types.EventOverflowRetry:
		a.AutoRetries++
		a.OverflowRetries++

	default:
		slog.Warn("analytics: unknown event type", slog.Int("type", int(event.Type)))
	}
}

// CompressionRatio returns the fraction of input tokens saved across the session.
// Returns 0 if no input tokens have been processed.
func (a *Analytics) CompressionRatio() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.TotalInputTokens == 0 {
		return 0
	}
	return float64(a.SavedInputTokens) / float64(a.TotalInputTokens)
}

// EstExtraMessages estimates how many additional context-window messages the
// compression savings unlock, given an average tokens-per-request size.
func (a *Analytics) EstExtraMessages(avgTokensPerReq int) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	denom := avgTokensPerReq
	if denom < 1 {
		denom = 1
	}
	return a.SavedInputTokens / denom
}

// AvgTTFTImprovement returns the average time-to-first-token improvement per
// request in seconds, based on the prefill throughput (tokens/s).
func (a *Analytics) AvgTTFTImprovement(prefillSpeed int) float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.TotalRequests == 0 || prefillSpeed <= 0 {
		return 0
	}
	avgSaved := float64(a.SavedInputTokens) / float64(a.TotalRequests)
	return avgSaved / float64(prefillSpeed)
}

// Snapshot returns a value-type copy of the current analytics state.
// Safe to read without holding a lock after the call returns.
func (a *Analytics) Snapshot() AnalyticsSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	ps := make(map[types.Provider]ProviderStats, len(a.perProvider))
	for k, v := range a.perProvider {
		ps[k] = *v
	}
	avgPerReq := 0
	if a.TotalRequests > 0 {
		avgPerReq = (a.TotalInputTokens - a.SavedInputTokens) / a.TotalRequests
	}
	compressionRatio := 0.0
	if a.TotalInputTokens > 0 {
		compressionRatio = float64(a.SavedInputTokens) / float64(a.TotalInputTokens)
	}
	return AnalyticsSnapshot{
		SessionStart:            a.SessionStart,
		TotalRequests:           a.TotalRequests,
		TotalInputTokens:        a.TotalInputTokens,
		TotalOutputTokens:       a.TotalOutputTokens,
		SavedInputTokens:        a.SavedInputTokens,
		Layer1Savings:           a.Layer1Savings,
		Layer3Savings:           a.Layer3Savings,
		CacheHits:               a.CacheHits,
		CacheMisses:             a.CacheMisses,
		PromptCacheReadTokens:   a.PromptCacheReadTokens,
		PromptCacheCreateTokens: a.PromptCacheCreateTokens,
		PromptCacheReadRequests: a.PromptCacheReadRequests,
		SecretsRedacted:         a.SecretsRedacted,
		Errors:                  a.Errors,
		AutoRetries:             a.AutoRetries,
		RateLimitRetries:        a.RateLimitRetries,
		OverflowRetries:         a.OverflowRetries,
		LatencyAnthropicMs:      a.LatencyAnthropicMs,
		LatencyOpenAIMs:         a.LatencyOpenAIMs,
		PerProvider:             ps,
		AvgTokensPerRequest:     avgPerReq,
		CompressionRatio:        compressionRatio,
	}
}

// RecentRequests returns up to n most-recent request metrics from the ring buffer.
func (a *Analytics) RecentRequests(n int) []types.RequestMetrics {
	return a.RequestLog.Last(n)
}

// drainInput reads all currently buffered events from input and records them.
// Used by RunCollector on shutdown to flush pending events before returning.
func drainInput(input <-chan types.AnalyticsEvent, a *Analytics) {
	for {
		select {
		case event, ok := <-input:
			if !ok {
				return
			}
			a.Record(event)
		default:
			return
		}
	}
}

// RunCollector reads AnalyticsEvents from input and calls a.Record for each.
// Returns when done is closed, then signals completion by closing done (caller
// must pass a buffered or separate done channel; this func simply returns).
func RunCollector(input <-chan types.AnalyticsEvent, a *Analytics, done <-chan struct{}) {
	for {
		select {
		case event, ok := <-input:
			if !ok {
				return
			}
			a.Record(event)
		case <-done:
			drainInput(input, a)
			return
		}
	}
}

// AnalyticsSnapshot is a point-in-time copy of Analytics state (no mutex).
type AnalyticsSnapshot struct {
	SessionStart time.Time `json:"session_start"`

	TotalRequests     int `json:"total_requests"`
	TotalInputTokens  int `json:"total_input_tokens"`
	TotalOutputTokens int `json:"total_output_tokens"`
	SavedInputTokens  int `json:"saved_input_tokens"`

	Layer1Savings int `json:"layer1_savings"`
	Layer3Savings int `json:"layer3_savings"`

	CacheHits   int `json:"cache_hits"`
	CacheMisses int `json:"cache_misses"`

	PromptCacheReadTokens   int `json:"prompt_cache_read_tokens"`
	PromptCacheCreateTokens int `json:"prompt_cache_create_tokens"`
	PromptCacheReadRequests int `json:"prompt_cache_read_requests"`

	SecretsRedacted int `json:"secrets_redacted"`

	Errors           int `json:"errors"`
	AutoRetries      int `json:"auto_retries"`
	RateLimitRetries int `json:"rate_limit_retries"`
	OverflowRetries  int `json:"overflow_retries"`

	LatencyAnthropicMs float64 `json:"latency_anthropic_ms"`
	LatencyOpenAIMs    float64 `json:"latency_openai_ms"`

	PerProvider map[types.Provider]ProviderStats `json:"per_provider"`

	// Computed convenience fields populated by Snapshot().
	AvgTokensPerRequest int     `json:"avg_tokens_per_request"`
	CompressionRatio    float64 `json:"compression_ratio"` // fraction of tokens saved (0-1)
}

// EstExtraMessages estimates how many additional messages the token savings unlock.
func (s AnalyticsSnapshot) EstExtraMessages(avgPerReq int) int {
	if avgPerReq < 1 {
		avgPerReq = 1
	}
	return s.SavedInputTokens / avgPerReq
}

// AvgTTFTImprovement returns the average TTFT improvement per request in seconds.
func (s AnalyticsSnapshot) AvgTTFTImprovement(prefillSpeed int) float64 {
	if s.TotalRequests == 0 || prefillSpeed <= 0 {
		return 0
	}
	avgSaved := float64(s.SavedInputTokens) / float64(s.TotalRequests)
	return avgSaved / float64(prefillSpeed)
}

// PromptCacheHitRate returns the fraction of requests that reported a
// provider-side prompt-cache read hit.
func (s AnalyticsSnapshot) PromptCacheHitRate() float64 {
	if s.TotalRequests == 0 {
		return 0
	}
	return float64(s.PromptCacheReadRequests) / float64(s.TotalRequests)
}

// providerStats returns (creating if needed) the ProviderStats for p.
// Must be called with a.mu held.
func (a *Analytics) providerStats(p types.Provider) *ProviderStats {
	if ps, ok := a.perProvider[p]; ok {
		return ps
	}
	ps := &ProviderStats{}
	a.perProvider[p] = ps
	return ps
}

// updateRunningAvg returns an updated Cumulative Moving Average.
// n is the new total count (after incrementing).
func updateRunningAvg(current, newVal float64, n int) float64 {
	if n <= 1 {
		return newVal
	}
	return current + (newVal-current)/float64(n)
}

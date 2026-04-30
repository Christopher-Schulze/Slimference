// Package quality computes lightweight signals that hint at whether
// compression is hurting model behaviour. Intended to run alongside
// existing analytics; never replaces them.
//
// Signals (T77):
//
//   - Re-read rate: how often the same logical tool key reappears within
//     a sliding window of recent turns. Healthy sessions read once,
//     edit, move on; high re-read rate is a "compression too aggressive"
//     symptom.
//   - Cache-miss spike: a rolling prompt-cache hit-ratio detector that
//     flags when ratio drops below a baseline, indicating compression
//     destabilised the cacheable prefix.
//   - Net savings: tokens_saved minus an estimate of cache-invalidation
//     cost. A negative net is the strongest "compression is destructive"
//     signal we can produce without round-tripping the model.
//
// All public surface is safe for concurrent use by the proxy hot path.
package quality

import (
	"sync"
	"time"
)

// ReReadDetector maintains a per-session map of tool keys with the turn
// index at which they were last observed. When the same key appears
// within `windowTurns`, the re-read counter increments.
type ReReadDetector struct {
	mu          sync.Mutex
	windowTurns int
	maxSessions int
	maxKeys     int
	sessions    map[string]*sessionState
	totalChecks int64
	totalHits   int64
}

type sessionState struct {
	turn        int
	keyLastSeen map[string]int
}

// NewReReadDetector returns a detector with the supplied window size.
// windowTurns < 1 falls back to 10. The detector caps memory by
// evicting the oldest session and key entries when limits are hit.
func NewReReadDetector(windowTurns int) *ReReadDetector {
	if windowTurns < 1 {
		windowTurns = 10
	}
	return &ReReadDetector{
		windowTurns: windowTurns,
		maxSessions: 256,
		maxKeys:     512,
		sessions:    make(map[string]*sessionState),
	}
}

// Observe records that toolKey was used in sessionID's current turn.
// Returns true when the key was last seen within the configured
// window (a re-read), false otherwise. Empty keys are ignored.
func (d *ReReadDetector) Observe(sessionID, toolKey string) bool {
	if toolKey == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.totalChecks++

	st := d.sessions[sessionID]
	if st == nil {
		if len(d.sessions) >= d.maxSessions {
			d.evictOldestSessionLocked()
		}
		st = &sessionState{keyLastSeen: make(map[string]int)}
		d.sessions[sessionID] = st
	}
	st.turn++

	prev, ok := st.keyLastSeen[toolKey]
	hit := ok && st.turn-prev <= d.windowTurns
	if hit {
		d.totalHits++
	}
	if len(st.keyLastSeen) >= d.maxKeys {
		// Evict the entry seen longest ago.
		oldestKey := ""
		oldestTurn := st.turn + 1
		for k, t := range st.keyLastSeen {
			if t < oldestTurn {
				oldestTurn = t
				oldestKey = k
			}
		}
		if oldestKey != "" {
			delete(st.keyLastSeen, oldestKey)
		}
	}
	st.keyLastSeen[toolKey] = st.turn
	return hit
}

func (d *ReReadDetector) evictOldestSessionLocked() {
	// Simple eviction: drop the session with the smallest highest-turn
	// (loosely "oldest activity"). Linear scan is fine for the small
	// session caps we use.
	oldest := ""
	oldestTurn := -1
	for id, st := range d.sessions {
		if oldest == "" || st.turn < oldestTurn {
			oldest = id
			oldestTurn = st.turn
		}
	}
	if oldest != "" {
		delete(d.sessions, oldest)
	}
}

// Stats returns the current detector counters.
func (d *ReReadDetector) Stats() ReReadStats {
	d.mu.Lock()
	defer d.mu.Unlock()
	rate := 0.0
	if d.totalChecks > 0 {
		rate = float64(d.totalHits) / float64(d.totalChecks)
	}
	return ReReadStats{
		Sessions:    len(d.sessions),
		TotalChecks: d.totalChecks,
		TotalHits:   d.totalHits,
		Rate:        rate,
	}
}

// Reset zeroes all counters and drops every tracked session. Test helper.
func (d *ReReadDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sessions = make(map[string]*sessionState)
	d.totalChecks = 0
	d.totalHits = 0
}

// ReReadStats is the snapshot type exposed for /admin/status.quality.
type ReReadStats struct {
	Sessions    int     `json:"sessions"`
	TotalChecks int64   `json:"total_checks"`
	TotalHits   int64   `json:"total_hits"`
	Rate        float64 `json:"rate"`
}

// CacheMissSpikeDetector tracks the rolling prompt-cache hit ratio and
// raises an `Active` flag when the rate drops by `dropThreshold` against
// a recent baseline. Used to flag situations where a compression-config
// change destabilised the cacheable prefix.
type CacheMissSpikeDetector struct {
	mu              sync.Mutex
	windowSize      int
	dropThreshold   float64
	samples         []bool // true=hit, false=miss; ring buffer
	head            int
	filled          int
	baselineRate    float64
	baselineSet     bool
	lastSpikeAt     time.Time
	totalSpikeCount int64
	clock           func() time.Time
}

// NewCacheMissSpikeDetector returns a detector. Sensible defaults are
// applied when the inputs are non-positive: 50-sample window, 0.25 drop
// threshold (25% relative drop from baseline triggers a spike).
func NewCacheMissSpikeDetector(window int, dropThreshold float64) *CacheMissSpikeDetector {
	if window <= 0 {
		window = 50
	}
	if dropThreshold <= 0 {
		dropThreshold = 0.25
	}
	return &CacheMissSpikeDetector{
		windowSize:    window,
		dropThreshold: dropThreshold,
		samples:       make([]bool, window),
		clock:         time.Now,
	}
}

// Observe records a single cache-hit observation. Returns true when the
// observation pushed the rolling hit-rate below
// `baseline * (1 - dropThreshold)`.
func (d *CacheMissSpikeDetector) Observe(hit bool) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.samples[d.head] = hit
	d.head = (d.head + 1) % d.windowSize
	if d.filled < d.windowSize {
		d.filled++
	}
	if d.filled < d.windowSize {
		return false
	}
	hits := 0
	for i := 0; i < d.filled; i++ {
		if d.samples[i] {
			hits++
		}
	}
	rate := float64(hits) / float64(d.filled)
	// Establish the baseline as the first fully-populated rolling rate.
	if !d.baselineSet {
		d.baselineRate = rate
		d.baselineSet = true
		return false
	}
	threshold := d.baselineRate * (1 - d.dropThreshold)
	if rate < threshold {
		d.lastSpikeAt = d.clock()
		d.totalSpikeCount++
		return true
	}
	// Slow re-baseline so a healthy session lifts the bar.
	d.baselineRate = 0.95*d.baselineRate + 0.05*rate
	return false
}

// Stats returns the detector snapshot.
func (d *CacheMissSpikeDetector) Stats() CacheMissSpikeStats {
	d.mu.Lock()
	defer d.mu.Unlock()
	return CacheMissSpikeStats{
		WindowSize:      d.windowSize,
		Filled:          d.filled,
		BaselineRate:    d.baselineRate,
		Active:          !d.lastSpikeAt.IsZero(),
		LastSpikeUnix:   maxZeroUnix(d.lastSpikeAt),
		TotalSpikeCount: d.totalSpikeCount,
	}
}

// Reset zeroes the detector state. Test helper.
func (d *CacheMissSpikeDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.samples = make([]bool, d.windowSize)
	d.head = 0
	d.filled = 0
	d.baselineRate = 0
	d.baselineSet = false
	d.lastSpikeAt = time.Time{}
	d.totalSpikeCount = 0
}

// CacheMissSpikeStats is the snapshot type exposed for /admin/status.quality.
type CacheMissSpikeStats struct {
	WindowSize      int     `json:"window_size"`
	Filled          int     `json:"filled"`
	BaselineRate    float64 `json:"baseline_rate"`
	Active          bool    `json:"active"`
	LastSpikeUnix   int64   `json:"last_spike_unix"`
	TotalSpikeCount int64   `json:"total_spike_count"`
}

// NetSavings tracks aggregate token-savings minus estimated
// cache-invalidation cost. The estimator is intentionally minimal: when
// a compression change forces a cache miss the next request is treated
// as having paid the cache-invalidation cost equal to the prior
// `tokens_saved`. Net savings = saved - invalidation_cost.
type NetSavings struct {
	mu                sync.Mutex
	totalSaved        int64
	totalInvalidation int64
}

// NewNetSavings returns a fresh tracker.
func NewNetSavings() *NetSavings { return &NetSavings{} }

// RecordSaved adds saved tokens to the running total.
func (n *NetSavings) RecordSaved(saved int) {
	if saved <= 0 {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.totalSaved += int64(saved)
}

// RecordInvalidation logs the estimated cost of a cache invalidation
// caused by compression-config drift. Pass the prior-request saved
// token count.
func (n *NetSavings) RecordInvalidation(cost int) {
	if cost <= 0 {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.totalInvalidation += int64(cost)
}

// Stats returns the cumulative net savings.
func (n *NetSavings) Stats() NetSavingsStats {
	n.mu.Lock()
	defer n.mu.Unlock()
	return NetSavingsStats{
		TotalSaved:        n.totalSaved,
		TotalInvalidation: n.totalInvalidation,
		NetSaved:          n.totalSaved - n.totalInvalidation,
	}
}

// Reset zeroes the counters. Test helper.
func (n *NetSavings) Reset() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.totalSaved = 0
	n.totalInvalidation = 0
}

// NetSavingsStats is the snapshot type exposed for /admin/status.quality.
type NetSavingsStats struct {
	TotalSaved        int64 `json:"total_saved"`
	TotalInvalidation int64 `json:"total_invalidation"`
	NetSaved          int64 `json:"net_saved"`
}

// QualitySnapshot bundles all three signals so admin endpoints can emit
// one canonical block.
type QualitySnapshot struct {
	ReRead         ReReadStats         `json:"reread"`
	CacheMissSpike CacheMissSpikeStats `json:"cache_miss_spike"`
	NetSavings     NetSavingsStats     `json:"net_savings"`
}

func maxZeroUnix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

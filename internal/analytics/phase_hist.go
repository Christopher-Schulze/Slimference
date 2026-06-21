package analytics

import (
	"slices"
	"sync"
	"time"
)

// PhaseHistogram records the duration distribution of a single pipeline
// phase (e.g. Layer-1 compression, upstream call, Layer-2 summarisation).
// A rolling ring buffer of the last N observations feeds an on-demand
// p50/p95 calculation; atomic-ish counters hold cumulative totals.
//
// Safe for concurrent use. Record() is cheap (mutex + single ring write);
// Snapshot() is called at most once per admin/TUI tick so the sort cost
// is amortised.
type PhaseHistogram struct {
	mu sync.Mutex
	// name identifies the phase in emitted snapshots.
	name string
	// ring is the rolling window of observations in nanoseconds.
	ring []int64
	// ringIdx is the write position; wraps modulo cap(ring).
	ringIdx int
	// ringFilled tracks whether the ring has wrapped at least once.
	ringFilled bool
	// totalNs and count are cumulative, never reset.
	totalNs int64
	count   int64
	maxNs   int64
}

// NewPhaseHistogram returns a histogram with the given rolling window size.
// Passing window <= 0 uses the default 200.
func NewPhaseHistogram(name string, window int) *PhaseHistogram {
	if window <= 0 {
		window = 200
	}
	return &PhaseHistogram{
		name: name,
		ring: make([]int64, window),
	}
}

// Record adds one observation. Observations > 0 only; a zero duration is
// silently dropped since it is almost certainly instrumentation noise.
func (h *PhaseHistogram) Record(d time.Duration) {
	if d <= 0 {
		return
	}
	h.mu.Lock()
	ns := d.Nanoseconds()
	h.ring[h.ringIdx] = ns
	h.ringIdx++
	if h.ringIdx >= len(h.ring) {
		h.ringIdx = 0
		h.ringFilled = true
	}
	h.totalNs += ns
	h.count++
	if ns > h.maxNs {
		h.maxNs = ns
	}
	h.mu.Unlock()
}

// PhaseSnapshot captures histogram state at a point in time.
type PhaseSnapshot struct {
	Name       string  `json:"name"`
	Count      int64   `json:"count"`
	P50Ms      float64 `json:"p50_ms"`
	P95Ms      float64 `json:"p95_ms"`
	MaxMs      float64 `json:"max_ms"`
	AvgMs      float64 `json:"avg_ms"`
	SampleSize int     `json:"sample_size"`
}

// Snapshot returns current percentiles. When count is 0 the snapshot is
// zero-valued. p50/p95 are computed over the rolling window, max and avg
// over the full cumulative history.
func (h *PhaseHistogram) Snapshot() PhaseSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()

	s := PhaseSnapshot{Name: h.name, Count: h.count}
	if h.count == 0 {
		return s
	}
	s.AvgMs = nsToMs(h.totalNs / h.count)
	s.MaxMs = nsToMs(h.maxNs)

	// Build a sorted copy of the populated ring slice.
	var populated []int64
	if h.ringFilled {
		populated = make([]int64, len(h.ring))
		copy(populated, h.ring)
	} else {
		populated = make([]int64, h.ringIdx)
		copy(populated, h.ring[:h.ringIdx])
	}
	s.SampleSize = len(populated)
	if len(populated) == 0 {
		return s
	}
	slices.Sort(populated)
	s.P50Ms = nsToMs(percentile(populated, 0.50))
	s.P95Ms = nsToMs(percentile(populated, 0.95))
	return s
}

// percentile returns the p-th percentile of sorted samples using
// linear-interpolation rank (nearest lower + fraction).
func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := p * float64(len(sorted)-1)
	lo := int(rank)
	hi := lo + 1
	if hi >= len(sorted) {
		return sorted[lo]
	}
	frac := rank - float64(lo)
	return sorted[lo] + int64(frac*float64(sorted[hi]-sorted[lo]))
}

func nsToMs(ns int64) float64 {
	return float64(ns) / 1e6
}

// PipelineHistograms bundles the per-phase recorders that the proxy hot
// path instruments. One instance per proxy. Zero value is NOT usable;
// call NewPipelineHistograms first.
type PipelineHistograms struct {
	L1       *PhaseHistogram
	L2       *PhaseHistogram
	Upstream *PhaseHistogram
	Total    *PhaseHistogram
}

// NewPipelineHistograms returns a fresh histogram bundle with a 200-sample
// rolling window per phase. That keeps memory overhead negligible (about
// 8 KB per proxy) while yielding stable percentiles.
func NewPipelineHistograms() *PipelineHistograms {
	return &PipelineHistograms{
		L1:       NewPhaseHistogram("l1", 200),
		L2:       NewPhaseHistogram("l2", 200),
		Upstream: NewPhaseHistogram("upstream", 200),
		Total:    NewPhaseHistogram("total", 200),
	}
}

// Snapshot returns one snapshot per registered phase in a stable order
// (L1 -> L2 -> Upstream -> Total) so admin/TUI output is consistent.
func (p *PipelineHistograms) Snapshot() []PhaseSnapshot {
	return []PhaseSnapshot{
		p.L1.Snapshot(),
		p.L2.Snapshot(),
		p.Upstream.Snapshot(),
		p.Total.Snapshot(),
	}
}

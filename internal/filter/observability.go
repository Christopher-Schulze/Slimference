package filter

import (
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type FilterStats struct {
	Name              string
	Elapsed           time.Duration
	Panicked          bool
	Matched           bool
	InBytes           int
	OutBytes          int
	ContentClass      string
	SafetyClass       string
	Action            string
	Reason            string
	Signals           []string
	PreservedEvidence []string
}

type filterCounter struct {
	attempts   atomic.Int64
	calls      atomic.Int64
	misses     atomic.Int64
	panics     atomic.Int64
	bytesIn    atomic.Int64
	bytesOut   atomic.Int64
	bytesSaved atomic.Int64
	totalNs    atomic.Int64
	lastMu     sync.Mutex
	last       filterCounterLast
}

type filterCounterLast struct {
	ContentClass      string
	SafetyClass       string
	Action            string
	Reason            string
	Signals           []string
	PreservedEvidence []string
}

type FilterObservability struct {
	mu       sync.Map
	slowMs   int64
	slowOnce sync.Map
}

func NewFilterObservability(slowThresholdMs int) *FilterObservability {
	thresh := int64(slowThresholdMs)
	if thresh <= 0 {
		thresh = 50
	}
	return &FilterObservability{slowMs: thresh}
}

func (o *FilterObservability) getOrCreate(name string) *filterCounter {
	val, _ := o.mu.LoadOrStore(name, &filterCounter{})
	return val.(*filterCounter)
}

func (o *FilterObservability) Record(stats FilterStats) {
	c := o.getOrCreate(stats.Name)
	c.attempts.Add(1)
	c.totalNs.Add(stats.Elapsed.Nanoseconds())
	if stats.Panicked {
		c.panics.Add(1)
	}
	if stats.Matched {
		c.calls.Add(1)
	} else if !stats.Panicked {
		c.misses.Add(1)
	}
	c.bytesIn.Add(int64(stats.InBytes))
	c.bytesOut.Add(int64(stats.OutBytes))
	if stats.Matched && stats.InBytes > stats.OutBytes {
		c.bytesSaved.Add(int64(stats.InBytes - stats.OutBytes))
	}
	if stats.Elapsed.Milliseconds() > o.slowMs {
		_, loaded := o.slowOnce.LoadOrStore(stats.Name, true)
		if !loaded {
			slog.Warn("filter_slow",
				"filter", stats.Name,
				"elapsed_ms", stats.Elapsed.Milliseconds(),
				"in_bytes", stats.InBytes,
			)
		}
	}
	c.recordLast(stats)
}

func (c *filterCounter) recordLast(stats FilterStats) {
	if stats.ContentClass == "" && stats.SafetyClass == "" && stats.Action == "" && stats.Reason == "" && len(stats.Signals) == 0 && len(stats.PreservedEvidence) == 0 {
		return
	}
	c.lastMu.Lock()
	c.last = filterCounterLast{
		ContentClass:      strings.TrimSpace(stats.ContentClass),
		SafetyClass:       strings.TrimSpace(stats.SafetyClass),
		Action:            strings.TrimSpace(stats.Action),
		Reason:            strings.TrimSpace(stats.Reason),
		Signals:           append([]string(nil), stats.Signals...),
		PreservedEvidence: append([]string(nil), stats.PreservedEvidence...),
	}
	c.lastMu.Unlock()
}

func (o *FilterObservability) Snapshot() map[string]FilterSnapshot {
	out := make(map[string]FilterSnapshot)
	o.mu.Range(func(key, val any) bool {
		name := key.(string)
		c := val.(*filterCounter)
		attempts := c.attempts.Load()
		calls := c.calls.Load()
		totalNs := c.totalNs.Load()
		avgMs := float64(0)
		if attempts > 0 {
			avgMs = float64(totalNs) / float64(attempts) / 1e6
		}
		hitRate := float64(0)
		if attempts > 0 {
			hitRate = float64(calls) / float64(attempts)
		}
		out[name] = FilterSnapshot{
			Name:       name,
			Attempts:   attempts,
			Calls:      calls,
			Matches:    calls,
			Misses:     c.misses.Load(),
			Panics:     c.panics.Load(),
			BytesIn:    c.bytesIn.Load(),
			BytesOut:   c.bytesOut.Load(),
			BytesSaved: c.bytesSaved.Load(),
			HitRate:    hitRate,
			AvgMs:      avgMs,
		}
		last := c.snapshotLast()
		snap := out[name]
		snap.ContentClass = last.ContentClass
		snap.SafetyClass = last.SafetyClass
		snap.LastAction = last.Action
		snap.LastReason = last.Reason
		snap.Signals = last.Signals
		snap.PreservedEvidence = last.PreservedEvidence
		out[name] = snap
		return true
	})
	return out
}

func (c *filterCounter) snapshotLast() filterCounterLast {
	c.lastMu.Lock()
	defer c.lastMu.Unlock()
	out := c.last
	out.Signals = append([]string(nil), c.last.Signals...)
	out.PreservedEvidence = append([]string(nil), c.last.PreservedEvidence...)
	return out
}

type FilterSnapshot struct {
	Name              string   `json:"name"`
	Attempts          int64    `json:"attempts"`
	Calls             int64    `json:"calls"`
	Matches           int64    `json:"matches"`
	Misses            int64    `json:"misses"`
	Panics            int64    `json:"panics"`
	BytesIn           int64    `json:"bytes_in"`
	BytesOut          int64    `json:"bytes_out"`
	BytesSaved        int64    `json:"bytes_saved"`
	HitRate           float64  `json:"hit_rate"`
	AvgMs             float64  `json:"avg_ms"`
	ContentClass      string   `json:"content_class,omitempty"`
	SafetyClass       string   `json:"safety_class,omitempty"`
	LastAction        string   `json:"last_action,omitempty"`
	LastReason        string   `json:"last_reason,omitempty"`
	Signals           []string `json:"signals,omitempty"`
	PreservedEvidence []string `json:"preserved_evidence,omitempty"`
}

var globalObservability = NewFilterObservability(50)

func GlobalFilterObservability() *FilterObservability {
	return globalObservability
}

func runFilter(name string, fn func() ([]byte, bool)) (result []byte, matched bool, stats FilterStats) {
	stats = FilterStats{
		Name: name,
	}
	start := time.Now()
	defer func() {
		stats.Elapsed = time.Since(start)
		if r := recover(); r != nil {
			stats.Panicked = true
			stats.Matched = false
			slog.Error("filter_panic",
				"filter", name,
				"panic", fmt.Sprintf("%v", r),
				"stack", string(debug.Stack()),
			)
		}
	}()
	result, matched = fn()
	if matched {
		stats.Matched = true
	}
	return result, matched, stats
}

func setGlobalObservability(o *FilterObservability) {
	globalObservability = o
}

package filter

import (
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

type FilterStats struct {
	Name     string
	Elapsed  time.Duration
	Panicked bool
	Matched  bool
	InBytes  int
	OutBytes int
}

type filterCounter struct {
	calls      atomic.Int64
	panics     atomic.Int64
	bytesSaved atomic.Int64
	totalNs    atomic.Int64
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
	c.calls.Add(1)
	c.totalNs.Add(stats.Elapsed.Nanoseconds())
	if stats.Panicked {
		c.panics.Add(1)
	}
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
}

func (o *FilterObservability) Snapshot() map[string]FilterSnapshot {
	out := make(map[string]FilterSnapshot)
	o.mu.Range(func(key, val any) bool {
		name := key.(string)
		c := val.(*filterCounter)
		calls := c.calls.Load()
		totalNs := c.totalNs.Load()
		avgMs := float64(0)
		if calls > 0 {
			avgMs = float64(totalNs) / float64(calls) / 1e6
		}
		out[name] = FilterSnapshot{
			Name:       name,
			Calls:      calls,
			Panics:     c.panics.Load(),
			BytesSaved: c.bytesSaved.Load(),
			AvgMs:      avgMs,
		}
		return true
	})
	return out
}

type FilterSnapshot struct {
	Name       string  `json:"name"`
	Calls      int64   `json:"calls"`
	Panics     int64   `json:"panics"`
	BytesSaved int64   `json:"bytes_saved"`
	AvgMs      float64 `json:"avg_ms"`
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

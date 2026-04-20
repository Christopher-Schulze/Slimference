package proxy

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/types"
)

// newProxyForQueueTest constructs a minimal Proxy with just the analytics
// queue wired up. It does not run Start() or any goroutines; tests drive the
// counters directly.
func newProxyForQueueTest(t *testing.T, capacity int) *Proxy {
	t.Helper()
	return &Proxy{
		analyticsQueue: make(chan types.AnalyticsEvent, capacity),
	}
}

func TestTrySendAnalytics_EnqueueCounter(t *testing.T) {
	p := newProxyForQueueTest(t, 4)
	for i := 0; i < 3; i++ {
		p.trySendAnalytics(types.AnalyticsEvent{Type: types.EventRequestProcessed})
	}
	if got := p.analyticsEnqueued.Load(); got != 3 {
		t.Fatalf("enqueued = %d, want 3", got)
	}
	if got := p.analyticsDropped.Load(); got != 0 {
		t.Fatalf("dropped = %d, want 0", got)
	}
	stats := p.AnalyticsQueueStats()
	if stats.Capacity != 4 {
		t.Fatalf("capacity = %d, want 4", stats.Capacity)
	}
	if stats.Depth != 3 {
		t.Fatalf("depth = %d, want 3", stats.Depth)
	}
	if stats.EnqueuedTotal != 3 {
		t.Fatalf("enqueuedTotal = %d", stats.EnqueuedTotal)
	}
}

func TestTrySendAnalytics_DropCounter(t *testing.T) {
	// Capacity 2, send 5, expect 2 enqueued + 3 dropped.
	p := newProxyForQueueTest(t, 2)
	for i := 0; i < 5; i++ {
		p.trySendAnalytics(types.AnalyticsEvent{Type: types.EventRequestProcessed})
	}
	if got := p.analyticsEnqueued.Load(); got != 2 {
		t.Fatalf("enqueued = %d, want 2", got)
	}
	if got := p.analyticsDropped.Load(); got != 3 {
		t.Fatalf("dropped = %d, want 3", got)
	}
}

func TestNoteAnalyticsDrop_WarnRateLimit(t *testing.T) {
	// With a tiny interval each CAS succeeds because time advances.
	// With a huge interval only the very first call logs.
	prev := analyticsWarnIntervalNs
	analyticsWarnIntervalNs = int64(time.Hour)
	t.Cleanup(func() { analyticsWarnIntervalNs = prev })

	p := newProxyForQueueTest(t, 1)
	// Fill the queue so subsequent sends drop.
	p.analyticsQueue <- types.AnalyticsEvent{Type: types.EventRequestProcessed}

	for i := 0; i < 10; i++ {
		p.trySendAnalytics(types.AnalyticsEvent{Type: types.EventErrorOccurred})
	}
	if got := p.analyticsDropped.Load(); got != 10 {
		t.Fatalf("dropped = %d, want 10", got)
	}
	// lastWarn was set on the first drop; must be non-zero.
	if p.analyticsLastWarn.Load() == 0 {
		t.Fatal("lastWarn not set")
	}
}

func TestTrySendAnalytics_ConcurrentSafe(t *testing.T) {
	p := newProxyForQueueTest(t, 8)
	const workers = 16
	const per = 100
	var done int64
	for w := 0; w < workers; w++ {
		go func() {
			for i := 0; i < per; i++ {
				p.trySendAnalytics(types.AnalyticsEvent{Type: types.EventRequestProcessed})
			}
			atomic.AddInt64(&done, 1)
		}()
	}
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt64(&done) < workers && time.Now().Before(deadline) {
		// Drain to keep progress moving.
		select {
		case <-p.analyticsQueue:
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if atomic.LoadInt64(&done) < workers {
		t.Fatalf("workers did not finish: %d/%d", done, workers)
	}
	total := p.analyticsEnqueued.Load() + p.analyticsDropped.Load()
	if total != workers*per {
		t.Fatalf("total events = %d, want %d", total, workers*per)
	}
}

func TestAnalyticsQueueStatsSnapshot(t *testing.T) {
	p := newProxyForQueueTest(t, 3)
	p.analyticsEnqueued.Store(42)
	p.analyticsDropped.Store(7)
	p.analyticsQueue <- types.AnalyticsEvent{}
	s := p.AnalyticsQueueStats()
	if s.Capacity != 3 || s.Depth != 1 || s.EnqueuedTotal != 42 || s.DroppedTotal != 7 {
		t.Fatalf("snapshot = %+v", s)
	}
}

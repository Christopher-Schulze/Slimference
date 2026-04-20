package analytics

import (
	"sync"
	"testing"
	"time"
)

func TestPhaseHistogram_EmptySnapshot(t *testing.T) {
	h := NewPhaseHistogram("empty", 10)
	s := h.Snapshot()
	if s.Count != 0 || s.P50Ms != 0 || s.P95Ms != 0 || s.MaxMs != 0 || s.AvgMs != 0 {
		t.Fatalf("empty snapshot non-zero: %+v", s)
	}
	if s.Name != "empty" {
		t.Fatalf("name = %q", s.Name)
	}
}

func TestPhaseHistogram_ZeroDurationDropped(t *testing.T) {
	h := NewPhaseHistogram("x", 10)
	h.Record(0)
	h.Record(-1 * time.Millisecond)
	if h.Snapshot().Count != 0 {
		t.Fatal("zero/negative durations must be dropped")
	}
}

func TestPhaseHistogram_CumulativeCounters(t *testing.T) {
	h := NewPhaseHistogram("x", 10)
	h.Record(1 * time.Millisecond)
	h.Record(2 * time.Millisecond)
	h.Record(3 * time.Millisecond)
	s := h.Snapshot()
	if s.Count != 3 {
		t.Fatalf("count = %d", s.Count)
	}
	if s.AvgMs != 2.0 {
		t.Fatalf("avg = %v, want 2.0", s.AvgMs)
	}
	if s.MaxMs != 3.0 {
		t.Fatalf("max = %v, want 3.0", s.MaxMs)
	}
}

func TestPhaseHistogram_RollingWindowWraps(t *testing.T) {
	h := NewPhaseHistogram("x", 5)
	// Write 10 observations into a 5-slot ring. P95 should be among the last 5.
	for i := 1; i <= 10; i++ {
		h.Record(time.Duration(i) * time.Millisecond)
	}
	s := h.Snapshot()
	if s.SampleSize != 5 {
		t.Fatalf("sample size = %d, want 5", s.SampleSize)
	}
	// Last 5 values are 6..10 ms. P95 of [6,7,8,9,10] = 9.8 (linear interp).
	if s.P95Ms < 9.5 || s.P95Ms > 10.0 {
		t.Fatalf("p95 = %v, want ~9.8", s.P95Ms)
	}
	// Cumulative avg covers all 10, not just the window.
	if s.Count != 10 {
		t.Fatalf("count = %d, want 10", s.Count)
	}
}

func TestPhaseHistogram_PercentileInterpolation(t *testing.T) {
	// Explicit check on the percentile helper to pin linear-interp behaviour.
	sorted := []int64{1_000_000, 2_000_000, 3_000_000, 4_000_000, 5_000_000} // 1-5 ms
	if got := percentile(sorted, 0.50); got != 3_000_000 {
		t.Errorf("p50 = %d ns, want 3 ms", got)
	}
	if got := percentile(sorted, 0); got != 1_000_000 {
		t.Errorf("p0 = %d ns", got)
	}
	if got := percentile(sorted, 1); got != 5_000_000 {
		t.Errorf("p100 = %d ns", got)
	}
	if got := percentile(sorted, 0.25); got != 2_000_000 {
		t.Errorf("p25 = %d ns, want 2 ms", got)
	}
}

func TestPhaseHistogram_ConcurrentSafe(t *testing.T) {
	h := NewPhaseHistogram("x", 64)
	var wg sync.WaitGroup
	const workers = 8
	const per = 500
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for i := 0; i < per; i++ {
				h.Record(time.Microsecond * time.Duration(1+i%10))
			}
		}()
	}
	wg.Wait()
	s := h.Snapshot()
	if s.Count != int64(workers*per) {
		t.Fatalf("count = %d, want %d", s.Count, workers*per)
	}
	if s.AvgMs <= 0 {
		t.Fatalf("avg non-positive: %v", s.AvgMs)
	}
}

func TestPipelineHistograms_SnapshotOrder(t *testing.T) {
	h := NewPipelineHistograms()
	h.L1.Record(time.Millisecond)
	h.L2.Record(2 * time.Millisecond)
	h.Upstream.Record(10 * time.Millisecond)
	h.Total.Record(20 * time.Millisecond)
	snaps := h.Snapshot()
	want := []string{"l1", "l2", "l3", "upstream", "total"}
	if len(snaps) != len(want) {
		t.Fatalf("len = %d, want %d", len(snaps), len(want))
	}
	for i, w := range want {
		if snaps[i].Name != w {
			t.Errorf("snap[%d] name = %q, want %q", i, snaps[i].Name, w)
		}
	}
}

func BenchmarkPhaseHistogram_Record(b *testing.B) {
	h := NewPhaseHistogram("x", 200)
	d := 500 * time.Microsecond
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Record(d)
	}
}

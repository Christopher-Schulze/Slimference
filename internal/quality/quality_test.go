package quality

import (
	"testing"
	"time"
)

func TestReReadDetector_Observe_HitInWindow(t *testing.T) {
	t.Parallel()
	d := NewReReadDetector(3)
	if d.Observe("s1", "tool:read:a") {
		t.Fatal("first observation must not be a hit")
	}
	if d.Observe("s1", "tool:read:b") {
		t.Fatal("distinct key must not be a hit")
	}
	if !d.Observe("s1", "tool:read:a") {
		t.Fatal("re-read inside window must be a hit")
	}
	stats := d.Stats()
	if stats.TotalChecks != 3 || stats.TotalHits != 1 {
		t.Fatalf("stats wrong: %+v", stats)
	}
}

func TestReReadDetector_OutsideWindowMisses(t *testing.T) {
	t.Parallel()
	d := NewReReadDetector(2)
	d.Observe("s1", "k1")      // turn 1
	d.Observe("s1", "k2")      // turn 2
	d.Observe("s1", "k1")      // turn 3, prev=1, distance=2 == window -> hit
	d.Observe("s1", "filler1") // turn 4
	d.Observe("s1", "filler2") // turn 5
	if d.Observe("s1", "k1") { // turn 6, prev=3, distance=3 > 2 -> miss
		t.Fatal("outside window must miss")
	}
}

func TestReReadDetector_EmptyKeyIgnored(t *testing.T) {
	t.Parallel()
	d := NewReReadDetector(3)
	if d.Observe("s1", "") {
		t.Fatal("empty key must not register")
	}
	if d.Stats().TotalChecks != 0 {
		t.Fatal("empty key must not increment checks")
	}
}

func TestReReadDetector_DefaultWindow(t *testing.T) {
	t.Parallel()
	d := NewReReadDetector(0)
	if d.windowTurns != 10 {
		t.Fatalf("default window expected 10, got %d", d.windowTurns)
	}
}

func TestReReadDetector_SessionEviction(t *testing.T) {
	t.Parallel()
	d := NewReReadDetector(5)
	d.maxSessions = 2
	d.Observe("s1", "k")
	d.Observe("s2", "k")
	d.Observe("s3", "k")
	if got := d.Stats().Sessions; got > 2 {
		t.Fatalf("session cap not enforced: %d", got)
	}
}

func TestReReadDetector_KeyEviction(t *testing.T) {
	t.Parallel()
	d := NewReReadDetector(100)
	d.maxKeys = 2
	d.Observe("s1", "ka")
	d.Observe("s1", "kb")
	d.Observe("s1", "kc") // ka should be evicted
	if d.Observe("s1", "ka") {
		t.Fatal("evicted key must miss on re-observe")
	}
}

func TestReReadDetector_Reset(t *testing.T) {
	t.Parallel()
	d := NewReReadDetector(3)
	d.Observe("s", "k")
	d.Observe("s", "k")
	if d.Stats().TotalHits == 0 {
		t.Fatal("setup expected hit")
	}
	d.Reset()
	stats := d.Stats()
	if stats.Sessions != 0 || stats.TotalChecks != 0 {
		t.Fatalf("reset failed: %+v", stats)
	}
}

func TestReReadDetector_StatsRateZero(t *testing.T) {
	t.Parallel()
	d := NewReReadDetector(3)
	if got := d.Stats().Rate; got != 0 {
		t.Fatalf("rate must be 0, got %v", got)
	}
}

func TestCacheMissSpikeDetector_TriggersOnDrop(t *testing.T) {
	t.Parallel()
	d := NewCacheMissSpikeDetector(4, 0.25)
	fixed := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	d.clock = func() time.Time { return fixed }
	// Fill window with hits to set baseline = 1.0.
	for i := 0; i < 4; i++ {
		d.Observe(true)
	}
	// Now feed misses to drag rolling rate below 0.75.
	spike := false
	for i := 0; i < 4; i++ {
		if d.Observe(false) {
			spike = true
		}
	}
	if !spike {
		t.Fatal("expected spike alarm")
	}
	stats := d.Stats()
	if !stats.Active || stats.TotalSpikeCount == 0 {
		t.Fatalf("stats not updated: %+v", stats)
	}
	if stats.LastSpikeUnix != fixed.Unix() {
		t.Fatalf("last spike unix = %d", stats.LastSpikeUnix)
	}
}

func TestCacheMissSpikeDetector_NoSpikeWhileFilling(t *testing.T) {
	t.Parallel()
	d := NewCacheMissSpikeDetector(4, 0.25)
	if d.Observe(false) || d.Observe(true) {
		t.Fatal("observations before window full must not spike")
	}
}

func TestCacheMissSpikeDetector_BaselineDriftUp(t *testing.T) {
	t.Parallel()
	d := NewCacheMissSpikeDetector(4, 0.25)
	for i := 0; i < 4; i++ {
		d.Observe(true)
	}
	// All-hits keep the baseline near 1.0; no spike.
	for i := 0; i < 5; i++ {
		if d.Observe(true) {
			t.Fatal("steady state must not trigger")
		}
	}
}

func TestCacheMissSpikeDetector_Defaults(t *testing.T) {
	t.Parallel()
	d := NewCacheMissSpikeDetector(0, 0)
	if d.windowSize != 50 || d.dropThreshold != 0.25 {
		t.Fatalf("defaults wrong: window=%d threshold=%v", d.windowSize, d.dropThreshold)
	}
}

func TestCacheMissSpikeDetector_Reset(t *testing.T) {
	t.Parallel()
	d := NewCacheMissSpikeDetector(2, 0.25)
	d.Observe(true)
	d.Observe(false)
	d.Observe(false)
	d.Reset()
	stats := d.Stats()
	if stats.Filled != 0 || stats.Active {
		t.Fatalf("reset failed: %+v", stats)
	}
}

func TestNetSavings_RecordAndStats(t *testing.T) {
	t.Parallel()
	n := NewNetSavings()
	n.RecordSaved(100)
	n.RecordSaved(50)
	n.RecordInvalidation(30)
	stats := n.Stats()
	if stats.TotalSaved != 150 || stats.TotalInvalidation != 30 || stats.NetSaved != 120 {
		t.Fatalf("stats wrong: %+v", stats)
	}
}

func TestNetSavings_NonPositiveIgnored(t *testing.T) {
	t.Parallel()
	n := NewNetSavings()
	n.RecordSaved(0)
	n.RecordSaved(-5)
	n.RecordInvalidation(0)
	n.RecordInvalidation(-2)
	if n.Stats().TotalSaved != 0 {
		t.Fatal("non-positive saved must be ignored")
	}
	if n.Stats().TotalInvalidation != 0 {
		t.Fatal("non-positive invalidation must be ignored")
	}
}

func TestNetSavings_Reset(t *testing.T) {
	t.Parallel()
	n := NewNetSavings()
	n.RecordSaved(7)
	n.RecordInvalidation(3)
	n.Reset()
	stats := n.Stats()
	if stats.TotalSaved != 0 || stats.NetSaved != 0 {
		t.Fatalf("reset failed: %+v", stats)
	}
}

func TestMaxZeroUnix(t *testing.T) {
	t.Parallel()
	if got := maxZeroUnix(time.Time{}); got != 0 {
		t.Fatalf("zero time must produce 0, got %d", got)
	}
	when := time.Unix(1700000000, 0)
	if got := maxZeroUnix(when); got != 1700000000 {
		t.Fatalf("non-zero time wrong: %d", got)
	}
}

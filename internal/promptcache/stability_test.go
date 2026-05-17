package promptcache

import (
	"crypto/sha256"
	"testing"
	"time"
)

func hashOf(s string) PrefixHash { return sha256.Sum256([]byte(s)) }

func TestObserve_FirstHitIsCold(t *testing.T) {
	tr := NewTracker(10, time.Minute)
	o := tr.Observe("s", hashOf("system+tools+turn1"))
	if o.Confidence != ConfidenceCold || o.HitCount != 1 || !o.IsFirst {
		t.Fatalf("unexpected first observation: %+v", o)
	}
}

func TestObserve_SecondSameHashIsHot(t *testing.T) {
	tr := NewTracker(10, time.Minute)
	h := hashOf("prefix")
	_ = tr.Observe("s", h)
	o := tr.Observe("s", h)
	if o.Confidence != ConfidenceHot || o.HitCount != 2 {
		t.Fatalf("expected Hot after repeat, got %+v", o)
	}
}

func TestObserve_ChangedHashResetsStreak(t *testing.T) {
	tr := NewTracker(10, time.Minute)
	_ = tr.Observe("s", hashOf("a"))
	_ = tr.Observe("s", hashOf("a"))
	o := tr.Observe("s", hashOf("b"))
	if o.Confidence != ConfidenceCold || o.HitCount != 1 {
		t.Fatalf("expected reset on hash change, got %+v", o)
	}
}

func TestObserve_EmptySessionReturnsCold(t *testing.T) {
	tr := NewTracker(10, time.Minute)
	o := tr.Observe("", hashOf("x"))
	if o.Confidence != ConfidenceCold || o.HitCount != 0 || !o.IsFirst {
		t.Fatalf("empty session must be no-op, got %+v", o)
	}
}

func TestObserve_TTLEvictsOldEntries(t *testing.T) {
	tr := NewTracker(10, 100*time.Millisecond)
	frozen := time.Unix(1, 0)
	tr.now = func() time.Time { return frozen }
	_ = tr.Observe("s", hashOf("p"))
	// Advance past TTL.
	tr.now = func() time.Time { return frozen.Add(time.Second) }
	o := tr.Observe("s", hashOf("p"))
	// Old entry was evicted; new observation is treated as cold first hit.
	if o.Confidence != ConfidenceCold || o.HitCount != 1 || !o.IsFirst {
		t.Fatalf("expected fresh cold after TTL eviction, got %+v", o)
	}
}

func TestTracker_LRUEvictsOldestWhenFull(t *testing.T) {
	tr := NewTracker(3, time.Minute)
	frozen := time.Unix(100, 0)
	tr.now = func() time.Time { frozen = frozen.Add(time.Millisecond); return frozen }
	_ = tr.Observe("s1", hashOf("a"))
	_ = tr.Observe("s2", hashOf("b"))
	_ = tr.Observe("s3", hashOf("c"))
	// Touch s1 to mark MRU.
	_ = tr.Observe("s1", hashOf("a"))
	// Add s4 → should evict s2 (LRU).
	_ = tr.Observe("s4", hashOf("d"))
	if _, ok := tr.Snapshot("s2"); ok {
		t.Fatalf("s2 should have been LRU-evicted")
	}
	if _, ok := tr.Snapshot("s1"); !ok {
		t.Fatalf("s1 should remain after MRU touch")
	}
}

func TestTracker_LenReportsLive(t *testing.T) {
	tr := NewTracker(10, time.Minute)
	if tr.Len() != 0 {
		t.Fatalf("expected empty Len, got %d", tr.Len())
	}
	_ = tr.Observe("a", hashOf("x"))
	_ = tr.Observe("b", hashOf("y"))
	if tr.Len() != 2 {
		t.Fatalf("expected Len=2, got %d", tr.Len())
	}
}

func TestSnapshot_MissReturnsFalse(t *testing.T) {
	tr := NewTracker(10, time.Minute)
	if _, ok := tr.Snapshot("nope"); ok {
		t.Fatalf("missing session should return ok=false")
	}
	if _, ok := tr.Snapshot(""); ok {
		t.Fatalf("empty session id must return ok=false")
	}
}

func TestSnapshot_HitReturnsCurrentState(t *testing.T) {
	tr := NewTracker(10, time.Minute)
	_ = tr.Observe("s", hashOf("p"))
	_ = tr.Observe("s", hashOf("p"))
	snap, ok := tr.Snapshot("s")
	if !ok || snap.HitCount != 2 || snap.Confidence != ConfidenceHot {
		t.Fatalf("unexpected snapshot: %+v ok=%v", snap, ok)
	}
}

func TestForget_RemovesEntry(t *testing.T) {
	tr := NewTracker(10, time.Minute)
	_ = tr.Observe("s", hashOf("p"))
	tr.Forget("s")
	if tr.Len() != 0 {
		t.Fatalf("expected empty after Forget, Len=%d", tr.Len())
	}
}

func TestForget_EmptySessionIsNoOp(t *testing.T) {
	tr := NewTracker(10, time.Minute)
	tr.Forget("")
	tr.Forget("never-existed")
	// no panic, no change
}

func TestConfidenceFor(t *testing.T) {
	cases := map[int]Confidence{
		0: ConfidenceCold,
		1: ConfidenceCold,
		2: ConfidenceHot,
		5: ConfidenceHot,
	}
	for hc, want := range cases {
		if got := confidenceFor(hc); got != want {
			t.Errorf("confidenceFor(%d)=%v want %v", hc, got, want)
		}
	}
}

func TestNewTracker_ZeroFallsBackToDefaults(t *testing.T) {
	tr := NewTracker(0, 0)
	if tr.max != 1024 {
		t.Fatalf("default max should be 1024, got %d", tr.max)
	}
	if tr.ttl != 30*time.Minute {
		t.Fatalf("default ttl should be 30m, got %v", tr.ttl)
	}
}

func TestObserve_ConcurrencySafe(t *testing.T) {
	tr := NewTracker(100, time.Minute)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			_ = tr.Observe("a", hashOf("p"))
		}
		close(done)
	}()
	for i := 0; i < 1000; i++ {
		_ = tr.Observe("b", hashOf("q"))
	}
	<-done
	// If we reach here without race-detector firing, we're good.
	if tr.Len() != 2 {
		t.Fatalf("expected 2 sessions, got %d", tr.Len())
	}
}

func TestEvictExpired_SkipsMissingEntry(t *testing.T) {
	// Defensive: if order has a name not in entries (shouldn't
	// happen, but the code handles it), the eviction loop must
	// gracefully skip.
	tr := NewTracker(10, time.Minute)
	tr.order = append(tr.order, "ghost")
	tr.evictExpiredLocked(time.Now())
	if len(tr.order) != 0 {
		t.Fatalf("ghost entry should have been pruned, order=%v", tr.order)
	}
}

func TestTouchLocked_NoOpWhenAlreadyMRU(t *testing.T) {
	tr := NewTracker(10, time.Minute)
	_ = tr.Observe("only", hashOf("x"))
	// Already MRU; touching must not change order.
	tr.mu.Lock()
	tr.touchLocked("only")
	tr.mu.Unlock()
	if len(tr.order) != 1 || tr.order[0] != "only" {
		t.Fatalf("touch-already-MRU broke order: %v", tr.order)
	}
}

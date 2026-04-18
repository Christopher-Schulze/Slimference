package caching

import (
	"testing"
	"time"
)

// TestResponseCache_StageAPointerHit registers a Stage A pointer and verifies
// GetByOriginal resolves to the authoritative Stage B entry.
func TestResponseCache_StageAPointerHit(t *testing.T) {
	t.Parallel()
	cache := NewResponseCache(10, time.Minute)

	compKey := [32]byte{1, 2, 3}
	origKey := [32]byte{9, 8, 7}
	entry := makeEntry(`{"text":"cached"}`)

	cache.Set(compKey, entry)
	cache.RegisterOriginalPointer(origKey, compKey)

	got, resolved, ok := cache.GetByOriginal(origKey)
	if !ok {
		t.Fatal("expected pointer hit")
	}
	if resolved != compKey {
		t.Fatalf("resolved compKey mismatch")
	}
	if string(got.Response) != `{"text":"cached"}` {
		t.Fatalf("unexpected response: %s", got.Response)
	}
	if got.HitCount != 1 {
		t.Fatalf("HitCount = %d, want 1", got.HitCount)
	}
}

// TestResponseCache_StageAPointerMiss verifies a missing pointer returns miss.
func TestResponseCache_StageAPointerMiss(t *testing.T) {
	t.Parallel()
	cache := NewResponseCache(10, time.Minute)
	_, _, ok := cache.GetByOriginal([32]byte{42})
	if ok {
		t.Fatal("expected miss for unregistered pointer")
	}
}

// TestResponseCache_StageAPointerOrphan ensures the defensive orphan branch
// drops a stale pointer when the Stage B entry has vanished under it.
func TestResponseCache_StageAPointerOrphan(t *testing.T) {
	t.Parallel()
	cache := NewResponseCache(10, time.Minute)

	compKey := [32]byte{1}
	origKey := [32]byte{2}

	cache.Set(compKey, makeEntry("ok"))
	cache.RegisterOriginalPointer(origKey, compKey)

	// Manually corrupt state: drop the Stage B entry without touching the
	// pointer. Subsequent GetByOriginal must detect the orphan and prune.
	cache.mu.Lock()
	delete(cache.entries, compKey)
	cache.mu.Unlock()

	if _, _, ok := cache.GetByOriginal(origKey); ok {
		t.Fatal("expected orphan pointer to miss")
	}
	// Pointer must have been pruned.
	cache.mu.RLock()
	_, stillThere := cache.origToCompressed[origKey]
	cache.mu.RUnlock()
	if stillThere {
		t.Fatal("expected orphan pointer to be deleted on lookup")
	}
}

// TestResponseCache_StageAPointerTTLExpired verifies the pointer path respects
// the TTL and deletes the expired entry on lookup.
func TestResponseCache_StageAPointerTTLExpired(t *testing.T) {
	t.Parallel()
	cache := NewResponseCache(10, time.Millisecond)

	compKey := [32]byte{1}
	origKey := [32]byte{2}
	cache.Set(compKey, makeEntry("stale"))
	cache.RegisterOriginalPointer(origKey, compKey)

	time.Sleep(5 * time.Millisecond)

	if _, _, ok := cache.GetByOriginal(origKey); ok {
		t.Fatal("expected TTL expiry to miss")
	}
	if _, ok := cache.Get(compKey); ok {
		t.Fatal("expected Stage B entry to be removed after TTL miss")
	}
}

// TestResponseCache_RegisterOriginalPointerNoEntry verifies registration is
// a no-op when the target Stage B entry does not exist.
func TestResponseCache_RegisterOriginalPointerNoEntry(t *testing.T) {
	t.Parallel()
	cache := NewResponseCache(10, time.Minute)

	origKey := [32]byte{1}
	compKey := [32]byte{2}
	cache.RegisterOriginalPointer(origKey, compKey)

	if _, _, ok := cache.GetByOriginal(origKey); ok {
		t.Fatal("registration without a Stage B entry must not succeed")
	}
}

// TestResponseCache_PointerPrunedOnEviction ensures LRU eviction also drops
// pointers to the evicted entry.
func TestResponseCache_PointerPrunedOnEviction(t *testing.T) {
	t.Parallel()
	cache := NewResponseCache(2, time.Minute)

	comp1 := [32]byte{1}
	comp2 := [32]byte{2}
	comp3 := [32]byte{3}
	orig1 := [32]byte{10}

	cache.Set(comp1, makeEntry("first"))
	cache.RegisterOriginalPointer(orig1, comp1)

	cache.Set(comp2, makeEntry("second"))
	// Inserting comp3 evicts comp1 (LRU-oldest).
	cache.Set(comp3, makeEntry("third"))

	if _, _, ok := cache.GetByOriginal(orig1); ok {
		t.Fatal("expected orig1 pointer to be pruned when comp1 was evicted")
	}
}

// TestResponseCache_PointerPrunedOnInvalidate ensures Invalidate also drops
// pointers to invalidated entries.
func TestResponseCache_PointerPrunedOnInvalidate(t *testing.T) {
	t.Parallel()
	cache := NewResponseCache(10, time.Minute)

	compKey := [32]byte{1}
	origKey := [32]byte{10}
	entry := makeEntry("dep")
	entry.DependencyPaths = []string{"/tmp/file.go"}

	cache.Set(compKey, entry)
	cache.RegisterOriginalPointer(origKey, compKey)
	cache.Invalidate("/tmp/file.go")

	if _, _, ok := cache.GetByOriginal(origKey); ok {
		t.Fatal("expected pointer to be pruned when Stage B entry was invalidated")
	}
}

// TestResponseCache_PointerPrunedOnCleanup ensures Cleanup (TTL sweep) also
// drops pointers.
func TestResponseCache_PointerPrunedOnCleanup(t *testing.T) {
	t.Parallel()
	cache := NewResponseCache(10, time.Millisecond)

	compKey := [32]byte{1}
	origKey := [32]byte{10}
	cache.Set(compKey, makeEntry("fresh"))
	cache.RegisterOriginalPointer(origKey, compKey)

	time.Sleep(5 * time.Millisecond)
	cache.Cleanup()

	if _, _, ok := cache.GetByOriginal(origKey); ok {
		t.Fatal("expected pointer to be pruned after Cleanup")
	}
}

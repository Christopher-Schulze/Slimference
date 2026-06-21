package caching

import (
	"testing"
	"time"
)

func TestAgeSnapshot_EmptyCache(t *testing.T) {
	t.Parallel()
	cache := NewResponseCache(10, time.Minute)
	got := cache.AgeSnapshot()
	if got.Count != 0 || got.P50Ms != 0 || got.MaxMs != 0 {
		t.Fatalf("expected zero histogram, got %+v", got)
	}
}

func TestAgeSnapshot_PopulatedCache(t *testing.T) {
	t.Parallel()
	cache := NewResponseCache(10, time.Hour)
	for i := range 5 {
		var key [32]byte
		key[0] = byte(i)
		cache.Set(key, &CacheEntry{
			CreatedAt:  time.Now().Add(-time.Duration(i) * 100 * time.Millisecond),
			Response:   []byte("body"),
			StatusCode: 200,
		})
	}
	got := cache.AgeSnapshot()
	if got.Count != 5 {
		t.Fatalf("count: %d", got.Count)
	}
	if got.MaxMs <= got.P50Ms {
		t.Fatalf("expected MaxMs > P50Ms: max=%d p50=%d", got.MaxMs, got.P50Ms)
	}
	if got.P50Ms < 0 || got.P95Ms < 0 || got.P99Ms < 0 {
		t.Fatalf("ages must be non-negative: %+v", got)
	}
}

func TestAgeSnapshot_SingleEntry(t *testing.T) {
	t.Parallel()
	cache := NewResponseCache(10, time.Hour)
	var key [32]byte
	key[0] = 1
	cache.Set(key, &CacheEntry{
		CreatedAt:  time.Now().Add(-50 * time.Millisecond),
		Response:   []byte("x"),
		StatusCode: 200,
	})
	got := cache.AgeSnapshot()
	if got.Count != 1 {
		t.Fatalf("count: %d", got.Count)
	}
	// All percentiles fall on the same single entry.
	if got.P50Ms != got.P95Ms || got.P95Ms != got.P99Ms {
		t.Fatalf("single-entry percentiles must agree: %+v", got)
	}
}

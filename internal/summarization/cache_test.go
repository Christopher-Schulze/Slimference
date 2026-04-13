package summarization

import (
	"testing"
	"time"
)

func TestSummaryCache_GetStoreInvalidate(t *testing.T) {
	t.Parallel()
	c := NewSummaryCache()
	if c.Get() != nil {
		t.Fatal("expected nil")
	}
	cur, r := c.GetCurrent()
	if cur != nil || r != [2]int{} {
		t.Fatalf("GetCurrent empty: %#v %v", cur, r)
	}
	if !c.IsStale(time.Minute) {
		t.Fatal("empty cache should be stale")
	}

	sum := &CachedSummary{
		Summary:          "s",
		CoveredRange:     [2]int{0, 3},
		OriginalTokens:   100,
		CompressedTokens: 20,
		CreatedAt:        time.Now(),
	}
	c.Store(sum)
	if got := c.Get(); got == nil || got.Summary != "s" {
		t.Fatalf("Get: %#v", got)
	}
	cur, r = c.GetCurrent()
	if cur == nil || r != [2]int{0, 3} {
		t.Fatalf("GetCurrent: %#v %v", cur, r)
	}
	if c.IsStale(time.Millisecond) {
		t.Fatal("fresh summary should not be stale for 1ms maxAge")
	}
	if c.IsStale(24 * time.Hour) {
		t.Fatal("just-created summary should not be stale vs 24h")
	}
	c.Store(&CachedSummary{
		Summary:      "old",
		CoveredRange: [2]int{0, 1},
		CreatedAt:    time.Now().Add(-48 * time.Hour),
	})
	if !c.IsStale(24 * time.Hour) {
		t.Fatal("48h-old summary should be stale vs 24h maxAge")
	}

	c.Invalidate()
	if c.Get() != nil {
		t.Fatal("after invalidate")
	}
}

func TestSummaryCache_Compressing(t *testing.T) {
	t.Parallel()
	c := NewSummaryCache()
	if c.Compressing.Load() {
		t.Fatal("initially false")
	}
	c.Compressing.Store(true)
	if !c.Compressing.Load() {
		t.Fatal("expected true")
	}
}

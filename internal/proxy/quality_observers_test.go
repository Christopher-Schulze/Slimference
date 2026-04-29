package proxy

import (
	"testing"

	"github.com/slimference/slimference/internal/config"
	dbg "github.com/slimference/slimference/internal/debug"
)

func TestObserveQualityToolKey_RoundTrip(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	p.ObserveQualityToolKey("sess-1", "tool:read:src/auth/handler.go")
	p.ObserveQualityToolKey("sess-1", "tool:read:src/auth/handler.go")
	stats := p.QualitySnapshot().ReRead
	if stats.TotalChecks != 2 {
		t.Fatalf("checks: %d", stats.TotalChecks)
	}
	if stats.TotalHits == 0 {
		t.Fatal("re-read in window must register a hit")
	}
}

func TestObserveQualityCacheHit_FeedsSpikeDetector(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	for i := 0; i < 50; i++ {
		p.ObserveQualityCacheHit(true)
	}
	if p.QualitySnapshot().CacheMissSpike.Filled != 50 {
		t.Fatalf("expected window full")
	}
}

func TestObserveQualitySavings_AccumulatesNetSavings(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	p.ObserveQualitySavings(120)
	p.ObserveQualityInvalidation(20)
	stats := p.QualitySnapshot().NetSavings
	if stats.NetSaved != 100 || stats.TotalSaved != 120 || stats.TotalInvalidation != 20 {
		t.Fatalf("net savings off: %+v", stats)
	}
}

func TestObserveQuality_FromRequestSummary(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	summary := dbg.RequestSummary{
		CacheHit: true,
		Tokens:   dbg.TokenCounts{Saved: 75},
	}
	p.observeQuality(summary)
	got := p.QualitySnapshot()
	if got.NetSavings.TotalSaved != 75 {
		t.Fatalf("savings not observed: %+v", got.NetSavings)
	}
}

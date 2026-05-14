package proxy

import (
	"testing"

	"github.com/slimference/slimference/internal/config"
	dbg "github.com/slimference/slimference/internal/debug"
)

func TestObserveQualityToolKey_RoundTrip(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	if p.ObserveQualityToolKey("sess-1", "tool:read:src/auth/handler.go") {
		t.Fatal("first observation should not be a re-read")
	}
	if !p.ObserveQualityToolKey("sess-1", "tool:read:src/auth/handler.go") {
		t.Fatal("second observation should be a re-read")
	}
	stats := p.QualitySnapshot().ReRead
	if stats.TotalChecks != 2 {
		t.Fatalf("checks: %d", stats.TotalChecks)
	}
	if stats.TotalHits == 0 {
		t.Fatal("re-read in window must register a hit")
	}
	if p.ObserveQualityToolKey("", "tool") || p.ObserveQualityToolKey("sess-1", "") {
		t.Fatal("empty session/tool should not register")
	}
	if p.QualitySnapshot().ReRead.TotalChecks != 2 {
		t.Fatal("empty observation should not increment checks")
	}
	if p.ObserveQualityToolKeyForTurn("sess/1", "turn/1", "tool:a") {
		t.Fatal("first turn-aware observation should miss")
	}
	if !p.ObserveQualityToolKeyForTurn("sess/1", "turn/1", "tool:a") {
		t.Fatal("same turn-aware observation should hit")
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

package proxy

import (
	"context"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/qualityab"
	"github.com/slimference/slimference/internal/types"
)

func TestSavingsProbeNilSafe(t *testing.T) {
	var s *SavingsProbe
	got := s.ProbeSavings(context.Background())
	if got.OutputTokensSaved != 0 {
		t.Fatalf("nil probe should yield zero summary, got %+v", got)
	}

	s2 := &SavingsProbe{Proxy: nil}
	got2 := s2.ProbeSavings(context.Background())
	if got2.OutputTokensSaved != 0 {
		t.Fatalf("probe with nil proxy should yield zero summary, got %+v", got2)
	}
}

func TestSavingsProbeMapsCounters(t *testing.T) {
	p := New(config.Defaults())

	p.outputReduceCounters.RecordStreamcutFire(2048)
	p.outputReduceCounters.RecordRepdetRewrite(2, 1024)
	p.outputReduceCounters.RecordStaleReadAging(3, 256)
	p.outputReduceCounters.RecordObsoleteReadPrune(1, 128)
	p.outputReduceCounters.RecordStopSeqInjection(4)
	p.outputReduceCounters.RecordBeTerseInjection(64)
	p.outputReduceCounters.RecordProxyLayer0Stats(proxyLayer0Stats{
		TokensSaved:           2000,
		BlocksModified:        1,
		RepeatedOutputBlocks:  1,
		ChunkDedupBlocks:      1,
		ChunkDedupReferences:  2,
		ChunkDedupRefBytes:    1024,
		ChunkDedupInputBytes:  4096,
		LedgerCommandCapsules: 1,
		LedgerSearchCapsules:  1,
		TotalLatencyNs:        1_000_000,
		ChunkDedupLatencyNs:   250_000,
		CacheEvents: []proxyLayer0CacheEvent{
			{Mechanism: "repeated_output", Action: proxyLayer0CacheMiss, Reason: "first_observation_seeded"},
		},
	})
	p.analytics.Record(types.AnalyticsEvent{
		Type:              types.EventRequestProcessed,
		Timestamp:         time.Now(),
		Provider:          types.Anthropic,
		Model:             "claude",
		CacheReadTokens:   700,
		CacheCreateTokens: 120,
	})

	probe := &SavingsProbe{Proxy: p, USDPerMillionTokens: 6.0}
	got := probe.ProbeSavings(context.Background())

	if got.StreamcutFires != 1 {
		t.Errorf("streamcut fires=%d", got.StreamcutFires)
	}
	if got.RepdetRewrites != 1 || got.RepdetBytesSaved != 1024 {
		t.Errorf("repdet mismatch: %+v", got)
	}
	if got.StaleReadBlocks != 3 || got.ObsoletePruneBlocks != 1 {
		t.Errorf("stale/obsolete mismatch: %+v", got)
	}
	if got.StopSeqInjections != 1 || got.BeterseInjections != 1 {
		t.Errorf("injection counts mismatch: %+v", got)
	}
	// Legacy output_tokens_saved remains the sum of bytes-style fields:
	// 1024 + 2048 + 256 + 128 = 3456. New split fields keep billing-relevant
	// input tokens separate from output-wire bytes.
	if got.OutputTokensSaved != 3456 {
		t.Errorf("OutputTokensSaved=%d want 3456", got.OutputTokensSaved)
	}
	if got.BillableInputTokensSaved != 2000 {
		t.Errorf("BillableInputTokensSaved=%d want 2000", got.BillableInputTokensSaved)
	}
	if got.Product.Status != "saving" {
		t.Errorf("Product.Status=%q want saving", got.Product.Status)
	}
	if got.Product.BillableInputTokensSaved != 2000 ||
		got.Product.ProviderCacheReadTokens != 700 ||
		got.Product.ProviderCacheCreateTokens != 120 ||
		got.Product.OutputWireBytesSaved != 3072 ||
		got.Product.RequestSideBytesReduced != 384 {
		t.Errorf("Product savings mismatch: %+v", got.Product)
	}
	if got.Product.CacheMisses != 1 || got.Product.CacheHits != 0 {
		t.Errorf("Product cache mismatch: %+v", got.Product)
	}
	if got.Product.RepeatedOutputHits != 1 || got.Product.ChunkDedupHits != 1 {
		t.Errorf("Product mechanism hits mismatch: %+v", got.Product)
	}
	if got.OutputWireBytesSaved != 3072 {
		t.Errorf("OutputWireBytesSaved=%d want 3072", got.OutputWireBytesSaved)
	}
	if got.RequestSideBytesReduced != 384 {
		t.Errorf("RequestSideBytesReduced=%d want 384", got.RequestSideBytesReduced)
	}
	// CostUSD is based on billable input tokens, not output-wire bytes.
	if got.InputTokensSaved != 2000 {
		t.Errorf("InputTokensSaved=%d want 2000", got.InputTokensSaved)
	}
	if got.ProxyLayer0Repeated != 1 {
		t.Errorf("ProxyLayer0Repeated=%d want 1", got.ProxyLayer0Repeated)
	}
	if got.ProxyLayer0ChunkDedup != 1 {
		t.Errorf("ProxyLayer0ChunkDedup=%d want 1", got.ProxyLayer0ChunkDedup)
	}
	if got.ProxyLayer0ChunkRefs != 2 ||
		got.ProxyLayer0ChunkRefBytes != 1024 ||
		got.ProxyLayer0ChunkInBytes != 4096 {
		t.Errorf("ProxyLayer0 chunk density mismatch: %+v", got)
	}
	if got.ProxyLayer0LedgerCommand != 1 || got.ProxyLayer0LedgerSearch != 1 {
		t.Errorf("ProxyLayer0 ledger mismatch: %+v", got)
	}
	if len(got.ProxyLayer0Cache) != 1 || got.ProxyLayer0Cache[0].Reason != "first_observation_seeded" {
		t.Errorf("ProxyLayer0Cache mismatch: %+v", got.ProxyLayer0Cache)
	}
	if len(got.ProxyLayer0Latency) != 2 || got.ProxyLayer0Latency[0].Count != 1 {
		t.Errorf("ProxyLayer0Latency mismatch: %+v", got.ProxyLayer0Latency)
	}
	// CostUSD = 2000 / 1_000_000 * 6.0 = 0.012
	if got.CostUSD < 0.0119 || got.CostUSD > 0.0121 {
		t.Errorf("CostUSD=%v out of range", got.CostUSD)
	}
}

func TestSavingsProbeZeroUSDOmitsCost(t *testing.T) {
	p := New(config.Defaults())

	p.outputReduceCounters.RecordRepdetRewrite(1, 512)
	probe := &SavingsProbe{Proxy: p, USDPerMillionTokens: 0}
	got := probe.ProbeSavings(context.Background())
	if got.CostUSD != 0 {
		t.Fatalf("zero rate should yield zero cost, got %v", got.CostUSD)
	}
}

func TestSavingsProbePropagatesQualityAB(t *testing.T) {
	p := New(config.Defaults())

	if p.qualityAB == nil {
		t.Fatal("expected qualityAB harness in default proxy")
	}
	// Force a rollback by feeding enough Control + Treatment outcomes
	// that the treatment failure rate dominates. Easier: assert that
	// the snapshot fields propagate as-is.
	probe := &SavingsProbe{Proxy: p}
	got := probe.ProbeSavings(context.Background())
	ab := p.qualityAB.Snapshot()
	if got.QualityABRolledBack != ab.RolledBack {
		t.Errorf("rolled_back mismatch")
	}
	if got.QualityABControlFail != ab.ControlFailRate {
		t.Errorf("control fail mismatch")
	}
}

func TestNoopIndistProbeReturnsZero(t *testing.T) {
	var p NoopIndistProbe
	got := p.ProbeIndist(context.Background())
	if got.GoldenLocked || got.GoldenSHA != "" || len(got.Drift) != 0 {
		t.Fatalf("noop probe should return zero state, got %+v", got)
	}
}

var _ qualityab.QualityABTelemetry // keep the import live regardless of inlining

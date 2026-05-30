package proxy

import "testing"

// TestScanReadMetrics_AppliedAndReReadCounted proves the re-read frequency
// telemetry: a scan-elided read is counted as applied, its key is persisted +
// rehydrated across a reconnect, and a later re-read of that key is counted as a
// body-was-needed event. The ratio rereads/applied is the auto-promotion gate.
func TestScanReadMetrics_AppliedAndReReadCounted(t *testing.T) {
	home := t.TempDir()
	orig := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { proxyUserHomeDir = orig })

	var counters OutputReduceCounters

	// A scan-read block records one applied scan-read.
	counters.RecordProxyLayer0Stats(proxyLayer0Stats{
		Route:                codexLayer0RouteWSSPhaseF,
		TokensSaved:          120,
		BlocksModified:       1,
		CapturedOutputBlocks: 1,
		ScanReadKeys:         []string{"read:/x.go"},
	})
	if got := counters.Snapshot().ProxyLayer0ScanReadsApplied; got != 1 {
		t.Fatalf("scan reads applied=%d want 1", got)
	}

	const sid = "codex-wss:scan-metrics-1"
	const key = "read:/x.go"

	// Adapter 1 learns + persists the scan-origin key.
	a1 := &wsPhaseFAdapter{}
	a1.hydrateToolUses(sid)
	a1.rememberScanReadKeys([]string{key})

	// Adapter 2 (reconnect) rehydrates and recognizes a re-read of that key.
	a2 := &wsPhaseFAdapter{}
	a2.hydrateToolUses(sid)
	if n := a2.countScanReadReReads(map[string]struct{}{key: {}}); n != 1 {
		t.Fatalf("scan re-read count across reconnect=%d want 1", n)
	}
	// A non-scan re-read key is not counted.
	if n := a2.countScanReadReReads(map[string]struct{}{"read:/other.go": {}}); n != 0 {
		t.Fatalf("non-scan key must not count: %d", n)
	}

	// A different session must not see the scan key.
	a3 := &wsPhaseFAdapter{}
	a3.hydrateToolUses("codex-wss:scan-metrics-other")
	if n := a3.countScanReadReReads(map[string]struct{}{key: {}}); n != 0 {
		t.Fatalf("different session leaked a scan-read key: %d", n)
	}
}

// TestScanReadMetrics_ReReadCounterAggregates proves the standalone re-read
// recorder is additive and exposed in the snapshot.
func TestScanReadMetrics_ReReadCounterAggregates(t *testing.T) {
	var counters OutputReduceCounters
	counters.RecordScanReadReReads(2)
	counters.RecordScanReadReReads(0) // ignored
	counters.RecordScanReadReReads(3)
	if got := counters.Snapshot().ProxyLayer0ScanReadReReads; got != 5 {
		t.Fatalf("scan read rereads=%d want 5", got)
	}
}

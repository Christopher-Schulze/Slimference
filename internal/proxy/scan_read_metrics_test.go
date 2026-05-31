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
	if n := len(a2.scanReadReReadHits(map[string]struct{}{key: {}})); n != 1 {
		t.Fatalf("scan re-read count across reconnect=%d want 1", n)
	}
	// A non-scan re-read key is not counted.
	if n := len(a2.scanReadReReadHits(map[string]struct{}{"read:/other.go": {}})); n != 0 {
		t.Fatalf("non-scan key must not count: %d", n)
	}

	// A different session must not see the scan key.
	a3 := &wsPhaseFAdapter{}
	a3.hydrateToolUses("codex-wss:scan-metrics-other")
	if n := len(a3.scanReadReReadHits(map[string]struct{}{key: {}})); n != 0 {
		t.Fatalf("different session leaked a scan-read key: %d", n)
	}
}

// TestScanReadSelfRegulation_BlocksWhenRereadRateHigh proves the auto
// self-regulation: scan is allowed during cold-start and while the re-read rate
// is low, and is suppressed once enough scans show a high re-read rate - and the
// decision survives a reconnect via the persisted A and B sets.
func TestScanReadSelfRegulation_BlocksWhenRereadRateHigh(t *testing.T) {
	home := t.TempDir()
	orig := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { proxyUserHomeDir = orig })

	const sid = "codex-wss:selfreg-1"
	keys := []string{"r:a", "r:b", "r:c", "r:d", "r:e", "r:f", "r:g", "r:h"}

	a := &wsPhaseFAdapter{}
	a.hydrateToolUses(sid)

	// Cold-start: fewer than the min sample of scans -> never block.
	a.rememberScanReadKeys(keys[:4])
	a.rememberScanRereadKeys(keys[:4]) // 4/4 re-read, but below min sample
	if a.scanSelfRegBlock() {
		t.Fatalf("must not block during cold-start (applied=%d < %d)", 4, scanSelfRegMinSample)
	}

	// Enough scans, low re-read rate -> allow (fresh session).
	a2 := &wsPhaseFAdapter{}
	a2.hydrateToolUses("codex-wss:selfreg-low")
	a2.rememberScanReadKeys(keys)       // A = 8
	a2.rememberScanRereadKeys(keys[:3]) // B = 3, rate 0.375 < 0.5
	if a2.scanSelfRegBlock() {
		t.Fatalf("must allow when re-read rate below threshold (3/8)")
	}

	// High re-read rate at/above threshold -> block.
	a2.rememberScanRereadKeys(keys[3:4]) // B = 4, rate 0.5 >= 0.5
	if !a2.scanSelfRegBlock() {
		t.Fatalf("must block when re-read rate reaches threshold (4/8)")
	}

	// Reconnect: a fresh adapter rehydrates A and B and keeps blocking.
	a3 := &wsPhaseFAdapter{}
	a3.hydrateToolUses("codex-wss:selfreg-low")
	if !a3.scanSelfRegBlock() {
		t.Fatalf("self-reg block must survive reconnect via persisted A/B sets")
	}
	if hits := a3.scanReadReReadHits(map[string]struct{}{"r:a": {}, "r:zzz": {}}); len(hits) != 1 || hits[0] != "r:a" {
		t.Fatalf("scan re-read hits must rehydrate: %v", hits)
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

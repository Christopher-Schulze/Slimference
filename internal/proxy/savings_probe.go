package proxy

import (
	"context"

	"github.com/Christopher-Schulze/Slimference/internal/control"
	"github.com/Christopher-Schulze/Slimference/internal/qualityab"
)

// SavingsProbe maps the proxy's Phase F counters into the
// control.SavingsSummary surfaced via /admin/state and the TUI.
//
// Implementation lives in the proxy package because mapping reads
// Proxy-internal counters; the control package owns the SavingsSummary
// shape but knows nothing about which Proxy backs it.
type SavingsProbe struct {
	Proxy *Proxy
	// USDPerMillionTokens is the rough conversion factor for the cost
	// estimate. Pulled from cfg.Analytics.GainUSDPerMillionTokens at
	// startup so this probe does not depend on the live config.
	USDPerMillionTokens float64
}

// ProbeSavings implements control.SavingsProbe.
func (s *SavingsProbe) ProbeSavings(_ context.Context) control.SavingsSummary {
	if s == nil || s.Proxy == nil {
		return control.SavingsSummary{}
	}
	snap := s.Proxy.OutputReduceCountersSnapshot()
	toolPrune := ToolPruneStats{}
	if s.Proxy.toolPrune != nil {
		ts := s.Proxy.toolPrune.Snapshot()
		toolPrune = ToolPruneStats{
			PrunedTotal:    ts.PrunedTotal,
			ReattachTotal:  ts.ReattachTotal,
			MissTotal:      ts.MissTotal,
			RetryTotal:     ts.RetryTotal,
			TokensSavedSum: ts.TokensSavedSum,
		}
	}
	outputReduce := s.Proxy.outputReduce.Snapshot()
	analyticsQueue := s.Proxy.AnalyticsQueueStats()
	analyticsSnap := s.Proxy.analytics.Snapshot()
	ab := qualityab.QualityABTelemetry{}
	if s.Proxy.qualityAB != nil {
		ab = s.Proxy.qualityAB.Snapshot()
	}
	outputWireBytes := snap.RepdetBytesSaved + snap.StreamcutBytesObserved
	requestSideBytes := snap.StaleReadBytesReplaced + snap.ObsoleteReadBytesPruned + snap.WSSStatefulPrefixElisionBytes
	prefixTokensSaved := int64(snap.WSSStatefulPrefixElisionTokens)

	billableInputTokensSaved := int64(snap.ProxyLayer0TokensSaved) + toolPrune.TokensSavedSum + prefixTokensSaved
	out := control.SavingsSummary{
		InputTokensSaved:                  billableInputTokensSaved,
		OutputTokensSaved:                 int64(outputWireBytes + requestSideBytes),
		BillableInputTokensSaved:          billableInputTokensSaved,
		ProviderCacheReadTokens:           int64(analyticsSnap.PromptCacheReadTokens),
		ProviderCacheCreateTokens:         int64(analyticsSnap.PromptCacheCreateTokens),
		OutputWireBytesSaved:              int64(outputWireBytes),
		RequestSideBytesReduced:           int64(requestSideBytes),
		ToolPruneTokensSaved:              toolPrune.TokensSavedSum,
		ToolPrunePrunedTools:              toolPrune.PrunedTotal,
		ToolPruneReattached:               toolPrune.ReattachTotal,
		ToolPruneMisses:                   toolPrune.MissTotal,
		ToolPruneRetries:                  toolPrune.RetryTotal,
		OutputReduceInjectedTurns:         outputReduce.InjectedTurns,
		OutputReduceObservedTokens:        outputReduce.OutputTokensObserved,
		OutputReduceInputOverhead:         outputReduce.InputOverheadTokens,
		AnalyticsEventsDropped:            analyticsQueue.DroppedTotal,
		AnalyticsProofEventsDropped:       analyticsQueue.ProofDroppedTotal,
		AnalyticsLowPriorityEventsDropped: analyticsQueue.LowPriorityDroppedTotal,
		ProxyLayer0ToolResults:            int64(snap.ProxyLayer0ToolResultBlocks),
		ProxyLayer0ToolMisses:             int64(snap.ProxyLayer0ToolUseUnresolved),
		ProxyLayer0Commands:               int64(snap.ProxyLayer0CommandResolvedBlocks),
		ProxyLayer0CommandMisses:          int64(snap.ProxyLayer0CommandUnresolved),
		ProxyLayer0ReadAttempts:           int64(snap.ProxyLayer0ReadDeltaAttempts),
		ProxyLayer0ReadMisses:             int64(snap.ProxyLayer0ReadDeltaMisses),
		ProxyLayer0Blocks:                 int64(snap.ProxyLayer0BlocksModified),
		ProxyLayer0ReadDelta:              int64(snap.ProxyLayer0ReadDeltaBlocks),
		ProxyLayer0Captured:               int64(snap.ProxyLayer0CapturedBlocks),
		ProxyLayer0Envelope:               int64(snap.ProxyLayer0EnvelopeBlocks),
		ProxyLayer0Repeated:               int64(snap.ProxyLayer0RepeatedOutputBlocks),
		ProxyLayer0ChunkDedup:             int64(snap.ProxyLayer0ChunkDedupBlocks),
		ProxyLayer0ChunkRefs:              int64(snap.ProxyLayer0ChunkDedupReferences),
		ProxyLayer0ChunkRefBytes:          int64(snap.ProxyLayer0ChunkDedupRefBytes),
		ProxyLayer0ChunkInBytes:           int64(snap.ProxyLayer0ChunkDedupInputBytes),
		ProxyLayer0Routes: control.ProxyLayer0RoutesSummary{
			HTTP:      proxyLayer0RouteSummary(snap.ProxyLayer0Routes.HTTP),
			WSSPhaseF: proxyLayer0RouteSummary(snap.ProxyLayer0Routes.WSSPhaseF),
		},
		ProxyLayer0Policy:      proxyLayer0PolicySummary(snap.ProxyLayer0Policy),
		ProxyLayer0Cache:       proxyLayer0CacheSummary(snap.ProxyLayer0Cache),
		ProxyLayer0Latency:     proxyLayer0LatencySummary(snap.ProxyLayer0Latency),
		StreamcutFires:         int64(snap.StreamcutFired),
		RepdetRewrites:         int64(snap.RepdetResponsesRewritten),
		RepdetBytesSaved:       int64(snap.RepdetBytesSaved),
		StaleReadBlocks:        int64(snap.StaleReadBlocksReplaced),
		ObsoletePruneBlocks:    int64(snap.ObsoleteReadBlocksPruned),
		StopSeqInjections:      int64(snap.StopSeqRequestsModified),
		BeterseInjections:      int64(snap.BeterseInjections),
		QualityABRolledBack:    ab.RolledBack,
		QualityABControlFail:   ab.ControlFailRate,
		QualityABTreatmentFail: ab.TreatmentFailRate,
	}
	if s.USDPerMillionTokens > 0 && out.InputTokensSaved > 0 {
		out.CostUSD = float64(out.InputTokensSaved) / 1_000_000.0 * s.USDPerMillionTokens
	}
	out.Product = out.ProductSignals()
	return out
}

func proxyLayer0LatencySummary(entries []ProxyLayer0LatencyEntry) []control.ProxyLayer0LatencyEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]control.ProxyLayer0LatencyEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, control.ProxyLayer0LatencyEntry{
			Route:      entry.Route,
			Mechanism:  entry.Mechanism,
			Count:      entry.Count,
			P50Ms:      entry.P50Ms,
			P95Ms:      entry.P95Ms,
			MaxMs:      entry.MaxMs,
			AvgMs:      entry.AvgMs,
			SampleSize: entry.SampleSize,
		})
	}
	return out
}

func proxyLayer0CacheSummary(entries []ProxyLayer0CacheEntry) []control.ProxyLayer0CacheEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]control.ProxyLayer0CacheEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, control.ProxyLayer0CacheEntry{
			Route:     entry.Route,
			Mechanism: entry.Mechanism,
			Action:    entry.Action,
			Reason:    entry.Reason,
			Count:     int64(entry.Count),
		})
	}
	return out
}

func proxyLayer0PolicySummary(entries []ProxyLayer0PolicyEntry) []control.ProxyLayer0PolicyEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]control.ProxyLayer0PolicyEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, control.ProxyLayer0PolicyEntry{
			Route:       entry.Route,
			Mechanism:   entry.Mechanism,
			Action:      entry.Action,
			Reason:      entry.Reason,
			BlockReason: entry.BlockReason,
			Count:       int64(entry.Count),
		})
	}
	return out
}

func proxyLayer0RouteSummary(t ProxyLayer0RouteTelemetry) control.ProxyLayer0RouteSummary {
	return control.ProxyLayer0RouteSummary{
		ToolResults:      int64(t.ToolResultBlocks),
		ToolMisses:       int64(t.ToolUseUnresolved),
		Commands:         int64(t.CommandResolvedBlocks),
		CommandMisses:    int64(t.CommandUnresolved),
		ReadAttempts:     int64(t.ReadDeltaAttempts),
		ReadMisses:       int64(t.ReadDeltaMisses),
		RequestsModified: int64(t.RequestsModified),
		TokensSaved:      int64(t.TokensSaved),
		BlocksModified:   int64(t.BlocksModified),
		ReadDeltaBlocks:  int64(t.ReadDeltaBlocks),
		CapturedBlocks:   int64(t.CapturedBlocks),
		EnvelopeBlocks:   int64(t.EnvelopeBlocks),
		RepeatedBlocks:   int64(t.RepeatedOutputBlocks),
		ChunkDedupBlocks: int64(t.ChunkDedupBlocks),
		ChunkDedupRefs:   int64(t.ChunkDedupReferences),
		ChunkDedupRefB:   int64(t.ChunkDedupRefBytes),
		ChunkDedupInB:    int64(t.ChunkDedupInputBytes),
		Cache:            proxyLayer0CacheSummary(t.Cache),
	}
}

// NoopIndistProbe is a placeholder probe used until T198 wires the
// real tshark-based capture. Returns the zero IndistState.
type NoopIndistProbe struct{}

// ProbeIndist implements control.IndistProbe.
func (NoopIndistProbe) ProbeIndist(_ context.Context) control.IndistState {
	return control.IndistState{}
}

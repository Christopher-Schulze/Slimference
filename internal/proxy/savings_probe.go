package proxy

import (
	"context"

	"github.com/slimference/slimference/internal/control"
	"github.com/slimference/slimference/internal/qualityab"
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
	ab := qualityab.QualityABTelemetry{}
	if s.Proxy.qualityAB != nil {
		ab = s.Proxy.qualityAB.Snapshot()
	}

	out := control.SavingsSummary{
		InputTokensSaved:       int64(snap.ProxyLayer0TokensSaved),
		OutputTokensSaved:      int64(snap.RepdetBytesSaved + snap.StreamcutBytesObserved + snap.StaleReadBytesReplaced + snap.ObsoleteReadBytesPruned),
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
	if s.USDPerMillionTokens > 0 && out.OutputTokensSaved > 0 {
		out.CostUSD = float64(out.OutputTokensSaved) / 1_000_000.0 * s.USDPerMillionTokens
	}
	return out
}

// NoopIndistProbe is a placeholder probe used until T198 wires the
// real tshark-based capture. Returns the zero IndistState.
type NoopIndistProbe struct{}

// ProbeIndist implements control.IndistProbe.
func (NoopIndistProbe) ProbeIndist(_ context.Context) control.IndistState {
	return control.IndistState{}
}

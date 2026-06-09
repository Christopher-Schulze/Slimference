package proxy

import (
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/compactsignal"
)

// precompactSignalTTL is how recently a Codex PreCompact hook must
// have fired for the proxy to escalate Layer-1 aggression on the
// current request. Sixty seconds gives the user time to start typing
// the next prompt while keeping the signal fresh enough to avoid
// spurious escalation on a stale marker file. Tunable at config-flag
// granularity later; literal for now to keep the surface tight.
const precompactSignalTTL = 60 * time.Second

// hasRecentPreCompactSignal returns true if a PreCompact marker exists
// for this session and is younger than precompactSignalTTL. Returns
// false when the proxy was constructed without a compact-signal store
// (e.g., os.UserHomeDir() failed during New), so callers can skip the
// nil-check.
func (p *Proxy) hasRecentPreCompactSignal(sessionID string) bool {
	if p == nil || p.compactSignals == nil || sessionID == "" {
		return false
	}
	return p.compactSignals.HasRecentSignal(compactsignal.PhasePre, sessionID, precompactSignalTTL)
}

// precompactShrinkWindow returns the more-aggressive sliding window
// size when a PreCompact signal is active. Empirically: halve the
// window, floor at 1. If the configured window is already 1, no
// further shrink is possible and the function is a no-op. Centralised
// here so the bias policy is one place to tune.
func precompactShrinkWindow(window int) int {
	if window <= 1 {
		return window
	}
	return window / 2
}

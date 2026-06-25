package proxy

import "strconv"

// crossTurnKeystoneVerdict composes the cross-turn in-transit reduction gate
// (the §0.6 stateless-detach keystone) into ONE fail-closed decision, so every
// cross-turn reduction slice (read-coalescing, superseded-command pruning,
// stale-read aging, tool-schema dedup) consults a single contract instead of
// the five historical guards.
//
// A reduced delta / full-history turn is safe to APPLY only when ALL hold:
//   - recoveryReady: the omitted bytes are stateless-recovery-ready — the
//     Slimference-owned response chain exists, so the mutated turn can detach
//     previous_response_id and continue statelessly. This kills V2 (server-state
//     poisoning -> upstream invalid_request 400).
//   - mutationRecoverable: the reduction is structurally recoverable (archive /
//     stateless continuation), so comprehension is never lost (recovery always
//     exists).
//   - !cacheBustDemoted: the reduction does not bust the provider cache for this
//     scope beyond the proven threshold. This accounts for V3.
//
// Any unmet condition -> not apply-eligible (shadow / full-pass), fail-closed.
// blockReason names the single first failing condition so live telemetry can
// size how many turns the keystone could safely apply on (the proof-gate-step-2
// sizing) before any mutation is flipped on.
func crossTurnKeystoneVerdict(recoveryReady, mutationRecoverable, cacheBustDemoted bool) (applyEligible bool, blockReason string) {
	switch {
	case !recoveryReady:
		return false, "no_stateless_recovery_chain"
	case !mutationRecoverable:
		return false, "not_structurally_recoverable"
	case cacheBustDemoted:
		return false, "provider_cache_bust_demoted"
	default:
		return true, ""
	}
}

// attachWSSKeystoneVerdictDebugFacts records the unified keystone verdict as
// observe-only telemetry on the request meta. It mutates nothing on the wire;
// it only exposes, per turn, whether the cross-turn keystone could safely apply
// and (when not) the single blocking reason. Mirrors the survival pattern of the
// delta-stateless-recovery and cache-bust-demoted facts.
func attachWSSKeystoneVerdictDebugFacts(meta *wssRequestMeta, recoveryReady, mutationRecoverable, cacheBustDemoted bool) {
	if meta == nil {
		return
	}
	if meta.DebugFacts == nil {
		meta.DebugFacts = make(map[string]string)
	}
	eligible, reason := crossTurnKeystoneVerdict(recoveryReady, mutationRecoverable, cacheBustDemoted)
	meta.DebugFacts["wss.keystone_apply_eligible"] = strconv.FormatBool(eligible)
	if reason != "" {
		meta.DebugFacts["wss.keystone_block_reason"] = reason
	}
}

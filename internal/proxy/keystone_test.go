package proxy

import "testing"

func TestCrossTurnKeystoneVerdict(t *testing.T) {
	tests := []struct {
		name                string
		recoveryReady       bool
		mutationRecoverable bool
		cacheBustDemoted    bool
		wantEligible        bool
		wantReason          string
	}{
		{"all green -> apply eligible", true, true, false, true, ""},
		{"no recovery chain kills V2 -> blocked first", false, true, false, false, "no_stateless_recovery_chain"},
		{"no recovery dominates even if other flags bad", false, false, true, false, "no_stateless_recovery_chain"},
		{"not structurally recoverable -> blocked", true, false, false, false, "not_structurally_recoverable"},
		{"recoverable but cache-bust demoted (V3) -> blocked", true, true, true, false, "provider_cache_bust_demoted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eligible, reason := crossTurnKeystoneVerdict(tt.recoveryReady, tt.mutationRecoverable, tt.cacheBustDemoted)
			if eligible != tt.wantEligible || reason != tt.wantReason {
				t.Fatalf("crossTurnKeystoneVerdict(%v,%v,%v) = (%v,%q) want (%v,%q)",
					tt.recoveryReady, tt.mutationRecoverable, tt.cacheBustDemoted, eligible, reason, tt.wantEligible, tt.wantReason)
			}
		})
	}
}

// TestCrossTurnKeystoneVerdictFailClosed is the safety guard: apply is eligible
// ONLY when every condition is satisfied. If any future edit makes a missing
// recovery chain, an unrecoverable mutation, or a cache-bust still report
// eligible, the keystone would open V2/V3 — this must never happen.
func TestCrossTurnKeystoneVerdictFailClosed(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		for _, recoverable := range []bool{false, true} {
			for _, demoted := range []bool{false, true} {
				eligible, _ := crossTurnKeystoneVerdict(recovery, recoverable, demoted)
				wantEligible := recovery && recoverable && !demoted
				if eligible != wantEligible {
					t.Fatalf("fail-closed violated: recovery=%v recoverable=%v demoted=%v eligible=%v want %v",
						recovery, recoverable, demoted, eligible, wantEligible)
				}
			}
		}
	}
}

func TestAttachWSSKeystoneVerdictDebugFacts(t *testing.T) {
	// Nil meta is a no-op (no panic).
	attachWSSKeystoneVerdictDebugFacts(nil, true, true, false)

	var meta wssRequestMeta
	attachWSSKeystoneVerdictDebugFacts(&meta, true, true, false)
	if meta.DebugFacts["wss.keystone_apply_eligible"] != "true" {
		t.Fatalf("eligible turn must report true, got %q", meta.DebugFacts["wss.keystone_apply_eligible"])
	}
	if _, ok := meta.DebugFacts["wss.keystone_block_reason"]; ok {
		t.Fatalf("eligible turn must not carry a block reason, got %q", meta.DebugFacts["wss.keystone_block_reason"])
	}

	var blocked wssRequestMeta
	attachWSSKeystoneVerdictDebugFacts(&blocked, false, true, false)
	if blocked.DebugFacts["wss.keystone_apply_eligible"] != "false" {
		t.Fatalf("blocked turn must report false")
	}
	if blocked.DebugFacts["wss.keystone_block_reason"] != "no_stateless_recovery_chain" {
		t.Fatalf("blocked turn must name the reason, got %q", blocked.DebugFacts["wss.keystone_block_reason"])
	}
}

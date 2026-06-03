package savingspolicy

import "testing"

func TestDecideCodexToolOutputAutoEnablesRecoverableChunkDedup(t *testing.T) {
	t.Parallel()
	got := DecideCodexToolOutput(CodexToolOutputInput{
		Mode:                     "auto",
		Route:                    CodexRouteWSSPhaseF,
		ArchiveRecoveryAvailable: true,
		OutputBytes:              9000,
		ChunkMinBytes:            8192,
	})
	if !got.ReadDelta || !got.RepeatedOutput || !got.ChunkDedup || !got.NeedsRecoveryNote || got.Loosened {
		t.Fatalf("auto policy should enable safe reducers plus recoverable chunk dedup: %+v", got)
	}
	if got.Reason != "auto_recoverable_chunk_dedup" {
		t.Fatalf("reason=%q", got.Reason)
	}
}

func TestDecideCodexToolOutputLoosensForContextRisk(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		in   CodexToolOutputInput
	}{
		{name: "recent edit", in: CodexToolOutputInput{Mode: "auto", Route: CodexRouteWSSPhaseF, ArchiveRecoveryAvailable: true, RecentlyEdited: true, OutputBytes: 9000, ChunkMinBytes: 1}},
		{name: "post-collapse reread", in: CodexToolOutputInput{Mode: "auto", Route: CodexRouteWSSPhaseF, ArchiveRecoveryAvailable: true, PostCollapseReRead: true, OutputBytes: 9000, ChunkMinBytes: 1}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DecideCodexToolOutput(tc.in)
			if !got.Loosened || got.ChunkDedup || got.ReadDelta || got.RepeatedOutput {
				t.Fatalf("context-risk signal should full-pass managed reducers: %+v", got)
			}
		})
	}
}

func TestDecideCodexToolOutputModes(t *testing.T) {
	t.Parallel()
	off := DecideCodexToolOutput(CodexToolOutputInput{Mode: "off", Route: CodexRouteWSSPhaseF, ArchiveRecoveryAvailable: true, OutputBytes: 9000})
	if off.ReadDelta || off.RepeatedOutput || off.ChunkDedup {
		t.Fatalf("off mode must disable policy-managed reducers: %+v", off)
	}
	conservative := DecideCodexToolOutput(CodexToolOutputInput{
		Mode:                     "conservative",
		Route:                    CodexRouteWSSPhaseF,
		ArchiveRecoveryAvailable: true,
		OutputBytes:              9000,
		ChunkMinBytes:            1,
	})
	if !conservative.ReadDelta || !conservative.RepeatedOutput || conservative.ChunkDedup {
		t.Fatalf("conservative mode should keep lossless reducers and skip auto chunk dedup: %+v", conservative)
	}
	forced := DecideCodexToolOutput(CodexToolOutputInput{
		Mode:                     "conservative",
		Route:                    CodexRouteWSSPhaseF,
		ArchiveRecoveryAvailable: true,
		ExplicitChunkDedup:       true,
		OutputBytes:              9000,
		ChunkMinBytes:            1,
	})
	if !forced.ChunkDedup || !forced.NeedsRecoveryNote {
		t.Fatalf("explicit chunk toggle should still work in conservative mode: %+v", forced)
	}
}

func TestDecideCodexToolOutputNeverEnablesFirstReadScan(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		in   CodexToolOutputInput
	}{
		{name: "max read with recovery still does not scan", in: CodexToolOutputInput{Mode: "max", Route: CodexRouteWSSPhaseF, IsRead: true, ArchiveRecoveryAvailable: true, OutputBytes: 9000}},
		{name: "max read without recovery", in: CodexToolOutputInput{Mode: "max", IsRead: true, ArchiveRecoveryAvailable: false, OutputBytes: 9000}},
		{name: "max non-read", in: CodexToolOutputInput{Mode: "max", Route: CodexRouteWSSPhaseF, IsRead: false, ArchiveRecoveryAvailable: true, OutputBytes: 9000}},
		{name: "auto read", in: CodexToolOutputInput{Mode: "auto", Route: CodexRouteWSSPhaseF, IsRead: true, ArchiveRecoveryAvailable: true, OutputBytes: 9000}},
		{name: "conservative read", in: CodexToolOutputInput{Mode: "conservative", Route: CodexRouteWSSPhaseF, IsRead: true, ArchiveRecoveryAvailable: true, OutputBytes: 9000}},
		{name: "recent edit full-passes the read", in: CodexToolOutputInput{Mode: "max", Route: CodexRouteWSSPhaseF, IsRead: true, ArchiveRecoveryAvailable: true, RecentlyEdited: true, OutputBytes: 9000}},
		{name: "post-collapse reread full-passes the read", in: CodexToolOutputInput{Mode: "max", Route: CodexRouteWSSPhaseF, IsRead: true, ArchiveRecoveryAvailable: true, PostCollapseReRead: true, OutputBytes: 9000}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DecideCodexToolOutput(tc.in)
			if (tc.in.RecentlyEdited || tc.in.PostCollapseReRead) && !got.Loosened {
				t.Fatalf("context-risk read must loosen (full-pass), not scan: %+v", got)
			}
			if got.Reason == "first_read_elision" {
				t.Fatalf("first-read elision must not be a policy outcome: %+v", got)
			}
		})
	}
}

func TestValidCodexMode(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"", "off", "conservative", "safe", "auto", "max", "aggressive"} {
		if !ValidCodexMode(mode) {
			t.Fatalf("mode %q should be valid", mode)
		}
	}
	if ValidCodexMode("reckless") {
		t.Fatal("unknown mode must be invalid")
	}
}

func TestDecideCodexMechanismMatrix(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		in     CodexMechanismInput
		action CodexPolicyAction
		reason string
		note   bool
	}{
		{
			name: "wss lossless read delta allow",
			in: CodexMechanismInput{
				Mode: string(CodexModeAuto), Route: CodexRouteWSSPhaseF,
				Mechanism: CodexMechanismReadDelta, Risk: CodexRiskLossless, Recovery: CodexRecoveryExact,
			},
			action: CodexPolicyAllow, reason: "lossless_or_exact_reducer",
		},
		{
			name: "wss recoverable chunk allow",
			in: CodexMechanismInput{
				Mode: string(CodexModeAuto), Route: CodexRouteWSSPhaseF,
				Mechanism: CodexMechanismChunkDedup, Risk: CodexRiskRecoverable, Recovery: CodexRecoveryArchive,
				ArchiveRecoveryAvailable: true, OutputBytes: 9000, MinBytes: 4096,
			},
			action: CodexPolicyAllow, reason: "recoverable_chunk_dedup", note: true,
		},
		{
			name: "http archive refs blocked even in max",
			in: CodexMechanismInput{
				Mode: string(CodexModeMax), Route: CodexRouteHTTP,
				Mechanism: CodexMechanismChunkDedup, Risk: CodexRiskRecoverable, Recovery: CodexRecoveryArchive,
				ArchiveRecoveryAvailable: true, OutputBytes: 9000, MinBytes: 1, Explicit: true,
			},
			action: CodexPolicyBlock, reason: "http_archive_recovery_unproven",
		},
		{
			name: "conservative recoverable chunk needs explicit",
			in: CodexMechanismInput{
				Mode: string(CodexModeConservative), Route: CodexRouteWSSPhaseF,
				Mechanism: CodexMechanismChunkDedup, Risk: CodexRiskRecoverable, Recovery: CodexRecoveryArchive,
				ArchiveRecoveryAvailable: true, OutputBytes: 9000, MinBytes: 1,
			},
			action: CodexPolicyBlock, reason: "conservative_requires_explicit_recovery",
		},
		{
			name: "recent edit full pass",
			in: CodexMechanismInput{
				Mode: string(CodexModeAuto), Route: CodexRouteWSSPhaseF,
				Mechanism: CodexMechanismChunkDedup, Risk: CodexRiskRecoverable, Recovery: CodexRecoveryArchive,
				ArchiveRecoveryAvailable: true, RecentlyEdited: true, OutputBytes: 9000,
			},
			action: CodexPolicyFullPass, reason: "recent_edit_full_context",
		},
		{
			name: "first read elision remains shadow only",
			in: CodexMechanismInput{
				Mode: string(CodexModeMax), Route: CodexRouteWSSPhaseF,
				Mechanism: CodexMechanismFirstReadElision, Risk: CodexRiskReconstructive, Recovery: CodexRecoveryArchive,
				ArchiveRecoveryAvailable: true, OutputBytes: 9000,
			},
			action: CodexPolicyShadow, reason: "capture_or_ab_proof_required",
		},
		{
			name: "semantic reasoning compression shadow only",
			in: CodexMechanismInput{
				Mode: string(CodexModeMax), Route: CodexRouteWSSPhaseF,
				Mechanism: CodexMechanismReasoningCompact, Risk: CodexRiskSemantic, Recovery: CodexRecoveryArchive,
				ArchiveRecoveryAvailable: true, OutputBytes: 9000,
			},
			action: CodexPolicyShadow, reason: "capture_or_ab_proof_required",
		},
		{
			name: "recoverable generic mechanism needs archive",
			in: CodexMechanismInput{
				Mode: string(CodexModeAuto), Route: CodexRouteWSSPhaseF,
				Mechanism: "future_recoverable", Risk: CodexRiskRecoverable, Recovery: CodexRecoveryArchive,
				OutputBytes: 9000,
			},
			action: CodexPolicyBlock, reason: "archive_recovery_unavailable",
		},
		{
			name: "recoverable generic mechanism allow with archive",
			in: CodexMechanismInput{
				Mode: string(CodexModeAuto), Route: CodexRouteWSSPhaseF,
				Mechanism: "future_recoverable", Risk: CodexRiskRecoverable, Recovery: CodexRecoveryArchive,
				ArchiveRecoveryAvailable: true, OutputBytes: 9000,
			},
			action: CodexPolicyAllow, reason: "recoverable_with_archive", note: true,
		},
		{
			name: "session budget hit full pass",
			in: CodexMechanismInput{
				Mode: string(CodexModeAuto), Route: CodexRouteWSSPhaseF,
				Mechanism: CodexMechanismChunkDedup, Risk: CodexRiskRecoverable, Recovery: CodexRecoveryArchive,
				ArchiveRecoveryAvailable: true, OutputBytes: 9000, SessionIntegrityBudgetHit: true,
			},
			action: CodexPolicyFullPass, reason: "session_integrity_budget",
		},
		{
			name: "quality spike full pass",
			in: CodexMechanismInput{
				Mode: string(CodexModeAuto), Route: CodexRouteWSSPhaseF,
				Mechanism: CodexMechanismChunkDedup, Risk: CodexRiskRecoverable, Recovery: CodexRecoveryArchive,
				ArchiveRecoveryAvailable: true, OutputBytes: 9000, QualitySpike: true,
			},
			action: CodexPolicyFullPass, reason: "quality_signal_full_context",
		},
		{
			name: "archive recovery loop full pass",
			in: CodexMechanismInput{
				Mode: string(CodexModeAuto), Route: CodexRouteWSSPhaseF,
				Mechanism: CodexMechanismChunkDedup, Risk: CodexRiskRecoverable, Recovery: CodexRecoveryArchive,
				ArchiveRecoveryAvailable: true, OutputBytes: 9000, ArchiveRecoveryLoop: true,
			},
			action: CodexPolicyFullPass, reason: "archive_recovery_loop_full_context",
		},
		{
			name: "missing tool retry full pass",
			in: CodexMechanismInput{
				Mode: string(CodexModeAuto), Route: CodexRouteWSSPhaseF,
				Mechanism: CodexMechanismChunkDedup, Risk: CodexRiskRecoverable, Recovery: CodexRecoveryArchive,
				ArchiveRecoveryAvailable: true, OutputBytes: 9000, MissingToolRetry: true,
			},
			action: CodexPolicyFullPass, reason: "missing_tool_retry_full_context",
		},
		{
			name: "degraded route full pass",
			in: CodexMechanismInput{
				Mode: string(CodexModeAuto), Route: CodexRouteWSSPhaseF,
				Mechanism: CodexMechanismChunkDedup, Risk: CodexRiskRecoverable, Recovery: CodexRecoveryArchive,
				ArchiveRecoveryAvailable: true, OutputBytes: 9000, DegradedRoute: true,
			},
			action: CodexPolicyFullPass, reason: "degraded_route_full_context",
		},
		{
			name: "host budget full pass",
			in: CodexMechanismInput{
				Mode: string(CodexModeAuto), Route: CodexRouteWSSPhaseF,
				Mechanism: CodexMechanismChunkDedup, Risk: CodexRiskRecoverable, Recovery: CodexRecoveryArchive,
				ArchiveRecoveryAvailable: true, OutputBytes: 9000, HostBudgetExceeded: true,
			},
			action: CodexPolicyFullPass, reason: "host_budget_full_context",
		},
		{
			name: "host budget keeps lossless exact reducers",
			in: CodexMechanismInput{
				Mode: string(CodexModeAuto), Route: CodexRouteWSSPhaseF,
				Mechanism: CodexMechanismReadDelta, Risk: CodexRiskLossless, Recovery: CodexRecoveryExact,
				OutputBytes: 9000, HostBudgetExceeded: true,
			},
			action: CodexPolicyAllow, reason: "lossless_or_exact_reducer_host_budget",
		},
		{
			name: "latency budget full pass",
			in: CodexMechanismInput{
				Mode: string(CodexModeAuto), Route: CodexRouteWSSPhaseF,
				Mechanism: CodexMechanismChunkDedup, Risk: CodexRiskRecoverable, Recovery: CodexRecoveryArchive,
				ArchiveRecoveryAvailable: true, OutputBytes: 9000, LatencyBudgetExceeded: true,
			},
			action: CodexPolicyFullPass, reason: "latency_budget_full_context",
		},
		{
			name: "negative savings history full pass",
			in: CodexMechanismInput{
				Mode: string(CodexModeAuto), Route: CodexRouteWSSPhaseF,
				Mechanism: CodexMechanismChunkDedup, Risk: CodexRiskRecoverable, Recovery: CodexRecoveryArchive,
				ArchiveRecoveryAvailable: true, OutputBytes: 9000, NegativeSavingsHistory: true,
			},
			action: CodexPolicyFullPass, reason: "negative_savings_full_context",
		},
		{
			name: "below min bytes blocks chunk",
			in: CodexMechanismInput{
				Mode: string(CodexModeAuto), Route: CodexRouteWSSPhaseF,
				Mechanism: CodexMechanismChunkDedup, Risk: CodexRiskRecoverable, Recovery: CodexRecoveryArchive,
				ArchiveRecoveryAvailable: true, OutputBytes: 1000, MinBytes: 4096,
			},
			action: CodexPolicyBlock, reason: "below_min_bytes",
		},
		{
			name: "unsupported shape blocks",
			in: CodexMechanismInput{
				Mode: string(CodexModeAuto), Route: CodexRouteWSSPhaseF,
				Mechanism: "unsupported", Risk: "unknown", Recovery: "unknown",
				OutputBytes: 9000,
			},
			action: CodexPolicyBlock, reason: "unsupported_policy_shape",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DecideCodexMechanism(tc.in)
			if got.Action != tc.action || got.Reason != tc.reason || got.NeedsRecoveryNote != tc.note {
				t.Fatalf("decision mismatch: got=%+v want action=%s reason=%s note=%v", got, tc.action, tc.reason, tc.note)
			}
		})
	}
}

func TestDecideCodexToolOutputRuntimeSignalsFullPass(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*CodexToolOutputInput)
		reason string
	}{
		{name: "quality spike", mutate: func(in *CodexToolOutputInput) { in.QualitySpike = true }, reason: "quality_signal_full_context"},
		{name: "archive recovery loop", mutate: func(in *CodexToolOutputInput) { in.ArchiveRecoveryLoop = true }, reason: "archive_recovery_loop_full_context"},
		{name: "missing tool retry", mutate: func(in *CodexToolOutputInput) { in.MissingToolRetry = true }, reason: "missing_tool_retry_full_context"},
		{name: "degraded route", mutate: func(in *CodexToolOutputInput) { in.DegradedRoute = true }, reason: "degraded_route_full_context"},
		{name: "latency budget", mutate: func(in *CodexToolOutputInput) { in.LatencyBudgetExceeded = true }, reason: "latency_budget_full_context"},
		{name: "negative savings", mutate: func(in *CodexToolOutputInput) { in.NegativeSavingsHistory = true }, reason: "negative_savings_full_context"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := CodexToolOutputInput{
				Mode:                     string(CodexModeAuto),
				Route:                    CodexRouteWSSPhaseF,
				ArchiveRecoveryAvailable: true,
				OutputBytes:              9000,
				ChunkMinBytes:            1,
			}
			tc.mutate(&in)
			got := DecideCodexToolOutput(in)
			if !got.Loosened || got.ReadDelta || got.RepeatedOutput || got.ChunkDedup || got.Reason != tc.reason {
				t.Fatalf("runtime signal should force full-pass: got=%+v want reason=%s", got, tc.reason)
			}
		})
	}
}

func TestDecideCodexToolOutputHostBudgetKeepsLosslessReducers(t *testing.T) {
	t.Parallel()
	got := DecideCodexToolOutput(CodexToolOutputInput{
		Mode:                     string(CodexModeAuto),
		Route:                    CodexRouteWSSPhaseF,
		ArchiveRecoveryAvailable: true,
		OutputBytes:              9000,
		ChunkMinBytes:            1,
		HostBudgetExceeded:       true,
	})
	if got.Loosened || !got.ReadDelta || !got.RepeatedOutput || got.ChunkDedup {
		t.Fatalf("host budget should keep lossless reducers but demote chunk: %+v", got)
	}
	if actionForMechanism(got.Mechanisms, CodexMechanismReadDelta) != CodexPolicyAllow ||
		actionForMechanism(got.Mechanisms, CodexMechanismRepeatedOutput) != CodexPolicyAllow ||
		actionForMechanism(got.Mechanisms, CodexMechanismChunkDedup) != CodexPolicyFullPass {
		t.Fatalf("host budget mechanism actions mismatch: %+v", got.Mechanisms)
	}
}

func TestDecideCodexToolOutputRecentEditUncertaintyOnlyDemotesChunk(t *testing.T) {
	t.Parallel()
	got := DecideCodexToolOutput(CodexToolOutputInput{
		Mode:                     string(CodexModeAuto),
		Route:                    CodexRouteWSSPhaseF,
		ArchiveRecoveryAvailable: true,
		OutputBytes:              9000,
		ChunkMinBytes:            1,
		RecentEditUncertainty:    true,
	})
	if got.Loosened || !got.ReadDelta || !got.RepeatedOutput || got.ChunkDedup {
		t.Fatalf("recent edit uncertainty should keep lossless reducers but demote chunk: %+v", got)
	}
	for _, mechanism := range []CodexMechanism{CodexMechanismReadDelta, CodexMechanismRepeatedOutput} {
		if actionForMechanism(got.Mechanisms, mechanism) != CodexPolicyAllow {
			t.Fatalf("%s should remain allowed: %+v", mechanism, got.Mechanisms)
		}
	}
	if actionForMechanism(got.Mechanisms, CodexMechanismChunkDedup) != CodexPolicyFullPass {
		t.Fatalf("chunk dedup should full-pass on edit uncertainty: %+v", got.Mechanisms)
	}
}

func TestDecideCodexToolOutputChunkIntegrityBudgetOnlyDemotesChunk(t *testing.T) {
	t.Parallel()
	got := DecideCodexToolOutput(CodexToolOutputInput{
		Mode:                     string(CodexModeAuto),
		Route:                    CodexRouteWSSPhaseF,
		ArchiveRecoveryAvailable: true,
		OutputBytes:              9000,
		ChunkMinBytes:            1,
		ChunkIntegrityBudgetHit:  true,
	})
	if got.Loosened || !got.ReadDelta || !got.RepeatedOutput || got.ChunkDedup {
		t.Fatalf("chunk integrity budget should keep lossless reducers but demote chunk: %+v", got)
	}
	if actionForMechanism(got.Mechanisms, CodexMechanismReadDelta) != CodexPolicyAllow ||
		actionForMechanism(got.Mechanisms, CodexMechanismRepeatedOutput) != CodexPolicyAllow ||
		actionForMechanism(got.Mechanisms, CodexMechanismChunkDedup) != CodexPolicyFullPass {
		t.Fatalf("chunk integrity budget mechanism actions mismatch: %+v", got.Mechanisms)
	}
}

func TestDecideCodexToolOutputIncludesShadowTelemetryForFutureCandidates(t *testing.T) {
	t.Parallel()
	got := DecideCodexToolOutput(CodexToolOutputInput{
		Mode: string(CodexModeAuto), Route: CodexRouteWSSPhaseF,
		IsRead: true, ArchiveRecoveryAvailable: true, OutputBytes: 9000, ChunkMinBytes: 1,
	})
	if got.ChunkDedup != true || len(got.Mechanisms) == 0 {
		t.Fatalf("expected chunk dedup plus mechanism telemetry: %+v", got)
	}
	if actionForMechanism(got.Mechanisms, CodexMechanismFirstReadElision) != CodexPolicyShadow {
		t.Fatalf("first-read elision must stay shadow-only: %+v", got.Mechanisms)
	}
	if actionForMechanism(got.Mechanisms, CodexMechanismServerStateMirror) != CodexPolicyShadow {
		t.Fatalf("server-state mirror must stay shadow-only: %+v", got.Mechanisms)
	}
}

func TestDecideCodexToolOutputHTTPBlocksArchiveRefs(t *testing.T) {
	t.Parallel()
	got := DecideCodexToolOutput(CodexToolOutputInput{
		Mode: string(CodexModeMax), Route: CodexRouteHTTP, ArchiveRecoveryAvailable: true,
		ExplicitChunkDedup: true, OutputBytes: 9000, ChunkMinBytes: 1,
	})
	if got.ChunkDedup || got.NeedsRecoveryNote {
		t.Fatalf("HTTP must not emit archive-backed chunk refs: %+v", got)
	}
	if actionForMechanism(got.Mechanisms, CodexMechanismChunkDedup) != CodexPolicyBlock {
		t.Fatalf("HTTP chunk mechanism should be blocked: %+v", got.Mechanisms)
	}
}

func actionForMechanism(decisions []CodexMechanismDecision, mechanism CodexMechanism) CodexPolicyAction {
	for _, decision := range decisions {
		if decision.Mechanism == mechanism {
			return decision.Action
		}
	}
	return ""
}

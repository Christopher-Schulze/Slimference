// Package savingspolicy centralizes Codex savings decisions. Reducers stay
// mechanical; this package decides which mechanisms may run for a given
// workload and safety signal.
package savingspolicy

import "strings"

type CodexMode string

const (
	CodexModeOff          CodexMode = "off"
	CodexModeConservative CodexMode = "conservative"
	CodexModeAuto         CodexMode = "auto"
	CodexModeMax          CodexMode = "max"
)

type CodexRoute string

const (
	CodexRouteUnknown   CodexRoute = ""
	CodexRouteHTTP      CodexRoute = "http"
	CodexRouteWSSPhaseF CodexRoute = "wss_phasef"
)

type CodexClient string

const (
	CodexClientUnknown CodexClient = ""
	CodexClientCLI     CodexClient = "cli"
	CodexClientDesktop CodexClient = "desktop"
)

type CodexWorkload string

const (
	CodexWorkloadUnknown CodexWorkload = ""
	CodexWorkloadRead    CodexWorkload = "read"
	CodexWorkloadSearch  CodexWorkload = "search"
	CodexWorkloadCommand CodexWorkload = "command"
)

type CodexRisk string

const (
	CodexRiskLossless       CodexRisk = "lossless"
	CodexRiskRecoverable    CodexRisk = "recoverable"
	CodexRiskReconstructive CodexRisk = "reconstructive"
	CodexRiskSemantic       CodexRisk = "semantic"
)

type CodexRecovery string

const (
	CodexRecoveryNone    CodexRecovery = "none"
	CodexRecoveryExact   CodexRecovery = "exact"
	CodexRecoveryArchive CodexRecovery = "archive"
)

type CodexProof string

const (
	CodexProofNone   CodexProof = "none"
	CodexProofUnit   CodexProof = "unit"
	CodexProofReplay CodexProof = "replay"
	CodexProofLive   CodexProof = "live"
)

type CodexMechanism string

const (
	CodexMechanismReadDelta          CodexMechanism = "read_delta"
	CodexMechanismRepeatedOutput     CodexMechanism = "repeated_output"
	CodexMechanismRangedRead         CodexMechanism = "ranged_read"
	CodexMechanismSearchDelta        CodexMechanism = "search_delta"
	CodexMechanismChunkDedup         CodexMechanism = "chunk_dedup"
	CodexMechanismFirstReadElision   CodexMechanism = "first_read_elision"
	CodexMechanismServerStateMirror  CodexMechanism = "server_state_mirror"
	CodexMechanismPredictivePostEdit CodexMechanism = "predictive_post_edit"
	CodexMechanismPatchContextDedup  CodexMechanism = "patch_context_dedup"
	CodexMechanismReasoningCompact   CodexMechanism = "reasoning_compaction"
)

type CodexPolicyAction string

const (
	CodexPolicyAllow    CodexPolicyAction = "allow"
	CodexPolicyShadow   CodexPolicyAction = "shadow"
	CodexPolicyFullPass CodexPolicyAction = "full_pass"
	CodexPolicyBlock    CodexPolicyAction = "block"
)

type CodexMechanismInput struct {
	Mode                      string
	Route                     CodexRoute
	Client                    CodexClient
	Workload                  CodexWorkload
	Mechanism                 CodexMechanism
	Risk                      CodexRisk
	Recovery                  CodexRecovery
	Proof                     CodexProof
	ArchiveRecoveryAvailable  bool
	Explicit                  bool
	OutputBytes               int
	MinBytes                  int
	RecentlyEdited            bool
	PostCollapseReRead        bool
	RecentEditUncertainty     bool
	SessionIntegrityBudgetHit bool
	QualitySpike              bool
	ArchiveRecoveryLoop       bool
	MissingToolRetry          bool
	DegradedRoute             bool
	HostBudgetExceeded        bool
	LatencyBudgetExceeded     bool
	NegativeSavingsHistory    bool
}

type CodexMechanismDecision struct {
	Mechanism         CodexMechanism    `json:"mechanism"`
	Action            CodexPolicyAction `json:"action"`
	NeedsRecoveryNote bool              `json:"needs_recovery_note,omitempty"`
	Reason            string            `json:"reason"`
	BlockReason       string            `json:"block_reason,omitempty"`
}

type CodexToolOutputInput struct {
	Mode                     string
	Route                    CodexRoute
	Client                   CodexClient
	Workload                 CodexWorkload
	ArchiveRecoveryAvailable bool
	ExplicitChunkDedup       bool
	ChunkProof               CodexProof
	OutputBytes              int
	ChunkMinBytes            int
	IsRead                   bool
	RecentlyEdited           bool
	PostCollapseReRead       bool
	RecentEditUncertainty    bool
	QualitySpike             bool
	ArchiveRecoveryLoop      bool
	MissingToolRetry         bool
	DegradedRoute            bool
	HostBudgetExceeded       bool
	LatencyBudgetExceeded    bool
	NegativeSavingsHistory   bool
	ChunkIntegrityBudgetHit  bool
}

type CodexToolOutputDecision struct {
	ReadDelta         bool
	RepeatedOutput    bool
	ChunkDedup        bool
	NeedsRecoveryNote bool
	Loosened          bool
	Reason            string
	EffectiveMode     CodexMode
	Mechanisms        []CodexMechanismDecision
}

func ValidCodexMode(mode string) bool {
	switch NormalizeCodexMode(mode) {
	case CodexModeOff, CodexModeConservative, CodexModeAuto, CodexModeMax:
		return true
	default:
		return false
	}
}

func NormalizeCodexMode(mode string) CodexMode {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", string(CodexModeAuto):
		return CodexModeAuto
	case string(CodexModeOff), "disabled", "none":
		return CodexModeOff
	case string(CodexModeConservative), "safe":
		return CodexModeConservative
	case string(CodexModeMax), "aggressive":
		return CodexModeMax
	default:
		return CodexMode("")
	}
}

func ValidCodexProof(proof string) bool {
	return NormalizeCodexProof(proof) != ""
}

func NormalizeCodexProof(proof string) CodexProof {
	switch strings.ToLower(strings.TrimSpace(proof)) {
	case "", string(CodexProofNone), "off", "disabled":
		return CodexProofNone
	case string(CodexProofUnit):
		return CodexProofUnit
	case string(CodexProofReplay):
		return CodexProofReplay
	case string(CodexProofLive):
		return CodexProofLive
	default:
		return CodexProof("")
	}
}

func DecideCodexToolOutput(in CodexToolOutputInput) CodexToolOutputDecision {
	mode := NormalizeCodexMode(in.Mode)
	if mode == "" {
		mode = CodexModeConservative
	}
	decision := CodexToolOutputDecision{
		EffectiveMode:  mode,
		ReadDelta:      true,
		RepeatedOutput: true,
		Reason:         "safe_lossless_reducers",
	}
	if mode == CodexModeOff {
		return CodexToolOutputDecision{
			EffectiveMode: mode,
			Reason:        "policy_off",
			Mechanisms:    toolOutputMechanismDecisions(in, mode),
		}
	}
	if reason, ok := toolOutputLoosenReason(in); ok {
		decision.Loosened = true
		decision.ReadDelta = false
		decision.RepeatedOutput = false
		decision.ChunkDedup = false
		decision.Reason = reason
		decision.Mechanisms = toolOutputMechanismDecisions(in, mode)
		return decision
	}
	if mode == CodexModeConservative {
		chunk := DecideCodexMechanism(chunkMechanismInput(in, mode))
		decision.ChunkDedup = chunk.Action == CodexPolicyAllow
		if decision.ChunkDedup {
			decision.NeedsRecoveryNote = true
			decision.Reason = "explicit_chunk_dedup"
		}
		decision.Mechanisms = toolOutputMechanismDecisions(in, mode)
		return decision
	}
	chunk := DecideCodexMechanism(chunkMechanismInput(in, mode))
	if chunk.Action == CodexPolicyAllow {
		decision.ChunkDedup = true
		decision.NeedsRecoveryNote = true
		decision.Reason = "auto_recoverable_chunk_dedup"
	}
	if mode == CodexModeMax && decision.ChunkDedup {
		decision.Reason = "max_recoverable_chunk_dedup"
	}
	decision.Mechanisms = toolOutputMechanismDecisions(in, mode)
	return decision
}

func DecideCodexMechanism(in CodexMechanismInput) CodexMechanismDecision {
	mode := NormalizeCodexMode(in.Mode)
	if mode == "" {
		mode = CodexModeConservative
	}
	base := CodexMechanismDecision{Mechanism: in.Mechanism}
	if mode == CodexModeOff {
		return block(base, "policy_off")
	}
	if in.RecentlyEdited {
		return fullPass(base, "recent_edit_full_context")
	}
	if in.PostCollapseReRead {
		return fullPass(base, "post_collapse_reread_full_context")
	}
	if in.SessionIntegrityBudgetHit {
		if in.Risk == CodexRiskLossless && (in.Recovery == CodexRecoveryExact || in.Recovery == CodexRecoveryNone) {
			return allow(base, "lossless_or_exact_reducer_integrity_budget", false)
		}
		return fullPass(base, "session_integrity_budget")
	}
	if in.HostBudgetExceeded && in.Risk == CodexRiskLossless && (in.Recovery == CodexRecoveryExact || in.Recovery == CodexRecoveryNone) {
		return allow(base, "lossless_or_exact_reducer_host_budget", false)
	}
	if in.LatencyBudgetExceeded && in.Risk == CodexRiskLossless && (in.Recovery == CodexRecoveryExact || in.Recovery == CodexRecoveryNone) {
		return allow(base, "lossless_or_exact_reducer_latency_budget", false)
	}
	if in.NegativeSavingsHistory && in.Risk == CodexRiskLossless && (in.Recovery == CodexRecoveryExact || in.Recovery == CodexRecoveryNone) {
		return allow(base, "lossless_or_exact_reducer_negative_savings", false)
	}
	if reason, ok := mechanismDemotionReason(in); ok {
		return fullPass(base, reason)
	}
	if isFutureHighRisk(in.Mechanism) {
		return shadow(base, "capture_or_ab_proof_required")
	}
	if in.Risk == CodexRiskLossless && (in.Recovery == CodexRecoveryExact || in.Recovery == CodexRecoveryNone) {
		return allow(base, "lossless_or_exact_reducer", false)
	}
	if in.Mechanism == CodexMechanismChunkDedup {
		return decideChunkDedup(base, in, mode)
	}
	if in.Risk == CodexRiskRecoverable && in.Recovery == CodexRecoveryArchive {
		if in.Route == CodexRouteHTTP {
			return block(base, "http_archive_recovery_unproven")
		}
		if !in.ArchiveRecoveryAvailable {
			return block(base, "archive_recovery_unavailable")
		}
		if mode == CodexModeConservative && !in.Explicit {
			return block(base, "conservative_requires_explicit_recovery")
		}
		if mode == CodexModeAuto && !in.Explicit && !proofAtLeast(in.Proof, CodexProofLive) {
			return shadow(base, "live_proof_required")
		}
		if mode == CodexModeMax && !in.Explicit && !proofAtLeast(in.Proof, CodexProofReplay) {
			return shadow(base, "replay_or_live_proof_required")
		}
		return allow(base, "recoverable_with_archive", true)
	}
	if in.Risk == CodexRiskReconstructive || in.Risk == CodexRiskSemantic {
		return shadow(base, "drawdown_proof_required")
	}
	return block(base, "unsupported_policy_shape")
}

func chunkMechanismInput(in CodexToolOutputInput, mode CodexMode) CodexMechanismInput {
	return CodexMechanismInput{
		Mode:                      string(mode),
		Route:                     in.Route,
		Client:                    in.Client,
		Workload:                  in.Workload,
		Mechanism:                 CodexMechanismChunkDedup,
		Risk:                      CodexRiskRecoverable,
		Recovery:                  CodexRecoveryArchive,
		Proof:                     NormalizeCodexProof(string(in.ChunkProof)),
		ArchiveRecoveryAvailable:  in.ArchiveRecoveryAvailable,
		Explicit:                  in.ExplicitChunkDedup,
		OutputBytes:               in.OutputBytes,
		MinBytes:                  in.ChunkMinBytes,
		RecentlyEdited:            in.RecentlyEdited,
		PostCollapseReRead:        in.PostCollapseReRead,
		RecentEditUncertainty:     in.RecentEditUncertainty,
		QualitySpike:              in.QualitySpike,
		ArchiveRecoveryLoop:       in.ArchiveRecoveryLoop,
		MissingToolRetry:          in.MissingToolRetry,
		DegradedRoute:             in.DegradedRoute,
		HostBudgetExceeded:        in.HostBudgetExceeded,
		LatencyBudgetExceeded:     in.LatencyBudgetExceeded,
		NegativeSavingsHistory:    in.NegativeSavingsHistory,
		SessionIntegrityBudgetHit: in.ChunkIntegrityBudgetHit,
	}
}

func decideChunkDedup(base CodexMechanismDecision, in CodexMechanismInput, mode CodexMode) CodexMechanismDecision {
	if in.Route == CodexRouteHTTP {
		return block(base, "http_archive_recovery_unproven")
	}
	if !in.ArchiveRecoveryAvailable {
		return block(base, "archive_recovery_unavailable")
	}
	if !bytesCandidate(in.OutputBytes, in.MinBytes) {
		return block(base, "below_min_bytes")
	}
	if in.RecentEditUncertainty {
		return fullPass(base, "recent_edit_uncertain_chunk_full_context")
	}
	if mode == CodexModeConservative && !in.Explicit {
		return block(base, "conservative_requires_explicit_recovery")
	}
	if mode == CodexModeAuto && !in.Explicit && !proofAtLeast(in.Proof, CodexProofLive) {
		return shadow(base, "live_proof_required")
	}
	if mode == CodexModeMax && !in.Explicit && !proofAtLeast(in.Proof, CodexProofReplay) {
		return shadow(base, "replay_or_live_proof_required")
	}
	return allow(base, "recoverable_chunk_dedup", true)
}

func toolOutputMechanismDecisions(in CodexToolOutputInput, mode CodexMode) []CodexMechanismDecision {
	workload := in.Workload
	if workload == CodexWorkloadUnknown {
		if in.IsRead {
			workload = CodexWorkloadRead
		} else {
			workload = CodexWorkloadCommand
		}
	}
	common := CodexMechanismInput{
		Mode:                      string(mode),
		Route:                     in.Route,
		Client:                    in.Client,
		Workload:                  workload,
		ArchiveRecoveryAvailable:  in.ArchiveRecoveryAvailable,
		OutputBytes:               in.OutputBytes,
		MinBytes:                  in.ChunkMinBytes,
		RecentlyEdited:            in.RecentlyEdited,
		PostCollapseReRead:        in.PostCollapseReRead,
		RecentEditUncertainty:     in.RecentEditUncertainty,
		QualitySpike:              in.QualitySpike,
		ArchiveRecoveryLoop:       in.ArchiveRecoveryLoop,
		MissingToolRetry:          in.MissingToolRetry,
		DegradedRoute:             in.DegradedRoute,
		HostBudgetExceeded:        in.HostBudgetExceeded,
		LatencyBudgetExceeded:     in.LatencyBudgetExceeded,
		NegativeSavingsHistory:    in.NegativeSavingsHistory,
		SessionIntegrityBudgetHit: in.ChunkIntegrityBudgetHit,
	}
	decisions := []CodexMechanismDecision{
		DecideCodexMechanism(withMechanism(common, CodexMechanismReadDelta, CodexRiskLossless, CodexRecoveryExact, CodexProofLive, false)),
		DecideCodexMechanism(withMechanism(common, CodexMechanismRepeatedOutput, CodexRiskLossless, CodexRecoveryExact, CodexProofLive, false)),
	}
	if in.IsRead {
		decisions = append(decisions,
			DecideCodexMechanism(withMechanism(common, CodexMechanismRangedRead, CodexRiskLossless, CodexRecoveryExact, CodexProofLive, false)),
			DecideCodexMechanism(withMechanism(common, CodexMechanismFirstReadElision, CodexRiskReconstructive, CodexRecoveryArchive, CodexProofReplay, false)),
		)
	} else if workload == CodexWorkloadSearch {
		decisions = append(decisions, DecideCodexMechanism(withMechanism(common, CodexMechanismSearchDelta, CodexRiskLossless, CodexRecoveryExact, CodexProofReplay, false)))
	}
	decisions = append(decisions, DecideCodexMechanism(withMechanism(common, CodexMechanismChunkDedup, CodexRiskRecoverable, CodexRecoveryArchive, in.ChunkProof, in.ExplicitChunkDedup)))
	for _, mechanism := range []CodexMechanism{
		CodexMechanismServerStateMirror,
		CodexMechanismPredictivePostEdit,
		CodexMechanismPatchContextDedup,
		CodexMechanismReasoningCompact,
	} {
		decisions = append(decisions, DecideCodexMechanism(withMechanism(common, mechanism, CodexRiskReconstructive, CodexRecoveryArchive, CodexProofNone, false)))
	}
	return decisions
}

func proofAtLeast(got, want CodexProof) bool {
	return proofRank(NormalizeCodexProof(string(got))) >= proofRank(want)
}

func proofRank(proof CodexProof) int {
	switch proof {
	case CodexProofUnit:
		return 1
	case CodexProofReplay:
		return 2
	case CodexProofLive:
		return 3
	default:
		return 0
	}
}

func withMechanism(in CodexMechanismInput, mechanism CodexMechanism, risk CodexRisk, recovery CodexRecovery, proof CodexProof, explicit bool) CodexMechanismInput {
	in.Mechanism = mechanism
	in.Risk = risk
	in.Recovery = recovery
	in.Proof = proof
	in.Explicit = explicit
	return in
}

func isFutureHighRisk(mechanism CodexMechanism) bool {
	switch mechanism {
	case CodexMechanismFirstReadElision, CodexMechanismServerStateMirror,
		CodexMechanismPredictivePostEdit, CodexMechanismPatchContextDedup,
		CodexMechanismReasoningCompact:
		return true
	default:
		return false
	}
}

func toolOutputLoosenReason(in CodexToolOutputInput) (string, bool) {
	switch {
	case in.RecentlyEdited:
		return "recent_edit_full_context", true
	case in.PostCollapseReRead:
		return "post_collapse_reread_full_context", true
	case in.QualitySpike:
		return "quality_signal_full_context", true
	case in.ArchiveRecoveryLoop:
		return "archive_recovery_loop_full_context", true
	case in.MissingToolRetry:
		return "missing_tool_retry_full_context", true
	case in.DegradedRoute:
		return "degraded_route_full_context", true
	default:
		return "", false
	}
}

func mechanismDemotionReason(in CodexMechanismInput) (string, bool) {
	switch {
	case in.QualitySpike:
		return "quality_signal_full_context", true
	case in.ArchiveRecoveryLoop:
		return "archive_recovery_loop_full_context", true
	case in.MissingToolRetry:
		return "missing_tool_retry_full_context", true
	case in.DegradedRoute:
		return "degraded_route_full_context", true
	case in.HostBudgetExceeded:
		return "host_budget_full_context", true
	case in.LatencyBudgetExceeded:
		return "latency_budget_full_context", true
	case in.NegativeSavingsHistory:
		return "negative_savings_full_context", true
	default:
		return "", false
	}
}

func bytesCandidate(outputBytes, minBytes int) bool {
	if minBytes < 0 {
		minBytes = 0
	}
	return outputBytes >= minBytes
}

func allow(decision CodexMechanismDecision, reason string, note bool) CodexMechanismDecision {
	decision.Action = CodexPolicyAllow
	decision.Reason = reason
	decision.NeedsRecoveryNote = note
	return decision
}

func block(decision CodexMechanismDecision, reason string) CodexMechanismDecision {
	decision.Action = CodexPolicyBlock
	decision.Reason = reason
	decision.BlockReason = reason
	return decision
}

func shadow(decision CodexMechanismDecision, reason string) CodexMechanismDecision {
	decision.Action = CodexPolicyShadow
	decision.Reason = reason
	decision.BlockReason = reason
	return decision
}

func fullPass(decision CodexMechanismDecision, reason string) CodexMechanismDecision {
	decision.Action = CodexPolicyFullPass
	decision.Reason = reason
	return decision
}

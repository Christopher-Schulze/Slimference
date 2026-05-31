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

type CodexToolOutputInput struct {
	Mode                     string
	ArchiveRecoveryAvailable bool
	ExplicitChunkDedup       bool
	OutputBytes              int
	ChunkMinBytes            int
	IsRead                   bool
	RecentlyEdited           bool
	PostCollapseReRead       bool
}

type CodexToolOutputDecision struct {
	ReadDelta         bool
	RepeatedOutput    bool
	ChunkDedup        bool
	ScanRead          bool
	NeedsRecoveryNote bool
	Loosened          bool
	Reason            string
	EffectiveMode     CodexMode
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
		return CodexToolOutputDecision{EffectiveMode: mode, Reason: "policy_off"}
	}
	if in.RecentlyEdited || in.PostCollapseReRead {
		decision.Loosened = true
		decision.ChunkDedup = false
		if in.RecentlyEdited {
			decision.Reason = "recent_edit_full_context"
		} else {
			decision.Reason = "post_collapse_reread_full_context"
		}
		return decision
	}
	if mode == CodexModeConservative {
		decision.ChunkDedup = in.ExplicitChunkDedup && in.ArchiveRecoveryAvailable && chunkCandidate(in)
		if decision.ChunkDedup {
			decision.NeedsRecoveryNote = true
			decision.Reason = "explicit_chunk_dedup"
		}
		return decision
	}
	if in.ExplicitChunkDedup || (in.ArchiveRecoveryAvailable && chunkCandidate(in)) {
		decision.ChunkDedup = true
		decision.NeedsRecoveryNote = true
		decision.Reason = "auto_recoverable_chunk_dedup"
	}
	if mode == CodexModeMax && in.ArchiveRecoveryAvailable && in.OutputBytes > 0 {
		decision.ChunkDedup = true
		decision.NeedsRecoveryNote = true
		decision.Reason = "max_recoverable_chunk_dedup"
	}
	// First-read scan-mode (lossy: bodies elided to signatures) only in max mode,
	// only on reads, and only when archive recovery is available. The Loosened
	// early-return above guarantees a recent-edit or post-collapse re-read never
	// reaches here, so a re-read full-passes and recovers the elided bodies.
	// NOT in auto: scan-compacting first reads cannibalizes the lossless
	// read-delta/chunk-dedup seeding (repeat reads would full-pass via recovery
	// instead of deduping), which is net-negative on repeat-read workloads. Auto
	// promotion is gated on a scan<->lossless interaction design, not just the
	// re-read economics. When scan does run (max, or via the self-regulation
	// call site), per-session self-regulation suppresses it once the measured
	// re-read rate would make it net-negative.
	if mode == CodexModeMax && in.IsRead && in.ArchiveRecoveryAvailable {
		decision.ScanRead = true
		decision.NeedsRecoveryNote = true
		decision.Reason = "max_recoverable_scan_read"
	}
	return decision
}

func chunkCandidate(in CodexToolOutputInput) bool {
	minBytes := in.ChunkMinBytes
	if minBytes < 0 {
		minBytes = 0
	}
	return in.OutputBytes >= minBytes
}

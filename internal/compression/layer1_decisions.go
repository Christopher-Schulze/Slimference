package compression

// Layer1DecisionRecord is content-free telemetry for one Layer 1 sub-layer in
// one compression call. It records the safety contract and the observed token
// effect without copying prompt, tool, or archive payloads.
type Layer1DecisionRecord struct {
	SubLayer        string           `json:"sub_layer"`
	Tier            Layer1SafetyTier `json:"tier"`
	Attempted       bool             `json:"attempted"`
	Applied         bool             `json:"applied"`
	Reason          string           `json:"reason"`
	SavedTokens     int              `json:"saved_tokens,omitempty"`
	RequiresArchive bool             `json:"requires_archive,omitempty"`
	ArchiveWrites   int              `json:"archive_writes,omitempty"`
	Recovery        string           `json:"recovery,omitempty"`
	DefaultEligible bool             `json:"default_eligible"`
}

// BuildLayer1DecisionRecords turns aggregate Layer1Result counters into a
// stable per-sub-layer audit trail. It separates registered-but-unseen
// sub-layers from sub-layers that were actually reached by the reducer, so
// proofs do not overstate which mechanisms a workload exercised.
func BuildLayer1DecisionRecords(result Layer1Result) []Layer1DecisionRecord {
	registry := Layer1SubLayerRegistry()
	out := make([]Layer1DecisionRecord, 0, len(registry))
	for _, info := range registry {
		saved := layer1SavedForSubLayer(result, info.ID)
		attempted := layer1Attempted(result, info.ID, saved)
		record := Layer1DecisionRecord{
			SubLayer:        info.ID,
			Tier:            info.Tier,
			Attempted:       attempted,
			Applied:         saved > 0,
			Reason:          layer1DecisionReason(info, attempted, saved),
			SavedTokens:     saved,
			RequiresArchive: info.RequiresArchive,
			ArchiveWrites:   result.ArchiveWrites[info.ID],
			Recovery:        info.Recovery,
			DefaultEligible: info.DefaultEligible,
		}
		out = append(out, record)
	}
	return out
}

func layer1Attempted(result Layer1Result, id string, saved int) bool {
	if result.Attempts[id] > 0 {
		return true
	}
	if saved > 0 {
		return true
	}
	return result.ArchiveWrites[id] > 0
}

func layer1DecisionReason(info Layer1SubLayerInfo, attempted bool, saved int) string {
	if !attempted {
		return "not_attempted"
	}
	if saved > 0 {
		return "applied_positive_savings"
	}
	if info.RequiresArchive {
		return "full_pass_or_archive_unavailable"
	}
	if !info.DefaultEligible {
		return "not_default_eligible"
	}
	return "not_applicable_or_no_positive_savings"
}

func layer1SavedForSubLayer(result Layer1Result, id string) int {
	switch id {
	case "ansi_strip":
		return result.ANSISaved
	case "json_compact":
		return result.JSONSaved
	case "semantic_dictionary":
		return result.DictionarySaved
	case "dedup":
		saved := result.DedupSaved - result.NearDedupSaved
		if saved < 0 {
			return 0
		}
		return saved
	case "dedup_near":
		return result.NearDedupSaved
	case "delta":
		return result.DeltaSaved
	case "comment_strip":
		return result.CommentSaved
	case "structure_extract":
		return result.StructureSaved
	case "tool_compressor":
		saved := result.ToolCompressorSaved - result.ToolOutputInWindowSaved
		if saved < 0 {
			return 0
		}
		return saved
	case "success_short_circuit":
		return result.SuccessShortSaved
	case "image_replace":
		return result.ImageSaved
	case "repeated_collapse":
		return result.RepeatedCollapseSaved
	case "graph_pruning":
		return result.GraphPruningSaved
	case "preview_pass":
		return result.PreviewSaved
	case "tool_output_in_window":
		return result.ToolOutputInWindowSaved
	case "loop_nudge":
		return result.LoopNudgeSaved
	default:
		return 0
	}
}

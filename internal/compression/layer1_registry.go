package compression

// Layer1SafetyTier describes the product safety contract for a Layer 1 sub-layer.
type Layer1SafetyTier string

const (
	Layer1SafetyExact                  Layer1SafetyTier = "exact"
	Layer1SafetyReversible             Layer1SafetyTier = "reversible"
	Layer1SafetyRecoverableWithArchive Layer1SafetyTier = "recoverable_with_archive"
	Layer1SafetyTaskPreservingSummary  Layer1SafetyTier = "task_preserving_summary"
	Layer1SafetyNonDefault             Layer1SafetyTier = "non_default"
)

// Layer1SubLayerInfo is metadata only: no payload, no functions, no archive ids.
type Layer1SubLayerInfo struct {
	ID              string
	Tier            Layer1SafetyTier
	DefaultEligible bool
	RequiresArchive bool
	ModelRisk       string
	Recovery        string
}

var layer1SubLayerRegistry = []Layer1SubLayerInfo{
	{
		ID:              "ansi_strip",
		Tier:            Layer1SafetyExact,
		DefaultEligible: true,
		ModelRisk:       "terminal escape bytes only",
		Recovery:        "not needed",
	},
	{
		ID:              "json_compact",
		Tier:            Layer1SafetyExact,
		DefaultEligible: true,
		ModelRisk:       "none when JSON parse succeeds",
		Recovery:        "not needed",
	},
	{
		ID:              "semantic_dictionary",
		Tier:            Layer1SafetyReversible,
		DefaultEligible: true,
		ModelRisk:       "path aliases require reading the inline legend",
		Recovery:        "inline dictionary legend",
	},
	{
		ID:              "dedup",
		Tier:            Layer1SafetyReversible,
		DefaultEligible: true,
		ModelRisk:       "attention recency can drop for repeated content",
		Recovery:        "previous in-session full block plus archive attribution when active",
	},
	{
		ID:              "delta",
		Tier:            Layer1SafetyReversible,
		DefaultEligible: true,
		ModelRisk:       "requires previous version plus diff reconstruction",
		Recovery:        "previous in-session full block plus archive attribution when active",
	},
	{
		ID:              "comment_strip",
		Tier:            Layer1SafetyRecoverableWithArchive,
		DefaultEligible: true,
		RequiresArchive: true,
		ModelRisk:       "comments can carry constraints, intent, licenses, or safety notes",
		Recovery:        "content archive",
	},
	{
		ID:              "structure_extract",
		Tier:            Layer1SafetyRecoverableWithArchive,
		DefaultEligible: true,
		RequiresArchive: true,
		ModelRisk:       "function bodies and implementation details are elided",
		Recovery:        "content archive",
	},
	{
		ID:              "tool_compressor",
		Tier:            Layer1SafetyTaskPreservingSummary,
		DefaultEligible: true,
		RequiresArchive: true,
		ModelRisk:       "non-actionable lines may later become relevant",
		Recovery:        "content archive",
	},
	{
		ID:              "success_short_circuit",
		Tier:            Layer1SafetyTaskPreservingSummary,
		DefaultEligible: true,
		ModelRisk:       "success-only logs are summarized",
		Recovery:        "success classifier refuses failures and errors",
	},
	{
		ID:              "image_replace",
		Tier:            Layer1SafetyRecoverableWithArchive,
		DefaultEligible: true,
		RequiresArchive: true,
		ModelRisk:       "inline binary payload is replaced by a reference",
		Recovery:        "content archive",
	},
	{
		ID:              "repeated_collapse",
		Tier:            Layer1SafetyReversible,
		DefaultEligible: true,
		ModelRisk:       "attention recency can drop for repeated tool outputs",
		Recovery:        "previous in-session full block plus archive attribution when active",
	},
	{
		ID:              "graph_pruning",
		Tier:            Layer1SafetyRecoverableWithArchive,
		DefaultEligible: true,
		RequiresArchive: true,
		ModelRisk:       "older operation bodies can be pruned after superseding operations",
		Recovery:        "content archive",
	},
	{
		ID:              "preview_pass",
		Tier:            Layer1SafetyRecoverableWithArchive,
		DefaultEligible: true,
		RequiresArchive: true,
		ModelRisk:       "large tool result bodies are replaced by structural preview",
		Recovery:        "content archive",
	},
	{
		ID:              "tool_output_in_window",
		Tier:            Layer1SafetyRecoverableWithArchive,
		DefaultEligible: true,
		RequiresArchive: true,
		ModelRisk:       "large in-window tool output is summarized",
		Recovery:        "content archive",
	},
	{
		ID:              "loop_nudge",
		Tier:            Layer1SafetyTaskPreservingSummary,
		DefaultEligible: false,
		ModelRisk:       "adds guidance to break a retry loop",
		Recovery:        "config opt-in and quality policy",
	},
}

// Layer1SubLayerRegistry returns the Layer 1 safety contract in stable order.
func Layer1SubLayerRegistry() []Layer1SubLayerInfo {
	out := make([]Layer1SubLayerInfo, len(layer1SubLayerRegistry))
	copy(out, layer1SubLayerRegistry)
	return out
}

func layer1SubLayerByID(id string) (Layer1SubLayerInfo, bool) {
	for _, info := range layer1SubLayerRegistry {
		if info.ID == id {
			return info, true
		}
	}
	return Layer1SubLayerInfo{}, false
}

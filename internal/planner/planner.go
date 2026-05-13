package planner

import "strings"

type Layer string

const (
	Layer0         Layer = "l0"
	Layer1         Layer = "l1"
	Layer2         Layer = "l2"
	Layer3         Layer = "l3"
	Layer4         Layer = "l4_output"
	LayerWebSocket Layer = "websocket"
)

type Action string

const (
	ActionRun       Action = "run"
	ActionCheapOnly Action = "cheap_only"
	ActionBypass    Action = "bypass"
	ActionInspect   Action = "inspect"
	ActionShadow    Action = "shadow"
	ActionMutate    Action = "mutate"
	ActionTunnel    Action = "tunnel"
)

type RequestFacts struct {
	Provider                    string
	Model                       string
	RouteMode                   string
	EstimatedInputTokens        int
	ExpectedOutputTokens        int
	TaskShape                   string
	ContentClasses              []string
	ManualDisabled              map[Layer]bool
	RecentEdit                  bool
	ExternalLayer2Allowed       bool
	Layer2Acknowledged          bool
	ProviderCacheSupported      bool
	PreviousResponseIDAvailable bool
	OutputReduceCooldown        bool
	NegativeSavingsHistory      bool
	WebSocketShapeKnown         bool
	WebSocketMutationRequested  bool
	LiveCorpusConfidence        string
	LatencyBudgetMs             int
}

type LayerDecision struct {
	Layer                 Layer  `json:"layer"`
	Action                Action `json:"action"`
	Reason                string `json:"reason"`
	ExpectedSavingsTokens int    `json:"expected_savings_tokens,omitempty"`
	Risk                  string `json:"risk"`
	Confidence            string `json:"confidence"`
}

type CompressionPlan struct {
	Provider      string          `json:"provider,omitempty"`
	Model         string          `json:"model,omitempty"`
	RouteMode     string          `json:"route_mode,omitempty"`
	Decisions     []LayerDecision `json:"decisions"`
	SafetyBlocked bool            `json:"safety_blocked"`
}

func Plan(facts RequestFacts) CompressionPlan {
	normalized := normalizeFacts(facts)
	plan := CompressionPlan{
		Provider:  normalized.Provider,
		Model:     normalized.Model,
		RouteMode: normalized.RouteMode,
	}
	plan.Decisions = append(plan.Decisions,
		decideL0(normalized),
		decideL1(normalized),
		decideL2(normalized),
		decideL3(normalized),
		decideL4(normalized),
		decideWebSocket(normalized),
	)
	for _, decision := range plan.Decisions {
		if decision.Risk == "blocked" {
			plan.SafetyBlocked = true
			break
		}
	}
	return plan
}

func normalizeFacts(facts RequestFacts) RequestFacts {
	facts.Provider = strings.TrimSpace(facts.Provider)
	facts.Model = strings.TrimSpace(facts.Model)
	facts.RouteMode = strings.TrimSpace(facts.RouteMode)
	facts.TaskShape = strings.TrimSpace(facts.TaskShape)
	facts.LiveCorpusConfidence = strings.TrimSpace(facts.LiveCorpusConfidence)
	if facts.LiveCorpusConfidence == "" {
		facts.LiveCorpusConfidence = "unknown"
	}
	if facts.LatencyBudgetMs == 0 {
		facts.LatencyBudgetMs = 50
	}
	return facts
}

func decideL0(f RequestFacts) LayerDecision {
	if disabled(f, Layer0) {
		return decision(Layer0, ActionBypass, "operator_disabled", 0, "none", "high")
	}
	if f.EstimatedInputTokens < 200 && !hasClass(f, "tool_output") {
		return decision(Layer0, ActionBypass, "small_request", 0, "none", "high")
	}
	return decision(Layer0, ActionRun, "tool_or_medium_request", f.EstimatedInputTokens/10, "low", confidenceFromCorpus(f))
}

func decideL1(f RequestFacts) LayerDecision {
	if disabled(f, Layer1) {
		return decision(Layer1, ActionBypass, "operator_disabled", 0, "none", "high")
	}
	if f.NegativeSavingsHistory {
		return decision(Layer1, ActionBypass, "negative_savings_history", 0, "blocked", "high")
	}
	if f.RecentEdit {
		return decision(Layer1, ActionCheapOnly, "recent_edit_preserve_full_context", f.EstimatedInputTokens/30, "low", "medium")
	}
	if f.EstimatedInputTokens >= 1000 || hasAnyClass(f, "source_file", "log", "json", "markdown", "sql") {
		return decision(Layer1, ActionRun, "large_or_structured_input", f.EstimatedInputTokens/5, "medium", confidenceFromCorpus(f))
	}
	return decision(Layer1, ActionCheapOnly, "cheap_passes_only", f.EstimatedInputTokens/25, "low", "high")
}

func decideL2(f RequestFacts) LayerDecision {
	if disabled(f, Layer2) {
		return decision(Layer2, ActionBypass, "operator_disabled", 0, "none", "high")
	}
	if !f.ExternalLayer2Allowed || !f.Layer2Acknowledged {
		return decision(Layer2, ActionBypass, "external_summary_policy_not_ready", 0, "none", "high")
	}
	if f.RecentEdit {
		return decision(Layer2, ActionBypass, "recent_edit_window", 0, "medium", "high")
	}
	if f.EstimatedInputTokens >= 15000 {
		return decision(Layer2, ActionRun, "long_context_threshold", f.EstimatedInputTokens/3, "medium", confidenceFromCorpus(f))
	}
	if f.EstimatedInputTokens >= 7000 && hasClass(f, "repeated_tool_output") {
		return decision(Layer2, ActionShadow, "adaptive_roi_candidate", f.EstimatedInputTokens/4, "medium", "medium")
	}
	return decision(Layer2, ActionBypass, "below_roi_threshold", 0, "none", "high")
}

func decideL3(f RequestFacts) LayerDecision {
	if disabled(f, Layer3) {
		return decision(Layer3, ActionBypass, "operator_disabled", 0, "none", "high")
	}
	if !f.ProviderCacheSupported {
		return decision(Layer3, ActionBypass, "provider_cache_unsupported", 0, "none", "high")
	}
	if f.EstimatedInputTokens < 1000 && !f.PreviousResponseIDAvailable {
		return decision(Layer3, ActionBypass, "prefix_too_small", 0, "none", "high")
	}
	reason := "stable_prefix_cache_hint"
	if f.PreviousResponseIDAvailable {
		reason = "previous_response_state_available"
	}
	return decision(Layer3, ActionRun, reason, f.EstimatedInputTokens/2, "low", "provider_reported")
}

func decideL4(f RequestFacts) LayerDecision {
	if disabled(f, Layer4) {
		return decision(Layer4, ActionBypass, "operator_disabled", 0, "none", "high")
	}
	if f.OutputReduceCooldown {
		return decision(Layer4, ActionCheapOnly, "quality_cooldown_soften_profile", maxInt(f.ExpectedOutputTokens/10, 10), "medium", "high")
	}
	if f.ExpectedOutputTokens >= 200 || f.EstimatedInputTokens >= 1000 {
		return decision(Layer4, ActionRun, "output_tokens_or_task_size_justify_directive", maxInt(f.ExpectedOutputTokens/5, 20), "medium", confidenceFromCorpus(f))
	}
	return decision(Layer4, ActionBypass, "output_too_small", 0, "none", "high")
}

func decideWebSocket(f RequestFacts) LayerDecision {
	if disabled(f, LayerWebSocket) {
		return decision(LayerWebSocket, ActionTunnel, "operator_disabled", 0, "none", "high")
	}
	if !strings.Contains(strings.ToLower(f.RouteMode), "websocket") {
		return decision(LayerWebSocket, ActionBypass, "not_websocket_route", 0, "none", "high")
	}
	if !f.WebSocketMutationRequested {
		return decision(LayerWebSocket, ActionInspect, "inspect_only_default", 0, "low", "high")
	}
	if !f.WebSocketShapeKnown {
		return decision(LayerWebSocket, ActionInspect, "unknown_shape_blocks_mutation", 0, "blocked", "high")
	}
	if f.LiveCorpusConfidence != "high" {
		return decision(LayerWebSocket, ActionShadow, "mutation_requires_high_live_corpus_confidence", f.EstimatedInputTokens/4, "medium", "medium")
	}
	return decision(LayerWebSocket, ActionMutate, "known_shape_and_high_corpus_confidence", f.EstimatedInputTokens/3, "high", "high")
}

func decision(layer Layer, action Action, reason string, expected int, risk, confidence string) LayerDecision {
	if expected < 0 {
		expected = 0
	}
	return LayerDecision{
		Layer:                 layer,
		Action:                action,
		Reason:                reason,
		ExpectedSavingsTokens: expected,
		Risk:                  risk,
		Confidence:            confidence,
	}
}

func disabled(f RequestFacts, layer Layer) bool {
	return f.ManualDisabled != nil && f.ManualDisabled[layer]
}

func hasClass(f RequestFacts, class string) bool {
	for _, candidate := range f.ContentClasses {
		if strings.EqualFold(strings.TrimSpace(candidate), class) {
			return true
		}
	}
	return false
}

func hasAnyClass(f RequestFacts, classes ...string) bool {
	for _, class := range classes {
		if hasClass(f, class) {
			return true
		}
	}
	return false
}

func confidenceFromCorpus(f RequestFacts) string {
	switch f.LiveCorpusConfidence {
	case "high", "medium", "low":
		return f.LiveCorpusConfidence
	default:
		return "unknown"
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

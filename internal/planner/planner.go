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
	Layer2ModelFacingAllowed    bool
	ProviderCacheSupported      bool
	PreviousResponseIDAvailable bool
	ToolPruneCooldown           bool
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
	if isCodexWebSocketRoute(f) && f.EstimatedInputTokens >= 7000 {
		return decision(Layer2, ActionShadow, "codex_wss_l2_requires_fixture_live_proof", f.EstimatedInputTokens/4, "medium", confidenceFromCorpus(f))
	}
	if f.EstimatedInputTokens >= 15000 {
		if !f.Layer2ModelFacingAllowed {
			return decision(Layer2, ActionShadow, "context_ledger_shadow_summary_replacement_blocked", f.EstimatedInputTokens/4, "medium", confidenceFromCorpus(f))
		}
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
	if isCodexWebSocketRoute(f) && (f.PreviousResponseIDAvailable || f.EstimatedInputTokens >= 1000) {
		return decision(Layer3, ActionShadow, "codex_wss_l3_requires_fixture_live_proof", f.EstimatedInputTokens/2, "medium", "provider_reported")
	}
	if isCodexChatGPT(f) && !f.PreviousResponseIDAvailable {
		return decision(Layer3, ActionBypass, "codex_cache_accounting_only", 0, "none", "provider_reported")
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
	if d, guarded := decideL4ShapeGuard(f); guarded {
		return d
	}
	if f.OutputReduceCooldown || f.ToolPruneCooldown {
		return decision(Layer4, ActionCheapOnly, "quality_cooldown_soften_layer4", maxInt(f.ExpectedOutputTokens/10, 10), "medium", "high")
	}
	if f.ExpectedOutputTokens >= 200 || f.EstimatedInputTokens >= 1000 {
		return decision(Layer4, ActionRun, "output_tokens_or_task_size_justify_directive", maxInt(f.ExpectedOutputTokens/5, 20), "medium", confidenceFromCorpus(f))
	}
	return decision(Layer4, ActionBypass, "output_too_small", 0, "none", "high")
}

func decideL4ShapeGuard(f RequestFacts) (LayerDecision, bool) {
	shape := strings.ToLower(strings.TrimSpace(f.TaskShape))
	switch shape {
	case "exact_reply":
		return decision(Layer4, ActionBypass, "exact_reply", 0, "none", "high"), true
	case "command_output_relay":
		return decision(Layer4, ActionBypass, "command_output_relay_exact_output", 0, "none", "high"), true
	case "repair_followup":
		return decision(Layer4, ActionBypass, "repair_followup_low_roi", 0, "none", "high"), true
	case "read_only_analysis":
		return decision(Layer4, ActionBypass, "unproven_task_shape_ab_required", 0, "none", "high"), true
	case "planning":
		return decision(Layer4, ActionBypass, "unproven_task_shape_ab_required", 0, "none", "high"), true
	case "direct_answer":
		if f.EstimatedInputTokens > 0 && f.EstimatedInputTokens < 12000 {
			return decision(Layer4, ActionBypass, "direct_answer_low_roi", 0, "none", "high"), true
		}
	case "code_edit", "debugging", "explanation_deep_analysis", "review", "tool_result_reasoning", "new_file_generation", "final_summary":
		return decision(Layer4, ActionBypass, "unproven_task_shape_ab_required", 0, "none", "high"), true
	case "unknown":
		return decision(Layer4, ActionBypass, "unknown_task_shape", 0, "none", "high"), true
	}
	return LayerDecision{}, false
}

func decideWebSocket(f RequestFacts) LayerDecision {
	if disabled(f, LayerWebSocket) {
		return decision(LayerWebSocket, ActionTunnel, "operator_disabled", 0, "none", "high")
	}
	if !strings.Contains(strings.ToLower(f.RouteMode), "websocket") {
		if isCodexChatGPT(f) {
			return decision(LayerWebSocket, ActionBypass, "codex_cli_http_provider", 0, "none", "high")
		}
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

func isCodexChatGPT(f RequestFacts) bool {
	return strings.EqualFold(strings.TrimSpace(f.Provider), "codex_chatgpt")
}

func isCodexWebSocketRoute(f RequestFacts) bool {
	return isCodexChatGPT(f) && strings.Contains(strings.ToLower(f.RouteMode), "websocket")
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

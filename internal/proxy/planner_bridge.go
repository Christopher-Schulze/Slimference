package proxy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/planner"
	"github.com/slimference/slimference/internal/sessions"
	"github.com/slimference/slimference/internal/types"
)

type plannerInput struct {
	provider                    types.Provider
	model                       string
	routeMode                   string
	estimatedInputTokens        int
	expectedOutputTokens        int
	taskShape                   string
	contentClasses              []string
	recentEdit                  bool
	providerCacheSupported      bool
	previousResponseIDAvailable bool
	toolPruneCooldown           bool
	outputReduceCooldown        bool
	webSocketShapeKnown         bool
	webSocketMutationRequested  bool
	liveCorpusConfidence        string
	negativeSavingsHistory      bool
}

func (p *Proxy) dryRunPlan(in plannerInput) *dbg.PlanSummary {
	if p == nil {
		return nil
	}
	return debugPlanSummary(p.buildCompressionPlan(in))
}

func (p *Proxy) buildCompressionPlan(in plannerInput) planner.CompressionPlan {
	manualDisabled := map[planner.Layer]bool{
		planner.Layer1: !p.isLayerEnabled(1),
		planner.Layer2: !p.isLayerEnabled(2),
		planner.Layer3: !p.isLayerEnabled(3),
		planner.Layer4: !p.config.Compression.OutputReduce.Enabled,
	}
	caps := types.CapabilitiesFor(in.provider)
	providerCacheSupported := in.providerCacheSupported ||
		caps.SupportsCachedPrefix ||
		caps.SupportsPromptCacheUsage ||
		caps.SupportsPromptCacheKey ||
		caps.SupportsPromptCacheRetention
	return planner.Plan(planner.RequestFacts{
		Provider:                    in.provider.String(),
		Model:                       in.model,
		RouteMode:                   in.routeMode,
		EstimatedInputTokens:        in.estimatedInputTokens,
		ExpectedOutputTokens:        in.expectedOutputTokens,
		TaskShape:                   in.taskShape,
		ContentClasses:              normalizedPlannerClasses(in.contentClasses),
		ManualDisabled:              manualDisabled,
		RecentEdit:                  in.recentEdit,
		ExternalLayer2Allowed:       p.config.Compression.Layer2Enabled,
		Layer2Acknowledged:          p.config.Compression.Layer2Enabled,
		ProviderCacheSupported:      providerCacheSupported,
		PreviousResponseIDAvailable: in.previousResponseIDAvailable,
		ToolPruneCooldown:           in.toolPruneCooldown,
		OutputReduceCooldown:        in.outputReduceCooldown,
		NegativeSavingsHistory:      in.negativeSavingsHistory,
		WebSocketShapeKnown:         in.webSocketShapeKnown,
		WebSocketMutationRequested:  in.webSocketMutationRequested,
		LiveCorpusConfidence:        in.liveCorpusConfidence,
		LatencyBudgetMs:             p.config.Compression.Layer2LatencyBudgetMs,
	})
}

func (p *Proxy) plannerRecentEditFact(sessionID string, messages []types.Message) bool {
	if requestHasEditIntent(messages) {
		return true
	}
	return sessionHasRecentEditedFile(sessionID, 2)
}

func sessionHasRecentEditedFile(sessionID string, previousTurns int) bool {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false
	}
	home, err := proxyUserHomeDir()
	if err != nil {
		return false
	}
	state, err := sessions.LoadHookState(sessions.DefaultHookStateDir(home), sessionID)
	if err != nil {
		return false
	}
	start := len(state.Turns) - 1 - previousTurns
	if start < 0 {
		start = 0
	}
	for _, turn := range state.Turns[start:] {
		if len(turn.FilesEdited) > 0 {
			return true
		}
	}
	return false
}

func (p *Proxy) plannerLiveCorpusConfidence() string {
	if p == nil || p.config == nil {
		return "unknown"
	}
	tuning := p.config.Compression.Tuning
	if confidence := normalizePlannerConfidence(tuning.PlannerLiveCorpusConfidence); confidence != "" && confidence != "unknown" {
		return confidence
	}
	if confidence := liveCorpusConfidenceFromMetadataPath(tuning.PlannerLiveCorpusMetadataPath); confidence != "" {
		return confidence
	}
	if confidence := normalizePlannerConfidence(tuning.PlannerLiveCorpusConfidence); confidence != "" {
		return confidence
	}
	return "unknown"
}

func (p *Proxy) webSocketShapeKnown() bool {
	return p != nil && p.webSocketShapes != nil && p.webSocketShapes.Known()
}

func normalizePlannerConfidence(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "high", "medium", "low", "unknown":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func liveCorpusConfidenceFromMetadataPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		path = filepath.Join(path, "metadata.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var meta struct {
		Synthetic            bool   `json:"synthetic"`
		EvidenceLevel        string `json:"evidence_level"`
		ExpectedRequestCount int    `json:"expected_request_count"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return ""
	}
	evidence := strings.ToLower(strings.TrimSpace(meta.EvidenceLevel))
	switch {
	case meta.Synthetic:
		return "low"
	case evidence == "live_operator" || evidence == "real_operator":
		return "high"
	case strings.Contains(evidence, "live") || strings.Contains(evidence, "real"):
		return "medium"
	case meta.ExpectedRequestCount > 0:
		return "medium"
	default:
		return "unknown"
	}
}

func plannerDecisionForLayer(plan planner.CompressionPlan, layer planner.Layer) (planner.LayerDecision, bool) {
	for _, decision := range plan.Decisions {
		if decision.Layer == layer {
			return decision, true
		}
	}
	return planner.LayerDecision{}, false
}

func plannerActionForLayer(plan planner.CompressionPlan, layer planner.Layer, fallback planner.Action) planner.Action {
	decision, ok := plannerDecisionForLayer(plan, layer)
	if !ok {
		return fallback
	}
	return decision.Action
}

func plannerHardBypassForLayer2(plan planner.CompressionPlan) bool {
	decision, ok := plannerDecisionForLayer(plan, planner.Layer2)
	if !ok || decision.Action != planner.ActionBypass {
		return false
	}
	switch decision.Reason {
	case "operator_disabled", "external_summary_policy_not_ready", "recent_edit_window":
		return true
	default:
		return false
	}
}

func debugPlanSummary(plan planner.CompressionPlan) *dbg.PlanSummary {
	out := &dbg.PlanSummary{
		Provider:      plan.Provider,
		Model:         plan.Model,
		RouteMode:     plan.RouteMode,
		SafetyBlocked: plan.SafetyBlocked,
		Decisions:     make([]dbg.PlanDecisionSummary, 0, len(plan.Decisions)),
	}
	for _, d := range plan.Decisions {
		out.Decisions = append(out.Decisions, dbg.PlanDecisionSummary{
			Layer:                 string(d.Layer),
			Action:                string(d.Action),
			Reason:                d.Reason,
			ExpectedSavingsTokens: d.ExpectedSavingsTokens,
			Risk:                  d.Risk,
			Confidence:            d.Confidence,
		})
	}
	return out
}

func plannerClassesFromMessages(messages []types.Message) []string {
	classes := []string{"conversation"}
	seenTool := false
	seenSource := false
	seenStructured := false
	seenRepeatedToolOutput := false
	toolUses := proxyToolUseIndex(messages)
	repeatKeys := make(map[string]int)
	for _, msg := range messages {
		if msg.Role == "tool" || msg.HasToolResult() {
			seenTool = true
		}
		for _, block := range msg.Content {
			text := block.Text
			if block.Type == "tool_result" || block.ToolResultID != "" {
				seenTool = true
				if key := plannerToolOutputRepeatKey(block, toolUses); key != "" {
					repeatKeys[key]++
					if repeatKeys[key] > 1 {
						seenRepeatedToolOutput = true
					}
				}
			}
			if looksLikeSource(text) {
				seenSource = true
			}
			if looksStructured(text) {
				seenStructured = true
			}
		}
	}
	if seenTool {
		classes = append(classes, "tool_output")
	}
	if seenRepeatedToolOutput {
		classes = append(classes, "repeated_tool_output")
	}
	if seenSource {
		classes = append(classes, "source_file")
	}
	if seenStructured {
		classes = append(classes, "json")
	}
	return classes
}

func plannerToolOutputRepeatKey(block types.ContentBlock, toolUses map[string]types.ContentBlock) string {
	use, _ := proxyResolveToolUseDetailed(block, toolUses)
	commandLine := strings.TrimSpace(proxyLayer0CommandLine(use))
	if commandLine == "" {
		return ""
	}
	return "cmd:" + commandLine
}

func requestHasEditIntent(messages []types.Message) bool {
	for _, msg := range messages {
		for _, block := range msg.Content {
			name := strings.ToLower(strings.TrimSpace(block.ToolName))
			switch name {
			case "edit", "write", "apply_patch", "multiedit", "multi_edit", "update_plan":
				return true
			}
			input := strings.ToLower(block.ToolInput)
			if strings.Contains(input, "apply_patch") ||
				strings.Contains(input, "\"cmd\":\"write") ||
				strings.Contains(input, "\"command\":\"write") {
				return true
			}
		}
	}
	return false
}

func normalizedPlannerClasses(classes []string) []string {
	seen := make(map[string]struct{}, len(classes))
	out := make([]string, 0, len(classes))
	for _, class := range classes {
		class = strings.ToLower(strings.TrimSpace(class))
		if class == "" {
			continue
		}
		if _, ok := seen[class]; ok {
			continue
		}
		seen[class] = struct{}{}
		out = append(out, class)
	}
	return out
}

func looksLikeSource(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	return strings.Contains(lower, "package ") ||
		strings.Contains(lower, "func ") ||
		strings.Contains(lower, "function ") ||
		strings.Contains(lower, "class ") ||
		strings.Contains(lower, "def ") ||
		strings.Contains(lower, "#include") ||
		strings.Contains(lower, "import ")
}

func looksStructured(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[")
}

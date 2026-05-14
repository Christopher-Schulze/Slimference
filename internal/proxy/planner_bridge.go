package proxy

import (
	"strings"

	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/planner"
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
	plan := planner.Plan(planner.RequestFacts{
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
		OutputReduceCooldown:        in.outputReduceCooldown,
		NegativeSavingsHistory:      in.negativeSavingsHistory,
		WebSocketShapeKnown:         in.webSocketShapeKnown,
		WebSocketMutationRequested:  in.webSocketMutationRequested,
		LiveCorpusConfidence:        in.liveCorpusConfidence,
		LatencyBudgetMs:             p.config.Compression.Layer2LatencyBudgetMs,
	})
	return debugPlanSummary(plan)
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
	for _, msg := range messages {
		if msg.Role == "tool" || msg.HasToolResult() {
			seenTool = true
		}
		for _, block := range msg.Content {
			text := block.Text
			if block.Type == "tool_result" || block.ToolResultID != "" {
				seenTool = true
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
	if seenSource {
		classes = append(classes, "source_file")
	}
	if seenStructured {
		classes = append(classes, "json")
	}
	return classes
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

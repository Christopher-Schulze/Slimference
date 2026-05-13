package planner

import "testing"

func findDecision(t *testing.T, plan CompressionPlan, layer Layer) LayerDecision {
	t.Helper()
	for _, decision := range plan.Decisions {
		if decision.Layer == layer {
			return decision
		}
	}
	t.Fatalf("missing decision for %s in %+v", layer, plan)
	return LayerDecision{}
}

func TestPlan_SmallRequestBypassesHeavyWork(t *testing.T) {
	t.Parallel()
	plan := Plan(RequestFacts{Provider: "openai", RouteMode: "upstream", EstimatedInputTokens: 100, ExpectedOutputTokens: 50})
	if got := findDecision(t, plan, Layer0).Action; got != ActionBypass {
		t.Fatalf("L0 action=%s", got)
	}
	if got := findDecision(t, plan, Layer1).Action; got != ActionCheapOnly {
		t.Fatalf("L1 action=%s", got)
	}
	if got := findDecision(t, plan, Layer2).Action; got != ActionBypass {
		t.Fatalf("L2 action=%s", got)
	}
	if got := findDecision(t, plan, Layer4).Action; got != ActionBypass {
		t.Fatalf("L4 action=%s", got)
	}
	if plan.SafetyBlocked {
		t.Fatalf("small request should not be safety blocked")
	}
}

func TestPlan_ToolAndStructuredInputRunsL0L1(t *testing.T) {
	t.Parallel()
	plan := Plan(RequestFacts{
		EstimatedInputTokens: 5000,
		ContentClasses:       []string{" tool_output ", "source_file"},
		LiveCorpusConfidence: "high",
	})
	if d := findDecision(t, plan, Layer0); d.Action != ActionRun || d.Confidence != "high" {
		t.Fatalf("L0=%+v", d)
	}
	if d := findDecision(t, plan, Layer1); d.Action != ActionRun || d.ExpectedSavingsTokens == 0 {
		t.Fatalf("L1=%+v", d)
	}
}

func TestPlan_ManualDisableOverridesLayer(t *testing.T) {
	t.Parallel()
	plan := Plan(RequestFacts{
		EstimatedInputTokens: 20000,
		ManualDisabled:       map[Layer]bool{Layer0: true, Layer1: true, Layer2: true, Layer3: true, Layer4: true},
	})
	for _, layer := range []Layer{Layer0, Layer1, Layer2, Layer3, Layer4} {
		if d := findDecision(t, plan, layer); d.Action != ActionBypass || d.Reason != "operator_disabled" {
			t.Fatalf("%s=%+v", layer, d)
		}
	}
}

func TestPlan_L1SafetyGates(t *testing.T) {
	t.Parallel()
	blocked := Plan(RequestFacts{EstimatedInputTokens: 5000, NegativeSavingsHistory: true})
	if d := findDecision(t, blocked, Layer1); d.Action != ActionBypass || d.Risk != "blocked" {
		t.Fatalf("negative history L1=%+v", d)
	}
	if !blocked.SafetyBlocked {
		t.Fatalf("negative history should mark plan blocked")
	}
	recent := Plan(RequestFacts{EstimatedInputTokens: 5000, RecentEdit: true})
	if d := findDecision(t, recent, Layer1); d.Action != ActionCheapOnly || d.Reason != "recent_edit_preserve_full_context" {
		t.Fatalf("recent edit L1=%+v", d)
	}
}

func TestPlan_L2PolicyAndThresholds(t *testing.T) {
	t.Parallel()
	notReady := Plan(RequestFacts{EstimatedInputTokens: 20000})
	if d := findDecision(t, notReady, Layer2); d.Action != ActionBypass || d.Risk != "none" {
		t.Fatalf("not ready L2=%+v", d)
	}
	ready := Plan(RequestFacts{EstimatedInputTokens: 20000, ExternalLayer2Allowed: true, Layer2Acknowledged: true, LiveCorpusConfidence: "medium"})
	if d := findDecision(t, ready, Layer2); d.Action != ActionRun || d.Confidence != "medium" {
		t.Fatalf("ready L2=%+v", d)
	}
	adaptive := Plan(RequestFacts{EstimatedInputTokens: 8000, ContentClasses: []string{"repeated_tool_output"}, ExternalLayer2Allowed: true, Layer2Acknowledged: true})
	if d := findDecision(t, adaptive, Layer2); d.Action != ActionShadow || d.Reason != "adaptive_roi_candidate" {
		t.Fatalf("adaptive L2=%+v", d)
	}
	below := Plan(RequestFacts{EstimatedInputTokens: 8000, ExternalLayer2Allowed: true, Layer2Acknowledged: true})
	if d := findDecision(t, below, Layer2); d.Action != ActionBypass || d.Reason != "below_roi_threshold" {
		t.Fatalf("below L2=%+v", d)
	}
	recent := Plan(RequestFacts{EstimatedInputTokens: 20000, ExternalLayer2Allowed: true, Layer2Acknowledged: true, RecentEdit: true})
	if d := findDecision(t, recent, Layer2); d.Action != ActionBypass || d.Reason != "recent_edit_window" {
		t.Fatalf("recent L2=%+v", d)
	}
}

func TestPlan_L3CacheAndPreviousResponse(t *testing.T) {
	t.Parallel()
	unsupported := Plan(RequestFacts{EstimatedInputTokens: 5000})
	if d := findDecision(t, unsupported, Layer3); d.Action != ActionBypass || d.Reason != "provider_cache_unsupported" {
		t.Fatalf("unsupported L3=%+v", d)
	}
	small := Plan(RequestFacts{EstimatedInputTokens: 100, ProviderCacheSupported: true})
	if d := findDecision(t, small, Layer3); d.Action != ActionBypass || d.Reason != "prefix_too_small" {
		t.Fatalf("small L3=%+v", d)
	}
	cache := Plan(RequestFacts{EstimatedInputTokens: 5000, ProviderCacheSupported: true})
	if d := findDecision(t, cache, Layer3); d.Action != ActionRun || d.Reason != "stable_prefix_cache_hint" || d.Confidence != "provider_reported" {
		t.Fatalf("cache L3=%+v", d)
	}
	prev := Plan(RequestFacts{EstimatedInputTokens: 100, ProviderCacheSupported: true, PreviousResponseIDAvailable: true})
	if d := findDecision(t, prev, Layer3); d.Action != ActionRun || d.Reason != "previous_response_state_available" {
		t.Fatalf("prev L3=%+v", d)
	}
}

func TestPlan_L4OutputReduce(t *testing.T) {
	t.Parallel()
	cooldown := Plan(RequestFacts{ExpectedOutputTokens: 1000, OutputReduceCooldown: true})
	if d := findDecision(t, cooldown, Layer4); d.Action != ActionCheapOnly || d.Reason != "quality_cooldown_soften_profile" || d.Risk != "medium" {
		t.Fatalf("cooldown L4=%+v", d)
	}
	run := Plan(RequestFacts{ExpectedOutputTokens: 300, LiveCorpusConfidence: "low"})
	if d := findDecision(t, run, Layer4); d.Action != ActionRun || d.ExpectedSavingsTokens != 60 || d.Confidence != "low" {
		t.Fatalf("run L4=%+v", d)
	}
	min := Plan(RequestFacts{EstimatedInputTokens: 1000, ExpectedOutputTokens: 10})
	if d := findDecision(t, min, Layer4); d.Action != ActionRun || d.ExpectedSavingsTokens != 20 {
		t.Fatalf("min L4=%+v", d)
	}
}

func TestPlan_WebSocketModes(t *testing.T) {
	t.Parallel()
	notWS := Plan(RequestFacts{RouteMode: "upstream"})
	if d := findDecision(t, notWS, LayerWebSocket); d.Action != ActionBypass {
		t.Fatalf("not websocket=%+v", d)
	}
	inspect := Plan(RequestFacts{RouteMode: "websocket_tunnel"})
	if d := findDecision(t, inspect, LayerWebSocket); d.Action != ActionInspect || d.Reason != "inspect_only_default" {
		t.Fatalf("inspect=%+v", d)
	}
	unknown := Plan(RequestFacts{RouteMode: "websocket_tunnel", WebSocketMutationRequested: true})
	if d := findDecision(t, unknown, LayerWebSocket); d.Action != ActionInspect || d.Risk != "blocked" {
		t.Fatalf("unknown=%+v", d)
	}
	shadow := Plan(RequestFacts{RouteMode: "websocket_tunnel", WebSocketMutationRequested: true, WebSocketShapeKnown: true, EstimatedInputTokens: 4000, LiveCorpusConfidence: "medium"})
	if d := findDecision(t, shadow, LayerWebSocket); d.Action != ActionShadow || d.ExpectedSavingsTokens != 1000 {
		t.Fatalf("shadow=%+v", d)
	}
	mutate := Plan(RequestFacts{RouteMode: "websocket_tunnel", WebSocketMutationRequested: true, WebSocketShapeKnown: true, EstimatedInputTokens: 3000, LiveCorpusConfidence: "high"})
	if d := findDecision(t, mutate, LayerWebSocket); d.Action != ActionMutate || d.ExpectedSavingsTokens != 1000 || d.Risk != "high" {
		t.Fatalf("mutate=%+v", d)
	}
	disabled := Plan(RequestFacts{RouteMode: "websocket_tunnel", ManualDisabled: map[Layer]bool{LayerWebSocket: true}})
	if d := findDecision(t, disabled, LayerWebSocket); d.Action != ActionTunnel {
		t.Fatalf("disabled=%+v", d)
	}
}

func TestPlan_NormalizationAndHelpers(t *testing.T) {
	t.Parallel()
	plan := Plan(RequestFacts{Provider: " openai ", Model: " gpt ", RouteMode: " websocket ", TaskShape: " debug ", ContentClasses: []string{"JSON"}, LiveCorpusConfidence: "strange", LatencyBudgetMs: 0})
	if plan.Provider != "openai" || plan.Model != "gpt" || plan.RouteMode != "websocket" {
		t.Fatalf("normalization failed: %+v", plan)
	}
	if d := findDecision(t, plan, Layer1); d.Confidence != "unknown" {
		t.Fatalf("unexpected confidence: %+v", d)
	}
	if !hasAnyClass(RequestFacts{ContentClasses: []string{"a"}}, "b", "a") {
		t.Fatal("hasAnyClass false negative")
	}
	if d := decision(Layer0, ActionRun, "x", -1, "low", "high"); d.ExpectedSavingsTokens != 0 {
		t.Fatalf("negative expected not clamped: %+v", d)
	}
}

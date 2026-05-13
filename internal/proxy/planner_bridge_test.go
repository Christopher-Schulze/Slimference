package proxy

import (
	"testing"

	"github.com/slimference/slimference/internal/config"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/planner"
	"github.com/slimference/slimference/internal/types"
)

func TestDryRunPlan_AttachesProviderAndDisabledLayers(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.Enabled = false
	p := New(cfg)
	p.SetLayerEnabled(2, false)

	plan := p.dryRunPlan(plannerInput{
		provider:                    types.OpenAI,
		model:                       "gpt-5.1",
		routeMode:                   "upstream",
		estimatedInputTokens:        4000,
		expectedOutputTokens:        700,
		contentClasses:              []string{" tool_output ", "tool_output", "json"},
		previousResponseIDAvailable: true,
		liveCorpusConfidence:        "high",
	})
	if plan == nil || plan.Provider != "openai" || plan.Model != "gpt-5.1" || plan.RouteMode != "upstream" {
		t.Fatalf("bad plan identity: %+v", plan)
	}
	if !hasPlanAction(plan.Decisions, "l2", "bypass", "operator_disabled") {
		t.Fatalf("expected disabled L2 decision: %+v", plan.Decisions)
	}
	if !hasPlanAction(plan.Decisions, "l4_output", "bypass", "operator_disabled") {
		t.Fatalf("expected disabled L4 decision: %+v", plan.Decisions)
	}
	if !hasPlanAction(plan.Decisions, "l3", "run", "previous_response_state_available") {
		t.Fatalf("expected L3 previous-response decision: %+v", plan.Decisions)
	}
}

func TestDryRunPlan_NilProxy(t *testing.T) {
	t.Parallel()
	var p *Proxy
	if got := p.dryRunPlan(plannerInput{provider: types.OpenAI}); got != nil {
		t.Fatalf("nil proxy plan = %+v, want nil", got)
	}
}

func TestDebugPlanSummary(t *testing.T) {
	t.Parallel()
	plan := debugPlanSummary(planner.CompressionPlan{
		Provider:      "codex_chatgpt",
		Model:         "codex",
		RouteMode:     "websocket_tunnel",
		SafetyBlocked: true,
		Decisions: []planner.LayerDecision{{
			Layer:                 planner.LayerWebSocket,
			Action:                planner.ActionInspect,
			Reason:                "unknown_shape_blocks_mutation",
			ExpectedSavingsTokens: 0,
			Risk:                  "blocked",
			Confidence:            "high",
		}},
	})
	if plan.Provider != "codex_chatgpt" || !plan.SafetyBlocked || len(plan.Decisions) != 1 {
		t.Fatalf("bad debug plan: %+v", plan)
	}
	if d := plan.Decisions[0]; d.Layer != "websocket" || d.Action != "inspect" || d.Risk != "blocked" {
		t.Fatalf("bad debug decision: %+v", d)
	}
}

func TestPlannerClassesFromMessages(t *testing.T) {
	t.Parallel()
	messages := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "please read this"}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", Text: "package main\nfunc main() {}\n"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: `{"ok":true}`}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "[1,2,3]"}}},
	}
	classes := plannerClassesFromMessages(messages)
	for _, want := range []string{"conversation", "tool_output", "source_file", "json"} {
		if !hasString(classes, want) {
			t.Fatalf("classes=%v missing %s", classes, want)
		}
	}
}

func TestPlannerClassHelpers(t *testing.T) {
	t.Parallel()
	classes := normalizedPlannerClasses([]string{"", " JSON ", "json", "Source_File"})
	if len(classes) != 2 || classes[0] != "json" || classes[1] != "source_file" {
		t.Fatalf("normalized classes = %+v", classes)
	}
	sourceSamples := []string{
		"function x() {}",
		"class Example {}",
		"def f(): pass",
		"#include <stdio.h>",
		"import React from 'react'",
	}
	for _, sample := range sourceSamples {
		if !looksLikeSource(sample) {
			t.Fatalf("expected source sample: %q", sample)
		}
	}
	if looksLikeSource("plain prose") || looksStructured("plain prose") {
		t.Fatal("plain prose must not be source or structured")
	}
	if !looksStructured(" {\"x\":1}") || !looksStructured(" [1]") {
		t.Fatal("structured JSON-like text not detected")
	}
}

func hasPlanAction(decisions []dbg.PlanDecisionSummary, layer, action, reason string) bool {
	for _, decision := range decisions {
		if decision.Layer == layer && decision.Action == action && decision.Reason == reason {
			return true
		}
	}
	return false
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

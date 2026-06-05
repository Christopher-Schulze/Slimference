package proxy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/slimference/slimference/internal/config"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/planner"
	"github.com/slimference/slimference/internal/sessions"
	"github.com/slimference/slimference/internal/types"
	"github.com/slimference/slimference/internal/wscompact"
)

func TestDryRunPlan_AttachesProviderAndDisabledLayers(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.Enabled = false
	p := New(cfg)

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
	if !hasPlanAction(plan.Decisions, "l4_output", "bypass", "operator_disabled") {
		t.Fatalf("expected disabled L4 decision: %+v", plan.Decisions)
	}
	if !hasPlanAction(plan.Decisions, "l2", "run", "previous_response_state_available") {
		t.Fatalf("expected L2 previous-response decision: %+v", plan.Decisions)
	}
}

func TestDryRunPlan_NilProxy(t *testing.T) {
	t.Parallel()
	var p *Proxy
	if got := p.dryRunPlan(plannerInput{provider: types.OpenAI}); got != nil {
		t.Fatalf("nil proxy plan = %+v, want nil", got)
	}
}

func TestBuildCompressionPlan_DrivesRecentEditActions(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	p := New(cfg)

	plan := p.buildCompressionPlan(plannerInput{
		provider:             types.Anthropic,
		model:                "claude",
		routeMode:            "upstream",
		estimatedInputTokens: 20_000,
		contentClasses:       []string{"source_file"},
		recentEdit:           true,
	})
	l1, ok := plannerDecisionForLayer(plan, planner.Layer1)
	if !ok || l1.Action != planner.ActionCheapOnly || l1.Reason != "recent_edit_preserve_full_context" {
		t.Fatalf("L1 recent-edit decision = %+v, ok=%v", l1, ok)
	}
	if got := plannerActionForLayer(plan, planner.Layer2, planner.ActionInspect); got == planner.ActionInspect {
		t.Fatalf("expected concrete L2 action, got fallback %q", got)
	}
	if got := plannerActionForLayer(plan, planner.Layer("missing"), planner.ActionInspect); got != planner.ActionInspect {
		t.Fatalf("missing layer action = %q, want fallback", got)
	}
}

func TestPlannerRecentEditFact_UsesSessionHookState(t *testing.T) {
	home := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	cfg := config.Defaults()
	p := New(cfg)
	sessionID := "planner-edit-session"
	if err := sessions.ObserveHookFile(sessions.DefaultHookStateDir(home), sessionID, "/repo/main.go", "edit"); err != nil {
		t.Fatalf("observe edit: %v", err)
	}
	messages := []types.Message{{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Read"}}}}
	if !p.plannerRecentEditFact(sessionID, messages) {
		t.Fatal("session-owned edit state must drive planner recent-edit fact")
	}
	if p.plannerRecentEditFact("", messages) {
		t.Fatal("empty session without edit tool must not mark recent edit")
	}
	proxyUserHomeDir = func() (string, error) { return "", errors.New("home") }
	if p.plannerRecentEditFact(sessionID, messages) {
		t.Fatal("home lookup failure must fail closed to no session edit fact")
	}
}

func TestPlannerRecentEditFact_RequestIntentAndMissingState(t *testing.T) {
	home := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	p := New(config.Defaults())
	if !p.plannerRecentEditFact("no-state-needed", []types.Message{{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Edit"}}}}) {
		t.Fatal("current request edit intent must mark recent edit")
	}
	if sessionHasRecentEditedFile("missing-session", 2) {
		t.Fatal("missing hook-state file must not mark recent edit")
	}
	stateDir := sessions.DefaultHookStateDir(home)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, sessions.SafeSessionID("broken-session")+".json"), []byte(`{`), 0o644); err != nil {
		t.Fatal(err)
	}
	if sessionHasRecentEditedFile("broken-session", 2) {
		t.Fatal("invalid hook-state JSON must not mark recent edit")
	}
}

func TestPlannerLiveCorpusConfidence_ConfigAndMetadata(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Compression.Tuning.PlannerLiveCorpusConfidence = "medium"
	if got := New(cfg).plannerLiveCorpusConfidence(); got != "medium" {
		t.Fatalf("configured confidence = %q", got)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "metadata.json")
	if err := os.WriteFile(path, []byte(`{"synthetic":false,"evidence_level":"live_operator","expected_request_count":7}`), 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	cfg = config.Defaults()
	cfg.Compression.Tuning.PlannerLiveCorpusMetadataPath = dir
	if got := New(cfg).plannerLiveCorpusConfidence(); got != "high" {
		t.Fatalf("live metadata confidence = %q", got)
	}
	if err := os.WriteFile(path, []byte(`{"synthetic":true,"evidence_level":"synthetic","expected_request_count":7}`), 0o644); err != nil {
		t.Fatalf("write synthetic metadata: %v", err)
	}
	if got := New(cfg).plannerLiveCorpusConfidence(); got != "low" {
		t.Fatalf("synthetic metadata confidence = %q", got)
	}
}

func TestPlannerLiveCorpusConfidence_FallbackBranches(t *testing.T) {
	t.Parallel()
	var p *Proxy
	if got := p.plannerLiveCorpusConfidence(); got != "unknown" {
		t.Fatalf("nil proxy confidence = %q", got)
	}
	if got := normalizePlannerConfidence(" invalid "); got != "" {
		t.Fatalf("invalid normalized confidence = %q", got)
	}
	cfg := config.Defaults()
	cfg.Compression.Tuning.PlannerLiveCorpusConfidence = "invalid"
	if got := New(cfg).plannerLiveCorpusConfidence(); got != "unknown" {
		t.Fatalf("invalid configured confidence fallback = %q", got)
	}
	if got := liveCorpusConfidenceFromMetadataPath(filepath.Join(t.TempDir(), "missing.json")); got != "" {
		t.Fatalf("missing metadata confidence = %q", got)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "metadata.json")
	badDir := filepath.Join(t.TempDir(), "metadata.json")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := liveCorpusConfidenceFromMetadataPath(badDir); got != "" {
		t.Fatalf("unreadable metadata confidence = %q", got)
	}
	if err := os.WriteFile(path, []byte(`{"synthetic":false,"evidence_level":"realish","expected_request_count":0}`), 0o644); err != nil {
		t.Fatalf("write realish metadata: %v", err)
	}
	if got := liveCorpusConfidenceFromMetadataPath(path); got != "medium" {
		t.Fatalf("realish metadata confidence = %q", got)
	}
	if err := os.WriteFile(path, []byte(`{"synthetic":false,"evidence_level":"","expected_request_count":3}`), 0o644); err != nil {
		t.Fatalf("write count metadata: %v", err)
	}
	if got := liveCorpusConfidenceFromMetadataPath(path); got != "medium" {
		t.Fatalf("count metadata confidence = %q", got)
	}
	if err := os.WriteFile(path, []byte(`{"synthetic":false}`), 0o644); err != nil {
		t.Fatalf("write unknown metadata: %v", err)
	}
	if got := liveCorpusConfidenceFromMetadataPath(path); got != "unknown" {
		t.Fatalf("unknown metadata confidence = %q", got)
	}
	if err := os.WriteFile(path, []byte(`{`), 0o644); err != nil {
		t.Fatalf("write invalid metadata: %v", err)
	}
	if got := liveCorpusConfidenceFromMetadataPath(path); got != "" {
		t.Fatalf("invalid metadata confidence = %q", got)
	}
}

func TestWebSocketShapeKnown_UsesRegistry(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	p := New(cfg)
	if p.webSocketShapeKnown() {
		t.Fatal("fresh websocket shape registry must be unknown")
	}
	p.webSocketShapes.Observe(wscompact.FrameSummary{
		Route:        "/backend-api/dev",
		Direction:    wscompact.DirectionClientToServer,
		JSON:         true,
		JSONTopLevel: "object",
		JSONKeys:     []string{"type"},
		JSONTypes:    []string{"type:string"},
		MessageType:  "hello",
		Opcode:       "text",
	})
	if p.webSocketShapeKnown() {
		t.Fatal("inspect-only websocket JSON shape must not mark mutation shape known")
	}
	p.webSocketShapes.Observe(wscompact.FrameSummary{
		Route:        "/backend-api/codex/responses",
		Direction:    wscompact.DirectionClientToServer,
		JSON:         true,
		JSONTopLevel: "object",
		JSONKeys:     []string{"request", "type"},
		JSONTypes:    []string{"request:object", "type:string"},
		MessageType:  "request",
		Opcode:       "text",
	})
	if !p.webSocketShapeKnown() {
		t.Fatal("registered websocket Phase-F request shape must mark mutation shape known")
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

func TestPlannerClassesFromMessagesDetectsRepeatedToolOutput(t *testing.T) {
	t.Parallel()
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-1", ToolName: "exec_command", ToolInput: `{"command":["bash","-lc","cat docs/todo.md"],"workdir":"/repo/project"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-1", Text: "first read"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-2", ToolName: "exec_command", ToolInput: `{"command":["bash","-lc","cat docs/todo.md"],"workdir":"/repo/project"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-2", Text: "second read"}}},
	}
	classes := plannerClassesFromMessages(messages)
	if !hasString(classes, "repeated_tool_output") {
		t.Fatalf("classes=%v missing repeated_tool_output", classes)
	}
}

func TestPlannerClassesFromMessagesDoesNotInventRepeatedToolOutput(t *testing.T) {
	t.Parallel()
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-1", ToolName: "read_file", ToolInput: `{"path":"a.go"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-1", Text: "package a"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-2", ToolName: "read_file", ToolInput: `{"path":"b.go"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-2", Text: "package b"}}},
	}
	classes := plannerClassesFromMessages(messages)
	if hasString(classes, "repeated_tool_output") {
		t.Fatalf("classes=%v should not contain repeated_tool_output", classes)
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

func TestRequestHasEditIntent(t *testing.T) {
	t.Parallel()
	if requestHasEditIntent([]types.Message{{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Read"}}}}) {
		t.Fatal("read-only tool must not count as edit")
	}
	editSamples := [][]types.Message{
		{{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Edit"}}}},
		{{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "shell", ToolInput: `{"command":"apply_patch <<'PATCH'"}`}}}},
		{{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "exec_command", ToolInput: `{"cmd":"write file"}`}}}},
	}
	for _, sample := range editSamples {
		if !requestHasEditIntent(sample) {
			t.Fatalf("expected edit intent for %+v", sample)
		}
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

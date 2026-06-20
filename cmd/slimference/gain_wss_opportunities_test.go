package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
)

func TestQueryWSSShadowMirrorOpportunityRowsFallbackRanksMajorLanes(t *testing.T) {
	tmp := t.TempDir()
	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { osUserHomeDir = origHome })
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", "")

	decisionsPath := filepath.Join(tmp, ".slimference", "debug", "decisions.jsonl")
	if err := os.MkdirAll(filepath.Dir(decisionsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	writeDecisionSummary(t, decisionsPath, dbg.RequestSummary{
		RequestID: "req-exact",
		Timestamp: now,
		DebugFacts: map[string]string{
			"wss.request_shape":                      "full_history",
			"wss.shadow_mirror_referenceable_bytes":  "3000",
			"wss.shadow_mirror_bytes":                "6000",
			"wss.shadow_mirror_referenceable_blocks": "1",
			"wss.shadow_mirror_blocks":               "1",
		},
		Tokens: dbg.TokenCounts{Saved: 100},
	})
	writeDecisionSummary(t, decisionsPath, dbg.RequestSummary{
		RequestID: "req-codex-payload",
		Timestamp: now,
		DebugFacts: map[string]string{
			"wss.request_shape":                            "full_history",
			"wss.shadow_mirror_normalized_density_by_kind": "codex_exec_payload=4000/8000/1/1",
		},
		Tokens: dbg.TokenCounts{Saved: 200},
	})
	writeDecisionSummary(t, decisionsPath, dbg.RequestSummary{
		RequestID:              "req-delta",
		Timestamp:              now,
		PreviousResponseIDUsed: true,
		DebugFacts: map[string]string{
			"wss.shadow_mirror_normalized_density_by_kind": "codex_exec_payload=2000/4000/1/1",
		},
		Tokens: dbg.TokenCounts{Saved: 50},
	})
	writeDecisionSummary(t, decisionsPath, dbg.RequestSummary{
		RequestID: "req-root",
		Timestamp: now,
		DebugFacts: map[string]string{
			"wss.request_shape":                            "root",
			"wss.shadow_mirror_normalized_density_by_kind": "text=1000/2000/1/1",
		},
	})
	writeDecisionSummary(t, decisionsPath, dbg.RequestSummary{
		RequestID: "req-stateful-safe",
		Timestamp: now,
		DebugFacts: map[string]string{
			"wss.request_shape": "full_history",
			"wss.shadow_mirror_stateful_safe_density_by_kind":     "stateful_safe_tool_output_git_status=1200/2400/1/1",
			"wss.previous_response_id":                            "true",
			"wss.structured_mutation_guard":                       "wss_full_history_downstream_delta_proof_gate",
			"wss.history_mutation_recovery_guard":                 "true",
			"wss.cache_bust_demoted_mechanisms":                   "stateful_safe_tool_output",
			"wss.shadow_mirror_normalized_density_by_kind":        "bad",
			"wss.shadow_mirror_normalized_referenceable_segments": "1",
		},
	})

	rows, err := queryWSSShadowMirrorOpportunityRows("all", now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 5 {
		t.Fatalf("expected WSS opportunity rows, got %+v", rows)
	}
	byCommand := make(map[string]gainOpportunityRow, len(rows))
	for _, row := range rows {
		byCommand[row.Command] = row
	}
	assertGainOpportunityLane(t, byCommand["full_history/exact_block"], "t408_backend_reference_contract", "reference_only_backend_contract_required")
	assertGainOpportunityLane(t, byCommand["full_history/codex_exec_payload"], "t408_reference_or_t418_parser_recovery", "requires_parser_or_recovery_product_slice")
	assertGainOpportunityLane(t, byCommand["delta/codex_exec_payload"], "t405_t354_stateful_delta", "requires_downstream_state_zero400_gate")
	assertGainOpportunityLane(t, byCommand["root/text"], "t406_t418_parser_frontier", "requires_command_output_first_parser_gate")
	stateful := byCommand["full_history/stateful_safe_tool_output_git_status"]
	assertGainOpportunityLane(t, stateful, "t417_class_b_server_state", "mixed_previous_response_state_requires_exact_lineage_split")
	if !strings.Contains(strings.Join(stateful.PromotionBlockers, "|"), "cache_bust_demotion_present_exact_class_scope") {
		t.Fatalf("stateful-safe blockers missing cache-bust precision: %+v", stateful)
	}
}

func TestQueryWSSShadowMirrorOpportunityRowsMissingFallbackIsEmpty(t *testing.T) {
	tmp := t.TempDir()
	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { osUserHomeDir = origHome })
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", "")

	rows, err := queryWSSShadowMirrorOpportunityRows("today", time.Now(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("missing fallback should produce no rows, got %+v", rows)
	}
}

func TestGainWSSShadowMirrorHelpersHandleEdges(t *testing.T) {
	tmp := t.TempDir()
	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { osUserHomeDir = origHome })
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", "")

	if rows := parseGainWSSShadowMirrorKindRows("bad,codex_exec_payload=x/1/1/1,tool_result_command_git=30/60/1/1"); len(rows) != 1 || rows[0].kind != "tool_result_command_git" || rows[0].referenceableBytes != 30 {
		t.Fatalf("parse rows edge mismatch: %+v", rows)
	}
	if got := gainWSSRequestShape(dbg.RequestSummary{DebugFacts: map[string]string{"wss.delta_shape": "true"}}); got != "delta" {
		t.Fatalf("delta shape fallback: %q", got)
	}
	if got := gainWSSShadowMirrorCandidateAction("full_history", "tool_result_command_git_status"); !strings.Contains(got, "exact command family") && !strings.Contains(got, "resolved tool-result command family") {
		t.Fatalf("tool-result action: %q", got)
	}
	if got := gainOpportunityDecisionsLogPath(); got == "" {
		t.Fatal("fallback decisions path should resolve from user home")
	}
	if gainContainsString([]string{"a", "b"}, "c") {
		t.Fatal("gainContainsString false branch failed")
	}
	if gainHasUpstreamOrHTTP400Error(dbg.RequestSummary{Errors: []string{"plain parse miss"}}) {
		t.Fatal("plain error should not be classified as upstream/400")
	}
	if !gainHasUpstreamOrHTTP400Error(dbg.RequestSummary{Errors: []string{"upstream invalid_request 400"}}) {
		t.Fatal("upstream invalid_request 400 should be classified")
	}
	if gainWSSShadowMirrorFactsPresent(map[string]string{"other": "1"}) {
		t.Fatal("non-shadow facts should not be treated as shadow mirror facts")
	}
	rows := []gainOpportunityRow{
		{Command: "b", LocalTokensHeadroom: 10, InputTokens: 20},
		{Command: "a", LocalTokensHeadroom: 10, InputTokens: 20},
		{Command: "c", LocalTokensHeadroom: 5, InputTokens: 50},
	}
	sortGainOpportunityRows(rows)
	if rows[0].Command != "a" || rows[2].Command != "c" {
		t.Fatalf("sort order mismatch: %+v", rows)
	}
	if got := gainDedupeStrings([]string{"", "a", "a", " b "}); strings.Join(got, ",") != "a,b" {
		t.Fatalf("dedupe mismatch: %v", got)
	}
	if gainMaxInt(1, 2) != 2 {
		t.Fatal("gainMaxInt second branch failed")
	}
}

func TestGainWSSShadowMirrorPolicyMatrixBranches(t *testing.T) {
	lanes := map[string]string{
		"full_history/stateful_safe_tool_output":    "t417_class_b_server_state",
		"full_history/codex_exec_payload_command_x": "t408_reference_or_t418_parser_recovery",
		"full_history/other":                        "t408_backend_reference_contract",
		"delta/codex_exec_payload":                  "t405_t354_stateful_delta",
		"root/text":                                 "t406_t418_parser_frontier",
		"unknown/text":                              "capture_shape_resolution",
	}
	for key, want := range lanes {
		shape, kind, _ := strings.Cut(key, "/")
		if got := gainWSSShadowMirrorCandidateLane(shape, kind); got != want {
			t.Fatalf("lane %s: got %s want %s", key, got, want)
		}
	}
	actions := []string{
		gainWSSShadowMirrorCandidateAction("full_history", "stateful_safe_tool_output"),
		gainWSSShadowMirrorCandidateAction("full_history", "codex_exec_payload_command_rg"),
		gainWSSShadowMirrorCandidateAction("full_history", "plain_text"),
		gainWSSShadowMirrorCandidateAction("delta", "codex_exec_payload"),
		gainWSSShadowMirrorCandidateAction("root", "text"),
		gainWSSShadowMirrorCandidateAction("unknown", "text"),
	}
	for _, action := range actions {
		if strings.TrimSpace(action) == "" {
			t.Fatalf("empty action in %v", actions)
		}
	}
	for _, tc := range []struct {
		lane  string
		stage string
		gate  string
	}{
		{"t417_class_b_server_state", "product_candidate_no_observed_blockers", "t417_exact_lineage_net_positive_zero400_gate"},
		{"t408_backend_reference_contract", "t408_backend_reference_candidate_needs_accepted_contract", "t408_backend_reference_acceptance_or_exact_rehydrate_contract"},
		{"t408_reference_or_t418_parser_recovery", "t408_reference_or_t418_parser_recovery_candidate_needs_contract", "t408_backend_reference_or_t418_parser_recovery_gate"},
		{"t405_t354_stateful_delta", "t405_t354_candidate_needs_downstream_state_gate", "t405_t354_downstream_state_zero400_gate"},
		{"t406_t418_parser_frontier", "t418_parser_candidate_needs_release_gate", "t418_command_output_first_or_t406_stateful_safe_parser_gate"},
		{"capture_shape_resolution", "needs_shape_resolution", "shape_resolution_gate"},
	} {
		blockers := gainWSSShadowMirrorLaneBlockers(tc.lane, 100)
		if got := gainWSSShadowMirrorPromotionStage(tc.lane, blockers); got != tc.stage && tc.lane != "t417_class_b_server_state" {
			t.Fatalf("stage %s: got %s want %s blockers=%v", tc.lane, got, tc.stage, blockers)
		}
		if got := gainWSSShadowMirrorProofGate(tc.lane, false); got != tc.gate {
			t.Fatalf("gate %s: got %s want %s", tc.lane, got, tc.gate)
		}
	}
	if got := gainWSSShadowMirrorPromotionStage("t408_backend_reference_contract", []string{"erroring_shape"}); got != "not_safe_erroring" {
		t.Fatalf("error stage: %s", got)
	}
	if got := gainWSSShadowMirrorPromotionStage("t408_backend_reference_contract", []string{"no_incremental_local_headroom"}); got != "not_economic" {
		t.Fatalf("not economic stage: %s", got)
	}
	if got := gainWSSShadowMirrorProofGate("t408_backend_reference_contract", true); got != "fix_or_exclude_erroring_shape_before_promotion" {
		t.Fatalf("error proof gate: %s", got)
	}
}

func TestQueryWSSShadowMirrorOpportunityRowsPathErrors(t *testing.T) {
	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "", errors.New("home failed") }
	t.Cleanup(func() { osUserHomeDir = origHome })
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", "")

	rows, err := queryWSSShadowMirrorOpportunityRows("today", time.Now(), 10)
	if err != nil || len(rows) != 0 {
		t.Fatalf("home error should fail closed without rows: rows=%+v err=%v", rows, err)
	}
}

func TestQueryWSSShadowMirrorOpportunityRowsReplayAndWindowErrors(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", tmp)
	if _, err := queryWSSShadowMirrorOpportunityRows("today", time.Now(), 10); err == nil {
		t.Fatal("expected replay error when decisions path is a directory")
	}

	decisionsPath := filepath.Join(t.TempDir(), "decisions.jsonl")
	writeDecisionSummary(t, decisionsPath, dbg.RequestSummary{RequestID: "req", Timestamp: time.Now()})
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", decisionsPath)
	if _, err := queryWSSShadowMirrorOpportunityRows("bad-period", time.Now(), 10); err == nil {
		t.Fatal("expected window error for bad period")
	}
}

func TestWSSShadowMirrorOpportunityAccumulatorFinalizeEdges(t *testing.T) {
	var empty wssShadowMirrorOpportunityAccumulator
	if rows := empty.finalize(10); len(rows) != 0 {
		t.Fatalf("empty finalize rows: %+v", rows)
	}
	empty.add(dbg.RequestSummary{})

	acc := wssShadowMirrorOpportunityAccumulator{rows: map[string]*gainOpportunityRow{
		"negative": {
			Scope:         wssShadowMirrorOpportunityScope,
			Command:       "full_history/tool_result",
			InputTokens:   10,
			OutputTokens:  20,
			CandidateLane: "t408_backend_reference_contract",
		},
	}}
	rows := acc.finalize(0)
	if len(rows) != 1 || rows[0].LocalTokensHeadroom != 0 || rows[0].PromotionStage != "not_economic" {
		t.Fatalf("negative headroom finalize mismatch: %+v", rows)
	}
	if !gainContainsString(rows[0].PromotionBlockers, "no_incremental_local_headroom") {
		t.Fatalf("expected no-headroom blocker: %+v", rows[0])
	}
	if gainMaxInt(3, 2) != 3 {
		t.Fatal("gainMaxInt first branch failed")
	}
	if !gainContainsString([]string{"a", "b"}, "b") {
		t.Fatal("gainContainsString true branch failed")
	}
}

func assertGainOpportunityLane(t *testing.T, row gainOpportunityRow, lane, blocker string) {
	t.Helper()
	if row.CandidateLane != lane || row.LocalTokensHeadroom <= 0 {
		t.Fatalf("row lane/headroom mismatch: got %+v want lane %s", row, lane)
	}
	if blocker != "" && !strings.Contains(strings.Join(row.PromotionBlockers, "|"), blocker) {
		t.Fatalf("row blockers missing %q: %+v", blocker, row)
	}
}

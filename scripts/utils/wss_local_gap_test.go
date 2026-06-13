package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/evidence"
)

func TestWSSLocalGapReportSeparatesLocalAndProviderCache(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path,
		dbg.RequestSummary{
			RequestID:            "full-history-guarded",
			Timestamp:            time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC),
			Path:                 "/backend-api/codex/responses",
			RouteMode:            "websocket_phasef",
			CacheReadTokens:      1000,
			ProviderCachedTokens: 2000,
			CacheCreateTokens:    300,
			OutputTokens:         11,
			Tokens:               dbg.TokenCounts{Original: 10000, Final: 9500, Saved: 500},
			DebugFacts: map[string]string{
				"wss.request_shape":        "full_history",
				"wss.tool_command_classes": "rg_search=1,git_show_stat=1",
			},
			EvidenceDecisions: []evidence.BlockDecision{
				{
					Mechanism:      "captured_output",
					ContentClass:   evidence.ContentSearch,
					Action:         evidence.ActionFullPass,
					Reason:         "wss_search_output_risk_gate",
					OriginalTokens: 6000,
					FinalTokens:    6000,
				},
				{
					Mechanism:      "read_delta",
					ContentClass:   evidence.ContentPlain,
					Action:         evidence.ActionApplied,
					Reason:         "positive_net_savings",
					OriginalTokens: 1000,
					FinalTokens:    100,
					SavedTokens:    900,
					NetTokens:      900,
				},
			},
		},
		dbg.RequestSummary{
			RequestID:              "delta-guarded",
			Path:                   "/backend-api/codex/responses",
			RouteMode:              "websocket_phasef",
			PreviousResponseIDUsed: true,
			ProviderOutputTokens:   7,
			Tokens:                 dbg.TokenCounts{Original: 5000, Final: 5000, Saved: 0},
			DebugFacts: map[string]string{
				"wss.tool_command_classes": "go_test=1",
			},
			EvidenceDecisions: []evidence.BlockDecision{{
				Mechanism:      "repeated_tool_output",
				ContentClass:   evidence.ContentPlain,
				Action:         evidence.ActionFullPass,
				Reason:         "wss_stateful_delta_mutation_proof_gate",
				OriginalTokens: 3500,
				FinalTokens:    3500,
			}},
		},
		dbg.RequestSummary{
			RequestID: "http-ignored",
			Path:      "/v1/responses",
			RouteMode: "http",
			Tokens:    dbg.TokenCounts{Original: 1_000_000, Final: 1, Saved: 999_999},
		},
	)

	report, err := loadWSSLocalGapReport(wssLocalGapFlags{
		path:          path,
		minLocalRatio: 0.48,
	})
	if err != nil {
		t.Fatalf("loadWSSLocalGapReport() error = %v", err)
	}
	if report.Requests != 3 || report.WSSRequests != 2 || report.PhaseFRequests != 2 {
		t.Fatalf("bad request counts: %+v", report)
	}
	if report.OriginalTokens != 15000 ||
		report.LocalSavedTokens != 500 ||
		report.LocalSavingsRatio != 500.0/15000.0 ||
		report.PositiveSavingsOrig != 10000 ||
		report.PositiveSavingsRatio != 500.0/10000.0 ||
		report.ProviderCacheReadTokens != 1000 ||
		report.ProviderCacheTokens != 2000 ||
		report.ProviderCacheCreate != 300 ||
		report.OutputTokens != 18 {
		t.Fatalf("bad local/provider totals: %+v", report)
	}
	if report.GatePassed || len(report.GateFailures) != 1 ||
		!strings.Contains(report.GateFailures[0], "local_savings_ratio") {
		t.Fatalf("expected local savings gate failure: %+v", report.GateFailures)
	}
	if len(report.Guards) != 2 ||
		report.Guards[0].Reason != "wss_search_output_risk_gate" ||
		report.Guards[0].GuardedPotential != 6000 ||
		report.Guards[0].Mechanisms["captured_output"] != 1 ||
		report.Guards[0].RequestShapes["full_history"] != 1 {
		t.Fatalf("bad guard ranking: %+v", report.Guards)
	}
	if len(report.Mechanisms) != 3 ||
		report.Mechanisms[0].Mechanism != "captured_output" ||
		report.Mechanisms[0].FullPass != 1 ||
		report.Mechanisms[0].GuardedPotential != 6000 {
		t.Fatalf("bad mechanism ranking: %+v", report.Mechanisms)
	}
	if len(report.ActionablePotential) != 2 ||
		report.ActionablePotential[0].Category != "proof_latch_candidate" ||
		report.ActionablePotential[0].Source != "evidence:wss_search_output_risk_gate" ||
		report.ActionablePotential[0].Tokens != 6000 ||
		report.ActionablePotential[0].TokenBasis != "full_pass_block_original_tokens" ||
		report.ActionablePotential[0].Decisions != 1 ||
		report.ActionablePotential[0].Mechanisms["captured_output"] != 1 ||
		report.ActionablePotential[0].ToolCommandClasses["rg_search"] != 1 ||
		report.ActionablePotential[0].ToolCommandClasses["git_show_stat"] != 1 ||
		report.ActionablePotential[1].Category != "unsafe_without_fresh_live_proof" ||
		report.ActionablePotential[1].Source != "evidence:wss_stateful_delta_mutation_proof_gate" ||
		report.ActionablePotential[1].Tokens != 3500 ||
		report.ActionablePotential[1].ToolCommandClasses["go_test"] != 1 {
		t.Fatalf("bad actionable potential rows: %+v", report.ActionablePotential)
	}
	if len(report.RequestShapes) != 2 ||
		report.RequestShapes[0].Shape != "full_history" ||
		report.RequestShapes[0].ProviderCacheTokens != 3000 ||
		report.RequestShapes[0].GuardedPotential != 6000 {
		t.Fatalf("bad shape rows: %+v", report.RequestShapes)
	}
	notes := strings.Join(report.Notes, "\n")
	if !strings.Contains(notes, "Provider-cache tokens") ||
		!strings.Contains(notes, "highest guarded_potential_tokens") {
		t.Fatalf("missing policy notes: %+v", report.Notes)
	}
}

func TestRunWSSLocalGapJSONAndText(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "wss-ok",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 1000, Final: 400, Saved: 600},
		DebugFacts: map[string]string{
			"wss.request_shape": "root",
		},
		EvidenceDecisions: []evidence.BlockDecision{{
			Mechanism:      "read_delta",
			ContentClass:   evidence.ContentPlain,
			Action:         evidence.ActionApplied,
			Reason:         "positive_net_savings",
			OriginalTokens: 1000,
			FinalTokens:    400,
			SavedTokens:    600,
			NetTokens:      600,
		}},
	})

	var stdout, stderr bytes.Buffer
	if code := runWSSLocalGap([]string{path, "--min-local-ratio=0.48"}, &stdout, &stderr); code != 0 {
		t.Fatalf("runWSSLocalGap text code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "S_local saved/ratio:") ||
		!strings.Contains(stdout.String(), "Positive-savings ratio:") ||
		!strings.Contains(stdout.String(), "60.00%") ||
		!strings.Contains(stdout.String(), "read_delta") {
		t.Fatalf("text output missing expected fields:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWSSLocalGap([]string{path, "--json", "--min-local-ratio=0.48"}, &stdout, &stderr); code != 0 {
		t.Fatalf("runWSSLocalGap json code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var report wssLocalGapReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("parse json output: %v\n%s", err, stdout.String())
	}
	if !report.GatePassed || report.LocalSavingsRatio != 0.6 || report.PositiveSavingsRatio != 0.6 {
		t.Fatalf("bad json report: %+v", report)
	}
}

func TestWSSLocalGapSinceAndSavedGate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path,
		dbg.RequestSummary{
			RequestID: "old",
			Timestamp: time.Date(2026, 6, 13, 9, 0, 0, 0, time.UTC),
			Path:      "/backend-api/codex/responses",
			RouteMode: "websocket_phasef",
			Tokens:    dbg.TokenCounts{Original: 1000, Final: 0, Saved: 1000},
		},
		dbg.RequestSummary{
			RequestID: "new",
			Timestamp: time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC),
			Path:      "/backend-api/codex/responses",
			RouteMode: "websocket_phasef",
			Tokens:    dbg.TokenCounts{Original: 1000, Final: 900, Saved: 100},
		},
	)

	report, err := loadWSSLocalGapReport(wssLocalGapFlags{
		path:          path,
		since:         time.Date(2026, 6, 13, 9, 30, 0, 0, time.UTC),
		minLocalSaved: 200,
	})
	if err != nil {
		t.Fatalf("loadWSSLocalGapReport() error = %v", err)
	}
	if report.PhaseFRequests != 1 ||
		report.OriginalTokens != 1000 ||
		report.LocalSavedTokens != 100 ||
		report.GatePassed ||
		len(report.GateFailures) != 1 ||
		!strings.Contains(report.GateFailures[0], "local_saved_tokens") {
		t.Fatalf("since/saved gate failed: %+v", report)
	}
}

func TestWSSLocalGapRequestGuardsExposeNoEvidenceAndMissingShapeFacts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path,
		dbg.RequestSummary{
			RequestID: "legacy-no-evidence",
			Path:      "/backend-api/codex/responses",
			RouteMode: "websocket_phasef",
			Tokens:    dbg.TokenCounts{Original: 9000, Final: 9000, Saved: 0},
		},
		dbg.RequestSummary{
			RequestID:    "bypassed-no-evidence",
			Path:         "/backend-api/codex/responses",
			RouteMode:    "websocket_phasef",
			BypassReason: "wss_tool_output_state_full_pass",
			Tokens:       dbg.TokenCounts{Original: 4000, Final: 4000, Saved: 0},
			DebugFacts: map[string]string{
				"wss.request_shape":        "delta",
				"wss.output_reduce_reason": "prompt_cache_prefix_full_pass",
				"wss.messages":             "1",
				"wss.tool_results":         "0",
				"wss.source_tool_bytes":    "0",
			},
		},
		dbg.RequestSummary{
			RequestID: "prefix-tools-no-evidence",
			Path:      "/backend-api/codex/responses",
			RouteMode: "websocket_phasef",
			Tokens:    dbg.TokenCounts{Original: 3000, Final: 3000, Saved: 0},
			DebugFacts: map[string]string{
				"wss.request_shape":                                  "root",
				"wss.output_reduce_reason":                           "prompt_cache_prefix_full_pass",
				"wss.messages":                                       "1",
				"wss.tool_results":                                   "0",
				"wss.source_tool_bytes":                              "0",
				"wss.tool_definition_bytes":                          "123",
				"wss.tool_definition_name_bytes":                     "14",
				"wss.tool_definition_description_bytes":              "51",
				"wss.tool_definition_parameters_bytes":               "41",
				"wss.tool_definition_other_bytes":                    "17",
				"wss.tool_definitions":                               "17",
				"wss.instructions_bytes":                             "45",
				"wss.tool_definition_default_keep":                   "12",
				"wss.tool_definition_default_keep_bytes":             "90",
				"wss.tool_definition_default_keep_description_bytes": "30",
				"wss.tool_definition_default_keep_parameters_bytes":  "24",
				"wss.tool_definition_default_keep_names":             "exec_command,apply_patch",
				"wss.tool_definition_nondefault":                     "4",
				"wss.tool_definition_nondefault_bytes":               "30",
				"wss.tool_definition_nondefault_description_bytes":   "21",
				"wss.tool_definition_nondefault_parameters_bytes":    "17",
				"wss.tool_definition_nondefault_names":               "request_user_input,get_goal",
				"wss.tool_definition_unnamed":                        "1",
				"wss.tool_definition_unnamed_bytes":                  "3",
			},
		},
		dbg.RequestSummary{
			RequestID: "tool-output-disabled-predicate",
			Path:      "/backend-api/codex/responses",
			RouteMode: "websocket_phasef",
			Tokens:    dbg.TokenCounts{Original: 5000, Final: 5000, Saved: 0},
			DebugFacts: map[string]string{
				"wss.request_shape":                       "delta",
				"wss.output_reduce_reason":                "disabled",
				"wss.output_reduce_disabled_predicate":    "tool_output_context",
				"wss.output_reduce_input_tokens":          "3210",
				"wss.output_reduce_eligible_input_tokens": "0",
				"wss.messages":                            "1",
				"wss.tool_results":                        "1",
				"wss.source_tool_bytes":                   "900",
				"wss.tool_command_classes":                "rg_search=2",
			},
		},
	)

	report, err := loadWSSLocalGapReport(wssLocalGapFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSLocalGapReport() error = %v", err)
	}
	missingShape := wssLocalGapRequestGuardRow{}
	bypass := wssLocalGapRequestGuardRow{}
	noToolResults := wssLocalGapRequestGuardRow{}
	noOutputReduce := wssLocalGapRequestGuardRow{}
	disabledPredicate := wssLocalGapRequestGuardRow{}
	for _, row := range report.RequestGuards {
		switch row.Guard {
		case "wss.request_shape=(missing)":
			missingShape = row
		case "bypass_reason=wss_tool_output_state_full_pass":
			bypass = row
		case "no_evidence:wss.tool_results=0":
			noToolResults = row
		case "no_evidence:wss.output_reduce_reason=prompt_cache_prefix_full_pass":
			noOutputReduce = row
		case "no_evidence:wss.output_reduce_disabled_predicate=tool_output_context":
			disabledPredicate = row
		}
	}
	if missingShape.Requests != 1 ||
		missingShape.OriginalTokens != 9000 ||
		missingShape.NoEvidenceRequests != 1 ||
		missingShape.NoEvidenceOrigTokens != 9000 ||
		missingShape.RequestShapes["unknown"] != 1 {
		t.Fatalf("missing-shape no-evidence guard row mismatch: %+v", missingShape)
	}
	if bypass.Requests != 1 ||
		bypass.OriginalTokens != 4000 ||
		bypass.ZeroSavingsOrigTokens != 4000 ||
		bypass.NoEvidenceOrigTokens != 4000 ||
		bypass.RequestShapes["delta"] != 1 {
		t.Fatalf("bypass no-evidence guard row mismatch: %+v", bypass)
	}
	if noToolResults.Requests != 2 ||
		noToolResults.OriginalTokens != 7000 ||
		noToolResults.NoEvidenceOrigTokens != 7000 ||
		noToolResults.RequestShapes["delta"] != 1 ||
		noToolResults.RequestShapes["root"] != 1 {
		t.Fatalf("no-tool-results no-evidence guard row mismatch: %+v", noToolResults)
	}
	if noOutputReduce.Requests != 2 ||
		noOutputReduce.OriginalTokens != 7000 ||
		noOutputReduce.NoEvidenceOrigTokens != 7000 ||
		noOutputReduce.RequestShapes["delta"] != 1 ||
		noOutputReduce.RequestShapes["root"] != 1 {
		t.Fatalf("no-output-reduce no-evidence guard row mismatch: %+v", noOutputReduce)
	}
	if disabledPredicate.Requests != 1 ||
		disabledPredicate.OriginalTokens != 5000 ||
		disabledPredicate.NoEvidenceOrigTokens != 5000 ||
		disabledPredicate.RequestShapes["delta"] != 1 {
		t.Fatalf("disabled-predicate no-evidence guard row mismatch: %+v", disabledPredicate)
	}
	if len(report.ActionablePotential) != 4 ||
		report.ActionablePotential[0].Category != "needs_instrumentation" ||
		report.ActionablePotential[0].Source != "no_evidence:wss.request_shape_missing" ||
		report.ActionablePotential[0].TokenBasis != "request_original_tokens" ||
		report.ActionablePotential[0].Tokens != 9000 ||
		report.ActionablePotential[0].Requests != 1 ||
		report.ActionablePotential[1].Category != "not_output_reduce_target" ||
		report.ActionablePotential[1].Source != "no_evidence:wss.output_reduce_disabled_predicate=tool_output_context" ||
		report.ActionablePotential[1].Tokens != 5000 ||
		report.ActionablePotential[1].Requests != 1 ||
		report.ActionablePotential[1].OutputReduceInputTokens != 3210 ||
		report.ActionablePotential[1].OutputReduceEligibleInputTokens != 0 ||
		report.ActionablePotential[1].ToolCommandClasses["rg_search"] != 2 ||
		report.ActionablePotential[2].Category != "prefix_safe_new_mechanism_required" ||
		report.ActionablePotential[2].Source != "no_evidence:wss.output_reduce_reason=prompt_cache_prefix_full_pass" ||
		report.ActionablePotential[2].Tokens != 4000 ||
		report.ActionablePotential[2].Requests != 1 ||
		report.ActionablePotential[3].Category != "prefix_safe_new_mechanism_required" ||
		report.ActionablePotential[3].Source != "no_evidence:prompt_cache_prefix_tools_and_instructions" ||
		report.ActionablePotential[3].Tokens != 3000 ||
		report.ActionablePotential[3].Requests != 1 ||
		report.ActionablePotential[3].PrefixToolDefinitionBytes != 123 ||
		report.ActionablePotential[3].PrefixInstructionBytes != 45 ||
		report.ActionablePotential[3].PrefixToolNameBytes != 14 ||
		report.ActionablePotential[3].PrefixToolDescriptionBytes != 51 ||
		report.ActionablePotential[3].PrefixToolParametersBytes != 41 ||
		report.ActionablePotential[3].PrefixToolOtherBytes != 17 ||
		report.ActionablePotential[3].PrefixToolDefinitions != 17 ||
		report.ActionablePotential[3].PrefixMaxToolDefinitions != 17 ||
		report.ActionablePotential[3].PrefixDefaultKeepTools != 12 ||
		report.ActionablePotential[3].PrefixDefaultKeepBytes != 90 ||
		report.ActionablePotential[3].PrefixDefaultDescriptionBytes != 30 ||
		report.ActionablePotential[3].PrefixDefaultParametersBytes != 24 ||
		report.ActionablePotential[3].PrefixDefaultKeepNames["exec_command"] != 1 ||
		report.ActionablePotential[3].PrefixDefaultKeepNames["apply_patch"] != 1 ||
		report.ActionablePotential[3].PrefixNonDefaultTools != 4 ||
		report.ActionablePotential[3].PrefixNonDefaultBytes != 30 ||
		report.ActionablePotential[3].PrefixNonDefaultDescriptionBytes != 21 ||
		report.ActionablePotential[3].PrefixNonDefaultParametersBytes != 17 ||
		report.ActionablePotential[3].PrefixNonDefaultNames["request_user_input"] != 1 ||
		report.ActionablePotential[3].PrefixNonDefaultNames["get_goal"] != 1 ||
		report.ActionablePotential[3].PrefixUnnamedTools != 1 ||
		report.ActionablePotential[3].PrefixUnnamedBytes != 3 {
		t.Fatalf("bad no-evidence actionable rows: %+v", report.ActionablePotential)
	}
}

func TestWSSLocalGapNoEvidenceActionClassifiesDefaultKeepPrefix(t *testing.T) {
	t.Parallel()

	category, source, policy, nextStep := wssLocalGapNoEvidenceAction(dbg.RequestSummary{
		DebugFacts: map[string]string{
			"wss.request_shape":                      "delta",
			"wss.output_reduce_reason":               "prompt_cache_prefix_full_pass",
			"wss.tool_definition_bytes":              "12000",
			"wss.tool_definition_default_keep_bytes": "12000",
			"wss.tool_definition_nondefault_bytes":   "0",
			"wss.instructions_bytes":                 "9000",
			"wss.tool_definition_default_keep_names": "exec_command,request_user_input,update_goal",
			"wss.tool_definition_nondefault_names":   "",
			"wss.tool_definition_default_keep":       "3",
			"wss.tool_definition_nondefault":         "0",
			"wss.tool_definition_unnamed":            "0",
			"wss.tool_definition_unnamed_bytes":      "0",
			"wss.output_reduce_disabled_predicate":   "prompt_cache_prefix",
			"wss.tool_definition_description_bytes":  "6000",
			"wss.tool_definition_parameters_bytes":   "5000",
		},
	}, "fact")
	if category != "prefix_capability_context_guarded" ||
		source != "no_evidence:prompt_cache_prefix_default_keep_tools_and_instructions" ||
		!strings.Contains(policy, "model-facing capability context") ||
		!strings.Contains(policy, "suppress command_execution") ||
		!strings.Contains(nextStep, "keep this mass in the product path") {
		t.Fatalf("default-keep prefix action mismatch: category=%q source=%q policy=%q next=%q", category, source, policy, nextStep)
	}
}

func TestWSSLocalGapNoEvidenceActionClassifiesPreviousResponseBypassAsProofBlocked(t *testing.T) {
	t.Parallel()

	category, source, policy, nextStep := wssLocalGapNoEvidenceAction(dbg.RequestSummary{
		BypassReason: "wss_previous_response_tool_output_full_pass",
		DebugFacts: map[string]string{
			"wss.request_shape":                    "delta",
			"wss.output_reduce_reason":             "disabled",
			"wss.output_reduce_disabled_predicate": "tool_output_context",
			"wss.tool_results":                     "1",
		},
	}, "fact")
	if category != "unsafe_without_fresh_live_proof" ||
		source != "no_evidence:bypass_reason=wss_previous_response_tool_output_full_pass" ||
		!strings.Contains(policy, "protects Codex server state") ||
		!strings.Contains(nextStep, "downstream-delta live proof") {
		t.Fatalf("previous-response bypass action mismatch: category=%q source=%q policy=%q next=%q", category, source, policy, nextStep)
	}
}

func TestWSSLocalGapResolvedLegacyShapeDoesNotBecomeShapeInstrumentationBlocker(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID:              "legacy-delta-no-shape-fact",
		Path:                   "/backend-api/codex/responses",
		RouteMode:              "websocket_phasef",
		PreviousResponseIDUsed: true,
		Tokens:                 dbg.TokenCounts{Original: 6000, Final: 6000, Saved: 0},
		DebugFacts: map[string]string{
			"wss.output_reduce_reason":                "disabled",
			"wss.output_reduce_disabled_predicate":    "tool_output_context",
			"wss.output_reduce_input_tokens":          "5900",
			"wss.output_reduce_eligible_input_tokens": "0",
			"wss.tool_results":                        "1",
			"wss.source_tool_bytes":                   "0",
		},
	})

	report, err := loadWSSLocalGapReport(wssLocalGapFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSLocalGapReport() error = %v", err)
	}
	if report.RequestShapeSources["legacy_previous_response_id"] != 1 {
		t.Fatalf("legacy previous_response_id source not recorded: %+v", report.RequestShapeSources)
	}
	for _, row := range report.RequestGuards {
		if row.Guard == "wss.request_shape=(missing)" {
			t.Fatalf("resolved legacy shape must not be a shape-missing blocker: %+v", row)
		}
	}
	if len(report.ActionablePotential) != 1 ||
		report.ActionablePotential[0].Category != "not_output_reduce_target" ||
		report.ActionablePotential[0].Source != "no_evidence:wss.output_reduce_disabled_predicate=tool_output_context" ||
		report.ActionablePotential[0].RequestShapes["delta"] != 1 ||
		report.ActionablePotential[0].OutputReduceInputTokens != 5900 {
		t.Fatalf("resolved legacy shape should classify by concrete predicate: %+v", report.ActionablePotential)
	}
}

func TestParseWSSLocalGapFlagsRejectsBadValues(t *testing.T) {
	t.Parallel()

	if _, err := parseWSSLocalGapFlags([]string{"decisions.jsonl", "--min-local-ratio=1.1"}); err == nil {
		t.Fatal("expected bad ratio error")
	}
	if _, err := parseWSSLocalGapFlags([]string{"decisions.jsonl", "--min-local-saved=-1"}); err == nil {
		t.Fatal("expected bad saved-token error")
	}
	if _, err := parseWSSLocalGapFlags([]string{"one.jsonl", "two.jsonl"}); err == nil {
		t.Fatal("expected multiple log error")
	}
}

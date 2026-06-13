package main

import (
	"path/filepath"
	"testing"

	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/evidence"
)

func TestWSSLocalGapClassifiesSourceToolOutputFullPassEvidence(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "source-delta-full-pass",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 6000, Final: 6000, Saved: 0},
		DebugFacts: map[string]string{
			"wss.request_shape":        "delta",
			"wss.tool_command_classes": "read_like=1",
		},
		EvidenceDecisions: []evidence.BlockDecision{{
			Mechanism:      "captured_output",
			ContentClass:   evidence.ContentCode,
			Action:         evidence.ActionFullPass,
			Reason:         "wss_source_tool_output_full_pass",
			OriginalTokens: 4200,
			FinalTokens:    4200,
		}},
	})

	report, err := loadWSSLocalGapReport(wssLocalGapFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSLocalGapReport() error = %v", err)
	}
	if report.NoEvidenceRequests != 0 || report.NoEvidenceOrigTokens != 0 {
		t.Fatalf("source full-pass evidence must not be counted as no-evidence: %+v", report)
	}
	if len(report.ActionablePotential) != 1 {
		t.Fatalf("expected one actionable row, got %+v", report.ActionablePotential)
	}
	row := report.ActionablePotential[0]
	if row.Category != "source_context_guard" ||
		row.Source != "evidence:wss_source_tool_output_full_pass" ||
		row.Tokens != 4200 ||
		row.Decisions != 1 ||
		row.ToolCommandClasses["read_like"] != 1 {
		t.Fatalf("bad source-context actionable row: %+v", row)
	}
}

func TestWSSLocalGapClassifiesNoEvidenceLargeSourceBytesAsSourceGuard(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "source-delta-no-evidence",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 10859, Final: 10859, Saved: 0},
		DebugFacts: map[string]string{
			"wss.request_shape":                    "delta",
			"wss.previous_response_id":             "true",
			"wss.output_reduce_reason":             "disabled",
			"wss.output_reduce_disabled_predicate": "tool_output_context",
			"wss.tool_results":                     "1",
			"wss.source_tool_bytes":                "5321",
			"wss.source_tool_max_bytes":            "5321",
			"wss.tool_command_classes":             "read_like=1",
		},
	})

	report, err := loadWSSLocalGapReport(wssLocalGapFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSLocalGapReport() error = %v", err)
	}
	if len(report.ActionablePotential) != 1 {
		t.Fatalf("expected one actionable row, got %+v", report.ActionablePotential)
	}
	row := report.ActionablePotential[0]
	if row.Category != "source_context_guard" ||
		row.Source != "no_evidence:wss.source_tool_bytes>=4096" ||
		row.Tokens != 10859 ||
		row.Requests != 1 ||
		row.ToolCommandClasses["read_like"] != 1 ||
		row.Policy == "" ||
		row.NextStep == "" {
		t.Fatalf("bad no-evidence source-context row: %+v", row)
	}
}

func TestWSSLocalGapClassifiesEmptyToolOutputContext(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "empty-tool-output",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 9000, Final: 9000, Saved: 0},
		DebugFacts: map[string]string{
			"wss.request_shape":                       "delta",
			"wss.output_reduce_reason":                "disabled",
			"wss.output_reduce_disabled_predicate":    "tool_output_context",
			"wss.messages":                            "1",
			"wss.tool_results":                        "1",
			"wss.tool_result_bytes":                   "102",
			"wss.tool_result_output_bytes":            "0",
			"wss.source_tool_bytes":                   "0",
			"wss.tool_command_classes":                "git_status=1",
			"wss.output_reduce_input_tokens":          "9000",
			"wss.output_reduce_eligible_input_tokens": "0",
		},
	})

	report, err := loadWSSLocalGapReport(wssLocalGapFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSLocalGapReport() error = %v", err)
	}
	if len(report.ActionablePotential) != 1 {
		t.Fatalf("expected one actionable row, got %+v", report.ActionablePotential)
	}
	row := report.ActionablePotential[0]
	if row.Category != "empty_tool_output_context" ||
		row.Source != "no_evidence:empty_tool_output" ||
		row.Tokens != 9000 ||
		row.Requests != 1 ||
		row.ToolCommandClasses["git_status"] != 1 {
		t.Fatalf("bad empty-tool-output row: %+v", row)
	}
}

func TestWSSLocalGapClassifiesMissingPayloadBytesAsPrefixBound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "prefix-bound-tool-output",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 9386, Final: 9386, Saved: 0},
		DebugFacts: map[string]string{
			"wss.request_shape":                    "delta",
			"wss.previous_response_id":             "true",
			"wss.output_reduce_reason":             "disabled",
			"wss.output_reduce_disabled_predicate": "tool_output_context",
			"wss.messages":                         "1",
			"wss.tool_results":                     "1",
			"wss.source_tool_bytes":                "0",
			"wss.prefix_estimated_tokens":          "9058",
			"wss.prefix_total_bytes":               "36232",
			"wss.tool_command_classes":             "git_status=1",
		},
	})

	report, err := loadWSSLocalGapReport(wssLocalGapFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSLocalGapReport() error = %v", err)
	}
	if len(report.ActionablePotential) != 1 {
		t.Fatalf("expected one actionable row, got %+v", report.ActionablePotential)
	}
	row := report.ActionablePotential[0]
	if row.Category != "prefix_bound_tool_output_context" ||
		row.Source != "no_evidence:missing_tool_output_bytes_prefix_bound" ||
		row.Tokens != 9386 ||
		row.Requests != 1 ||
		row.PrefixEstimatedTokens != 9058 ||
		row.ToolCommandClasses["git_status"] != 1 ||
		row.Policy == "" ||
		row.NextStep == "" {
		t.Fatalf("bad prefix-bound row: %+v", row)
	}
}

func TestWSSLocalGapClassifiesTinyPayloadBytesAsSmallToolOutput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "tiny-tool-output",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 8427, Final: 8427, Saved: 0},
		DebugFacts: map[string]string{
			"wss.request_shape":                       "delta",
			"wss.previous_response_id":                "true",
			"wss.output_reduce_reason":                "disabled",
			"wss.output_reduce_disabled_predicate":    "tool_output_context",
			"wss.messages":                            "1",
			"wss.tool_results":                        "1",
			"wss.tool_result_bytes":                   "152",
			"wss.tool_result_output_bytes":            "49",
			"wss.source_tool_bytes":                   "0",
			"wss.tool_command_classes":                "git_diff_stat=1",
			"wss.output_reduce_input_tokens":          "8427",
			"wss.output_reduce_eligible_input_tokens": "0",
		},
	})

	report, err := loadWSSLocalGapReport(wssLocalGapFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSLocalGapReport() error = %v", err)
	}
	if len(report.ActionablePotential) != 1 {
		t.Fatalf("expected one actionable row, got %+v", report.ActionablePotential)
	}
	row := report.ActionablePotential[0]
	if row.Category != "small_tool_output_context" ||
		row.Source != "no_evidence:wss.tool_result_output_bytes<=255" ||
		row.Tokens != 8427 ||
		row.Requests != 1 ||
		row.OutputReduceInputTokens != 8427 ||
		row.ToolCommandClasses["git_diff_stat"] != 1 ||
		row.Policy == "" ||
		row.NextStep == "" {
		t.Fatalf("bad small-tool-output row: %+v", row)
	}
}

func TestWSSLocalGapClassifiesNoEvidenceDownstreamGuardBeforeOutputReduceDisabled(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "full-history-downstream-guard-no-evidence",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 12951, Final: 12951, Saved: 0},
		DebugFacts: map[string]string{
			"wss.request_shape":                    "full_history",
			"wss.output_reduce_reason":             "disabled",
			"wss.output_reduce_disabled_predicate": "tool_output_context",
			"wss.structured_mutation_guard":        "wss_full_history_downstream_delta_proof_gate",
			"wss.tool_results":                     "1",
			"wss.tool_result_bytes":                "30662",
			"wss.tool_result_output_bytes":         "30662",
			"wss.source_tool_bytes":                "0",
			"wss.tool_command_classes":             "git_status=2,rg_search=1",
		},
	})

	report, err := loadWSSLocalGapReport(wssLocalGapFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSLocalGapReport() error = %v", err)
	}
	if len(report.ActionablePotential) != 1 {
		t.Fatalf("expected one actionable row, got %+v", report.ActionablePotential)
	}
	row := report.ActionablePotential[0]
	if row.Category != "unsafe_without_fresh_live_proof" ||
		row.Source != "no_evidence:wss.structured_mutation_guard=wss_full_history_downstream_delta_proof_gate" ||
		row.Tokens != 12951 ||
		row.RequestShapes["full_history"] != 1 ||
		row.ToolCommandClasses["git_status"] != 2 ||
		row.ToolCommandClasses["rg_search"] != 1 ||
		row.OutputReduceInputTokens != 0 ||
		row.OutputReduceEligibleInputTokens != 0 {
		t.Fatalf("bad downstream-guard row: %+v", row)
	}
}

package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
)

func TestWSSPostEditInventoryBlocksWithoutExactState(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "post-edit-missing-state",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 12000, Final: 12000},
		DebugFacts: map[string]string{
			"wss.request_shape":         "full_history",
			"wss.tool_command_classes":  "read_like=1",
			"wss.source_tool_bytes":     "8000",
			"wss.read_after_edit":       "true",
			"wss.read_after_edit_count": "1",
			"wss.read_full_count":       "1",
			"wss.read_file_path_hash":   "path-hash",
			"wss.read_range_hash":       "range-hash",
		},
	})

	report, err := loadWSSPostEditInventory(wssPostEditInventoryFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSPostEditInventory() error = %v", err)
	}
	if report.Verdict != "post_edit_exact_state_missing" ||
		report.PostEditReadRequests != 1 ||
		report.PostEditFullReadRequests != 1 ||
		report.MissingExactStateRequests != 1 ||
		report.PostEditCandidateTokensEstimate != 2000 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if !strings.Contains(report.NextAction, "predictive_post_edit shadow-only") {
		t.Fatalf("next action should keep predictive_post_edit shadow-only: %q", report.NextAction)
	}
}

func TestWSSPostEditInventoryFindsRepeatedExactStateCandidates(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	baseFacts := map[string]string{
		"wss.request_shape":         "full_history",
		"wss.tool_command_classes":  "read_like=1",
		"wss.source_tool_bytes":     "4000",
		"wss.read_after_edit":       "true",
		"wss.read_after_edit_count": "1",
		"wss.read_full_count":       "1",
		"wss.read_file_path_hash":   "path-hash",
		"wss.read_range_hash":       "range-hash",
		"wss.file_hash_after":       "file-hash-after",
		"wss.edit_turn_seq":         "7",
		"wss.changed_range":         "lines:10:12",
	}
	writeJSONLFile(t, path,
		dbg.RequestSummary{
			RequestID:  "post-edit-first",
			Path:       "/backend-api/codex/responses",
			RouteMode:  "websocket_phasef",
			Tokens:     dbg.TokenCounts{Original: 9000, Final: 9000},
			DebugFacts: cloneStringMapForPostEditTest(baseFacts),
		},
		dbg.RequestSummary{
			RequestID:  "post-edit-repeat",
			Path:       "/backend-api/codex/responses",
			RouteMode:  "websocket_phasef",
			Tokens:     dbg.TokenCounts{Original: 9000, Final: 8500, Saved: 500},
			DebugFacts: cloneStringMapForPostEditTest(baseFacts),
		},
	)

	report, err := loadWSSPostEditInventory(wssPostEditInventoryFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSPostEditInventory() error = %v", err)
	}
	if report.Verdict != "shadow_exact_repeat_ready" ||
		report.ExactStateRequests != 2 ||
		report.MissingExactStateRequests != 0 ||
		report.RepeatedPostEditStateCandidates != 1 ||
		report.ExactStateFacts["wss.file_hash_after"] != 2 ||
		report.LocalSavedTokens != 500 {
		t.Fatalf("unexpected repeated exact-state report: %+v", report)
	}
}

func TestWSSPostEditInventoryCountsPatchContextTelemetry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path,
		dbg.RequestSummary{
			RequestID: "patch-first",
			Path:      "/backend-api/codex/responses",
			RouteMode: "websocket_phasef",
			Tokens:    dbg.TokenCounts{Original: 7000, Final: 7000},
			DebugFacts: map[string]string{
				"wss.request_shape":            "full_history",
				"wss.tool_command_classes":     "git_diff_stat=1",
				"wss.patch_context_candidate":  "true",
				"wss.patch_context_kind":       "git_diff_stat",
				"wss.patch_context_hash":       "patch-hash",
				"wss.patch_context_bytes":      "2400",
				"wss.tool_result_output_bytes": "2400",
			},
		},
		dbg.RequestSummary{
			RequestID: "patch-repeat",
			Path:      "/backend-api/codex/responses",
			RouteMode: "websocket_phasef",
			Tokens:    dbg.TokenCounts{Original: 7000, Final: 6800, Saved: 200},
			DebugFacts: map[string]string{
				"wss.request_shape":           "full_history",
				"wss.tool_command_classes":    "git_diff_stat=1",
				"wss.patch_context_candidate": "true",
				"wss.patch_context_kind":      "git_diff_stat",
				"wss.patch_context_hash":      "patch-hash",
				"wss.patch_context_bytes":     "2400",
			},
		},
	)

	report, err := loadWSSPostEditInventory(wssPostEditInventoryFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSPostEditInventory() error = %v", err)
	}
	if report.Verdict != "shadow_exact_repeat_ready" ||
		report.PatchContextRequests != 2 ||
		report.PatchContextExactTelemetryRequests != 2 ||
		report.MissingPatchContextTelemetry != 0 ||
		report.RepeatedPatchContextCandidates != 1 ||
		report.PatchContextTokensEstimate != 1200 ||
		report.PatchContextKinds["git_diff_stat"] != 2 {
		t.Fatalf("unexpected patch-context report: %+v", report)
	}
}

func TestWSSPostEditInventoryPatchRiskBlocksPromotion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "patch-conflict",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 7000, Final: 7000},
		DebugFacts: map[string]string{
			"wss.tool_command_classes":    "git_diff=1",
			"wss.patch_context_candidate": "true",
			"wss.patch_context_kind":      "git_diff",
			"wss.patch_context_hash":      "patch-hash",
			"wss.patch_context_conflict":  "true",
		},
	})

	report, err := loadWSSPostEditInventory(wssPostEditInventoryFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSPostEditInventory() error = %v", err)
	}
	if report.Verdict != "promotion_blocked_patch_risk" ||
		report.PatchContextRiskRequests != 1 ||
		report.RiskReasons["wss.patch_context_conflict"] != 1 {
		t.Fatalf("unexpected patch risk report: %+v", report)
	}
}

func TestRunWSSPostEditInventoryJSONAndRequireExactState(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "json-missing",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 4000, Final: 4000},
		DebugFacts: map[string]string{
			"wss.tool_command_classes": "read_like=1",
			"wss.read_after_edit":      "true",
			"wss.source_tool_bytes":    "1600",
		},
	})

	var stdout, stderr bytes.Buffer
	code := runWSSPostEditInventory([]string{path, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run json code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var report wssPostEditInventoryReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode json output: %v\nstdout=%s", err, stdout.String())
	}
	if report.Verdict != "post_edit_exact_state_missing" || report.PostEditReadRequests != 1 {
		t.Fatalf("unexpected json report: %+v", report)
	}

	stdout.Reset()
	stderr.Reset()
	code = runWSSPostEditInventory([]string{path, "--require-exact-state"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("require-exact-state code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "exact-state telemetry missing") {
		t.Fatalf("stderr should explain exact-state miss, got %q", stderr.String())
	}
}

func cloneStringMapForPostEditTest(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

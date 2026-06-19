package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/evidence"
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

func TestWSSPostEditInventoryKeepsEditTurnLineageSeparate(t *testing.T) {
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
		"wss.changed_range":         "lines:10:12",
	}
	firstFacts := cloneStringMapForPostEditTest(baseFacts)
	firstFacts["wss.edit_turn_seq"] = "7"
	secondFacts := cloneStringMapForPostEditTest(baseFacts)
	secondFacts["wss.edit_turn_seq"] = "8"
	writeJSONLFile(t, path,
		dbg.RequestSummary{
			RequestID:  "post-edit-first-lineage",
			Path:       "/backend-api/codex/responses",
			RouteMode:  "websocket_phasef",
			Tokens:     dbg.TokenCounts{Original: 9000, Final: 9000},
			DebugFacts: firstFacts,
		},
		dbg.RequestSummary{
			RequestID:  "post-edit-second-lineage",
			Path:       "/backend-api/codex/responses",
			RouteMode:  "websocket_phasef",
			Tokens:     dbg.TokenCounts{Original: 9000, Final: 9000},
			DebugFacts: secondFacts,
		},
	)

	report, err := loadWSSPostEditInventory(wssPostEditInventoryFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSPostEditInventory() error = %v", err)
	}
	if report.Verdict != "shadow_measure_only_no_repeat" ||
		report.ExactStateRequests != 2 ||
		report.RepeatedPostEditStateCandidates != 0 {
		t.Fatalf("different edit turns must not collapse into one repeat candidate: %+v", report)
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

func TestWSSPostEditInventoryCountsMultiHashPatchContextTelemetry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path,
		dbg.RequestSummary{
			RequestID: "patch-multi-first",
			Path:      "/backend-api/codex/responses",
			RouteMode: "websocket_phasef",
			Tokens:    dbg.TokenCounts{Original: 9000, Final: 9000},
			DebugFacts: map[string]string{
				"wss.request_shape":            "full_history",
				"wss.tool_command_classes":     "git_diff=1,git_show_stat=1",
				"wss.patch_context_candidate":  "true",
				"wss.patch_context_kinds":      "git_diff=1,git_show_stat=1",
				"wss.patch_context_hash_count": "2",
				"wss.patch_context_hashes":     "hash-a,hash-b",
				"wss.patch_context_bytes":      "3600",
				"wss.tool_result_output_bytes": "3600",
			},
		},
		dbg.RequestSummary{
			RequestID: "patch-multi-repeat",
			Path:      "/backend-api/codex/responses",
			RouteMode: "websocket_phasef",
			Tokens:    dbg.TokenCounts{Original: 9000, Final: 8600, Saved: 400},
			DebugFacts: map[string]string{
				"wss.request_shape":            "full_history",
				"wss.tool_command_classes":     "git_diff=1,git_show_stat=1",
				"wss.patch_context_candidate":  "true",
				"wss.patch_context_kinds":      "git_diff=1,git_show_stat=1",
				"wss.patch_context_hash_count": "2",
				"wss.patch_context_hashes":     "hash-a,hash-b",
				"wss.patch_context_bytes":      "3600",
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
		report.PatchContextTokensEstimate != 1800 {
		t.Fatalf("unexpected multi-hash patch-context report: %+v", report)
	}
}

func TestWSSPostEditInventoryCountsAppliedRepeatedDiffEvidence(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "patch-repeat-applied",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 9000, Final: 7800, Saved: 1200},
		DebugFacts: map[string]string{
			"wss.request_shape":           "full_history",
			"wss.tool_command_classes":    "git_diff=1",
			"wss.patch_context_candidate": "true",
			"wss.patch_context_kind":      "git_diff",
			"wss.patch_context_hash":      "patch-hash",
			"wss.patch_context_bytes":     "4800",
		},
		EvidenceDecisions: []evidence.BlockDecision{{
			Mechanism:      "repeated_tool_output",
			CommandClass:   "git_diff",
			ContentClass:   evidence.ContentDiff,
			SafetyClass:    evidence.SafetyExact,
			Action:         evidence.ActionApplied,
			Reason:         "positive_net_savings",
			OriginalTokens: 1500,
			FinalTokens:    300,
			SavedTokens:    1200,
			NetTokens:      1200,
		}},
	})

	report, err := loadWSSPostEditInventory(wssPostEditInventoryFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSPostEditInventory() error = %v", err)
	}
	if report.Verdict != "product_exact_repeat_active" ||
		report.PatchContextRepeatedApplied != 1 ||
		report.PatchContextRepeatedSavedTokens != 1200 ||
		report.PerLog[0].PatchContextRepeatedApplied != 1 {
		t.Fatalf("unexpected applied repeated patch report: %+v", report)
	}
}

func TestWSSPostEditInventoryRiskBlocksAppliedRepeatedDiffEvidence(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "patch-repeat-risk",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 9000, Final: 7800, Saved: 1200},
		DebugFacts: map[string]string{
			"wss.tool_command_classes":    "git_diff=1",
			"wss.patch_context_candidate": "true",
			"wss.patch_context_kind":      "git_diff",
			"wss.patch_context_hash":      "patch-hash",
			"wss.patch_context_conflict":  "true",
		},
		EvidenceDecisions: []evidence.BlockDecision{{
			Mechanism:      "repeated_tool_output",
			CommandClass:   "git_diff",
			ContentClass:   evidence.ContentDiff,
			SafetyClass:    evidence.SafetyExact,
			Action:         evidence.ActionApplied,
			Reason:         "positive_net_savings",
			OriginalTokens: 1500,
			FinalTokens:    300,
			SavedTokens:    1200,
			NetTokens:      1200,
		}},
	})

	report, err := loadWSSPostEditInventory(wssPostEditInventoryFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSPostEditInventory() error = %v", err)
	}
	if report.Verdict != "promotion_blocked_patch_risk" ||
		report.PatchContextRepeatedApplied != 0 ||
		report.PatchContextRepeatedSavedTokens != 0 ||
		report.PatchContextRiskRequests != 1 {
		t.Fatalf("unexpected risk-blocked applied patch report: %+v", report)
	}
}

func TestWSSPostEditInventoryDoesNotCountRepeatedCodeEvidenceAsPatchContext(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "code-repeat-applied",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 9000, Final: 7800, Saved: 1200},
		EvidenceDecisions: []evidence.BlockDecision{{
			Mechanism:      "repeated_tool_output",
			CommandClass:   "other",
			ContentClass:   evidence.ContentCode,
			SafetyClass:    evidence.SafetyExact,
			Action:         evidence.ActionApplied,
			Reason:         "positive_net_savings",
			OriginalTokens: 1500,
			FinalTokens:    300,
			SavedTokens:    1200,
			NetTokens:      1200,
		}},
	})

	report, err := loadWSSPostEditInventory(wssPostEditInventoryFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSPostEditInventory() error = %v", err)
	}
	if report.PatchContextRepeatedApplied != 0 ||
		report.PatchContextRepeatedSavedTokens != 0 ||
		report.Verdict != "candidate_telemetry_missing" {
		t.Fatalf("code repeated-output must not count as patch context: %+v", report)
	}
}

func TestWSSPostEditInventoryPatchRiskBlocksPromotionAndRepeatCandidate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path,
		dbg.RequestSummary{
			RequestID: "patch-conflict-first",
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
		},
		dbg.RequestSummary{
			RequestID: "patch-conflict-repeat",
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
		},
	)

	report, err := loadWSSPostEditInventory(wssPostEditInventoryFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSPostEditInventory() error = %v", err)
	}
	if report.Verdict != "promotion_blocked_patch_risk" ||
		report.PatchContextExactTelemetryRequests != 2 ||
		report.RepeatedPatchContextCandidates != 0 ||
		report.PatchContextRiskRequests != 2 ||
		report.RiskReasons["wss.patch_context_conflict"] != 2 {
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

func TestRunWSSPostEditInventoryHelpAndFlagErrors(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	if code := runWSSPostEditInventory([]string{"--help"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "wss-post-edit-inventory") {
		t.Fatalf("help code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runWSSPostEditInventory(nil, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("missing path code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	for _, args := range [][]string{
		{"--unknown"},
		{"--since=not-a-time", "decisions.jsonl"},
		{"a", "b"},
	} {
		stdout.Reset()
		stderr.Reset()
		if code := runWSSPostEditInventory(args, &stdout, &stderr); code != 2 {
			t.Fatalf("args %v should fail parse with code 2, got %d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}
}

func TestWSSPostEditInventorySinceFileAndNoSurfaceText(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path,
		dbg.RequestSummary{
			RequestID: "old-row",
			Timestamp: time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC),
			Path:      "/backend-api/codex/responses",
			RouteMode: "websocket_phasef",
			Tokens:    dbg.TokenCounts{Original: 1000, Final: 1000},
			DebugFacts: map[string]string{
				"wss.tool_command_classes": "read_like=1",
				"wss.read_after_edit":      "true",
			},
		},
		dbg.RequestSummary{
			RequestID: "new-no-surface",
			Timestamp: time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC),
			Path:      "/backend-api/codex/responses",
			RouteMode: "websocket_phasef",
			Tokens:    dbg.TokenCounts{Original: 2000, Final: 2000},
			DebugFacts: map[string]string{
				"wss.request_shape":        "root",
				"wss.tool_command_classes": "go_test=1",
			},
		},
	)
	sinceFile := filepath.Join(dir, "since.txt")
	if err := os.WriteFile(sinceFile, []byte("2026-06-18T12:00:00Z\n"), 0o644); err != nil {
		t.Fatalf("write since file: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runWSSPostEditInventory([]string{path, "--since-file", sinceFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run since-file code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Verdict: no_post_edit_or_patch_surface") ||
		!strings.Contains(out, "Provider cached tokens:") {
		t.Fatalf("text output missing no-surface verdict:\n%s", out)
	}
}

func cloneStringMapForPostEditTest(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

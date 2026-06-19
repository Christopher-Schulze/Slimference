package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/evidence"
)

func TestWSSFirstReadInventoryBlocksPromotionWithoutDependencyTrace(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "first-read-source",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 12000, Final: 12000, Saved: 0},
		DebugFacts: map[string]string{
			"wss.request_shape":            "delta",
			"wss.tool_command_classes":     "read_like=1",
			"wss.source_tool_results":      "1",
			"wss.source_tool_bytes":        "8000",
			"wss.tool_result_output_bytes": "7800",
		},
		EvidenceDecisions: []evidence.BlockDecision{{
			Mechanism:      "captured_output",
			ContentClass:   evidence.ContentCode,
			Action:         evidence.ActionFullPass,
			Reason:         "wss_source_tool_output_full_pass",
			OriginalTokens: 2000,
			FinalTokens:    2000,
		}},
	})

	report, err := loadWSSFirstReadInventory(wssFirstReadInventoryFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSFirstReadInventory() error = %v", err)
	}
	if report.Verdict != "dependency_trace_missing" ||
		report.CandidateRequests != 1 ||
		report.MissingDependencyTraceRequests != 1 ||
		report.CandidateOutputTokensEstimate != 2000 ||
		report.FirstReadFullPassRequests != 1 ||
		report.ContextRiskRequests != 1 ||
		report.ReadLikeByShape["delta"] != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if !strings.Contains(report.NextAction, "dependency telemetry") {
		t.Fatalf("next action should demand dependency telemetry: %q", report.NextAction)
	}
}

func TestWSSFirstReadInventoryCountsTraceAndReadDelta(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID:   "traced-read-delta",
		Path:        "/backend-api/codex/responses",
		RouteMode:   "websocket_phasef",
		ReReadCount: 1,
		Tokens:      dbg.TokenCounts{Original: 9000, Final: 7000, Saved: 2000},
		Mechanisms: []dbg.MechanismAccounting{{
			Name:        "read_delta",
			Count:       1,
			SavedTokens: 2000,
			NetTokens:   2000,
			Reason:      "unchanged",
		}},
		DebugFacts: map[string]string{
			"wss.request_shape":        "full_history",
			"wss.tool_command_classes": "read_like=1",
			"wss.source_tool_results":  "1",
			"wss.source_tool_bytes":    "4000",
			"wss.read_file_hash":       "hash-redacted",
			"wss.read_range":           "1:200",
		},
	})

	report, err := loadWSSFirstReadInventory(wssFirstReadInventoryFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSFirstReadInventory() error = %v", err)
	}
	if report.Verdict != "promotion_blocked_dependency_risk" ||
		report.DependencyTraceRequests != 1 ||
		report.MissingDependencyTraceRequests != 0 ||
		report.DependencyTraceFacts["wss.read_file_hash"] != 1 ||
		report.DependencyTraceFacts["wss.read_range"] != 1 ||
		report.ExistingReadDeltaRequests != 1 ||
		report.ExistingReadDeltaSavedTokens != 2000 ||
		report.ReReadCount != 1 {
		t.Fatalf("unexpected traced report: %+v", report)
	}
}

func TestRunWSSFirstReadInventoryJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "json-run",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 4000, Final: 4000, Saved: 0},
		DebugFacts: map[string]string{
			"wss.request_shape":        "root",
			"wss.tool_command_classes": "read_like=1",
			"wss.source_tool_bytes":    "1600",
		},
	})

	var stdout, stderr bytes.Buffer
	code := runWSSFirstReadInventory([]string{path, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var report wssFirstReadInventoryReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode json output: %v\nstdout=%s", err, stdout.String())
	}
	if report.CandidateRequests != 1 || report.Verdict != "dependency_trace_missing" {
		t.Fatalf("unexpected json report: %+v", report)
	}
}

func TestWSSFirstReadInventoryReadsLiveCorpusSessionFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	category := filepath.Join(dir, "cli_repeat_read")
	if err := os.MkdirAll(category, 0o755); err != nil {
		t.Fatalf("mkdir category: %v", err)
	}
	path := filepath.Join(category, "session_wss_proof_export_001.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "live-corpus-session",
		Path:      "/backend-api/codex/responses",
		RouteMode: "wss_phasef",
		Tokens:    dbg.TokenCounts{Original: 8000, Final: 8000, Saved: 0},
		DebugFacts: map[string]string{
			"wss.request_shape":        "full_history",
			"wss.tool_command_classes": "read_like=1",
			"wss.source_tool_bytes":    "2000",
		},
	})

	report, err := loadWSSFirstReadInventory(wssFirstReadInventoryFlags{path: dir})
	if err != nil {
		t.Fatalf("loadWSSFirstReadInventory() error = %v", err)
	}
	if report.Logs != 1 || report.CandidateRequests != 1 || report.ReadLikeByShape["full_history"] != 1 {
		t.Fatalf("live corpus session file was not counted: %+v", report)
	}
}

func TestWSSFirstReadInventoryDistinguishesMissingCandidateTelemetry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "session_wss_proof_export_001.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "stripped-corpus-row",
		Path:      "/backend-api/codex/responses",
		RouteMode: "wss_phasef",
		Tokens:    dbg.TokenCounts{Original: 8000, Final: 7600, Saved: 400},
	})

	report, err := loadWSSFirstReadInventory(wssFirstReadInventoryFlags{path: dir})
	if err != nil {
		t.Fatalf("loadWSSFirstReadInventory() error = %v", err)
	}
	if report.Verdict != "candidate_telemetry_missing" || report.RequestsWithoutCandidateFacts != 1 {
		t.Fatalf("missing candidate telemetry should not look like no surface: %+v", report)
	}
}

func TestRunWSSFirstReadInventoryRequiresTrace(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "require-trace",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 4000, Final: 4000, Saved: 0},
		DebugFacts: map[string]string{
			"wss.request_shape":        "delta",
			"wss.tool_command_classes": "read_like=1",
			"wss.source_tool_bytes":    "1600",
		},
	})

	var stdout, stderr bytes.Buffer
	code := runWSSFirstReadInventory([]string{path, "--require-dependency-trace"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run code=%d want 1 stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "dependency trace missing") {
		t.Fatalf("stderr should explain missing trace, got %q", stderr.String())
	}
}

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

func TestWSSLocalGapInventoryScansDirectoryAndSortsRecoverableGap(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	protectedDir := filepath.Join(dir, "cap-protected")
	appliedDir := filepath.Join(dir, "cap-applied")
	if err := os.MkdirAll(protectedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(appliedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSONLFile(t, filepath.Join(protectedDir, "decisions.jsonl"), dbg.RequestSummary{
		RequestID: "protected-prefix",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 6000, Final: 6000, Saved: 0},
		DebugFacts: map[string]string{
			"wss.request_shape":                      "root",
			"wss.output_reduce_reason":               "prompt_cache_prefix_full_pass",
			"wss.tool_definition_bytes":              "12000",
			"wss.tool_definition_default_keep_bytes": "12000",
			"wss.tool_definition_nondefault_bytes":   "0",
			"wss.instructions_bytes":                 "9000",
			"wss.tool_definition_default_keep":       "3",
		},
	})
	writeJSONLFile(t, filepath.Join(appliedDir, "run.decisions.jsonl"), dbg.RequestSummary{
		RequestID: "applied",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 10000, Final: 5000, Saved: 5000},
		DebugFacts: map[string]string{
			"wss.request_shape": "full_history",
		},
		EvidenceDecisions: []evidence.BlockDecision{{
			Mechanism:      "read_delta",
			ContentClass:   evidence.ContentPlain,
			Action:         evidence.ActionApplied,
			Reason:         "positive_net_savings",
			OriginalTokens: 10000,
			FinalTokens:    5000,
			SavedTokens:    5000,
			NetTokens:      5000,
		}},
	})

	report, err := loadWSSLocalGapInventory(wssLocalGapInventoryFlags{path: dir})
	if err != nil {
		t.Fatalf("loadWSSLocalGapInventory() error = %v", err)
	}
	if report.Logs != 2 ||
		report.PhaseFRequests != 2 ||
		report.OriginalTokens != 16000 ||
		report.LocalSavedTokens != 5000 ||
		report.PolicyCeiling != 10000 ||
		report.TargetDeficit != 2680 ||
		report.CeilingDeficit != 0 ||
		report.RecoverableGap != 5000 {
		t.Fatalf("bad inventory totals: %+v", report)
	}
	if len(report.Rows) != 2 ||
		report.Rows[0].Name != "cap-applied" ||
		report.Rows[0].RecoverableGap != 5000 ||
		report.Rows[1].Name != "cap-protected" ||
		report.Rows[1].PolicyProtectedTokens != 6000 {
		t.Fatalf("bad inventory rows: %+v", report.Rows)
	}
}

func TestRunWSSLocalGapInventoryJSONAndText(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "single",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 1000, Final: 600, Saved: 400},
		DebugFacts: map[string]string{
			"wss.request_shape": "full_history",
		},
		EvidenceDecisions: []evidence.BlockDecision{{
			Mechanism:      "read_delta",
			ContentClass:   evidence.ContentPlain,
			Action:         evidence.ActionApplied,
			Reason:         "positive_net_savings",
			OriginalTokens: 1000,
			FinalTokens:    600,
			SavedTokens:    400,
			NetTokens:      400,
		}},
	})

	var stdout, stderr bytes.Buffer
	if code := runWSSLocalGapInventory([]string{path}, &stdout, &stderr); code != 0 {
		t.Fatalf("runWSSLocalGapInventory text code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "WSS Local Gap Inventory") ||
		!strings.Contains(stdout.String(), "Policy ceiling/ratio") ||
		!strings.Contains(stdout.String(), "recoverable=600") {
		t.Fatalf("text inventory missing expected fields:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWSSLocalGapInventory([]string{path, "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("runWSSLocalGapInventory json code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var report wssLocalGapInventoryReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("parse json output: %v\n%s", err, stdout.String())
	}
	if report.Logs != 1 ||
		report.LocalSavedTokens != 400 ||
		report.PolicyCeiling != 1000 ||
		report.RecoverableGap != 600 {
		t.Fatalf("bad json inventory: %+v", report)
	}
}

func TestWSSLocalGapTopNonPrefixActionSkipsCapabilityPrefix(t *testing.T) {
	t.Parallel()

	row, ok := wssLocalGapTopNonPrefixAction([]wssLocalGapActionableRow{
		{Category: "prefix_capability_context_guarded", Tokens: 40000},
		{Category: "resource_budget_guard", Source: "evidence:session_integrity_budget", Tokens: 2710},
	})
	if !ok ||
		row.Category != "resource_budget_guard" ||
		row.Source != "evidence:session_integrity_budget" ||
		row.Tokens != 2710 {
		t.Fatalf("top non-prefix row mismatch: ok=%v row=%+v", ok, row)
	}
}

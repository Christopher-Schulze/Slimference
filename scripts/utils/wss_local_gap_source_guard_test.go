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

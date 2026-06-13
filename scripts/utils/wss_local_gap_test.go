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
				"wss.request_shape": "full_history",
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
	if !report.GatePassed || report.LocalSavingsRatio != 0.6 {
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
				"wss.request_shape": "delta",
			},
		},
	)

	report, err := loadWSSLocalGapReport(wssLocalGapFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSLocalGapReport() error = %v", err)
	}
	missingShape := wssLocalGapRequestGuardRow{}
	bypass := wssLocalGapRequestGuardRow{}
	for _, row := range report.RequestGuards {
		switch row.Guard {
		case "wss.request_shape=(missing)":
			missingShape = row
		case "bypass_reason=wss_tool_output_state_full_pass":
			bypass = row
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

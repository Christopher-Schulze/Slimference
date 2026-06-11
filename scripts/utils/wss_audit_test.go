package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
)

func TestWSSAuditReport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path,
		dbg.RequestSummary{
			RequestID:              "wss-1",
			Timestamp:              time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC),
			SessionID:              "codex-wss:s1",
			Path:                   "/backend-api/codex/responses",
			RouteMode:              "websocket_phasef",
			PreviousResponseIDUsed: true,
			ReReadCount:            2,
			Tokens:                 dbg.TokenCounts{Saved: 40},
			Plan:                   &dbg.PlanSummary{ContentClasses: []string{"tool_output", "repeated_tool_output"}},
			DebugFacts: map[string]string{
				"wss.shadow_mirror_blocks":                            "2",
				"wss.shadow_mirror_bytes":                             "1000",
				"wss.shadow_mirror_referenceable_blocks":              "1",
				"wss.shadow_mirror_referenceable_bytes":               "400",
				"wss.shadow_mirror_normalized_segments":               "2",
				"wss.shadow_mirror_normalized_bytes":                  "800",
				"wss.shadow_mirror_normalized_referenceable_segments": "1",
				"wss.shadow_mirror_normalized_referenceable_bytes":    "300",
				"wss.shadow_mirror_normalized_density_by_kind":        "codex_exec_payload=300/500/1/1,tool_result=0/300/0/1",
			},
		},
		dbg.RequestSummary{
			RequestID: "http-1",
			Path:      "/v1/chat/completions",
			RouteMode: "http",
		},
		dbg.RequestSummary{
			RequestID: "wss-2",
			Path:      "/backend-api/codex/responses",
			RouteMode: "websocket_phasef",
			Plan:      &dbg.PlanSummary{ContentClasses: []string{"tool_output"}},
		},
	)

	report, err := loadWSSAuditReport(wssAuditFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSAuditReport() error = %v", err)
	}
	if report.Requests != 3 || report.WSSRequests != 2 || report.PhaseFRequests != 2 {
		t.Fatalf("bad request counts: %+v", report)
	}
	if report.UniqueSessions != 1 || report.MissingSessionID != 1 || report.PreviousResponseIDUsed != 1 ||
		report.PositiveSavings != 1 || report.TokensSaved != 40 {
		t.Fatalf("bad WSS counters: %+v", report)
	}
	if report.ReReadRequests != 1 || report.ReReadCount != 2 {
		t.Fatalf("bad re-read counters: %+v", report)
	}
	if report.ShadowMirror == nil ||
		report.ShadowMirror.ReferenceableBytes != 400 ||
		report.ShadowMirror.ReferenceableBytePct != 40 ||
		report.ShadowMirror.NormalizedReferenceableBytes != 300 ||
		report.ShadowMirror.NormalizedReferenceableBytePct != 37.5 ||
		len(report.ShadowMirror.NormalizedReferenceableBytesByKind) != 2 {
		t.Fatalf("bad shadow mirror report: %+v", report.ShadowMirror)
	}
	if got := report.ShadowMirror.NormalizedReferenceableBytesByKind[0]; got.Kind != "codex_exec_payload" || got.ReferenceableBytes != 300 || got.Bytes != 500 || got.ReferenceableBytePct != 60 {
		t.Fatalf("bad shadow mirror kind row: %+v", got)
	}
	if report.ContentClasses["tool_output"] != 2 || report.ContentClasses["repeated_tool_output"] != 1 {
		t.Fatalf("bad content classes: %+v", report.ContentClasses)
	}
	if len(report.Sessions) != 2 {
		t.Fatalf("session count = %d, want 2: %+v", len(report.Sessions), report.Sessions)
	}
	foundMissing := false
	for _, session := range report.Sessions {
		foundMissing = foundMissing || session.SessionID == "(missing)"
	}
	if !foundMissing {
		t.Fatalf("missing session bucket not present: %+v", report.Sessions)
	}
	notes := strings.Join(report.Notes, "\n")
	if !strings.Contains(notes, "session id") || !strings.Contains(notes, "re-read canary") {
		t.Fatalf("expected session/re-read notes, got %+v", report.Notes)
	}
}

func TestWSSAuditGateFailures(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "wss-1",
		SessionID: "codex-wss:s1",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
	})

	report, err := loadWSSAuditReport(wssAuditFlags{
		path:                   path,
		expectDistinctSessions: 2,
		minPhaseF:              2,
		requireSavings:         true,
	})
	if err != nil {
		t.Fatalf("loadWSSAuditReport() error = %v", err)
	}
	if report.GatePassed || len(report.GateFailures) != 3 {
		t.Fatalf("expected three gate failures, got passed=%v failures=%+v", report.GatePassed, report.GateFailures)
	}
}

func TestWSSAuditSinceFiltersOldRecords(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path,
		dbg.RequestSummary{
			RequestID: "old",
			Timestamp: time.Date(2026, 5, 30, 11, 0, 0, 0, time.UTC),
			SessionID: "codex-wss:old",
			Path:      "/backend-api/codex/responses",
			RouteMode: "websocket_phasef",
			Tokens:    dbg.TokenCounts{Saved: 3},
		},
		dbg.RequestSummary{
			RequestID: "new",
			Timestamp: time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC),
			SessionID: "codex-wss:new",
			Path:      "/backend-api/codex/responses",
			RouteMode: "websocket_phasef",
		},
		dbg.RequestSummary{
			RequestID: "untimed",
			SessionID: "codex-wss:untimed",
			Path:      "/backend-api/codex/responses",
			RouteMode: "websocket_phasef",
			Tokens:    dbg.TokenCounts{Saved: 99},
		},
	)

	report, err := loadWSSAuditReport(wssAuditFlags{
		path:  path,
		since: time.Date(2026, 5, 30, 11, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("loadWSSAuditReport() error = %v", err)
	}
	if report.Requests != 1 || report.UniqueSessions != 1 || report.Sessions[0].SessionID != "codex-wss:new" || report.TokensSaved != 0 {
		t.Fatalf("since filter failed: %+v", report)
	}
}

func TestRunWSSAuditJSONAndText(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "wss-1",
		SessionID: "codex-wss:s1",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Saved: 3},
		DebugFacts: map[string]string{
			"wss.shadow_mirror_blocks":                            "1",
			"wss.shadow_mirror_bytes":                             "300",
			"wss.shadow_mirror_referenceable_blocks":              "0",
			"wss.shadow_mirror_referenceable_bytes":               "0",
			"wss.shadow_mirror_normalized_segments":               "1",
			"wss.shadow_mirror_normalized_bytes":                  "200",
			"wss.shadow_mirror_normalized_referenceable_segments": "1",
			"wss.shadow_mirror_normalized_referenceable_bytes":    "120",
			"wss.shadow_mirror_normalized_density_by_kind":        "codex_exec_payload=120/200/1/1",
		},
	})

	var stdout, stderr bytes.Buffer
	if code := runWSSAudit([]string{path}, &stdout, &stderr); code != 0 {
		t.Fatalf("runWSSAudit text code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Phase-F requests:") ||
		!strings.Contains(stdout.String(), "re-read requests/count:") ||
		!strings.Contains(stdout.String(), "Shadow mirror density:") ||
		!strings.Contains(stdout.String(), "codex_exec_payload") ||
		!strings.Contains(stdout.String(), "codex-wss:s1") {
		t.Fatalf("text output missing details:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	statePath := writeAggregateStateFile(t, aggregateSampleAdminState)
	if code := runWSSAudit([]string{path, "--admin-state-file=" + statePath}, &stdout, &stderr); code != 0 {
		t.Fatalf("runWSSAudit policy text code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Policy decisions:") ||
		!strings.Contains(stdout.String(), "wss_phasef/chunk_dedup/allow recoverable_chunk_dedup: 3") ||
		!strings.Contains(stdout.String(), "Cache decisions:") ||
		!strings.Contains(stdout.String(), "wss_phasef/read_delta/miss first_observation_seeded: 1") ||
		!strings.Contains(stdout.String(), "Chunk dedup density:") ||
		!strings.Contains(stdout.String(), "references:              4") {
		t.Fatalf("text output missing policy/cache details:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWSSAudit([]string{path, "--json", "--admin-state-file=" + statePath}, &stdout, &stderr); code != 0 {
		t.Fatalf("runWSSAudit json code=%d stderr=%s", code, stderr.String())
	}
	var report wssAuditReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json output did not parse: %v\n%s", err, stdout.String())
	}
	if report.TokensSaved != 3 {
		t.Fatalf("tokens saved = %d, want 3", report.TokensSaved)
	}
	if report.ShadowMirror == nil || report.ShadowMirror.NormalizedReferenceableBytes != 120 || report.ShadowMirror.NormalizedReferenceableBytePct != 60 {
		t.Fatalf("shadow mirror missing from JSON report: %+v", report.ShadowMirror)
	}
	if len(report.Policy) != 2 || report.PolicySource == "" {
		t.Fatalf("policy join missing from JSON report: %+v", report)
	}
	if len(report.Cache) != 2 {
		t.Fatalf("cache join missing from JSON report: %+v", report)
	}
	if report.ChunkDedupReferences != 4 || report.ChunkDedupRefBytes != 8192 || report.ChunkDedupInputBytes != 16384 {
		t.Fatalf("chunk density join missing from JSON report: %+v", report)
	}
}

func TestRunWSSAuditGateFailureExitCode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "wss-1",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
	})

	var stdout, stderr bytes.Buffer
	if code := runWSSAudit([]string{path, "--expect-distinct-sessions=1"}, &stdout, &stderr); code != 3 {
		t.Fatalf("runWSSAudit gate code=%d want 3 stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "gate:") || !strings.Contains(stdout.String(), "FAIL") {
		t.Fatalf("gate output missing failure:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWSSAudit([]string{path, "--expect-distinct-sessions=1", "--json"}, &stdout, &stderr); code != 3 {
		t.Fatalf("runWSSAudit json gate code=%d want 3 stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var report wssAuditReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json gate output did not parse: %v\n%s", err, stdout.String())
	}
	if report.GatePassed || len(report.GateFailures) == 0 {
		t.Fatalf("json gate output missing failure: %+v", report)
	}
}

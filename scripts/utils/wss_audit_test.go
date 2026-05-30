package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbg "github.com/slimference/slimference/internal/debug"
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
	})

	var stdout, stderr bytes.Buffer
	if code := runWSSAudit([]string{path}, &stdout, &stderr); code != 0 {
		t.Fatalf("runWSSAudit text code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Phase-F requests:") ||
		!strings.Contains(stdout.String(), "re-read requests/count:") ||
		!strings.Contains(stdout.String(), "codex-wss:s1") {
		t.Fatalf("text output missing details:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWSSAudit([]string{path, "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("runWSSAudit json code=%d stderr=%s", code, stderr.String())
	}
	var report wssAuditReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json output did not parse: %v\n%s", err, stdout.String())
	}
	if report.TokensSaved != 3 {
		t.Fatalf("tokens saved = %d, want 3", report.TokensSaved)
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

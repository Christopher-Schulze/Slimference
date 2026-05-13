package analytics

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/types"
)

func TestReadOutputReduceReport(t *testing.T) {
	t.Parallel()

	logDir := t.TempDir()
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	path := OutputReduceLogPath(logDir, now)
	lines := []persistedPromptEnvelope{
		{
			Type: "analytics_event",
			Payload: mustJSON(t, types.AnalyticsEvent{
				Type:                    types.EventRequestProcessed,
				OutputTokens:            120,
				OutputReduceApplied:     true,
				OutputReduceProfile:     "codex",
				OutputReduceReason:      "applied",
				OutputReduceAddedTokens: 14,
				OutputReduceTaskShape:   "code_edit",
			}),
		},
		{
			Type: "analytics_event",
			Payload: mustJSON(t, types.AnalyticsEvent{
				Type:               types.EventRequestProcessed,
				OutputTokens:       40,
				OutputReduceReason: "below_min_tokens",
			}),
		},
		{
			Type:    "session_snapshot",
			Payload: mustJSON(t, AnalyticsSnapshot{TotalRequests: 99}),
		},
	}
	var content strings.Builder
	content.WriteString("\n{\n")
	content.WriteString(`{"type":"analytics_event","payload":`)
	content.WriteString("\n")
	content.WriteString(`{"type":"analytics_event","payload":{"Type":"bad"}}`)
	content.WriteByte('\n')
	for _, line := range lines {
		data, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		content.Write(data)
		content.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(content.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := ReadOutputReduceReport(logDir, "today", now)
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalRequests != 2 || report.AppliedRequests != 1 || report.SkippedRequests != 1 {
		t.Fatalf("report counts=%+v", report)
	}
	if report.InputOverheadTokens != 14 || report.OutputTokensObserved != 160 || report.AppliedOutputTokens != 120 {
		t.Fatalf("report tokens=%+v", report)
	}
	if report.Profiles["codex"] != 1 || report.TaskShapes["code_edit"] != 1 || report.Reasons["applied"] != 1 || report.Reasons["below_min_tokens"] != 1 {
		t.Fatalf("maps=%+v %+v %+v", report.Profiles, report.TaskShapes, report.Reasons)
	}
	if report.AvgOutputTokens != 80 || report.AvgAppliedOutputTokens != 120 || report.AvgInputOverheadPerApply != 14 {
		t.Fatalf("averages=%+v", report)
	}
}

func TestReadOutputReduceReportEmptyAndErrors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	report, err := ReadOutputReduceReport(t.TempDir(), "today", now)
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalRequests != 0 || report.Profiles != nil || report.TaskShapes != nil || report.Reasons != nil {
		t.Fatalf("empty report=%+v", report)
	}
	if _, err := ReadOutputReduceReport(t.TempDir(), "bad", now); err == nil {
		t.Fatal("expected bad period error")
	}
	logDirFile := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(logDirFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadOutputReduceReport(logDirFile, "all", now); err == nil {
		t.Fatal("expected all-period read dir error")
	}
	if _, err := ReadOutputReduceReport(logDirFile, "today", now); err == nil {
		t.Fatal("expected today open error through file parent")
	}
}

func TestWriteOutputReduceCSV(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := WriteOutputReduceCSV(&buf, OutputReduceReport{
		Period:                   "week",
		TotalRequests:            2,
		AppliedRequests:          1,
		SkippedRequests:          1,
		InputOverheadTokens:      14,
		OutputTokensObserved:     160,
		AppliedOutputTokens:      120,
		AvgOutputTokens:          80,
		AvgAppliedOutputTokens:   120,
		AvgInputOverheadPerApply: 14,
		Profiles:                 map[string]int{"codex": 1, " ": 9},
		TaskShapes:               map[string]int{"code_edit": 1},
		Reasons:                  map[string]int{"applied": 1, "below_min_tokens": 1, "": 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"period,total_requests,applied_requests",
		"week,2,1,1,14,160,120,80.00,120.00,14.00",
		"profile,codex,1",
		"task_shape,code_edit,1",
		"reason,below_min_tokens,1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("csv missing %q in %q", want, out)
		}
	}
	if err := WriteOutputReduceCSV(promptCacheErrWriter{}, OutputReduceReport{}); err == nil {
		t.Fatal("expected csv write error")
	}
}

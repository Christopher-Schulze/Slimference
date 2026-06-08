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

func TestReadPromptCacheReport(t *testing.T) {
	t.Parallel()

	logDir := t.TempDir()
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(logDir, now.Format(dateFormat)+".jsonl")

	lines := []persistedPromptEnvelope{
		{
			Type: "analytics_event",
			Payload: mustJSON(t, types.AnalyticsEvent{
				Type:              types.EventRequestProcessed,
				CacheReadTokens:   120,
				CacheCreateTokens: 30,
			}),
		},
		{
			Type: "analytics_event",
			Payload: mustJSON(t, types.AnalyticsEvent{
				Type:              types.EventRequestProcessed,
				CacheReadTokens:   0,
				CacheCreateTokens: 10,
			}),
		},
		{
			Type: "analytics_event",
			Payload: mustJSON(t, types.AnalyticsEvent{
				Type:            types.EventRequestProcessed,
				CacheReadTokens: 40,
			}),
		},
		{
			Type:    "session_snapshot",
			Payload: mustJSON(t, AnalyticsSnapshot{TotalRequests: 99}),
		},
	}

	var content strings.Builder
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

	report, err := ReadPromptCacheReport(logDir, "today", now)
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalRequests != 3 {
		t.Fatalf("TotalRequests=%d", report.TotalRequests)
	}
	if report.CacheReadRequests != 2 {
		t.Fatalf("CacheReadRequests=%d", report.CacheReadRequests)
	}
	if report.CacheReadTokens != 160 {
		t.Fatalf("CacheReadTokens=%d", report.CacheReadTokens)
	}
	if report.CacheCreateTokens != 40 {
		t.Fatalf("CacheCreateTokens=%d", report.CacheCreateTokens)
	}
	if report.EstimatedSavedRead != 108 {
		t.Fatalf("EstimatedSavedRead=%d", report.EstimatedSavedRead)
	}
}

func TestWritePromptCacheCSV(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := WritePromptCacheCSV(&buf, PromptCacheReport{
		Period:             "week",
		TotalRequests:      4,
		CacheReadRequests:  1,
		CacheReadTokens:    120,
		CacheCreateTokens:  20,
		EstimatedSavedRead: 108,
		HitRate:            0.25,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"period,total_requests", "week,4,1,25.00,120,20,108"} {
		if !strings.Contains(out, want) {
			t.Fatalf("csv missing %q in %q", want, out)
		}
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

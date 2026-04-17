package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/analytics"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/filter"
	"github.com/slimference/slimference/internal/types"
)

func TestParseOutputFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		wantFmt string
		wantLen int
		wantErr bool
	}{
		{name: "default text", args: []string{"report.jsonl"}, wantFmt: outputText, wantLen: 1},
		{name: "json", args: []string{"--json", "report.jsonl"}, wantFmt: outputJSON, wantLen: 1},
		{name: "csv", args: []string{"report.jsonl", "--csv"}, wantFmt: outputCSV, wantLen: 1},
		{name: "unknown flag", args: []string{"--xml"}, wantErr: true},
		{name: "multiple outputs", args: []string{"--json", "--csv", "report.jsonl"}, wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotFmt, gotRest, err := parseOutputFlag(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseOutputFlag() error = %v", err)
			}
			if gotFmt != tt.wantFmt {
				t.Fatalf("format = %q, want %q", gotFmt, tt.wantFmt)
			}
			if len(gotRest) != tt.wantLen {
				t.Fatalf("rest len = %d, want %d", len(gotRest), tt.wantLen)
			}
		})
	}
}

func TestLoadSessionReport_snapshotFallback(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "analytics.jsonl")
	env := struct {
		Type      string                      `json:"type"`
		Timestamp time.Time                   `json:"timestamp"`
		Payload   analytics.AnalyticsSnapshot `json:"payload"`
	}{
		Type:      "session_snapshot",
		Timestamp: time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
		Payload: analytics.AnalyticsSnapshot{
			TotalRequests:     3,
			TotalInputTokens:  1200,
			SavedInputTokens:  300,
			TotalOutputTokens: 90,
			Layer1Savings:     200,
			Layer2Savings:     100,
			CacheHits:         1,
			PerProvider: map[types.Provider]analytics.ProviderStats{
				types.Anthropic: {
					Messages:         3,
					InputTokensOrig:  1200,
					InputTokensSaved: 300,
				},
			},
		},
	}
	writeJSONLFile(t, path, env)

	report, err := loadSessionReport(path)
	if err != nil {
		t.Fatalf("loadSessionReport() error = %v", err)
	}
	if report.Source != "session_snapshot" {
		t.Fatalf("source = %q, want session_snapshot", report.Source)
	}
	if report.TotalRequests != 3 {
		t.Fatalf("TotalRequests = %d, want 3", report.TotalRequests)
	}
	if report.CompTokens != 900 {
		t.Fatalf("CompTokens = %d, want 900", report.CompTokens)
	}
	if report.ByProvider["anthropic"].Saved != 300 {
		t.Fatalf("provider saved = %d, want 300", report.ByProvider["anthropic"].Saved)
	}
}

func TestLoadCombinedReport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	analyticsPath := filepath.Join(dir, "analytics.jsonl")
	decisionsPath := filepath.Join(dir, "decisions.jsonl")
	filterDBPath := filepath.Join(dir, "filter.db")
	now := time.Date(2026, 4, 17, 15, 0, 0, 0, time.UTC)

	analyticsEnv := struct {
		Type      string               `json:"type"`
		Timestamp time.Time            `json:"timestamp"`
		Payload   types.AnalyticsEvent `json:"payload"`
	}{
		Type:      "analytics_event",
		Timestamp: now,
		Payload: types.AnalyticsEvent{
			Type:             types.EventRequestProcessed,
			Provider:         types.Anthropic,
			InputTokensOrig:  1000,
			InputTokensComp:  700,
			OutputTokens:     120,
			Layers:           []int{1, 2},
			CompressionRatio: 0.7,
		},
	}
	writeJSONLFile(t, analyticsPath, analyticsEnv)

	decision := dbg.RequestSummary{
		RequestID: "r1",
		Tokens: dbg.TokenCounts{
			Original: 1000,
			Final:    700,
			Saved:    300,
			Ratio:    0.7,
		},
		Layer1Breakdown: map[string]dbg.SubLayerBreakdown{
			"dedup": {Blocks: 2, Saved: 120},
		},
	}
	writeJSONLFile(t, decisionsPath, decision)

	db, err := filter.OpenDB(filterDBPath)
	if err != nil {
		t.Fatalf("OpenDB() error = %v", err)
	}
	if err := filter.RecordFilterRun(db, "[git] git status", dir, 400, 100, 75, now); err != nil {
		t.Fatalf("RecordFilterRun() error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close() error = %v", err)
	}

	report, err := loadCombinedReport(analyticsPath, decisionsPath, filterDBPath)
	if err != nil {
		t.Fatalf("loadCombinedReport() error = %v", err)
	}
	if report.ProxySavedTokens != 300 {
		t.Fatalf("ProxySavedTokens = %d, want 300", report.ProxySavedTokens)
	}
	if report.Layer0SavedTokensEst != 300 {
		t.Fatalf("Layer0SavedTokensEst = %d, want 300", report.Layer0SavedTokensEst)
	}
	if report.CombinedInputTokensEst != 1400 {
		t.Fatalf("CombinedInputTokensEst = %d, want 1400", report.CombinedInputTokensEst)
	}
	if report.CombinedSavedTokensEst != 600 {
		t.Fatalf("CombinedSavedTokensEst = %d, want 600", report.CombinedSavedTokensEst)
	}
	if report.Decisions.SubLayerTotals["dedup"] != 120 {
		t.Fatalf("dedup total = %d, want 120", report.Decisions.SubLayerTotals["dedup"])
	}
}

func writeJSONLFile(t *testing.T, path string, values ...interface{}) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("os.Create(%s): %v", path, err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Fatalf("close %s: %v", path, err)
		}
	}()

	enc := json.NewEncoder(f)
	for _, value := range values {
		if err := enc.Encode(value); err != nil {
			t.Fatalf("encode %s: %v", path, err)
		}
	}
}

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/analytics"
	"github.com/Christopher-Schulze/Slimference/internal/types"
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

func writeJSONLFile(t *testing.T, path string, values ...any) {
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

func writeFileForLocalArtifactTest(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

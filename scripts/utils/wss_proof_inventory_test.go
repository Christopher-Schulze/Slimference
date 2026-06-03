package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestWSSProofInventoryScansMatrixRowsOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofRepeatReadFrames(t, framesPath, "inventory")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath,
		wssProofMatrixRecord{
			ID:               "cli-search",
			Client:           "cli",
			WorkloadClass:    "search_loop",
			FramesPath:       framesPath,
			ExpectedReducers: []string{"captured_output"},
			LiveDelta: &codexCaptureLiveDelta{
				BillableInputTokensSaved: 99,
				ProxyLayer0Captured:      1,
				ProviderCacheReadTokens:  321,
				HostBudgetStatus:         "ok",
				HostBudgetCompressionOK:  true,
				HostBudgetDegradationOK:  true,
			},
		},
		map[string]string{"type": "response.completed", "client": "not-a-matrix"},
	)

	report, err := loadWSSProofInventory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if report.MatrixFiles != 1 || report.Rows != 1 || report.Clients["cli"] != 1 || report.WorkloadClasses["search_loop"] != 1 {
		t.Fatalf("bad inventory aggregate: %+v", report)
	}
	if report.PositiveTokenRows != 1 || report.LiveReducerHits["captured_output"] != 1 ||
		report.LiveReducerHits["provider_cache_read"] != 321 || report.HostBudgetOKRows != 1 {
		t.Fatalf("missing live signals: %+v", report)
	}
	if len(report.MissingMaxxWorkloads) == 0 {
		t.Fatalf("expected missing maxx workloads: %+v", report)
	}
}

func TestRunWSSProofInventoryJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofRepeatReadFrames(t, framesPath, "inventory-json")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "desktop-repeat",
		Client:        "desktop",
		WorkloadClass: "repeat_full_read",
		FramesPath:    framesPath,
		LiveDelta:     proofMatrixLiveDelta(false),
	})

	var stdout, stderr bytes.Buffer
	code := runWSSProofInventory([]string{matrixPath, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"rows": 1`) || !strings.Contains(stdout.String(), `"repeat_full_read": 1`) {
		t.Fatalf("unexpected json output: %s", stdout.String())
	}
}

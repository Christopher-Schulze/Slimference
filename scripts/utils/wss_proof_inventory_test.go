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
		wssProofMatrixRecord{
			ID:               "cli-provider-cache",
			Client:           "cli",
			WorkloadClass:    "provider_cache_long_session",
			FramesPath:       framesPath,
			ExpectedReducers: []string{"provider_cache_read", "host_budget_ok"},
			LiveDelta: &codexCaptureLiveDelta{
				BillableInputTokensSaved: 17,
				ProviderCacheReadTokens:  4444,
				HostBudgetStatus:         "ok",
				HostBudgetCompressionOK:  true,
				HostBudgetDegradationOK:  true,
			},
		},
		wssProofMatrixRecord{
			ID:               "desktop-log-chunk-incomplete",
			Client:           "desktop",
			WorkloadClass:    "chunk_dedup_log_output",
			FramesPath:       framesPath,
			ExpectedReducers: []string{"chunk_dedup", "chunk_dedup_refs"},
			LiveDelta: &codexCaptureLiveDelta{
				BillableInputTokensSaved: 23,
				ProxyLayer0Captured:      1,
				HostBudgetStatus:         "ok",
				HostBudgetCompressionOK:  true,
				HostBudgetDegradationOK:  true,
			},
		},
		wssProofMatrixRecord{
			ID:               "cli-tool-heavy",
			Client:           "cli",
			WorkloadClass:    "tool_heavy",
			FramesPath:       framesPath,
			ExpectedReducers: []string{"tool_prune", "tool_prune_tokens_saved", "host_budget_ok"},
			LiveDelta: &codexCaptureLiveDelta{
				ToolPrunePruned:         3,
				ToolPruneTokensSaved:    1200,
				HostBudgetStatus:        "ok",
				HostBudgetCompressionOK: true,
				HostBudgetDegradationOK: true,
			},
		},
		map[string]string{"type": "response.completed", "client": "not-a-matrix"},
	)

	report, err := loadWSSProofInventory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if report.MatrixFiles != 1 || report.Rows != 4 || report.Clients["cli"] != 3 ||
		report.Clients["desktop"] != 1 || report.WorkloadClasses["search_loop"] != 1 {
		t.Fatalf("bad inventory aggregate: %+v", report)
	}
	if report.PositiveTokenRows != 4 || report.LiveReducerHits["captured_output"] != 2 ||
		report.LiveReducerHits["provider_cache_read"] != 4765 ||
		report.LiveReducerHits["tool_prune_tokens_saved"] != 1200 || report.HostBudgetOKRows != 4 {
		t.Fatalf("missing live signals: %+v", report)
	}
	if len(report.MissingMaxxWorkloads) == 0 || strings.Contains(strings.Join(report.MissingMaxxWorkloads, ","), "provider_cache_long_session") {
		t.Fatalf("expected missing maxx workloads: %+v", report)
	}
	providerStatus := findInventoryWorkloadStatus(t, report, "provider_cache_long_session")
	if !providerStatus.Complete || len(providerStatus.MissingSignals) != 0 {
		t.Fatalf("provider cache workload should be complete: %+v", providerStatus)
	}
	logStatus := findInventoryWorkloadStatus(t, report, "chunk_dedup_log_output")
	if !logStatus.Complete || len(logStatus.MissingSignals) != 0 {
		t.Fatalf("log output workload should complete through captured-output or chunk fallback signals: %+v", logStatus)
	}
	toolStatus := findInventoryWorkloadStatus(t, report, "tool_heavy")
	if !toolStatus.Complete || toolStatus.PositiveTokenRows != 1 {
		t.Fatalf("tool-heavy should complete through tool-prune token savings: %+v", toolStatus)
	}
}

func TestWSSProofInventoryRequiresLogOrTestReducerSignal(t *testing.T) {
	status := &wssProofInventoryWorkloadStatus{
		WorkloadClass:     "chunk_dedup_test_output",
		Rows:              1,
		PositiveTokenRows: 1,
		HostBudgetOKRows:  1,
		LiveReducerHits: map[string]int64{
			"host_budget_ok": 1,
		},
	}
	status.MissingSignals = missingInventorySignals(status.LiveReducerHits, maxxWSSProofRequiredSignals[status.WorkloadClass])
	status.MissingSignals = append(status.MissingSignals, missingInventoryAlternativeSignals(status.LiveReducerHits, maxxWSSProofAlternativeSignals[status.WorkloadClass])...)
	status.Complete = status.Present &&
		maxxWorkloadHasPositiveEconomicSignal(status, status.WorkloadClass) &&
		status.SafetyIssueRows == 0 &&
		status.HostBudgetOKRows > 0 &&
		len(status.MissingSignals) == 0

	if status.Complete || !strings.Contains(strings.Join(status.MissingSignals, ","), "captured_output_or_codex_exec_envelope_or_chunk_dedup_refs") {
		t.Fatalf("test output workload without product reducer signal should stay incomplete: %+v", status)
	}

	status.Present = true
	status.LiveReducerHits["codex_exec_envelope"] = 1
	status.MissingSignals = missingInventorySignals(status.LiveReducerHits, maxxWSSProofRequiredSignals[status.WorkloadClass])
	status.MissingSignals = append(status.MissingSignals, missingInventoryAlternativeSignals(status.LiveReducerHits, maxxWSSProofAlternativeSignals[status.WorkloadClass])...)
	status.Complete = status.Present &&
		maxxWorkloadHasPositiveEconomicSignal(status, status.WorkloadClass) &&
		status.SafetyIssueRows == 0 &&
		status.HostBudgetOKRows > 0 &&
		len(status.MissingSignals) == 0
	if !status.Complete {
		t.Fatalf("test output workload should complete with Codex envelope reducer signal: %+v", status)
	}
}

func TestWSSProofInventoryHostResourceLongWorkdayRequiresSavings(t *testing.T) {
	status := &wssProofInventoryWorkloadStatus{
		WorkloadClass:     "host_resource_long_workday",
		Rows:              1,
		PositiveTokenRows: 0,
		HostBudgetOKRows:  1,
		LiveReducerHits: map[string]int64{
			"host_budget_ok": 1,
		},
	}
	if maxxWorkloadHasPositiveEconomicSignal(status, status.WorkloadClass) {
		t.Fatalf("host-resource long-workday without positive token savings must not complete: %+v", status)
	}

	status.PositiveTokenRows = 1
	if !maxxWorkloadHasPositiveEconomicSignal(status, status.WorkloadClass) {
		t.Fatalf("host-resource long-workday should complete its economic signal with savings plus host budget: %+v", status)
	}
}

func TestWSSProofInventoryOutputReduceRequiresObservedOutputTokens(t *testing.T) {
	status := &wssProofInventoryWorkloadStatus{
		WorkloadClass:    "output_reduce_aggressive",
		Rows:             1,
		HostBudgetOKRows: 1,
		LiveReducerHits: map[string]int64{
			"output_reduce_injected": 1,
			"host_budget_ok":         1,
		},
	}
	status.MissingSignals = missingInventorySignals(status.LiveReducerHits, maxxWSSProofRequiredSignals[status.WorkloadClass])
	status.MissingSignals = append(status.MissingSignals, missingInventoryAlternativeSignals(status.LiveReducerHits, maxxWSSProofAlternativeSignals[status.WorkloadClass])...)
	if maxxWorkloadHasPositiveEconomicSignal(status, status.WorkloadClass) || len(status.MissingSignals) == 0 {
		t.Fatalf("output-reduce without observed output tokens must stay incomplete: %+v", status)
	}

	status.LiveReducerHits["output_reduce_output_tokens"] = 42
	status.MissingSignals = missingInventorySignals(status.LiveReducerHits, maxxWSSProofRequiredSignals[status.WorkloadClass])
	status.MissingSignals = append(status.MissingSignals, missingInventoryAlternativeSignals(status.LiveReducerHits, maxxWSSProofAlternativeSignals[status.WorkloadClass])...)
	if !maxxWorkloadHasPositiveEconomicSignal(status, status.WorkloadClass) || len(status.MissingSignals) != 0 {
		t.Fatalf("output-reduce with observed output tokens should complete economic signal: %+v", status)
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

func findInventoryWorkloadStatus(t *testing.T, report wssProofInventoryReport, workload string) wssProofInventoryWorkloadStatus {
	t.Helper()
	for _, status := range report.MaxxWorkloadStatus {
		if status.WorkloadClass == workload {
			return status
		}
	}
	t.Fatalf("missing workload status for %s: %+v", workload, report.MaxxWorkloadStatus)
	return wssProofInventoryWorkloadStatus{}
}

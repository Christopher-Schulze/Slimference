package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWSSProofCleanMatrixFiltersOnlyReleaseCleanRows(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofControlFrames(t, framesPath, "clean-matrix")
	inputPath := filepath.Join(dir, "input.jsonl")
	outputPath := filepath.Join(dir, "clean.jsonl")
	writeJSONLFile(t, inputPath,
		wssProofMatrixRecord{
			ID:               "clean-repeat",
			Client:           "cli",
			WorkloadClass:    "repeat_full_read",
			FramesPath:       framesPath,
			ExpectedReducers: []string{"read_delta", "host_budget_ok"},
			LiveDelta: &codexCaptureLiveDelta{
				BillableInputTokensSaved: 10,
				ProxyLayer0ReadDelta:     1,
				HostBudgetStatus:         "ok",
				HostBudgetCompressionOK:  true,
				HostBudgetDegradationOK:  true,
			},
		},
		wssProofMatrixRecord{
			ID:            "clean-zero",
			Client:        "desktop",
			WorkloadClass: "no_savings_control",
			FramesPath:    framesPath,
			ExpectedReducers: []string{
				"host_budget_ok",
			},
			ExpectedZeroSavings: true,
			LiveDelta: &codexCaptureLiveDelta{
				HostBudgetStatus:        "ok",
				HostBudgetCompressionOK: true,
				HostBudgetDegradationOK: true,
			},
		},
		wssProofMatrixRecord{
			ID:               "stale-expected-good-live",
			Client:           "cli",
			WorkloadClass:    "chunk_dedup_log_output",
			FramesPath:       framesPath,
			ExpectedReducers: []string{"chunk_dedup", "chunk_dedup_refs", "host_budget_ok"},
			LiveDelta: &codexCaptureLiveDelta{
				BillableInputTokensSaved: 20,
				ProxyLayer0Envelope:      1,
				HostBudgetStatus:         "ok",
				HostBudgetCompressionOK:  true,
				HostBudgetDegradationOK:  true,
			},
		},
		wssProofMatrixRecord{
			ID:                  "bad-zero",
			Client:              "desktop",
			WorkloadClass:       "no_savings_control",
			FramesPath:          framesPath,
			ExpectedZeroSavings: true,
			LiveDelta: &codexCaptureLiveDelta{
				BillableInputTokensSaved: 1,
				HostBudgetStatus:         "ok",
				HostBudgetCompressionOK:  true,
				HostBudgetDegradationOK:  true,
			},
		},
		wssProofMatrixRecord{
			ID:            "bad-host",
			Client:        "cli",
			WorkloadClass: "host_resource_long_workday",
			FramesPath:    framesPath,
			LiveDelta: &codexCaptureLiveDelta{
				ProviderCacheReadTokens: 100,
				HostBudgetStatus:        "attention",
				HostBudgetExceeded:      true,
				HostBudgetCompressionOK: true,
				HostBudgetDegradationOK: true,
			},
		},
		wssProofMatrixRecord{
			ID:               "bad-reducer",
			Client:           "cli",
			WorkloadClass:    "repeat_full_read",
			FramesPath:       framesPath,
			ExpectedReducers: []string{"read_delta"},
			LiveDelta: &codexCaptureLiveDelta{
				BillableInputTokensSaved: 10,
				HostBudgetStatus:         "ok",
				HostBudgetCompressionOK:  true,
				HostBudgetDegradationOK:  true,
			},
		},
		wssProofMatrixRecord{
			ID:               "bad-prefix-token-only",
			Client:           "cli",
			WorkloadClass:    "prefix_elision_tool_oracle",
			FramesPath:       framesPath,
			ExpectedReducers: []string{"wss_stateful_prefix_elision"},
			LiveDelta: &codexCaptureLiveDelta{
				BillableInputTokensSaved:         10,
				WSSStatefulPrefixElisionRequests: 1,
				WSSStatefulPrefixElisionTokens:   10,
				HostBudgetStatus:                 "ok",
				HostBudgetCompressionOK:          true,
				HostBudgetDegradationOK:          true,
			},
		},
		wssProofMatrixRecord{
			ID:                 "bad-prefix-tool-suppressed",
			Client:             "cli",
			WorkloadClass:      "prefix_elision_tool_oracle",
			FramesPath:         framesPath,
			ExpectedReducers:   []string{"wss_stateful_prefix_elision"},
			MinFunctionCalls:   3,
			MinFunctionOutputs: 3,
			LiveDelta: &codexCaptureLiveDelta{
				BillableInputTokensSaved:         10,
				WSSStatefulPrefixElisionRequests: 1,
				WSSStatefulPrefixElisionTokens:   10,
				HostBudgetStatus:                 "ok",
				HostBudgetCompressionOK:          true,
				HostBudgetDegradationOK:          true,
			},
		},
		wssProofMatrixRecord{
			ID:            "bad-missing-live",
			Client:        "cli",
			WorkloadClass: "repeat_full_read",
			FramesPath:    framesPath,
		},
	)

	report, err := writeWSSProofCleanMatrix(inputPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if report.RowsRead != 9 || report.RowsWritten != 3 || report.RowsSkipped != 6 || report.RowsNormalized != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	for _, reason := range []string{"expected_zero_local_savings", "host_budget", "expected_reducer_miss", "prefix_elision_tool_oracle", "function_call_minima", "missing_live_delta"} {
		if report.SkippedReasons[reason] != 1 {
			t.Fatalf("missing skip reason %s in %+v", reason, report.SkippedReasons)
		}
	}
	rows, err := readWSSProofInventoryRows(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].ID != "clean-repeat" || rows[1].ID != "clean-zero" || rows[2].ID != "stale-expected-good-live" {
		t.Fatalf("unexpected clean rows: %+v", rows)
	}
	if strings.Join(rows[2].ExpectedReducers, ",") != "codex_exec_envelope,host_budget_ok" {
		t.Fatalf("stale expected reducers were not normalized to observed signals: %+v", rows[2].ExpectedReducers)
	}
}

func TestWSSProofCleanMatrixRefusesOverwrite(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.jsonl")
	outputPath := filepath.Join(dir, "clean.jsonl")
	writeJSONLFile(t, inputPath, wssProofMatrixRecord{
		ID:            "clean",
		Client:        "cli",
		WorkloadClass: "repeat_full_read",
		FramesPath:    filepath.Join(dir, "frames.jsonl"),
		LiveDelta: &codexCaptureLiveDelta{
			BillableInputTokensSaved: 1,
			HostBudgetStatus:         "ok",
			HostBudgetCompressionOK:  true,
			HostBudgetDegradationOK:  true,
		},
	})
	if err := os.WriteFile(outputPath, []byte("existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := writeWSSProofCleanMatrix(inputPath, outputPath); err == nil {
		t.Fatal("expected overwrite refusal")
	}
}

func TestRunWSSProofCleanMatrixJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.jsonl")
	outputPath := filepath.Join(dir, "clean.jsonl")
	writeJSONLFile(t, inputPath, wssProofMatrixRecord{
		ID:            "clean",
		Client:        "cli",
		WorkloadClass: "repeat_full_read",
		FramesPath:    filepath.Join(dir, "frames.jsonl"),
		LiveDelta: &codexCaptureLiveDelta{
			BillableInputTokensSaved: 1,
			HostBudgetStatus:         "ok",
			HostBudgetCompressionOK:  true,
			HostBudgetDegradationOK:  true,
		},
	})

	var stdout, stderr bytes.Buffer
	code := runWSSProofCleanMatrix([]string{inputPath, outputPath, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"rows_written": 1`) {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

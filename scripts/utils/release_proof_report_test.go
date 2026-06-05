package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseProofReportSeparatesEconomicsAndRequiresProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofControlFrames(t, framesPath, "release-separate")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:               "cli-tool",
		Client:           "cli",
		WorkloadClass:    "tool_heavy",
		FramesPath:       framesPath,
		ExpectedReducers: []string{"tool_prune", "tool_prune_tokens_saved", "host_budget_ok"},
		LiveDelta: &codexCaptureLiveDelta{
			BillableInputTokensSaved:         10,
			InputTokensSaved:                 11,
			RequestSideBytesReduced:          12,
			OutputWireBytesSaved:             13,
			ProviderCacheReadTokens:          14,
			ProviderCacheCreateTokens:        15,
			ToolPrunePruned:                  1,
			ToolPruneTokensSaved:             16,
			OutputReduceInjected:             1,
			OutputReduceInputOverheadTokens:  5,
			OutputReduceOutputTokensObserved: 17,
			HostBudgetStatus:                 "ok",
			HostBudgetCompressionOK:          true,
			HostBudgetDegradationOK:          true,
		},
	})

	report, err := loadReleaseProofReport(releaseProofReportFlags{matrixPath: matrixPath})
	if err != nil {
		t.Fatal(err)
	}
	if report.Economics.LocalBillableInputTokensSaved != 10 ||
		report.Economics.OutputWireBytesSaved != 13 ||
		report.Economics.ProviderCacheReadTokens != 14 ||
		report.Economics.ToolPruneTokensSaved != 16 ||
		report.Economics.OutputReduceInputOverhead != 5 ||
		report.Economics.OutputReduceObservedTokens != 17 ||
		report.Economics.OutputReduceNetObservedTokens != 12 {
		t.Fatalf("economics were not kept separate: %+v", report.Economics)
	}
	if report.GatePassed || !strings.Contains(strings.Join(report.GateFailures, "\n"), "missing --resource-profile-proof bundle directories for cli and desktop") {
		t.Fatalf("report must fail closed without resource proof: %+v", report)
	}

	profilePath := filepath.Join(dir, "resource-profile.txt")
	writeTextFile(t, profilePath, "sample profile\n")
	report, err = loadReleaseProofReport(releaseProofReportFlags{
		matrixPath:            matrixPath,
		resourceProfileProofs: []string{profilePath},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.ResourceProfileProofOK || !strings.Contains(strings.Join(report.ResourceProfileProofIssues, "\n"), "bundle directory") {
		t.Fatalf("plain resource proof file must not pass: %+v", report.ResourceProfileProofIssues)
	}
}

func TestReleaseProofReportPassesWithCompleteMatrixAndProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofControlFrames(t, framesPath, "release-complete")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	cliBundle := writeReleaseResourceProofBundle(t, dir, "cli")
	desktopBundle := writeReleaseResourceProofBundle(t, dir, "desktop")

	var rows []interface{}
	for i, workload := range requiredWSSProofWorkloads {
		rows = append(rows, releaseProofRow("release-"+workload, proofClientForIndex(i), workload, framesPath, &codexCaptureLiveDelta{
			BillableInputTokensSaved: 1,
			ProxyLayer0ReadDelta:     1,
			HostBudgetStatus:         "ok",
			HostBudgetCompressionOK:  true,
			HostBudgetDegradationOK:  true,
		}))
	}
	rows = append(rows,
		releaseProofRow("chunk-similar", "cli", "chunk_dedup_similar_outputs", framesPath, &codexCaptureLiveDelta{
			BillableInputTokensSaved: 2,
			ProxyLayer0ChunkDedup:    1,
			ProxyLayer0ChunkRefs:     1,
			HostBudgetStatus:         "ok",
			HostBudgetCompressionOK:  true,
			HostBudgetDegradationOK:  true,
		}),
		releaseProofRow("chunk-log", "cli", "chunk_dedup_log_output", framesPath, &codexCaptureLiveDelta{
			BillableInputTokensSaved: 3,
			ProxyLayer0Captured:      1,
			HostBudgetStatus:         "ok",
			HostBudgetCompressionOK:  true,
			HostBudgetDegradationOK:  true,
		}),
		releaseProofRow("chunk-test", "desktop", "chunk_dedup_test_output", framesPath, &codexCaptureLiveDelta{
			BillableInputTokensSaved: 4,
			ProxyLayer0Envelope:      1,
			HostBudgetStatus:         "ok",
			HostBudgetCompressionOK:  true,
			HostBudgetDegradationOK:  true,
		}),
		releaseProofRow("output", "desktop", "output_reduce_aggressive", framesPath, &codexCaptureLiveDelta{
			OutputReduceInjected:             1,
			OutputReduceInputOverheadTokens:  8,
			OutputReduceOutputTokensObserved: 50,
			HostBudgetStatus:                 "ok",
			HostBudgetCompressionOK:          true,
			HostBudgetDegradationOK:          true,
		}),
		releaseProofRow("tool", "desktop", "tool_heavy", framesPath, &codexCaptureLiveDelta{
			ToolPrunePruned:         1,
			ToolPruneTokensSaved:    5,
			HostBudgetStatus:        "ok",
			HostBudgetCompressionOK: true,
			HostBudgetDegradationOK: true,
		}),
		releaseProofRow("provider", "cli", "provider_cache_long_session", framesPath, &codexCaptureLiveDelta{
			ProviderCacheReadTokens: 100,
			HostBudgetStatus:        "ok",
			HostBudgetCompressionOK: true,
			HostBudgetDegradationOK: true,
		}),
		releaseProofRow("host", "desktop", "host_resource_long_workday", framesPath, &codexCaptureLiveDelta{
			ProviderCacheReadTokens: 200,
			HostBudgetStatus:        "ok",
			HostBudgetCompressionOK: true,
			HostBudgetDegradationOK: true,
		}),
	)
	writeJSONLFile(t, matrixPath, rows...)

	report, err := loadReleaseProofReport(releaseProofReportFlags{
		matrixPath:            matrixPath,
		resourceProfileProofs: []string{cliBundle, desktopBundle},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed {
		t.Fatalf("complete report should pass: %+v", report.GateFailures)
	}
	if report.Economics.LocalBillableInputTokensSaved == 0 ||
		report.Economics.ProviderCacheReadTokens == 0 ||
		report.Economics.ToolPruneTokensSaved == 0 ||
		report.Economics.OutputReduceInputOverhead == 0 ||
		report.Economics.OutputReduceObservedTokens == 0 ||
		report.Economics.OutputReduceNetObservedTokens == 0 {
		t.Fatalf("missing separated economics: %+v", report.Economics)
	}
}

func TestReleaseProofReportRequiresCLIAndDesktopResourceBundles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofControlFrames(t, framesPath, "release-requires-both-clients")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeCompleteReleaseProofRows(t, matrixPath, framesPath)

	cliBundle := writeReleaseResourceProofBundle(t, dir, "cli")
	report, err := loadReleaseProofReport(releaseProofReportFlags{
		matrixPath:            matrixPath,
		resourceProfileProofs: []string{cliBundle},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.GatePassed || !strings.Contains(strings.Join(report.GateFailures, "\n"), "missing valid resource proof bundle for desktop") {
		t.Fatalf("single-client resource proof must fail closed: %+v", report.GateFailures)
	}
}

func TestReleaseProofReportRejectsAnomalyRows(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofControlFrames(t, framesPath, "release-anomaly")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeCompleteReleaseProofRows(t, matrixPath, framesPath)
	appendJSONLFile(t, matrixPath,
		wssProofMatrixRecord{
			ID:                  "zero-violation-row",
			Client:              "cli",
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
			ID:            "host-attention-row",
			Client:        "desktop",
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
	)

	report, err := loadReleaseProofReport(releaseProofReportFlags{
		matrixPath:            matrixPath,
		resourceProfileProofs: []string{writeReleaseResourceProofBundle(t, dir, "cli"), writeReleaseResourceProofBundle(t, dir, "desktop")},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(report.GateFailures, "\n")
	if report.GatePassed ||
		!strings.Contains(joined, "host-attention-row") ||
		!strings.Contains(joined, "zero-violation-row") {
		t.Fatalf("anomaly rows must fail release proof gate: passed=%v failures=%v", report.GatePassed, report.GateFailures)
	}
}

func TestReleaseProofReportReportsOutputReduceOverheadWithoutCounterfactualClaim(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofControlFrames(t, framesPath, "release-output-overhead")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeCompleteReleaseProofRows(t, matrixPath, framesPath)
	appendJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "output-overhead-row",
		Client:        "cli",
		WorkloadClass: "output_reduce_aggressive",
		FramesPath:    framesPath,
		LiveDelta: &codexCaptureLiveDelta{
			OutputReduceInjected:             1,
			OutputReduceInputOverheadTokens:  1000,
			OutputReduceOutputTokensObserved: 100,
			HostBudgetStatus:                 "ok",
			HostBudgetCompressionOK:          true,
			HostBudgetDegradationOK:          true,
		},
	})

	report, err := loadReleaseProofReport(releaseProofReportFlags{
		matrixPath:            matrixPath,
		resourceProfileProofs: []string{writeReleaseResourceProofBundle(t, dir, "cli"), writeReleaseResourceProofBundle(t, dir, "desktop")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed {
		t.Fatalf("release proof should report output-reduce overhead without inventing a counterfactual failure: %+v", report.GateFailures)
	}
	if report.Economics.OutputReduceInputOverhead <= report.Economics.OutputReduceObservedTokens {
		t.Fatalf("test fixture should exercise overhead-dominates-observed reporting: %+v", report.Economics)
	}
}

func TestReleaseResourceProofBundleRejectsBadHostBudget(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	resourceBundle := writeReleaseResourceProofBundle(t, dir, "cli")
	writeTextFile(t, filepath.Join(resourceBundle, "admin-before.json"), `{
  "host_budget": {
    "status": "unknown",
    "rss_bytes": 10,
    "cpu_window_seconds": 1,
    "compression_ok": true,
    "degradation_ok": true
  }
}`)
	writeTextFile(t, filepath.Join(resourceBundle, "admin-after.json"), `{
  "host_budget": {
    "status": "exceeded",
    "exceeded": true,
    "rss_bytes": 10,
    "cpu_window_seconds": 1,
    "compression_ok": true,
    "degradation_ok": true
  }
}`)

	result := validateReleaseResourceProof(resourceBundle)
	joined := strings.Join(result.Issues, "\n")
	if result.OK ||
		!strings.Contains(joined, "admin-before host_budget status is not ok") ||
		!strings.Contains(joined, "admin-after host_budget status is not ok") {
		t.Fatalf("bad host budget must fail validation: %+v", result)
	}
}

func TestReleaseResourceProofBundleRejectsMissingExpectedReducers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	resourceBundle := writeReleaseResourceProofBundle(t, dir, "cli")
	writeJSONLFile(t, filepath.Join(resourceBundle, "matrix.jsonl"), wssProofMatrixRecord{
		ID:               "host-resource-cli",
		Client:           "cli",
		WorkloadClass:    "host_resource_long_workday",
		FramesPath:       filepath.Join(resourceBundle, "frames.jsonl"),
		ExpectedReducers: []string{"host_budget_ok", "read_delta"},
		LiveDelta: &codexCaptureLiveDelta{
			ProviderCacheReadTokens: 100,
			HostBudgetStatus:        "ok",
			HostBudgetCompressionOK: true,
			HostBudgetDegradationOK: true,
		},
	})

	result := validateReleaseResourceProof(resourceBundle)
	if result.OK || !strings.Contains(strings.Join(result.Issues, "\n"), "matrix.jsonl has no positive host_resource_long_workday row with host_budget_ok") {
		t.Fatalf("missing expected reducer must fail resource proof validation: %+v", result)
	}
}

func TestRunReleaseProofReportExitCodes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := runReleaseProofReport([]string{"--help"}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "release-proof-report") {
		t.Fatalf("help failed code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func releaseProofRow(id, client, workload, framesPath string, live *codexCaptureLiveDelta) wssProofMatrixRecord {
	return wssProofMatrixRecord{
		ID:            id,
		Client:        client,
		WorkloadClass: workload,
		FramesPath:    framesPath,
		LiveDelta:     live,
	}
}

func proofClientForIndex(i int) string {
	if i%2 == 0 {
		return "cli"
	}
	return "desktop"
}

func writeCompleteReleaseProofRows(t *testing.T, matrixPath, framesPath string) {
	t.Helper()
	var rows []interface{}
	for i, workload := range requiredWSSProofWorkloads {
		rows = append(rows, releaseProofRow("release-"+workload, proofClientForIndex(i), workload, framesPath, &codexCaptureLiveDelta{
			BillableInputTokensSaved: 1,
			ProxyLayer0ReadDelta:     1,
			HostBudgetStatus:         "ok",
			HostBudgetCompressionOK:  true,
			HostBudgetDegradationOK:  true,
		}))
	}
	rows = append(rows,
		releaseProofRow("chunk-similar", "cli", "chunk_dedup_similar_outputs", framesPath, &codexCaptureLiveDelta{
			BillableInputTokensSaved: 2,
			ProxyLayer0ChunkDedup:    1,
			ProxyLayer0ChunkRefs:     1,
			HostBudgetStatus:         "ok",
			HostBudgetCompressionOK:  true,
			HostBudgetDegradationOK:  true,
		}),
		releaseProofRow("chunk-log", "cli", "chunk_dedup_log_output", framesPath, &codexCaptureLiveDelta{
			BillableInputTokensSaved: 3,
			ProxyLayer0Captured:      1,
			HostBudgetStatus:         "ok",
			HostBudgetCompressionOK:  true,
			HostBudgetDegradationOK:  true,
		}),
		releaseProofRow("chunk-test", "desktop", "chunk_dedup_test_output", framesPath, &codexCaptureLiveDelta{
			BillableInputTokensSaved: 4,
			ProxyLayer0Envelope:      1,
			HostBudgetStatus:         "ok",
			HostBudgetCompressionOK:  true,
			HostBudgetDegradationOK:  true,
		}),
		releaseProofRow("output", "desktop", "output_reduce_aggressive", framesPath, &codexCaptureLiveDelta{
			OutputReduceInjected:             1,
			OutputReduceInputOverheadTokens:  8,
			OutputReduceOutputTokensObserved: 50,
			HostBudgetStatus:                 "ok",
			HostBudgetCompressionOK:          true,
			HostBudgetDegradationOK:          true,
		}),
		releaseProofRow("tool", "desktop", "tool_heavy", framesPath, &codexCaptureLiveDelta{
			ToolPrunePruned:         1,
			ToolPruneTokensSaved:    5,
			HostBudgetStatus:        "ok",
			HostBudgetCompressionOK: true,
			HostBudgetDegradationOK: true,
		}),
		releaseProofRow("provider", "cli", "provider_cache_long_session", framesPath, &codexCaptureLiveDelta{
			ProviderCacheReadTokens: 100,
			HostBudgetStatus:        "ok",
			HostBudgetCompressionOK: true,
			HostBudgetDegradationOK: true,
		}),
		releaseProofRow("host", "desktop", "host_resource_long_workday", framesPath, &codexCaptureLiveDelta{
			ProviderCacheReadTokens: 200,
			HostBudgetStatus:        "ok",
			HostBudgetCompressionOK: true,
			HostBudgetDegradationOK: true,
		}),
	)
	writeJSONLFile(t, matrixPath, rows...)
}

func writeTextFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendJSONLFile(t *testing.T, path string, values ...interface{}) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, value := range values {
		if err := enc.Encode(value); err != nil {
			t.Fatal(err)
		}
	}
}

func writeReleaseResourceProofBundle(t *testing.T, dir, client string) string {
	t.Helper()
	bundle := filepath.Join(dir, "resource-proof-"+client)
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	hostBudgetJSON := `{
  "host_budget": {
    "status": "ok",
    "rss_bytes": 10,
    "cpu_window_seconds": 1,
    "compression_ok": true,
    "degradation_ok": true
  }
}`
	writeTextFile(t, filepath.Join(bundle, "admin-before.json"), hostBudgetJSON)
	writeTextFile(t, filepath.Join(bundle, "admin-after.json"), hostBudgetJSON)
	writeTextFile(t, filepath.Join(bundle, "ps-before.txt"), "PID RSS %CPU COMMAND\n1 10 0.1 slimference\n")
	writeTextFile(t, filepath.Join(bundle, "ps-after.txt"), "PID RSS %CPU COMMAND\n1 11 0.1 slimference\n")
	writeTextFile(t, filepath.Join(bundle, "slimference.sample.txt"), "sample profile\n")
	writeTextFile(t, filepath.Join(bundle, "workday-finish.json"), `{
  "schema_version": 1,
  "current": {
    "host_budget": {
      "status": "ok",
      "rss_bytes": 11,
      "cpu_window_seconds": 1,
      "compression_ok": true,
      "degradation_ok": true
    }
  },
  "delta": {
    "host_budget": {
      "status": "ok",
      "rss_bytes": 11,
      "cpu_window_seconds": 1,
      "compression_ok": true,
      "degradation_ok": true
    },
    "wss": {
      "parse_failures": 0,
      "degraded_sessions": 0,
      "compression_errors": 0
    }
  }
}`)
	writeJSONLFile(t, filepath.Join(bundle, "matrix.jsonl"), releaseProofRow("host-resource-"+client, client, "host_resource_long_workday", filepath.Join(bundle, "frames.jsonl"), &codexCaptureLiveDelta{
		ProviderCacheReadTokens: 100,
		HostBudgetStatus:        "ok",
		HostBudgetCompressionOK: true,
		HostBudgetDegradationOK: true,
	}))
	return bundle
}

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dbg "github.com/slimference/slimference/internal/debug"
)

func TestWSSProofExportCorpusWritesScrubbedLiveCategories(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath,
		wssProofMatrixRecord{
			ID:            "desktop-search",
			Client:        "desktop",
			WorkloadClass: "search_loop",
			FramesPath:    filepath.Join(dir, "frames-search.jsonl"),
			Model:         "gpt-5.5",
			LiveDelta: &codexCaptureLiveDelta{
				BillableInputTokensSaved: 900,
				ProxyLayer0Captured:      1,
				HostBudgetStatus:         "ok",
				HostBudgetCompressionOK:  true,
				HostBudgetDegradationOK:  true,
			},
		},
		wssProofMatrixRecord{
			ID:            "cli-provider-cache",
			Client:        "cli",
			WorkloadClass: "provider_cache_long_session",
			FramesPath:    filepath.Join(dir, "frames-provider.jsonl"),
			Model:         "gpt-5.5",
			LiveDelta: &codexCaptureLiveDelta{
				ProviderCacheReadTokens:   3456,
				ProviderCacheCreateTokens: 512,
				HostBudgetStatus:          "ok",
				HostBudgetCompressionOK:   true,
				HostBudgetDegradationOK:   true,
			},
		},
		wssProofMatrixRecord{
			ID:            "cli-large-tool-output",
			Client:        "cli",
			WorkloadClass: "large_tool_output",
			FramesPath:    filepath.Join(dir, "frames-large.jsonl"),
			Model:         "gpt-5.5",
			LiveDelta: &codexCaptureLiveDelta{
				BillableInputTokensSaved: 2048,
				ProxyLayer0Envelope:      1,
				HostBudgetStatus:         "ok",
				HostBudgetCompressionOK:  true,
				HostBudgetDegradationOK:  true,
			},
		},
		wssProofMatrixRecord{
			ID:            "desktop-tool-heavy",
			Client:        "desktop",
			WorkloadClass: "tool_heavy",
			FramesPath:    filepath.Join(dir, "frames-tool-heavy.jsonl"),
			Model:         "gpt-5.5",
			LiveDelta: &codexCaptureLiveDelta{
				ToolPrunePruned:         1,
				ToolPruneTokensSaved:    26,
				HostBudgetStatus:        "ok",
				HostBudgetCompressionOK: true,
				HostBudgetDegradationOK: true,
			},
		},
		wssProofMatrixRecord{
			ID:            "cli-output-no-output-tokens",
			Client:        "cli",
			WorkloadClass: "output_reduce_aggressive",
			FramesPath:    filepath.Join(dir, "frames-output.jsonl"),
			Model:         "gpt-5.5",
			LiveDelta: &codexCaptureLiveDelta{
				OutputReduceInjected:    1,
				HostBudgetStatus:        "ok",
				HostBudgetCompressionOK: true,
				HostBudgetDegradationOK: true,
			},
		},
		wssProofMatrixRecord{
			ID:            "cli-output",
			Client:        "cli",
			WorkloadClass: "output_reduce_aggressive",
			FramesPath:    filepath.Join(dir, "frames-output-ok.jsonl"),
			Model:         "gpt-5.5",
			LiveDelta: &codexCaptureLiveDelta{
				OutputReduceInjected:             1,
				OutputReduceInputOverheadTokens:  7,
				OutputReduceOutputTokensObserved: 42,
				HostBudgetStatus:                 "ok",
				HostBudgetCompressionOK:          true,
				HostBudgetDegradationOK:          true,
			},
		},
		wssProofMatrixRecord{
			ID:            "bad-row",
			Client:        "cli",
			WorkloadClass: "search_loop",
			FramesPath:    filepath.Join(dir, "frames-bad.jsonl"),
			LiveDelta: &codexCaptureLiveDelta{
				BillableInputTokensSaved: 1,
				ParseFailures:            1,
			},
		},
	)

	root := filepath.Join(dir, "corpus")
	report, err := exportWSSProofCorpus(matrixPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if report.RowsRead != 7 || report.RowsExported != 5 || report.RowsSkipped != 2 ||
		report.CategoriesWritten != 5 || report.SkippedReasons["safety_issue"] != 1 ||
		report.SkippedReasons["no_economic_signal"] != 1 {
		t.Fatalf("bad export report: %+v", report)
	}

	searchMeta := readExportMetadata(t, filepath.Join(root, "desktop_search_loop", "metadata.json"))
	if searchMeta.ClientFamily != "codex_desktop" || searchMeta.WorkloadClass != "search_loop" ||
		searchMeta.Synthetic || searchMeta.EvidenceLevel != "live_operator" ||
		searchMeta.ExpectedSavingsMin != 0 || searchMeta.ExpectedSavedTokensMin != 900 ||
		searchMeta.ExpectedMaxErrors != 0 {
		t.Fatalf("bad search metadata: %+v", searchMeta)
	}

	providerMeta := readExportMetadata(t, filepath.Join(root, "cli_provider_cache_long_session", "metadata.json"))
	if providerMeta.ClientFamily != "codex_cli" || providerMeta.WorkloadClass != "provider_cache_long_session" ||
		providerMeta.ExpectedSavingsMin != 0 || providerMeta.ExpectedSavedTokensMin != 0 ||
		providerMeta.ExpectedProviderCacheReadMin != 3456 ||
		!containsString(providerMeta.ScenarioValidators, "cache_reuse") {
		t.Fatalf("bad provider metadata: %+v", providerMeta)
	}

	largeMeta := readExportMetadata(t, filepath.Join(root, "cli_large_tool_output", "metadata.json"))
	if largeMeta.ClientFamily != "codex_cli" || largeMeta.WorkloadClass != "large_tool_output" ||
		largeMeta.ExpectedSavedTokensMin != 2048 || largeMeta.ExpectedMaxErrors != 0 {
		t.Fatalf("bad large-tool-output metadata: %+v", largeMeta)
	}

	toolMeta := readExportMetadata(t, filepath.Join(root, "desktop_tool_heavy", "metadata.json"))
	if toolMeta.ClientFamily != "codex_desktop" || toolMeta.WorkloadClass != "tool_heavy" ||
		toolMeta.ExpectedSavedTokensMin != 26 ||
		!containsString(toolMeta.ScenarioValidators, "tool_heavy") ||
		!containsString(toolMeta.ScenarioValidators, "host_budget_ok") {
		t.Fatalf("bad tool-heavy metadata: %+v", toolMeta)
	}

	rec := readFirstExportSummary(t, filepath.Join(root, "cli_provider_cache_long_session", "session_wss_proof_export_001.jsonl"))
	if rec.CacheReadTokens != 3456 || rec.ProviderCachedTokens != 3456 ||
		rec.Tokens.Original != 0 || rec.Tokens.Final != 0 ||
		rec.Tokens.Saved != 3456 || rec.Provider != "codex_chatgpt" ||
		rec.Source != "wss-proof-export" {
		t.Fatalf("bad provider summary: %+v", rec)
	}

	toolRec := readFirstExportSummary(t, filepath.Join(root, "desktop_tool_heavy", "session_wss_proof_export_001.jsonl"))
	if !toolRec.ToolPrune.Applied || toolRec.ToolPrune.PrunedTools != 1 || toolRec.ToolPrune.SavedTokens != 26 ||
		toolRec.Tokens.Saved != 26 {
		t.Fatalf("bad tool-heavy summary: %+v", toolRec)
	}

	outputMeta := readExportMetadata(t, filepath.Join(root, "cli_output_reduce_aggressive", "metadata.json"))
	if outputMeta.ExpectedOutputReduceAppliedMin != 1 || outputMeta.ExpectedOutputReduceOverheadMax != 7 ||
		outputMeta.ExpectedOutputReduceNetObservedMin != 35 ||
		!containsString(outputMeta.ScenarioValidators, "output_reduce") {
		t.Fatalf("bad output-reduce metadata: %+v", outputMeta)
	}
	outputRec := readFirstExportSummary(t, filepath.Join(root, "cli_output_reduce_aggressive", "session_wss_proof_export_001.jsonl"))
	if !outputRec.OutputReduce.Applied || outputRec.OutputReduce.AddedTokens != 7 || outputRec.OutputTokens != 42 {
		t.Fatalf("bad output-reduce summary: %+v", outputRec)
	}
}

func TestRunWSSProofExportCorpusJSON(t *testing.T) {
	dir := t.TempDir()
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "cli-output",
		Client:        "cli",
		WorkloadClass: "output_reduce_aggressive",
		FramesPath:    filepath.Join(dir, "frames-output.jsonl"),
		LiveDelta: &codexCaptureLiveDelta{
			OutputReduceInjected:             1,
			OutputReduceInputOverheadTokens:  5,
			OutputReduceOutputTokensObserved: 42,
			HostBudgetStatus:                 "ok",
			HostBudgetCompressionOK:          true,
			HostBudgetDegradationOK:          true,
		},
	})
	var stdout, stderr bytes.Buffer
	code := runWSSProofExportCorpus([]string{matrixPath, filepath.Join(dir, "corpus"), "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"rows_exported": 1`) ||
		!strings.Contains(stdout.String(), `"output_reduce_aggressive"`) {
		t.Fatalf("unexpected json report: %s", stdout.String())
	}
}

func readExportMetadata(t *testing.T, path string) CategoryMetadataLite {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var meta CategoryMetadataLite
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	return meta
}

func readFirstExportSummary(t *testing.T, path string) dbg.RequestSummary {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(strings.SplitN(string(raw), "\n", 2)[0])
	var rec dbg.RequestSummary
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
)

func TestWSSProofExportCorpusWritesScrubbedLiveCategories(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	framesSearchPath := filepath.Join(dir, "frames-search.jsonl")
	writeJSONLFile(t, framesSearchPath, map[string]any{
		"direction": "server_to_client",
		"payload": map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"usage": map[string]any{"input_tokens": 2100, "output_tokens": 33},
			},
		},
	})
	writeJSONLFile(t, matrixPath,
		wssProofMatrixRecord{
			ID:            "desktop-search",
			Client:        "desktop",
			WorkloadClass: "search_loop",
			FramesPath:    framesSearchPath,
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
			ID:            "cli-ok-summary-mypy",
			Client:        "cli",
			WorkloadClass: "ok_summary_mypy_product",
			FramesPath:    filepath.Join(dir, "frames-ok-summary.jsonl"),
			Model:         "gpt-5.5",
			LiveDelta: &codexCaptureLiveDelta{
				BillableInputTokensSaved: 765,
				ProviderInputTokens:      101259,
				ProviderOutputTokens:     602,
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
	if report.RowsRead != 8 || report.RowsExported != 6 || report.RowsSkipped != 2 ||
		report.CategoriesWritten != 6 || report.SkippedReasons["safety_issue"] != 1 ||
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

	okSummaryMeta := readExportMetadata(t, filepath.Join(root, "cli_ok_summary_tool_output", "metadata.json"))
	if okSummaryMeta.ClientFamily != "codex_cli" || okSummaryMeta.WorkloadClass != "ok_summary_tool_output" ||
		okSummaryMeta.ExpectedSavedTokensMin != 765 || okSummaryMeta.ExpectedMaxErrors != 0 ||
		!containsString(okSummaryMeta.ScenarioValidators, "host_budget_ok") {
		t.Fatalf("bad ok-summary metadata: %+v", okSummaryMeta)
	}

	toolMeta := readExportMetadata(t, filepath.Join(root, "desktop_tool_heavy", "metadata.json"))
	if toolMeta.ClientFamily != "codex_desktop" || toolMeta.WorkloadClass != "tool_heavy" ||
		toolMeta.ExpectedSavedTokensMin != 26 ||
		!containsString(toolMeta.ScenarioValidators, "tool_heavy") ||
		!containsString(toolMeta.ScenarioValidators, "host_budget_ok") {
		t.Fatalf("bad tool-heavy metadata: %+v", toolMeta)
	}

	rec := readFirstExportSummary(t, filepath.Join(root, "cli_provider_cache_long_session", "session_wss_proof_export_001.jsonl"))
	if rec.CacheReadTokens != 0 || rec.ProviderCachedTokens != 3456 ||
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
	searchRec := readFirstExportSummary(t, filepath.Join(root, "desktop_search_loop", "session_wss_proof_export_001.jsonl"))
	if searchRec.ProviderInputTokens != 2100 || searchRec.ProviderOutputTokens != 33 ||
		searchRec.Tokens.Original != 3000 || searchRec.Tokens.Final != 2100 ||
		searchRec.Tokens.Saved != 900 {
		t.Fatalf("bad search summary denominator: %+v", searchRec)
	}
	if searchRec.DebugFacts["wss.tool_command_classes"] != "rg_search=1" ||
		searchRec.DebugFacts["wss.tool_command_classed"] != "1" ||
		searchRec.DebugFacts["wss.tool_command_unclassed"] != "0" {
		t.Fatalf("search export should carry content-free command facts: %+v", searchRec.DebugFacts)
	}

	okSummaryRec := readFirstExportSummary(t, filepath.Join(root, "cli_ok_summary_tool_output", "session_wss_proof_export_001.jsonl"))
	if okSummaryRec.ProviderInputTokens != 101259 || okSummaryRec.ProviderOutputTokens != 602 ||
		okSummaryRec.Tokens.Original != 102024 || okSummaryRec.Tokens.Final != 101259 ||
		okSummaryRec.Tokens.Saved != 765 {
		t.Fatalf("bad ok-summary denominator: %+v", okSummaryRec)
	}
	if okSummaryRec.DebugFacts["wss.tool_command_classes"] != "mypy=1" {
		t.Fatalf("mypy export should carry precise command facts: %+v", okSummaryRec.DebugFacts)
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

func TestWSSProofExportCorpusAddsContentFreeReadDependencyFacts(t *testing.T) {
	dir := t.TempDir()
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath,
		wssProofMatrixRecord{
			ID:            "cli-repeat-read",
			Client:        "cli",
			WorkloadClass: "repeat_full_read",
			FramesPath:    filepath.Join(dir, "frames-repeat.jsonl"),
			LiveDelta: &codexCaptureLiveDelta{
				BillableInputTokensSaved: 1200,
				HostBudgetStatus:         "ok",
				HostBudgetCompressionOK:  true,
				HostBudgetDegradationOK:  true,
			},
		},
		wssProofMatrixRecord{
			ID:            "cli-ranged-read",
			Client:        "cli",
			WorkloadClass: "ranged_read",
			FramesPath:    filepath.Join(dir, "frames-ranged.jsonl"),
			LiveDelta: &codexCaptureLiveDelta{
				BillableInputTokensSaved: 400,
				HostBudgetStatus:         "ok",
				HostBudgetCompressionOK:  true,
				HostBudgetDegradationOK:  true,
			},
		},
		wssProofMatrixRecord{
			ID:            "cli-edit-read",
			Client:        "cli",
			WorkloadClass: "apply_patch_then_read",
			FramesPath:    filepath.Join(dir, "frames-edit-read.jsonl"),
			LiveDelta: &codexCaptureLiveDelta{
				BillableInputTokensSaved: 700,
				HostBudgetStatus:         "ok",
				HostBudgetCompressionOK:  true,
				HostBudgetDegradationOK:  true,
			},
		},
	)

	root := filepath.Join(dir, "corpus")
	report, err := exportWSSProofCorpus(matrixPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if report.RowsRead != 3 || report.RowsExported != 3 {
		t.Fatalf("bad export report: %+v", report)
	}
	repeat := readFirstExportSummary(t, filepath.Join(root, "cli_repeat_read", "session_wss_proof_export_001.jsonl"))
	if repeat.DebugFacts["wss.tool_command_classes"] != "read_like=1" ||
		repeat.DebugFacts["wss.dependency_trace"] != "true" ||
		repeat.DebugFacts["wss.read_trace_requests"] != "1" ||
		repeat.DebugFacts["wss.read_full_count"] != "1" ||
		repeat.DebugFacts["wss.read_file_path_hash"] == "" ||
		repeat.DebugFacts["wss.read_range"] != "full" {
		t.Fatalf("repeat-read export missing trace facts: %+v", repeat.DebugFacts)
	}
	ranged := readFirstExportSummary(t, filepath.Join(root, "cli_ranged_read", "session_wss_proof_export_001.jsonl"))
	if ranged.DebugFacts["wss.tool_command_classes"] != "read_like=1" ||
		ranged.DebugFacts["wss.dependency_trace"] != "true" ||
		ranged.DebugFacts["wss.read_partial_count"] != "1" ||
		ranged.DebugFacts["wss.read_range"] != "" ||
		ranged.DebugFacts["wss.read_range_hash"] == "" {
		t.Fatalf("ranged-read export missing trace facts: %+v", ranged.DebugFacts)
	}
	editRead := readFirstExportSummary(t, filepath.Join(root, "cli_apply_patch_edit_read", "session_wss_proof_export_001.jsonl"))
	if editRead.DebugFacts["wss.read_after_edit"] != "true" ||
		editRead.DebugFacts["wss.read_after_edit_count"] != "1" {
		t.Fatalf("edit-read export missing after-edit facts: %+v", editRead.DebugFacts)
	}
	assertExportDebugFactsDoNotLeak(t, repeat.DebugFacts, "cli-repeat-read")
	assertExportDebugFactsDoNotLeak(t, ranged.DebugFacts, "cli-ranged-read")
	assertExportDebugFactsDoNotLeak(t, editRead.DebugFacts, "cli-edit-read")
}

func TestWSSProofExportCorpusAddsContentFreePatchContextFacts(t *testing.T) {
	dir := t.TempDir()
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "cli-git-status-diff",
		Client:        "cli",
		WorkloadClass: "git_status_diff",
		FramesPath:    filepath.Join(dir, "frames-git-status-diff.jsonl"),
		LiveDelta: &codexCaptureLiveDelta{
			BillableInputTokensSaved: 900,
			HostBudgetStatus:         "ok",
			HostBudgetCompressionOK:  true,
			HostBudgetDegradationOK:  true,
		},
	})

	root := filepath.Join(dir, "corpus")
	report, err := exportWSSProofCorpus(matrixPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if report.RowsRead != 1 || report.RowsExported != 1 {
		t.Fatalf("bad export report: %+v", report)
	}
	rec := readFirstExportSummary(t, filepath.Join(root, "cli_git_status", "session_wss_proof_export_001.jsonl"))
	if rec.DebugFacts["wss.patch_context_candidate"] != "true" ||
		rec.DebugFacts["wss.patch_context_kind"] != "git_status_diff" ||
		rec.DebugFacts["wss.patch_context_hash"] == "" ||
		rec.DebugFacts["wss.patch_context_bytes"] != "3600" {
		t.Fatalf("git_status_diff export missing patch context facts: %+v", rec.DebugFacts)
	}
	assertExportDebugFactsDoNotLeak(t, rec.DebugFacts, "cli-git-status-diff")
}

func TestWSSProofExportCorpusRefreshesExistingRowsWithWireDenominators(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "corpus")
	categoryDir := filepath.Join(root, "cli_search_loop")
	if err := os.MkdirAll(categoryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := wssProofCorpusSummary{RequestSummary: dbg.RequestSummary{
		RequestID:    "same-search",
		Source:       "wss-proof-export",
		Provider:     "codex_chatgpt",
		ClientFamily: "codex_cli",
		RouteMode:    "wss_phasef",
		Tokens:       dbg.TokenCounts{Saved: 400},
	}}
	writeJSONLFile(t, filepath.Join(categoryDir, "session_wss_proof_export_001.jsonl"), existing)
	writeJSONLFile(t, filepath.Join(categoryDir, "session_wss_proof_export_002.jsonl"), existing)

	framesPath := filepath.Join(dir, "frames.jsonl")
	writeJSONLFile(t, framesPath, map[string]any{
		"direction": "server_to_client",
		"payload": map[string]any{
			"response": map[string]any{
				"usage": map[string]any{"input_tokens": 600},
			},
		},
	})
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "same-search",
		Client:        "cli",
		WorkloadClass: "search_loop",
		FramesPath:    framesPath,
		LiveDelta: &codexCaptureLiveDelta{
			BillableInputTokensSaved: 400,
			ProxyLayer0Captured:      1,
			HostBudgetStatus:         "ok",
			HostBudgetCompressionOK:  true,
			HostBudgetDegradationOK:  true,
		},
	})

	report, err := exportWSSProofCorpus(matrixPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if report.RowsExported != 1 || report.CategoriesWritten != 1 {
		t.Fatalf("bad export report: %+v", report)
	}
	rec := readFirstExportSummary(t, filepath.Join(categoryDir, "session_wss_proof_export_001.jsonl"))
	if rec.Tokens.Original != 1000 || rec.Tokens.Final != 600 || rec.ProviderInputTokens != 600 {
		t.Fatalf("existing row was not refreshed with denominator: %+v", rec)
	}
	second := readFirstExportSummary(t, filepath.Join(categoryDir, "session_wss_proof_export_002.jsonl"))
	if second.RequestID != "same-search" || second.Tokens.Original != 1000 || second.ProviderInputTokens != 600 {
		t.Fatalf("duplicate proof row was not preserved and refreshed: %+v", second)
	}
}

func TestWSSProofExportCorpusCountsReleaseSafeSearchCapExtraWithoutInflatingOriginal(t *testing.T) {
	dir := t.TempDir()
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	if err := os.WriteFile(matrixPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	framesPath := filepath.Join(dir, "frames-search.jsonl")
	writeJSONLFile(t, framesPath, map[string]any{
		"direction": "server_to_client",
		"payload": map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"usage": map[string]any{"input_tokens": 2100, "output_tokens": 33},
			},
		},
	})
	searchCapPath := filepath.Join(dir, "focused-search-cap.json")
	searchCapReport := wssProofMatrixReport{
		CaptureReports: []wssProofMatrixCapture{{
			ID:             "desktop-search-cap",
			Client:         "desktop",
			WorkloadClass:  "search_loop",
			FramesPath:     framesPath,
			Model:          "gpt-5.5",
			SearchCapProof: validCorpusSearchCapProof(120, 50),
			GatePassed:     true,
			LiveDelta: &codexCaptureLiveDelta{
				BillableInputTokensSaved: 900,
				ProxyLayer0Captured:      1,
				HostBudgetStatus:         "ok",
				HostBudgetCompressionOK:  true,
				HostBudgetDegradationOK:  true,
			},
		}},
	}
	data, err := json.Marshal(searchCapReport)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(searchCapPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(dir, "corpus")
	categoryDir := filepath.Join(root, "desktop_search_loop")
	if err := os.MkdirAll(categoryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSONLFile(t, filepath.Join(categoryDir, "session_wss_proof_export_001.jsonl"), wssProofCorpusSummary{RequestSummary: dbg.RequestSummary{
		RequestID:           "desktop-search-cap",
		Source:              "wss-proof-export",
		Provider:            "codex_chatgpt",
		ClientFamily:        "codex_desktop",
		RouteMode:           "wss_phasef",
		ProviderInputTokens: 2100,
		Tokens:              dbg.TokenCounts{Original: 3000, Final: 2100, Saved: 900},
	}})
	report, err := exportWSSProofCorpusWithOptions(matrixPath, root, wssProofCorpusExportOptions{searchCapProofReportPath: searchCapPath})
	if err != nil {
		t.Fatal(err)
	}
	if report.RowsRead != 1 || report.RowsExported != 1 || report.CategoriesWritten != 1 ||
		report.SearchCapProofReportPath != searchCapPath {
		t.Fatalf("bad export report: %+v", report)
	}
	rec := readFirstExportSummary(t, filepath.Join(root, "desktop_search_loop", "session_wss_proof_export_001.jsonl"))
	if rec.ProviderInputTokens != 2100 || rec.Tokens.Original != 3000 ||
		rec.Tokens.Final != 1980 || rec.Tokens.Saved != 1020 {
		t.Fatalf("search-cap extra accounting inflated denominator or missed final reduction: %+v", rec)
	}
	meta := readExportMetadata(t, filepath.Join(root, "desktop_search_loop", "metadata.json"))
	if meta.ExpectedSavedTokensMin != 1020 {
		t.Fatalf("search-cap extra not reflected in metadata: %+v", meta)
	}
}

func TestWSSProofCorpusSearchCapExtraReducerTokensFailsClosed(t *testing.T) {
	valid := wssProofMatrixRecord{
		Client:         "desktop",
		WorkloadClass:  "search_loop",
		FramesPath:     "frames.jsonl",
		SearchCapProof: validCorpusSearchCapProof(120, 50),
		GatePassed:     true,
		LiveDelta:      &codexCaptureLiveDelta{ProviderInputTokens: 2100},
	}
	if got := wssProofCorpusSearchCapExtraReducerTokens(valid); got != 120 {
		t.Fatalf("valid release-safe proof extra=%d want 120", got)
	}
	weakRetention := valid
	weakRetention.SearchCapProof = validCorpusSearchCapProof(120, 39.5)
	if got := wssProofCorpusSearchCapExtraReducerTokens(weakRetention); got != 0 {
		t.Fatalf("weak retention must not count extra, got %d", got)
	}
	failedGate := valid
	failedGate.GatePassed = false
	if got := wssProofCorpusSearchCapExtraReducerTokens(failedGate); got != 0 {
		t.Fatalf("failed row gate must not count extra, got %d", got)
	}
	noDenominator := valid
	noDenominator.LiveDelta = &codexCaptureLiveDelta{ProviderInputTokens: 120}
	if got := wssProofCorpusSearchCapExtraReducerTokens(noDenominator); got != 0 {
		t.Fatalf("extra without enough provider denominator must not count, got %d", got)
	}
}

func TestWSSProofExportCorpusRefreshesDoubleCountedProviderCacheRows(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "corpus")
	categoryDir := filepath.Join(root, "cli_provider_cache_long_session")
	if err := os.MkdirAll(categoryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := wssProofCorpusSummary{RequestSummary: dbg.RequestSummary{
		RequestID:            "same-provider-cache",
		Source:               "wss-proof-export",
		Provider:             "codex_chatgpt",
		ClientFamily:         "codex_cli",
		RouteMode:            "wss_phasef",
		CacheReadTokens:      3456,
		ProviderCachedTokens: 3456,
		Tokens:               dbg.TokenCounts{Saved: 3456},
	}}
	writeJSONLFile(t, filepath.Join(categoryDir, "session_wss_proof_export_001.jsonl"), existing)

	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "same-provider-cache",
		Client:        "cli",
		WorkloadClass: "provider_cache_long_session",
		FramesPath:    filepath.Join(dir, "frames-provider.jsonl"),
		LiveDelta: &codexCaptureLiveDelta{
			ProviderCacheReadTokens: 3456,
			HostBudgetStatus:        "ok",
			HostBudgetCompressionOK: true,
			HostBudgetDegradationOK: true,
		},
	})

	report, err := exportWSSProofCorpus(matrixPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if report.RowsExported != 1 || report.CategoriesWritten != 1 {
		t.Fatalf("bad export report: %+v", report)
	}
	rec := readFirstExportSummary(t, filepath.Join(categoryDir, "session_wss_proof_export_001.jsonl"))
	if rec.CacheReadTokens != 0 || rec.ProviderCachedTokens != 3456 || rec.Tokens.Saved != 3456 {
		t.Fatalf("double-counted provider cache row was not refreshed: %+v", rec)
	}
	meta := readExportMetadata(t, filepath.Join(categoryDir, "metadata.json"))
	if meta.ExpectedProviderCacheReadMin != 3456 || meta.ExpectedSavedTokensMin != 0 {
		t.Fatalf("bad provider-cache metadata after refresh: %+v", meta)
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

func TestWSSProofExportCorpusAppendsExistingCategory(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "corpus")
	categoryDir := filepath.Join(root, "cli_test_failure")
	if err := os.MkdirAll(categoryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := wssProofCorpusSummary{RequestSummary: dbg.RequestSummary{
		RequestID:    "existing-strong-proof",
		Source:       "wss-proof-export",
		Provider:     "codex_chatgpt",
		ClientFamily: "codex_cli",
		RouteMode:    "wss_phasef",
		Tokens:       dbg.TokenCounts{Saved: 11147},
	}}
	writeJSONLFile(t, filepath.Join(categoryDir, "session_wss_proof_export_001.jsonl"), existing)

	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "new-cargo-proof",
		Client:        "cli",
		WorkloadClass: "build_test_lint_failure",
		FramesPath:    filepath.Join(dir, "frames-cargo.jsonl"),
		LiveDelta: &codexCaptureLiveDelta{
			BillableInputTokensSaved: 934,
			ProxyLayer0Envelope:      1,
			HostBudgetStatus:         "ok",
			HostBudgetCompressionOK:  true,
			HostBudgetDegradationOK:  true,
		},
	})

	report, err := exportWSSProofCorpus(matrixPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if report.RowsExported != 1 || report.CategoriesWritten != 1 {
		t.Fatalf("bad export report: %+v", report)
	}
	meta := readExportMetadata(t, filepath.Join(categoryDir, "metadata.json"))
	if meta.ExpectedRequestCount != 2 || meta.ExpectedSavedTokensMin != 12081 {
		t.Fatalf("existing category was not appended safely: %+v", meta)
	}
	first := readFirstExportSummary(t, filepath.Join(categoryDir, "session_wss_proof_export_001.jsonl"))
	second := readFirstExportSummary(t, filepath.Join(categoryDir, "session_wss_proof_export_002.jsonl"))
	if first.RequestID != "existing-strong-proof" || first.Tokens.Saved != 11147 ||
		second.RequestID != "new-cargo-proof" || second.Tokens.Saved != 934 {
		t.Fatalf("unexpected merged records: first=%+v second=%+v", first, second)
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

func assertExportDebugFactsDoNotLeak(t *testing.T, facts map[string]string, raw string) {
	t.Helper()
	for key, value := range facts {
		if strings.Contains(value, raw) {
			t.Fatalf("debug fact %s leaked raw string %q in %q", key, raw, value)
		}
	}
}

func validCorpusSearchCapProof(extra int, retention float64) *searchCapProofReport {
	selected := &searchCapProofSelection{
		Name:                    "candidate_25x15",
		MaxFilesShown:           25,
		MaxMatchesPerFile:       15,
		ExtraReducerTokens:      extra,
		SavedBytesVsDefault:     extra * 8,
		MatchRetentionPct:       retention,
		OmittedMatchesVsDefault: 10,
	}
	replay := &searchCapProofReplaySummary{
		ReducerTokensSaved:    1000 + extra,
		BytesSaved:            extra * 8,
		SearchRequestTurns:    2,
		SearchMutatedRequests: 2,
		SearchCapProofLatch:   true,
		GatePassed:            true,
	}
	return &searchCapProofReport{
		Path:                    "focused-search-cap.json",
		SearchOutputs:           releaseSearchCapMinSearchOutputs,
		MinCandidateRetainedPct: releaseSearchCapMinRetainedPct,
		MinSearchOutputs:        releaseSearchCapMinSearchOutputs,
		MinExtraReducerTokens:   releaseSearchCapMinExtraReducerTokens,
		DefaultReplay: searchCapProofReplaySummary{
			ReducerTokensSaved:    1000,
			SearchRequestTurns:    2,
			SearchMutatedRequests: 2,
			SearchCapProofLatch:   true,
			GatePassed:            true,
		},
		SelectedCandidate: selected,
		Candidates: []searchCapProofCandidateRow{{
			Name:                    selected.Name,
			MaxFilesShown:           selected.MaxFilesShown,
			MaxMatchesPerFile:       selected.MaxMatchesPerFile,
			Applied:                 true,
			SavedBytesVsDefault:     selected.SavedBytesVsDefault,
			MatchRetentionPct:       selected.MatchRetentionPct,
			OmittedMatchesVsDefault: selected.OmittedMatchesVsDefault,
			ExtraReducerTokens:      selected.ExtraReducerTokens,
			Replay:                  replay,
			GatePassed:              true,
		}},
		GatePassed: true,
	}
}

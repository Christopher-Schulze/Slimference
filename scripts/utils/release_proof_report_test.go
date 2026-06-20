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
	if report.ProofSchemaVersion != releaseProofReportSchemaVersion {
		t.Fatalf("proof schema version = %d, want %d", report.ProofSchemaVersion, releaseProofReportSchemaVersion)
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

func TestReleaseProofReportAcceptsFocusedSearchCapProofArtifact(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofControlFrames(t, framesPath, "release-search-cap")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeCompleteReleaseProofRows(t, matrixPath, framesPath)
	searchCapProofPath := filepath.Join(dir, "search-cap-proof.json")
	writeReleaseSearchCapProofReport(t, searchCapProofPath, true, "candidate_25x15", "candidate_25x15")
	codexBefore, codexAfter := writeReleaseCodexStatusProofPair(t, dir)

	report, err := loadReleaseProofReport(releaseProofReportFlags{
		matrixPath:               matrixPath,
		resourceProfileProofs:    []string{writeReleaseResourceProofBundle(t, dir, "cli"), writeReleaseResourceProofBundle(t, dir, "desktop")},
		searchCapProofReportPath: searchCapProofPath,
		codexStatusBeforePath:    codexBefore,
		codexStatusAfterPath:     codexAfter,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed || report.SearchCapProof == nil || !report.SearchCapProof.OK {
		t.Fatalf("valid search-cap proof artifact should pass: %+v", report)
	}
	if report.CodexRouteHygiene == nil || !report.CodexRouteHygiene.OK {
		t.Fatalf("valid search-cap promotion should include clean route hygiene proof: %+v", report.CodexRouteHygiene)
	}
	if report.SearchCapProof.MaxFilesShown != 25 ||
		report.SearchCapProof.MaxMatchesPerFile != 15 ||
		report.SearchCapProof.TotalExtraReducerTokens != 14 ||
		report.SearchCapProof.MinMatchRetentionPct != 40.25 ||
		!report.SearchCapProof.DownstreamStateProof ||
		report.SearchCapProof.DownstreamCandidates != 2 ||
		report.SearchCapProof.DownstreamPassing != 2 ||
		report.SearchCapProof.DownstreamNetSavedTokens != 14 ||
		report.SearchCapProof.RequiredReducerHits["captured_output"] != 2 {
		t.Fatalf("unexpected search-cap summary: %+v", report.SearchCapProof)
	}

	var text bytes.Buffer
	writeReleaseProofReportText(&text, report)
	if !strings.Contains(text.String(), "Search-cap proof: ok=true") ||
		!strings.Contains(text.String(), "selected=candidate_25x15 25/15") ||
		!strings.Contains(text.String(), "downstream=true downstream_candidates=2 passing=2 downstream_net=14") ||
		!strings.Contains(text.String(), "Codex route hygiene: ok=true") {
		t.Fatalf("text report missing search-cap summary:\n%s", text.String())
	}
}

func TestReleaseProofReportAcceptsDefaultRetentionFloorSearchCapProof(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofControlFrames(t, framesPath, "release-search-cap-default-floor")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeCompleteReleaseProofRows(t, matrixPath, framesPath)
	searchCapProofPath := filepath.Join(dir, "search-cap-proof-default-floor.json")
	writeReleaseSearchCapProofReportWithExtras(t, searchCapProofPath, true, "default_retention_floor", "default_retention_floor", 6, 8)
	codexBefore, codexAfter := writeReleaseCodexStatusProofPair(t, dir)

	report, err := loadReleaseProofReport(releaseProofReportFlags{
		matrixPath:               matrixPath,
		resourceProfileProofs:    []string{writeReleaseResourceProofBundle(t, dir, "cli"), writeReleaseResourceProofBundle(t, dir, "desktop")},
		searchCapProofReportPath: searchCapProofPath,
		codexStatusBeforePath:    codexBefore,
		codexStatusAfterPath:     codexAfter,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed || report.SearchCapProof == nil || !report.SearchCapProof.OK {
		t.Fatalf("default retention-floor search-cap proof should pass: %+v", report)
	}
	if report.SearchCapProof.SelectedCandidate != "default_retention_floor" ||
		report.SearchCapProof.MaxFilesShown != 30 ||
		report.SearchCapProof.MaxMatchesPerFile != 20 ||
		report.SearchCapProof.TotalExtraReducerTokens != 14 {
		t.Fatalf("unexpected default floor summary: %+v", report.SearchCapProof)
	}
}

func TestReleaseProofReportRejectsBadSearchCapProofArtifact(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofControlFrames(t, framesPath, "release-search-cap-bad")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeCompleteReleaseProofRows(t, matrixPath, framesPath)
	codexBefore, codexAfter := writeReleaseCodexStatusProofPair(t, dir)
	searchCapProofPath := filepath.Join(dir, "search-cap-proof-bad.json")
	writeReleaseSearchCapProofReport(t, searchCapProofPath, true, "candidate_25x15", "candidate_30x15")

	report, err := loadReleaseProofReport(releaseProofReportFlags{
		matrixPath:               matrixPath,
		resourceProfileProofs:    []string{writeReleaseResourceProofBundle(t, dir, "cli"), writeReleaseResourceProofBundle(t, dir, "desktop")},
		searchCapProofReportPath: searchCapProofPath,
		codexStatusBeforePath:    codexBefore,
		codexStatusAfterPath:     codexAfter,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(report.GateFailures, "\n")
	if report.GatePassed || report.SearchCapProof == nil || report.SearchCapProof.OK ||
		!strings.Contains(joined, "selected cap 30/15 differs from 25/15") {
		t.Fatalf("inconsistent search-cap proof must fail: passed=%v proof=%+v failures=%v", report.GatePassed, report.SearchCapProof, report.GateFailures)
	}

	zeroExtraPath := filepath.Join(dir, "search-cap-proof-zero-extra.json")
	writeReleaseSearchCapProofReportWithExtras(t, zeroExtraPath, true, "candidate_25x15", "candidate_25x15", 0, 0)
	report, err = loadReleaseProofReport(releaseProofReportFlags{
		matrixPath:               matrixPath,
		resourceProfileProofs:    []string{writeReleaseResourceProofBundle(t, dir, "cli"), writeReleaseResourceProofBundle(t, dir, "desktop")},
		searchCapProofReportPath: zeroExtraPath,
		codexStatusBeforePath:    codexBefore,
		codexStatusAfterPath:     codexAfter,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(report.GateFailures, "\n")
	if report.GatePassed || report.SearchCapProof == nil || report.SearchCapProof.OK ||
		!strings.Contains(joined, "total search-cap extra reducer tokens must be positive") ||
		!strings.Contains(joined, "selected search-cap candidate has non-positive extra reducer tokens") {
		t.Fatalf("non-positive search-cap proof must fail: passed=%v proof=%+v failures=%v", report.GatePassed, report.SearchCapProof, report.GateFailures)
	}

	weakThresholdPath := filepath.Join(dir, "search-cap-proof-weak-threshold.json")
	writeReleaseSearchCapProofReportWithConfig(t, weakThresholdPath, true, "candidate_25x15", "candidate_25x15", 6, 8, 39.5, 1, 0, 39.5, 39.75)
	report, err = loadReleaseProofReport(releaseProofReportFlags{
		matrixPath:               matrixPath,
		resourceProfileProofs:    []string{writeReleaseResourceProofBundle(t, dir, "cli"), writeReleaseResourceProofBundle(t, dir, "desktop")},
		searchCapProofReportPath: weakThresholdPath,
		codexStatusBeforePath:    codexBefore,
		codexStatusAfterPath:     codexAfter,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(report.GateFailures, "\n")
	if report.GatePassed || report.SearchCapProof == nil || report.SearchCapProof.OK ||
		!strings.Contains(joined, "search_cap_proof min retention 39.50% < release min 40.00%") ||
		!strings.Contains(joined, "search_cap_proof min search outputs 1 < release min 2") ||
		!strings.Contains(joined, "search_cap_proof min extra reducer tokens 0 < release min 1") ||
		!strings.Contains(joined, "selected search-cap candidate retention 39.50% < release min 40.00%") {
		t.Fatalf("weak search-cap threshold proof must fail: passed=%v proof=%+v failures=%v", report.GatePassed, report.SearchCapProof, report.GateFailures)
	}

	aggregateOnlyPath := filepath.Join(dir, "search-cap-proof-aggregate-only.json")
	writeReleaseSearchCapAggregateOnlyProofReport(t, aggregateOnlyPath)
	report, err = loadReleaseProofReport(releaseProofReportFlags{
		matrixPath:               matrixPath,
		resourceProfileProofs:    []string{writeReleaseResourceProofBundle(t, dir, "cli"), writeReleaseResourceProofBundle(t, dir, "desktop")},
		searchCapProofReportPath: aggregateOnlyPath,
		codexStatusBeforePath:    codexBefore,
		codexStatusAfterPath:     codexAfter,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(report.GateFailures, "\n")
	if report.GatePassed || report.SearchCapProof == nil || report.SearchCapProof.OK ||
		!strings.Contains(joined, "validated search_loop capture_reports 1 != captures 2") ||
		!strings.Contains(joined, "missing validated Desktop search-cap capture report") ||
		!strings.Contains(joined, "expected at least 2 validated positive search-cap capture reports, got 1") {
		t.Fatalf("aggregate-only search-cap proof must fail: passed=%v proof=%+v failures=%v", report.GatePassed, report.SearchCapProof, report.GateFailures)
	}

	failedRowPath := filepath.Join(dir, "search-cap-proof-failed-row.json")
	writeReleaseSearchCapFailedRowProofReport(t, failedRowPath)
	report, err = loadReleaseProofReport(releaseProofReportFlags{
		matrixPath:               matrixPath,
		resourceProfileProofs:    []string{writeReleaseResourceProofBundle(t, dir, "cli"), writeReleaseResourceProofBundle(t, dir, "desktop")},
		searchCapProofReportPath: failedRowPath,
		codexStatusBeforePath:    codexBefore,
		codexStatusAfterPath:     codexAfter,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(report.GateFailures, "\n")
	if report.GatePassed || report.SearchCapProof == nil || report.SearchCapProof.OK ||
		!strings.Contains(joined, "desktop-search-cap: focused search-cap capture row gate failed: row replay failed") ||
		!strings.Contains(joined, "missing validated Desktop search-cap capture report") {
		t.Fatalf("failed search-cap row must fail: passed=%v proof=%+v failures=%v", report.GatePassed, report.SearchCapProof, report.GateFailures)
	}

	contradictoryPath := filepath.Join(dir, "search-cap-proof-contradictory.json")
	writeReleaseSearchCapContradictoryProofReport(t, contradictoryPath)
	report, err = loadReleaseProofReport(releaseProofReportFlags{
		matrixPath:               matrixPath,
		resourceProfileProofs:    []string{writeReleaseResourceProofBundle(t, dir, "cli"), writeReleaseResourceProofBundle(t, dir, "desktop")},
		searchCapProofReportPath: contradictoryPath,
		codexStatusBeforePath:    codexBefore,
		codexStatusAfterPath:     codexAfter,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(report.GateFailures, "\n")
	for _, want := range []string{
		"focused wss-proof-matrix gate passed but still contains gate failures",
		"focused search-cap capture row gate passed but still contains gate failures",
		"search_cap_proof gate passed but still contains gate failures",
	} {
		if report.GatePassed || report.SearchCapProof == nil || report.SearchCapProof.OK || !strings.Contains(joined, want) {
			t.Fatalf("contradictory search-cap proof missing %q: passed=%v proof=%+v failures=%v", want, report.GatePassed, report.SearchCapProof, report.GateFailures)
		}
	}

	envelopeOnlyPath := filepath.Join(dir, "search-cap-proof-envelope-only.json")
	writeReleaseSearchCapProofReportWithReducerHits(t, envelopeOnlyPath, map[string]int64{"codex_exec_envelope": 2})
	report, err = loadReleaseProofReport(releaseProofReportFlags{
		matrixPath:               matrixPath,
		resourceProfileProofs:    []string{writeReleaseResourceProofBundle(t, dir, "cli"), writeReleaseResourceProofBundle(t, dir, "desktop")},
		searchCapProofReportPath: envelopeOnlyPath,
		codexStatusBeforePath:    codexBefore,
		codexStatusAfterPath:     codexAfter,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(report.GateFailures, "\n")
	if report.GatePassed || report.SearchCapProof == nil || report.SearchCapProof.OK ||
		!strings.Contains(joined, "focused search-cap proof missing required captured_output reducer hit") {
		t.Fatalf("envelope-only search-cap proof must fail: passed=%v proof=%+v failures=%v", report.GatePassed, report.SearchCapProof, report.GateFailures)
	}

	missingDownstreamPath := filepath.Join(dir, "search-cap-proof-missing-downstream.json")
	writeReleaseSearchCapMissingDownstreamProofReport(t, missingDownstreamPath)
	report, err = loadReleaseProofReport(releaseProofReportFlags{
		matrixPath:               matrixPath,
		resourceProfileProofs:    []string{writeReleaseResourceProofBundle(t, dir, "cli"), writeReleaseResourceProofBundle(t, dir, "desktop")},
		searchCapProofReportPath: missingDownstreamPath,
		codexStatusBeforePath:    codexBefore,
		codexStatusAfterPath:     codexAfter,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(report.GateFailures, "\n")
	if report.GatePassed || report.SearchCapProof == nil || report.SearchCapProof.OK ||
		!strings.Contains(joined, "search_cap_proof downstream_state_proof failed") ||
		!strings.Contains(joined, "expected live downstream-state proof for every positive search-cap capture") {
		t.Fatalf("missing downstream search-cap proof must fail: passed=%v proof=%+v failures=%v", report.GatePassed, report.SearchCapProof, report.GateFailures)
	}

	negativeDownstreamPath := filepath.Join(dir, "search-cap-proof-negative-downstream.json")
	writeReleaseSearchCapNegativeDownstreamProofReport(t, negativeDownstreamPath)
	report, err = loadReleaseProofReport(releaseProofReportFlags{
		matrixPath:               matrixPath,
		resourceProfileProofs:    []string{writeReleaseResourceProofBundle(t, dir, "cli"), writeReleaseResourceProofBundle(t, dir, "desktop")},
		searchCapProofReportPath: negativeDownstreamPath,
		codexStatusBeforePath:    codexBefore,
		codexStatusAfterPath:     codexAfter,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(report.GateFailures, "\n")
	if report.GatePassed || report.SearchCapProof == nil || report.SearchCapProof.OK ||
		!strings.Contains(joined, "search_cap_proof downstream_state_proof net saved tokens must be positive") ||
		!strings.Contains(joined, "expected live downstream-state proof for every positive search-cap capture") {
		t.Fatalf("negative downstream net must fail product search-cap promotion: passed=%v proof=%+v failures=%v", report.GatePassed, report.SearchCapProof, report.GateFailures)
	}

	badJSONPath := filepath.Join(dir, "not-json.json")
	writeTextFile(t, badJSONPath, "{not-json")
	if _, err := loadReleaseProofReport(releaseProofReportFlags{
		matrixPath:               matrixPath,
		resourceProfileProofs:    []string{writeReleaseResourceProofBundle(t, dir, "cli"), writeReleaseResourceProofBundle(t, dir, "desktop")},
		searchCapProofReportPath: badJSONPath,
		codexStatusBeforePath:    codexBefore,
		codexStatusAfterPath:     codexAfter,
	}); err == nil {
		t.Fatal("invalid search-cap proof JSON should return an error")
	}
}

func TestReleaseProofReportRequiresCleanCodexRouteHygieneForSearchCapPromotion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofControlFrames(t, framesPath, "release-search-cap-route-hygiene")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeCompleteReleaseProofRows(t, matrixPath, framesPath)
	searchCapProofPath := filepath.Join(dir, "search-cap-proof.json")
	writeReleaseSearchCapProofReport(t, searchCapProofPath, true, "candidate_25x15", "candidate_25x15")
	resourceProofs := []string{
		writeReleaseResourceProofBundle(t, dir, "cli"),
		writeReleaseResourceProofBundle(t, dir, "desktop"),
	}

	report, err := loadReleaseProofReport(releaseProofReportFlags{
		matrixPath:               matrixPath,
		resourceProfileProofs:    resourceProofs,
		searchCapProofReportPath: searchCapProofPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.GatePassed || !strings.Contains(strings.Join(report.GateFailures, "\n"), "missing Codex route hygiene proof for search-cap promotion") {
		t.Fatalf("search-cap promotion without route hygiene proof must fail: %+v", report)
	}

	cleanBefore := writeReleaseCodexStatusProof(t, dir, "codex-before.json", false, false, false, "")
	badAfter := writeReleaseCodexStatusProof(t, dir, "codex-after.json", true, true, true, "top-level model_provider already set")
	report, err = loadReleaseProofReport(releaseProofReportFlags{
		matrixPath:               matrixPath,
		resourceProfileProofs:    resourceProofs,
		searchCapProofReportPath: searchCapProofPath,
		codexStatusBeforePath:    cleanBefore,
		codexStatusAfterPath:     badAfter,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(report.GateFailures, "\n")
	for _, want := range []string{
		"invalid Codex route hygiene proof",
		"after: advanced shared Codex route enabled",
		"after: marker-owned Codex route complete",
		"after: legacy openai_base_url/chatgpt_base_url keys present",
		"after: Codex route conflict: top-level model_provider already set",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing route hygiene failure %q in:\n%s", want, joined)
		}
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
		wssProofMatrixRecord{
			ID:            "proof-drop-row",
			Client:        "cli",
			WorkloadClass: "repeat_read",
			FramesPath:    framesPath,
			LiveDelta: &codexCaptureLiveDelta{
				BillableInputTokensSaved:    1,
				AnalyticsProofEventsDropped: 1,
				HostBudgetStatus:            "ok",
				HostBudgetCompressionOK:     true,
				HostBudgetDegradationOK:     true,
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
		!strings.Contains(joined, "zero-violation-row") ||
		!strings.Contains(joined, "proof-drop-row") {
		t.Fatalf("anomaly rows must fail release proof gate: passed=%v failures=%v", report.GatePassed, report.GateFailures)
	}
}

func TestReleaseResourceProofBundleRejectsProofEventDrops(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	resourceBundle := writeReleaseResourceProofBundle(t, dir, "cli")
	writeTextFile(t, filepath.Join(resourceBundle, "admin-after.json"), `{
  "host_budget": {
    "status": "ok",
    "rss_bytes": 10,
    "cpu_window_seconds": 1,
    "compression_ok": true,
    "degradation_ok": true
  },
  "wss": {
    "analytics_proof_events_dropped": 1
  }
}`)

	result := validateReleaseResourceProof(resourceBundle)
	if result.OK || !strings.Contains(strings.Join(result.Issues, "\n"), "analytics proof events dropped=1") {
		t.Fatalf("proof event drops must fail resource proof validation: %+v", result)
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
	stdout.Reset()
	stderr.Reset()
	if code := runReleaseProofReport([]string{"matrix.jsonl", "--search-cap-proof-report"}, &stdout, &stderr); code != 2 {
		t.Fatalf("missing search-cap proof report value should be usage error, code=%d stderr=%s", code, stderr.String())
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

func writeReleaseCodexStatusProofPair(t *testing.T, dir string) (string, string) {
	t.Helper()
	return writeReleaseCodexStatusProof(t, dir, "codex-status-before.json", false, false, false, ""),
		writeReleaseCodexStatusProof(t, dir, "codex-status-after.json", false, false, false, "")
}

func writeReleaseCodexStatusProof(t *testing.T, dir, name string, enabled, complete, legacy bool, conflict string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	body := map[string]any{
		"route": map[string]any{
			"enabled":     enabled,
			"complete":    complete,
			"legacy_keys": legacy,
			"conflict":    conflict,
			"base_url":    "http://127.0.0.1:8990",
			"transport":   "",
		},
		"daemon": map[string]any{
			"reachable": false,
		},
	}
	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeTextFile(t, path, string(data)+"\n")
	return path
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

func writeReleaseSearchCapProofReport(t *testing.T, path string, gatePassed bool, cliCandidate, desktopCandidate string) {
	t.Helper()
	writeReleaseSearchCapProofReportWithExtras(t, path, gatePassed, cliCandidate, desktopCandidate, 6, 8)
}

func writeReleaseSearchCapProofReportWithExtras(t *testing.T, path string, gatePassed bool, cliCandidate, desktopCandidate string, cliExtra, desktopExtra int) {
	t.Helper()
	writeReleaseSearchCapProofReportWithConfig(t, path, gatePassed, cliCandidate, desktopCandidate, cliExtra, desktopExtra, releaseSearchCapMinRetainedPct, releaseSearchCapMinSearchOutputs, releaseSearchCapMinExtraReducerTokens, 40.25, 41.5)
}

func writeReleaseSearchCapProofReportWithConfig(t *testing.T, path string, gatePassed bool, cliCandidate, desktopCandidate string, cliExtra, desktopExtra int, minRetention float64, minOutputs, minExtra int, cliRetention, desktopRetention float64) {
	t.Helper()
	writeReleaseSearchCapProofReportWithConfigAndReducerHits(t, path, gatePassed, cliCandidate, desktopCandidate, cliExtra, desktopExtra, minRetention, minOutputs, minExtra, cliRetention, desktopRetention, map[string]int64{"captured_output": 2})
}

func writeReleaseSearchCapProofReportWithReducerHits(t *testing.T, path string, requiredReducerHits map[string]int64) {
	t.Helper()
	writeReleaseSearchCapProofReportWithConfigAndReducerHits(t, path, true, "candidate_25x15", "candidate_25x15", 6, 8, releaseSearchCapMinRetainedPct, releaseSearchCapMinSearchOutputs, releaseSearchCapMinExtraReducerTokens, 40.25, 41.5, requiredReducerHits)
}

func writeReleaseSearchCapProofReportWithConfigAndReducerHits(t *testing.T, path string, gatePassed bool, cliCandidate, desktopCandidate string, cliExtra, desktopExtra int, minRetention float64, minOutputs, minExtra int, cliRetention, desktopRetention float64, requiredReducerHits map[string]int64) {
	t.Helper()
	report := wssProofMatrixReport{
		Path:                "focused-search-cap.jsonl",
		Captures:            2,
		CLI:                 1,
		Desktop:             1,
		PositiveSavings:     2,
		WorkloadClasses:     map[string]int{"search_loop": 2},
		RequiredReducerHits: requiredReducerHits,
		GatePassed:          gatePassed,
		CaptureReports: []wssProofMatrixCapture{
			releaseSearchCapCapture("cli-search-cap", "cli", cliCandidate, cliExtra, minRetention, minOutputs, minExtra, cliRetention),
			releaseSearchCapCapture("desktop-search-cap", "desktop", desktopCandidate, desktopExtra, minRetention, minOutputs, minExtra, desktopRetention),
		},
	}
	if !gatePassed {
		report.GateFailures = []string{"focused proof failed"}
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeReleaseSearchCapAggregateOnlyProofReport(t *testing.T, path string) {
	t.Helper()
	report := wssProofMatrixReport{
		Path:                "focused-search-cap.jsonl",
		Captures:            2,
		CLI:                 1,
		Desktop:             1,
		PositiveSavings:     2,
		WorkloadClasses:     map[string]int{"search_loop": 2},
		RequiredReducerHits: map[string]int64{"captured_output": 2},
		GatePassed:          true,
		CaptureReports: []wssProofMatrixCapture{
			releaseSearchCapCapture("cli-search-cap", "cli", "candidate_25x15", 7, releaseSearchCapMinRetainedPct, releaseSearchCapMinSearchOutputs, releaseSearchCapMinExtraReducerTokens, 41.25),
		},
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeReleaseSearchCapFailedRowProofReport(t *testing.T, path string) {
	t.Helper()
	failed := releaseSearchCapCapture("desktop-search-cap", "desktop", "candidate_25x15", 8, releaseSearchCapMinRetainedPct, releaseSearchCapMinSearchOutputs, releaseSearchCapMinExtraReducerTokens, 41.5)
	failed.GatePassed = false
	failed.GateFailures = []string{"row replay failed"}
	report := wssProofMatrixReport{
		Path:            "focused-search-cap.jsonl",
		Captures:        2,
		CLI:             1,
		Desktop:         1,
		PositiveSavings: 2,
		WorkloadClasses: map[string]int{"search_loop": 2},
		RequiredReducerHits: map[string]int64{
			"captured_output": 2,
		},
		GatePassed: true,
		CaptureReports: []wssProofMatrixCapture{
			releaseSearchCapCapture("cli-search-cap", "cli", "candidate_25x15", 7, releaseSearchCapMinRetainedPct, releaseSearchCapMinSearchOutputs, releaseSearchCapMinExtraReducerTokens, 41.25),
			failed,
		},
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeReleaseSearchCapContradictoryProofReport(t *testing.T, path string) {
	t.Helper()
	cli := releaseSearchCapCapture("cli-search-cap", "cli", "candidate_25x15", 7, releaseSearchCapMinRetainedPct, releaseSearchCapMinSearchOutputs, releaseSearchCapMinExtraReducerTokens, 41.25)
	desktop := releaseSearchCapCapture("desktop-search-cap", "desktop", "candidate_25x15", 8, releaseSearchCapMinRetainedPct, releaseSearchCapMinSearchOutputs, releaseSearchCapMinExtraReducerTokens, 41.5)
	desktop.GateFailures = []string{"hidden row issue"}
	desktop.SearchCapProof.GateFailures = []string{"hidden nested issue"}
	desktop.SearchCapProof.DownstreamStateProof.GateFailures = []string{"hidden downstream issue"}
	report := wssProofMatrixReport{
		Path:            "focused-search-cap.jsonl",
		Captures:        2,
		CLI:             1,
		Desktop:         1,
		PositiveSavings: 2,
		WorkloadClasses: map[string]int{"search_loop": 2},
		RequiredReducerHits: map[string]int64{
			"captured_output": 2,
		},
		GatePassed:     true,
		GateFailures:   []string{"hidden matrix issue"},
		CaptureReports: []wssProofMatrixCapture{cli, desktop},
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeReleaseSearchCapMissingDownstreamProofReport(t *testing.T, path string) {
	t.Helper()
	cli := releaseSearchCapCapture("cli-search-cap", "cli", "candidate_25x15", 7, releaseSearchCapMinRetainedPct, releaseSearchCapMinSearchOutputs, releaseSearchCapMinExtraReducerTokens, 41.25)
	desktop := releaseSearchCapCapture("desktop-search-cap", "desktop", "candidate_25x15", 8, releaseSearchCapMinRetainedPct, releaseSearchCapMinSearchOutputs, releaseSearchCapMinExtraReducerTokens, 41.5)
	desktop.SearchCapProof.DownstreamStateProof = searchCapDownstreamStateProof{
		GateFailures: []string{"no live mutated search-output downstream candidate observed"},
	}
	report := wssProofMatrixReport{
		Path:            "focused-search-cap.jsonl",
		Captures:        2,
		CLI:             1,
		Desktop:         1,
		PositiveSavings: 2,
		WorkloadClasses: map[string]int{"search_loop": 2},
		RequiredReducerHits: map[string]int64{
			"captured_output": 2,
		},
		GatePassed:     true,
		CaptureReports: []wssProofMatrixCapture{cli, desktop},
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeReleaseSearchCapNegativeDownstreamProofReport(t *testing.T, path string) {
	t.Helper()
	cli := releaseSearchCapCapture("cli-search-cap", "cli", "candidate_25x15", 7, releaseSearchCapMinRetainedPct, releaseSearchCapMinSearchOutputs, releaseSearchCapMinExtraReducerTokens, 41.25)
	desktop := releaseSearchCapCapture("desktop-search-cap", "desktop", "candidate_25x15", 8, releaseSearchCapMinRetainedPct, releaseSearchCapMinSearchOutputs, releaseSearchCapMinExtraReducerTokens, 41.5)
	desktop.SearchCapProof.DownstreamStateProof.NetCapturedLocalSavedTokens = -1
	report := wssProofMatrixReport{
		Path:            "focused-search-cap.jsonl",
		Captures:        2,
		CLI:             1,
		Desktop:         1,
		PositiveSavings: 2,
		WorkloadClasses: map[string]int{"search_loop": 2},
		RequiredReducerHits: map[string]int64{
			"captured_output": 2,
		},
		GatePassed:     true,
		CaptureReports: []wssProofMatrixCapture{cli, desktop},
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func releaseSearchCapCapture(id, client, candidate string, extraTokens int, minRetention float64, minOutputs, minExtra int, retention float64) wssProofMatrixCapture {
	files := 25
	matches := 15
	if candidate == "default_retention_floor" {
		files = 30
		matches = 20
	}
	if strings.Contains(candidate, "30x15") {
		files = 30
	}
	return wssProofMatrixCapture{
		ID:            id,
		Client:        client,
		WorkloadClass: "search_loop",
		SearchCapProof: &searchCapProofReport{
			SearchOutputs:           minOutputs,
			MinCandidateRetainedPct: minRetention,
			MinSearchOutputs:        minOutputs,
			MinExtraReducerTokens:   minExtra,
			GatePassed:              true,
			DownstreamStateProof: searchCapDownstreamStateProof{
				MutatedSearchOutputCandidates: 1,
				MutatedDeltaCandidates:        1,
				CandidatesWithCleanCurrent:    1,
				CandidatesWithFollowingTurn:   1,
				CandidatesWithCleanFollowing:  1,
				CandidatesPassing:             1,
				NetCapturedLocalSavedTokens:   extraTokens,
				GatePassed:                    true,
			},
			DefaultReplay: searchCapProofReplaySummary{
				SearchRequestTurns:    2,
				SearchMutatedRequests: 2,
				SearchCapProofLatch:   true,
			},
			SelectedCandidate: &searchCapProofSelection{
				Name:               candidate,
				MaxFilesShown:      files,
				MaxMatchesPerFile:  matches,
				ExtraReducerTokens: extraTokens,
				MatchRetentionPct:  retention,
			},
			Candidates: []searchCapProofCandidateRow{
				{
					Name:               candidate,
					MaxFilesShown:      files,
					MaxMatchesPerFile:  matches,
					ExtraReducerTokens: extraTokens,
					Replay: &searchCapProofReplaySummary{
						SearchRequestTurns:    2,
						SearchMutatedRequests: 2,
						SearchCapProofLatch:   true,
					},
					GatePassed: true,
				},
			},
		},
		GatePassed: true,
	}
}

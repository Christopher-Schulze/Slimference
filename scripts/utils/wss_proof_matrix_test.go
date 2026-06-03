package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	dbg "github.com/slimference/slimference/internal/debug"
)

func TestWSSProofMatrixPassesRepresentativeSet(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	decisionsPath := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, decisionsPath, dbg.RequestSummary{
		RequestID: "wss-1",
		SessionID: "codex-wss:proof",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Saved: 12},
		Plan:      &dbg.PlanSummary{ContentClasses: []string{"tool_output"}},
	})

	classes := []string{
		"repeat_full_read",
		"similar_files",
		"changed_file",
		"ranged_read",
		"search_loop",
		"git_status_diff",
		"build_test_lint_failure",
		"apply_patch_then_read",
		"long_mixed_workday",
		"no_savings_control",
	}
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	var records []interface{}
	for i, class := range classes {
		framesPath := filepath.Join(dir, fmt.Sprintf("frames-%02d.jsonl", i))
		expectedZero := class == "no_savings_control"
		if expectedZero {
			writeProofControlFrames(t, framesPath, fmt.Sprintf("session-%02d", i))
		} else {
			writeProofRepeatReadFrames(t, framesPath, fmt.Sprintf("session-%02d", i))
		}
		client := "cli"
		if i >= 5 {
			client = "desktop"
		}
		expectedReducers := []string{"read_delta"}
		if expectedZero {
			expectedReducers = []string{"none"}
		}
		records = append(records, wssProofMatrixRecord{
			ID:                  fmt.Sprintf("%s-%02d", client, i),
			Client:              client,
			WorkloadClass:       class,
			FramesPath:          framesPath,
			DecisionsPath:       decisionsPath,
			CodexVersion:        "codex-cli 0.test",
			SlimferenceCommit:   "test",
			Repo:                "Slimference",
			Model:               "gpt-5-codex",
			ExpectedReducers:    expectedReducers,
			ExpectedZeroSavings: expectedZero,
			LiveDelta:           proofMatrixLiveDelta(expectedZero),
		})
	}
	writeJSONLFile(t, matrixPath, records...)

	report, err := loadWSSProofMatrixReport(matrixPath)
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed || report.Captures != 10 || report.CLI != 5 || report.Desktop != 5 {
		t.Fatalf("proof matrix should pass: %+v", report)
	}
	if report.PositiveSavings < 9 || report.ExpectedZero != 1 || len(report.MissingWorkloads) != 0 {
		t.Fatalf("bad proof aggregate: %+v", report)
	}
	if report.PositiveTokenSavings != 9 || report.PositiveReplayByteSavings != 9 {
		t.Fatalf("bad token/replay aggregate: %+v", report)
	}
	if got := report.CaptureReports[0].ExpectedReducerHits["read_delta"]; got != 1 {
		t.Fatalf("expected reducer hit not recorded: %+v", report.CaptureReports[0].ExpectedReducerHits)
	}
}

func TestWSSProofMatrixLiveTokensGateBeatsReplayBytes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofRepeatReadFrames(t, framesPath, "token-gate")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "token-zero",
		Client:        "cli",
		WorkloadClass: "repeat_full_read",
		FramesPath:    framesPath,
		LiveDelta:     &codexCaptureLiveDelta{},
	})

	report, err := loadWSSProofMatrixReport(matrixPath)
	if err != nil {
		t.Fatal(err)
	}
	if report.GatePassed || report.CapturesWithIssues != 1 {
		t.Fatalf("expected live-token gate failure, got %+v", report)
	}
	if len(report.CaptureReports) != 1 || !strings.Contains(strings.Join(report.CaptureReports[0].GateFailures, "\n"), "live billable_input_tokens_saved") {
		t.Fatalf("missing token gate failure: %+v", report.CaptureReports)
	}
}

func TestWSSProofMatrixRequireLiveTokenDelta(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofRepeatReadFrames(t, framesPath, "strict-token-gate")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "legacy-replay-only",
		Client:        "cli",
		WorkloadClass: "repeat_full_read",
		FramesPath:    framesPath,
	})

	legacyReport, err := loadWSSProofMatrixReport(matrixPath)
	if err != nil {
		t.Fatal(err)
	}
	if !legacyReport.CaptureReports[0].GatePassed || legacyReport.GatePassed {
		t.Fatalf("single-row aggregate should fail but capture should use replay fallback in legacy mode: %+v", legacyReport)
	}
	if legacyReport.PositiveSavings != 1 || legacyReport.PositiveTokenSavings != 0 || legacyReport.PositiveReplayByteSavings != 1 {
		t.Fatalf("legacy fallback counters wrong: %+v", legacyReport)
	}

	strictReport, err := loadWSSProofMatrixReportWithOptions(matrixPath, wssProofMatrixOptions{requireLiveTokenDelta: true})
	if err != nil {
		t.Fatal(err)
	}
	if strictReport.CaptureReports[0].GatePassed || strictReport.PositiveSavings != 0 || strictReport.PositiveReplayByteSavings != 1 {
		t.Fatalf("strict mode should fail without counting replay as positive savings: %+v", strictReport)
	}
	if !strings.Contains(strings.Join(strictReport.CaptureReports[0].GateFailures, "\n"), "live_delta is required") {
		t.Fatalf("missing strict live_delta failure: %+v", strictReport.CaptureReports[0].GateFailures)
	}
}

func TestWSSProofMatrixFocusedGate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofRepeatReadFrames(t, framesPath, "focused-search")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:               "focused-search",
		Client:           "cli",
		WorkloadClass:    "search_loop",
		FramesPath:       framesPath,
		ExpectedReducers: []string{"read_delta"},
		LiveDelta:        proofMatrixLiveDelta(false),
	})

	releaseReport, err := loadWSSProofMatrixReportWithOptions(matrixPath, wssProofMatrixOptions{
		requireLiveTokenDelta: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if releaseReport.GatePassed || !strings.Contains(strings.Join(releaseReport.GateFailures, "\n"), "expected at least 10 captures") {
		t.Fatalf("single-row release gate should fail: %+v", releaseReport)
	}

	focusedReport, err := loadWSSProofMatrixReportWithOptions(matrixPath, wssProofMatrixOptions{
		requireLiveTokenDelta: true,
		requiredWorkloads:     []string{"search_loop"},
		minCaptures:           1,
		minCLI:                1,
		minDesktop:            0,
		minPositive:           1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !focusedReport.GatePassed || len(focusedReport.MissingWorkloads) != 0 {
		t.Fatalf("focused proof gate should pass: %+v", focusedReport)
	}
}

func TestParseWSSProofMatrixFocusedFlags(t *testing.T) {
	flags, err := parseWSSProofMatrixFlags([]string{
		"matrix.jsonl",
		"--require-live-token-delta",
		"--required-workload=search_loop",
		"--required-workloads=git_status_diff,ranged_read",
		"--min-captures=3",
		"--min-cli=2",
		"--min-desktop=1",
		"--min-positive=3",
		"--expected-reducer", "chunk_dedup",
		"--expected-reducer=host_budget_ok",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !flags.requireLiveTokenDelta || flags.minCaptures != 3 || flags.minCLI != 2 || flags.minDesktop != 1 || flags.minPositive != 3 {
		t.Fatalf("focused flags not parsed: %+v", flags)
	}
	if strings.Join(flags.requiredWorkloads, ",") != "search_loop,git_status_diff,ranged_read" {
		t.Fatalf("required workloads not parsed: %+v", flags.requiredWorkloads)
	}
	if strings.Join(flags.expectedReducers, ",") != "chunk_dedup,host_budget_ok" {
		t.Fatalf("expected reducers not parsed: %+v", flags.expectedReducers)
	}
	if _, err := parseWSSProofMatrixFlags([]string{"--min-captures=-1"}); err == nil {
		t.Fatal("negative minimum should fail")
	}
	if _, err := parseWSSProofMatrixFlags([]string{"--expected-reducer"}); err == nil {
		t.Fatal("missing expected reducer value should fail")
	}
}

func TestWSSProofMatrixRequiredReducerAggregateGate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofRepeatReadFrames(t, framesPath, "aggregate-reducer")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "aggregate-reducer",
		Client:        "desktop",
		WorkloadClass: "similar_files",
		FramesPath:    framesPath,
		LiveDelta: &codexCaptureLiveDelta{
			BillableInputTokensSaved: 100,
			ProxyLayer0ChunkDedup:    1,
			ProxyLayer0ChunkRefs:     2,
			HostBudgetStatus:         "ok",
			HostBudgetCompressionOK:  true,
			HostBudgetDegradationOK:  true,
		},
	})

	report, err := loadWSSProofMatrixReportWithOptions(matrixPath, wssProofMatrixOptions{
		requireLiveTokenDelta: true,
		requiredWorkloads:     []string{"similar_files"},
		expectedReducers:      []string{"chunk_dedup", "chunk_dedup_refs", "host_budget_ok"},
		minCaptures:           1,
		minDesktop:            1,
		minPositive:           1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed {
		t.Fatalf("required reducer aggregate gate should pass: %+v", report)
	}
	if report.RequiredReducerHits["chunk_dedup_refs"] != 2 || report.RequiredReducerHits["host_budget_ok"] != 1 {
		t.Fatalf("required reducer hits not recorded: %+v", report.RequiredReducerHits)
	}
}

func TestWSSProofMatrixRequiredReducerAggregateGateFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofRepeatReadFrames(t, framesPath, "aggregate-reducer-fail")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "aggregate-reducer-fail",
		Client:        "cli",
		WorkloadClass: "similar_files",
		FramesPath:    framesPath,
		LiveDelta: &codexCaptureLiveDelta{
			BillableInputTokensSaved: 100,
		},
	})

	report, err := loadWSSProofMatrixReportWithOptions(matrixPath, wssProofMatrixOptions{
		requireLiveTokenDelta: true,
		requiredWorkloads:     []string{"similar_files"},
		expectedReducers:      []string{"chunk_dedup", "not_a_reducer"},
		minCaptures:           1,
		minCLI:                1,
		minPositive:           1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.GatePassed {
		t.Fatalf("required reducer aggregate gate should fail: %+v", report)
	}
	failures := strings.Join(report.GateFailures, "\n")
	if !strings.Contains(failures, "chunk_dedup") || !strings.Contains(failures, "unknown:not_a_reducer") {
		t.Fatalf("missing required reducer failures: %s", failures)
	}
}

func TestWSSProofMatrixExpectedReducerGate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofRepeatReadFrames(t, framesPath, "missing-reducer")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:               "missing-reducer",
		Client:           "cli",
		WorkloadClass:    "repeat_full_read",
		FramesPath:       framesPath,
		ExpectedReducers: []string{"read_delta", "not_a_reducer"},
		LiveDelta: &codexCaptureLiveDelta{
			BillableInputTokensSaved: 100,
		},
	})

	report, err := loadWSSProofMatrixReport(matrixPath)
	if err != nil {
		t.Fatal(err)
	}
	if report.GatePassed || report.CapturesWithIssues != 1 {
		t.Fatalf("expected reducer gate failure, got %+v", report)
	}
	failures := strings.Join(report.CaptureReports[0].GateFailures, "\n")
	if !strings.Contains(failures, "expected reducer read_delta did not fire") ||
		!strings.Contains(failures, "unknown expected reducer: not_a_reducer") {
		t.Fatalf("missing reducer failures:\n%s", failures)
	}
}

func TestWSSProofMatrixExtendedExpectedSignals(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofRepeatReadFrames(t, framesPath, "extended-signals")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "extended-signals",
		Client:        "cli",
		WorkloadClass: "search_loop",
		FramesPath:    framesPath,
		ExpectedReducers: []string{
			"chunk_dedup_refs",
			"tool_prune",
			"tool_prune_reattach",
			"tool_prune_retry",
			"output_reduce_injected",
			"output_reduce_skipped",
			"output_reduce_downgraded",
			"stop_seq",
			"streamcut",
			"repdet",
			"stale_read",
			"obsolete_prune",
			"beterse",
			"host_budget_ok",
		},
		LiveDelta: &codexCaptureLiveDelta{
			BillableInputTokensSaved: 100,
			ProxyLayer0ChunkRefs:     1,
			ToolPrunePruned:          1,
			ToolPruneReattach:        1,
			ToolPruneRetry:           1,
			OutputReduceInjected:     1,
			OutputReduceSkipped:      1,
			OutputReduceDowngrades:   1,
			StopSeqRequestsModified:  1,
			StreamcutFired:           1,
			RepdetResponsesRewritten: 1,
			StaleReadBlocksReplaced:  1,
			ObsoleteReadBlocksPruned: 1,
			BeterseInjections:        1,
			HostBudgetStatus:         "ok",
			HostBudgetCompressionOK:  true,
			HostBudgetDegradationOK:  true,
		},
	})

	report, err := loadWSSProofMatrixReportWithOptions(matrixPath, wssProofMatrixOptions{
		requireLiveTokenDelta: true,
		requiredWorkloads:     []string{"search_loop"},
		minCaptures:           1,
		minCLI:                1,
		minPositive:           1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed {
		t.Fatalf("extended signal proof should pass: %+v", report)
	}
	if got := report.CaptureReports[0].ExpectedReducerHits["host_budget_ok"]; got != 1 {
		t.Fatalf("host_budget_ok hit = %d", got)
	}
}

func TestWSSProofMatrixHostBudgetFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofRepeatReadFrames(t, framesPath, "host-budget-fail")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "host-budget-fail",
		Client:        "cli",
		WorkloadClass: "search_loop",
		FramesPath:    framesPath,
		LiveDelta: &codexCaptureLiveDelta{
			BillableInputTokensSaved: 100,
			HostBudgetStatus:         "attention",
			HostBudgetExceeded:       true,
			HostBudgetReasons:        []string{"rss_budget_exceeded"},
			HostBudgetCompressionOK:  true,
			HostBudgetDegradationOK:  true,
		},
	})

	report, err := loadWSSProofMatrixReportWithOptions(matrixPath, wssProofMatrixOptions{
		requireLiveTokenDelta: true,
		requiredWorkloads:     []string{"search_loop"},
		minCaptures:           1,
		minCLI:                1,
		minPositive:           1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.GatePassed || report.CapturesWithIssues != 1 {
		t.Fatalf("host budget failure should fail matrix: %+v", report)
	}
	if !strings.Contains(strings.Join(report.CaptureReports[0].GateFailures, "\n"), "host budget not ok") {
		t.Fatalf("missing host budget failure: %+v", report.CaptureReports[0].GateFailures)
	}
}

func TestRunWSSProofMatrixJSONFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofControlFrames(t, framesPath, "control")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "bad",
		Client:        "cli",
		WorkloadClass: "repeat_full_read",
		FramesPath:    framesPath,
	})

	var stdout, stderr bytes.Buffer
	code := runWSSProofMatrix([]string{matrixPath, "--json"}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("runWSSProofMatrix code=%d want 3 stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var report wssProofMatrixReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json output did not parse: %v\n%s", err, stdout.String())
	}
	if report.GatePassed || len(report.GateFailures) == 0 || report.CapturesWithIssues == 0 {
		t.Fatalf("expected failed proof matrix, got %+v", report)
	}

	stdout.Reset()
	stderr.Reset()
	code = runWSSProofMatrix([]string{"--help"}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "wss-proof-matrix") {
		t.Fatalf("help failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func writeProofRepeatReadFrames(t *testing.T, path, session string) {
	t.Helper()
	var file strings.Builder
	for i := 0; i < 140; i++ {
		fmt.Fprintf(&file, "proof matrix repeated content line %03d\n", i)
	}
	writeJSONLFile(t, path,
		wssABReplayTestRecord("server_to_client", map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{
				"type":      "function_call",
				"call_id":   session + "-read-1",
				"name":      "read_file",
				"arguments": `{"path":"src/proof.md"}`,
			},
		}),
		wssABReplayTestRecord("client_to_server", wssABReplayTestOutputBody(session+"-read-1", session, session+"-resp-1", file.String())),
		wssABReplayTestRecord("server_to_client", map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{
				"type":      "function_call",
				"call_id":   session + "-read-2",
				"name":      "read_file",
				"arguments": `{"path":"src/proof.md"}`,
			},
		}),
		wssABReplayTestRecord("client_to_server", wssABReplayTestOutputBody(session+"-read-2", session, session+"-resp-2", file.String())),
	)
}

func writeProofControlFrames(t *testing.T, path, session string) {
	t.Helper()
	writeJSONLFile(t, path, wssABReplayTestRecord("client_to_server", map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": session,
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": "small control turn",
		}},
		"stream": true,
	}))
}

func proofMatrixLiveDelta(expectedZero bool) *codexCaptureLiveDelta {
	if expectedZero {
		return &codexCaptureLiveDelta{}
	}
	return &codexCaptureLiveDelta{
		BillableInputTokensSaved:  100,
		InputTokensSaved:          100,
		PhasefBridged:             1,
		CompressedMessagesMutated: 1,
		FramesReencoded:           1,
		PhasefMutations:           1,
		ProxyLayer0ReadDelta:      1,
	}
}

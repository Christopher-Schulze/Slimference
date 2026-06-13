package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
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
		if class == "search_loop" {
			writeProofSearchFrames(t, framesPath, fmt.Sprintf("session-%02d", i))
		} else if expectedZero {
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
	if !report.CaptureReports[0].Replay.ToolOutputMutation {
		t.Fatalf("proof matrix replay must disclose lab/proof tool-output mutation: %+v", report.CaptureReports[0].Replay)
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
	if len(report.CaptureReports) != 1 || !strings.Contains(strings.Join(report.CaptureReports[0].GateFailures, "\n"), "positive live economic signal") {
		t.Fatalf("missing token gate failure: %+v", report.CaptureReports)
	}
}

func TestWSSProofMatrixFailsOnReplayUpstreamError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofSearchFrames(t, framesPath, "upstream-error-matrix")
	appendJSONLFile(t, framesPath, wssABReplayTestRecord("server_to_client", map[string]any{
		"type":   "error",
		"status": 400,
		"error": map[string]any{
			"type":    "invalid_request_error",
			"message": "Invalid request",
		},
	}))
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "search-upstream-error",
		Client:        "cli",
		WorkloadClass: "search_loop",
		FramesPath:    framesPath,
		LiveDelta:     proofMatrixLiveDelta(false),
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
		t.Fatalf("upstream error replay should fail focused proof: %+v", report)
	}
	capture := report.CaptureReports[0]
	if capture.Replay.UpstreamInvalidRequests != 1 ||
		!strings.Contains(strings.Join(capture.GateFailures, "\n"), "upstream_error_frames=1") {
		t.Fatalf("missing upstream-error gate failure: %+v", capture)
	}
}

func TestWSSProofMatrixExpectedZeroAllowsProviderCacheEvidence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofControlFrames(t, framesPath, "provider-cache")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:                  "provider-cache",
		Client:              "cli",
		WorkloadClass:       "provider_cache_long_session",
		FramesPath:          framesPath,
		ExpectedReducers:    []string{"provider_cache_read", "host_budget_ok"},
		ExpectedZeroSavings: true,
		LiveDelta: &codexCaptureLiveDelta{
			ProviderCacheReadTokens: 3456,
			HostBudgetStatus:        "ok",
			HostBudgetCompressionOK: true,
			HostBudgetDegradationOK: true,
		},
	})

	report, err := loadWSSProofMatrixReportWithOptions(matrixPath, wssProofMatrixOptions{
		requireLiveTokenDelta: true,
		requiredWorkloads:     []string{"provider_cache_long_session"},
		expectedReducers:      []string{"provider_cache_read", "host_budget_ok"},
		minCaptures:           1,
		minCLI:                1,
		minPositive:           1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed || report.PositiveTokenSavings != 1 || report.ExpectedZero != 1 {
		t.Fatalf("provider-cache evidence should be economic-positive while local-zero: %+v", report)
	}
}

func TestWSSProofMatrixExpectedZeroRejectsLocalSavings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofControlFrames(t, framesPath, "expected-zero-local")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:                  "expected-zero-local",
		Client:              "cli",
		WorkloadClass:       "no_savings_control",
		FramesPath:          framesPath,
		ExpectedZeroSavings: true,
		LiveDelta: &codexCaptureLiveDelta{
			BillableInputTokensSaved: 100,
		},
	})

	report, err := loadWSSProofMatrixReportWithOptions(matrixPath, wssProofMatrixOptions{
		requireLiveTokenDelta: true,
		requiredWorkloads:     []string{"no_savings_control"},
		minCaptures:           1,
		minCLI:                1,
		minPositive:           0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.GatePassed || !strings.Contains(strings.Join(report.CaptureReports[0].GateFailures, "\n"), "expected zero local savings") {
		t.Fatalf("local savings must fail expected-zero rows: %+v", report)
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
	if strictReport.CaptureReports[0].GatePassed || strictReport.PositiveSavings != 0 || strictReport.PositiveReplayByteSavings != 0 {
		t.Fatalf("strict mode should fail without counting failed replay row as positive savings: %+v", strictReport)
	}
	if !strings.Contains(strings.Join(strictReport.CaptureReports[0].GateFailures, "\n"), "live_delta is required") {
		t.Fatalf("missing strict live_delta failure: %+v", strictReport.CaptureReports[0].GateFailures)
	}
}

func TestWSSProofMatrixCoverageCountsOnlyPassedRows(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofSearchFrames(t, framesPath, "failed-coverage")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "failed-coverage",
		Client:        "cli",
		WorkloadClass: "search_loop",
		FramesPath:    framesPath,
		LiveDelta: &codexCaptureLiveDelta{
			BillableInputTokensSaved: 100,
			ParseFailures:            1,
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
	if report.GatePassed || report.Captures != 1 || report.CapturesWithIssues != 1 {
		t.Fatalf("failed row should fail the matrix gate: %+v", report)
	}
	if report.CLI != 0 || report.PositiveSavings != 0 || report.PositiveTokenSavings != 0 || report.WorkloadClasses["search_loop"] != 0 {
		t.Fatalf("failed row must not count toward coverage or savings: %+v", report)
	}
	failures := strings.Join(report.GateFailures, "\n")
	for _, want := range []string{
		"expected at least 1 valid captures, got 0",
		"expected at least 1 CLI captures, got 0",
		"missing workload classes: search_loop",
		"expected at least 1 positive-token-savings or expected-zero captures, got 0",
		"1 capture(s) failed per-capture gates",
	} {
		if !strings.Contains(failures, want) {
			t.Fatalf("missing gate failure %q in:\n%s", want, failures)
		}
	}
}

func TestWSSProofMatrixFocusedGate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofSearchFrames(t, framesPath, "focused-search")
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
	if releaseReport.GatePassed || !strings.Contains(strings.Join(releaseReport.GateFailures, "\n"), "expected at least 10 valid captures") {
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

func TestWSSProofMatrixSearchCapProofGate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeSearchCapProofFullHistoryFrames(t, framesPath, "matrix-search-cap", 96)
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "matrix-search-cap",
		Client:        "cli",
		WorkloadClass: "search_loop",
		FramesPath:    framesPath,
		LiveDelta:     proofMatrixLiveDelta(false),
	})
	var candidates searchCapProfileCandidateFlags
	if err := candidates.Set("8:6"); err != nil {
		t.Fatal(err)
	}
	if err := candidates.Set("4:4"); err != nil {
		t.Fatal(err)
	}

	report, err := loadWSSProofMatrixReportWithOptions(matrixPath, wssProofMatrixOptions{
		requireLiveTokenDelta:            true,
		requiredWorkloads:                []string{"search_loop"},
		searchCapCandidates:              []searchCapProfileCandidate(candidates),
		searchCapMinCandidateRetainedPct: 40,
		searchCapMinSearchOutputs:        1,
		minCaptures:                      1,
		minCLI:                           1,
		minPositive:                      1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed || len(report.CaptureReports) != 1 {
		t.Fatalf("search-cap proof should pass matrix gate: %+v", report)
	}
	proof := report.CaptureReports[0].SearchCapProof
	if proof == nil || proof.SelectedCandidate == nil || proof.SelectedCandidate.Name != "candidate_4x4" {
		t.Fatalf("search-cap proof not attached or wrong selection: %+v", report.CaptureReports[0])
	}
	if len(report.CaptureReports[0].Replay.Elisions) != 0 {
		t.Fatalf("matrix report must not carry raw replay elision previews: %+v", report.CaptureReports[0].Replay.Elisions)
	}

	var text bytes.Buffer
	writeWSSProofMatrixText(&text, report)
	if !strings.Contains(text.String(), "search_cap: candidate_4x4") {
		t.Fatalf("matrix text missing search-cap selection:\n%s", text.String())
	}
}

func TestWSSProofMatrixSearchCapProofSatisfiesFullHistorySearchMutationGate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeSearchCapProofFullHistoryFrames(t, framesPath, "matrix-search-cap-full-history", 96)
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "matrix-search-cap-full-history",
		Client:        "cli",
		WorkloadClass: "search_loop",
		FramesPath:    framesPath,
		LiveDelta:     proofMatrixLiveDelta(false),
	})
	var candidates searchCapProfileCandidateFlags
	if err := candidates.Set("8:6"); err != nil {
		t.Fatal(err)
	}

	report, err := loadWSSProofMatrixReportWithOptions(matrixPath, wssProofMatrixOptions{
		requireLiveTokenDelta:            true,
		requiredWorkloads:                []string{"search_loop"},
		searchCapCandidates:              []searchCapProfileCandidate(candidates),
		searchCapMinCandidateRetainedPct: 40,
		searchCapMinSearchOutputs:        1,
		minCaptures:                      1,
		minCLI:                           1,
		minPositive:                      1,
	})
	if err != nil {
		t.Fatal(err)
	}
	capture := report.CaptureReports[0]
	if !report.GatePassed || capture.SearchCapProof == nil || !capture.SearchCapProof.GatePassed {
		t.Fatalf("search-cap proof should satisfy full-history search mutation gate: %+v", report)
	}
	if !capture.SearchCapProof.DefaultReplay.SearchCapProofLatch ||
		capture.SearchCapProof.DefaultReplay.SearchMutatedRequests == 0 ||
		capture.SearchCapProof.DefaultReplay.ToolOutputMutation ||
		capture.SearchCapProof.DefaultReplay.DeltaToolOutputMutation {
		t.Fatalf("search-cap proof should use product latch, not lab mutation: %+v", capture.SearchCapProof.DefaultReplay)
	}
	if strings.Contains(strings.Join(capture.GateFailures, "\n"), "named search-output mutation") {
		t.Fatalf("search-cap proof should suppress baseline mutation failure: %+v", capture.GateFailures)
	}
}

func writeProofSearchDeltaFrames(t *testing.T, path, session string) {
	t.Helper()
	callID := session + "-search-1"
	writeJSONLFile(t, path,
		wssABReplayTestRecord("server_to_client", map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{
				"type":      "function_call",
				"call_id":   callID,
				"name":      "exec_command",
				"arguments": `{"cmd":"rg -n needle src"}`,
			},
		}),
		wssABReplayTestRecord("client_to_server", wssABReplayTestOutputBody(
			callID,
			session,
			session+"-previous-response",
			wssABReplaySearchOutputFixture("needle", 96),
		)),
	)
}

func TestWSSProofMatrixSearchCapDefaultsFailClosed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeSearchCapProofFullHistoryFrames(t, framesPath, "matrix-search-cap-defaults", 96)
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "matrix-search-cap-defaults",
		Client:        "cli",
		WorkloadClass: "search_loop",
		FramesPath:    framesPath,
		LiveDelta:     proofMatrixLiveDelta(false),
	})
	var candidates searchCapProfileCandidateFlags
	if err := candidates.Set("8:6"); err != nil {
		t.Fatal(err)
	}

	report, err := loadWSSProofMatrixReportWithOptions(matrixPath, wssProofMatrixOptions{
		requireLiveTokenDelta: true,
		requiredWorkloads:     []string{"search_loop"},
		searchCapCandidates:   []searchCapProfileCandidate(candidates),
		minCaptures:           1,
		minCLI:                1,
		minPositive:           1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.GatePassed || report.CapturesWithIssues != 1 {
		t.Fatalf("default search-cap thresholds should fail weak capture: %+v", report)
	}
	proof := report.CaptureReports[0].SearchCapProof
	if proof == nil ||
		proof.MinCandidateRetainedPct != releaseSearchCapMinRetainedPct ||
		proof.MinSearchOutputs != releaseSearchCapMinSearchOutputs ||
		proof.MinExtraReducerTokens != releaseSearchCapMinExtraReducerTokens {
		t.Fatalf("search-cap defaults not embedded in proof: %+v", proof)
	}
	if got := strings.Join(report.CaptureReports[0].GateFailures, "\n"); !strings.Contains(got, "search_cap_proof: search outputs 1 < min 2") {
		t.Fatalf("missing default min-search-output failure:\n%s", got)
	}
}

func TestWSSProofMatrixSearchCapProofFailureFailsCapture(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeSearchCapProofFullHistoryFrames(t, framesPath, "matrix-search-cap-fail", 96)
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "matrix-search-cap-fail",
		Client:        "cli",
		WorkloadClass: "search_loop",
		FramesPath:    framesPath,
		LiveDelta:     proofMatrixLiveDelta(false),
	})
	var candidates searchCapProfileCandidateFlags
	if err := candidates.Set("8:6"); err != nil {
		t.Fatal(err)
	}

	report, err := loadWSSProofMatrixReportWithOptions(matrixPath, wssProofMatrixOptions{
		requireLiveTokenDelta:     true,
		requiredWorkloads:         []string{"search_loop"},
		searchCapCandidates:       []searchCapProfileCandidate(candidates),
		searchCapMinSearchOutputs: 2,
		minCaptures:               1,
		minCLI:                    1,
		minPositive:               1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.GatePassed || report.CapturesWithIssues != 1 {
		t.Fatalf("search-cap proof failure should fail capture: %+v", report)
	}
	if got := strings.Join(report.CaptureReports[0].GateFailures, "\n"); !strings.Contains(got, "search_cap_proof: search outputs 1 < min 2") {
		t.Fatalf("missing search-cap proof failure:\n%s", got)
	}
}

func TestWSSProofMatrixSearchLoopRequiresSearchMutation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofSearchFramesWithCount(t, framesPath, "focused-search-no-mutation", 1)
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "focused-search-no-mutation",
		Client:        "cli",
		WorkloadClass: "search_loop",
		FramesPath:    framesPath,
		LiveDelta:     proofMatrixLiveDelta(false),
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
	if report.GatePassed || !strings.Contains(strings.Join(report.CaptureReports[0].GateFailures, "\n"), "named search-output mutation") {
		t.Fatalf("search_loop without search mutation should fail: %+v", report)
	}
}

func TestWriteWSSProofMatrixTextIncludesCaptureFailures(t *testing.T) {
	report := wssProofMatrixReport{
		Path:                      "matrix.jsonl",
		Captures:                  1,
		CLI:                       1,
		PositiveSavings:           1,
		PositiveTokenSavings:      1,
		PositiveReplayByteSavings: 0,
		CapturesWithIssues:        1,
		WorkloadClasses:           map[string]int{"search_loop": 1},
		GateFailures:              []string{"1 capture(s) failed per-capture gates"},
		CaptureReports: []wssProofMatrixCapture{{
			ID:            "search-no-mutation",
			Client:        "cli",
			WorkloadClass: "search_loop",
			LiveDelta:     proofMatrixLiveDelta(false),
			Replay:        wssABReplayReport{BytesSaved: 0, MutatedRequests: 0},
			GateFailures:  []string{"search_loop proof has no named search-output mutation"},
		}},
	}

	var out bytes.Buffer
	writeWSSProofMatrixText(&out, report)
	text := out.String()
	for _, want := range []string{
		"WSS proof matrix: matrix.jsonl",
		"search_loop",
		"search-no-mutation",
		"billable_tokens=100 economic_tokens=100",
		"search_loop proof has no named search-output mutation",
		"1 capture(s) failed per-capture gates",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("matrix text missing %q:\n%s", want, text)
		}
	}
}

func TestWSSProofMatrixFocusedGateIgnoresOutOfScopeRows(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	goodFramesPath := filepath.Join(dir, "good-frames.jsonl")
	badFramesPath := filepath.Join(dir, "bad-frames.jsonl")
	writeProofRepeatReadFrames(t, goodFramesPath, "focused-good")
	writeProofRepeatReadFrames(t, badFramesPath, "focused-bad")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath,
		wssProofMatrixRecord{
			ID:               "old-exploratory-failure",
			Client:           "cli",
			WorkloadClass:    "build_test_lint_failure",
			FramesPath:       badFramesPath,
			ExpectedReducers: []string{"not_a_reducer"},
			LiveDelta:        &codexCaptureLiveDelta{},
		},
		wssProofMatrixRecord{
			ID:               "focused-host-resource",
			Client:           "cli",
			WorkloadClass:    "host_resource_long_workday",
			FramesPath:       goodFramesPath,
			ExpectedReducers: []string{"read_delta", "codex_exec_envelope"},
			LiveDelta: &codexCaptureLiveDelta{
				BillableInputTokensSaved: 100,
				ProviderCacheReadTokens:  1000,
				HostBudgetStatus:         "ok",
				HostBudgetCompressionOK:  true,
				HostBudgetDegradationOK:  true,
			},
		},
	)

	releaseReport, err := loadWSSProofMatrixReportWithOptions(matrixPath, wssProofMatrixOptions{
		requireLiveTokenDelta: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if releaseReport.GatePassed {
		t.Fatalf("unfocused release report must still fail on the exploratory row: %+v", releaseReport)
	}

	focusedReport, err := loadWSSProofMatrixReportWithOptions(matrixPath, wssProofMatrixOptions{
		requireLiveTokenDelta: true,
		requiredWorkloads:     []string{"host_resource_long_workday"},
		expectedReducers:      []string{"host_budget_ok"},
		minCaptures:           1,
		minCLI:                1,
		minPositive:           1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !focusedReport.GatePassed || focusedReport.Captures != 1 || len(focusedReport.CaptureReports) != 1 {
		t.Fatalf("focused proof should ignore out-of-scope rows: %+v", focusedReport)
	}
	if focusedReport.CaptureReports[0].ID != "focused-host-resource" {
		t.Fatalf("unexpected focused capture: %+v", focusedReport.CaptureReports)
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
		"--search-cap-candidate", "30:15",
		"--search-cap-candidate=25:15",
		"--search-cap-min-retained-pct=40",
		"--search-cap-min-search-outputs=2",
		"--search-cap-min-extra-tokens=1",
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
	if len(flags.searchCapCandidates) != 2 ||
		flags.searchCapCandidates[0].Options.MaxFilesShown != 30 ||
		flags.searchCapCandidates[1].Options.MaxMatchesPerFile != 15 ||
		flags.searchCapMinCandidateRetainedPct != 40 ||
		flags.searchCapMinSearchOutputs != 2 ||
		flags.searchCapMinExtraReducerTokens != 1 ||
		!flags.searchCapMinExtraReducerTokensIsSet {
		t.Fatalf("search-cap proof flags not parsed: %+v", flags)
	}
	if _, err := parseWSSProofMatrixFlags([]string{"--min-captures=-1"}); err == nil {
		t.Fatal("negative minimum should fail")
	}
	if _, err := parseWSSProofMatrixFlags([]string{"--expected-reducer"}); err == nil {
		t.Fatal("missing expected reducer value should fail")
	}
	if _, err := parseWSSProofMatrixFlags([]string{"--search-cap-min-search-outputs=1"}); err == nil {
		t.Fatal("search-cap thresholds without candidates should fail")
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

func TestWSSProofMatrixToolPruneTokensCountAsEconomicSignal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofRepeatReadFrames(t, framesPath, "tool-heavy")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "tool-heavy",
		Client:        "desktop",
		WorkloadClass: "tool_heavy",
		FramesPath:    framesPath,
		ExpectedReducers: []string{
			"tool_prune",
			"tool_prune_tokens_saved",
			"host_budget_ok",
		},
		LiveDelta: &codexCaptureLiveDelta{
			ToolPrunePruned:         1,
			ToolPruneTokensSaved:    26,
			HostBudgetStatus:        "ok",
			HostBudgetCompressionOK: true,
			HostBudgetDegradationOK: true,
		},
	})

	report, err := loadWSSProofMatrixReportWithOptions(matrixPath, wssProofMatrixOptions{
		requireLiveTokenDelta: true,
		requiredWorkloads:     []string{"tool_heavy"},
		minCaptures:           1,
		minDesktop:            1,
		minCLI:                0,
		minPositive:           1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed || report.PositiveTokenSavings != 1 || len(report.CaptureReports) != 1 || !report.CaptureReports[0].GatePassed {
		t.Fatalf("tool-prune live signal should pass: %+v", report)
	}
}

func TestWSSProofMatrixOutputReduceRequiresObservedOutputTokens(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofRepeatReadFrames(t, framesPath, "output-reduce")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "output-reduce-no-output-tokens",
		Client:        "cli",
		WorkloadClass: "output_reduce_aggressive",
		FramesPath:    framesPath,
		ExpectedReducers: []string{
			"output_reduce_injected",
			"host_budget_ok",
		},
		LiveDelta: &codexCaptureLiveDelta{
			OutputReduceInjected:    1,
			HostBudgetStatus:        "ok",
			HostBudgetCompressionOK: true,
			HostBudgetDegradationOK: true,
		},
	})

	report, err := loadWSSProofMatrixReportWithOptions(matrixPath, wssProofMatrixOptions{
		requireLiveTokenDelta: true,
		requiredWorkloads:     []string{"output_reduce_aggressive"},
		minCaptures:           1,
		minCLI:                1,
		minPositive:           1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.GatePassed || report.PositiveTokenSavings != 0 {
		t.Fatalf("output-reduce proof without output tokens must fail: %+v", report)
	}
	if got := strings.Join(report.CaptureReports[0].GateFailures, "\n"); !strings.Contains(got, "positive live economic signal") {
		t.Fatalf("missing economic-signal failure:\n%s", got)
	}

	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "output-reduce-with-output-tokens",
		Client:        "cli",
		WorkloadClass: "output_reduce_aggressive",
		FramesPath:    framesPath,
		ExpectedReducers: []string{
			"output_reduce_injected",
			"output_reduce_output_tokens",
			"host_budget_ok",
		},
		LiveDelta: &codexCaptureLiveDelta{
			OutputReduceInjected:             1,
			OutputReduceOutputTokensObserved: 42,
			HostBudgetStatus:                 "ok",
			HostBudgetCompressionOK:          true,
			HostBudgetDegradationOK:          true,
		},
	})
	report, err = loadWSSProofMatrixReportWithOptions(matrixPath, wssProofMatrixOptions{
		requireLiveTokenDelta: true,
		requiredWorkloads:     []string{"output_reduce_aggressive"},
		expectedReducers: []string{
			"output_reduce_injected",
			"output_reduce_output_tokens",
			"host_budget_ok",
		},
		minCaptures: 1,
		minCLI:      1,
		minPositive: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed || report.PositiveTokenSavings != 1 {
		t.Fatalf("output-reduce proof with observed output tokens should pass: %+v", report)
	}
	if got := report.RequiredReducerHits["output_reduce_output_tokens"]; got != 42 {
		t.Fatalf("output_reduce_output_tokens hit = %d", got)
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
	writeProofSearchFrames(t, framesPath, "extended-signals")
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
			"tool_prune_tokens_saved",
			"output_reduce_injected",
			"output_reduce_skipped",
			"output_reduce_downgraded",
			"stop_seq",
			"streamcut",
			"repdet",
			"stale_read",
			"obsolete_prune",
			"beterse",
			"provider_cache_read",
			"provider_cache_create",
			"host_budget_ok",
		},
		LiveDelta: &codexCaptureLiveDelta{
			BillableInputTokensSaved:  100,
			ProxyLayer0ChunkRefs:      1,
			ToolPrunePruned:           1,
			ToolPruneReattach:         1,
			ToolPruneRetry:            1,
			ToolPruneTokensSaved:      222,
			OutputReduceInjected:      1,
			OutputReduceSkipped:       1,
			OutputReduceDowngrades:    1,
			StopSeqRequestsModified:   1,
			StreamcutFired:            1,
			RepdetResponsesRewritten:  1,
			StaleReadBlocksReplaced:   1,
			ObsoleteReadBlocksPruned:  1,
			BeterseInjections:         1,
			ProviderCacheReadTokens:   777,
			ProviderCacheCreateTokens: 111,
			HostBudgetStatus:          "ok",
			HostBudgetCompressionOK:   true,
			HostBudgetDegradationOK:   true,
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
	if got := report.CaptureReports[0].ExpectedReducerHits["provider_cache_read"]; got != 777 {
		t.Fatalf("provider_cache_read hit = %d", got)
	}
	if got := report.CaptureReports[0].ExpectedReducerHits["tool_prune_tokens_saved"]; got != 222 {
		t.Fatalf("tool_prune_tokens_saved hit = %d", got)
	}
}

func TestWSSProofMatrixHostBudgetFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	writeProofSearchFrames(t, framesPath, "host-budget-fail")
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
		wssABReplayTestRecord("client_to_server", wssABReplayTestOutputBody(session+"-read-1", session, "", file.String())),
		wssABReplayTestRecord("server_to_client", map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{
				"type":      "function_call",
				"call_id":   session + "-read-2",
				"name":      "read_file",
				"arguments": `{"path":"src/proof.md"}`,
			},
		}),
		wssABReplayTestRecord("client_to_server", wssABReplayTestOutputBody(session+"-read-2", session, "", file.String())),
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

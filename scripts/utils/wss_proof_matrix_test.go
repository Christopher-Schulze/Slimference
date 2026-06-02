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

package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestWSSSavingsBaselineAggregatesProductSearchAndGuardGaps(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	readPath := filepath.Join(dir, "repeat-read.frames.jsonl")
	rootSearchPath := filepath.Join(dir, "root-search.frames.jsonl")
	deltaSearchPath := filepath.Join(dir, "delta-search.frames.jsonl")
	invalidPath := filepath.Join(dir, "matrix.jsonl")

	writeProofRepeatReadFrames(t, readPath, "baseline-read")
	writeProofSearchFrames(t, rootSearchPath, "baseline-root-search")
	writeBaselineDeltaSearchFrames(t, deltaSearchPath, "baseline-delta-search")
	writeJSONLFile(t, invalidPath, map[string]any{"not": "a replay frame"})

	report, err := loadWSSSavingsBaselineReport(wssSavingsBaselineFlags{
		path:             dir,
		outputFormat:     outputJSON,
		searchCapFiles:   25,
		searchCapMatches: 15,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed || report.FrameFiles != 3 || report.SkippedFiles != 1 {
		t.Fatalf("unexpected baseline status: %+v", report)
	}
	if report.Totals.ProductPositiveFiles == 0 || report.Totals.ProductReducerTokensSaved == 0 {
		t.Fatalf("product read-delta savings missing: %+v", report.Totals)
	}
	if report.Totals.SearchCapPositiveExtraFiles == 0 || report.Totals.SearchCapExtraTokens == 0 {
		t.Fatalf("root search-cap proof savings missing: %+v", report.Totals)
	}
	if report.Totals.SearchDeltaGuardedFiles != 1 {
		t.Fatalf("delta search guard gap not counted: %+v", report.Totals)
	}
	if !strings.Contains(strings.Join(report.Findings, "\n"), "search_delta_guarded_files=1") {
		t.Fatalf("findings did not surface delta guard gap: %+v", report.Findings)
	}
}

func TestRunWSSSavingsBaselineJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "repeat-read.frames.jsonl")
	writeProofRepeatReadFrames(t, framesPath, "baseline-json")

	var stdout, stderr bytes.Buffer
	code := runWSSSavingsBaseline([]string{dir, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var report wssSavingsBaselineReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode json output: %v\nstdout=%s", err, stdout.String())
	}
	if report.FrameFiles != 1 || !report.GatePassed || report.Totals.ProductReducerTokensSaved <= 0 {
		t.Fatalf("unexpected json report: %+v", report)
	}
}

func TestRunWSSSavingsBaselineSuppressesReplayWarningLogs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "upstream-error.frames.jsonl")
	writeJSONLFile(t, framesPath,
		wssABReplayTestRecord("client_to_server", map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": "baseline-upstream-error",
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "continue",
			}},
			"stream": true,
		}),
		wssABReplayTestRecord("server_to_client", map[string]any{
			"type":   "error",
			"status": 400,
			"error": map[string]any{
				"type":    "invalid_request_error",
				"message": "Invalid request",
			},
		}),
	)

	var stdout, stderr bytes.Buffer
	code := runWSSSavingsBaseline([]string{framesPath, "--json"}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("run code=%d want 3 stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if stderr.String() != "" {
		t.Fatalf("baseline should keep replay warning logs out of stderr, got %q", stderr.String())
	}
	var report wssSavingsBaselineReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode json output: %v\nstdout=%s", err, stdout.String())
	}
	if report.GatePassed || report.Totals.ProductSafetyIssueFiles != 1 ||
		!strings.Contains(strings.Join(report.GateFailures, "\n"), "upstream_error_frames=1") {
		t.Fatalf("expected upstream error to stay in JSON gate output: %+v", report)
	}
}

func TestWSSSavingsBaselineIncludesUnsafeDeltaLabWhenRequested(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "delta-search.frames.jsonl")
	writeBaselineDeltaSearchFrames(t, framesPath, "baseline-unsafe-delta")

	report, err := loadWSSSavingsBaselineReport(wssSavingsBaselineFlags{
		path:                  framesPath,
		outputFormat:          outputJSON,
		searchCapFiles:        25,
		searchCapMatches:      15,
		includeUnsafeDeltaLab: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rows) != 1 || report.Rows[0].UnsafeDeltaLab == nil {
		t.Fatalf("unsafe delta lab summary missing: %+v", report)
	}
	if report.Rows[0].UnsafeDeltaLabExtraTokens <= 0 {
		t.Fatalf("unsafe delta lab should expose diagnostic savings potential: %+v", report.Rows[0])
	}
}

func writeBaselineDeltaSearchFrames(t *testing.T, path, session string) {
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
			"resp-"+session,
			wssABReplaySearchOutputFixture("needle", 96),
		)),
	)
}

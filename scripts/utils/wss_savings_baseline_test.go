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
	t354Path := filepath.Join(dir, "t354-clean.frames.jsonl")
	invalidPath := filepath.Join(dir, "matrix.jsonl")

	writeProofRepeatReadFrames(t, readPath, "baseline-read")
	writeProofSearchFrames(t, rootSearchPath, "baseline-root-search")
	writeBaselineDeltaSearchFrames(t, deltaSearchPath, "baseline-delta-search")
	writeJSONLFile(t, t354Path,
		wssT354TestFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-before", "call_mutated", 180), false),
		wssT354TestFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-before", "call_mutated", 40), true),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-mutated"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)
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
	if !report.GatePassed || report.FrameFiles != 4 || report.SkippedFiles != 1 {
		t.Fatalf("unexpected baseline status: %+v", report)
	}
	if report.Totals.ProductPositiveFiles == 0 || report.Totals.ProductReducerTokensSaved == 0 {
		t.Fatalf("product read-delta savings missing: %+v", report.Totals)
	}
	if report.Totals.ProductPositiveFiles < 2 || report.Totals.ProductReducerTokensSaved == 0 {
		t.Fatalf("full-history product search savings missing: %+v", report.Totals)
	}
	if report.Totals.SearchDeltaGuardedFiles != 0 {
		t.Fatalf("proofed delta search should not remain guarded after downstream-state unlock: %+v", report.Totals)
	}
	if report.Totals.SearchCapPositiveExtraFiles == 0 || report.Totals.SearchCapExtraTokens <= 0 {
		t.Fatalf("search-cap replay savings missing after delta unlock: %+v", report.Totals)
	}
	if !strings.Contains(strings.Join(report.Findings, "\n"), "search_cap_extra_tokens=") {
		t.Fatalf("findings should surface search-cap savings: %+v", report.Findings)
	}
	if report.Totals.T354CandidateFiles != 1 ||
		report.Totals.T354MutatedCandidates != 1 ||
		report.Totals.T354DeltaCandidates != 1 ||
		report.Totals.T354CandidatesPassing != 1 ||
		report.Totals.T354UnsafeCandidates != 0 {
		t.Fatalf("T354 downstream proof counters missing: %+v", report.Totals)
	}
	if report.Totals.T354CapturedLocalSavedTokens <= 0 ||
		report.Totals.T354RetryOrResendExtraTokens != 0 ||
		report.Totals.T354NetCapturedLocalSavedTokens <= 0 ||
		report.Totals.T354ProviderInputTokens != 2000 ||
		report.Totals.T354ProviderCachedTokens != 600 ||
		report.Totals.T354ProviderOutputTokens != 24 {
		t.Fatalf("T354 economics counters missing: %+v", report.Totals)
	}
	if !strings.Contains(strings.Join(report.Findings, "\n"), "t354_candidates_passing=1") {
		t.Fatalf("findings did not surface T354 passing candidate: %+v", report.Findings)
	}
	if !strings.Contains(strings.Join(report.Findings, "\n"), "t354_captured_local_saved_tokens_estimate=") ||
		!strings.Contains(strings.Join(report.Findings, "\n"), "t354_provider_cached_tokens=600") {
		t.Fatalf("findings did not surface T354 economics: %+v", report.Findings)
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

func TestWSSSavingsBaselineCountsFinalOpenT354CandidateAsUnproven(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-shadow-final-open.frames.jsonl")
	writeSearchCapProofCapturedShadowFrames(t, path, "baseline-t354-shadow", 96)

	report, err := loadWSSSavingsBaselineReport(wssSavingsBaselineFlags{
		path:             path,
		outputFormat:     outputJSON,
		searchCapFiles:   25,
		searchCapMatches: 15,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed ||
		report.Totals.T354MutatedCandidates != 3 ||
		report.Totals.T354CandidatesPassing != 2 ||
		report.Totals.T354MissingFollowingTurnCandidates != 1 ||
		report.Totals.T354UnsafeCandidates != 0 ||
		report.Totals.T354UnprovenCandidates != 1 {
		t.Fatalf("baseline must mirror T354 safety vs unproven classification: %+v", report.Totals)
	}
	if !strings.Contains(strings.Join(report.Findings, "\n"), "t354_unproven_candidates=1") {
		t.Fatalf("baseline findings must surface unproven T354 candidates: %+v", report.Findings)
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

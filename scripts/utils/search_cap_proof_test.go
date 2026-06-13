package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSearchCapProofSelectsReplaySafeCandidate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	writeSearchCapProofFullHistoryFrames(t, path, "search-cap-proof", 96)

	var stdout, stderr bytes.Buffer
	code := runSearchCapProof([]string{
		"--frames", path,
		"--candidate", "8:6",
		"--candidate", "4:4",
		"--min-candidate-retained-pct", "40",
		"--json",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runSearchCapProof code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var report searchCapProofReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json output did not parse: %v\n%s", err, stdout.String())
	}
	if !report.GatePassed || report.SelectedCandidate == nil || report.SelectedCandidate.Name != "candidate_4x4" {
		t.Fatalf("expected 4x4 selected proof candidate: %+v", report)
	}
	if report.DefaultReplay.ReducerTokensSaved <= 0 || report.SelectedCandidate.ExtraReducerTokens <= 0 {
		t.Fatalf("expected positive default and extra replay savings: %+v", report)
	}
	if len(report.Candidates) != 2 || !report.Candidates[0].GatePassed || !report.Candidates[1].GatePassed {
		t.Fatalf("unexpected candidate gates: %+v", report.Candidates)
	}
	if report.Candidates[1].ExtraReducerTokens <= report.Candidates[0].ExtraReducerTokens {
		t.Fatalf("retention-floor 4x4 candidate should beat 8x6: %+v", report.Candidates)
	}
	if strings.Contains(stdout.String(), "needle match") {
		t.Fatalf("proof report must stay content-free, got raw match text:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = runSearchCapProof([]string{
		"--frames=" + path,
		"--candidate=4:4",
		"--min-candidate-retained-pct=40",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runSearchCapProof text code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "selected candidate: candidate_4x4") ||
		strings.Contains(stdout.String(), "needle match") {
		t.Fatalf("unexpected text report:\n%s", stdout.String())
	}
}

func TestRunSearchCapProofFailsWithoutPassingCandidate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	writeSearchCapProofFullHistoryFrames(t, path, "search-cap-proof-fail", 96)

	var stdout, stderr bytes.Buffer
	code := runSearchCapProof([]string{
		"--frames", path,
		"--candidate", "4:4",
		"--min-candidate-retained-pct", "100",
		"--json",
	}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("runSearchCapProof code=%d want 3 stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var report searchCapProofReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json output did not parse: %v\n%s", err, stdout.String())
	}
	if report.GatePassed || report.SelectedCandidate != nil ||
		!strings.Contains(strings.Join(report.GateFailures, "\n"), "no candidate passed") {
		t.Fatalf("expected no-candidate proof failure: %+v", report)
	}
}

func TestRunSearchCapProofRejectsWeakCaptureAndWeakSavings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	writeSearchCapProofFullHistoryFrames(t, path, "search-cap-proof-weak", 96)

	var stdout, stderr bytes.Buffer
	code := runSearchCapProof([]string{
		"--frames", path,
		"--candidate", "8:6",
		"--min-search-outputs", "2",
		"--json",
	}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("runSearchCapProof weak-capture code=%d want 3 stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var report searchCapProofReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json output did not parse: %v\n%s", err, stdout.String())
	}
	if report.GatePassed || report.SelectedCandidate != nil ||
		!strings.Contains(strings.Join(report.GateFailures, "\n"), "search outputs 1 < min 2") {
		t.Fatalf("expected min-search-outputs gate failure: %+v", report)
	}

	stdout.Reset()
	stderr.Reset()
	code = runSearchCapProof([]string{
		"--frames", path,
		"--candidate", "8:6",
		"--min-extra-reducer-tokens", "999999",
		"--json",
	}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("runSearchCapProof weak-savings code=%d want 3 stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	report = searchCapProofReport{}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json output did not parse: %v\n%s", err, stdout.String())
	}
	if report.GatePassed || report.SelectedCandidate != nil ||
		!strings.Contains(strings.Join(report.Candidates[0].GateFailures, "\n"), "expected at least +999999") {
		t.Fatalf("expected min-extra-reducer-tokens gate failure: %+v", report)
	}
}

func writeSearchCapProofFullHistoryFrames(t *testing.T, path, session string, lines int) {
	t.Helper()
	callID := session + "-search-1"
	writeJSONLFile(t, path, wssABReplayTestRecord("client_to_server", map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": session + "-response",
		"prompt_cache_key":     session,
		"input": []map[string]any{
			{
				"type":      "function_call",
				"call_id":   callID,
				"name":      "exec_command",
				"arguments": map[string]any{"cmd": "cd /repo/search && rg -n needle src"},
			},
			{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  wssABReplaySearchOutputFixture("needle", lines),
			},
		},
		"stream": true,
	}))
}

func TestRunSearchCapProofRejectsMissingCandidate(t *testing.T) {
	var stdout, stderr bytes.Buffer
	for _, args := range [][]string{
		{"--frames", "frames.jsonl"},
		{"--frames", "frames.jsonl", "--candidate", "8:6", "--min-candidate-retained-pct", "-0.1"},
		{"--frames", "frames.jsonl", "--candidate", "8:6", "--min-search-outputs", "-1"},
		{"--frames", "frames.jsonl", "--candidate", "8:6", "--min-extra-reducer-tokens", "-1"},
	} {
		stdout.Reset()
		stderr.Reset()
		code := runSearchCapProof(args, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("runSearchCapProof args=%v code=%d want 2 stdout=%s stderr=%s", args, code, stdout.String(), stderr.String())
		}
	}
}

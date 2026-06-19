package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestWSST354ShapeProofPassesCleanMutatedDeltaAndFollowingTurn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-clean.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestFrame("client_to_server", wssT354TestToolOutputRequest("resp-before", "call_mutated"), true),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-mutated"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed || report.Totals.CandidatesPassing != 1 ||
		report.Totals.MutatedDeltaCandidates != 1 ||
		report.Totals.UpstreamErrorFrames != 0 ||
		report.Totals.Lost != 0 {
		t.Fatalf("clean mutated delta should pass exact T354 shape proof: %+v", report)
	}
	if len(report.Rows) != 1 || len(report.Rows[0].Candidates) != 1 ||
		!report.Rows[0].Candidates[0].FollowingTurnClean {
		t.Fatalf("candidate/following proof missing: %+v", report.Rows)
	}
}

func TestWSST354ShapeProofBlocksMissingFollowingTurn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-missing-follow.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestFrame("client_to_server", wssT354TestToolOutputRequest("resp-before", "call_mutated"), true),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-mutated"), false),
	)

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if report.GatePassed ||
		report.Totals.MissingFollowingTurnCandidates != 1 ||
		!strings.Contains(strings.Join(report.GateFailures, "\n"), "missing_following_turn") {
		t.Fatalf("missing following turn must block T354 unlock proof: %+v", report)
	}
}

func TestWSST354ShapeProofBlocksInvalidRequest400(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-400.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestFrame("client_to_server", wssT354TestToolOutputRequest("resp-before", "call_mutated"), true),
		wssT354TestFrame("server_to_client", map[string]any{
			"type":   "error",
			"status": 400,
			"error": map[string]any{
				"type":    "invalid_request_error",
				"message": "Invalid request",
			},
		}, false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)

	var stdout, stderr bytes.Buffer
	code := runWSST354ShapeProof([]string{path, "--json"}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("run code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var report wssT354ShapeProofReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v\nstdout=%s", err, stdout.String())
	}
	if report.GatePassed ||
		report.Totals.HTTP400Errors != 1 ||
		report.Totals.InvalidRequestErrors != 1 ||
		!strings.Contains(strings.Join(report.GateFailures, "\n"), "invalid_request=1") {
		t.Fatalf("invalid_request 400 must block T354 unlock proof: %+v", report)
	}
}

func wssT354TestFrame(direction string, payload any, mutated bool) map[string]any {
	rec := wssABReplayTestRecord(direction, payload)
	if mutated {
		rec["mutated"] = true
	}
	return rec
}

func wssT354TestToolOutputRequest(previousResponseID, callID string) map[string]any {
	return map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": previousResponseID,
		"prompt_cache_key":     "t354-shape-proof-test",
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": callID,
			"output":  strings.Repeat("stable proof output line\n", 80),
		}},
		"stream": true,
	}
}

func wssT354TestUserDeltaRequest(previousResponseID string) map[string]any {
	return map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": previousResponseID,
		"prompt_cache_key":     "t354-shape-proof-test",
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": "continue",
		}},
		"stream": true,
	}
}

func wssT354TestCompleted(responseID string) map[string]any {
	return map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id": responseID,
			"usage": map[string]any{
				"input_tokens":  1000,
				"output_tokens": 12,
			},
		},
	}
}

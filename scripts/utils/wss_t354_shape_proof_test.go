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
		wssT354TestFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-before", "call_mutated", 180), false),
		wssT354TestFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-before", "call_mutated", 40), true),
		wssT354TestFrame("server_to_client", wssT354TestOutputItemDone("item-mutated", "call_mutated"), false),
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
	if report.Totals.CapturedLocalSavedTokens <= 0 ||
		report.Totals.RetryOrResendExtraTokens != 0 ||
		report.Totals.NetCapturedLocalSavedTokens <= 0 {
		t.Fatalf("candidate economics missing: %+v", report.Totals)
	}
	if report.Totals.ProviderUsage.InputTokens != 2000 ||
		report.Totals.ProviderUsage.CachedTokens != 600 ||
		report.Totals.ProviderUsage.OutputTokens != 24 {
		t.Fatalf("provider usage must stay separate from local savings: %+v", report.Totals.ProviderUsage)
	}
	if report.Totals.MetadataComparisons != 1 ||
		report.Totals.MetadataMismatches != 0 ||
		report.Totals.CandidatesWithServerOutputID != 1 {
		t.Fatalf("metadata consistency and server output-id proof missing: %+v", report.Totals)
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
		report.Totals.UnprovenCandidates != 1 ||
		!strings.Contains(strings.Join(report.GateFailures, "\n"), "missing_following_turn") {
		t.Fatalf("missing following turn must block T354 unlock proof: %+v", report)
	}
}

func TestWSST354ShapeProofAcceptsFinalOpenCandidateWhenDownstreamProven(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-shadow-final-open.frames.jsonl")
	writeSearchCapProofCapturedShadowFrames(t, path, "t354-shape-shadow", 96)

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed ||
		report.Totals.MutatedToolOutputCandidates != 3 ||
		report.Totals.CandidatesPassing != 2 ||
		report.Totals.MissingFollowingTurnCandidates != 1 ||
		report.Totals.UnsafeCandidates != 0 ||
		report.Totals.UnprovenCandidates != 1 {
		t.Fatalf("final open candidate must be unproven, not unsafe, after clean downstream proof: %+v", report)
	}
	if strings.Contains(strings.Join(report.GateFailures, "\n"), "missing_following_turn") {
		t.Fatalf("final open candidate must not block a proven downstream row: %+v", report)
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

func TestWSST354ShapeProofDoesNotPairDifferentSequences(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-sequence-mismatch.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestSequencedFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-before", "call_mutated", 180), false, 11),
		wssT354TestSequencedFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-before", "call_mutated", 40), true, 12),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-mutated"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed || report.Totals.CandidatesPassing != 1 {
		t.Fatalf("sequence mismatch fixture should still prove clean downstream shape: %+v", report)
	}
	if report.Totals.CapturedLocalSavedTokens != 0 || report.Totals.NetCapturedLocalSavedTokens != 0 {
		t.Fatalf("different sequences must not create captured local savings: %+v", report.Totals)
	}
}

func TestWSST354ShapeProofBlocksReferenceMetadataMismatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-metadata-mismatch.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestSequencedFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-before", "call_original", 180), false, 31),
		wssT354TestSequencedFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-before", "call_changed", 40), true, 31),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-mutated"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if report.GatePassed ||
		report.Totals.MetadataComparisons != 1 ||
		report.Totals.MetadataMismatches != 1 ||
		report.Totals.CandidatesPassing != 0 ||
		!strings.Contains(strings.Join(report.GateFailures, "\n"), "metadata_reference_mismatch") {
		t.Fatalf("reference metadata mismatch must block T354 unlock proof: %+v", report)
	}
}

func TestWSST354ShapeProofBlocksReferenceMetadataMismatchEvenWithCleanCandidate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-mixed-metadata-mismatch.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestSequencedFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-a", "call_clean", 180), false, 61),
		wssT354TestSequencedFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-a", "call_clean", 40), true, 61),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-clean"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-clean"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-clean-follow"), false),
		wssT354TestSequencedFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-clean-follow", "call_original", 180), false, 62),
		wssT354TestSequencedFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-clean-follow", "call_changed", 40), true, 62),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-mismatch"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-mismatch"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-mismatch-follow"), false),
	)

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if report.GatePassed ||
		report.Totals.CandidatesPassing != 1 ||
		report.Totals.UnsafeCandidates != 1 ||
		report.Totals.MetadataMismatches != 1 ||
		!strings.Contains(strings.Join(report.GateFailures, "\n"), "metadata_reference_mismatch") {
		t.Fatalf("metadata mismatch must remain a hard safety failure even with another clean candidate: %+v", report)
	}
}

func TestWSST354ShapeProofSkipsMetadataMismatchForShapeChangedRebuild(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-shape-changed-rebuild.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestSequencedFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-before", "call_delta", 180), false, 41),
		wssT354TestSequencedFrame("client_to_server", wssT354TestFullHistoryToolOutputRequestLines("call_delta", 40), true, 41),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-mutated"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed ||
		report.Totals.MetadataComparisons != 0 ||
		report.Totals.MetadataMismatches != 0 ||
		report.Totals.CandidatesPassing != 1 {
		t.Fatalf("shape-changing stateless rebuild must not be blocked as metadata mismatch: %+v", report)
	}
}

func TestWSST354ShapeProofIgnoresStructuredToolOutputContentMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-structured-output-content.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestSequencedFrame("client_to_server", wssT354TestObjectOutputRequest("resp-before", "call_object", "content-original"), false, 51),
		wssT354TestSequencedFrame("client_to_server", wssT354TestObjectOutputRequest("resp-before", "call_object", "content-mutated"), true, 51),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-mutated"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed ||
		report.Totals.MetadataComparisons != 1 ||
		report.Totals.MetadataMismatches != 0 ||
		report.Totals.CandidatesPassing != 1 {
		t.Fatalf("structured tool output content must not be treated as hidden metadata: %+v", report)
	}
}

func wssT354TestFrame(direction string, payload any, mutated bool) map[string]any {
	rec := wssABReplayTestRecord(direction, payload)
	if mutated {
		rec["mutated"] = true
	}
	return rec
}

func wssT354TestSequencedFrame(direction string, payload any, mutated bool, sequence int64) map[string]any {
	rec := wssT354TestFrame(direction, payload, mutated)
	rec["sequence"] = sequence
	return rec
}

func wssT354TestToolOutputRequest(previousResponseID, callID string) map[string]any {
	return wssT354TestToolOutputRequestLines(previousResponseID, callID, 80)
}

func wssT354TestToolOutputRequestLines(previousResponseID, callID string, lines int) map[string]any {
	return map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": previousResponseID,
		"prompt_cache_key":     "t354-shape-proof-test",
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": callID,
			"output":  strings.Repeat("stable proof output line\n", lines),
		}},
		"stream": true,
	}
}

func wssT354TestFullHistoryToolOutputRequestLines(callID string, lines int) map[string]any {
	return map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "t354-shape-proof-test",
		"input": []map[string]any{
			{"type": "message", "role": "assistant", "content": "history"},
			{"type": "function_call", "call_id": callID, "name": "exec_command", "arguments": map[string]any{"cmd": "rg -n needle internal"}},
			{"type": "function_call_output", "call_id": callID, "output": strings.Repeat("stable proof output line\n", lines)},
		},
		"stream": true,
	}
}

func wssT354TestObjectOutputRequest(previousResponseID, callID, contentID string) map[string]any {
	return map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": previousResponseID,
		"prompt_cache_key":     "t354-shape-proof-test",
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": callID,
			"output": map[string]any{
				"id":     contentID,
				"type":   "content",
				"status": "ok",
			},
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
				"input_tokens": 1000,
				"input_tokens_details": map[string]any{
					"cached_tokens": 300,
				},
				"output_tokens": 12,
			},
		},
	}
}

func wssT354TestOutputItemDone(itemID, callID string) map[string]any {
	return map[string]any{
		"type":    "response.output_item.done",
		"item_id": itemID,
		"item": map[string]any{
			"type":    "function_call",
			"id":      itemID,
			"call_id": callID,
		},
	}
}

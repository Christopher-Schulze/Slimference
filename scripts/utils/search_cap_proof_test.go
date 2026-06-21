package main

import (
	"bytes"
	"encoding/json"
	"fmt"
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
		"--min-search-outputs", "1",
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
	if !report.DownstreamStateProof.GatePassed ||
		report.DownstreamStateProof.MutatedSearchOutputCandidates != 1 ||
		report.DownstreamStateProof.CandidatesPassing != 1 {
		t.Fatalf("expected live mutated search-output downstream proof: %+v", report.DownstreamStateProof)
	}
	if report.ProductReplay.SearchCapProofLatch ||
		!report.ProductReplay.GatePassed ||
		report.DefaultReplay.ReducerTokensSaved <= 0 ||
		report.SelectedCandidate.ExtraReducerTokens <= 0 ||
		report.SelectedCandidate.ProductExtraReducerTokens <= 0 {
		t.Fatalf("expected product baseline plus positive default and candidate replay savings: %+v", report)
	}
	if len(report.Candidates) != 2 || !report.Candidates[0].GatePassed || !report.Candidates[1].GatePassed {
		t.Fatalf("unexpected candidate gates: %+v", report.Candidates)
	}
	for _, candidate := range report.Candidates {
		if !candidate.GatePassed {
			continue
		}
		if candidate.Replay == nil ||
			!candidate.Replay.SearchCapProofLatch ||
			candidate.Replay.ToolOutputMutation ||
			candidate.Replay.DeltaToolOutputMutation ||
			candidate.Replay.SearchMutatedRequests == 0 {
			t.Fatalf("passing candidate must prove product search-cap latch only: %+v", candidate)
		}
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
		"--min-search-outputs=1",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runSearchCapProof text code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "selected candidate: candidate_4x4") ||
		strings.Contains(stdout.String(), "needle match") {
		t.Fatalf("unexpected text report:\n%s", stdout.String())
	}
}

func TestRunSearchCapProofRejectsReplayOnlyWithoutLiveDownstream(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	writeSearchCapProofReplayOnlyFullHistoryFrames(t, path, "search-cap-proof-replay-only", 96)

	var stdout, stderr bytes.Buffer
	code := runSearchCapProof([]string{
		"--frames", path,
		"--candidate", "4:4",
		"--min-candidate-retained-pct", "40",
		"--min-search-outputs", "1",
		"--json",
	}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("runSearchCapProof replay-only code=%d want 3 stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var report searchCapProofReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json output did not parse: %v\n%s", err, stdout.String())
	}
	if report.GatePassed || report.DownstreamStateProof.GatePassed ||
		!strings.Contains(strings.Join(report.GateFailures, "\n"), "candidate_0:current_turn_not_terminal") {
		t.Fatalf("replay-only search-cap proof must fail closed on downstream-state proof: %+v", report)
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
		!strings.Contains(strings.Join(report.GateFailures, "\n"), "no candidate or default retention-floor latch passed") {
		t.Fatalf("expected no-candidate proof failure: %+v", report)
	}
}

func TestRunSearchCapProofPromotesDefaultRetentionFloor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	writeSearchCapProofFullHistoryFrames(t, path, "search-cap-proof-default-floor", 100)

	var stdout, stderr bytes.Buffer
	code := runSearchCapProof([]string{
		"--frames", path,
		"--candidate", "4:4",
		"--candidate", "8:6",
		"--min-candidate-retained-pct", "50",
		"--min-search-outputs", "1",
		"--min-extra-reducer-tokens", "1",
		"--json",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runSearchCapProof default floor code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var report searchCapProofReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json output did not parse: %v\n%s", err, stdout.String())
	}
	if !report.GatePassed || report.SelectedCandidate == nil || report.SelectedCandidate.Name != "candidate_4x4" {
		t.Fatalf("expected strongest passing full-history candidate selection: %+v", report)
	}
	if report.ProductReplay.SearchCapProofLatch ||
		report.ProductReplay.SearchMutatedRequests == 0 ||
		!report.DefaultReplay.SearchCapProofLatch ||
		report.DefaultReplay.SearchMutatedRequests == 0 ||
		!report.DownstreamStateProof.GatePassed ||
		report.DownstreamStateProof.CandidatesPassing != 1 ||
		report.SelectedCandidate.ExtraReducerTokens <= 0 ||
		report.SelectedCandidate.ProductExtraReducerTokens != report.SelectedCandidate.ExtraReducerTokens ||
		report.SelectedCandidate.MinRetainedPct != 50 ||
		report.SelectedCandidate.MatchRetentionPct < 50 {
		t.Fatalf("full-history candidate did not prove product-safe positive savings: %+v", report)
	}
	for _, candidate := range report.Candidates {
		if !candidate.GatePassed {
			t.Fatalf("full-history candidates should pass when they save beyond product baseline: %+v", report.Candidates)
		}
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
		"--min-search-outputs", "1",
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
	original := map[string]any{
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
	}
	writeJSONLFile(t, path,
		wssT354TestFrame("client_to_server", original, false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted(session+"-response-2"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest(session+"-response-2"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted(session+"-following-response"), false),
	)
}

func writeSearchCapProofReplayOnlyFullHistoryFrames(t *testing.T, path, session string, lines int) {
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

func TestRunSearchCapProofDefaultsUseReleaseCandidateSetAndFloors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	writeSearchCapProofDistributedSearchFrames(t, path, "search-cap-proof-defaults", 2, 50, 2)

	var stdout, stderr bytes.Buffer
	code := runSearchCapProof([]string{
		"--frames", path,
		"--json",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runSearchCapProof default candidates code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var report searchCapProofReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json output did not parse: %v\n%s", err, stdout.String())
	}
	if !report.GatePassed || report.SelectedCandidate == nil {
		t.Fatalf("expected release-default proof candidate: %+v", report)
	}
	if !report.DownstreamStateProof.GatePassed ||
		report.DownstreamStateProof.MutatedSearchOutputCandidates != 1 ||
		report.DownstreamStateProof.CandidatesPassing != 1 {
		t.Fatalf("release-default fixture must include live downstream proof: %+v", report.DownstreamStateProof)
	}
	if report.MinCandidateRetainedPct != searchCapReleaseMinRetainedPct ||
		report.MinSearchOutputs != searchCapReleaseMinSearchOutputs ||
		report.MinExtraReducerTokens != searchCapReleaseMinExtraReducerTokens {
		t.Fatalf("release floors not reflected in report: %+v", report)
	}
	wantNames := []string{"candidate_30x15", "candidate_25x15", "candidate_20x10"}
	if len(report.Candidates) != len(wantNames) {
		t.Fatalf("candidate count=%d want %d: %+v", len(report.Candidates), len(wantNames), report.Candidates)
	}
	passing := 0
	for i, name := range wantNames {
		candidate := report.Candidates[i]
		if candidate.Name != name || candidate.MinRetainedPct != searchCapReleaseMinRetainedPct {
			t.Fatalf("candidate[%d]=%+v want name=%s min=%.2f", i, candidate, name, searchCapReleaseMinRetainedPct)
		}
		if !candidate.GatePassed {
			continue
		}
		passing++
		if candidate.Replay == nil ||
			!candidate.Replay.SearchCapProofLatch ||
			candidate.Replay.ToolOutputMutation ||
			candidate.Replay.DeltaToolOutputMutation ||
			candidate.ExtraReducerTokens <= 0 {
			t.Fatalf("passing release-default candidate must prove safe positive search-cap latch: %+v", candidate)
		}
	}
	if passing == 0 {
		t.Fatalf("expected at least one positive release-default candidate: %+v", report.Candidates)
	}
	if report.SelectedCandidate.Name != "candidate_20x10" {
		t.Fatalf("expected most saving release candidate selected, got %+v", report.SelectedCandidate)
	}
	if strings.Contains(stdout.String(), "needle match") {
		t.Fatalf("proof report must stay content-free, got raw match text:\n%s", stdout.String())
	}
}

func TestRunSearchCapProofIgnoresCapturedOriginalShadowsForCurrentProof(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	writeSearchCapProofCapturedShadowFrames(t, path, "search-cap-proof-shadow", 96)

	proof, err := loadSearchCapDownstreamStateProof(path, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if proof.GatePassed ||
		proof.MutatedSearchOutputCandidates != 0 ||
		!strings.Contains(strings.Join(proof.GateFailures, "\n"), "no live mutated search-output downstream candidate observed") {
		t.Fatalf("captured mutation shadows must not count as current search-cap proof: %+v", proof)
	}
}

func TestRunSearchCapProofAllowsCurrentProofSubsetOfReplayMutations(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	writeSearchCapProofFullHistoryFrames(t, path, "search-cap-proof-subset", 96)

	proof, err := loadSearchCapDownstreamStateProof(path, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !proof.GatePassed ||
		proof.MutatedSearchOutputCandidates != 1 ||
		proof.CandidatesPassing != 1 ||
		strings.Contains(strings.Join(proof.GateFailures, "\n"), "marker mismatch") {
		t.Fatalf("clean current downstream subset should not require marker/replay count equality: %+v", proof)
	}
}

func TestRunSearchCapProofRejectsReplayMutationsWithoutCurrentMarkers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("search-cap-proof-no-marker-response"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("search-cap-proof-no-marker-following"), false),
	)

	proof, err := loadSearchCapDownstreamStateProof(path, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if proof.GatePassed ||
		!strings.Contains(strings.Join(proof.GateFailures, "\n"), "no current search mutation markers observed for replay_mutations=1") {
		t.Fatalf("replay mutations without live current markers must fail closed: %+v", proof)
	}
}

func TestRunSearchCapProofDoesNotUseCapturedDownstreamEconomicsAsProductGate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	writeSearchCapProofNegativeDownstreamEconomicsFrames(t, path, "search-cap-proof-negative-economics", 96)

	var stdout, stderr bytes.Buffer
	code := runSearchCapProof([]string{
		"--frames", path,
		"--candidate", "4:4",
		"--min-search-outputs", "1",
		"--json",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("captured downstream economics should not block current full-history proof code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var report searchCapProofReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json output did not parse: %v\n%s", err, stdout.String())
	}
	if !report.GatePassed ||
		report.SelectedCandidate == nil ||
		report.DownstreamStateProof.NetCapturedLocalSavedTokens != 0 {
		t.Fatalf("captured downstream economics must be diagnostic-only for current full-history proof: %+v", report)
	}
}

func writeSearchCapProofNegativeDownstreamEconomicsFrames(t *testing.T, path, session string, lines int) {
	t.Helper()
	callID := session + "-search-1"
	original := map[string]any{
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
	}
	writeJSONLFile(t, path,
		wssABReplayTestRecord("server_to_client", map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{
				"type":      "function_call",
				"call_id":   callID,
				"name":      "exec_command",
				"arguments": `{"cmd":"cd /repo/search && rg -n needle src"}`,
			},
		}),
		wssT354TestFrame("client_to_server", original, false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted(session+"-response-2"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest(session+"-response-2"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted(session+"-following-response"), false),
	)
}

func writeSearchCapProofDistributedSearchFrames(t *testing.T, path, session string, outputs, files, matchesPerFile int) {
	t.Helper()
	items := searchCapProofDistributedSearchItems(session, outputs, files, matchesPerFile)
	writeJSONLFile(t, path,
		wssABReplayTestRecord("client_to_server", map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": session + "-response",
			"prompt_cache_key":     session,
			"input":                items,
			"stream":               true,
		}),
		wssT354TestFrame("server_to_client", wssT354TestCompleted(session+"-response-2"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest(session+"-response-2"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted(session+"-following-response"), false),
	)
}

func writeSearchCapProofCapturedShadowFrames(t *testing.T, path, session string, lines int) {
	t.Helper()
	writeJSONLFile(t, path,
		searchCapProofCapturedShadowRequest(session, "search-1", session+"-response", lines, false),
		searchCapProofCapturedShadowRequest(session, "search-1", session+"-response", 20, true),
		wssT354TestFrame("server_to_client", wssT354TestCompleted(session+"-mutated-response-1"), false),
		searchCapProofCapturedShadowRequest(session, "search-2", session+"-mutated-response-1", lines, false),
		searchCapProofCapturedShadowRequest(session, "search-2", session+"-mutated-response-1", 20, true),
		wssT354TestFrame("server_to_client", wssT354TestCompleted(session+"-mutated-response-2"), false),
		searchCapProofCapturedShadowRequest(session, "search-3", session+"-mutated-response-2", lines, false),
		searchCapProofCapturedShadowRequest(session, "search-3", session+"-mutated-response-2", 20, true),
		wssT354TestFrame("server_to_client", wssT354TestCompleted(session+"-mutated-response-3"), false),
	)
}

func searchCapProofCapturedShadowRequest(session, suffix, previousResponseID string, lines int, mutated bool) map[string]any {
	callID := session + "-" + suffix
	return wssT354TestFrame("client_to_server", map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": previousResponseID,
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
	}, mutated)
}

func searchCapProofDistributedSearchItems(session string, outputs, files, matchesPerFile int) []map[string]any {
	items := make([]map[string]any, 0, outputs*2)
	for outputIndex := range outputs {
		callID := fmt.Sprintf("%s-search-%d", session, outputIndex+1)
		items = append(items,
			map[string]any{
				"type":      "function_call",
				"call_id":   callID,
				"name":      "exec_command",
				"arguments": map[string]any{"cmd": fmt.Sprintf("cd /repo/search && rg -n needle src/pkg%d", outputIndex+1)},
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  searchCapProofDistributedSearchOutput("needle", files, matchesPerFile),
			},
		)
	}
	return items
}

func searchCapProofDistributedSearchOutput(needle string, files, matchesPerFile int) string {
	var out strings.Builder
	for fileIndex := range files {
		for matchIndex := range matchesPerFile {
			fmt.Fprintf(&out, "src/pkg/file_%03d.go:%d:%s match with enough surrounding deterministic context for compaction\n", fileIndex, matchIndex+10, needle)
		}
	}
	return out.String()
}

func writeSearchCapProofBroadDeltaFrames(t *testing.T, path, session string, lines int) {
	t.Helper()
	callID := session + "-search-1"
	writeJSONLFile(t, path,
		wssABReplayTestRecord("server_to_client", map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{
				"type":      "function_call",
				"call_id":   callID,
				"name":      "exec_command",
				"arguments": `{"cmd":"cd /repo/search && rg -n needle src"}`,
			},
		}),
		wssABReplayTestRecord("client_to_server", wssABReplayTestOutputBody(
			callID,
			session,
			session+"-response",
			wssABReplayBroadSearchOutputFixture("needle", lines),
		)),
		wssT354TestFrame("client_to_server", wssABReplayTestOutputBody(
			callID,
			session,
			session+"-response",
			wssABReplaySearchOutputFixture("needle", 20),
		), true),
		wssT354TestFrame("server_to_client", wssT354TestCompleted(session+"-mutated-response"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest(session+"-mutated-response"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted(session+"-following-response"), false),
	)
}

func TestRunSearchCapProofRejectsBadArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	for _, args := range [][]string{
		{"--frames", "frames.jsonl", "--candidate", "bad"},
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

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkdaySavingsStartAndFinishJSONDelta(t *testing.T) {
	baseState := writeAggregateStateFile(t, aggregateSampleAdminState)
	currentBody := strings.ReplaceAll(aggregateSampleAdminState, `"input_tokens_saved": 42000`, `"input_tokens_saved": 43000`)
	currentBody = strings.ReplaceAll(currentBody, `"tokens_saved": 42000`, `"tokens_saved": 43000`)
	currentBody = strings.ReplaceAll(currentBody, `"compressed_messages_mutated": 5`, `"compressed_messages_mutated": 6`)
	currentBody = strings.ReplaceAll(currentBody, `"frames_reencoded": 5`, `"frames_reencoded": 6`)
	currentBody = strings.Replace(currentBody, `"billable_input_tokens_saved": 42000,`, `"billable_input_tokens_saved": 42000,
    "analytics_proof_events_dropped": 1,`, 1)
	currentBody = strings.Replace(currentBody, `"recert_attempt_id": "attempt-1"`, `"recert_attempt_id": "attempt-2"`, 1)
	currentBody = strings.Replace(currentBody, `"recert_status": "passed"`, `"recert_status": "running"`, 1)
	currentBody = strings.Replace(currentBody, `"needs_recert": false`, `"needs_recert": true`, 1)
	currentBody = strings.Replace(currentBody, `"disk_write_ops": 200`, `"disk_write_ops": 260`, 1)
	currentBody = strings.Replace(currentBody, `"disk_write_ops_delta": 20`, `"disk_write_ops_delta": 60`, 1)
	currentBody = strings.Replace(currentBody, `"cpu_window_percent": 2.5`, `"cpu_window_percent": 4.5`, 1)
	currentBody = strings.Replace(currentBody, `"cpu_window_seconds": 3.5`, `"cpu_window_seconds": 5.5`, 1)
	currentBody = strings.Replace(currentBody, `"state_bytes": 4096`, `"state_bytes": 8192`, 1)
	currentState := writeAggregateStateFile(t, currentBody)
	baseline := filepath.Join(t.TempDir(), "workday-baseline.json")

	var stdout, stderr bytes.Buffer
	code := runWorkdaySavings([]string{"start", "--admin-state-file=" + baseState, "--baseline-file=" + baseline, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("start exit=%d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(baseline); err != nil {
		t.Fatalf("baseline not written: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	code = runWorkdaySavings([]string{"finish", "--admin-state-file=" + currentState, "--baseline-file=" + baseline, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("finish exit=%d stderr=%s", code, stderr.String())
	}
	var got workdaySavingsResult
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("bad finish JSON: %v\n%s", err, stdout.String())
	}
	if got.Delta.WSS.InputTokensSaved != 1000 {
		t.Fatalf("delta input tokens: got=%d want=1000", got.Delta.WSS.InputTokensSaved)
	}
	if got.Delta.WSS.CompressedMessagesMutated != 1 || got.Delta.WSS.FramesReencoded != 1 {
		t.Fatalf("delta mutation counters mismatch: %+v", got.Delta.WSS)
	}
	if got.Delta.WSS.AnalyticsProofEventsDropped != 1 {
		t.Fatalf("delta proof drops: got=%d want=1", got.Delta.WSS.AnalyticsProofEventsDropped)
	}
	if got.Delta.Aggregate.TotalTokensSaved != 1000 {
		t.Fatalf("delta aggregate tokens: got=%d want=1000", got.Delta.Aggregate.TotalTokensSaved)
	}
	if got.Current.CodexRoute.RecertAttemptID != "attempt-2" || got.Delta.CodexRoute.RecertAttemptID != "attempt-2" {
		t.Fatalf("workday report should carry current recert snapshot: current=%+v delta=%+v", got.Current.CodexRoute, got.Delta.CodexRoute)
	}
	if got.Current.HostBudget.CPUWindowPercent != 4.5 ||
		got.Current.HostBudget.CPUWindowSeconds != 5.5 ||
		got.Delta.HostBudget.CPUWindowSeconds != 5.5 ||
		got.Delta.HostBudget.DiskWriteOps != 60 ||
		got.Delta.HostBudget.DiskWriteOpsDelta != 60 ||
		got.Delta.HostBudget.StateBytes != 8192 {
		t.Fatalf("workday report should carry host budget current/delta proof: current=%+v delta=%+v", got.Current.HostBudget, got.Delta.HostBudget)
	}
	if len(got.Delta.Notes) == 0 || !strings.Contains(strings.Join(got.Delta.Notes, "\n"), "Route-ready is not a savings claim") {
		t.Fatalf("delta notes should preserve honest proof language: %+v", got.Delta.Notes)
	}
	if !strings.Contains(strings.Join(got.Delta.Notes, "\n"), "Host budget finished as ok") {
		t.Fatalf("delta notes should mention final host budget state: %+v", got.Delta.Notes)
	}
	if !strings.Contains(strings.Join(got.Delta.Notes, "\n"), "Recert attempt changed") {
		t.Fatalf("delta notes should mention recert attempt changes: %+v", got.Delta.Notes)
	}
	if !strings.Contains(strings.Join(got.Delta.Notes, "\n"), "Recert status changed") ||
		!strings.Contains(strings.Join(got.Delta.Notes, "\n"), "needs_recert changed") ||
		!strings.Contains(strings.Join(got.Delta.Notes, "\n"), "Finish snapshot still needs WSS recert repair") {
		t.Fatalf("delta notes should mention route repair state changes: %+v", got.Delta.Notes)
	}
}

func TestWorkdaySavingsTextMentionsFlush(t *testing.T) {
	statePath := writeAggregateStateFile(t, aggregateSampleAdminState)
	baseline := filepath.Join(t.TempDir(), "workday-baseline.json")
	var stdout, stderr bytes.Buffer
	code := runWorkdaySavings([]string{"start", "--admin-state-file", statePath, "--baseline-file", baseline}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "close sessions so WSS counters flush") || !strings.Contains(out, "workday-savings finish") {
		t.Fatalf("start output should guide flush-aware finish:\n%s", out)
	}
}

func TestWorkdaySavingsHelpMentionsRouteSnapshot(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runWorkdaySavings([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Codex traffic / auto-recert snapshot") {
		t.Fatalf("help should mention route snapshot:\n%s", stdout.String())
	}
}

func TestWorkdaySavingsRejectsMissingAction(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runWorkdaySavings([]string{"--admin-state-file=/tmp/state.json"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit: got=%d want=2", code)
	}
	if !strings.Contains(stderr.String(), "requires action") {
		t.Fatalf("stderr should mention action: %s", stderr.String())
	}
}

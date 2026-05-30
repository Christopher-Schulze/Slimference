package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestWSSABReplayReportReadDeltaRecoverable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")

	var file strings.Builder
	for i := 0; i < 140; i++ {
		fmt.Fprintf(&file, "A/B replay line %03d with stable repeated content.\n", i)
	}
	writeJSONLFile(t, path,
		wssABReplayTestRecord("server_to_client", map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{
				"type":      "function_call",
				"call_id":   "read-1",
				"name":      "read_file",
				"arguments": `{"path":"src/a.md"}`,
			},
		}),
		wssABReplayTestRecord("client_to_server", wssABReplayTestOutputBody("read-1", "ab-session", "resp-1", file.String())),
		wssABReplayTestRecord("server_to_client", map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{
				"type":      "function_call",
				"call_id":   "read-2",
				"name":      "read_file",
				"arguments": `{"path":"src/a.md"}`,
			},
		}),
		wssABReplayTestRecord("client_to_server", wssABReplayTestOutputBody("read-2", "ab-session", "resp-2", file.String())),
	)

	report, err := loadWSSABReplayReport(wssABReplayFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if report.Frames != 4 || report.RequestTurns != 2 || report.MutatedRequests != 1 {
		t.Fatalf("unexpected replay counts: %+v", report)
	}
	if report.Lost != 0 || report.BytesSaved <= 0 || !report.GatePassed {
		t.Fatalf("repeat read should save without lost comprehension: %+v", report)
	}
	if len(report.Elisions) != 1 || report.Elisions[0].Severity != "recoverable_prior_full" {
		t.Fatalf("repeat read should be recoverable, got %+v", report.Elisions)
	}
}

func TestRunWSSABReplayJSONAndGateFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	writeJSONLFile(t, path, wssABReplayTestRecord("client_to_server", map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "ab-note-session",
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": "continue",
		}},
		"stream": true,
	}))

	var stdout, stderr bytes.Buffer
	code := runWSSABReplay([]string{path, "--archive-recovery-note", "--fail-on-lost", "--json"}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("runWSSABReplay code=%d want 3 stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var report wssABReplayReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json output did not parse: %v\n%s", err, stdout.String())
	}
	if report.GatePassed || report.Lost == 0 || len(report.GateFailures) == 0 {
		t.Fatalf("expected gate failure from archive note extra context: %+v", report)
	}

	stdout.Reset()
	stderr.Reset()
	code = runWSSABReplay([]string{path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runWSSABReplay default code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "WSS A/B replay:") || !strings.Contains(stdout.String(), "gate:") {
		t.Fatalf("text output missing summary:\n%s", stdout.String())
	}
}

func TestRunWSSABReplayAllowsExpectedRecoveryNoteExtra(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	writeJSONLFile(t, path, wssABReplayTestRecord("client_to_server", map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "ab-note-session",
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": "continue",
		}},
		"stream": true,
	}))

	var stdout, stderr bytes.Buffer
	code := runWSSABReplay([]string{path, "--archive-recovery-note", "--allow-recovery-note-extra", "--fail-on-lost", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runWSSABReplay code=%d want 0 stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var report wssABReplayReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json output did not parse: %v\n%s", err, stdout.String())
	}
	if !report.GatePassed || report.ExpectedExtras != 1 || report.Lost != 1 {
		t.Fatalf("expected recovery-note extra to be separated from gate loss: %+v", report)
	}
}

func TestWSSABReplayReportChunkDedupProofGate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	shared := strings.Repeat("shared fixture payload for content-defined chunk replay\n", 2600)
	first := shared + "first file tail\n"
	second := shared + "second file tail\n"
	writeJSONLFile(t, path,
		wssABReplayTestRecord("server_to_client", map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{
				"type":      "function_call",
				"call_id":   "read-a",
				"name":      "read_file",
				"arguments": `{"path":"src/a.md"}`,
			},
		}),
		wssABReplayTestRecord("client_to_server", wssABReplayTestOutputBody("read-a", "ab-chunk-session", "resp-a", first)),
		wssABReplayTestRecord("server_to_client", map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{
				"type":      "function_call",
				"call_id":   "read-b",
				"name":      "read_file",
				"arguments": `{"path":"src/b.md"}`,
			},
		}),
		wssABReplayTestRecord("client_to_server", wssABReplayTestOutputBody("read-b", "ab-chunk-session", "resp-b", second)),
	)

	report, err := loadWSSABReplayReport(wssABReplayFlags{
		path:                   path,
		failOnLost:             true,
		archiveRecoveryNote:    true,
		allowRecoveryNoteExtra: true,
		codexChunkDedup:        true,
		chunkDedupMinBytes:     0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Frames != 4 || report.RequestTurns != 2 || report.MutatedRequests < 2 {
		t.Fatalf("unexpected replay counts: %+v", report)
	}
	if !report.GatePassed || report.ExpectedExtras != 1 || report.BytesSaved <= 0 {
		t.Fatalf("chunk replay should pass the proof gate with savings: %+v", report)
	}
	foundChunkReference := false
	for _, elision := range report.Elisions {
		if elision.Severity == "elided_with_reference" && strings.Contains(elision.Preview, "shared fixture") {
			foundChunkReference = true
		}
	}
	if !foundChunkReference {
		t.Fatalf("expected at least one referenced chunk elision, got %+v", report.Elisions)
	}
}

func TestWSSABReplayAutoPolicySeparatesRecoveryNoteExtra(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	shared := strings.Repeat("auto policy replay shared chunk line\n", 3000)
	writeJSONLFile(t, path,
		wssABReplayTestRecord("server_to_client", map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{
				"type":      "function_call",
				"call_id":   "read-a",
				"name":      "read_file",
				"arguments": `{"path":"src/a.md"}`,
			},
		}),
		wssABReplayTestRecord("client_to_server", wssABReplayTestOutputBody("read-a", "ab-auto-session", "resp-a", shared+"tail a\n")),
		wssABReplayTestRecord("server_to_client", map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{
				"type":      "function_call",
				"call_id":   "read-b",
				"name":      "read_file",
				"arguments": `{"path":"src/b.md"}`,
			},
		}),
		wssABReplayTestRecord("client_to_server", wssABReplayTestOutputBody("read-b", "ab-auto-session", "resp-b", shared+"tail b\n")),
	)

	report, err := loadWSSABReplayReport(wssABReplayFlags{
		path:       path,
		failOnLost: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed || report.ExpectedExtras != 1 || report.BytesSaved <= 0 {
		t.Fatalf("auto policy replay should pass while separating the recovery note: %+v", report)
	}
}

func TestParseWSSABReplayFrameLine(t *testing.T) {
	frame, err := parseWSSABReplayFrameLine([]byte(`{"dir":"c2s","payload":"{\"type\":\"request\",\"input\":[]}"}`))
	if err != nil {
		t.Fatal(err)
	}
	if frame.Direction != "c2s" || !bytes.Contains(frame.Payload, []byte(`"input":[]`)) {
		t.Fatalf("bad parsed frame: %+v payload=%s", frame, frame.Payload)
	}
	for _, line := range [][]byte{
		[]byte(`{"dir":"sideways","payload":{}}`),
		[]byte(`{"dir":"c2s","payload":[]}`),
		[]byte(`{"dir":"c2s"}`),
	} {
		if _, err := parseWSSABReplayFrameLine(line); err == nil {
			t.Fatalf("expected parse error for %s", line)
		}
	}
}

func TestParseWSSABReplayFlagsRejectsBadChunkMinBytes(t *testing.T) {
	for _, args := range [][]string{
		{"frames.jsonl", "--chunk-dedup-min-bytes"},
		{"frames.jsonl", "--chunk-dedup-min-bytes", "abc"},
		{"frames.jsonl", "--chunk-dedup-min-bytes", "-1"},
	} {
		if _, err := parseWSSABReplayFlags(args); err == nil {
			t.Fatalf("expected parse error for %v", args)
		}
	}
	flags, err := parseWSSABReplayFlags([]string{"frames.jsonl", "--codex-chunk-dedup", "--chunk-dedup-min-bytes=123"})
	if err != nil {
		t.Fatal(err)
	}
	if !flags.codexChunkDedup || !flags.archiveRecoveryNote || !flags.allowRecoveryNoteExtra || flags.chunkDedupMinBytes != 123 {
		t.Fatalf("bad parsed flags: %+v", flags)
	}
}

func wssABReplayTestRecord(direction string, payload any) map[string]any {
	return map[string]any{
		"direction": direction,
		"payload":   payload,
	}
}

func wssABReplayTestOutputBody(callID string, promptCacheKey string, previousResponseID string, output string) map[string]any {
	return map[string]any{
		"model":                "gpt-5-codex",
		"prompt_cache_key":     promptCacheKey,
		"previous_response_id": previousResponseID,
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": callID,
			"output":  output,
		}},
		"stream": true,
	}
}

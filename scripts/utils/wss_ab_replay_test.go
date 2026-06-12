package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/abharness"
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
		wssABReplayTestRecord("client_to_server", wssABReplayTestOutputBody("read-1", "ab-session", "", file.String())),
		wssABReplayTestRecord("server_to_client", map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{
				"type":      "function_call",
				"call_id":   "read-2",
				"name":      "read_file",
				"arguments": `{"path":"src/a.md"}`,
			},
		}),
		wssABReplayTestRecord("client_to_server", wssABReplayTestOutputBody("read-2", "ab-session", "", file.String())),
	)

	report, err := loadWSSABReplayReport(wssABReplayFlags{path: path, toolOutputMutation: true})
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
	if !report.ToolOutputMutation {
		t.Fatalf("proof replay must report explicit tool-output mutation: %+v", report)
	}
}

func TestWSSABReplayProductDefaultKeepsSafeReadDeltaSavings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	writeProofRepeatReadFrames(t, path, "product-default")

	report, err := loadWSSABReplayReport(wssABReplayFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if report.MutatedRequests != 1 || report.BytesSaved <= 0 || report.ReducerReadDeltaBlocks != 1 || report.ToolOutputMutation {
		t.Fatalf("product-default replay should keep safe read-delta savings without lab tool-output mutation: %+v", report)
	}
	if report.Lost != 0 || !report.GatePassed {
		t.Fatalf("product-default read-delta replay should pass comprehension gate: %+v", report)
	}
}

func TestWSSABReplayReportTracksSearchProofStats(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	writeProofSearchFrames(t, path, "search-report")

	report, err := loadWSSABReplayReport(wssABReplayFlags{path: path, toolOutputMutation: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.SearchRequestTurns != 1 || report.SearchMutatedRequests != 1 ||
		report.SearchCapturedMutated != 0 || report.SearchUpstreamErrors != 0 {
		t.Fatalf("unexpected search proof stats: %+v", report)
	}

	var stdout, stderr bytes.Buffer
	code := runWSSABReplay([]string{path, "--tool-output-mutation"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runWSSABReplay code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "search_turns:") ||
		!strings.Contains(stdout.String(), "requests=1 mutated=1") {
		t.Fatalf("text output missing search proof stats:\n%s", stdout.String())
	}
}

func TestWriteWSSABReplayTextIncludesProofDiagnostics(t *testing.T) {
	report := wssABReplayReport{
		Path:                          "frames.jsonl",
		Frames:                        3,
		RequestTurns:                  1,
		MutatedRequests:               1,
		CapturedMutatedRequests:       1,
		RequestShapes:                 replayShapeCounts{Root: 1},
		MutatedShapes:                 replayShapeCounts{Root: 1},
		CapturedMutatedShapes:         replayShapeCounts{Delta: 1},
		BytesBefore:                   1000,
		BytesAfter:                    700,
		BytesSaved:                    300,
		ReducerTokensSaved:            42,
		ReducerBlocksModified:         2,
		ReducerReadDeltaBlocks:        1,
		ReducerRepeatedBlocks:         1,
		ReducerChunkBlocks:            1,
		ReducerCapturedBlocks:         1,
		ReducerEnvelopeBlocks:         1,
		ReducerChunkRefs:              2,
		ReducerChunkRefBytes:          512,
		ReducerChunkInputBytes:        2048,
		CompoundedEstimateTokens:      9000,
		FootprintAppliedDecisions:     2,
		HighFootprintAppliedDecisions: 1,
		UniformChunkBudgetControl: &wssABReplayUniformControlReport{
			ReducerTokensSaved:        30,
			CompoundedEstimateTokens:  5000,
			DeltaReducerTokensSaved:   12,
			DeltaCompoundedEstimate:   4000,
			DeltaHighFootprintApplied: 1,
			Improved:                  true,
		},
		UpstreamErrorFrames:     1,
		UpstreamHTTP400Errors:   1,
		UpstreamInvalidRequests: 1,
		SearchRequestTurns:      1,
		SearchMutatedRequests:   1,
		SearchUpstreamErrors:    1,
		SearchHTTP400Errors:     1,
		SearchInvalidRequests:   1,
		SearchCapFiles:          25,
		SearchCapMatches:        15,
		ToolOutputMutation:      true,
		Lost:                    1,
		ExpectedExtras:          1,
		Elisions: []abharness.Elision{{
			Turn:     2,
			Block:    3,
			Severity: "recoverable_prior_full",
			Bytes:    64,
			Preview:  "proof preview",
		}},
		GateFailures: []string{"search_loop proof has no named search-output mutation"},
		Notes:        []string{"proof note"},
	}

	var out bytes.Buffer
	writeWSSABReplayText(&out, report)
	text := out.String()
	for _, want := range []string{
		"captured_mutated: 1",
		"reducer_blocks:   modified=2 read_delta=1 repeated=1 chunk=1 captured=1 envelope=1",
		"chunk_refs:       refs=2 referenced_bytes=512 input_bytes=2048",
		"compounded:      estimate=9000 footprint_decisions=2 high=1",
		"uniform_control: reducer_tokens=30 compounded=5000 delta_tokens=12 delta_compounded=4000 delta_high=1 improved=true lost=0",
		"search_turns:     requests=1 mutated=1 captured=0 upstream_errors=1 invalid_request=1 http_400=1 response_failed=0",
		"search_cap:       files=25 matches=15",
		"expected_extras:  1",
		"recoverable_prior_full",
		"search_loop proof has no named search-output mutation",
		"proof note",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text report missing %q:\n%s", want, text)
		}
	}
}

func TestWSSABReplayReportIncludesRequestShapes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	writeJSONLFile(t, path,
		wssABReplayTestRecord("client_to_server", map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": "shape-session",
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "start",
			}},
			"stream": true,
		}),
		wssABReplayTestRecord("client_to_server", wssABReplayTestOutputBody("delta-call", "shape-session", "resp-delta", "delta tool output")),
		wssABReplayTestRecord("client_to_server", wssABReplayTestFullHistoryBody("full-call", "shape-session", "resp-full", "src/shape.go", "full-history output")),
		map[string]any{
			"direction": "client_to_server",
			"mutated":   true,
			"payload":   wssABReplayTestFullHistoryBody("full-captured", "shape-session", "", "src/shape.go", "captured full-history output"),
		},
	)

	report, err := loadWSSABReplayReport(wssABReplayFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if report.RequestShapes.Root != 1 || report.RequestShapes.Delta != 1 || report.RequestShapes.FullHistory != 1 {
		t.Fatalf("unexpected request shape counts: %+v", report.RequestShapes)
	}
	if report.MutatedShapes.Root != 0 || report.MutatedShapes.Delta != 0 || report.MutatedShapes.FullHistory != 0 {
		t.Fatalf("shape-only report should not include mutations: %+v", report.MutatedShapes)
	}
	if report.CapturedMutatedRequests != 1 || report.CapturedMutatedShapes.FullHistory != 1 {
		t.Fatalf("captured mutated shape not reported: requests=%d shapes=%+v", report.CapturedMutatedRequests, report.CapturedMutatedShapes)
	}

	var stdout, stderr bytes.Buffer
	code := runWSSABReplay([]string{path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runWSSABReplay code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "request_shapes:") ||
		!strings.Contains(stdout.String(), "full_history=1") ||
		!strings.Contains(stdout.String(), "captured_shapes:") {
		t.Fatalf("text output missing request shape summary:\n%s", stdout.String())
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

func TestRunWSSABReplayFailsOnUpstreamInvalidRequest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	writeJSONLFile(t, path,
		wssABReplayTestRecord("client_to_server", map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": "upstream-error-session",
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

	report, err := loadWSSABReplayReport(wssABReplayFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed || report.UpstreamErrorFrames != 1 ||
		report.UpstreamHTTP400Errors != 1 || report.UpstreamInvalidRequests != 1 {
		t.Fatalf("default replay should report but not fail upstream errors: %+v", report)
	}

	var stdout, stderr bytes.Buffer
	code := runWSSABReplay([]string{path, "--fail-on-upstream-error", "--json"}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("runWSSABReplay code=%d want 3 stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var gated wssABReplayReport
	if err := json.Unmarshal(stdout.Bytes(), &gated); err != nil {
		t.Fatalf("json output did not parse: %v\n%s", err, stdout.String())
	}
	if gated.GatePassed || gated.UpstreamErrorFrames != 1 || gated.UpstreamInvalidRequests != 1 ||
		!strings.Contains(strings.Join(gated.GateFailures, "\n"), "upstream_error_frames=1") {
		t.Fatalf("expected upstream error gate failure: %+v", gated)
	}
}

func TestWSSABReplayReportsResponseFailedFrame(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	writeJSONLFile(t, path,
		wssABReplayTestRecord("server_to_client", map[string]any{
			"type": "response.failed",
			"response": map[string]any{
				"status": "failed",
				"error":  map[string]any{"type": "server_error"},
			},
		}),
	)

	report, err := loadWSSABReplayReport(wssABReplayFlags{path: path, failOnUpstreamError: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.GatePassed || report.UpstreamErrorFrames != 1 || report.UpstreamResponseFailures != 1 {
		t.Fatalf("response.failed should be an upstream error gate failure: %+v", report)
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
	shared := wssABReplayLinePayload("shared fixture payload for content-defined chunk replay", 2600)
	first := shared + wssABReplayLinePayload("first file tail", 1800)
	second := shared + wssABReplayLinePayload("second file tail", 1800)
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
		wssABReplayTestRecord("client_to_server", wssABReplayTestOutputBody("read-a", "ab-chunk-session", "", first)),
		wssABReplayTestRecord("server_to_client", map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{
				"type":      "function_call",
				"call_id":   "read-b",
				"name":      "read_file",
				"arguments": `{"path":"src/b.md"}`,
			},
		}),
		wssABReplayTestRecord("client_to_server", wssABReplayTestOutputBody("read-b", "ab-chunk-session", "", second)),
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
	if !report.GatePassed || report.ExpectedExtras != 1 || report.BytesSaved <= 0 ||
		report.ReducerTokensSaved <= 0 || report.ReducerChunkBlocks != 1 || report.ReducerChunkRefs == 0 {
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

func TestWSSABReplayReportUniformControlGate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	lowShared := strings.Repeat("t359 replay low budget contender line\n", 260)
	highShared := strings.Repeat("t359 replay high budget contender line with much more session footprint\n", 4000)
	writeJSONLFile(t, path,
		wssABReplayTestRecord("server_to_client", map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{"type": "function_call", "call_id": "seed-low", "name": "read_file", "arguments": `{"path":"low.seed"}`},
		}),
		wssABReplayTestRecord("server_to_client", map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{"type": "function_call", "call_id": "seed-high", "name": "read_file", "arguments": `{"path":"high.seed"}`},
		}),
		wssABReplayTestRecord("client_to_server", wssABReplayTestOutputsBody("t359-cli-compound", "", []wssABReplayTestOutput{
			{CallID: "seed-low", Output: lowShared + "seed low tail\n"},
			{CallID: "seed-high", Output: highShared + "seed high tail\n"},
		})),
		wssABReplayTestRecord("server_to_client", map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{"type": "function_call", "call_id": "low", "name": "read_file", "arguments": `{"path":"low.go"}`},
		}),
		wssABReplayTestRecord("server_to_client", map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{"type": "function_call", "call_id": "high", "name": "read_file", "arguments": `{"path":"high.go"}`},
		}),
		wssABReplayTestRecord("client_to_server", wssABReplayTestOutputsBody("t359-cli-compound", "", []wssABReplayTestOutput{
			{CallID: "low", Output: lowShared + "fresh low tail\n"},
			{CallID: "high", Output: highShared + "fresh high tail\n"},
		})),
	)

	report, err := loadWSSABReplayReport(wssABReplayFlags{
		path:                       path,
		failOnLost:                 true,
		codexChunkDedup:            true,
		chunkDedupMinBytes:         4096,
		chunkDedupMaxSessionRefPct: 5,
		uniformChunkBudgetControl:  true,
		requireCompoundImprovement: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed || report.UniformChunkBudgetControl == nil ||
		report.UniformChunkBudgetControl.DeltaHighFootprintApplied <= 0 ||
		!report.UniformChunkBudgetControl.Improved {
		t.Fatalf("uniform control gate should prove high-footprint lift: %+v", report)
	}
}

func TestWSSABReplayAutoPolicySeparatesRecoveryNoteExtra(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	shared := wssABReplayLinePayload("auto policy replay shared chunk line", 3000)
	tailA := wssABReplayLinePayload("auto policy replay fresh tail a", 2200)
	tailB := wssABReplayLinePayload("auto policy replay fresh tail b", 2200)
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
		wssABReplayTestRecord("client_to_server", wssABReplayTestOutputBody("read-a", "ab-auto-session", "", shared+tailA)),
		wssABReplayTestRecord("server_to_client", map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{
				"type":      "function_call",
				"call_id":   "read-b",
				"name":      "read_file",
				"arguments": `{"path":"src/b.md"}`,
			},
		}),
		wssABReplayTestRecord("client_to_server", wssABReplayTestOutputBody("read-b", "ab-auto-session", "", shared+tailB)),
	)

	report, err := loadWSSABReplayReport(wssABReplayFlags{
		path:               path,
		failOnLost:         true,
		toolOutputMutation: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed || !report.ToolOutputMutation || report.ExpectedExtras != 1 || report.BytesSaved <= 0 ||
		report.ReducerTokensSaved <= 0 || report.ReducerChunkBlocks != 1 {
		t.Fatalf("auto policy replay should pass while separating the recovery note: %+v", report)
	}
}

func wssABReplayLinePayload(prefix string, lines int) string {
	var b strings.Builder
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&b, "%s %04d with stable chunkable context and deterministic fixture bytes.\n", prefix, i)
	}
	return b.String()
}

func TestParseWSSABReplayFrameLine(t *testing.T) {
	frame, err := parseWSSABReplayFrameLine([]byte(`{"dir":"c2s","payload":"{\"type\":\"request\",\"input\":[]}"}`))
	if err != nil {
		t.Fatal(err)
	}
	if frame.Direction != "c2s" || !bytes.Contains(frame.Payload, []byte(`"input":[]`)) {
		t.Fatalf("bad parsed frame: %+v payload=%s", frame, frame.Payload)
	}
	mutatedFrame, err := parseWSSABReplayFrameLine([]byte(`{"dir":"c2s","mutated":true,"payload":{"type":"request","input":[]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !mutatedFrame.Mutated {
		t.Fatalf("mutated capture marker not parsed: %+v", mutatedFrame)
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
		{"frames.jsonl", "--chunk-dedup-max-session-ref-pct"},
		{"frames.jsonl", "--chunk-dedup-max-session-ref-pct", "101"},
		{"frames.jsonl", "--search-cap-files"},
		{"frames.jsonl", "--search-cap-files", "-1"},
		{"frames.jsonl", "--search-cap-matches"},
		{"frames.jsonl", "--search-cap-matches", "bad"},
	} {
		if _, err := parseWSSABReplayFlags(args); err == nil {
			t.Fatalf("expected parse error for %v", args)
		}
	}
	flags, err := parseWSSABReplayFlags([]string{"frames.jsonl", "--codex-chunk-dedup", "--chunk-dedup-min-bytes=123"})
	if err != nil {
		t.Fatal(err)
	}
	if !flags.codexChunkDedup || !flags.archiveRecoveryNote || !flags.allowRecoveryNoteExtra || !flags.toolOutputMutation || flags.chunkDedupMinBytes != 123 {
		t.Fatalf("bad parsed flags: %+v", flags)
	}
	flags, err = parseWSSABReplayFlags([]string{"frames.jsonl", "--codex-chunk-dedup", "--chunk-dedup-max-session-ref-pct=35"})
	if err != nil {
		t.Fatal(err)
	}
	if flags.chunkDedupMaxSessionRefPct != 35 {
		t.Fatalf("bad max session ref pct flag: %+v", flags)
	}
	flags, err = parseWSSABReplayFlags([]string{"frames.jsonl", "--codex-wss-tool-output-mutation"})
	if err != nil {
		t.Fatal(err)
	}
	if !flags.toolOutputMutation {
		t.Fatalf("explicit WSS tool-output mutation flag not parsed: %+v", flags)
	}
	flags, err = parseWSSABReplayFlags([]string{"frames.jsonl", "--delta-tool-output-mutation-lab"})
	if err != nil {
		t.Fatal(err)
	}
	if !flags.toolOutputMutation || !flags.deltaToolOutputMutationLab {
		t.Fatalf("delta WSS tool-output mutation lab flag not parsed: %+v", flags)
	}
	flags, err = parseWSSABReplayFlags([]string{"frames.jsonl", "--search-cap-files=25", "--search-cap-matches", "15"})
	if err != nil {
		t.Fatal(err)
	}
	if flags.searchCapFiles != 25 || flags.searchCapMatches != 15 {
		t.Fatalf("search cap flags not parsed: %+v", flags)
	}
}

func wssABReplayTestRecord(direction string, payload any) map[string]any {
	return map[string]any{
		"direction": direction,
		"payload":   payload,
	}
}

func wssABReplayTestOutputBody(callID string, promptCacheKey string, previousResponseID string, output string) map[string]any {
	return wssABReplayTestOutputsBody(promptCacheKey, previousResponseID, []wssABReplayTestOutput{{CallID: callID, Output: output}})
}

type wssABReplayTestOutput struct {
	CallID string
	Output string
}

func wssABReplayTestOutputsBody(promptCacheKey string, previousResponseID string, outputs []wssABReplayTestOutput) map[string]any {
	input := make([]map[string]any, 0, len(outputs))
	for _, output := range outputs {
		input = append(input, map[string]any{
			"type":    "function_call_output",
			"call_id": output.CallID,
			"output":  output.Output,
		})
	}
	body := map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": promptCacheKey,
		"input":            input,
		"stream":           true,
	}
	if previousResponseID != "" {
		body["previous_response_id"] = previousResponseID
	}
	return body
}

func writeProofSearchFrames(t *testing.T, path, session string) {
	t.Helper()
	writeProofSearchFramesWithCount(t, path, session, 96)
}

func writeProofSearchFramesWithCount(t *testing.T, path, session string, lines int) {
	t.Helper()
	writeJSONLFile(t, path,
		wssABReplayTestRecord("server_to_client", map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{
				"type":      "function_call",
				"call_id":   session + "-search-1",
				"name":      "exec_command",
				"arguments": `{"cmd":"rg -n needle src"}`,
			},
		}),
		wssABReplayTestRecord("client_to_server", wssABReplayTestOutputBody(
			session+"-search-1",
			session,
			"",
			wssABReplaySearchOutputFixture("needle", lines),
		)),
	)
}

func wssABReplaySearchOutputFixture(needle string, count int) string {
	var out strings.Builder
	for i := 0; i < count; i++ {
		fmt.Fprintf(&out, "src/pkg/file_%03d.go:%d:%s match with enough surrounding deterministic context for compaction\n", i%12, i+10, needle)
	}
	return out.String()
}

func wssABReplayTestFullHistoryBody(callID string, promptCacheKey string, previousResponseID string, path string, output string) map[string]any {
	return map[string]any{
		"model":                "gpt-5-codex",
		"prompt_cache_key":     promptCacheKey,
		"previous_response_id": previousResponseID,
		"input": []map[string]any{
			{"type": "function_call", "call_id": callID, "name": "read_file", "arguments": map[string]any{"path": path}},
			{"type": "function_call_output", "call_id": callID, "output": output},
		},
		"stream": true,
	}
}

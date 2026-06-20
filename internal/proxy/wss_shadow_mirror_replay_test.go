package proxy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
	"github.com/Christopher-Schulze/Slimference/internal/servermirror"
)

func TestRunWSSShadowMirrorReplayRanksCommandPayloadAndSkipsMutated(t *testing.T) {
	payload := strings.Repeat("ok  github.com/Christopher-Schulze/Slimference/internal/proxy 0.010s\n", 20)
	firstOutput := "Chunk ID: first\nProcess exited with code 0\nOutput:\n" + payload
	secondOutput := "Chunk ID: second\nProcess exited with code 0\nOutput:\n" + payload
	mutatedOutput := "Chunk ID: mutated\nProcess exited with code 0\nOutput:\n" + strings.Repeat("mutated payload must not count\n", 50)

	frames := []WSSABReplayFrame{
		{
			Direction: wsmitm.DirClientToServer,
			Payload: wssShadowReplayTestPayload(t, map[string]any{
				"model":            "gpt-5-codex",
				"prompt_cache_key": "shadow-replay-session",
				"input": []map[string]any{
					wssShadowReplayShellCall("call-1", "go test ./..."),
					wssShadowReplayShellOutput("call-1", "go test ./...", firstOutput),
				},
				"stream": true,
			}),
			SocketSeq: 7,
		},
		{
			Direction: wsmitm.DirClientToServer,
			Payload: wssShadowReplayTestPayload(t, map[string]any{
				"model":                "gpt-5-codex",
				"prompt_cache_key":     "shadow-replay-session",
				"previous_response_id": "resp-1",
				"input": []map[string]any{
					wssShadowReplayShellCall("call-2", "go test ./..."),
					wssShadowReplayShellOutput("call-2", "go test ./...", secondOutput),
				},
				"stream": true,
			}),
			SocketSeq: 7,
		},
		{
			Direction: wsmitm.DirClientToServer,
			Payload: wssShadowReplayTestPayload(t, map[string]any{
				"model":            "gpt-5-codex",
				"prompt_cache_key": "shadow-replay-session",
				"input": []map[string]any{
					wssShadowReplayShellCall("call-mutated", "go test ./..."),
					wssShadowReplayShellOutput("call-mutated", "go test ./...", mutatedOutput),
				},
				"stream": true,
			}),
			Mutated:   true,
			SocketSeq: 7,
		},
	}

	report, err := RunWSSShadowMirrorReplay(frames)
	if err != nil {
		t.Fatal(err)
	}
	if report.Frames != 3 || report.RequestTurns != 2 || report.CapturedMutatedRequests != 1 {
		t.Fatalf("unexpected counts: %+v", report)
	}
	if report.Normalized.ReferenceableBytes != len(payload) || report.Normalized.CandidateTokensEstimate <= 0 {
		t.Fatalf("normalized payload should be referenceable once: %+v", report.Normalized)
	}
	if report.Exact.ReferenceableBytes != 0 {
		t.Fatalf("volatile Codex exec envelope must not exact-match: %+v", report.Exact)
	}
	row := wssShadowReplayFindRow(report.Rows, "codex_exec_payload_command_go")
	if row == nil {
		t.Fatalf("missing go command row: %+v", report.Rows)
	}
	if row.RequestShape != "full_history" || row.ReferenceableBytes != len(payload) ||
		row.ReferenceableRequests != 1 || row.ReferenceableSegments != 1 {
		t.Fatalf("bad command row: %+v", row)
	}
	if strings.Contains(row.Kind, "mutated") || report.Normalized.Bytes >= len(payload)*2+len(mutatedOutput) {
		t.Fatalf("mutated capture appears to have been counted: row=%+v normalized=%+v", row, report.Normalized)
	}
}

func TestRunWSSShadowMirrorReplayMissingSessionAndServerFrames(t *testing.T) {
	frames := []WSSABReplayFrame{
		{
			Direction: wsmitm.DirServerToClient,
			Payload:   []byte(`{"type":"response.output_text.delta","delta":"ignored"}`),
		},
		{
			Direction: wsmitm.DirClientToServer,
			Payload: wssShadowReplayTestPayload(t, map[string]any{
				"model": "gpt-5-codex",
				"input": []map[string]any{{
					"type":    "message",
					"role":    "user",
					"content": "no stable session id here",
				}},
				"stream": true,
			}),
		},
	}

	report, err := RunWSSShadowMirrorReplay(frames)
	if err != nil {
		t.Fatal(err)
	}
	if report.Frames != 2 || report.RequestTurns != 1 || report.MissingSessionID != 1 {
		t.Fatalf("unexpected missing-session report: %+v", report)
	}
	if report.Normalized.Bytes != 0 || len(report.Rows) != 0 {
		t.Fatalf("missing session must not observe/predict mirror state: %+v", report)
	}
}

func TestRunWSSShadowMirrorReplaySkipsNonRequestClientFrames(t *testing.T) {
	report, err := RunWSSShadowMirrorReplay([]WSSABReplayFrame{
		{
			Direction: wsmitm.DirClientToServer,
			Payload:   []byte(`{"type":"ping","body":null}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Frames != 1 || report.RequestTurns != 0 || len(report.Rows) != 0 || len(report.StatefulSafeRows) != 0 {
		t.Fatalf("non-request client frame should only count as a frame: %+v", report)
	}
}

func TestRunWSSShadowMirrorReplayEmptyInput(t *testing.T) {
	report, err := RunWSSShadowMirrorReplay(nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Frames != 0 || report.Exact.ReferenceableBytePct != 0 || report.Normalized.CandidateTokensEstimate != 0 {
		t.Fatalf("empty replay should return zero report: %+v", report)
	}
}

func TestWSSShadowMirrorReplayShapeRankTrimsFullHistory(t *testing.T) {
	if got := wssShadowMirrorReplayShapeRank(" full_history "); got != 0 {
		t.Fatalf("trimmed full_history rank=%d, want 0", got)
	}
}

func TestWSSShadowMirrorReplaySortsRowsByHeadroomThenShape(t *testing.T) {
	rows := sortedWSSShadowMirrorReplayRows(map[string]*WSSShadowMirrorReplayRow{
		"root\x00text": {
			RequestShape:          "root",
			Kind:                  "text",
			Bytes:                 100,
			ReferenceableBytes:    20,
			ReferenceableSegments: 1,
		},
		"delta\x00tool": {
			RequestShape:          "delta",
			Kind:                  "tool",
			Bytes:                 100,
			ReferenceableBytes:    20,
			ReferenceableSegments: 1,
		},
		"full_history\x00command": {
			RequestShape:          "full_history",
			Kind:                  "command",
			Bytes:                 200,
			ReferenceableBytes:    80,
			ReferenceableSegments: 1,
		},
		"root\x00zero": {
			RequestShape: "root",
			Kind:         "zero",
			Bytes:        100,
		},
	})
	if len(rows) != 3 {
		t.Fatalf("zero-reference row should be filtered: %+v", rows)
	}
	if rows[0].Kind != "command" || rows[1].RequestShape != "delta" || rows[2].RequestShape != "root" {
		t.Fatalf("unexpected row order: %+v", rows)
	}
	if rows[0].ReferenceableBytePct != 40 || rows[0].CandidateTokensEstimate != 20 {
		t.Fatalf("row metrics not finalized: %+v", rows[0])
	}
	if percentInt(1, 0) != 0 || wssShadowMirrorReplayShapeRank("unknown") <= wssShadowMirrorReplayShapeRank("root") {
		t.Fatal("helper edge cases changed")
	}
}

func TestRunWSSShadowMirrorReplayErrorPathsAndNilGuards(t *testing.T) {
	_, err := RunWSSShadowMirrorReplay([]WSSABReplayFrame{{
		Direction: wsmitm.DirClientToServer,
		Payload:   []byte(`{"type":"request","body":`),
	}})
	if err == nil || !strings.Contains(err.Error(), "extract request body") {
		t.Fatalf("invalid frame should fail at request body extraction, got %v", err)
	}

	_, err = RunWSSShadowMirrorReplay([]WSSABReplayFrame{{
		Direction: wsmitm.DirClientToServer,
		Payload: wssShadowReplayTestPayload(t, map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": "bad-input",
			"input":            map[string]any{"not": "an array"},
			"stream":           true,
		}),
	}})
	if err == nil || !strings.Contains(err.Error(), "extract messages") {
		t.Fatalf("bad Codex input should fail at message extraction, got %v", err)
	}

	var exact *WSSShadowMirrorReplayExact
	exact.add(1, 2, 3, 4)
	exact.finalize()
	addWSSShadowMirrorReplayRows(map[string]*WSSShadowMirrorReplayRow{}, "root", map[string]servermirror.SegmentKindReport{
		" ":    {Segments: 1, Bytes: 10},
		"zero": {Bytes: 10},
	})
	if rows := sortedWSSShadowMirrorReplayRows(nil); rows != nil {
		t.Fatalf("empty rows should stay nil: %+v", rows)
	}
	if bytesToTokenEstimate(0) != 0 {
		t.Fatal("zero bytes should estimate zero tokens")
	}
}

func wssShadowReplayTestPayload(t *testing.T, body map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"type": "request",
		"body": body,
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func wssShadowReplayShellCall(callID, command string) map[string]any {
	return map[string]any{
		"type":    "local_shell_call",
		"call_id": callID,
		"command": []string{"bash", "-lc", command},
	}
}

func wssShadowReplayShellOutput(callID, command, output string) map[string]any {
	return map[string]any{
		"type":              "local_shell_call_output",
		"call_id":           callID,
		"command":           []string{"bash", "-lc", command},
		"aggregated_output": output,
	}
}

func wssShadowReplayFindRow(rows []WSSShadowMirrorReplayRow, kind string) *WSSShadowMirrorReplayRow {
	for i := range rows {
		if rows[i].Kind == kind {
			return &rows[i]
		}
	}
	return nil
}

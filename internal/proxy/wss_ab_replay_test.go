package proxy

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/abharness"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/proxy/wsmitm"
)

func TestRunWSSPhaseFABReplayReadDeltaIsRecoverable(t *testing.T) {
	home := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false

	var file strings.Builder
	for i := 0; i < 160; i++ {
		fmt.Fprintf(&file, "Replay fixture line %03d with stable content for comprehension comparison.\n", i)
	}
	frames := []WSSABReplayFrame{
		wssReplayServerToolCallFrame("read-1", "read_file", map[string]any{"path": "src/replay.md"}),
		wssReplayClientToolOutputFrame("read-1", "replay-session", "resp-1", file.String()),
		wssReplayServerToolCallFrame("read-2", "read_file", map[string]any{"path": "src/replay.md"}),
		wssReplayClientToolOutputFrame("read-2", "replay-session", "resp-2", file.String()),
	}

	got, err := RunWSSPhaseFABReplay(cfg, frames)
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestTurns != 2 || got.MutatedRequests != 1 {
		t.Fatalf("unexpected replay activity: %+v", got)
	}
	if got.Report.Lost() != 0 || got.Report.Saved() <= 0 {
		t.Fatalf("read-delta replay should save with no lost comprehension: %+v", got.Report)
	}
	if len(got.Report.Elisions) != 1 || got.Report.Elisions[0].Severity != abharness.SeverityRecoverable {
		t.Fatalf("repeat read should be classified recoverable, got %+v", got.Report.Elisions)
	}
}

func TestRunWSSPhaseFABReplayRecoveryNoteIsAuditedAsExtra(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.ArchiveRecoveryNoteEnabled = true

	frames := []WSSABReplayFrame{{
		Direction: wsmitm.DirClientToServer,
		Payload: mustMarshal(map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": "note-audit-session",
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "continue",
			}},
			"stream": true,
		}),
	}}

	got, err := RunWSSPhaseFABReplay(cfg, frames)
	if err != nil {
		t.Fatal(err)
	}
	if got.Report.Lost() != 1 {
		t.Fatalf("model-facing recovery note must be audited as context change: %+v", got.Report)
	}
	foundExtra := false
	for _, elision := range got.Report.Elisions {
		if elision.Severity == abharness.SeverityExtra {
			foundExtra = true
		}
	}
	if !foundExtra {
		t.Fatalf("want extra-block audit for note injection, got %+v", got.Report.Elisions)
	}
}

func TestRunWSSPhaseFABReplayRejectsBadFrames(t *testing.T) {
	if _, err := RunWSSPhaseFABReplay(config.Defaults(), []WSSABReplayFrame{{
		Direction: wsmitm.DirClientToServer,
		Payload:   []byte(`not json`),
	}}); err == nil {
		t.Fatal("malformed frame should fail the offline replay")
	}
	if _, err := RunWSSPhaseFABReplay(config.Defaults(), []WSSABReplayFrame{{
		Direction: wsmitm.Direction("sideways"),
		Payload:   []byte(`{"type":"request","input":[]}`),
	}}); err == nil {
		t.Fatal("unsupported direction should fail the offline replay")
	}
}

func wssReplayServerToolCallFrame(callID string, name string, arguments map[string]any) WSSABReplayFrame {
	return WSSABReplayFrame{
		Direction: wsmitm.DirServerToClient,
		Payload: mustMarshal(map[string]any{
			"type": string(wsmitm.FrameKindResponseOutputItemDone),
			"item": map[string]any{
				"type":      "function_call",
				"call_id":   callID,
				"name":      name,
				"arguments": wssReplayArgumentsString(arguments),
			},
		}),
	}
}

func wssReplayClientToolOutputFrame(callID string, promptCacheKey string, previousResponseID string, output string) WSSABReplayFrame {
	return WSSABReplayFrame{
		Direction: wsmitm.DirClientToServer,
		Payload: mustMarshal(map[string]any{
			"model":                "gpt-5-codex",
			"prompt_cache_key":     promptCacheKey,
			"previous_response_id": previousResponseID,
			"input": []map[string]any{{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  output,
			}},
			"stream": true,
		}),
	}
}

func wssReplayArgumentsString(arguments map[string]any) string {
	body, err := json.Marshal(arguments)
	if err != nil {
		panic(err)
	}
	return string(body)
}

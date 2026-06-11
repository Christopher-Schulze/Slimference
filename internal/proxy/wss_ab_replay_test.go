package proxy

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/abharness"
	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func TestRunWSSPhaseFABReplayReadDeltaIsRecoverable(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true

	var file strings.Builder
	for i := 0; i < 160; i++ {
		fmt.Fprintf(&file, "Replay fixture line %03d with stable content for comprehension comparison.\n", i)
	}
	frames := []WSSABReplayFrame{
		wssReplayServerToolCallFrame("read-1", "read_file", map[string]any{"path": "src/replay.md"}),
		wssReplayClientToolOutputFrame("read-1", "replay-session", "", file.String()),
		wssReplayServerToolCallFrame("read-2", "read_file", map[string]any{"path": "src/replay.md"}),
		wssReplayClientToolOutputFrame("read-2", "replay-session", "", file.String()),
	}

	got, err := RunWSSPhaseFABReplay(cfg, frames)
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestTurns != 2 || got.MutatedRequests != 1 {
		t.Fatalf("unexpected replay activity: %+v", got)
	}
	if got.ReducerStats.TokensSaved <= 0 || got.ReducerStats.ReadDeltaBlocks != 1 {
		t.Fatalf("read-delta reducer stats not recorded: %+v", got.ReducerStats)
	}
	if got.Report.Lost() != 0 || got.Report.Saved() <= 0 {
		t.Fatalf("read-delta replay should save with no lost comprehension: %+v", got.Report)
	}
	if len(got.Report.Elisions) != 1 || got.Report.Elisions[0].Severity != abharness.SeverityRecoverable {
		t.Fatalf("repeat read should be classified recoverable, got %+v", got.Report.Elisions)
	}

	again, err := RunWSSPhaseFABReplay(cfg, frames)
	if err != nil {
		t.Fatal(err)
	}
	if again.Report.Saved() != got.Report.Saved() || again.MutatedRequests != got.MutatedRequests {
		t.Fatalf("offline replay must be isolated from prior disk cache state: first=%+v again=%+v", got, again)
	}
}

func TestRunWSSPhaseFABReplayChangedReadDeltaExpandsArchive(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true

	var before strings.Builder
	var after strings.Builder
	for i := 0; i < 160; i++ {
		line := fmt.Sprintf("Replay delta fixture line %03d with stable content for archive-backed comparison.\n", i)
		before.WriteString(line)
		if i == 80 {
			after.WriteString("Replay delta fixture line 080 changed with exact archived replacement bytes.\n")
			continue
		}
		after.WriteString(line)
	}
	frames := []WSSABReplayFrame{
		wssReplayServerToolCallFrame("read-1", "read_file", map[string]any{"path": "src/replay-delta.md"}),
		wssReplayClientToolOutputFrame("read-1", "replay-delta-session", "", before.String()),
		wssReplayServerToolCallFrame("read-2", "read_file", map[string]any{"path": "src/replay-delta.md"}),
		wssReplayClientToolOutputFrame("read-2", "replay-delta-session", "", after.String()),
	}

	got, err := RunWSSPhaseFABReplay(cfg, frames)
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestTurns != 2 || got.MutatedRequests != 1 {
		t.Fatalf("unexpected changed-read replay activity: %+v", got)
	}
	if got.ReducerStats.TokensSaved <= 0 || got.ReducerStats.ReadDeltaBlocks != 1 {
		t.Fatalf("changed read-delta reducer stats not recorded: %+v", got.ReducerStats)
	}
	if got.Report.Lost() != 0 || got.Report.Saved() <= 0 {
		t.Fatalf("changed read-delta should save with exact archive recovery: %+v", got.Report)
	}
	if len(got.Report.Elisions) != 1 || got.Report.Elisions[0].Severity != abharness.SeverityReferenced {
		t.Fatalf("changed read-delta should be verified through archive expansion, got %+v", got.Report.Elisions)
	}
}

func TestRunWSSPhaseFABReplayClassifiesRequestShapes(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false

	frames := []WSSABReplayFrame{
		{
			Direction: wsmitm.DirClientToServer,
			Payload: mustMarshal(map[string]any{
				"model":            "gpt-5-codex",
				"prompt_cache_key": "shape-session",
				"input": []map[string]any{{
					"type":    "message",
					"role":    "user",
					"content": "start",
				}},
				"stream": true,
			}),
		},
		wssReplayClientToolOutputFrame("delta-call", "shape-session", "resp-delta", "delta tool output"),
		{
			Direction: wsmitm.DirClientToServer,
			Payload: mustMarshal(map[string]any{
				"model":                "gpt-5-codex",
				"prompt_cache_key":     "shape-session",
				"previous_response_id": "resp-full",
				"input": []map[string]any{
					{"type": "function_call", "call_id": "full-call", "name": "read_file", "arguments": map[string]any{"path": "src/shape.go"}},
					{"type": "function_call_output", "call_id": "full-call", "output": "full-history output"},
				},
				"stream": true,
			}),
		},
	}

	got, err := RunWSSPhaseFABReplay(cfg, frames)
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestShapes.Root != 1 || got.RequestShapes.Delta != 1 || got.RequestShapes.FullHistory != 1 {
		t.Fatalf("unexpected request shape counts: %+v", got.RequestShapes)
	}
	if got.MutatedShapes.Root != 0 || got.MutatedShapes.Delta != 0 || got.MutatedShapes.FullHistory != 0 {
		t.Fatalf("shape-only replay should not report mutations: %+v", got.MutatedShapes)
	}
}

func TestRunWSSPhaseFABReplayCountsMutatedFullHistoryShape(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true

	var file strings.Builder
	for i := 0; i < 160; i++ {
		fmt.Fprintf(&file, "Full-history fixture line %03d with stable content for shape accounting.\n", i)
	}
	frames := []WSSABReplayFrame{
		wssReplayFullHistoryToolOutputFrame("read-1", "full-history-session", "resp-full-1", "src/full.go", file.String()),
		wssReplayFullHistoryToolOutputFrame("read-2", "full-history-session", "resp-full-2", "src/full.go", file.String()),
	}

	got, err := RunWSSPhaseFABReplay(cfg, frames)
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestShapes.FullHistory != 2 || got.RequestShapes.Root != 0 || got.RequestShapes.Delta != 0 {
		t.Fatalf("unexpected full-history request counts: %+v", got.RequestShapes)
	}
	if got.MutatedShapes.FullHistory != 1 || got.MutatedShapes.Root != 0 || got.MutatedShapes.Delta != 0 {
		t.Fatalf("unexpected full-history mutation counts: %+v", got.MutatedShapes)
	}
	if got.ReducerStats.ReadDeltaBlocks != 1 || got.Report.Saved() <= 0 {
		t.Fatalf("full-history repeated read should produce one audited saving: stats=%+v report=%+v", got.ReducerStats, got.Report)
	}
}

func TestRunWSSPhaseFABReplayCountsCapturedMutatedFullHistoryShape(t *testing.T) {
	frame := wssReplayFullHistoryToolOutputFrame("read-1", "captured-full-history-session", "", "src/full.go", "captured output")
	frame.Mutated = true
	got, err := RunWSSPhaseFABReplay(config.Defaults(), []WSSABReplayFrame{frame})
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestTurns != 0 || got.MutatedRequests != 0 {
		t.Fatalf("captured B-side frames should be counted but not replayed as fresh turns: %+v", got)
	}
	if got.CapturedMutatedRequests != 1 || got.CapturedMutatedShapes.FullHistory != 1 ||
		got.CapturedMutatedShapes.Root != 0 || got.CapturedMutatedShapes.Delta != 0 {
		t.Fatalf("captured full-history mutation not counted: requests=%d shapes=%+v", got.CapturedMutatedRequests, got.CapturedMutatedShapes)
	}
}

func TestRunWSSPhaseFABReplayTracksNamedSearchProofStats(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true

	frames := []WSSABReplayFrame{
		wssReplayServerToolCallFrame("search-1", "exec_command", map[string]any{"cmd": "rg -n needle src"}),
		wssReplayClientToolOutputFrame("search-1", "search-proof-session", "", wssReplaySearchOutputFixture("needle", 96)),
		{
			Direction: wsmitm.DirServerToClient,
			Payload: mustMarshal(map[string]any{
				"type":   string(wsmitm.FrameKindError),
				"status": 400,
				"error": map[string]any{
					"type":    "invalid_request_error",
					"message": "Invalid request",
				},
			}),
		},
	}

	got, err := RunWSSPhaseFABReplay(cfg, frames)
	if err != nil {
		t.Fatal(err)
	}
	if got.SearchStats.RequestTurns != 1 || got.SearchStats.MutatedRequests != 1 {
		t.Fatalf("named search request/mutation not counted: %+v", got.SearchStats)
	}
	if got.SearchStats.UpstreamErrorFrames != 1 || got.SearchStats.HTTP400Errors != 1 ||
		got.SearchStats.InvalidRequestErrors != 1 || got.SearchStats.ResponseFailedFrames != 0 {
		t.Fatalf("search upstream error not attributed: %+v", got.SearchStats)
	}
}

func TestRunWSSPhaseFABReplayTracksCapturedMutatedSearchStats(t *testing.T) {
	frame := wssReplayClientToolOutputFrame("search-captured", "captured-search-session", "", wssReplaySearchOutputFixture("needle", 40))
	frame.Mutated = true
	frames := []WSSABReplayFrame{
		wssReplayServerToolCallFrame("search-captured", "exec_command", map[string]any{"cmd": "rg -n needle src"}),
		frame,
	}

	got, err := RunWSSPhaseFABReplay(config.Defaults(), frames)
	if err != nil {
		t.Fatal(err)
	}
	if got.SearchStats.RequestTurns != 0 || got.SearchStats.MutatedRequests != 0 ||
		got.SearchStats.CapturedMutatedRequests != 1 {
		t.Fatalf("captured mutated search stats wrong: %+v", got.SearchStats)
	}
}

func TestRunWSSPhaseFABReplayAttributesSearchResponseFailed(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true

	frames := []WSSABReplayFrame{
		wssReplayServerToolCallFrame("search-failed", "exec_command", map[string]any{"cmd": "rg -n needle src"}),
		wssReplayClientToolOutputFrame("search-failed", "search-response-failed-session", "", wssReplaySearchOutputFixture("needle", 96)),
		{
			Direction: wsmitm.DirServerToClient,
			Payload: mustMarshal(map[string]any{
				"type": string(wsmitm.FrameKindResponseFailed),
				"response": map[string]any{
					"status": "failed",
					"error":  map[string]any{"type": "server_error"},
				},
			}),
		},
	}

	got, err := RunWSSPhaseFABReplay(cfg, frames)
	if err != nil {
		t.Fatal(err)
	}
	if got.SearchStats.UpstreamErrorFrames != 1 || got.SearchStats.ResponseFailedFrames != 1 ||
		got.SearchStats.InvalidRequestErrors != 0 || got.SearchStats.HTTP400Errors != 0 {
		t.Fatalf("search response.failed not attributed precisely: %+v", got.SearchStats)
	}
}

func TestRunWSSPhaseFABReplayDoesNotTreatUnresolvedSearchOutputAsProof(t *testing.T) {
	got, err := RunWSSPhaseFABReplay(config.Defaults(), []WSSABReplayFrame{
		wssReplayClientToolOutputFrame("missing-search-use", "unresolved-search-session", "", wssReplaySearchOutputFixture("needle", 96)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.SearchStats.RequestTurns != 0 || got.SearchStats.MutatedRequests != 0 {
		t.Fatalf("unresolved search-looking output must not count as named search proof: %+v", got.SearchStats)
	}
}

func TestWSSReplaySearchClassifierUsesRememberedToolUse(t *testing.T) {
	frame := wssReplayClientToolOutputFrame("remembered-search", "remembered-search-session", "", wssReplaySearchOutputFixture("needle", 8))
	adapter := &wsPhaseFAdapter{
		toolUses: map[string]types.ContentBlock{
			"remembered-search": {
				Type:      "tool_use",
				ToolUseID: "remembered-search",
				ToolName:  "exec_command",
				ToolInput: `{"cmd":"rg -n needle src"}`,
			},
		},
	}

	ok, err := wssReplayRequestHasNamedSearchOutput(frame.Payload, adapter)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("remembered tool use should classify the output as named search proof")
	}
	if index := wssReplayToolUseIndex(nil, adapter); len(index) != 1 || index["remembered-search"].ToolName != "exec_command" {
		t.Fatalf("remembered tool-use index not merged: %+v", index)
	}
	if index := wssReplayToolUseIndex(nil, nil); len(index) != 0 {
		t.Fatalf("nil adapter should return no remembered tool uses: %+v", index)
	}
	if _, err := wssReplayRequestHasNamedSearchOutput([]byte(`{"input":`), nil); err == nil {
		t.Fatal("malformed request should fail search proof classification")
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
	if got.Report.Lost() == 0 {
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

func TestRunWSSPhaseFABReplayInstructionsAreModelFacing(t *testing.T) {
	direct := mustMarshal(map[string]any{
		"model":        "gpt-5-codex",
		"instructions": "base instructions",
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": "continue",
		}},
		"stream": true,
	})
	compressed := mustMarshal(map[string]any{
		"model":        "gpt-5-codex",
		"instructions": "base instructions\n\nextra recovery note",
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": "continue",
		}},
		"stream": true,
	})

	before, err := extractWSSReplayModelFacingMessages(direct)
	if err != nil {
		t.Fatal(err)
	}
	after, err := extractWSSReplayModelFacingMessages(compressed)
	if err != nil {
		t.Fatal(err)
	}
	rep := abharness.Compare([]abharness.Turn{{Before: before, After: after}})
	if rep.Lost() == 0 || len(rep.Elisions) == 0 {
		t.Fatalf("top-level Codex instructions must be audited as model-facing context: %+v", rep)
	}
	if rep.Elisions[0].Severity != abharness.SeverityChanged {
		t.Fatalf("instruction mutation should be classified as changed model-facing context, got %+v", rep.Elisions)
	}
}

func TestWSSReplayExpectedInstructionExtraOnlyAllowsOutputReduceSuffix(t *testing.T) {
	base := mustMarshal(map[string]any{
		"model":        "gpt-5-codex",
		"instructions": "base instructions",
		"input":        []map[string]any{{"type": "message", "role": "user", "content": "continue"}},
	})
	withOutputReduce := mustMarshal(map[string]any{
		"model":        "gpt-5-codex",
		"instructions": "base instructions\n\n#slimference-output-rules\nAnswer directly.",
		"input":        []map[string]any{{"type": "message", "role": "user", "content": "continue"}},
	})
	unknownExtra := mustMarshal(map[string]any{
		"model":        "gpt-5-codex",
		"instructions": "base instructions\n\nunknown extra",
		"input":        []map[string]any{{"type": "message", "role": "user", "content": "continue"}},
	})
	changedPrefix := mustMarshal(map[string]any{
		"model":        "gpt-5-codex",
		"instructions": "changed instructions\n\n#slimference-output-rules\nAnswer directly.",
		"input":        []map[string]any{{"type": "message", "role": "user", "content": "continue"}},
	})

	if !wssReplayExpectedInstructionExtra(base, withOutputReduce) {
		t.Fatal("output-reduce suffix should be a known expected instruction extra")
	}
	if wssReplayExpectedInstructionExtra(base, unknownExtra) {
		t.Fatal("unknown instruction additions must not be expected extras")
	}
	if wssReplayExpectedInstructionExtra(base, changedPrefix) {
		t.Fatal("changed instruction prefixes must not be expected extras")
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
	body := map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": promptCacheKey,
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": callID,
			"output":  output,
		}},
		"stream": true,
	}
	if previousResponseID != "" {
		body["previous_response_id"] = previousResponseID
	}
	return WSSABReplayFrame{
		Direction: wsmitm.DirClientToServer,
		Payload:   mustMarshal(body),
	}
}

func wssReplayFullHistoryToolOutputFrame(callID string, promptCacheKey string, previousResponseID string, path string, output string) WSSABReplayFrame {
	return WSSABReplayFrame{
		Direction: wsmitm.DirClientToServer,
		Payload: mustMarshal(map[string]any{
			"model":                "gpt-5-codex",
			"prompt_cache_key":     promptCacheKey,
			"previous_response_id": previousResponseID,
			"input": []map[string]any{
				{"type": "function_call", "call_id": callID, "name": "read_file", "arguments": map[string]any{"path": path}},
				{"type": "function_call_output", "call_id": callID, "output": output},
			},
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

func wssReplaySearchOutputFixture(needle string, count int) string {
	var out strings.Builder
	for i := 0; i < count; i++ {
		fmt.Fprintf(&out, "src/pkg/file_%03d.go:%d:%s match with enough surrounding deterministic context for compaction\n", i%12, i+10, needle)
	}
	return out.String()
}

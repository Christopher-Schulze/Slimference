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

func TestRunWSSPhaseFABReplayUnwrapsRequestEnvelopeBody(t *testing.T) {
	frames := []WSSABReplayFrame{{
		Direction: wsmitm.DirClientToServer,
		Payload: mustMarshal(map[string]any{
			"type": string(wsmitm.FrameKindRequest),
			"body": map[string]any{
				"model":            "gpt-5-codex",
				"prompt_cache_key": "envelope-prefix-session",
				"instructions":     "envelope instructions",
				"input": []map[string]any{{
					"type":    "message",
					"role":    "user",
					"content": "start",
				}},
				"stream": true,
			},
		}),
	}}

	got, err := RunWSSPhaseFABReplay(config.Defaults(), frames)
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestTurns != 1 || got.RequestShapes.Root != 1 {
		t.Fatalf("envelope body must be replayed as a real request: %+v", got)
	}
	root := wssReplayPrefixSurfaceForTest(t, got.PrefixSurfaces, "root")
	if root.InstructionPrefixRequests != 1 || root.InstructionBytes == 0 || root.PromptCacheRequests != 1 {
		t.Fatalf("envelope body prefix surface not measured: %+v", root)
	}
}

func TestRunWSSPhaseFABReplayReportsPrefixSurfaces(t *testing.T) {
	frames := []WSSABReplayFrame{
		{
			Direction: wsmitm.DirClientToServer,
			Payload: mustMarshal(map[string]any{
				"model":            "gpt-5-codex",
				"prompt_cache_key": "prefix-session",
				"instructions":     "root instructions",
				"input": []map[string]any{{
					"type":    "message",
					"role":    "user",
					"content": "start",
				}},
				"tools": []map[string]any{
					codexToolDefinition("Bash", "Run shell commands"),
					codexToolDefinition("ColdTool", "Cold nondefault schema"),
				},
				"stream": true,
			}),
		},
		{
			Direction: wsmitm.DirClientToServer,
			Payload: mustMarshal(map[string]any{
				"model":                "gpt-5-codex",
				"prompt_cache_key":     "prefix-session",
				"previous_response_id": "resp-delta-prefix",
				"instructions":         "delta instructions",
				"input": []map[string]any{{
					"type":    "function_call_output",
					"call_id": "delta-call",
					"output":  "delta output",
				}},
				"tools": []map[string]any{
					codexToolDefinition("Bash", "Run shell commands"),
				},
				"stream": true,
			}),
		},
	}

	got, err := RunWSSPhaseFABReplay(config.Defaults(), frames)
	if err != nil {
		t.Fatal(err)
	}
	root := wssReplayPrefixSurfaceForTest(t, got.PrefixSurfaces, "root")
	if root.Requests != 1 || root.PreviousResponseRequests != 0 || root.StatefulCandidateRequests != 0 ||
		root.ToolDefinitions != 2 || root.DefaultKeepTools != 1 || root.NonDefaultTools != 1 ||
		root.NonDefaultToolRequests != 1 || root.InstructionPrefixRequests != 1 || root.PrefixBytes == 0 {
		t.Fatalf("root prefix surface mismatch: %+v", root)
	}
	delta := wssReplayPrefixSurfaceForTest(t, got.PrefixSurfaces, "delta")
	if delta.Requests != 1 || delta.PreviousResponseRequests != 1 || delta.StatefulCandidateRequests != 1 ||
		delta.StatefulCandidatePrefixBytes == 0 || delta.DefaultKeepOnlyToolRequests != 1 ||
		delta.NonDefaultTools != 0 || delta.InstructionBytes == 0 {
		t.Fatalf("delta prefix surface mismatch: %+v", delta)
	}
}

func TestRunWSSPhaseFABReplayReportsUnnamedPrefixSurface(t *testing.T) {
	frames := []WSSABReplayFrame{{
		Direction: wsmitm.DirClientToServer,
		Payload: mustMarshal(map[string]any{
			"model":                "gpt-5-codex",
			"prompt_cache_key":     "unnamed-prefix-session",
			"previous_response_id": "resp-unnamed-prefix",
			"input": []map[string]any{{
				"type":    "function_call_output",
				"call_id": "delta-call",
				"output":  "delta output",
			}},
			"tools":  []map[string]any{{"type": "unknown_special_tool"}},
			"stream": true,
		}),
	}}

	got, err := RunWSSPhaseFABReplay(config.Defaults(), frames)
	if err != nil {
		t.Fatal(err)
	}
	delta := wssReplayPrefixSurfaceForTest(t, got.PrefixSurfaces, "delta")
	if delta.UnnamedTools != 1 || delta.UnnamedToolRequests != 1 || delta.UnnamedBytes == 0 ||
		delta.StatefulCandidateRequests != 1 {
		t.Fatalf("unnamed tool prefix surface mismatch: %+v", delta)
	}
}

func TestRunWSSPhaseFABReplaySkipsClientControlFrames(t *testing.T) {
	got, err := RunWSSPhaseFABReplay(config.Defaults(), []WSSABReplayFrame{{
		Direction: wsmitm.DirClientToServer,
		Payload:   mustMarshal(map[string]any{"type": string(wsmitm.FrameKindPing)}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestTurns != 0 || got.RequestShapes.Root != 0 || len(got.PrefixSurfaces) != 0 {
		t.Fatalf("client control frame must not be replayed as request body: %+v", got)
	}
}

func TestRunWSSPhaseFABReplayDefaultToolPruneMutatesOnlyFullHistoryShape(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.Enabled = false
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.Tuning.ToolPruneEnabled = false
	cfg.Compression.Tuning.WSSFullHistoryToolPruneEnabled = true
	cfg.Compression.Tuning.ToolPruneIdleThresholdTurns = 1

	frames := []WSSABReplayFrame{
		wssReplayServerToolCallFrame("cold-call", "ColdTool", map[string]any{"arg": "seed"}),
		wssReplayClientToolOutputFrame("cold-call", "tool-prune-replay", "resp-cold", "cold tool output"),
		wssReplayServerToolCallFrame("bash-call", "Bash", map[string]any{"cmd": "echo ok"}),
		wssReplayClientToolOutputFrame("bash-call", "tool-prune-replay", "resp-bash", "ok"),
		{
			Direction: wsmitm.DirClientToServer,
			Payload: mustMarshal(map[string]any{
				"model":                "gpt-5-codex",
				"prompt_cache_key":     "tool-prune-replay",
				"previous_response_id": "resp-full-history-tool-prune",
				"input": []map[string]any{
					{"type": "function_call", "call_id": "bash-call", "name": "Bash", "arguments": map[string]any{"cmd": "echo ok"}},
					{"type": "function_call_output", "call_id": "bash-call", "output": "ok"},
				},
				"tools": []map[string]any{
					codexToolDefinition("Bash", "Run a shell command"),
					codexToolDefinition("ColdTool", strings.Repeat("Idle expensive schema. ", 80)),
				},
				"stream": true,
			}),
		},
	}

	got, err := RunWSSPhaseFABReplay(cfg, frames)
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestShapes.Delta != 2 || got.RequestShapes.FullHistory != 1 || got.RequestShapes.Root != 0 {
		t.Fatalf("unexpected request shape counts: %+v", got.RequestShapes)
	}
	if got.MutatedShapes.FullHistory != 1 || got.MutatedShapes.Delta != 0 || got.MutatedShapes.Root != 0 {
		t.Fatalf("default tool-prune safe slice must mutate only full-history, got %+v", got.MutatedShapes)
	}
	if got.MutatedRequests != 1 {
		t.Fatalf("tool-prune replay should have exactly one mutation: %+v", got)
	}
	if got.Report.Lost() == 0 {
		t.Fatalf("tool-prune replay must expose tool-schema capability changes: %+v", got.Report)
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

func TestRunWSSPhaseFABReplayReportsGuardedDeltaObserveOnly(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false

	readPayload := strings.Repeat("guarded delta read observation line\n", 120)
	repeatedPayload := strings.Repeat("guarded delta report observation row\n", 120)
	frames := []WSSABReplayFrame{
		wssReplayServerToolCallFrame("read-delta-1", "read_file", map[string]any{"path": "src/guarded.go"}),
		wssReplayClientToolOutputFrame("read-delta-1", "guarded-observe-session", "resp-read-1", readPayload),
		wssReplayServerToolCallFrame("read-delta-2", "read_file", map[string]any{"path": "src/guarded.go"}),
		wssReplayClientToolOutputFrame("read-delta-2", "guarded-observe-session", "resp-read-2", readPayload),
		wssReplayServerToolCallFrame("report-delta-1", "exec_command", map[string]any{"cmd": "python generate_report.py"}),
		wssReplayClientToolOutputFrame("report-delta-1", "guarded-observe-session", "resp-report-1", repeatedPayload),
		wssReplayServerToolCallFrame("report-delta-2", "exec_command", map[string]any{"cmd": "python generate_report.py"}),
		wssReplayClientToolOutputFrame("report-delta-2", "guarded-observe-session", "resp-report-2", repeatedPayload),
	}

	got, err := RunWSSPhaseFABReplay(cfg, frames)
	if err != nil {
		t.Fatal(err)
	}
	if got.MutatedRequests != 0 || got.Report.Saved() != 0 || got.ReducerStats.TokensSaved != 0 ||
		got.ReducerStats.ReadDeltaBlocks != 0 || got.ReducerStats.RepeatedOutputBlocks != 0 {
		t.Fatalf("guarded delta observe-only must not claim wire savings: %+v", got)
	}
	if got.ObserveStats.GuardedDeltaReadDeltaMisses != 1 ||
		got.ObserveStats.GuardedDeltaReadDeltaHits != 1 ||
		got.ObserveStats.GuardedDeltaRepeatedOutputMisses != 1 ||
		got.ObserveStats.GuardedDeltaRepeatedOutputHits != 1 {
		t.Fatalf("guarded delta observe-only counters mismatch: %+v", got.ObserveStats)
	}
}

func TestRunWSSPhaseFABReplayUniformChunkBudgetControlShowsCompoundLift(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexChunkDedupEnabled = true
	cfg.Compression.OutputReduce.CodexChunkDedupMinBytes = 4096
	cfg.Compression.OutputReduce.CodexChunkDedupMaxReferencePercent = 100
	cfg.Compression.OutputReduce.CodexChunkDedupMaxSessionReferencePercent = 5

	lowShared := strings.Repeat("t359 replay low budget contender line\n", 260)
	highShared := strings.Repeat("t359 replay high budget contender line with much more session footprint\n", 4000)
	frames := []WSSABReplayFrame{
		wssReplayServerToolCallFrame("seed-low", "read_file", map[string]any{"path": "low.seed"}),
		wssReplayServerToolCallFrame("seed-high", "read_file", map[string]any{"path": "high.seed"}),
		wssReplayClientToolOutputsFrame("t359-compound-session", "", []wssReplayToolOutput{
			{CallID: "seed-low", Output: lowShared + "seed low tail\n"},
			{CallID: "seed-high", Output: highShared + "seed high tail\n"},
		}),
		wssReplayServerToolCallFrame("low", "read_file", map[string]any{"path": "low.go"}),
		wssReplayServerToolCallFrame("high", "read_file", map[string]any{"path": "high.go"}),
		wssReplayClientToolOutputsFrame("t359-compound-session", "", []wssReplayToolOutput{
			{CallID: "low", Output: lowShared + "fresh low tail\n"},
			{CallID: "high", Output: highShared + "fresh high tail\n"},
		}),
	}

	priority, err := RunWSSPhaseFABReplayWithOptions(cfg, frames, WSSABReplayOptions{})
	if err != nil {
		t.Fatal(err)
	}
	uniform, err := RunWSSPhaseFABReplayWithOptions(cfg, frames, WSSABReplayOptions{UniformChunkDedupBudget: true})
	if err != nil {
		t.Fatal(err)
	}
	if priority.Report.Lost() != 1 || uniform.Report.Lost() != 1 {
		t.Fatalf("control proof should only contain the expected recovery-note extra: priority=%+v uniform=%+v", priority.Report, uniform.Report)
	}
	if priority.ReducerStats.HighFootprintAppliedDecisions <= uniform.ReducerStats.HighFootprintAppliedDecisions {
		t.Fatalf("footprint priority should improve high-footprint selection: priority=%+v uniform=%+v", priority.ReducerStats, uniform.ReducerStats)
	}
	if priority.ReducerStats.HighFootprintAppliedDecisions == 0 || uniform.ReducerStats.HighFootprintAppliedDecisions != 0 {
		t.Fatalf("priority should spend budget on high-footprint candidate: priority=%+v uniform=%+v", priority.ReducerStats, uniform.ReducerStats)
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

func TestRunWSSPhaseFABReplayToolSchemaIsModelFacing(t *testing.T) {
	direct := mustMarshal(map[string]any{
		"model": "gpt-5-codex",
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": "continue",
		}},
		"tools": []map[string]any{
			codexToolDefinition("Bash", "Run shell commands"),
			codexToolDefinition("ColdTool", "Cold nondefault schema"),
		},
		"stream": true,
	})
	compressed := mustMarshal(map[string]any{
		"model": "gpt-5-codex",
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": "continue",
		}},
		"tools": []map[string]any{
			codexToolDefinition("Bash", "Run shell commands"),
		},
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
		t.Fatalf("tool schema removal must be audited as model-facing context loss: %+v", rep)
	}
}

func TestWSSReplayToolSchemaCanonicalizationIgnoresJSONFormatting(t *testing.T) {
	direct := []byte(`{"model":"gpt-5-codex","input":[],"tools":[{"type":"function","function":{"name":"Bash","description":"Run shell commands","parameters":{"type":"object","properties":{}}}}],"stream":true}`)
	reshaped := []byte(`{"stream":true,"tools":[{"function":{"parameters":{"properties":{},"type":"object"},"description":"Run shell commands","name":"Bash"},"type":"function"}],"input":[],"model":"gpt-5-codex"}`)

	before, err := extractWSSReplayModelFacingMessages(direct)
	if err != nil {
		t.Fatal(err)
	}
	after, err := extractWSSReplayModelFacingMessages(reshaped)
	if err != nil {
		t.Fatal(err)
	}
	rep := abharness.Compare([]abharness.Turn{{Before: before, After: after}})
	if rep.Lost() != 0 {
		t.Fatalf("canonical tool schema should ignore JSON formatting/order only: %+v", rep)
	}
	if got, ok := codexReplayCanonicalJSON(nil); ok || got != "" {
		t.Fatalf("empty canonical JSON should fail, got %q ok=%t", got, ok)
	}
	if got, ok := codexReplayCanonicalJSON(json.RawMessage(`{`)); ok || got != "" {
		t.Fatalf("malformed canonical JSON should fail, got %q ok=%t", got, ok)
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
	return wssReplayClientToolOutputsFrame(promptCacheKey, previousResponseID, []wssReplayToolOutput{{CallID: callID, Output: output}})
}

type wssReplayToolOutput struct {
	CallID string
	Output string
}

func wssReplayClientToolOutputsFrame(promptCacheKey string, previousResponseID string, outputs []wssReplayToolOutput) WSSABReplayFrame {
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

func wssReplayPrefixSurfaceForTest(t *testing.T, rows []WSSABReplayPrefixSurface, shape string) WSSABReplayPrefixSurface {
	t.Helper()
	for _, row := range rows {
		if row.Shape == shape {
			return row
		}
	}
	t.Fatalf("missing prefix surface for shape %q in %+v", shape, rows)
	return WSSABReplayPrefixSurface{}
}

package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/Christopher-Schulze/Slimference/internal/abharness"
	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/contentarchive"
	"github.com/Christopher-Schulze/Slimference/internal/outputreduce"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// WSSABReplayFrame is one decompressed Codex WSS envelope in replay order.
type WSSABReplayFrame struct {
	Direction wsmitm.Direction
	Payload   []byte
}

// WSSABReplayResult is the offline comprehension report plus basic reducer
// activity counters for the replay.
type WSSABReplayResult struct {
	Report                    abharness.Report
	RequestTurns              int
	MutatedRequests           int
	ExpectedInstructionExtras int
	ReducerStats              WSSABReplayReducerStats
}

// WSSABReplayReducerStats is the content-free reducer activity observed while
// replaying WSS frames. It is intentionally separate from Report.Saved(): the
// comprehension report expands archive references back to their original bytes,
// while these counters describe the model-facing compressed request sent by the
// reducer.
type WSSABReplayReducerStats struct {
	TokensSaved          int
	BlocksModified       int
	ReadDeltaBlocks      int
	RepeatedOutputBlocks int
	ChunkDedupBlocks     int
	CapturedOutputBlocks int
	CodexEnvelopeBlocks  int
	ChunkDedupReferences int
	ChunkDedupRefBytes   int
	ChunkDedupInputBytes int
}

// RunWSSPhaseFABReplay runs the real Codex WSS Phase-F reducer against a frame
// sequence and compares the model-facing request context against byte-equal
// direct forwarding. It is the T249 bridge between the reducer and the offline
// comprehension harness.
func RunWSSPhaseFABReplay(cfg *config.Config, frames []WSSABReplayFrame) (WSSABReplayResult, error) {
	home, err := os.MkdirTemp("", "slimference-wss-ab-replay-*")
	if err != nil {
		return WSSABReplayResult{}, fmt.Errorf("create isolated replay home: %w", err)
	}
	defer os.RemoveAll(home)

	wssABReplayHomeMu.Lock()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return home, nil }
	defer func() {
		proxyUserHomeDir = oldHome
		wssABReplayHomeMu.Unlock()
	}()
	return runWSSPhaseFABReplay(cfg, frames, contentarchive.DefaultDir(home))
}

var wssABReplayHomeMu sync.Mutex

func runWSSPhaseFABReplay(cfg *config.Config, frames []WSSABReplayFrame, archiveDir string) (WSSABReplayResult, error) {
	if cfg == nil {
		cfg = config.Defaults()
	}
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	turns := make([]abharness.Turn, 0, len(frames))
	out := WSSABReplayResult{}

	for i, frame := range frames {
		env, err := wsmitm.Parse(frame.Payload)
		if err != nil {
			return WSSABReplayResult{}, fmt.Errorf("parse frame %d: %w", i, err)
		}
		switch frame.Direction {
		case wsmitm.DirServerToClient:
			if _, err := adapter.handle(context.Background(), frame.Direction, &env); err != nil {
				return WSSABReplayResult{}, fmt.Errorf("handle server frame %d: %w", i, err)
			}
		case wsmitm.DirClientToServer:
			before, err := extractWSSReplayModelFacingMessages(frame.Payload)
			if err != nil {
				return WSSABReplayResult{}, fmt.Errorf("extract direct request %d: %w", i, err)
			}
			mutatedBody, runtimeMessages, changed, stats, _ := adapter.applyInputPipeline(frame.Payload)
			out.ReducerStats.add(stats)
			after, err := extractWSSReplayModelFacingMessages(mutatedBody)
			if err != nil {
				return WSSABReplayResult{}, fmt.Errorf("extract compressed request %d: %w", i, err)
			}
			if len(before) > 0 || len(after) > 0 {
				turns = append(turns, abharness.Turn{Before: before, After: after})
				out.RequestTurns++
			}
			if changed && !bytes.Equal(frame.Payload, mutatedBody) {
				out.MutatedRequests++
			}
			if wssReplayExpectedInstructionExtra(frame.Payload, mutatedBody) {
				out.ExpectedInstructionExtras++
			}
			rememberReplayRequestState(adapter, runtimeMessages)
		default:
			return WSSABReplayResult{}, fmt.Errorf("frame %d has unsupported direction %q", i, frame.Direction)
		}
	}
	out.Report = abharness.CompareWithArchiveExpansion(turns, func(id string) ([]byte, error) {
		_, body, err := contentarchive.Get(archiveDir, id)
		return body, err
	})
	return out, nil
}

func wssReplayExpectedInstructionExtra(before, after []byte) bool {
	beforeInstructions, beforeOK := codexReplayInstructions(before)
	afterInstructions, afterOK := codexReplayInstructions(after)
	if !beforeOK || !afterOK || beforeInstructions == afterInstructions {
		return false
	}
	if strings.Contains(beforeInstructions, outputreduce.DefaultMarker) {
		return false
	}
	return strings.HasPrefix(afterInstructions, beforeInstructions) &&
		strings.Contains(afterInstructions, outputreduce.DefaultMarker)
}

func extractWSSReplayModelFacingMessages(body []byte) ([]types.Message, error) {
	messages, _, err := extractMessages(types.CodexChatGPT, body)
	if err != nil {
		return nil, err
	}
	instructions, ok := codexReplayInstructions(body)
	if !ok {
		return messages, nil
	}
	system := types.Message{
		Index: -1,
		Role:  "system",
		Content: []types.ContentBlock{{
			Type: "text",
			Text: instructions,
		}},
	}
	return append([]types.Message{system}, messages...), nil
}

func codexReplayInstructions(body []byte) (string, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", false
	}
	instructionsRaw, ok := raw["instructions"]
	if !ok {
		return "", false
	}
	var instructions string
	if err := json.Unmarshal(instructionsRaw, &instructions); err != nil {
		return "", false
	}
	if instructions == "" {
		return "", false
	}
	return instructions, true
}

func (s *WSSABReplayReducerStats) add(stats proxyLayer0Stats) {
	if s == nil {
		return
	}
	s.TokensSaved += stats.TokensSaved
	s.BlocksModified += stats.BlocksModified
	s.ReadDeltaBlocks += stats.ReadDeltaBlocks
	s.RepeatedOutputBlocks += stats.RepeatedOutputBlocks
	s.ChunkDedupBlocks += stats.ChunkDedupBlocks
	s.CapturedOutputBlocks += stats.CapturedOutputBlocks
	s.CodexEnvelopeBlocks += stats.CodexExecEnvelopeBlocks
	s.ChunkDedupReferences += stats.ChunkDedupReferences
	s.ChunkDedupRefBytes += stats.ChunkDedupRefBytes
	s.ChunkDedupInputBytes += stats.ChunkDedupInputBytes
}

func rememberReplayRequestState(adapter *wsPhaseFAdapter, messages []types.Message) {
	if adapter == nil {
		return
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	adapter.messages = messages
	adapter.repdetIndex = buildRepdetIndex(messages)
	if adapter.toolUses == nil {
		adapter.toolUses = make(map[string]types.ContentBlock)
	}
	for id, use := range proxyToolUseIndex(messages) {
		adapter.toolUses[id] = use
	}
}

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
	"github.com/Christopher-Schulze/Slimference/internal/evidence"
	"github.com/Christopher-Schulze/Slimference/internal/filter"
	"github.com/Christopher-Schulze/Slimference/internal/outputreduce"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
	"github.com/Christopher-Schulze/Slimference/internal/savingspolicy"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// WSSABReplayFrame is one decompressed Codex WSS envelope in replay order.
type WSSABReplayFrame struct {
	Direction wsmitm.Direction
	Payload   []byte
	Mutated   bool
}

// WSSABReplayOptions contains proof-only controls for offline replay. These
// options are not product configuration and are intentionally unreachable from
// the normal WSS runtime path.
type WSSABReplayOptions struct {
	UniformChunkDedupBudget bool
}

// WSSABReplayResult is the offline comprehension report plus basic reducer
// activity counters for the replay.
type WSSABReplayResult struct {
	Report                    abharness.Report
	RequestTurns              int
	MutatedRequests           int
	CapturedMutatedRequests   int
	RequestShapes             WSSABReplayShapeCounts
	MutatedShapes             WSSABReplayShapeCounts
	CapturedMutatedShapes     WSSABReplayShapeCounts
	ExpectedInstructionExtras int
	ReducerStats              WSSABReplayReducerStats
	SearchStats               WSSABReplaySearchStats
	ObserveStats              WSSABReplayObserveStats
}

// WSSABReplayShapeCounts classifies request bodies by the state shape Codex
// sends over WSS. Delta and full-history requests both carry a previous
// response id; full-history additionally resends prior assistant/tool-use
// context.
type WSSABReplayShapeCounts struct {
	Root        int `json:"root"`
	Delta       int `json:"delta"`
	FullHistory int `json:"full_history"`
}

// WSSABReplaySearchStats is content-free proof metadata for the named-search
// WSS path. It distinguishes "the capture had no upstream errors" from "the
// capture actually exercised the search-output mutation surface."
type WSSABReplaySearchStats struct {
	RequestTurns            int
	MutatedRequests         int
	CapturedMutatedRequests int
	UpstreamErrorFrames     int
	HTTP400Errors           int
	InvalidRequestErrors    int
	ResponseFailedFrames    int
}

// WSSABReplayObserveStats counts cache state learned on guarded WSS delta
// turns. These counters prove local state seeding without claiming wire savings.
type WSSABReplayObserveStats struct {
	GuardedDeltaReadDeltaHits        int
	GuardedDeltaReadDeltaMisses      int
	GuardedDeltaRepeatedOutputHits   int
	GuardedDeltaRepeatedOutputMisses int
}

// WSSABReplayReducerStats is the content-free reducer activity observed while
// replaying WSS frames. It is intentionally separate from Report.Saved(): the
// comprehension report expands archive references back to their original bytes,
// while these counters describe the model-facing compressed request sent by the
// reducer.
type WSSABReplayReducerStats struct {
	TokensSaved                   int
	BlocksModified                int
	ReadDeltaBlocks               int
	RepeatedOutputBlocks          int
	ChunkDedupBlocks              int
	CapturedOutputBlocks          int
	CodexEnvelopeBlocks           int
	ChunkDedupReferences          int
	ChunkDedupRefBytes            int
	ChunkDedupInputBytes          int
	CompoundedEstimateTokens      int
	FootprintAppliedDecisions     int
	HighFootprintAppliedDecisions int
}

// RunWSSPhaseFABReplay runs the real Codex WSS Phase-F reducer against a frame
// sequence and compares the model-facing request context against byte-equal
// direct forwarding. It is the T249 bridge between the reducer and the offline
// comprehension harness.
func RunWSSPhaseFABReplay(cfg *config.Config, frames []WSSABReplayFrame) (WSSABReplayResult, error) {
	return RunWSSPhaseFABReplayWithOptions(cfg, frames, WSSABReplayOptions{})
}

// RunWSSPhaseFABReplayWithOptions is the same replay harness with explicit
// proof-only control options.
func RunWSSPhaseFABReplayWithOptions(cfg *config.Config, frames []WSSABReplayFrame, options WSSABReplayOptions) (WSSABReplayResult, error) {
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
	return runWSSPhaseFABReplay(cfg, frames, contentarchive.DefaultDir(home), options)
}

var wssABReplayHomeMu sync.Mutex

func runWSSPhaseFABReplay(cfg *config.Config, frames []WSSABReplayFrame, archiveDir string, options WSSABReplayOptions) (WSSABReplayResult, error) {
	if cfg == nil {
		cfg = config.Defaults()
	}
	p := New(cfg)
	p.wssABReplayUniformChunkBudget = options.UniformChunkDedupBudget
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	turns := make([]abharness.Turn, 0, len(frames))
	out := WSSABReplayResult{}
	lastRequestWasSearch := false

	for i, frame := range frames {
		env, err := wsmitm.Parse(frame.Payload)
		if err != nil {
			return WSSABReplayResult{}, fmt.Errorf("parse frame %d: %w", i, err)
		}
		switch frame.Direction {
		case wsmitm.DirServerToClient:
			if wssReplayUpstreamErrorKind(env.Kind) && lastRequestWasSearch {
				out.SearchStats.recordUpstreamError(&env)
			}
			if _, err := adapter.handle(context.Background(), frame.Direction, &env); err != nil {
				return WSSABReplayResult{}, fmt.Errorf("handle server frame %d: %w", i, err)
			}
		case wsmitm.DirClientToServer:
			searchRequest, err := wssReplayRequestHasNamedSearchOutput(frame.Payload, adapter)
			if err != nil {
				return WSSABReplayResult{}, fmt.Errorf("classify search request %d: %w", i, err)
			}
			before, err := extractWSSReplayModelFacingMessages(frame.Payload)
			if err != nil {
				return WSSABReplayResult{}, fmt.Errorf("extract direct request %d: %w", i, err)
			}
			shape, err := wssReplayRequestShape(frame.Payload)
			if err != nil {
				return WSSABReplayResult{}, fmt.Errorf("classify request shape %d: %w", i, err)
			}
			if frame.Mutated {
				out.CapturedMutatedRequests++
				out.CapturedMutatedShapes.add(shape)
				if searchRequest {
					out.SearchStats.CapturedMutatedRequests++
				}
				lastRequestWasSearch = searchRequest
				continue
			}
			out.RequestShapes.add(shape)
			if searchRequest {
				out.SearchStats.RequestTurns++
			}
			mutatedBody, runtimeMessages, changed, stats, _ := adapter.applyInputPipeline(frame.Payload)
			out.ReducerStats.add(stats)
			out.ObserveStats.add(shape, stats)
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
				out.MutatedShapes.add(shape)
				if searchRequest {
					out.SearchStats.MutatedRequests++
				}
			}
			if wssReplayExpectedInstructionExtra(frame.Payload, mutatedBody) {
				out.ExpectedInstructionExtras++
			}
			rememberReplayRequestState(adapter, runtimeMessages)
			lastRequestWasSearch = searchRequest
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

func wssReplayRequestShape(body []byte) (string, error) {
	messages, raw, err := extractMessages(types.CodexChatGPT, body)
	if err != nil {
		return "", err
	}
	return wssRequestShape(wssRequestMetaFromRaw(raw), messages), nil
}

func wssReplayRequestHasNamedSearchOutput(body []byte, adapter *wsPhaseFAdapter) (bool, error) {
	messages, _, err := extractMessages(types.CodexChatGPT, body)
	if err != nil {
		return false, err
	}
	toolUses := wssReplayToolUseIndex(messages, adapter)
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type != "tool_result" {
				continue
			}
			use, commandFromToolUse := proxyResolveToolUseDetailed(block, toolUses)
			commandLine := proxyLayer0CommandLine(use)
			workload := savingspolicy.CodexWorkloadCommand
			if filter.SearchOutputKeyFromCommandLine(commandLine) != "" {
				workload = savingspolicy.CodexWorkloadSearch
			}
			if proxyWSSSearchOutputProofAllowed(commandLine, use, commandFromToolUse, workload) {
				return true, nil
			}
		}
	}
	return false, nil
}

func wssReplayToolUseIndex(messages []types.Message, adapter *wsPhaseFAdapter) map[string]types.ContentBlock {
	out := proxyToolUseIndex(messages)
	if adapter == nil {
		return out
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	for id, use := range adapter.toolUses {
		if _, exists := out[id]; !exists {
			if out == nil {
				out = make(map[string]types.ContentBlock)
			}
			out[id] = use
		}
	}
	return out
}

func wssReplayUpstreamErrorKind(kind wsmitm.FrameKind) bool {
	return kind == wsmitm.FrameKindError || kind == wsmitm.FrameKindResponseFailed
}

func (s *WSSABReplaySearchStats) recordUpstreamError(env *wsmitm.Envelope) {
	if s == nil {
		return
	}
	s.UpstreamErrorFrames++
	if env != nil && env.Kind == wsmitm.FrameKindResponseFailed {
		s.ResponseFailedFrames++
	}
	status, errorType, _ := wssUpstreamErrorFields(env)
	if status == "400" {
		s.HTTP400Errors++
	}
	if errorType == "invalid_request_error" {
		s.InvalidRequestErrors++
	}
}

func (c *WSSABReplayShapeCounts) add(shape string) {
	if c == nil {
		return
	}
	switch shape {
	case "root":
		c.Root++
	case "delta":
		c.Delta++
	case "full_history":
		c.FullHistory++
	}
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
	for _, decision := range stats.EvidenceDecisions {
		if decision.Action != evidence.ActionApplied || decision.SavedTokens <= 0 || decision.FootprintScore <= 0 {
			continue
		}
		s.CompoundedEstimateTokens += decision.FootprintScore
		s.FootprintAppliedDecisions++
		if decision.FootprintScoreBucket == "high" {
			s.HighFootprintAppliedDecisions++
		}
	}
}

func (s *WSSABReplayObserveStats) add(shape string, stats proxyLayer0Stats) {
	if s == nil || shape != "delta" || !wssReplayStatsHasGuardedDeltaEvidence(stats) {
		return
	}
	for _, event := range stats.CacheEvents {
		switch event.Mechanism {
		case savingspolicy.CodexMechanismReadDelta:
			switch event.Action {
			case proxyLayer0CacheHit:
				s.GuardedDeltaReadDeltaHits++
			case proxyLayer0CacheMiss:
				s.GuardedDeltaReadDeltaMisses++
			}
		case savingspolicy.CodexMechanismRepeatedOutput:
			switch event.Action {
			case proxyLayer0CacheHit:
				s.GuardedDeltaRepeatedOutputHits++
			case proxyLayer0CacheMiss:
				s.GuardedDeltaRepeatedOutputMisses++
			}
		}
	}
}

func wssReplayStatsHasGuardedDeltaEvidence(stats proxyLayer0Stats) bool {
	for _, decision := range stats.EvidenceDecisions {
		if decision.Action == evidence.ActionFullPass && decision.Reason == "wss_stateful_delta_mutation_proof_gate" {
			return true
		}
	}
	return false
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

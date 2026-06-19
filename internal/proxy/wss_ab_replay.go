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
	Sequence  int64
	SocketSeq uint64
}

// WSSABReplayOptions contains proof-only controls for offline replay. These
// options are not product configuration and are intentionally unreachable from
// the normal WSS runtime path.
type WSSABReplayOptions struct {
	UniformChunkDedupBudget bool
	StatefulPrefixElision   bool
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
	PrefixSurfaces            []WSSABReplayPrefixSurface
	PrefixElisionStats        WSSABReplayPrefixElisionStats
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

// WSSABReplayPrefixSurface is content-free proof metadata for repeated WSS
// root-prefix mass. It measures tool schema and instruction bytes that could
// only be recovered by a separate stateful-prefix-elision proof.
type WSSABReplayPrefixSurface struct {
	Shape                        string `json:"shape"`
	Requests                     int    `json:"requests"`
	PreviousResponseRequests     int    `json:"previous_response_requests"`
	PromptCacheRequests          int    `json:"prompt_cache_requests"`
	ToolPrefixRequests           int    `json:"tool_prefix_requests"`
	InstructionPrefixRequests    int    `json:"instruction_prefix_requests"`
	PrefixBytes                  int    `json:"prefix_bytes"`
	ToolDefinitions              int    `json:"tool_definitions"`
	ToolDefinitionBytes          int    `json:"tool_definition_bytes"`
	InstructionBytes             int    `json:"instruction_bytes"`
	DefaultKeepTools             int    `json:"default_keep_tools"`
	DefaultKeepBytes             int    `json:"default_keep_bytes"`
	NonDefaultTools              int    `json:"nondefault_tools"`
	NonDefaultBytes              int    `json:"nondefault_bytes"`
	UnnamedTools                 int    `json:"unnamed_tools"`
	UnnamedBytes                 int    `json:"unnamed_bytes"`
	DefaultKeepOnlyToolRequests  int    `json:"default_keep_only_tool_requests"`
	NonDefaultToolRequests       int    `json:"nondefault_tool_requests"`
	UnnamedToolRequests          int    `json:"unnamed_tool_requests"`
	StatefulCandidateRequests    int    `json:"stateful_candidate_requests"`
	StatefulCandidatePrefixBytes int    `json:"stateful_candidate_prefix_bytes"`
}

// WSSABReplayPrefixElisionStats reports proof-only stateful tool-prefix
// elision in the offline replay harness. Instruction fields are kept for
// report compatibility and must stay zero because Codex WSS requires
// top-level instructions on previous_response_id requests.
type WSSABReplayPrefixElisionStats struct {
	Requests              int `json:"requests"`
	ToolRequests          int `json:"tool_requests"`
	InstructionRequests   int `json:"instruction_requests"`
	PrefixBytesSaved      int `json:"prefix_bytes_saved"`
	ToolBytesSaved        int `json:"tool_bytes_saved"`
	InstructionBytesSaved int `json:"instruction_bytes_saved"`
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
	prefixElider := wssReplayPrefixElisionState{}

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
			requestBody, ok, err := wssReplayRequestBody(frame.Payload)
			if err != nil {
				return WSSABReplayResult{}, fmt.Errorf("extract request body %d: %w", i, err)
			}
			if !ok {
				lastRequestWasSearch = false
				continue
			}
			searchRequest, err := wssReplayRequestHasNamedSearchOutput(requestBody, adapter)
			if err != nil {
				return WSSABReplayResult{}, fmt.Errorf("classify search request %d: %w", i, err)
			}
			before, err := extractWSSReplayModelFacingMessages(requestBody)
			if err != nil {
				return WSSABReplayResult{}, fmt.Errorf("extract direct request %d: %w", i, err)
			}
			shape, err := wssReplayRequestShape(requestBody)
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
			out.addPrefixSurface(shape, requestBody)
			if searchRequest {
				out.SearchStats.RequestTurns++
			}
			mutatedBody, runtimeMessages, changed, stats, _ := adapter.applyInputPipeline(requestBody)
			out.ReducerStats.add(stats)
			out.ObserveStats.add(shape, stats)
			if options.StatefulPrefixElision {
				var prefixChanged bool
				var prefixStats WSSABReplayPrefixElisionStats
				mutatedBody, prefixStats, prefixChanged = prefixElider.apply(mutatedBody)
				out.PrefixElisionStats.add(prefixStats)
				if prefixChanged {
					changed = true
				}
			}
			after, err := extractWSSReplayModelFacingMessages(mutatedBody)
			if err != nil {
				return WSSABReplayResult{}, fmt.Errorf("extract compressed request %d: %w", i, err)
			}
			recoveryNote := archiveRecoveryNoteText(p.config.Compression.OutputReduce.ArchiveRecoveryNoteText)
			expectedInstructionExtra := wssReplayExpectedInstructionExtra(requestBody, mutatedBody, recoveryNote)
			after = wssReplayMessagesWithoutExpectedArchiveRecoveryNote(after, stats, recoveryNote, expectedInstructionExtra)
			if len(before) > 0 || len(after) > 0 {
				turns = append(turns, abharness.Turn{Before: before, After: after})
				out.RequestTurns++
			}
			if changed && !bytes.Equal(requestBody, mutatedBody) {
				out.MutatedRequests++
				out.MutatedShapes.add(shape)
				if searchRequest {
					out.SearchStats.MutatedRequests++
				}
			}
			if expectedInstructionExtra {
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

func wssReplayRequestBody(payload []byte) ([]byte, bool, error) {
	env, err := wsmitm.Parse(payload)
	if err != nil {
		return nil, false, err
	}
	body, _, ok := wsRequestBody(&env)
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), body...), true, nil
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

func (r *WSSABReplayResult) addPrefixSurface(shape string, body []byte) {
	if r == nil {
		return
	}
	if shape == "" {
		shape = "unknown"
	}
	row := r.prefixSurfaceForShape(shape)
	row.Requests++

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return
	}
	previousResponse := strings.TrimSpace(rawJSONString(raw["previous_response_id"])) != ""
	if previousResponse {
		row.PreviousResponseRequests++
	}
	if strings.TrimSpace(rawJSONString(raw["prompt_cache_key"])) != "" {
		row.PromptCacheRequests++
	}

	metrics := wssRootPrefixMetrics(body)
	prefixBytes := metrics.ToolDefinitionBytes + metrics.InstructionBytes
	row.PrefixBytes += prefixBytes
	row.ToolDefinitions += metrics.ToolDefinitions
	row.ToolDefinitionBytes += metrics.ToolDefinitionBytes
	row.InstructionBytes += metrics.InstructionBytes
	row.DefaultKeepTools += metrics.DefaultKeepTools
	row.DefaultKeepBytes += metrics.DefaultKeepBytes
	row.NonDefaultTools += metrics.NonDefaultTools
	row.NonDefaultBytes += metrics.NonDefaultBytes
	row.UnnamedTools += metrics.UnnamedTools
	row.UnnamedBytes += metrics.UnnamedBytes
	if metrics.ToolDefinitionBytes > 0 {
		row.ToolPrefixRequests++
	}
	if metrics.InstructionBytes > 0 {
		row.InstructionPrefixRequests++
	}
	if metrics.ToolDefinitions > 0 && metrics.DefaultKeepTools == metrics.ToolDefinitions {
		row.DefaultKeepOnlyToolRequests++
	}
	if metrics.NonDefaultTools > 0 {
		row.NonDefaultToolRequests++
	}
	if metrics.UnnamedTools > 0 {
		row.UnnamedToolRequests++
	}
	if previousResponse && prefixBytes > 0 {
		row.StatefulCandidateRequests++
		row.StatefulCandidatePrefixBytes += prefixBytes
	}
}

func (r *WSSABReplayResult) prefixSurfaceForShape(shape string) *WSSABReplayPrefixSurface {
	for i := range r.PrefixSurfaces {
		if r.PrefixSurfaces[i].Shape == shape {
			return &r.PrefixSurfaces[i]
		}
	}
	r.PrefixSurfaces = append(r.PrefixSurfaces, WSSABReplayPrefixSurface{Shape: shape})
	return &r.PrefixSurfaces[len(r.PrefixSurfaces)-1]
}

func (s *WSSABReplayPrefixElisionStats) add(other WSSABReplayPrefixElisionStats) {
	if s == nil {
		return
	}
	s.Requests += other.Requests
	s.ToolRequests += other.ToolRequests
	s.InstructionRequests += other.InstructionRequests
	s.PrefixBytesSaved += other.PrefixBytesSaved
	s.ToolBytesSaved += other.ToolBytesSaved
	s.InstructionBytesSaved += other.InstructionBytesSaved
}

type wssReplayPrefixElisionState struct {
	seenTools map[string]struct{}
}

func (s *wssReplayPrefixElisionState) apply(body []byte) ([]byte, WSSABReplayPrefixElisionStats, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return body, WSSABReplayPrefixElisionStats{}, false
	}
	scope := strings.TrimSpace(rawJSONString(raw["prompt_cache_key"]))
	if scope == "" {
		return body, WSSABReplayPrefixElisionStats{}, false
	}
	previousResponse := strings.TrimSpace(rawJSONString(raw["previous_response_id"])) != ""
	stats := WSSABReplayPrefixElisionStats{}
	changed := false

	if tools, ok := codexReplayToolSchemaSurface(body); ok {
		key := scope + "\x00" + tools
		if previousResponse && s.hasSeenTools(key) {
			stats.ToolRequests = 1
			stats.ToolBytesSaved = len(raw["tools"])
			delete(raw, "tools")
			changed = true
		} else {
			s.markSeenTools(key)
		}
	}
	if !changed {
		return body, WSSABReplayPrefixElisionStats{}, false
	}
	stats.Requests = 1
	stats.PrefixBytesSaved = stats.ToolBytesSaved + stats.InstructionBytesSaved
	out, err := json.Marshal(raw)
	if err != nil {
		return body, WSSABReplayPrefixElisionStats{}, false
	}
	return out, stats, true
}

func (s *wssReplayPrefixElisionState) hasSeenTools(key string) bool {
	if s == nil || s.seenTools == nil {
		return false
	}
	_, ok := s.seenTools[key]
	return ok
}

func (s *wssReplayPrefixElisionState) markSeenTools(key string) {
	if s == nil || key == "" {
		return
	}
	if s.seenTools == nil {
		s.seenTools = make(map[string]struct{})
	}
	s.seenTools[key] = struct{}{}
}

func wssReplayExpectedInstructionExtra(before, after []byte, recoveryNote string) bool {
	beforeInstructions, beforeOK := codexReplayInstructions(before)
	afterInstructions, afterOK := codexReplayInstructions(after)
	if !beforeOK || !afterOK || beforeInstructions == afterInstructions {
		return false
	}
	if strings.Contains(beforeInstructions, outputreduce.DefaultMarker) {
		return false
	}
	if !strings.HasPrefix(afterInstructions, beforeInstructions) {
		return false
	}
	extra := strings.TrimSpace(strings.TrimPrefix(afterInstructions, beforeInstructions))
	if strings.Contains(extra, outputreduce.DefaultMarker) {
		return true
	}
	stripped, ok := wssReplayStripInstructionNote(afterInstructions, recoveryNote)
	return ok && stripped == beforeInstructions
}

func extractWSSReplayModelFacingMessages(body []byte) ([]types.Message, error) {
	messages, _, err := extractMessages(types.CodexChatGPT, body)
	if err != nil {
		return nil, err
	}
	prefix := make([]types.Message, 0, 2)
	instructions, ok := codexReplayInstructions(body)
	if ok {
		prefix = append(prefix, types.Message{
			Index: -2,
			Role:  "system",
			Content: []types.ContentBlock{{
				Type: "text",
				Text: instructions,
			}},
		})
	}
	toolSchema, ok := codexReplayToolSchemaSurface(body)
	if ok {
		prefix = append(prefix, types.Message{
			Index: -1,
			Role:  "system",
			Content: []types.ContentBlock{{
				Type: "text",
				Text: toolSchema,
			}},
		})
	}
	if len(prefix) == 0 {
		return messages, nil
	}
	return append(prefix, messages...), nil
}

func wssReplayMessagesWithoutExpectedArchiveRecoveryNote(messages []types.Message, stats proxyLayer0Stats, note string, expectedInstructionExtra bool) []types.Message {
	if !proxyLayer0StatsNeedsArchiveRecoveryNote(stats) && !expectedInstructionExtra {
		return messages
	}
	note = strings.TrimSpace(note)
	if note == "" {
		return messages
	}
	out := make([]types.Message, 0, len(messages))
	changed := false
	for _, msg := range messages {
		if msg.Index != -2 || len(msg.Content) == 0 {
			out = append(out, msg)
			continue
		}
		block := msg.Content[0]
		stripped, ok := wssReplayStripInstructionNote(block.Text, note)
		if !ok {
			out = append(out, msg)
			continue
		}
		changed = true
		if strings.TrimSpace(stripped) == "" {
			continue
		}
		next := msg
		next.Content = append([]types.ContentBlock(nil), msg.Content...)
		next.Content[0].Text = stripped
		out = append(out, next)
	}
	if !changed {
		return messages
	}
	return out
}

func wssReplayStripInstructionNote(text string, note string) (string, bool) {
	if strings.TrimSpace(text) == note {
		return "", true
	}
	paragraph := "\n\n" + note
	if strings.HasSuffix(text, paragraph) {
		return strings.TrimSuffix(text, paragraph), true
	}
	trimmedRight := strings.TrimRight(text, " \t\r\n")
	if trimmedRight != text && strings.HasSuffix(trimmedRight, paragraph) {
		return strings.TrimSuffix(trimmedRight, paragraph), true
	}
	if strings.HasPrefix(text, note+"\n\n") {
		return strings.TrimPrefix(text, note+"\n\n"), true
	}
	if strings.Contains(text, paragraph+"\n\n") {
		return strings.Replace(text, paragraph, "", 1), true
	}
	return text, false
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

func codexReplayToolSchemaSurface(body []byte) (string, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", false
	}
	toolsRaw, ok := raw["tools"]
	if !ok {
		return "", false
	}
	canonical, ok := codexReplayCanonicalJSON(toolsRaw)
	if !ok {
		return "", false
	}
	return "codex tools: " + canonical, true
}

func codexReplayCanonicalJSON(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", false
	}
	return string(data), true
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

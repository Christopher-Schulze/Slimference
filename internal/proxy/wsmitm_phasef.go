package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/beterse"
	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/outputreduce"
	"github.com/Christopher-Schulze/Slimference/internal/outstop"
	"github.com/Christopher-Schulze/Slimference/internal/outstop/repdet"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
	"github.com/Christopher-Schulze/Slimference/internal/qualityab"
	"github.com/Christopher-Schulze/Slimference/internal/servermirror"
	"github.com/Christopher-Schulze/Slimference/internal/sessions"
	"github.com/Christopher-Schulze/Slimference/internal/staleread"
	"github.com/Christopher-Schulze/Slimference/internal/tokens"
	"github.com/Christopher-Schulze/Slimference/internal/toolprune"
	"github.com/Christopher-Schulze/Slimference/internal/toolusecache"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

type wsPhaseFAdapter struct {
	p *Proxy

	mu              sync.Mutex
	messages        []types.Message
	repdetIndex     *repdet.Index
	toolUses        map[string]types.ContentBlock
	sessionID       string
	degraded        bool
	degradedReason  string
	toolUseHydrated bool
	collapsedKeys   map[string]struct{}
	qualityCohort   qualityab.Cohort
	counters        wsPhaseFCounters
}

type wsPhaseFCounters struct {
	requestsSeen           atomic.Int64
	requestBodiesSeen      atomic.Int64
	requestMessagesIndexed atomic.Int64
	responseTextDeltasSeen atomic.Int64
	terminalResponsesSeen  atomic.Int64
	mutations              atomic.Int64
}

type wsPhaseFTelemetry struct {
	RequestsSeen           int64
	RequestBodiesSeen      int64
	RequestMessagesIndexed int64
	ResponseTextDeltasSeen int64
	TerminalResponsesSeen  int64
	Mutations              int64
}

type wssRequestMeta struct {
	SessionID          string
	PreviousResponseID string
	Model              string
	ClientFamily       string
	BypassReason       string
	DebugFacts         map[string]string
}

func (a *wsPhaseFAdapter) snapshot() wsPhaseFTelemetry {
	if a == nil {
		return wsPhaseFTelemetry{}
	}
	return wsPhaseFTelemetry{
		RequestsSeen:           a.counters.requestsSeen.Load(),
		RequestBodiesSeen:      a.counters.requestBodiesSeen.Load(),
		RequestMessagesIndexed: a.counters.requestMessagesIndexed.Load(),
		ResponseTextDeltasSeen: a.counters.responseTextDeltasSeen.Load(),
		TerminalResponsesSeen:  a.counters.terminalResponsesSeen.Load(),
		Mutations:              a.counters.mutations.Load(),
	}
}

func (d *PhaseFDispatcher) newWSPhaseFAdapter() *wsPhaseFAdapter {
	return &wsPhaseFAdapter{p: d.Proxy}
}

func (a *wsPhaseFAdapter) handle(_ context.Context, dir wsmitm.Direction, env *wsmitm.Envelope) (bool, error) {
	if a == nil || a.p == nil || a.p.config == nil || env == nil || env.Kind.IsControl() {
		return false, nil
	}
	switch dir {
	case wsmitm.DirClientToServer:
		if env.Kind != wsmitm.FrameKindRequest && !wsRequestBodyCandidate(env) {
			return false, nil
		}
		return a.handleRequest(env), nil
	case wsmitm.DirServerToClient:
		if env.Kind == wsmitm.FrameKindUnknown {
			return false, nil
		}
		return a.handleResponse(env), nil
	default:
		return false, nil
	}
}

func wsRequestBodyCandidate(env *wsmitm.Envelope) bool {
	return jsonObject(env.Body) || jsonObject(env.Request) || wsEnvelopeLooksLikeRequestBody(env)
}

// wssShadowMirror is the T254 server-state mirror running in SHADOW mode on the
// WSS path: it predicts how much of each frame the server already holds and
// records forwarded content, as telemetry only. It never mutates a frame. The
// data decides whether a mirror-backed mutation is worth building.
var wssShadowMirror = servermirror.New()

// recordShadowMirror predicts the new frame's pre-pipeline content against the
// mirror (content the server already holds, beyond the reducers that already
// ran), then records the forwarded content. Returns the prediction. Pure shadow:
// no frame is changed.
func recordShadowMirror(sessionID string, pre, forwarded []types.Message) servermirror.Report {
	if sessionID == "" {
		return servermirror.Report{}
	}
	rep := wssShadowMirror.Predict(sessionID, pre)
	wssShadowMirror.Observe(sessionID, forwarded)
	return rep
}

func (a *wsPhaseFAdapter) handleRequest(env *wsmitm.Envelope) bool {
	a.counters.requestsSeen.Add(1)
	body, replace, ok := wsRequestBody(env)
	if !ok {
		return false
	}
	a.counters.requestBodiesSeen.Add(1)
	mutated, messages, changed, l0Stats, reReadCount, meta, outputReduceStats := a.applyInputPipelineDetailed(body)
	if len(messages) > 0 {
		a.counters.requestMessagesIndexed.Add(1)
	}
	a.mu.Lock()
	a.messages = messages
	a.repdetIndex = buildRepdetIndex(messages)
	if a.toolUses == nil {
		a.toolUses = make(map[string]types.ContentBlock)
	}
	for id, use := range proxyToolUseIndex(messages) {
		a.toolUses[id] = use
	}
	a.mu.Unlock()
	// T254 server-state mirror, SHADOW only: predict referenceable content the
	// server already holds (pre-pipeline = full model intent) and record this
	// frame's forwarded content. Telemetry-only; never changes a frame.
	if sid := meta.SessionID; sid != "" {
		pre := messages
		if changed {
			pre, _, _ = extractMessages(types.CodexChatGPT, body)
		}
		if rep := recordShadowMirror(sid, pre, messages); rep.ReferenceableBlocks > 0 {
			slog.Info("wss server-state mirror shadow",
				"session", sid,
				"total_blocks", rep.Blocks,
				"referenceable_blocks", rep.ReferenceableBlocks,
				"predicted_referenceable_bytes", rep.PotentialSavedBytes)
		}
	}
	if !changed {
		a.recordRequestPlan(body, mutated, messages, l0Stats, false, meta.BypassReason, reReadCount, meta, outputReduceStats)
		return false
	}
	if err := replace(mutated); err != nil {
		a.recordRequestPlan(body, mutated, messages, l0Stats, false, "replace_failed", reReadCount, meta, outputReduceStats)
		return false
	}
	a.counters.mutations.Add(1)
	a.recordRequestPlan(body, mutated, messages, l0Stats, true, "", reReadCount, meta, outputReduceStats)
	return true
}

func (a *wsPhaseFAdapter) applyInputPipeline(body []byte) ([]byte, []types.Message, bool, proxyLayer0Stats, int) {
	out, messages, changed, l0Stats, reReadCount, _, _ := a.applyInputPipelineDetailed(body)
	return out, messages, changed, l0Stats, reReadCount
}

func (a *wsPhaseFAdapter) applyInputPipelineDetailed(body []byte) ([]byte, []types.Message, bool, proxyLayer0Stats, int, wssRequestMeta, outputreduce.Stats) {
	out := body
	var l0Stats proxyLayer0Stats
	reReadCount := 0
	var meta wssRequestMeta
	outputReduceStats := outputreduce.Stats{Profile: "wss_phasef", Reason: "disabled"}
	requestContainsToolOutput := wssBodyContainsToolOutput(out)
	messages, raw, err := extractMessages(types.CodexChatGPT, out)
	if err == nil {
		meta = wssRequestMetaFromRaw(raw)
	}
	if err == nil && len(messages) > 0 {
		requestContainsToolOutput = requestContainsToolOutput || messagesContainToolResult(messages)
		sessionID := meta.SessionID
		turnID := meta.PreviousResponseID
		a.hydrateToolUses(sessionID)
		rememberedToolUses := a.loadToolUses()
		if !wssBodyHasUserPromptInput(out) {
			a.observeWSSToolPruneUsage(sessionID, messages, rememberedToolUses)
		}
		reReadKeys, count := a.observeWSSQualityToolKeysForSession(sessionID, turnID, messages, rememberedToolUses)
		reReadCount = count
		suppressedKeys := a.restoreKeysForReReads(reReadKeys)
		a.observeWSSRecentEditsForSession(sessionID, messages, rememberedToolUses)
		if degraded, reason := a.degradedState(); degraded {
			meta.BypassReason = "wss_session_degraded_full_pass"
			meta.DebugFacts = wssRequestDebugFacts(body, body, messages, l0Stats, false, meta.BypassReason, meta, outputReduceStats)
			meta.DebugFacts["wss.degraded_reason"] = reason
			return body, messages, false, l0Stats, reReadCount, meta, outputReduceStats
		}
		statefulToolOutputMutationSafe := wssStatefulToolOutputMutationSafe(meta, requestContainsToolOutput, messages, rememberedToolUses)
		structuredMutationAllowed := true
		if wssPreviousResponseToolOutputFullPass(meta, requestContainsToolOutput, statefulToolOutputMutationSafe) {
			meta.BypassReason = "wss_previous_response_tool_output_full_pass"
			structuredMutationAllowed = false
		} else if wssToolOutputStateFullPass(meta, requestContainsToolOutput, a.p.config.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled, statefulToolOutputMutationSafe) {
			meta.BypassReason = "wss_tool_output_state_full_pass"
			structuredMutationAllowed = false
		}
		if meta.BypassReason == "wss_previous_response_tool_output_full_pass" &&
			a.p.config.Compression.OutputReduce.ReadDeltaRecentFullPassTurns > 0 {
			meta.DebugFacts = wssRequestDebugFacts(body, body, messages, l0Stats, false, meta.BypassReason, meta, outputReduceStats)
			return body, messages, false, l0Stats, reReadCount, meta, outputReduceStats
		}
		if a.p.config.Compression.OutputReduce.StaleReadAgingEnabled {
			aged, stats := staleread.AgeMessages(messages, staleread.Options{
				MinTurnGap: a.p.config.Compression.OutputReduce.StaleReadAgingMinTurnGap,
			})
			if stats.BlocksReplaced > 0 {
				if rebuilt, rebuildErr := reconstructBody(types.CodexChatGPT, out, aged); rebuildErr == nil {
					out = rebuilt
					messages = aged
					a.p.outputReduceCounters.RecordStaleReadAging(stats.BlocksReplaced, stats.BytesReplaced)
				}
			}
		}
		if a.p.config.Compression.OutputReduce.ObsoleteReadPruneEnabled {
			pruned, stats := staleread.PruneObsoleteReads(messages, staleread.ObsoleteOptions{})
			if stats.BlocksReplaced > 0 {
				if rebuilt, rebuildErr := reconstructBody(types.CodexChatGPT, out, pruned); rebuildErr == nil {
					out = rebuilt
					messages = pruned
					a.p.outputReduceCounters.RecordObsoleteReadPrune(stats.BlocksReplaced, stats.BytesReplaced)
				}
			}
		}
		chunkSettings := a.p.codexChunkDedupSettings()
		result := reduceCodexLayer0(codexLayer0Request{
			Route:                     codexLayer0RouteWSSPhaseF,
			Messages:                  messages,
			SessionID:                 sessionID,
			TurnID:                    turnID,
			RememberedToolUse:         rememberedToolUses,
			SuppressedToolKey:         suppressedKeys,
			RecentFullPassTurns:       a.p.config.Compression.OutputReduce.ReadDeltaRecentFullPassTurns,
			ChunkDedupEnabled:         chunkSettings.Enabled,
			ExplicitChunkDedup:        chunkSettings.Explicit,
			ChunkDedupProof:           chunkSettings.Proof,
			ChunkDedupMinBytes:        chunkSettings.MinBytes,
			ChunkDedupMaxRefPct:       chunkSettings.MaxRefPct,
			ChunkStore:                chunkSettings.Store,
			PolicyMode:                chunkSettings.PolicyMode,
			ArchiveRecovery:           chunkSettings.ArchiveRecovery,
			HostBudgetExceeded:        a.p.codexHostBudgetExceeded(),
			LatencyBudgetExceeded:     a.p.codexLayer0LatencyExceeded.Load(),
			StructuredMutationBlocked: !structuredMutationAllowed && !statefulToolOutputMutationSafe && !a.p.config.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled,
		})
		l0Messages, stats := result.Messages, result.Stats
		l0Stats = stats
		if stats.TokensSaved > 0 {
			if rebuilt, rebuildErr := reconstructBody(types.CodexChatGPT, out, l0Messages); rebuildErr == nil {
				out = rebuilt
				messages = l0Messages
				a.p.recordCodexLayer0Stats(stats)
				a.rememberCollapsedReadKeys(stats.ReadDeltaKeys)
			} else {
				l0Stats = stats.withoutSavings()
				a.p.recordCodexLayer0Stats(l0Stats)
			}
		} else {
			a.p.recordCodexLayer0Stats(stats)
		}
	}
	if pruned, changed := a.applyWSSToolPrune(out, messages, meta.SessionID); changed {
		out = pruned
		if refreshed, _, err := extractMessages(types.CodexChatGPT, out); err == nil {
			messages = refreshed
		}
	}
	blockOutputReduce := requestContainsToolOutput || l0Stats.BlocksModified > 0
	if injected, stats := a.applyWSSOutputReduce(out, blockOutputReduce); stats.Reason != "disabled" {
		outputReduceStats = stats
		if stats.Applied {
			out = injected
		}
		if a.p.outputReduce != nil {
			a.p.outputReduce.ObserveInjection(stats)
		}
	}
	if a.p.config.Compression.OutputReduce.StopSequencesEnabled {
		if injected, res := outstop.MergeIntoBody(types.CodexChatGPT, out); res.OK && res.AddedCount > 0 {
			out = injected
			a.p.outputReduceCounters.RecordStopSeqInjection(res.AddedCount)
		}
	}
	archiveNoteEnabled := a.p.config.Compression.OutputReduce.ArchiveRecoveryNoteEnabled || l0Stats.ChunkDedupBlocks > 0
	if a.p.reserveArchiveRecoveryNote(meta.SessionID, archiveNoteEnabled) {
		note := archiveRecoveryNoteText(a.p.config.Compression.OutputReduce.ArchiveRecoveryNoteText)
		if injected, res := beterse.Inject(types.CodexChatGPT, out, note); res.Applied {
			out = injected
		} else {
			a.p.forgetArchiveRecoveryNote(meta.SessionID)
		}
	}
	if a.p.config.Compression.OutputReduce.BeTerseHintEnabled && a.p.qualityAB != nil {
		cohort := a.p.qualityAB.Cohort(meta.SessionID)
		recordCohort := cohort
		if cohort == qualityab.CohortTreatment {
			if injected, res := beterse.Inject(types.CodexChatGPT, out, a.p.config.Compression.OutputReduce.BeTerseHintText); res.Applied {
				out = injected
				a.p.outputReduceCounters.RecordBeTerseInjection(res.Bytes)
			} else {
				recordCohort = qualityab.CohortControl
			}
		}
		a.rememberWSSQualityCohort(recordCohort)
	}
	return out, messages, !bytes.Equal(body, out), l0Stats, reReadCount, meta, outputReduceStats
}

func (a *wsPhaseFAdapter) observeWSSToolPruneUsage(sessionID string, messages []types.Message, rememberedToolUses map[string]types.ContentBlock) {
	if a == nil || a.p == nil || a.p.toolPrune == nil || !a.p.config.Compression.Tuning.ToolPruneEnabled {
		return
	}
	used := extractUsedToolNamesWithResolved(messages, rememberedToolUses)
	if len(used) == 0 {
		return
	}
	a.p.toolPrune.ObserveTurn(sessionID, used)
}

func (a *wsPhaseFAdapter) applyWSSToolPrune(body []byte, messages []types.Message, sessionID string) ([]byte, bool) {
	if a == nil || a.p == nil || a.p.toolPrune == nil || !a.p.config.Compression.Tuning.ToolPruneEnabled {
		return body, false
	}
	if sessionID == "" || !wssBodyHasUserPromptInput(body) {
		return body, false
	}
	out := body
	reattachedToolNames := []string(nil)
	if mentions := messageMentionsAnyPrunedTool(messages, a.p.toolPrune, sessionID); len(mentions) > 0 {
		defs := a.p.toolPrune.PeekPrunedDefs(sessionID, mentions)
		if reattached, n, err := toolprune.ReattachToolDefinitions(out, types.CodexChatGPT, defs); err == nil && n > 0 {
			a.p.toolPrune.ForgetPrunedDefs(sessionID, mentions)
			out = reattached
			reattachedToolNames = make([]string, 0, len(defs))
			for name := range defs {
				reattachedToolNames = append(reattachedToolNames, name)
			}
			for range n {
				a.p.toolPrune.MarkReattached()
			}
		}
	}
	toolNames, schemaSafe := toolprune.ExtractToolNamesForPruning(out, types.CodexChatGPT)
	if !schemaSafe || len(toolNames) == 0 {
		return out, !bytes.Equal(body, out)
	}
	usedToolNames := extractUsedToolNames(messages)
	usedToolNames = append(usedToolNames, reattachedToolNames...)
	a.p.toolPrune.ObserveTurn(sessionID, usedToolNames)
	decision := a.p.toolPrune.DecideWithOptions(sessionID, toolNames, toolprune.DecisionOptions{
		MinKeep:    1,
		AlwaysKeep: a.p.config.Compression.Tuning.ToolPruneAlwaysKeep,
	})
	a.p.toolPrune.MarkAlwaysKept(decision.AlwaysKept)
	if len(decision.Pruned) == 0 {
		return out, !bytes.Equal(body, out)
	}
	toPrune := make(map[string]bool, len(decision.Pruned))
	for _, name := range decision.Pruned {
		toPrune[name] = true
	}
	prunedBody, removed, err := toolprune.PruneToolDefinitions(out, types.CodexChatGPT, toPrune)
	if err != nil || len(removed) == 0 {
		return out, !bytes.Equal(body, out)
	}
	saved := tokens.ForProvider(types.CodexChatGPT).CountString(string(out)) - tokens.ForProvider(types.CodexChatGPT).CountString(string(prunedBody))
	if saved <= 0 {
		return out, !bytes.Equal(body, out)
	}
	for name, def := range removed {
		a.p.toolPrune.RememberPrunedDef(sessionID, name, def)
	}
	a.p.toolPrune.MarkPruned(saved)
	return prunedBody, true
}

func (a *wsPhaseFAdapter) applyWSSOutputReduce(body []byte, blockedByToolOutput bool) ([]byte, outputreduce.Stats) {
	if a == nil || a.p == nil || a.p.config == nil || !a.p.config.Compression.OutputReduce.Enabled || !a.p.isLayerEnabled(3) {
		return body, outputreduce.Stats{Reason: "disabled"}
	}
	if blockedByToolOutput || wssBodyContainsToolOutput(body) {
		return body, outputreduce.Stats{Reason: "disabled"}
	}
	if wssBodyHasPromptCachePrefix(body) {
		return body, outputreduce.Stats{Reason: "prompt_cache_prefix_full_pass"}
	}
	if !wssBodyHasUserPromptInput(body) {
		return body, outputreduce.Stats{Reason: "disabled"}
	}
	inputTokens := wssOutputReduceInputTokens(body)
	minTokens := a.p.config.Compression.OutputReduce.MinInputTokens
	if inputTokens < minTokens {
		return body, outputreduce.Stats{Reason: "below_min_tokens"}
	}
	taskShape := outputreduce.DetectTaskShape(types.CodexChatGPT, body)
	if taskShape == outputreduce.ShapeExactReply {
		return body, outputreduce.Stats{Profile: "wss_phasef", Reason: "exact_reply", TaskShape: taskShape}
	}
	if a.p.config.Compression.OutputReduce.ConciseChatEnabled {
		if shape, reason := outputreduce.ConciseChatEligibility(types.CodexChatGPT, body, taskShape); reason == "" {
			if inputTokens < a.p.config.Compression.OutputReduce.ConciseChatMinInputTokens {
				return body, outputreduce.Stats{Profile: string(outputreduce.ProfileConciseChat), Reason: "concise_chat_low_roi", TaskShape: shape}
			}
			hint := beterse.ConciseChatHint(a.p.config.Compression.OutputReduce.ConciseChatText)
			if injected, res := beterse.Inject(types.CodexChatGPT, body, hint); res.Applied {
				return injected, outputreduce.Stats{
					Applied:     true,
					Profile:     string(outputreduce.ProfileConciseChat),
					AddedBytes:  res.Bytes,
					AddedTokens: tokens.ForProvider(types.CodexChatGPT).CountString(hint),
					Reason:      "applied",
					TaskShape:   shape,
				}
			}
			return body, outputreduce.Stats{Profile: string(outputreduce.ProfileConciseChat), Reason: "unsupported_shape", TaskShape: shape}
		}
	}
	if reason := outputreduce.LowROISkipReason(taskShape, inputTokens); reason != "" {
		return body, outputreduce.Stats{Profile: "wss_phasef", Reason: reason, TaskShape: taskShape}
	}
	// Codex WSS Phase-F rejects some model-facing request rewrites after a
	// previously accepted turn. WSS output-shaping directive injection is an
	// experimental non-product path; keep deterministic input reducers, but do
	// not inject behavioral output directives into this websocket route.
	return body, outputreduce.Stats{Profile: "wss_phasef", Reason: "codex_wss_directive_disabled", TaskShape: taskShape}
}

func wssBodyContainsToolOutput(body []byte) bool {
	if wssBodyContainsFunctionCallOutput(body) {
		return true
	}
	messages, _, err := extractMessages(types.CodexChatGPT, body)
	if err != nil {
		return false
	}
	return messagesContainToolResult(messages)
}

func messagesContainToolResult(messages []types.Message) bool {
	for _, message := range messages {
		if message.HasToolResult() {
			return true
		}
	}
	return false
}

func wssToolOutputStateFullPass(meta wssRequestMeta, containsToolOutput bool, mutationEnabled bool, statefulMutationSafe bool) bool {
	if !containsToolOutput || mutationEnabled || statefulMutationSafe {
		return false
	}
	return meta.SessionID != "" || meta.PreviousResponseID != ""
}

func wssBodyHasPromptCachePrefix(body []byte) bool {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return false
	}
	if rawJSONString(root["prompt_cache_key"]) == "" {
		return false
	}
	if _, ok := root["instructions"]; ok {
		return true
	}
	if _, ok := root["tools"]; ok {
		return true
	}
	return false
}

func wssBodyContainsFunctionCallOutput(body []byte) bool {
	var root struct {
		Input []map[string]json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		return false
	}
	for _, item := range root.Input {
		var itemType string
		if err := json.Unmarshal(item["type"], &itemType); err == nil && itemType == "function_call_output" {
			return true
		}
	}
	return false
}

func wssBodyHasUserPromptInput(body []byte) bool {
	var root struct {
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(body, &root); err != nil || len(root.Input) == 0 {
		return false
	}
	var inputText string
	if err := json.Unmarshal(root.Input, &inputText); err == nil {
		return strings.TrimSpace(inputText) != ""
	}
	var inputItems []map[string]json.RawMessage
	if err := json.Unmarshal(root.Input, &inputItems); err != nil {
		return false
	}
	for _, item := range inputItems {
		var itemType string
		_ = json.Unmarshal(item["type"], &itemType)
		var role string
		_ = json.Unmarshal(item["role"], &role)
		if itemType == "message" && role == "user" && len(item["content"]) > 0 {
			return true
		}
	}
	return false
}

func (a *wsPhaseFAdapter) rememberWSSQualityCohort(cohort qualityab.Cohort) {
	if cohort != qualityab.CohortControl && cohort != qualityab.CohortTreatment {
		return
	}
	a.mu.Lock()
	a.qualityCohort = cohort
	a.mu.Unlock()
}

func (a *wsPhaseFAdapter) recordWSSQualityOutcome(kind wsmitm.FrameKind) {
	if a.p == nil || a.p.qualityAB == nil {
		return
	}
	a.mu.Lock()
	cohort := a.qualityCohort
	a.qualityCohort = ""
	a.mu.Unlock()
	if cohort != qualityab.CohortControl && cohort != qualityab.CohortTreatment {
		return
	}
	outcome := qualityab.OutcomeSuccess
	if kind == wsmitm.FrameKindResponseFailed || kind == wsmitm.FrameKindResponseIncomplete {
		outcome = qualityab.OutcomeUpstreamError
	}
	a.p.qualityAB.RecordOutcome(cohort, outcome)
}

func (a *wsPhaseFAdapter) observeWSSRecentEdits(body []byte, messages []types.Message, rememberedToolUses map[string]types.ContentBlock) {
	sessionID := wsCodexSessionID(body)
	a.observeWSSRecentEditsForSession(sessionID, messages, rememberedToolUses)
}

func (a *wsPhaseFAdapter) observeWSSRecentEditsForSession(sessionID string, messages []types.Message, rememberedToolUses map[string]types.ContentBlock) {
	if sessionID == "" {
		return
	}
	paths := proxyEditedPathsFromMessages(messages, rememberedToolUses)
	if len(paths) == 0 {
		return
	}
	home, err := proxyUserHomeDir()
	if err != nil {
		return
	}
	dir := sessions.DefaultHookStateDir(home)
	for _, path := range paths {
		_ = sessions.ObserveHookFile(dir, sessionID, path, "edit")
	}
}

func (a *wsPhaseFAdapter) observeWSSQualityToolKeys(body []byte, messages []types.Message, rememberedToolUses map[string]types.ContentBlock) (map[string]struct{}, int) {
	sessionID := wsCodexSessionID(body)
	turnID := wssPreviousResponseID(body)
	return a.observeWSSQualityToolKeysForSession(sessionID, turnID, messages, rememberedToolUses)
}

func (a *wsPhaseFAdapter) observeWSSQualityToolKeysForSession(sessionID, turnID string, messages []types.Message, rememberedToolUses map[string]types.ContentBlock) (map[string]struct{}, int) {
	if sessionID == "" || a == nil || a.p == nil {
		return nil, 0
	}
	toolUses := proxyToolUseIndex(messages)
	for id, use := range rememberedToolUses {
		if _, ok := toolUses[id]; !ok {
			toolUses[id] = use
		}
	}
	seen := make(map[string]struct{})
	reReadKeys := make(map[string]struct{})
	reReads := 0
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type != "tool_result" {
				continue
			}
			use, _ := proxyResolveToolUseDetailed(block, toolUses)
			commandLine := proxyLayer0CommandLine(use)
			key := proxyLayer0QualityToolKeyForUse(use, commandLine)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			if a.p.ObserveQualityToolKeyForTurn(sessionID, turnID, key) {
				reReadKeys[key] = struct{}{}
				reReads++
			}
		}
	}
	return reReadKeys, reReads
}

func (a *wsPhaseFAdapter) rememberCollapsedReadKeys(keys []string) {
	if a == nil || len(keys) == 0 {
		return
	}
	a.mu.Lock()
	if a.collapsedKeys == nil {
		a.collapsedKeys = make(map[string]struct{}, len(keys))
	}
	for _, key := range keys {
		if key != "" {
			a.collapsedKeys[key] = struct{}{}
		}
	}
	sid := a.sessionID
	a.mu.Unlock()
	// Persist so the re-read full-pass recovery survives a WSS socket reconnect:
	// a re-read on a fresh adapter rehydrates the collapsed key and loosens.
	a.persistCollapsedKeys(sid, keys)
}

func (a *wsPhaseFAdapter) collapsedKeysDir() string {
	home, err := proxyUserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return toolusecache.CollapsedKeysDir(home)
}

// hydrateCollapsedKeys loads the persisted collapsed read keys for the session
// into the per-socket set so a re-read after a reconnect is still recognized as
// a post-collapse re-read and full-passes (recovers the elided bodies).
func (a *wsPhaseFAdapter) hydrateCollapsedKeys(sessionID string) {
	if a == nil || sessionID == "" {
		return
	}
	dir := a.collapsedKeysDir()
	if dir == "" {
		return
	}
	loaded, err := toolusecache.Load(dir, sessionID)
	if err != nil || len(loaded) == 0 {
		return
	}
	a.mu.Lock()
	if a.collapsedKeys == nil {
		a.collapsedKeys = make(map[string]struct{}, len(loaded))
	}
	for key := range loaded {
		if key != "" {
			a.collapsedKeys[key] = struct{}{}
		}
	}
	a.mu.Unlock()
}

// persistCollapsedKeys writes the collapsed read keys for the session to disk.
// The key itself is the payload (stored as Entry.ToolUseID); no tool output is
// persisted. Best-effort; a persistence error never affects the stream.
func (a *wsPhaseFAdapter) persistCollapsedKeys(sessionID string, keys []string) {
	if a == nil || sessionID == "" || len(keys) == 0 {
		return
	}
	dir := a.collapsedKeysDir()
	if dir == "" {
		return
	}
	add := make(map[string]toolusecache.Entry, len(keys))
	for _, key := range keys {
		if key != "" {
			add[key] = toolusecache.Entry{ToolUseID: key}
		}
	}
	if len(add) == 0 {
		return
	}
	_, _ = toolusecache.MergeAsync(dir, sessionID, add)
}

func (a *wsPhaseFAdapter) restoreKeysForReReads(reReadKeys map[string]struct{}) map[string]struct{} {
	if a == nil || len(reReadKeys) == 0 {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.collapsedKeys) == 0 {
		return nil
	}
	out := make(map[string]struct{})
	for key := range reReadKeys {
		if _, collapsed := a.collapsedKeys[key]; collapsed {
			out[key] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (a *wsPhaseFAdapter) recordRequestPlan(body []byte, mutated []byte, messages []types.Message, l0Stats proxyLayer0Stats, replaced bool, bypassReason string, reReadCount int, meta wssRequestMeta, outputReduceStats outputreduce.Stats) {
	if a == nil || a.p == nil || a.p.debugRecorder == nil {
		return
	}
	originalTokens, finalTokens := wssPlannerTokenCounts(body, mutated, messages, l0Stats, replaced)
	saved := originalTokens - finalTokens
	ratio := 0.0
	if originalTokens > 0 {
		ratio = float64(finalTokens) / float64(originalTokens)
	}
	classes := wssPlannerContentClasses(messages, l0Stats)
	layersApplied := []int(nil)
	if replaced && l0Stats.TokensSaved > 0 {
		layersApplied = []int{0}
	}
	outputReduceSummary := dbg.OutputReduceSummary{
		Applied:     outputReduceStats.Applied,
		Profile:     outputReduceStats.Profile,
		Reason:      outputReduceStats.Reason,
		AddedTokens: outputReduceStats.AddedTokens,
		TaskShape:   string(outputReduceStats.TaskShape),
	}
	if outputReduceSummary.Profile == "" {
		outputReduceSummary.Profile = "wss_phasef"
	}
	if outputReduceSummary.Reason == "" {
		outputReduceSummary.Reason = "disabled"
	}
	debugFacts := wssRequestDebugFacts(body, mutated, messages, l0Stats, replaced, bypassReason, meta, outputReduceStats)
	for k, v := range meta.DebugFacts {
		debugFacts[k] = v
	}
	summary := dbg.RequestSummary{
		RequestID:              newRequestIDFn(),
		Timestamp:              time.Now(),
		SessionID:              meta.SessionID,
		TurnID:                 meta.PreviousResponseID,
		Source:                 "proxy",
		Provider:               types.CodexChatGPT.String(),
		Path:                   "/backend-api/codex/responses",
		ClientFamily:           firstNonEmpty(meta.ClientFamily, "codex"),
		RouteMode:              "websocket_phasef",
		BypassReason:           bypassReason,
		Model:                  meta.Model,
		TotalMessages:          len(messages),
		MessagesInWindow:       len(messages),
		MessagesCompressed:     l0Stats.BlocksModified,
		LayersApplied:          layersApplied,
		PreviousResponseIDUsed: meta.PreviousResponseID != "",
		Tokens: dbg.TokenCounts{
			Original:    originalTokens,
			AfterLayer0: finalTokens,
			AfterLayer1: finalTokens,
			Final:       finalTokens,
			Saved:       saved,
			Ratio:       ratio,
		},
		OutputReduce:      outputReduceSummary,
		EvidenceDecisions: l0Stats.EvidenceDecisions,
		DebugFacts:        debugFacts,
		ReReadCount:       reReadCount,
		NetSavedTokens:    saved,
		Plan: a.p.dryRunPlan(plannerInput{
			provider:                    types.CodexChatGPT,
			model:                       meta.Model,
			routeMode:                   "websocket_phasef",
			estimatedInputTokens:        originalTokens,
			contentClasses:              classes,
			previousResponseIDAvailable: meta.PreviousResponseID != "",
			webSocketShapeKnown:         len(messages) > 0,
			webSocketMutationRequested:  bypassReason == "",
			liveCorpusConfidence:        a.p.plannerLiveCorpusConfidence(),
			negativeSavingsHistory:      saved < 0,
		}),
	}
	a.p.debugRecorder.Record(summary)
	a.p.observeQuality(summary)
}

const (
	wssSourceToolResultFullPassMinBytes = 4096
	wssSafeStatusToolOutputMaxBytes     = 2 * 1024 * 1024
)

func wssPreviousResponseToolOutputFullPass(meta wssRequestMeta, requestContainsToolOutput bool, statefulMutationSafe bool) bool {
	return meta.PreviousResponseID != "" && requestContainsToolOutput && !statefulMutationSafe
}

func wssStatefulToolOutputMutationSafe(meta wssRequestMeta, requestContainsToolOutput bool, messages []types.Message, rememberedToolUses map[string]types.ContentBlock) bool {
	if !requestContainsToolOutput || (meta.SessionID == "" && meta.PreviousResponseID == "") {
		return false
	}
	toolUses := proxyToolUseIndex(messages)
	for id, use := range rememberedToolUses {
		if _, exists := toolUses[id]; !exists {
			toolUses[id] = use
		}
	}
	if len(toolUses) == 0 {
		return false
	}
	seenToolResult := false
	for _, message := range messages {
		for _, block := range message.Content {
			if block.Type != "tool_result" {
				continue
			}
			seenToolResult = true
			toolUse, resolved := proxyResolveToolUseDetailed(block, toolUses)
			if !resolved || !wssSafeStatefulStatusToolOutput(toolUse, block.Text) {
				return false
			}
		}
	}
	return seenToolResult
}

func wssSafeStatefulStatusToolOutput(toolUse types.ContentBlock, output string) bool {
	commandLine := proxyLayer0CommandLine(toolUse)
	if !wssSafeGitStatusCommand(commandLine) {
		return false
	}
	payload := output
	if _, execPayload, ok := splitCodexExecEnvelope(output); ok {
		payload = execPayload
	}
	payload = strings.TrimSpace(payload)
	if payload == "" || len(payload) > wssSafeStatusToolOutputMaxBytes {
		return false
	}
	if looksLikeSource(payload) || proxyToolResultLooksLikeSearchOutput(payload) {
		return false
	}
	return true
}

func wssSafeGitStatusCommand(commandLine string) bool {
	argv := strings.Fields(strings.TrimSpace(commandLine))
	if len(argv) < 2 {
		return false
	}
	bin := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(argv[0]), ".exe"))
	if bin != "git" && !strings.HasSuffix(bin, "/git") {
		return false
	}
	i := 1
	for i < len(argv) {
		arg := strings.TrimSpace(argv[i])
		switch {
		case arg == "-C" || arg == "-c" || arg == "--git-dir" || arg == "--work-tree":
			i += 2
			continue
		case strings.HasPrefix(arg, "--git-dir="), strings.HasPrefix(arg, "--work-tree="), strings.HasPrefix(arg, "-c"):
			i++
			continue
		case strings.HasPrefix(arg, "-"):
			i++
			continue
		}
		break
	}
	if i >= len(argv) || strings.ToLower(argv[i]) != "status" {
		return false
	}
	i++
	hasCompactStatusFlag := false
	for i < len(argv) {
		arg := strings.ToLower(strings.TrimSpace(argv[i]))
		switch {
		case arg == "--short" || arg == "-s" || arg == "--porcelain" || strings.HasPrefix(arg, "--porcelain="):
			hasCompactStatusFlag = true
		case arg == "--branch" || arg == "-b" ||
			arg == "--ignored" || strings.HasPrefix(arg, "--ignored=") ||
			arg == "--renames" || arg == "--no-renames" || strings.HasPrefix(arg, "--find-renames") ||
			arg == "--ahead-behind" || arg == "--no-ahead-behind" ||
			strings.HasPrefix(arg, "--untracked-files="):
		case arg == "--untracked-files":
			i++
			if i >= len(argv) {
				return false
			}
			value := strings.ToLower(strings.TrimSpace(argv[i]))
			if value != "all" && value != "normal" && value != "no" {
				return false
			}
		default:
			return false
		}
		i++
	}
	return hasCompactStatusFlag
}

func wssRiskyPreviousResponseSourceToolOutput(meta wssRequestMeta, messages []types.Message) bool {
	_, maxBytes := wssSourceToolResultBytes(messages)
	return meta.PreviousResponseID != "" && maxBytes >= wssSourceToolResultFullPassMinBytes
}

func wssSourceToolResultBytes(messages []types.Message) (totalBytes int, maxBytes int) {
	for _, message := range messages {
		for _, block := range message.Content {
			if block.Type == "tool_result" && looksLikeSource(block.Text) {
				size := len(block.Text)
				totalBytes += size
				if size > maxBytes {
					maxBytes = size
				}
			}
		}
	}
	return totalBytes, maxBytes
}

func wssMessageShapeCounts(messages []types.Message) (toolResults int, sourceToolResults int, toolUses int) {
	for _, message := range messages {
		for _, block := range message.Content {
			switch block.Type {
			case "tool_result":
				toolResults++
				if looksLikeSource(block.Text) {
					sourceToolResults++
				}
			case "tool_use":
				toolUses++
			}
		}
	}
	return toolResults, sourceToolResults, toolUses
}

func wssRequestDebugFacts(body []byte, mutated []byte, messages []types.Message, l0Stats proxyLayer0Stats, replaced bool, bypassReason string, meta wssRequestMeta, outputReduceStats outputreduce.Stats) map[string]string {
	toolResults, sourceToolResults, toolUses := wssMessageShapeCounts(messages)
	sourceToolBytes, sourceToolMaxBytes := wssSourceToolResultBytes(messages)
	facts := map[string]string{
		"wss.original_bytes":         strconv.Itoa(len(body)),
		"wss.final_bytes":            strconv.Itoa(len(mutated)),
		"wss.changed":                strconv.FormatBool(replaced || !bytes.Equal(body, mutated)),
		"wss.previous_response_id":   strconv.FormatBool(meta.PreviousResponseID != ""),
		"wss.messages":               strconv.Itoa(len(messages)),
		"wss.tool_results":           strconv.Itoa(toolResults),
		"wss.source_tool_results":    strconv.Itoa(sourceToolResults),
		"wss.source_tool_bytes":      strconv.Itoa(sourceToolBytes),
		"wss.source_tool_max_bytes":  strconv.Itoa(sourceToolMaxBytes),
		"wss.tool_uses":              strconv.Itoa(toolUses),
		"wss.layer0_blocks_modified": strconv.Itoa(l0Stats.BlocksModified),
		"wss.layer0_tokens_saved":    strconv.Itoa(l0Stats.TokensSaved),
		"wss.output_reduce_applied":  strconv.FormatBool(outputReduceStats.Applied),
		"wss.output_reduce_added":    strconv.Itoa(outputReduceStats.AddedTokens),
		"wss.output_reduce_reason":   outputReduceStats.Reason,
		"wss.replace_applied":        strconv.FormatBool(replaced),
		"wss.session_id_present":     strconv.FormatBool(meta.SessionID != ""),
	}
	if bypassReason != "" {
		facts["wss.bypass_reason"] = bypassReason
	}
	if facts["wss.output_reduce_reason"] == "" {
		facts["wss.output_reduce_reason"] = "disabled"
	}
	return facts
}

func wssPlannerContentClasses(messages []types.Message, l0Stats proxyLayer0Stats) []string {
	classes := append([]string{"websocket"}, plannerClassesFromMessages(messages)...)
	if l0Stats.ReadDeltaBlocks > 0 || l0Stats.RepeatedOutputBlocks > 0 || l0Stats.ChunkDedupBlocks > 0 {
		classes = append(classes, "repeated_tool_output")
	}
	if l0Stats.ChunkDedupBlocks > 0 {
		classes = append(classes, "chunk_dedup")
	}
	return classes
}

func wssPlannerTokenCount(body []byte, messages []types.Message) int {
	// Codex bills in o200k_base; count planner telemetry with the same encoding.
	tok := tokens.ForProvider(types.CodexChatGPT)
	if len(messages) > 0 {
		return tok.CountMessages(messages)
	}
	return tok.CountString(string(body))
}

func wssPlannerTokenCounts(body []byte, mutated []byte, messages []types.Message, l0Stats proxyLayer0Stats, replaced bool) (int, int) {
	if replaced || l0Stats.TokensSaved > 0 {
		originalMessages, _, _ := extractMessages(types.CodexChatGPT, body)
		return wssPlannerTokenCount(body, originalMessages), wssPlannerTokenCount(mutated, messages)
	}
	// No local mutation means this debug/planner row cannot claim savings.
	// Use a cheap estimate so a no-op WSS request does not load the heavy
	// o200k encoder only to write "saved=0" telemetry.
	estimated := tokens.Estimate(len(body))
	return estimated, estimated
}

func wssOutputReduceInputTokens(body []byte) int {
	// Output-reduce gating only needs a conservative workload-size signal. Exact
	// o200k accounting is still used for real Layer-0 savings claims; here the
	// estimate keeps default WSS user turns from loading the BPE tables before
	// any mutation is known to be useful.
	return tokens.Estimate(len(body))
}

func wssPlannerModel(body []byte) string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return ""
	}
	return wssPlannerModelFromRaw(raw)
}

func wssPreviousResponseIDAvailable(body []byte) bool {
	return wssPreviousResponseID(body) != ""
}

func wssPreviousResponseID(body []byte) string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return ""
	}
	return wssPreviousResponseIDFromRaw(raw)
}

func (a *wsPhaseFAdapter) handleResponse(env *wsmitm.Envelope) bool {
	a.recordWSSUpstreamError(env)
	a.rememberToolUsesFromResponse(env)
	a.recordWSSProviderUsage(env)
	if env.Kind.IsTextDelta() {
		a.counters.responseTextDeltasSeen.Add(1)
	}
	if env.Kind.IsTerminal() {
		a.counters.terminalResponsesSeen.Add(1)
		a.recordWSSQualityOutcome(env.Kind)
	}
	mutated := false
	if a.applyRepdetDelta(env) {
		mutated = true
	}
	if a.applyRepdetResponse(env) {
		mutated = true
	}
	if mutated {
		a.counters.mutations.Add(1)
	}
	return mutated
}

func (a *wsPhaseFAdapter) recordWSSUpstreamError(env *wsmitm.Envelope) {
	if a == nil || a.p == nil || a.p.debugRecorder == nil || env == nil {
		return
	}
	if env.Kind != wsmitm.FrameKindError && env.Kind != wsmitm.FrameKindResponseFailed && env.Kind != wsmitm.FrameKindResponseIncomplete {
		return
	}
	status, errorType, message := wssUpstreamErrorFields(env)
	errSummary := formatWSSUpstreamError(env.Kind, status, errorType, message)
	if errSummary == "" {
		errSummary = "upstream_error kind=" + string(env.Kind)
	}
	a.markDegraded(errSummary)
	slog.Warn("codex wss upstream error", "kind", env.Kind, "status", status, "error_type", errorType)
	a.p.debugRecorder.Record(dbg.RequestSummary{
		RequestID:    newRequestIDFn(),
		Timestamp:    time.Now(),
		SessionID:    a.currentSessionID(),
		Source:       "proxy",
		Provider:     types.CodexChatGPT.String(),
		Path:         "/backend-api/codex/responses",
		ClientFamily: "codex",
		RouteMode:    "websocket_phasef",
		BypassReason: "upstream_error",
		Errors:       []string{errSummary},
	})
}

func (a *wsPhaseFAdapter) markDegraded(reason string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.degraded = true
	a.degradedReason = reason
	a.mu.Unlock()
}

func (a *wsPhaseFAdapter) degradedState() (bool, string) {
	if a == nil {
		return false, ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.degraded, a.degradedReason
}

func (a *wsPhaseFAdapter) currentSessionID() string {
	if a == nil {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessionID
}

func wssUpstreamErrorFields(env *wsmitm.Envelope) (string, string, string) {
	if env == nil {
		return "", "", ""
	}
	status := wssStatusField(env.Fields["status"])
	errorType, message := wssErrorObjectFields(env.Fields["error"])
	if env.Kind == wsmitm.FrameKindResponseFailed || env.Kind == wsmitm.FrameKindResponseIncomplete {
		responseType, responseMessage := wssResponseErrorFields(env.Response)
		if errorType == "" {
			errorType = responseType
		}
		if message == "" {
			message = responseMessage
		}
	}
	return status, errorType, message
}

func wssStatusField(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var numeric int
	if err := json.Unmarshal(raw, &numeric); err == nil {
		return strings.TrimSpace(string(raw))
	}
	return strings.TrimSpace(rawJSONString(raw))
}

func wssErrorObjectFields(raw json.RawMessage) (string, string) {
	if len(raw) == 0 {
		return "", ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", rawJSONString(raw)
	}
	return strings.TrimSpace(rawJSONString(fields["type"])), strings.TrimSpace(rawJSONString(fields["message"]))
}

func wssResponseErrorFields(raw json.RawMessage) (string, string) {
	if len(raw) == 0 {
		return "", ""
	}
	var response map[string]json.RawMessage
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", ""
	}
	return wssErrorObjectFields(response["error"])
}

func formatWSSUpstreamError(kind wsmitm.FrameKind, status string, errorType string, message string) string {
	parts := []string{"upstream_error kind=" + string(kind)}
	if status != "" {
		parts = append(parts, "status="+status)
	}
	if errorType != "" {
		parts = append(parts, "type="+errorType)
	}
	if message != "" {
		parts = append(parts, "message="+truncateWSSUpstreamErrorMessage(message))
	}
	return strings.Join(parts, " ")
}

func truncateWSSUpstreamErrorMessage(message string) string {
	message = strings.TrimSpace(strings.ReplaceAll(message, "\n", " "))
	if len(message) <= 240 {
		return message
	}
	return message[:240] + "..."
}

func (a *wsPhaseFAdapter) recordWSSProviderUsage(env *wsmitm.Envelope) {
	if a == nil || a.p == nil || env == nil || env.Kind != wsmitm.FrameKindResponseCompleted || len(env.Response) == 0 {
		return
	}
	usage := extractOpenAICacheUsageFromBody(env.Response)
	if usage.InputTokens <= 0 && usage.OutputTokens <= 0 && usage.ReadTokens <= 0 && usage.CreateTokens <= 0 {
		return
	}
	if a.p.outputReduce != nil {
		a.p.outputReduce.ObserveOutput(usage.OutputTokens)
	}
	a.p.trySendAnalytics(types.AnalyticsEvent{
		Type:              types.EventRequestProcessed,
		Timestamp:         time.Now(),
		Provider:          types.CodexChatGPT,
		InputTokensOrig:   usage.InputTokens,
		InputTokensComp:   usage.InputTokens,
		OutputTokens:      usage.OutputTokens,
		CompressionRatio:  1,
		CacheHit:          usage.ReadTokens > 0,
		CacheReadTokens:   usage.ReadTokens,
		CacheCreateTokens: usage.CreateTokens,
	})
}

func (a *wsPhaseFAdapter) rememberToolUsesFromResponse(env *wsmitm.Envelope) {
	if a == nil || env == nil {
		return
	}
	defer a.persistToolUses()
	a.rememberToolUseItem(env.Item)
	if len(env.Response) == 0 {
		return
	}
	var response map[string]json.RawMessage
	if err := json.Unmarshal(env.Response, &response); err != nil {
		return
	}
	var output []json.RawMessage
	if err := json.Unmarshal(response["output"], &output); err != nil {
		return
	}
	for _, item := range output {
		a.rememberToolUseItem(item)
	}
}

func (a *wsPhaseFAdapter) rememberToolUseItem(raw json.RawMessage) {
	if a == nil || len(raw) == 0 {
		return
	}
	msg, ok, err := codexInputItemToMessage(0, raw)
	if err != nil || !ok {
		return
	}
	toolUses := proxyToolUseIndex([]types.Message{msg})
	if len(toolUses) == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.toolUses == nil {
		a.toolUses = make(map[string]types.ContentBlock, len(toolUses))
	}
	for id, use := range toolUses {
		a.toolUses[id] = use
	}
}

func (a *wsPhaseFAdapter) toolUseCacheDir() string {
	home, err := proxyUserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return toolusecache.DefaultDir(home)
}

// hydrateToolUses loads the persisted call_id -> command-metadata map for the
// session once per adapter, so cross-turn read-delta resolution survives a WSS
// socket reconnect. The in-memory toolUses map is per-socket and resets on
// reconnect; without rehydration a later re-read would resolve to no command and
// the read-delta saving would not fire.
func (a *wsPhaseFAdapter) hydrateToolUses(sessionID string) {
	if a == nil || sessionID == "" {
		return
	}
	a.mu.Lock()
	if a.toolUseHydrated && a.sessionID == sessionID {
		a.mu.Unlock()
		return
	}
	a.sessionID = sessionID
	a.toolUseHydrated = true
	a.mu.Unlock()

	// Rehydrate collapsed read keys independently of the tool-use map so the
	// re-read full-pass recovery survives a reconnect even when no tool-use
	// metadata is cached yet.
	a.hydrateCollapsedKeys(sessionID)

	dir := a.toolUseCacheDir()
	if dir == "" {
		return
	}
	loaded, err := toolusecache.Load(dir, sessionID)
	if err != nil || len(loaded) == 0 {
		return
	}
	a.mu.Lock()
	if a.toolUses == nil {
		a.toolUses = make(map[string]types.ContentBlock, len(loaded))
	}
	for id, e := range loaded {
		if _, exists := a.toolUses[id]; !exists {
			a.toolUses[id] = types.ContentBlock{Type: e.Type, ToolUseID: e.ToolUseID, ToolName: e.ToolName, ToolInput: e.ToolInput}
		}
	}
	a.mu.Unlock()
}

// persistToolUses writes the adapter's current call_id -> command-metadata map to
// disk for the session. Content-free: only tool name + command arguments, never
// tool output. Best-effort; a persistence error never affects the stream.
func (a *wsPhaseFAdapter) persistToolUses() {
	if a == nil {
		return
	}
	a.mu.Lock()
	sid := a.sessionID
	add := make(map[string]toolusecache.Entry, len(a.toolUses))
	for id, use := range a.toolUses {
		add[id] = toolusecache.Entry{ToolUseID: use.ToolUseID, ToolName: use.ToolName, ToolInput: use.ToolInput, Type: use.Type}
	}
	a.mu.Unlock()
	if sid == "" || len(add) == 0 {
		return
	}
	dir := a.toolUseCacheDir()
	if dir == "" {
		return
	}
	_, _ = toolusecache.MergeAsync(dir, sid, add)
	// Opportunistically bound the cache directory (once every 64 writes) so it
	// cannot grow without limit across many conversations.
	if tooluseSaveCount.Add(1)%64 == 0 {
		_, _ = toolusecache.Prune(dir, 0, 0)
	}
}

// tooluseSaveCount gates the opportunistic toolusecache prune.
var tooluseSaveCount atomic.Uint64

func (a *wsPhaseFAdapter) applyRepdetDelta(env *wsmitm.Envelope) bool {
	if !a.p.config.Compression.OutputReduce.RepetitionDetectionEnabled || !env.Kind.IsTextDelta() || env.Delta == "" {
		return false
	}
	idx := a.loadRepdetIndex()
	if idx == nil || len(idx.Blocks()) == 0 {
		return false
	}
	rewritten, matches := idx.Rewrite(env.Delta)
	if len(matches) == 0 {
		return false
	}
	saved := 0
	for _, m := range matches {
		saved += m.Length
	}
	env.Delta = rewritten
	a.p.outputReduceCounters.RecordRepdetRewrite(len(matches), saved)
	return true
}

func (a *wsPhaseFAdapter) applyRepdetResponse(env *wsmitm.Envelope) bool {
	// WSS response.completed frames are the terminal client-visible answer. Delta
	// frames already carry any safe output-wire repdet rewrite; rewriting the
	// terminal aggregate too double-counts savings and can corrupt final code or
	// patch text. Keep terminal WSS responses byte-equal until a separate
	// terminal-safe proof exists.
	return false
}

func (a *wsPhaseFAdapter) loadRepdetIndex() *repdet.Index {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.repdetIndex
}

func (a *wsPhaseFAdapter) loadToolUses() map[string]types.ContentBlock {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.toolUses) == 0 {
		return nil
	}
	out := make(map[string]types.ContentBlock, len(a.toolUses))
	for id, use := range a.toolUses {
		out[id] = use
	}
	return out
}

type wsRequestReplacer func([]byte) error

func wsRequestBody(env *wsmitm.Envelope) ([]byte, wsRequestReplacer, bool) {
	if jsonObject(env.Body) {
		return env.Body, func(next []byte) error {
			env.Body = append(json.RawMessage(nil), next...)
			if env.Fields != nil {
				env.Fields["body"] = append(json.RawMessage(nil), next...)
			}
			return nil
		}, true
	}
	if jsonObject(env.Request) {
		return env.Request, func(next []byte) error {
			env.Request = append(json.RawMessage(nil), next...)
			if env.Fields != nil {
				env.Fields["request"] = append(json.RawMessage(nil), next...)
			}
			return nil
		}, true
	}
	if wsEnvelopeLooksLikeRequestBody(env) {
		return env.Raw, func(next []byte) error {
			parsed, err := wsmitm.Parse(next)
			if err != nil {
				return err
			}
			*env = parsed
			return nil
		}, true
	}
	return nil, nil, false
}

func wsEnvelopeLooksLikeRequestBody(env *wsmitm.Envelope) bool {
	if !jsonObject(env.Raw) {
		return false
	}
	_, hasInput := env.Fields["input"]
	_, hasMessages := env.Fields["messages"]
	if hasInput || hasMessages {
		return true
	}
	_, hasModel := env.Fields["model"]
	_, hasStream := env.Fields["stream"]
	return hasModel && hasStream
}

func jsonObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

func wsCodexSessionID(body []byte) string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return ""
	}
	return wssCodexSessionIDFromRaw(raw)
}

func wssRequestMetaFromRaw(raw map[string]json.RawMessage) wssRequestMeta {
	if len(raw) == 0 {
		return wssRequestMeta{}
	}
	return wssRequestMeta{
		SessionID:          wssCodexSessionIDFromRaw(raw),
		PreviousResponseID: wssPreviousResponseIDFromRaw(raw),
		Model:              wssPlannerModelFromRaw(raw),
		ClientFamily:       wssCodexClientFamilyFromRaw(raw),
	}
}

func wssCodexClientFamilyFromRaw(raw map[string]json.RawMessage) string {
	for _, source := range []json.RawMessage{raw["client_metadata"], raw["metadata"]} {
		if family := codexMetadataClientFamily(source); family != "" {
			return family
		}
	}
	return "codex"
}

func codexMetadataClientFamily(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ""
	}
	for _, key := range []string{"client_family", "client", "source", "origin"} {
		if family := normalizeCodexClientFamily(rawJSONString(fields[key])); family != "" {
			return family
		}
	}
	turnRaw := rawJSONString(fields["x-codex-turn-metadata"])
	if turnRaw == "" {
		return ""
	}
	var turn map[string]json.RawMessage
	if err := json.Unmarshal([]byte(turnRaw), &turn); err != nil {
		return ""
	}
	for _, key := range []string{"client_family", "client", "source", "origin", "thread_source"} {
		if family := normalizeCodexClientFamily(rawJSONString(turn[key])); family != "" {
			return family
		}
	}
	return ""
}

func normalizeCodexClientFamily(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case strings.Contains(value, "cli"):
		return "codex_cli"
	case strings.HasPrefix(value, "codex/"), strings.HasPrefix(value, "codex "):
		return "codex_cli"
	case strings.Contains(value, "desktop"), strings.Contains(value, "app"), strings.Contains(value, "chatgpt"):
		return "codex_desktop_app"
	default:
		return ""
	}
}

func wssPlannerModelFromRaw(raw map[string]json.RawMessage) string {
	return rawJSONString(raw["model"])
}

func wssPreviousResponseIDFromRaw(raw map[string]json.RawMessage) string {
	return rawJSONString(raw["previous_response_id"])
}

func wssCodexSessionIDFromRaw(raw map[string]json.RawMessage) string {
	// Prefer Codex's explicit thread/session metadata over prompt_cache_key. The
	// prompt cache key can be stable for a shared instruction prefix, while the
	// turn metadata is the narrower readcache namespace when present.
	if s := codexStrongThreadSessionID(raw); s != "" {
		return s
	}
	if s := rawJSONString(raw["prompt_cache_key"]); s != "" {
		return "codex-wss:" + s
	}
	return ""
}

// codexTurnMetadataSessionID pulls the stable thread/session id out of Codex's
// `client_metadata.x-codex-turn-metadata` (a JSON string).
func codexTurnMetadataSessionID(clientMetadata json.RawMessage) string {
	if len(clientMetadata) == 0 {
		return ""
	}
	var cm map[string]json.RawMessage
	if err := json.Unmarshal(clientMetadata, &cm); err != nil {
		return ""
	}
	turnRaw := rawJSONString(cm["x-codex-turn-metadata"])
	if turnRaw == "" {
		return ""
	}
	var turn map[string]json.RawMessage
	if err := json.Unmarshal([]byte(turnRaw), &turn); err != nil {
		return ""
	}
	for _, key := range []string{"thread_id", "session_id"} {
		if s := rawJSONString(turn[key]); s != "" {
			return s
		}
	}
	return ""
}

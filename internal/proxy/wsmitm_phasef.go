package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/beterse"
	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/evidence"
	"github.com/Christopher-Schulze/Slimference/internal/filter"
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

	mu                                sync.Mutex
	messages                          []types.Message
	repdetIndex                       *repdet.Index
	toolUses                          map[string]types.ContentBlock
	sessionID                         string
	degraded                          bool
	degradedReason                    string
	toolUseHydrated                   bool
	collapsedKeys                     map[string]struct{}
	qualityCohort                     qualityab.Cohort
	responseChains                    map[string]wssResponseChain
	pendingChain                      wssResponseChain
	pendingOutput                     []json.RawMessage
	pendingRecovery                   *wssRecoveryCandidate
	activeRecovery                    *wssRecoveryCandidate
	recoveryAccepted                  bool
	recoveryResponseID                string
	recoveryWriter                    func([]byte) error
	historyRecoveryGuarded            bool
	historyRecoveryGuardedResponseIDs map[string]struct{}
	pendingHistoryRecoveryGuarded     bool
	historyStatelessMode              bool
	// lastDecisionRequestID correlates the turn's response usage frame with
	// the decision record written at request time (T352-A attribution).
	lastDecisionRequestID      string
	lastUsageSessionID         string
	lastUsageMutatedMechanisms proxyLayer0MechanismMask
	cacheBustSessions          map[string]*wssProviderCacheBustSession
	sessionTurnSeq             map[string]int
	counters                   wsPhaseFCounters
	socketSeq                  atomic.Uint64
	socketDecisionRequestID    string
}

const (
	wssProviderCacheBustRingSize      = 8
	wssProviderCacheBustWarmupTurns   = 3
	wssProviderCacheBustDropThreshold = 0.30
	wssProviderCacheBustMinPrevShare  = 0.50
)

type wssProviderCacheBustSample struct {
	cachedShare       float64
	mutatedMechanisms proxyLayer0MechanismMask
}

type wssProviderCacheBustSession struct {
	samples [wssProviderCacheBustRingSize]wssProviderCacheBustSample
	next    int
	count   int
	seen    int
	demoted proxyLayer0MechanismMask
}

type wssProviderCacheBustEvent struct {
	Fired           bool
	Trigger         proxyLayer0MechanismMask
	Demoted         proxyLayer0MechanismMask
	PreviousShare   float64
	CurrentShare    float64
	ObservedSamples int
}

func (s *wssProviderCacheBustSession) observe(cachedShare float64, mutatedMechanisms proxyLayer0MechanismMask) wssProviderCacheBustEvent {
	event := wssProviderCacheBustEvent{
		Demoted:         s.demoted,
		CurrentShare:    cachedShare,
		ObservedSamples: s.seen + 1,
	}
	if previous, ok := s.last(); ok {
		event.PreviousShare = previous.cachedShare
		if event.ObservedSamples >= wssProviderCacheBustWarmupTurns &&
			previous.mutatedMechanisms != 0 &&
			previous.cachedShare >= wssProviderCacheBustMinPrevShare &&
			cachedShare < previous.cachedShare-wssProviderCacheBustDropThreshold {
			s.demoted |= previous.mutatedMechanisms
			event.Fired = true
			event.Trigger = previous.mutatedMechanisms
			event.Demoted = s.demoted
		}
	}
	s.samples[s.next] = wssProviderCacheBustSample{
		cachedShare:       cachedShare,
		mutatedMechanisms: mutatedMechanisms,
	}
	s.next = (s.next + 1) % wssProviderCacheBustRingSize
	if s.count < wssProviderCacheBustRingSize {
		s.count++
	}
	s.seen++
	return event
}

func (s *wssProviderCacheBustSession) last() (wssProviderCacheBustSample, bool) {
	if s.count == 0 {
		return wssProviderCacheBustSample{}, false
	}
	idx := s.next - 1
	if idx < 0 {
		idx = wssProviderCacheBustRingSize - 1
	}
	return s.samples[idx], true
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
	SessionID              string
	PreviousResponseID     string
	Model                  string
	ClientFamily           string
	SocketSeq              uint64
	TurnSeq                int
	RemainingTurnsEstimate int
	HasUserPromptInput     bool
	HasToolDefinitions     bool
	HasPromptCachePrefix   bool
	OriginalMessages       []types.Message
	ToolUseIndex           map[string]types.ContentBlock
	RepdetIndex            *repdet.Index
	ToolPrune              dbg.ToolPruneSummary
	BypassReason           string
	DebugFacts             map[string]string
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

func (a *wsPhaseFAdapter) setSocketSeq(seq uint64) {
	if a != nil && seq > 0 {
		a.socketSeq.Store(seq)
	}
}

func (a *wsPhaseFAdapter) observeWSSRequestTurnSeq(sessionID string) int {
	if a == nil || sessionID == "" {
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sessionTurnSeq == nil {
		a.sessionTurnSeq = make(map[string]int)
	}
	a.sessionTurnSeq[sessionID]++
	return a.sessionTurnSeq[sessionID]
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
		return a.handleResponse(env)
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

func attachShadowMirrorDebugFacts(meta *wssRequestMeta, rep servermirror.Report) {
	if meta == nil || (rep.Blocks == 0 && rep.NormalizedSegments == 0) {
		return
	}
	if meta.DebugFacts == nil {
		meta.DebugFacts = make(map[string]string)
	}
	meta.DebugFacts["wss.shadow_mirror_blocks"] = strconv.Itoa(rep.Blocks)
	meta.DebugFacts["wss.shadow_mirror_bytes"] = strconv.Itoa(rep.BlockBytes)
	meta.DebugFacts["wss.shadow_mirror_referenceable_blocks"] = strconv.Itoa(rep.ReferenceableBlocks)
	meta.DebugFacts["wss.shadow_mirror_referenceable_bytes"] = strconv.Itoa(rep.PotentialSavedBytes)
	meta.DebugFacts["wss.shadow_mirror_normalized_segments"] = strconv.Itoa(rep.NormalizedSegments)
	meta.DebugFacts["wss.shadow_mirror_normalized_bytes"] = strconv.Itoa(rep.NormalizedBytes)
	meta.DebugFacts["wss.shadow_mirror_normalized_referenceable_segments"] = strconv.Itoa(rep.NormalizedReferenceableSegments)
	meta.DebugFacts["wss.shadow_mirror_normalized_referenceable_bytes"] = strconv.Itoa(rep.NormalizedPotentialSavedBytes)
	if byKind := formatShadowMirrorKindReport(rep.NormalizedPotentialSavedBytesByKind); byKind != "" {
		meta.DebugFacts["wss.shadow_mirror_normalized_by_kind"] = byKind
	}
	if byKind := formatShadowMirrorKindDensityReport(rep.NormalizedPotentialSavedBytesByKind); byKind != "" {
		meta.DebugFacts["wss.shadow_mirror_normalized_density_by_kind"] = byKind
	}
}

func formatShadowMirrorKindReport(byKind map[string]servermirror.SegmentKindReport) string {
	if len(byKind) == 0 {
		return ""
	}
	keys := make([]string, 0, len(byKind))
	for key := range byKind {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		row := byKind[key]
		parts = append(parts, fmt.Sprintf("%s=%d/%d/%d", key, row.PotentialSavedBytes, row.ReferenceableSegments, row.Segments))
	}
	return strings.Join(parts, ",")
}

func formatShadowMirrorKindDensityReport(byKind map[string]servermirror.SegmentKindReport) string {
	if len(byKind) == 0 {
		return ""
	}
	keys := make([]string, 0, len(byKind))
	for key := range byKind {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		row := byKind[key]
		parts = append(parts, fmt.Sprintf("%s=%d/%d/%d/%d", key, row.PotentialSavedBytes, row.Bytes, row.ReferenceableSegments, row.Segments))
	}
	return strings.Join(parts, ",")
}

func (a *wsPhaseFAdapter) handleRequest(env *wsmitm.Envelope) bool {
	a.counters.requestsSeen.Add(1)
	body, replace, ok := wsRequestBody(env)
	if !ok {
		return false
	}
	a.counters.requestBodiesSeen.Add(1)
	mutated, messages, changed, l0Stats, reReadCount, meta, outputReduceStats := a.applyInputPipelineDetailed(body)
	recoveryBody := body
	if changed {
		recoveryBody = mutated
	}
	a.prepareWSSRecoveryCandidate(env, recoveryBody, meta)
	if len(messages) > 0 {
		a.counters.requestMessagesIndexed.Add(1)
	}
	toolUses := meta.ToolUseIndex
	if toolUses == nil && len(messages) > 0 {
		toolUses = proxyToolUseIndex(messages)
	}
	repdetIndex := meta.RepdetIndex
	if repdetIndex == nil && a.p.config.Compression.OutputReduce.RepetitionDetectionEnabled && len(messages) > 0 {
		repdetIndex = buildRepdetIndex(messages)
	}
	a.mu.Lock()
	a.messages = messages
	a.repdetIndex = repdetIndex
	if a.toolUses == nil {
		a.toolUses = make(map[string]types.ContentBlock)
	}
	for id, use := range toolUses {
		a.toolUses[id] = use
	}
	a.mu.Unlock()
	// T254 server-state mirror, SHADOW only: predict referenceable content the
	// server already holds (pre-pipeline = full model intent) and record this
	// frame's forwarded content. Telemetry-only; never changes a frame.
	if sid := meta.SessionID; sid != "" {
		pre := messages
		if changed && len(meta.OriginalMessages) > 0 {
			pre = meta.OriginalMessages
		}
		if rep := recordShadowMirror(sid, pre, messages); rep.Blocks > 0 || rep.NormalizedSegments > 0 {
			attachShadowMirrorDebugFacts(&meta, rep)
			if rep.ReferenceableBlocks > 0 || rep.NormalizedReferenceableSegments > 0 {
				slog.Info("wss server-state mirror shadow",
					"session", sid,
					"total_blocks", rep.Blocks,
					"referenceable_blocks", rep.ReferenceableBlocks,
					"predicted_referenceable_bytes", rep.PotentialSavedBytes,
					"normalized_segments", rep.NormalizedSegments,
					"normalized_referenceable_segments", rep.NormalizedReferenceableSegments,
					"normalized_predicted_referenceable_bytes", rep.NormalizedPotentialSavedBytes)
			}
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
	statelessHistoryContinuation := false
	if rewritten, ok := a.wssStatelessHistoryContinuationBody(out); ok {
		out = rewritten
		statelessHistoryContinuation = true
	}
	var l0Stats proxyLayer0Stats
	reReadCount := 0
	var meta wssRequestMeta
	outputReduceStats := outputreduce.Stats{Profile: "wss_phasef", Reason: "disabled"}
	requestContainsToolOutput := false
	toolPruneAppliedInMessagePath := false
	messages, raw, err := extractMessagesFn(types.CodexChatGPT, out)
	if err == nil {
		meta = wssRequestMetaFromRaw(raw)
		meta.SocketSeq = a.socketSeq.Load()
		meta.OriginalMessages = messages
		if statelessHistoryContinuation {
			if meta.DebugFacts == nil {
				meta.DebugFacts = make(map[string]string)
			}
			meta.DebugFacts["wss.stateless_history_continuation"] = "true"
		}
	} else {
		requestContainsToolOutput = wssBodyContainsFunctionCallOutput(out)
	}
	if err == nil && len(messages) > 0 {
		requestContainsToolOutput = messagesContainToolResult(messages)
		sessionID := meta.SessionID
		meta.TurnSeq = a.observeWSSRequestTurnSeq(sessionID)
		meta.RemainingTurnsEstimate = a.p.codexFootprintRemainingTurns("wss_phasef", meta.TurnSeq)
		turnID := meta.PreviousResponseID
		a.hydrateToolUses(sessionID)
		rememberedToolUses := a.loadToolUses()
		currentToolUses, requestRepdetIndex := wssRequestIndexes(messages, a.p.config.Compression.OutputReduce.RepetitionDetectionEnabled)
		mergedToolUses := mergedProxyToolUseIndex(currentToolUses, rememberedToolUses)
		meta.ToolUseIndex = mergedToolUses
		meta.RepdetIndex = requestRepdetIndex
		if !meta.HasUserPromptInput {
			a.observeWSSToolPruneUsageWithToolUses(sessionID, messages, mergedToolUses)
		}
		reReadKeys, count := a.observeWSSQualityToolKeysForSessionWithToolUses(sessionID, turnID, messages, mergedToolUses)
		reReadCount = count
		suppressedKeys := a.restoreKeysForReReads(reReadKeys)
		a.observeWSSRecentEditsForSessionWithToolUses(sessionID, messages, mergedToolUses)
		if degraded, reason := a.degradedState(); degraded {
			meta.BypassReason = "wss_session_degraded_full_pass"
			meta.DebugFacts = wssRequestDebugFacts(body, body, messages, l0Stats, false, meta.BypassReason, meta, outputReduceStats)
			meta.DebugFacts["wss.degraded_reason"] = reason
			return body, messages, false, l0Stats, reReadCount, meta, outputReduceStats
		}
		toolOutputResults, toolOutputResolved, toolOutputInferred := wssToolOutputResolutionStatsWithToolUses(messages, mergedToolUses)
		toolOutputKnown := toolOutputResults > 0 && toolOutputResolved+toolOutputInferred == toolOutputResults
		statefulToolOutputMutationSafe := wssStatefulToolOutputMutationSafeWithToolUses(meta, requestContainsToolOutput, messages, mergedToolUses)
		chunkSettings := a.p.codexChunkDedupSettings()
		deltaShape := wssRequestIsDeltaShape(messages)
		// 2026-06-11 live A/B: archive-backed structured mutations on
		// previous_response_id delta-shaped turns poison server state and
		// surface as a follow-up 400. Later full-history history-reducer
		// captures showed the same downstream-delta failure class. Capture L
		// then proved reconnect full-history search mutation can poison the
		// following turn too. Keep first-socket full-history structured
		// mutations eligible for archive-backed savings. History-only reducers
		// on full-history continuations are safe because a mutated chain enters
		// stateless full-history continuation mode before the next tool output.
		requestShape := wssRequestShape(meta, messages)
		historyMutationRecoveryGuarded := a.wssHistoryMutationRecoveryGuarded(meta.PreviousResponseID)
		a.rememberWSSHistoryRecoveryGuardRequest(historyMutationRecoveryGuarded)
		fullHistoryDownstreamStateMutationBlocked := meta.SocketSeq > 0 && requestShape == "full_history"
		fullHistoryHistoryMutationBlocked := false
		reconnectFullHistoryToolOutputMutationBlocked := meta.SocketSeq > 1 && requestShape == "full_history"
		structuredMutationRecoverable := wssStructuredMutationRecoverable(requestContainsToolOutput, toolOutputKnown, deltaShape)
		structuredMutationAllowed := true
		structuredMutationGuardReason := ""
		cacheBustDemoted := a.wssCacheBustDemotedMechanisms(sessionID)
		if cacheBustDemoted != 0 {
			if meta.DebugFacts == nil {
				meta.DebugFacts = make(map[string]string)
			}
			meta.DebugFacts["wss.cache_bust_demoted_mechanisms"] = cacheBustDemoted.String()
		}
		if wssPreviousResponseUnknownToolOutputFullPass(meta, requestContainsToolOutput, statefulToolOutputMutationSafe, toolOutputKnown) {
			historyMutationGuardReason := ""
			if meta.PreviousResponseID != "" && deltaShape {
				historyMutationGuardReason = "wss_stateful_delta_mutation_proof_gate"
			} else if historyMutationRecoveryGuarded && requestShape == "full_history" {
				historyMutationGuardReason = "wss_recovery_history_mutation_guard"
			} else if fullHistoryHistoryMutationBlocked {
				historyMutationGuardReason = "wss_full_history_downstream_delta_proof_gate"
			}
			if deltaShape {
				observedStats := a.observeWSSPreviousResponseDeltaLayer0(messages, mergedToolUses, sessionID, turnID, suppressedKeys, chunkSettings, meta, cacheBustDemoted)
				l0Stats = mergeWSSLayer0ObservationStats(l0Stats, observedStats)
			}
			historyResult := a.applyWSSHistoryReducers(out, messages, historyMutationGuardReason, cacheBustDemoted, meta.TurnSeq)
			l0Stats = mergeWSSHistoryReducerStats(l0Stats, historyResult.Stats)
			changed := false
			detachedPreviousResponseID := false
			if historyResult.Mutated {
				if rebuilt, rebuildErr := reconstructBodyFn(types.CodexChatGPT, out, historyResult.Messages); rebuildErr == nil {
					out = rebuilt
					if requestShape == "full_history" && meta.PreviousResponseID != "" {
						if detached, detachedOK := detachCodexPreviousResponseID(out); detachedOK {
							out = detached
							detachedPreviousResponseID = true
							a.markWSSHistoryStatelessMode()
						}
					}
					messages = historyResult.Messages
					changed = true
					if historyResult.StaleBlocksReplaced > 0 {
						a.p.outputReduceCounters.RecordStaleReadAging(historyResult.StaleBlocksReplaced, historyResult.StaleBytesReplaced)
					}
					if historyResult.ObsoleteBlocksPruned > 0 {
						a.p.outputReduceCounters.RecordObsoleteReadPrune(historyResult.ObsoleteBlocksPruned, historyResult.ObsoleteBytesPruned)
					}
					a.p.recordCodexLayer0Stats(l0Stats)
				} else {
					l0Stats = l0Stats.withoutSavings()
					a.p.recordCodexLayer0Stats(l0Stats)
				}
			} else if proxyLayer0StatsHasTelemetry(l0Stats) {
				a.p.recordCodexLayer0Stats(l0Stats)
			}
			meta.BypassReason = "wss_previous_response_tool_output_full_pass"
			if changed {
				meta.BypassReason = "wss_previous_response_history_only"
			}
			meta.DebugFacts = wssRequestDebugFacts(body, out, messages, l0Stats, changed, meta.BypassReason, meta, outputReduceStats)
			if detachedPreviousResponseID {
				meta.DebugFacts["wss.full_history_detached_previous_response"] = "true"
			}
			if historyMutationRecoveryGuarded && requestShape == "full_history" {
				meta.DebugFacts["wss.history_mutation_recovery_guard"] = "true"
			}
			meta.DebugFacts["wss.tool_results_resolved"] = strconv.Itoa(toolOutputResolved)
			meta.DebugFacts["wss.tool_results_inferred"] = strconv.Itoa(toolOutputInferred)
			meta.DebugFacts["wss.tool_results_total"] = strconv.Itoa(toolOutputResults)
			return out, messages, changed, l0Stats, reReadCount, meta, outputReduceStats
		} else if requestContainsToolOutput && reconnectFullHistoryToolOutputMutationBlocked && !a.p.config.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled {
			structuredMutationAllowed = false
			structuredMutationGuardReason = "wss_full_history_downstream_delta_proof_gate"
		} else if wssToolOutputStructuredMutationBlocked(meta, requestContainsToolOutput, a.p.config.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled, statefulToolOutputMutationSafe, structuredMutationRecoverable) {
			structuredMutationAllowed = false
			structuredMutationGuardReason = "wss_stateful_structured_mutation_guard"
		}
		deltaMutationLabEnabled := a.p.config.Compression.OutputReduce.CodexWSSDeltaToolOutputMutationLabEnabled
		statefulDeltaMutationBlocked := meta.PreviousResponseID != "" &&
			deltaShape &&
			!deltaMutationLabEnabled
		historyMutationGuardReason := ""
		if statefulDeltaMutationBlocked {
			historyMutationGuardReason = "wss_stateful_delta_mutation_proof_gate"
		} else if historyMutationRecoveryGuarded && requestShape == "full_history" {
			historyMutationGuardReason = "wss_recovery_history_mutation_guard"
		} else if fullHistoryHistoryMutationBlocked {
			historyMutationGuardReason = "wss_full_history_downstream_delta_proof_gate"
		}
		downstreamStateMutationGuardReason := ""
		if statefulDeltaMutationBlocked {
			downstreamStateMutationGuardReason = "wss_stateful_delta_mutation_proof_gate"
		} else if fullHistoryDownstreamStateMutationBlocked {
			downstreamStateMutationGuardReason = "wss_full_history_downstream_delta_proof_gate"
		}
		effectiveMutationGuardReason := structuredMutationGuardReason
		if statefulDeltaMutationBlocked {
			effectiveMutationGuardReason = "wss_stateful_delta_mutation_proof_gate"
		}
		stagedMessages := messages
		messageMutationPending := false
		historyStats := proxyLayer0Stats{Route: codexLayer0RouteWSSPhaseF}
		staleBlocksReplaced := 0
		staleBytesReplaced := 0
		obsoleteBlocksPruned := 0
		obsoleteBytesPruned := 0
		if a.p.config.Compression.OutputReduce.StaleReadAgingEnabled {
			staleGuardReason := ""
			if historyMutationGuardReason != "" {
				staleGuardReason = historyMutationGuardReason
			} else if cacheBustDemoted.Has(proxyLayer0MechanismStaleRead) {
				staleGuardReason = "cache_bust_guard"
			}
			if staleGuardReason != "" {
				aged, stats := staleread.AgeMessages(stagedMessages, staleread.Options{
					MinTurnGap: a.p.config.Compression.OutputReduce.StaleReadAgingMinTurnGap,
				})
				if stats.BlocksReplaced > 0 {
					beforeTokens := wssPlannerTokenCount(out, stagedMessages)
					afterTokens := wssPlannerTokenCount(out, aged)
					historyStats.EvidenceDecisions = append(historyStats.EvidenceDecisions, proxyHistoryMutationEvidenceDecision(proxyLayer0MechanismStaleRead, evidence.ActionFullPass, staleGuardReason, beforeTokens, afterTokens, meta.TurnSeq, a.p.config.Savings.CachedPriceRatio))
				}
			} else {
				beforeTokens := 0
				afterTokens := 0
				aged, stats := staleread.AgeMessages(stagedMessages, staleread.Options{
					MinTurnGap: a.p.config.Compression.OutputReduce.StaleReadAgingMinTurnGap,
				})
				if stats.BlocksReplaced > 0 {
					beforeTokens = wssPlannerTokenCount(out, stagedMessages)
					afterTokens = wssPlannerTokenCount(out, aged)
					stagedMessages = aged
					messageMutationPending = true
					staleBlocksReplaced = stats.BlocksReplaced
					staleBytesReplaced = stats.BytesReplaced
					historyStats.StaleReadBlocks = stats.BlocksReplaced
					historyStats.StaleReadBytesSaved = stats.BytesReplaced
					historyStats.StaleReadTokensSaved = beforeTokens - afterTokens
					historyStats.EvidenceDecisions = append(historyStats.EvidenceDecisions, proxyHistoryMutationEvidenceDecision(proxyLayer0MechanismStaleRead, evidence.ActionApplied, "positive_net_savings", beforeTokens, afterTokens, meta.TurnSeq, a.p.config.Savings.CachedPriceRatio))
				}
			}
		}
		if a.p.config.Compression.OutputReduce.ObsoleteReadPruneEnabled {
			obsoleteGuardReason := ""
			if historyMutationGuardReason != "" {
				obsoleteGuardReason = historyMutationGuardReason
			} else if cacheBustDemoted.Has(proxyLayer0MechanismObsoletePrune) {
				obsoleteGuardReason = "cache_bust_guard"
			}
			if obsoleteGuardReason != "" {
				pruned, stats := staleread.PruneObsoleteReads(stagedMessages, staleread.ObsoleteOptions{})
				if stats.BlocksReplaced > 0 {
					beforeTokens := wssPlannerTokenCount(out, stagedMessages)
					afterTokens := wssPlannerTokenCount(out, pruned)
					historyStats.EvidenceDecisions = append(historyStats.EvidenceDecisions, proxyHistoryMutationEvidenceDecision(proxyLayer0MechanismObsoletePrune, evidence.ActionFullPass, obsoleteGuardReason, beforeTokens, afterTokens, meta.TurnSeq, a.p.config.Savings.CachedPriceRatio))
				}
			} else {
				beforeTokens := 0
				afterTokens := 0
				pruned, stats := staleread.PruneObsoleteReads(stagedMessages, staleread.ObsoleteOptions{})
				if stats.BlocksReplaced > 0 {
					beforeTokens = wssPlannerTokenCount(out, stagedMessages)
					afterTokens = wssPlannerTokenCount(out, pruned)
					stagedMessages = pruned
					messageMutationPending = true
					obsoleteBlocksPruned = stats.BlocksReplaced
					obsoleteBytesPruned = stats.BytesReplaced
					historyStats.ObsoletePruneBlocks = stats.BlocksReplaced
					historyStats.ObsoletePruneBytesSaved = stats.BytesReplaced
					historyStats.ObsoletePruneTokensSaved = beforeTokens - afterTokens
					historyStats.EvidenceDecisions = append(historyStats.EvidenceDecisions, proxyHistoryMutationEvidenceDecision(proxyLayer0MechanismObsoletePrune, evidence.ActionApplied, "positive_net_savings", beforeTokens, afterTokens, meta.TurnSeq, a.p.config.Savings.CachedPriceRatio))
				}
			}
		}
		searchCapDeltaProofed := a.p.config.Compression.OutputReduce.CodexSearchCapDeltaMutationEnabled
		result := reduceCodexLayer0(codexLayer0Request{
			Route:                   codexLayer0RouteWSSPhaseF,
			Messages:                stagedMessages,
			ToolUseIndex:            mergedToolUses,
			SessionID:               sessionID,
			TurnID:                  turnID,
			SuppressedToolKey:       suppressedKeys,
			RecentFullPassTurns:     a.p.config.Compression.OutputReduce.ReadDeltaRecentFullPassTurns,
			ChunkDedupEnabled:       chunkSettings.Enabled,
			ExplicitChunkDedup:      chunkSettings.Explicit,
			ChunkDedupProof:         chunkSettings.Proof,
			ChunkDedupMinBytes:      chunkSettings.MinBytes,
			ChunkDedupMaxRefPct:     chunkSettings.MaxRefPct,
			ChunkStore:              chunkSettings.Store,
			PolicyMode:              chunkSettings.PolicyMode,
			ArchiveRecovery:         chunkSettings.ArchiveRecovery,
			TurnSeq:                 meta.TurnSeq,
			RemainingTurnsEstimate:  meta.RemainingTurnsEstimate,
			CachedPriceRatio:        a.p.config.Savings.CachedPriceRatio,
			UniformChunkDedupBudget: a.p.wssABReplayUniformChunkBudget,
			SearchCompactOptions: filter.SearchCompactOptions{
				MaxFilesShown:     a.p.config.Compression.OutputReduce.CodexSearchCapMaxFiles,
				MaxMatchesPerFile: a.p.config.Compression.OutputReduce.CodexSearchCapMaxMatchesPerFile,
			},
			HostBudgetExceeded:        a.p.codexHostBudgetExceeded(),
			LatencyBudgetExceeded:     a.p.codexLayer0LatencyExceeded.Load(),
			StructuredMutationBlocked: !structuredMutationAllowed && !statefulToolOutputMutationSafe && !a.p.config.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled,
			WSSSearchMutationAllowed: (structuredMutationAllowed || searchCapDeltaProofed) &&
				(!statefulDeltaMutationBlocked || searchCapDeltaProofed) &&
				(a.p.config.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled || structuredMutationRecoverable || searchCapDeltaProofed),
			CacheBustDemotedMechanisms:   cacheBustDemoted,
			HistoryMutationGuardReason:   downstreamStateMutationGuardReason,
			StatefulDeltaMutationBlocked: statefulDeltaMutationBlocked,
		})
		l0Messages, stats := result.Messages, result.Stats
		l0Stats = stats
		l0Stats = mergeWSSHistoryReducerStats(l0Stats, historyStats)
		if stats.TokensSaved > 0 {
			stagedMessages = l0Messages
			messageMutationPending = true
		}
		toolPruneAppliedInMessagePath = true
		if pruned, changed, toolPrune := a.applyWSSToolPrune(out, stagedMessages, meta); toolPrune.GuardReason != "" {
			meta.ToolPrune = toolPrune.Summary
			if meta.DebugFacts == nil {
				meta.DebugFacts = make(map[string]string)
			}
			meta.DebugFacts["wss.tool_prune_guard"] = toolPrune.GuardReason
		} else if changed {
			meta.ToolPrune = toolPrune.Summary
			out = pruned
		} else {
			meta.ToolPrune = toolPrune.Summary
		}
		if messageMutationPending {
			if rebuilt, rebuildErr := reconstructBodyFn(types.CodexChatGPT, out, stagedMessages); rebuildErr == nil {
				out = rebuilt
				messages = stagedMessages
				if a.p.config.Compression.OutputReduce.RepetitionDetectionEnabled {
					meta.RepdetIndex = buildRepdetIndex(stagedMessages)
				}
				if staleBlocksReplaced > 0 {
					a.p.outputReduceCounters.RecordStaleReadAging(staleBlocksReplaced, staleBytesReplaced)
				}
				if obsoleteBlocksPruned > 0 {
					a.p.outputReduceCounters.RecordObsoleteReadPrune(obsoleteBlocksPruned, obsoleteBytesPruned)
				}
				a.p.recordCodexLayer0Stats(stats)
				if stats.TokensSaved > 0 {
					a.rememberCollapsedReadKeys(stats.ReadDeltaKeys)
				}
			} else if stats.TokensSaved > 0 {
				l0Stats = stats.withoutSavings()
				a.p.recordCodexLayer0Stats(l0Stats)
			} else {
				a.p.recordCodexLayer0Stats(stats)
			}
		} else {
			a.p.recordCodexLayer0Stats(stats)
		}
		if structuredMutationGuardReason != "" {
			if meta.DebugFacts == nil {
				meta.DebugFacts = make(map[string]string)
			}
			meta.DebugFacts["wss.structured_mutation_guard"] = structuredMutationGuardReason
		}
		if effectiveMutationGuardReason != "" {
			if meta.DebugFacts == nil {
				meta.DebugFacts = make(map[string]string)
			}
			meta.DebugFacts["wss.effective_mutation_guard"] = effectiveMutationGuardReason
		}
		if historyMutationGuardReason != "" {
			if meta.DebugFacts == nil {
				meta.DebugFacts = make(map[string]string)
			}
			meta.DebugFacts["wss.history_mutation_guard"] = historyMutationGuardReason
		}
		if downstreamStateMutationGuardReason != "" && downstreamStateMutationGuardReason != historyMutationGuardReason {
			if meta.DebugFacts == nil {
				meta.DebugFacts = make(map[string]string)
			}
			meta.DebugFacts["wss.downstream_state_mutation_guard"] = downstreamStateMutationGuardReason
		}
		if statefulDeltaMutationBlocked {
			if meta.DebugFacts == nil {
				meta.DebugFacts = make(map[string]string)
			}
			meta.DebugFacts["wss.stateful_delta_mutation_blocked"] = "true"
		}
		if toolOutputResults > 0 {
			if meta.DebugFacts == nil {
				meta.DebugFacts = make(map[string]string)
			}
			meta.DebugFacts["wss.tool_results_resolved"] = strconv.Itoa(toolOutputResolved)
			meta.DebugFacts["wss.tool_results_inferred"] = strconv.Itoa(toolOutputInferred)
			meta.DebugFacts["wss.tool_results_total"] = strconv.Itoa(toolOutputResults)
		}
	}
	if !toolPruneAppliedInMessagePath {
		if pruned, changed, toolPrune := a.applyWSSToolPrune(out, messages, meta); toolPrune.GuardReason != "" {
			meta.ToolPrune = toolPrune.Summary
			if meta.DebugFacts == nil {
				meta.DebugFacts = make(map[string]string)
			}
			meta.DebugFacts["wss.tool_prune_guard"] = toolPrune.GuardReason
		} else if changed {
			meta.ToolPrune = toolPrune.Summary
			out = pruned
		} else {
			meta.ToolPrune = toolPrune.Summary
		}
	}
	blockOutputReduce := requestContainsToolOutput || l0Stats.BlocksModified > 0
	toolOutputPresenceKnown := err == nil
	if injected, stats := a.applyWSSOutputReduce(out, blockOutputReduce, toolOutputPresenceKnown, toolOutputPresenceKnown, meta.HasUserPromptInput, toolOutputPresenceKnown, meta.HasPromptCachePrefix); stats.Reason != "disabled" {
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
	toolUses := mergedProxyToolUseIndex(proxyToolUseIndex(messages), rememberedToolUses)
	a.observeWSSToolPruneUsageWithToolUses(sessionID, messages, toolUses)
}

func (a *wsPhaseFAdapter) observeWSSToolPruneUsageWithToolUses(sessionID string, messages []types.Message, toolUses map[string]types.ContentBlock) {
	if a == nil || a.p == nil || a.p.toolPrune == nil || !a.p.config.Compression.Tuning.ToolPruneEnabled {
		return
	}
	used := extractUsedToolNamesWithResolvedToolUses(messages, toolUses)
	if len(used) == 0 {
		return
	}
	a.p.toolPrune.ObserveTurn(sessionID, used)
}

type wssToolPruneResult struct {
	Summary     dbg.ToolPruneSummary
	GuardReason string
}

func (a *wsPhaseFAdapter) applyWSSToolPrune(body []byte, messages []types.Message, meta wssRequestMeta) ([]byte, bool, wssToolPruneResult) {
	if a == nil || a.p == nil || a.p.toolPrune == nil || !a.p.config.Compression.Tuning.ToolPruneEnabled {
		return body, false, wssToolPruneResult{}
	}
	sessionID := meta.SessionID
	if sessionID == "" || !meta.HasUserPromptInput {
		return body, false, wssToolPruneResult{}
	}
	summary := dbg.ToolPruneSummary{
		Reason:        "no_tools",
		SessionKeySet: true,
	}
	out := body
	reattachedToolNames := []string(nil)
	mentions := messageMentionsAnyPrunedTool(messages, a.p.toolPrune, sessionID)
	if reason := wssToolPruneMutationGuardReason(messages, meta, mentions); reason != "" {
		a.observeWSSToolPruneUsageWithToolUses(sessionID, messages, meta.ToolUseIndex)
		summary.Reason = reason
		return body, false, wssToolPruneResult{Summary: summary, GuardReason: reason}
	}
	if len(mentions) > 0 {
		defs := a.p.toolPrune.PeekPrunedDefs(sessionID, mentions)
		if reattached, n, err := toolprune.ReattachToolDefinitions(out, types.CodexChatGPT, defs); err == nil && n > 0 {
			a.p.toolPrune.ForgetPrunedDefs(sessionID, mentions)
			out = reattached
			summary.Reattached += n
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
		if !schemaSafe {
			summary.Reason = "unknown_tool_schema_full_pass"
		}
		return out, !bytes.Equal(body, out), wssToolPruneResult{Summary: summary}
	}
	usedToolNames := extractUsedToolNamesWithResolvedToolUses(messages, meta.ToolUseIndex)
	usedToolNames = append(usedToolNames, reattachedToolNames...)
	a.p.toolPrune.ObserveTurn(sessionID, usedToolNames)
	decision := a.p.toolPrune.DecideWithOptions(sessionID, toolNames, toolprune.DecisionOptions{
		MinKeep:    1,
		AlwaysKeep: a.p.config.Compression.Tuning.ToolPruneAlwaysKeep,
	})
	summary.Reason = decision.Reason
	summary.AlwaysKept = decision.AlwaysKept
	summary.Cooldown = decision.Reason == "quality_cooldown"
	a.p.toolPrune.MarkAlwaysKept(decision.AlwaysKept)
	if len(decision.Pruned) == 0 {
		return out, !bytes.Equal(body, out), wssToolPruneResult{Summary: summary}
	}
	toPrune := make(map[string]bool, len(decision.Pruned))
	for _, name := range decision.Pruned {
		toPrune[name] = true
	}
	prunedBody, removed, err := toolprune.PruneToolDefinitions(out, types.CodexChatGPT, toPrune)
	if err != nil || len(removed) == 0 {
		return out, !bytes.Equal(body, out), wssToolPruneResult{Summary: summary}
	}
	saved := tokens.ForProvider(types.CodexChatGPT).CountString(string(out)) - tokens.ForProvider(types.CodexChatGPT).CountString(string(prunedBody))
	if saved <= 0 {
		return out, !bytes.Equal(body, out), wssToolPruneResult{Summary: summary}
	}
	for name, def := range removed {
		a.p.toolPrune.RememberPrunedDef(sessionID, name, def)
	}
	a.p.toolPrune.MarkPruned(saved)
	summary.Applied = true
	summary.PrunedTools = len(removed)
	summary.SavedTokens = saved
	return prunedBody, true, wssToolPruneResult{Summary: summary}
}

func wssToolPruneMutationGuardReason(messages []types.Message, meta wssRequestMeta, reattachMentions []string) string {
	if meta.PreviousResponseID == "" {
		return ""
	}
	if !meta.HasToolDefinitions && len(reattachMentions) == 0 {
		return ""
	}
	if len(messages) > 0 && !wssRequestIsDeltaShape(messages) {
		return ""
	}
	return "wss_tool_prune_delta_guard"
}

func (a *wsPhaseFAdapter) applyWSSOutputReduce(body []byte, blockedByToolOutput bool, toolOutputPresenceKnown bool, userPromptInputKnown bool, hasUserPromptInput bool, promptCachePrefixKnown bool, hasPromptCachePrefix bool) ([]byte, outputreduce.Stats) {
	if a == nil || a.p == nil || a.p.config == nil || !a.p.config.Compression.OutputReduce.Enabled || !a.p.isLayerEnabled(3) {
		return body, outputreduce.Stats{Reason: "disabled"}
	}
	if blockedByToolOutput || (!toolOutputPresenceKnown && wssBodyContainsToolOutputFn(body)) {
		return body, outputreduce.Stats{Reason: "disabled"}
	}
	if promptCachePrefixKnown {
		if hasPromptCachePrefix {
			return body, outputreduce.Stats{Reason: "prompt_cache_prefix_full_pass"}
		}
	} else if wssBodyHasPromptCachePrefixFn(body) {
		return body, outputreduce.Stats{Reason: "prompt_cache_prefix_full_pass"}
	}
	if userPromptInputKnown {
		if !hasUserPromptInput {
			return body, outputreduce.Stats{Reason: "disabled"}
		}
	} else if !wssBodyHasUserPromptInputFn(body) {
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

var wssBodyContainsToolOutputFn = wssBodyContainsToolOutput

func messagesContainToolResult(messages []types.Message) bool {
	for _, message := range messages {
		if message.HasToolResult() {
			return true
		}
	}
	return false
}

func wssRequestIndexes(messages []types.Message, includeRepdet bool) (map[string]types.ContentBlock, *repdet.Index) {
	toolUses := make(map[string]types.ContentBlock)
	var repdetIndex *repdet.Index
	if includeRepdet {
		repdetIndex = repdet.NewIndex()
	}
	for _, msg := range messages {
		for _, block := range msg.Content {
			switch block.Type {
			case "tool_use":
				if block.ToolUseID != "" {
					toolUses[block.ToolUseID] = block
				}
			case "tool_result":
				addRepdetToolResultBlock(repdetIndex, block)
			}
		}
	}
	return toolUses, repdetIndex
}

func wssToolOutputStructuredMutationBlocked(meta wssRequestMeta, containsToolOutput bool, mutationEnabled bool, statefulMutationSafe bool, recoverable bool) bool {
	if !containsToolOutput || mutationEnabled || statefulMutationSafe || recoverable {
		return false
	}
	return meta.SessionID != "" || meta.PreviousResponseID != ""
}

func wssStructuredMutationRecoverable(containsToolOutput bool, toolOutputKnown bool, deltaShape bool) bool {
	return containsToolOutput && toolOutputKnown && !deltaShape
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

var wssBodyHasPromptCachePrefixFn = wssBodyHasPromptCachePrefix

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
	return wssInputHasUserPromptInput(root.Input)
}

var wssBodyHasUserPromptInputFn = wssBodyHasUserPromptInput

func wssRawHasUserPromptInput(raw map[string]json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	return wssInputHasUserPromptInput(raw["input"])
}

func wssInputHasUserPromptInput(input json.RawMessage) bool {
	if len(input) == 0 {
		return false
	}
	var inputText string
	if err := json.Unmarshal(input, &inputText); err == nil {
		return strings.TrimSpace(inputText) != ""
	}
	var inputItems []map[string]json.RawMessage
	if err := json.Unmarshal(input, &inputItems); err != nil {
		return false
	}
	for _, item := range inputItems {
		var itemType string
		_ = json.Unmarshal(item["type"], &itemType)
		var role string
		_ = json.Unmarshal(item["role"], &role)
		if itemType == "message" && role == "user" && wssContentHasUserPromptText(item["content"]) {
			return true
		}
	}
	return false
}

func wssContentHasUserPromptText(content json.RawMessage) bool {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 {
		return false
	}
	if trimmed[0] == '"' {
		return strings.TrimSpace(rawJSONString(content)) != ""
	}
	var parts []map[string]json.RawMessage
	if err := json.Unmarshal(content, &parts); err != nil {
		return false
	}
	for _, part := range parts {
		switch rawJSONString(part["type"]) {
		case "input_text", "text":
			if strings.TrimSpace(rawJSONString(part["text"])) != "" {
				return true
			}
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
	toolUses := mergedProxyToolUseIndex(proxyToolUseIndex(messages), rememberedToolUses)
	a.observeWSSRecentEditsForSessionWithToolUses(sessionID, messages, toolUses)
}

func (a *wsPhaseFAdapter) observeWSSRecentEditsForSessionWithToolUses(sessionID string, messages []types.Message, toolUses map[string]types.ContentBlock) {
	if sessionID == "" {
		return
	}
	paths := proxyEditedPathsFromMessagesWithToolUses(messages, toolUses)
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
	toolUses := mergedProxyToolUseIndex(proxyToolUseIndex(messages), rememberedToolUses)
	return a.observeWSSQualityToolKeysForSessionWithToolUses(sessionID, turnID, messages, toolUses)
}

func (a *wsPhaseFAdapter) observeWSSQualityToolKeysForSessionWithToolUses(sessionID, turnID string, messages []types.Message, toolUses map[string]types.ContentBlock) (map[string]struct{}, int) {
	if sessionID == "" || a == nil || a.p == nil {
		return nil, 0
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
	originalTokens, finalTokens := wssPlannerTokenCountsWithOriginal(body, mutated, meta.OriginalMessages, messages, l0Stats, replaced)
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
	requestID := newRequestIDFn()
	mutatedMechanisms := proxyLayer0MechanismMaskFromStats(l0Stats)
	if mutatedMechanisms != 0 {
		debugFacts["wss.layer0_mutated_mechanisms"] = mutatedMechanisms.String()
	}
	a.mu.Lock()
	a.lastDecisionRequestID = requestID
	a.lastUsageSessionID = meta.SessionID
	a.lastUsageMutatedMechanisms = mutatedMechanisms
	if a.socketDecisionRequestID == "" && meta.SocketSeq > 0 {
		a.socketDecisionRequestID = requestID
	}
	a.mu.Unlock()
	summary := dbg.RequestSummary{
		RequestID:              requestID,
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
		ToolPrune:         meta.ToolPrune,
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

func (a *wsPhaseFAdapter) attachWSSSocketLifecycle(snap wsmitm.SessionTelemetry, phaseF wsPhaseFTelemetry) {
	if a == nil || a.p == nil || snap.CloseInitiator == "" {
		return
	}
	a.p.observeCodexFootprintSessionLength("wss_phasef", phaseF.TerminalResponsesSeen)
	if a.p.debugRecorder == nil {
		return
	}
	a.mu.Lock()
	requestID := a.socketDecisionRequestID
	a.mu.Unlock()
	if requestID == "" {
		return
	}
	facts := map[string]string{
		"wss.socket_closed":          "true",
		"wss.socket_close_initiator": snap.CloseInitiator,
		"wss.socket_age_ms":          strconv.FormatInt(snap.AgeMillis, 10),
		"wss.socket_c2s_frames":      strconv.FormatInt(snap.C2SFrames, 10),
		"wss.socket_s2c_frames":      strconv.FormatInt(snap.S2CFrames, 10),
		"wss.socket_c2s_bytes":       strconv.FormatInt(snap.C2SBytes, 10),
		"wss.socket_s2c_bytes":       strconv.FormatInt(snap.S2CBytes, 10),
		"wss.socket_turns_completed": strconv.FormatInt(phaseF.TerminalResponsesSeen, 10),
	}
	if snap.CloseError != "" {
		facts["wss.socket_close_error"] = snap.CloseError
	}
	a.p.debugRecorder.AttachDebugFacts(requestID, facts)
}

const (
	wssSourceToolResultFullPassMinBytes = 4096
	wssSafeStatusToolOutputMaxBytes     = 2 * 1024 * 1024
	wssSafeGitLogOnelineMaxCommits      = 200
	wssSafeListingOutputMaxBytes        = 16 * 1024
	wssSafeListingOutputMaxEntries      = 300
	wssSafeListingOutputMaxLineBytes    = 512
	wssSafeFindMaxDepth                 = 6
	wssSafeTreeMaxDepth                 = 6
)

func wssPreviousResponseUnknownToolOutputFullPass(meta wssRequestMeta, requestContainsToolOutput bool, statefulMutationSafe bool, toolOutputKnown bool) bool {
	return meta.PreviousResponseID != "" && requestContainsToolOutput && !statefulMutationSafe && !toolOutputKnown
}

func wssStatefulToolOutputMutationSafe(meta wssRequestMeta, requestContainsToolOutput bool, messages []types.Message, rememberedToolUses map[string]types.ContentBlock) bool {
	toolUses := mergedProxyToolUseIndex(proxyToolUseIndex(messages), rememberedToolUses)
	return wssStatefulToolOutputMutationSafeWithToolUses(meta, requestContainsToolOutput, messages, toolUses)
}

func wssStatefulToolOutputMutationSafeWithToolUses(meta wssRequestMeta, requestContainsToolOutput bool, messages []types.Message, toolUses map[string]types.ContentBlock) bool {
	if !requestContainsToolOutput || (meta.SessionID == "" && meta.PreviousResponseID == "") {
		return false
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

func wssToolOutputResolutionStats(messages []types.Message, rememberedToolUses map[string]types.ContentBlock) (int, int, int) {
	toolUses := mergedProxyToolUseIndex(proxyToolUseIndex(messages), rememberedToolUses)
	return wssToolOutputResolutionStatsWithToolUses(messages, toolUses)
}

func wssToolOutputResolutionStatsWithToolUses(messages []types.Message, toolUses map[string]types.ContentBlock) (int, int, int) {
	total := 0
	resolved := 0
	inferred := 0
	for _, message := range messages {
		for _, block := range message.Content {
			if block.Type != "tool_result" {
				continue
			}
			total++
			if use, ok := proxyResolveToolUseDetailed(block, toolUses); ok && proxyLayer0CommandLine(use) != "" {
				resolved++
				continue
			}
			// The reducer resolves a command class from the payload shape when
			// tool_use metadata is missing (evicted cache, reconnect, never
			// seen); a gate stricter than the reducer it guards only withholds
			// savings. Inference is deterministic per command class and the
			// structured mutation stays archive-backed regardless.
			if proxyInferCommandLineFromToolResult(block.Text) != "" {
				inferred++
			}
		}
	}
	return total, resolved, inferred
}

func wssSafeStatefulStatusToolOutput(toolUse types.ContentBlock, output string) bool {
	commandLine := proxyLayer0CommandLine(toolUse)
	payload := output
	if _, execPayload, ok := splitCodexExecEnvelope(output); ok {
		payload = execPayload
	}
	trimmedPayload := strings.TrimSpace(payload)
	if trimmedPayload == "" || len(payload) > wssSafeStatusToolOutputMaxBytes {
		return false
	}
	if looksLikeSource(trimmedPayload) || proxyToolResultLooksLikeSearchOutput(trimmedPayload) {
		return false
	}
	argv := filter.ArgvForCapturedOutput(commandLine)
	switch {
	case wssSafeGitStatusCommand(commandLine):
		_, ok := filter.TryCompactGitStatus(argv, []byte(payload))
		return ok
	case wssSafeGitDiffStatCommand(commandLine):
		_, ok := filter.TryCompactGitDiff(argv, []byte(payload))
		return ok
	case wssSafeGitDiffNameOnlyPathListOutput(commandLine, payload):
		return true
	case wssSafeGitDiffNameStatusPathListOutput(commandLine, payload):
		return true
	case wssSafeGitLogOnelineOutput(commandLine, payload):
		return true
	case wssSafeGitLsFilesPathListOutput(commandLine, payload):
		return true
	case wssSafeWcOutput(commandLine, payload):
		return true
	case wssSafeLsListingOutput(commandLine, payload):
		return true
	case wssSafeFormatPathListOutput(commandLine, payload):
		return true
	case wssSafeFindListingOutput(commandLine, payload):
		return true
	case wssSafeTreeListingOutput(commandLine, payload):
		return true
	default:
		return false
	}
}

func wssSafeGitStatusCommand(commandLine string) bool {
	argv, i, ok := wssGitSubcommand(commandLine, "status")
	if !ok {
		return false
	}
	i++
	for i < len(argv) {
		arg := strings.ToLower(strings.TrimSpace(argv[i]))
		switch {
		case arg == "--short" || arg == "-s" || arg == "--porcelain" || strings.HasPrefix(arg, "--porcelain="):
		case arg == "--branch" || arg == "-b" ||
			arg == "--ignored" || strings.HasPrefix(arg, "--ignored=") ||
			arg == "--renames" || arg == "--no-renames" || strings.HasPrefix(arg, "--find-renames") ||
			arg == "--ahead-behind" || arg == "--no-ahead-behind" ||
			strings.HasPrefix(arg, "--untracked-files="):
		case arg == "--":
			return true
		case arg == "--untracked-files":
			i++
			if i >= len(argv) {
				return false
			}
			value := strings.ToLower(strings.TrimSpace(argv[i]))
			if value != "all" && value != "normal" && value != "no" {
				return false
			}
		case strings.HasPrefix(arg, "-"):
			return false
		default:
			// Pathspecs are safe here because the captured output still has to
			// pass the strict porcelain parser before mutation is allowed.
		}
		i++
	}
	return true
}

func wssSafeGitDiffStatCommand(commandLine string) bool {
	argv, i, ok := wssGitSubcommand(commandLine, "diff")
	if !ok {
		return false
	}
	i++
	hasStat := false
	for i < len(argv) {
		arg := strings.ToLower(strings.TrimSpace(argv[i]))
		switch {
		case arg == "--stat" || strings.HasPrefix(arg, "--stat="):
			hasStat = true
		case arg == "--cached" || arg == "--staged" || arg == "--no-renames" ||
			strings.HasPrefix(arg, "--find-renames") || strings.HasPrefix(arg, "--find-copies") ||
			strings.HasPrefix(arg, "--diff-filter=") || arg == "--relative" || strings.HasPrefix(arg, "--relative="):
		case arg == "--diff-filter":
			i++
			if i >= len(argv) {
				return false
			}
		case arg == "--":
		case strings.HasPrefix(arg, "-"):
			return false
		}
		i++
	}
	return hasStat
}

func wssSafeGitDiffNameOnlyPathListOutput(commandLine, payload string) bool {
	argv := filter.ArgvForCapturedOutput(commandLine)
	if len(argv) == 0 {
		return false
	}
	if _, ok := filter.TryCompactGitDiff(argv, []byte(payload)); !ok {
		return false
	}
	return wssSafePlainPathListPayload(payload)
}

func wssSafeGitDiffNameStatusPathListOutput(commandLine, payload string) bool {
	argv := filter.ArgvForCapturedOutput(commandLine)
	if len(argv) == 0 {
		return false
	}
	if _, ok := filter.TryCompactGitDiff(argv, []byte(payload)); !ok {
		return false
	}
	return wssSafeGitNameStatusPathListPayload(payload)
}

func wssGitSubcommand(commandLine, subcommand string) ([]string, int, bool) {
	return wssGitSubcommandFromArgv(filter.ArgvForCapturedOutput(commandLine), subcommand)
}

func wssGitSubcommandFromArgv(argv []string, subcommand string) ([]string, int, bool) {
	if len(argv) < 2 {
		return nil, 0, false
	}
	bin := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(argv[0]), ".exe"))
	if bin != "git" && !strings.HasSuffix(bin, "/git") {
		return nil, 0, false
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
	if i >= len(argv) || strings.ToLower(argv[i]) != subcommand {
		return nil, 0, false
	}
	return argv, i, true
}

func wssSafeGitLogOnelineOutput(commandLine, payload string) bool {
	argv, i, ok := wssGitSubcommandFromArgv(filter.ArgvForCapturedOutput(commandLine), "log")
	if !ok {
		return false
	}
	hasOneline := false
	maxCount := 0
	for i++; i < len(argv); i++ {
		arg := strings.ToLower(strings.TrimSpace(argv[i]))
		switch {
		case arg == "--":
			i = len(argv)
		case arg == "--oneline":
			hasOneline = true
		case arg == "-n" || arg == "--max-count":
			i++
			if i >= len(argv) {
				return false
			}
			n, ok := parsePositiveBoundedInt(argv[i], wssSafeGitLogOnelineMaxCommits)
			if !ok {
				return false
			}
			maxCount = n
		case strings.HasPrefix(arg, "--max-count="):
			n, ok := parsePositiveBoundedInt(strings.TrimPrefix(arg, "--max-count="), wssSafeGitLogOnelineMaxCommits)
			if !ok {
				return false
			}
			maxCount = n
		case strings.HasPrefix(arg, "-n") && len(arg) > 2:
			n, ok := parsePositiveBoundedInt(strings.TrimPrefix(arg, "-n"), wssSafeGitLogOnelineMaxCommits)
			if !ok {
				return false
			}
			maxCount = n
		case strings.HasPrefix(arg, "-") && len(arg) > 1 && allASCIIDigits(arg[1:]):
			n, ok := parsePositiveBoundedInt(arg[1:], wssSafeGitLogOnelineMaxCommits)
			if !ok {
				return false
			}
			maxCount = n
		case strings.HasPrefix(arg, "-"):
			return false
		}
	}
	return hasOneline && maxCount > 0 && wssGitLogOnelinePayloadSafe(payload, maxCount)
}

func wssGitLogOnelinePayloadSafe(payload string, maxCount int) bool {
	lines := strings.Split(strings.TrimSpace(payload), "\n")
	if len(lines) == 0 || len(lines) > maxCount {
		return false
	}
	for _, raw := range lines {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" {
			return false
		}
		hash, subject, ok := strings.Cut(line, " ")
		if !ok || strings.TrimSpace(subject) == "" {
			return false
		}
		if len(hash) < 7 || len(hash) > 40 || !allASCIIHex(hash) {
			return false
		}
	}
	return true
}

func wssSafeGitLsFilesPathListOutput(commandLine, payload string) bool {
	argv := filter.ArgvForCapturedOutput(commandLine)
	if len(argv) == 0 {
		return false
	}
	if _, ok := filter.TryCompactGitLsFiles(argv, []byte(payload)); !ok {
		return false
	}
	return wssSafePlainPathListPayload(payload)
}

func parsePositiveBoundedInt(raw string, maxValue int) (int, bool) {
	if raw == "" || !allASCIIDigits(raw) {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 || n > maxValue {
		return 0, false
	}
	return n, true
}

func allASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func allASCIIHex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			continue
		}
		return false
	}
	return true
}

func wssSafeWcOutput(commandLine, payload string) bool {
	argv := filter.ArgvForCapturedOutput(commandLine)
	if len(argv) == 0 {
		return false
	}
	_, ok := filter.TryCompactWc(argv, []byte(payload))
	return ok
}

func wssSafeLsListingOutput(commandLine, payload string) bool {
	argv := filter.ArgvForCapturedOutput(commandLine)
	if len(argv) == 0 || wssCommandBase(argv[0]) != "ls" {
		return false
	}
	if !wssSafeLsArgs(argv[1:]) {
		return false
	}
	return wssSafeListingPayload(payload)
}

func wssSafeFormatPathListOutput(commandLine, payload string) bool {
	argv := filter.ArgvForCapturedOutput(commandLine)
	if len(argv) == 0 {
		return false
	}
	if _, ok := filter.TryCompactFormatOutput(argv, []byte(payload)); !ok {
		return false
	}
	return wssSafePlainPathListPayload(payload)
}

func wssSafePlainPathListPayload(payload string) bool {
	if len(payload) == 0 || len(payload) > wssSafeListingOutputMaxBytes || strings.ContainsRune(payload, '\x00') {
		return false
	}
	trimmed := strings.TrimSpace(payload)
	if trimmed == "" || looksLikeSource(trimmed) || proxyToolResultLooksLikeSearchOutput(trimmed) {
		return false
	}
	entries := 0
	for _, raw := range strings.Split(trimmed, "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" {
			continue
		}
		if !wssSafePlainPathListLine(line) {
			return false
		}
		entries++
		if entries > wssSafeListingOutputMaxEntries {
			return false
		}
	}
	return entries > 0
}

func wssSafePlainPathListLine(line string) bool {
	if len(line) > wssSafeListingOutputMaxLineBytes || strings.ContainsAny(line, " \t") ||
		strings.Contains(line, "://") || strings.HasPrefix(line, "-") {
		return false
	}
	for _, r := range line {
		switch r {
		case ':', ';', '|', '<', '>', '"', '\'', '`', '$', '\\':
			return false
		}
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func wssSafeGitNameStatusPathListPayload(payload string) bool {
	if len(payload) == 0 || len(payload) > wssSafeListingOutputMaxBytes || strings.ContainsRune(payload, '\x00') {
		return false
	}
	trimmed := strings.TrimSpace(payload)
	if trimmed == "" || looksLikeSource(trimmed) || proxyToolResultLooksLikeSearchOutput(trimmed) {
		return false
	}
	entries := 0
	for _, raw := range strings.Split(trimmed, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		status, path, ok := strings.Cut(line, "\t")
		if !ok || strings.Contains(path, "\t") || len(status) != 1 || !strings.Contains("AMDTUXB", status) {
			return false
		}
		if !wssSafePlainPathListLine(path) {
			return false
		}
		entries++
		if entries > wssSafeListingOutputMaxEntries {
			return false
		}
	}
	return entries > 0
}

func wssSafeLsArgs(args []string) bool {
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			return false
		}
		if arg == "--" {
			return true
		}
		if strings.HasPrefix(arg, "--") {
			switch {
			case arg == "--all" || arg == "--almost-all" || arg == "--directory" ||
				arg == "--classify" || arg == "--file-type" || arg == "--color=never":
				continue
			case strings.HasPrefix(arg, "--indicator-style="):
				continue
			default:
				return false
			}
		}
		if strings.HasPrefix(arg, "-") && arg != "-" {
			for _, ch := range arg[1:] {
				switch ch {
				case '1', 'a', 'A', 'd', 'p', 'F':
				default:
					return false
				}
			}
		}
	}
	return true
}

func wssSafeFindListingOutput(commandLine, payload string) bool {
	argv := filter.ArgvForCapturedOutput(commandLine)
	if len(argv) == 0 || wssCommandBase(argv[0]) != "find" {
		return false
	}
	if !wssSafeFindArgs(argv[1:]) {
		return false
	}
	return wssSafeListingPayload(payload)
}

func wssSafeFindArgs(args []string) bool {
	sawMaxDepth := false
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			return false
		}
		switch arg {
		case "-exec", "-execdir", "-ok", "-okdir", "-delete", "-printf", "-fprintf",
			"-ls", "-fls", "-print0", "-fprint", "-fprint0":
			return false
		case "-maxdepth", "-mindepth":
			isMaxDepth := arg == "-maxdepth"
			i++
			if i >= len(args) {
				return false
			}
			_, ok := parsePositiveBoundedInt(args[i], wssSafeFindMaxDepth)
			if !ok && strings.TrimSpace(args[i]) != "0" {
				return false
			}
			if isMaxDepth {
				sawMaxDepth = true
			}
		case "-name", "-iname", "-path", "-ipath", "-regex", "-iregex", "-type":
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return false
			}
		case "-print", "-empty", "-not", "!", "-a", "-and", "-o", "-or", "(", ")":
		default:
			if strings.HasPrefix(arg, "-") {
				return false
			}
		}
	}
	return sawMaxDepth
}

func wssSafeTreeListingOutput(commandLine, payload string) bool {
	argv := filter.ArgvForCapturedOutput(commandLine)
	if len(argv) == 0 || wssCommandBase(argv[0]) != "tree" {
		return false
	}
	if !wssSafeTreeArgs(argv[1:]) {
		return false
	}
	return wssSafeListingPayload(payload)
}

func wssSafeTreeArgs(args []string) bool {
	sawDepth := false
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			return false
		}
		if arg == "--" {
			for _, rest := range args[i+1:] {
				if strings.TrimSpace(rest) == "" {
					return false
				}
			}
			return sawDepth
		}
		if strings.HasPrefix(arg, "--") {
			switch {
			case arg == "--dirsfirst" || arg == "--noreport":
				continue
			case arg == "--charset":
				i++
				if i >= len(args) || strings.TrimSpace(args[i]) == "" {
					return false
				}
				continue
			case strings.HasPrefix(arg, "--charset="):
				if strings.TrimSpace(strings.TrimPrefix(arg, "--charset=")) == "" {
					return false
				}
				continue
			default:
				return false
			}
		}
		if strings.HasPrefix(arg, "-") && arg != "-" {
			for j := 1; j < len(arg); j++ {
				switch arg[j] {
				case 'a', 'd', 'f', 'F':
					continue
				case 'L':
					depth := strings.TrimSpace(arg[j+1:])
					if depth == "" {
						i++
						if i >= len(args) {
							return false
						}
						depth = strings.TrimSpace(args[i])
					}
					if _, ok := parsePositiveBoundedInt(depth, wssSafeTreeMaxDepth); !ok {
						return false
					}
					sawDepth = true
					j = len(arg)
				default:
					return false
				}
			}
		}
	}
	return sawDepth
}

func wssSafeListingPayload(payload string) bool {
	if len(payload) == 0 || len(payload) > wssSafeListingOutputMaxBytes || strings.ContainsRune(payload, '\x00') {
		return false
	}
	trimmed := strings.TrimSpace(payload)
	if trimmed == "" || looksLikeSource(trimmed) || proxyToolResultLooksLikeSearchOutput(trimmed) {
		return false
	}
	entries := 0
	for _, raw := range strings.Split(trimmed, "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" {
			continue
		}
		if len(line) > wssSafeListingOutputMaxLineBytes || strings.HasPrefix(line, "total ") {
			return false
		}
		entries++
		if entries > wssSafeListingOutputMaxEntries {
			return false
		}
	}
	return entries > 0
}

func wssCommandBase(path string) string {
	return strings.ToLower(strings.TrimSuffix(filepath.Base(strings.TrimSpace(path)), ".exe"))
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

func wssRequestIsDeltaShape(messages []types.Message) bool {
	if len(messages) == 0 {
		return false
	}
	for _, message := range messages {
		if wssMessageHasHistoryShape(message) {
			return false
		}
	}
	return true
}

func wssRequestHasHistoryShape(messages []types.Message) bool {
	for _, message := range messages {
		if wssMessageHasHistoryShape(message) {
			return true
		}
	}
	return false
}

func wssMessageHasHistoryShape(message types.Message) bool {
	if message.Role == "assistant" {
		return true
	}
	for _, block := range message.Content {
		if block.Type == "tool_use" {
			return true
		}
	}
	return false
}

func wssRequestShape(meta wssRequestMeta, messages []types.Message) string {
	if wssRequestHasHistoryShape(messages) {
		return "full_history"
	}
	if meta.PreviousResponseID == "" {
		return "root"
	}
	if wssRequestIsDeltaShape(messages) {
		return "delta"
	}
	return "full_history"
}

func mergeWSSHistoryReducerStats(base proxyLayer0Stats, history proxyLayer0Stats) proxyLayer0Stats {
	if history.StaleReadBlocks == 0 && history.ObsoletePruneBlocks == 0 && len(history.EvidenceDecisions) == 0 {
		return base
	}
	base.StaleReadBlocks += history.StaleReadBlocks
	base.StaleReadBytesSaved += history.StaleReadBytesSaved
	base.StaleReadTokensSaved += history.StaleReadTokensSaved
	base.ObsoletePruneBlocks += history.ObsoletePruneBlocks
	base.ObsoletePruneBytesSaved += history.ObsoletePruneBytesSaved
	base.ObsoletePruneTokensSaved += history.ObsoletePruneTokensSaved
	historyBlocks := history.StaleReadBlocks + history.ObsoletePruneBlocks
	historyTokens := history.StaleReadTokensSaved + history.ObsoletePruneTokensSaved
	base.BlocksModified += historyBlocks
	base.TokensSaved += historyTokens
	if len(history.EvidenceDecisions) > 0 {
		base.EvidenceDecisions = append(append([]evidence.BlockDecision(nil), history.EvidenceDecisions...), base.EvidenceDecisions...)
	}
	return base
}

func mergeWSSLayer0ObservationStats(base proxyLayer0Stats, observed proxyLayer0Stats) proxyLayer0Stats {
	base.ToolResultBlocks += observed.ToolResultBlocks
	base.ToolUseUnresolvedBlocks += observed.ToolUseUnresolvedBlocks
	base.CommandResolvedBlocks += observed.CommandResolvedBlocks
	base.CommandUnresolvedBlocks += observed.CommandUnresolvedBlocks
	base.ReadDeltaAttempts += observed.ReadDeltaAttempts
	base.ReadDeltaMisses += observed.ReadDeltaMisses
	base.ToolResultBytes += observed.ToolResultBytes
	base.TokensSaved += observed.TokensSaved
	base.BlocksModified += observed.BlocksModified
	base.ReadDeltaBlocks += observed.ReadDeltaBlocks
	base.CapturedOutputBlocks += observed.CapturedOutputBlocks
	base.CodexExecEnvelopeBlocks += observed.CodexExecEnvelopeBlocks
	base.RepeatedOutputBlocks += observed.RepeatedOutputBlocks
	base.ChunkDedupBlocks += observed.ChunkDedupBlocks
	base.ChunkDedupReferences += observed.ChunkDedupReferences
	base.ChunkDedupRefBytes += observed.ChunkDedupRefBytes
	base.ChunkDedupInputBytes += observed.ChunkDedupInputBytes
	base.StaleReadBlocks += observed.StaleReadBlocks
	base.StaleReadBytesSaved += observed.StaleReadBytesSaved
	base.StaleReadTokensSaved += observed.StaleReadTokensSaved
	base.ObsoletePruneBlocks += observed.ObsoletePruneBlocks
	base.ObsoletePruneBytesSaved += observed.ObsoletePruneBytesSaved
	base.ObsoletePruneTokensSaved += observed.ObsoletePruneTokensSaved
	base.ReadDeltaKeys = append(base.ReadDeltaKeys, observed.ReadDeltaKeys...)
	base.PolicyDecisions = append(base.PolicyDecisions, observed.PolicyDecisions...)
	base.CacheEvents = append(base.CacheEvents, observed.CacheEvents...)
	base.EvidenceDecisions = append(base.EvidenceDecisions, observed.EvidenceDecisions...)
	base.TotalLatencyNs += observed.TotalLatencyNs
	base.ReadDeltaLatencyNs += observed.ReadDeltaLatencyNs
	base.FilterLatencyNs += observed.FilterLatencyNs
	base.RepeatedOutputLatencyNs += observed.RepeatedOutputLatencyNs
	base.ChunkDedupLatencyNs += observed.ChunkDedupLatencyNs
	if base.Route == "" {
		base.Route = observed.Route
	}
	return base
}

func proxyLayer0StatsHasTelemetry(stats proxyLayer0Stats) bool {
	return stats.ToolResultBlocks > 0 ||
		stats.ToolResultBytes > 0 ||
		len(stats.PolicyDecisions) > 0 ||
		len(stats.CacheEvents) > 0 ||
		len(stats.EvidenceDecisions) > 0 ||
		stats.TotalLatencyNs > 0
}

func (a *wsPhaseFAdapter) observeWSSPreviousResponseDeltaLayer0(messages []types.Message, toolUses map[string]types.ContentBlock, sessionID, turnID string, suppressedKeys map[string]struct{}, chunkSettings codexChunkDedupSettings, meta wssRequestMeta, cacheBustDemoted proxyLayer0MechanismMask) proxyLayer0Stats {
	result := reduceCodexLayer0(codexLayer0Request{
		Route:                   codexLayer0RouteWSSPhaseF,
		Messages:                messages,
		ToolUseIndex:            toolUses,
		SessionID:               sessionID,
		TurnID:                  turnID,
		SuppressedToolKey:       suppressedKeys,
		RecentFullPassTurns:     a.p.config.Compression.OutputReduce.ReadDeltaRecentFullPassTurns,
		ChunkDedupEnabled:       chunkSettings.Enabled,
		ExplicitChunkDedup:      chunkSettings.Explicit,
		ChunkDedupProof:         chunkSettings.Proof,
		ChunkDedupMinBytes:      chunkSettings.MinBytes,
		ChunkDedupMaxRefPct:     chunkSettings.MaxRefPct,
		ChunkStore:              chunkSettings.Store,
		PolicyMode:              chunkSettings.PolicyMode,
		ArchiveRecovery:         chunkSettings.ArchiveRecovery,
		TurnSeq:                 meta.TurnSeq,
		RemainingTurnsEstimate:  meta.RemainingTurnsEstimate,
		CachedPriceRatio:        a.p.config.Savings.CachedPriceRatio,
		UniformChunkDedupBudget: a.p.wssABReplayUniformChunkBudget,
		SearchCompactOptions: filter.SearchCompactOptions{
			MaxFilesShown:     a.p.config.Compression.OutputReduce.CodexSearchCapMaxFiles,
			MaxMatchesPerFile: a.p.config.Compression.OutputReduce.CodexSearchCapMaxMatchesPerFile,
		},
		HostBudgetExceeded:           a.p.codexHostBudgetExceeded(),
		LatencyBudgetExceeded:        a.p.codexLayer0LatencyExceeded.Load(),
		StructuredMutationBlocked:    true,
		CacheBustDemotedMechanisms:   cacheBustDemoted,
		HistoryMutationGuardReason:   "wss_stateful_delta_mutation_proof_gate",
		StatefulDeltaMutationBlocked: true,
	})
	return result.Stats
}

func (a *wsPhaseFAdapter) wssGuardedHistoryReducerEvidence(out []byte, messages []types.Message, guardReason string, turnSeq int) proxyLayer0Stats {
	if guardReason == "" {
		return proxyLayer0Stats{}
	}
	historyStats := proxyLayer0Stats{Route: codexLayer0RouteWSSPhaseF}
	if a.p.config.Compression.OutputReduce.StaleReadAgingEnabled {
		aged, stats := staleread.AgeMessages(messages, staleread.Options{
			MinTurnGap: a.p.config.Compression.OutputReduce.StaleReadAgingMinTurnGap,
		})
		if stats.BlocksReplaced > 0 {
			beforeTokens := wssPlannerTokenCount(out, messages)
			afterTokens := wssPlannerTokenCount(out, aged)
			historyStats.EvidenceDecisions = append(historyStats.EvidenceDecisions, proxyHistoryMutationEvidenceDecision(proxyLayer0MechanismStaleRead, evidence.ActionFullPass, guardReason, beforeTokens, afterTokens, turnSeq, a.p.config.Savings.CachedPriceRatio))
		}
	}
	if a.p.config.Compression.OutputReduce.ObsoleteReadPruneEnabled {
		pruned, stats := staleread.PruneObsoleteReads(messages, staleread.ObsoleteOptions{})
		if stats.BlocksReplaced > 0 {
			beforeTokens := wssPlannerTokenCount(out, messages)
			afterTokens := wssPlannerTokenCount(out, pruned)
			historyStats.EvidenceDecisions = append(historyStats.EvidenceDecisions, proxyHistoryMutationEvidenceDecision(proxyLayer0MechanismObsoletePrune, evidence.ActionFullPass, guardReason, beforeTokens, afterTokens, turnSeq, a.p.config.Savings.CachedPriceRatio))
		}
	}
	return historyStats
}

type wssHistoryReducerResult struct {
	Messages             []types.Message
	Stats                proxyLayer0Stats
	Mutated              bool
	StaleBlocksReplaced  int
	StaleBytesReplaced   int
	ObsoleteBlocksPruned int
	ObsoleteBytesPruned  int
}

func (a *wsPhaseFAdapter) applyWSSHistoryReducers(body []byte, messages []types.Message, guardReason string, cacheBustDemoted proxyLayer0MechanismMask, turnSeq int) wssHistoryReducerResult {
	result := wssHistoryReducerResult{
		Messages: messages,
		Stats:    proxyLayer0Stats{Route: codexLayer0RouteWSSPhaseF},
	}
	stagedMessages := messages
	if a.p.config.Compression.OutputReduce.StaleReadAgingEnabled {
		staleGuardReason := ""
		if guardReason != "" {
			staleGuardReason = guardReason
		} else if cacheBustDemoted.Has(proxyLayer0MechanismStaleRead) {
			staleGuardReason = "cache_bust_guard"
		}
		aged, stats := staleread.AgeMessages(stagedMessages, staleread.Options{
			MinTurnGap: a.p.config.Compression.OutputReduce.StaleReadAgingMinTurnGap,
		})
		if stats.BlocksReplaced > 0 {
			beforeTokens := wssPlannerTokenCount(body, stagedMessages)
			afterTokens := wssPlannerTokenCount(body, aged)
			if staleGuardReason != "" {
				result.Stats.EvidenceDecisions = append(result.Stats.EvidenceDecisions, proxyHistoryMutationEvidenceDecision(proxyLayer0MechanismStaleRead, evidence.ActionFullPass, staleGuardReason, beforeTokens, afterTokens, turnSeq, a.p.config.Savings.CachedPriceRatio))
			} else {
				stagedMessages = aged
				result.Mutated = true
				result.StaleBlocksReplaced = stats.BlocksReplaced
				result.StaleBytesReplaced = stats.BytesReplaced
				result.Stats.StaleReadBlocks = stats.BlocksReplaced
				result.Stats.StaleReadBytesSaved = stats.BytesReplaced
				result.Stats.StaleReadTokensSaved = beforeTokens - afterTokens
				result.Stats.EvidenceDecisions = append(result.Stats.EvidenceDecisions, proxyHistoryMutationEvidenceDecision(proxyLayer0MechanismStaleRead, evidence.ActionApplied, "positive_net_savings", beforeTokens, afterTokens, turnSeq, a.p.config.Savings.CachedPriceRatio))
			}
		}
	}
	if a.p.config.Compression.OutputReduce.ObsoleteReadPruneEnabled {
		obsoleteGuardReason := ""
		if guardReason != "" {
			obsoleteGuardReason = guardReason
		} else if cacheBustDemoted.Has(proxyLayer0MechanismObsoletePrune) {
			obsoleteGuardReason = "cache_bust_guard"
		}
		pruned, stats := staleread.PruneObsoleteReads(stagedMessages, staleread.ObsoleteOptions{})
		if stats.BlocksReplaced > 0 {
			beforeTokens := wssPlannerTokenCount(body, stagedMessages)
			afterTokens := wssPlannerTokenCount(body, pruned)
			if obsoleteGuardReason != "" {
				result.Stats.EvidenceDecisions = append(result.Stats.EvidenceDecisions, proxyHistoryMutationEvidenceDecision(proxyLayer0MechanismObsoletePrune, evidence.ActionFullPass, obsoleteGuardReason, beforeTokens, afterTokens, turnSeq, a.p.config.Savings.CachedPriceRatio))
			} else {
				stagedMessages = pruned
				result.Mutated = true
				result.ObsoleteBlocksPruned = stats.BlocksReplaced
				result.ObsoleteBytesPruned = stats.BytesReplaced
				result.Stats.ObsoletePruneBlocks = stats.BlocksReplaced
				result.Stats.ObsoletePruneBytesSaved = stats.BytesReplaced
				result.Stats.ObsoletePruneTokensSaved = beforeTokens - afterTokens
				result.Stats.EvidenceDecisions = append(result.Stats.EvidenceDecisions, proxyHistoryMutationEvidenceDecision(proxyLayer0MechanismObsoletePrune, evidence.ActionApplied, "positive_net_savings", beforeTokens, afterTokens, turnSeq, a.p.config.Savings.CachedPriceRatio))
			}
		}
	}
	result.Messages = stagedMessages
	result.Stats.BlocksModified = result.Stats.StaleReadBlocks + result.Stats.ObsoletePruneBlocks
	result.Stats.TokensSaved = result.Stats.StaleReadTokensSaved + result.Stats.ObsoletePruneTokensSaved
	return result
}

func wssRequestDebugFacts(body []byte, mutated []byte, messages []types.Message, l0Stats proxyLayer0Stats, replaced bool, bypassReason string, meta wssRequestMeta, outputReduceStats outputreduce.Stats) map[string]string {
	toolResults, sourceToolResults, toolUses := wssMessageShapeCounts(messages)
	sourceToolBytes, sourceToolMaxBytes := wssSourceToolResultBytes(messages)
	deltaShape := wssRequestIsDeltaShape(messages)
	facts := map[string]string{
		"wss.original_bytes":           strconv.Itoa(len(body)),
		"wss.final_bytes":              strconv.Itoa(len(mutated)),
		"wss.changed":                  strconv.FormatBool(replaced || !bytes.Equal(body, mutated)),
		"wss.previous_response_id":     strconv.FormatBool(meta.PreviousResponseID != ""),
		"wss.turn_seq":                 strconv.Itoa(meta.TurnSeq),
		"wss.remaining_turns_estimate": strconv.Itoa(meta.RemainingTurnsEstimate),
		"wss.request_shape":            wssRequestShape(meta, messages),
		"wss.delta_shape":              strconv.FormatBool(deltaShape),
		"wss.messages":                 strconv.Itoa(len(messages)),
		"wss.tool_results":             strconv.Itoa(toolResults),
		"wss.source_tool_results":      strconv.Itoa(sourceToolResults),
		"wss.source_tool_bytes":        strconv.Itoa(sourceToolBytes),
		"wss.source_tool_max_bytes":    strconv.Itoa(sourceToolMaxBytes),
		"wss.tool_uses":                strconv.Itoa(toolUses),
		"wss.layer0_blocks_modified":   strconv.Itoa(l0Stats.BlocksModified),
		"wss.layer0_tokens_saved":      strconv.Itoa(l0Stats.TokensSaved),
		"wss.stale_read_blocks":        strconv.Itoa(l0Stats.StaleReadBlocks),
		"wss.stale_read_tokens":        strconv.Itoa(l0Stats.StaleReadTokensSaved),
		"wss.obsolete_prune_blocks":    strconv.Itoa(l0Stats.ObsoletePruneBlocks),
		"wss.obsolete_prune_tokens":    strconv.Itoa(l0Stats.ObsoletePruneTokensSaved),
		"wss.output_reduce_applied":    strconv.FormatBool(outputReduceStats.Applied),
		"wss.output_reduce_added":      strconv.Itoa(outputReduceStats.AddedTokens),
		"wss.output_reduce_reason":     outputReduceStats.Reason,
		"wss.replace_applied":          strconv.FormatBool(replaced),
		"wss.session_id_present":       strconv.FormatBool(meta.SessionID != ""),
	}
	if bypassReason != "" {
		facts["wss.bypass_reason"] = bypassReason
	}
	if facts["wss.output_reduce_reason"] == "" {
		facts["wss.output_reduce_reason"] = "disabled"
	}
	if meta.SocketSeq > 0 {
		facts["wss.socket_seq"] = strconv.FormatUint(meta.SocketSeq, 10)
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
	return wssPlannerTokenCountsWithOriginal(body, mutated, nil, messages, l0Stats, replaced)
}

func wssPlannerTokenCountsWithOriginal(body []byte, mutated []byte, originalMessages []types.Message, messages []types.Message, l0Stats proxyLayer0Stats, replaced bool) (int, int) {
	if replaced || l0Stats.TokensSaved > 0 {
		if len(originalMessages) == 0 {
			originalMessages, _, _ = extractMessages(types.CodexChatGPT, body)
		}
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

func (a *wsPhaseFAdapter) handleResponse(env *wsmitm.Envelope) (bool, error) {
	if a.recordWSSUpstreamError(env) {
		return false, wsmitm.ErrFrameConsumed
	}
	a.observeWSSRecoveryResponse(env)
	a.rememberWSSResponseState(env)
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
	return mutated, nil
}

func (a *wsPhaseFAdapter) recordWSSUpstreamError(env *wsmitm.Envelope) bool {
	if a == nil || a.p == nil || env == nil {
		return false
	}
	if env.Kind != wsmitm.FrameKindError && env.Kind != wsmitm.FrameKindResponseFailed && env.Kind != wsmitm.FrameKindResponseIncomplete {
		return false
	}
	status, errorType, message := wssUpstreamErrorFields(env)
	errSummary := formatWSSUpstreamError(env.Kind, status, errorType, message)
	if errSummary == "" {
		errSummary = "upstream_error kind=" + string(env.Kind)
	}
	if a.failActiveWSSRecovery(errSummary, status, errorType, message, env.Kind) {
		a.markDegraded(errSummary)
		slog.Warn("codex wss recovery retry rejected", "kind", env.Kind, "status", status, "error_type", errorType)
		return false
	}
	recoveryFacts := a.wssRecoveryDebugFacts(status, errorType, message)
	if a.tryWSSRecoveryRetry(status, errorType, message, errSummary) {
		return true
	}
	a.markDegraded(errSummary)
	slog.Warn("codex wss upstream error", "kind", env.Kind, "status", status, "error_type", errorType)
	if a.p.debugRecorder == nil {
		return false
	}
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
		DebugFacts:   recoveryFacts,
	})
	return false
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

func (a *wsPhaseFAdapter) wssCacheBustDemotedMechanisms(sessionID string) proxyLayer0MechanismMask {
	if a == nil || sessionID == "" {
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cacheBustSessions == nil {
		return 0
	}
	session := a.cacheBustSessions[sessionID]
	if session == nil {
		return 0
	}
	return session.demoted
}

func (a *wsPhaseFAdapter) observeWSSProviderCacheBust(sessionID string, inputTokens int, cachedTokens int, mutatedMechanisms proxyLayer0MechanismMask) wssProviderCacheBustEvent {
	if a == nil || sessionID == "" || inputTokens <= 0 || cachedTokens < 0 {
		return wssProviderCacheBustEvent{}
	}
	cachedShare := float64(cachedTokens) / float64(inputTokens)
	if cachedShare > 1 {
		cachedShare = 1
	}
	a.mu.Lock()
	if a.cacheBustSessions == nil {
		a.cacheBustSessions = make(map[string]*wssProviderCacheBustSession)
	}
	session := a.cacheBustSessions[sessionID]
	if session == nil {
		session = &wssProviderCacheBustSession{}
		a.cacheBustSessions[sessionID] = session
	}
	event := session.observe(cachedShare, mutatedMechanisms)
	a.mu.Unlock()
	if event.Fired {
		slog.Warn("codex wss provider cache bust guard demoted layer0 mechanisms",
			slog.String("session", sessionID),
			slog.String("trigger_mechanisms", event.Trigger.String()),
			slog.String("demoted_mechanisms", event.Demoted.String()),
			slog.Float64("previous_cached_share", event.PreviousShare),
			slog.Float64("current_cached_share", event.CurrentShare),
			slog.Int("observed_samples", event.ObservedSamples))
	}
	return event
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
	// Attribute the provider-reported usage (incl. server-side prompt-cache
	// cached_tokens) to this turn's decision record so per-session savings
	// carry the billable cache truth, not just local reduction (T352-A).
	a.mu.Lock()
	requestID := a.lastDecisionRequestID
	sessionID := a.lastUsageSessionID
	mutatedMechanisms := a.lastUsageMutatedMechanisms
	a.mu.Unlock()
	if a.p.debugRecorder != nil {
		if requestID != "" {
			a.p.debugRecorder.AttachProviderUsage(requestID, usage.InputTokens, usage.ReadTokens, usage.CreateTokens, usage.OutputTokens)
		}
	}
	a.observeWSSProviderCacheBust(sessionID, usage.InputTokens, usage.ReadTokens, mutatedMechanisms)
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
	saved := len(env.Delta) - len(rewritten)
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
		SessionID:            wssCodexSessionIDFromRaw(raw),
		PreviousResponseID:   wssPreviousResponseIDFromRaw(raw),
		Model:                wssPlannerModelFromRaw(raw),
		ClientFamily:         wssCodexClientFamilyFromRaw(raw),
		HasUserPromptInput:   wssRawHasUserPromptInput(raw),
		HasToolDefinitions:   wssRawHasToolDefinitions(raw),
		HasPromptCachePrefix: wssRawHasPromptCachePrefix(raw),
	}
}

func wssRawHasPromptCachePrefix(raw map[string]json.RawMessage) bool {
	if len(raw) == 0 || rawJSONString(raw["prompt_cache_key"]) == "" {
		return false
	}
	if _, ok := raw["instructions"]; ok {
		return true
	}
	if _, ok := raw["tools"]; ok {
		return true
	}
	return false
}

func wssRawHasToolDefinitions(raw map[string]json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	_, ok := raw["tools"]
	return ok
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

func detachCodexPreviousResponseID(body []byte) ([]byte, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return body, false
	}
	if _, ok := raw["previous_response_id"]; !ok {
		return body, false
	}
	delete(raw, "previous_response_id")
	out, err := json.Marshal(raw)
	if err != nil {
		return body, false
	}
	return out, true
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

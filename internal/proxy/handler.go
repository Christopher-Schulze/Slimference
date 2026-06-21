package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/beterse"
	"github.com/Christopher-Schulze/Slimference/internal/caching"
	"github.com/Christopher-Schulze/Slimference/internal/compression"
	"github.com/Christopher-Schulze/Slimference/internal/contentarchive"
	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/outputreduce"
	"github.com/Christopher-Schulze/Slimference/internal/outstop"
	"github.com/Christopher-Schulze/Slimference/internal/outstop/streamcut"
	"github.com/Christopher-Schulze/Slimference/internal/planner"
	"github.com/Christopher-Schulze/Slimference/internal/promptcache"
	"github.com/Christopher-Schulze/Slimference/internal/qualityab"
	"github.com/Christopher-Schulze/Slimference/internal/resilience"
	"github.com/Christopher-Schulze/Slimference/internal/security"
	"github.com/Christopher-Schulze/Slimference/internal/staleread"
	"github.com/Christopher-Schulze/Slimference/internal/tokens"
	"github.com/Christopher-Schulze/Slimference/internal/toolprune"
	"github.com/Christopher-Schulze/Slimference/internal/types"
	"github.com/klauspost/compress/zstd"
)

var reconstructBodyFn = reconstructBody
var extractMessagesFn = extractMessages
var newRequestWithContextFn = http.NewRequestWithContext
var newRequestIDFn = newRequestID
var newZstdReaderFn = zstd.NewReader
var newZstdWriterFn = zstd.NewWriter

// newRequestID generates a short random hex request ID for debug correlation.
func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// pipelineStash carries inputs for aggressive recompression on context overflow (spec §17.4).
type pipelineStash struct {
	messages  []types.Message
	origBody  []byte
	provider  types.Provider
	sessionID string
}

func responseCacheRouteKey(r *http.Request) string {
	if r == nil || r.URL == nil {
		return ""
	}
	method := strings.ToUpper(strings.TrimSpace(r.Method))
	path := r.URL.EscapedPath()
	if path == "" {
		path = r.URL.Path
	}
	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}
	if method == "" {
		return path
	}
	return method + " " + path
}

func (p *Proxy) responseCacheEffectiveRouteKey(r *http.Request, sessionID string) string {
	base := responseCacheRouteKey(r)
	partition := p.responseCachePolicyPartition(sessionID)
	if partition == "" {
		return base
	}
	if base == "" {
		return "policy:" + partition
	}
	return base + "#policy:" + partition
}

func (p *Proxy) responseCachePolicyPartition(sessionID string) string {
	if p == nil || p.config == nil {
		return ""
	}
	outputReduce := p.config.Compression.OutputReduce
	var parts []string
	if outputReduce.StopSequencesEnabled {
		parts = append(parts, "stopseq=on")
	}
	if outputReduce.BeTerseHintEnabled && p.qualityAB != nil {
		cohort := p.qualityAB.Cohort(sessionID)
		parts = append(parts, "beterse="+string(cohort))
		if cohort == qualityab.CohortTreatment {
			parts = append(parts, "beterse_hint="+shortResponseCachePolicyHash(outputReduce.BeTerseHintText))
		}
	}
	return strings.Join(parts, ";")
}

func shortResponseCachePolicyHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

func splitProviderCacheUsage(provider types.Provider, usage cacheUsage) (cacheReadTokens, providerCachedTokens int) {
	switch provider {
	case types.OpenAI, types.CodexChatGPT:
		return 0, usage.ReadTokens
	default:
		return usage.ReadTokens, 0
	}
}

type pipelineStashKey struct{}

type requestBodyEncoding string

const (
	requestBodyEncodingIdentity requestBodyEncoding = ""
	requestBodyEncodingZstd     requestBodyEncoding = "zstd"
)

type requestBodyEncodingKey struct{}

func decodeRequestBodyForPipeline(body []byte, contentEncoding string) ([]byte, requestBodyEncoding, error) {
	encoding := requestBodyEncoding(strings.ToLower(strings.TrimSpace(contentEncoding)))
	switch encoding {
	case "", "identity":
		return body, requestBodyEncodingIdentity, nil
	case requestBodyEncodingZstd:
		decoder, err := newZstdReaderFn(nil)
		if err != nil {
			return nil, "", fmt.Errorf("create zstd decoder: %w", err)
		}
		defer decoder.Close()
		decoded, err := decoder.DecodeAll(body, nil)
		if err != nil {
			return nil, "", fmt.Errorf("decode zstd body: %w", err)
		}
		return decoded, requestBodyEncodingZstd, nil
	default:
		return nil, "", fmt.Errorf("unsupported content-encoding %q", contentEncoding)
	}
}

func encodeRequestBodyForPipeline(body []byte, encoding requestBodyEncoding) ([]byte, error) {
	switch encoding {
	case requestBodyEncodingIdentity:
		return body, nil
	case requestBodyEncodingZstd:
		encoder, err := newZstdWriterFn(nil)
		if err != nil {
			return nil, fmt.Errorf("create zstd encoder: %w", err)
		}
		defer encoder.Close() //nolint:errcheck
		return encoder.EncodeAll(body, nil), nil
	default:
		return nil, fmt.Errorf("unsupported request body encoding %q", encoding)
	}
}

// handleCompressibleRequest applies the full compression pipeline and forwards to upstream.
// This is the hot path: called for every POST /v1/messages and POST /v1/chat/completions.
func (p *Proxy) handleCompressibleRequest(w http.ResponseWriter, r *http.Request, provider types.Provider, body []byte) {
	start := time.Now()
	reqID := newRequestIDFn()
	wireBody := body
	decodedBody, requestEncoding, err := decodeRequestBodyForPipeline(body, r.Header.Get("Content-Encoding"))
	if err != nil {
		slog.Warn("request body decode failed; passthrough",
			"error", err,
			"provider", provider,
			"content_encoding", r.Header.Get("Content-Encoding"),
			"body_bytes", len(body),
		)
		p.handlePassthroughWithAttribution(w, r, provider, wireBody, nil, "decode_failed")
		return
	}
	body = decodedBody
	r = r.WithContext(context.WithValue(r.Context(), requestBodyEncodingKey{}, requestEncoding))
	r = r.WithContext(context.WithValue(r.Context(), origBodyKey{}, body))
	sessionID := extractSessionID(provider, body, r.Header)
	clientFamily := extractClientFamily(provider, body, r.Header)

	// --- 1. Extract messages ---
	messages, rawBody, err := extractMessages(provider, body)
	if err != nil {
		if provider == types.CodexChatGPT {
			slog.Warn("codex body parse failed; passthrough",
				"error", err,
				"content_type", r.Header.Get("Content-Type"),
				"content_encoding", r.Header.Get("Content-Encoding"),
				"body_bytes", len(wireBody),
			)
			p.handlePassthroughWithAttribution(w, r, provider, wireBody, body, "parse_failed")
			return
		}
		slog.Error("extract messages", "error", err)
		p.proxyError(w, http.StatusBadRequest, fmt.Sprintf("parse request: %v", err))
		return
	}
	_ = rawBody
	if len(messages) == 0 {
		p.handlePassthroughWithAttribution(w, r, provider, body, body, "empty_messages")
		return
	}

	model := extractModel(body)
	// Request-scoped logger: all debug/warn/info calls inside this function carry req_id.
	log := slog.With("req_id", reqID, "provider", provider, "model", model)

	// T62: Anthropic-version negotiation. Unknown versions downgrade to
	// conservative / passthrough so an upstream schema drift never causes
	// mis-compression. For non-Anthropic providers the call is a no-op.
	pipelineMode := PipelineFull
	if provider == types.Anthropic {
		pipelineMode = ClassifyAnthropicVersion(
			r.Header.Get("anthropic-version"),
			&p.config.Proxy,
		)
		if pipelineMode != PipelineFull {
			log.Warn("pipeline_mode_downgrade",
				"mode", pipelineMode.String(),
				"header", r.Header.Get("anthropic-version"),
			)
		}
	}

	// T76 WP3: opportunistic re-injection. If any message text references
	// a local-archive URI, expand the archived content back into the
	// message before further processing. Best-effort: a missing or
	// unreadable archive entry leaves the marker in place so the model
	// still sees a stable reference rather than silently failing.
	messages = p.reinjectArchivedContentForSession(sessionID, messages)

	// T77: observe tool-use blocks for the re-read detector.
	reReadCount := 0
	if p.qualityReRead != nil {
		for _, msg := range messages {
			for _, block := range msg.Content {
				if block.Type == "tool_use" && block.ToolName != "" {
					if p.ObserveQualityToolKey(sessionID, block.ToolName) {
						reReadCount++
					}
				}
			}
		}
	}

	// Count original tokens before any compression.
	origTokens := tokens.CountMessages(messages)
	log.Debug("request started", "messages", len(messages), "orig_tokens", origTokens)

	// T112: adaptive sliding window resolution.
	windowDecision := resolveWindow(
		messages,
		p.config.Compression.SlidingWindow,
		p.config.Compression.Tuning.AdaptiveWindowEnabled,
		p.config.Compression.Tuning.AdaptiveWindowMin,
		p.config.Compression.Tuning.AdaptiveWindowMax,
	)
	effectiveWindow := windowDecision.Size

	// PreCompact-signal escalation: if the Codex hook recently wrote a
	// pre-compaction marker for this session (within precompactSignalTTL),
	// shrink the sliding window further so Layer-1 has more old
	// messages to compact. This is the single hottest moment to save
	// tokens — Codex is about to compact anyway, and our deterministic
	// compaction is provably better.
	if p.hasRecentPreCompactSignal(sessionID) {
		shrunk := precompactShrinkWindow(effectiveWindow)
		if shrunk < effectiveWindow {
			log.Debug("precompact_signal_active",
				"session_id", sessionID,
				"effective_window_before", effectiveWindow,
				"effective_window_after", shrunk)
			effectiveWindow = shrunk
		}
	} else if effectiveWindow != p.config.Compression.SlidingWindow {
		log.Debug("adaptive_window", "decision", windowDecision.String())
	}

	// --- 2.5. Stale-read aging (T170) ---
	// Replace superseded older Read tool_results with neutral context-elision
	// markers. Runs before secret detection (cheaper scan input) and
	// before compression layers.
	// Lossless: the most-recent read of any given path always survives.
	if p.config.Compression.OutputReduce.StaleReadAgingEnabled {
		aged, agingStats := staleread.AgeMessages(messages, staleread.Options{
			MinTurnGap: p.config.Compression.OutputReduce.StaleReadAgingMinTurnGap,
		})
		if agingStats.BlocksReplaced > 0 {
			messages = aged
			p.outputReduceCounters.RecordStaleReadAging(agingStats.BlocksReplaced, agingStats.BytesReplaced)
			log.Debug("stale-read aging applied",
				"blocks_replaced", agingStats.BlocksReplaced,
				"bytes_replaced", agingStats.BytesReplaced,
				"paths_aged", agingStats.PathsAged,
			)
		}
	}

	// --- 2.6. Multi-turn obsolete-read pruning (T174) ---
	// Replace reads that happened before a subsequent file mutation
	// with neutral context-elision markers. Pairs with t170:
	// staleread.AgeMessages keeps the most-recent read; this prunes
	// any read older than a mutation regardless of newer reads.
	if p.config.Compression.OutputReduce.ObsoleteReadPruneEnabled {
		pruned, pruneStats := staleread.PruneObsoleteReads(messages, staleread.ObsoleteOptions{})
		if pruneStats.BlocksReplaced > 0 {
			messages = pruned
			p.outputReduceCounters.RecordObsoleteReadPrune(pruneStats.BlocksReplaced, pruneStats.BytesReplaced)
			log.Debug("obsolete-read prune applied",
				"blocks_replaced", pruneStats.BlocksReplaced,
				"bytes_replaced", pruneStats.BytesReplaced,
				"paths_pruned", pruneStats.PathsPruned,
			)
		}
	}

	// --- 3. Secret detection ---
	if p.secretsDetector != nil {
		var detections []security.Detection
		messages, detections, err = p.secretsDetector.ScanMessages(messages)
		if err != nil {
			// mode=="block" returns error when secrets found.
			p.proxyError(w, http.StatusBadRequest, fmt.Sprintf("secret detected: %v", err))
			return
		}
		if len(detections) > 0 {
			log.Warn("secrets detected/redacted", "count", len(detections))
			p.trySendAnalytics(types.AnalyticsEvent{
				Type:         types.EventSecretDetected,
				Timestamp:    time.Now(),
				Provider:     provider,
				SecretsFound: len(detections),
			})
		}
	}

	r = r.WithContext(context.WithValue(r.Context(), pipelineStashKey{}, pipelineStash{
		messages:  messages,
		origBody:  body,
		provider:  provider,
		sessionID: sessionID,
	}))

	repairSignal := outputreduce.DetectRepairSignal(provider, body)
	p.consumeOutputReduceRepairSignal(sessionID, repairSignal)

	recentEditFact := p.plannerRecentEditFact(sessionID, messages)
	liveCorpusConfidence := p.plannerLiveCorpusConfidence()
	runtimePlan := p.buildCompressionPlan(plannerInput{
		provider:             provider,
		model:                model,
		routeMode:            "upstream",
		estimatedInputTokens: origTokens,
		contentClasses:       plannerClassesFromMessages(messages),
		recentEdit:           recentEditFact,
		liveCorpusConfidence: liveCorpusConfidence,
	})
	layer0Action := plannerActionForLayer(runtimePlan, planner.Layer0, planner.ActionRun)
	layer1Action := plannerActionForLayer(runtimePlan, planner.Layer1, planner.ActionRun)
	// --- 3.5 Stage A cache pre-check (T20) ---
	// If an identical original request already produced a cached upstream
	// response, serve it without running the deterministic compression path.
	var stageACacheKey [32]byte
	effectiveRouteKey := p.responseCacheEffectiveRouteKey(r, sessionID)
	stageAEnabled := p.isLayerEnabled(2) && caching.IsRequestCacheSafeWithRoute(effectiveRouteKey, body)
	if stageAEnabled {
		stageACacheKey = p.responseCache.ComputeRequestKeyWithRoute(provider, effectiveRouteKey, body, r.Header)
		if cached, _, ok := p.responseCache.GetByOriginal(stageACacheKey); ok {
			p.serveStageACacheHit(w, cached, reqID, start, provider, model, len(messages), origTokens, log, windowDecision)
			return
		}
	}

	compressedMessages := messages
	var layer0Savings, layer1Savings int
	var l0Stats proxyLayer0Stats
	appliedLayers := make([]int, 0, 3)

	// --- 3.75 Layer 0: proxy-side tool-output compaction ---
	// Codex CLI `exec` currently does not emit reliable PostToolUse hook
	// events, so the CLI hot path must not depend on hook delivery for
	// Layer-0 savings. Once Codex resends function_call/function_call_output
	// history through the proxied Responses request, run the same
	// deterministic captured-output filters here.
	if p.isProviderEnabled(provider) && pipelineMode == PipelineFull && layer0Action != planner.ActionBypass {
		chunkSettings := p.codexHTTPChunkDedupSettings(provider)
		result := reduceCodexLayer0(codexLayer0Request{
			Route:                 codexLayer0RouteHTTP,
			Messages:              compressedMessages,
			SessionID:             sessionID,
			ChunkDedupEnabled:     chunkSettings.Enabled,
			ExplicitChunkDedup:    chunkSettings.Explicit,
			ChunkDedupProof:       chunkSettings.Proof,
			ChunkDedupMinBytes:    chunkSettings.MinBytes,
			ChunkDedupMaxRefPct:   chunkSettings.MaxRefPct,
			ChunkStore:            chunkSettings.Store,
			PolicyMode:            chunkSettings.PolicyMode,
			ArchiveRecovery:       chunkSettings.ArchiveRecovery,
			CachedPriceRatio:      p.config.Savings.CachedPriceRatio,
			HostBudgetExceeded:    p.codexHostBudgetExceeded(),
			LatencyBudgetExceeded: p.codexLayer0LatencyExceeded.Load(),
		})
		l0Messages, stats := result.Messages, result.Stats
		l0Stats = stats
		if stats.TokensSaved > 0 {
			compressedMessages = l0Messages
			layer0Savings = stats.TokensSaved
			appliedLayers = append(appliedLayers, 0)
			log.Debug("proxy layer0 applied",
				"saved", stats.TokensSaved,
				"blocks", stats.BlocksModified,
				"read_delta_blocks", stats.ReadDeltaBlocks,
				"captured_output_blocks", stats.CapturedOutputBlocks,
				"codex_exec_envelope_blocks", stats.CodexExecEnvelopeBlocks,
				"repeated_output_blocks", stats.RepeatedOutputBlocks)
		}
	}

	// --- 4. Layer 1: Deterministic compression ---
	var layer1Breakdown map[string]dbg.SubLayerBreakdown
	var layer1Decisions []dbg.Layer1DecisionSummary
	if p.isLayerEnabled(1) && p.isProviderEnabled(provider) && pipelineMode == PipelineFull && layer1Action != planner.ActionBypass {
		l1Start := time.Now()
		// T76: thread the request id as the session scope so any archive
		// entry produced by lossy sub-layers carries a correlatable id.
		result := p.layer1.CompressWithSessionOptions(reqID, compressedMessages, compression.Layer1CompressOptions{
			CoordinatorSubsume: layer1Action == planner.ActionCheapOnly,
		})
		p.pipelineHist.L1.Record(time.Since(l1Start))
		if result.TokensSaved > 0 {
			compressedMessages = result.Messages
			layer1Savings = result.TokensSaved
			appliedLayers = append(appliedLayers, 1)
			log.Debug("layer1 applied",
				"json_saved", result.JSONSaved,
				"dedup_saved", result.DedupSaved,
				"structure_saved", result.StructureSaved,
				"total_saved", result.TokensSaved,
			)
		}
		layer1Breakdown = buildLayer1Breakdown(result)
		layer1Decisions = buildLayer1Decisions(result)
	}

	// --- 6. Prompt cache breakpoints (Anthropic only) ---
	if provider == types.Anthropic && p.isLayerEnabled(1) {
		stableBoundary := compression.CompressiblePrefixEnd(compressedMessages, effectiveWindow)
		if stableBoundary > 0 {
			hint := compression.PromptCacheHintCold
			if obs := p.observePromptCacheStability(sessionID, compressedMessages, stableBoundary); obs.Confidence == promptcache.ConfidenceHot {
				hint = compression.PromptCacheHintHot
				log.Debug("prompt_cache_stability_hot",
					"session_id", sessionID,
					"hit_count", obs.HitCount,
					"stable_boundary", stableBoundary)
			}
			compressedMessages = compression.OptimizeCacheBreakpointsHint(compressedMessages, stableBoundary, hint)
		}
	}

	compressedTokens := tokens.CountMessages(compressedMessages)

	// Zero-downside guarantee (docs/spec.md §1): if compression expanded the output,
	// revert to original messages so the proxy never makes things worse.
	if origTokens > 0 && compressedTokens > origTokens {
		log.Debug("compression expanded output, reverting to original",
			"orig", origTokens, "comp", compressedTokens)
		compressedMessages = messages
		compressedTokens = origTokens
		appliedLayers = nil
		layer0Savings = 0
		layer1Savings = 0
	}

	// --- 7. Reconstruct request body ---
	newBody, err := reconstructBodyFn(provider, body, compressedMessages)
	if err != nil {
		log.Error("body reconstruction failed", "error", err)
		p.proxyError(w, http.StatusInternalServerError, "request reconstruction failed")
		return
	}
	if proxyLayer0StatsNeedsArchiveRecoveryNote(l0Stats) {
		note := archiveRecoveryNoteText(p.config.Compression.OutputReduce.ArchiveRecoveryNoteText)
		noteReserved := p.reserveArchiveRecoveryNote(sessionID, true)
		if !noteReserved && strings.TrimSpace(sessionID) == "" {
			log.Warn("http archive recovery note missing session; reverting archive-backed refs")
			compressedMessages = messages
			compressedTokens = origTokens
			layer0Savings = 0
			layer1Savings = 0
			appliedLayers = nil
			l0Stats = l0Stats.withoutSavings()
			newBody, err = reconstructBodyFn(provider, body, compressedMessages)
			if err != nil {
				log.Error("body reconstruction failed after archive-note session fallback", "error", err)
				p.proxyError(w, http.StatusInternalServerError, "request reconstruction failed")
				return
			}
		} else if noteReserved {
			injectedBody, res := beterse.Inject(provider, newBody, note)
			if !res.Applied {
				p.forgetArchiveRecoveryNote(sessionID)
				log.Warn("http archive recovery note injection failed; reverting archive-backed refs")
				compressedMessages = messages
				compressedTokens = origTokens
				layer0Savings = 0
				layer1Savings = 0
				appliedLayers = nil
				l0Stats = l0Stats.withoutSavings()
				newBody, err = reconstructBodyFn(provider, body, compressedMessages)
				if err != nil {
					log.Error("body reconstruction failed after archive-note fallback", "error", err)
					p.proxyError(w, http.StatusInternalServerError, "request reconstruction failed")
					return
				}
			} else {
				newBody = injectedBody
				compressedTokens += tokens.ForProvider(provider).CountString(note)
			}
		}
	}
	p.recordCodexLayer0Stats(l0Stats)

	totalSaved := origTokens - compressedTokens
	compressionRatio := 1.0
	if origTokens > 0 {
		compressionRatio = float64(compressedTokens) / float64(origTokens)
	}

	// --- 7.5 Tool-definition pruning (T103/T151, Layer 3, default off) ---
	toolPruneSummary := dbg.ToolPruneSummary{Reason: "disabled"}
	toolPruneSessionKey := resolveToolPruneSessionKey(sessionID, reqID)
	preToolPruneBody := newBody
	if p.config.Compression.Tuning.ToolPruneEnabled && p.toolPrune != nil {
		toolPruneSummary = dbg.ToolPruneSummary{
			Reason:        "no_tools",
			SessionKeySet: toolPruneSessionKey != "",
		}
		// T103b: reattach previously-pruned tool definitions when the
		// current message text mentions a pruned tool by name. Runs
		// before the prune decision so a freshly reattached tool also
		// shows up in the active list and survives the next idle
		// check.
		reattachedToolNames := []string(nil)
		if mentions := messageMentionsAnyPrunedTool(messages, p.toolPrune, toolPruneSessionKey); len(mentions) > 0 {
			defs := p.toolPrune.PeekPrunedDefs(toolPruneSessionKey, mentions)
			if reattached, n, err := toolprune.ReattachToolDefinitions(newBody, provider, defs); err == nil && n > 0 {
				p.toolPrune.ForgetPrunedDefs(toolPruneSessionKey, mentions)
				newBody = reattached
				preToolPruneBody = newBody
				toolPruneSummary.Reattached += n
				reattachedToolNames = make([]string, 0, len(defs))
				for name := range defs {
					reattachedToolNames = append(reattachedToolNames, name)
				}
				for range n {
					p.toolPrune.MarkReattached()
				}
				log.Debug("tool-prune reattached",
					"count", n,
					"tools", mentions,
				)
			}
		}
		if toolNames, schemaSafe := toolprune.ExtractToolNamesForPruning(newBody, provider); !schemaSafe {
			toolPruneSummary.Reason = "unknown_tool_schema_full_pass"
		} else if len(toolNames) > 0 {
			usedToolNames := extractUsedToolNames(messages)
			usedToolNames = append(usedToolNames, reattachedToolNames...)
			p.toolPrune.ObserveTurn(toolPruneSessionKey, usedToolNames)
			decision := p.toolPrune.DecideWithOptions(toolPruneSessionKey, toolNames, toolprune.DecisionOptions{
				MinKeep:    1,
				AlwaysKeep: p.config.Compression.Tuning.ToolPruneAlwaysKeep,
			})
			toolPruneSummary.Reason = decision.Reason
			toolPruneSummary.AlwaysKept = decision.AlwaysKept
			toolPruneSummary.Cooldown = decision.Reason == "quality_cooldown"
			p.toolPrune.MarkAlwaysKept(decision.AlwaysKept)
			if len(decision.Pruned) > 0 {
				toPrune := make(map[string]bool, len(decision.Pruned))
				for _, n := range decision.Pruned {
					toPrune[n] = true
				}
				if prunedBody, removed, err := toolprune.PruneToolDefinitions(newBody, provider, toPrune); err == nil && len(removed) > 0 {
					savedEst := estimateTokensFromText(string(newBody)) - estimateTokensFromText(string(prunedBody))
					if savedEst > 0 {
						newBody = prunedBody
						toolPruneSummary.Applied = true
						toolPruneSummary.PrunedTools = len(removed)
						toolPruneSummary.SavedTokens = savedEst
						p.toolPrune.MarkPruned(savedEst)
						// T103b: cache pruned defs so a future turn
						// that mentions the tool name can reattach
						// them. The map iteration order is fine
						// because each (name, def) pair is independent.
						for name, def := range removed {
							p.toolPrune.RememberPrunedDef(toolPruneSessionKey, name, def)
						}
						log.Debug("tool-prune applied",
							"pruned", len(removed),
							"saved_est", savedEst,
						)
					}
				}
			}
		}
	}

	// --- 8. Output-token reduction (T130) ---
	outputReduceStats := outputreduce.Stats{Reason: "disabled"}
	outputReduceCooldown := false
	outputReduceMinTokens := p.config.Compression.OutputReduce.MinInputTokens
	taskShape := outputreduce.DetectTaskShape(provider, newBody)
	if p.config.Compression.OutputReduce.Enabled && p.isLayerEnabled(3) && compressedTokens >= outputReduceMinTokens {
		profileName := p.config.Compression.OutputReduce.Profile
		if configuredProfile, err := outputreduce.ParseProfile(profileName); err == nil {
			effective := outputreduce.ResolveProfile(provider, configuredProfile)
			effective = outputreduce.SafeProfileForShape(effective, taskShape)
			if p.outputReduce != nil {
				outputReduceCooldown = p.outputReduce.InCooldown(provider.String(), model, effective, taskShape)
				effective = p.outputReduce.SelectProfile(provider.String(), model, effective, taskShape)
			}
			profileName = string(effective)
		}
		opts := outputreduce.Options{
			Enabled:             true,
			Profile:             profileName,
			CustomDirectivePath: p.config.Compression.OutputReduce.CustomDirectivePath,
			SignatureMarker:     p.config.Compression.OutputReduce.SignatureMarker,
			MaxAddedBytes:       p.config.Compression.OutputReduce.MaxAddedBytes,
			TaskShape:           taskShape,
			InputTokens:         compressedTokens,
		}
		if injectedBody, stats, err := outputreduce.InjectBody(provider, newBody, opts); err != nil {
			log.Warn("output-reduce injection skipped", "error", err)
			outputReduceStats = outputreduce.Stats{Reason: "error"}
		} else {
			outputReduceStats = stats
			if stats.Applied {
				newBody = injectedBody
				log.Debug("output-reduce injected",
					"profile", stats.Profile,
					"added_tokens", stats.AddedTokens,
					"added_bytes", stats.AddedBytes,
				)
			}
		}
	} else if injectedBody, stats := p.injectConciseChatHint(provider, newBody, taskShape, messages, compressedTokens); stats.Applied {
		newBody = injectedBody
		outputReduceStats = stats
		compressedTokens += stats.AddedTokens
		totalSaved = origTokens - compressedTokens
		if origTokens > 0 {
			compressionRatio = float64(compressedTokens) / float64(origTokens)
		}
		log.Debug("concise-chat hint injected",
			"profile", stats.Profile,
			"added_tokens", stats.AddedTokens,
			"added_bytes", stats.AddedBytes,
			"shape", stats.TaskShape,
		)
	} else if p.config.Compression.OutputReduce.Enabled && p.isLayerEnabled(3) && compressedTokens < outputReduceMinTokens {
		outputReduceStats = outputreduce.Stats{Reason: "below_min_tokens"}
	}
	if p.outputReduce != nil {
		p.outputReduce.ObserveInjection(outputReduceStats)
	}
	if outputReduceStats.Applied && outputReduceStats.Profile != string(outputreduce.ProfileConciseChat) && outputReduceStats.AddedTokens > 0 {
		compressedTokens += outputReduceStats.AddedTokens
		totalSaved = origTokens - compressedTokens
		if origTokens > 0 {
			compressionRatio = float64(compressedTokens) / float64(origTokens)
		}
	}

	prePromptCacheBody := newBody
	promptCacheDecision := openAIPromptCacheDecision{Reason: "disabled"}
	if injectedBody, decision := p.injectOpenAIPromptCache(provider, newBody, model, compressedTokens, sessionID); decision.Applied {
		newBody = injectedBody
		promptCacheDecision = decision
		log.Debug("openai prompt-cache fields injected",
			"key_set", decision.Key != "",
			"retention", decision.Retention,
		)
	} else {
		promptCacheDecision = decision
	}

	// --- 8.5 Response cache lookup (Layer 2) ---
	var cacheKey [32]byte
	requestCacheSafe := p.isLayerEnabled(2) && caching.IsRequestCacheSafeWithRoute(effectiveRouteKey, newBody)
	if requestCacheSafe {
		cacheKey = p.responseCache.ComputeRequestKeyWithRoute(provider, effectiveRouteKey, newBody, r.Header)
		if cached, ok := p.responseCache.Get(cacheKey); ok {
			log.Debug("cache hit")
			for k, vv := range cached.Headers {
				for _, v := range vv {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(cached.StatusCode)
			w.Write(cached.Response) //nolint:errcheck

			cacheLayers := append(append([]int{}, appliedLayers...), 2)
			cacheLatencyMs := float64(time.Since(start).Microseconds()) / 1000.0
			outputTokens := estimateTokensFromText(string(cached.Response))

			if p.debugRecorder != nil {
				summary := dbg.RequestSummary{
					RequestID:          reqID,
					Timestamp:          start,
					Source:             "proxy",
					Provider:           provider.String(),
					ClientFamily:       clientFamily,
					Host:               r.Host,
					Path:               r.URL.Path,
					RouteMode:          "local_cache",
					Model:              model,
					TotalMessages:      len(messages),
					MessagesInWindow:   effectiveWindow,
					MessagesCompressed: max(0, len(messages)-effectiveWindow),
					LayersApplied:      cacheLayers,
					Tokens: dbg.TokenCounts{
						Original:    origTokens,
						AfterLayer1: origTokens - layer1Savings,
						Final:       compressedTokens,
						Saved:       totalSaved,
						Ratio:       compressionRatio,
					},
					Layer1Breakdown:   layer1Breakdown,
					CacheHit:          true,
					CacheReadTokens:   0,
					CacheCreateTokens: 0,
					OutputTokens:      outputTokens,
					ToolPrune:         toolPruneSummary,
					OutputReduce: dbg.OutputReduceSummary{
						Applied:     outputReduceStats.Applied,
						Profile:     outputReduceStats.Profile,
						Reason:      outputReduceStats.Reason,
						AddedTokens: outputReduceStats.AddedTokens,
						TaskShape:   string(outputReduceStats.TaskShape),
					},
					ProxyLatencyMs: cacheLatencyMs,
					ReReadCount:    reReadCount,
					NetSavedTokens: totalSaved,
					AdaptiveWindow: dbg.AdaptiveWindowSummary{
						Size:   windowDecision.Size,
						Score:  windowDecision.Score,
						Reason: windowDecision.Reason,
					},
					Plan: p.dryRunPlan(plannerInput{
						provider:                    provider,
						model:                       model,
						routeMode:                   "local_cache",
						estimatedInputTokens:        origTokens,
						expectedOutputTokens:        outputTokens,
						taskShape:                   string(outputReduceStats.TaskShape),
						contentClasses:              plannerClassesFromMessages(messages),
						recentEdit:                  recentEditFact,
						providerCacheSupported:      true,
						previousResponseIDAvailable: false,
						toolPruneCooldown:           toolPruneSummary.Cooldown,
						outputReduceCooldown:        outputReduceCooldown,
						liveCorpusConfidence:        p.plannerLiveCorpusConfidence(),
						negativeSavingsHistory:      totalSaved < 0,
					}),
				}
				p.debugRecorder.Record(summary)
				p.observeQuality(summary)
			}

			p.trySendAnalytics(types.AnalyticsEvent{
				Type:                    types.EventRequestProcessed,
				Timestamp:               time.Now(),
				Provider:                provider,
				Model:                   model,
				InputTokensOrig:         origTokens,
				InputTokensComp:         compressedTokens,
				OutputTokens:            outputTokens,
				CompressionRatio:        compressionRatio,
				Layers:                  cacheLayers,
				LatencyMs:               cacheLatencyMs,
				CacheHit:                true,
				TokensSaved:             totalSaved,
				OutputReduceApplied:     outputReduceStats.Applied,
				OutputReduceProfile:     outputReduceStats.Profile,
				OutputReduceReason:      outputReduceStats.Reason,
				OutputReduceAddedTokens: outputReduceStats.AddedTokens,
				OutputReduceTaskShape:   string(outputReduceStats.TaskShape),
			})

			log.Info("request_processed",
				"input_orig", origTokens,
				"input_comp", compressedTokens,
				"saved", totalSaved,
				"output", outputTokens,
				"ratio", fmt.Sprintf("%.2f", compressionRatio),
				"layers", cacheLayers,
				"cache_hit", true,
				"latency_ms", fmt.Sprintf("%.2f", cacheLatencyMs),
				"proxy_overhead_ms", fmt.Sprintf("%.2f", cacheLatencyMs),
			)
			return
		}
	}

	latencyStart := time.Now()
	upstreamStart := latencyStart

	// --- 8.5 Server-state lever (T78, default on with fail-open) ---
	upstreamBody := newBody
	serverStateKey := ""
	serverStateUsed := false
	if p.config.Proxy.ServerStateEnabled && p.serverState != nil {
		caps := types.CapabilitiesFor(provider)
		if caps.SupportsResponseID {
			if key := extractServerStateKey(provider, body); key != "" {
				serverStateKey = key
				if prevID := p.serverState.Get(key); prevID != "" {
					if rewritten, ok := rewriteWithPreviousID(provider, newBody, prevID); ok {
						upstreamBody = rewritten
						serverStateUsed = true
						p.serverState.MarkSkipped()
					}
				}
			}
		}
	}

	// --- 8.7 Trailing-commentary stop-sequence injection (T165) ---
	// Inject curated phrases into the upstream-bound body after all
	// body mutations so server-state rewrites do not stomp our edit.
	// Cache key (computed upstream in 8.5) is on the pre-injection
	// body so cached responses stay reachable regardless of stop-seq
	// policy.
	if p.config.Compression.OutputReduce.StopSequencesEnabled {
		if injected, res := outstop.MergeIntoBody(provider, upstreamBody); res.OK && res.AddedCount > 0 {
			upstreamBody = injected
			p.outputReduceCounters.RecordStopSeqInjection(res.AddedCount)
			log.Debug("outstop merged",
				"provider", provider.String(),
				"field", res.FieldUsed,
				"added", res.AddedCount,
			)
		}
	}

	// --- 8.8 Be-terse hint injection (T169, qualityab-gated) ---
	// Gated by config toggle (default off) + per-session cohort
	// routing via internal/qualityab. Treatment cohort gets the
	// hint; control cohort sees the original body. Harness
	// auto-rolls-back when treatment failure rate exceeds control's
	// by 5pp on 50+ samples.
	abCohort := qualityab.CohortControl
	beterseInjected := false
	if p.config.Compression.OutputReduce.BeTerseHintEnabled && p.qualityAB != nil {
		abCohort = p.qualityAB.Cohort(sessionID)
		if abCohort == qualityab.CohortTreatment {
			if injected, res := beterse.Inject(provider, upstreamBody, p.config.Compression.OutputReduce.BeTerseHintText); res.Applied {
				upstreamBody = injected
				beterseInjected = true
				p.outputReduceCounters.RecordBeTerseInjection(res.Bytes)
				log.Debug("be-terse hint injected",
					"provider", provider.String(),
					"field", res.FieldUsed,
					"bytes", res.Bytes,
					"cohort", string(abCohort),
				)
			}
		}
	}

	// --- 9. Forward to upstream ---
	upstreamResp, err := p.doUpstreamRequest(r, provider, upstreamBody)
	if unsupportedFields, unsupportedPromptCache := peekPromptCacheUnsupportedFields(upstreamResp); err == nil && promptCacheDecision.Applied && upstreamResp != nil && unsupportedPromptCache {
		log.Warn("openai prompt-cache fields rejected, retrying without cache hints",
			"reason", promptCacheDecision.Reason)
		p.markOpenAIPromptCacheRejected(provider, model, unsupportedFields, time.Now())
		upstreamResp.Body.Close()
		upstreamBody = prePromptCacheBody
		promptCacheDecision = openAIPromptCacheDecision{Reason: "rejected_retry"}
		if p.config.Proxy.ServerStateEnabled && p.serverState != nil && serverStateUsed {
			if rewritten, ok := rewriteWithPreviousID(provider, prePromptCacheBody, p.serverState.Get(serverStateKey)); ok {
				upstreamBody = rewritten
			}
		}
		upstreamResp, err = p.doUpstreamRequest(r, provider, upstreamBody)
	}
	if err == nil && toolPruneSummary.Applied && upstreamResp != nil && peekMissingToolDefinitionError(upstreamResp) {
		log.Warn("tool-prune missing-tool response, retrying with full tool schema",
			"session_key_set", toolPruneSessionKey != "",
			"pruned_tools", toolPruneSummary.PrunedTools)
		if p.toolPrune != nil {
			p.toolPrune.MarkMiss(toolPruneSessionKey)
			p.toolPrune.MarkRetry()
		}
		toolPruneSummary.Miss = true
		toolPruneSummary.Retry = true
		toolPruneSummary.Cooldown = true
		upstreamResp.Body.Close()
		upstreamBody = p.rewriteToolPruneRetryBody(provider, preToolPruneBody, serverStateUsed, serverStateKey)
		upstreamResp, err = p.doUpstreamRequest(r, provider, upstreamBody)
	}
	if err == nil && serverStateUsed && upstreamResp != nil {
		if shouldRecover, _ := peekUnknownPreviousIDError(upstreamResp); shouldRecover {
			log.Warn("server-state previous_response_id rejected, retrying with full body",
				"session", serverStateKey)
			p.serverState.Forget(serverStateKey)
			p.serverState.MarkRecover()
			upstreamResp.Body.Close()
			upstreamResp, err = p.doUpstreamRequest(r, provider, newBody)
		}
	}
	if err != nil {
		p.pipelineHist.Upstream.Record(time.Since(upstreamStart))
		p.pipelineHist.Total.Record(time.Since(start))
		p.healthMon.record(provider, false)
		log.Error("upstream request failed", "error", err)
		p.proxyError(w, http.StatusBadGateway, fmt.Sprintf("upstream error: %v", err))
		p.trySendAnalytics(types.AnalyticsEvent{
			Type:      types.EventErrorOccurred,
			Timestamp: time.Now(),
			Provider:  provider,
			Error:     err.Error(),
		})
		return
	}
	p.pipelineHist.Upstream.Record(time.Since(upstreamStart))
	p.healthMon.record(provider, upstreamResp.StatusCode < 500)

	// Report be-terse cohort outcome to the qualityab harness so
	// auto-rollback can fire if treatment-side failures pile up.
	// Treatment outcomes are only recorded when the hint was
	// actually injected - if Inject returned !Applied (e.g. body
	// shape mismatch), the request didn't carry the lever and
	// reporting it as treatment would misattribute the result.
	// Control cohort always records (we need its baseline).
	if p.config.Compression.OutputReduce.BeTerseHintEnabled && p.qualityAB != nil {
		recordCohort := abCohort
		if abCohort == qualityab.CohortTreatment && !beterseInjected {
			recordCohort = qualityab.CohortControl
		}
		outcome := qualityab.OutcomeSuccess
		if upstreamResp.StatusCode >= 400 {
			outcome = qualityab.OutcomeUpstreamError
		}
		p.qualityAB.RecordOutcome(recordCohort, outcome)
	}

	// --- 9. Stream / passthrough response ---
	var outputTokens int
	var responseBody []byte
	var upstreamCacheUsage cacheUsage

	if isStreamingRequest(body) {
		var cutter *streamcut.Cutter
		if p.config.Compression.OutputReduce.StreamCutEnabled && outstop.ShapeAllowsStopOptimization(taskShape) {
			// 3-line holdback (T184): the trailing-commentary opener
			// is queued, so when the cutter fires the opener bytes
			// never reach the client. Lossless for natural stream
			// ends - Flush emits any queued lines.
			cutter = streamcut.NewCutterWithHoldback(provider.String(), 3)
		}
		var fire streamCutFire
		outputTokens, upstreamCacheUsage, fire = streamingRelayWithCutter(r.Context(), w, upstreamResp, provider.String(), cutter)
		if fire.Fired {
			p.outputReduceCounters.RecordStreamcutFire(fire.BytesObserved)
			log.Debug("streamcut terminated upstream", "bytes_observed", fire.BytesObserved)
		}
	} else {
		// T167: for non-streaming responses we build a per-request
		// repdet Index from tool_result blocks and rewrite any
		// verbatim echo into a "[unchanged: <name>]" marker before
		// forwarding to the client. Per-provider helpers handle their
		// own wire shape; providers we don't recognise fall through to
		// plain passthrough so we never break a response to optimise.
		responseBody = p.passthroughWithOptionalRepdet(w, upstreamResp, provider, messages, log)
		outputTokens = estimateTokensFromText(string(responseBody))
		upstreamCacheUsage = extractCacheUsageFromBody(provider.String(), responseBody)
		if upstreamCacheUsage.OutputTokens > 0 {
			outputTokens = upstreamCacheUsage.OutputTokens
		}
	}
	if p.outputReduce != nil {
		p.outputReduce.ObserveOutput(outputTokens)
		outcome := outputreduce.Outcome{
			Provider:            provider.String(),
			Model:               model,
			Profile:             outputReduceStats.Profile,
			TaskShape:           outputReduceStats.TaskShape,
			Applied:             outputReduceStats.Applied,
			InputOverheadTokens: outputReduceStats.AddedTokens,
			OutputTokens:        outputTokens,
			Failed:              upstreamResp.StatusCode >= 400,
			RepairSignal:        repairSignal.Repair,
			UserReaskSignal:     repairSignal.UserReask,
		}
		p.outputReduce.ObserveOutcome(outcome)
		p.rememberOutputReduceSignal(sessionID, outcome)
	}

	// --- 9b. Server-state response-id capture (T78) ---
	if p.config.Proxy.ServerStateEnabled && p.serverState != nil &&
		serverStateKey != "" && responseBody != nil &&
		upstreamResp.StatusCode == http.StatusOK {
		if id := extractResponseID(provider, responseBody); id != "" {
			p.serverState.Set(serverStateKey, id)
		}
	}

	// --- 9c. T76c: opportunistic archive re-injection signal. When the
	// upstream response echoes a `local-archive://<id>` URI, count it
	// so /admin/status.content_archive.re_inject_count reflects the
	// model's actual reach into archived content. The next request
	// will re-expand the URI through reinjectArchivedContent.
	if responseBody != nil && upstreamResp.StatusCode == http.StatusOK {
		if ids := extractArchiveIDs(string(responseBody)); len(ids) > 0 {
			home, err := os.UserHomeDir()
			if err == nil {
				contentarchive.RecordReInjectBatch(contentarchive.DefaultDir(home), len(ids))
			}
		}
	}

	// T28: feed the observed upstream input_tokens back into the
	// per-provider tokenizer so its bytes-per-token ratio converges on
	// reality over time.
	if upstreamCacheUsage.InputTokens > 0 {
		tokens.ObserveUpstreamUsage(provider, model, upstreamCacheUsage.InputTokens, compressedTokens)
	}
	p.observeOpenAIPromptCacheNet(provider, model, promptCacheDecision, upstreamCacheUsage, time.Now())

	proxyLatencyMs := float64(time.Since(latencyStart).Microseconds()) / 1000.0
	p.pipelineHist.Total.Record(time.Since(start))

	// --- 10. Cache successful response (Layer 2) ---
	if requestCacheSafe && responseBody != nil && upstreamResp.StatusCode == http.StatusOK {
		dependencyPaths := caching.ExtractDependencyPaths(body)
		canCacheResponse := true
		if len(dependencyPaths) > 0 {
			if p.fileWatcher == nil {
				canCacheResponse = false
				log.Warn("skipping layer2 cache store because dependency invalidation is unavailable",
					"dependency_paths", len(dependencyPaths))
			} else {
				for _, path := range dependencyPaths {
					if err := p.fileWatcher.Watch(path); err != nil {
						canCacheResponse = false
						log.Warn("skipping layer2 cache store because dependency watch failed",
							"path", path,
							"error", err)
						break
					}
					if !p.fileWatcher.IsWatching(path) {
						canCacheResponse = false
						log.Warn("skipping layer2 cache store because dependency watch was not armed",
							"path", path)
						break
					}
				}
			}
		}
		if canCacheResponse {
			entry := &caching.CacheEntry{
				Response:        responseBody,
				Headers:         make(map[string][]string),
				StatusCode:      upstreamResp.StatusCode,
				CreatedAt:       time.Now(),
				TokensSaved:     origTokens - compressedTokens,
				DependencyPaths: dependencyPaths,
			}
			// Copy headers for cache.
			maps.Copy(entry.Headers, upstreamResp.Header)
			p.responseCache.Set(cacheKey, entry)
			// Register the Stage A pointer (T20) so the next identical
			// original request can skip the compression pipeline entirely.
			if stageAEnabled {
				p.responseCache.RegisterOriginalPointer(stageACacheKey, cacheKey)
			}
		}
	}

	// --- Debug decision recording ---
	if p.debugRecorder != nil {
		cacheReadTokens, providerCachedTokens := splitProviderCacheUsage(provider, upstreamCacheUsage)
		summary := dbg.RequestSummary{
			RequestID:          reqID,
			Timestamp:          start,
			SessionID:          sessionID,
			Source:             "proxy",
			Provider:           provider.String(),
			ClientFamily:       clientFamily,
			Host:               r.Host,
			Path:               r.URL.Path,
			RouteMode:          "upstream",
			Model:              model,
			TotalMessages:      len(messages),
			MessagesInWindow:   effectiveWindow,
			MessagesCompressed: max(0, len(messages)-effectiveWindow),
			LayersApplied:      appliedLayers,
			Tokens: dbg.TokenCounts{
				Original:    origTokens,
				AfterLayer0: origTokens - layer0Savings,
				AfterLayer1: origTokens - layer0Savings - layer1Savings,
				Final:       compressedTokens,
				Saved:       totalSaved,
				Ratio:       compressionRatio,
			},
			Layer1Breakdown:      layer1Breakdown,
			Layer1Decisions:      layer1Decisions,
			EvidenceDecisions:    l0Stats.EvidenceDecisions,
			CacheHit:             false,
			CacheReadTokens:      cacheReadTokens,
			CacheCreateTokens:    upstreamCacheUsage.CreateTokens,
			ProviderInputTokens:  upstreamCacheUsage.InputTokens,
			ProviderCachedTokens: providerCachedTokens,
			ProviderOutputTokens: outputTokens,
			OutputTokens:         outputTokens,
			PromptCache: dbg.PromptCacheSummary{
				Applied:            promptCacheDecision.Applied,
				Reason:             promptCacheDecision.Reason,
				KeySet:             promptCacheDecision.Key != "",
				Retention:          promptCacheDecision.Retention,
				StablePrefixHash:   promptCacheDecision.StablePrefixHash,
				StablePrefixTokens: promptCacheDecision.StablePrefixTokens,
			},
			ToolPrune: toolPruneSummary,
			OutputReduce: dbg.OutputReduceSummary{
				Applied:     outputReduceStats.Applied,
				Profile:     outputReduceStats.Profile,
				Reason:      outputReduceStats.Reason,
				AddedTokens: outputReduceStats.AddedTokens,
				TaskShape:   string(outputReduceStats.TaskShape),
			},
			PreviousResponseIDUsed: serverStateUsed,
			ProxyLatencyMs:         proxyLatencyMs,
			ReReadCount:            reReadCount,
			NetSavedTokens:         totalSaved,
			AdaptiveWindow: dbg.AdaptiveWindowSummary{
				Size:   windowDecision.Size,
				Score:  windowDecision.Score,
				Reason: windowDecision.Reason,
			},
			Plan: p.dryRunPlan(plannerInput{
				provider:                    provider,
				model:                       model,
				routeMode:                   "upstream",
				estimatedInputTokens:        origTokens,
				expectedOutputTokens:        outputTokens,
				taskShape:                   string(outputReduceStats.TaskShape),
				contentClasses:              plannerClassesFromMessages(messages),
				recentEdit:                  recentEditFact,
				providerCacheSupported:      promptCacheDecision.Applied || upstreamCacheUsage.ReadTokens > 0 || upstreamCacheUsage.CreateTokens > 0,
				previousResponseIDAvailable: serverStateUsed,
				toolPruneCooldown:           toolPruneSummary.Cooldown,
				outputReduceCooldown:        outputReduceCooldown,
				liveCorpusConfidence:        p.plannerLiveCorpusConfidence(),
				negativeSavingsHistory:      totalSaved < 0,
			}),
		}
		p.debugRecorder.Record(summary)
		p.observeQuality(summary)
	}

	p.trySendAnalytics(types.AnalyticsEvent{
		Type:                    types.EventRequestProcessed,
		Timestamp:               time.Now(),
		Provider:                provider,
		Model:                   model,
		InputTokensOrig:         origTokens,
		InputTokensComp:         compressedTokens,
		OutputTokens:            outputTokens,
		CompressionRatio:        compressionRatio,
		Layers:                  appliedLayers,
		LatencyMs:               proxyLatencyMs,
		TokensSaved:             totalSaved,
		CacheReadTokens:         upstreamCacheUsage.ReadTokens,
		CacheCreateTokens:       upstreamCacheUsage.CreateTokens,
		OutputReduceApplied:     outputReduceStats.Applied,
		OutputReduceProfile:     outputReduceStats.Profile,
		OutputReduceReason:      outputReduceStats.Reason,
		OutputReduceAddedTokens: outputReduceStats.AddedTokens,
		OutputReduceTaskShape:   string(outputReduceStats.TaskShape),
	})

	log.Info("request_processed",
		"input_orig", origTokens,
		"input_comp", compressedTokens,
		"saved", totalSaved,
		"output", outputTokens,
		"ratio", fmt.Sprintf("%.2f", compressionRatio),
		"layers", appliedLayers,
		"latency_ms", fmt.Sprintf("%.2f", proxyLatencyMs),
		"proxy_overhead_ms", fmt.Sprintf("%.2f", float64(time.Since(start).Microseconds())/1000.0),
	)
}

func (p *Proxy) passthroughWithOptionalRepdet(w http.ResponseWriter, upstreamResp *http.Response, provider types.Provider, messages []types.Message, log *slog.Logger) []byte {
	if !p.config.Compression.OutputReduce.RepetitionDetectionEnabled {
		return passthrough(w, upstreamResp)
	}
	switch provider {
	case types.Anthropic:
		return p.passthroughAnthropicWithRepdet(w, upstreamResp, messages, log)
	case types.OpenAI, types.CodexChatGPT:
		return p.passthroughOpenAIWithRepdet(w, upstreamResp, messages, log)
	default:
		return passthrough(w, upstreamResp)
	}
}

// serveStageACacheHit writes a cached response for a Stage A hit (pre-compression).
// The entire compression pipeline is skipped; only Layer 2 is reported as applied.
// Spec: T20, double-keyed cache.
func (p *Proxy) serveStageACacheHit(
	w http.ResponseWriter,
	cached *caching.CacheEntry,
	reqID string,
	start time.Time,
	provider types.Provider,
	model string,
	totalMessages int,
	origTokens int,
	log *slog.Logger,
	aw windowDecision,
) {
	for k, vv := range cached.Headers {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(cached.StatusCode)
	_, _ = w.Write(cached.Response)

	latencyMs := float64(time.Since(start).Microseconds()) / 1000.0
	outputTokens := estimateTokensFromText(string(cached.Response))

	if p.debugRecorder != nil {
		summary := dbg.RequestSummary{
			RequestID:          reqID,
			Timestamp:          start,
			Source:             "proxy",
			Provider:           provider.String(),
			RouteMode:          "local_cache",
			Model:              model,
			TotalMessages:      totalMessages,
			MessagesInWindow:   aw.Size,
			MessagesCompressed: 0,
			LayersApplied:      []int{2},
			Tokens: dbg.TokenCounts{
				Original:    origTokens,
				AfterLayer1: origTokens,
				Final:       origTokens,
				Saved:       0,
				Ratio:       1.0,
			},
			CacheHit:          true,
			CacheReadTokens:   0,
			CacheCreateTokens: 0,
			OutputTokens:      outputTokens,
			ProxyLatencyMs:    latencyMs,
			ReReadCount:       0,
			NetSavedTokens:    0,
			AdaptiveWindow: dbg.AdaptiveWindowSummary{
				Size:   aw.Size,
				Score:  aw.Score,
				Reason: aw.Reason,
			},
			Plan: p.dryRunPlan(plannerInput{
				provider:                    provider,
				model:                       model,
				routeMode:                   "local_cache",
				estimatedInputTokens:        origTokens,
				expectedOutputTokens:        outputTokens,
				contentClasses:              []string{"conversation"},
				providerCacheSupported:      true,
				previousResponseIDAvailable: false,
				liveCorpusConfidence:        p.plannerLiveCorpusConfidence(),
			}),
		}
		p.debugRecorder.Record(summary)
		p.observeQuality(summary)
	}

	p.trySendAnalytics(types.AnalyticsEvent{
		Type:             types.EventRequestProcessed,
		Timestamp:        time.Now(),
		Provider:         provider,
		Model:            model,
		InputTokensOrig:  origTokens,
		InputTokensComp:  origTokens,
		OutputTokens:     outputTokens,
		CompressionRatio: 1.0,
		Layers:           []int{2},
		LatencyMs:        latencyMs,
		CacheHit:         true,
		TokensSaved:      0,
	})

	log.Info("request_processed",
		"input_orig", origTokens,
		"input_comp", origTokens,
		"saved", 0,
		"output", outputTokens,
		"ratio", "1.00",
		"layers", []int{2},
		"cache_hit", true,
		"cache_stage", "A",
		"latency_ms", fmt.Sprintf("%.2f", latencyMs),
		"proxy_overhead_ms", fmt.Sprintf("%.2f", latencyMs),
	)
}

// buildLayer1Breakdown converts a Layer1Result into a per-sub-layer map for debug recording.
func buildLayer1Breakdown(r compression.Layer1Result) map[string]dbg.SubLayerBreakdown {
	bd := make(map[string]dbg.SubLayerBreakdown, 10)
	addBD := func(name string, saved int) {
		if saved > 0 {
			bd[name] = dbg.SubLayerBreakdown{Blocks: 1, Saved: saved}
		}
	}
	addBD("ansi_strip", r.ANSISaved)
	addBD("json_compact", r.JSONSaved)
	addBD("comment_strip", r.CommentSaved)
	addBD("dedup", r.DedupSaved)
	addBD("structure_extract", r.StructureSaved)
	addBD("delta_encoding", r.DeltaSaved)
	addBD("success_shortcircuit", r.SuccessShortSaved)
	addBD("tool_compressor", r.ToolCompressorSaved)
	addBD("image_replace", r.ImageSaved)
	addBD("semantic_dictionary", r.DictionarySaved)
	addBD("repeated_collapse", r.RepeatedCollapseSaved)
	addBD("graph_pruning", r.GraphPruningSaved)
	return bd
}

func buildLayer1Decisions(r compression.Layer1Result) []dbg.Layer1DecisionSummary {
	if len(r.Decisions) == 0 {
		return nil
	}
	out := make([]dbg.Layer1DecisionSummary, 0, len(r.Decisions))
	for _, decision := range r.Decisions {
		out = append(out, dbg.Layer1DecisionSummary{
			SubLayer:        decision.SubLayer,
			Tier:            string(decision.Tier),
			Attempted:       decision.Attempted,
			Applied:         decision.Applied,
			Reason:          decision.Reason,
			SavedTokens:     decision.SavedTokens,
			RequiresArchive: decision.RequiresArchive,
			ArchiveWrites:   decision.ArchiveWrites,
			Recovery:        decision.Recovery,
			DefaultEligible: decision.DefaultEligible,
		})
	}
	return out
}

// doUpstreamRequest builds and executes the upstream HTTP request.
// Rate-limit (429) and overload (529) responses are retried with exponential backoff
// via a direct status-code-only loop (spec §17.3) - body is never buffered for 200/SSE.
// Context overflow (400) is handled separately with aggressive recompression (spec §17.4).
func (p *Proxy) doUpstreamRequest(r *http.Request, provider types.Provider, body []byte) (*http.Response, error) {
	upstreamURL := p.upstreamURL(provider, r.URL.Path, r.URL.RawQuery)

	// Build the forwarded header set once; reused across retry attempts.
	fwdHeaders := make(http.Header)
	for k, vv := range r.Header {
		switch k {
		case "Host", "Content-Length", "Connection", "Transfer-Encoding":
			continue
		}
		for _, v := range vv {
			fwdHeaders.Add(k, v)
		}
	}
	fwdHeaders.Set("Content-Type", "application/json")

	client := p.httpClients[provider]
	if client == nil {
		client = http.DefaultClient
	}

	// buildReq creates a fresh request per attempt (body reader is consumed each time).
	buildReq := func(b []byte) (*http.Request, error) {
		encoding, _ := r.Context().Value(requestBodyEncodingKey{}).(requestBodyEncoding)
		wireBody, err := encodeRequestBodyForPipeline(b, encoding)
		if err != nil {
			return nil, err
		}
		req, err := newRequestWithContextFn(r.Context(), r.Method, upstreamURL, bytes.NewReader(wireBody))
		if err != nil {
			return nil, fmt.Errorf("build upstream request: %w", err)
		}
		for k, vv := range fwdHeaders {
			for _, v := range vv {
				req.Header.Add(k, v)
			}
		}
		req.ContentLength = int64(len(wireBody))
		return req, nil
	}

	// §17.3: direct retry loop for 429/529.
	// Must NOT use resilience.Do here: resilience.Do calls io.ReadAll on every response body,
	// which would buffer complete SSE streams in memory and break streaming entirely.
	// Instead: check status code only; never touch the body for non-error responses.
	const maxRateLimitRetries = 2
	var (
		resp *http.Response
		err  error
	)
	for attempt := 0; attempt <= maxRateLimitRetries; attempt++ {
		if r.Context().Err() != nil {
			return nil, r.Context().Err()
		}
		req, buildErr := buildReq(body)
		if buildErr != nil {
			return nil, buildErr
		}
		resp, err = client.Do(req)
		if err != nil {
			return nil, err
		}
		isRateLimited := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 529
		if !isRateLimited || attempt == maxRateLimitRetries {
			break
		}
		// Rate limited: body is a small JSON error - safe to discard immediately.
		resp.Body.Close()
		backoff := parseRetryAfter(resp.Header.Get("Retry-After"))
		if backoff == 0 {
			backoff = resilience.ExponentialBackoff(attempt, time.Second, 30*time.Second)
		}
		slog.Warn("rate limited, retrying", "attempt", attempt+1, "backoff", backoff, "status", resp.StatusCode)
		p.trySendAnalytics(types.AnalyticsEvent{
			Type:      types.EventRateLimitRetry,
			Timestamp: time.Now(),
			Provider:  provider,
		})
		select {
		case <-r.Context().Done():
			return nil, r.Context().Err()
		case <-time.After(backoff):
		}
	}

	// §17.4: context overflow recovery - retry with aggressive recompression, then original body.
	if resp.StatusCode == http.StatusBadRequest {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		resp.Body.Close()
		if isContextOverflow(bodyBytes) {
			p.trySendAnalytics(types.AnalyticsEvent{
				Type:      types.EventOverflowRetry,
				Timestamp: time.Now(),
				Provider:  provider,
			})
			if err := r.Context().Err(); err != nil {
				return nil, err
			}
			if stash, ok := r.Context().Value(pipelineStashKey{}).(pipelineStash); ok {
				if aggBody, err := p.buildAggressiveCompressedBodyContext(r.Context(), stash); err == nil && len(aggBody) > 0 {
					slog.Warn("context overflow: retrying with aggressive compression (sliding_window=2, summary target 10%)")
					if aggReq, err := buildReq(aggBody); err == nil {
						resp2, err2 := client.Do(aggReq)
						if err2 == nil && resp2.StatusCode != http.StatusBadRequest {
							return resp2, nil
						}
						if resp2 != nil {
							resp2.Body.Close()
						}
					}
				} else if ctxErr := r.Context().Err(); ctxErr != nil {
					return nil, ctxErr
				}
			}
			if err := r.Context().Err(); err != nil {
				return nil, err
			}
			slog.Warn("context overflow detected, retrying with original body")
			origBody := p.getOriginalBody(r)
			origReq, err := buildReq(origBody)
			if err != nil {
				return nil, fmt.Errorf("build overflow fallback request: %w", err)
			}
			return client.Do(origReq)
		}
		// Non-overflow 400: restore body for caller.
		resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	return resp, nil
}

// parseRetryAfter parses the Retry-After response header and returns how long to wait.
// Supports both integer-seconds and HTTP-date formats. Returns 0 if absent or unparseable.
// Result is capped at 30 seconds per spec §17.3.
func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}
	if secs, err := strconv.Atoi(header); err == nil {
		d := min(time.Duration(secs)*time.Second, 30*time.Second)
		return d
	}
	if t, err := http.ParseTime(header); err == nil {
		d := time.Until(t)
		if d < 0 {
			return 0
		}
		if d > 30*time.Second {
			d = 30 * time.Second
		}
		return d
	}
	return 0
}

// getOriginalBody returns the original uncompressed body.
// After handler reads the body, we stash it in the request context.
func (p *Proxy) getOriginalBody(r *http.Request) []byte {
	if v := r.Context().Value(origBodyKey{}); v != nil {
		return v.([]byte)
	}
	return nil
}

type origBodyKey struct{}

// isContextOverflow checks if a 400 response body indicates a context length error.
func isContextOverflow(body []byte) bool {
	return bytes.Contains(body, []byte("context_length_exceeded")) ||
		bytes.Contains(body, []byte("prompt too long")) ||
		bytes.Contains(body, []byte("maximum context length"))
}

// buildAggressiveCompressedBodyContext re-runs Layer 1 with a minimal sliding
// window.
func (p *Proxy) buildAggressiveCompressedBodyContext(ctx context.Context, stash pipelineStash) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg := p.config.Compression
	cfg.SlidingWindow = aggressiveSlidingWindow(cfg.Tuning.OverflowSlidingWindow)
	l1 := compression.NewDeterministicCompressor(&cfg)
	msgs := l1.Compress(stash.messages).Messages

	return reconstructBodyFn(stash.provider, stash.origBody, msgs)
}

// aggressiveSlidingWindow returns the configured overflow sliding window,
// defaulting to 2 when the tuning block is empty (legacy configs).
func aggressiveSlidingWindow(v int) int {
	if v < 1 {
		return 2
	}
	return v
}

// proxyError writes an error response to the client.
func (p *Proxy) proxyError(w http.ResponseWriter, statusCode int, msg string) {
	http.Error(w, msg, statusCode)
}

// handlePassthrough proxies a request to upstream without any modification.
func resolveToolPruneSessionKey(sessionID string, reqID string) string {
	if sessionID != "" {
		return sessionID
	}
	return reqID
}

func (p *Proxy) injectConciseChatHint(provider types.Provider, body []byte, taskShape outputreduce.TaskShape, messages []types.Message, inputTokens int) ([]byte, outputreduce.Stats) {
	stats := outputreduce.Stats{Reason: "disabled", Profile: string(outputreduce.ProfileConciseChat), TaskShape: taskShape}
	if p == nil || p.config == nil || !p.config.Compression.OutputReduce.Enabled || !p.config.Compression.OutputReduce.ConciseChatEnabled || !p.isLayerEnabled(3) {
		return body, stats
	}
	if messagesContainToolResult(messages) {
		stats.Reason = "tool_context_full_pass"
		return body, stats
	}
	shape, reason := outputreduce.ConciseChatEligibility(provider, body, taskShape)
	stats.TaskShape = shape
	if reason != "" {
		stats.Reason = reason
		return body, stats
	}
	if inputTokens < p.config.Compression.OutputReduce.ConciseChatMinInputTokens {
		stats.Reason = "concise_chat_low_roi"
		return body, stats
	}
	hint := beterse.ConciseChatHint(p.config.Compression.OutputReduce.ConciseChatText)
	injected, res := beterse.Inject(provider, body, hint)
	if !res.Applied {
		stats.Reason = "unsupported_shape"
		return body, stats
	}
	stats.Applied = true
	stats.Reason = "applied"
	stats.AddedBytes = res.Bytes
	stats.AddedTokens = estimateTokensFromText(hint)
	return injected, stats
}

func (p *Proxy) rewriteToolPruneRetryBody(provider types.Provider, preToolPruneBody []byte, serverStateUsed bool, serverStateKey string) []byte {
	if p.config.Proxy.ServerStateEnabled && p.serverState != nil && serverStateUsed {
		if rewritten, ok := rewriteWithPreviousID(provider, preToolPruneBody, p.serverState.Get(serverStateKey)); ok {
			return rewritten
		}
	}
	return preToolPruneBody
}

func (p *Proxy) handlePassthrough(w http.ResponseWriter, r *http.Request, provider types.Provider, body []byte) {
	p.handlePassthroughWithAttribution(w, r, provider, body, body, "passthrough")
}

func (p *Proxy) handlePassthroughWithAttribution(w http.ResponseWriter, r *http.Request, provider types.Provider, body []byte, attributionBody []byte, reason string) {
	start := time.Now()
	upstreamURL := p.upstreamURL(provider, r.URL.Path, r.URL.RawQuery)

	upstreamReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, bytes.NewReader(body))
	if err != nil {
		p.proxyError(w, http.StatusBadGateway, "build request failed")
		return
	}

	for k, vv := range r.Header {
		switch k {
		case "Host", "Content-Length", "Connection", "Transfer-Encoding":
			continue
		}
		for _, v := range vv {
			upstreamReq.Header.Add(k, v)
		}
	}
	if len(body) > 0 {
		upstreamReq.ContentLength = int64(len(body))
		if upstreamReq.Header.Get("Content-Type") == "" {
			upstreamReq.Header.Set("Content-Type", "application/json")
		}
	}

	client := p.httpClients[provider]
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(upstreamReq)
	if err != nil {
		p.proxyError(w, http.StatusBadGateway, fmt.Sprintf("upstream: %v", err))
		return
	}
	p.recordPassthroughFlight(r, provider, attributionBody, reason, start)

	if isStreamingRequest(body) {
		streamingRelay(r.Context(), w, resp, provider.String())
	} else {
		passthrough(w, resp)
	}
}

func (p *Proxy) recordPassthroughFlight(r *http.Request, provider types.Provider, body []byte, reason string, start time.Time) {
	if p == nil || p.debugRecorder == nil || r == nil || r.URL == nil || provider != types.CodexChatGPT {
		return
	}
	sessionID := extractSessionID(provider, body, r.Header)
	clientFamily := extractClientFamily(provider, body, r.Header)
	model := extractModel(body)
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "passthrough"
	}
	p.debugRecorder.Record(dbg.RequestSummary{
		RequestID:      newRequestIDFn(),
		Timestamp:      start,
		SessionID:      sessionID,
		Source:         "proxy",
		Provider:       provider.String(),
		Host:           r.Host,
		Path:           r.URL.Path,
		ClientFamily:   clientFamily,
		RouteMode:      "passthrough",
		BypassReason:   reason,
		Model:          model,
		ProxyLatencyMs: float64(time.Since(start).Microseconds()) / 1000.0,
		Plan: p.dryRunPlan(plannerInput{
			provider:             provider,
			model:                model,
			routeMode:            "passthrough",
			contentClasses:       []string{"passthrough"},
			liveCorpusConfidence: p.plannerLiveCorpusConfidence(),
		}),
	})
}

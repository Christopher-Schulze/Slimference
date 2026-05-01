package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"time"

	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/caching"
	"github.com/slimference/slimference/internal/compression"
	"github.com/slimference/slimference/internal/contentarchive"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/readcache"
	"github.com/slimference/slimference/internal/resilience"
	"github.com/slimference/slimference/internal/security"
	"github.com/slimference/slimference/internal/summarization"
	"github.com/slimference/slimference/internal/tokens"
	"github.com/slimference/slimference/internal/toolprune"
	"github.com/slimference/slimference/internal/types"
)

var reconstructBodyFn = reconstructBody
var newRequestWithContextFn = http.NewRequestWithContext
var newRequestIDFn = newRequestID

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

type pipelineStashKey struct{}

// handleCompressibleRequest applies the full compression pipeline and forwards to upstream.
// This is the hot path: called for every POST /v1/messages and POST /v1/chat/completions.
func (p *Proxy) handleCompressibleRequest(w http.ResponseWriter, r *http.Request, provider types.Provider, body []byte) {
	start := time.Now()
	reqID := newRequestIDFn()
	sessionID := summarization.ExtractSessionID(provider, body, r.Header)

	// --- 1. Extract messages ---
	messages, rawBody, err := extractMessages(provider, body)
	if err != nil {
		slog.Error("extract messages", "error", err)
		p.proxyError(w, http.StatusBadRequest, fmt.Sprintf("parse request: %v", err))
		return
	}
	_ = rawBody
	if len(messages) == 0 {
		p.handlePassthrough(w, r, provider, body)
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
	messages = p.reinjectArchivedContent(messages)

	// T77: observe tool-use blocks for the re-read detector.
	reReadCount := 0
	if p.qualityReRead != nil {
		for _, msg := range messages {
			for _, block := range msg.Content {
				if block.Type == "tool_use" && block.ToolName != "" {
					if p.qualityReRead.Observe(reqID, block.ToolName) {
						reReadCount++
					}
				}
			}
		}
	}

	// Count original tokens before any compression.
	origTokens := tokens.CountMessages(messages)
	log.Debug("request started", "messages", len(messages), "orig_tokens", origTokens)

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

	// --- 3.5 Stage A cache pre-check (T20) ---
	// If an identical original request already produced a cached upstream
	// response, serve it without running Layer 1 or Layer 2 at all.
	var stageACacheKey [32]byte
	stageAEnabled := p.isLayerEnabled(3) && caching.IsRequestCacheSafe(body)
	if stageAEnabled {
		stageACacheKey = p.responseCache.ComputeRequestKeyWithHeaders(provider, body, r.Header)
		if cached, _, ok := p.responseCache.GetByOriginal(stageACacheKey); ok {
			p.serveStageACacheHit(w, cached, reqID, start, provider, model, len(messages), origTokens, log)
			return
		}
	}

	compressedMessages := messages
	var layer1Savings, layer2Savings int
	appliedLayers := make([]int, 0, 3)

	// --- 4. Layer 1: Deterministic compression ---
	var layer1Breakdown map[string]dbg.SubLayerBreakdown
	if p.isLayerEnabled(1) && p.isProviderEnabled(provider) && pipelineMode == PipelineFull {
		l1Start := time.Now()
		// T76: thread the request id as the session scope so any archive
		// entry produced by lossy sub-layers carries a correlatable id.
		// T100: when the coordinator is enabled and Layer 2 will fire
		// (origTokens >= MinTokensForLayer2), tell the L1 compressor to
		// skip heavy sub-layers since L2 will replace the prefix anyway.
		coordinatorActive := p.config.Compression.Tuning.CoordinatorEnabled &&
			p.isLayerEnabled(2) &&
			origTokens >= p.config.Compression.MinTokensForLayer2
		p.layer1.SetCoordinatorSubsume(coordinatorActive)
		result := p.layer1.CompressWithSession(reqID, messages)
		p.layer1.SetCoordinatorSubsume(false)
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
	}

	// --- 4.5 Mid-exchange summary (T99, default off) ---
	if p.config.Compression.Tuning.MidExchangeEnabled && pipelineMode == PipelineFull && p.layer2 != nil {
		// T99b: live summary path via Layer2.ApplyMidExchange; falls
		// back internally to the deterministic stub when the chain
		// has no provider or the call errors out.
		newMsgs, saved, applied := p.layer2.ApplyMidExchange(r.Context(), compressedMessages, p.config.Compression.Tuning.MidExchangeThresholdTokens)
		if applied {
			compressedMessages = newMsgs
			layer2Savings += saved
			appliedLayers = append(appliedLayers, 2)
			log.Debug("mid_exchange applied", "saved", saved)
		}
	}

	// --- 5. Layer 2: MiniMax summary ---
	if p.isLayerEnabled(2) && p.isProviderEnabled(provider) && pipelineMode == PipelineFull {
		l2Start := time.Now()
		if newMsgs, saved, applied := p.layer2.ApplyToMessagesSession(sessionID, compressedMessages); applied {
			compressedMessages = newMsgs
			layer2Savings = saved
			appliedLayers = append(appliedLayers, 2)
			log.Debug("layer2 applied", "saved", saved)
		}
		p.pipelineHist.L2.Record(time.Since(l2Start))
	}

	// --- 6. Prompt cache breakpoints (Anthropic only) ---
	if provider == types.Anthropic && p.isLayerEnabled(1) {
		stableBoundary := compression.CompressiblePrefixEnd(compressedMessages, p.config.Compression.SlidingWindow)
		if stableBoundary > 0 {
			compressedMessages = compression.OptimizeCacheBreakpoints(compressedMessages, stableBoundary)
		}
	}

	compressedTokens := tokens.CountMessages(compressedMessages)

	// Zero-downside guarantee (spec+.md §1): if compression expanded the output,
	// revert to original messages so the proxy never makes things worse.
	if origTokens > 0 && compressedTokens > origTokens {
		log.Debug("compression expanded output, reverting to original",
			"orig", origTokens, "comp", compressedTokens)
		compressedMessages = messages
		compressedTokens = origTokens
		appliedLayers = nil
		layer1Savings = 0
		layer2Savings = 0
	}

	// --- 7. Reconstruct request body ---
	newBody, err := reconstructBodyFn(provider, body, compressedMessages)
	if err != nil {
		log.Error("body reconstruction failed", "error", err)
		p.proxyError(w, http.StatusInternalServerError, "request reconstruction failed")
		return
	}

	totalSaved := origTokens - compressedTokens
	compressionRatio := 1.0
	if origTokens > 0 {
		compressionRatio = float64(compressedTokens) / float64(origTokens)
	}

	// --- 7.5 Tool-definition pruning (T103, Layer 4, default off) ---
	if p.config.Compression.Tuning.ToolPruneEnabled && p.toolPrune != nil && reqID != "" {
		// T103b: reattach previously-pruned tool definitions when the
		// current message text mentions a pruned tool by name. Runs
		// before the prune decision so a freshly reattached tool also
		// shows up in the active list and survives the next idle
		// check.
		if mentions := messageMentionsAnyPrunedTool(messages, p.toolPrune, reqID); len(mentions) > 0 {
			defs := p.toolPrune.LookupPrunedDefs(reqID, mentions)
			if reattached, n, err := toolprune.ReattachToolDefinitions(newBody, provider, defs); err == nil && n > 0 {
				newBody = reattached
				for range n {
					p.toolPrune.MarkReattached()
				}
				log.Debug("tool-prune reattached",
					"count", n,
					"tools", mentions,
				)
			}
		}
		if toolNames := toolprune.ExtractToolNames(newBody, provider); len(toolNames) > 0 {
			p.toolPrune.ObserveTurn(reqID, extractUsedToolNames(messages))
			decision := p.toolPrune.Decide(reqID, toolNames, 1)
			if len(decision.Pruned) > 0 {
				toPrune := make(map[string]bool, len(decision.Pruned))
				for _, n := range decision.Pruned {
					toPrune[n] = true
				}
				if prunedBody, removed, err := toolprune.PruneToolDefinitions(newBody, provider, toPrune); err == nil && len(removed) > 0 {
					savedEst := estimateTokensFromText(string(newBody)) - estimateTokensFromText(string(prunedBody))
					if savedEst > 0 {
						newBody = prunedBody
						p.toolPrune.MarkPruned(savedEst)
						// T103b: cache pruned defs so a future turn
						// that mentions the tool name can reattach
						// them. The map iteration order is fine
						// because each (name, def) pair is independent.
						for name, def := range removed {
							p.toolPrune.RememberPrunedDef(reqID, name, def)
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

	// --- 8. Response cache lookup (Layer 3) ---
	var cacheKey [32]byte
	requestCacheSafe := p.isLayerEnabled(3) && caching.IsRequestCacheSafe(newBody)
	if requestCacheSafe {
		cacheKey = p.responseCache.ComputeRequestKeyWithHeaders(provider, newBody, r.Header)
		if cached, ok := p.responseCache.Get(cacheKey); ok {
			log.Debug("cache hit")
			for k, vv := range cached.Headers {
				for _, v := range vv {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(cached.StatusCode)
			w.Write(cached.Response) //nolint:errcheck

			cacheLayers := append(append([]int{}, appliedLayers...), 3)
			cacheLatencyMs := float64(time.Since(start).Microseconds()) / 1000.0
			outputTokens := estimateTokensFromText(string(cached.Response))

			if p.debugRecorder != nil {
				summary := dbg.RequestSummary{
					RequestID:          reqID,
					Timestamp:          start,
					Provider:           provider.String(),
					Model:              model,
					TotalMessages:      len(messages),
					MessagesInWindow:   p.config.Compression.SlidingWindow,
					MessagesCompressed: max(0, len(messages)-p.config.Compression.SlidingWindow),
					LayersApplied:      cacheLayers,
					Tokens: dbg.TokenCounts{
						Original:    origTokens,
						AfterLayer1: origTokens - layer1Savings,
						AfterLayer2: origTokens - layer1Savings - layer2Savings,
						Final:       compressedTokens,
						Saved:       totalSaved,
						Ratio:       compressionRatio,
					},
					Layer1Breakdown:   layer1Breakdown,
					CacheHit:          true,
					CacheReadTokens:   0,
					CacheCreateTokens: 0,
					ProxyLatencyMs:    cacheLatencyMs,
					ReReadCount:       reReadCount,
					NetSavedTokens:    totalSaved,
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
				InputTokensComp:  compressedTokens,
				OutputTokens:     outputTokens,
				CompressionRatio: compressionRatio,
				Layers:           cacheLayers,
				LatencyMs:        cacheLatencyMs,
				CacheHit:         true,
				TokensSaved:      totalSaved,
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

	// --- 8.5 Server-state lever (T78, default off) ---
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

	// --- 9. Forward to upstream ---
	upstreamResp, err := p.doUpstreamRequest(r, provider, upstreamBody)
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

	// --- 9. Stream / passthrough response ---
	var outputTokens int
	var responseBody []byte
	var upstreamCacheUsage cacheUsage

	if isStreamingRequest(body) {
		outputTokens, upstreamCacheUsage = streamingRelayWithUsage(r.Context(), w, upstreamResp, provider.String())
	} else {
		responseBody = passthrough(w, upstreamResp)
		outputTokens = estimateTokensFromText(string(responseBody))
		if provider == types.Anthropic {
			upstreamCacheUsage = extractAnthropicCacheUsageFromBody(responseBody)
		}
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
		tokens.ObserveUpstreamUsage(provider, upstreamCacheUsage.InputTokens, compressedTokens)
	}

	proxyLatencyMs := float64(time.Since(latencyStart).Microseconds()) / 1000.0
	p.pipelineHist.Total.Record(time.Since(start))

	// --- 10. Cache successful response (Layer 3) ---
	if requestCacheSafe && responseBody != nil && upstreamResp.StatusCode == http.StatusOK {
		dependencyPaths := caching.ExtractDependencyPaths(body)
		canCacheResponse := true
		if len(dependencyPaths) > 0 {
			if p.fileWatcher == nil {
				canCacheResponse = false
				log.Warn("skipping layer3 cache store because dependency invalidation is unavailable",
					"dependency_paths", len(dependencyPaths))
			} else {
				for _, path := range dependencyPaths {
					if err := p.fileWatcher.Watch(path); err != nil {
						canCacheResponse = false
						log.Warn("skipping layer3 cache store because dependency watch failed",
							"path", path,
							"error", err)
						break
					}
					if !p.fileWatcher.IsWatching(path) {
						canCacheResponse = false
						log.Warn("skipping layer3 cache store because dependency watch was not armed",
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
			for k, vv := range upstreamResp.Header {
				entry.Headers[k] = vv
			}
			p.responseCache.Set(cacheKey, entry)
			// Register the Stage A pointer (T20) so the next identical
			// original request can skip the compression pipeline entirely.
			if stageAEnabled {
				p.responseCache.RegisterOriginalPointer(stageACacheKey, cacheKey)
			}
		}
	}

	// --- 11. Trigger async Layer 2 compression if needed ---
	if p.isLayerEnabled(2) && p.layer2.ShouldTriggerCompressionSession(sessionID, messages) {
		select {
		case p.compressQueue <- types.CompressJob{Messages: messages, Timestamp: time.Now(), SessionID: sessionID}:
		default:
			// Queue full, compression already in progress - skip.
		}
	}

	// --- Debug decision recording ---
	if p.debugRecorder != nil {
		summary := dbg.RequestSummary{
			RequestID:          reqID,
			Timestamp:          start,
			Provider:           provider.String(),
			Model:              model,
			TotalMessages:      len(messages),
			MessagesInWindow:   p.config.Compression.SlidingWindow,
			MessagesCompressed: max(0, len(messages)-p.config.Compression.SlidingWindow),
			LayersApplied:      appliedLayers,
			Tokens: dbg.TokenCounts{
				Original:    origTokens,
				AfterLayer1: origTokens - layer1Savings,
				AfterLayer2: origTokens - layer1Savings - layer2Savings,
				Final:       compressedTokens,
				Saved:       totalSaved,
				Ratio:       compressionRatio,
			},
			Layer1Breakdown:   layer1Breakdown,
			CacheHit:          false,
			CacheReadTokens:   upstreamCacheUsage.ReadTokens,
			CacheCreateTokens: upstreamCacheUsage.CreateTokens,
			ProxyLatencyMs:    proxyLatencyMs,
			ReReadCount:       reReadCount,
			NetSavedTokens:    totalSaved,
		}
		p.debugRecorder.Record(summary)
		p.observeQuality(summary)
	}

	p.trySendAnalytics(types.AnalyticsEvent{
		Type:              types.EventRequestProcessed,
		Timestamp:         time.Now(),
		Provider:          provider,
		Model:             model,
		InputTokensOrig:   origTokens,
		InputTokensComp:   compressedTokens,
		OutputTokens:      outputTokens,
		CompressionRatio:  compressionRatio,
		Layers:            appliedLayers,
		LatencyMs:         proxyLatencyMs,
		TokensSaved:       totalSaved,
		CacheReadTokens:   upstreamCacheUsage.ReadTokens,
		CacheCreateTokens: upstreamCacheUsage.CreateTokens,
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

// serveStageACacheHit writes a cached response for a Stage A hit (pre-compression).
// The entire compression pipeline is skipped; only Layer 3 is reported as applied.
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
			Provider:           provider.String(),
			Model:              model,
			TotalMessages:      totalMessages,
			MessagesInWindow:   p.config.Compression.SlidingWindow,
			MessagesCompressed: 0,
			LayersApplied:      []int{3},
			Tokens: dbg.TokenCounts{
				Original:    origTokens,
				AfterLayer1: origTokens,
				AfterLayer2: origTokens,
				Final:       origTokens,
				Saved:       0,
				Ratio:       1.0,
			},
			CacheHit:          true,
			CacheReadTokens:   0,
			CacheCreateTokens: 0,
			ProxyLatencyMs:    latencyMs,
			ReReadCount:       0,
			NetSavedTokens:    0,
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
		Layers:           []int{3},
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
		"layers", []int{3},
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
	addBD("repeated_collapse", r.RepeatedCollapseSaved)
	addBD("graph_pruning", r.GraphPruningSaved)
	return bd
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
		req, err := newRequestWithContextFn(r.Context(), r.Method, upstreamURL, bytes.NewReader(b))
		if err != nil {
			return nil, fmt.Errorf("build upstream request: %w", err)
		}
		for k, vv := range fwdHeaders {
			for _, v := range vv {
				req.Header.Add(k, v)
			}
		}
		req.ContentLength = int64(len(b))
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
		d := time.Duration(secs) * time.Second
		if d > 30*time.Second {
			d = 30 * time.Second
		}
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
// window, applies any already-cached Layer 2 summary read-only, and enqueues
// a fresh async Layer 2 job so the next request benefits from an updated
// summary. Spec+.md §17.4: the overflow recover path must be bounded by local
// CPU - no synchronous MiniMax call is permitted here, because a hanging
// provider would hang the user-facing recover.
func (p *Proxy) buildAggressiveCompressedBodyContext(ctx context.Context, stash pipelineStash) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg := p.config.Compression
	cfg.SlidingWindow = aggressiveSlidingWindow(cfg.Tuning.OverflowSlidingWindow)
	cfg.Summary.TargetRatio = aggressiveTargetRatio(cfg.Tuning.OverflowTargetRatio)
	if cfg.Summary.TargetRatio < cfg.Summary.MinRatio {
		cfg.Summary.TargetRatio = cfg.Summary.MinRatio
	}
	l1 := compression.NewDeterministicCompressor(&cfg)
	msgs := l1.Compress(stash.messages).Messages

	// Read-only Layer 2 pass: consume any existing cached summary. Never call
	// MiniMax synchronously - that is exactly what this path must not do.
	if p.layer2 != nil {
		if applied, _, ok := p.layer2.ApplyToMessagesSession(stash.sessionID, msgs); ok {
			msgs = applied
		}
	}

	// Enqueue a non-blocking async Layer 2 job so the next request benefits
	// from an up-to-date summary. Drop silently if the queue is full - we
	// already responded.
	if p.isLayerEnabled(2) && p.layer2 != nil && p.layer2.ShouldTriggerCompressionSession(stash.sessionID, stash.messages) {
		select {
		case p.compressQueue <- types.CompressJob{Messages: stash.messages, Timestamp: time.Now(), SessionID: stash.sessionID}:
		default:
		}
	}

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

// aggressiveTargetRatio returns the configured overflow target ratio,
// defaulting to 0.10 when the tuning block is empty.
func aggressiveTargetRatio(v float64) float64 {
	if v <= 0 {
		return 0.10
	}
	return v
}

func (p *Proxy) compressionContext() context.Context {
	if p.workerCtx != nil {
		return p.workerCtx
	}
	return context.Background()
}

// proxyError writes an error response to the client.
func (p *Proxy) proxyError(w http.ResponseWriter, statusCode int, msg string) {
	http.Error(w, msg, statusCode)
}

// handlePassthrough proxies a request to upstream without any modification.
func (p *Proxy) handlePassthrough(w http.ResponseWriter, r *http.Request, provider types.Provider, body []byte) {
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
		upstreamReq.Header.Set("Content-Type", "application/json")
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

	if isStreamingRequest(body) {
		streamingRelay(r.Context(), w, resp, provider.String())
	} else {
		passthrough(w, resp)
	}
}

// healthHandler responds to GET /health with full proxy status JSON.
func (p *Proxy) healthHandler(w http.ResponseWriter, _ *http.Request) {
	status := struct {
		Status            string          `json:"status"`
		Service           string          `json:"service"`
		Version           string          `json:"version"`
		Layers            map[string]bool `json:"layers"`
		Providers         map[string]bool `json:"providers"`
		QueueDepth        map[string]int  `json:"queue_depth"`
		CacheEntries      int             `json:"cache_entries"`
		MiniMaxConfigured bool            `json:"minimax_configured"`
	}{
		Status:  "ok",
		Service: "slimference",
		Version: Version,
		Layers: map[string]bool{
			"1": p.isLayerEnabled(1),
			"2": p.isLayerEnabled(2),
			"3": p.isLayerEnabled(3),
		},
		Providers: map[string]bool{
			"anthropic": p.isProviderEnabled(types.Anthropic),
			"openai":    p.isProviderEnabled(types.OpenAI),
		},
		QueueDepth: map[string]int{
			"compress":  len(p.compressQueue),
			"analytics": len(p.analyticsQueue),
		},
		CacheEntries:      p.responseCache.Len(),
		MiniMaxConfigured: p.config.Compression.MiniMax.APIKey() != "",
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(status)
}

// compressionWorker processes CompressJob items from the queue asynchronously.
func (p *Proxy) compressionWorker() {
	defer p.wg.Done()
	for {
		select {
		case <-p.shutdownCh:
			return
		default:
		}
		select {
		case <-p.shutdownCh:
			return
		case job := <-p.compressQueue:
			p.runCompressionJob(job)
		}
	}
}

// runCompressionJob executes a full MiniMax summarization cycle.
func (p *Proxy) runCompressionJob(job types.CompressJob) {
	p.layer2.SetCompressingSession(job.SessionID, true)
	defer p.layer2.SetCompressingSession(job.SessionID, false)

	slog.Debug("compression job started", "messages", len(job.Messages))
	p.layer2.RunCompressionJobSession(p.compressionContext(), job.SessionID, job.Messages)

	p.trySendAnalytics(types.AnalyticsEvent{
		Type:      types.EventCompressionComplete,
		Timestamp: time.Now(),
	})
}

// analyticsWorker reads from analyticsQueue and records events.
func (p *Proxy) analyticsWorker() {
	defer p.wg.Done()
	for {
		select {
		case event := <-p.analyticsQueue:
			p.processAnalyticsEvent(event)
		case <-p.shutdownCh:
			p.drainAnalyticsQueue()
			return
		}
	}
}

func (p *Proxy) drainAnalyticsQueue() {
	for {
		select {
		case event := <-p.analyticsQueue:
			p.processAnalyticsEvent(event)
		default:
			return
		}
	}
}

func (p *Proxy) processAnalyticsEvent(event types.AnalyticsEvent) {
	p.analytics.Record(event)
	if p.sessionLogger != nil {
		p.sessionLogger.Log(
			"INFO", "analytics",
			fmt.Sprintf("event: %v provider=%v saved=%d", event.Type, event.Provider, event.TokensSaved),
		)
	}
	if event.Type == types.EventRequestProcessed || event.Type == types.EventOverflowRetry {
		p.maybeCaptureCheckpoint(event)
	}
	// Fan out to TUI via program.Send if available.
	if event.Type == types.EventRequestProcessed {
		p.tuiSendMu.RLock()
		fn := p.tuiSendFn
		p.tuiSendMu.RUnlock()
		if fn != nil {
			fn(types.RequestMetrics{
				Timestamp:        event.Timestamp,
				Provider:         event.Provider,
				Model:            event.Model,
				InputTokensOrig:  event.InputTokensOrig,
				InputTokensComp:  event.InputTokensComp,
				OutputTokens:     event.OutputTokens,
				CompressionRatio: event.CompressionRatio,
				Layers:           event.Layers,
				LatencyMs:        event.LatencyMs,
				CacheHit:         event.CacheHit,
			})
		}
	}
}

// cleanupExpiredCache removes expired entries from the response cache.
func (p *Proxy) cleanupExpiredCache() {
	p.responseCache.Cleanup()
}

// cacheJanitorInterval is the period between cache cleanup runs; overridden in tests.
var cacheJanitorInterval = 60 * time.Second

// analyticsFlushInterval is the period between analytics snapshots; overridden in tests.
var analyticsFlushInterval = 5 * time.Minute

// cacheJanitor periodically removes expired cache entries.
// interval is captured at goroutine start so tests can modify the package var without a data race.
func (p *Proxy) cacheJanitor(interval time.Duration) {
	defer p.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.cleanupExpiredCache()
		case <-p.shutdownCh:
			return
		}
	}
}

// flushAnalyticsSnapshot writes an analytics snapshot to disk if a persister is configured.
func (p *Proxy) flushAnalyticsSnapshot() {
	if p.persister != nil {
		snap := p.analytics.Snapshot()
		if err := p.persister.WriteSnapshot(snap); err != nil {
			slog.Warn("analytics flush failed", "error", err)
		}
	}
}

// analyticsPeriodicFlush writes analytics snapshots to disk every 5 minutes.
// interval is captured at goroutine start so tests can modify the package var without a data race.
func (p *Proxy) analyticsPeriodicFlush(interval time.Duration) {
	defer p.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.flushAnalyticsSnapshot()
		case <-p.shutdownCh:
			return
		}
	}
}

// ErrShutdownTimeout is returned by Shutdown when ctx was cancelled before
// every worker goroutine finished. When this happens a goroutine pprof dump
// is written to ~/.slimference/shutdown-hang-<ts>.pprof (best-effort) and
// callers may translate the error to a dedicated process exit code.
var ErrShutdownTimeout = errors.New("shutdown timeout exceeded")

// shutdownDumpWriterFn is overridden in tests to capture the pprof dump
// without touching the user filesystem.
var shutdownDumpWriterFn = defaultShutdownDumpWriter

// applyDrainTimeout wraps ctx with a deadline drawn from
// `[proxy] drain_timeout_seconds` when the caller's context has no
// deadline. Returns the (possibly wrapped) context and a cancel func.
// T85: turns the operator-config drain knob into a hard ceiling so a
// hung request cannot block exit indefinitely even when the caller
// passed context.Background().
func (p *Proxy) applyDrainTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if p.config == nil || p.config.Proxy.DrainTimeoutSeconds <= 0 {
		return ctx, func() {}
	}
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, time.Duration(p.config.Proxy.DrainTimeoutSeconds)*time.Second)
}

// Shutdown performs a graceful shutdown of the proxy. Safe to call multiple
// times - only the first call does work; subsequent calls return nil. On
// timeout Shutdown returns ErrShutdownTimeout so process-level callers can
// translate the outcome into a distinct exit code (T60).
// Nil ctx is tolerated and replaced with context.Background so operator-
// scripts that call Shutdown(nil) never crash. T85 caps no-deadline calls
// at `[proxy] drain_timeout_seconds` when set so a stuck connection
// cannot block exit indefinitely.
func (p *Proxy) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	wrapped, cancel := p.applyDrainTimeout(ctx)
	defer cancel()
	var result error
	p.shutdownOnce.Do(func() {
		result = p.doShutdown(wrapped)
	})
	return result
}

func (p *Proxy) doShutdown(ctx context.Context) error {
	slog.Info("proxy shutdown initiated")
	if p.workerCancel != nil {
		p.workerCancel()
	}

	// server may be nil when Shutdown is called on a freshly New'd Proxy
	// that never Start()ed. Tolerate that so unit tests and the integrate
	// adapter-smoke path do not panic.
	if p.server != nil {
		if err := p.server.Shutdown(ctx); err != nil {
			slog.Warn("server shutdown error", "error", err)
		}
	}

	if p.shutdownCh != nil {
		close(p.shutdownCh)
	}

	workersDone := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(workersDone)
	}()

	var result error
	select {
	case <-workersDone:
		slog.Info("all workers stopped")
	case <-ctx.Done():
		dumpPath, dumpErr := shutdownDumpWriterFn()
		slog.Warn("shutdown timeout, some workers may still be running",
			"goroutines", runtime.NumGoroutine(),
			"dump_path", dumpPath,
			"dump_err", dumpErr,
		)
		result = ErrShutdownTimeout
	}

	// Final analytics flush.
	if p.persister != nil {
		snap := p.analytics.Snapshot()
		if err := p.persister.WriteSnapshot(snap); err != nil {
			slog.Warn("final analytics flush failed", "error", err)
		}
		p.persister.Close()
	}

	if p.fileWatcher != nil {
		p.fileWatcher.Close()
	}
	return result
}

// defaultShutdownDumpWriter writes a goroutine pprof dump to a stable path
// under the user's state dir. Best-effort: any filesystem error is reported
// back and logged by the caller, never propagated as a shutdown failure.
func defaultShutdownDumpWriter() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".slimference")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("shutdown-hang-%s.pprof",
		time.Now().UTC().Format("20060102T150405"))
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	_ = pprof.Lookup("goroutine").WriteTo(f, 1)
	return path, nil
}

// GetAnalytics returns a snapshot of the current analytics state.
func (p *Proxy) GetAnalytics() analytics.AnalyticsSnapshot {
	return p.analytics.Snapshot()
}

// GetRecentRequests returns the last n request metrics from the ring buffer.
func (p *Proxy) GetRecentRequests(n int) []types.RequestMetrics {
	return p.analytics.RecentRequests(n)
}

// FlushCaches invalidates all in-memory caches.
func (p *Proxy) FlushCaches() {
	p.responseCache.Flush()
	p.layer1.Reset()
	p.layer2.InvalidateAllSessions()
	if home, err := os.UserHomeDir(); err == nil {
		if err := readcache.Clear(readcache.DefaultDir(home)); err != nil {
			slog.Warn("read cache flush failed", "error", err)
		}
	}
	slog.Info("all caches flushed")
}

// messageMentionsAnyPrunedTool flushes the per-session pruned-tools
// cache for any tool the current request's message text mentions. Used
// by T103b to decide which previously-pruned tool definitions to
// reattach. Returns the list of mentioned tool names (deduplicated).
func messageMentionsAnyPrunedTool(messages []types.Message, tracker *toolprune.UsageTracker, sessionID string) []string {
	if tracker == nil || sessionID == "" {
		return nil
	}
	candidates := tracker.PrunedToolNames(sessionID)
	if len(candidates) == 0 {
		return nil
	}
	var text string
	for _, msg := range messages {
		for _, b := range msg.Content {
			if b.Text != "" {
				text += b.Text + "\n"
			}
		}
	}
	return toolprune.MentionedTools(text, candidates)
}

// extractUsedToolNames returns the distinct tool names from tool_use
// blocks in the message list. Used by T103 to feed the usage tracker.
func extractUsedToolNames(messages []types.Message) []string {
	seen := make(map[string]bool)
	var names []string
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type == "tool_use" && block.ToolName != "" {
				if !seen[block.ToolName] {
					seen[block.ToolName] = true
					names = append(names, block.ToolName)
				}
			}
		}
	}
	return names
}

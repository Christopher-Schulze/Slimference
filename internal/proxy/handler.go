package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/caching"
	"github.com/slimference/slimference/internal/compression"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/resilience"
	"github.com/slimference/slimference/internal/security"
	"github.com/slimference/slimference/internal/summarization"
	"github.com/slimference/slimference/internal/tokens"
	"github.com/slimference/slimference/internal/types"
)

var reconstructBodyFn = reconstructBody
var newRequestWithContextFn = http.NewRequestWithContext

// newRequestID generates a short random hex request ID for debug correlation.
func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// pipelineStash carries inputs for aggressive recompression on context overflow (spec §17.4).
type pipelineStash struct {
	messages []types.Message
	origBody []byte
	provider types.Provider
}

type pipelineStashKey struct{}

// handleCompressibleRequest applies the full compression pipeline and forwards to upstream.
// This is the hot path: called for every POST /v1/messages and POST /v1/chat/completions.
func (p *Proxy) handleCompressibleRequest(w http.ResponseWriter, r *http.Request, provider types.Provider, body []byte) {
	start := time.Now()
	reqID := newRequestID()

	// --- 1. Extract messages ---
	messages, rawBody, err := extractMessages(provider, body)
	if err != nil {
		slog.Error("extract messages", "error", err)
		p.proxyError(w, http.StatusBadRequest, fmt.Sprintf("parse request: %v", err))
		return
	}
	_ = rawBody

	model := extractModel(body)
	// Request-scoped logger: all debug/warn/info calls inside this function carry req_id.
	log := slog.With("req_id", reqID, "provider", provider, "model", model)

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
			select {
			case p.analyticsQueue <- types.AnalyticsEvent{
				Type:         types.EventSecretDetected,
				Timestamp:    time.Now(),
				Provider:     provider,
				SecretsFound: len(detections),
			}:
			default:
			}
		}
	}

	r = r.WithContext(context.WithValue(r.Context(), pipelineStashKey{}, pipelineStash{
		messages: messages,
		origBody: body,
		provider: provider,
	}))

	compressedMessages := messages
	var layer1Savings, layer2Savings int
	appliedLayers := make([]int, 0, 3)

	// --- 4. Layer 1: Deterministic compression ---
	var layer1Breakdown map[string]dbg.SubLayerBreakdown
	if p.isLayerEnabled(1) && p.isProviderEnabled(provider) {
		result := p.layer1.Compress(messages)
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

	// --- 5. Layer 2: MiniMax summary ---
	if p.isLayerEnabled(2) && p.isProviderEnabled(provider) {
		if newMsgs, saved, applied := p.layer2.ApplyToMessages(compressedMessages); applied {
			compressedMessages = newMsgs
			layer2Savings = saved
			appliedLayers = append(appliedLayers, 2)
			log.Debug("layer2 applied", "saved", saved)
		}
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
					Layer1Breakdown: layer1Breakdown,
					CacheHit:        true,
					ProxyLatencyMs:  cacheLatencyMs,
				}
				p.debugRecorder.Record(summary)
			}

			select {
			case p.analyticsQueue <- types.AnalyticsEvent{
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
			}:
			default:
			}

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

	// --- 9. Forward to upstream ---
	upstreamResp, err := p.doUpstreamRequest(r, provider, newBody)
	if err != nil {
		p.healthMon.record(provider, false)
		log.Error("upstream request failed", "error", err)
		p.proxyError(w, http.StatusBadGateway, fmt.Sprintf("upstream error: %v", err))
		select {
		case p.analyticsQueue <- types.AnalyticsEvent{
			Type:      types.EventErrorOccurred,
			Timestamp: time.Now(),
			Provider:  provider,
			Error:     err.Error(),
		}:
		default:
		}
		return
	}
	p.healthMon.record(provider, upstreamResp.StatusCode < 500)

	// --- 9. Stream / passthrough response ---
	var outputTokens int
	var responseBody []byte

	if isStreamingRequest(body) {
		outputTokens = streamingRelay(r.Context(), w, upstreamResp, provider.String())
	} else {
		responseBody = passthrough(w, upstreamResp)
		outputTokens = estimateTokensFromText(string(responseBody))
	}

	proxyLatencyMs := float64(time.Since(latencyStart).Microseconds()) / 1000.0

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
		}
	}

	// --- 11. Trigger async Layer 2 compression if needed ---
	if p.isLayerEnabled(2) && p.layer2.ShouldTriggerCompression(messages) {
		select {
		case p.compressQueue <- types.CompressJob{Messages: messages, Timestamp: time.Now()}:
		default:
			// Queue full, compression already in progress - skip.
		}
	}

	// --- 12. Analytics ---
	_ = layer1Savings
	_ = layer2Savings

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
			Layer1Breakdown: layer1Breakdown,
			CacheHit:        false,
			ProxyLatencyMs:  proxyLatencyMs,
		}
		p.debugRecorder.Record(summary)
	}

	select {
	case p.analyticsQueue <- types.AnalyticsEvent{
		Type:             types.EventRequestProcessed,
		Timestamp:        time.Now(),
		Provider:         provider,
		Model:            model,
		InputTokensOrig:  origTokens,
		InputTokensComp:  compressedTokens,
		OutputTokens:     outputTokens,
		CompressionRatio: compressionRatio,
		Layers:           appliedLayers,
		LatencyMs:        proxyLatencyMs,
		TokensSaved:      totalSaved,
	}:
	default:
	}

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
		select {
		case p.analyticsQueue <- types.AnalyticsEvent{
			Type:      types.EventRateLimitRetry,
			Timestamp: time.Now(),
			Provider:  provider,
		}:
		default:
		}
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
			select {
			case p.analyticsQueue <- types.AnalyticsEvent{
				Type:      types.EventOverflowRetry,
				Timestamp: time.Now(),
				Provider:  provider,
			}:
			default:
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
				}
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

// buildAggressiveCompressedBody re-runs Layer 1-2 with a minimal sliding window and stronger summarization.
func (p *Proxy) buildAggressiveCompressedBody(stash pipelineStash) ([]byte, error) {
	return p.buildAggressiveCompressedBodyContext(context.Background(), stash)
}

// buildAggressiveCompressedBodyContext re-runs Layer 1-2 with a minimal sliding window and stronger summarization.
func (p *Proxy) buildAggressiveCompressedBodyContext(ctx context.Context, stash pipelineStash) ([]byte, error) {
	cfg := p.config.Compression
	cfg.SlidingWindow = 2
	cfg.Summary.TargetRatio = 0.10
	if cfg.Summary.TargetRatio < cfg.Summary.MinRatio {
		cfg.Summary.TargetRatio = cfg.Summary.MinRatio
	}
	l1 := compression.NewDeterministicCompressor(&cfg)
	l2 := summarization.NewLayer2(&cfg)
	msgs := l1.Compress(stash.messages).Messages
	l2.RunCompressionJobContext(ctx, msgs)
	msgs2, _, ok := l2.ApplyToMessages(msgs)
	if !ok {
		msgs2 = msgs
	}
	return reconstructBodyFn(stash.provider, stash.origBody, msgs2)
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
		case job := <-p.compressQueue:
			p.runCompressionJob(job)
		case <-p.shutdownCh:
			return
		}
	}
}

// runCompressionJob executes a full MiniMax summarization cycle.
func (p *Proxy) runCompressionJob(job types.CompressJob) {
	p.layer2.GetCache().Compressing.Store(true)
	defer p.layer2.GetCache().Compressing.Store(false)

	slog.Debug("compression job started", "messages", len(job.Messages))
	p.layer2.RunCompressionJob(job.Messages)

	select {
	case p.analyticsQueue <- types.AnalyticsEvent{
		Type:      types.EventCompressionComplete,
		Timestamp: time.Now(),
	}:
	default:
	}
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

// Shutdown performs a graceful shutdown of the proxy. Safe to call multiple times -
// only the first call does work; subsequent calls return immediately.
func (p *Proxy) Shutdown(ctx context.Context) error {
	p.shutdownOnce.Do(func() {
		slog.Info("proxy shutdown initiated")

		if err := p.server.Shutdown(ctx); err != nil {
			slog.Warn("server shutdown error", "error", err)
		}

		close(p.shutdownCh)

		workersDone := make(chan struct{})
		go func() {
			p.wg.Wait()
			close(workersDone)
		}()

		select {
		case <-workersDone:
			slog.Info("all workers stopped")
		case <-ctx.Done():
			slog.Warn("shutdown timeout, some workers may still be running")
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
	})

	return nil
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
	p.layer2.GetCache().Invalidate()
	slog.Info("all caches flushed")
}

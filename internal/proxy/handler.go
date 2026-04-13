package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/tokenproxy/tokenproxy/internal/analytics"
	"github.com/tokenproxy/tokenproxy/internal/caching"
	"github.com/tokenproxy/tokenproxy/internal/compression"
	dbg "github.com/tokenproxy/tokenproxy/internal/debug"
	"github.com/tokenproxy/tokenproxy/internal/security"
	"github.com/tokenproxy/tokenproxy/internal/summarization"
	"github.com/tokenproxy/tokenproxy/internal/tokens"
	"github.com/tokenproxy/tokenproxy/internal/types"
)

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

	// --- 2. Response cache lookup (Layer 3) ---
	var cacheKey [32]byte
	if p.isLayerEnabled(3) {
		cacheKey = p.responseCache.ComputeKey(messages, model)
		if cached, ok := p.responseCache.Get(cacheKey); ok {
			slog.Debug("cache hit", "provider", provider, "model", model)
			for k, vv := range cached.Headers {
				for _, v := range vv {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(cached.StatusCode)
			w.Write(cached.Response) //nolint:errcheck
			p.analyticsQueue <- types.AnalyticsEvent{
				Type:        types.EventCacheHit,
				Timestamp:   time.Now(),
				Provider:    provider,
				Model:       model,
				TokensSaved: cached.TokensSaved,
				CacheHit:    true,
				Layers:      []int{3},
			}
			return
		}
	}

	// Count original tokens before any compression.
	origTokens := tokens.CountMessages(messages)

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
			slog.Warn("secrets detected/redacted", "count", len(detections))
			p.analyticsQueue <- types.AnalyticsEvent{
				Type:         types.EventSecretDetected,
				Timestamp:    time.Now(),
				Provider:     provider,
				SecretsFound: len(detections),
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
			slog.Debug("layer1 applied",
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
			slog.Debug("layer2 applied", "saved", saved)
		}
	}

	// --- 6. Prompt cache breakpoints (Anthropic only) ---
	if provider == types.Anthropic && p.isLayerEnabled(1) {
		stableBoundary := compression.CompressiblePrefixEnd(compressedMessages, p.config.Compression.SlidingWindow)
		if stableBoundary > 0 {
			compressedMessages = compression.OptimizeCacheBreakpoints(compressedMessages, stableBoundary)
		}
	}

	// --- 7. Reconstruct request body ---
	newBody, _ := reconstructBody(provider, body, compressedMessages)

	compressedTokens := tokens.CountMessages(compressedMessages)
	latencyStart := time.Now()

	// --- 8. Forward to upstream ---
	upstreamResp, err := p.doUpstreamRequest(r, provider, newBody)
	if err != nil {
		slog.Error("upstream request failed", "provider", provider, "error", err)
		p.proxyError(w, http.StatusBadGateway, fmt.Sprintf("upstream error: %v", err))
		p.analyticsQueue <- types.AnalyticsEvent{
			Type:      types.EventErrorOccurred,
			Timestamp: time.Now(),
			Provider:  provider,
			Error:     err.Error(),
		}
		return
	}

	// --- 9. Stream / passthrough response ---
	var outputTokens int
	var responseBody []byte

	if isStreamingRequest(body) {
		outputTokens = streamingRelay(w, upstreamResp, provider.String())
	} else {
		responseBody = passthrough(w, upstreamResp)
		outputTokens = estimateTokensFromText(string(responseBody))
	}

	proxyLatencyMs := float64(time.Since(latencyStart).Microseconds()) / 1000.0

	// --- 10. Cache successful response (Layer 3) ---
	if p.isLayerEnabled(3) && responseBody != nil && upstreamResp.StatusCode == http.StatusOK {
		entry := &caching.CacheEntry{
			Response:    responseBody,
			Headers:     make(map[string][]string),
			StatusCode:  upstreamResp.StatusCode,
			CreatedAt:   time.Now(),
			TokensSaved: origTokens - compressedTokens,
		}
		// Copy headers for cache.
		for k, vv := range upstreamResp.Header {
			entry.Headers[k] = vv
		}
		p.responseCache.Set(cacheKey, entry)
	}

	// --- 11. Trigger async Layer 2 compression if needed ---
	if p.isLayerEnabled(2) && p.layer2.ShouldTriggerCompression(messages) {
		select {
		case p.compressQueue <- types.CompressJob{Messages: messages, Timestamp: time.Now()}:
		default:
			// Queue full, compression already in progress - skip.
		}
	}

	totalSaved := origTokens - compressedTokens

	// --- 12. Analytics ---
	var compressionRatio float64
	if origTokens > 0 {
		compressionRatio = float64(compressedTokens) / float64(origTokens)
	} else {
		compressionRatio = 1.0
	}

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
			MessagesCompressed: len(messages) - p.config.Compression.SlidingWindow,
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

	p.analyticsQueue <- types.AnalyticsEvent{
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
	}

	slog.Info("request_processed",
		"provider", provider,
		"model", model,
		"input_orig", origTokens,
		"input_comp", compressedTokens,
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

// doUpstreamRequest builds and executes the upstream HTTP request with retry logic.
func (p *Proxy) doUpstreamRequest(r *http.Request, provider types.Provider, body []byte) (*http.Response, error) {
	upstreamURL := p.upstreamURL(provider, r.URL.Path, r.URL.RawQuery)

	upstreamReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}

	// Copy all request headers except those that must be rewritten.
	for k, vv := range r.Header {
		switch k {
		case "Host", "Content-Length", "Connection", "Transfer-Encoding":
			continue
		}
		for _, v := range vv {
			upstreamReq.Header.Add(k, v)
		}
	}
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.ContentLength = int64(len(body))

	client := p.httpClients[provider]
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(upstreamReq)
	if err != nil {
		return nil, err
	}

	// Handle context overflow (400): spec §17.4 — retry with aggressive recompression, then raw body.
	if resp.StatusCode == http.StatusBadRequest {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		resp.Body.Close()
		if isContextOverflow(bodyBytes) {
			if stash, ok := r.Context().Value(pipelineStashKey{}).(pipelineStash); ok {
				if aggBody, err := p.buildAggressiveCompressedBody(stash); err == nil && len(aggBody) > 0 {
					slog.Warn("context overflow: retrying with aggressive compression (sliding_window=2, summary target 10%)")
					aggReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, bytes.NewReader(aggBody))
					if err == nil {
						for k, vv := range upstreamReq.Header {
							for _, v := range vv {
								aggReq.Header.Add(k, v)
							}
						}
						aggReq.Header.Set("Content-Type", "application/json")
						aggReq.ContentLength = int64(len(aggBody))
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
			origReq, _ := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, bytes.NewReader(p.getOriginalBody(r)))
			for k, vv := range upstreamReq.Header {
				for _, v := range vv {
					origReq.Header.Add(k, v)
				}
			}
			origReq.Header.Set("Content-Type", "application/json")
			origReq.ContentLength = int64(len(p.getOriginalBody(r)))
			return client.Do(origReq)
		}
		// Reconstruct a ReadCloser for non-overflow 400s.
		resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	return resp, nil
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

// buildAggressiveCompressedBody re-runs Layer 1–2 with a minimal sliding window and stronger summarization.
func (p *Proxy) buildAggressiveCompressedBody(stash pipelineStash) ([]byte, error) {
	cfg := p.config.Compression
	cfg.SlidingWindow = 2
	cfg.Summary.TargetRatio = 0.10
	if cfg.Summary.TargetRatio < cfg.Summary.MinRatio {
		cfg.Summary.TargetRatio = cfg.Summary.MinRatio
	}
	l1 := compression.NewDeterministicCompressor(&cfg)
	l2 := summarization.NewLayer2(&cfg)
	msgs := l1.Compress(stash.messages).Messages
	l2.RunCompressionJob(msgs)
	msgs2, _, ok := l2.ApplyToMessages(msgs)
	if !ok {
		msgs2 = msgs
	}
	return reconstructBody(stash.provider, stash.origBody, msgs2)
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
		streamingRelay(w, resp, provider.String())
	} else {
		passthrough(w, resp)
	}
}

// healthHandler responds to GET /health with a 200 JSON body.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","service":"tokenproxy"}`)) //nolint:errcheck
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

	p.analyticsQueue <- types.AnalyticsEvent{
		Type:      types.EventCompressionComplete,
		Timestamp: time.Now(),
	}
}

// analyticsWorker reads from analyticsQueue and records events.
func (p *Proxy) analyticsWorker() {
	defer p.wg.Done()
	for {
		select {
		case event := <-p.analyticsQueue:
			p.analytics.Record(event)
			if p.sessionLogger != nil {
				p.sessionLogger.Log(
					"INFO", "analytics",
					fmt.Sprintf("event: %v provider=%v saved=%d", event.Type, event.Provider, event.TokensSaved),
				)
			}
			// Fan out to TUI via program.Send if available.
			if p.tuiSendFn != nil && event.Type == types.EventRequestProcessed {
				rm := types.RequestMetrics{
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
				}
				p.tuiSendFn(rm)
			}
		case <-p.shutdownCh:
			return
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
func (p *Proxy) cacheJanitor() {
	defer p.wg.Done()
	ticker := time.NewTicker(cacheJanitorInterval)
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
func (p *Proxy) analyticsPeriodicFlush() {
	defer p.wg.Done()
	ticker := time.NewTicker(analyticsFlushInterval)
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

// Shutdown performs a graceful shutdown of the proxy.
func (p *Proxy) Shutdown(ctx context.Context) error {
	slog.Info("proxy shutdown initiated")

	if err := p.server.Shutdown(ctx); err != nil {
		slog.Warn("server shutdown error", "error", err)
	}

	close(p.shutdownCh)

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
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

package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/buildinfo"
	"github.com/slimference/slimference/internal/caching"
	"github.com/slimference/slimference/internal/compression"
	"github.com/slimference/slimference/internal/config"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/security"
	"github.com/slimference/slimference/internal/sessions"
	"github.com/slimference/slimference/internal/summarization"
	"github.com/slimference/slimference/internal/types"
)

// newFileWatcherFunc is called by New to create the file watcher; overridden in tests.
var newFileWatcherFunc = caching.NewFileWatcher
var errRequestBodyTooLarge = errors.New("request body too large")

const maxRequestBodySize = 32 * 1024 * 1024

// Version is the binary version string exposed by health/status surfaces.
var Version = buildinfo.Version

// Proxy is the core Slimference instance. It owns all compression layers, goroutines,
// and the HTTP server. Its lifecycle matches the TUI lifecycle: one instance per run.
type Proxy struct {
	config *config.Config

	// HTTP server and upstream clients.
	server      *http.Server
	httpClients map[types.Provider]*http.Client
	listenerMu  sync.RWMutex
	listener    net.Listener

	// Compression layers.
	layer1          *compression.DeterministicCompressor
	layer2          *summarization.Layer2
	responseCache   *caching.ResponseCache
	fileWatcher     *caching.FileWatcher
	secretsDetector *security.Detector

	// Analytics.
	analytics     *analytics.Analytics
	persister     *analytics.Persister
	sessionLogger *sessions.SessionLogger

	// Async pipelines.
	compressQueue  chan types.CompressJob
	analyticsQueue chan types.AnalyticsEvent
	workerCtx      context.Context
	workerCancel   context.CancelFunc
	shutdownCh     chan struct{}
	shutdownOnce   sync.Once
	wg             sync.WaitGroup

	// Analytics queue telemetry (T42). Counters are updated via trySendAnalytics
	// so every non-blocking send site is instrumented uniformly. Tests pause the
	// rate-limited warn via analyticsWarnClock.
	analyticsEnqueued atomic.Int64
	analyticsDropped  atomic.Int64
	analyticsLastWarn atomic.Int64 // unix-nano of last drop warn, for 1/min rate limit

	// Pipeline phase histograms (T58). Per-phase p50/p95/avg/max on a
	// 200-sample rolling window so TUI + /admin/status can surface
	// which layer is responsible for latency.
	pipelineHist *analytics.PipelineHistograms

	// Runtime toggle atomics. Index 0=Anthropic, 1=OpenAI, 2=CodexChatGPT.
	// Index 0=Layer1, 1=Layer2, 2=Layer3 for layers.
	providerEnabled [3]atomic.Bool
	layerEnabled    [3]atomic.Bool

	// Bypass (T67) short-circuits every isLayerEnabled / isProviderEnabled
	// check when set, producing a transparent passthrough relay. Hot-
	// reloadable via admin POST /admin/bypass and the `B` TUI hotkey;
	// persisted alongside other toggles.
	bypassMode atomic.Bool

	// Debug decision recorder - records per-request Layer 1 summaries for "slimference debug last".
	debugRecorder *dbg.Recorder

	// Health monitor - tracks per-provider upstream health from actual request results (spec §17.5).
	healthMon *healthMonitor

	// TUI send function - set after TUI program is created.
	// Protected by tuiSendMu so race detector is satisfied even though in practice
	// SetTUISendFn is called before Start() and the goroutines launch.
	tuiSendMu sync.RWMutex
	tuiSendFn func(types.RequestMetrics)
}

// recoverMiddleware wraps an HTTP handler with panic recovery (spec §17.2).
// On panic: logs the stack trace and attempts to forward the original request
// unmodified via handlePassthrough. If the original body is unavailable (panic
// happened before it was read), returns 502 Bad Gateway.
func (p *Proxy) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("handler panic recovered",
					"error", rec,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				// Best-effort passthrough: use the body stashed in context before compression.
				if body, ok := r.Context().Value(origBodyKey{}).([]byte); ok && body != nil {
					provider := detectProvider(r.URL.Path, body)
					p.handlePassthrough(w, r, provider, body)
					return
				}
				http.Error(w, "proxy error: internal panic", http.StatusBadGateway)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// New creates and initializes a fully configured Proxy. It does not start listening.
func New(cfg *config.Config) *Proxy {
	workerCtx, workerCancel := context.WithCancel(context.Background())
	p := &Proxy{
		config:         cfg,
		httpClients:    make(map[types.Provider]*http.Client),
		compressQueue:  make(chan types.CompressJob, 4),
		analyticsQueue: make(chan types.AnalyticsEvent, 256),
		workerCtx:      workerCtx,
		workerCancel:   workerCancel,
		shutdownCh:     make(chan struct{}),
		pipelineHist:   analytics.NewPipelineHistograms(),
	}

	// Default all toggles to enabled.
	p.providerEnabled[types.Anthropic].Store(true)
	p.providerEnabled[types.OpenAI].Store(true)
	p.providerEnabled[types.CodexChatGPT].Store(true)
	p.layerEnabled[0].Store(cfg.Compression.Layer1Enabled)
	p.layerEnabled[1].Store(cfg.Compression.Layer2Enabled)
	p.layerEnabled[2].Store(cfg.Compression.Layer3Enabled)

	// Build upstream HTTP clients with sensible timeouts.
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second, // SSE streams can be long
		DisableCompression:    true,              // we handle our own compression
	}
	upstreamClient := &http.Client{Transport: transport}
	p.httpClients[types.Anthropic] = upstreamClient
	p.httpClients[types.OpenAI] = upstreamClient
	p.httpClients[types.CodexChatGPT] = upstreamClient

	// Layer 1: Deterministic compressor.
	p.layer1 = compression.NewDeterministicCompressor(&cfg.Compression)

	// T61: install tool-compressor heuristic tuning from config so the
	// package-global atomic reflects the user's overrides.
	compression.SetToolCompressorTuning(compression.ToolCompressorTuning{
		AggressiveAfterMultiplier: cfg.Compression.Tuning.ToolCompressor.AggressiveAfterMultiplier,
		GitModerateDiffLimit:      cfg.Compression.Tuning.ToolCompressor.GitModerateDiffLimit,
		TestMaxFailureLines:       cfg.Compression.Tuning.ToolCompressor.TestMaxFailureLines,
	})

	// Layer 2: MiniMax summarizer.
	p.layer2 = summarization.NewLayer2(&cfg.Compression)

	// Layer 3: Response cache.
	p.responseCache = caching.NewResponseCache(
		cfg.Cache.ResponseCacheMaxEntries,
		cfg.Cache.ResponseCacheTTL(),
	)

	// File watcher for cache invalidation.
	fw, err := newFileWatcherFunc(func(path string) {
		p.responseCache.Invalidate(path)
	})
	if err != nil {
		slog.Warn("file watcher init failed, disabling", "error", err)
	} else {
		p.fileWatcher = fw
	}

	// Secret detector.
	mode := cfg.Secrets.Mode
	if mode != "off" {
		customPatterns := make([]security.SecretPattern, 0, len(cfg.Secrets.CustomPatterns))
		for _, cp := range cfg.Secrets.CustomPatterns {
			sp, err := security.CompilePattern(cp.Name, cp.Regex)
			if err != nil {
				slog.Warn("invalid custom secret pattern", "name", cp.Name, "error", err)
				continue
			}
			customPatterns = append(customPatterns, sp)
		}
		p.secretsDetector = security.NewDetector(mode, customPatterns, cfg.Secrets.Allowlist)
	}

	// Analytics.
	p.analytics = analytics.NewAnalytics()
	p.sessionLogger = sessions.NewSessionLogger()

	// Health monitor: tracks upstream health from actual request outcomes, no polling (spec §17.5).
	p.healthMon = newHealthMonitor()

	// Debug decision recorder (ring buffer capacity from config, default 100).
	decisionsLog := strings.TrimSpace(cfg.Debug.DecisionsLog)
	maxEntries := cfg.Debug.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 100
	}
	p.debugRecorder = dbg.NewRecorder(maxEntries, decisionsLog)

	// Persistent analytics log.
	if cfg.Analytics.LogDir != "" {
		persister, err := analytics.NewPersister(cfg.Analytics.ResolvedLogDir())
		if err != nil {
			slog.Warn("analytics persister init failed", "error", err)
		} else {
			p.persister = persister
		}
	}

	// HTTP server with the proxy mux.
	mux := http.NewServeMux()
	mux.HandleFunc("/health", p.healthHandler)
	mux.HandleFunc(AdminStatusPath, p.adminStatusHandler)
	mux.HandleFunc(AdminProviderPath, p.adminProviderHandler)
	mux.HandleFunc(AdminLayerPath, p.adminLayerHandler)
	mux.HandleFunc(AdminSecuritySuspendPath, p.adminSecuritySuspendHandler)
	mux.HandleFunc(AdminBypassPath, p.adminBypassHandler)
	mux.HandleFunc(AdminFlushPath, p.adminFlushHandler)
	mux.HandleFunc("/", p.ServeHTTP)

	p.server = &http.Server{
		Handler:      p.recoverMiddleware(mux),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 0, // no write timeout for SSE streams
		IdleTimeout:  120 * time.Second,
	}

	return p
}

// SetTUISendFn wires up the TUI event delivery function after the TUI program is created.
func (p *Proxy) SetTUISendFn(fn func(types.RequestMetrics)) {
	p.tuiSendMu.Lock()
	p.tuiSendFn = fn
	p.tuiSendMu.Unlock()
}

// Config returns the proxy configuration.
func (p *Proxy) Config() *config.Config {
	return p.config
}

// analyticsWarnIntervalNs is the minimum gap between "analytics_queue_full"
// warn log emissions. Tests may override by writing directly.
var analyticsWarnIntervalNs int64 = int64(time.Minute)

// trySendAnalytics sends ev on analyticsQueue non-blocking and records the
// outcome atomically. On drop, it emits a rate-limited slog.Warn so operators
// can distinguish a stuck collector from a healthy burst. All non-blocking
// analytics send sites must route through this helper to keep counters honest.
func (p *Proxy) trySendAnalytics(ev types.AnalyticsEvent) {
	select {
	case p.analyticsQueue <- ev:
		p.analyticsEnqueued.Add(1)
	default:
		p.analyticsDropped.Add(1)
		now := time.Now().UnixNano()
		last := p.analyticsLastWarn.Load()
		if now-last >= analyticsWarnIntervalNs &&
			p.analyticsLastWarn.CompareAndSwap(last, now) {
			slog.Warn("analytics_queue_full",
				"event", "analytics_drop",
				"dropped_total", p.analyticsDropped.Load(),
				"capacity", cap(p.analyticsQueue),
				"depth", len(p.analyticsQueue),
			)
		}
	}
}

// AnalyticsQueueStats returns a snapshot of analytics queue telemetry.
// Safe for concurrent use; values are consistent at read time.
type AnalyticsQueueStats struct {
	Capacity      int   `json:"capacity"`
	Depth         int   `json:"depth"`
	EnqueuedTotal int64 `json:"enqueued_total"`
	DroppedTotal  int64 `json:"dropped_total"`
}

// AnalyticsQueueStats reports current analytics queue telemetry.
func (p *Proxy) AnalyticsQueueStats() AnalyticsQueueStats {
	return AnalyticsQueueStats{
		Capacity:      cap(p.analyticsQueue),
		Depth:         len(p.analyticsQueue),
		EnqueuedTotal: p.analyticsEnqueued.Load(),
		DroppedTotal:  p.analyticsDropped.Load(),
	}
}

// Start binds the listener and begins serving. It is non-blocking; call from a goroutine.
func (p *Proxy) Start() error {
	addr := p.config.ListenAddr()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	p.listenerMu.Lock()
	p.listener = ln
	p.listenerMu.Unlock()

	slog.Info("proxy listening", "addr", addr)

	// Start background workers.
	p.wg.Add(1)
	go p.compressionWorker()
	p.wg.Add(1)
	go p.analyticsWorker()
	p.wg.Add(1)
	go p.cacheJanitor(cacheJanitorInterval)
	p.wg.Add(1)
	go p.analyticsPeriodicFlush(analyticsFlushInterval)

	return p.server.Serve(ln)
}

// ServeHTTP is the main HTTP handler for all incoming requests.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Detect provider from URL path.
	provider := detectProvider(r.URL.Path, nil)

	// Fast passthrough for non-compress-eligible paths.
	if !isCompressiblePath(r.URL.Path) {
		body, err := readBody(r)
		if err != nil {
			if errors.Is(err, errRequestBodyTooLarge) {
				p.proxyError(w, http.StatusRequestEntityTooLarge, err.Error())
				return
			}
			p.proxyError(w, http.StatusBadRequest, "read body failed")
			return
		}
		p.handlePassthrough(w, r, provider, body)
		return
	}

	// Read and stash the request body (needed for retry-on-overflow).
	body, err := readBody(r)
	if err != nil {
		if errors.Is(err, errRequestBodyTooLarge) {
			p.proxyError(w, http.StatusRequestEntityTooLarge, err.Error())
			return
		}
		p.proxyError(w, http.StatusBadRequest, "read body failed")
		return
	}

	// Re-detect provider with body available.
	provider = detectProvider(r.URL.Path, body)

	// If this provider is toggled off: passthrough without compression.
	if !p.isProviderEnabled(provider) {
		p.handlePassthrough(w, r, provider, body)
		return
	}

	// Attach original body to context for retry-on-overflow fallback.
	ctx := context.WithValue(r.Context(), origBodyKey{}, body)
	r = r.WithContext(ctx)

	p.handleCompressibleRequest(w, r, provider, body)
}

// isCompressiblePath returns true for the endpoints that support message compression.
func isCompressiblePath(path string) bool {
	// Anthropic: POST /v1/messages (not /v1/messages/batches)
	if path == "/v1/messages" {
		return true
	}
	// OpenAI: POST /v1/chat/completions
	if path == "/v1/chat/completions" {
		return true
	}
	// Also handle paths with leading slash variations.
	clean := strings.TrimSuffix(path, "/")
	return clean == "/v1/messages" || clean == "/v1/chat/completions"
}

// upstreamURL constructs the full upstream URL for the given provider.
// T66: CodexChatGPT routes to chatgpt.com (or chatgpt_base_url override) so
// Codex's /backend-api/codex/* paths hit the real ChatGPT backend rather
// than api.openai.com (which would 404 the Codex-specific routes).
func (p *Proxy) upstreamURL(provider types.Provider, path, rawQuery string) string {
	var base string
	switch provider {
	case types.Anthropic:
		base = strings.TrimSuffix(p.config.Upstream.Anthropic.BaseURL, "/")
	case types.OpenAI:
		base = strings.TrimSuffix(p.config.Upstream.OpenAI.BaseURL, "/")
	case types.CodexChatGPT:
		base = strings.TrimSuffix(p.config.Upstream.CodexChatGPT.BaseURL, "/")
		if base == "" {
			base = "https://chatgpt.com"
		}
	default:
		base = "https://api.anthropic.com"
	}
	u := base + path
	if rawQuery != "" {
		u += "?" + rawQuery
	}
	return u
}

// Toggle methods - goroutine-safe via atomic.Bool.

// SetProviderEnabled enables or disables compression for a provider.
func (p *Proxy) SetProviderEnabled(prov types.Provider, enabled bool) {
	if int(prov) < len(p.providerEnabled) {
		p.providerEnabled[prov].Store(enabled)
	}
}

// SetLayerEnabled enables or disables a compression layer (1-indexed).
func (p *Proxy) SetLayerEnabled(layer int, enabled bool) {
	if layer >= 1 && layer <= len(p.layerEnabled) {
		p.layerEnabled[layer-1].Store(enabled)
	}
}

func (p *Proxy) isProviderEnabled(prov types.Provider) bool {
	if p.bypassMode.Load() {
		return false
	}
	if int(prov) < len(p.providerEnabled) {
		return p.providerEnabled[prov].Load()
	}
	return false
}

// SetBypass toggles the T67 master bypass. While on, every request is
// forwarded byte-equal without passing through Layer 1/2/3.
func (p *Proxy) SetBypass(enabled bool) { p.bypassMode.Store(enabled) }

// Bypass reports the current master-bypass state.
func (p *Proxy) Bypass() bool { return p.bypassMode.Load() }

func (p *Proxy) isLayerEnabled(layer int) bool {
	if p.bypassMode.Load() {
		return false
	}
	if layer >= 1 && layer <= len(p.layerEnabled) {
		return p.layerEnabled[layer-1].Load()
	}
	return false
}

// IsProviderEnabled is the exported version for TUI.
func (p *Proxy) IsProviderEnabled(prov types.Provider) bool {
	return p.isProviderEnabled(prov)
}

// IsLayerEnabled is the exported version for TUI.
func (p *Proxy) IsLayerEnabled(layer int) bool {
	return p.isLayerEnabled(layer)
}

// ListenAddr returns the address the proxy is listening on, or "" if not started.
func (p *Proxy) ListenAddr() string {
	p.listenerMu.RLock()
	l := p.listener
	p.listenerMu.RUnlock()
	if l != nil {
		return l.Addr().String()
	}
	return p.config.ListenAddr()
}

// HasListener reports whether Start has successfully bound a listener.
func (p *Proxy) HasListener() bool {
	p.listenerMu.RLock()
	defer p.listenerMu.RUnlock()
	return p.listener != nil
}

// GetLayer2Cache returns the Layer 2 summary cache for TUI inspection.
func (p *Proxy) GetLayer2Cache() *summarization.SummaryCache {
	if p.layer2 == nil {
		return nil
	}
	return p.layer2.GetCache()
}

// ClearLayer2ForTesting removes Layer 2 so cmd/slimference can cover GetLayer2Status when the cache is absent.
func (p *Proxy) ClearLayer2ForTesting() { p.layer2 = nil }

// CompressQueue returns the compression job queue (read-only access for TUI).
func (p *Proxy) CompressQueue() chan types.CompressJob {
	return p.compressQueue
}

// SessionLogger returns the session logger for the debug view.
func (p *Proxy) SessionLogger() *sessions.SessionLogger {
	return p.sessionLogger
}

// DebugRecorder returns the debug decision recorder for the "debug last" CLI command.
func (p *Proxy) DebugRecorder() *dbg.Recorder {
	return p.debugRecorder
}

// GetProviderHealth returns the health snapshot for the given provider,
// derived from actual request outcomes (spec §17.5). Never pings upstream APIs.
func (p *Proxy) GetProviderHealth(prov types.Provider) types.ProviderHealthInfo {
	if p.healthMon == nil {
		return types.ProviderHealthInfo{Status: types.ProviderHealthIdle}
	}
	return p.healthMon.getStatus(prov)
}

// Handler returns the HTTP handler for the proxy, including the /health endpoint.
// Used by integration tests to exercise the real handler without starting a TCP listener.
func (p *Proxy) Handler() http.Handler {
	return p.server.Handler
}

// readBody reads and closes the request body, returning its contents.
func readBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodySize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxRequestBodySize {
		return nil, errRequestBodyTooLarge
	}
	return body, nil
}

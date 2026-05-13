package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/buildinfo"
	"github.com/slimference/slimference/internal/caching"
	"github.com/slimference/slimference/internal/compression"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/contentarchive"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/outputreduce"
	"github.com/slimference/slimference/internal/quality"
	"github.com/slimference/slimference/internal/security"
	"github.com/slimference/slimference/internal/sessions"
	"github.com/slimference/slimference/internal/summarization"
	"github.com/slimference/slimference/internal/tlsca"
	"github.com/slimference/slimference/internal/tlsdial"
	"github.com/slimference/slimference/internal/toolprune"
	"github.com/slimference/slimference/internal/types"
)

// newFileWatcherFunc is called by New to create the file watcher; overridden in tests.
var newFileWatcherFunc = caching.NewFileWatcher
var errRequestBodyTooLarge = errors.New("request body too large")
var proxyUserHomeDir = os.UserHomeDir

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
	// Quality signals (T77). Re-read detector tracks repeated tool-key
	// observations within a short window; cache-miss spike detector
	// flags rolling prompt-cache regressions; net-savings keeps the
	// "saved minus invalidation cost" running total. All three are
	// exposed via /admin/status.quality.
	qualityReRead     *quality.ReReadDetector
	qualityCacheSpike *quality.CacheMissSpikeDetector
	qualityNetSavings *quality.NetSavings
	// bypassExpiryNano is the unix-nano deadline for a duration-bounded
	// bypass (T81). Zero means "no expiry"; non-zero means bypass auto-
	// reverts when time.Now().UnixNano() >= the stored value. Read on
	// every bypass check so revert is lazy and lock-free.
	bypassExpiryNano atomic.Int64
	// bypassAutoRevertCount counts how many lazy auto-reverts have fired
	// for /admin/status.bypass observability. T81.
	bypassAutoRevertCount atomic.Int64
	// bypassNextRequestCount tracks how many remaining requests should
	// bypass before auto-reverting. T81: `slimference bypass on
	// --next-request[=N]` sets this; each matched request decrements
	// it. When it reaches zero, bypass turns off.
	bypassNextRequestCount atomic.Int64
	// bypassToolsMu / bypassTools (T81 follow-up): per-tool bypass set.
	// When a request's last user-turn tool_result names a bypassed tool,
	// the request is forwarded byte-equal. Empty set means the per-tool
	// gate is off; the global bypass overrides this set.
	bypassToolsMu sync.RWMutex
	bypassTools   map[string]struct{}
	// bypassRoutesMu / bypassRoutes (T81 follow-up): per-route bypass set.
	// Matched against r.URL.Path with exact equality so an operator can
	// disable compression on `/v1/messages` while leaving `/v1/chat/
	// completions` compressed (e.g. for staged rollouts).
	bypassRoutesMu sync.RWMutex
	bypassRoutes   map[string]struct{}
	// toolPrune holds the per-session tool-usage tracker (T103). Always
	// constructed; the actual prune-decision wiring is gated by
	// [compression.tuning] tool_prune_enabled.
	toolPrune *toolprune.UsageTracker
	// serverState holds the T78 per-session response-id store. Always
	// constructed; live wiring is gated by [proxy] server_state_enabled.
	serverState *sessions.ResponseStateStore
	// outputReduce tracks T130 prompt-injection overhead and observed output.
	outputReduce          *outputreduce.Tracker
	openAIPromptCacheMu   sync.Mutex
	openAIPromptCacheRate map[string]promptCacheRateBucket
	webSocketTunnel       *WebSocketTunnel

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
					provider := detectProviderWithUA(r.URL.Path, body, r.Header.Get("User-Agent"))
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
		config:            cfg,
		httpClients:       make(map[types.Provider]*http.Client),
		compressQueue:     make(chan types.CompressJob, 4),
		analyticsQueue:    make(chan types.AnalyticsEvent, 256),
		workerCtx:         workerCtx,
		workerCancel:      workerCancel,
		shutdownCh:        make(chan struct{}),
		pipelineHist:      analytics.NewPipelineHistograms(),
		qualityReRead:     quality.NewReReadDetector(10),
		qualityCacheSpike: quality.NewCacheMissSpikeDetector(50, 0.25),
		qualityNetSavings: quality.NewNetSavings(),
		toolPrune:         toolprune.NewUsageTracker(20),
		serverState:       sessions.NewResponseStateStore(1024),
		outputReduce: outputreduce.NewTrackerWithAutoTune(cfg.Compression.OutputReduce.Enabled, cfg.Compression.OutputReduce.Profile, outputreduce.AutoTuneConfig{
			Enabled:             cfg.Compression.OutputReduce.AutoTuneEnabled,
			MinSamples:          cfg.Compression.OutputReduce.AutoTuneMinSamples,
			MinNetSavingsPct:    cfg.Compression.OutputReduce.MinNetSavingsPct,
			MaxFailureRateDelta: cfg.Compression.OutputReduce.MaxFailureRateDelta,
			CooldownTurns:       cfg.Compression.OutputReduce.CooldownTurns,
		}),
	}

	// Default all toggles to enabled.
	p.providerEnabled[types.Anthropic].Store(true)
	p.providerEnabled[types.OpenAI].Store(true)
	p.providerEnabled[types.CodexChatGPT].Store(true)
	p.layerEnabled[0].Store(cfg.Compression.Layer1Enabled)
	p.layerEnabled[1].Store(cfg.Compression.Layer2Enabled)
	p.layerEnabled[2].Store(cfg.Compression.Layer3Enabled)

	tlsResolver, err := tlsdial.NewResolver(cfg.Transparent.DefaultTLSProfile, cfg.Transparent.TLSProfiles)
	if err != nil {
		slog.Warn("transparent TLS profile config invalid; using Go stdlib", "error", err)
		tlsResolver, _ = tlsdial.NewResolver("go_stdlib", nil)
	}

	// Build upstream HTTP clients with sensible timeouts.
	transport := newUpstreamTransport(cfg, tlsResolver)
	upstreamClient := &http.Client{Transport: transport}
	p.httpClients[types.Anthropic] = upstreamClient
	p.httpClients[types.OpenAI] = upstreamClient
	p.httpClients[types.CodexChatGPT] = upstreamClient
	p.webSocketTunnel = &WebSocketTunnel{
		Dialer:      newProfiledWebSocketDialer(tlsResolver),
		Logger:      slog.Default(),
		BypassPaths: cfg.Transparent.AudioBypassPaths,
	}

	// Layer 1: Deterministic compressor.
	p.layer1 = compression.NewDeterministicCompressor(&cfg.Compression)

	// T76: wire the content-archive recorder so lossy Layer 1 sub-layers
	// archive original content before mutation. This is the safety net
	// that lets aggressive defaults (T74 default-on, T100 coordinator,
	// T103 tool pruning) ship without being lossy. Best-effort: if home
	// is unavailable, the compressor falls back to no archiving and
	// continues to compress as before.
	if home, err := os.UserHomeDir(); err == nil {
		recorder := compression.NewDiskRecorder(
			contentarchive.DefaultDir(home),
			contentarchive.Limits{},
		)
		p.layer1.WithRecorder(recorder)
	}

	// T61: install tool-compressor heuristic tuning from config so the
	// package-global atomic reflects the user's overrides.
	compression.SetToolCompressorTuning(compression.ToolCompressorTuning{
		AggressiveAfterMultiplier: cfg.Compression.Tuning.ToolCompressor.AggressiveAfterMultiplier,
		GitModerateDiffLimit:      cfg.Compression.Tuning.ToolCompressor.GitModerateDiffLimit,
		TestMaxFailureLines:       cfg.Compression.Tuning.ToolCompressor.TestMaxFailureLines,
	})

	// T86: load the optional prompt-override file once at startup so
	// the operator can iterate on the system prompt without rebuilding.
	// Best-effort: a missing or unreadable file logs a warning and
	// keeps the compile-time default.
	if path := cfg.Compression.PromptOverridePath; path != "" {
		if version, err := summarization.LoadPromptOverrideFromPath(path); err != nil {
			slog.Warn("prompt override load failed", "path", path, "error", err)
		} else {
			slog.Info("prompt override loaded", "path", path, "version", version)
		}
	}

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

	var handler http.Handler = mux
	if cfg.Transparent.Enabled {
		if signer, err := newTransparentSigner(cfg); err != nil {
			slog.Error("transparent proxy disabled: CA init failed", "error", err)
		} else {
			connect := NewConnectInterceptor(signer, mux, cfg.Transparent.InterceptHosts)
			connect.SetLogger(slog.Default())
			connect.SetDebugRecorder(p.debugRecorder)
			connect.SetWebSocketTunnel(p.webSocketTunnel)
			handler = connect
			slog.Info("transparent proxy enabled", "intercept_hosts", cfg.Transparent.InterceptHosts)
		}
	}

	p.server = &http.Server{
		Handler:      p.recoverMiddleware(handler),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 0, // no write timeout for SSE streams
		IdleTimeout:  120 * time.Second,
	}

	return p
}

func newUpstreamTransport(cfg *config.Config, resolver tlsdial.Resolver) *http.Transport {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second, // SSE streams can be long
		DisableCompression:    true,              // we handle our own compression
	}
	if cfg.Transparent.Enabled {
		transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			return tlsdial.Dial(ctx, network, host, port, resolver.Resolve(host))
		}
	}
	return transport
}

func newProfiledWebSocketDialer(resolver tlsdial.Resolver) WebSocketDialer {
	return func(host, port string) (net.Conn, error) {
		return tlsdial.Dial(context.Background(), "tcp", host, port, resolver.Resolve(host))
	}
}

func newTransparentSigner(cfg *config.Config) (*tlsca.Signer, error) {
	caDir := strings.TrimSpace(cfg.Transparent.CADir)
	if caDir == "" {
		home, err := proxyUserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home for transparent CA: %w", err)
		}
		caDir = filepath.Join(home, ".slimference")
	} else {
		caDir = config.ExpandHomePath(caDir)
	}
	ca, err := tlsca.LoadOrGenerateCA(caDir)
	if err != nil {
		return nil, err
	}
	return tlsca.NewSigner(ca, cfg.Transparent.CertCacheSize), nil
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
	// Detect provider from URL path + UA. UA disambiguates Codex (which
	// sends /v1/responses through openai_base_url) from generic OpenAI.
	userAgent := r.Header.Get("User-Agent")
	provider := detectProviderWithUA(r.URL.Path, nil, userAgent)
	if IsWebSocketUpgrade(r) {
		p.handleDirectWebSocketUpgrade(w, r, provider)
		return
	}

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
	provider = detectProviderWithUA(r.URL.Path, body, userAgent)

	if !isProviderCompressiblePath(provider, r.URL.Path) {
		p.handlePassthrough(w, r, provider, body)
		return
	}

	// If this provider is toggled off: passthrough without compression.
	if !p.isProviderEnabled(provider) {
		p.handlePassthrough(w, r, provider, body)
		return
	}

	// T81 follow-up: per-route bypass. The path-equality check is
	// cheap enough to run before message extraction so we save the
	// compression CPU on routes the operator has explicitly carved
	// out (e.g. for staged rollouts).
	if p.IsRouteBypassed(r.URL.Path) {
		p.handlePassthrough(w, r, provider, body)
		return
	}

	// T81 follow-up: per-tool bypass. Route through passthrough when
	// the request's last user turn carries a tool_result whose tool
	// is in the bypass set. Cheap byte-scan that avoids parsing the
	// full body when the set is empty.
	if p.hasBypassedTool(body) {
		p.handlePassthrough(w, r, provider, body)
		return
	}

	// Attach original body to context for retry-on-overflow fallback.
	ctx := context.WithValue(r.Context(), origBodyKey{}, body)
	r = r.WithContext(ctx)

	p.handleCompressibleRequest(w, r, provider, body)
}

// handleDirectWebSocketUpgrade supports non-mutating Codex CLI-only routing
// via `codex -c openai_base_url=http://127.0.0.1:8990/backend-api/codex`.
// That path reaches Slimference as a plain local WebSocket upgrade, not a
// CONNECT-MITM stream, so it must be tunneled before the JSON compression
// handler tries to read a nonexistent request body.
func (p *Proxy) handleDirectWebSocketUpgrade(w http.ResponseWriter, r *http.Request, provider types.Provider) {
	if p.webSocketTunnel == nil {
		http.Error(w, "websocket tunnel unavailable", http.StatusBadGateway)
		return
	}
	host, ok := p.upstreamHost(provider)
	if !ok {
		http.Error(w, "upstream host unavailable", http.StatusBadGateway)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		slog.Error("websocket direct: hijack failed", "path", r.URL.Path, "err", err)
		return
	}
	defer clientConn.Close()
	if p.debugRecorder != nil {
		p.debugRecorder.Record(dbg.RequestSummary{
			RequestID: newRequestIDFn(),
			Timestamp: time.Now(),
			Source:    "proxy",
			Provider:  provider.String(),
			Host:      r.Host,
			Path:      r.URL.Path,
			RouteMode: "websocket_tunnel",
		})
	}
	p.webSocketTunnel.ServeUpgrade(clientConn, r, host)
}

func (p *Proxy) upstreamHost(provider types.Provider) (string, bool) {
	u, err := url.Parse(p.upstreamURL(provider, "", ""))
	if err != nil {
		return "", false
	}
	host := u.Hostname()
	if host == "" {
		return "", false
	}
	return host, true
}

// hasBypassedTool reports whether the request body references a tool
// from the per-tool bypass set. Returns false fast when the set is
// empty so the hot-path overhead is one map-load. T81 follow-up.
func (p *Proxy) hasBypassedTool(body []byte) bool {
	p.bypassToolsMu.RLock()
	if len(p.bypassTools) == 0 {
		p.bypassToolsMu.RUnlock()
		return false
	}
	tools := make([]string, 0, len(p.bypassTools))
	for t := range p.bypassTools {
		tools = append(tools, t)
	}
	p.bypassToolsMu.RUnlock()
	bodyStr := string(body)
	for _, tool := range tools {
		// Both Anthropic (`"name":"X"`) and OpenAI/Codex
		// (`"function":{"name":"X"}` or top-level `"name":"X"`)
		// shapes carry the tool name as `"name":"<tool>"` in JSON.
		needle := `"name":"` + tool + `"`
		if strings.Contains(bodyStr, needle) {
			return true
		}
	}
	return false
}

// isCompressiblePath returns true for the endpoints that support message compression.
func isCompressiblePath(path string) bool {
	clean := strings.TrimSuffix(path, "/")
	// Anthropic: POST /v1/messages (not /v1/messages/batches)
	if clean == "/v1/messages" {
		return true
	}
	// OpenAI: POST /v1/chat/completions
	if clean == "/v1/chat/completions" {
		return true
	}
	// Codex: these paths may carry Responses API conversation input. The
	// provider-specific gate below keeps generic OpenAI /v1/responses traffic
	// passthrough unless UA/body detection proves it is Codex.
	return clean == "/v1/responses" || strings.HasPrefix(clean, "/backend-api/codex/")
}

func isProviderCompressiblePath(provider types.Provider, path string) bool {
	clean := strings.TrimSuffix(path, "/")
	switch provider {
	case types.Anthropic:
		return clean == "/v1/messages"
	case types.OpenAI:
		return clean == "/v1/chat/completions"
	case types.CodexChatGPT:
		return clean == "/v1/responses" || strings.HasPrefix(clean, "/backend-api/codex/")
	default:
		return false
	}
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
	// T81: route through Bypass() so a duration-bounded bypass auto-
	// reverts at the deadline rather than sticking until next admin call.
	if p.Bypass() {
		return false
	}
	if int(prov) < len(p.providerEnabled) {
		return p.providerEnabled[prov].Load()
	}
	return false
}

// SetBypass toggles the T67 master bypass. While on, every request is
// forwarded byte-equal without passing through Layer 1/2/3. Setting
// false also clears any T81 duration-bounded bypass timer.
func (p *Proxy) SetBypass(enabled bool) {
	p.bypassMode.Store(enabled)
	if !enabled {
		p.bypassExpiryNano.Store(0)
	}
}

// SetBypassFor enables bypass with an automatic revert after d. T81. A
// non-positive duration is treated as "until explicitly cleared".
func (p *Proxy) SetBypassFor(d time.Duration) {
	p.bypassMode.Store(true)
	if d <= 0 {
		p.bypassExpiryNano.Store(0)
		return
	}
	p.bypassExpiryNano.Store(time.Now().Add(d).UnixNano())
}

// SetBypassForNextRequests enables bypass and auto-reverts after the
// next n matched requests. n <= 0 is treated as 1. T81.
func (p *Proxy) SetBypassForNextRequests(n int) {
	if n <= 0 {
		n = 1
	}
	p.bypassMode.Store(true)
	p.bypassExpiryNano.Store(0)
	p.bypassNextRequestCount.Store(int64(n))
}

// ConsumeBypassRequest decrements the per-request bypass counter when
// it is active and clears bypass once the budget reaches zero. Called
// by the proxy at the end of each request that flowed through the
// bypass path. Returns true when this call was the one that flipped
// bypass off.
func (p *Proxy) ConsumeBypassRequest() bool {
	if !p.bypassMode.Load() {
		return false
	}
	remaining := p.bypassNextRequestCount.Load()
	if remaining <= 0 {
		return false
	}
	if p.bypassNextRequestCount.Add(-1) <= 0 {
		p.bypassMode.Store(false)
		p.bypassExpiryNano.Store(0)
		p.bypassAutoRevertCount.Add(1)
		return true
	}
	return false
}

// BypassNextRequestCount exposes the remaining per-request bypass
// budget for telemetry / TUI surfaces.
func (p *Proxy) BypassNextRequestCount() int64 { return p.bypassNextRequestCount.Load() }

// BypassExpiresAt returns the duration-bounded bypass deadline (zero
// time when none is set or bypass is off). T81.
func (p *Proxy) BypassExpiresAt() time.Time {
	if !p.bypassMode.Load() {
		return time.Time{}
	}
	v := p.bypassExpiryNano.Load()
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(0, v).UTC()
}

// BypassAutoRevertCount returns the cumulative count of lazy
// auto-reverts since process start. T81 telemetry.
func (p *Proxy) BypassAutoRevertCount() int64 {
	return p.bypassAutoRevertCount.Load()
}

// SetBypassedTools replaces the per-tool bypass set. An empty list
// disables the per-tool gate. T81 follow-up.
func (p *Proxy) SetBypassedTools(tools []string) {
	p.bypassToolsMu.Lock()
	defer p.bypassToolsMu.Unlock()
	if len(tools) == 0 {
		p.bypassTools = nil
		return
	}
	p.bypassTools = make(map[string]struct{}, len(tools))
	for _, t := range tools {
		if t == "" {
			continue
		}
		p.bypassTools[t] = struct{}{}
	}
}

// BypassedTools returns the current per-tool bypass set as a sorted
// slice. T81 follow-up telemetry.
func (p *Proxy) BypassedTools() []string {
	p.bypassToolsMu.RLock()
	defer p.bypassToolsMu.RUnlock()
	if len(p.bypassTools) == 0 {
		return nil
	}
	out := make([]string, 0, len(p.bypassTools))
	for t := range p.bypassTools {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// IsToolBypassed reports whether the named tool is in the per-tool
// bypass set. T81 follow-up.
func (p *Proxy) IsToolBypassed(toolName string) bool {
	if toolName == "" {
		return false
	}
	p.bypassToolsMu.RLock()
	defer p.bypassToolsMu.RUnlock()
	_, ok := p.bypassTools[toolName]
	return ok
}

// SetBypassedRoutes replaces the per-route bypass set. An empty list
// disables the per-route gate. T81 follow-up.
func (p *Proxy) SetBypassedRoutes(routes []string) {
	p.bypassRoutesMu.Lock()
	defer p.bypassRoutesMu.Unlock()
	if len(routes) == 0 {
		p.bypassRoutes = nil
		return
	}
	p.bypassRoutes = make(map[string]struct{}, len(routes))
	for _, r := range routes {
		if r == "" {
			continue
		}
		p.bypassRoutes[r] = struct{}{}
	}
}

// BypassedRoutes returns the current per-route bypass set as a sorted
// slice. T81 follow-up telemetry.
func (p *Proxy) BypassedRoutes() []string {
	p.bypassRoutesMu.RLock()
	defer p.bypassRoutesMu.RUnlock()
	if len(p.bypassRoutes) == 0 {
		return nil
	}
	out := make([]string, 0, len(p.bypassRoutes))
	for r := range p.bypassRoutes {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// IsRouteBypassed reports whether the request path is in the per-route
// bypass set. T81 follow-up.
func (p *Proxy) IsRouteBypassed(path string) bool {
	if path == "" {
		return false
	}
	p.bypassRoutesMu.RLock()
	defer p.bypassRoutesMu.RUnlock()
	_, ok := p.bypassRoutes[path]
	return ok
}

// observeQuality feeds a finalized RequestSummary into the T77 quality
// detectors so /admin/status.quality reflects shipped traffic.
func (p *Proxy) observeQuality(summary dbg.RequestSummary) {
	if p.qualityCacheSpike != nil {
		p.qualityCacheSpike.Observe(summary.CacheHit)
	}
	if p.qualityNetSavings != nil {
		p.qualityNetSavings.RecordSaved(summary.Tokens.Saved)
	}
}

// QualitySnapshot returns the current quality-signal snapshot for
// /admin/status.quality (T77).
func (p *Proxy) QualitySnapshot() quality.QualitySnapshot {
	return quality.QualitySnapshot{
		ReRead:         p.qualityReRead.Stats(),
		CacheMissSpike: p.qualityCacheSpike.Stats(),
		NetSavings:     p.qualityNetSavings.Stats(),
	}
}

// ObserveQualityToolKey is the proxy-side hook for the re-read
// detector. Called when a tool_use block is observed during request
// processing. Empty session or key are no-ops.
func (p *Proxy) ObserveQualityToolKey(sessionID, toolKey string) {
	p.qualityReRead.Observe(sessionID, toolKey)
}

// ObserveQualityCacheHit feeds prompt-cache hit/miss outcomes into the
// rolling spike detector so /admin/status.quality.cache_miss_spike
// reflects recent traffic.
func (p *Proxy) ObserveQualityCacheHit(hit bool) {
	p.qualityCacheSpike.Observe(hit)
}

// ObserveQualitySavings logs saved tokens for the cumulative net-savings
// counter. Pass 0 to skip.
func (p *Proxy) ObserveQualitySavings(saved int) {
	p.qualityNetSavings.RecordSaved(saved)
}

// ObserveQualityInvalidation logs the estimated cost of a cache
// invalidation triggered by a compression-config change.
func (p *Proxy) ObserveQualityInvalidation(cost int) {
	p.qualityNetSavings.RecordInvalidation(cost)
}

// Bypass reports the current master-bypass state and lazily reverts a
// duration-bounded bypass whose deadline has passed. Lock-free: a stale
// expiry just means a single extra reverted check, never an incorrect
// "still on" reading after the deadline.
func (p *Proxy) Bypass() bool {
	if !p.bypassMode.Load() {
		return false
	}
	expiry := p.bypassExpiryNano.Load()
	if expiry == 0 {
		return true
	}
	if time.Now().UnixNano() < expiry {
		return true
	}
	if p.bypassMode.CompareAndSwap(true, false) {
		p.bypassExpiryNano.Store(0)
		p.bypassAutoRevertCount.Add(1)
	}
	return false
}

func (p *Proxy) isLayerEnabled(layer int) bool {
	// T81: route through Bypass() so duration-bounded bypass auto-
	// reverts on next read after the deadline.
	if p.Bypass() {
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

// GetRecentFlights returns the normalized flight records shown by the TUI.
func (p *Proxy) GetRecentFlights(n int) []dbg.FlightRequestSummary {
	if p.debugRecorder == nil {
		return nil
	}
	summaries := p.debugRecorder.Last(n, false)
	flights := make([]dbg.FlightRequestSummary, 0, len(summaries))
	for _, summary := range summaries {
		summary.EnsureFlight()
		if summary.Flight != nil {
			flights = append(flights, *summary.Flight)
		}
	}
	return flights
}

// GetProviderHealth returns the health snapshot for the given provider,
// derived from actual request outcomes (spec §17.5). Never pings upstream APIs.
func (p *Proxy) GetProviderHealth(prov types.Provider) types.ProviderHealthInfo {
	if p.healthMon == nil {
		return types.ProviderHealthInfo{Status: types.ProviderHealthIdle}
	}
	return p.healthMon.getStatus(prov)
}

// AnyProviderDegraded reports whether at least one tracked provider is
// currently `degraded` or `down`. Surfaced via /admin/status.any_provider_degraded
// so future TUI / menubar surfaces can render a single banner instead
// of computing the worst-of across providers themselves. T83.
func (p *Proxy) AnyProviderDegraded() bool {
	if p.healthMon == nil {
		return false
	}
	return p.healthMon.anyDegraded()
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

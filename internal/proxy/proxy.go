package proxy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/slimference/slimference/internal/analytics"
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

// Proxy is the core Slimference instance. It owns all compression layers, goroutines,
// and the HTTP server. Its lifecycle matches the TUI lifecycle: one instance per run.
type Proxy struct {
	config *config.Config

	// HTTP server and upstream clients.
	server      *http.Server
	httpClients map[types.Provider]*http.Client
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
	shutdownCh     chan struct{}
	wg             sync.WaitGroup

	// Runtime toggle atomics. Index 0=Anthropic, 1=OpenAI for providers.
	// Index 0=Layer1, 1=Layer2, 2=Layer3 for layers.
	providerEnabled [2]atomic.Bool
	layerEnabled    [3]atomic.Bool

	// Debug decision recorder - records per-request Layer 1 summaries for "slimference debug last".
	debugRecorder *dbg.Recorder

	// TUI send function - set after TUI program is created.
	tuiSendFn func(types.RequestMetrics)
}

// New creates and initializes a fully configured Proxy. It does not start listening.
func New(cfg *config.Config) *Proxy {
	p := &Proxy{
		config:         cfg,
		httpClients:    make(map[types.Provider]*http.Client),
		compressQueue:  make(chan types.CompressJob, 4),
		analyticsQueue: make(chan types.AnalyticsEvent, 256),
		shutdownCh:     make(chan struct{}),
	}

	// Default all toggles to enabled.
	p.providerEnabled[types.Anthropic].Store(true)
	p.providerEnabled[types.OpenAI].Store(true)
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
		DisableCompression:    true,               // we handle our own compression
	}
	upstreamClient := &http.Client{Transport: transport}
	p.httpClients[types.Anthropic] = upstreamClient
	p.httpClients[types.OpenAI] = upstreamClient

	// Layer 1: Deterministic compressor.
	p.layer1 = compression.NewDeterministicCompressor(&cfg.Compression)

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
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/", p.ServeHTTP)

	p.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 0, // no write timeout for SSE streams
		IdleTimeout:  120 * time.Second,
	}

	return p
}

// SetTUISendFn wires up the TUI event delivery function after the TUI program is created.
func (p *Proxy) SetTUISendFn(fn func(types.RequestMetrics)) {
	p.tuiSendFn = fn
}

// Config returns the proxy configuration.
func (p *Proxy) Config() *config.Config {
	return p.config
}

// Start binds the listener and begins serving. It is non-blocking; call from a goroutine.
func (p *Proxy) Start() error {
	addr := p.config.ListenAddr()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	p.listener = ln

	slog.Info("proxy listening", "addr", addr)

	// Start background workers.
	p.wg.Add(1)
	go p.compressionWorker()
	p.wg.Add(1)
	go p.analyticsWorker()
	p.wg.Add(1)
	go p.cacheJanitor()
	p.wg.Add(1)
	go p.analyticsPeriodicFlush()

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
			p.proxyError(w, http.StatusBadRequest, "read body failed")
			return
		}
		p.handlePassthrough(w, r, provider, body)
		return
	}

	// Read and stash the request body (needed for retry-on-overflow).
	body, err := readBody(r)
	if err != nil {
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
func (p *Proxy) upstreamURL(provider types.Provider, path, rawQuery string) string {
	var base string
	switch provider {
	case types.Anthropic:
		base = strings.TrimSuffix(p.config.Upstream.Anthropic.BaseURL, "/")
	case types.OpenAI:
		base = strings.TrimSuffix(p.config.Upstream.OpenAI.BaseURL, "/")
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
	if int(prov) < len(p.providerEnabled) {
		return p.providerEnabled[prov].Load()
	}
	return false
}

func (p *Proxy) isLayerEnabled(layer int) bool {
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
	if p.listener != nil {
		return p.listener.Addr().String()
	}
	return p.config.ListenAddr()
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

// readBody reads and closes the request body, returning its contents.
func readBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()
	return io.ReadAll(io.LimitReader(r.Body, 32*1024*1024)) // 32MB max
}

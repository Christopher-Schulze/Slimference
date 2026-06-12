package proxy

import (
	"context"
	"encoding/json"
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

	"github.com/Christopher-Schulze/Slimference/internal/analytics"
	"github.com/Christopher-Schulze/Slimference/internal/buildinfo"
	"github.com/Christopher-Schulze/Slimference/internal/caching"
	"github.com/Christopher-Schulze/Slimference/internal/chunkdedup"
	"github.com/Christopher-Schulze/Slimference/internal/compactsignal"
	"github.com/Christopher-Schulze/Slimference/internal/compression"
	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/contentarchive"
	"github.com/Christopher-Schulze/Slimference/internal/control"
	"github.com/Christopher-Schulze/Slimference/internal/control/apps"
	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/hostmetrics"
	"github.com/Christopher-Schulze/Slimference/internal/outputreduce"
	"github.com/Christopher-Schulze/Slimference/internal/promptcache"
	"github.com/Christopher-Schulze/Slimference/internal/quality"
	"github.com/Christopher-Schulze/Slimference/internal/qualityab"
	"github.com/Christopher-Schulze/Slimference/internal/readcache"
	"github.com/Christopher-Schulze/Slimference/internal/security"
	"github.com/Christopher-Schulze/Slimference/internal/sessions"
	"github.com/Christopher-Schulze/Slimference/internal/tlsca"
	"github.com/Christopher-Schulze/Slimference/internal/tlsdial"
	"github.com/Christopher-Schulze/Slimference/internal/toolprune"
	"github.com/Christopher-Schulze/Slimference/internal/toolusecache"
	"github.com/Christopher-Schulze/Slimference/internal/types"
	"github.com/Christopher-Schulze/Slimference/internal/wscompact"
)

// newFileWatcherFunc is called by New to create the file watcher; overridden in tests.
var newFileWatcherFunc = caching.NewFileWatcher
var errRequestBodyTooLarge = errors.New("request body too large")
var proxyUserHomeDir = os.UserHomeDir

const (
	maxRequestBodySize               = 32 * 1024 * 1024
	codexLayer0LatencyBudget         = 25 * time.Millisecond
	codexLayer0LatencyRecoveryBudget = 12 * time.Millisecond
	codexLayer0LatencyStrikeLimit    = int64(3)
	codexLayer0LatencyStateTTL       = 30 * time.Minute
	// codexLayer0LatencyProductiveBudget applies when the reducer pass saved
	// tokens: time spent producing real savings is not overhead pressure and
	// is invisible next to multi-second inference; only pathological
	// productive passes count as breaches.
	codexLayer0LatencyProductiveBudget = 250 * time.Millisecond
	// codexLayer0LatencyDecayAfter lets one strike decay on a dead-zone
	// sample (between recovery and breach budgets) once no breach happened
	// for this long, so the gate cannot latch permanently when demoted
	// passes never get under the recovery budget.
	codexLayer0LatencyDecayAfter = 60 * time.Second
	codexFootprintEMAAlpha       = 0.1
)

type codexFootprintEstimate struct {
	samples  int
	estimate float64
}

// Version is the binary version string exposed by health/status surfaces.
var Version = buildinfo.Version

func shortChunkID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

// Proxy is the core Slimference instance. It owns all compression layers, goroutines,
// and the HTTP server. Its lifecycle matches the TUI lifecycle: one instance per run.
type Proxy struct {
	config    *config.Config
	startedAt time.Time

	// HTTP server and upstream clients.
	server      *http.Server
	httpClients map[types.Provider]*http.Client
	listenerMu  sync.RWMutex
	listener    net.Listener

	// Compression layers.
	layer1          *compression.DeterministicCompressor
	responseCache   *caching.ResponseCache
	fileWatcher     *caching.FileWatcher
	secretsDetector *security.Detector

	// compactSignals reads PreCompact/PostCompact markers written by
	// the Codex hooks. When a recent PreCompact marker exists for the
	// active session, the proxy escalates Layer-1 aggression on the
	// next request (smaller sliding window, lower thresholds). See
	// internal/compactsignal/store.go.
	compactSignals *compactsignal.Store

	// promptCacheStability tracks per-session prefix-hash stability
	// across turns. When the same prefix hash repeats, the proxy
	// pushes Anthropic cache breakpoints to the latest stable
	// position, maximising the cached-token volume eligible for the
	// 90% prompt-cache discount on the next request. See
	// internal/promptcache/stability.go.
	promptCacheStability *promptcache.Tracker

	// Analytics.
	analytics     *analytics.Analytics
	persister     *analytics.Persister
	sessionLogger *sessions.SessionLogger

	// Async pipelines.
	analyticsQueue chan types.AnalyticsEvent
	workerCtx      context.Context
	workerCancel   context.CancelFunc
	shutdownCh     chan struct{}
	shutdownOnce   sync.Once
	wg             sync.WaitGroup

	// Analytics queue telemetry (T42). Counters are updated via trySendAnalytics
	// so every non-blocking send site is instrumented uniformly. Tests pause the
	// rate-limited warn via analyticsWarnClock.
	analyticsEnqueued           atomic.Int64
	analyticsDropped            atomic.Int64
	analyticsProofDropped       atomic.Int64
	analyticsLowPriorityDropped atomic.Int64
	analyticsLastWarn           atomic.Int64 // unix-nano of last drop warn, for 1/min rate limit

	// Pipeline phase histograms (T58). Per-phase p50/p95/avg/max on a
	// 200-sample rolling window so TUI + /admin/status can surface
	// which layer is responsible for latency.
	pipelineHist *analytics.PipelineHistograms

	// Runtime toggle atomics. Index 0=Anthropic, 1=OpenAI, 2=CodexChatGPT.
	// Layer index 1 is reserved for compatibility and left disabled.
	providerEnabled [3]atomic.Bool
	layerEnabled    [3]atomic.Bool

	// Bypass (T67) short-circuits every isLayerEnabled / isProviderEnabled
	// check when set, producing a transparent passthrough relay. Hot-
	// reloadable via admin POST /admin/bypass and the `B` TUI hotkey;
	// persisted alongside other toggles.
	bypassMode atomic.Bool
	// hostBudgetExceeded is updated by /admin/state snapshots and gates
	// optional Codex reducers without re-measuring RSS/state size in the
	// frame hot path.
	hostBudgetExceeded atomic.Bool
	// codexLayer0LatencyExceeded is set after repeated Layer-0 reducer
	// latency budget breaches and clears after enough cheap frames. It gates
	// optional Codex reducers before local overhead can become UX-visible.
	codexLayer0LatencyExceeded atomic.Bool
	codexLayer0LatencyStrikes  atomic.Int64
	// codexLayer0LatencyLastBreach holds the unixnano of the last breach (or
	// rate-limited dead-zone decay) so strikes can decay when breaches stop.
	codexLayer0LatencyLastBreach atomic.Int64
	resourceWindowMu             sync.Mutex
	lastResourceSample           hostmetrics.ProcessSnapshot
	lastResourceSampleAt         time.Time
	codexFootprintMu             sync.Mutex
	codexFootprintByFamily       map[string]codexFootprintEstimate
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
	outputReduce *outputreduce.Tracker
	// codexChunkDedup holds T255 content-defined chunk identities for
	// proof-gated Codex tool-output/file-read partial-overlap dedup.
	codexChunkDedup *chunkdedup.Store
	// outputReduceCounters tracks T185 cumulative counters for the
	// T165/T166/T167 mechanisms. Atomic, no lock, snapshot-on-read.
	outputReduceCounters OutputReduceCounters
	// qualityAB hosts T186 cohort routing and outcome tracking for
	// T169 be-terse hint and future gated levers. nil when not
	// constructed.
	qualityAB                *qualityab.Harness
	outputReduceRepairMu     sync.Mutex
	outputReduceRepair       map[string]pendingOutputReduceSignal
	archiveRecoveryNoteMu    sync.Mutex
	archiveRecoveryNote      map[string]struct{}
	openAIPromptCacheMu      sync.Mutex
	openAIPromptCacheRate    map[string]promptCacheRateBucket
	openAIPromptCacheRejects map[string]time.Time
	openAIPromptCacheNet     map[string]promptCacheNetBucket
	webSocketTunnel          *WebSocketTunnel
	webSocketShapes          *wscompact.ShapeRegistry

	// Debug decision recorder - records per-request Layer 1 summaries for "slimference debug last".
	debugRecorder *dbg.Recorder

	// Health monitor - tracks per-provider upstream health from actual request results (spec §17.5).
	healthMon *healthMonitor

	// TUI send function - set after TUI program is created.
	// Protected by tuiSendMu so race detector is satisfied even though in practice
	// SetTUISendFn is called before Start() and the goroutines launch.
	tuiSendMu sync.RWMutex
	tuiSendFn func(types.RequestMetrics)

	// appsManagerPtr holds the per-app policy manager (T193). Lock-
	// free atomic pointer so SIGHUP-reload doesn't block traffic. nil
	// until SetAppsManager wires it; routing then falls back to the
	// implicit "all apps enabled" policy.
	appsManagerPtr atomic.Pointer[apps.Manager]

	// wssDispatcherPtr points at the active WSS dispatcher used by the
	// scoped Codex route and, when enabled, transparent SNI mode.
	// /admin/state reads it for WSS bridge and mutation telemetry.
	wssDispatcherPtr atomic.Pointer[PhaseFDispatcher]

	// adminState holds the probe set used by the /admin/state
	// endpoint. Wired by cmd/slimference at startup; nil before
	// then so the handler responds 503.
	adminState adminStateProvider
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
		config:                 cfg,
		startedAt:              time.Now(),
		httpClients:            make(map[types.Provider]*http.Client),
		analyticsQueue:         make(chan types.AnalyticsEvent, 256),
		workerCtx:              workerCtx,
		workerCancel:           workerCancel,
		shutdownCh:             make(chan struct{}),
		pipelineHist:           analytics.NewPipelineHistograms(),
		qualityReRead:          quality.NewReReadDetector(10),
		qualityCacheSpike:      quality.NewCacheMissSpikeDetector(50, 0.25),
		qualityNetSavings:      quality.NewNetSavings(),
		toolPrune:              toolprune.NewUsageTracker(cfg.Compression.Tuning.ToolPruneIdleThresholdTurns),
		serverState:            sessions.NewResponseStateStore(1024),
		outputReduceRepair:     make(map[string]pendingOutputReduceSignal),
		archiveRecoveryNote:    make(map[string]struct{}),
		codexFootprintByFamily: make(map[string]codexFootprintEstimate),
		qualityAB:              qualityab.New(qualityab.Options{}),
		outputReduce: outputreduce.NewTrackerWithAutoTune(cfg.Compression.OutputReduce.Enabled, cfg.Compression.OutputReduce.Profile, outputreduce.AutoTuneConfig{
			Enabled:             cfg.Compression.OutputReduce.AutoTuneEnabled,
			MinSamples:          cfg.Compression.OutputReduce.AutoTuneMinSamples,
			MinNetSavingsPct:    cfg.Compression.OutputReduce.MinNetSavingsPct,
			MaxFailureRateDelta: cfg.Compression.OutputReduce.MaxFailureRateDelta,
			CooldownTurns:       cfg.Compression.OutputReduce.CooldownTurns,
		}),
	}

	// PreCompact/PostCompact signal store. Rooted at the user's home
	// so the hook subprocesses and the proxy share the same path
	// without coordination. Failure to resolve home is silent: the
	// proxy continues with the signal store disabled (HasRecentSignal
	// will return false on every probe).
	if home, err := proxyUserHomeDir(); err == nil && home != "" {
		p.compactSignals = compactsignal.DefaultStore(home)
	}

	if home, err := proxyUserHomeDir(); err == nil && home != "" {
		archiveDir := contentarchive.DefaultDir(home)
		p.codexChunkDedup = chunkdedup.NewStoreWithLimits(chunkdedup.Config{}, chunkdedup.StoreLimits{
			MaxSessions:         cfg.Compression.OutputReduce.CodexChunkDedupMaxSessions,
			MaxChunksPerSession: cfg.Compression.OutputReduce.CodexChunkDedupMaxChunksPerSession,
			TTL:                 time.Duration(cfg.Compression.OutputReduce.CodexChunkDedupTTLSeconds) * time.Second,
			MaxSessionRefPct:    cfg.Compression.OutputReduce.CodexChunkDedupMaxSessionReferencePercent,
		}, func(sessionID, chunkID string, chunk []byte) string {
			entry, err := contentarchive.Put(archiveDir, contentarchive.Input{
				SessionID: sessionID,
				SubLayer:  "chunkdedup:" + shortChunkID(chunkID),
				Original:  string(chunk),
			}, contentarchive.Limits{})
			if err != nil || entry == nil {
				return ""
			}
			return entry.URI
		})
	} else {
		p.codexChunkDedup = chunkdedup.NewStore(chunkdedup.Config{}, nil)
	}

	// Prefix-stability tracker for the Anthropic prompt-cache
	// breakpoint optimiser (L2). Defaults: 1024 sessions in LRU,
	// 30 min TTL.
	p.promptCacheStability = promptcache.NewTracker(0, 0)
	p.loadCodexLayer0LatencyBudgetState()

	// Default all toggles to enabled.
	p.providerEnabled[types.Anthropic].Store(true)
	p.providerEnabled[types.OpenAI].Store(true)
	p.providerEnabled[types.CodexChatGPT].Store(true)
	p.layerEnabled[0].Store(cfg.Compression.Layer1Enabled)
	p.layerEnabled[1].Store(cfg.Compression.Layer2Enabled)
	p.layerEnabled[2].Store(cfg.Compression.OutputReduce.Enabled)

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
	p.webSocketShapes = wscompact.NewShapeRegistry()
	scopedWSSDispatcher := &PhaseFDispatcher{Proxy: p}
	p.webSocketTunnel = &WebSocketTunnel{
		Dialer:      newProfiledWebSocketDialer(tlsResolver),
		Logger:      slog.Default(),
		BypassPaths: cfg.Transparent.AudioBypassPaths,
		Inspector:   p.webSocketShapes,
		FrameBridge: func(ctx context.Context, client, upstream net.Conn, opts WebSocketBridgeOptions) error {
			p.SetWSSDispatcher(scopedWSSDispatcher)
			scopedWSSDispatcher.counters.mitmBridged.Add(1)
			scopedWSSDispatcher.counters.phasefBridged.Add(1)
			return scopedWSSDispatcher.runWSMITM(ctx, client, upstream, opts)
		},
		ByteBridge: func(ctx context.Context, client, upstream net.Conn, opts WebSocketBridgeOptions) error {
			p.SetWSSDispatcher(scopedWSSDispatcher)
			scopedWSSDispatcher.counters.mitmBridged.Add(1)
			return scopedWSSDispatcher.runWSBridge(ctx, client, upstream, opts)
		},
	}

	// Layer 1: Deterministic compressor.
	p.layer1 = compression.NewDeterministicCompressor(&cfg.Compression)

	// T76: wire the content-archive recorder so archive-required Layer 1
	// sub-layers can prove recovery before mutation. If home or archive
	// writes are unavailable, those sub-layers full-pass rather than
	// shipping unrecoverable summaries.
	if home, err := proxyUserHomeDir(); err == nil {
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

	// Layer 2: Response cache.
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
	mux.HandleFunc("/admin/health", p.healthHandler)
	mux.HandleFunc(AdminHealthPath, p.healthHandler)
	mux.HandleFunc(AdminStatusPath, p.adminStatusHandler)
	mux.HandleFunc(AdminProviderPath, p.adminProviderHandler)
	mux.HandleFunc(AdminLayerPath, p.adminLayerHandler)
	mux.HandleFunc(AdminSecuritySuspendPath, p.adminSecuritySuspendHandler)
	mux.HandleFunc(AdminBypassPath, p.adminBypassHandler)
	mux.HandleFunc(AdminFlushPath, p.adminFlushHandler)
	mux.HandleFunc(AdminStatePath, p.adminStateHandler)
	mux.HandleFunc(AdminAppsPath, p.adminAppsHandler)
	mux.HandleFunc("/", p.ServeHTTP)

	var handler http.Handler = mux
	if cfg.Transparent.Enabled || cfg.Transparent.ScopedDesktopProxy {
		var (
			signer *tlsca.Signer
			err    error
		)
		if cfg.Transparent.Enabled {
			signer, err = newTransparentSigner(cfg)
		} else {
			signer, err = newExistingTransparentSigner(cfg)
		}
		if err != nil {
			if cfg.Transparent.Enabled {
				slog.Error("transparent proxy disabled: CA init failed", "error", err)
			} else {
				slog.Info("scoped desktop proxy inactive: CA not installed", "error", err)
			}
		} else {
			hosts := cfg.Transparent.InterceptHosts
			if !cfg.Transparent.Enabled {
				hosts = []string{"chatgpt.com"}
			}
			connect := NewConnectInterceptor(signer, mux, hosts)
			connect.SetLogger(slog.Default())
			connect.SetDebugRecorder(p.debugRecorder)
			connect.SetWebSocketTunnel(p.webSocketTunnel)
			connect.SetWebSocketPhaseFDecider(shouldBridgeCodexConversationWSS)
			handler = connect
			slog.Info("connect proxy enabled", "transparent", cfg.Transparent.Enabled, "scoped_desktop_proxy", cfg.Transparent.ScopedDesktopProxy, "intercept_hosts", hosts)
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

func (p *Proxy) daemonResourceSnapshot() hostmetrics.ProcessSnapshot {
	snap := hostmetrics.CurrentProcess(os.Getpid())
	if snap.CPUKnown && !p.startedAt.IsZero() {
		uptime := time.Since(p.startedAt).Seconds()
		if uptime > 0 {
			snap.CPUPercent = (snap.CPUUserSeconds + snap.CPUSystemSeconds) / uptime * 100
		}
	}
	if p != nil {
		p.annotateResourceWindow(&snap)
	}
	return snap
}

func (p *Proxy) annotateResourceWindow(snap *hostmetrics.ProcessSnapshot) {
	if p == nil || snap == nil {
		return
	}
	now := time.Now()
	p.resourceWindowMu.Lock()
	prev := p.lastResourceSample
	prevAt := p.lastResourceSampleAt
	p.lastResourceSample = *snap
	p.lastResourceSampleAt = now
	p.resourceWindowMu.Unlock()

	if prevAt.IsZero() {
		return
	}
	elapsed := now.Sub(prevAt).Seconds()
	if elapsed <= 0 {
		return
	}
	if snap.CPUKnown && prev.CPUKnown {
		delta := (snap.CPUUserSeconds + snap.CPUSystemSeconds) - (prev.CPUUserSeconds + prev.CPUSystemSeconds)
		if delta >= 0 {
			snap.CPUWindowSeconds = elapsed
			snap.CPUWindowPercent = delta / elapsed * 100
			snap.CPUWindowKnown = true
		}
	}
	if snap.DiskIOKnown && prev.DiskIOKnown {
		readDelta := snap.DiskReadOps - prev.DiskReadOps
		writeDelta := snap.DiskWriteOps - prev.DiskWriteOps
		if readDelta >= 0 && writeDelta >= 0 {
			snap.DiskReadOpsDelta = readDelta
			snap.DiskWriteOpsDelta = writeDelta
			snap.DiskWindowKnown = true
		}
	}
}

func (p *Proxy) codexHostBudgetExceeded() bool {
	if p == nil {
		return false
	}
	return p.hostBudgetExceeded.Load()
}

func (p *Proxy) codexRuntimeBudgetExceeded() bool {
	if p == nil {
		return false
	}
	return p.hostBudgetExceeded.Load() || p.codexLayer0LatencyExceeded.Load()
}

func (p *Proxy) observeCodexFootprintSessionLength(family string, turns int64) {
	if p == nil || turns < 1 {
		return
	}
	family = codexFootprintFamily(family)
	p.codexFootprintMu.Lock()
	defer p.codexFootprintMu.Unlock()
	if p.codexFootprintByFamily == nil {
		p.codexFootprintByFamily = make(map[string]codexFootprintEstimate)
	}
	row := p.codexFootprintByFamily[family]
	if row.samples == 0 {
		row.estimate = float64(turns)
	} else {
		row.estimate = codexFootprintEMAAlpha*float64(turns) + (1-codexFootprintEMAAlpha)*row.estimate
	}
	row.samples++
	p.codexFootprintByFamily[family] = row
}

func (p *Proxy) codexFootprintRemainingTurns(family string, turnSeq int) int {
	if p == nil || turnSeq < 1 {
		return 0
	}
	p.codexFootprintMu.Lock()
	defer p.codexFootprintMu.Unlock()
	row, ok := p.codexFootprintByFamily[codexFootprintFamily(family)]
	if !ok || row.samples == 0 {
		return 0
	}
	estimatedLength := int(row.estimate + 0.5)
	remaining := estimatedLength - turnSeq
	if remaining < 1 {
		return 1
	}
	return remaining
}

func codexFootprintFamily(family string) string {
	family = strings.ToLower(strings.TrimSpace(family))
	if family == "" {
		return "unknown"
	}
	return family
}

func (p *Proxy) recordCodexLayer0Stats(stats proxyLayer0Stats) {
	if p == nil {
		return
	}
	p.outputReduceCounters.RecordProxyLayer0Stats(stats)
	p.observeCodexLayer0LatencyBudget(stats)
}

func (p *Proxy) observeCodexLayer0LatencyBudget(stats proxyLayer0Stats) {
	if p == nil || stats.TotalLatencyNs <= 0 {
		return
	}
	latency := time.Duration(stats.TotalLatencyNs)
	budget := codexLayer0LatencyBudget
	// Scale the budget with the payload the pass actually had to process
	// (token counting and filters are O(bytes)); a flat budget turns
	// legitimate work on large tool outputs into false overhead strikes.
	// Normal small frames keep the strict base budget.
	if stats.ToolResultBytes > 0 {
		budget += time.Duration(stats.ToolResultBytes/2048) * time.Millisecond
		if budget > codexLayer0LatencyProductiveBudget {
			budget = codexLayer0LatencyProductiveBudget
		}
	}
	if stats.TokensSaved > 0 {
		budget = codexLayer0LatencyProductiveBudget
	}
	if latency > budget {
		p.codexLayer0LatencyLastBreach.Store(time.Now().UnixNano())
		strikes := p.codexLayer0LatencyStrikes.Add(1)
		if strikes > codexLayer0LatencyStrikeLimit {
			strikes = codexLayer0LatencyStrikeLimit
			p.codexLayer0LatencyStrikes.Store(strikes)
		}
		if strikes >= codexLayer0LatencyStrikeLimit {
			p.codexLayer0LatencyExceeded.Store(true)
		}
		p.saveCodexLayer0LatencyBudgetState()
		return
	}
	if latency > codexLayer0LatencyRecoveryBudget {
		lastBreach := p.codexLayer0LatencyLastBreach.Load()
		if p.codexLayer0LatencyStrikes.Load() <= 0 ||
			(lastBreach > 0 && time.Since(time.Unix(0, lastBreach)) < codexLayer0LatencyDecayAfter) {
			return
		}
		// Rate-limit dead-zone decay to one strike per decay window.
		p.codexLayer0LatencyLastBreach.Store(time.Now().UnixNano())
	}
	for {
		strikes := p.codexLayer0LatencyStrikes.Load()
		if strikes <= 0 {
			p.codexLayer0LatencyExceeded.Store(false)
			return
		}
		if p.codexLayer0LatencyStrikes.CompareAndSwap(strikes, strikes-1) {
			if strikes == 1 {
				p.codexLayer0LatencyExceeded.Store(false)
			}
			p.saveCodexLayer0LatencyBudgetState()
			return
		}
	}
}

type codexLayer0LatencyBudgetState struct {
	Version   int       `json:"version"`
	Strikes   int64     `json:"strikes"`
	Exceeded  bool      `json:"exceeded"`
	UpdatedAt time.Time `json:"updated_at"`
}

func codexLayer0LatencyBudgetStatePath(home string) string {
	return filepath.Join(home, ".slimference", "runtime-budget", "codex-layer0-latency.json")
}

func (p *Proxy) loadCodexLayer0LatencyBudgetState() {
	if p == nil {
		return
	}
	home, err := proxyUserHomeDir()
	if err != nil || home == "" {
		return
	}
	data, err := os.ReadFile(codexLayer0LatencyBudgetStatePath(home))
	if err != nil {
		return
	}
	var state codexLayer0LatencyBudgetState
	if err := json.Unmarshal(data, &state); err != nil {
		return
	}
	// Version 1 strike debt was accumulated by binaries whose demoted passes
	// could not recover (pre-unlatch) and whose budget did not scale with
	// payload size; loading it would re-latch a healed gate. Only trust
	// state written by the current accounting.
	if state.Version < 2 {
		return
	}
	if state.UpdatedAt.IsZero() || time.Since(state.UpdatedAt) > codexLayer0LatencyStateTTL {
		return
	}
	if state.Strikes < 0 {
		state.Strikes = 0
	}
	if state.Strikes > codexLayer0LatencyStrikeLimit {
		state.Strikes = codexLayer0LatencyStrikeLimit
	}
	p.codexLayer0LatencyStrikes.Store(state.Strikes)
	p.codexLayer0LatencyExceeded.Store(state.Exceeded && state.Strikes >= codexLayer0LatencyStrikeLimit)
}

func (p *Proxy) saveCodexLayer0LatencyBudgetState() {
	if p == nil {
		return
	}
	home, err := proxyUserHomeDir()
	if err != nil || home == "" {
		return
	}
	path := codexLayer0LatencyBudgetStatePath(home)
	state := codexLayer0LatencyBudgetState{
		Version:   2,
		Strikes:   p.codexLayer0LatencyStrikes.Load(),
		Exceeded:  p.codexLayer0LatencyExceeded.Load(),
		UpdatedAt: time.Now(),
	}
	if state.Strikes < 0 {
		state.Strikes = 0
	}
	if state.Strikes > codexLayer0LatencyStrikeLimit {
		state.Strikes = codexLayer0LatencyStrikeLimit
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func (p *Proxy) uptimeSeconds() int64 {
	if p == nil || p.startedAt.IsZero() {
		return 0
	}
	return int64(time.Since(p.startedAt).Seconds())
}

func (p *Proxy) daemonStateBytes() (int64, bool) {
	home, err := proxyUserHomeDir()
	if err != nil || home == "" {
		return 0, false
	}
	return daemonStateBytesForHome(home, 20_000)
}

func daemonStateBytesForHome(home string, maxEntriesPerDir int) (int64, bool) {
	dirs := []string{
		contentarchive.DefaultDir(home),
		readcache.DefaultDir(home),
		toolusecache.DefaultDir(home),
		toolusecache.CollapsedKeysDir(home),
	}
	var total int64
	known := false
	for _, dir := range dirs {
		size, ok, complete := hostmetrics.DirectorySizeBytesBounded(dir, maxEntriesPerDir)
		if ok {
			total += size
			known = true
		}
		if ok && !complete {
			total += control.DefaultHostStateBudgetBytes + 1
		}
	}
	return total, known
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
	transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		return tlsdial.Dial(ctx, network, host, port, resolver.Resolve(host))
	}
	return transport
}

func newProfiledWebSocketDialer(resolver tlsdial.Resolver) WebSocketDialer {
	return func(host, port string) (net.Conn, error) {
		return tlsdial.Dial(context.Background(), "tcp", host, port, resolver.Resolve(host))
	}
}

func newTransparentSigner(cfg *config.Config) (*tlsca.Signer, error) {
	caDir, err := transparentCADir(cfg)
	if err != nil {
		return nil, err
	}
	ca, err := tlsca.LoadOrGenerateCA(caDir)
	if err != nil {
		return nil, err
	}
	return tlsca.NewSigner(ca, cfg.Transparent.CertCacheSize), nil
}

func newExistingTransparentSigner(cfg *config.Config) (*tlsca.Signer, error) {
	caDir, err := transparentCADir(cfg)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(caDir, "ca", "root.key")); err != nil {
		return nil, fmt.Errorf("transparent CA key missing: %w", err)
	}
	if _, err := os.Stat(filepath.Join(caDir, "ca", "root.crt")); err != nil {
		return nil, fmt.Errorf("transparent CA cert missing: %w", err)
	}
	return newTransparentSigner(cfg)
}

func transparentCADir(cfg *config.Config) (string, error) {
	caDir := strings.TrimSpace(cfg.Transparent.CADir)
	if caDir != "" {
		return config.ExpandHomePath(caDir), nil
	}
	home, err := proxyUserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home for transparent CA: %w", err)
	}
	return filepath.Join(home, ".slimference"), nil
}

func shouldBridgeCodexConversationWSS(host string, r *http.Request) bool {
	if !strings.EqualFold(host, "chatgpt.com") {
		return false
	}
	if r == nil || strings.TrimRight(r.URL.Path, "/") != "/backend-api/codex/responses" {
		return false
	}
	protocols := r.Header.Values("Sec-WebSocket-Protocol")
	if len(protocols) == 0 {
		return true
	}
	for _, header := range protocols {
		for _, part := range strings.Split(header, ",") {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(part)), "responses_websockets") {
				return true
			}
		}
	}
	return false
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
		return
	default:
	}

	priority := analyticsEventPriorityOf(ev)
	if priority == analyticsEventPriorityProof {
		p.tryDrainOneAnalyticsEvent()
		select {
		case p.analyticsQueue <- ev:
			p.analyticsEnqueued.Add(1)
			return
		default:
		}
	}
	p.noteAnalyticsDrop(ev, priority)
}

// AnalyticsQueueStats returns a snapshot of analytics queue telemetry.
// Safe for concurrent use; values are consistent at read time.
type AnalyticsQueueStats struct {
	Capacity                int   `json:"capacity"`
	Depth                   int   `json:"depth"`
	EnqueuedTotal           int64 `json:"enqueued_total"`
	DroppedTotal            int64 `json:"dropped_total"`
	ProofDroppedTotal       int64 `json:"proof_dropped_total"`
	LowPriorityDroppedTotal int64 `json:"low_priority_dropped_total"`
}

// AnalyticsQueueStats reports current analytics queue telemetry.
func (p *Proxy) AnalyticsQueueStats() AnalyticsQueueStats {
	return AnalyticsQueueStats{
		Capacity:                cap(p.analyticsQueue),
		Depth:                   len(p.analyticsQueue),
		EnqueuedTotal:           p.analyticsEnqueued.Load(),
		DroppedTotal:            p.analyticsDropped.Load(),
		ProofDroppedTotal:       p.analyticsProofDropped.Load(),
		LowPriorityDroppedTotal: p.analyticsLowPriorityDropped.Load(),
	}
}

type analyticsEventPriority int

const (
	analyticsEventPriorityLow analyticsEventPriority = iota
	analyticsEventPriorityProof
)

func analyticsEventPriorityOf(ev types.AnalyticsEvent) analyticsEventPriority {
	switch ev.Type {
	case types.EventRequestProcessed,
		types.EventSecretDetected,
		types.EventErrorOccurred,
		types.EventRateLimitRetry,
		types.EventOverflowRetry:
		return analyticsEventPriorityProof
	default:
		return analyticsEventPriorityLow
	}
}

func (p *Proxy) tryDrainOneAnalyticsEvent() {
	select {
	case event := <-p.analyticsQueue:
		p.processAnalyticsEvent(event)
	default:
	}
}

func (p *Proxy) noteAnalyticsDrop(ev types.AnalyticsEvent, priority analyticsEventPriority) {
	p.analyticsDropped.Add(1)
	if priority == analyticsEventPriorityProof {
		p.analyticsProofDropped.Add(1)
	} else {
		p.analyticsLowPriorityDropped.Add(1)
	}
	now := time.Now().UnixNano()
	last := p.analyticsLastWarn.Load()
	if now-last >= analyticsWarnIntervalNs &&
		p.analyticsLastWarn.CompareAndSwap(last, now) {
		slog.Warn("analytics_queue_full",
			"event", "analytics_drop",
			"event_type", int(ev.Type),
			"proof_priority", priority == analyticsEventPriorityProof,
			"dropped_total", p.analyticsDropped.Load(),
			"proof_dropped_total", p.analyticsProofDropped.Load(),
			"low_priority_dropped_total", p.analyticsLowPriorityDropped.Load(),
			"capacity", cap(p.analyticsQueue),
			"depth", len(p.analyticsQueue),
		)
	}
}

// Start binds the listener and begins serving. It is non-blocking; call from a goroutine.
func (p *Proxy) Start() error {
	addr := p.config.ListenAddr()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	serveLn := p.wrapRawScopedWSSListener(ln)
	p.listenerMu.Lock()
	p.listener = serveLn
	p.listenerMu.Unlock()

	slog.Info("proxy listening", "addr", addr)

	p.wg.Add(1)
	go p.analyticsWorker()
	p.wg.Add(1)
	go p.cacheJanitor(cacheJanitorInterval)
	p.wg.Add(1)
	go p.analyticsPeriodicFlush(analyticsFlushInterval)

	return p.server.Serve(serveLn)
}

func (p *Proxy) wrapRawScopedWSSListener(ln net.Listener) net.Listener {
	if p == nil || p.webSocketTunnel == nil {
		return ln
	}
	host, ok := p.upstreamHost(types.CodexChatGPT)
	if !ok {
		return ln
	}
	return &rawScopedWSSListener{
		Listener:     ln,
		Tunnel:       p.webSocketTunnel,
		UpstreamHost: host,
		OnIntercept:  p.recordRawScopedWSS,
	}
}

func (p *Proxy) recordRawScopedWSS(path string, header []byte) {
	if p == nil || p.debugRecorder == nil {
		return
	}
	p.debugRecorder.Record(dbg.RequestSummary{
		RequestID: newRequestIDFn(),
		Timestamp: time.Now(),
		Source:    "proxy",
		Provider:  types.CodexChatGPT.String(),
		Host:      "raw-scoped",
		Path:      path,
		RouteMode: rawScopedWSSRouteMode(path),
		Plan: p.dryRunPlan(plannerInput{
			provider:                   types.CodexChatGPT,
			routeMode:                  rawScopedWSSRouteMode(path),
			contentClasses:             []string{"websocket"},
			webSocketShapeKnown:        p.webSocketShapeKnown(),
			webSocketMutationRequested: p.webSocketTunnel != nil && p.webSocketTunnel.FrameBridge != nil && !isCodexBridgePath(path),
			liveCorpusConfidence:       p.plannerLiveCorpusConfidence(),
		}),
	})
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
		p.handlePassthroughWithAttribution(w, r, provider, body, body, "non_compressible_path")
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
		p.handlePassthroughWithAttribution(w, r, provider, body, body, "provider_path_passthrough")
		return
	}

	// If this provider is toggled off: passthrough without compression.
	if !p.isProviderEnabled(provider) {
		p.handlePassthroughWithAttribution(w, r, provider, body, body, "provider_disabled")
		return
	}

	// T81 follow-up: per-route bypass. The path-equality check is
	// cheap enough to run before message extraction so we save the
	// compression CPU on routes the operator has explicitly carved
	// out (e.g. for staged rollouts).
	if p.IsRouteBypassed(r.URL.Path) {
		p.handlePassthroughWithAttribution(w, r, provider, body, body, "route_bypass")
		return
	}

	// T81 follow-up: per-tool bypass. Route through passthrough when
	// the request's last user turn carries a tool_result whose tool
	// is in the bypass set. Cheap byte-scan that avoids parsing the
	// full body when the set is empty.
	if p.hasBypassedTool(body) {
		p.handlePassthroughWithAttribution(w, r, provider, body, body, "tool_bypass")
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
	if provider == types.CodexChatGPT && p.config.Proxy.DirectCodexWebSocketPolicy == "force_https_fallback" {
		if p.debugRecorder != nil {
			p.debugRecorder.Record(dbg.RequestSummary{
				RequestID: newRequestIDFn(),
				Timestamp: time.Now(),
				Source:    "proxy",
				Provider:  provider.String(),
				Host:      r.Host,
				Path:      r.URL.Path,
				RouteMode: "websocket_force_https_fallback",
				Plan: p.dryRunPlan(plannerInput{
					provider:                   provider,
					routeMode:                  "websocket_force_https_fallback",
					contentClasses:             []string{"websocket"},
					webSocketShapeKnown:        p.webSocketShapeKnown(),
					webSocketMutationRequested: false,
					liveCorpusConfidence:       p.plannerLiveCorpusConfidence(),
				}),
			})
		}
		http.Error(w, "codex websocket disabled by Slimference direct CLI policy", http.StatusServiceUnavailable)
		return
	}
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
	routeMode := "websocket_tunnel"
	mutationRequested := p.webSocketTunnel.FrameBridge != nil && !p.webSocketTunnel.IsAudioBypassPath(r.URL.Path)
	if isCodexBridgePath(r.URL.Path) {
		routeMode = "websocket_bridge"
		mutationRequested = false
	}
	if mutationRequested {
		routeMode = "websocket_phasef"
	}
	if p.debugRecorder != nil {
		p.debugRecorder.Record(dbg.RequestSummary{
			RequestID: newRequestIDFn(),
			Timestamp: time.Now(),
			Source:    "proxy",
			Provider:  provider.String(),
			Host:      r.Host,
			Path:      r.URL.Path,
			RouteMode: routeMode,
			Plan: p.dryRunPlan(plannerInput{
				provider:                   provider,
				routeMode:                  routeMode,
				contentClasses:             []string{"websocket"},
				webSocketShapeKnown:        p.webSocketShapeKnown(),
				webSocketMutationRequested: mutationRequested,
				liveCorpusConfidence:       p.plannerLiveCorpusConfidence(),
			}),
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
// Specific allowlist: only paths that we know carry the chat/responses message
// shape are matched. Adjacent Codex Desktop App endpoints (realtime/voice
// calls, image generation, model listings, memories, computer-use plugin
// installs, etc.) all share the `/backend-api/codex/` prefix but DO NOT
// carry the messages shape - they pass through byte-equal.
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
	// OpenAI Responses API (and Codex via openai_base_url override).
	if clean == "/v1/responses" {
		return true
	}
	// Codex ChatGPT subscription wire. Only the responses endpoint
	// carries the conversation message shape. Realtime / voice
	// (/backend-api/codex/realtime/*), models listing
	// (/backend-api/codex/models), memories
	// (/backend-api/codex/memories/*), and plugin / image / computer-
	// use sidebands all share the same prefix but are NOT message
	// traffic - tightening here keeps voice + image + computer-use
	// + plugin flows untouched.
	return clean == "/backend-api/codex/responses"
}

func isProviderCompressiblePath(provider types.Provider, path string) bool {
	clean := strings.TrimSuffix(path, "/")
	switch provider {
	case types.Anthropic:
		return clean == "/v1/messages"
	case types.OpenAI:
		return clean == "/v1/chat/completions"
	case types.CodexChatGPT:
		return clean == "/v1/responses" || clean == "/backend-api/codex/responses"
	default:
		return false
	}
}

// upstreamURL constructs the full upstream URL for the given provider.
// T66: CodexChatGPT routes to chatgpt.com (or chatgpt_base_url override) so
// Codex's /backend-api/codex/* paths hit the real ChatGPT backend rather
// than api.openai.com (which would 404 the Codex-specific routes).
func (p *Proxy) upstreamURL(provider types.Provider, path, rawQuery string) string {
	path = canonicalCodexBridgePath(path)
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
	layer = normalizeRuntimeLayer(layer)
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

// ObserveQualityToolKey is the proxy-side hook for the re-read detector.
// Called when a tool_use block is observed during request processing. Empty
// session or key are no-ops. Returns true when the detector reports a re-read.
func (p *Proxy) ObserveQualityToolKey(sessionID, toolKey string) bool {
	return p.ObserveQualityToolKeyForTurn(sessionID, "", toolKey)
}

func (p *Proxy) ObserveQualityToolKeyForTurn(sessionID, turnID, toolKey string) bool {
	sessionID = sessions.SafeOptionalSessionID(sessionID)
	turnID = sessions.SafeOptionalTurnID(turnID)
	toolKey = strings.TrimSpace(toolKey)
	if sessionID == "" || toolKey == "" || p.qualityReRead == nil {
		return false
	}
	return p.qualityReRead.ObserveTurn(sessionID, turnID, toolKey)
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
	layer = normalizeRuntimeLayer(layer)
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
	layer = normalizeRuntimeLayer(layer)
	return p.isLayerEnabled(layer)
}

func normalizeRuntimeLayer(layer int) int {
	return layer
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

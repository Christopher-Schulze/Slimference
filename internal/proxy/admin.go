package proxy

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/caching"
	"github.com/slimference/slimference/internal/checkpoints"
	"github.com/slimference/slimference/internal/compression"
	"github.com/slimference/slimference/internal/contentarchive"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/filter"
	"github.com/slimference/slimference/internal/outputreduce"
	"github.com/slimference/slimference/internal/quality"
	"github.com/slimference/slimference/internal/qualityab"
	"github.com/slimference/slimference/internal/readcache"
	"github.com/slimference/slimference/internal/repetition"
	"github.com/slimference/slimference/internal/toolarchive"
	"github.com/slimference/slimference/internal/types"
)

func adminCacheAgeFrom(h caching.AgeHistogram) AdminCacheAgeStatus {
	return AdminCacheAgeStatus{
		Count: h.Count,
		P50Ms: h.P50Ms,
		P95Ms: h.P95Ms,
		P99Ms: h.P99Ms,
		MaxMs: h.MaxMs,
	}
}

const (
	AdminBasePath            = "/_slimference/admin"
	AdminHealthPath          = AdminBasePath + "/health"
	AdminStatusPath          = AdminBasePath + "/status"
	AdminProviderPath        = AdminBasePath + "/provider"
	AdminLayerPath           = AdminBasePath + "/layer"
	AdminFlushPath           = AdminBasePath + "/flush"
	AdminSecuritySuspendPath = AdminBasePath + "/security/suspend"
	AdminBypassPath          = AdminBasePath + "/bypass"
	// AdminStatePath (T199) returns the aggregated control.SetupState
	// snapshot used by the TUI install dashboard and external auditors.
	AdminStatePath = AdminBasePath + "/state"
	// AdminAppsPath (T199) accepts POST {id, enabled} to toggle the
	// per-app routing policy. GET returns the current Policy.
	AdminAppsPath = AdminBasePath + "/apps"
)

// AdminBypassRequest is the POST body for toggling the master bypass
// (T67) and its T81 scoped extensions.
type AdminBypassRequest struct {
	Enabled bool `json:"enabled"`
	// DurationSeconds (T81): when > 0 and Enabled=true, bypass auto-
	// reverts after this many seconds.
	DurationSeconds int `json:"duration_seconds,omitempty"`
	// NextRequests (T81): when > 0 and Enabled=true, bypass auto-
	// reverts after this many requests have flowed through.
	NextRequests int `json:"next_requests,omitempty"`
}

// AdminBypassResponse echoes the effective bypass state.
type AdminBypassResponse struct {
	Enabled           bool  `json:"enabled"`
	ExpiresAtUnix     int64 `json:"expires_at_unix,omitempty"`
	NextRequestBudget int64 `json:"next_request_budget,omitempty"`
}

// AdminSecuritySuspendRequest is the JSON payload for the suspend endpoint
// (T59). SuspendSeconds <= 0 clears the suspension; values > MaxSuspendSeconds
// are clamped server-side via security.Detector.SuspendUntil.
type AdminSecuritySuspendRequest struct {
	SuspendSeconds int `json:"suspend_seconds"`
}

// AdminSecuritySuspendResponse echoes the resulting state so the client can
// confirm the effective deadline (which may differ from the requested one
// due to server-side clamping).
type AdminSecuritySuspendResponse struct {
	Active       bool   `json:"active"`
	UntilUnixSec int64  `json:"until_unix_sec,omitempty"`
	Mode         string `json:"mode"`
}

type AdminReadCacheStatus struct {
	Evaluations     int `json:"evaluations"`
	Allows          int `json:"allows"`
	Blocks          int `json:"blocks"`
	UnchangedBlocks int `json:"unchanged_blocks"`
	DeltaBlocks     int `json:"delta_blocks"`
	Sessions        int `json:"sessions"`
	TrackedFiles    int `json:"tracked_files"`
	// HitRate is Blocks / (Blocks + Allows), or 0 when no decisions
	// have been recorded. Surfaced as a derived field so monitoring
	// tools don't have to recompute it. T57 stretch.
	HitRate float64 `json:"hit_rate"`
}

type AdminCheckpointStatus struct {
	Count       int       `json:"count"`
	Captures    int       `json:"captures"`
	Restores    int       `json:"restores"`
	Bytes       int64     `json:"bytes"`
	LastCapture time.Time `json:"last_capture"`
	LastRestore time.Time `json:"last_restore"`
	LastTrigger string    `json:"last_trigger"`
}

type AdminToolArchiveStatus struct {
	Count        int       `json:"count"`
	Archived     int       `json:"archived"`
	Expanded     int       `json:"expanded"`
	BytesRaw     int64     `json:"bytes_raw"`
	BytesStored  int64     `json:"bytes_stored"`
	LastArchived time.Time `json:"last_archived"`
	LastExpanded time.Time `json:"last_expanded"`
}

// AdminContentArchiveStatus reports T76 reversibility-archive telemetry:
// how many lossy Layer 1 mutations were archived, expanded, re-injected,
// and how much disk the archive holds.
// AdminCacheAgeStatus exposes T102 response-cache age histogram.
type AdminCacheAgeStatus struct {
	Count int   `json:"count"`
	P50Ms int64 `json:"p50_ms"`
	P95Ms int64 `json:"p95_ms"`
	P99Ms int64 `json:"p99_ms"`
	MaxMs int64 `json:"max_ms"`
}

type AdminContentArchiveStatus struct {
	Count          int       `json:"count"`
	Archived       int       `json:"archived"`
	Expanded       int       `json:"expanded"`
	ReInjectCount  int       `json:"re_inject_count"`
	Evictions      int       `json:"evictions"`
	BytesRaw       int64     `json:"bytes_raw"`
	BytesStored    int64     `json:"bytes_stored"`
	LastArchived   time.Time `json:"last_archived"`
	LastExpanded   time.Time `json:"last_expanded"`
	LastReInjected time.Time `json:"last_re_injected"`
}

type AdminStatus struct {
	Status           string                              `json:"status"`
	Service          string                              `json:"service"`
	Version          string                              `json:"version"`
	Layers           map[string]bool                     `json:"layers"`
	Providers        map[string]bool                     `json:"providers"`
	QueueDepth       map[string]int                      `json:"queue_depth"`
	CacheEntries     int                                 `json:"cache_entries"`
	ListenPort       int                                 `json:"listen_port"`
	PrefillSpeed     int                                 `json:"prefill_speed"`
	Analytics        analytics.AnalyticsSnapshot         `json:"analytics"`
	RecentRequests   []types.RequestMetrics              `json:"recent_requests"`
	RecentFlights    []dbg.FlightRequestSummary          `json:"recent_flights"`
	Layer0           map[string]filter.FilterSnapshot    `json:"layer0"`
	ReadCache        AdminReadCacheStatus                `json:"read_cache"`
	Checkpoints      AdminCheckpointStatus               `json:"checkpoints"`
	ToolArchive      AdminToolArchiveStatus              `json:"tool_archive"`
	ContentArchive   AdminContentArchiveStatus           `json:"content_archive"`
	CacheAge         AdminCacheAgeStatus                 `json:"cache_age"`
	ProviderHealth   map[string]types.ProviderHealthInfo `json:"provider_health"`
	AnalyticsQueue   AnalyticsQueueStats                 `json:"analytics_queue"`
	PromptCache      PromptCacheStats                    `json:"prompt_cache"`
	Pipeline         []analytics.PhaseSnapshot           `json:"pipeline"`
	AnthropicVersion AnthropicVersionStats               `json:"anthropic_version"`
	Bypass           bool                                `json:"bypass"`
	BypassDetail     BypassStats                         `json:"bypass_detail"`
	Quality          quality.QualitySnapshot             `json:"quality"`
	AnyDegraded      bool                                `json:"any_provider_degraded"`
	Repetition       RepetitionStats                     `json:"repetition"`
	ToolPrune        ToolPruneStats                      `json:"tool_prune"`
	ServerState      ServerStateStats                    `json:"server_state"`
	OutputReduce     outputreduce.Snapshot               `json:"output_reduce"`
	// OutputReduceCounters surfaces T165/T166/T167 wire counters
	// (stop-sequence injection, streamcut fire, repdet rewrite).
	OutputReduceCounters OutputReduceTelemetry `json:"output_reduce_counters"`
	// QualityAB surfaces T186 cohort routing + auto-rollback state
	// for the T169 be-terse hint and future gated levers.
	QualityAB qualityab.QualityABTelemetry `json:"quality_ab"`
}

// AnthropicVersionStats reports T62 version-negotiation telemetry.
type AnthropicVersionStats struct {
	SupportedVersions []string `json:"supported_versions"`
	UnknownBehavior   string   `json:"unknown_behavior"`
	UnknownSeenTotal  int64    `json:"unknown_seen_total"`
}

// PromptCacheStats reports cumulative prompt-cache breakpoint telemetry (T45).
type PromptCacheStats struct {
	BreakpointsInjectedTotal int64 `json:"breakpoints_injected_total"`
	CacheReadTokens          int   `json:"cache_read_tokens"`
	CacheCreateTokens        int   `json:"cache_create_tokens"`
	EstimatedSavedReadTokens int   `json:"estimated_saved_read_tokens"`
}

// RepetitionStats exposes T93 posttool repetition store snapshot.
type RepetitionStats struct {
	Rows           int64 `json:"rows"`
	UniqueSessions int64 `json:"unique_sessions"`
	MaxHitCount    int64 `json:"max_hit_count"`
}

// ToolPruneStats exposes T103 tool-pruning usage tracker snapshot.
type ToolPruneStats struct {
	Sessions         int   `json:"sessions"`
	PrunedTotal      int64 `json:"pruned_total"`
	ReattachTotal    int64 `json:"reattach_total"`
	MissTotal        int64 `json:"miss_total"`
	RetryTotal       int64 `json:"retry_total"`
	AlwaysKeepTotal  int64 `json:"always_keep_total"`
	DisabledSessions int   `json:"disabled_sessions"`
	TokensSavedSum   int64 `json:"tokens_saved_sum"`
}

// ServerStateStats exposes T78 per-session response-id store snapshot.
type ServerStateStats struct {
	Sessions     int   `json:"sessions"`
	SkipTotal    int64 `json:"skip_total"`
	RecoverTotal int64 `json:"recover_total"`
}

// BypassStats exposes T81 bypass-state telemetry beyond the bare bool.
type BypassStats struct {
	Enabled           bool  `json:"enabled"`
	ExpiresAtUnix     int64 `json:"expires_at_unix"`
	NextRequestBudget int64 `json:"next_request_budget"`
	AutoRevertCount   int64 `json:"auto_revert_count"`
}

type AdminToggleProviderRequest struct {
	Provider string `json:"provider"`
	Enabled  bool   `json:"enabled"`
}

type AdminToggleLayerRequest struct {
	Layer   int  `json:"layer"`
	Enabled bool `json:"enabled"`
}

type adminActionResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func (p *Proxy) adminStatusSnapshot() AdminStatus {
	readStatus := AdminReadCacheStatus{}
	checkpointStatus := AdminCheckpointStatus{}
	toolArchiveStatus := AdminToolArchiveStatus{}
	contentArchiveStatus := AdminContentArchiveStatus{}
	repetitionStatus := RepetitionStats{}
	if home, err := os.UserHomeDir(); err == nil {
		// T93: read the repetition store snapshot if the file exists.
		if db, err := openRepetitionDB(home); err == nil && db != nil {
			defer func() { _ = db.Close() }()
			if s, err := repetitionSnapshot(db); err == nil {
				repetitionStatus = RepetitionStats{
					Rows:           s.Rows,
					UniqueSessions: s.UniqueSessions,
					MaxHitCount:    s.MaxHitCount,
				}
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if stats, err := readcache.Snapshot(readcache.DefaultDir(home)); err == nil {
			rate := 0.0
			if total := stats.Blocks + stats.Allows; total > 0 {
				rate = float64(stats.Blocks) / float64(total)
			}
			readStatus = AdminReadCacheStatus{
				Evaluations:     stats.Evaluations,
				Allows:          stats.Allows,
				Blocks:          stats.Blocks,
				UnchangedBlocks: stats.UnchangedBlocks,
				DeltaBlocks:     stats.DeltaBlocks,
				Sessions:        stats.Sessions,
				TrackedFiles:    stats.TrackedFiles,
				HitRate:         rate,
			}
		}
		if stats, err := checkpoints.Snapshot(checkpoints.DefaultDir(home)); err == nil {
			checkpointStatus = AdminCheckpointStatus{
				Count:       stats.Count,
				Captures:    stats.Captures,
				Restores:    stats.Restores,
				Bytes:       stats.Bytes,
				LastCapture: stats.LastCapture,
				LastRestore: stats.LastRestore,
				LastTrigger: stats.LastTrigger,
			}
		}
		if stats, err := toolarchive.Snapshot(toolarchive.DefaultDir(home)); err == nil {
			toolArchiveStatus = AdminToolArchiveStatus{
				Count:        stats.Count,
				Archived:     stats.Archived,
				Expanded:     stats.Expanded,
				BytesRaw:     stats.BytesRaw,
				BytesStored:  stats.BytesStored,
				LastArchived: stats.LastArchived,
				LastExpanded: stats.LastExpanded,
			}
		}
		if stats, err := contentarchive.LoadStats(contentarchive.DefaultDir(home)); err == nil {
			snap, _ := contentarchive.Snapshot(contentarchive.DefaultDir(home))
			contentArchiveStatus = AdminContentArchiveStatus{
				Count:          snap.Count,
				Archived:       stats.Archived,
				Expanded:       stats.Expanded,
				ReInjectCount:  stats.ReInjectCount,
				Evictions:      stats.Evictions,
				BytesRaw:       snap.BytesRaw,
				BytesStored:    snap.BytesStored,
				LastArchived:   stats.LastArchived,
				LastExpanded:   stats.LastExpanded,
				LastReInjected: stats.LastReInjected,
			}
		}
	}

	analyticsSnap := p.GetAnalytics()
	return AdminStatus{
		Status:  "ok",
		Service: "slimference",
		Version: Version,
		Layers: map[string]bool{
			"1": p.isLayerEnabled(1),
			"2": p.isLayerEnabled(2),
		},
		Providers: map[string]bool{
			"anthropic":     p.isProviderEnabled(types.Anthropic),
			"openai":        p.isProviderEnabled(types.OpenAI),
			"codex_chatgpt": p.isProviderEnabled(types.CodexChatGPT),
		},
		QueueDepth: map[string]int{
			"analytics": len(p.analyticsQueue),
		},
		CacheEntries:   p.responseCache.Len(),
		ListenPort:     p.config.Proxy.ListenPort,
		PrefillSpeed:   p.config.Usage.EstimatedPrefillSpeed,
		Analytics:      analyticsSnap,
		RecentRequests: p.GetRecentRequests(20),
		RecentFlights:  p.GetRecentFlights(20),
		Layer0:         filter.GlobalFilterObservability().Snapshot(),
		ReadCache:      readStatus,
		Checkpoints:    checkpointStatus,
		ToolArchive:    toolArchiveStatus,
		ContentArchive: contentArchiveStatus,
		CacheAge:       adminCacheAgeFrom(p.responseCache.AgeSnapshot()),
		ProviderHealth: map[string]types.ProviderHealthInfo{
			"anthropic":     p.GetProviderHealth(types.Anthropic),
			"openai":        p.GetProviderHealth(types.OpenAI),
			"codex_chatgpt": p.GetProviderHealth(types.CodexChatGPT),
		},
		// T83 surfaces a single composite degradation flag so callers
		// (slimference watch, TUI, future menubar) can render a status
		// banner without computing the worst-of across providers
		// themselves.
		AnyDegraded:    p.AnyProviderDegraded(),
		AnalyticsQueue: p.AnalyticsQueueStats(),
		PromptCache: PromptCacheStats{
			BreakpointsInjectedTotal: compression.PromptCacheBreakpointsInjected(),
			CacheReadTokens:          analyticsSnap.PromptCacheReadTokens,
			CacheCreateTokens:        analyticsSnap.PromptCacheCreateTokens,
			EstimatedSavedReadTokens: int(float64(analyticsSnap.PromptCacheReadTokens) * 0.9),
		},
		Pipeline: p.pipelineHist.Snapshot(),
		AnthropicVersion: AnthropicVersionStats{
			SupportedVersions: p.config.Proxy.AnthropicVersions,
			UnknownBehavior:   p.config.Proxy.AnthropicUnknownBehavior,
			UnknownSeenTotal:  AnthropicUnknownVersionCount(),
		},
		Bypass: p.Bypass(),
		BypassDetail: BypassStats{
			Enabled:           p.Bypass(),
			ExpiresAtUnix:     bypassExpiresUnix(p),
			NextRequestBudget: p.BypassNextRequestCount(),
			AutoRevertCount:   p.BypassAutoRevertCount(),
		},
		Quality:    p.QualitySnapshot(),
		Repetition: repetitionStatus,
		ToolPrune: func() ToolPruneStats {
			if p.toolPrune == nil {
				return ToolPruneStats{}
			}
			s := p.toolPrune.Snapshot()
			return ToolPruneStats{
				Sessions:         s.Sessions,
				PrunedTotal:      s.PrunedTotal,
				ReattachTotal:    s.ReattachTotal,
				MissTotal:        s.MissTotal,
				RetryTotal:       s.RetryTotal,
				AlwaysKeepTotal:  s.AlwaysKeepTotal,
				DisabledSessions: s.DisabledSessions,
				TokensSavedSum:   s.TokensSavedSum,
			}
		}(),
		ServerState: func() ServerStateStats {
			if p.serverState == nil {
				return ServerStateStats{}
			}
			s := p.serverState.Snapshot()
			return ServerStateStats{
				Sessions:     s.Sessions,
				SkipTotal:    s.SkipTotal,
				RecoverTotal: s.RecoverTotal,
			}
		}(),
		OutputReduce: func() outputreduce.Snapshot {
			if p.outputReduce == nil {
				return outputreduce.Snapshot{}
			}
			return p.outputReduce.Snapshot()
		}(),
		OutputReduceCounters: p.outputReduceCounters.Snapshot(),
		QualityAB: func() qualityab.QualityABTelemetry {
			if p.qualityAB == nil {
				return qualityab.QualityABTelemetry{}
			}
			return p.qualityAB.Snapshot()
		}(),
	}
}

// openRepetitionDB returns the repetition.db connection if the file
// exists, else (nil, nil). Errors are surfaced for the caller to log.
func openRepetitionDB(home string) (*sql.DB, error) {
	path := repetition.DefaultPath(home)
	if _, err := os.Stat(path); err != nil {
		return nil, nil
	}
	return repetition.Open(path)
}

// repetitionSnapshot is a thin wrapper to keep the import surface tidy.
func repetitionSnapshot(db *sql.DB) (repetition.Stats, error) {
	return repetition.Snapshot(db)
}

// bypassExpiresUnix returns the unix-second deadline of any active
// duration-bounded bypass; zero when none.
func bypassExpiresUnix(p *Proxy) int64 {
	if t := p.BypassExpiresAt(); !t.IsZero() {
		return t.Unix()
	}
	return 0
}

// adminBypassHandler returns or sets the master bypass state (T67),
// honouring T81 duration / next-request scoping when the body sets
// the corresponding fields.
func (p *Proxy) adminBypassHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeAdminJSON(w, http.StatusOK, AdminBypassResponse{
			Enabled:           p.Bypass(),
			ExpiresAtUnix:     bypassExpiresUnix(p),
			NextRequestBudget: p.BypassNextRequestCount(),
		})
	case http.MethodPost:
		var req AdminBypassRequest
		if !decodeAdminJSON(r, &req) {
			writeAdminJSON(w, http.StatusBadRequest, adminActionResponse{
				OK: false, Error: "invalid JSON payload",
			})
			return
		}
		switch {
		case req.Enabled && req.NextRequests > 0:
			p.SetBypassForNextRequests(req.NextRequests)
		case req.Enabled && req.DurationSeconds > 0:
			p.SetBypassFor(time.Duration(req.DurationSeconds) * time.Second)
		default:
			p.SetBypass(req.Enabled)
		}
		writeAdminJSON(w, http.StatusOK, AdminBypassResponse{
			Enabled:           p.Bypass(),
			ExpiresAtUnix:     bypassExpiresUnix(p),
			NextRequestBudget: p.BypassNextRequestCount(),
		})
	default:
		writeAdminJSON(w, http.StatusMethodNotAllowed, adminActionResponse{OK: false})
	}
}

func (p *Proxy) AdminStatusSnapshot() AdminStatus {
	return p.adminStatusSnapshot()
}

func writeAdminJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeAdminJSON(r *http.Request, dst any) bool {
	if r.Body == nil {
		return false
	}
	return json.NewDecoder(r.Body).Decode(dst) == nil
}

func (p *Proxy) adminStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAdminJSON(w, http.StatusMethodNotAllowed, adminActionResponse{OK: false})
		return
	}
	writeAdminJSON(w, http.StatusOK, p.adminStatusSnapshot())
}

// adminSecuritySuspendHandler implements the T59 per-session override.
// GET returns the current state. POST with {suspend_seconds: N} sets or
// clears the suspension (N <= 0 clears). Server-side clamping ensures the
// detector can never be suspended past security.MaxSuspendDuration.
func (p *Proxy) adminSecuritySuspendHandler(w http.ResponseWriter, r *http.Request) {
	if p.secretsDetector == nil {
		writeAdminJSON(w, http.StatusServiceUnavailable, adminActionResponse{
			OK:    false,
			Error: "secrets detector not configured",
		})
		return
	}
	switch r.Method {
	case http.MethodGet:
		active, until := p.secretsDetector.SuspendState()
		resp := AdminSecuritySuspendResponse{
			Active: active,
			Mode:   p.secretsDetector.Mode(),
		}
		if active {
			resp.UntilUnixSec = until.Unix()
		}
		writeAdminJSON(w, http.StatusOK, resp)
	case http.MethodPost:
		var req AdminSecuritySuspendRequest
		if !decodeAdminJSON(r, &req) {
			writeAdminJSON(w, http.StatusBadRequest, adminActionResponse{
				OK: false, Error: "invalid JSON payload",
			})
			return
		}
		var effective time.Time
		if req.SuspendSeconds <= 0 {
			effective = p.secretsDetector.SuspendUntil(time.Time{})
		} else {
			effective = p.secretsDetector.SuspendUntil(
				time.Now().Add(time.Duration(req.SuspendSeconds) * time.Second))
		}
		active, _ := p.secretsDetector.SuspendState()
		resp := AdminSecuritySuspendResponse{
			Active: active,
			Mode:   p.secretsDetector.Mode(),
		}
		if active {
			resp.UntilUnixSec = effective.Unix()
		}
		writeAdminJSON(w, http.StatusOK, resp)
	default:
		writeAdminJSON(w, http.StatusMethodNotAllowed, adminActionResponse{OK: false})
	}
}

func (p *Proxy) adminProviderHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAdminJSON(w, http.StatusMethodNotAllowed, adminActionResponse{OK: false})
		return
	}
	var req AdminToggleProviderRequest
	if !decodeAdminJSON(r, &req) {
		writeAdminJSON(w, http.StatusBadRequest, adminActionResponse{OK: false})
		return
	}
	switch req.Provider {
	case "anthropic":
		p.SetProviderEnabled(types.Anthropic, req.Enabled)
	case "openai":
		p.SetProviderEnabled(types.OpenAI, req.Enabled)
	case "codex_chatgpt":
		p.SetProviderEnabled(types.CodexChatGPT, req.Enabled)
	default:
		writeAdminJSON(w, http.StatusBadRequest, adminActionResponse{OK: false})
		return
	}
	writeAdminJSON(w, http.StatusOK, adminActionResponse{OK: true})
}

func (p *Proxy) adminLayerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAdminJSON(w, http.StatusMethodNotAllowed, adminActionResponse{OK: false})
		return
	}
	var req AdminToggleLayerRequest
	if !decodeAdminJSON(r, &req) {
		writeAdminJSON(w, http.StatusBadRequest, adminActionResponse{OK: false})
		return
	}
	if req.Layer < 1 || req.Layer > 3 {
		writeAdminJSON(w, http.StatusBadRequest, adminActionResponse{OK: false})
		return
	}
	p.SetLayerEnabled(req.Layer, req.Enabled)
	writeAdminJSON(w, http.StatusOK, adminActionResponse{OK: true})
}

func (p *Proxy) adminFlushHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAdminJSON(w, http.StatusMethodNotAllowed, adminActionResponse{OK: false})
		return
	}
	p.FlushCaches()
	writeAdminJSON(w, http.StatusOK, adminActionResponse{OK: true})
}

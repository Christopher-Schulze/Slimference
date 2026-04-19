package proxy

import (
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/checkpoints"
	"github.com/slimference/slimference/internal/readcache"
	"github.com/slimference/slimference/internal/toolarchive"
	"github.com/slimference/slimference/internal/types"
)

const (
	AdminBasePath     = "/_slimference/admin"
	AdminStatusPath   = AdminBasePath + "/status"
	AdminProviderPath = AdminBasePath + "/provider"
	AdminLayerPath    = AdminBasePath + "/layer"
	AdminFlushPath    = AdminBasePath + "/flush"
)

type AdminLayer2Status struct {
	HasCache    bool      `json:"has_cache"`
	Compressing bool      `json:"compressing"`
	LastRun     time.Time `json:"last_run"`
	QueueDepth  int       `json:"queue_depth"`
}

type AdminReadCacheStatus struct {
	Evaluations     int `json:"evaluations"`
	Allows          int `json:"allows"`
	Blocks          int `json:"blocks"`
	UnchangedBlocks int `json:"unchanged_blocks"`
	DeltaBlocks     int `json:"delta_blocks"`
	Sessions        int `json:"sessions"`
	TrackedFiles    int `json:"tracked_files"`
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

type AdminStatus struct {
	Status            string                              `json:"status"`
	Service           string                              `json:"service"`
	Version           string                              `json:"version"`
	Layers            map[string]bool                     `json:"layers"`
	Providers         map[string]bool                     `json:"providers"`
	QueueDepth        map[string]int                      `json:"queue_depth"`
	CacheEntries      int                                 `json:"cache_entries"`
	MiniMaxConfigured bool                                `json:"minimax_configured"`
	ListenPort        int                                 `json:"listen_port"`
	PrefillSpeed      int                                 `json:"prefill_speed"`
	Analytics         analytics.AnalyticsSnapshot         `json:"analytics"`
	RecentRequests    []types.RequestMetrics              `json:"recent_requests"`
	Layer2            AdminLayer2Status                   `json:"layer2"`
	ReadCache         AdminReadCacheStatus                `json:"read_cache"`
	Checkpoints       AdminCheckpointStatus               `json:"checkpoints"`
	ToolArchive       AdminToolArchiveStatus              `json:"tool_archive"`
	ProviderHealth    map[string]types.ProviderHealthInfo `json:"provider_health"`
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
	OK bool `json:"ok"`
}

func (p *Proxy) adminStatusSnapshot() AdminStatus {
	layer2 := AdminLayer2Status{}
	if p.layer2 != nil {
		cache := p.layer2.GetCache()
		layer2.Compressing = cache.Compressing.Load()
		layer2.QueueDepth = len(p.compressQueue)
		if cs := cache.Get(); cs != nil {
			layer2.HasCache = true
			layer2.LastRun = cs.CreatedAt
		}
	}
	readStatus := AdminReadCacheStatus{}
	checkpointStatus := AdminCheckpointStatus{}
	toolArchiveStatus := AdminToolArchiveStatus{}
	if home, err := os.UserHomeDir(); err == nil {
		if stats, err := readcache.Snapshot(readcache.DefaultDir(home)); err == nil {
			readStatus = AdminReadCacheStatus{
				Evaluations:     stats.Evaluations,
				Allows:          stats.Allows,
				Blocks:          stats.Blocks,
				UnchangedBlocks: stats.UnchangedBlocks,
				DeltaBlocks:     stats.DeltaBlocks,
				Sessions:        stats.Sessions,
				TrackedFiles:    stats.TrackedFiles,
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
	}

	return AdminStatus{
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
		ListenPort:        p.config.Proxy.ListenPort,
		PrefillSpeed:      p.config.Usage.EstimatedPrefillSpeed,
		Analytics:         p.GetAnalytics(),
		RecentRequests:    p.GetRecentRequests(20),
		Layer2:            layer2,
		ReadCache:         readStatus,
		Checkpoints:       checkpointStatus,
		ToolArchive:       toolArchiveStatus,
		ProviderHealth: map[string]types.ProviderHealthInfo{
			"anthropic": p.GetProviderHealth(types.Anthropic),
			"openai":    p.GetProviderHealth(types.OpenAI),
		},
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

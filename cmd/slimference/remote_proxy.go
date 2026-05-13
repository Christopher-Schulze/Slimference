package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/daemon"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/proxy"
	"github.com/slimference/slimference/internal/sessions"
	"github.com/slimference/slimference/internal/tui"
	"github.com/slimference/slimference/internal/types"
)

type remoteProxyAdapter struct {
	cfg    *config.Config
	client *http.Client
	logger *fileSessionLogger

	mu          sync.RWMutex
	lastRefresh time.Time
	status      proxy.AdminStatus
}

func newRemoteProxyAdapter(cfg *config.Config) *remoteProxyAdapter {
	a := &remoteProxyAdapter{
		cfg: cfg,
		client: &http.Client{
			Timeout: 1200 * time.Millisecond,
		},
		logger: &fileSessionLogger{
			path: config.ExpandHomePath(cfg.Logging.File),
		},
		status: proxy.AdminStatus{
			Layers: map[string]bool{
				"1": cfg.Compression.Layer1Enabled,
				"2": cfg.Compression.Layer2Enabled,
				"3": cfg.Compression.Layer3Enabled,
			},
			Providers: map[string]bool{
				"anthropic": true,
				"openai":    true,
			},
			ListenPort:   cfg.Proxy.ListenPort,
			PrefillSpeed: cfg.Usage.EstimatedPrefillSpeed,
			ProviderHealth: map[string]types.ProviderHealthInfo{
				"anthropic": {Status: types.ProviderHealthIdle},
				"openai":    {Status: types.ProviderHealthIdle},
			},
		},
	}
	return a
}

func (a *remoteProxyAdapter) statusURL(path string) string {
	return strings.TrimRight(a.cfg.ListenURL(), "/") + path
}

func (a *remoteProxyAdapter) refresh() {
	a.mu.RLock()
	if time.Since(a.lastRefresh) < 300*time.Millisecond {
		a.mu.RUnlock()
		return
	}
	a.mu.RUnlock()

	resp, err := a.client.Get(a.statusURL(proxy.AdminStatusPath))
	if err != nil {
		a.mu.Lock()
		a.lastRefresh = time.Now()
		a.status.Analytics = analytics.AnalyticsSnapshot{}
		a.status.RecentRequests = nil
		a.status.Layer2 = proxy.AdminLayer2Status{}
		a.status.ReadCache = proxy.AdminReadCacheStatus{}
		a.status.ProviderHealth = map[string]types.ProviderHealthInfo{
			"anthropic": {Status: types.ProviderHealthIdle},
			"openai":    {Status: types.ProviderHealthIdle},
		}
		a.mu.Unlock()
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}

	var status proxy.AdminStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return
	}

	a.mu.Lock()
	a.status = status
	a.lastRefresh = time.Now()
	a.mu.Unlock()
}

func (a *remoteProxyAdapter) post(path string, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	resp, err := a.client.Post(a.statusURL(path), "application/json", bytes.NewReader(body))
	if err != nil {
		return
	}
	defer resp.Body.Close()
	a.mu.Lock()
	a.lastRefresh = time.Time{}
	a.mu.Unlock()
}

func (a *remoteProxyAdapter) SetProviderEnabled(prov types.Provider, enabled bool) {
	a.mu.Lock()
	switch prov {
	case types.Anthropic:
		a.status.Providers["anthropic"] = enabled
	case types.OpenAI:
		a.status.Providers["openai"] = enabled
	}
	a.mu.Unlock()

	switch prov {
	case types.Anthropic:
		a.post(proxy.AdminProviderPath, proxy.AdminToggleProviderRequest{Provider: "anthropic", Enabled: enabled})
	case types.OpenAI:
		a.post(proxy.AdminProviderPath, proxy.AdminToggleProviderRequest{Provider: "openai", Enabled: enabled})
	}
}

func (a *remoteProxyAdapter) SetLayerEnabled(layer int, enabled bool) {
	a.mu.Lock()
	a.status.Layers[strconvItoa(layer)] = enabled
	a.mu.Unlock()
	a.post(proxy.AdminLayerPath, proxy.AdminToggleLayerRequest{Layer: layer, Enabled: enabled})
}

func (a *remoteProxyAdapter) IsProviderEnabled(prov types.Provider) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	switch prov {
	case types.Anthropic:
		return a.status.Providers["anthropic"]
	case types.OpenAI:
		return a.status.Providers["openai"]
	default:
		return false
	}
}

func (a *remoteProxyAdapter) IsLayerEnabled(layer int) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status.Layers[strconvItoa(layer)]
}

func (a *remoteProxyAdapter) FlushCaches() {
	a.post(proxy.AdminFlushPath, struct{}{})
}

func (a *remoteProxyAdapter) GetAnalytics() analytics.AnalyticsSnapshot {
	a.refresh()
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status.Analytics
}

func (a *remoteProxyAdapter) GetRecentRequests(n int) []types.RequestMetrics {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if n <= 0 || len(a.status.RecentRequests) <= n {
		return append([]types.RequestMetrics(nil), a.status.RecentRequests...)
	}
	start := len(a.status.RecentRequests) - n
	return append([]types.RequestMetrics(nil), a.status.RecentRequests[start:]...)
}

func (a *remoteProxyAdapter) GetRecentFlights(int) []dbg.FlightRequestSummary {
	return nil
}

func (a *remoteProxyAdapter) GetLayer2Status() tui.Layer2Status {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return tui.Layer2Status{
		HasCache:    a.status.Layer2.HasCache,
		Compressing: a.status.Layer2.Compressing,
		LastRun:     a.status.Layer2.LastRun,
		QueueDepth:  a.status.Layer2.QueueDepth,
	}
}

func (a *remoteProxyAdapter) GetQualityStatus() tui.QualityStatus {
	a.refresh()
	a.mu.RLock()
	defer a.mu.RUnlock()
	return tui.QualityStatus{
		ReReadSessions:    a.status.Quality.ReRead.Sessions,
		ReReadTotalChecks: a.status.Quality.ReRead.TotalChecks,
		ReReadTotalHits:   a.status.Quality.ReRead.TotalHits,
		ReReadRate:        a.status.Quality.ReRead.Rate,
		BaselineHitRate:   a.status.Quality.CacheMissSpike.BaselineRate,
		SpikeActive:       a.status.Quality.CacheMissSpike.Active,
		LastSpikeUnix:     a.status.Quality.CacheMissSpike.LastSpikeUnix,
		TotalSpikeCount:   a.status.Quality.CacheMissSpike.TotalSpikeCount,
		TotalSaved:        a.status.Quality.NetSavings.TotalSaved,
		TotalInvalidation: a.status.Quality.NetSavings.TotalInvalidation,
		NetSaved:          a.status.Quality.NetSavings.NetSaved,
	}
}

func (a *remoteProxyAdapter) GetReadCacheStatus() tui.ReadCacheStatus {
	a.refresh()
	a.mu.RLock()
	defer a.mu.RUnlock()
	return tui.ReadCacheStatus{
		Evaluations:     a.status.ReadCache.Evaluations,
		Allows:          a.status.ReadCache.Allows,
		Blocks:          a.status.ReadCache.Blocks,
		UnchangedBlocks: a.status.ReadCache.UnchangedBlocks,
		DeltaBlocks:     a.status.ReadCache.DeltaBlocks,
		Sessions:        a.status.ReadCache.Sessions,
		TrackedFiles:    a.status.ReadCache.TrackedFiles,
		HitRate:         a.status.ReadCache.HitRate,
	}
}

func (a *remoteProxyAdapter) GetCheckpointStatus() tui.CheckpointStatus {
	a.refresh()
	a.mu.RLock()
	defer a.mu.RUnlock()
	return tui.CheckpointStatus{
		Count:       a.status.Checkpoints.Count,
		Captures:    a.status.Checkpoints.Captures,
		Restores:    a.status.Checkpoints.Restores,
		Bytes:       a.status.Checkpoints.Bytes,
		LastCapture: a.status.Checkpoints.LastCapture,
		LastRestore: a.status.Checkpoints.LastRestore,
		LastTrigger: a.status.Checkpoints.LastTrigger,
	}
}

func (a *remoteProxyAdapter) GetToolArchiveStatus() tui.ToolArchiveStatus {
	a.refresh()
	a.mu.RLock()
	defer a.mu.RUnlock()
	return tui.ToolArchiveStatus{
		Count:        a.status.ToolArchive.Count,
		Archived:     a.status.ToolArchive.Archived,
		Expanded:     a.status.ToolArchive.Expanded,
		BytesRaw:     a.status.ToolArchive.BytesRaw,
		BytesStored:  a.status.ToolArchive.BytesStored,
		LastArchived: a.status.ToolArchive.LastArchived,
		LastExpanded: a.status.ToolArchive.LastExpanded,
	}
}

func (a *remoteProxyAdapter) GetProviderHealth(prov types.Provider) types.ProviderHealthInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()
	switch prov {
	case types.Anthropic:
		return a.status.ProviderHealth["anthropic"]
	case types.OpenAI:
		return a.status.ProviderHealth["openai"]
	default:
		return types.ProviderHealthInfo{Status: types.ProviderHealthIdle}
	}
}

func (a *remoteProxyAdapter) SessionLogger() tui.SessionLoggerInterface {
	return a.logger
}

func (a *remoteProxyAdapter) Shutdown(context.Context) error {
	return nil
}

func (a *remoteProxyAdapter) Config() tui.ProxyConfigInterface {
	return &configAdapter{cfg: a.cfg}
}

// Bypass queries the running daemon's admin endpoint. Returns false if the
// daemon is unreachable - the caller treats "unknown" as "not bypassing".
func (a *remoteProxyAdapter) Bypass() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status.Bypass
}

// SetBypass pushes the new state through the admin endpoint. Failures are
// logged but not propagated: the TUI renders the outcome on the next tick
// via Bypass().
func (a *remoteProxyAdapter) SetBypass(enabled bool) {
	body, _ := json.Marshal(proxy.AdminBypassRequest{Enabled: enabled})
	req, err := http.NewRequest(http.MethodPost,
		"http://"+a.cfg.ListenAddr()+proxy.AdminBypassPath,
		bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

type fileSessionLogger struct {
	path string
}

func (l *fileSessionLogger) Recent(n int) []sessions.LogEntry {
	if l.path == "" {
		return nil
	}
	lines, err := daemon.ReadRecentLogLines(l.path, n, time.Time{})
	if err != nil || len(lines) == 0 {
		return nil
	}
	out := make([]sessions.LogEntry, 0, len(lines))
	for _, line := range lines {
		out = append(out, parseLogEntry(line))
	}
	return out
}

func (l *fileSessionLogger) Format(entry sessions.LogEntry) string {
	return entry.Message
}

func parseLogEntry(line string) sessions.LogEntry {
	entry := sessions.LogEntry{
		Timestamp: time.Now(),
		Level:     "INFO",
		Message:   line,
	}

	var payload struct {
		Time  string `json:"time"`
		Level string `json:"level"`
		Msg   string `json:"msg"`
	}
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return entry
	}
	if ts, err := time.Parse(time.RFC3339Nano, payload.Time); err == nil {
		entry.Timestamp = ts
	}
	if payload.Level != "" {
		entry.Level = strings.ToUpper(payload.Level)
	}
	if payload.Msg != "" {
		entry.Message = payload.Msg
	}
	return entry
}

func strconvItoa(n int) string {
	if n == 1 {
		return "1"
	}
	if n == 2 {
		return "2"
	}
	if n == 3 {
		return "3"
	}
	return ""
}

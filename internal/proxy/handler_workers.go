package proxy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// healthHandler responds to GET /health with full proxy status JSON.
func (p *Proxy) healthHandler(w http.ResponseWriter, _ *http.Request) {
	resource := p.daemonResourceSnapshot()
	stateBytes, _ := p.daemonStateBytes()
	status := struct {
		Status            string          `json:"status"`
		Service           string          `json:"service"`
		Version           string          `json:"version"`
		PID               int             `json:"pid"`
		RSSBytes          int64           `json:"rss_bytes"`
		UptimeSec         int64           `json:"uptime_sec"`
		CPUUserSeconds    float64         `json:"cpu_user_seconds"`
		CPUSystemSeconds  float64         `json:"cpu_system_seconds"`
		CPUPercent        float64         `json:"cpu_percent"`
		CPUWindowPercent  float64         `json:"cpu_window_percent"`
		CPUWindowSeconds  float64         `json:"cpu_window_seconds"`
		DiskReadOps       int64           `json:"disk_read_ops"`
		DiskWriteOps      int64           `json:"disk_write_ops"`
		DiskReadOpsDelta  int64           `json:"disk_read_ops_delta"`
		DiskWriteOpsDelta int64           `json:"disk_write_ops_delta"`
		StateBytes        int64           `json:"state_bytes"`
		Layers            map[string]bool `json:"layers"`
		Providers         map[string]bool `json:"providers"`
		QueueDepth        map[string]int  `json:"queue_depth"`
		CacheEntries      int             `json:"cache_entries"`
	}{
		Status:            "ok",
		Service:           "slimference",
		Version:           Version,
		PID:               os.Getpid(),
		RSSBytes:          resource.RSSBytes,
		UptimeSec:         p.uptimeSeconds(),
		CPUUserSeconds:    resource.CPUUserSeconds,
		CPUSystemSeconds:  resource.CPUSystemSeconds,
		CPUPercent:        resource.CPUPercent,
		CPUWindowPercent:  resource.CPUWindowPercent,
		CPUWindowSeconds:  resource.CPUWindowSeconds,
		DiskReadOps:       resource.DiskReadOps,
		DiskWriteOps:      resource.DiskWriteOps,
		DiskReadOpsDelta:  resource.DiskReadOpsDelta,
		DiskWriteOpsDelta: resource.DiskWriteOpsDelta,
		StateBytes:        stateBytes,
		Layers: map[string]bool{
			"1": p.isLayerEnabled(1),
			"2": p.isLayerEnabled(2),
		},
		Providers: map[string]bool{
			"anthropic": p.isProviderEnabled(types.Anthropic),
			"openai":    p.isProviderEnabled(types.OpenAI),
		},
		QueueDepth: map[string]int{
			"analytics": len(p.analyticsQueue),
		},
		CacheEntries: p.responseCache.Len(),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(status)
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

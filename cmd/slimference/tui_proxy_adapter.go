package main

import (
	"context"
	"fmt"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/analytics"
	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/control/apps"
	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/proxy"
	"github.com/Christopher-Schulze/Slimference/internal/tui"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// proxyAdapter adapts proxy.Proxy to tui.ProxyInterface to avoid import cycle.
type proxyAdapter struct {
	p *proxy.Proxy
}

func newProxyAdapter(p *proxy.Proxy) tui.ProxyInterface {
	return &proxyAdapter{p: p}
}

func (a *proxyAdapter) SetProviderEnabled(prov types.Provider, enabled bool) {
	a.p.SetProviderEnabled(prov, enabled)
}
func (a *proxyAdapter) SetLayerEnabled(layer int, enabled bool) {
	a.p.SetLayerEnabled(layer, enabled)
}
func (a *proxyAdapter) IsProviderEnabled(prov types.Provider) bool {
	return a.p.IsProviderEnabled(prov)
}
func (a *proxyAdapter) IsLayerEnabled(layer int) bool {
	return a.p.IsLayerEnabled(layer)
}
func (a *proxyAdapter) FlushCaches() {
	a.p.FlushCaches()
}
func (a *proxyAdapter) GetAnalytics() analytics.AnalyticsSnapshot {
	return a.p.GetAnalytics()
}
func (a *proxyAdapter) GetRecentRequests(n int) []types.RequestMetrics {
	return a.p.GetRecentRequests(n)
}
func (a *proxyAdapter) GetRecentFlights(n int) []dbg.FlightRequestSummary {
	return a.p.GetRecentFlights(n)
}
func (a *proxyAdapter) GetQualityStatus() tui.QualityStatus {
	q := a.p.QualitySnapshot()
	return tui.QualityStatus{
		ReReadSessions:    q.ReRead.Sessions,
		ReReadTotalChecks: q.ReRead.TotalChecks,
		ReReadTotalHits:   q.ReRead.TotalHits,
		ReReadRate:        q.ReRead.Rate,
		BaselineHitRate:   q.CacheMissSpike.BaselineRate,
		SpikeActive:       q.CacheMissSpike.Active,
		LastSpikeUnix:     q.CacheMissSpike.LastSpikeUnix,
		TotalSpikeCount:   q.CacheMissSpike.TotalSpikeCount,
		TotalSaved:        q.NetSavings.TotalSaved,
		TotalInvalidation: q.NetSavings.TotalInvalidation,
		NetSaved:          q.NetSavings.NetSaved,
	}
}
func (a *proxyAdapter) GetProductStatus() tui.ProductStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	state, ok := a.p.SetupStateSnapshot(ctx)
	if !ok {
		return tui.ProductStatus{}
	}
	return productStatusFromSetupState(state)
}
func (a *proxyAdapter) GetReadCacheStatus() tui.ReadCacheStatus {
	status := a.p.AdminStatusSnapshot().ReadCache
	return tui.ReadCacheStatus{
		Evaluations:     status.Evaluations,
		Allows:          status.Allows,
		Blocks:          status.Blocks,
		UnchangedBlocks: status.UnchangedBlocks,
		DeltaBlocks:     status.DeltaBlocks,
		Sessions:        status.Sessions,
		TrackedFiles:    status.TrackedFiles,
		HitRate:         status.HitRate,
	}
}
func (a *proxyAdapter) GetProviderHealth(prov types.Provider) types.ProviderHealthInfo {
	return a.p.GetProviderHealth(prov)
}
func (a *proxyAdapter) SessionLogger() tui.SessionLoggerInterface {
	return a.p.SessionLogger() // *sessions.SessionLogger implements tui.SessionLoggerInterface
}
func (a *proxyAdapter) Shutdown(ctx context.Context) error {
	return a.p.Shutdown(ctx)
}
func (a *proxyAdapter) Config() tui.ProxyConfigInterface {
	return &configAdapter{cfg: a.p.Config()}
}
func (a *proxyAdapter) Bypass() bool           { return a.p.Bypass() }
func (a *proxyAdapter) SetBypass(enabled bool) { a.p.SetBypass(enabled) }

// AppEntries returns the per-app routing state from the in-process
// AppsManager. In-process callers don't go through HTTP (unlike the
// remote adapter); they read the manager directly.
func (a *proxyAdapter) AppEntries() []tui.AppEntry {
	m := a.p.AppsManager()
	if m == nil {
		return nil
	}
	pol := m.Policy()
	detected := m.DetectedBinaries()
	out := make([]tui.AppEntry, 0)
	for _, id := range apps.KnownApps {
		entry := tui.AppEntry{
			ID:      string(id),
			Enabled: pol.IsEnabled(id),
		}
		if paths, ok := detected[id]; ok && len(paths) > 0 {
			entry.Detected = true
			entry.BinPath = paths[0]
		}
		out = append(out, entry)
	}
	return out
}

// SetAppEnabled updates the in-process AppsManager.
func (a *proxyAdapter) SetAppEnabled(id string, enabled bool) error {
	m := a.p.AppsManager()
	if m == nil {
		return fmt.Errorf("apps manager not wired")
	}
	return m.SetEnabled(apps.AppID(id), enabled)
}

// configAdapter adapts config.Config to tui.ProxyConfigInterface.
type configAdapter struct {
	cfg *config.Config
}

func (ca *configAdapter) GetListenPort() int   { return ca.cfg.Proxy.ListenPort }
func (ca *configAdapter) GetPrefillSpeed() int { return ca.cfg.Usage.EstimatedPrefillSpeed }

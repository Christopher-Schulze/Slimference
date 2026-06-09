package proxy

import (
	"github.com/Christopher-Schulze/Slimference/internal/control/apps"
)

// SetAppsManager installs (or replaces) the per-app policy manager.
// Safe to call concurrently with traffic; the next routing decision
// observes the new manager. Passing nil clears it (back to legacy
// "all apps enabled" mode).
func (p *Proxy) SetAppsManager(m *apps.Manager) {
	p.appsManagerPtr.Store(m)
}

// AppsManager returns the currently installed manager, or nil when
// none has been wired. Stable across reloads because Manager methods
// snapshot policy under their own lock.
func (p *Proxy) AppsManager() *apps.Manager {
	return p.appsManagerPtr.Load()
}

// OutputReduceCountersSnapshot returns a value-copy of the Phase F
// counters so probes / admin handlers can serialise them without
// holding any proxy-internal locks.
func (p *Proxy) OutputReduceCountersSnapshot() OutputReduceTelemetry {
	return p.outputReduceCounters.Snapshot()
}

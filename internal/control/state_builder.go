package control

import (
	"context"
	"time"
)

// Build composes a SetupState by calling each probe. Missing probes
// leave the corresponding state-field at its zero value (the renderer
// shows that as "unknown" / "absent"). Probe errors are absorbed
// silently - the snapshot is best-effort.
//
// Build is safe to call concurrently. Probes execute in parallel
// goroutines to honour the ≤ 100 ms budget when the slowest probe
// dominates.
func Build(ctx context.Context, p Probes) SetupState {
	state := SetupState{}
	now := time.Now
	if p.Clock != nil {
		now = p.Clock
	}

	// Run each probe in its own goroutine so the wall-clock budget
	// is bounded by the slowest probe rather than the sum.
	done := make(chan struct{}, 9)
	go func() {
		if p.CA != nil {
			state.CA = p.CA.ProbeCA(ctx)
		}
		done <- struct{}{}
	}()
	go func() {
		if p.Daemon != nil {
			state.Daemon = p.Daemon.ProbeDaemon(ctx)
		}
		done <- struct{}{}
	}()
	go func() {
		if p.Listener != nil {
			state.Listener = p.Listener.ProbeListener(ctx)
		}
		done <- struct{}{}
	}()
	go func() {
		if p.NetworkRedir != nil {
			state.NetworkRedir = p.NetworkRedir.ProbeNetwork(ctx)
		}
		done <- struct{}{}
	}()
	go func() {
		if p.Indist != nil {
			state.Indist = p.Indist.ProbeIndist(ctx)
		}
		done <- struct{}{}
	}()
	go func() {
		if p.Apps != nil {
			state.Apps = p.Apps.ProbeApps(ctx)
		}
		done <- struct{}{}
	}()
	go func() {
		if p.CodexRoute != nil {
			state.CodexRoute = p.CodexRoute.ProbeCodexRoute(ctx)
		}
		done <- struct{}{}
	}()
	go func() {
		if p.Savings != nil {
			state.Savings = p.Savings.ProbeSavings(ctx)
		}
		done <- struct{}{}
	}()
	go func() {
		if p.WSS != nil {
			state.WSS = p.WSS.ProbeWSS(ctx)
		}
		done <- struct{}{}
	}()
	for i := 0; i < 9; i++ {
		<-done
	}
	state.HostBudget = EvaluateHostBudget(state.Daemon, state.WSS)
	state.Savings.Product = state.Savings.ProductSignalsWithHostBudget(state.HostBudget)
	state.UpdatedAt = now()
	return state
}

// IsHealthy reports whether the aggregate state suggests a working
// transparent/lab install. "Healthy" means: CA material installed, daemon
// running + healthy, transparent listener bound, network redirect armed.
// Keychain trust is reported separately because the scoped Codex CLI WSS path
// and the Desktop --with-ca-env probe do not require macOS Keychain trust.
// Per-app integration status doesn't count toward IsHealthy (apps can be
// intentionally off).
func (s SetupState) IsHealthy() bool {
	if !s.CA.Installed {
		return false
	}
	if !s.Daemon.Running || !s.Daemon.HealthOK {
		return false
	}
	if !(s.Listener.BoundOn443 || s.Listener.BoundOnSNIPeek) {
		return false
	}
	if !(s.NetworkRedir.HostsActive || s.NetworkRedir.PFCtlActive) {
		return false
	}
	return true
}

// EnabledApps returns the AppEntry list filtered to currently-enabled
// integrations. Useful for status surfaces that want to display only
// the integrations that are on.
func (s SetupState) EnabledApps() []AppEntry {
	out := make([]AppEntry, 0, len(s.Apps))
	for _, a := range s.Apps {
		if a.Enabled {
			out = append(out, a)
		}
	}
	return out
}

// DetectedApps returns the AppEntry list filtered to apps where the
// binary was found on disk. The TUI uses this to show "you have X
// installed, here's whether we intercept it".
func (s SetupState) DetectedApps() []AppEntry {
	out := make([]AppEntry, 0, len(s.Apps))
	for _, a := range s.Apps {
		if a.Detected {
			out = append(out, a)
		}
	}
	return out
}

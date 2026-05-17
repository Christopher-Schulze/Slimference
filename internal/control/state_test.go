package control

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/control/apps"
)

type fakeCAProbe struct {
	state CAState
	calls atomic.Int32
}

func (f *fakeCAProbe) ProbeCA(ctx context.Context) CAState {
	f.calls.Add(1)
	return f.state
}

type fakeDaemonProbe struct{ s DaemonState }

func (f *fakeDaemonProbe) ProbeDaemon(ctx context.Context) DaemonState { return f.s }

type fakeListenerProbe struct{ s ListenerState }

func (f *fakeListenerProbe) ProbeListener(ctx context.Context) ListenerState { return f.s }

type fakeNetworkProbe struct{ s NetworkState }

func (f *fakeNetworkProbe) ProbeNetwork(ctx context.Context) NetworkState { return f.s }

type fakeIndistProbe struct{ s IndistState }

func (f *fakeIndistProbe) ProbeIndist(ctx context.Context) IndistState { return f.s }

type fakeAppsProbe struct{ s []AppEntry }

func (f *fakeAppsProbe) ProbeApps(ctx context.Context) []AppEntry { return f.s }

type fakeSavingsProbe struct{ s SavingsSummary }

func (f *fakeSavingsProbe) ProbeSavings(ctx context.Context) SavingsSummary { return f.s }

func TestBuildEveryProbeFires(t *testing.T) {
	ca := &fakeCAProbe{state: CAState{Installed: true, InKeychain: true, Fingerprint: "abc"}}
	daemon := &fakeDaemonProbe{s: DaemonState{Running: true, HealthOK: true, PID: 123}}
	listener := &fakeListenerProbe{s: ListenerState{BoundOn443: true, Method: "privileged-port"}}
	network := &fakeNetworkProbe{s: NetworkState{HostsActive: true, HostsEntries: []string{"chatgpt.com"}}}
	indist := &fakeIndistProbe{s: IndistState{GoldenLocked: true}}
	appsP := &fakeAppsProbe{s: []AppEntry{{ID: apps.AppCodexCLI, Enabled: true, Detected: true}}}
	savings := &fakeSavingsProbe{s: SavingsSummary{InputTokensSaved: 1000}}
	clock := func() time.Time { return time.Unix(99, 0) }

	state := Build(context.Background(), Probes{
		CA: ca, Daemon: daemon, Listener: listener, NetworkRedir: network,
		Indist: indist, Apps: appsP, Savings: savings, Clock: clock,
	})

	if state.CA.Fingerprint != "abc" {
		t.Errorf("CA not populated: %+v", state.CA)
	}
	if state.Daemon.PID != 123 {
		t.Errorf("daemon: %+v", state.Daemon)
	}
	if !state.Listener.BoundOn443 {
		t.Errorf("listener: %+v", state.Listener)
	}
	if !state.NetworkRedir.HostsActive {
		t.Errorf("network: %+v", state.NetworkRedir)
	}
	if !state.Indist.GoldenLocked {
		t.Errorf("indist: %+v", state.Indist)
	}
	if len(state.Apps) != 1 || state.Apps[0].ID != apps.AppCodexCLI {
		t.Errorf("apps: %+v", state.Apps)
	}
	if state.Savings.InputTokensSaved != 1000 {
		t.Errorf("savings: %+v", state.Savings)
	}
	if state.UpdatedAt != time.Unix(99, 0) {
		t.Errorf("UpdatedAt: %v", state.UpdatedAt)
	}
}

func TestBuildAllProbesNilYieldsZeroState(t *testing.T) {
	state := Build(context.Background(), Probes{})
	if state.CA.Installed {
		t.Errorf("nil probes should leave CA zero, got %+v", state.CA)
	}
	if state.IsHealthy() {
		t.Errorf("zero state must not be healthy")
	}
}

func TestBuildPartialProbesPopulateOnlyTheirField(t *testing.T) {
	ca := &fakeCAProbe{state: CAState{Installed: true, InKeychain: true}}
	state := Build(context.Background(), Probes{CA: ca})
	if !state.CA.Installed {
		t.Errorf("CA probe ran but field empty")
	}
	if state.Daemon.Running {
		t.Errorf("daemon should remain zero with no probe")
	}
}

func TestBuildHonoursClockDefault(t *testing.T) {
	before := time.Now()
	state := Build(context.Background(), Probes{})
	if state.UpdatedAt.Before(before) {
		t.Errorf("default clock should produce UpdatedAt >= now")
	}
}

func TestIsHealthyAllGood(t *testing.T) {
	s := SetupState{
		CA:           CAState{Installed: true, InKeychain: true},
		Daemon:       DaemonState{Running: true, HealthOK: true},
		Listener:     ListenerState{BoundOn443: true},
		NetworkRedir: NetworkState{HostsActive: true},
	}
	if !s.IsHealthy() {
		t.Errorf("expected healthy")
	}
}

func TestIsHealthyMissingCA(t *testing.T) {
	s := SetupState{
		CA:           CAState{Installed: false, InKeychain: true},
		Daemon:       DaemonState{Running: true, HealthOK: true},
		Listener:     ListenerState{BoundOn443: true},
		NetworkRedir: NetworkState{HostsActive: true},
	}
	if s.IsHealthy() {
		t.Errorf("missing CA installed → unhealthy")
	}
}

func TestIsHealthyMissingKeychain(t *testing.T) {
	s := SetupState{
		CA:           CAState{Installed: true, InKeychain: false},
		Daemon:       DaemonState{Running: true, HealthOK: true},
		Listener:     ListenerState{BoundOn443: true},
		NetworkRedir: NetworkState{HostsActive: true},
	}
	if s.IsHealthy() {
		t.Errorf("missing keychain → unhealthy")
	}
}

func TestIsHealthyDaemonNotRunning(t *testing.T) {
	s := SetupState{
		CA:           CAState{Installed: true, InKeychain: true},
		Daemon:       DaemonState{Running: false, HealthOK: true},
		Listener:     ListenerState{BoundOn443: true},
		NetworkRedir: NetworkState{HostsActive: true},
	}
	if s.IsHealthy() {
		t.Errorf("daemon down → unhealthy")
	}
}

func TestIsHealthyDaemonUnhealthy(t *testing.T) {
	s := SetupState{
		CA:           CAState{Installed: true, InKeychain: true},
		Daemon:       DaemonState{Running: true, HealthOK: false},
		Listener:     ListenerState{BoundOn443: true},
		NetworkRedir: NetworkState{HostsActive: true},
	}
	if s.IsHealthy() {
		t.Errorf("daemon unhealthy → not healthy")
	}
}

func TestIsHealthyListenerOnSNIPeekOK(t *testing.T) {
	// Either direct 443 or the unprivileged SNI-peek listener is acceptable.
	s := SetupState{
		CA:           CAState{Installed: true, InKeychain: true},
		Daemon:       DaemonState{Running: true, HealthOK: true},
		Listener:     ListenerState{BoundOn443: false, BoundOn8990: true, BoundOnSNIPeek: true},
		NetworkRedir: NetworkState{HostsActive: true},
	}
	if !s.IsHealthy() {
		t.Errorf("SNI-peek listener should be healthy")
	}
}

func TestIsHealthyAdminOnlyListenerFails(t *testing.T) {
	s := SetupState{
		CA:           CAState{Installed: true, InKeychain: true},
		Daemon:       DaemonState{Running: true, HealthOK: true},
		Listener:     ListenerState{BoundOn443: false, BoundOn8990: true, BoundOnSNIPeek: false},
		NetworkRedir: NetworkState{HostsActive: true},
	}
	if s.IsHealthy() {
		t.Errorf("admin-only listener must not count as transparent MITM healthy")
	}
}

func TestIsHealthyListenerNoneFails(t *testing.T) {
	s := SetupState{
		CA:           CAState{Installed: true, InKeychain: true},
		Daemon:       DaemonState{Running: true, HealthOK: true},
		Listener:     ListenerState{BoundOn443: false, BoundOn8990: false, BoundOnSNIPeek: false},
		NetworkRedir: NetworkState{HostsActive: true},
	}
	if s.IsHealthy() {
		t.Errorf("no listener → unhealthy")
	}
}

func TestIsHealthyNetworkPFCtlOK(t *testing.T) {
	s := SetupState{
		CA:           CAState{Installed: true, InKeychain: true},
		Daemon:       DaemonState{Running: true, HealthOK: true},
		Listener:     ListenerState{BoundOn443: true},
		NetworkRedir: NetworkState{HostsActive: false, PFCtlActive: true},
	}
	if !s.IsHealthy() {
		t.Errorf("pfctl-only redirect should still be healthy")
	}
}

func TestIsHealthyNoNetworkRedirectFails(t *testing.T) {
	s := SetupState{
		CA:           CAState{Installed: true, InKeychain: true},
		Daemon:       DaemonState{Running: true, HealthOK: true},
		Listener:     ListenerState{BoundOn443: true},
		NetworkRedir: NetworkState{HostsActive: false, PFCtlActive: false},
	}
	if s.IsHealthy() {
		t.Errorf("no network redirect → unhealthy")
	}
}

func TestEnabledAppsFiltersByEnabled(t *testing.T) {
	s := SetupState{Apps: []AppEntry{
		{ID: apps.AppCodexCLI, Enabled: true},
		{ID: apps.AppCodexDesktop, Enabled: false},
		{ID: apps.AppClaudeCode, Enabled: true},
	}}
	got := s.EnabledApps()
	if len(got) != 2 {
		t.Errorf("expected 2 enabled, got %d", len(got))
	}
}

func TestEnabledAppsEmpty(t *testing.T) {
	if got := (SetupState{}).EnabledApps(); len(got) != 0 {
		t.Errorf("got %d", len(got))
	}
}

func TestDetectedAppsFilters(t *testing.T) {
	s := SetupState{Apps: []AppEntry{
		{ID: apps.AppCodexCLI, Detected: true},
		{ID: apps.AppClaudeCode, Detected: false},
	}}
	if got := s.DetectedApps(); len(got) != 1 || got[0].ID != apps.AppCodexCLI {
		t.Errorf("got %+v", got)
	}
}

func TestBuildProbesRunInParallel(t *testing.T) {
	// Each probe sleeps 30 ms. If sequential, total is 7 * 30 = 210
	// ms. If parallel, ~30-50 ms. Assert <120 ms wall clock.
	slow := func() time.Time { time.Sleep(30 * time.Millisecond); return time.Unix(0, 0) }
	p := Probes{
		CA:           sleepProbe{slow},
		Daemon:       sleepProbe{slow},
		Listener:     sleepProbe{slow},
		NetworkRedir: sleepProbe{slow},
		Indist:       sleepProbe{slow},
		Apps:         sleepProbe{slow},
		Savings:      sleepProbe{slow},
	}
	t0 := time.Now()
	_ = Build(context.Background(), p)
	if d := time.Since(t0); d > 120*time.Millisecond {
		t.Errorf("Build took %v - probes appear sequential", d)
	}
}

// sleepProbe satisfies every probe interface; calls clock() once
// per ProbeX so we can measure parallelism.
type sleepProbe struct{ clock func() time.Time }

func (s sleepProbe) ProbeCA(ctx context.Context) CAState         { s.clock(); return CAState{} }
func (s sleepProbe) ProbeDaemon(ctx context.Context) DaemonState { s.clock(); return DaemonState{} }
func (s sleepProbe) ProbeListener(ctx context.Context) ListenerState {
	s.clock()
	return ListenerState{}
}
func (s sleepProbe) ProbeNetwork(ctx context.Context) NetworkState { s.clock(); return NetworkState{} }
func (s sleepProbe) ProbeIndist(ctx context.Context) IndistState   { s.clock(); return IndistState{} }
func (s sleepProbe) ProbeApps(ctx context.Context) []AppEntry      { s.clock(); return nil }
func (s sleepProbe) ProbeSavings(ctx context.Context) SavingsSummary {
	s.clock()
	return SavingsSummary{}
}

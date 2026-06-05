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

type fakeWSSProbe struct{ s WSSState }

func (f fakeWSSProbe) ProbeWSS(ctx context.Context) WSSState { return f.s }

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

func TestSavingsSummaryProductSignalsSaving(t *testing.T) {
	summary := SavingsSummary{
		BillableInputTokensSaved: 2000,
		OutputWireBytesSaved:     512,
		RequestSideBytesReduced:  128,
		CostUSD:                  0.012,
		ProxyLayer0ReadDelta:     3,
		ProxyLayer0Repeated:      2,
		ProxyLayer0ChunkDedup:    1,
		ToolPruneTokensSaved:     26,
		ToolPrunePrunedTools:     1,
		ToolPruneReattached:      1,
		ProxyLayer0Cache: []ProxyLayer0CacheEntry{
			{Action: "hit", Count: 4},
			{Action: "miss", Count: 5},
		},
	}

	got := summary.ProductSignals()
	if got.Status != "saving" {
		t.Fatalf("Status=%q want saving", got.Status)
	}
	if got.BillableInputTokensSaved != 2000 ||
		got.OutputWireBytesSaved != 512 ||
		got.RequestSideBytesReduced != 128 ||
		got.CostUSD != 0.012 {
		t.Fatalf("savings fields mismatch: %+v", got)
	}
	if got.CacheHits != 4 || got.CacheMisses != 5 {
		t.Fatalf("cache fields mismatch: %+v", got)
	}
	if got.ReadDeltaHits != 3 || got.RepeatedOutputHits != 2 || got.ChunkDedupHits != 1 {
		t.Fatalf("mechanism hits mismatch: %+v", got)
	}
	if got.ToolPruneTokensSaved != 26 || got.ToolPrunePrunedTools != 1 || got.ToolPruneReattached != 1 {
		t.Fatalf("tool prune product signals mismatch: %+v", got)
	}
}

func TestSavingsSummaryProductSignalsStatusPriority(t *testing.T) {
	tests := []struct {
		name string
		in   SavingsSummary
		want string
	}{
		{name: "idle", in: SavingsSummary{}, want: "idle"},
		{
			name: "active without savings",
			in: SavingsSummary{
				ProxyLayer0ToolResults: 1,
				ProxyLayer0Cache: []ProxyLayer0CacheEntry{
					{Action: "miss", Count: 1},
				},
			},
			want: "active_no_savings",
		},
		{
			name: "attention beats savings",
			in: SavingsSummary{
				BillableInputTokensSaved: 100,
				ProxyLayer0CommandMisses: 1,
			},
			want: "attention",
		},
		{
			name: "rollback is safety issue",
			in:   SavingsSummary{QualityABRolledBack: true},
			want: "attention",
		},
		{
			name: "tool prune miss is attention",
			in: SavingsSummary{
				ToolPruneTokensSaved: 20,
				ToolPruneMisses:      1,
			},
			want: "attention",
		},
		{
			name: "output reduce injection without savings is active",
			in: SavingsSummary{
				OutputReduceInjectedTurns: 1,
				OutputReduceInputOverhead: 9,
			},
			want: "active_no_savings",
		},
		{
			name: "proof analytics drop is attention",
			in: SavingsSummary{
				BillableInputTokensSaved:    100,
				AnalyticsProofEventsDropped: 1,
			},
			want: "attention",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.ProductSignals().Status; got != tt.want {
				t.Fatalf("Status=%q want %q", got, tt.want)
			}
		})
	}
}

func TestSavingsSummaryProductSignalsWithHostBudgetMarksAttention(t *testing.T) {
	summary := SavingsSummary{BillableInputTokensSaved: 100}
	got := summary.ProductSignalsWithHostBudget(HostBudgetState{
		Status:   "attention",
		Exceeded: true,
		Reasons:  []string{"wss_parse_or_degrade"},
	})
	if got.Status != "attention" || got.SafetyIssues == 0 {
		t.Fatalf("host budget attention must surface in product signals: %+v", got)
	}
}

func TestEvaluateHostBudgetStatus(t *testing.T) {
	tests := []struct {
		name    string
		daemon  DaemonState
		wss     WSSState
		want    string
		reasons int
	}{
		{name: "unknown without probes", want: "unknown"},
		{
			name: "unknown resources with active WSS",
			wss:  WSSState{EngineActive: true, MutationActive: true},
			want: "unknown",
		},
		{
			name:   "unknown without rss even if state measured",
			daemon: DaemonState{StateBytes: 1024},
			wss:    WSSState{EngineActive: true},
			want:   "unknown",
		},
		{
			name:   "ok under budget",
			daemon: DaemonState{RSSBytes: 64 * 1024 * 1024, DiskReadOps: 7, DiskWriteOps: 11},
			wss:    WSSState{EngineActive: true, MutationActive: true},
			want:   "ok",
		},
		{
			name:    "rss exceeded",
			daemon:  DaemonState{RSSBytes: DefaultHostRSSBudgetBytes + 1},
			wss:     WSSState{EngineActive: true},
			want:    "attention",
			reasons: 1,
		},
		{
			name:    "state exceeded",
			daemon:  DaemonState{RSSBytes: 64 * 1024 * 1024, StateBytes: DefaultHostStateBudgetBytes + 1},
			wss:     WSSState{EngineActive: true},
			want:    "attention",
			reasons: 1,
		},
		{
			name:    "cpu window exceeded",
			daemon:  DaemonState{RSSBytes: 64 * 1024 * 1024, CPUWindowPercent: DefaultHostCPUWindowBudgetPercent + 0.1, CPUWindowSeconds: DefaultHostCPUWindowMinSampleSeconds},
			wss:     WSSState{EngineActive: true},
			want:    "attention",
			reasons: 1,
		},
		{
			name:   "short cpu window ignored",
			daemon: DaemonState{RSSBytes: 64 * 1024 * 1024, CPUWindowPercent: DefaultHostCPUWindowBudgetPercent * 10, CPUWindowSeconds: DefaultHostCPUWindowMinSampleSeconds / 2},
			wss:    WSSState{EngineActive: true},
			want:   "ok",
		},
		{
			name:    "disk write window exceeded",
			daemon:  DaemonState{RSSBytes: 64 * 1024 * 1024, DiskWriteOpsDelta: DefaultHostDiskWriteOpsWindowBudget + 1},
			wss:     WSSState{EngineActive: true},
			want:    "attention",
			reasons: 1,
		},
		{
			name: "wss errors exceeded",
			wss: WSSState{
				EngineActive:      true,
				CompressionErrors: 1,
				ParseFailures:     1,
			},
			want:    "attention",
			reasons: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateHostBudget(tt.daemon, tt.wss)
			if got.Status != tt.want {
				t.Fatalf("Status=%q want %q: %+v", got.Status, tt.want, got)
			}
			if tt.daemon.DiskReadOps != 0 || tt.daemon.DiskWriteOps != 0 {
				if got.DiskReadOps != tt.daemon.DiskReadOps || got.DiskWriteOps != tt.daemon.DiskWriteOps {
					t.Fatalf("disk counters not propagated: %+v", got)
				}
			}
			if len(got.Reasons) != tt.reasons {
				t.Fatalf("Reasons=%v want count %d", got.Reasons, tt.reasons)
			}
		})
	}
}

func TestBuildPopulatesHostBudget(t *testing.T) {
	state := Build(context.Background(), Probes{
		Daemon: &fakeDaemonProbe{s: DaemonState{RSSBytes: DefaultHostRSSBudgetBytes + 1}},
		WSS:    fakeWSSProbe{s: WSSState{EngineActive: true}},
	})
	if !state.HostBudget.Exceeded || state.HostBudget.Status != "attention" {
		t.Fatalf("HostBudget not populated from daemon/wss: %+v", state.HostBudget)
	}
}

func TestBuildPropagatesWSSHostBudgetIntoProductSignals(t *testing.T) {
	state := Build(context.Background(), Probes{
		Savings: &fakeSavingsProbe{s: SavingsSummary{BillableInputTokensSaved: 100}},
		WSS: fakeWSSProbe{s: WSSState{
			EngineActive:      true,
			ParseFailures:     1,
			CompressionErrors: 1,
		}},
	})
	if state.Savings.Product.Status != "attention" || state.Savings.Product.SafetyIssues == 0 {
		t.Fatalf("product signals did not surface WSS safety: %+v", state.Savings.Product)
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

func TestIsHealthyDoesNotRequireKeychainTrust(t *testing.T) {
	s := SetupState{
		CA:           CAState{Installed: true, InKeychain: false},
		Daemon:       DaemonState{Running: true, HealthOK: true},
		Listener:     ListenerState{BoundOn443: true},
		NetworkRedir: NetworkState{HostsActive: true},
	}
	if !s.IsHealthy() {
		t.Errorf("Keychain trust is not part of aggregate health")
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

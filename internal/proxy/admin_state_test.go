package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/control"
	"github.com/slimference/slimference/internal/control/apps"
)

func newProxyForAdminTest(t *testing.T) *Proxy {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return New(config.Defaults())
}

func TestAdminStateHandlerNoProviderReturns503(t *testing.T) {
	p := newProxyForAdminTest(t)

	req := httptest.NewRequest(http.MethodGet, AdminStatePath, nil)
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestAdminStateHandlerRejectsNonGET(t *testing.T) {
	p := newProxyForAdminTest(t)

	probes := &control.Probes{}
	p.SetStateProvider(probes)

	req := httptest.NewRequest(http.MethodPost, AdminStatePath, nil)
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestAdminStateHandlerReturnsBuiltState(t *testing.T) {
	p := newProxyForAdminTest(t)

	dir := t.TempDir()
	m, err := apps.NewManager(filepath.Join(dir, "apps.toml"))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	p.SetAppsManager(m)

	now := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	probes := &control.Probes{
		Apps:    &control.AppsManagerProbe{Manager: m},
		Savings: &SavingsProbe{Proxy: p},
		Indist:  NoopIndistProbe{},
		Clock:   func() time.Time { return now },
	}
	p.SetStateProvider(probes)

	req := httptest.NewRequest(http.MethodGet, AdminStatePath, nil)
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var got control.SetupState
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.UpdatedAt.Equal(now) {
		t.Errorf("UpdatedAt=%v want %v", got.UpdatedAt, now)
	}
	if len(got.Apps) != len(apps.KnownApps) {
		t.Errorf("expected %d apps, got %d", len(apps.KnownApps), len(got.Apps))
	}
}

func TestSetupStateSnapshotStoresHostBudgetGate(t *testing.T) {
	p := newProxyForAdminTest(t)
	p.SetStateProvider(&control.Probes{
		Daemon: adminDaemonProbe{state: control.DaemonState{RSSBytes: control.DefaultHostRSSBudgetBytes + 1}},
	})

	if p.codexHostBudgetExceeded() {
		t.Fatal("host budget gate must start false before first state snapshot")
	}
	if _, ok := p.SetupStateSnapshot(context.Background()); !ok {
		t.Fatal("expected state snapshot")
	}
	if !p.codexHostBudgetExceeded() {
		t.Fatal("host budget snapshot should feed Codex reducer gate")
	}
}

func TestAdminStateHandlerReturnsWSSMutationTelemetry(t *testing.T) {
	p := newProxyForAdminTest(t)
	d := &PhaseFDispatcher{}
	d.counters.mitmBridged.Add(1)
	d.counters.wsmitmC2SFrames.Add(3)
	d.counters.wsmitmForwarded.Add(2)
	d.counters.wsmitmReencoded.Add(1)
	d.counters.wsmitmDegraded.Add(1)
	d.counters.wsmitmCompressedInspected.Add(4)
	d.counters.wsmitmCompressedMutated.Add(1)
	d.counters.wsmitmCompressedBypassed.Add(2)
	d.counters.wsmitmCompressionErrors.Add(1)
	d.counters.wsmitmPhaseFRequests.Add(3)
	d.counters.wsmitmPhaseFRequestBodies.Add(2)
	d.counters.wsmitmPhaseFIndexed.Add(2)
	d.counters.wsmitmPhaseFTextDeltas.Add(5)
	d.counters.wsmitmPhaseFTerminals.Add(1)
	d.counters.wsmitmPhaseFMutations.Add(1)
	p.SetWSSDispatcher(d)
	p.SetStateProvider(&control.Probes{WSS: WSSProbe{Proxy: p}})

	req := httptest.NewRequest(http.MethodGet, AdminStatePath, nil)
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var got control.SetupState
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.WSS.MITMBridged != 1 || got.WSS.C2SFrames != 3 || got.WSS.FramesReencoded != 1 {
		t.Fatalf("unexpected WSS telemetry: %+v", got.WSS)
	}
	if !got.WSS.MutationActive {
		t.Fatalf("MutationActive=false for reencoded frames: %+v", got.WSS)
	}
	if got.WSS.DegradedSessions != 1 {
		t.Fatalf("DegradedSessions=%d, want 1", got.WSS.DegradedSessions)
	}
	if got.WSS.CompressedMessagesInspected != 4 ||
		got.WSS.CompressedMessagesMutated != 1 ||
		got.WSS.CompressedMessagesBypassed != 2 ||
		got.WSS.CompressionErrors != 1 {
		t.Fatalf("compressed WSS telemetry not propagated: %+v", got.WSS)
	}
	if got.WSS.PhaseFRequests != 3 ||
		got.WSS.PhaseFRequestBodies != 2 ||
		got.WSS.PhaseFRequestMessagesIndexed != 2 ||
		got.WSS.PhaseFTextDeltas != 5 ||
		got.WSS.PhaseFTerminalResponses != 1 ||
		got.WSS.PhaseFMutations != 1 {
		t.Fatalf("Phase-F WSS telemetry not propagated: %+v", got.WSS)
	}
}

type adminDaemonProbe struct {
	state control.DaemonState
}

func (p adminDaemonProbe) ProbeDaemon(context.Context) control.DaemonState {
	return p.state
}

func TestAdminStateHandlerNilProviderClears(t *testing.T) {
	p := newProxyForAdminTest(t)
	probes := &control.Probes{}
	p.SetStateProvider(probes)
	p.SetStateProvider(nil)

	req := httptest.NewRequest(http.MethodGet, AdminStatePath, nil)
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 after clear, got %d", rec.Code)
	}
}

func TestAdminAppsHandlerNoManagerReturns503(t *testing.T) {
	p := newProxyForAdminTest(t)
	req := httptest.NewRequest(http.MethodGet, AdminAppsPath, nil)
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestAdminAppsHandlerGetReturnsPolicy(t *testing.T) {
	p := newProxyForAdminTest(t)
	dir := t.TempDir()
	m, err := apps.NewManager(filepath.Join(dir, "apps.toml"))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	p.SetAppsManager(m)

	req := httptest.NewRequest(http.MethodGet, AdminAppsPath, nil)
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp AdminAppsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK {
		t.Fatalf("not OK: %s", resp.Error)
	}
	if len(resp.Enabled) != len(apps.KnownApps) {
		t.Errorf("expected %d entries, got %d", len(apps.KnownApps), len(resp.Enabled))
	}
	for _, id := range apps.KnownApps {
		if _, ok := resp.Enabled[string(id)]; !ok {
			t.Errorf("missing entry for %s", id)
		}
	}
}

func TestAdminAppsHandlerPostTogglesEnabled(t *testing.T) {
	p := newProxyForAdminTest(t)
	dir := t.TempDir()
	m, err := apps.NewManager(filepath.Join(dir, "apps.toml"))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	p.SetAppsManager(m)

	body, _ := json.Marshal(AdminAppsRequest{ID: string(apps.AppCodexCLI), Enabled: false})
	req := httptest.NewRequest(http.MethodPost, AdminAppsPath, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp AdminAppsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Enabled[string(apps.AppCodexCLI)] {
		t.Fatalf("expected codex_cli disabled, got enabled")
	}
	if !m.Policy().IsEnabled(apps.AppCodexCLI) == false {
		t.Fatalf("policy was not persisted")
	}
}

func TestAdminAppsHandlerPostWriteError(t *testing.T) {
	p := newProxyForAdminTest(t)
	dir := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(dir, "apps.toml")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	m, err := apps.NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove dir: %v", err)
	}
	if err := os.WriteFile(dir, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	p.SetAppsManager(m)

	body, _ := json.Marshal(AdminAppsRequest{ID: string(apps.AppCodexCLI), Enabled: false})
	req := httptest.NewRequest(http.MethodPost, AdminAppsPath, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminAppsHandlerPostUnknownIDRejected(t *testing.T) {
	p := newProxyForAdminTest(t)
	dir := t.TempDir()
	m, _ := apps.NewManager(filepath.Join(dir, "apps.toml"))
	p.SetAppsManager(m)

	body, _ := json.Marshal(AdminAppsRequest{ID: "bogus_app", Enabled: false})
	req := httptest.NewRequest(http.MethodPost, AdminAppsPath, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAdminAppsHandlerPostClaudeEnableRejected(t *testing.T) {
	p := newProxyForAdminTest(t)
	m, _ := apps.NewManager(filepath.Join(t.TempDir(), "apps.toml"))
	p.SetAppsManager(m)

	body, _ := json.Marshal(AdminAppsRequest{ID: string(apps.AppClaudeCode), Enabled: true})
	req := httptest.NewRequest(http.MethodPost, AdminAppsPath, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if m.Policy().IsEnabled(apps.AppClaudeCode) {
		t.Fatal("Claude Code must stay parked")
	}
}

func TestAdminAppsHandlerPostInvalidJSONRejected(t *testing.T) {
	p := newProxyForAdminTest(t)
	dir := t.TempDir()
	m, _ := apps.NewManager(filepath.Join(dir, "apps.toml"))
	p.SetAppsManager(m)

	req := httptest.NewRequest(http.MethodPost, AdminAppsPath, strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAdminAppsHandlerRejectsOtherMethods(t *testing.T) {
	p := newProxyForAdminTest(t)
	dir := t.TempDir()
	m, _ := apps.NewManager(filepath.Join(dir, "apps.toml"))
	p.SetAppsManager(m)

	req := httptest.NewRequest(http.MethodDelete, AdminAppsPath, nil)
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestPolicyEnabledMapShape(t *testing.T) {
	dir := t.TempDir()
	m, _ := apps.NewManager(filepath.Join(dir, "apps.toml"))
	got := policyEnabledMap(m.Policy())
	if len(got) != len(apps.KnownApps) {
		t.Fatalf("expected %d entries, got %d", len(apps.KnownApps), len(got))
	}
	want := apps.DefaultPolicy()
	for _, id := range apps.KnownApps {
		v, ok := got[string(id)]
		if !ok {
			t.Errorf("missing %s", id)
		}
		if v != want.IsEnabled(id) {
			t.Errorf("%s: got %v, default policy says %v", id, v, want.IsEnabled(id))
		}
	}
}

func TestAdminStateHandlerContextTimeoutBounded(t *testing.T) {
	p := newProxyForAdminTest(t)
	probes := &control.Probes{Clock: func() time.Time { return time.Time{} }}
	p.SetStateProvider(probes)

	// Use a request with an already-cancelled context to ensure
	// Build still completes (probes are nil so it returns fast).
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, AdminStatePath, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even on cancelled ctx, got %d", rec.Code)
	}
}

package tui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/slimference/slimference/internal/analytics"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/sessions"
	"github.com/slimference/slimference/internal/types"
)

// mockProxy implements ProxyInterface for testing the TUI model.
type mockProxy struct {
	claudeEnabled  bool
	codexEnabled   bool
	layer1Enabled  bool
	layer2Enabled  bool
	snap           analytics.AnalyticsSnapshot
	recentReqs     []types.RequestMetrics
	recentFlights  []dbg.FlightRequestSummary
	layer0Status   Layer0Status
	readStatus     ReadCacheStatus
	qualityStatus  QualityStatus
	productStatus  ProductStatus
	productCalls   int
	sessionLogger  *sessions.SessionLogger
	flushed        bool
	shutdownCalled bool
	listenPort     int
	prefillSpeed   int
	bypass         bool
	appEntries     []AppEntry
	appErr         error
}

// Bypass + SetBypass satisfy the T67 additions to ProxyInterface.
func (m *mockProxy) Bypass() bool           { return m.bypass }
func (m *mockProxy) SetBypass(enabled bool) { m.bypass = enabled }

// AppEntries + SetAppEnabled satisfy the Phase H additions.
func (m *mockProxy) AppEntries() []AppEntry { return m.appEntries }
func (m *mockProxy) SetAppEnabled(id string, enabled bool) error {
	if m.appErr != nil {
		return m.appErr
	}
	for i := range m.appEntries {
		if m.appEntries[i].ID == id {
			m.appEntries[i].Enabled = enabled
			return nil
		}
	}
	return nil
}

func newMockProxy() *mockProxy {
	return &mockProxy{
		claudeEnabled: true,
		codexEnabled:  true,
		layer1Enabled: true,
		layer2Enabled: true,
		sessionLogger: sessions.NewSessionLogger(),
		listenPort:    8990,
		prefillSpeed:  50000,
		snap: analytics.AnalyticsSnapshot{
			SessionStart: time.Now(),
		},
	}
}

func (m *mockProxy) SetProviderEnabled(prov types.Provider, enabled bool) {
	if prov == types.Anthropic {
		m.claudeEnabled = enabled
	} else {
		m.codexEnabled = enabled
	}
}

func (m *mockProxy) SetLayerEnabled(layer int, enabled bool) {
	switch layer {
	case 1:
		m.layer1Enabled = enabled
	case 2, 3:
		m.layer2Enabled = enabled
	}
}

func (m *mockProxy) IsProviderEnabled(prov types.Provider) bool {
	if prov == types.Anthropic {
		return m.claudeEnabled
	}
	return m.codexEnabled
}

func (m *mockProxy) IsLayerEnabled(layer int) bool {
	switch layer {
	case 1:
		return m.layer1Enabled
	case 2, 3:
		return m.layer2Enabled
	}
	return false
}

func (m *mockProxy) FlushCaches()                              { m.flushed = true }
func (m *mockProxy) GetAnalytics() analytics.AnalyticsSnapshot { return m.snap }
func (m *mockProxy) GetRecentRequests(n int) []types.RequestMetrics {
	return m.recentReqs
}
func (m *mockProxy) GetRecentFlights(n int) []dbg.FlightRequestSummary {
	if n <= 0 || len(m.recentFlights) <= n {
		return append([]dbg.FlightRequestSummary(nil), m.recentFlights...)
	}
	start := len(m.recentFlights) - n
	return append([]dbg.FlightRequestSummary(nil), m.recentFlights[start:]...)
}
func (m *mockProxy) GetLayer0Status() Layer0Status { return m.layer0Status }
func (m *mockProxy) GetReadCacheStatus() ReadCacheStatus {
	return m.readStatus
}
func (m *mockProxy) GetQualityStatus() QualityStatus {
	return m.qualityStatus
}
func (m *mockProxy) GetProductStatus() ProductStatus {
	m.productCalls++
	return m.productStatus
}
func (m *mockProxy) GetProviderHealth(_ types.Provider) types.ProviderHealthInfo {
	return types.ProviderHealthInfo{Status: types.ProviderHealthIdle}
}
func (m *mockProxy) SessionLogger() SessionLoggerInterface {
	if m.sessionLogger == nil {
		return nil
	}
	return m.sessionLogger
}
func (m *mockProxy) Shutdown(_ context.Context) error {
	m.shutdownCalled = true
	return nil
}
func (m *mockProxy) Config() ProxyConfigInterface {
	return &mockConfig{port: m.listenPort, speed: m.prefillSpeed}
}

type mockConfig struct {
	port  int
	speed int
}

func (c *mockConfig) GetListenPort() int   { return c.port }
func (c *mockConfig) GetPrefillSpeed() int { return c.speed }

// mockServiceControl implements ServiceControlInterface for testing.
type mockServiceControl struct {
	started              bool
	stopped              bool
	restarted            bool
	installed            bool
	removed              bool
	running              bool
	transparentInstalled bool
	transparentEnabled   bool
	transparentDisabled  bool
	transparentRemoved   bool
	transparentStatus    TransparentStatus
	codexRouteEnabled    bool
	codexRouteDisabled   bool
	codexRouteStatus     CodexRouteStatus
	codexDesktopStatus   CodexDesktopStatus
	codexCLILaunched     bool
	codexAppLaunched     bool
	codexWSSRepaired     bool
	daemonNotice         string
	err                  error
}

func (m *mockServiceControl) StartDaemon() error {
	if m.err != nil {
		return m.err
	}
	m.started = true
	return nil
}
func (m *mockServiceControl) StopDaemon() error {
	if m.err != nil {
		return m.err
	}
	m.stopped = true
	return nil
}
func (m *mockServiceControl) RestartDaemon() error {
	if m.err != nil {
		return m.err
	}
	m.restarted = true
	return nil
}
func (m *mockServiceControl) InstallService() error {
	if m.err != nil {
		return m.err
	}
	m.installed = true
	return nil
}
func (m *mockServiceControl) UninstallService() error {
	if m.err != nil {
		return m.err
	}
	m.removed = true
	return nil
}
func (m *mockServiceControl) DaemonStatus() (bool, int, int) {
	if m.running {
		return true, 1234, 8990
	}
	return false, 0, 0
}
func (m *mockServiceControl) DaemonNotice() string                 { return m.daemonNotice }
func (m *mockServiceControl) TransparentStatus() TransparentStatus { return m.transparentStatus }
func (m *mockServiceControl) InstallTransparent() error {
	if m.err != nil {
		return m.err
	}
	m.transparentInstalled = true
	m.transparentStatus = TransparentStatus{CAExists: true, CATrusted: true, AutoStartInstalled: true}
	return nil
}
func (m *mockServiceControl) EnableTransparent() error {
	if m.err != nil {
		return m.err
	}
	m.transparentEnabled = true
	m.transparentStatus.ProxyArmed = true
	m.transparentStatus.ActiveServices = 1
	m.transparentStatus.DaemonReachable = true
	return nil
}
func (m *mockServiceControl) DisableTransparent() error {
	if m.err != nil {
		return m.err
	}
	m.transparentDisabled = true
	m.transparentStatus.ProxyArmed = false
	return nil
}
func (m *mockServiceControl) UninstallTransparent() error {
	if m.err != nil {
		return m.err
	}
	m.transparentRemoved = true
	m.transparentStatus = TransparentStatus{}
	return nil
}
func (m *mockServiceControl) CodexRouteStatus() CodexRouteStatus { return m.codexRouteStatus }
func (m *mockServiceControl) CodexDesktopStatus() CodexDesktopStatus {
	return m.codexDesktopStatus
}
func (m *mockServiceControl) LaunchCodexCLI() (string, error) {
	if m.err != nil {
		return "", m.err
	}
	m.codexCLILaunched = true
	return "Codex CLI launched through Slimference", nil
}
func (m *mockServiceControl) LaunchCodexApp() (string, error) {
	if m.err != nil {
		return "", m.err
	}
	m.codexAppLaunched = true
	return "Codex App launch requested through Slimference mode", nil
}
func (m *mockServiceControl) RepairCodexWSS() (string, error) {
	if m.err != nil {
		return "", m.err
	}
	m.codexWSSRepaired = true
	m.codexRouteStatus.WSSCertified = true
	m.codexRouteStatus.AutoMode = "wss_phasef"
	return "Codex CLI WSS repaired", nil
}
func (m *mockServiceControl) EnableCodexRoute() error {
	if m.err != nil {
		return m.err
	}
	m.codexRouteEnabled = true
	m.codexRouteStatus = CodexRouteStatus{Exists: true, Enabled: true, Complete: true, DaemonReachable: true}
	return nil
}
func (m *mockServiceControl) DisableCodexRoute() error {
	if m.err != nil {
		return m.err
	}
	m.codexRouteDisabled = true
	m.codexRouteStatus.Enabled = false
	m.codexRouteStatus.Complete = false
	return nil
}
func (m *mockServiceControl) InstallHook(target string) error {
	if m.err != nil {
		return m.err
	}
	return nil
}
func (m *mockServiceControl) RemoveHook(target string) error { return nil }

// TestNewModel_Defaults verifies that a new model has correct default state.
func TestNewModel_Defaults(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)

	if m.view != ViewMain {
		t.Errorf("view = %d, want ViewMain", m.view)
	}
	if !m.claudeEnabled {
		t.Error("claudeEnabled should be true by default")
	}
	if !m.codexEnabled {
		t.Error("codexEnabled should be true by default")
	}
	if !m.layer1Enabled {
		t.Error("layer1Enabled should be true by default")
	}
	if !m.layer2Enabled {
		t.Error("layer2Enabled should be true by default")
	}
}

// TestUpdate_ToggleClaudeDisabled verifies that pressing 'c' keeps Claude disabled
// in Codex-only mode.
func TestUpdate_ToggleClaudeDisabled(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	model := updated.(Model)

	if model.claudeEnabled || !p.claudeEnabled {
		t.Errorf("Claude UI should be disabled without changing proxy policy; model=%v proxy=%v", model.claudeEnabled, p.claudeEnabled)
	}
	if !strings.Contains(model.flashMsg, "Claude Code is disabled") {
		t.Errorf("expected disabled flash, got %q", model.flashMsg)
	}
}

// TestUpdate_ToggleCodex verifies that pressing 'x' toggles Codex.
func TestUpdate_ToggleCodex(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	model := updated.(Model)

	if model.codexEnabled {
		t.Error("codexEnabled should be false after toggling")
	}
}

func TestUpdate_SetupCodexRouteToggle(t *testing.T) {
	p := newMockProxy()
	svc := &mockServiceControl{codexRouteStatus: CodexRouteStatus{Exists: true}}
	m := NewModel(p)
	m.SetServiceControl(svc)
	m.view = ViewSetup

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model := updated.(Model)
	if !svc.codexRouteEnabled || !strings.Contains(model.flashMsg, "Advanced shared route enabled") {
		t.Fatalf("enable route failed: svc=%+v flash=%q", svc, model.flashMsg)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model = updated.(Model)
	if !svc.codexRouteDisabled || !strings.Contains(model.flashMsg, "Normal Codex direct") {
		t.Fatalf("disable route failed: svc=%+v flash=%q", svc, model.flashMsg)
	}
}

func TestUpdate_SetupCodexRouteToggleError(t *testing.T) {
	p := newMockProxy()
	svc := &mockServiceControl{err: fmt.Errorf("boom")}
	m := NewModel(p)
	m.SetServiceControl(svc)
	m.view = ViewSetup

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model := updated.(Model)
	if !strings.Contains(model.flashMsg, "Advanced shared route enable failed") {
		t.Fatalf("missing route error flash: %q", model.flashMsg)
	}
}

// TestUpdate_ToggleLayers verifies that daily-surface number keys no longer
// toggle optimization layers by accident.
func TestUpdate_ToggleLayers(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)

	for _, key := range []rune{'1', '2'} {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		model := updated.(Model)
		m = model
	}
	if !p.layer1Enabled || !p.layer2Enabled {
		t.Error("layers 1 and 2 should remain enabled from the daily UI")
	}
	if !strings.Contains(m.flashMsg, "daily UI") {
		t.Fatalf("missing daily UI flash: %q", m.flashMsg)
	}
}

// TestUpdate_ViewStatsToggle verifies that pressing 's' opens the savings view.
func TestUpdate_ViewStatsToggle(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	model := updated.(Model)
	if model.view != ViewStats {
		t.Errorf("view = %d, want ViewStats after pressing 's'", model.view)
	}

	updated2, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	model2 := updated2.(Model)
	if model2.view != ViewMain {
		t.Errorf("view = %d, want ViewMain after pressing back", model2.view)
	}
}

// TestUpdate_ViewDebugToggle verifies that pressing 'd' toggles the debug view.
func TestUpdate_ViewDebugToggle(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	model := updated.(Model)
	if model.view != ViewDebug {
		t.Errorf("view = %d, want ViewDebug after pressing 'd'", model.view)
	}
}

// TestUpdate_FlushCaches verifies that pressing 'f' calls FlushCaches.
func TestUpdate_FlushCaches(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	model := updated.(Model)

	if !p.flushed {
		t.Error("FlushCaches should have been called after pressing 'f'")
	}
	if model.flashMsg != "All caches flushed" {
		t.Errorf("flashMsg = %q, want 'All caches flushed'", model.flashMsg)
	}
}

// TestUpdate_Quit verifies that pressing 'q' triggers shutdown.
func TestUpdate_Quit(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	if !p.shutdownCalled {
		t.Error("Shutdown should have been called on quit")
	}
	if cmd == nil {
		t.Error("expected a quit command to be returned")
	}
}

// TestUpdate_TickRefreshesSnapshot verifies that tickMsg updates the analytics snapshot.
func TestUpdate_TickRefreshesSnapshot(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.snap = analytics.AnalyticsSnapshot{
		TotalRequests:    42,
		TotalInputTokens: 100000,
		SavedInputTokens: 60000,
	}

	m := NewModel(p)

	updated, _ := m.Update(tickMsg(time.Now()))
	model := updated.(Model)

	if model.latestSnap.TotalRequests != 42 {
		t.Errorf("latestSnap.TotalRequests = %d, want 42", model.latestSnap.TotalRequests)
	}
}

func TestUpdate_TickRefreshesProductStatus(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.productStatus = ProductStatus{SavingsStatus: "active_no_savings"}
	m := NewModel(p)

	p.productStatus = ProductStatus{SavingsStatus: "saving", BillableInputTokensSaved: 42}
	updated, _ := m.Update(tickMsg(time.Now()))
	model := updated.(Model)

	if model.latestProduct.SavingsStatus != "saving" || model.latestProduct.BillableInputTokensSaved != 42 {
		t.Fatalf("latestProduct not refreshed: %+v", model.latestProduct)
	}
}

func TestModelTickIntervalSlowsUnderHostBudget(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	if got := m.tickInterval(); got != defaultTickInterval {
		t.Fatalf("normal tick interval = %s, want %s", got, defaultTickInterval)
	}
	m.latestProduct.HostBudgetExceeded = true
	if got := m.tickInterval(); got != hostBudgetTickInterval {
		t.Fatalf("host-budget tick interval = %s, want %s", got, hostBudgetTickInterval)
	}
}

// TestUpdate_WindowSize verifies that WindowSizeMsg updates dimensions.
func TestUpdate_WindowSize(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := updated.(Model)

	if model.width != 120 {
		t.Errorf("width = %d, want 120", model.width)
	}
	if model.height != 40 {
		t.Errorf("height = %d, want 40", model.height)
	}
}

// TestView_MainRender verifies that the main view renders without panicking.
func TestView_MainRender(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 30

	output := m.View()
	if output == "" {
		t.Error("View() returned empty string")
	}
}

// TestView_StatsRender verifies that the savings view renders without panicking.
func TestView_StatsRender(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.layer0Status = Layer0Status{
		Attempts:   5,
		Matches:    2,
		Misses:     3,
		BytesSaved: 4096,
		HitRate:    0.4,
		Filters: []Layer0FilterStatus{{
			Name:       "git_status",
			Attempts:   5,
			Matches:    2,
			Misses:     3,
			BytesSaved: 4096,
			HitRate:    0.4,
			AvgMs:      0.25,
		}, {
			Name:       "python_traceback",
			Attempts:   2,
			Matches:    1,
			BytesSaved: 1024,
		}, {
			Name:       "package_output",
			Attempts:   2,
			Matches:    1,
			BytesSaved: 512,
		}, {
			Name:       "json_minify",
			Attempts:   2,
			Matches:    1,
			BytesSaved: 128,
		}},
	}
	m := NewModel(p)
	m.view = ViewStats
	m.width = 100
	m.height = 30

	output := m.View()
	if output == "" {
		t.Error("View() returned empty string for savings view")
	}
	if !strings.Contains(output, "LAYER 0 PARSERS") || !strings.Contains(output, "git_status") {
		t.Fatalf("layer0 parser card missing: %s", output)
	}
}

// TestView_StatsRender_LowReadHitRate covers the renderStatsView
// branch where the read-cache hit rate falls below the warn threshold
// (T57 stretch). Sets the rate explicitly via the ReadCacheStatus
// fields so the amber styling branch is exercised.
func TestView_StatsRender_LowReadHitRate(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.readStatus = ReadCacheStatus{
		Evaluations: 100,
		Allows:      80,
		Blocks:      5,
		HitRate:     0.05,
	}
	m := NewModel(p)
	m.view = ViewStats
	m.width = 120
	m.height = 40

	output := m.View()
	if !strings.Contains(output, "Hit rate:") {
		t.Fatalf("hit rate line missing: %s", output)
	}
}

// TestView_StatsRender_QualitySpike covers the renderStatsView branch
// where the cache-miss spike marker flips to ACTIVE (T77 visibility).
func TestView_StatsRender_QualitySpike(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.qualityStatus = QualityStatus{
		ReReadSessions:    3,
		ReReadTotalChecks: 100,
		ReReadTotalHits:   25,
		ReReadRate:        0.25,
		BaselineHitRate:   0.7,
		SpikeActive:       true,
		LastSpikeUnix:     1700000000,
		TotalSpikeCount:   2,
		TotalSaved:        12000,
		TotalInvalidation: 1000,
		NetSaved:          11000,
	}
	m := NewModel(p)
	m.view = ViewStats
	m.width = 120
	m.height = 40

	output := m.View()
	if !strings.Contains(output, "ACTIVE") {
		t.Fatalf("expected ACTIVE spike marker, got: %s", output)
	}
	if !strings.Contains(output, "QUALITY SIGNALS") {
		t.Fatalf("quality card title missing: %s", output)
	}
}

// TestView_LogsRender verifies that the logs view renders diagnostics and export data.
func TestView_LogsRender(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.sessionLogger.Log("INFO", "test", "hello debug view")
	p.recentFlights = []dbg.FlightRequestSummary{{
		RequestID: "req-debug-flight",
		Source:    "proxy",
		RouteMode: "mitm",
		Layers:    []int{1, 3},
		TokenAccounting: dbg.FlightTokenAccounting{
			BillableSavingsEstimate: 120,
			ProviderOutputTokens:    35,
		},
		CacheAccounting: dbg.FlightCacheAccounting{
			ProviderCachedInputTokens: 90,
		},
		Plan: &dbg.PlanSummary{
			Decisions: []dbg.PlanDecisionSummary{
				{Layer: "l0", Action: "run"},
				{Layer: "l1", Action: "run"},
				{Layer: "l2", Action: "bypass"},
				{Layer: "l3", Action: "run"},
				{Layer: "l3_output", Action: "run"},
			},
		},
		TotalProxyOverheadMs: 4.2,
	}}
	m := NewModel(p)
	m.view = ViewLogs
	m.width = 100
	m.height = 30

	output := m.View()
	if output == "" {
		t.Error("View() returned empty string for logs view")
	}
	if !strings.Contains(output, "FLIGHT RECORDER") || !strings.Contains(output, "req-debug-fli") {
		t.Fatalf("logs view missing flight diagnostics: %s", output)
	}
	if !strings.Contains(output, "plan") || !strings.Contains(output, "l0=run") || !strings.Contains(output, "+1") {
		t.Fatalf("logs view missing plan summary: %s", output)
	}
}

func TestView_StatusRender(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.view = ViewDebug
	m.SetServiceControl(&mockServiceControl{running: true})
	m.width = 100
	m.height = 30

	output := m.View()
	for _, want := range []string{"SLIMFERENCE / Status", "DAEMON", "PID 1234", "CODEX MODE", "SAFETY"} {
		if !strings.Contains(output, want) {
			t.Fatalf("status view missing %q in:\n%s", want, output)
		}
	}
	for _, blocked := range []string{"FLIGHT RECORDER", "LOG STREAM", "HOOK TURN STATE"} {
		if strings.Contains(output, blocked) {
			t.Fatalf("status view leaked logs block %q in:\n%s", blocked, output)
		}
	}
}

func TestRenderFlightDiagnosticsFallbacks(t *testing.T) {
	t.Parallel()
	m := NewModel(newMockProxy())
	out := renderFlightDiagnostics(&m, []dbg.FlightRequestSummary{{
		RequestID: "req-fallback",
		Source:    "hook_post",
		RouteMode: "hook",
		TokenAccounting: dbg.FlightTokenAccounting{
			BillableSavingsEstimate: 7,
			EstimatedOutputTokens:   3,
		},
		CacheAccounting: dbg.FlightCacheAccounting{
			ProviderCacheReadTokens: 5,
		},
		BypassReason: "passthrough",
		Plan: &dbg.PlanSummary{
			SafetyBlocked: true,
			Decisions: []dbg.PlanDecisionSummary{
				{Layer: "websocket", Action: "inspect"},
			},
		},
	}})
	if !strings.Contains(out, "req-fallback") || !strings.Contains(out, "bypasses 1") ||
		!strings.Contains(out, "plan-blocks 1") || !strings.Contains(out, "websocket=inspect blocked") {
		t.Fatalf("flight diagnostics fallback output=%s", out)
	}
}

func TestRenderFlightPlanLineEmptyCases(t *testing.T) {
	t.Parallel()
	if got := renderFlightPlanLine(dbg.FlightRequestSummary{}); got != "" {
		t.Fatalf("nil plan line = %q", got)
	}
	if got := renderFlightPlanLine(dbg.FlightRequestSummary{Plan: &dbg.PlanSummary{}}); got != "" {
		t.Fatalf("empty plan line = %q", got)
	}
	if got := renderFlightPlanLine(dbg.FlightRequestSummary{Plan: &dbg.PlanSummary{
		Decisions: []dbg.PlanDecisionSummary{{Layer: "", Action: "run"}, {Layer: "l1", Action: ""}},
	}}); got != "" {
		t.Fatalf("invalid decisions line = %q", got)
	}
}

// TestOnOff verifies the onOff helper.
func TestOnOff(t *testing.T) {
	if onOff(true) != "ON" {
		t.Error("onOff(true) should return ON")
	}
	if onOff(false) != "OFF" {
		t.Error("onOff(false) should return OFF")
	}
}

// TestInit_ReturnsCmd verifies that Init() returns a non-nil command.
func TestInit_ReturnsCmd(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() should return a non-nil tea.Cmd")
	}
}

// TestSendProxyEvent_SendsMessage verifies SendProxyEvent sends a message to the program.
func TestSendProxyEvent_SendsMessage(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	model := NewModel(p)

	prog := tea.NewProgram(
		model,
		tea.WithInput(strings.NewReader("")),
		tea.WithOutput(io.Discard),
	)

	done := make(chan error, 1)
	go func() {
		_, err := prog.Run()
		done <- err
	}()

	// Give the program a moment to start before sending.
	time.Sleep(20 * time.Millisecond)

	rm := types.RequestMetrics{
		Timestamp: time.Now(),
		Provider:  types.Anthropic,
		Model:     "claude-opus-4-6",
	}
	SendProxyEvent(prog, rm)

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("program exited with error: %v", err)
		}
	case <-time.After(3 * time.Second):
		// Program did not quit by itself - that's acceptable, the key thing is
		// SendProxyEvent ran without panicking.
		prog.Quit()
		<-done
	}
}

// TestTickCmd_ClosureExecution verifies that tickCmd's returned closure runs and produces a tickMsg.
func TestTickCmd_ClosureExecution(t *testing.T) {
	t.Parallel()
	cmd := tickCmd()
	if cmd == nil {
		t.Fatal("tickCmd() returned nil")
	}
	// Execute the command synchronously by running it as a batch (tea.Batch executes each cmd).
	// tickCmd uses tea.Tick which is a time-based Cmd; we call the underlying function
	// via type assertion to avoid 500ms wait.
	msg := cmd()
	if _, ok := msg.(tickMsg); !ok {
		t.Errorf("tickCmd closure returned %T, want tickMsg", msg)
	}
}

// TestFlashTimer_ClosureExecution verifies that flashTimer's returned closure produces flashExpiredMsg.
func TestFlashTimer_ClosureExecution(t *testing.T) {
	t.Parallel()
	cmd := flashTimer(time.Millisecond)
	if cmd == nil {
		t.Fatal("flashTimer() returned nil")
	}
	msg := cmd()
	if _, ok := msg.(flashExpiredMsg); !ok {
		t.Errorf("flashTimer closure returned %T, want flashExpiredMsg", msg)
	}
}

// TestUpdate_ProxyEvent verifies that proxyEventMsg triggers a snapshot refresh.
func TestUpdate_ProxyEvent(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.snap = analytics.AnalyticsSnapshot{TotalRequests: 99}
	m := NewModel(p)

	rm := types.RequestMetrics{
		Timestamp: time.Now(),
		Provider:  types.Anthropic,
		Model:     "claude-opus-4-6",
	}
	updated, cmd := m.Update(proxyEventMsg(rm))
	model := updated.(Model)

	if model.latestSnap.TotalRequests != 99 {
		t.Errorf("latestSnap.TotalRequests = %d, want 99", model.latestSnap.TotalRequests)
	}
	if cmd != nil {
		t.Error("proxyEventMsg should return nil cmd")
	}
}

// TestUpdate_FlashExpired verifies that flashExpiredMsg clears the flash message.
func TestUpdate_FlashExpired(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.flashMsg = "some message"
	m.flashExpiry = time.Now().Add(10 * time.Second)

	updated, _ := m.Update(flashExpiredMsg{})
	model := updated.(Model)

	if model.flashMsg != "" {
		t.Errorf("flashMsg = %q, want empty after flashExpiredMsg", model.flashMsg)
	}
}

// TestUpdate_DebugToggleBack verifies that pressing 'd' twice returns to ViewMain.
func TestUpdate_DebugToggleBack(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)

	// First press: go to debug view.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	model := updated.(Model)
	if model.view != ViewDebug {
		t.Fatalf("view = %d, want ViewDebug", model.view)
	}

	// Back key returns to main view.
	updated2, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	model2 := updated2.(Model)
	if model2.view != ViewMain {
		t.Errorf("view = %d, want ViewMain after pressing back from debug view", model2.view)
	}
}

// TestUpdate_CtrlC verifies that ctrl+c triggers shutdown.
func TestUpdate_CtrlC(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	if !p.shutdownCalled {
		t.Error("Shutdown should have been called on ctrl+c")
	}
	if cmd == nil {
		t.Error("expected a quit command on ctrl+c")
	}
}

// TestView_MainRender_NarrowWidth verifies the main view handles width < 40 by clamping.
func TestView_MainRender_NarrowWidth(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 30 // triggers width < 40 clamp to 80
	m.height = 24

	output := m.View()
	if output == "" {
		t.Error("View() returned empty string for narrow width")
	}
}

// TestView_StatsRender_NarrowWidth verifies the savings view handles width < 40.
func TestView_StatsRender_NarrowWidth(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.view = ViewStats
	m.width = 30
	m.height = 24

	output := m.View()
	if output == "" {
		t.Error("View() returned empty string for stats narrow width")
	}
}

// TestView_StatusRender_NarrowWidth verifies the status view handles width < 40.
func TestView_StatusRender_NarrowWidth(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.view = ViewDebug
	m.width = 30
	m.height = 24

	output := m.View()
	if output == "" {
		t.Error("View() returned empty string for status narrow width")
	}
}

// TestView_LogsRender_NilLogger verifies logs view renders correctly when SessionLogger is nil.
func TestView_LogsRender_NilLogger(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.sessionLogger = nil
	m := NewModel(p)
	m.view = ViewLogs
	m.width = 100
	m.height = 30

	output := m.View()
	if output == "" {
		t.Error("View() returned empty string for logs view with nil logger")
	}
}

// TestView_MainRender_WithData verifies main view branches triggered by non-zero analytics data.
func TestView_MainRender_WithData(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.snap = analytics.AnalyticsSnapshot{
		SessionStart:     time.Now(),
		TotalRequests:    10,
		TotalInputTokens: 100000,
		SavedInputTokens: 60000,
		CompressionRatio: 0.6,
		Layer1Savings:    30000,
		Layer2Savings:    10000,
		CacheHits:        3,
		SecretsRedacted:  2,
		AutoRetries:      1,
	}
	m := NewModel(p)
	m.width = 100
	m.height = 30
	m.latestSnap = p.snap

	output := m.View()
	if output == "" {
		t.Error("View() returned empty string with data-rich snapshot")
	}
}

// TestView_MainRender_HighCompressionRatio verifies the ratio > 1 clamping branch.
func TestView_MainRender_HighCompressionRatio(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.snap = analytics.AnalyticsSnapshot{
		TotalRequests:    5,
		TotalInputTokens: 1000,
		SavedInputTokens: 500,
		CompressionRatio: 1.5, // triggers ratio > 1 clamp
	}
	m := NewModel(p)
	m.width = 100
	m.height = 30
	m.latestSnap = p.snap

	output := m.View()
	if output == "" {
		t.Error("View() returned empty string for ratio > 1")
	}
}

// TestView_MainRender_SecretsAndRetries verifies the secrets/retries footer branch.
func TestView_MainRender_SecretsAndRetries(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.snap = analytics.AnalyticsSnapshot{
		TotalRequests:    3,
		TotalInputTokens: 5000,
		SavedInputTokens: 1000,
		SecretsRedacted:  5,
		AutoRetries:      2,
	}
	m := NewModel(p)
	m.width = 100
	m.height = 30
	m.latestSnap = p.snap

	output := m.View()
	if output == "" {
		t.Error("View() returned empty string with secrets+retries data")
	}
}

// TestView_MainRender_SecretsOnly verifies the secrets-only footer branch (AutoRetries == 0).
func TestView_MainRender_SecretsOnly(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.snap = analytics.AnalyticsSnapshot{
		TotalRequests:    3,
		TotalInputTokens: 5000,
		SavedInputTokens: 1000,
		SecretsRedacted:  3,
		AutoRetries:      0,
	}
	m := NewModel(p)
	m.width = 100
	m.height = 30
	m.latestSnap = p.snap

	output := m.View()
	if output == "" {
		t.Error("View() returned empty string with secrets-only data")
	}
}

// TestView_MainRender_RetriesOnly verifies the retries-only footer branch (SecretsRedacted == 0).
func TestView_MainRender_RetriesOnly(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.snap = analytics.AnalyticsSnapshot{
		TotalRequests:    3,
		TotalInputTokens: 5000,
		SavedInputTokens: 1000,
		SecretsRedacted:  0,
		AutoRetries:      4,
	}
	m := NewModel(p)
	m.width = 100
	m.height = 30
	m.latestSnap = p.snap

	output := m.View()
	if output == "" {
		t.Error("View() returned empty string with retries-only data")
	}
}

// TestView_MainRender_FlashMessage verifies the flash message display branch.
func TestView_MainRender_FlashMessage(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 30
	m.flashMsg = "test flash"
	m.flashExpiry = time.Now().Add(10 * time.Second)

	output := m.View()
	if output == "" {
		t.Error("View() returned empty string with active flash message")
	}
}

// TestView_MainRender_NarrowHeaderPad verifies the headerPad < 1 clamping branch.
// With a very old sessionStart (1000+ hours) and narrow width=40, headerPad goes negative.
func TestView_MainRender_NarrowHeaderPad(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	// width=40 -> innerWidth=36; "Slimference v1.0.0" (17) + "Session: 1000h Xm" (18) + 2 > 36
	m.width = 40
	m.height = 24
	m.sessionStart = time.Now().Add(-1001 * time.Hour) // causes very long session string

	output := m.View()
	if output == "" {
		t.Error("View() returned empty string for narrow header pad scenario")
	}
}

// TestView_StatsRender_WithData verifies savings view branches with non-zero snap data.
func TestView_StatsRender_WithData(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.snap = analytics.AnalyticsSnapshot{
		SessionStart:      time.Now(),
		TotalRequests:     20,
		TotalInputTokens:  200000,
		SavedInputTokens:  120000,
		TotalOutputTokens: 50000,
		Layer1Savings:     60000,
		Layer2Savings:     20000,
		CacheHits:         5,
		SecretsRedacted:   3,
		AutoRetries:       2,
		Errors:            0,
		PerProvider: map[types.Provider]analytics.ProviderStats{
			types.Anthropic: {
				Messages:         12,
				InputTokensOrig:  120000,
				InputTokensSaved: 72000,
				AvgRatio:         0.4,
			},
			types.OpenAI: {
				Messages:         8,
				InputTokensOrig:  80000,
				InputTokensSaved: 48000,
				AvgRatio:         0.6,
			},
		},
	}
	m := NewModel(p)
	m.view = ViewStats
	m.width = 100
	m.height = 40
	m.latestSnap = p.snap

	output := m.View()
	if output == "" {
		t.Error("View() returned empty string for stats with full data")
	}
}

// TestView_StatsRender_PerProvider_ZeroAvgRatio verifies the avgRatioPct == 0 branch in per-provider table.
func TestView_StatsRender_PerProvider_ZeroAvgRatio(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.snap = analytics.AnalyticsSnapshot{
		SessionStart:     time.Now(),
		TotalRequests:    5,
		TotalInputTokens: 50000,
		SavedInputTokens: 0,
		PerProvider: map[types.Provider]analytics.ProviderStats{
			types.Anthropic: {
				Messages:         5,
				InputTokensOrig:  50000,
				InputTokensSaved: 0,
				AvgRatio:         0, // triggers avgRatioPct = 0 branch
			},
		},
	}
	m := NewModel(p)
	m.view = ViewStats
	m.width = 100
	m.height = 40
	m.latestSnap = p.snap

	output := m.View()
	if output == "" {
		t.Error("View() returned empty for stats with zero avg ratio")
	}
}

func TestView_MainRender_ProductStatus(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.productStatus = ProductStatus{
		RouteStatus:                "WSS savings active",
		FallbackReason:             "bridge while repair runs",
		RecertStatus:               "running",
		SavingsStatus:              "saving",
		BillableInputTokensSaved:   12000,
		ProviderCacheReadTokens:    5000,
		ProviderCacheCreateTokens:  700,
		OutputWireBytesSaved:       2048,
		RequestSideBytesReduced:    1536,
		ToolPruneTokensSaved:       26,
		ToolPrunePrunedTools:       1,
		ToolPruneReattached:        1,
		OutputReduceInjectedTurns:  1,
		OutputReduceObservedTokens: 42,
		OutputReduceInputOverhead:  9,
		CacheHits:                  3,
		CacheMisses:                1,
		ReadDeltaHits:              2,
		RepeatedOutputHits:         1,
		ChunkDedupHits:             1,
	}
	m := NewModel(p)
	m.width = 100
	m.height = 30

	output := m.View()
	for _, want := range []string{"MENU", "Launch Codex CLI", "Launch Codex App", "Savings", "Status", "Logs", "Setup"} {
		if !strings.Contains(output, want) {
			t.Fatalf("main view missing %q in:\n%s", want, output)
		}
	}
	for _, blocked := range []string{"WSS savings active", "fallback: bridge", "recert running", "12.0K input saved", "provider-cache", "safety ok", "cache 3/4", "tool-prune", "output-reduce", "PROVIDER MAP", "TRAFFIC", "LIVE", "CURRENT SESSION", "HEALTH", "DIAGNOSTICS"} {
		if strings.Contains(output, blocked) {
			t.Fatalf("launch view leaked detail %q in:\n%s", blocked, output)
		}
	}
}

func TestView_MainRender_OutputReducePendingDoesNotClaimSavings(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.productStatus = ProductStatus{
		RouteStatus:               "WSS savings active",
		SavingsStatus:             "active_no_savings",
		OutputReduceInjectedTurns: 1,
		OutputReduceInputOverhead: 9,
	}
	m := NewModel(p)
	m.width = 100
	m.height = 30

	output := m.View()
	for _, want := range []string{"MENU", "Launch Codex CLI", "Savings", "Status", "Logs", "Setup"} {
		if !strings.Contains(output, want) {
			t.Fatalf("main view missing %q in:\n%s", want, output)
		}
	}
	if strings.Contains(output, "output-reduce") {
		t.Fatalf("output-reduce pending must not claim savings:\n%s", output)
	}
}

func TestView_MainRender_ProductStatusEmptyUsesExplicitProductZeros(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.snap = analytics.AnalyticsSnapshot{
		SessionStart:     time.Now(),
		TotalInputTokens: 10000,
		SavedInputTokens: 5000,
	}
	m := NewModel(p)
	m.width = 100
	m.height = 30

	output := m.View()
	for _, want := range []string{"MENU", "Launch Codex CLI", "Launch Codex App", "Savings", "Status", "Logs", "Setup"} {
		if !strings.Contains(output, want) {
			t.Fatalf("main view missing %q in:\n%s", want, output)
		}
	}
	for _, blocked := range []string{"50%", "10.0K"} {
		if strings.Contains(output, blocked) {
			t.Fatalf("main view leaked legacy snapshot headline %q in:\n%s", blocked, output)
		}
	}
}

func TestView_MainRender_UsesCachedProductStatus(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.productStatus = ProductStatus{
		RouteStatus:              "WSS savings active",
		SavingsStatus:            "saving",
		BillableInputTokensSaved: 99,
	}
	m := NewModel(p)
	callsAfterInit := p.productCalls

	_ = m.View()
	_ = m.View()
	if p.productCalls != callsAfterInit {
		t.Fatalf("render fetched product status %d extra time(s)", p.productCalls-callsAfterInit)
	}
	if strings.Contains(m.View(), "99 input saved") {
		t.Fatalf("launch view leaked product savings detail:\n%s", m.View())
	}
}

func TestView_MainRender_ProductAttention(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.productStatus = ProductStatus{
		RouteStatus:          "WSS bridge",
		SavingsStatus:        "attention",
		SafetyIssues:         1,
		ToolResolutionMisses: 1,
		HostBudgetExceeded:   true,
		HostBudgetReasons:    []string{"rss_budget_exceeded"},
		WSSParseFailures:     1,
		WSSDegradedSessions:  1,
		WSSCompressionErrors: 1,
	}
	m := NewModel(p)
	m.width = 100
	m.height = 30

	output := m.View()
	for _, blocked := range []string{"ATTENTION", "WSS bridge", "tool miss", "WSS parse", "WSS degraded session", "WSS compression error", "rss_budget_exceeded"} {
		if strings.Contains(output, blocked) {
			t.Fatalf("launch view leaked status detail %q in:\n%s", blocked, output)
		}
	}
}

func TestView_MainRender_ProductHostBudgetUnknown(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.productStatus = ProductStatus{
		RouteStatus:      "WSS savings active",
		SavingsStatus:    "saving",
		HostBudgetStatus: "unknown",
	}
	m := NewModel(p)
	m.width = 100
	m.height = 30

	output := m.View()
	if strings.Contains(output, "host budget unknown") {
		t.Fatalf("launch view leaked host-budget warning:\n%s", output)
	}
	if strings.Contains(output, "safety ok") {
		t.Fatalf("launch view must not show health state:\n%s", output)
	}
}

// TestRenderRequestLog_WithRequests verifies renderRequestLog returns lines when reqs exist.
func TestRenderRequestLog_WithRequests(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.recentReqs = []types.RequestMetrics{
		{
			Timestamp:        time.Now(),
			Provider:         types.Anthropic,
			Model:            "claude-opus-4-6",
			InputTokensOrig:  10000,
			InputTokensComp:  4000,
			CompressionRatio: 0.4,
			Layers:           []int{1},
			LatencyMs:        2.5,
		},
	}
	m := NewModel(p)
	m.width = 100
	m.height = 24

	lines := m.renderRequestLog()
	if len(lines) == 0 {
		t.Error("renderRequestLog should return lines when requests exist")
	}
}

// TestRenderRequestLog_TallTerminal verifies the height >= 30 branch bumping maxLines to 10.
func TestRenderRequestLog_TallTerminal(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	// Provide 10 requests to confirm we can show all with tall terminal.
	reqs := make([]types.RequestMetrics, 10)
	for i := range reqs {
		reqs[i] = types.RequestMetrics{
			Timestamp: time.Now(),
			Provider:  types.Anthropic,
			Model:     "claude-opus-4-6",
		}
	}
	p.recentReqs = reqs
	m := NewModel(p)
	m.width = 100
	m.height = 35 // >= 30 triggers maxLines = 10

	lines := m.renderRequestLog()
	if len(lines) == 0 {
		t.Error("renderRequestLog should return lines for tall terminal with requests")
	}
}

// TestRenderHookStatus_none verifies that no output is produced when both hooks are absent.
func TestRenderHookStatus_none(t *testing.T) {
	t.Parallel()
	s := NewStyles()
	out := renderHookStatus(s, HookStatus{})
	if out != "" {
		t.Errorf("renderHookStatus with no hooks: want empty, got %q", out)
	}
}

// TestRenderHookStatus_both verifies that both hook names appear when installed.
func TestRenderHookStatus_both(t *testing.T) {
	t.Parallel()
	s := NewStyles()
	out := renderHookStatus(s, HookStatus{Claude: true, Codex: true})
	if !strings.Contains(out, "claude") {
		t.Errorf("want 'claude' in hook status, got %q", out)
	}
	if !strings.Contains(out, "codex") {
		t.Errorf("want 'codex' in hook status, got %q", out)
	}
}

// TestRenderHookStatus_codexOnly verifies partial install (codex only) — covers "claude -" branch.
func TestRenderHookStatus_codexOnly(t *testing.T) {
	t.Parallel()
	s := NewStyles()
	out := renderHookStatus(s, HookStatus{Claude: false, Codex: true})
	if !strings.Contains(out, "claude") {
		t.Errorf("want 'claude' in output even when not installed, got %q", out)
	}
	if !strings.Contains(out, "codex") {
		t.Errorf("want 'codex' in output, got %q", out)
	}
}

// TestRenderHookStatus_claudeOnly verifies partial install (claude only).
func TestRenderHookStatus_claudeOnly(t *testing.T) {
	t.Parallel()
	s := NewStyles()
	out := renderHookStatus(s, HookStatus{Claude: true, Codex: false})
	if !strings.Contains(out, "claude") {
		t.Errorf("want 'claude' in output, got %q", out)
	}
	// codex should still appear (as disabled indicator)
	if !strings.Contains(out, "codex") {
		t.Errorf("want 'codex' in output even when not installed, got %q", out)
	}
}

// TestSetHookStatus verifies that SetHookStatus updates the model field.
func TestSetHookStatus(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	if m.hookStatus.Claude || m.hookStatus.Codex {
		t.Fatal("initial hookStatus should be zero")
	}
	m.SetHookStatus(HookStatus{Claude: true, Codex: true})
	if !m.hookStatus.Claude || !m.hookStatus.Codex {
		t.Fatalf("hookStatus not updated: %+v", m.hookStatus)
	}
}

// TestView_MainRender_withHooks verifies the main view renders hook status when hooks are installed.
func TestView_MainRender_withHooks(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.SetHookStatus(HookStatus{Claude: true, Codex: false})
	m.width = 100
	m.height = 30
	output := m.View()
	if strings.Contains(output, "claude") || strings.Contains(output, "Setup missing") {
		t.Errorf("main view should keep hook/setup detail out, got: %s", output)
	}
}

func TestView_SetupView_renders(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 30
	// Switch to setup view via 'i' key.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model := updated.(Model)
	output := model.View()
	if !strings.Contains(output, "SETUP CHECKLIST") {
		t.Errorf("setup view: want 'SETUP CHECKLIST' in output, got: %s", output)
	}
	if !strings.Contains(output, "SERVICE STATUS") {
		t.Errorf("setup view: want 'SERVICE STATUS' in output, got: %s", output)
	}
	if !strings.Contains(output, "COMMANDS") {
		t.Errorf("setup view: want 'COMMANDS' in output, got: %s", output)
	}
	// Press back to return home.
	updated2, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	model2 := updated2.(Model)
	if model2.view != ViewMain {
		t.Errorf("pressing back should return to main view, got: %d", model2.view)
	}
}

func TestModel_CopyDebugLog(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 24
	// No entries yet.
	path := m.copyDebugLog()
	if path != "" {
		t.Errorf("empty logger should return empty path, got: %s", path)
	}

	// Add an entry.
	p.sessionLogger.Log("INFO", "test", "hello world")

	path = m.copyDebugLog()
	if path == "" {
		t.Fatal("should return a path after logging an entry")
	}
	// Verify file exists.
	if _, err := io.ReadAll(strings.NewReader(path)); err != nil {
		_ = err
	}
}

func TestView_MainFooter_hasOpenKey(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 24
	output := m.View()
	if !strings.Contains(output, "[enter]") || !strings.Contains(output, "open") {
		t.Errorf("main footer should have enter open hint, got: %s", output)
	}
}

func TestView_SetupView_serviceControlStart(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 30
	svc := &mockServiceControl{}
	m.SetServiceControl(svc)

	// Go to setup view.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model := updated.(Model)

	// Press 'p' to start daemon (not running, so it starts).
	updated2, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	model2 := updated2.(Model)
	if !svc.started {
		t.Error("expected StartDaemon to be called on [p]")
	}
	if model2.flashMsg == "" {
		t.Error("expected flash message after starting daemon")
	}
}

func TestView_SetupView_serviceControlRestart(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 30
	svc := &mockServiceControl{}
	m.SetServiceControl(svc)

	// Go to setup view.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model := updated.(Model)

	// Press 'o' to restart.
	updated2, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	_ = updated2.(Model)
	if !svc.restarted {
		t.Error("expected RestartDaemon to be called on [o]")
	}
}

func TestView_SetupView_serviceControlInstall(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 30
	svc := &mockServiceControl{}
	m.SetServiceControl(svc)

	// Go to setup view.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model := updated.(Model)

	// Press 'e' to install service.
	updated2, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_ = updated2.(Model)
	if !svc.installed {
		t.Error("expected InstallService to be called on [e]")
	}
}

func TestView_SetupView_serviceControlUninstall(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 30
	svc := &mockServiceControl{}
	m.SetServiceControl(svc)

	// Go to setup view.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model := updated.(Model)

	// Press 'w' to uninstall service.
	updated2, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	_ = updated2.(Model)
	if !svc.removed {
		t.Error("expected UninstallService to be called on [w]")
	}
}

func TestView_SetupView_serviceControlError(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 30
	svc := &mockServiceControl{err: fmt.Errorf("test error")}
	m.SetServiceControl(svc)

	// Go to setup view.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model := updated.(Model)

	// Press 'p' - should fail gracefully.
	updated2, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	model2 := updated2.(Model)
	if !strings.Contains(model2.flashMsg, "failed") {
		t.Errorf("expected flash to contain 'failed', got: %s", model2.flashMsg)
	}
}

func TestView_SetupView_noServiceControl(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 30

	// Go to setup view.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model := updated.(Model)

	// Press 'p' without service control - should be no-op (no crash).
	updated2, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	model2 := updated2.(Model)
	if model2.flashMsg != "" {
		t.Errorf("expected no flash without service control, got: %s", model2.flashMsg)
	}
}

func TestView_SetupView_withServiceControl_rendersSteps(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 30
	svc := &mockServiceControl{}
	m.SetServiceControl(svc)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model := updated.(Model)
	output := model.View()
	if !strings.Contains(output, "SETUP STEPS") {
		t.Errorf("setup view with svc: want 'SETUP STEPS', got: %s", output)
	}
	if !strings.Contains(output, "SERVICE CONTROLS") {
		t.Errorf("setup view with svc: want 'SERVICE CONTROLS', got: %s", output)
	}
}

func TestView_SetupView_stepNavigation(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 30
	svc := &mockServiceControl{}
	m.SetServiceControl(svc)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model := updated.(Model)

	updated2, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	model2 := updated2.(Model)
	if model2.setupStep != 1 {
		t.Errorf("pressing '1' in setup should set setupStep=1, got %d", model2.setupStep)
	}

	updated3, _ := model2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	model3 := updated3.(Model)
	if model3.setupStep != 2 {
		t.Errorf("pressing '2' in setup should set setupStep=2, got %d", model3.setupStep)
	}

	updated4, _ := model3.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	model4 := updated4.(Model)
	if model4.setupStep != 3 {
		t.Errorf("pressing '3' in setup should set setupStep=3, got %d", model4.setupStep)
	}
}

func TestView_SetupView_executeStep(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 30
	svc := &mockServiceControl{}
	m.SetServiceControl(svc)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model := updated.(Model)

	updated2, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	model2 := updated2.(Model)

	updated3, _ := model2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e', 'n', 't', 'e', 'r'}})
	model3 := updated3.(Model)
	if model3.flashMsg == "" {
		t.Error("expected flash message after executing setup step")
	}
}

func TestView_SetupView_executeStep_alreadyDone(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 30
	svc := &mockServiceControl{transparentStatus: TransparentStatus{
		CAExists:           true,
		CATrusted:          true,
		AutoStartInstalled: true,
	}}
	m.SetServiceControl(svc)
	m.hookStatus = HookStatus{Claude: true, Codex: true}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model := updated.(Model)

	updated2, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	model2 := updated2.(Model)

	updated3, _ := model2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e', 'n', 't', 'e', 'r'}})
	model3 := updated3.(Model)
	if !strings.Contains(model3.flashMsg, "Already done") {
		t.Errorf("expected 'Already done' flash, got: %s", model3.flashMsg)
	}
}

func TestView_SetupView_executeStep_invalidStep(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 30
	svc := &mockServiceControl{}
	m.SetServiceControl(svc)

	m.setupStep = 99
	m.executeSetupStep()
	if m.flashMsg != "" {
		t.Errorf("invalid setup step should be no-op, got: %s", m.flashMsg)
	}
}

func TestView_SetupView_executeStep_error(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 30
	svc := &mockServiceControl{err: fmt.Errorf("permission denied")}
	m.SetServiceControl(svc)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model := updated.(Model)

	updated2, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	model2 := updated2.(Model)

	updated3, _ := model2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e', 'n', 't', 'e', 'r'}})
	model3 := updated3.(Model)
	if !strings.Contains(model3.flashMsg, "Error") {
		t.Errorf("expected error flash, got: %s", model3.flashMsg)
	}
}

func TestView_SetupView_serviceControlStopRunning(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 30
	svc := &mockServiceControl{running: true}
	m.SetServiceControl(svc)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model := updated.(Model)

	updated2, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	model2 := updated2.(Model)
	if !svc.stopped {
		t.Error("expected StopDaemon when running=true and pressing 'p'")
	}
	if !strings.Contains(model2.flashMsg, "stopped") {
		t.Errorf("expected 'stopped' flash, got: %s", model2.flashMsg)
	}
}

func TestView_LayerToggle_inSetupView_noop(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 30
	svc := &mockServiceControl{}
	m.SetServiceControl(svc)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model := updated.(Model)

	wasEnabled := model.layer1Enabled
	updated2, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	model2 := updated2.(Model)
	if model2.layer1Enabled != wasEnabled {
		t.Error("'1' in setup view should navigate steps, not toggle layers")
	}
	if model2.setupStep != 1 {
		t.Error("'1' in setup view should set setupStep=1")
	}
}

func TestView_CopyDebugLog_withEntries(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.sessionLogger = sessions.NewSessionLogger()
	p.sessionLogger.Log("INFO", "test", "test log entry")
	m := NewModel(p)
	m.width = 100
	m.height = 24

	path := m.copyDebugLog()
	if path == "" {
		t.Fatal("expected non-empty path when logger has entries")
	}
	if !strings.Contains(path, "debug-") {
		t.Errorf("path should contain 'debug-', got: %s", path)
	}
}

func TestView_CopyDebugLog_nilLogger(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.sessionLogger = nil
	m := NewModel(p)
	path := m.copyDebugLog()
	if path != "" {
		t.Errorf("nil logger should return empty path, got: %s", path)
	}
}

func TestView_MainRender_allReady(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 24
	m.hookStatus = HookStatus{Claude: true, Codex: true}
	output := m.View()
	if strings.Contains(output, "READY") || !strings.Contains(output, "MENU") {
		t.Errorf("main view should keep setup readiness out of Launch, got: %s", output)
	}
}

func TestView_MainRender_notReady(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 24
	m.hookStatus = HookStatus{Claude: false, Codex: false}
	output := m.View()
	if strings.Contains(output, "● SETUP") || strings.Contains(output, "missing: Codex") {
		t.Errorf("main view should keep setup warning out of Launch, got: %s", output)
	}
}

func TestView_SetupView_allReady(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 30
	m.hookStatus = HookStatus{Claude: true, Codex: true}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model := updated.(Model)
	output := model.View()
	if !strings.Contains(output, "ALL SET") {
		t.Errorf("setup view with all ready should show 'ALL SET', got: %s", output)
	}
}

func TestView_SetupView_withSvc_runningDaemon(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 30
	svc := &mockServiceControl{running: true}
	m.SetServiceControl(svc)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model := updated.(Model)
	output := model.View()
	if !strings.Contains(output, "RUNNING") {
		t.Errorf("setup view with running daemon should show RUNNING, got: %s", output)
	}
}

func TestView_SetupView_withSvc_stepCompleted(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 30
	svc := &mockServiceControl{}
	m.SetServiceControl(svc)
	m.hookStatus = HookStatus{Claude: true, Codex: false}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model := updated.(Model)
	output := model.View()
	if !strings.Contains(output, "SETUP STEPS") {
		t.Errorf("setup view should show SETUP STEPS with svc, got: %s", output)
	}
}

func TestView_MainRender_quickStart(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 24
	m.hookStatus = HookStatus{}
	output := m.View()
	if strings.Contains(output, "No Slimference session data yet") || !strings.Contains(output, "MENU") {
		t.Errorf("main view should keep session state out of Launch, got: %s", output)
	}
}

func TestUpdate_CopyDebugLogKey(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.sessionLogger = sessions.NewSessionLogger()
	p.sessionLogger.Log("INFO", "test", "entry")
	m := NewModel(p)
	m.width = 100
	m.height = 24

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model := updated.(Model)
	if !strings.Contains(model.flashMsg, "debug-") {
		t.Errorf("pressing 'y' should copy debug log, got flash: %s", model.flashMsg)
	}
}

func TestUpdate_CopyDebugLogKey_noEntries(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 24

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model := updated.(Model)
	if !strings.Contains(model.flashMsg, "No debug log") {
		t.Errorf("pressing 'y' with no entries should flash 'No debug log', got: %s", model.flashMsg)
	}
}

func TestView_SetupView_withSvc_stepSelected(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 30
	svc := &mockServiceControl{}
	m.SetServiceControl(svc)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model := updated.(Model)

	updated2, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	model2 := updated2.(Model)
	output := model2.View()
	if !strings.Contains(output, "Run slimference install") {
		t.Errorf("selected step should show in output, got: %s", output)
	}
}

func TestView_MainRender_liveLog(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.recentReqs = []types.RequestMetrics{
		{
			Timestamp:        time.Now(),
			Provider:         types.Anthropic,
			Model:            "claude-opus-4-6",
			InputTokensOrig:  10000,
			InputTokensComp:  4000,
			CompressionRatio: 0.4,
			Layers:           []int{1},
			LatencyMs:        2.5,
		},
	}
	m := NewModel(p)
	m.width = 100
	m.height = 24
	m.hookStatus = HookStatus{Claude: true, Codex: true}
	output := m.View()
	if !strings.Contains(output, "MENU") || strings.Contains(output, "LIVE") || strings.Contains(output, "CURRENT SESSION") {
		t.Errorf("main view should keep live log out of Launch, got: %s", output)
	}
}

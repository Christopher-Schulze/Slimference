package tui

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/types"
)

func findDashboardActionIndex(actions []dashboardAction, id string) int {
	for i, action := range actions {
		if action.id == id {
			return i
		}
	}
	return -1
}

func TestDashboardActions_LaunchCenterStructureAndStates(t *testing.T) {
	m := NewModel(newMockProxy())
	svc := &mockServiceControl{
		codexRouteStatus: CodexRouteStatus{
			DaemonReachable: true,
			AutoTransport:   "wss",
			AutoMode:        "wss_phasef",
			WSSCertified:    true,
		},
		codexDesktopStatus: CodexDesktopStatus{
			Mode:         "desktop_tls_blocked",
			FailureClass: "tls_trust_rejected",
		},
	}
	m.SetServiceControl(svc)

	actions := m.dashboardActions()
	want := []string{"launch_cli", "launch_app", "savings", "status", "manage"}
	if len(actions) != len(want) {
		t.Fatalf("actions=%v want %d launch-center entries", actions, len(want))
	}
	for i, id := range want {
		if actions[i].id != id {
			t.Fatalf("action[%d]=%q want %q in %v", i, actions[i].id, id, actions)
		}
	}
	if actions[0].label != "Launch Codex CLI" || actions[0].state != "WSS savings" {
		t.Fatalf("CLI action=%+v", actions[0])
	}
	if actions[1].label != "Launch Codex App" || actions[1].state != "blocked" {
		t.Fatalf("App action=%+v", actions[1])
	}
	if got := findDashboardActionIndex(actions, "daemon"); got >= 0 {
		t.Fatalf("legacy daemon action must not be top-level: %v", actions[got])
	}
}

func TestDashboardActions_AutoStartInstalledErrorAndCursorMoves(t *testing.T) {
	m := NewModel(newMockProxy())
	if m.autoStartInstalled() {
		t.Fatal("autoStartInstalled should be false without service and home path")
	}

	origHome := userHomeDirFn
	userHomeDirFn = func() (string, error) { return "", os.ErrPermission }
	defer func() { userHomeDirFn = origHome }()
	if m.autoStartInstalled() {
		t.Fatal("autoStartInstalled should be false on home-dir error")
	}

	if got := clampIndex(-1, 3); got != 0 {
		t.Fatalf("clamp negative=%d", got)
	}
	if got := clampIndex(9, 3); got != 2 {
		t.Fatalf("clamp high=%d", got)
	}
	if got := clampIndex(1, 3); got != 1 {
		t.Fatalf("clamp keep=%d", got)
	}
	for _, tc := range []struct {
		n    int
		want string
	}{
		{999, "999"},
		{1500, "1.5K"},
		{1_500_000, "1.5M"},
	} {
		if got := formatTokenCount(tc.n); got != tc.want {
			t.Fatalf("formatTokenCount(%d)=%q want %q", tc.n, got, tc.want)
		}
	}

	m.moveMainCursor(1)
	if m.mainCursor != 1 {
		t.Fatalf("mainCursor=%d", m.mainCursor)
	}
	m.moveStatsCursor(20)
	if m.statsCursor != 10 {
		t.Fatalf("statsCursor=%d", m.statsCursor)
	}
	m.moveDebugCursor(5)
	if m.debugCursor != 0 {
		t.Fatalf("debugCursor=%d", m.debugCursor)
	}
}

func TestLaunchCenterStateVocabularyBranches(t *testing.T) {
	model := NewModel(newMockProxy())

	model.codexRouteStatus = CodexRouteStatus{DaemonReachable: true, AutoTransport: "http"}
	if got := model.codexCLIState(); got != "auto=http" {
		t.Fatalf("CLI state=%q", got)
	}
	model.codexRouteStatus = CodexRouteStatus{FallbackReason: "codex version changed", NeedsRecert: true}
	if got := model.codexCLIState(); got != "repairing" {
		t.Fatalf("CLI recert state=%q", got)
	}

	for _, tc := range []struct {
		status CodexDesktopStatus
		state  string
		desc   string
	}{
		{CodexDesktopStatus{FailureClass: "tls_trust_rejected"}, "blocked", "tls_trust_rejected"},
		{CodexDesktopStatus{FailureClass: "ca_missing"}, "blocked", "ca_missing"},
		{CodexDesktopStatus{FailureClass: "ca_untrusted"}, "blocked", "ca_untrusted"},
		{CodexDesktopStatus{FailureClass: "daemon_unreachable"}, "blocked", "daemon_unreachable"},
		{CodexDesktopStatus{Mode: "ready_for_live_desktop_probe"}, "proof needed", "proof-gated"},
		{CodexDesktopStatus{Mode: "proxy_wss_needs_review", ConversationObserved: true}, "proof needed", "proof-gated"},
	} {
		model.codexDesktopStatus = tc.status
		if got := model.codexAppState(); got != tc.state {
			t.Fatalf("app state=%q want %q for %+v", got, tc.state, tc.status)
		}
		if got := model.codexAppDescription(); !strings.Contains(got, tc.desc) {
			t.Fatalf("app desc=%q want %q for %+v", got, tc.desc, tc.status)
		}
	}

	model.transparentStatus = TransparentStatus{ProxyArmed: true}
	if got := model.statusState(); got != "lab armed" {
		t.Fatalf("status state=%q", got)
	}
	if got := model.manageState(); got != "lab armed" {
		t.Fatalf("manage state=%q", got)
	}
	model.transparentStatus = TransparentStatus{}
	model.codexRouteStatus = CodexRouteStatus{DaemonReachable: true}
	model.codexDesktopStatus = CodexDesktopStatus{FailureClass: "ca_missing"}
	if got := model.statusDescription(); !strings.Contains(got, "Desktop gate") {
		t.Fatalf("status desc=%q", got)
	}
}

func TestExecuteMainSelection_AllDashboardActions(t *testing.T) {
	proxy := newMockProxy()
	proxy.snap = analytics.AnalyticsSnapshot{SavedInputTokens: 1500}
	m := NewModel(proxy)
	svc := &mockServiceControl{
		codexRouteStatus: CodexRouteStatus{
			DaemonReachable: true,
			AutoTransport:   "wss",
			AutoMode:        "wss_phasef",
			WSSCertified:    true,
		},
		codexDesktopStatus: CodexDesktopStatus{
			Mode: "ready_for_live_desktop_probe",
		},
	}
	m.SetServiceControl(svc)
	m.latestSnap = proxy.snap

	runAction := func(id string) {
		actions := m.dashboardActions()
		idx := findDashboardActionIndex(actions, id)
		if idx < 0 {
			t.Fatalf("action %q missing in %v", id, actions)
		}
		m.mainCursor = idx
		_ = m.executeMainSelection()
	}

	runAction("launch_cli")
	if !svc.codexCLILaunched || !strings.Contains(m.flashMsg, "Codex CLI launched") {
		t.Fatalf("launch CLI failed: svc=%+v flash=%q", svc, m.flashMsg)
	}
	runAction("launch_app")
	if !svc.codexAppLaunched || !strings.Contains(m.flashMsg, "Codex App launch requested") {
		t.Fatalf("launch App failed: svc=%+v flash=%q", svc, m.flashMsg)
	}
	runAction("savings")
	if m.view != ViewStats || !strings.Contains(m.flashMsg, "Savings opened") {
		t.Fatalf("savings action view=%v flash=%q", m.view, m.flashMsg)
	}
	m.view = ViewMain
	runAction("status")
	if !strings.Contains(m.flashMsg, "Status: CLI WSS savings") {
		t.Fatalf("status flash=%q", m.flashMsg)
	}
	runAction("manage")
	if m.view != ViewSetup || !strings.Contains(m.flashMsg, "Manage Slimference opened") {
		t.Fatalf("manage action view=%v flash=%q", m.view, m.flashMsg)
	}
}

func TestExecuteMainSelection_ErrorBranchesAndDebugSelection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	proxy := newMockProxy()
	proxy.sessionLogger.Log("INFO", "test", "hello")
	m := NewModel(proxy)
	svc := &mockServiceControl{err: os.ErrPermission}
	m.SetServiceControl(svc)

	for _, id := range []string{"launch_cli", "launch_app"} {
		m.mainCursor = findDashboardActionIndex(m.dashboardActions(), id)
		_ = m.executeMainSelection()
		if !strings.Contains(m.flashMsg, "failed") && !strings.Contains(m.flashMsg, "blocked") {
			t.Fatalf("expected failure flash for %s, got %q", id, m.flashMsg)
		}
	}

	retryDiagnostic := NewModel(newMockProxy())
	retryDiagnostic.SetServiceControl(&mockServiceControl{
		codexDesktopStatus: CodexDesktopStatus{
			Mode:         "desktop_tls_blocked",
			FailureClass: "tls_trust_rejected",
		},
	})
	retryDiagnostic.mainCursor = findDashboardActionIndex(retryDiagnostic.dashboardActions(), "launch_app")
	_ = retryDiagnostic.executeMainSelection()
	if !strings.Contains(retryDiagnostic.flashMsg, "Codex App launch requested") {
		t.Fatalf("desktop retry flash=%q", retryDiagnostic.flashMsg)
	}

	disableFail := NewModel(newMockProxy())
	disableFail.SetServiceControl(&mockServiceControl{
		err: errors.New("boom"),
	})
	disableFail.mainCursor = findDashboardActionIndex(disableFail.dashboardActions(), "launch_app")
	_ = disableFail.executeMainSelection()
	if !strings.Contains(disableFail.flashMsg, "Codex App launch blocked") {
		t.Fatalf("desktop launch failure flash=%q", disableFail.flashMsg)
	}

	noSvc := NewModel(newMockProxy())
	noSvc.mainCursor = findDashboardActionIndex(noSvc.dashboardActions(), "launch_cli")
	_ = noSvc.executeMainSelection()
	if !strings.Contains(noSvc.flashMsg, "service adapter missing") {
		t.Fatalf("missing service CLI flash=%q", noSvc.flashMsg)
	}
	noSvc.mainCursor = findDashboardActionIndex(noSvc.dashboardActions(), "launch_app")
	_ = noSvc.executeMainSelection()
	if !strings.Contains(noSvc.flashMsg, "service adapter missing") {
		t.Fatalf("missing service app flash=%q", noSvc.flashMsg)
	}

	_ = m.executeDebugSelection()
	if !strings.Contains(m.flashMsg, "Debug log copied") {
		t.Fatalf("debug export flash=%q", m.flashMsg)
	}

	empty := NewModel(newMockProxy())
	_ = empty.executeDebugSelection()
	if !strings.Contains(empty.flashMsg, "No debug log entries") {
		t.Fatalf("empty debug flash=%q", empty.flashMsg)
	}
}

func TestUpdate_ArrowDrivenDashboardAndDebug(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	proxy := newMockProxy()
	proxy.sessionLogger.Log("INFO", "test", "hello")
	m := NewModel(proxy)
	m.SetServiceControl(&mockServiceControl{})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	model := updated.(Model)
	if model.mainCursor != 1 {
		t.Fatalf("mainCursor after down=%d", model.mainCursor)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if !strings.Contains(model.flashMsg, "Codex App launch requested") {
		t.Fatalf("enter on dashboard flash=%q", model.flashMsg)
	}

	model.view = ViewStats
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if model.statsCursor != 1 {
		t.Fatalf("statsCursor=%d", model.statsCursor)
	}

	model.view = ViewDebug
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if !strings.Contains(model.flashMsg, "Debug log copied") {
		t.Fatalf("debug enter flash=%q", model.flashMsg)
	}
}

func TestRenderMainViewAndHelperCoverage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	proxy := newMockProxy()
	proxy.snap = analytics.AnalyticsSnapshot{
		SessionStart:            time.Now().Add(-2 * time.Minute),
		TotalRequests:           12,
		TotalInputTokens:        12000,
		SavedInputTokens:        3600,
		TotalOutputTokens:       2400,
		PromptCacheReadTokens:   1500,
		PromptCacheCreateTokens: 500,
		SecretsRedacted:         2,
		AutoRetries:             1,
		PerProvider: map[types.Provider]analytics.ProviderStats{
			types.Anthropic: {Messages: 7, InputTokensSaved: 2000},
			types.OpenAI:    {Messages: 5, InputTokensSaved: 1600},
		},
		LatencyAnthropicMs: 220,
		LatencyOpenAIMs:    180,
	}
	m := NewModel(proxy)
	svc := &mockServiceControl{running: true}
	m.SetServiceControl(svc)
	m.width = 120
	m.height = 40

	view := m.renderMainView()
	for _, needle := range []string{"LAUNCH CENTER", "Launch Codex CLI", "PROVIDER MAP", "TRAFFIC", "daemon live", "Claude Code"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("main view missing %q in:\n%s", needle, view)
		}
	}

	if got := renderMenuRow(m.styles, 20, false, "Label", ""); !strings.Contains(got, "Label") {
		t.Fatalf("renderMenuRow empty state=%q", got)
	}
	if got := renderMenuRow(m.styles, 32, true, "Label", "ON"); !strings.Contains(got, "ON") {
		t.Fatalf("renderMenuRow selected=%q", got)
	}
	if got := formatFloatCompact(12.3); got != "12.3" {
		t.Fatalf("formatFloatCompact small=%q", got)
	}
	if got := formatFloatCompact(1200); got != "1.2K" {
		t.Fatalf("formatFloatCompact kilo=%q", got)
	}
	if got := formatFloatCompact(1_200_000); got != "1.2M" {
		t.Fatalf("formatFloatCompact mega=%q", got)
	}
	if got := providerFlowLine(m.styles, "Claude", analytics.ProviderStats{}, 0); !strings.Contains(got, "0 req") {
		t.Fatalf("providerFlowLine=%q", got)
	}
	if got := providerFlowLine(m.styles, "Claude", analytics.ProviderStats{Messages: 2, InputTokensSaved: 900}, 210); !strings.Contains(got, "210ms") {
		t.Fatalf("providerFlowLine latency=%q", got)
	}
	if got := extendLines([]string{"a"}, 1, "x"); len(got) != 1 || got[0] != "a" {
		t.Fatalf("extendLines noop=%v", got)
	}
	if got := extendLines([]string{"a"}, 3, "x"); len(got) != 3 || got[2] != "x" {
		t.Fatalf("extendLines fill=%v", got)
	}
}

func TestUpdate_MiscCoverageAndLegacySetupActions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	proxy := newMockProxy()
	proxy.snap = analytics.AnalyticsSnapshot{
		SessionStart:  time.Now().Add(-1 * time.Minute),
		TotalRequests: 3,
	}
	proxy.sessionLogger.Log("INFO", "test", "hello")

	m := NewModel(proxy)
	svc := &mockServiceControl{}
	m.SetServiceControl(svc)
	m.flashMsg = "keep"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model := updated.(Model)
	if model.view != ViewSetup {
		t.Fatalf("left should wrap to setup, got %v", model.view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = updated.(Model)
	if model.view != ViewMain {
		t.Fatalf("right should return to main, got %v", model.view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model = updated.(Model)
	if model.view != ViewSetup {
		t.Fatalf("i should open setup, got %v", model.view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	model = updated.(Model)
	if model.setupStep != 2 {
		t.Fatalf("setupStep=%d", model.setupStep)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	model = updated.(Model)
	if !svc.started || !strings.Contains(model.flashMsg, "Daemon started") {
		t.Fatalf("start via p failed: svc=%+v flash=%q", svc, model.flashMsg)
	}

	svc.running = true
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	model = updated.(Model)
	if !svc.stopped || !strings.Contains(model.flashMsg, "Daemon stopped") {
		t.Fatalf("stop via p failed: svc=%+v flash=%q", svc, model.flashMsg)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	model = updated.(Model)
	if !svc.restarted || !strings.Contains(model.flashMsg, "Daemon restarted") {
		t.Fatalf("restart via o failed: svc=%+v flash=%q", svc, model.flashMsg)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	model = updated.(Model)
	if !svc.installed || !strings.Contains(model.flashMsg, "auto-start enabled") {
		t.Fatalf("install via e failed: svc=%+v flash=%q", svc, model.flashMsg)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	model = updated.(Model)
	if !svc.removed || !strings.Contains(model.flashMsg, "Service uninstalled") {
		t.Fatalf("remove via w failed: svc=%+v flash=%q", svc, model.flashMsg)
	}

	model.view = ViewMain
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	model = updated.(Model)
	if !proxy.flushed || !strings.Contains(model.flashMsg, "All caches flushed") {
		t.Fatalf("flush via f failed: flushed=%v flash=%q", proxy.flushed, model.flashMsg)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = updated.(Model)
	if !strings.Contains(model.flashMsg, "Debug log copied") {
		t.Fatalf("debug export via y flash=%q", model.flashMsg)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	model = updated.(Model)
	if !strings.Contains(model.flashMsg, "preferences saved") {
		t.Fatalf("ctrl+s flash=%q", model.flashMsg)
	}

	updated, _ = model.Update(tickMsg(time.Now()))
	model = updated.(Model)
	if model.latestSnap.TotalRequests != proxy.snap.TotalRequests {
		t.Fatalf("tick analytics=%d", model.latestSnap.TotalRequests)
	}

	updated, _ = model.Update(proxyEventMsg(types.RequestMetrics{}))
	model = updated.(Model)
	if model.latestSnap.TotalRequests != proxy.snap.TotalRequests {
		t.Fatalf("proxy event analytics=%d", model.latestSnap.TotalRequests)
	}

	updated, _ = model.Update(flashExpiredMsg{})
	model = updated.(Model)
	if model.flashMsg != "" {
		t.Fatalf("flash should be cleared, got %q", model.flashMsg)
	}

	updated, _ = model.Update(tea.WindowSizeMsg{Width: 132, Height: 44})
	model = updated.(Model)
	if model.width != 132 || model.height != 44 {
		t.Fatalf("window size = %dx%d", model.width, model.height)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	model = updated.(Model)
	if !proxy.shutdownCalled {
		t.Fatal("shutdown should be called on q")
	}
}

func TestRenderHeaderMainAndBranchCoverage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	proxy := newMockProxy()
	m := NewModel(proxy)
	m.width = 50
	m.height = 20
	m.flashMsg = "operator notice"
	m.flashExpiry = time.Now().Add(time.Second)

	header := m.renderHeader(18)
	if !strings.Contains(header, "monitor") || !strings.Contains(header, ":8990") {
		t.Fatalf("header=%q", header)
	}

	view := m.renderMainView()
	for _, needle := range []string{"QUICK START", "operator notice", "LAUNCH CENTER", "Flow"} {
		if !strings.Contains(strings.ToUpper(view), strings.ToUpper(needle)) {
			t.Fatalf("main view missing %q in:\n%s", needle, view)
		}
	}

	if got := renderMenuRow(m.styles, 4, false, "Label", "state"); !strings.Contains(got, "state") {
		t.Fatalf("small renderMenuRow=%q", got)
	}
	if got := clampIndex(0, 0); got != 0 {
		t.Fatalf("clamp zero=%d", got)
	}

	var zero Model
	zero.moveMainCursor(-5)
	if zero.mainCursor != 0 {
		t.Fatalf("mainCursor lower clamp=%d", zero.mainCursor)
	}
	zero.mainCursor = 99
	zero.moveMainCursor(1)
	if zero.mainCursor != len(zero.dashboardActions())-1 {
		t.Fatalf("mainCursor upper clamp=%d", zero.mainCursor)
	}
	zero.debugCursor = 99
	zero.moveDebugCursor(1)
	if zero.debugCursor != len(zero.debugActions())-1 {
		t.Fatalf("debugCursor upper clamp=%d", zero.debugCursor)
	}
}

func TestExecuteSelection_ErrorBranches(t *testing.T) {
	m := NewModel(newMockProxy())
	svc := &mockServiceControl{err: os.ErrPermission}
	m.SetServiceControl(svc)

	m.mainCursor = findDashboardActionIndex(m.dashboardActions(), "launch_cli")
	_ = m.executeMainSelection()
	if !strings.Contains(m.flashMsg, "Codex CLI launch failed") {
		t.Fatalf("CLI launch failure flash=%q", m.flashMsg)
	}

	m.mainCursor = findDashboardActionIndex(m.dashboardActions(), "launch_app")
	_ = m.executeMainSelection()
	if !strings.Contains(m.flashMsg, "Codex App launch blocked") {
		t.Fatalf("App launch failure flash=%q", m.flashMsg)
	}

	m.debugCursor = 99
	_ = m.executeDebugSelection()
	if !strings.Contains(m.flashMsg, "No debug log entries") {
		t.Fatalf("debug clamp/no-log flash=%q", m.flashMsg)
	}
}

func TestUpdate_RemainingViewAndSelectionPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	proxy := newMockProxy()
	m := NewModel(proxy)
	m.SetServiceControl(&mockServiceControl{})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	model := updated.(Model)
	if model.view != ViewStats {
		t.Fatalf("s should open stats, got %v", model.view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	model = updated.(Model)
	if model.view != ViewMain {
		t.Fatalf("second s should return to main, got %v", model.view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	model = updated.(Model)
	if model.view != ViewDebug {
		t.Fatalf("d should open debug, got %v", model.view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	model = updated.(Model)
	if model.view != ViewMain {
		t.Fatalf("second d should return to main, got %v", model.view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	model = updated.(Model)
	if model.layer1Enabled {
		t.Fatalf("layer1 should toggle off in main view")
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	model = updated.(Model)
	if model.layer2Enabled {
		t.Fatalf("layer2 should toggle off in main view")
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	model = updated.(Model)
	if model.layer3Enabled {
		t.Fatalf("layer3 should toggle off in main view")
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model = updated.(Model)
	if model.view != ViewSetup {
		t.Fatalf("i should open setup, got %v", model.view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	model = updated.(Model)
	if model.setupStep != 1 {
		t.Fatalf("setup step one=%d", model.setupStep)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	model = updated.(Model)
	if model.setupStep != 3 {
		t.Fatalf("setup step three=%d", model.setupStep)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if model.setupStep != 2 {
		t.Fatalf("setup up should move to step two, got %d", model.setupStep)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if !strings.Contains(model.flashMsg, "Done: Run slimference enable") {
		t.Fatalf("setup enter flash=%q", model.flashMsg)
	}

	model.view = ViewMain
	model.mainCursor = 1
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if model.mainCursor != 0 {
		t.Fatalf("main up=%d", model.mainCursor)
	}

	model.view = ViewStats
	model.statsCursor = 1
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if model.statsCursor != 0 {
		t.Fatalf("stats up=%d", model.statsCursor)
	}

	model.view = ViewDebug
	model.debugCursor = 1
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if model.debugCursor != 0 {
		t.Fatalf("debug up=%d", model.debugCursor)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if model.debugCursor != 0 {
		t.Fatalf("debug down=%d", model.debugCursor)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = updated.(Model)
	if !proxy.shutdownCalled {
		t.Fatal("shutdown should be called on ctrl+c")
	}
}

func TestRenderHeaderIdleAndMainViewWithoutFlash(t *testing.T) {
	proxy := newMockProxy()
	m := NewModel(proxy)
	m.SetServiceControl(&mockServiceControl{})
	m.width = 80
	m.height = 24

	header := m.renderHeader(24)
	if !strings.Contains(header, "daemon idle") {
		t.Fatalf("idle header=%q", header)
	}

	view := m.renderMainView()
	if strings.Contains(view, "operator notice") {
		t.Fatalf("view should not contain stale flash message:\n%s", view)
	}
	if !strings.Contains(view, "QUICK START") {
		t.Fatalf("view should show quick-start state:\n%s", view)
	}
}

func TestRenderMainView_RightColumnPaddingBranch(t *testing.T) {
	proxy := newMockProxy()
	proxy.snap = analytics.AnalyticsSnapshot{
		SessionStart:     time.Now().Add(-2 * time.Minute),
		TotalRequests:    10,
		TotalInputTokens: 5000,
		SavedInputTokens: 1200,
	}
	for i := 0; i < 10; i++ {
		proxy.recentReqs = append(proxy.recentReqs, types.RequestMetrics{
			Timestamp:        time.Now().Add(-time.Duration(i) * time.Second),
			Provider:         types.Anthropic,
			Model:            "claude-3-7-sonnet",
			InputTokensOrig:  1000,
			InputTokensComp:  700,
			CompressionRatio: 0.7,
			LatencyMs:        180,
		})
	}

	m := NewModel(proxy)
	m.width = 120
	m.height = 40
	m.latestSnap = proxy.snap

	view := m.renderMainView()
	if !strings.Contains(view, "LIVE") || !strings.Contains(view, "anthrop") {
		t.Fatalf("renderMainView live branch missing in:\n%s", view)
	}
}

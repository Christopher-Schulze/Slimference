package tui

import (
	"errors"
	"os"
	"path/filepath"
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

func TestDashboardActions_ServiceAndAutoStartStates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	m := NewModel(newMockProxy())
	svc := &mockServiceControl{}
	m.SetServiceControl(svc)

	actions := m.dashboardActions()
	if got := findDashboardActionIndex(actions, "daemon"); got < 0 || actions[got].label != "Start daemon" {
		t.Fatalf("daemon action=%v", actions)
	}
	if got := findDashboardActionIndex(actions, "autostart"); got < 0 || actions[got].label != "Enable auto-start" {
		t.Fatalf("autostart action=%v", actions)
	}

	svc.running = true
	if err := os.MkdirAll(filepath.Join(home, "Library", "LaunchAgents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "Library", "LaunchAgents", "com.slimference.daemon.plist"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	actions = m.dashboardActions()
	if got := findDashboardActionIndex(actions, "daemon"); got < 0 || actions[got].label != "Stop daemon" || !strings.Contains(actions[got].state, "PID 1234") {
		t.Fatalf("running daemon action=%v", actions)
	}
	if got := findDashboardActionIndex(actions, "autostart"); got < 0 || actions[got].label != "Disable auto-start" {
		t.Fatalf("running autostart action=%v", actions)
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

func TestExecuteMainSelection_AllDashboardActions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	proxy := newMockProxy()
	m := NewModel(proxy)
	svc := &mockServiceControl{}
	m.SetServiceControl(svc)

	runAction := func(id string) {
		actions := m.dashboardActions()
		idx := findDashboardActionIndex(actions, id)
		if idx < 0 {
			t.Fatalf("action %q missing in %v", id, actions)
		}
		m.mainCursor = idx
		_ = m.executeMainSelection()
	}

	runAction("daemon")
	if !svc.started || !strings.Contains(m.flashMsg, "Daemon started") {
		t.Fatalf("start failed: svc=%+v flash=%q", svc, m.flashMsg)
	}

	svc.running = true
	runAction("daemon")
	if !svc.stopped || !strings.Contains(m.flashMsg, "Daemon stopped") {
		t.Fatalf("stop failed: svc=%+v flash=%q", svc, m.flashMsg)
	}

	runAction("restart")
	if !svc.restarted || !strings.Contains(m.flashMsg, "Daemon restarted") {
		t.Fatalf("restart failed: svc=%+v flash=%q", svc, m.flashMsg)
	}

	runAction("autostart")
	if !svc.installed || !strings.Contains(m.flashMsg, "Auto-start enabled") {
		t.Fatalf("enable autostart failed: svc=%+v flash=%q", svc, m.flashMsg)
	}

	if err := os.MkdirAll(filepath.Join(home, "Library", "LaunchAgents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "Library", "LaunchAgents", "com.slimference.daemon.plist"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runAction("autostart")
	if !svc.removed || !strings.Contains(m.flashMsg, "Auto-start disabled") {
		t.Fatalf("disable autostart failed: svc=%+v flash=%q", svc, m.flashMsg)
	}

	runAction("transparent")
	if !svc.transparentEnabled || !strings.Contains(m.flashMsg, "Global lab armed") {
		t.Fatalf("arm transparent failed: svc=%+v flash=%q", svc, m.flashMsg)
	}
	runAction("transparent")
	if !svc.transparentDisabled || !strings.Contains(m.flashMsg, "Global lab disarmed") {
		t.Fatalf("disarm transparent failed: svc=%+v flash=%q", svc, m.flashMsg)
	}

	runAction("codex")
	if m.codexEnabled || !strings.Contains(m.flashMsg, "Codex: OFF") {
		t.Fatalf("codex toggle flash=%q enabled=%v", m.flashMsg, m.codexEnabled)
	}
	runAction("layer1")
	if m.layer1Enabled || !strings.Contains(m.flashMsg, "Layer 1: OFF") {
		t.Fatalf("layer1 toggle flash=%q enabled=%v", m.flashMsg, m.layer1Enabled)
	}
	runAction("layer2")
	if m.layer2Enabled || !strings.Contains(m.flashMsg, "Layer 2: OFF") {
		t.Fatalf("layer2 toggle flash=%q enabled=%v", m.flashMsg, m.layer2Enabled)
	}
	runAction("layer3")
	if m.layer3Enabled || !strings.Contains(m.flashMsg, "Layer 3: OFF") {
		t.Fatalf("layer3 toggle flash=%q enabled=%v", m.flashMsg, m.layer3Enabled)
	}
	runAction("flush")
	if !proxy.flushed || !strings.Contains(m.flashMsg, "All caches flushed") {
		t.Fatalf("flush failed: flushed=%v flash=%q", proxy.flushed, m.flashMsg)
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

	for _, id := range []string{"daemon", "restart", "autostart", "transparent"} {
		m.mainCursor = findDashboardActionIndex(m.dashboardActions(), id)
		_ = m.executeMainSelection()
		if !strings.Contains(m.flashMsg, "failed") {
			t.Fatalf("expected failure flash for %s, got %q", id, m.flashMsg)
		}
	}

	enableFail := NewModel(newMockProxy())
	enableFail.SetServiceControl(&mockServiceControl{
		err: errors.New("boom"),
		transparentStatus: TransparentStatus{
			CAExists:           true,
			CATrusted:          true,
			AutoStartInstalled: true,
		},
	})
	enableFail.mainCursor = findDashboardActionIndex(enableFail.dashboardActions(), "transparent")
	_ = enableFail.executeMainSelection()
	if !strings.Contains(enableFail.flashMsg, "Global lab arm failed") {
		t.Fatalf("transparent enable failure flash=%q", enableFail.flashMsg)
	}

	disableFail := NewModel(newMockProxy())
	disableFail.SetServiceControl(&mockServiceControl{
		err: errors.New("boom"),
		transparentStatus: TransparentStatus{
			ProxyArmed: true,
		},
	})
	disableFail.mainCursor = findDashboardActionIndex(disableFail.dashboardActions(), "transparent")
	_ = disableFail.executeMainSelection()
	if !strings.Contains(disableFail.flashMsg, "Global lab disarm failed") {
		t.Fatalf("transparent disable failure flash=%q", disableFail.flashMsg)
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
	if !strings.Contains(model.flashMsg, "Daemon restarted") {
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
	for _, needle := range []string{"CONTROL SURFACE", "PROVIDER MAP", "TRAFFIC", "daemon live", "Claude Code", "Layer 2 semantic"} {
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
	for _, needle := range []string{"QUICK START", "operator notice", "CONTROL SURFACE", "Flow"} {
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
	home := t.TempDir()
	t.Setenv("HOME", home)

	m := NewModel(newMockProxy())
	svc := &mockServiceControl{running: true, err: os.ErrPermission}
	m.SetServiceControl(svc)

	m.mainCursor = findDashboardActionIndex(m.dashboardActions(), "daemon")
	_ = m.executeMainSelection()
	if !strings.Contains(m.flashMsg, "Stop failed") {
		t.Fatalf("daemon stop failure flash=%q", m.flashMsg)
	}

	if err := os.MkdirAll(filepath.Join(home, "Library", "LaunchAgents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "Library", "LaunchAgents", "com.slimference.daemon.plist"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.mainCursor = findDashboardActionIndex(m.dashboardActions(), "autostart")
	_ = m.executeMainSelection()
	if !strings.Contains(m.flashMsg, "Disable auto-start failed") {
		t.Fatalf("autostart disable failure flash=%q", m.flashMsg)
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

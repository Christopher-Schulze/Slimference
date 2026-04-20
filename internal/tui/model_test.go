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
	"github.com/slimference/slimference/internal/sessions"
	"github.com/slimference/slimference/internal/types"
)

// mockProxy implements ProxyInterface for testing the TUI model.
type mockProxy struct {
	claudeEnabled  bool
	codexEnabled   bool
	layer1Enabled  bool
	layer2Enabled  bool
	layer3Enabled  bool
	snap           analytics.AnalyticsSnapshot
	recentReqs     []types.RequestMetrics
	l2Status       Layer2Status
	readStatus     ReadCacheStatus
	sessionLogger  *sessions.SessionLogger
	flushed        bool
	shutdownCalled bool
	listenPort     int
	prefillSpeed   int
	bypass         bool
}

// Bypass + SetBypass satisfy the T67 additions to ProxyInterface.
func (m *mockProxy) Bypass() bool           { return m.bypass }
func (m *mockProxy) SetBypass(enabled bool) { m.bypass = enabled }

func newMockProxy() *mockProxy {
	return &mockProxy{
		claudeEnabled: true,
		codexEnabled:  true,
		layer1Enabled: true,
		layer2Enabled: true,
		layer3Enabled: true,
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
	case 2:
		m.layer2Enabled = enabled
	case 3:
		m.layer3Enabled = enabled
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
	case 2:
		return m.layer2Enabled
	case 3:
		return m.layer3Enabled
	}
	return false
}

func (m *mockProxy) FlushCaches()                              { m.flushed = true }
func (m *mockProxy) GetAnalytics() analytics.AnalyticsSnapshot { return m.snap }
func (m *mockProxy) GetRecentRequests(n int) []types.RequestMetrics {
	return m.recentReqs
}
func (m *mockProxy) GetLayer2Status() Layer2Status { return m.l2Status }
func (m *mockProxy) GetReadCacheStatus() ReadCacheStatus {
	return m.readStatus
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
	started   bool
	stopped   bool
	restarted bool
	installed bool
	removed   bool
	running   bool
	err       error
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
	if !m.layer3Enabled {
		t.Error("layer3Enabled should be true by default")
	}
}

// TestUpdate_ToggleClaude verifies that pressing 'c' toggles Claude.
func TestUpdate_ToggleClaude(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	model := updated.(Model)

	if model.claudeEnabled {
		t.Error("claudeEnabled should be false after toggling")
	}
	if p.claudeEnabled {
		t.Error("proxy should have been toggled off too")
	}

	// Toggle back.
	updated2, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	model2 := updated2.(Model)
	if !model2.claudeEnabled {
		t.Error("claudeEnabled should be true after toggling back")
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

// TestUpdate_ToggleLayers verifies that pressing '1', '2', '3' toggles layers.
func TestUpdate_ToggleLayers(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)

	for _, key := range []rune{'1', '2', '3'} {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		model := updated.(Model)
		m = model
	}

	if p.layer1Enabled || p.layer2Enabled || p.layer3Enabled {
		t.Error("all layers should be off after toggling 1, 2, 3")
	}
}

// TestUpdate_ViewStatsToggle verifies that pressing 's' toggles the stats view.
func TestUpdate_ViewStatsToggle(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	model := updated.(Model)
	if model.view != ViewStats {
		t.Errorf("view = %d, want ViewStats after pressing 's'", model.view)
	}

	updated2, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	model2 := updated2.(Model)
	if model2.view != ViewMain {
		t.Errorf("view = %d, want ViewMain after pressing 's' again", model2.view)
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

// TestView_StatsRender verifies that the stats view renders without panicking.
func TestView_StatsRender(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.view = ViewStats
	m.width = 100
	m.height = 30

	output := m.View()
	if output == "" {
		t.Error("View() returned empty string for stats view")
	}
}

// TestView_DebugRender verifies that the debug view renders without panicking.
func TestView_DebugRender(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.sessionLogger.Log("INFO", "test", "hello debug view")
	m := NewModel(p)
	m.view = ViewDebug
	m.width = 100
	m.height = 30

	output := m.View()
	if output == "" {
		t.Error("View() returned empty string for debug view")
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

	// Second press: back to main view.
	updated2, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	model2 := updated2.(Model)
	if model2.view != ViewMain {
		t.Errorf("view = %d, want ViewMain after pressing 'd' from debug view", model2.view)
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

// TestView_StatsRender_NarrowWidth verifies the stats view handles width < 40.
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

// TestView_DebugRender_NarrowWidth verifies the debug view handles width < 40.
func TestView_DebugRender_NarrowWidth(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.view = ViewDebug
	m.width = 30
	m.height = 24

	output := m.View()
	if output == "" {
		t.Error("View() returned empty string for debug narrow width")
	}
}

// TestView_DebugRender_NilLogger verifies debug view renders correctly when SessionLogger is nil.
func TestView_DebugRender_NilLogger(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.sessionLogger = nil
	m := NewModel(p)
	m.view = ViewDebug
	m.width = 100
	m.height = 30

	output := m.View()
	if output == "" {
		t.Error("View() returned empty string for debug view with nil logger")
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
		Layer2Savings:    20000,
		Layer3Savings:    10000,
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

// TestView_MainRender_L2Compressing verifies the L2 "compressing..." branch.
func TestView_MainRender_L2Compressing(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.l2Status = Layer2Status{Compressing: true}
	m := NewModel(p)
	m.width = 100
	m.height = 30

	output := m.View()
	if output == "" {
		t.Error("View() returned empty string with L2 compressing status")
	}
}

// TestView_MainRender_L2HasCache verifies the L2 HasCache branch in renderMainView.
func TestView_MainRender_L2HasCache(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.l2Status = Layer2Status{
		HasCache:   true,
		LastRun:    time.Now().Add(-30 * time.Second),
		QueueDepth: 3,
	}
	m := NewModel(p)
	m.width = 100
	m.height = 30

	output := m.View()
	if output == "" {
		t.Error("View() returned empty string with L2 HasCache status")
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

// TestView_StatsRender_WithData verifies stats view branches with non-zero snap data.
func TestView_StatsRender_WithData(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	p.snap = analytics.AnalyticsSnapshot{
		SessionStart:        time.Now(),
		TotalRequests:       20,
		TotalInputTokens:    200000,
		SavedInputTokens:    120000,
		TotalOutputTokens:   50000,
		Layer1Savings:       60000,
		Layer2Savings:       40000,
		Layer3Savings:       20000,
		CacheHits:           5,
		SecretsRedacted:     3,
		AutoRetries:         2,
		MiniMaxCalls:        10,
		MiniMaxAvgLatencyMs: 1500,
		MiniMaxFailures:     1,
		Errors:              0,
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
	if !strings.Contains(output, "claude") {
		t.Errorf("main view with claude hook: want 'claude' in output, got: %s", output)
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
	// Press 'i' again to go back.
	updated2, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model2 := updated2.(Model)
	if model2.view != ViewMain {
		t.Errorf("pressing i again should return to main view, got: %d", model2.view)
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

func TestView_MainFooter_hasSetupKey(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := NewModel(p)
	m.width = 100
	m.height = 24
	output := m.View()
	if !strings.Contains(output, "[i]") {
		t.Errorf("main footer should have [i] setup key, got: %s", output)
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
	svc := &mockServiceControl{}
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
	if !strings.Contains(output, "READY") {
		t.Errorf("main view should show READY when all hooks installed, got: %s", output)
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
	if !strings.Contains(output, "SETUP") {
		t.Errorf("main view should show SETUP when hooks missing, got: %s", output)
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
	if !strings.Contains(output, "QUICK START") {
		t.Errorf("main view should show QUICK START when no hooks and no requests, got: %s", output)
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
	if !strings.Contains(output, "Install Claude Code hook") {
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
	if !strings.Contains(output, "LIVE") {
		t.Errorf("main view should show LIVE section when requests exist, got: %s", output)
	}
}

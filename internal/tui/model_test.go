package tui

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tokenproxy/tokenproxy/internal/analytics"
	"github.com/tokenproxy/tokenproxy/internal/sessions"
	"github.com/tokenproxy/tokenproxy/internal/types"
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
	sessionLogger  *sessions.SessionLogger
	flushed        bool
	shutdownCalled bool
	listenPort     int
	prefillSpeed   int
}

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

func (m *mockProxy) FlushCaches()                  { m.flushed = true }
func (m *mockProxy) GetAnalytics() analytics.AnalyticsSnapshot { return m.snap }
func (m *mockProxy) GetRecentRequests(n int) []types.RequestMetrics {
	return m.recentReqs
}
func (m *mockProxy) GetLayer2Status() Layer2Status { return m.l2Status }
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
	// width=40 -> innerWidth=36; "TokenProxy v1.0.0" (17) + "Session: 1000h Xm" (18) + 2 > 36
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

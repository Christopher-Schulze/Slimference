package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tokenproxy/tokenproxy/internal/analytics"
	"github.com/tokenproxy/tokenproxy/internal/sessions"
	"github.com/tokenproxy/tokenproxy/internal/types"
)

// Version is the display version string.
const Version = "1.0.0"

// ViewMode selects which view is rendered.
type ViewMode int

const (
	ViewMain  ViewMode = iota // default live dashboard
	ViewStats                  // detailed statistics
	ViewDebug                  // debug log tail
)

// ProxyInterface defines the subset of proxy.Proxy the TUI requires.
// Using an interface decouples the tui package from the proxy package
// (preventing the import cycle: proxy -> tui -> proxy).
type ProxyInterface interface {
	SetProviderEnabled(prov types.Provider, enabled bool)
	SetLayerEnabled(layer int, enabled bool)
	IsProviderEnabled(prov types.Provider) bool
	IsLayerEnabled(layer int) bool
	FlushCaches()
	GetAnalytics() analytics.AnalyticsSnapshot
	GetRecentRequests(n int) []types.RequestMetrics
	GetLayer2Status() Layer2Status
	SessionLogger() SessionLoggerInterface
	Shutdown(ctx context.Context) error
	Config() ProxyConfigInterface
}

// HookStatus records which LLM agent hooks are currently installed.
type HookStatus struct {
	Claude bool
	Codex  bool
}

// SetHookStatus updates the hook installation status shown in the main view.
func (m *Model) SetHookStatus(s HookStatus) {
	m.hookStatus = s
}

// Layer2Status provides the TUI with a snapshot of Layer 2 compression state.
// It is a plain value type populated by the proxy adapter.
type Layer2Status struct {
	HasCache    bool
	Compressing bool
	LastRun     time.Time
	QueueDepth  int
}

// SessionLoggerInterface exposes minimal session logger methods needed by the TUI.
type SessionLoggerInterface interface {
	Recent(n int) []sessions.LogEntry
	Format(entry sessions.LogEntry) string
}

// ProxyConfigInterface exposes the minimal config fields the TUI needs.
type ProxyConfigInterface interface {
	GetListenPort() int
	GetPrefillSpeed() int
}

// Message types for the BubbleTea Update loop.

type tickMsg time.Time

// proxyEventMsg carries a request metrics update from the proxy to the TUI.
type proxyEventMsg types.RequestMetrics

// flashExpiredMsg signals that the flash message should be cleared.
type flashExpiredMsg struct{}

// Model is the BubbleTea model for the TokenProxy TUI.
// It holds all display state and communicates with the proxy via the ProxyInterface.
type Model struct {
	proxy        ProxyInterface
	keys         KeyMap
	styles       Styles
	sessionStart time.Time

	// Toggle states (mirrored from proxy for rendering).
	claudeEnabled bool
	codexEnabled  bool
	layer1Enabled bool
	layer2Enabled bool
	layer3Enabled bool

	// Current view.
	view ViewMode

	// Live data.
	latestSnap analytics.AnalyticsSnapshot

	// Terminal dimensions.
	width  int
	height int

	// Hook installation status (set once at startup).
	hookStatus HookStatus

	// Flash message.
	flashMsg    string
	flashExpiry time.Time
}

// NewModel creates a TUI model wired to the given proxy.
func NewModel(proxy ProxyInterface) Model {
	return Model{
		proxy:         proxy,
		keys:          DefaultKeyMap(),
		styles:        NewStyles(),
		sessionStart:  time.Now(),
		claudeEnabled: proxy.IsProviderEnabled(types.Anthropic),
		codexEnabled:  proxy.IsProviderEnabled(types.OpenAI),
		layer1Enabled: proxy.IsLayerEnabled(1),
		layer2Enabled: proxy.IsLayerEnabled(2),
		layer3Enabled: proxy.IsLayerEnabled(3),
		view:          ViewMain,
		width:         80,
		height:        24,
	}
}

// Init starts the tick timer and returns the initial command.
func (m Model) Init() tea.Cmd {
	return tickCmd()
}

// Update processes messages and returns the updated model and next command.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		switch msg.String() {
		case "c":
			m.claudeEnabled = !m.claudeEnabled
			m.proxy.SetProviderEnabled(types.Anthropic, m.claudeEnabled)
			m.setFlash(fmt.Sprintf("Claude Code: %s", onOff(m.claudeEnabled)))

		case "x":
			m.codexEnabled = !m.codexEnabled
			m.proxy.SetProviderEnabled(types.OpenAI, m.codexEnabled)
			m.setFlash(fmt.Sprintf("Codex: %s", onOff(m.codexEnabled)))

		case "1":
			m.layer1Enabled = !m.layer1Enabled
			m.proxy.SetLayerEnabled(1, m.layer1Enabled)
			m.setFlash(fmt.Sprintf("Layer 1: %s", onOff(m.layer1Enabled)))

		case "2":
			m.layer2Enabled = !m.layer2Enabled
			m.proxy.SetLayerEnabled(2, m.layer2Enabled)
			m.setFlash(fmt.Sprintf("Layer 2: %s", onOff(m.layer2Enabled)))

		case "3":
			m.layer3Enabled = !m.layer3Enabled
			m.proxy.SetLayerEnabled(3, m.layer3Enabled)
			m.setFlash(fmt.Sprintf("Layer 3: %s", onOff(m.layer3Enabled)))

		case "s":
			if m.view == ViewStats {
				m.view = ViewMain
			} else {
				m.view = ViewStats
			}

		case "d":
			if m.view == ViewDebug {
				m.view = ViewMain
			} else {
				m.view = ViewDebug
			}

		case "f":
			m.proxy.FlushCaches()
			m.setFlash("All caches flushed")
			return m, flashTimer(2 * time.Second)

		case "q", "ctrl+c":
			m.proxy.Shutdown(context.Background())
			return m, tea.Quit
		}

	case tickMsg:
		m.latestSnap = m.proxy.GetAnalytics()
		return m, tickCmd()

	case proxyEventMsg:
		// Immediate update when a new request arrives.
		m.latestSnap = m.proxy.GetAnalytics()
		return m, nil

	case flashExpiredMsg:
		m.flashMsg = ""

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	return m, nil
}

// View renders the current model state to a string.
func (m Model) View() string {
	switch m.view {
	case ViewStats:
		return m.renderStatsView()
	case ViewDebug:
		return m.renderDebugView()
	default:
		return m.renderMainView()
	}
}

// SendProxyEvent is called from the proxy goroutine to push a request event into the TUI.
func SendProxyEvent(program *tea.Program, rm types.RequestMetrics) {
	program.Send(proxyEventMsg(rm))
}

// setFlash sets a temporary status message.
func (m *Model) setFlash(msg string) {
	m.flashMsg = msg
	m.flashExpiry = time.Now().Add(2 * time.Second)
}

// tickCmd returns a command that sends a tickMsg after 500ms.
func tickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// flashTimer returns a command that clears the flash message after d.
func flashTimer(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(_ time.Time) tea.Msg {
		return flashExpiredMsg{}
	})
}

// onOff returns "ON" or "OFF".
func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

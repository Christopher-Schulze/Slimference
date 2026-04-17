package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/hooks"
	"github.com/slimference/slimference/internal/sessions"
	"github.com/slimference/slimference/internal/types"
)

var userHomeDirFn = os.UserHomeDir
var writeFileFn = os.WriteFile

// Version is the display version string.
const Version = "2.0.0"

// ViewMode selects which view is rendered.
type ViewMode int

const (
	ViewMain  ViewMode = iota // default live dashboard
	ViewStats                 // detailed statistics
	ViewDebug                 // debug log tail
	ViewSetup                 // install wizard / service management
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
	GetProviderHealth(prov types.Provider) types.ProviderHealthInfo
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

// ServiceControlInterface exposes daemon lifecycle operations the TUI can trigger.
// Implemented by a thin adapter in cmd/slimference/main.go that calls the daemon package.
type ServiceControlInterface interface {
	// StartDaemon forks a background daemon process. Returns error if already running.
	StartDaemon() error
	// StopDaemon stops the running daemon. Returns error if not running.
	StopDaemon() error
	// RestartDaemon stops and starts the daemon.
	RestartDaemon() error
	// InstallService installs the launchd/systemd auto-start service.
	InstallService() error
	// UninstallService removes the auto-start service.
	UninstallService() error
	// DaemonStatus returns (running bool, pid int, port int).
	DaemonStatus() (bool, int, int)
	// InstallHook installs a hook for the given target ("claude" or "codex").
	InstallHook(target string) error
	// RemoveHook removes a hook for the given target.
	RemoveHook(target string) error
}

// Message types for the BubbleTea Update loop.

type tickMsg time.Time

// proxyEventMsg carries a request metrics update from the proxy to the TUI.
type proxyEventMsg types.RequestMetrics

// flashExpiredMsg signals that the flash message should be cleared.
type flashExpiredMsg struct{}

// Model is the BubbleTea model for the Slimference TUI.
// It holds all display state and communicates with the proxy via the ProxyInterface.
type Model struct {
	proxy        ProxyInterface
	svc          ServiceControlInterface
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

	// Setup wizard state.
	setupStep   int    // current wizard step (0=overview, 1..N=individual steps)
	setupAction string // pending action description

	// Flash message.
	flashMsg    string
	flashExpiry time.Time
}

// SetServiceControl sets the service control interface for daemon operations.
func (m *Model) SetServiceControl(svc ServiceControlInterface) {
	m.svc = svc
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

		case "i":
			if m.view == ViewSetup {
				m.view = ViewMain
				m.setupStep = 0
			} else {
				m.view = ViewSetup
				m.setupStep = 0
			}

		case "1":
			if m.view == ViewSetup {
				m.setupStep = 1
				return m, nil
			}
			m.layer1Enabled = !m.layer1Enabled
			m.proxy.SetLayerEnabled(1, m.layer1Enabled)
			m.setFlash(fmt.Sprintf("Layer 1: %s", onOff(m.layer1Enabled)))

		case "2":
			if m.view == ViewSetup {
				m.setupStep = 2
				return m, nil
			}
			m.layer2Enabled = !m.layer2Enabled
			m.proxy.SetLayerEnabled(2, m.layer2Enabled)
			m.setFlash(fmt.Sprintf("Layer 2: %s", onOff(m.layer2Enabled)))

		case "3":
			if m.view == ViewSetup {
				m.setupStep = 3
				return m, nil
			}
			m.layer3Enabled = !m.layer3Enabled
			m.proxy.SetLayerEnabled(3, m.layer3Enabled)
			m.setFlash(fmt.Sprintf("Layer 3: %s", onOff(m.layer3Enabled)))

		case "enter":
			if m.view == ViewSetup && m.svc != nil {
				m.executeSetupStep()
				return m, flashTimer(3 * time.Second)
			}

		case "p":
			if m.view == ViewSetup && m.svc != nil {
				running, _, _ := m.svc.DaemonStatus()
				if running {
					if err := m.svc.StopDaemon(); err != nil {
						m.setFlash("Stop failed: " + err.Error())
					} else {
						m.setFlash("Daemon stopped")
					}
				} else {
					if err := m.svc.StartDaemon(); err != nil {
						m.setFlash("Start failed: " + err.Error())
					} else {
						m.setFlash("Daemon started")
					}
				}
				return m, flashTimer(3 * time.Second)
			}

		case "o":
			if m.view == ViewSetup && m.svc != nil {
				if err := m.svc.RestartDaemon(); err != nil {
					m.setFlash("Restart failed: " + err.Error())
				} else {
					m.setFlash("Daemon restarted")
				}
				return m, flashTimer(3 * time.Second)
			}

		case "e":
			if m.view == ViewSetup && m.svc != nil {
				if err := m.svc.InstallService(); err != nil {
					m.setFlash("Install failed: " + err.Error())
				} else {
					m.setFlash("Service installed (auto-start enabled)")
				}
				return m, flashTimer(3 * time.Second)
			}

		case "w":
			if m.view == ViewSetup && m.svc != nil {
				if err := m.svc.UninstallService(); err != nil {
					m.setFlash("Uninstall failed: " + err.Error())
				} else {
					m.setFlash("Service uninstalled")
				}
				return m, flashTimer(3 * time.Second)
			}

		case "f":
			m.proxy.FlushCaches()
			m.setFlash("All caches flushed")
			return m, flashTimer(2 * time.Second)

		case "y":
			path := m.copyDebugLog()
			if path != "" {
				m.setFlash("Debug log copied to " + path)
			} else {
				m.setFlash("No debug log entries to copy")
			}
			return m, flashTimer(3 * time.Second)

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
	case ViewSetup:
		return m.renderSetupView()
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

// copyDebugLog writes the recent debug log entries to a timestamped file.
// Returns the file path on success, or "" if nothing to copy.
func (m *Model) copyDebugLog() string {
	logger := m.proxy.SessionLogger()
	if logger == nil {
		return ""
	}
	entries := logger.Recent(200)
	if len(entries) == 0 {
		return ""
	}

	home, err := userHomeDirFn()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".slimference", "exports")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return ""
	}

	name := fmt.Sprintf("debug-%s.log", time.Now().Format("2006-01-02-150405"))
	path := filepath.Join(dir, name)

	var buf []byte
	for _, e := range entries {
		buf = append(buf, logger.Format(e)...)
		buf = append(buf, '\n')
	}
	if err := writeFileFn(path, buf, 0644); err != nil {
		return ""
	}
	return path
}

// setupStep describes a single wizard step with an action.
type setupStep struct {
	label   string               // display label
	check   func() bool          // true = already done
	action  func(m *Model) error // execute this step
	confirm string               // "Press Enter to <confirm>"
}

// setupSteps returns the ordered list of wizard steps.
func (m *Model) setupSteps() []setupStep {
	steps := []setupStep{
		{
			label:   "Install Claude Code hook",
			check:   func() bool { return m.hookStatus.Claude },
			action:  func(m *Model) error { return m.svc.InstallHook("claude") },
			confirm: "Install Claude Code hook",
		},
		{
			label:   "Install Codex hook",
			check:   func() bool { return m.hookStatus.Codex },
			action:  func(m *Model) error { return m.svc.InstallHook("codex") },
			confirm: "Install Codex hook",
		},
		{
			label: "Install auto-start service (launchd)",
			check: func() bool {
				home, _ := userHomeDirFn()
				_, err := os.Stat(filepath.Join(home, "Library", "LaunchAgents", "com.slimference.daemon.plist"))
				return err == nil
			},
			action:  func(m *Model) error { return m.svc.InstallService() },
			confirm: "Install launchd auto-start service",
		},
	}
	return steps
}

// executeSetupStep runs the action for the current setup step.
func (m *Model) executeSetupStep() {
	steps := m.setupSteps()
	if m.setupStep < 1 || m.setupStep > len(steps) {
		return
	}
	step := steps[m.setupStep-1]
	if step.check() {
		m.setFlash("Already done: " + step.label)
		return
	}
	if err := step.action(m); err != nil {
		m.setFlash("Error: " + err.Error())
		return
	}
	// Refresh hook status after install.
	if home, err := userHomeDirFn(); err == nil {
		claude, codex := hooks.InstalledStatus(home)
		m.hookStatus = HookStatus{Claude: claude, Codex: codex}
	}
	m.setFlash("Done: " + step.label)
}

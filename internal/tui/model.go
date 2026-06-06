package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/buildinfo"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/hooks"
	"github.com/slimference/slimference/internal/sessions"
	"github.com/slimference/slimference/internal/types"
)

var userHomeDirFn = os.UserHomeDir
var writeFileFn = os.WriteFile
var chmodFn = os.Chmod
var shutdownTimeout = 5 * time.Second

const (
	defaultTickInterval      = 500 * time.Millisecond
	hostBudgetTickInterval   = 2 * time.Second
	statusRefreshMinInterval = 2 * time.Second
)

// Version is the display version string.
var Version = buildinfo.Version

// ViewMode selects which view is rendered.
type ViewMode int

const (
	ViewMain  ViewMode = iota // default live dashboard
	ViewStats                 // detailed statistics
	ViewDebug                 // operator status
	ViewSetup                 // install wizard / service management
	ViewApps                  // per-app routing toggles (Phase H)
	ViewLogs                  // logs and diagnostics export
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
	GetRecentFlights(n int) []dbg.FlightRequestSummary
	GetLayer0Status() Layer0Status
	GetReadCacheStatus() ReadCacheStatus
	GetCheckpointStatus() CheckpointStatus
	GetToolArchiveStatus() ToolArchiveStatus
	GetQualityStatus() QualityStatus
	GetProductStatus() ProductStatus
	GetProviderHealth(prov types.Provider) types.ProviderHealthInfo
	SessionLogger() SessionLoggerInterface
	Shutdown(ctx context.Context) error
	Config() ProxyConfigInterface
	// T67: bypass flag. Bypass() reports state, SetBypass toggles.
	Bypass() bool
	SetBypass(enabled bool)

	// Phase H — per-app routing.
	// AppEntries returns the current state of every known app.
	AppEntries() []AppEntry
	// SetAppEnabled flips the policy for one app. Errors propagate.
	SetAppEnabled(id string, enabled bool) error
}

// AppEntry is a per-app row for the Apps view. Mirrors
// internal/control.AppEntry without dragging that import into the
// TUI's surface.
type AppEntry struct {
	ID       string
	Enabled  bool
	Detected bool
	BinPath  string
	Routed   int64
	Bypassed int64
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

type Layer0FilterStatus struct {
	Name       string
	Attempts   int64
	Matches    int64
	Misses     int64
	Panics     int64
	BytesSaved int64
	HitRate    float64
	AvgMs      float64
}

type Layer0Status struct {
	Filters    []Layer0FilterStatus
	Attempts   int64
	Matches    int64
	Misses     int64
	Panics     int64
	BytesSaved int64
	HitRate    float64
}

type ReadCacheStatus struct {
	Evaluations     int
	Allows          int
	Blocks          int
	UnchangedBlocks int
	DeltaBlocks     int
	Sessions        int
	TrackedFiles    int
	HitRate         float64 // Blocks / (Blocks + Allows). T57 stretch.
}

type CheckpointStatus struct {
	Count       int
	Captures    int
	Restores    int
	Bytes       int64
	LastCapture time.Time
	LastRestore time.Time
	LastTrigger string
}

type ToolArchiveStatus struct {
	Count        int
	Archived     int
	Expanded     int
	BytesRaw     int64
	BytesStored  int64
	LastArchived time.Time
	LastExpanded time.Time
}

// QualityStatus surfaces the T77 quality signals (re-read, cache-miss
// spike, net savings) so the TUI can render them without depending on
// internal/quality or the proxy package directly.
type QualityStatus struct {
	ReReadSessions    int
	ReReadTotalChecks int64
	ReReadTotalHits   int64
	ReReadRate        float64
	BaselineHitRate   float64
	SpikeActive       bool
	LastSpikeUnix     int64
	TotalSpikeCount   int64
	TotalSaved        int64
	TotalInvalidation int64
	NetSaved          int64
}

type ProductStatus struct {
	RouteStatus                string
	FallbackReason             string
	RecertStatus               string
	SavingsStatus              string
	BillableInputTokensSaved   int64
	ProviderCacheReadTokens    int64
	ProviderCacheCreateTokens  int64
	OutputWireBytesSaved       int64
	RequestSideBytesReduced    int64
	ToolPruneTokensSaved       int64
	ToolPrunePrunedTools       int64
	ToolPruneReattached        int64
	ToolPruneMisses            int64
	ToolPruneRetries           int64
	OutputReduceInjectedTurns  int64
	OutputReduceObservedTokens int64
	OutputReduceInputOverhead  int64
	CostUSD                    float64
	CacheHits                  int64
	CacheMisses                int64
	ReadDeltaHits              int64
	RepeatedOutputHits         int64
	ChunkDedupHits             int64
	ToolResolutionMisses       int64
	SafetyIssues               int64
	HostBudgetStatus           string
	HostBudgetExceeded         bool
	HostBudgetReasons          []string
	WSSParseFailures           int64
	WSSDegradedSessions        int64
	WSSCompressionErrors       int64
	WSSCompressedMutated       int64
	WSSCompressedInspected     int64
	WSSByteBridgeOnly          bool
	WSSMutationActive          bool
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

// TransparentStatus is the TUI-facing snapshot of system transparent-mode state.
type TransparentStatus struct {
	CAExists           bool
	CATrusted          bool
	AutoStartInstalled bool
	ProxyArmed         bool
	DaemonReachable    bool
	NetworkUnavailable bool
	ActiveServices     int
	Detail             string
}

// CodexRouteStatus is the TUI-facing snapshot of the scoped
// marker-owned Codex provider route in ~/.codex/config.toml.
type CodexRouteStatus struct {
	Exists              bool
	Enabled             bool
	Complete            bool
	Conflict            string
	LegacyKeys          bool
	DaemonReachable     bool
	Transport           string
	AutoTransport       string
	AutoMode            string
	WSSCertified        bool
	WSSBridgeAvailable  bool
	NeedsRecert         bool
	FallbackReason      string
	CertificationPath   string
	BridgeProofPath     string
	RecertStatePath     string
	RecertLogPath       string
	RecertStatus        string
	RecertAttemptID     string
	RecertStartedAt     time.Time
	RecertFinishedAt    time.Time
	RecertLastSuccessAt time.Time
	RecertRetryAfter    time.Time
	RecertLastError     string
	RecertCommand       string
	Detail              string
}

// CodexDesktopStatus is the TUI-facing Codex.app proxy capability state.
type CodexDesktopStatus struct {
	Mode                 string
	FailureClass         string
	DaemonReachable      bool
	AppServerActive      bool
	CATrusted            bool
	CAExists             bool
	ConversationObserved bool
	Detail               string
}

// Installed reports whether transparent mode is installed but not necessarily armed.
func (s TransparentStatus) Installed() bool {
	return s.CAExists && s.CATrusted && s.AutoStartInstalled
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
	// DaemonNotice returns non-actionable lifecycle diagnostics, such as
	// old macOS U/UE processes that require reboot while the current daemon is healthy.
	DaemonNotice() string
	// TransparentStatus returns CA, daemon, and system proxy state for transparent mode.
	TransparentStatus() TransparentStatus
	// InstallTransparent installs the local CA trust and launchd daemon without arming the proxy.
	InstallTransparent() error
	// EnableTransparent routes system HTTPS traffic through Slimference.
	EnableTransparent() error
	// DisableTransparent restores direct system HTTPS routing.
	DisableTransparent() error
	// UninstallTransparent disables routing and removes keychain trust / launchd.
	UninstallTransparent() error
	// CodexRouteStatus returns the scoped Codex CLI/App route state.
	CodexRouteStatus() CodexRouteStatus
	// CodexDesktopStatus returns the process-local Desktop launch capability state.
	CodexDesktopStatus() CodexDesktopStatus
	// LaunchCodexCLI opens the proven scoped Codex CLI path.
	LaunchCodexCLI() (string, error)
	// LaunchCodexApp opens Codex.app through proven Desktop Slimference mode.
	// It blocks instead of launching direct when Desktop savings are not green.
	LaunchCodexApp() (string, error)
	// RepairCodexWSS runs the guided CLI WSS recertification repair.
	RepairCodexWSS() (string, error)
	// EnableCodexRoute writes the marker-owned Codex provider route.
	EnableCodexRoute() error
	// DisableCodexRoute removes the marker-owned Codex provider route.
	DisableCodexRoute() error
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

	// Current view.
	view        ViewMode
	mainCursor  int
	statsCursor int
	debugCursor int

	// Live data.
	latestSnap    analytics.AnalyticsSnapshot
	latestProduct ProductStatus

	// Terminal dimensions.
	width  int
	height int

	// Hook installation status (set once at startup).
	hookStatus HookStatus

	// Setup wizard state.
	setupStep   int    // current wizard step (0=overview, 1..N=individual steps)
	setupCursor int    // selected setup row for arrow navigation (0-indexed)
	setupAction string // pending action description

	// Apps view state (Phase H).
	appsCursor int    // selected app row in ViewApps (0-indexed)
	appsFlash  string // ephemeral flash for the apps screen ("toggled" etc.)

	transparentStatus    TransparentStatus
	transparentStatusAt  time.Time
	codexRouteStatus     CodexRouteStatus
	codexRouteStatusAt   time.Time
	codexDesktopStatus   CodexDesktopStatus
	codexDesktopStatusAt time.Time

	// Flash message.
	flashMsg    string
	flashExpiry time.Time
}

// SetServiceControl sets the service control interface for daemon operations.
func (m *Model) SetServiceControl(svc ServiceControlInterface) {
	m.svc = svc
	m.refreshTransparentStatus(true)
	m.refreshCodexRouteStatus(true)
	m.refreshCodexDesktopStatus(true)
}

// NewModel creates a TUI model wired to the given proxy. If a persisted
// state file exists (T31) it is re-applied so the user's last toggle and
// view choices survive restarts; corrupt or missing files silently fall
// back to proxy-derived defaults.
func NewModel(proxy ProxyInterface) Model {
	m := Model{
		proxy:         proxy,
		keys:          DefaultKeyMap(),
		styles:        NewStyles(),
		sessionStart:  time.Now(),
		claudeEnabled: proxy.IsProviderEnabled(types.Anthropic),
		codexEnabled:  proxy.IsProviderEnabled(types.OpenAI),
		layer1Enabled: proxy.IsLayerEnabled(1),
		layer2Enabled: proxy.IsLayerEnabled(2),
		view:          ViewMain,
		width:         80,
		height:        24,
	}
	m.refreshProductStatus()
	if state, err := LoadPersistedState(); err == nil && state != nil {
		applyPersistedState(&m, *state)
	}
	return m
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
			m.claudeEnabled = false
			m.setFlash("Claude Code is disabled in Codex-only mode")

		case "x":
			m.codexEnabled = !m.codexEnabled
			m.proxy.SetProviderEnabled(types.OpenAI, m.codexEnabled)
			m.persistStateBestEffort()
			m.setFlash(fmt.Sprintf("Codex: %s", onOff(m.codexEnabled)))

		case "s":
			m.view = ViewStats
			m.persistStateBestEffort()

		case "d":
			m.view = ViewDebug
			m.persistStateBestEffort()

		case "i":
			m.view = ViewSetup
			m.enterSetupView()
			m.persistStateBestEffort()

		case " ":
			// Phase H: space toggles the selected app's enabled state.
			if m.view == ViewApps {
				entries := m.proxy.AppEntries()
				if len(entries) == 0 {
					return m, nil
				}
				if m.appsCursor >= len(entries) {
					m.appsCursor = len(entries) - 1
				}
				e := entries[m.appsCursor]
				if appEntryIsClaudeCode(e) {
					m.appsFlash = "Claude Code parked: Codex-only hosts are active; explicit Claude opt-in needed later"
					return m, nil
				}
				if err := m.proxy.SetAppEnabled(e.ID, !e.Enabled); err != nil {
					m.appsFlash = "error: " + err.Error()
				} else {
					state := "enabled"
					if e.Enabled {
						state = "disabled"
					}
					m.appsFlash = e.ID + " " + state
				}
				return m, nil
			}

		case "1":
			if m.view == ViewSetup {
				m.selectSetupStep(0)
				m.persistStateBestEffort()
				return m, nil
			}
			m.setFlash("Runtime layer toggles moved out of the daily UI; use config/CLI for advanced control")
			return m, flashTimer(3 * time.Second)

		case "2":
			if m.view == ViewSetup {
				m.selectSetupStep(1)
				m.persistStateBestEffort()
				return m, nil
			}
			m.setFlash("Cache layer controls moved out of the daily UI; use config/CLI for advanced control")
			return m, flashTimer(3 * time.Second)

		case "3":
			if m.view == ViewSetup {
				m.selectSetupStep(2)
				m.persistStateBestEffort()
				return m, nil
			}
		case "4":
			if m.view == ViewSetup {
				m.selectSetupStep(3)
				m.persistStateBestEffort()
				return m, nil
			}
		case "5":
			if m.view == ViewSetup {
				m.selectSetupStep(4)
				m.persistStateBestEffort()
				return m, nil
			}

		case "enter":
			if m.view == ViewMain {
				return m, m.executeMainSelection()
			}
			if m.view == ViewStats {
				m.view = ViewMain
				m.persistStateBestEffort()
				return m, nil
			}
			if m.view == ViewDebug {
				m.view = ViewMain
				m.persistStateBestEffort()
				return m, nil
			}
			if m.view == ViewLogs {
				m.view = ViewMain
				m.persistStateBestEffort()
				return m, nil
			}
			if m.view == ViewSetup && m.svc != nil {
				m.syncSetupSelection()
				m.executeSetupStep()
				return m, flashTimer(3 * time.Second)
			}

		case "up", "k":
			if m.view == ViewMain {
				m.moveMainCursor(-1)
				m.persistStateBestEffort()
				return m, nil
			}
			if m.view == ViewStats {
				m.moveStatsCursor(-1)
				m.persistStateBestEffort()
				return m, nil
			}
			if m.view == ViewLogs {
				m.moveDebugCursor(-1)
				m.persistStateBestEffort()
				return m, nil
			}
			if m.view == ViewSetup && m.svc != nil {
				m.moveSetupCursor(-1)
				m.persistStateBestEffort()
				return m, nil
			}
			if m.view == ViewApps {
				if m.appsCursor > 0 {
					m.appsCursor--
				}
				return m, nil
			}

		case "down", "j":
			if m.view == ViewMain {
				m.moveMainCursor(1)
				m.persistStateBestEffort()
				return m, nil
			}
			if m.view == ViewStats {
				m.moveStatsCursor(1)
				m.persistStateBestEffort()
				return m, nil
			}
			if m.view == ViewLogs {
				m.moveDebugCursor(1)
				m.persistStateBestEffort()
				return m, nil
			}
			if m.view == ViewSetup && m.svc != nil {
				m.moveSetupCursor(1)
				m.persistStateBestEffort()
				return m, nil
			}
			if m.view == ViewApps {
				entries := m.proxy.AppEntries()
				if m.appsCursor < len(entries)-1 {
					m.appsCursor++
				}
				return m, nil
			}

		case "left", "h":
			if m.view != ViewMain {
				m.view = ViewMain
				m.setupStep = 0
				m.persistStateBestEffort()
			}
			return m, nil

		case "right", "l":
			return m, nil

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
				m.persistStateBestEffort()
				return m, flashTimer(3 * time.Second)
			}

		case "o":
			if m.view == ViewSetup && m.svc != nil {
				if err := m.svc.RestartDaemon(); err != nil {
					m.setFlash("Restart failed: " + err.Error())
				} else {
					m.setFlash("Daemon restarted")
				}
				m.persistStateBestEffort()
				return m, flashTimer(3 * time.Second)
			}

		case "e":
			if m.view == ViewSetup && m.svc != nil {
				if err := m.svc.InstallService(); err != nil {
					m.setFlash("Install failed: " + err.Error())
				} else {
					m.setFlash("Service installed (auto-start enabled)")
				}
				m.refreshTransparentStatus(true)
				m.persistStateBestEffort()
				return m, flashTimer(3 * time.Second)
			}

		case "w":
			if m.view == ViewSetup && m.svc != nil {
				if err := m.svc.UninstallService(); err != nil {
					m.setFlash("Uninstall failed: " + err.Error())
				} else {
					m.setFlash("Service uninstalled")
				}
				m.refreshTransparentStatus(true)
				m.persistStateBestEffort()
				return m, flashTimer(3 * time.Second)
			}

		case "r":
			if m.view == ViewSetup && m.svc != nil {
				status := m.codexRouteStatus
				if status.Enabled {
					if err := m.svc.DisableCodexRoute(); err != nil {
						m.setFlash("Advanced shared route disable failed: " + err.Error())
					} else {
						m.setFlash("Normal Codex direct")
					}
				} else {
					if err := m.svc.EnableCodexRoute(); err != nil {
						m.setFlash("Advanced shared route enable failed: " + err.Error())
					} else {
						m.setFlash("Advanced shared route enabled")
					}
				}
				m.refreshCodexRouteStatus(true)
				m.persistStateBestEffort()
				return m, flashTimer(3 * time.Second)
			}

		case "a":
			// Phase H: app routing lives behind Setup for the daily product
			// surface. The app screen remains available without promoting it
			// as a top-level tab.
			if m.view == ViewApps {
				m.view = ViewSetup
				m.enterSetupView()
				m.persistStateBestEffort()
				return m, nil
			}
			if m.view == ViewSetup && m.svc != nil {
				m.view = ViewApps
				m.appsCursor = 0
				m.appsFlash = ""
				m.persistStateBestEffort()
				return m, nil
			}
			m.setFlash("App routing lives in Setup")
			return m, flashTimer(2 * time.Second)

		case "g":
			if m.view == ViewSetup && m.svc != nil {
				status := m.transparentStatus
				if status.ProxyArmed {
					if err := m.svc.DisableTransparent(); err != nil {
						m.setFlash("Global lab disarm failed: " + err.Error())
					} else {
						m.setFlash("Global lab disarmed")
					}
				} else {
					if !status.Installed() {
						if err := m.svc.InstallTransparent(); err != nil {
							m.setFlash("Global lab asset install failed: " + err.Error())
							m.persistStateBestEffort()
							return m, flashTimer(3 * time.Second)
						}
					}
					if err := m.svc.EnableTransparent(); err != nil {
						m.setFlash("Global lab arm failed: " + err.Error())
					} else {
						m.setFlash("Global lab armed")
					}
				}
				m.refreshTransparentStatus(true)
				m.persistStateBestEffort()
				return m, flashTimer(3 * time.Second)
			}

		case "u":
			if m.view == ViewSetup && m.svc != nil {
				if err := m.svc.UninstallTransparent(); err != nil {
					m.setFlash("Slimference asset uninstall failed: " + err.Error())
				} else {
					m.setFlash("Slimference assets uninstalled")
				}
				m.refreshTransparentStatus(true)
				m.persistStateBestEffort()
				return m, flashTimer(3 * time.Second)
			}

		case "f":
			m.proxy.FlushCaches()
			m.setFlash("All caches flushed")
			return m, flashTimer(2 * time.Second)

		case "b", "B":
			if m.view != ViewMain {
				m.view = ViewMain
				m.setupStep = 0
				m.persistStateBestEffort()
			}
			return m, nil

		case "esc", "backspace":
			if m.view != ViewMain {
				m.view = ViewMain
				m.setupStep = 0
				m.persistStateBestEffort()
			}
			return m, nil

		case "y":
			path := m.copyDebugLog()
			if path != "" {
				m.setFlash("Debug log copied to " + path)
			} else {
				m.setFlash("No debug log entries to copy")
			}
			return m, flashTimer(3 * time.Second)

		case "q", "ctrl+c":
			ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cancel()
			_ = m.proxy.Shutdown(ctx)
			_ = SavePersistedState(stateFromModel(&m))
			return m, tea.Quit

		case "ctrl+s":
			// Explicit "save preferences now" without quitting.
			if err := SavePersistedState(stateFromModel(&m)); err == nil {
				m.flashMsg = "preferences saved"
			} else {
				m.flashMsg = "save failed: " + err.Error()
			}
			m.flashExpiry = time.Now().Add(3 * time.Second)
			return m, flashTimer(3 * time.Second)
		}

	case tickMsg:
		m.latestSnap = m.proxy.GetAnalytics()
		m.refreshProductStatus()
		m.refreshTransparentStatus(false)
		m.refreshCodexRouteStatus(false)
		m.refreshCodexDesktopStatus(false)
		return m, m.nextTickCmd()

	case proxyEventMsg:
		// Immediate update when a new request arrives.
		m.latestSnap = m.proxy.GetAnalytics()
		m.refreshProductStatus()
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
		return m.renderStatusView()
	case ViewSetup:
		return m.renderSetupView()
	case ViewApps:
		return m.renderAppsView()
	case ViewLogs:
		return m.renderLogsView()
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

func (m *Model) persistStateBestEffort() {
	_ = SavePersistedState(stateFromModel(m))
}

// tickCmd returns a command that sends a tickMsg after the default refresh
// interval. Tests and callers that do not have a model snapshot can use it
// directly; the live model uses nextTickCmd so host-budget pressure can slow
// status polling.
func tickCmd() tea.Cmd {
	return tickCmdAfter(defaultTickInterval)
}

func tickCmdAfter(interval time.Duration) tea.Cmd {
	if interval <= 0 {
		interval = defaultTickInterval
	}
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) nextTickCmd() tea.Cmd {
	return tickCmdAfter(m.tickInterval())
}

func (m Model) tickInterval() time.Duration {
	if m.latestProduct.HostBudgetExceeded {
		return hostBudgetTickInterval
	}
	return defaultTickInterval
}

func (m *Model) refreshProductStatus() {
	if m.proxy == nil {
		m.latestProduct = ProductStatus{}
		return
	}
	m.latestProduct = m.proxy.GetProductStatus()
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
	if err := os.MkdirAll(dir, 0700); err != nil {
		return ""
	}
	if err := chmodFn(dir, 0700); err != nil {
		return ""
	}

	name := fmt.Sprintf("debug-%s.log", time.Now().Format("2006-01-02-150405"))
	path := filepath.Join(dir, name)

	var buf []byte
	for _, e := range entries {
		buf = append(buf, logger.Format(e)...)
		buf = append(buf, '\n')
	}
	if err := writeFileFn(path, buf, 0600); err != nil {
		return ""
	}
	if err := chmodFn(path, 0600); err != nil {
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

type dashboardAction struct {
	group       string
	id          string
	label       string
	description string
	state       string
}

func (m *Model) autoStartInstalled() bool {
	home, err := userHomeDirFn()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(home, "Library", "LaunchAgents", "com.slimference.daemon.plist"))
	return err == nil
}

func (m *Model) refreshTransparentStatus(force bool) {
	if m.svc == nil {
		m.transparentStatus = TransparentStatus{}
		m.transparentStatusAt = time.Time{}
		return
	}
	if !force && !m.transparentStatusAt.IsZero() && time.Since(m.transparentStatusAt) < statusRefreshMinInterval {
		return
	}
	m.transparentStatus = m.svc.TransparentStatus()
	m.transparentStatusAt = time.Now()
}

func (m *Model) refreshCodexRouteStatus(force bool) {
	if m.svc == nil {
		m.codexRouteStatus = CodexRouteStatus{}
		m.codexRouteStatusAt = time.Time{}
		return
	}
	if !force && !m.codexRouteStatusAt.IsZero() && time.Since(m.codexRouteStatusAt) < statusRefreshMinInterval {
		return
	}
	m.codexRouteStatus = m.svc.CodexRouteStatus()
	m.codexRouteStatusAt = time.Now()
}

func (m *Model) refreshCodexDesktopStatus(force bool) {
	if m.svc == nil {
		m.codexDesktopStatus = CodexDesktopStatus{}
		m.codexDesktopStatusAt = time.Time{}
		return
	}
	if !force && !m.codexDesktopStatusAt.IsZero() && time.Since(m.codexDesktopStatusAt) < statusRefreshMinInterval {
		return
	}
	m.codexDesktopStatus = m.svc.CodexDesktopStatus()
	m.codexDesktopStatusAt = time.Now()
}

func (m *Model) dashboardActions() []dashboardAction {
	return []dashboardAction{
		dashboardAction{
			group: "Launch",
			id:    "launch_cli",
			label: "Launch Codex CLI",
		},
		dashboardAction{
			group: "Launch",
			id:    "launch_app",
			label: "Launch Codex App",
		},
		dashboardAction{
			group: "Inspect",
			id:    "savings",
			label: "Savings",
		},
		dashboardAction{
			group: "Inspect",
			id:    "status",
			label: "Status",
		},
		dashboardAction{
			group: "Inspect",
			id:    "logs",
			label: "Logs",
		},
		dashboardAction{
			group: "Manage",
			id:    "setup",
			label: "Setup",
		},
	}
}

func (m *Model) codexCLIState() string {
	status := m.codexRouteStatus
	switch {
	case status.WSSCertified && status.AutoMode == "wss_phasef":
		return "savings active"
	case status.WSSBridgeAvailable && status.AutoMode == "wss_bridge":
		return "safe fallback"
	case status.NeedsRecert && status.RecertStatus == "running":
		return "repairing"
	case status.NeedsRecert:
		return "repair needed"
	case !status.DaemonReachable:
		return "daemon off"
	case status.FallbackReason != "":
		return "safe fallback"
	case status.AutoTransport != "":
		return "ready"
	default:
		return "ready"
	}
}

func (m *Model) codexAppState() string {
	status := m.codexDesktopStatus
	switch {
	case status.AppServerActive:
		return "scoped active"
	case status.Mode == "desktop_app_server_phasef_proven" || status.Mode == "desktop_app_server_proven":
		return "savings active"
	case status.Mode == "desktop_app_server_route_ready":
		return "route ready"
	case status.Mode == "desktop_wss_bridge_only":
		return "fallback"
	case status.Mode == "desktop_proof_prompt_required":
		return "proof needed"
	case status.FailureClass != "":
		return "blocked"
	case status.Mode != "":
		return "proof needed"
	default:
		return "unknown"
	}
}

func (m *Model) codexAppDescription() string {
	status := m.codexDesktopStatus
	if status.AppServerActive {
		return "Codex.app is running with Slimference app-server shim; first prompt proves live traffic."
	}
	if status.Mode == "desktop_app_server_phasef_proven" || status.Mode == "desktop_app_server_proven" {
		return "Open Codex.app in Slimference mode; Desktop savings are proven."
	}
	if status.Mode == "desktop_app_server_route_ready" {
		return "Open Codex.app in Slimference mode. Normal Finder/Spotlight launches stay direct."
	}
	if status.Mode == "desktop_wss_bridge_only" || status.Mode == "desktop_proof_prompt_required" {
		return "Desktop Slimference mode is not ready yet; Launch Codex App blocks instead of opening direct."
	}
	if status.FailureClass != "" {
		return "Desktop Slimference is not green (" + status.FailureClass + "); start Codex.app normally outside Slimference for direct mode."
	}
	return "Desktop Slimference mode is not ready yet; Launch Codex App blocks instead of opening direct."
}

func (m *Model) savingsState() string {
	snap := m.latestSnap
	if snap.SavedInputTokens <= 0 {
		return "no data"
	}
	return fmt.Sprintf("%s saved", formatTokenCount(snap.SavedInputTokens))
}

func (m *Model) statusState() string {
	if m.transparentStatus.ProxyArmed {
		return "lab armed"
	}
	if !m.codexRouteStatus.DaemonReachable {
		return "needs repair"
	}
	if m.codexRouteStatus.WSSCertified && m.codexRouteStatus.AutoMode == "wss_phasef" {
		return "savings active"
	}
	if m.codexRouteStatus.WSSBridgeAvailable && m.codexRouteStatus.AutoMode == "wss_bridge" {
		return "safe fallback"
	}
	if m.codexRouteStatus.NeedsRecert && m.codexRouteStatus.RecertStatus == "running" {
		return "repairing"
	}
	if m.codexRouteStatus.NeedsRecert {
		return "WSS repair needed"
	}
	if m.codexRouteStatus.FallbackReason != "" {
		return "fallback"
	}
	return "healthy"
}

func (m *Model) statusDescription() string {
	if m.codexRouteStatus.WSSCertified && m.codexRouteStatus.AutoMode == "wss_phasef" {
		return "Show detailed route, daemon, Desktop, CA, lab, and proof state." + m.recertStatusSuffix()
	}
	if m.codexRouteStatus.WSSBridgeAvailable && m.codexRouteStatus.AutoMode == "wss_bridge" {
		return "Show safe fallback and repair details." + m.recertStatusSuffix()
	}
	if m.codexRouteStatus.FallbackReason != "" {
		return "Show why Slimference mode is safely paused: " + m.codexRouteStatus.FallbackReason + m.recertStatusSuffix()
	}
	if m.codexDesktopStatus.FailureClass != "" {
		return "Desktop gate: " + m.codexDesktopStatus.FailureClass
	}
	return "Show daemon, route, WSS cert, Desktop gate, CA, and lab safety state."
}

func (m *Model) recertStatusSuffix() string {
	status := m.codexRouteStatus
	if status.RecertStatus == "" && status.RecertLogPath == "" {
		return ""
	}
	parts := make([]string, 0, 4)
	if status.RecertStatus != "" {
		label := "recert " + status.RecertStatus
		if status.RecertAttemptID != "" {
			label += " " + status.RecertAttemptID
		}
		parts = append(parts, label)
	}
	if !status.RecertStartedAt.IsZero() {
		parts = append(parts, "started "+relativeTime(status.RecertStartedAt))
	}
	if !status.RecertLastSuccessAt.IsZero() {
		parts = append(parts, "last green "+relativeTime(status.RecertLastSuccessAt))
	}
	if !status.RecertRetryAfter.IsZero() {
		parts = append(parts, "retry "+relativeTime(status.RecertRetryAfter))
	}
	if status.RecertLastError != "" {
		parts = append(parts, "last error: "+status.RecertLastError)
	}
	if status.RecertLogPath != "" {
		parts = append(parts, "log "+status.RecertLogPath)
	}
	if len(parts) == 0 {
		return ""
	}
	return " [" + strings.Join(parts, "; ") + "]"
}

func relativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now()
	if t.After(now) {
		return "in " + roundDuration(t.Sub(now))
	}
	return roundDuration(now.Sub(t)) + " ago"
}

func roundDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return d.Round(time.Second).String()
	case d < time.Hour:
		return d.Round(time.Minute).String()
	default:
		return d.Round(time.Hour).String()
	}
}

func (m *Model) manageState() string {
	if m.transparentStatus.ProxyArmed {
		return "lab armed"
	}
	if m.autoStartInstalled() {
		return "installed"
	}
	if m.transparentStatus.CAExists || m.transparentStatus.CATrusted || m.transparentStatus.AutoStartInstalled {
		return "partial"
	}
	return "available"
}

func formatTokenCount(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func clampIndex(current int, total int) int {
	if total <= 0 {
		return 0
	}
	if current < 0 {
		return 0
	}
	if current >= total {
		return total - 1
	}
	return current
}

func (m *Model) moveMainCursor(delta int) {
	actions := m.dashboardActions()
	m.mainCursor = clampIndex(m.mainCursor+delta, len(actions))
}

func (m *Model) moveStatsCursor(delta int) {
	const totalCards = 11
	m.statsCursor = clampIndex(m.statsCursor+delta, totalCards)
}

func (m *Model) debugActions() []dashboardAction {
	return []dashboardAction{
		{
			group:       "Actions",
			id:          "copy_log",
			label:       "Export debug log",
			description: "Write the visible log stream to ~/.slimference/exports/ for later inspection.",
			state:       "write file",
		},
	}
}

func (m *Model) moveDebugCursor(delta int) {
	actions := m.debugActions()
	m.debugCursor = clampIndex(m.debugCursor+delta, len(actions))
}

func (m *Model) executeMainSelection() tea.Cmd {
	actions := m.dashboardActions()
	m.mainCursor = clampIndex(m.mainCursor, len(actions))
	item := actions[m.mainCursor]
	switch item.id {
	case "launch_cli":
		if m.svc == nil {
			m.setFlash("Codex CLI launch unavailable: service adapter missing")
			break
		}
		msg, err := m.svc.LaunchCodexCLI()
		if err != nil {
			m.setFlash("Codex CLI launch failed: " + err.Error())
		} else {
			m.setFlash(msg)
		}
	case "launch_app":
		m.refreshCodexDesktopStatus(true)
		if m.svc == nil {
			m.setFlash("Codex App launch unavailable: service adapter missing")
			break
		}
		msg, err := m.svc.LaunchCodexApp()
		if err != nil {
			m.setFlash("Codex App launch blocked: " + err.Error())
		} else {
			m.setFlash(msg)
		}
	case "savings":
		m.view = ViewStats
		m.setFlash("Savings opened")
	case "status":
		m.refreshTransparentStatus(true)
		m.refreshCodexRouteStatus(true)
		m.refreshCodexDesktopStatus(true)
		m.view = ViewDebug
		m.setFlash(m.launchCenterStatusFlash())
	case "logs":
		m.view = ViewLogs
		m.setFlash("Logs opened")
	case "setup":
		m.view = ViewSetup
		m.enterSetupView()
		m.setFlash("Setup opened")
	}
	m.persistStateBestEffort()
	return flashTimer(3 * time.Second)
}

func (m *Model) launchCenterStatusFlash() string {
	cli := m.codexCLIState()
	app := m.codexAppState()
	lab := "disarmed"
	if m.transparentStatus.ProxyArmed {
		lab = "armed"
	}
	return fmt.Sprintf("Status: CLI %s; App %s; lab %s", cli, app, lab)
}

func (m *Model) executeDebugSelection() tea.Cmd {
	actions := m.debugActions()
	m.debugCursor = clampIndex(m.debugCursor, len(actions))
	switch actions[m.debugCursor].id {
	case "copy_log":
		path := m.copyDebugLog()
		if path != "" {
			m.setFlash("Debug log copied to " + path)
		} else {
			m.setFlash("No debug log entries to copy")
		}
	}
	return flashTimer(3 * time.Second)
}

// setupSteps returns the ordered list of wizard steps.
func (m *Model) setupSteps() []setupStep {
	if m.svc == nil {
		return nil
	}
	steps := []setupStep{
		{
			label:   "Run slimference install (product: Codex CLI + Desktop support)",
			check:   func() bool { return m.transparentStatus.CAExists && m.transparentStatus.AutoStartInstalled },
			action:  func(m *Model) error { return m.svc.InstallTransparent() },
			confirm: "Install Codex-only Slimference integration",
		},
		{
			label:   "Run slimference enable (advanced shared route: auto)",
			check:   func() bool { return m.codexRouteStatus.Complete },
			action:  func(m *Model) error { return m.svc.EnableCodexRoute() },
			confirm: "Enable advanced shared Codex provider route",
		},
		{
			label:   "Install Codex hook",
			check:   func() bool { return m.hookStatus.Codex },
			action:  func(m *Model) error { return m.svc.InstallHook("codex") },
			confirm: "Install Codex lifecycle hook",
		},
		{
			label: "Repair Codex CLI WSS savings",
			check: func() bool {
				return m.codexRouteStatus.WSSCertified && m.codexRouteStatus.AutoMode == "wss_phasef"
			},
			action:  func(m *Model) error { _, err := m.svc.RepairCodexWSS(); return err },
			confirm: "Repair CLI WSS certification",
		},
		{
			label: "Repair auto-start service (launchd only)",
			check: func() bool {
				home, _ := userHomeDirFn()
				_, err := os.Stat(filepath.Join(home, "Library", "LaunchAgents", "com.slimference.daemon.plist"))
				return err == nil
			},
			action:  func(m *Model) error { return m.svc.InstallService() },
			confirm: "Repair launchd auto-start service",
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
	m.refreshTransparentStatus(true)
	m.refreshCodexRouteStatus(true)
	// Refresh hook status after install.
	if home, err := userHomeDirFn(); err == nil {
		claude, codex := hooks.InstalledStatus(home)
		m.hookStatus = HookStatus{Claude: claude, Codex: codex}
	}
	m.setFlash("Done: " + step.label)
}

func (m *Model) enterSetupView() {
	m.refreshTransparentStatus(true)
	m.refreshCodexRouteStatus(true)
	m.setupStep = 0
	m.setupCursor = 0
	if m.svc == nil {
		return
	}
	steps := m.setupSteps()
	for i, step := range steps {
		if !step.check() {
			m.selectSetupStep(i)
			return
		}
	}
	if len(steps) > 0 {
		m.selectSetupStep(0)
	}
}

func (m *Model) selectSetupStep(index int) {
	steps := m.setupSteps()
	if len(steps) == 0 {
		m.setupCursor = 0
		m.setupStep = 0
		return
	}
	if index < 0 {
		index = 0
	}
	if index >= len(steps) {
		index = len(steps) - 1
	}
	m.setupCursor = index
	m.setupStep = index + 1
	m.setupAction = steps[index].confirm
}

func (m *Model) moveSetupCursor(delta int) {
	steps := m.setupSteps()
	if len(steps) == 0 {
		return
	}
	next := m.setupCursor + delta
	if next < 0 {
		next = 0
	}
	if next >= len(steps) {
		next = len(steps) - 1
	}
	m.selectSetupStep(next)
}

func (m *Model) syncSetupSelection() {
	if m.svc == nil {
		m.setupStep = 0
		return
	}
	m.selectSetupStep(m.setupCursor)
}

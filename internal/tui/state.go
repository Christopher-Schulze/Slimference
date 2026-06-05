package tui

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/slimference/slimference/internal/types"
)

// PersistedState is a minimal snapshot of user-facing toggles and view
// choices that is written on quit and re-applied on startup. A missing or
// corrupt file must not crash the TUI - it is pure user preference.
type PersistedState struct {
	ClaudeEnabled bool   `json:"claude_enabled"`
	CodexEnabled  bool   `json:"codex_enabled"`
	Layer1Enabled bool   `json:"layer1_enabled"`
	Layer3Enabled bool   `json:"layer3_enabled"`
	View          string `json:"view"`
}

// tuiStatePathFn is overridable in tests so the default path
// (~/.slimference/tui_state.json) can be redirected to a tempdir.
var tuiStatePathFn = defaultTUIStatePath

// defaultTUIStatePath returns ~/.slimference/tui_state.json, falling back
// to the current working directory when the home dir cannot be resolved.
// Reuses the package-level userHomeDirFn already declared in model.go.
func defaultTUIStatePath() string {
	home, err := userHomeDirFn()
	if err != nil || home == "" {
		return ".slimference/tui_state.json"
	}
	return filepath.Join(home, ".slimference", "tui_state.json")
}

// LoadPersistedState reads and parses the state file. Returns (nil, nil)
// when the file does not exist, and logs-on-return for any other failure
// so the TUI can boot to defaults.
func LoadPersistedState() (*PersistedState, error) {
	path := tuiStatePathFn()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var s PersistedState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// stateMarshalFn is overridable in tests so the (normally unreachable)
// json.MarshalIndent error branch is covered without relying on an
// unmarshalable type.
var stateMarshalFn = func(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }

// SavePersistedState serialises s to the state file, creating parent
// directories as needed. An empty state path is a no-op so tests can
// disable persistence entirely.
func SavePersistedState(s PersistedState) error {
	path := tuiStatePathFn()
	if path == "" {
		return nil
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	data, err := stateMarshalFn(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// stateFromModel captures the subset of Model fields that operators benefit
// from preserving across restarts.
func stateFromModel(m *Model) PersistedState {
	return PersistedState{
		ClaudeEnabled: m.claudeEnabled,
		CodexEnabled:  m.codexEnabled,
		Layer1Enabled: m.layer1Enabled,
		Layer3Enabled: m.layer3Enabled,
		View:          viewModeToString(m.view),
	}
}

// applyPersistedState projects a PersistedState back onto the model and
// propagates the toggles to the proxy so runtime behaviour matches what
// the user last saw.
func applyPersistedState(m *Model, s PersistedState) {
	m.claudeEnabled = s.ClaudeEnabled
	m.codexEnabled = s.CodexEnabled
	m.layer1Enabled = s.Layer1Enabled
	m.layer3Enabled = s.Layer3Enabled
	if v, ok := viewModeFromString(s.View); ok {
		m.view = v
	}
	if m.proxy != nil {
		m.proxy.SetProviderEnabled(types.Anthropic, m.claudeEnabled)
		m.proxy.SetProviderEnabled(types.OpenAI, m.codexEnabled)
		m.proxy.SetLayerEnabled(1, m.layer1Enabled)
		m.proxy.SetLayerEnabled(3, m.layer3Enabled)
	}
}

// viewModeToString maps a ViewMode to its persisted string tag.
func viewModeToString(v ViewMode) string {
	switch v {
	case ViewMain:
		return "main"
	case ViewStats:
		return "stats"
	case ViewDebug:
		return "debug"
	case ViewSetup:
		return "setup"
	default:
		return ""
	}
}

// viewModeFromString is the inverse of viewModeToString. Unknown values
// leave the current view untouched (ok=false).
func viewModeFromString(s string) (ViewMode, bool) {
	switch s {
	case "main":
		return ViewMain, true
	case "stats":
		return ViewStats, true
	case "debug":
		return ViewDebug, true
	case "setup":
		return ViewSetup, true
	default:
		return 0, false
	}
}

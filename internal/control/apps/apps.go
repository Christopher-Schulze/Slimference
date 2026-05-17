// Package apps implements the T193 per-app activation state machine.
// Operators independently toggle Slimference's interception for each
// supported active client app (Codex CLI, Codex Desktop App). Claude
// Code remains a known, detected app ID for compatibility, but its
// routing toggle is parked and forced off.
// The policy is consulted by the SNI router (T189) on every connection
// to decide whether to MITM or transparently passthrough.
//
// Policy persists in `~/.config/slimference/apps.toml`. Changes are
// hot-reloaded on file change OR SIGHUP so the operator does not need
// to restart the daemon when flipping a toggle in the TUI.
package apps

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BurntSushi/toml"
)

// AppID identifies a client app whose traffic Slimference can intercept.
type AppID string

const (
	AppCodexCLI     AppID = "codex_cli"
	AppCodexDesktop AppID = "codex_desktop_app"
	AppClaudeCode   AppID = "claude_code"
)

// KnownApps is the closed set of app IDs we currently recognise.
// New entries land here when we extend the matrix. Unknown IDs in
// config are ignored (with a warning) so the daemon never crashes on
// schema drift.
var KnownApps = []AppID{AppCodexCLI, AppCodexDesktop, AppClaudeCode}

// IsKnown reports whether `id` is in the recognised set.
func (id AppID) IsKnown() bool {
	for _, k := range KnownApps {
		if k == id {
			return true
		}
	}
	return false
}

// Detection holds the heuristics that resolve a connection to one of
// the KnownApps. We use a cascade: UA prefix first (most reliable),
// then known binary paths on disk (best-effort identity hint).
type Detection struct {
	// UAPrefixes are matched against the request's User-Agent header
	// (case-sensitive, prefix match). The first hit wins.
	UAPrefixes map[string]AppID
	// BinaryPaths give a "is this app installed on the machine?" signal
	// for the TUI status surface. Not consulted on the hot path; the
	// connection-level detection always uses UA.
	BinaryPaths map[AppID][]string
}

// DefaultDetection ships with current upstream conventions. Operators
// can override via ApplyDetection but rarely need to.
func DefaultDetection() Detection {
	return Detection{
		UAPrefixes: map[string]AppID{
			"codex_cli_rs/":     AppCodexCLI,
			"codex-cli/":        AppCodexCLI,
			"codex_desktop_app": AppCodexDesktop,
			"Codex.app/":        AppCodexDesktop,
			"Codex/":            AppCodexDesktop,
			"claude-code/":      AppClaudeCode,
			"Claude-Code/":      AppClaudeCode,
		},
		BinaryPaths: map[AppID][]string{
			AppCodexCLI: {
				"~/.npm-global/bin/codex",
				"/usr/local/bin/codex",
				"/opt/homebrew/bin/codex",
			},
			AppCodexDesktop: {
				"/Applications/Codex.app",
			},
			AppClaudeCode: {
				"~/.local/bin/claude",
				"/usr/local/bin/claude",
			},
		},
	}
}

// Policy is a snapshot of the per-app toggle state. Read-only after
// construction; mutations always go through Manager.SetEnabled.
type Policy struct {
	SchemaVersion int
	Enabled       map[AppID]bool
}

// DefaultPolicy returns the post-install default: Codex CLI and the
// Codex Desktop App on, Claude Code forced off. Claude Code code stays
// in the repository, but the product path is Codex-only.
func DefaultPolicy() Policy {
	return Policy{
		SchemaVersion: 1,
		Enabled: map[AppID]bool{
			AppCodexCLI:     true,
			AppCodexDesktop: true,
			AppClaudeCode:   false,
		},
	}
}

// IsEnabled reports whether the policy currently routes `id` through
// the MITM path. Unknown IDs always return false (safer default).
func (p Policy) IsEnabled(id AppID) bool {
	if !id.IsKnown() {
		return false
	}
	if id == AppClaudeCode {
		return false
	}
	return p.Enabled[id]
}

// Manager owns the live policy + detection state. Reads are
// lock-free (atomic.Pointer); writes serialise via mu.
type Manager struct {
	mu        sync.Mutex
	policy    atomic.Pointer[Policy]
	detection atomic.Pointer[Detection]

	path     string
	onChange []func(Policy)
}

// NewManager builds a Manager rooted at `configPath` (typically
// `~/.config/slimference/apps.toml`). If the file does not exist
// the manager initialises with the default policy and writes it on
// first Save. If the file exists but is malformed, NewManager
// returns the error so callers can decide how to surface it.
func NewManager(configPath string) (*Manager, error) {
	m := &Manager{path: configPath}
	det := DefaultDetection()
	m.detection.Store(&det)

	if configPath == "" {
		pol := DefaultPolicy()
		m.policy.Store(&pol)
		return m, nil
	}

	info, err := os.Stat(configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("apps: stat %s: %w", configPath, err)
	}
	if err != nil || info.Size() == 0 {
		pol := DefaultPolicy()
		m.policy.Store(&pol)
		return m, nil
	}
	pol, err := loadFromFile(configPath)
	if err != nil {
		return nil, err
	}
	m.policy.Store(&pol)
	return m, nil
}

// Policy returns the current snapshot. Safe for concurrent readers.
func (m *Manager) Policy() Policy {
	if p := m.policy.Load(); p != nil {
		return *p
	}
	return DefaultPolicy()
}

// Detection returns the current detection rules.
func (m *Manager) Detection() Detection {
	if d := m.detection.Load(); d != nil {
		return *d
	}
	return DefaultDetection()
}

// SetEnabled flips the toggle for one app and persists. The
// onChange listeners fire AFTER the file has been written, so the
// SNI router and TUI observers see a consistent state.
func (m *Manager) SetEnabled(id AppID, enabled bool) error {
	if !id.IsKnown() {
		return fmt.Errorf("apps: unknown app id %q", id)
	}
	if id == AppClaudeCode && enabled {
		return errors.New("apps: claude_code is parked; Slimference is Codex-only")
	}
	m.mu.Lock()
	cur := m.Policy()
	newEnabled := make(map[AppID]bool, len(cur.Enabled))
	for k, v := range cur.Enabled {
		newEnabled[k] = v
	}
	newEnabled[id] = enabled
	newEnabled[AppClaudeCode] = false
	for _, k := range KnownApps {
		if _, ok := newEnabled[k]; !ok {
			newEnabled[k] = false
		}
	}
	next := normalizePolicy(Policy{SchemaVersion: cur.SchemaVersion, Enabled: newEnabled})
	if next.SchemaVersion == 0 {
		next.SchemaVersion = 1
	}
	if err := writeToFile(m.path, next); err != nil {
		m.mu.Unlock()
		return err
	}
	m.policy.Store(&next)
	listeners := append([]func(Policy){}, m.onChange...)
	m.mu.Unlock()
	for _, fn := range listeners {
		fn(next)
	}
	return nil
}

// OnChange registers a listener fired after a policy change. Listeners
// run synchronously on the goroutine that called SetEnabled / Reload.
// Heavy work should be offloaded.
func (m *Manager) OnChange(fn func(Policy)) {
	m.mu.Lock()
	m.onChange = append(m.onChange, fn)
	m.mu.Unlock()
}

// Reload re-reads the on-disk file and replaces the in-memory policy.
// Returns the loaded policy or an error if the file is malformed.
// Listeners fire on success.
func (m *Manager) Reload() (Policy, error) {
	if m.path == "" {
		return m.Policy(), nil
	}
	pol, err := loadFromFile(m.path)
	if err != nil {
		return Policy{}, err
	}
	pol = normalizePolicy(pol)
	m.mu.Lock()
	m.policy.Store(&pol)
	listeners := append([]func(Policy){}, m.onChange...)
	m.mu.Unlock()
	for _, fn := range listeners {
		fn(pol)
	}
	return pol, nil
}

// AppFromUserAgent returns the AppID identified by the request's
// User-Agent header, or ("", false) when no prefix matches.
// Detection is case-sensitive on the prefix per the captured conventions.
func (m *Manager) AppFromUserAgent(ua string) (AppID, bool) {
	det := m.Detection()
	for prefix, id := range det.UAPrefixes {
		if strings.HasPrefix(ua, prefix) {
			return id, true
		}
	}
	return "", false
}

// DetectedBinaries returns the subset of BinaryPaths that exist on
// disk for each AppID. Intended for the TUI "is this app installed?"
// signal, not the hot path. Paths beginning with "~" are expanded
// against the current user's home directory.
func (m *Manager) DetectedBinaries() map[AppID][]string {
	det := m.Detection()
	home, _ := os.UserHomeDir()
	out := make(map[AppID][]string, len(det.BinaryPaths))
	for id, paths := range det.BinaryPaths {
		var found []string
		for _, p := range paths {
			expanded := p
			if strings.HasPrefix(p, "~/") && home != "" {
				expanded = filepath.Join(home, p[2:])
			}
			if _, err := os.Stat(expanded); err == nil {
				found = append(found, expanded)
			}
		}
		if len(found) > 0 {
			out[id] = found
		}
	}
	return out
}

// fileShape mirrors the TOML schema. The on-disk file uses a nested
// table per app so it stays human-editable.
type fileShape struct {
	SchemaVersion int                 `toml:"schema_version"`
	Apps          map[string]appEntry `toml:"apps"`
}

type appEntry struct {
	Enabled bool `toml:"enabled"`
}

func loadFromFile(path string) (Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, fmt.Errorf("apps: read %s: %w", path, err)
	}
	var f fileShape
	if err := toml.Unmarshal(data, &f); err != nil {
		return Policy{}, fmt.Errorf("apps: parse %s: %w", path, err)
	}
	pol := DefaultPolicy()
	if f.SchemaVersion > 0 {
		pol.SchemaVersion = f.SchemaVersion
	}
	for name, entry := range f.Apps {
		id := AppID(name)
		if !id.IsKnown() {
			// Unknown app id in config: ignore silently so future
			// versions adding apps don't break old daemons.
			continue
		}
		pol.Enabled[id] = entry.Enabled
	}
	return normalizePolicy(pol), nil
}

func writeToFile(path string, pol Policy) error {
	if path == "" {
		return nil
	}
	pol = normalizePolicy(pol)
	apps := make(map[string]appEntry, len(pol.Enabled))
	for id, enabled := range pol.Enabled {
		apps[string(id)] = appEntry{Enabled: enabled}
	}
	f := fileShape{
		SchemaVersion: pol.SchemaVersion,
		Apps:          apps,
	}
	if f.SchemaVersion == 0 {
		f.SchemaVersion = 1
	}
	var buf strings.Builder
	// fileShape contains only TOML-encodable scalar/map fields and the
	// destination is an in-memory builder, so encoding cannot fail unless
	// the struct shape changes in the future.
	_ = toml.NewEncoder(&buf).Encode(f)
	header := fmt.Sprintf("# Slimference per-app integration policy (T193)\n# Written %s\n",
		time.Now().UTC().Format(time.RFC3339))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("apps: mkdir: %w", err)
	}
	if err := os.WriteFile(path, []byte(header+buf.String()), 0o644); err != nil {
		return fmt.Errorf("apps: write %s: %w", path, err)
	}
	return nil
}

func normalizePolicy(pol Policy) Policy {
	if pol.Enabled == nil {
		pol.Enabled = map[AppID]bool{}
	}
	for _, id := range KnownApps {
		if _, ok := pol.Enabled[id]; !ok {
			pol.Enabled[id] = false
		}
	}
	pol.Enabled[AppClaudeCode] = false
	if pol.SchemaVersion == 0 {
		pol.SchemaVersion = 1
	}
	return pol
}

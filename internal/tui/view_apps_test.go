package tui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRenderAppsViewEmpty(t *testing.T) {
	p := newMockProxy()
	p.appEntries = nil
	m := NewModel(p)
	m.view = ViewApps
	m.width = 0
	out := m.renderAppsView()
	if !strings.Contains(out, "No apps discovered") {
		t.Errorf("empty render missing prompt: %q", out)
	}
}

func TestRenderAppsViewWithEntries(t *testing.T) {
	p := newMockProxy()
	p.appEntries = []AppEntry{
		{ID: "codex_cli", Enabled: true, Detected: true, BinPath: "/usr/bin/codex", Routed: 1234, Bypassed: 5},
		{ID: "codex_desktop_app", Enabled: false, Detected: false},
		{ID: "claude_code", Enabled: true, Detected: true, BinPath: "/usr/bin/claude"},
	}
	m := NewModel(p)
	m.view = ViewApps
	m.width = 100
	out := m.renderAppsView()
	if !strings.Contains(out, "codex_cli") {
		t.Errorf("missing codex_cli row: %q", out)
	}
	if !strings.Contains(out, "ENABLED") {
		t.Errorf("missing ENABLED state: %q", out)
	}
	if !strings.Contains(out, "DISABLED") {
		t.Errorf("missing DISABLED state: %q", out)
	}
	if !strings.Contains(out, "/usr/bin/codex") {
		t.Errorf("missing binary path: %q", out)
	}
	if !strings.Contains(out, "not detected on disk") {
		t.Errorf("missing 'not detected' note: %q", out)
	}
	if !strings.Contains(out, "POLICY ON / HOSTS OFF") || !strings.Contains(out, "Codex-only mode") {
		t.Errorf("missing Claude inert-state truth: %q", out)
	}
}

func TestRenderAppsViewCursor(t *testing.T) {
	p := newMockProxy()
	p.appEntries = []AppEntry{
		{ID: "a"}, {ID: "b"}, {ID: "c"},
	}
	m := NewModel(p)
	m.view = ViewApps
	m.appsCursor = 1
	m.width = 80
	out := m.renderAppsView()
	// Cursor row has ▸ marker. Count lines starting with cursor marker.
	if !strings.Contains(out, "▸") {
		t.Errorf("cursor marker missing: %q", out)
	}
}

func TestRenderAppsViewFlash(t *testing.T) {
	p := newMockProxy()
	p.appEntries = []AppEntry{{ID: "x", Enabled: true}}
	m := NewModel(p)
	m.view = ViewApps
	m.appsFlash = "x disabled"
	m.width = 80
	out := m.renderAppsView()
	if !strings.Contains(out, "x disabled") {
		t.Errorf("flash missing: %q", out)
	}
}

func TestAppsViewKeyboardToggleAndCursor(t *testing.T) {
	p := newMockProxy()
	p.appEntries = []AppEntry{
		{ID: "codex_cli", Enabled: true},
		{ID: "codex_desktop_app", Enabled: false},
	}
	m := NewModel(p)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model := updated.(Model)
	if model.view != ViewApps || model.appsCursor != 0 || model.appsFlash != "" {
		t.Fatalf("apps key did not enter apps view cleanly: view=%v cursor=%d flash=%q", model.view, model.appsCursor, model.appsFlash)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if model.appsCursor != 1 {
		t.Fatalf("down cursor=%d want 1", model.appsCursor)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if model.appsCursor != 0 {
		t.Fatalf("up cursor=%d want 0", model.appsCursor)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	model = updated.(Model)
	if p.appEntries[0].Enabled {
		t.Fatal("space should disable selected app")
	}
	if !strings.Contains(model.appsFlash, "disabled") {
		t.Fatalf("missing disabled flash: %q", model.appsFlash)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = updated.(Model)
	if model.view != ViewMain {
		t.Fatalf("apps key from apps view should return main, got %v", model.view)
	}
}

func TestAppsViewKeyboardEmptyAndErrorBranches(t *testing.T) {
	p := newMockProxy()
	p.appEntries = nil
	m := NewModel(p)
	m.view = ViewApps
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	model := updated.(Model)
	if model.view != ViewApps {
		t.Fatalf("empty space should stay in apps view")
	}

	p.appEntries = []AppEntry{{ID: "codex_cli", Enabled: true}}
	p.appErr = errors.New("toggle failed")
	m = NewModel(p)
	m.view = ViewApps
	m.appsCursor = 99
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	model = updated.(Model)
	if model.appsCursor != 0 {
		t.Fatalf("cursor should clamp to last entry, got %d", model.appsCursor)
	}
	if !strings.Contains(model.appsFlash, "toggle failed") {
		t.Fatalf("missing error flash: %q", model.appsFlash)
	}
	if !strings.Contains(model.View(), "codex_cli") {
		t.Fatalf("apps View() did not render apps screen")
	}
}

func TestAppsViewClaudeCodeToggleIsParked(t *testing.T) {
	p := newMockProxy()
	p.appEntries = []AppEntry{{ID: "claude_code", Enabled: false}}
	m := NewModel(p)
	m.view = ViewApps

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	model := updated.(Model)
	if p.appEntries[0].Enabled {
		t.Fatal("Claude Code policy should not be toggled from Codex-only TUI")
	}
	if !strings.Contains(model.appsFlash, "Claude Code parked") {
		t.Fatalf("missing parked flash: %q", model.appsFlash)
	}
}

func TestConfigPathHomeFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SLIMFERENCE_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)
	want := filepath.Join(home, ".config", "slimference", "config.toml")
	if got := configPath(); got != want {
		t.Fatalf("configPath()=%q want %q", got, want)
	}
}

func TestBuildLeftPanelArmedBypassTiles(t *testing.T) {
	p := newMockProxy()
	p.bypass = true
	m := NewModel(p)
	m.transparentStatus = TransparentStatus{ProxyArmed: true}
	out := strings.Join(m.buildLeftPanel(120), "\n")
	if !strings.Contains(out, "MITM ARMED") {
		t.Fatalf("armed tile missing:\n%s", out)
	}
	if !strings.Contains(out, "BYPASS") {
		t.Fatalf("bypass tile missing:\n%s", out)
	}
}

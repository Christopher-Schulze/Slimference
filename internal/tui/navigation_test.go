package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestUpdate_ViewArrowNavigationCyclesViews(t *testing.T) {
	t.Parallel()
	m := NewModel(newMockProxy())

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	model := updated.(Model)
	if model.view != ViewStats {
		t.Fatalf("right from main: got %d want %d", model.view, ViewStats)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = updated.(Model)
	if model.view != ViewDebug {
		t.Fatalf("right from stats: got %d want %d", model.view, ViewDebug)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = updated.(Model)
	if model.view != ViewSetup {
		t.Fatalf("right from debug: got %d want %d", model.view, ViewSetup)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = updated.(Model)
	if model.view != ViewMain {
		t.Fatalf("right from setup should wrap to main, got %d", model.view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model = updated.(Model)
	if model.view != ViewSetup {
		t.Fatalf("left from main should wrap to setup, got %d", model.view)
	}
}

func TestUpdate_SetupArrowNavigation(t *testing.T) {
	t.Parallel()
	m := NewModel(newMockProxy())
	m.width = 100
	m.height = 30
	m.SetServiceControl(&mockServiceControl{})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model := updated.(Model)
	if model.setupStep != 1 || model.setupCursor != 0 {
		t.Fatalf("initial setup selection: step=%d cursor=%d", model.setupStep, model.setupCursor)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if model.setupStep != 2 || model.setupCursor != 1 {
		t.Fatalf("down selection: step=%d cursor=%d", model.setupStep, model.setupCursor)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if model.setupStep != 3 || model.setupCursor != 2 {
		t.Fatalf("second down selection: step=%d cursor=%d", model.setupStep, model.setupCursor)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if model.setupStep != 4 || model.setupCursor != 3 {
		t.Fatalf("third down selection: step=%d cursor=%d", model.setupStep, model.setupCursor)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if model.setupStep != 4 || model.setupCursor != 3 {
		t.Fatalf("fourth down selection: step=%d cursor=%d", model.setupStep, model.setupCursor)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if model.setupStep != 4 || model.setupCursor != 3 {
		t.Fatalf("down should clamp at last step: step=%d cursor=%d", model.setupStep, model.setupCursor)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if model.setupStep != 3 || model.setupCursor != 2 {
		t.Fatalf("up selection: step=%d cursor=%d", model.setupStep, model.setupCursor)
	}
}

func TestView_SetupView_ShowsArrowHintsAndTabs(t *testing.T) {
	t.Parallel()
	m := NewModel(newMockProxy())
	m.width = 100
	m.height = 30
	m.SetServiceControl(&mockServiceControl{})
	m.view = ViewSetup
	m.enterSetupView()

	output := m.View()
	for _, needle := range []string{"Dashboard", "Stats", "Debug", "Setup", "[↑/↓]", "[←/→]", "enable autostart"} {
		if !strings.Contains(output, needle) {
			t.Fatalf("setup view missing %q in output: %s", needle, output)
		}
	}
}

package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestUpdate_BackNavigationReturnsHome(t *testing.T) {
	t.Parallel()
	m := NewModel(newMockProxy())

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	model := updated.(Model)
	if model.view != ViewMain {
		t.Fatalf("right from main should stay home, got %d", model.view)
	}

	model.view = ViewStats
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.view != ViewMain {
		t.Fatalf("enter from savings should return home, got %d", model.view)
	}

	model.view = ViewDebug
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if model.view != ViewMain {
		t.Fatalf("esc from status should return home, got %d", model.view)
	}

	model.view = ViewSetup
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	model = updated.(Model)
	if model.view != ViewMain {
		t.Fatalf("b from setup should return home, got %d", model.view)
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
		t.Fatalf("down should clamp at last step: step=%d cursor=%d", model.setupStep, model.setupCursor)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if model.setupStep != 3 || model.setupCursor != 2 {
		t.Fatalf("up selection: step=%d cursor=%d", model.setupStep, model.setupCursor)
	}
}

func TestView_SetupView_HidesFooterLegendWithoutTabs(t *testing.T) {
	t.Parallel()
	m := NewModel(newMockProxy())
	m.width = 100
	m.height = 30
	m.SetServiceControl(&mockServiceControl{})
	m.view = ViewSetup
	m.enterSetupView()

	output := m.View()
	for _, needle := range []string{"SLIMFERENCE / Setup", "SETUP"} {
		if !strings.Contains(output, needle) {
			t.Fatalf("setup view missing %q in output: %s", needle, output)
		}
	}
	for _, blocked := range []string{"▶ Launch", "[←/→]", "[↑/↓]", "[b/esc]", "quit", "advanced shared route", "advanced lab", "uninstall Slimference assets"} {
		if strings.Contains(output, blocked) {
			t.Fatalf("setup view leaked tab navigation %q in output: %s", blocked, output)
		}
	}
}

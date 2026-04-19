package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all keyboard shortcuts for the TUI.
type KeyMap struct {
	ToggleClaude key.Binding
	ToggleCodex  key.Binding
	ToggleLayer1 key.Binding
	ToggleLayer2 key.Binding
	ToggleLayer3 key.Binding
	PrevView     key.Binding
	NextView     key.Binding
	CursorUp     key.Binding
	CursorDown   key.Binding
	Execute      key.Binding
	ViewStats    key.Binding
	ViewDebug    key.Binding
	FlushCaches  key.Binding
	Quit         key.Binding
}

// DefaultKeyMap returns the standard key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		ToggleClaude: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "toggle Claude Code"),
		),
		ToggleCodex: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "toggle Codex"),
		),
		ToggleLayer1: key.NewBinding(
			key.WithKeys("1"),
			key.WithHelp("1", "toggle Layer 1"),
		),
		ToggleLayer2: key.NewBinding(
			key.WithKeys("2"),
			key.WithHelp("2", "toggle Layer 2"),
		),
		ToggleLayer3: key.NewBinding(
			key.WithKeys("3"),
			key.WithHelp("3", "toggle Layer 3"),
		),
		PrevView: key.NewBinding(
			key.WithKeys("left", "h"),
			key.WithHelp("←", "previous view"),
		),
		NextView: key.NewBinding(
			key.WithKeys("right", "l"),
			key.WithHelp("→", "next view"),
		),
		CursorUp: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑", "move up"),
		),
		CursorDown: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓", "move down"),
		),
		Execute: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "execute"),
		),
		ViewStats: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "stats view"),
		),
		ViewDebug: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "debug log"),
		),
		FlushCaches: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "flush caches"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}

// footerHelp returns the compact help string shown in the footer.
func (km KeyMap) footerHelp() string {
	return "[←/→] views  [↑/↓] move  [enter] execute  [c/x] providers  [1-3] layers  [f] flush  [q] quit"
}

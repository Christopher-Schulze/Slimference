package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all keyboard shortcuts for the TUI.
type KeyMap struct {
	ToggleClaude key.Binding
	ToggleCodex  key.Binding
	ToggleLayer1 key.Binding
	ToggleLayer3 key.Binding
	PrevView     key.Binding
	NextView     key.Binding
	CursorUp     key.Binding
	CursorDown   key.Binding
	Execute      key.Binding
	ViewStats    key.Binding
	ViewDebug    key.Binding
	FlushCaches  key.Binding
	ToggleBypass key.Binding
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
		ToggleBypass: key.NewBinding(
			key.WithKeys("b", "B"),
			key.WithHelp("b", "toggle bypass"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}

// footerHelp returns the compact help string shown in the footer.
func (km KeyMap) footerHelp() string {
	return "[←/→] views  [↑/↓] move  [enter] execute  [c/x] providers  [1/3] layers  [f] flush  [b] bypass  [q] quit"
}

// bindingSpec names a binding and references the KeyMap field.
type bindingSpec struct {
	name    string
	binding key.Binding
}

// orderedBindings returns bindings in the canonical order used by the
// rendered help / markdown table. New bindings should be appended to the
// slice - not inserted in the middle - so external docs and golden test
// outputs stay stable.
func (km KeyMap) orderedBindings() []bindingSpec {
	return []bindingSpec{
		{"Navigation", km.PrevView},
		{"Navigation", km.NextView},
		{"Navigation", km.CursorUp},
		{"Navigation", km.CursorDown},
		{"Navigation", km.Execute},
		{"Views", km.ViewStats},
		{"Views", km.ViewDebug},
		{"Providers", km.ToggleClaude},
		{"Providers", km.ToggleCodex},
		{"Layers", km.ToggleLayer1},
		{"Layers", km.ToggleLayer3},
		{"Actions", km.FlushCaches},
		{"Actions", km.ToggleBypass},
		{"Actions", km.Quit},
	}
}

// RenderKeybindingsMarkdown returns a Markdown table describing every
// keybinding. Used to generate `docs/tui-keybindings.md` from code so the
// documentation cannot drift. T64.
func (km KeyMap) RenderKeybindingsMarkdown() string {
	var sb = stringBuilder{}
	sb.Write("# Slimference TUI Keybindings\n\n")
	sb.Write("Auto-generated from `internal/tui/keys.go`. Do not edit by hand;\n")
	sb.Write("run the generator or rerun the TUI key tests to regenerate.\n\n")
	sb.Write("| Category | Keys | Description |\n")
	sb.Write("|----------|------|-------------|\n")
	for _, spec := range km.orderedBindings() {
		keys := joinKeys(spec.binding.Keys())
		help := spec.binding.Help().Desc
		sb.Write("| ")
		sb.Write(spec.name)
		sb.Write(" | `")
		sb.Write(keys)
		sb.Write("` | ")
		sb.Write(help)
		sb.Write(" |\n")
	}
	return sb.String()
}

// small helpers - kept local to avoid pulling strings.Join/Builder into the
// hot-path file and to make the generator output byte-stable.
type stringBuilder struct{ b []byte }

func (s *stringBuilder) Write(x string) { s.b = append(s.b, x...) }
func (s *stringBuilder) String() string { return string(s.b) }

func joinKeys(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	out := keys[0]
	for _, k := range keys[1:] {
		out += ", " + k
	}
	return out
}

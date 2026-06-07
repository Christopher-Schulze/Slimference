package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all keyboard shortcuts for the TUI.
type KeyMap struct {
	ToggleClaude    key.Binding
	ToggleCodex     key.Binding
	ToggleLayer1    key.Binding
	ToggleLayer2    key.Binding
	SetupStep3      key.Binding
	SetupStep4      key.Binding
	SetupStep5      key.Binding
	PrevView        key.Binding
	NextView        key.Binding
	CursorUp        key.Binding
	CursorDown      key.Binding
	Execute         key.Binding
	ViewStats       key.Binding
	ViewDebug       key.Binding
	ViewSetup       key.Binding
	ViewApps        key.Binding
	FlushCaches     key.Binding
	ToggleBypass    key.Binding
	ServicePower    key.Binding
	ServiceRepair   key.Binding
	ToggleCodexMode key.Binding
	GlobalLab       key.Binding
	UninstallAssets key.Binding
	ExportDebug     key.Binding
	SavePrefs       key.Binding
	Quit            key.Binding
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
			key.WithHelp("1", "setup step 1"),
		),
		ToggleLayer2: key.NewBinding(
			key.WithKeys("2"),
			key.WithHelp("2", "setup step 2"),
		),
		SetupStep3: key.NewBinding(
			key.WithKeys("3"),
			key.WithHelp("3", "setup step 3"),
		),
		SetupStep4: key.NewBinding(
			key.WithKeys("4"),
			key.WithHelp("4", "setup step 4"),
		),
		SetupStep5: key.NewBinding(
			key.WithKeys("5"),
			key.WithHelp("5", "setup step 5"),
		),
		PrevView: key.NewBinding(
			key.WithKeys("left", "h"),
			key.WithHelp("←", "back"),
		),
		NextView: key.NewBinding(
			key.WithKeys("right", "l"),
			key.WithHelp("→", "unused"),
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
			key.WithHelp("enter", "open/back"),
		),
		ViewStats: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "savings view"),
		),
		ViewDebug: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "status view"),
		),
		ViewSetup: key.NewBinding(
			key.WithKeys("i"),
			key.WithHelp("i", "setup view"),
		),
		ViewApps: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "app routing"),
		),
		FlushCaches: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "flush caches"),
		),
		ToggleBypass: key.NewBinding(
			key.WithKeys("b", "B"),
			key.WithHelp("b", "back"),
		),
		ServicePower: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "start/stop daemon"),
		),
		ServiceRepair: key.NewBinding(
			key.WithKeys("o"),
			key.WithHelp("o", "restart/repair daemon"),
		),
		ToggleCodexMode: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "CLI-only advanced route"),
		),
		GlobalLab: key.NewBinding(
			key.WithKeys("g"),
			key.WithHelp("g", "CLI-only global lab"),
		),
		UninstallAssets: key.NewBinding(
			key.WithKeys("u"),
			key.WithHelp("u", "CLI-only uninstall"),
		),
		ExportDebug: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "export diagnostics"),
		),
		SavePrefs: key.NewBinding(
			key.WithKeys("ctrl+s"),
			key.WithHelp("ctrl+s", "save preferences"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}

// footerHelp intentionally returns no visible footer legend. The key bindings
// still exist and are documented through RenderKeybindingsMarkdown.
func (km KeyMap) footerHelp() string {
	return ""
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
		{"Navigation", km.CursorUp},
		{"Navigation", km.CursorDown},
		{"Navigation", km.Execute},
		{"Views", km.ViewStats},
		{"Views", km.ViewDebug},
		{"Views", km.ViewSetup},
		{"Setup", km.ToggleLayer1},
		{"Setup", km.ToggleLayer2},
		{"Setup", km.SetupStep3},
		{"Setup", km.SetupStep4},
		{"Setup", km.ViewApps},
		{"Setup", km.ServicePower},
		{"Setup", km.ServiceRepair},
		{"Providers", km.ToggleClaude},
		{"Providers", km.ToggleCodex},
		{"Actions", km.FlushCaches},
		{"Actions", km.ToggleBypass},
		{"Actions", km.ExportDebug},
		{"Actions", km.SavePrefs},
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

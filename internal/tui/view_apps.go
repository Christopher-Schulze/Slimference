package tui

import (
	"fmt"
	"strings"
)

// renderAppsView renders the Phase H per-app routing screen. Each
// known app gets a row showing enabled/detected state + counters.
// The keybindings (in keys.go + model Update) drive selection +
// space-toggle.
func (m Model) renderAppsView() string {
	s := m.styles
	width := m.width
	if width <= 0 {
		width = 80
	}

	entries := m.proxy.AppEntries()

	var b strings.Builder
	b.WriteString(s.PanelTitle.Render("APPS — per-app routing policy"))
	b.WriteString("\n\n")
	if len(entries) == 0 {
		b.WriteString(s.Muted.Render("  No apps discovered. Is the daemon running?"))
		b.WriteString("\n")
	}
	for i, e := range entries {
		state := "DISABLED"
		dot := "○"
		stateStyle := s.Muted
		if e.Enabled {
			state = "ENABLED"
			dot = "●"
			stateStyle = s.Saved
		}
		if appEntryIsClaudeCode(e) {
			dot = "○"
			state = "HOSTS OFF"
			stateStyle = s.Muted
			if e.Enabled {
				state = "POLICY ON / HOSTS OFF"
			}
		}
		marker := "  "
		if i == m.appsCursor {
			marker = "▸ "
		}
		line := fmt.Sprintf("%s%s %-20s %s  routed %s  bypassed %s",
			marker,
			stateStyle.Render(dot),
			e.ID,
			stateStyle.Render(state),
			s.Highlight.Render(formatTokens(int(e.Routed))),
			s.Dim.Render(formatTokens(int(e.Bypassed))),
		)
		b.WriteString("  " + line + "\n")
		if e.Detected && e.BinPath != "" {
			b.WriteString("      " + s.Dim.Render("found at "+e.BinPath) + "\n")
		} else if !e.Detected {
			b.WriteString("      " + s.Muted.Render("(not detected on disk)") + "\n")
		}
		if appEntryIsClaudeCode(e) {
			b.WriteString("      " + s.Muted.Render("Codex-only mode: Claude traffic is not hosts-routed; toggle is parked.") + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(s.Muted.Render("  ↑/↓ select   space toggle Codex apps   q quit"))
	b.WriteString("\n")
	if m.appsFlash != "" {
		b.WriteString("\n")
		b.WriteString("  " + s.Highlight.Render(m.appsFlash))
		b.WriteString("\n")
	}
	return b.String()
}

func appEntryIsClaudeCode(e AppEntry) bool {
	return e.ID == "claude_code"
}

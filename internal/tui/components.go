package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/slimference/slimference/internal/types"
)

// renderProgressBar renders a horizontal progress bar for a given ratio (0.0-1.0).
// totalWidth is the full width including label.
func renderProgressBar(s Styles, ratio float64, totalWidth int) string {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}

	label := fmt.Sprintf(" %d%%", int(math.Round(ratio*100)))
	barWidth := totalWidth - len(label) - 2
	if barWidth < 4 {
		barWidth = 4
	}

	filled := int(math.Round(float64(barWidth) * ratio))
	empty := barWidth - filled

	bar := s.BarFilled.Render(strings.Repeat("█", filled)) +
		s.BarEmpty.Render(strings.Repeat("░", empty))
	return bar + s.Saved.Render(label)
}

// renderHealthDot renders a small colored indicator for provider API health.
// Status is derived from actual request outcomes - no upstream polling (spec §17.5).
func renderHealthDot(s Styles, status types.ProviderHealthStatus) string {
	switch status {
	case types.ProviderHealthHealthy:
		return s.Dot.Render("●")
	case types.ProviderHealthDegraded:
		return s.Warning.Render("●")
	case types.ProviderHealthDown:
		return s.LogError.Render("●")
	default: // idle
		return s.Muted.Render("○")
	}
}

// renderProviderBadge renders a colored provider status badge.
func renderProviderBadge(s Styles, name string, enabled bool) string {
	if enabled {
		dot := s.Dot.Render("●")
		badge := s.OnBadge.Render("ON")
		return fmt.Sprintf("%s %s  [%s]", dot, name, badge)
	}
	dot := s.DotOff.Render("○")
	badge := s.OffBadge.Render("OFF")
	return fmt.Sprintf("%s %s  [%s]", dot, s.Muted.Render(name), badge)
}

// renderLayerLine renders a single compression layer status line.
func renderLayerLine(s Styles, num int, name string, enabled bool, saved int, extra string) string {
	var indicator, badge string
	if enabled {
		indicator = s.Dot.Render("●")
		badge = s.OnBadge.Render("ON ")
	} else {
		indicator = s.DotOff.Render("○")
		badge = s.OffBadge.Render("OFF")
	}
	savedStr := ""
	if saved > 0 {
		savedStr = "  " + s.Saved.Render(formatTokens(saved)+" saved")
	}
	extraStr := ""
	if extra != "" {
		extraStr = "   " + s.Dim.Render(extra)
	}
	return fmt.Sprintf("  [%d] %-14s %s %s%s%s",
		num,
		s.Normal.Render(name),
		indicator,
		badge,
		savedStr,
		extraStr,
	)
}

func renderSetupStepRow(s Styles, index int, label string, done bool, selected bool) string {
	number := s.StepIndex.Render(fmt.Sprintf("[%d]", index+1))
	switch {
	case done:
		return " " + s.StepDone.Render("✓") + "  " + number + "  " + s.Dim.Render(label)
	case selected:
		return " " + s.StepCursor.Render("▶") + "  " + number + "  " + s.StepCursor.Render(label)
	default:
		return " " + s.Muted.Render("○") + "  " + number + "  " + s.StepIdle.Render(label)
	}
}

func renderMenuRow(s Styles, width int, selected bool, label string, state string) string {
	if width < 12 {
		width = 12
	}
	marker := s.MenuMeta.Render("○")
	labelStyle := s.MenuIdle
	stateStyle := s.MenuMeta
	if selected {
		marker = s.MenuOn.Render("▶")
		labelStyle = s.MenuActive
		stateStyle = s.MenuActive
	}
	left := marker + " " + labelStyle.Render(label)
	if state == "" {
		return padRight(left, width)
	}
	right := stateStyle.Render(state)
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	row := left + strings.Repeat(" ", gap) + right
	if selected {
		return s.MenuActive.Width(width).Render(row)
	}
	return padRight(row, width)
}

func renderShortcutRow(s Styles, items ...string) string {
	rendered := make([]string, 0, len(items))
	for _, item := range items {
		rendered = append(rendered, s.Shortcut.Render(item))
	}
	return strings.Join(rendered, " ")
}

func renderInfoCard(style lipgloss.Style, title string, body []string) string {
	lines := []string{title, ""}
	lines = append(lines, body...)
	return style.Render(strings.Join(lines, "\n"))
}

func renderMetricLine(s Styles, key string, value string) string {
	return " " + s.MetricKey.Render(key) + "  " + s.MetricVal.Render(value)
}

func renderMetricPair(s Styles, leftKey string, leftValue string, rightKey string, rightValue string, width int) string {
	left := renderMetricLine(s, leftKey, leftValue)
	right := renderMetricLine(s, rightKey, rightValue)
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		gap = 2
	}
	return left + strings.Repeat(" ", gap) + right
}

func renderKPIRow(s Styles, items ...string) string {
	pills := make([]string, 0, len(items))
	for _, item := range items {
		pills = append(pills, s.TabIdle.Render(" "+item+" "))
	}
	return strings.Join(pills, " ")
}

// renderRequestLogLine renders a single row in the live request log.
func renderRequestLogLine(s Styles, rm types.RequestMetrics) string {
	ts := rm.Timestamp.Format("15:04:05")

	provider := rm.Provider.String()
	if len(provider) > 7 {
		provider = provider[:7]
	}

	model := shortModelName(rm.Model)
	if len(model) > 8 {
		model = model[:8]
	}

	var compStr string
	if rm.CacheHit {
		compStr = s.Highlight.Render("cache-hit")
	} else if rm.CompressionRatio > 0 && rm.InputTokensOrig > 0 {
		saved := int((1 - rm.CompressionRatio) * 100)
		compStr = s.Saved.Render(fmt.Sprintf("-%d%%", saved))
	} else {
		compStr = s.Muted.Render("passthru")
	}

	layerStr := formatLayers(rm.Layers)
	latencyStr := fmt.Sprintf("%.1fms", rm.LatencyMs)

	origStr := formatTokens(rm.InputTokensOrig)
	compTokenStr := formatTokens(rm.InputTokensComp)

	return fmt.Sprintf("  %s  %-9s %-9s %s -> %s  %-9s  %-8s  %s",
		s.LogTime.Render(ts),
		s.Normal.Render(provider),
		s.Dim.Render(model),
		s.Dim.Render(origStr),
		compStr,
		s.Saved.Render(compTokenStr),
		s.Dim.Render(layerStr),
		s.Muted.Render(latencyStr),
	)
}

// renderTable renders a simple table with given headers and rows.
func renderTable(s Styles, headers []string, rows [][]string, colWidths []int) string {
	// Build separator.
	sepParts := make([]string, len(headers))
	for i, w := range colWidths {
		sepParts[i] = strings.Repeat("-", w)
	}

	var sb strings.Builder

	// Header row.
	headerCells := make([]string, len(headers))
	for i, h := range headers {
		headerCells[i] = padRight(s.TableHeader.Render(h), colWidths[i])
	}
	sb.WriteString("  " + strings.Join(headerCells, " | ") + "\n")
	sb.WriteString("  " + s.TableBorder.Render(strings.Join(sepParts, "-+-")) + "\n")

	// Data rows.
	for _, row := range rows {
		cells := make([]string, len(row))
		for i, cell := range row {
			w := 0
			if i < len(colWidths) {
				w = colWidths[i]
			}
			cells[i] = padRight(s.TableCell.Render(cell), w)
		}
		sb.WriteString("  " + strings.Join(cells, " | ") + "\n")
	}

	return sb.String()
}

// renderSessionDuration formats a duration for display.
func renderSessionDuration(start time.Time) string {
	d := time.Since(start)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// formatTokens formats a token count for display: 1234 -> "1.2K", 1234567 -> "1.2M".
func formatTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// formatLayers returns a compact layer list: [1,2] -> "L1+L2", [] -> "-"
func formatLayers(layers []int) string {
	if len(layers) == 0 {
		return "-"
	}
	parts := make([]string, len(layers))
	for i, l := range layers {
		if l == 3 {
			l = 2
		}
		parts[i] = fmt.Sprintf("L%d", l)
	}
	return strings.Join(parts, "+")
}

// shortModelName returns a short display name for a model string.
func shortModelName(model string) string {
	switch {
	case strings.Contains(model, "opus"):
		return "opus"
	case strings.Contains(model, "sonnet"):
		return "sonnet"
	case strings.Contains(model, "haiku"):
		return "haiku"
	case strings.Contains(model, "o3"):
		return "o3"
	case strings.Contains(model, "o1"):
		return "o1"
	case strings.Contains(model, "gpt-4o"):
		return "gpt-4o"
	case strings.Contains(model, "gpt-4"):
		return "gpt-4"
	default:
		if len(model) > 10 {
			return model[:10]
		}
		return model
	}
}

// padRight pads or truncates a string to the given width using visible characters.
func padRight(s string, width int) string {
	// Count visible length (strip ANSI if needed - here we approximate).
	visible := lipgloss.Width(s)
	if visible >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visible)
}

// padLeft left-pads a string to the given width.
func padLeft(s string, width int) string {
	visible := lipgloss.Width(s)
	if visible >= width {
		return s
	}
	return strings.Repeat(" ", width-visible) + s
}

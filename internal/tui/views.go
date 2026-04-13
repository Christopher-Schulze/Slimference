package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/slimference/slimference/internal/analytics"
)

// renderMainView renders the primary dashboard.
func (m *Model) renderMainView() string {
	s := m.styles
	width := m.width
	if width < 40 {
		width = 80
	}
	innerWidth := width - 4 // account for border + padding

	var lines []string

	// --- Header ---
	versionStr := s.Title.Render("Slimference v" + Version)
	sessionStr := s.Dim.Render("Session: " + renderSessionDuration(m.sessionStart))
	headerPad := innerWidth - lipgloss.Width(versionStr) - lipgloss.Width(sessionStr) - 2
	if headerPad < 1 {
		headerPad = 1
	}
	lines = append(lines, versionStr+strings.Repeat(" ", headerPad)+sessionStr)
	lines = append(lines, "")

	// --- Provider toggles ---
	claudeStr := renderProviderBadge(s, "Claude Code", m.claudeEnabled)
	codexStr := renderProviderBadge(s, "Codex", m.codexEnabled)
	portStr := s.Dim.Render(fmt.Sprintf("Port: %d", m.proxy.Config().GetListenPort()))
	pad := innerWidth - lipgloss.Width(claudeStr) - lipgloss.Width(codexStr) - lipgloss.Width(portStr) - 6
	if pad < 1 {
		pad = 1
	}
	lines = append(lines, "  "+claudeStr+"     "+codexStr+strings.Repeat(" ", pad)+portStr)
	lines = append(lines, "")

	// --- Hook status ---
	hookStr := renderHookStatus(s, m.hookStatus)
	if hookStr != "" {
		lines = append(lines, "  "+hookStr)
		lines = append(lines, "")
	}

	// --- Usage section ---
	snap := m.latestSnap
	lines = append(lines, s.SectionHeader.Render("  Usage"))

	origStr := formatTokens(snap.TotalInputTokens)
	compStr := formatTokens(snap.TotalInputTokens - snap.SavedInputTokens)
	savedStr := formatTokens(snap.SavedInputTokens)
	ratio := snap.CompressionRatio
	if ratio > 1 {
		ratio = 1
	}
	pct := int(ratio * 100)
	if snap.TotalInputTokens > 0 {
		// ratio here is compressed/original, so savings = (1-ratio)*100
		pct = int((1 - ratio) * 100)
	}

	line1 := fmt.Sprintf("  Messages: %s sent    Tokens: %s -> %s  (%s saved)",
		s.Highlight.Render(fmt.Sprintf("%d", snap.TotalRequests)),
		s.Normal.Render(origStr),
		s.Normal.Render(compStr),
		s.Saved.Render(savedStr),
	)
	lines = append(lines, line1)

	avgPrefill := m.proxy.Config().GetPrefillSpeed()
	extraMsgs := snap.EstExtraMessages(snap.AvgTokensPerRequest)
	ttftImprove := snap.AvgTTFTImprovement(avgPrefill)

	line2 := fmt.Sprintf("  Compression: %s     ~%s extra messages gained this session",
		s.Saved.Render(fmt.Sprintf("%d%%", pct)),
		s.Saved.Render(fmt.Sprintf("%d", extraMsgs)),
	)
	lines = append(lines, line2)

	line3 := fmt.Sprintf("  Avg TTFT improvement: ~%s",
		s.Highlight.Render(fmt.Sprintf("%.1fs faster per response", ttftImprove)),
	)
	lines = append(lines, line3)

	// Progress bar.
	barRatio := float64(pct) / 100.0
	bar := "  " + renderProgressBar(s, barRatio, innerWidth-4)
	lines = append(lines, bar)
	lines = append(lines, "")

	// --- Layers section ---
	lines = append(lines, s.SectionHeader.Render("  Layers"))

	l2Status := m.proxy.GetLayer2Status()
	l2Extra := ""
	if l2Status.Compressing {
		l2Extra = "compressing..."
	} else if l2Status.HasCache {
		l2Extra = fmt.Sprintf("last: %s  queue: %d",
			formatAgo(l2Status.LastRun),
			l2Status.QueueDepth,
		)
	}

	lines = append(lines, renderLayerLine(s, 1, "Deterministic", m.layer1Enabled, snap.Layer1Savings, "(JSON, dedup, tree-sitter)"))
	lines = append(lines, renderLayerLine(s, 2, "MiniMax", m.layer2Enabled, snap.Layer2Savings, l2Extra))
	lines = append(lines, renderLayerLine(s, 3, "Cache", m.layer3Enabled, snap.Layer3Savings, fmt.Sprintf("hits: %d/%d", snap.CacheHits, snap.TotalRequests)))
	lines = append(lines, "")

	// --- Live request log ---
	lines = append(lines, s.SectionHeader.Render("  Live"))
	logLines := m.renderRequestLog()
	lines = append(lines, logLines...)
	lines = append(lines, "")

	// --- Secrets + retries footer line ---
	if snap.SecretsRedacted > 0 || snap.AutoRetries > 0 {
		infoLine := ""
		if snap.SecretsRedacted > 0 {
			infoLine += s.Warning.Render(fmt.Sprintf("  Secrets: %d redacted", snap.SecretsRedacted))
		}
		if snap.AutoRetries > 0 {
			infoLine += s.Dim.Render(fmt.Sprintf("   Retries: %d", snap.AutoRetries))
		}
		lines = append(lines, infoLine)
	}

	// Flash message.
	if m.flashMsg != "" && time.Now().Before(m.flashExpiry) {
		lines = append(lines, "  "+s.Flash.Render(m.flashMsg))
	}

	// Footer.
	footerStr := s.Footer.Render("  " + m.keys.footerHelp())
	lines = append(lines, footerStr)

	content := strings.Join(lines, "\n")

	border := s.Border.Width(width - 2)
	return border.Render(content)
}

// renderStatsView renders the detailed statistics screen.
func (m *Model) renderStatsView() string {
	s := m.styles
	width := m.width
	if width < 40 {
		width = 80
	}

	snap := m.latestSnap
	avgPrefill := m.proxy.Config().GetPrefillSpeed()
	extraMsgs := snap.EstExtraMessages(snap.AvgTokensPerRequest)
	ttftImprove := snap.AvgTTFTImprovement(avgPrefill)

	var lines []string

	lines = append(lines, s.Title.Render("Slimference - Detailed Statistics"))
	lines = append(lines, "")

	// Session summary.
	lines = append(lines, s.Subtitle.Render("  Session Summary"))
	lines = append(lines, fmt.Sprintf("  Started: %s    Duration: %s",
		snap.SessionStart.Format("2006-01-02 15:04:05"),
		renderSessionDuration(snap.SessionStart),
	))
	lines = append(lines, fmt.Sprintf("  Messages: %d sent    Errors: %d    MiniMax calls: %d",
		snap.TotalRequests, snap.Errors, snap.MiniMaxCalls,
	))
	lines = append(lines, "")

	// Usage savings table.
	lines = append(lines, s.Subtitle.Render("  Usage Savings"))
	ratio := 0
	if snap.TotalInputTokens > 0 {
		ratio = int((1 - float64(snap.TotalInputTokens-snap.SavedInputTokens)/float64(snap.TotalInputTokens)) * 100)
	}
	headers := []string{"Metric", "Original", "After", "Saved"}
	rows := [][]string{
		{"Total Input", formatTokens(snap.TotalInputTokens), formatTokens(snap.TotalInputTokens - snap.SavedInputTokens), fmt.Sprintf("%d%%", ratio)},
		{"Layer 1 (determ)", "-", "-", formatTokens(snap.Layer1Savings)},
		{"Layer 2 (MiniMax)", "-", "-", formatTokens(snap.Layer2Savings)},
		{"Layer 3 (cache)", "-", "-", formatTokens(snap.Layer3Savings)},
		{"Total Output", formatTokens(snap.TotalOutputTokens), "(passthru)", "-"},
	}
	lines = append(lines, renderTable(s, headers, rows, []int{20, 12, 12, 8}))

	// Capacity gain box.
	lines = append(lines, s.Subtitle.Render("  Effective Capacity Gain"))
	avgOrig := 0
	avgComp := 0
	if snap.TotalRequests > 0 {
		avgOrig = snap.TotalInputTokens / snap.TotalRequests
		avgComp = (snap.TotalInputTokens - snap.SavedInputTokens) / snap.TotalRequests
	}
	sessMultiplier := 1.0
	if snap.TotalInputTokens > 0 {
		sessMultiplier = float64(snap.TotalInputTokens) / float64(snap.TotalInputTokens-snap.SavedInputTokens)
	}
	capacityLines := []string{
		fmt.Sprintf("  Extra messages gained:  ~%d additional", extraMsgs),
		fmt.Sprintf("  Session extension:      ~%.1fx longer before limit", sessMultiplier),
		fmt.Sprintf("  Avg TTFT improvement:   ~%.1fs faster/response", ttftImprove),
		fmt.Sprintf("  Avg compression ratio:  %d%% (per request)", ratio),
		fmt.Sprintf("  Avg tokens/request:     %s (was %s)", formatTokens(avgComp), formatTokens(avgOrig)),
	}
	for _, cl := range capacityLines {
		lines = append(lines, s.Normal.Render(cl))
	}
	lines = append(lines, "")

	// Per-provider table.
	lines = append(lines, s.Subtitle.Render("  Per Provider"))
	pHeaders := []string{"Provider", "Messages", "Saved", "Avg %"}
	pRows := [][]string{}
	for prov, stats := range snap.PerProvider {
		avgRatioPct := 0
		if stats.AvgRatio > 0 {
			avgRatioPct = int((1 - stats.AvgRatio) * 100)
		}
		pRows = append(pRows, []string{
			prov.String(),
			fmt.Sprintf("%d", stats.Messages),
			formatTokens(stats.InputTokensSaved),
			fmt.Sprintf("%d%%", avgRatioPct),
		})
	}
	lines = append(lines, renderTable(s, pHeaders, pRows, []int{18, 10, 12, 8}))

	// MiniMax stats.
	lines = append(lines, s.Subtitle.Render("  MiniMax Compression Engine"))
	lines = append(lines, fmt.Sprintf("  Calls: %d    Avg latency: %.1fs    Failures: %d",
		snap.MiniMaxCalls, snap.MiniMaxAvgLatencyMs/1000, snap.MiniMaxFailures,
	))
	lines = append(lines, "")

	// Resilience.
	lines = append(lines, s.Subtitle.Render("  Resilience"))
	lines = append(lines, fmt.Sprintf("  Auto-retries: %d    Secrets redacted: %d",
		snap.AutoRetries, snap.SecretsRedacted,
	))
	lines = append(lines, "")

	lines = append(lines, s.Footer.Render("  [s] back  [q] quit"))

	content := strings.Join(lines, "\n")
	border := s.Border.Width(width - 2)
	return border.Render(content)
}

// renderDebugView renders the debug log tail.
func (m *Model) renderDebugView() string {
	s := m.styles
	width := m.width
	if width < 40 {
		width = 80
	}

	var lines []string
	lines = append(lines, s.Title.Render("Slimference - Debug Log"))
	lines = append(lines, "")

	if m.proxy.SessionLogger() != nil {
		entries := m.proxy.SessionLogger().Recent(25)
		for _, entry := range entries {
			formatted := m.proxy.SessionLogger().Format(entry)
			lineStyle := logLevelStyle(s, entry.Level)
			lines = append(lines, "  "+lineStyle.Render(formatted))
		}
	} else {
		lines = append(lines, s.Muted.Render("  No log entries yet."))
	}

	lines = append(lines, "")
	lines = append(lines, s.Footer.Render("  [d] back  [q] quit"))

	content := strings.Join(lines, "\n")
	border := s.Border.Width(width - 2)
	return border.Render(content)
}

// renderRequestLog renders the recent request log lines.
func (m *Model) renderRequestLog() []string {
	maxLines := 5
	if m.height >= 30 {
		maxLines = 10
	}

	reqs := m.proxy.GetRecentRequests(maxLines)
	if len(reqs) == 0 {
		return []string{m.styles.Muted.Render("  Waiting for requests...")}
	}

	lines := make([]string, 0, len(reqs))
	// Show most recent first.
	for i := len(reqs) - 1; i >= 0; i-- {
		lines = append(lines, renderRequestLogLine(m.styles, reqs[i]))
	}
	return lines
}

// formatAgo formats a time as a human-readable "ago" string.
func formatAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

// logLevelStyle returns the lipgloss style for a given log level string.
func logLevelStyle(s Styles, level string) lipgloss.Style {
	switch level {
	case "ERROR":
		return s.LogError
	case "WARN":
		return s.LogWarn
	case "DEBUG":
		return s.LogDebug
	default:
		return s.LogInfo
	}
}

// safeRatio returns the fraction of input tokens saved (0-1).
func safeRatio(snap analytics.AnalyticsSnapshot) float64 {
	if snap.TotalInputTokens == 0 {
		return 0
	}
	return float64(snap.SavedInputTokens) / float64(snap.TotalInputTokens)
}

// renderHookStatus renders a compact hook installation status line.
// Returns "" when both hooks are absent (no noise if hooks not set up).
func renderHookStatus(s Styles, h HookStatus) string {
	if !h.Claude && !h.Codex {
		return ""
	}
	var parts []string
	if h.Claude {
		parts = append(parts, s.Saved.Render("claude ✓"))
	} else {
		parts = append(parts, s.Muted.Render("claude -"))
	}
	if h.Codex {
		parts = append(parts, s.Saved.Render("codex ✓"))
	} else {
		parts = append(parts, s.Muted.Render("codex -"))
	}
	return s.Dim.Render("Hooks: ") + strings.Join(parts, "  ")
}

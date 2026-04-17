package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/types"
)

// renderMainView renders the primary dashboard with a two-column layout.
func (m *Model) renderMainView() string {
	s := m.styles
	width := m.width
	if width < 60 {
		width = 80
	}
	innerWidth := width - 4

	header := m.renderHeader(innerWidth)
	rule := s.HorizRule.Render(strings.Repeat("─", innerWidth))

	leftWidth := 36
	if innerWidth < 90 {
		leftWidth = 32
	}
	rightWidth := innerWidth - leftWidth - 1

	leftLines := m.buildLeftPanel(leftWidth)
	rightLines := m.buildRightPanel(rightWidth)

	h := len(leftLines)
	if len(rightLines) > h {
		h = len(rightLines)
	}
	emptyL := strings.Repeat(" ", leftWidth)
	emptyR := strings.Repeat(" ", rightWidth)
	for len(leftLines) < h {
		leftLines = append(leftLines, emptyL)
	}
	for len(rightLines) < h {
		rightLines = append(rightLines, emptyR)
	}

	div := s.Divider.Render("│")
	rows := make([]string, h)
	for i := range rows {
		rows[i] = leftLines[i] + div + rightLines[i]
	}

	flashLine := ""
	if m.flashMsg != "" && time.Now().Before(m.flashExpiry) {
		flashLine = "\n" + s.Flash.Render("  "+m.flashMsg)
	}

	content := header + "\n" + rule + "\n" +
		strings.Join(rows, "\n") + "\n" + rule +
		flashLine + "\n" + m.renderFooterBar()

	return s.Border.Width(width - 2).Render(content)
}

// renderStatsView renders the detailed statistics screen.
func (m *Model) renderStatsView() string {
	s := m.styles
	width := m.width
	if width < 60 {
		width = 80
	}

	snap := m.latestSnap
	avgPrefill := m.proxy.Config().GetPrefillSpeed()
	extraMsgs := snap.EstExtraMessages(snap.AvgTokensPerRequest)
	ttftImprove := snap.AvgTTFTImprovement(avgPrefill)

	innerWidth := width - 4
	rule := s.HorizRule.Render(strings.Repeat("─", innerWidth))

	var lines []string

	lines = append(lines, " "+s.Title.Render("SLIMFERENCE")+" "+s.Dim.Render("/ Stats"))
	lines = append(lines, rule)

	// Session summary.
	lines = append(lines, " "+s.PanelTitle.Render("SESSION"))
	lines = append(lines, fmt.Sprintf("  Started: %s    Duration: %s",
		snap.SessionStart.Format("2006-01-02 15:04"),
		renderSessionDuration(snap.SessionStart),
	))
	lines = append(lines, fmt.Sprintf("  Requests: %d    Errors: %d    MiniMax calls: %d",
		snap.TotalRequests, snap.Errors, snap.MiniMaxCalls,
	))
	lines = append(lines, "")

	// Usage savings table.
	lines = append(lines, " "+s.PanelTitle.Render("SAVINGS"))
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

	// Capacity gain.
	lines = append(lines, " "+s.PanelTitle.Render("CAPACITY GAIN"))
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
	lines = append(lines, s.Normal.Render(fmt.Sprintf("  Extra messages:    +%d", extraMsgs)))
	lines = append(lines, s.Normal.Render(fmt.Sprintf("  Session extended:  ~%.1fx longer before limit", sessMultiplier)))
	lines = append(lines, s.Normal.Render(fmt.Sprintf("  TTFT improvement:  ~%.1fs faster per response", ttftImprove)))
	lines = append(lines, s.Normal.Render(fmt.Sprintf("  Avg ratio:         %d%% compression", ratio)))
	lines = append(lines, s.Normal.Render(fmt.Sprintf("  Avg tokens/req:    %s (was %s)", formatTokens(avgComp), formatTokens(avgOrig))))
	lines = append(lines, "")

	// Per-provider table.
	lines = append(lines, " "+s.PanelTitle.Render("PER PROVIDER"))
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

	// Latency.
	lines = append(lines, " "+s.PanelTitle.Render("LATENCY"))
	latHeaders := []string{"Provider", "Avg ms", "TTFT saved/req"}
	latRows := [][]string{}
	if snap.LatencyAnthropicMs > 0 || snap.PerProvider[types.Anthropic].Messages > 0 {
		ttft := providerTTFTSaving(snap, types.Anthropic, avgPrefill)
		latRows = append(latRows, []string{"Anthropic", fmt.Sprintf("%.0fms", snap.LatencyAnthropicMs), fmt.Sprintf("~%.1fs", ttft)})
	}
	if snap.LatencyOpenAIMs > 0 || snap.PerProvider[types.OpenAI].Messages > 0 {
		ttft := providerTTFTSaving(snap, types.OpenAI, avgPrefill)
		latRows = append(latRows, []string{"OpenAI", fmt.Sprintf("%.0fms", snap.LatencyOpenAIMs), fmt.Sprintf("~%.1fs", ttft)})
	}
	if snap.MiniMaxAvgLatencyMs > 0 {
		latRows = append(latRows, []string{"MiniMax (async)", fmt.Sprintf("%.0fms", snap.MiniMaxAvgLatencyMs), "-"})
	}
	if len(latRows) == 0 {
		lines = append(lines, s.Muted.Render("  No requests yet."))
	} else {
		lines = append(lines, renderTable(s, latHeaders, latRows, []int{18, 10, 14}))
	}

	// Resilience.
	lines = append(lines, " "+s.PanelTitle.Render("RESILIENCE"))
	retriesDetail := ""
	if snap.AutoRetries > 0 {
		retriesDetail = fmt.Sprintf(" (%dx rate-limit, %dx overflow)", snap.RateLimitRetries, snap.OverflowRetries)
	}
	lines = append(lines, fmt.Sprintf("  Auto-retries: %d%s    Secrets redacted: %d",
		snap.AutoRetries, retriesDetail, snap.SecretsRedacted,
	))
	lines = append(lines, "")

	lines = append(lines, rule)
	lines = append(lines, " "+s.Key.Render("[s]")+s.FooterDesc.Render(" back")+
		s.KeySep.Render(" · ")+s.Key.Render("[q]")+s.FooterDesc.Render(" quit"))

	content := strings.Join(lines, "\n")
	return s.Border.Width(width - 2).Render(content)
}

// renderDebugView renders the debug log tail.
func (m *Model) renderDebugView() string {
	s := m.styles
	width := m.width
	if width < 60 {
		width = 80
	}

	innerWidth := width - 4
	rule := s.HorizRule.Render(strings.Repeat("─", innerWidth))

	var lines []string
	lines = append(lines, " "+s.Title.Render("SLIMFERENCE")+" "+s.Dim.Render("/ Debug Log"))
	lines = append(lines, rule)

	if m.proxy.SessionLogger() != nil {
		entries := m.proxy.SessionLogger().Recent(30)
		if len(entries) == 0 {
			lines = append(lines, s.Muted.Render("  No log entries yet."))
		} else {
			for _, entry := range entries {
				formatted := m.proxy.SessionLogger().Format(entry)
				lines = append(lines, "  "+logLevelStyle(s, entry.Level).Render(formatted))
			}
		}
	} else {
		lines = append(lines, s.Muted.Render("  No log entries yet."))
	}

	lines = append(lines, "")
	lines = append(lines, rule)
	lines = append(lines, " "+s.Key.Render("[d]")+s.FooterDesc.Render(" back")+
		s.KeySep.Render(" · ")+s.SetupCmd.Render(" [y] copy log to file ")+
		s.KeySep.Render(" · ")+s.Key.Render("[q]")+s.FooterDesc.Render(" quit"))

	content := strings.Join(lines, "\n")
	return s.Border.Width(width - 2).Render(content)
}

// renderSetupView renders the interactive install wizard and service management screen.
func (m *Model) renderSetupView() string {
	s := m.styles
	width := m.width
	if width < 60 {
		width = 80
	}
	innerWidth := width - 4
	rule := s.HorizRule.Render(strings.Repeat("─", innerWidth))

	var lines []string

	lines = append(lines, " "+s.Title.Render("SLIMFERENCE")+" "+s.Dim.Render("/ Setup Wizard"))
	lines = append(lines, rule)

	// Overall READY status.
	allReady := m.hookStatus.Claude && m.hookStatus.Codex
	if allReady {
		lines = append(lines, "")
		lines = append(lines, "   "+s.Saved.Render("✓ ALL SET — Slimference is ready"))
		lines = append(lines, "")
	} else {
		lines = append(lines, "")
		missing := []string{}
		if !m.hookStatus.Claude {
			missing = append(missing, "Claude Code hook")
		}
		if !m.hookStatus.Codex {
			missing = append(missing, "Codex hook")
		}
		lines = append(lines, "   "+s.Warning.Render("⚠ Setup incomplete: "+strings.Join(missing, ", ")))
		lines = append(lines, "")
	}
	lines = append(lines, rule)

	// Interactive wizard steps.
	if m.svc != nil {
		lines = append(lines, " "+s.PanelTitle.Render("SETUP STEPS"))
		lines = append(lines, " "+s.Dim.Render("Select a step [1-3], then press Enter to execute."))
		lines = append(lines, "")

		steps := m.setupSteps()
		for i, step := range steps {
			num := fmt.Sprintf("%d", i+1)
			if step.check() {
				lines = append(lines, "   "+s.Saved.Render("✓")+"  "+s.Dim.Render("["+num+"] "+step.label))
			} else if m.setupStep == i+1 {
				lines = append(lines, "   "+s.Highlight.Render("▶")+"  "+s.Normal.Render("["+num+"] "+step.label))
				lines = append(lines, "        "+s.SetupCmd.Render("Enter ↵ "+step.confirm))
			} else {
				lines = append(lines, "   "+s.Muted.Render("○")+"  "+s.Normal.Render("["+num+"] "+step.label))
			}
		}

		lines = append(lines, "")

		// Service controls.
		lines = append(lines, " "+s.PanelTitle.Render("SERVICE CONTROLS"))
		lines = append(lines, "")
		running, pid, port := m.svc.DaemonStatus()
		if running {
			lines = append(lines, "  "+s.Saved.Render("● RUNNING")+"  PID "+fmt.Sprintf("%d  port :%d", pid, port))
		} else {
			lines = append(lines, "  "+s.Muted.Render("○ STOPPED")+"  proxy running in TUI-process")
		}
		lines = append(lines, "  "+s.Dim.Render(
			"  [p] start/stop   [o] restart   [e] install service   [w] uninstall"))

	} else {
		// Fallback without service control.
		lines = append(lines, " "+s.PanelTitle.Render("SETUP CHECKLIST"))
		lines = append(lines, "")
		home, _ := os.UserHomeDir()
		check := func(label string, ok bool) string {
			if ok {
				return "  " + s.Saved.Render("✓") + "  " + s.Normal.Render(label)
			}
			return "  " + s.LogError.Render("✗") + "  " + s.Muted.Render(label)
		}
		_, cfgErr := os.Stat(configPath())
		lines = append(lines, check("Config file", cfgErr == nil))
		lines = append(lines, check("MiniMax API key (MINIMAX_API_KEY)", os.Getenv("MINIMAX_API_KEY") != ""))
		lines = append(lines, check("Claude Code hook installed", m.hookStatus.Claude))
		lines = append(lines, check("Codex hook installed", m.hookStatus.Codex))
		port := m.proxy.Config().GetListenPort()
		lines = append(lines, check(fmt.Sprintf("Proxy listening on :%d", port), true))
		pidPath := filepath.Join(home, ".slimference", "slimference.pid")
		if pidData, pidErr := os.ReadFile(pidPath); pidErr == nil && len(pidData) > 0 {
			lines = append(lines, check("Daemon running", true))
		}
		plistPath := filepath.Join(home, "Library", "LaunchAgents", "com.slimference.daemon.plist")
		if _, err := os.Stat(plistPath); err == nil {
			lines = append(lines, check("launchd auto-start service", true))
		}
		lines = append(lines, "")

		lines = append(lines, " "+s.PanelTitle.Render("SERVICE STATUS"))
		lines = append(lines, "")
		pidRunning := false
		if pidData, pidErr := os.ReadFile(pidPath); pidErr == nil && len(pidData) > 0 {
			pidRunning = true
		}
		if pidRunning {
			lines = append(lines, "  "+s.Saved.Render("● Daemon running"))
		} else {
			lines = append(lines, "  "+s.Muted.Render("○ Daemon not running — proxy in TUI mode"))
		}

		lines = append(lines, "")
		lines = append(lines, " "+s.PanelTitle.Render("COMMANDS"))
		lines = append(lines, "")
		lines = append(lines, "  "+s.Dim.Render("slimference hook install claude|codex"))
		lines = append(lines, "  "+s.Dim.Render("slimference service install"))
		lines = append(lines, "  "+s.Dim.Render("slimference start"))
	}

	lines = append(lines, "")
	lines = append(lines, rule)
	lines = append(lines, " "+s.Key.Render("[i]")+s.FooterDesc.Render(" back")+
		s.KeySep.Render(" · ")+s.Key.Render("[1-3]")+s.FooterDesc.Render(" select step")+
		s.KeySep.Render(" · ")+s.Key.Render("[enter]")+s.FooterDesc.Render(" execute")+
		s.KeySep.Render(" · ")+s.Key.Render("[q]")+s.FooterDesc.Render(" quit"))

	content := strings.Join(lines, "\n")
	return s.Border.Width(width - 2).Render(content)
}

// configPath returns the resolved config file path.
func configPath() string {
	if p := os.Getenv("SLIMFERENCE_CONFIG"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".slimference", "config.toml")
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

// providerTTFTSaving computes the estimated avg TTFT improvement per request for a
// single provider, based on per-provider token savings and the prefill speed (tokens/s).
func providerTTFTSaving(snap analytics.AnalyticsSnapshot, prov types.Provider, prefillSpeed int) float64 {
	if prefillSpeed <= 0 {
		return 0
	}
	ps, ok := snap.PerProvider[prov]
	if !ok || ps.Messages == 0 {
		return 0
	}
	avgSaved := float64(ps.InputTokensSaved) / float64(ps.Messages)
	return avgSaved / float64(prefillSpeed)
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

// renderHeader renders the title bar with version, session duration and port.
func (m *Model) renderHeader(innerWidth int) string {
	s := m.styles
	title := s.Title.Render("SLIMFERENCE v" + Version)
	right := s.Dim.Render("◷ "+renderSessionDuration(m.sessionStart)) +
		"  " + s.Muted.Render(fmt.Sprintf(":%d", m.proxy.Config().GetListenPort()))
	pad := innerWidth - lipgloss.Width(title) - lipgloss.Width(right) - 1
	if pad < 1 {
		pad = 1
	}
	return " " + title + strings.Repeat(" ", pad) + right
}

// renderFooterBar renders the full keyboard-hint footer line.
func (m *Model) renderFooterBar() string {
	s := m.styles
	k := func(key, desc string) string {
		return s.Key.Render("["+key+"]") + s.FooterDesc.Render(" "+desc)
	}
	sep := s.KeySep.Render(" · ")
	parts := []string{
		k("c", "claude"), k("x", "codex"),
		k("1-3", "layers"),
		k("s", "stats"), k("d", "debug"), k("i", "setup"),
		k("f", "flush"), k("q", "quit"),
	}
	return " " + strings.Join(parts, sep)
}

// buildLeftPanel builds the left column (providers + layers + hooks) padded to width.
func (m *Model) buildLeftPanel(width int) []string {
	s := m.styles
	snap := m.latestSnap

	pad := func(str string) string {
		w := lipgloss.Width(str)
		if w >= width {
			return str
		}
		return str + strings.Repeat(" ", width-w)
	}

	var lines []string
	add := func(str string) { lines = append(lines, pad(str)) }

	// SETUP STATUS - prominent green/red indicator at the top.
	allReady := m.hookStatus.Claude && m.hookStatus.Codex
	if allReady {
		add(" " + s.Saved.Render("● READY") + "  " + s.Dim.Render("all hooks installed"))
	} else {
		missing := []string{}
		if !m.hookStatus.Claude {
			missing = append(missing, "Claude")
		}
		if !m.hookStatus.Codex {
			missing = append(missing, "Codex")
		}
		add(" " + s.Warning.Render("● SETUP") + "  " + s.Dim.Render("missing: "+strings.Join(missing, ", ")))
		add(" " + s.Dim.Render("  press [i] to set up"))
	}
	add("")

	// PROVIDERS
	add(" " + s.PanelTitle.Render("PROVIDERS"))
	claudeHealth := m.proxy.GetProviderHealth(types.Anthropic)
	codexHealth := m.proxy.GetProviderHealth(types.OpenAI)
	add(" " + renderProviderBadge(s, "Claude Code", m.claudeEnabled) + "  " + renderHealthDot(s, claudeHealth.Status))
	add(" " + renderProviderBadge(s, "Codex", m.codexEnabled) + "  " + renderHealthDot(s, codexHealth.Status))
	add("")

	// LAYERS
	add(" " + s.PanelTitle.Render("LAYERS"))

	l2Status := m.proxy.GetLayer2Status()
	l2Extra := ""
	if l2Status.Compressing {
		l2Extra = "compressing..."
	} else if l2Status.HasCache {
		l2Extra = fmt.Sprintf("last: %s  q:%d", formatAgo(l2Status.LastRun), l2Status.QueueDepth)
	}

	add(renderLayerLine(s, 1, "Deterministic", m.layer1Enabled, snap.Layer1Savings, ""))
	add("   " + s.Muted.Render("struct · delta · dedup"))
	add(renderLayerLine(s, 2, "MiniMax", m.layer2Enabled, snap.Layer2Savings, ""))
	if l2Extra != "" {
		add("   " + s.Muted.Render(l2Extra))
	}
	add(renderLayerLine(s, 3, "Cache", m.layer3Enabled, snap.Layer3Savings, fmt.Sprintf("hits: %d/%d", snap.CacheHits, snap.TotalRequests)))
	add("")

	// HOOKS (only when at least one is installed)
	if m.hookStatus.Claude || m.hookStatus.Codex {
		add(" " + s.PanelTitle.Render("HOOKS"))
		add(" " + renderHookStatus(s, m.hookStatus))
	}

	return lines
}

// buildRightPanel builds the right column (savings + live log / quick-start) padded to width.
func (m *Model) buildRightPanel(width int) []string {
	s := m.styles
	snap := m.latestSnap

	pad := func(str string) string {
		w := lipgloss.Width(str)
		if w >= width {
			return str
		}
		return str + strings.Repeat(" ", width-w)
	}

	var lines []string
	add := func(str string) { lines = append(lines, pad(str)) }

	// SAVINGS
	add(" " + s.PanelTitle.Render("SAVINGS"))

	savedPct := 0
	if snap.TotalInputTokens > 0 {
		savedPct = int((1 - float64(snap.TotalInputTokens-snap.SavedInputTokens)/float64(snap.TotalInputTokens)) * 100)
	}

	origStr := formatTokens(snap.TotalInputTokens)
	compStr := formatTokens(snap.TotalInputTokens - snap.SavedInputTokens)
	savedTok := formatTokens(snap.SavedInputTokens)
	add(" " + s.BigSaved.Render(fmt.Sprintf("%d%%", savedPct)) +
		s.Dim.Render("  "+origStr+" → "+compStr+"  ") +
		s.Saved.Render(savedTok+" saved"))

	barWidth := width - 3
	if barWidth < 8 {
		barWidth = 8
	}
	add(" " + renderProgressBar(s, float64(savedPct)/100.0, barWidth))

	avgPrefill := m.proxy.Config().GetPrefillSpeed()
	extraMsgs := snap.EstExtraMessages(snap.AvgTokensPerRequest)
	ttftImprove := snap.AvgTTFTImprovement(avgPrefill)
	add(" " + s.Saved.Render(fmt.Sprintf("+%d msgs", extraMsgs)) +
		"  " + s.Highlight.Render(fmt.Sprintf("~%.1fs TTFT", ttftImprove)))

	if snap.SecretsRedacted > 0 {
		add(" " + s.Warning.Render(fmt.Sprintf("⚠  %d secrets redacted", snap.SecretsRedacted)))
	}
	if snap.AutoRetries > 0 {
		add(" " + s.Dim.Render(fmt.Sprintf("↺  %d auto-retries", snap.AutoRetries)))
	}
	add("")

	// LIVE log or QUICK START
	if snap.TotalRequests == 0 && !m.hookStatus.Claude && !m.hookStatus.Codex {
		add(" " + s.PanelTitle.Render("QUICK START"))
		add("")
		add(" " + s.Normal.Render("1. Install hooks:"))
		add("   " + s.SetupCmd.Render("$ slimference hook install claude"))
		add("   " + s.SetupCmd.Render("$ slimference hook install codex"))
		add("")
		add(" " + s.Normal.Render("2. Start Claude Code or Codex CLI"))
		add(" " + s.Muted.Render("   Requests appear here automatically."))
	} else {
		maxLog := 6
		if m.height >= 30 {
			maxLog = 10
		}
		add(" " + s.PanelTitle.Render("LIVE"))
		reqs := m.proxy.GetRecentRequests(maxLog)
		if len(reqs) == 0 {
			add(" " + s.Muted.Render("Waiting for requests..."))
		} else {
			for i := len(reqs) - 1; i >= 0; i-- {
				add(renderRequestLogLine(s, reqs[i]))
			}
		}
	}

	return lines
}

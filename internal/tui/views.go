package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/slimference/slimference/internal/analytics"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/sessions"
	"github.com/slimference/slimference/internal/types"
)

// renderMainView renders the five-item home menu.
func (m *Model) renderMainView() string {
	s := m.styles
	width := m.width
	if width < 60 {
		width = 80
	}
	innerWidth := width - 4

	header := m.renderHeader(innerWidth)
	rule := s.HorizRule.Render(strings.Repeat("─", innerWidth))

	menuLines := m.buildLeftPanel(innerWidth)

	flashLine := ""
	if m.flashMsg != "" && time.Now().Before(m.flashExpiry) {
		flashLine = "\n" + s.Flash.Render("  "+m.flashMsg)
	}

	content := header + "\n" + rule + "\n" +
		strings.Join(menuLines, "\n") + "\n" + rule +
		flashLine

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
	ratio := 0
	if snap.TotalInputTokens > 0 {
		ratio = int((1 - float64(snap.TotalInputTokens-snap.SavedInputTokens)/float64(snap.TotalInputTokens)) * 100)
	}

	innerWidth := width - 4
	rule := s.HorizRule.Render(strings.Repeat("─", innerWidth))

	var lines []string

	lines = append(lines, " "+s.Title.Render("SLIMFERENCE")+" "+s.Dim.Render("/ Savings"))
	lines = append(lines, rule)

	cardIndex := 0
	cardStyle := func(index int) lipgloss.Style {
		if m.statsCursor == index {
			return s.CardActive.Width(innerWidth - 2)
		}
		return s.Card.Width(innerWidth - 2)
	}
	appendCard := func(title string, body []string) {
		lines = append(lines, renderInfoCard(cardStyle(cardIndex), " "+s.PanelTitle.Render(title), body))
		lines = append(lines, "")
		cardIndex++
	}

	appendCard("SESSION SNAPSHOT", []string{
		" " + s.BigSaved.Render(fmt.Sprintf("%d%%", ratio)) + " " + s.Dim.Render("avg compression") +
			"    " + s.Highlight.Render(fmt.Sprintf("+%d msgs", extraMsgs)) +
			"    " + s.Saved.Render(fmt.Sprintf("~%.1fs TTFT", ttftImprove)),
		"",
		" " + renderKPIRow(s,
			fmt.Sprintf("%d requests", snap.TotalRequests),
			fmt.Sprintf("%d errors", snap.Errors),
			renderSessionDuration(snap.SessionStart),
		),
	})

	appendCard("SESSION", []string{
		fmt.Sprintf("  Started: %s", snap.SessionStart.Format("2006-01-02 15:04")),
		fmt.Sprintf("  Duration: %s", renderSessionDuration(snap.SessionStart)),
		fmt.Sprintf("  Requests: %d", snap.TotalRequests),
		fmt.Sprintf("  Errors: %d", snap.Errors),
	})

	headers := []string{"Metric", "Original", "After", "Saved"}
	rows := [][]string{
		{"Total Input", formatTokens(snap.TotalInputTokens), formatTokens(snap.TotalInputTokens - snap.SavedInputTokens), fmt.Sprintf("%d%%", ratio)},
		{"Deterministic", "-", "-", formatTokens(snap.Layer1Savings)},
		{"Cache layer", "-", "-", formatTokens(snap.Layer2Savings)},
		{"Total Output", formatTokens(snap.TotalOutputTokens), "(passthru)", "-"},
	}
	appendCard("SAVINGS", []string{renderTable(s, headers, rows, []int{20, 12, 12, 8})})

	readCache := m.proxy.GetReadCacheStatus()
	hitRateLine := fmt.Sprintf("  Hit rate:         %.1f%%", readCache.HitRate*100)
	hitRateStyle := s.Saved
	if readCache.HitRate < 0.40 && readCache.Blocks+readCache.Allows > 10 {
		hitRateStyle = s.Highlight
	}
	appendCard("READ CACHE", []string{
		s.Normal.Render(fmt.Sprintf("  Evaluations:      %d", readCache.Evaluations)),
		s.Normal.Render(fmt.Sprintf("  Blocks:           %d (%d unchanged, %d delta)", readCache.Blocks, readCache.UnchangedBlocks, readCache.DeltaBlocks)),
		s.Normal.Render(fmt.Sprintf("  Allows:           %d", readCache.Allows)),
		hitRateStyle.Render(hitRateLine),
		s.Normal.Render(fmt.Sprintf("  Tracked files:    %d across %d sessions", readCache.TrackedFiles, readCache.Sessions)),
	})

	layer0 := m.proxy.GetLayer0Status()
	layer0Lines := []string{
		s.Normal.Render(fmt.Sprintf("  Attempts:        %d", layer0.Attempts)),
		s.Normal.Render(fmt.Sprintf("  Matches:         %d (%.1f%% hit)", layer0.Matches, layer0.HitRate*100)),
		s.Normal.Render(fmt.Sprintf("  Misses/Panics:   %d / %d", layer0.Misses, layer0.Panics)),
		s.Normal.Render(fmt.Sprintf("  Bytes saved:     %s", formatBytesCompact(layer0.BytesSaved))),
	}
	if len(layer0.Filters) == 0 {
		layer0Lines = append(layer0Lines, s.Muted.Render("  No parser attempts recorded yet."))
	} else {
		limit := len(layer0.Filters)
		if limit > 3 {
			limit = 3
		}
		for i := 0; i < limit; i++ {
			f := layer0.Filters[i]
			layer0Lines = append(layer0Lines, s.Muted.Render(fmt.Sprintf("  %s  %d/%d · %s · %.2fms",
				f.Name, f.Matches, f.Attempts, formatBytesCompact(f.BytesSaved), f.AvgMs)))
		}
	}
	appendCard("LAYER 0 PARSERS", layer0Lines)

	checkpoints := m.proxy.GetCheckpointStatus()
	appendCard("CHECKPOINTS", []string{
		s.Normal.Render(fmt.Sprintf("  Captures:         %d (%d restores)", checkpoints.Captures, checkpoints.Restores)),
		s.Normal.Render(fmt.Sprintf("  Stored:           %d checkpoints · %s", checkpoints.Count, formatBytesCompact(checkpoints.Bytes))),
		s.Normal.Render(fmt.Sprintf("  Last trigger:     %s", fallbackLabel(checkpoints.LastTrigger, "none"))),
		s.Normal.Render(fmt.Sprintf("  Last capture:     %s", formatStatusTime(checkpoints.LastCapture))),
	})

	archive := m.proxy.GetToolArchiveStatus()
	appendCard("TOOL ARCHIVE", []string{
		s.Normal.Render(fmt.Sprintf("  Archived:         %d (%d expands)", archive.Archived, archive.Expanded)),
		s.Normal.Render(fmt.Sprintf("  Stored entries:   %d", archive.Count)),
		s.Normal.Render(fmt.Sprintf("  Raw vs stored:    %s -> %s", formatBytesCompact(archive.BytesRaw), formatBytesCompact(archive.BytesStored))),
		s.Normal.Render(fmt.Sprintf("  Last archive:     %s", formatStatusTime(archive.LastArchived))),
	})

	q := m.proxy.GetQualityStatus()
	spikeMarker := "no"
	if q.SpikeActive {
		spikeMarker = "ACTIVE"
	}
	appendCard("QUALITY SIGNALS (T77)", []string{
		s.Normal.Render(fmt.Sprintf("  Re-read sessions:  %d", q.ReReadSessions)),
		s.Normal.Render(fmt.Sprintf("  Re-read events:    %d / %d checks (rate %.2f%%)", q.ReReadTotalHits, q.ReReadTotalChecks, q.ReReadRate*100)),
		s.Normal.Render(fmt.Sprintf("  Cache miss spike:  %s (baseline %.1f%%, total %d)", spikeMarker, q.BaselineHitRate*100, q.TotalSpikeCount)),
		s.Normal.Render(fmt.Sprintf("  Tokens saved:      %s", formatTokens(int(q.TotalSaved)))),
		s.Normal.Render(fmt.Sprintf("  Invalidation cost: %s", formatTokens(int(q.TotalInvalidation)))),
		s.Normal.Render(fmt.Sprintf("  Net saved tokens:  %s", formatTokens(int(q.NetSaved)))),
	})

	promptHitRate := snap.PromptCacheHitRate() * 100
	appendCard("PROMPT CACHE", []string{
		s.Normal.Render(fmt.Sprintf("  Read hits:        %d / %d (%.1f%%)", snap.PromptCacheReadRequests, snap.TotalRequests, promptHitRate)),
		s.Normal.Render(fmt.Sprintf("  Read tokens:      %s", formatTokens(snap.PromptCacheReadTokens))),
		s.Normal.Render(fmt.Sprintf("  Create tokens:    %s", formatTokens(snap.PromptCacheCreateTokens))),
		s.Normal.Render(fmt.Sprintf("  Est. read savings %s", formatTokens(int(float64(snap.PromptCacheReadTokens)*0.9)))),
	})

	avgOrig := 0
	avgComp := 0
	if snap.TotalRequests > 0 {
		avgOrig = snap.TotalInputTokens / snap.TotalRequests
		avgComp = (snap.TotalInputTokens - snap.SavedInputTokens) / snap.TotalRequests
	}
	sessMultiplier := 1.0
	if snap.TotalInputTokens > 0 && snap.TotalInputTokens != snap.SavedInputTokens {
		sessMultiplier = float64(snap.TotalInputTokens) / float64(snap.TotalInputTokens-snap.SavedInputTokens)
	}
	appendCard("CAPACITY GAIN", []string{
		s.Normal.Render(fmt.Sprintf("  Extra messages:    +%d", extraMsgs)),
		s.Normal.Render(fmt.Sprintf("  Session extended:  ~%.1fx longer before limit", sessMultiplier)),
		s.Normal.Render(fmt.Sprintf("  TTFT improvement:  ~%.1fs faster per response", ttftImprove)),
		s.Normal.Render(fmt.Sprintf("  Avg ratio:         %d%% compression", ratio)),
		s.Normal.Render(fmt.Sprintf("  Avg tokens/req:    %s (was %s)", formatTokens(avgComp), formatTokens(avgOrig))),
	})

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
	appendCard("PER PROVIDER", []string{renderTable(s, pHeaders, pRows, []int{18, 10, 12, 8})})

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
	if len(latRows) == 0 {
		appendCard("LATENCY", []string{s.Muted.Render("  No requests yet.")})
	} else {
		appendCard("LATENCY", []string{renderTable(s, latHeaders, latRows, []int{18, 10, 14})})
	}

	retriesDetail := ""
	if snap.AutoRetries > 0 {
		retriesDetail = fmt.Sprintf(" (%dx rate-limit, %dx overflow)", snap.RateLimitRetries, snap.OverflowRetries)
	}
	appendCard("RESILIENCE", []string{
		fmt.Sprintf("  Auto-retries: %d%s", snap.AutoRetries, retriesDetail),
		fmt.Sprintf("  Secrets redacted: %d", snap.SecretsRedacted),
	})

	lines = append(lines, rule)

	content := strings.Join(lines, "\n")
	return s.Border.Width(width - 2).Render(content)
}

// renderStatusView renders runtime state without logs or diagnostics noise.
func (m *Model) renderStatusView() string {
	s := m.styles
	width := m.width
	if width < 60 {
		width = 80
	}

	innerWidth := width - 4
	rule := s.HorizRule.Render(strings.Repeat("─", innerWidth))

	var lines []string
	lines = append(lines, " "+s.Title.Render("SLIMFERENCE")+" "+s.Dim.Render("/ Status"))
	lines = append(lines, rule)

	daemonLines := []string{" " + s.PanelTitle.Render("DAEMON")}
	if m.svc != nil {
		running, pid, port := m.svc.DaemonStatus()
		if running {
			daemonLines = append(daemonLines, "", "  "+s.Saved.Render("● RUNNING")+"  PID "+fmt.Sprintf("%d  port :%d", pid, port))
		} else {
			daemonLines = append(daemonLines, "", "  "+s.Muted.Render("○ STOPPED")+"  port :"+fmt.Sprintf("%d", m.proxy.Config().GetListenPort()))
		}
		if notice := m.svc.DaemonNotice(); notice != "" {
			daemonLines = append(daemonLines, "  "+s.BannerWarn.Render("● NOTICE")+"  "+notice)
		}
	} else {
		daemonLines = append(daemonLines, "", "  "+s.Muted.Render("○ SERVICE ADAPTER")+"  unavailable", "  "+s.Muted.Render(fmt.Sprintf("listen port :%d", m.proxy.Config().GetListenPort())))
	}
	lines = append(lines, s.Card.Width(innerWidth-2).Render(strings.Join(daemonLines, "\n")))
	lines = append(lines, "")

	modeLines := []string{
		" " + s.PanelTitle.Render("CODEX MODE"),
		"",
		"  " + s.Muted.Render("Normal Codex direct. Launch here = Slimference mode."),
		renderCodexRouteStatusLine(s, m.codexRouteStatus),
		renderCodexDesktopStatusLine(s, m.codexDesktopStatus),
	}
	lines = append(lines, s.Card.Width(innerWidth-2).Render(strings.Join(modeLines, "\n")))
	lines = append(lines, "")

	transparent := TransparentStatus{}
	if m.svc != nil {
		transparent = m.transparentStatus
	}
	safetyLines := []string{
		" " + s.PanelTitle.Render("SAFETY"),
		"",
		renderCAStatusLine(s, transparent),
		renderTransparentStatusLine(s, transparent),
		"  " + s.Normal.Render(productSafetyLine(m.latestProduct)),
	}
	lines = append(lines, s.Card.Width(innerWidth-2).Render(strings.Join(safetyLines, "\n")))

	lines = append(lines, "")
	lines = append(lines, rule)

	content := strings.Join(lines, "\n")
	return s.Border.Width(width - 2).Render(content)
}

// renderActivityView renders scoped sessions and recent Slimference traffic.
func (m *Model) renderActivityView() string {
	s := m.styles
	width := m.width
	if width < 60 {
		width = 80
	}

	innerWidth := width - 4
	rule := s.HorizRule.Render(strings.Repeat("─", innerWidth))

	var lines []string
	lines = append(lines, " "+s.Title.Render("SLIMFERENCE")+" "+s.Dim.Render("/ Activity"))
	lines = append(lines, rule)
	lines = append(lines, s.Card.Width(innerWidth-2).Render(renderHookActivity(m, loadHookActivityStatuses(8))))
	lines = append(lines, "")
	lines = append(lines, s.Card.Width(innerWidth-2).Render(renderTrafficActivity(m, m.proxy.GetRecentFlights(8))))
	lines = append(lines, "")
	lines = append(lines, rule)

	content := strings.Join(lines, "\n")
	return s.Border.Width(width - 2).Render(content)
}

func renderHookActivity(m *Model, statuses []hookTurnDebugStatus) string {
	s := m.styles
	body := []string{" " + s.PanelTitle.Render("ACTIVE SESSIONS")}
	if len(statuses) == 0 {
		body = append(body, "", "  "+s.Muted.Render("No Slimference hook sessions yet."))
		return strings.Join(body, "\n")
	}
	for _, status := range statuses {
		state := s.Muted.Render("RECENT")
		if !status.Closed {
			state = s.Saved.Render("ACTIVE")
		}
		path := hookActivityPath(status)
		if path == "" {
			path = "path unknown"
		}
		line := fmt.Sprintf("%s  session %s  turn %s  updated %s",
			state,
			compactDebugLabel(status.SessionID, 20),
			compactDebugLabel(status.TurnID, 12),
			formatStatusTime(status.UpdatedAt),
		)
		body = append(body, "", "  "+line)
		body = append(body, "  "+s.Muted.Render(fmt.Sprintf("path %s", compactDebugLabel(path, 64))))
		body = append(body, "  "+s.Muted.Render(fmt.Sprintf("tools %d  read %d  edited %d  git-lists %d", len(status.Tools), len(status.FilesRead), len(status.FilesEdited), len(status.GitPathLists))))
		if last := hookActivityLastWork(status); last != "" {
			body = append(body, "  "+s.Muted.Render("last "+compactDebugLabel(last, 64)))
		}
	}
	return strings.Join(body, "\n")
}

func renderTrafficActivity(m *Model, flights []dbg.FlightRequestSummary) string {
	s := m.styles
	body := []string{" " + s.PanelTitle.Render("RECENT TRAFFIC")}
	if len(flights) == 0 {
		body = append(body, "", "  "+s.Muted.Render("No routed Slimference traffic yet."))
		return strings.Join(body, "\n")
	}
	start := 0
	if len(flights) > 6 {
		start = len(flights) - 6
	}
	for _, flight := range flights[start:] {
		saved := flight.TokenAccounting.BillableSavingsEstimate
		savedLabel := s.Muted.Render("0 saved")
		if saved > 0 {
			savedLabel = s.Saved.Render(fmt.Sprintf("%d saved", saved))
		}
		label := compactDebugLabel(firstNonEmpty(flight.SessionID, flight.RequestID, "request"), 20)
		target := compactDebugLabel(firstNonEmpty(flight.Path, flight.Host, flight.Source), 36)
		body = append(body, "", "  "+fmt.Sprintf("%s  %s  %s  %s",
			s.Highlight.Render(label),
			compactDebugLabel(firstNonEmpty(flight.ClientFamily, flight.Provider, "client"), 16),
			compactDebugLabel(firstNonEmpty(flight.RouteMode, "route"), 18),
			savedLabel,
		))
		body = append(body, "  "+s.Muted.Render(fmt.Sprintf("%s  model %s", target, compactDebugLabel(firstNonEmpty(flight.Provider, "unknown"), 18))))
	}
	return strings.Join(body, "\n")
}

func loadHookActivityStatuses(limit int) []hookTurnDebugStatus {
	home, _ := userHomeDirFn()
	dir := sessions.DefaultHookStateDir(strings.TrimSpace(home))
	return loadHookActivityStatusesFromDir(dir, limit)
}

func loadHookActivityStatusesFromDir(dir string, limit int) []hookTurnDebugStatus {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	statuses := make([]hookTurnDebugStatus, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		sessionID := strings.TrimSuffix(entry.Name(), ".json")
		state, err := sessions.LoadHookState(dir, sessionID)
		if err != nil || len(state.Turns) == 0 {
			continue
		}
		turn := state.Turns[len(state.Turns)-1]
		for _, candidate := range state.Turns {
			if candidate.ID == state.CurrentTurn {
				turn = candidate
				break
			}
		}
		statuses = append(statuses, hookTurnDebugStatus{
			Present:      true,
			StatePath:    filepath.Join(dir, entry.Name()),
			SessionID:    state.SessionID,
			TurnID:       turn.ID,
			Closed:       turn.Closed,
			UpdatedAt:    turn.UpdatedAt,
			Tools:        append([]string(nil), turn.Tools...),
			FilesRead:    append([]string(nil), turn.FilesRead...),
			FilesEdited:  append([]string(nil), turn.FilesEdited...),
			GitPathLists: append([]sessions.HookGitPathListState(nil), turn.GitPathLists...),
		})
	}
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].UpdatedAt.After(statuses[j].UpdatedAt)
	})
	if limit > 0 && len(statuses) > limit {
		statuses = statuses[:limit]
	}
	return statuses
}

func hookActivityPath(status hookTurnDebugStatus) string {
	for i := len(status.GitPathLists) - 1; i >= 0; i-- {
		if cwd := strings.TrimSpace(status.GitPathLists[i].CWD); cwd != "" {
			return cwd
		}
	}
	if last := hookActivityLastPath(status.FilesEdited); last != "" {
		return filepath.Dir(last)
	}
	if last := hookActivityLastPath(status.FilesRead); last != "" {
		return filepath.Dir(last)
	}
	return ""
}

func hookActivityLastWork(status hookTurnDebugStatus) string {
	if last := hookActivityLastPath(status.FilesEdited); last != "" {
		return "edited " + last
	}
	if last := hookActivityLastPath(status.FilesRead); last != "" {
		return "read " + last
	}
	if len(status.Tools) > 0 {
		return "tool " + status.Tools[len(status.Tools)-1]
	}
	return ""
}

func hookActivityLastPath(values []string) string {
	for i := len(values) - 1; i >= 0; i-- {
		if value := strings.TrimSpace(values[i]); value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// renderLogsView renders logs, flight diagnostics, and export actions.
func (m *Model) renderLogsView() string {
	s := m.styles
	width := m.width
	if width < 60 {
		width = 80
	}

	innerWidth := width - 4
	rule := s.HorizRule.Render(strings.Repeat("─", innerWidth))

	var lines []string
	lines = append(lines, " "+s.Title.Render("SLIMFERENCE")+" "+s.Dim.Render("/ Logs"))
	lines = append(lines, rule)

	actions := m.debugActions()
	if len(actions) > 0 {
		action := actions[clampIndex(m.debugCursor, len(actions))]
		lines = append(lines, s.CardActive.Width(innerWidth-2).Render(strings.Join([]string{
			" " + s.PanelTitle.Render("ACTION"),
			"",
			" " + renderMenuRow(s, innerWidth-8, true, action.label, action.state),
			" " + s.Muted.Render(action.description),
		}, "\n")))
		lines = append(lines, "")
	}

	flights := m.proxy.GetRecentFlights(8)
	lines = append(lines, s.Card.Width(innerWidth-2).Render(renderFlightDiagnostics(m, flights)))
	lines = append(lines, "")

	lines = append(lines, s.Card.Width(innerWidth-2).Render(renderHookTurnDiagnostics(m, loadLatestHookTurnDebugStatus())))
	lines = append(lines, "")

	if m.proxy.SessionLogger() != nil {
		entries := m.proxy.SessionLogger().Recent(30)
		if len(entries) == 0 {
			lines = append(lines, s.Card.Width(innerWidth-2).Render(strings.Join([]string{
				" " + s.PanelTitle.Render("LOG STREAM"),
				"",
				s.Muted.Render("  No log entries yet."),
			}, "\n")))
		} else {
			body := []string{
				" " + s.PanelTitle.Render("LOG STREAM"),
				"",
			}
			for _, entry := range entries {
				formatted := m.proxy.SessionLogger().Format(entry)
				body = append(body, "  "+logLevelStyle(s, entry.Level).Render(formatted))
			}
			lines = append(lines, s.CardActive.Width(innerWidth-2).Render(strings.Join(body, "\n")))
		}
	} else {
		lines = append(lines, s.Card.Width(innerWidth-2).Render(strings.Join([]string{
			" " + s.PanelTitle.Render("LOG STREAM"),
			"",
			s.Muted.Render("  No log entries yet."),
		}, "\n")))
	}

	lines = append(lines, "")
	lines = append(lines, rule)

	content := strings.Join(lines, "\n")
	return s.Border.Width(width - 2).Render(content)
}

func renderFlightDiagnostics(m *Model, flights []dbg.FlightRequestSummary) string {
	s := m.styles
	body := []string{" " + s.PanelTitle.Render("FLIGHT RECORDER")}
	if len(flights) == 0 {
		body = append(body, "", s.Muted.Render("  No flight records yet."))
		return strings.Join(body, "\n")
	}
	var saved, cached, output, bypasses, planBlocks int
	slowest := 0.0
	slowestID := ""
	for _, f := range flights {
		saved += flightSaved(f)
		cached += flightCached(f)
		output += flightOutput(f)
		if f.RouteMode == "raw_passthrough" || f.RouteMode == "local_cache" || f.BypassReason != "" {
			bypasses++
		}
		if f.Plan != nil && f.Plan.SafetyBlocked {
			planBlocks++
		}
		if f.TotalProxyOverheadMs > slowest {
			slowest = f.TotalProxyOverheadMs
			slowestID = f.RequestID
		}
	}
	body = append(body,
		"",
		" "+s.Normal.Render(fmt.Sprintf("requests %d  saved %s  cached %s  output %s", len(flights), formatTokens(saved), formatTokens(cached), formatTokens(output))),
		" "+s.Normal.Render(fmt.Sprintf("bypasses %d  plan-blocks %d  slowest %.1fms %s", bypasses, planBlocks, slowest, slowestID)),
		"",
	)
	for _, f := range flights {
		label := f.RequestID
		if len(label) > 14 {
			label = label[:14]
		}
		body = append(body, " "+s.Muted.Render(fmt.Sprintf("%-14s", label))+
			s.Normal.Render(fmt.Sprintf(" %s/%s L%v saved=%s cache=%s out=%s",
				f.Source, f.RouteMode, f.Layers, formatTokens(flightSaved(f)), formatTokens(flightCached(f)), formatTokens(flightOutput(f)))))
		if planLine := renderFlightPlanLine(f); planLine != "" {
			body = append(body, " "+s.Muted.Render("plan")+" "+s.Normal.Render(planLine))
		}
	}
	return strings.Join(body, "\n")
}

func renderFlightPlanLine(f dbg.FlightRequestSummary) string {
	if f.Plan == nil || len(f.Plan.Decisions) == 0 {
		return ""
	}
	parts := make([]string, 0, min(len(f.Plan.Decisions), 4))
	for _, decision := range f.Plan.Decisions {
		if decision.Layer == "" || decision.Action == "" {
			continue
		}
		parts = append(parts, decision.Layer+"="+decision.Action)
		if len(parts) == 4 {
			break
		}
	}
	if len(parts) == 0 {
		return ""
	}
	line := strings.Join(parts, " ")
	if len(f.Plan.Decisions) > len(parts) {
		line += fmt.Sprintf(" +%d", len(f.Plan.Decisions)-len(parts))
	}
	if f.Plan.SafetyBlocked {
		line += " blocked"
	}
	return line
}

func flightSaved(f dbg.FlightRequestSummary) int {
	return f.TokenAccounting.BillableSavingsEstimate
}

func flightCached(f dbg.FlightRequestSummary) int {
	if f.CacheAccounting.ProviderCachedInputTokens > 0 {
		return f.CacheAccounting.ProviderCachedInputTokens
	}
	return f.CacheAccounting.ProviderCacheReadTokens
}

func flightOutput(f dbg.FlightRequestSummary) int {
	if f.TokenAccounting.ProviderOutputTokens > 0 {
		return f.TokenAccounting.ProviderOutputTokens
	}
	return f.TokenAccounting.EstimatedOutputTokens
}

type hookTurnDebugStatus struct {
	Present      bool
	Error        string
	StatePath    string
	SessionID    string
	TurnID       string
	Closed       bool
	UpdatedAt    time.Time
	Tools        []string
	FilesRead    []string
	FilesEdited  []string
	GitPathLists []sessions.HookGitPathListState
}

func loadLatestHookTurnDebugStatus() hookTurnDebugStatus {
	home, _ := userHomeDirFn()
	dir := sessions.DefaultHookStateDir(strings.TrimSpace(home))
	return loadLatestHookTurnDebugStatusFromDir(dir)
}

func loadLatestHookTurnDebugStatusFromDir(dir string) hookTurnDebugStatus {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return hookTurnDebugStatus{}
		}
		return hookTurnDebugStatus{Error: err.Error()}
	}
	var latest hookTurnDebugStatus
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		sessionID := strings.TrimSuffix(entry.Name(), ".json")
		state, err := sessions.LoadHookState(dir, sessionID)
		if err != nil {
			return hookTurnDebugStatus{Present: true, StatePath: filepath.Join(dir, entry.Name()), SessionID: sessionID, Error: err.Error()}
		}
		turn := state.Turns[len(state.Turns)-1]
		for _, candidate := range state.Turns {
			if candidate.ID == state.CurrentTurn {
				turn = candidate
				break
			}
		}
		status := hookTurnDebugStatus{
			Present:      true,
			StatePath:    filepath.Join(dir, entry.Name()),
			SessionID:    state.SessionID,
			TurnID:       turn.ID,
			Closed:       turn.Closed,
			UpdatedAt:    turn.UpdatedAt,
			Tools:        append([]string(nil), turn.Tools...),
			FilesRead:    append([]string(nil), turn.FilesRead...),
			FilesEdited:  append([]string(nil), turn.FilesEdited...),
			GitPathLists: append([]sessions.HookGitPathListState(nil), turn.GitPathLists...),
		}
		if !latest.Present || status.UpdatedAt.After(latest.UpdatedAt) {
			latest = status
		}
	}
	return latest
}

func renderHookTurnDiagnostics(m *Model, status hookTurnDebugStatus) string {
	s := m.styles
	body := []string{" " + s.PanelTitle.Render("HOOK TURN STATE")}
	if status.Error != "" {
		body = append(body, "", s.Highlight.Render("  "+status.Error))
		return strings.Join(body, "\n")
	}
	if !status.Present {
		body = append(body, "", s.Muted.Render("  No hook turn-state file yet."))
		return strings.Join(body, "\n")
	}
	state := "open"
	if status.Closed {
		state = "closed"
	}
	body = append(body,
		"",
		" "+s.Normal.Render(fmt.Sprintf("session %s  turn %s  %s  updated %s", compactDebugLabel(status.SessionID, 18), status.TurnID, state, formatStatusTime(status.UpdatedAt))),
		" "+s.Normal.Render(fmt.Sprintf("tools %d  read %d  edited %d  git-lists %d", len(status.Tools), len(status.FilesRead), len(status.FilesEdited), len(status.GitPathLists))),
	)
	for _, hint := range hookTurnDecisionHints(status) {
		body = append(body, " "+s.Normal.Render("gate: "+hint))
	}
	if line := compactDebugList("tools", status.Tools, 3); line != "" {
		body = append(body, " "+s.Muted.Render(line))
	}
	if line := compactDebugList("read", status.FilesRead, 3); line != "" {
		body = append(body, " "+s.Muted.Render(line))
	}
	if line := compactDebugList("edited", status.FilesEdited, 3); line != "" {
		body = append(body, " "+s.Muted.Render(line))
	}
	if len(status.GitPathLists) > 0 {
		last := status.GitPathLists[len(status.GitPathLists)-1]
		body = append(body, " "+s.Muted.Render(fmt.Sprintf("git last %s paths=%d cwd=%s", compactDebugLabel(last.Source, 16), last.Count, compactDebugLabel(last.CWD, 28))))
	}
	return strings.Join(body, "\n")
}

func hookTurnDecisionHints(status hookTurnDebugStatus) []string {
	hints := []string{}
	if len(status.FilesEdited) > 0 {
		hints = append(hints, "recent edit observed; matching file reads stay literal")
	} else if len(status.FilesRead) > 0 {
		hints = append(hints, "file reads observed; scan-sized Go reads may AST-compact")
	}
	if len(status.GitPathLists) > 0 {
		hints = append(hints, "git path list recorded; exact same-turn name-only repeats may elide")
	}
	return hints
}

func compactDebugList(label string, values []string, limit int) string {
	if len(values) == 0 || limit <= 0 {
		return ""
	}
	n := len(values)
	if n > limit {
		values = values[n-limit:]
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, compactDebugLabel(value, 32))
	}
	line := label + ": " + strings.Join(parts, ", ")
	if n > len(values) {
		line += fmt.Sprintf(" +%d", n-len(values))
	}
	return line
}

func compactDebugLabel(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 1 {
		return value[:limit]
	}
	return value[:limit-1] + "…"
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

	lines = append(lines, " "+s.Title.Render("SLIMFERENCE")+" "+s.Dim.Render("/ Setup"))
	lines = append(lines, rule)

	// Overall READY status.
	transparent := TransparentStatus{}
	if m.svc != nil {
		transparent = m.transparentStatus
	}
	productReady := transparent.CAExists && transparent.AutoStartInstalled
	allReady := productReady || (m.svc == nil && m.hookStatus.Codex)
	statusCard := ""
	if transparent.ProxyArmed {
		statusCard = s.Card.Width(innerWidth - 2).Render(
			s.BannerGood.Render("ARMED") + " " + s.Normal.Render("System HTTPS is routed through Slimference."),
		)
	} else if allReady {
		message := "Slimference is installed. Normal Codex stays direct; use Slimference Launch when you want traffic in the pipeline."
		if m.svc == nil {
			message = "ALL SET - Slimference is ready for daily use."
		}
		statusCard = s.Card.Width(innerWidth - 2).Render(
			s.BannerGood.Render("READY") + " " + s.Normal.Render(message),
		)
	} else {
		missing := []string{}
		if m.svc != nil {
			if !transparent.CAExists {
				missing = append(missing, "local CA")
			}
			if !transparent.AutoStartInstalled {
				missing = append(missing, "launchd daemon")
			}
		} else {
			if !m.hookStatus.Codex {
				missing = append(missing, "Codex hook")
			}
		}
		statusCard = s.Card.Width(innerWidth - 2).Render(
			s.BannerWarn.Render("SETUP") + " " + s.Normal.Render("Missing: "+strings.Join(missing, ", ")),
		)
	}
	lines = append(lines, statusCard)
	lines = append(lines, "")

	// Interactive wizard steps.
	if m.svc != nil {
		steps := m.setupSteps()
		stepLines := []string{
			" " + s.PanelTitle.Render("SETUP STEPS"),
			" " + s.Dim.Render("One product install prepares Codex CLI and Desktop together; capability state lives under Status."),
			"",
		}
		for i, step := range steps {
			stepLines = append(stepLines, renderSetupStepRow(s, i, step.label, step.check(), m.setupCursor == i))
			if m.setupCursor == i && !step.check() {
				stepLines = append(stepLines, "   "+s.SetupCmd.Render("Enter ↵ "+step.confirm))
			}
		}
		lines = append(lines, s.CardActive.Width(innerWidth-2).Render(strings.Join(stepLines, "\n")))
		lines = append(lines, "")

		// Service controls.
		serviceLines := []string{
			" " + s.PanelTitle.Render("SERVICE CONTROLS"),
			"",
		}
		running, pid, port := m.svc.DaemonStatus()
		if running {
			serviceLines = append(serviceLines, "  "+s.Saved.Render("● RUNNING")+"  PID "+fmt.Sprintf("%d  port :%d", pid, port))
		} else {
			serviceLines = append(serviceLines, "  "+s.Muted.Render("○ STOPPED")+"  daemon not running")
			serviceLines = append(serviceLines, "  "+s.Muted.Render("Press [p] to start; press [o] to restart/repair launchd"))
		}
		if notice := m.svc.DaemonNotice(); notice != "" {
			serviceLines = append(serviceLines, "  "+s.BannerWarn.Render("● OLD PROCESS")+"  "+notice)
		}
		serviceLines = append(serviceLines, renderCAStatusLine(s, transparent))
		serviceLines = append(serviceLines, renderTransparentStatusLine(s, transparent))
		serviceLines = append(serviceLines, renderCodexRouteStatusLine(s, m.codexRouteStatus))
		serviceLines = append(serviceLines, "")
		serviceLines = append(serviceLines, "  "+s.Muted.Render("[r] advanced shared route  [p] start/stop  [o] restart/repair daemon"))
		serviceLines = append(serviceLines, "  "+s.Muted.Render("[a] app routing  [g] advanced lab  [u] uninstall Slimference assets"))
		serviceLines = append(serviceLines, "  "+s.Muted.Render("[e] enable autostart  [w] disable autostart"))
		lines = append(lines, s.Card.Width(innerWidth-2).Render(strings.Join(serviceLines, "\n")))

	} else {
		// Fallback without service control.
		home, _ := os.UserHomeDir()
		check := func(label string, ok bool) string {
			if ok {
				return "  " + s.StepDone.Render("✓") + "  " + s.Normal.Render(label)
			}
			return "  " + s.LogError.Render("✗") + "  " + s.Muted.Render(label)
		}
		checklistLines := []string{
			" " + s.PanelTitle.Render("SETUP CHECKLIST"),
			"",
		}
		_, cfgErr := os.Stat(configPath())
		checklistLines = append(checklistLines, check("Config file", cfgErr == nil))
		checklistLines = append(checklistLines, check("Codex hook installed", m.hookStatus.Codex))
		port := m.proxy.Config().GetListenPort()
		checklistLines = append(checklistLines, check(fmt.Sprintf("Proxy listening on :%d", port), true))
		pidPath := filepath.Join(home, ".slimference", "slimference.pid")
		if pidData, pidErr := os.ReadFile(pidPath); pidErr == nil && len(pidData) > 0 {
			checklistLines = append(checklistLines, check("Daemon running", true))
		}
		plistPath := filepath.Join(home, "Library", "LaunchAgents", "com.slimference.daemon.plist")
		if _, err := os.Stat(plistPath); err == nil {
			checklistLines = append(checklistLines, check("launchd auto-start service", true))
		}
		lines = append(lines, s.Card.Width(innerWidth-2).Render(strings.Join(checklistLines, "\n")))
		lines = append(lines, "")

		statusLines := []string{
			" " + s.PanelTitle.Render("SERVICE STATUS"),
			"",
		}
		pidRunning := false
		if pidData, pidErr := os.ReadFile(pidPath); pidErr == nil && len(pidData) > 0 {
			pidRunning = true
		}
		if pidRunning {
			statusLines = append(statusLines, "  "+s.Saved.Render("● Daemon running"))
		} else {
			statusLines = append(statusLines, "  "+s.Muted.Render("○ Daemon not running"))
		}
		lines = append(lines, s.Card.Width(innerWidth-2).Render(strings.Join(statusLines, "\n")))

		lines = append(lines, "")
		commandLines := []string{
			" " + s.PanelTitle.Render("COMMANDS (scoped Codex path)"),
			"",
			"  " + s.SetupCmd.Render("slimference install"),
			"  " + s.SetupCmd.Render("slimference codex run -- <prompt>") + s.Dim.Render(" # one-shot CLI"),
			"  " + s.SetupCmd.Render("slimference codex recertify wss --force") + s.Dim.Render(" # repair savings proof"),
			"  " + s.SetupCmd.Render("slimference enable") + s.Dim.Render("                  # advanced shared route"),
			"  " + s.SetupCmd.Render("slimference disable") + s.Dim.Render("                 # normal direct Codex"),
			"  " + s.SetupCmd.Render("slimference codex status") + s.Dim.Render("         # Codex status"),
			"  " + s.SetupCmd.Render("go run ./scripts/utils workday-savings start") + s.Dim.Render(" # begin measurement"),
		}
		lines = append(lines, s.Card.Width(innerWidth-2).Render(strings.Join(commandLines, "\n")))
	}

	lines = append(lines, "")
	lines = append(lines, rule)

	content := strings.Join(lines, "\n")
	return s.Border.Width(width - 2).Render(content)
}

// configPath returns the resolved config file path.
func configPath() string {
	if p := os.Getenv("SLIMFERENCE_CONFIG"); p != "" {
		return p
	}
	if p := os.Getenv("XDG_CONFIG_HOME"); p != "" {
		return filepath.Join(p, "slimference", "config.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "slimference", "config.toml")
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
	if t.IsZero() {
		return "never"
	}
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

func formatStatusTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return formatAgo(t)
}

func fallbackLabel(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func formatBytesCompact(n int64) string {
	if n < 0 {
		n = 0
	}
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
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

// renderHeader renders the quiet product title bar.
func (m *Model) renderHeader(innerWidth int) string {
	s := m.styles
	title := s.Title.Render("SLIMFERENCE v" + Version)
	bypassBadge := ""
	if m.proxy.Bypass() {
		bypassBadge = s.MenuWarn.Render("⚠ BYPASS")
	}
	right := bypassBadge
	pad := innerWidth - lipgloss.Width(title) - lipgloss.Width(right) - 1
	if pad < 1 {
		pad = 1
	}
	return " " + title + strings.Repeat(" ", pad) + right
}

// buildLeftPanel builds the daily launch menu padded to width.
func (m *Model) buildLeftPanel(width int) []string {
	s := m.styles

	pad := func(str string) string {
		w := lipgloss.Width(str)
		if w >= width {
			return str
		}
		return str + strings.Repeat(" ", width-w)
	}

	var lines []string
	add := func(str string) { lines = append(lines, pad(str)) }

	actions := m.dashboardActions()
	add(" " + s.PanelTitle.Render("MENU"))
	for i, action := range actions {
		add(" " + renderMenuRow(s, width-2, m.mainCursor == i, action.label, action.state))
	}

	return lines
}

func renderProductRouteLine(s Styles, product ProductPanel) string {
	switch product.RouteState {
	case "attention":
		return s.Warning.Render("● ATTENTION") + "  " + s.Dim.Render(product.RouteLine)
	case "saving":
		return s.Saved.Render("● SAVING") + "  " + s.Dim.Render(product.RouteLine)
	case "active":
		return s.Highlight.Render("● ACTIVE") + "  " + s.Dim.Render(product.RouteLine+" · no savings yet")
	default:
		return s.Muted.Render("○ IDLE") + "  " + s.Dim.Render(product.RouteLine)
	}
}

func productRouteDetail(product ProductStatus) string {
	route := fallbackLabel(product.RouteStatus, "direct")
	if product.FallbackReason != "" {
		route += " · fallback: " + product.FallbackReason
	}
	if product.RecertStatus != "" {
		route += " · recert " + product.RecertStatus
	}
	return route
}

func productToolPruneLine(product ProductStatus) string {
	if product.ToolPruneTokensSaved <= 0 &&
		product.ToolPrunePrunedTools <= 0 &&
		product.ToolPruneReattached <= 0 &&
		product.ToolPruneMisses <= 0 &&
		product.ToolPruneRetries <= 0 {
		return ""
	}
	parts := []string{"tool-prune " + formatTokens(int(product.ToolPruneTokensSaved)) + " input saved"}
	if product.ToolPrunePrunedTools > 0 {
		parts = append(parts, fmt.Sprintf("%d pruned", product.ToolPrunePrunedTools))
	}
	if product.ToolPruneReattached > 0 {
		parts = append(parts, fmt.Sprintf("%d reattach", product.ToolPruneReattached))
	}
	if product.ToolPruneMisses > 0 {
		parts = append(parts, fmt.Sprintf("%d miss", product.ToolPruneMisses))
	}
	if product.ToolPruneRetries > 0 {
		parts = append(parts, fmt.Sprintf("%d retry", product.ToolPruneRetries))
	}
	return strings.Join(parts, " · ")
}

func productOutputReduceLine(product ProductStatus) string {
	if product.OutputReduceInjectedTurns <= 0 &&
		product.OutputReduceObservedTokens <= 0 &&
		product.OutputReduceInputOverhead <= 0 {
		return ""
	}
	parts := []string{fmt.Sprintf("output-reduce %d inj", product.OutputReduceInjectedTurns)}
	if product.OutputReduceObservedTokens > 0 {
		parts = append(parts, formatTokens(int(product.OutputReduceObservedTokens))+" out")
	} else if product.OutputReduceInjectedTurns > 0 {
		parts = append(parts, "proof pending")
	}
	if product.OutputReduceInputOverhead > 0 {
		parts = append(parts, "+"+formatTokens(int(product.OutputReduceInputOverhead))+" input")
	}
	return strings.Join(parts, " · ")
}

func productSafetyLine(product ProductStatus) string {
	parts := make([]string, 0, 4)
	if product.SafetyIssues > 0 {
		parts = append(parts, fmt.Sprintf("%d safety issue(s)", product.SafetyIssues))
	}
	if product.ToolResolutionMisses > 0 {
		parts = append(parts, fmt.Sprintf("%d tool miss(es)", product.ToolResolutionMisses))
	}
	if product.WSSParseFailures > 0 {
		parts = append(parts, fmt.Sprintf("%d WSS parse failure(s)", product.WSSParseFailures))
	}
	if product.WSSDegradedSessions > 0 {
		parts = append(parts, fmt.Sprintf("%d WSS degraded session(s)", product.WSSDegradedSessions))
	}
	if product.WSSCompressionErrors > 0 {
		parts = append(parts, fmt.Sprintf("%d WSS compression error(s)", product.WSSCompressionErrors))
	}
	if product.HostBudgetExceeded || product.HostBudgetStatus == "unknown" {
		reason := product.HostBudgetStatus
		if len(product.HostBudgetReasons) > 0 {
			reason = strings.Join(product.HostBudgetReasons, ",")
		}
		parts = append(parts, "host budget "+fallbackLabel(reason, "attention"))
	}
	if len(parts) == 0 {
		return "safety ok"
	}
	return strings.Join(parts, " · ")
}

func formatFloatCompact(v float64) string {
	switch {
	case v >= 1_000_000:
		return fmt.Sprintf("%.1fM", v/1_000_000)
	case v >= 1_000:
		return fmt.Sprintf("%.1fK", v/1_000)
	default:
		return fmt.Sprintf("%.1f", v)
	}
}

func providerFlowLine(s Styles, label string, stats analytics.ProviderStats, latencyMs float64) string {
	state := fmt.Sprintf("%d req · %s saved", stats.Messages, formatTokens(stats.InputTokensSaved))
	if latencyMs > 0 {
		state += fmt.Sprintf(" · %.0fms", latencyMs)
	}
	return " " + padRight(label, 12) + "  " + s.Muted.Render(state)
}

func renderOptimizationLine(s Styles, label string, enabled bool, saved int, extra string) string {
	state := s.OffBadge.Render("OFF")
	dot := s.DotOff.Render("○")
	if enabled {
		state = s.OnBadge.Render("ON ")
		dot = s.Dot.Render("●")
	}
	savedStr := ""
	if saved > 0 {
		savedStr = "  " + s.Saved.Render(formatTokens(saved)+" saved")
	}
	extraStr := ""
	if extra != "" {
		extraStr = "  " + s.Dim.Render(extra)
	}
	return "  " + dot + " " + s.Normal.Render(label) + " [" + state + "]" + savedStr + extraStr
}

func renderTransparentStatusLine(s Styles, status TransparentStatus) string {
	switch {
	case status.ProxyArmed && status.DaemonReachable:
		return "  " + s.Saved.Render("● GLOBAL LAB") + fmt.Sprintf("  armed on %d service(s), daemon reachable", status.ActiveServices)
	case status.ProxyArmed:
		return "  " + s.LogError.Render("● GLOBAL LAB") + fmt.Sprintf("  armed on %d service(s), daemon unreachable", status.ActiveServices)
	case status.Installed():
		return "  " + s.Saved.Render("● GLOBAL LAB") + "  assets installed, currently disarmed"
	case status.CAExists || status.CATrusted || status.AutoStartInstalled:
		return "  " + s.BannerWarn.Render("● GLOBAL LAB") + "  partially installed"
	case status.NetworkUnavailable:
		return "  " + s.LogError.Render("● GLOBAL LAB") + "  networksetup unavailable"
	default:
		return "  " + s.Muted.Render("○ GLOBAL LAB") + "  not installed"
	}
}

func renderCAStatusLine(s Styles, status TransparentStatus) string {
	switch {
	case !status.CAExists:
		return "  " + s.BannerWarn.Render("● CA MATERIAL") + "  missing; run install before Desktop diagnostics"
	case status.CATrusted:
		return "  " + s.Saved.Render("● CA MATERIAL") + "  ready; Keychain trusted for Desktop/Lab fallback"
	default:
		return "  " + s.Saved.Render("● CA MATERIAL") + "  ready; CLI WSS and Desktop --with-ca-env do not need Keychain trust"
	}
}

func renderCodexRouteStatusLine(s Styles, status CodexRouteStatus) string {
	mode := status.Transport
	if mode == "" {
		mode = status.AutoTransport
	}
	if mode == "" {
		mode = "auto"
	}
	modeText := " · " + mode
	stateText := "advanced shared route ready"
	switch {
	case status.WSSCertified && (status.AutoMode == "wss_phasef" || (status.AutoMode == "" && mode == "wss")):
		stateText = "WSS savings active"
		modeText += " · savings proof green"
	case status.WSSBridgeAvailable && status.AutoMode == "wss_bridge":
		stateText = "WSS native bridge"
		modeText += " · no mutation until repair"
	case status.NeedsRecert:
		stateText = "WSS repair needed"
		modeText += " · repair needed"
	}
	switch {
	case status.Complete && status.DaemonReachable:
		return "  " + s.Saved.Render("● ADVANCED ROUTE") + "  " + stateText + s.Dim.Render(modeText)
	case status.Enabled && !status.DaemonReachable:
		return "  " + s.LogError.Render("● ADVANCED ROUTE") + "  configured but daemon unreachable; press [r] for normal direct Codex"
	case status.Enabled && status.Conflict != "":
		return "  " + s.BannerWarn.Render("● ADVANCED ROUTE") + "  configured with conflict: " + status.Conflict
	case status.Enabled:
		return "  " + s.BannerWarn.Render("● ADVANCED ROUTE") + "  configured but incomplete" + s.Dim.Render(modeText)
	case status.Exists:
		suffix := ""
		if status.FallbackReason != "" {
			suffix = " · auto " + status.AutoTransport + " · " + status.FallbackReason
		}
		return "  " + s.Muted.Render("○ NORMAL CODEX") + "  direct; advanced shared route off" + s.Dim.Render(suffix)
	default:
		return "  " + s.Muted.Render("○ NORMAL CODEX") + "  direct; Codex config not found"
	}
}

func renderCodexDesktopStatusLine(s Styles, status CodexDesktopStatus) string {
	state := "not proven"
	style := s.Muted
	switch {
	case status.AppServerActive:
		state = "scoped app active"
		style = s.Saved
	case status.Mode == "desktop_app_server_phasef_proven" || status.Mode == "desktop_app_server_proven":
		state = "savings active"
		style = s.Saved
	case status.Mode == "desktop_app_server_route_ready":
		state = "route ready"
		style = s.Saved
	case status.Mode == "desktop_wss_bridge_only":
		state = "safe fallback"
		style = s.BannerWarn
	case status.FailureClass != "":
		state = "blocked: " + status.FailureClass
		style = s.LogError
	case status.Mode != "":
		state = "proof needed"
		style = s.BannerWarn
	}
	detail := status.Mode
	if status.AppServerActive {
		detail = "Codex.app -> slimference app-server"
	}
	if detail == "" {
		detail = status.Detail
	}
	if detail != "" {
		detail = " · " + detail
	}
	return "  " + style.Render("● DESKTOP") + "  " + state + s.Dim.Render(detail)
}

func extendLines(lines []string, target int, filler string) []string {
	missing := target - len(lines)
	if missing <= 0 {
		return lines
	}
	for i := 0; i < missing; i++ {
		lines = append(lines, filler)
	}
	return lines
}

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
	flights := m.proxy.GetRecentFlights(32)
	flightOriginal, flightFinal, flightSaved, flightCached := aggregateFlightTokens(flights)
	original := snap.TotalInputTokens
	saved := snap.SavedInputTokens
	final := original - saved
	if original == 0 && flightOriginal > 0 {
		original = flightOriginal
		final = flightFinal
		saved = flightSaved
	}
	if saved == 0 && flightSaved > 0 {
		saved = flightSaved
	}
	if saved == 0 && m.latestProduct.BillableInputTokensSaved > 0 {
		saved = int(m.latestProduct.BillableInputTokensSaved)
	}
	if final < 0 {
		final = 0
	}
	ratio := savingsPercent(saved, original)

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

	appendCard("TOTAL", []string{
		" " + s.BigSaved.Render(fmt.Sprintf("%d%%", ratio)) + " " + s.Dim.Render("input saved"),
		"",
		" " + renderKPIRow(s,
			fmt.Sprintf("would be %s", formatTokens(original)),
			fmt.Sprintf("sent %s", formatTokens(final)),
			fmt.Sprintf("saved %s", formatTokens(saved)),
		),
		" " + s.Muted.Render(fmt.Sprintf("%d tracked request(s) · %s output tokens", max(snap.TotalRequests, len(flights)), formatTokens(snap.TotalOutputTokens))),
	})

	appendCard("SESSIONS", renderSavingsSessionLines(s, summarizeFlightSessions(flights, 8)))

	cacheLine := fmt.Sprintf("%s provider cache read", formatTokens(max(snap.PromptCacheReadTokens, flightCached)))
	readCache := m.proxy.GetReadCacheStatus()
	appendCard("CACHE", []string{
		" " + s.Normal.Render(cacheLine),
		" " + s.Normal.Render(fmt.Sprintf("%d local cache hit(s)", snap.CacheHits)),
		" " + s.Muted.Render(fmt.Sprintf("%d read-cache decision(s), %d hit(s)", readCache.Blocks+readCache.Allows, readCache.Blocks)),
	})

	safety := productSafetyLine(m.latestProduct)
	safetyStyle := s.Saved
	if safety != "safety ok" {
		safetyStyle = s.BannerWarn
	}
	appendCard("SAFETY", []string{
		" " + safetyStyle.Render(safety),
		" " + s.Muted.Render(fmt.Sprintf("%d error(s), %d auto-retry(s), %d secret(s) redacted", snap.Errors, snap.AutoRetries, snap.SecretsRedacted)),
	})

	lines = append(lines, rule)

	content := strings.Join(lines, "\n")
	return s.Border.Width(width - 2).Render(content)
}

type tuiSessionSavings struct {
	ID       string
	Client   string
	Route    string
	Requests int
	Original int
	Final    int
	Saved    int
	Cached   int
	LastSeen time.Time
}

func aggregateFlightTokens(flights []dbg.FlightRequestSummary) (original int, final int, saved int, cached int) {
	for _, flight := range flights {
		orig, fin, save, cache := flightTokenTotals(flight)
		original += orig
		final += fin
		saved += save
		cached += cache
	}
	if final == 0 && original > 0 {
		final = original - saved
		if final < 0 {
			final = 0
		}
	}
	return original, final, saved, cached
}

func summarizeFlightSessions(flights []dbg.FlightRequestSummary, limit int) []tuiSessionSavings {
	if limit <= 0 {
		return nil
	}
	byID := map[string]*tuiSessionSavings{}
	order := make([]string, 0, len(flights))
	for _, flight := range flights {
		id := firstNonEmpty(flight.SessionID, flight.RequestID, "session")
		if _, ok := byID[id]; !ok {
			byID[id] = &tuiSessionSavings{
				ID:     id,
				Client: userClientLabel(flight),
				Route:  userRouteLabel(flight),
			}
			order = append(order, id)
		}
		row := byID[id]
		orig, final, saved, cached := flightTokenTotals(flight)
		row.Requests++
		row.Original += orig
		row.Final += final
		row.Saved += saved
		row.Cached += cached
		if ts := flightTimestamp(flight); !ts.IsZero() && ts.After(row.LastSeen) {
			row.LastSeen = ts
		}
	}
	rows := make([]tuiSessionSavings, 0, len(order))
	for _, id := range order {
		rows = append(rows, *byID[id])
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].LastSeen.After(rows[j].LastSeen)
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

func renderSavingsSessionLines(s Styles, sessions []tuiSessionSavings) []string {
	if len(sessions) == 0 {
		return []string{" " + s.Muted.Render("No Slimference session data yet.")}
	}
	lines := make([]string, 0, len(sessions)*2)
	for _, session := range sessions {
		state := "RECENT"
		if !session.LastSeen.IsZero() && time.Since(session.LastSeen) < 15*time.Minute {
			state = "ACTIVE"
		}
		stateStyle := s.Muted
		if state == "ACTIVE" {
			stateStyle = s.Saved
		}
		ratio := savingsPercent(session.Saved, session.Original)
		lines = append(lines,
			" "+stateStyle.Render(state)+"  "+s.Normal.Render(session.Client)+
				s.Muted.Render(" · "+session.Route+" · "+compactDebugLabel(session.ID, 18)),
		)
		lines = append(lines,
			"   "+s.Muted.Render(fmt.Sprintf("%d req · %s -> %s · %s saved (%d%%)",
				session.Requests,
				formatTokens(session.Original),
				formatTokens(session.Final),
				formatTokens(session.Saved),
				ratio,
			)),
		)
	}
	return lines
}

func flightTokenTotals(flight dbg.FlightRequestSummary) (original int, final int, saved int, cached int) {
	tokens := flight.TokenAccounting
	original = tokens.EstimatedOriginalInputTokens
	final = tokens.EstimatedFinalInputTokens
	saved = tokens.BillableSavingsEstimate
	cached = flightCached(flight)
	if original == 0 && final > 0 {
		original = final + saved
	}
	if final == 0 && original > 0 {
		final = original - saved
		if final < 0 {
			final = 0
		}
	}
	return original, final, saved, cached
}

func savingsPercent(saved int, original int) int {
	if original <= 0 || saved <= 0 {
		return 0
	}
	return int(float64(saved) / float64(original) * 100)
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
	} else {
		daemonLines = append(daemonLines, "", "  "+s.Muted.Render("○ SERVICE ADAPTER")+"  unavailable", "  "+s.Muted.Render(fmt.Sprintf("listen port :%d", m.proxy.Config().GetListenPort())))
	}
	lines = append(lines, s.Card.Width(innerWidth-2).Render(strings.Join(daemonLines, "\n")))
	lines = append(lines, "")

	transparent := TransparentStatus{}
	if m.svc != nil {
		transparent = m.transparentStatus
	}
	installLines := []string{
		" " + s.PanelTitle.Render("INSTALL"),
		"",
		renderStatusInstallLine(s, "Codex CLI", statusCLIReady(m.codexRouteStatus), statusCLIDetail(m.codexRouteStatus)),
		renderStatusInstallLine(s, "Codex App", statusDesktopReady(m.codexDesktopStatus), statusDesktopDetail(m.codexDesktopStatus)),
		renderStatusInstallLine(s, "Local CA", transparent.CAExists, statusCADetail(transparent)),
		renderStatusInstallLine(s, "Autostart", transparent.AutoStartInstalled, statusAutostartDetail(transparent)),
	}
	lines = append(lines, s.Card.Width(innerWidth-2).Render(strings.Join(installLines, "\n")))
	lines = append(lines, "")

	flights := m.proxy.GetRecentFlights(8)
	usageLines := []string{
		" " + s.PanelTitle.Render("USING NOW"),
		"",
	}
	if len(flights) == 0 {
		usageLines = append(usageLines, "  "+s.Muted.Render("No active Slimference traffic seen yet."))
	} else {
		activityFlights := buildActivityFlightViews(flights)
		latest := activityFlights[len(activityFlights)-1]
		usageLines = append(usageLines,
			"  "+s.Saved.Render("● ROUTED")+"  "+activityFlightHeadline(latest),
			"  "+s.Muted.Render(fmt.Sprintf("%d recent routed request(s)", len(flights))),
		)
	}
	lines = append(lines, s.Card.Width(innerWidth-2).Render(strings.Join(usageLines, "\n")))
	lines = append(lines, "")

	healthLines := []string{
		" " + s.PanelTitle.Render("HEALTH"),
		"",
		"  " + s.Normal.Render(productSafetyLine(m.latestProduct)),
	}
	if m.latestProduct.HostBudgetExceeded {
		healthLines = append(healthLines, "  "+s.LogError.Render("● HOST BUDGET")+"  exceeded")
	}
	if transparent.ProxyArmed {
		healthLines = append(healthLines, "  "+s.LogError.Render("● LAB ROUTE")+"  armed globally")
	}
	if notice := m.svcNotice(); notice != "" {
		healthLines = append(healthLines, "  "+s.BannerWarn.Render("● NOTICE")+"  "+notice)
	}
	lines = append(lines, s.Card.Width(innerWidth-2).Render(strings.Join(healthLines, "\n")))

	lines = append(lines, "")
	lines = append(lines, rule)

	content := strings.Join(lines, "\n")
	return s.Border.Width(width - 2).Render(content)
}

func (m *Model) svcNotice() string {
	if m.svc == nil {
		return ""
	}
	return m.svc.DaemonNotice()
}

func renderStatusInstallLine(s Styles, label string, ok bool, detail string) string {
	if ok {
		return "  " + s.Saved.Render("● READY") + "    " + label + formatStatusDetail(s, detail)
	}
	return "  " + s.BannerWarn.Render("● CHECK") + "    " + label + formatStatusDetail(s, detail)
}

func formatStatusDetail(s Styles, detail string) string {
	if detail == "" {
		return ""
	}
	return s.Dim.Render(" · " + detail)
}

func statusCLIReady(status CodexRouteStatus) bool {
	return status.WSSCertified || status.WSSBridgeAvailable || status.Complete || status.Exists
}

func statusCLIDetail(status CodexRouteStatus) string {
	switch {
	case status.NeedsRecert:
		return "repair needed"
	case status.WSSCertified:
		return "WSS savings ready"
	case status.WSSBridgeAvailable:
		return "bridge ready"
	case status.Complete:
		return "launch ready"
	case status.Exists:
		return "installed"
	case status.Conflict != "":
		return "conflict: " + status.Conflict
	default:
		return "run Setup"
	}
}

func statusDesktopReady(status CodexDesktopStatus) bool {
	return status.AppServerActive ||
		status.Mode == "desktop_app_server_phasef_proven" ||
		status.Mode == "desktop_app_server_proven" ||
		status.Mode == "desktop_app_server_route_ready"
}

func statusDesktopDetail(status CodexDesktopStatus) string {
	switch {
	case status.FailureClass != "":
		return status.FailureClass
	case status.AppServerActive:
		return "app launch active"
	case status.Mode == "desktop_app_server_phasef_proven" || status.Mode == "desktop_app_server_proven":
		return "savings ready"
	case status.Mode == "desktop_app_server_route_ready":
		return "launch ready"
	case status.Mode == "desktop_wss_bridge_only":
		return "bridge fallback"
	case status.Mode != "":
		return "proof pending"
	default:
		return "run Setup"
	}
}

func statusCADetail(status TransparentStatus) string {
	switch {
	case status.CATrusted:
		return "trusted"
	case status.CAExists:
		return "ready"
	default:
		return "run Setup"
	}
}

func statusAutostartDetail(status TransparentStatus) string {
	if status.AutoStartInstalled {
		return "installed"
	}
	return "run Setup"
}

// renderActivityView renders current Slimference activity only. Direct Codex
// processes and old hook diagnostics are intentionally not mixed into this view.
func (m *Model) renderActivityView() string {
	s := m.styles
	width := m.width
	if width < 60 {
		width = 80
	}

	innerWidth := width - 4
	rule := s.HorizRule.Render(strings.Repeat("─", innerWidth))

	var lines []string
	flights := m.proxy.GetRecentFlights(8)
	activityFlights := buildActivityFlightViews(flights)
	lines = append(lines, " "+s.Title.Render("SLIMFERENCE")+" "+s.Dim.Render("/ Activity"))
	lines = append(lines, rule)
	lines = append(lines, s.Card.Width(innerWidth-2).Render(renderCurrentActivity(m, activityFlights)))
	lines = append(lines, "")
	lines = append(lines, s.Card.Width(innerWidth-2).Render(renderTrafficActivity(m, activityFlights)))
	lines = append(lines, "")
	lines = append(lines, rule)

	content := strings.Join(lines, "\n")
	return s.Border.Width(width - 2).Render(content)
}

type activityFlightView struct {
	Flight    dbg.FlightRequestSummary
	Thread    codexThreadMetadata
	HasThread bool
}

func buildActivityFlightViews(flights []dbg.FlightRequestSummary) []activityFlightView {
	threads := lookupCodexThreadMetadataForFlights(flights)
	items := make([]activityFlightView, 0, len(flights))
	for _, flight := range flights {
		id := normalizeCodexSessionID(flight.SessionID)
		thread, ok := threads[id]
		items = append(items, activityFlightView{Flight: flight, Thread: thread, HasThread: ok})
	}
	return items
}

func renderCurrentActivity(m *Model, flights []activityFlightView) string {
	s := m.styles
	body := []string{" " + s.PanelTitle.Render("NOW")}
	if len(flights) > 0 {
		flight := flights[len(flights)-1]
		body = append(body, "",
			"  "+s.Saved.Render("● ROUTED")+"  "+activityFlightHeadline(flight),
			"  "+s.Muted.Render(activityFlightDetail(flight)),
		)
		return strings.Join(body, "\n")
	}
	if m.lastLaunchLabel != "" {
		body = append(body, "",
			"  "+s.Highlight.Render("● LAUNCHED")+"  "+m.lastLaunchLabel+" via Slimference",
			"  "+s.Muted.Render("waiting for first routed request · "+formatStatusTime(m.lastLaunchAt)),
		)
		return strings.Join(body, "\n")
	}
	if m.codexDesktopStatus.AppServerActive {
		body = append(body, "",
			"  "+s.Highlight.Render("● DESKTOP")+"  Codex App app-server active",
			"  "+s.Muted.Render("waiting for first routed request"),
		)
		return strings.Join(body, "\n")
	}
	body = append(body, "",
		"  "+s.Muted.Render("○ IDLE")+"  no Slimference-routed traffic detected",
		"  "+s.Muted.Render("direct Codex windows are hidden here"),
	)
	return strings.Join(body, "\n")
}

func activityFlightHeadline(flight activityFlightView) string {
	client := activityClientLabel(flight)
	title := activitySessionTitle(flight)
	saved := flight.Flight.TokenAccounting.BillableSavingsEstimate
	if title != "" {
		if saved > 0 {
			return fmt.Sprintf("%s · %s · %s saved", client, compactDebugLabel(title, 58), formatTokens(saved))
		}
		return fmt.Sprintf("%s · %s · routed", client, compactDebugLabel(title, 58))
	}
	if saved > 0 {
		return fmt.Sprintf("%s · %s saved", client, formatTokens(saved))
	}
	return fmt.Sprintf("%s · routed", client)
}

func activityFlightDetail(flight activityFlightView) string {
	parts := make([]string, 0, 5)
	if flight.HasThread && strings.TrimSpace(flight.Thread.CWD) != "" {
		parts = append(parts, compactUserPath(flight.Thread.CWD))
	}
	if flight.HasThread && strings.TrimSpace(flight.Thread.Model) != "" {
		parts = append(parts, strings.TrimSpace(flight.Thread.Model))
	}
	parts = append(parts, userRouteLabel(flight.Flight))
	parts = append(parts, "session "+compactDebugLabel(firstNonEmpty(flight.Flight.SessionID, flight.Flight.RequestID, "unknown"), 18))
	if ts := activityTimestamp(flight); !ts.IsZero() {
		parts = append(parts, formatStatusTime(ts))
	}
	_, _, _, cached := flightTokenTotals(flight.Flight)
	if cached > 0 {
		parts = append(parts, formatTokens(cached)+" provider-cache read")
	}
	return strings.Join(parts, " · ")
}

func renderTrafficActivity(m *Model, flights []activityFlightView) string {
	s := m.styles
	body := []string{" " + s.PanelTitle.Render("RECENT ROUTES")}
	if len(flights) == 0 {
		body = append(body, "", "  "+s.Muted.Render("No routed Slimference traffic yet."))
		return strings.Join(body, "\n")
	}
	start := 0
	if len(flights) > 4 {
		start = len(flights) - 4
	}
	for _, flight := range flights[start:] {
		saved := flight.Flight.TokenAccounting.BillableSavingsEstimate
		savedLabel := s.Muted.Render("0 saved")
		if saved > 0 {
			savedLabel = s.Saved.Render(fmt.Sprintf("%d saved", saved))
		}
		body = append(body, "", "  "+fmt.Sprintf("%s  %s  %s",
			s.Highlight.Render(activityClientLabel(flight)),
			compactDebugLabel(firstNonEmpty(activitySessionTitle(flight), userRouteLabel(flight.Flight)), 42),
			savedLabel,
		))
		body = append(body, "  "+s.Muted.Render(activityFlightDetail(flight)))
	}
	return strings.Join(body, "\n")
}

func activityClientLabel(flight activityFlightView) string {
	if flight.HasThread {
		value := strings.ToLower(firstNonEmpty(flight.Thread.Source, flight.Thread.ThreadSource))
		switch {
		case strings.Contains(value, "cli"):
			return "Codex CLI"
		case strings.Contains(value, "desktop"), strings.Contains(value, "app"), strings.Contains(value, "chatgpt"):
			return "Codex App"
		}
	}
	return userClientLabel(flight.Flight)
}

func activitySessionTitle(flight activityFlightView) string {
	if !flight.HasThread {
		return ""
	}
	title := strings.TrimSpace(flight.Thread.Title)
	title = strings.TrimLeft(title, "›> \t")
	if title != "" {
		return title
	}
	return compactUserPath(flight.Thread.CWD)
}

func activityTimestamp(flight activityFlightView) time.Time {
	if flight.HasThread && !flight.Thread.UpdatedAt.IsZero() {
		return flight.Thread.UpdatedAt
	}
	return flightTimestamp(flight.Flight)
}

func userClientLabel(flight dbg.FlightRequestSummary) string {
	value := strings.ToLower(firstNonEmpty(flight.ClientFamily, flight.Provider, flight.Source))
	switch {
	case strings.Contains(value, "cli"):
		return "Codex CLI"
	case strings.Contains(value, "desktop"), strings.Contains(value, "app"), strings.Contains(value, "chatgpt"):
		return "Codex App"
	case strings.Contains(value, "claude"):
		return "Claude Code"
	case strings.Contains(value, "codex"):
		return "Codex"
	default:
		return "Codex"
	}
}

func userRouteLabel(flight dbg.FlightRequestSummary) string {
	value := strings.ToLower(firstNonEmpty(flight.RouteMode, flight.Source))
	switch {
	case strings.Contains(value, "cache"):
		return "cache"
	case strings.Contains(value, "phasef"), strings.Contains(value, "websocket"), strings.Contains(value, "mitm"):
		return "Slimference route"
	case strings.Contains(value, "raw"), strings.Contains(value, "passthrough"), strings.Contains(value, "upstream"):
		return "safe fallback"
	default:
		return "Slimference route"
	}
}

func flightTimestamp(flight dbg.FlightRequestSummary) time.Time {
	if len(flight.Events) == 0 {
		return time.Time{}
	}
	return flight.Events[len(flight.Events)-1].Timestamp
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func compactUserPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if home, err := userHomeDirFn(); err == nil && home != "" {
		if value == home {
			return "~"
		}
		if strings.HasPrefix(value, home+"/") {
			return "~/" + strings.TrimPrefix(value, home+"/")
		}
	}
	return value
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

	if m.proxy.SessionLogger() != nil {
		entries := m.proxy.SessionLogger().Recent(30)
		if len(entries) == 0 {
			lines = append(lines, s.Card.Width(innerWidth-2).Render(strings.Join([]string{
				" " + s.PanelTitle.Render("RECENT EVENTS"),
				"",
				s.Muted.Render("  No log entries yet."),
			}, "\n")))
		} else {
			body := []string{
				" " + s.PanelTitle.Render("RECENT EVENTS"),
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
			" " + s.PanelTitle.Render("RECENT EVENTS"),
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
	body := []string{" " + s.PanelTitle.Render("ROUTES")}
	if len(flights) == 0 {
		body = append(body, "", s.Muted.Render("  No routed requests yet."))
		return strings.Join(body, "\n")
	}
	var saved, cached, output, fallbacks, safetyBlocks int
	slowest := 0.0
	for _, f := range flights {
		saved += flightSaved(f)
		cached += flightCached(f)
		output += flightOutput(f)
		if f.RouteMode == "raw_passthrough" || f.RouteMode == "local_cache" || f.BypassReason != "" {
			fallbacks++
		}
		if f.Plan != nil && f.Plan.SafetyBlocked {
			safetyBlocks++
		}
		if f.TotalProxyOverheadMs > slowest {
			slowest = f.TotalProxyOverheadMs
		}
	}
	body = append(body,
		"",
		" "+s.Normal.Render(fmt.Sprintf("%d request(s) · %s saved · %s cache · %s output", len(flights), formatTokens(saved), formatTokens(cached), formatTokens(output))),
		" "+s.Muted.Render(fmt.Sprintf("%d fallback(s) · %d safety block(s) · slowest %.1fms", fallbacks, safetyBlocks, slowest)),
		"",
	)
	for _, f := range flights {
		body = append(body, " "+s.Normal.Render(userClientLabel(f))+
			s.Muted.Render(" · "+userRouteLabel(f)+" · session "+compactDebugLabel(firstNonEmpty(f.SessionID, f.RequestID), 18))+
			s.Saved.Render("  "+formatTokens(flightSaved(f))+" saved"))
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
			s.BannerWarn.Render("CHECK") + " " + s.Normal.Render("Machine-wide route is active. Use CLI lab commands to disarm."),
		)
	} else if allReady {
		message := "Slimference is installed."
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
		stepLines := []string{" " + s.PanelTitle.Render("SETUP"), ""}
		for i, step := range steps {
			stepLines = append(stepLines, renderSetupStepRow(s, innerWidth-8, i, step.label, step.check(), m.setupCursor == i))
		}
		lines = append(lines, s.CardActive.Width(innerWidth-2).Render(strings.Join(stepLines, "\n")))
		lines = append(lines, "")

		serviceLines := []string{
			" " + s.PanelTitle.Render("DAEMON"),
			"",
		}
		running, pid, port := m.svc.DaemonStatus()
		if running {
			serviceLines = append(serviceLines, "  "+s.Saved.Render("● RUNNING")+"  PID "+fmt.Sprintf("%d  port :%d", pid, port))
		} else {
			serviceLines = append(serviceLines, "  "+s.Muted.Render("○ STOPPED")+"  daemon not running")
		}
		if notice := m.svc.DaemonNotice(); notice != "" {
			serviceLines = append(serviceLines, "  "+s.BannerWarn.Render("● OLD PROCESS")+"  "+notice)
		}
		if transparent.AutoStartInstalled {
			serviceLines = append(serviceLines, "  "+s.Saved.Render("● AUTOSTART")+"  installed")
		} else {
			serviceLines = append(serviceLines, "  "+s.BannerWarn.Render("● AUTOSTART")+"  missing")
		}
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
			" " + s.PanelTitle.Render("COMMANDS"),
			"",
			"  " + s.SetupCmd.Render("slimference install"),
			"  " + s.SetupCmd.Render("slimference codex run -- <prompt>") + s.Dim.Render(" # one-shot CLI"),
			"  " + s.SetupCmd.Render("slimference codex recertify wss --force") + s.Dim.Render(" # repair savings proof"),
			"  " + s.SetupCmd.Render("slimference codex status") + s.Dim.Render("         # Codex status"),
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
		return "  " + s.Muted.Render("○ DIRECT") + "  advanced shared route off" + s.Dim.Render(suffix)
	default:
		return "  " + s.Muted.Render("○ DIRECT") + "  Codex config not found"
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

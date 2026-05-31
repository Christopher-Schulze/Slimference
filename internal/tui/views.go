package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/slimference/slimference/internal/analytics"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/sessions"
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
	tabs := renderViewTabs(s, m.view)
	rule := s.HorizRule.Render(strings.Repeat("─", innerWidth))

	leftWidth := 36
	if innerWidth < 90 {
		leftWidth = 32
	}
	rightWidth := innerWidth - leftWidth - 1

	leftLines := m.buildLeftPanel(leftWidth)
	rightLines := m.buildRightPanel(rightWidth)

	h := max(len(leftLines), len(rightLines))
	emptyL := strings.Repeat(" ", leftWidth)
	emptyR := strings.Repeat(" ", rightWidth)
	leftLines = extendLines(leftLines, h, emptyL)
	rightLines = extendLines(rightLines, h, emptyR)

	div := s.Divider.Render("│")
	rows := make([]string, h)
	for i := range rows {
		rows[i] = leftLines[i] + div + rightLines[i]
	}

	flashLine := ""
	if m.flashMsg != "" && time.Now().Before(m.flashExpiry) {
		flashLine = "\n" + s.Flash.Render("  "+m.flashMsg)
	}

	content := header + "\n" + tabs + "\n" + rule + "\n" +
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
	ratio := 0
	if snap.TotalInputTokens > 0 {
		ratio = int((1 - float64(snap.TotalInputTokens-snap.SavedInputTokens)/float64(snap.TotalInputTokens)) * 100)
	}

	innerWidth := width - 4
	rule := s.HorizRule.Render(strings.Repeat("─", innerWidth))

	var lines []string

	lines = append(lines, " "+s.Title.Render("SLIMFERENCE")+" "+s.Dim.Render("/ Stats"))
	lines = append(lines, " "+renderViewTabs(s, m.view))
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
		{"Layer 1 (determ)", "-", "-", formatTokens(snap.Layer1Savings)},
		{"Layer 2 (semantic)", "-", "-", formatTokens(snap.Layer2Savings)},
		{"Layer 3 (cache)", "-", "-", formatTokens(snap.Layer3Savings)},
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
	if snap.MiniMaxAvgLatencyMs > 0 {
		latRows = append(latRows, []string{"Layer 2 (async)", fmt.Sprintf("%.0fms", snap.MiniMaxAvgLatencyMs), "-"})
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
	lines = append(lines, " "+s.Key.Render("[←/→]")+s.FooterDesc.Render(" switch view")+
		s.KeySep.Render(" · ")+s.Key.Render("[↑/↓]")+s.FooterDesc.Render(" browse cards")+
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
	lines = append(lines, " "+renderViewTabs(s, m.view))
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
				" " + renderShortcutRow(s, "select Export debug log and press Enter"),
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
	lines = append(lines, " "+s.Key.Render("[←/→]")+s.FooterDesc.Render(" switch view")+
		s.KeySep.Render(" · ")+s.Key.Render("[↑/↓]")+s.FooterDesc.Render(" select action")+
		s.KeySep.Render(" · ")+s.Key.Render("[enter]")+s.FooterDesc.Render(" export log")+
		s.KeySep.Render(" · ")+s.Key.Render("[q]")+s.FooterDesc.Render(" quit"))

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
	lines = append(lines, " "+renderViewTabs(s, m.view))
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
		message := "Slimference is installed. Use the scoped Codex route when you want CLI/App traffic in the pipeline."
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
		}
		if notice := m.svc.DaemonNotice(); notice != "" {
			serviceLines = append(serviceLines, "  "+s.BannerWarn.Render("● OLD PROCESS")+"  "+notice)
		}
		serviceLines = append(serviceLines, renderCAStatusLine(s, transparent))
		serviceLines = append(serviceLines, renderTransparentStatusLine(s, transparent))
		serviceLines = append(serviceLines, renderCodexRouteStatusLine(s, m.codexRouteStatus))
		serviceLines = append(serviceLines, "")
		serviceLines = append(serviceLines, "  "+s.Muted.Render("[r] enable/disable Codex Mode  [p] start/stop  [o] restart/repair daemon"))
		serviceLines = append(serviceLines, "  "+s.Muted.Render("[a] global lab controls  [u] uninstall Slimference assets"))
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
		// Layer 2 API key removed from required checklist (Phase H: not part of 2-surface install).
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
			"  " + s.SetupCmd.Render("slimference codex run --transport=wss -- <prompt>") + s.Dim.Render(" # WSS cert"),
			"  " + s.SetupCmd.Render("slimference codex recertify wss --force") + s.Dim.Render(" # repair WSS proof"),
			"  " + s.SetupCmd.Render("slimference enable") + s.Dim.Render("                  # CLI/App route"),
			"  " + s.SetupCmd.Render("slimference disable") + s.Dim.Render("                 # direct fallback"),
			"  " + s.SetupCmd.Render("slimference codex status") + s.Dim.Render("         # route status"),
			"  " + s.SetupCmd.Render("go run ./scripts/utils workday-savings start") + s.Dim.Render(" # begin measurement"),
		}
		lines = append(lines, s.Card.Width(innerWidth-2).Render(strings.Join(commandLines, "\n")))
	}

	lines = append(lines, "")
	lines = append(lines, rule)
	lines = append(lines, " "+s.Key.Render("[←/→]")+s.FooterDesc.Render(" switch view")+
		s.KeySep.Render(" · ")+s.Key.Render("[↑/↓]")+s.FooterDesc.Render(" move")+
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

// renderHeader renders the title bar with version, session duration and port.
// T67: inserts a "BYPASS" badge when the master bypass is active so the
// operator never forgets that compression is off.
func (m *Model) renderHeader(innerWidth int) string {
	s := m.styles
	title := s.Title.Render("SLIMFERENCE v" + Version)
	status := s.MenuMeta.Render("monitor")
	if m.svc != nil {
		running, pid, port := m.svc.DaemonStatus()
		if running {
			status = s.MenuOn.Render(fmt.Sprintf("daemon live · PID %d · :%d", pid, port))
		} else {
			status = s.MenuWarn.Render(fmt.Sprintf("daemon idle · :%d", m.proxy.Config().GetListenPort()))
		}
	}
	bypassBadge := ""
	if m.proxy.Bypass() {
		bypassBadge = s.MenuWarn.Render("⚠ BYPASS ") + "  "
	}
	right := bypassBadge + status + "  " +
		s.Muted.Render(fmt.Sprintf(":%d", m.proxy.Config().GetListenPort())) +
		"  " + s.Dim.Render("◷ "+renderSessionDuration(m.sessionStart))
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
		k("←/→", "views"),
		k("↑/↓", "select"),
		k("enter", "apply"),
		k("a", "apps"),
		k("s", "stats"),
		k("i", "setup"),
		k("b", "bypass"),
		k("q", "quit"),
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

	allReady := m.hookStatus.Codex
	if allReady {
		add(" " + s.Saved.Render("● READY") + "  " + s.Dim.Render("Codex hook installed"))
	} else {
		missing := []string{}
		if !m.hookStatus.Codex {
			missing = append(missing, "Codex")
		}
		add(" " + s.Warning.Render("● SETUP") + "  " + s.Dim.Render("missing: "+strings.Join(missing, ", ")))
		add(" " + s.Dim.Render("  open Setup with ←/→ to complete installation"))
	}

	// Phase H — transparent MITM arm/disarm tile. Renders prominently
	// so the user always sees whether traffic is being intercepted.
	if m.transparentStatus.ProxyArmed {
		add(" " + s.Saved.Render("● MITM ARMED") + "  " + s.Dim.Render("intercepting Codex"))
	} else {
		add(" " + s.Muted.Render("○ MITM DISARMED") + "  " + s.Dim.Render("press [i] then [a] to arm"))
	}
	if m.proxy.Bypass() {
		add(" " + s.Warning.Render("● BYPASS") + "  " + s.Dim.Render("all traffic passes through unmodified (press [b])"))
	}
	add("")

	actions := m.dashboardActions()
	add(" " + s.PanelTitle.Render("LAUNCH CENTER"))
	currentGroup := ""
	for i, action := range actions {
		if action.group != currentGroup {
			if currentGroup != "" {
				add("")
			}
			currentGroup = action.group
			add(" " + s.MenuGroup.Render(strings.ToUpper(currentGroup)))
		}
		add(" " + renderMenuRow(s, width-2, m.mainCursor == i, action.label, action.state))
	}
	add("")

	if len(actions) > 0 {
		action := actions[clampIndex(m.mainCursor, len(actions))]
		add(" " + s.PanelTitle.Render("SELECTED ACTION"))
		add(" " + s.MetricVal.Render(action.label))
		add(" " + s.Muted.Render(action.description))
		add("")
	}

	codexHealth := m.proxy.GetProviderHealth(types.OpenAI)
	add(" " + s.PanelTitle.Render("AGENT HEALTH"))
	add(" " + renderProviderBadge(s, "Codex CLI / Desktop", m.codexEnabled) + "  " + renderHealthDot(s, codexHealth.Status))
	add(" " + s.Muted.Render("○ Claude Code  [OFF]  opt-in later"))
	add("")

	l2Status := m.proxy.GetLayer2Status()
	add(" " + s.PanelTitle.Render("BACKGROUND"))
	add(renderLayerLine(s, 1, "Deterministic", m.layer1Enabled, snap.Layer1Savings, ""))
	add(renderLayerLine(s, 2, "Semantic", m.layer2Enabled, snap.Layer2Savings, l2Summary(l2Status)))
	add(renderLayerLine(s, 3, "Cache", m.layer3Enabled, snap.Layer3Savings, fmt.Sprintf("hits: %d/%d", snap.CacheHits, snap.TotalRequests)))
	add("")

	readCache := m.proxy.GetReadCacheStatus()
	if readCache.Evaluations > 0 || readCache.TrackedFiles > 0 {
		add(" " + s.PanelTitle.Render("READ CACHE"))
		add(" " + s.Saved.Render(fmt.Sprintf("%d blocks", readCache.Blocks)) +
			"  " + s.Dim.Render(fmt.Sprintf("%d unchanged · %d delta", readCache.UnchangedBlocks, readCache.DeltaBlocks)))
		add(" " + s.Muted.Render(fmt.Sprintf("%d evals · %d files · %d sessions", readCache.Evaluations, readCache.TrackedFiles, readCache.Sessions)))
		add("")
	}

	checkpoints := m.proxy.GetCheckpointStatus()
	if checkpoints.Captures > 0 {
		add(" " + s.PanelTitle.Render("CHECKPOINTS"))
		add(" " + s.Saved.Render(fmt.Sprintf("%d captures", checkpoints.Captures)) +
			"  " + s.Dim.Render(fmt.Sprintf("%d restores · %s", checkpoints.Restores, fallbackLabel(checkpoints.LastTrigger, "manual"))))
		add(" " + s.Muted.Render(fmt.Sprintf("last: %s · %s stored", formatStatusTime(checkpoints.LastCapture), formatBytesCompact(checkpoints.Bytes))))
		add("")
	}

	archive := m.proxy.GetToolArchiveStatus()
	if archive.Archived > 0 {
		add(" " + s.PanelTitle.Render("TOOL ARCHIVE"))
		add(" " + s.Saved.Render(fmt.Sprintf("%d archived", archive.Archived)) +
			"  " + s.Dim.Render(fmt.Sprintf("%d expands", archive.Expanded)))
		add(" " + s.Muted.Render(fmt.Sprintf("%d entries · %s -> %s", archive.Count, formatBytesCompact(archive.BytesRaw), formatBytesCompact(archive.BytesStored))))
		add("")
	}

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
	product := m.proxy.GetProductStatus()

	pad := func(str string) string {
		w := lipgloss.Width(str)
		if w >= width {
			return str
		}
		return str + strings.Repeat(" ", width-w)
	}

	var lines []string
	add := func(str string) { lines = append(lines, pad(str)) }

	add(" " + s.PanelTitle.Render("PRODUCT"))
	if product.RouteStatus == "" && product.SavingsStatus == "" {
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
	} else {
		add(" " + renderProductRouteLine(s, product))
		add(" " + s.Saved.Render(formatTokens(int(product.BillableInputTokensSaved))+" input saved") +
			"  " + s.Dim.Render(formatBytesCompact(product.OutputWireBytesSaved)+" wire"))
		if product.ProviderCacheReadTokens > 0 || product.ProviderCacheCreateTokens > 0 {
			add(" " + s.Highlight.Render(formatTokens(int(product.ProviderCacheReadTokens))+" provider-cache read") +
				"  " + s.Dim.Render(formatTokens(int(product.ProviderCacheCreateTokens))+" create"))
		}
		add(" " + s.Muted.Render(fmt.Sprintf("cache %d/%d · read %d · repeated %d · chunk %d",
			product.CacheHits, product.CacheHits+product.CacheMisses,
			product.ReadDeltaHits, product.RepeatedOutputHits, product.ChunkDedupHits)))
		if product.SafetyIssues > 0 || product.HostBudgetExceeded {
			add(" " + s.Warning.Render(productSafetyLine(product)))
		} else {
			add(" " + s.Muted.Render("safety ok"))
		}
	}
	add("")

	duration := time.Since(snap.SessionStart)
	if duration < time.Second {
		duration = time.Second
	}
	tokenRateIn := float64(snap.TotalInputTokens) / duration.Seconds()
	tokenRateSaved := float64(snap.SavedInputTokens) / duration.Seconds()
	requestRate := float64(snap.TotalRequests) / duration.Minutes()
	add(" " + s.PanelTitle.Render("TRAFFIC"))
	add(renderMetricPair(s, "Requests/min", fmt.Sprintf("%.1f", requestRate), "Input/s", formatFloatCompact(tokenRateIn), width-2))
	add(renderMetricPair(s, "Saved/s", formatFloatCompact(tokenRateSaved), "Output", formatTokens(snap.TotalOutputTokens), width-2))

	if snap.SecretsRedacted > 0 {
		add(" " + s.Warning.Render(fmt.Sprintf("⚠  %d secrets redacted", snap.SecretsRedacted)))
	}
	if snap.AutoRetries > 0 {
		add(" " + s.Dim.Render(fmt.Sprintf("↺  %d auto-retries", snap.AutoRetries)))
	}
	if snap.PromptCacheReadTokens > 0 || snap.PromptCacheCreateTokens > 0 {
		add(" " + s.Highlight.Render(fmt.Sprintf("↻ prompt cache %.0f%%", snap.PromptCacheHitRate()*100)) +
			"  " + s.Dim.Render(fmt.Sprintf("%s read · %s create", formatTokens(snap.PromptCacheReadTokens), formatTokens(snap.PromptCacheCreateTokens))))
	}
	add("")

	add(" " + s.PanelTitle.Render("PROVIDER MAP"))
	add(providerFlowLine(s, "Codex", snap.PerProvider[types.OpenAI], snap.LatencyOpenAIMs))
	add("")

	// LIVE log or QUICK START
	if snap.TotalRequests == 0 && !m.hookStatus.Codex {
		add(" " + s.PanelTitle.Render("QUICK START"))
		add("")
		add(" " + s.Normal.Render("1. Install + arm:"))
		add("   " + s.SetupCmd.Render("$ slimference install"))
		add("   " + s.SetupCmd.Render("$ slimference enable"))
		add("")
		add(" " + s.Normal.Render("2. Start Codex CLI"))
		add(" " + s.Muted.Render("   Requests appear here automatically."))
		add("")
		add(" " + s.Dim.Render("   Press [a] for per-app routing toggles."))
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

func l2Summary(status Layer2Status) string {
	if status.Compressing {
		return "compressing now"
	}
	if status.HasCache {
		return fmt.Sprintf("last: %s · q:%d", formatAgo(status.LastRun), status.QueueDepth)
	}
	return "idle"
}

func renderProductRouteLine(s Styles, product ProductStatus) string {
	route := productRouteDetail(product)
	switch {
	case product.SafetyIssues > 0 || product.HostBudgetExceeded:
		return s.Warning.Render("● ATTENTION") + "  " + s.Dim.Render(route)
	case product.SavingsStatus == "saving":
		return s.Saved.Render("● SAVING") + "  " + s.Dim.Render(route)
	case product.SavingsStatus == "active_no_savings":
		return s.Highlight.Render("● ACTIVE") + "  " + s.Dim.Render(route+" · no savings yet")
	default:
		return s.Muted.Render("○ IDLE") + "  " + s.Dim.Render(route)
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

func productSafetyLine(product ProductStatus) string {
	parts := make([]string, 0, 4)
	if product.SafetyIssues > 0 {
		parts = append(parts, fmt.Sprintf("%d safety issue(s)", product.SafetyIssues))
	}
	if product.ToolResolutionMisses > 0 {
		parts = append(parts, fmt.Sprintf("%d tool miss(es)", product.ToolResolutionMisses))
	}
	if product.HostBudgetExceeded {
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
	stateText := "scoped CLI/App route ready"
	switch {
	case status.WSSCertified && (status.AutoMode == "wss_phasef" || (status.AutoMode == "" && mode == "wss")):
		stateText = "WSS savings active"
		modeText += " · Phase-F proven"
	case status.WSSBridgeAvailable && status.AutoMode == "wss_bridge":
		stateText = "WSS native bridge"
		modeText += " · no mutation until repair"
	case status.NeedsRecert:
		stateText = "WSS repair needed"
		modeText += " · repair needed"
	}
	switch {
	case status.Complete && status.DaemonReachable:
		return "  " + s.Saved.Render("● CODEX MODE") + "  " + stateText + s.Dim.Render(modeText)
	case status.Enabled && !status.DaemonReachable:
		return "  " + s.LogError.Render("● CODEX MODE") + "  configured but daemon unreachable; press [r] to disable"
	case status.Enabled && status.Conflict != "":
		return "  " + s.BannerWarn.Render("● CODEX MODE") + "  configured with conflict: " + status.Conflict
	case status.Enabled:
		return "  " + s.BannerWarn.Render("● CODEX MODE") + "  configured but incomplete" + s.Dim.Render(modeText)
	case status.Exists:
		suffix := ""
		if status.FallbackReason != "" {
			suffix = " · auto " + status.AutoTransport + " · " + status.FallbackReason
		}
		return "  " + s.Muted.Render("○ CODEX MODE") + "  disabled; press [r] to enable scoped CLI/App" + s.Dim.Render(suffix)
	default:
		return "  " + s.Muted.Render("○ CODEX MODE") + "  Codex config not found"
	}
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

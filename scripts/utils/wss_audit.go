package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	dbg "github.com/slimference/slimference/internal/debug"
)

type wssAuditReport struct {
	Path                   string                   `json:"path"`
	Requests               int                      `json:"requests"`
	WSSRequests            int                      `json:"wss_requests"`
	PhaseFRequests         int                      `json:"phasef_requests"`
	MissingSessionID       int                      `json:"missing_session_id"`
	PreviousResponseIDUsed int                      `json:"previous_response_id_used"`
	PositiveSavings        int                      `json:"positive_savings_requests"`
	TokensSaved            int                      `json:"tokens_saved"`
	RouteModes             map[string]int           `json:"route_modes,omitempty"`
	ContentClasses         map[string]int           `json:"content_classes,omitempty"`
	Sessions               []wssAuditSessionSummary `json:"sessions,omitempty"`
	Notes                  []string                 `json:"notes,omitempty"`
}

type wssAuditSessionSummary struct {
	SessionID              string    `json:"session_id"`
	Requests               int       `json:"requests"`
	PhaseFRequests         int       `json:"phasef_requests"`
	PreviousResponseIDUsed int       `json:"previous_response_id_used"`
	TokensSaved            int       `json:"tokens_saved"`
	FirstSeen              time.Time `json:"first_seen,omitempty"`
	LastSeen               time.Time `json:"last_seen,omitempty"`
}

const wssAuditHelpText = `wss-audit: inspect Codex WSS decisions without raw frame dumps

Usage:
  go run ./scripts/utils wss-audit <decisions.jsonl> [--json]

Reads content-free RequestSummary JSONL records and reports WSS route coverage,
Phase-F request counts, session-key continuity, previous_response_id usage, and
positive input-token savings. It does not inspect payload text or auth headers.`

func runWSSAudit(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprintln(stdout, wssAuditHelpText)
		return 0
	}
	outputFormat, rest, err := parseOutputFlag(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if len(rest) != 1 {
		fmt.Fprintln(stderr, "Usage: wss-audit <decisions.jsonl> [--json]")
		return 2
	}
	report, err := loadWSSAuditReport(rest[0])
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if outputFormat == outputJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	writeWSSAuditText(stdout, report)
	return 0
}

func loadWSSAuditReport(path string) (wssAuditReport, error) {
	summaries, err := dbg.ReplaySession(path)
	if err != nil {
		return wssAuditReport{}, fmt.Errorf("read decisions %s: %w", path, err)
	}
	report := wssAuditReport{
		Path:           path,
		Requests:       len(summaries),
		RouteModes:     make(map[string]int),
		ContentClasses: make(map[string]int),
	}
	sessionStats := make(map[string]*wssAuditSessionSummary)
	for _, summary := range summaries {
		route := wssAuditRouteMode(summary)
		if route == "" {
			route = "unknown"
		}
		report.RouteModes[route]++
		if !wssAuditIsWSS(summary, route) {
			continue
		}
		report.WSSRequests++
		if wssAuditIsPhaseF(route) {
			report.PhaseFRequests++
		}
		if strings.TrimSpace(summary.SessionID) == "" {
			report.MissingSessionID++
		}
		if summary.PreviousResponseIDUsed {
			report.PreviousResponseIDUsed++
		}
		if summary.Tokens.Saved > 0 {
			report.PositiveSavings++
			report.TokensSaved += summary.Tokens.Saved
		}
		for _, class := range wssAuditContentClasses(summary) {
			report.ContentClasses[class]++
		}
		sessionID := strings.TrimSpace(summary.SessionID)
		if sessionID == "" {
			sessionID = "(missing)"
		}
		stats := sessionStats[sessionID]
		if stats == nil {
			stats = &wssAuditSessionSummary{SessionID: sessionID}
			sessionStats[sessionID] = stats
		}
		stats.Requests++
		if wssAuditIsPhaseF(route) {
			stats.PhaseFRequests++
		}
		if summary.PreviousResponseIDUsed {
			stats.PreviousResponseIDUsed++
		}
		stats.TokensSaved += maxInt(0, summary.Tokens.Saved)
		if !summary.Timestamp.IsZero() {
			if stats.FirstSeen.IsZero() || summary.Timestamp.Before(stats.FirstSeen) {
				stats.FirstSeen = summary.Timestamp
			}
			if stats.LastSeen.IsZero() || summary.Timestamp.After(stats.LastSeen) {
				stats.LastSeen = summary.Timestamp
			}
		}
	}
	for _, stats := range sessionStats {
		report.Sessions = append(report.Sessions, *stats)
	}
	sort.Slice(report.Sessions, func(i, j int) bool {
		if report.Sessions[i].Requests != report.Sessions[j].Requests {
			return report.Sessions[i].Requests > report.Sessions[j].Requests
		}
		return report.Sessions[i].SessionID < report.Sessions[j].SessionID
	})
	report.Notes = wssAuditNotes(report)
	return report, nil
}

func wssAuditRouteMode(summary dbg.RequestSummary) string {
	if strings.TrimSpace(summary.RouteMode) != "" {
		return strings.TrimSpace(summary.RouteMode)
	}
	if summary.Plan != nil && strings.TrimSpace(summary.Plan.RouteMode) != "" {
		return strings.TrimSpace(summary.Plan.RouteMode)
	}
	return ""
}

func wssAuditIsWSS(summary dbg.RequestSummary, route string) bool {
	route = strings.ToLower(route)
	if strings.Contains(route, "websocket") || strings.Contains(route, "wss") {
		return true
	}
	path := strings.ToLower(summary.Path)
	return strings.Contains(path, "/backend-api/codex/responses") ||
		strings.Contains(path, "/backend-api/codex-bridge/responses")
}

func wssAuditIsPhaseF(route string) bool {
	route = strings.ToLower(strings.TrimSpace(route))
	return route == "websocket_phasef" || route == "wss_phasef" ||
		strings.Contains(route, "phasef")
}

func wssAuditContentClasses(summary dbg.RequestSummary) []string {
	if summary.Plan == nil {
		return nil
	}
	out := make([]string, 0, len(summary.Plan.ContentClasses))
	for _, class := range summary.Plan.ContentClasses {
		class = strings.TrimSpace(class)
		if class != "" {
			out = append(out, class)
		}
	}
	return out
}

func wssAuditNotes(report wssAuditReport) []string {
	var notes []string
	if report.WSSRequests == 0 {
		notes = append(notes, "No WSS request summaries found in this decisions log.")
	}
	if report.PhaseFRequests > 0 && report.MissingSessionID > 0 {
		notes = append(notes, "Some WSS Phase-F requests have no session id; repeat-read cache continuity is unsafe for those frames.")
	}
	if report.PhaseFRequests > 0 && report.PositiveSavings == 0 {
		notes = append(notes, "Route telemetry is present, but this log has no positive input-token savings.")
	}
	if report.PhaseFRequests > 0 && report.PreviousResponseIDUsed == 0 {
		notes = append(notes, "No previous_response_id usage observed; this may be a first-turn or non-delta capture.")
	}
	return notes
}

func writeWSSAuditText(w io.Writer, report wssAuditReport) {
	fmt.Fprintf(w, "=== WSS Audit: %s ===\n", filepath.Base(report.Path))
	fmt.Fprintf(w, "Requests analyzed:        %d\n", report.Requests)
	fmt.Fprintf(w, "WSS requests:             %d\n", report.WSSRequests)
	fmt.Fprintf(w, "Phase-F requests:         %d\n", report.PhaseFRequests)
	fmt.Fprintf(w, "missing session ids:      %d\n", report.MissingSessionID)
	fmt.Fprintf(w, "previous_response_id:     %d\n", report.PreviousResponseIDUsed)
	fmt.Fprintf(w, "positive savings reqs:    %d\n", report.PositiveSavings)
	fmt.Fprintf(w, "input tokens saved:       %d\n", report.TokensSaved)
	if len(report.RouteModes) > 0 {
		fmt.Fprintln(w, "\nRoutes:")
		for _, key := range sortedStringKeys(report.RouteModes) {
			fmt.Fprintf(w, "  %-24s %d\n", key, report.RouteModes[key])
		}
	}
	if len(report.ContentClasses) > 0 {
		fmt.Fprintln(w, "\nContent classes:")
		for _, key := range sortedStringKeys(report.ContentClasses) {
			fmt.Fprintf(w, "  %-24s %d\n", key, report.ContentClasses[key])
		}
	}
	if len(report.Sessions) > 0 {
		fmt.Fprintln(w, "\nSessions:")
		for _, session := range report.Sessions {
			fmt.Fprintf(w, "  %-32s requests=%d phasef=%d prev_id=%d saved=%d\n",
				truncateMiddle(session.SessionID, 32), session.Requests, session.PhaseFRequests,
				session.PreviousResponseIDUsed, session.TokensSaved)
		}
	}
	if len(report.Notes) > 0 {
		fmt.Fprintln(w, "\nNotes:")
		for _, note := range report.Notes {
			fmt.Fprintf(w, "  - %s\n", note)
		}
	}
}

func truncateMiddle(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	head := (maxLen - 3) / 2
	tail := maxLen - 3 - head
	return s[:head] + "..." + s[len(s)-tail:]
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

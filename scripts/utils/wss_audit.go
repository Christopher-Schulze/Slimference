package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/slimference/slimference/internal/control"
	dbg "github.com/slimference/slimference/internal/debug"
)

type wssAuditReport struct {
	Path                   string                           `json:"path"`
	Requests               int                              `json:"requests"`
	WSSRequests            int                              `json:"wss_requests"`
	PhaseFRequests         int                              `json:"phasef_requests"`
	UniqueSessions         int                              `json:"unique_sessions"`
	MissingSessionID       int                              `json:"missing_session_id"`
	PreviousResponseIDUsed int                              `json:"previous_response_id_used"`
	ReReadRequests         int                              `json:"re_read_requests"`
	ReReadCount            int                              `json:"re_read_count"`
	PositiveSavings        int                              `json:"positive_savings_requests"`
	TokensSaved            int                              `json:"tokens_saved"`
	Since                  *time.Time                       `json:"since,omitempty"`
	GatePassed             bool                             `json:"gate_passed"`
	GateFailures           []string                         `json:"gate_failures,omitempty"`
	RouteModes             map[string]int                   `json:"route_modes,omitempty"`
	ContentClasses         map[string]int                   `json:"content_classes,omitempty"`
	PolicySource           string                           `json:"policy_source,omitempty"`
	Policy                 []control.ProxyLayer0PolicyEntry `json:"policy,omitempty"`
	Sessions               []wssAuditSessionSummary         `json:"sessions,omitempty"`
	Notes                  []string                         `json:"notes,omitempty"`
}

type wssAuditSessionSummary struct {
	SessionID              string    `json:"session_id"`
	Requests               int       `json:"requests"`
	PhaseFRequests         int       `json:"phasef_requests"`
	PreviousResponseIDUsed int       `json:"previous_response_id_used"`
	ReReadCount            int       `json:"re_read_count"`
	TokensSaved            int       `json:"tokens_saved"`
	FirstSeen              time.Time `json:"first_seen,omitempty"`
	LastSeen               time.Time `json:"last_seen,omitempty"`
}

type wssAuditFlags struct {
	path                   string
	outputFormat           string
	expectDistinctSessions int
	minPhaseF              int
	requireSavings         bool
	adminStateFile         string
	since                  time.Time
	help                   bool
}

const wssAuditHelpText = `wss-audit: inspect Codex WSS decisions without raw frame dumps

Usage:
  go run ./scripts/utils wss-audit <decisions.jsonl> [flags]

Flags:
  --expect-distinct-sessions=<n>  Fail if fewer than n non-empty WSS session ids are present
  --min-phasef=<n>                Fail if fewer than n Phase-F request summaries are present
  --require-savings               Fail if no positive input-token savings are present
  --admin-state-file=<path>        Join current /admin/state policy counters into the report
  --since=<rfc3339>               Ignore records before this timestamp
  --json                          Output JSON

Reads content-free RequestSummary JSONL records and reports WSS route coverage,
Phase-F request counts, session-key continuity, previous_response_id usage, and
positive input-token savings. With --admin-state-file it also prints content-free
policy counters from the matching admin snapshot. It does not inspect payload text
or auth headers.`

func runWSSAudit(args []string, stdout, stderr io.Writer) int {
	flags, err := parseWSSAuditFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, wssAuditHelpText)
		return 0
	}
	if flags.path == "" {
		fmt.Fprintln(stderr, "Usage: wss-audit <decisions.jsonl> [--json]")
		return 2
	}
	report, err := loadWSSAuditReport(flags)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if flags.outputFormat == outputJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		fmt.Fprintln(stdout, string(data))
		if !report.GatePassed {
			return 3
		}
		return 0
	}
	writeWSSAuditText(stdout, report)
	if !report.GatePassed {
		return 3
	}
	return 0
}

func parseWSSAuditFlags(args []string) (wssAuditFlags, error) {
	flags := wssAuditFlags{outputFormat: outputText}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-h":
			flags.help = true
		case a == "--json":
			flags.outputFormat = outputJSON
		case a == "--require-savings":
			flags.requireSavings = true
		case a == "--admin-state-file":
			v, err := aggregateFlagValue(args, &i, a)
			if err != nil {
				return flags, err
			}
			flags.adminStateFile = v
		case strings.HasPrefix(a, "--admin-state-file="):
			flags.adminStateFile = strings.TrimPrefix(a, "--admin-state-file=")
		case a == "--since":
			v, err := aggregateFlagValue(args, &i, a)
			if err != nil {
				return flags, err
			}
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return flags, fmt.Errorf("--since must be RFC3339: %w", err)
			}
			flags.since = t
		case strings.HasPrefix(a, "--since="):
			t, err := time.Parse(time.RFC3339, strings.TrimPrefix(a, "--since="))
			if err != nil {
				return flags, fmt.Errorf("--since must be RFC3339: %w", err)
			}
			flags.since = t
		case a == "--expect-distinct-sessions":
			v, err := aggregateFlagValue(args, &i, a)
			if err != nil {
				return flags, err
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return flags, fmt.Errorf("--expect-distinct-sessions must be a non-negative integer")
			}
			flags.expectDistinctSessions = n
		case strings.HasPrefix(a, "--expect-distinct-sessions="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--expect-distinct-sessions="))
			if err != nil || n < 0 {
				return flags, fmt.Errorf("--expect-distinct-sessions must be a non-negative integer")
			}
			flags.expectDistinctSessions = n
		case a == "--min-phasef":
			v, err := aggregateFlagValue(args, &i, a)
			if err != nil {
				return flags, err
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return flags, fmt.Errorf("--min-phasef must be a non-negative integer")
			}
			flags.minPhaseF = n
		case strings.HasPrefix(a, "--min-phasef="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--min-phasef="))
			if err != nil || n < 0 {
				return flags, fmt.Errorf("--min-phasef must be a non-negative integer")
			}
			flags.minPhaseF = n
		case strings.HasPrefix(a, "-"):
			return flags, fmt.Errorf("unknown flag: %s", a)
		default:
			if flags.path != "" {
				return flags, fmt.Errorf("multiple decisions logs provided")
			}
			flags.path = a
		}
	}
	return flags, nil
}

func loadWSSAuditReport(flags wssAuditFlags) (wssAuditReport, error) {
	summaries, err := dbg.ReplaySession(flags.path)
	if err != nil {
		return wssAuditReport{}, fmt.Errorf("read decisions %s: %w", flags.path, err)
	}
	report := wssAuditReport{
		Path:           flags.path,
		GatePassed:     true,
		RouteModes:     make(map[string]int),
		ContentClasses: make(map[string]int),
	}
	if !flags.since.IsZero() {
		since := flags.since
		report.Since = &since
	}
	sessionStats := make(map[string]*wssAuditSessionSummary)
	for _, summary := range summaries {
		if !flags.since.IsZero() {
			if summary.Timestamp.IsZero() || summary.Timestamp.Before(flags.since) {
				continue
			}
		}
		report.Requests++
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
		if summary.ReReadCount > 0 {
			report.ReReadRequests++
			report.ReReadCount += summary.ReReadCount
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
		stats.ReReadCount += summary.ReReadCount
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
		if stats.SessionID != "(missing)" {
			report.UniqueSessions++
		}
		report.Sessions = append(report.Sessions, *stats)
	}
	sort.Slice(report.Sessions, func(i, j int) bool {
		if report.Sessions[i].Requests != report.Sessions[j].Requests {
			return report.Sessions[i].Requests > report.Sessions[j].Requests
		}
		return report.Sessions[i].SessionID < report.Sessions[j].SessionID
	})
	report.Notes = wssAuditNotes(report)
	report.GateFailures = wssAuditGateFailures(report, flags)
	report.GatePassed = len(report.GateFailures) == 0
	if flags.adminStateFile != "" {
		policy, err := loadWSSAuditPolicy(flags.adminStateFile)
		if err != nil {
			return wssAuditReport{}, err
		}
		report.PolicySource = "file:" + flags.adminStateFile
		report.Policy = policy
	}
	return report, nil
}

func loadWSSAuditPolicy(path string) ([]control.ProxyLayer0PolicyEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read admin state file %s: %w", path, err)
	}
	state, err := parseAdminStateJSON(data)
	if err != nil {
		return nil, err
	}
	return state.Savings.ProxyLayer0Policy, nil
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
	if report.ReReadCount > 0 {
		notes = append(notes, "WSS re-read canary observed repeated tool keys; review alongside savings to distinguish useful repeat reads from possible context-recall pressure.")
	}
	return notes
}

func wssAuditGateFailures(report wssAuditReport, flags wssAuditFlags) []string {
	var failures []string
	if flags.expectDistinctSessions > 0 && report.UniqueSessions < flags.expectDistinctSessions {
		failures = append(failures, fmt.Sprintf("expected at least %d distinct non-empty WSS session ids, got %d", flags.expectDistinctSessions, report.UniqueSessions))
	}
	if flags.minPhaseF > 0 && report.PhaseFRequests < flags.minPhaseF {
		failures = append(failures, fmt.Sprintf("expected at least %d Phase-F request summaries, got %d", flags.minPhaseF, report.PhaseFRequests))
	}
	if flags.requireSavings && report.PositiveSavings == 0 {
		failures = append(failures, "expected at least one positive input-token savings request")
	}
	return failures
}

func writeWSSAuditText(w io.Writer, report wssAuditReport) {
	fmt.Fprintf(w, "=== WSS Audit: %s ===\n", filepath.Base(report.Path))
	if report.Since != nil {
		fmt.Fprintf(w, "Since:                   %s\n", report.Since.Format(time.RFC3339))
	}
	fmt.Fprintf(w, "Requests analyzed:        %d\n", report.Requests)
	fmt.Fprintf(w, "WSS requests:             %d\n", report.WSSRequests)
	fmt.Fprintf(w, "Phase-F requests:         %d\n", report.PhaseFRequests)
	fmt.Fprintf(w, "unique session ids:       %d\n", report.UniqueSessions)
	fmt.Fprintf(w, "missing session ids:      %d\n", report.MissingSessionID)
	fmt.Fprintf(w, "previous_response_id:     %d\n", report.PreviousResponseIDUsed)
	fmt.Fprintf(w, "re-read requests/count:   %d / %d\n", report.ReReadRequests, report.ReReadCount)
	fmt.Fprintf(w, "positive savings reqs:    %d\n", report.PositiveSavings)
	fmt.Fprintf(w, "input tokens saved:       %d\n", report.TokensSaved)
	fmt.Fprintf(w, "gate:                     %s\n", passFail(report.GatePassed))
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
	if len(report.Policy) > 0 {
		fmt.Fprintln(w, "\nPolicy decisions:")
		if report.PolicySource != "" {
			fmt.Fprintf(w, "  source: %s\n", report.PolicySource)
		}
		for _, entry := range report.Policy {
			fmt.Fprintf(w, "  %s/%s/%s %s: %d\n",
				valueOrDash(entry.Route), entry.Mechanism, entry.Action, entry.Reason, entry.Count)
		}
	}
	if len(report.Sessions) > 0 {
		fmt.Fprintln(w, "\nSessions:")
		for _, session := range report.Sessions {
			fmt.Fprintf(w, "  %-32s requests=%d phasef=%d prev_id=%d reread=%d saved=%d\n",
				truncateMiddle(session.SessionID, 32), session.Requests, session.PhaseFRequests,
				session.PreviousResponseIDUsed, session.ReReadCount, session.TokensSaved)
		}
	}
	if len(report.Notes) > 0 {
		fmt.Fprintln(w, "\nNotes:")
		for _, note := range report.Notes {
			fmt.Fprintf(w, "  - %s\n", note)
		}
	}
	if len(report.GateFailures) > 0 {
		fmt.Fprintln(w, "\nGate failures:")
		for _, failure := range report.GateFailures {
			fmt.Fprintf(w, "  - %s\n", failure)
		}
	}
}

func passFail(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
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

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

	"github.com/Christopher-Schulze/Slimference/internal/control"
	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/evidence"
)

type wssAuditReport struct {
	Path                   string                           `json:"path"`
	Requests               int                              `json:"requests"`
	WSSRequests            int                              `json:"wss_requests"`
	PhaseFRequests         int                              `json:"phasef_requests"`
	UniqueSessions         int                              `json:"unique_sessions"`
	MissingSessionID       int                              `json:"missing_session_id"`
	PreviousResponseIDUsed int                              `json:"previous_response_id_used"`
	RequestShapes          map[string]int                   `json:"request_shapes,omitempty"`
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
	Cache                  []control.ProxyLayer0CacheEntry  `json:"cache,omitempty"`
	ChunkDedupReferences   int64                            `json:"chunk_dedup_references,omitempty"`
	ChunkDedupRefBytes     int64                            `json:"chunk_dedup_referenced_bytes,omitempty"`
	ChunkDedupInputBytes   int64                            `json:"chunk_dedup_input_bytes,omitempty"`
	HistoryReducers        []wssHistoryReducerSummary       `json:"history_reducers,omitempty"`
	ShadowMirror           *wssShadowMirrorSummary          `json:"shadow_mirror,omitempty"`
	Sessions               []wssAuditSessionSummary         `json:"sessions,omitempty"`
	Notes                  []string                         `json:"notes,omitempty"`
}

type wssAuditSessionSummary struct {
	SessionID              string         `json:"session_id"`
	Requests               int            `json:"requests"`
	PhaseFRequests         int            `json:"phasef_requests"`
	PreviousResponseIDUsed int            `json:"previous_response_id_used"`
	RequestShapes          map[string]int `json:"request_shapes,omitempty"`
	ReReadCount            int            `json:"re_read_count"`
	TokensSaved            int            `json:"tokens_saved"`
	FirstSeen              time.Time      `json:"first_seen,omitempty"`
	LastSeen               time.Time      `json:"last_seen,omitempty"`
}

type wssHistoryReducerSummary struct {
	Mechanism        string         `json:"mechanism"`
	Decisions        int            `json:"decisions"`
	Applied          int            `json:"applied"`
	FullPass         int            `json:"full_pass"`
	Skipped          int            `json:"skipped"`
	FailedOpen       int            `json:"failed_open"`
	OriginalTokens   int            `json:"original_tokens,omitempty"`
	FinalTokens      int            `json:"final_tokens,omitempty"`
	SavedTokens      int            `json:"saved_tokens,omitempty"`
	NetTokens        int            `json:"net_tokens,omitempty"`
	FootprintScore   int            `json:"footprint_score,omitempty"`
	Reasons          map[string]int `json:"reasons,omitempty"`
	FootprintBuckets map[string]int `json:"footprint_score_buckets,omitempty"`
	CacheImpact      map[string]int `json:"cache_impact,omitempty"`
}

type wssShadowMirrorSummary struct {
	Requests                           int                          `json:"requests"`
	Blocks                             int                          `json:"blocks"`
	Bytes                              int                          `json:"bytes"`
	ReferenceableBlocks                int                          `json:"referenceable_blocks"`
	ReferenceableBytes                 int                          `json:"referenceable_bytes"`
	ReferenceableBytePct               float64                      `json:"referenceable_byte_pct"`
	NormalizedSegments                 int                          `json:"normalized_segments"`
	NormalizedBytes                    int                          `json:"normalized_bytes"`
	NormalizedReferenceableSegments    int                          `json:"normalized_referenceable_segments"`
	NormalizedReferenceableBytes       int                          `json:"normalized_referenceable_bytes"`
	NormalizedReferenceableBytePct     float64                      `json:"normalized_referenceable_byte_pct"`
	NormalizedReferenceableBytesByKind []wssShadowMirrorKindSummary `json:"normalized_referenceable_bytes_by_kind,omitempty"`
}

type wssShadowMirrorKindSummary struct {
	Kind                  string  `json:"kind"`
	Segments              int     `json:"segments"`
	Bytes                 int     `json:"bytes"`
	ReferenceableSegments int     `json:"referenceable_segments"`
	ReferenceableBytes    int     `json:"referenceable_bytes"`
	ReferenceableBytePct  float64 `json:"referenceable_byte_pct"`
}

type wssShadowMirrorAccumulator struct {
	summary wssShadowMirrorSummary
	byKind  map[string]*wssShadowMirrorKindSummary
}

type wssAuditFlags struct {
	path                   string
	outputFormat           string
	expectDistinctSessions int
	minPhaseF              int
	minFullHistory         int
	requireSavings         bool
	requireHistoryEvidence bool
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
  --min-full-history=<n>          Fail if fewer than n full-history Phase-F request summaries are present
  --require-savings               Fail if no positive input-token savings are present
  --require-history-evidence      Fail if no stale/obsolete reducer evidence is present
  --admin-state-file=<path>        Join current /admin/state policy counters into the report
  --since=<rfc3339>               Ignore records before this timestamp
  --json                          Output JSON

Reads content-free RequestSummary JSONL records and reports WSS route coverage,
Phase-F request counts, session-key continuity, previous_response_id usage,
request-shape coverage, positive input-token savings, T353 history-reducer
evidence, and T355 server-state shadow-mirror density. With --admin-state-file it also prints
content-free policy and cache hit/miss counters from the matching admin snapshot.
It does not inspect payload text or auth headers.`

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
		case a == "--require-history-evidence":
			flags.requireHistoryEvidence = true
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
		case a == "--min-full-history":
			v, err := aggregateFlagValue(args, &i, a)
			if err != nil {
				return flags, err
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return flags, fmt.Errorf("--min-full-history must be a non-negative integer")
			}
			flags.minFullHistory = n
		case strings.HasPrefix(a, "--min-full-history="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--min-full-history="))
			if err != nil || n < 0 {
				return flags, fmt.Errorf("--min-full-history must be a non-negative integer")
			}
			flags.minFullHistory = n
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
		RequestShapes:  make(map[string]int),
	}
	if !flags.since.IsZero() {
		since := flags.since
		report.Since = &since
	}
	sessionStats := make(map[string]*wssAuditSessionSummary)
	historyReducers := make(map[string]*wssHistoryReducerSummary)
	shadowMirror := wssShadowMirrorAccumulator{byKind: make(map[string]*wssShadowMirrorKindSummary)}
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
		isPhaseF := wssAuditIsPhaseF(route)
		if isPhaseF {
			report.PhaseFRequests++
			shape := wssAuditRequestShape(summary)
			if shape != "" {
				report.RequestShapes[shape]++
			}
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
		accumulateWSSHistoryReducers(historyReducers, summary.EvidenceDecisions)
		shadowMirror.add(summary.DebugFacts)
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
		if isPhaseF {
			stats.PhaseFRequests++
			shape := wssAuditRequestShape(summary)
			if shape != "" {
				addWSSAuditCount(&stats.RequestShapes, shape)
			}
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
	if shadow := shadowMirror.finalize(); shadow != nil {
		report.ShadowMirror = shadow
	}
	report.HistoryReducers = finalizeWSSHistoryReducers(historyReducers)
	report.Notes = wssAuditNotes(report)
	report.GateFailures = wssAuditGateFailures(report, flags)
	report.GatePassed = len(report.GateFailures) == 0
	if flags.adminStateFile != "" {
		policy, cache, savings, err := loadWSSAuditTelemetry(flags.adminStateFile)
		if err != nil {
			return wssAuditReport{}, err
		}
		report.PolicySource = "file:" + flags.adminStateFile
		report.Policy = policy
		report.Cache = cache
		report.ChunkDedupReferences = savings.ProxyLayer0ChunkRefs
		report.ChunkDedupRefBytes = savings.ProxyLayer0ChunkRefBytes
		report.ChunkDedupInputBytes = savings.ProxyLayer0ChunkInBytes
	}
	return report, nil
}

func accumulateWSSHistoryReducers(out map[string]*wssHistoryReducerSummary, decisions []evidence.BlockDecision) {
	if out == nil {
		return
	}
	for _, decision := range decisions {
		mechanism := strings.TrimSpace(decision.Mechanism)
		if !isWSSHistoryReducerMechanism(mechanism) {
			continue
		}
		row := out[mechanism]
		if row == nil {
			row = &wssHistoryReducerSummary{Mechanism: mechanism}
			out[mechanism] = row
		}
		row.Decisions++
		switch decision.Action {
		case evidence.ActionApplied:
			row.Applied++
		case evidence.ActionFullPass:
			row.FullPass++
		case evidence.ActionSkipped:
			row.Skipped++
		case evidence.ActionFailedOpen:
			row.FailedOpen++
		}
		row.OriginalTokens += decision.OriginalTokens
		row.FinalTokens += decision.FinalTokens
		row.SavedTokens += decision.SavedTokens
		row.NetTokens += decision.NetTokens
		row.FootprintScore += decision.FootprintScore
		addWSSAuditCount(&row.Reasons, decision.Reason)
		addWSSAuditCount(&row.FootprintBuckets, decision.FootprintScoreBucket)
		addWSSAuditCount(&row.CacheImpact, decision.CacheImpact)
	}
}

func isWSSHistoryReducerMechanism(mechanism string) bool {
	switch mechanism {
	case "stale_read", "obsolete_prune":
		return true
	default:
		return false
	}
}

func addWSSAuditCount(counts *map[string]int, key string) {
	key = strings.TrimSpace(key)
	if counts == nil || key == "" {
		return
	}
	if *counts == nil {
		*counts = make(map[string]int)
	}
	(*counts)[key]++
}

func finalizeWSSHistoryReducers(rows map[string]*wssHistoryReducerSummary) []wssHistoryReducerSummary {
	if len(rows) == 0 {
		return nil
	}
	keys := make([]string, 0, len(rows))
	for key := range rows {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]wssHistoryReducerSummary, 0, len(keys))
	for _, key := range keys {
		out = append(out, *rows[key])
	}
	return out
}

func (a *wssShadowMirrorAccumulator) add(facts map[string]string) {
	if a == nil || len(facts) == 0 || !wssShadowMirrorFactsPresent(facts) {
		return
	}
	a.summary.Requests++
	a.summary.Blocks += intFact(facts, "wss.shadow_mirror_blocks")
	a.summary.Bytes += intFact(facts, "wss.shadow_mirror_bytes")
	a.summary.ReferenceableBlocks += intFact(facts, "wss.shadow_mirror_referenceable_blocks")
	a.summary.ReferenceableBytes += intFact(facts, "wss.shadow_mirror_referenceable_bytes")
	a.summary.NormalizedSegments += intFact(facts, "wss.shadow_mirror_normalized_segments")
	a.summary.NormalizedBytes += intFact(facts, "wss.shadow_mirror_normalized_bytes")
	a.summary.NormalizedReferenceableSegments += intFact(facts, "wss.shadow_mirror_normalized_referenceable_segments")
	a.summary.NormalizedReferenceableBytes += intFact(facts, "wss.shadow_mirror_normalized_referenceable_bytes")
	a.addKinds(facts["wss.shadow_mirror_normalized_density_by_kind"])
}

func (a *wssShadowMirrorAccumulator) addKinds(encoded string) {
	if a == nil || strings.TrimSpace(encoded) == "" {
		return
	}
	for _, part := range strings.Split(encoded, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, values, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		pieces := strings.Split(values, "/")
		if len(pieces) != 4 {
			continue
		}
		refBytes, okRefBytes := parseNonNegativeInt(pieces[0])
		bytes, okBytes := parseNonNegativeInt(pieces[1])
		refSegments, okRefSegments := parseNonNegativeInt(pieces[2])
		segments, okSegments := parseNonNegativeInt(pieces[3])
		if !okRefBytes || !okBytes || !okRefSegments || !okSegments {
			continue
		}
		row := a.byKind[name]
		if row == nil {
			row = &wssShadowMirrorKindSummary{Kind: name}
			a.byKind[name] = row
		}
		row.ReferenceableBytes += refBytes
		row.Bytes += bytes
		row.ReferenceableSegments += refSegments
		row.Segments += segments
	}
}

func (a *wssShadowMirrorAccumulator) finalize() *wssShadowMirrorSummary {
	if a == nil || a.summary.Requests == 0 {
		return nil
	}
	out := a.summary
	out.ReferenceableBytePct = pct(out.ReferenceableBytes, out.Bytes)
	out.NormalizedReferenceableBytePct = pct(out.NormalizedReferenceableBytes, out.NormalizedBytes)
	keys := make([]string, 0, len(a.byKind))
	for key := range a.byKind {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		row := *a.byKind[key]
		row.ReferenceableBytePct = pct(row.ReferenceableBytes, row.Bytes)
		out.NormalizedReferenceableBytesByKind = append(out.NormalizedReferenceableBytesByKind, row)
	}
	return &out
}

func wssShadowMirrorFactsPresent(facts map[string]string) bool {
	for key := range facts {
		if strings.HasPrefix(key, "wss.shadow_mirror_") {
			return true
		}
	}
	return false
}

func intFact(facts map[string]string, key string) int {
	value, ok := parseNonNegativeInt(facts[key])
	if !ok {
		return 0
	}
	return value
}

func parseNonNegativeInt(value string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func pct(part, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}

func loadWSSAuditTelemetry(path string) ([]control.ProxyLayer0PolicyEntry, []control.ProxyLayer0CacheEntry, control.SavingsSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, control.SavingsSummary{}, fmt.Errorf("read admin state file %s: %w", path, err)
	}
	state, err := parseAdminStateJSON(data)
	if err != nil {
		return nil, nil, control.SavingsSummary{}, err
	}
	return state.Savings.ProxyLayer0Policy, state.Savings.ProxyLayer0Cache, state.Savings, nil
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

func wssAuditRequestShape(summary dbg.RequestSummary) string {
	if summary.DebugFacts == nil {
		return "unknown"
	}
	shape := strings.ToLower(strings.TrimSpace(summary.DebugFacts["wss.request_shape"]))
	switch shape {
	case "root", "delta", "full_history":
		return shape
	case "":
		return "unknown"
	default:
		return "unknown"
	}
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
	if report.PhaseFRequests > 0 && len(report.RequestShapes) == 0 {
		notes = append(notes, "No WSS request-shape facts observed; Class-A/Class-B routing cannot be proven from this log.")
	}
	if report.PhaseFRequests > 0 && report.RequestShapes["unknown"] > 0 {
		notes = append(notes, "Some Phase-F rows have unknown request shape; use a fresh capture before treating shape percentages as complete.")
	}
	if report.RequestShapes["full_history"] > 0 {
		notes = append(notes, "Full-history Class-B rows observed; correlate them with savings, socket lifecycle, and upstream-error counters before widening guards.")
	} else if report.PhaseFRequests > 0 && report.RequestShapes["delta"] > 0 {
		notes = append(notes, "This capture has delta-shaped Phase-F traffic but no full-history Class-B rows; do not use it to prove T354 Class-B widening.")
	}
	if report.ReReadCount > 0 {
		notes = append(notes, "WSS re-read canary observed repeated tool keys; review alongside savings to distinguish useful repeat reads from possible context-recall pressure.")
	}
	if report.PhaseFRequests > 0 && len(report.HistoryReducers) == 0 {
		notes = append(notes, "No T353 stale-read / obsolete-prune evidence observed; history reducer calibration needs a fresher capture or a history-heavy workload.")
	}
	if report.PhaseFRequests > 0 && report.ShadowMirror == nil {
		notes = append(notes, "No T355 shadow-mirror telemetry observed; this capture may predate normalized mirror facts or contain no text blocks.")
	}
	if report.ShadowMirror != nil && report.ShadowMirror.NormalizedBytes > 0 && report.ShadowMirror.NormalizedReferenceableBytes == 0 {
		notes = append(notes, "T355 shadow mirror saw normalized bytes but no normalized referenceable bytes in this capture.")
	}
	if report.ShadowMirror != nil && report.ShadowMirror.NormalizedReferenceableBytes > 0 {
		notes = append(notes, "T355 shadow mirror found normalized referenceable bytes; treat this as measurement evidence only, not a product mutation proof.")
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
	if flags.minFullHistory > 0 && report.RequestShapes["full_history"] < flags.minFullHistory {
		failures = append(failures, fmt.Sprintf("expected at least %d full-history Phase-F request summaries, got %d", flags.minFullHistory, report.RequestShapes["full_history"]))
	}
	if flags.requireSavings && report.PositiveSavings == 0 {
		failures = append(failures, "expected at least one positive input-token savings request")
	}
	if flags.requireHistoryEvidence && len(report.HistoryReducers) == 0 {
		failures = append(failures, "expected stale-read or obsolete-prune history reducer evidence")
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
	if len(report.RequestShapes) > 0 {
		fmt.Fprintf(w, "request shapes:           %s\n", formatWSSAuditCounts(report.RequestShapes))
	}
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
	if len(report.Cache) > 0 {
		fmt.Fprintln(w, "\nCache decisions:")
		if report.PolicySource != "" {
			fmt.Fprintf(w, "  source: %s\n", report.PolicySource)
		}
		for _, entry := range report.Cache {
			fmt.Fprintf(w, "  %s/%s/%s %s: %d\n",
				valueOrDash(entry.Route), entry.Mechanism, entry.Action, entry.Reason, entry.Count)
		}
	}
	if report.ChunkDedupReferences > 0 || report.ChunkDedupRefBytes > 0 || report.ChunkDedupInputBytes > 0 {
		fmt.Fprintln(w, "\nChunk dedup density:")
		if report.PolicySource != "" {
			fmt.Fprintf(w, "  source: %s\n", report.PolicySource)
		}
		fmt.Fprintf(w, "  references:              %d\n", report.ChunkDedupReferences)
		fmt.Fprintf(w, "  referenced bytes:        %d\n", report.ChunkDedupRefBytes)
		fmt.Fprintf(w, "  input bytes:             %d\n", report.ChunkDedupInputBytes)
	}
	if len(report.HistoryReducers) > 0 {
		fmt.Fprintln(w, "\nHistory reducers:")
		for _, row := range report.HistoryReducers {
			fmt.Fprintf(w, "  %-16s decisions=%d applied=%d full_pass=%d skipped=%d failed_open=%d saved=%d net=%d footprint=%d reasons=%s\n",
				row.Mechanism,
				row.Decisions,
				row.Applied,
				row.FullPass,
				row.Skipped,
				row.FailedOpen,
				row.SavedTokens,
				row.NetTokens,
				row.FootprintScore,
				formatWSSAuditCounts(row.Reasons),
			)
		}
	}
	if report.ShadowMirror != nil {
		fmt.Fprintln(w, "\nShadow mirror density:")
		fmt.Fprintf(w, "  requests:                %d\n", report.ShadowMirror.Requests)
		fmt.Fprintf(w, "  exact blocks:            %d\n", report.ShadowMirror.Blocks)
		fmt.Fprintf(w, "  exact bytes:             %d\n", report.ShadowMirror.Bytes)
		fmt.Fprintf(w, "  exact referenceable:     %d blocks / %d bytes (%.2f%%)\n",
			report.ShadowMirror.ReferenceableBlocks,
			report.ShadowMirror.ReferenceableBytes,
			report.ShadowMirror.ReferenceableBytePct)
		fmt.Fprintf(w, "  normalized segments:     %d\n", report.ShadowMirror.NormalizedSegments)
		fmt.Fprintf(w, "  normalized bytes:        %d\n", report.ShadowMirror.NormalizedBytes)
		fmt.Fprintf(w, "  normalized referenceable:%d segments / %d bytes (%.2f%%)\n",
			report.ShadowMirror.NormalizedReferenceableSegments,
			report.ShadowMirror.NormalizedReferenceableBytes,
			report.ShadowMirror.NormalizedReferenceableBytePct)
		if len(report.ShadowMirror.NormalizedReferenceableBytesByKind) > 0 {
			fmt.Fprintln(w, "  by kind:")
			for _, row := range report.ShadowMirror.NormalizedReferenceableBytesByKind {
				fmt.Fprintf(w, "    %-24s %d/%d bytes %.2f%% (%d/%d segments)\n",
					row.Kind, row.ReferenceableBytes, row.Bytes, row.ReferenceableBytePct,
					row.ReferenceableSegments, row.Segments)
			}
		}
	}
	if len(report.Sessions) > 0 {
		fmt.Fprintln(w, "\nSessions:")
		for _, session := range report.Sessions {
			fmt.Fprintf(w, "  %-32s requests=%d phasef=%d prev_id=%d shapes=%s reread=%d saved=%d\n",
				truncateMiddle(session.SessionID, 32), session.Requests, session.PhaseFRequests,
				session.PreviousResponseIDUsed, formatWSSAuditCounts(session.RequestShapes), session.ReReadCount, session.TokensSaved)
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

func formatWSSAuditCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(counts))
	for _, key := range sortedStringKeys(counts) {
		parts = append(parts, fmt.Sprintf("%s:%d", key, counts[key]))
	}
	return strings.Join(parts, ",")
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

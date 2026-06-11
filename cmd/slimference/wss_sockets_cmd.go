package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
)

const (
	defaultWSSSocketRequestLimit = 20000
	maxWSSSocketRequestLimit     = 200000
)

type wssSocketDebugArgs struct {
	Limit                              int
	JSONOut                            bool
	SessionFilter                      string
	Since                              time.Time
	MaxActionableSockets               int
	MaxReconnectFullHistoryRequests    int
	MaxReconnectFullHistoryInputTokens int
}

type wssSocketReport struct {
	DecisionsLog                            string             `json:"decisions_log,omitempty"`
	RequestLimit                            int                `json:"request_limit"`
	SessionFilter                           string             `json:"session_filter,omitempty"`
	Since                                   time.Time          `json:"since,omitempty"`
	RequestsScanned                         int                `json:"requests_scanned"`
	RequestsFiltered                        int                `json:"requests_filtered"`
	WSSRequests                             int                `json:"wss_requests"`
	SocketCount                             int                `json:"socket_count"`
	ClosedSockets                           int                `json:"closed_sockets"`
	CloseInitiators                         map[string]int     `json:"close_initiators,omitempty"`
	CauseClasses                            map[string]int     `json:"cause_classes,omitempty"`
	ActionableSockets                       int                `json:"actionable_sockets"`
	ProviderInputTokens                     int                `json:"provider_input_tokens"`
	ProviderCachedTokens                    int                `json:"provider_cached_tokens"`
	LocalSavedTokens                        int                `json:"local_saved_tokens"`
	FullHistoryRequests                     int                `json:"full_history_requests"`
	FullHistoryProviderInputTokens          int                `json:"full_history_provider_input_tokens"`
	ReconnectFullHistoryRequests            int                `json:"reconnect_full_history_requests"`
	ReconnectFullHistoryProviderInputTokens int                `json:"reconnect_full_history_provider_input_tokens"`
	Sockets                                 []wssSocketSummary `json:"sockets"`
}

type wssSocketSummary struct {
	SocketKey                         string         `json:"socket_key"`
	SocketSeq                         uint64         `json:"socket_seq"`
	SocketInstance                    int            `json:"socket_instance"`
	SessionID                         string         `json:"session_id,omitempty"`
	FirstRequestID                    string         `json:"first_request_id,omitempty"`
	LastRequestID                     string         `json:"last_request_id,omitempty"`
	FirstTimestamp                    time.Time      `json:"first_ts,omitempty"`
	LastTimestamp                     time.Time      `json:"last_ts,omitempty"`
	Requests                          int            `json:"requests"`
	RequestShapes                     map[string]int `json:"request_shapes,omitempty"`
	RootRequests                      int            `json:"root_requests"`
	DeltaRequests                     int            `json:"delta_requests"`
	FullHistoryRequests               int            `json:"full_history_requests"`
	UnknownShapeRequests              int            `json:"unknown_shape_requests"`
	ProviderInputTokens               int            `json:"provider_input_tokens"`
	ProviderCachedTokens              int            `json:"provider_cached_tokens"`
	ProviderCachedRatio               float64        `json:"provider_cached_ratio"`
	LocalSavedTokens                  int            `json:"local_saved_tokens"`
	FullHistoryProviderInputTokens    int            `json:"full_history_provider_input_tokens"`
	ReconnectFullHistory              bool           `json:"reconnect_full_history"`
	ReconnectFullHistoryProviderInput int            `json:"reconnect_full_history_provider_input_tokens"`
	Cause                             string         `json:"cause"`
	CauseReason                       string         `json:"cause_reason,omitempty"`
	Actionable                        bool           `json:"actionable"`
	RecommendedAction                 string         `json:"recommended_action,omitempty"`
	Closed                            bool           `json:"closed"`
	CloseInitiator                    string         `json:"close_initiator,omitempty"`
	CloseError                        string         `json:"close_error,omitempty"`
	AgeMillis                         int64          `json:"age_ms,omitempty"`
	C2SFrames                         int64          `json:"c2s_frames,omitempty"`
	S2CFrames                         int64          `json:"s2c_frames,omitempty"`
	C2SBytes                          int64          `json:"c2s_bytes,omitempty"`
	S2CBytes                          int64          `json:"s2c_bytes,omitempty"`
	TurnsCompleted                    int64          `json:"turns_completed,omitempty"`
	estimatedClosedAt                 time.Time
}

func handleDebugWSSSockets(args []string) {
	opts, err := parseWSSSocketDebugArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		exitFn(1)
	}
	path := configuredDecisionsLogPath()
	if path == "" {
		fmt.Println("No decisions_log configured. Set [debug].decisions_log or SLIMFERENCE_DEBUG_DECISIONS_LOG.")
		return
	}
	summaries := readLastDecisionSummaries(path, opts.Limit)
	report := buildWSSSocketReportWithOptions(path, summaries, opts)
	printWSSSocketReport(report, opts.JSONOut)
	if violations := evaluateWSSSocketGate(report, opts); len(violations) > 0 {
		for _, violation := range violations {
			fmt.Fprintf(os.Stderr, "wss-sockets gate failed: %s\n", violation)
		}
		exitFn(1)
	}
}

func parseWSSSocketDebugArgs(args []string) (wssSocketDebugArgs, error) {
	opts := wssSocketDebugArgs{
		Limit:                              defaultWSSSocketRequestLimit,
		MaxActionableSockets:               -1,
		MaxReconnectFullHistoryRequests:    -1,
		MaxReconnectFullHistoryInputTokens: -1,
	}
	var gotLimit bool
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.TrimSpace(arg) == "" {
			continue
		}
		switch arg {
		case "--json", "-json":
			opts.JSONOut = true
			continue
		}
		if strings.HasPrefix(arg, "--limit=") {
			if gotLimit {
				return opts, fmt.Errorf("unexpected extra limit: %s", arg)
			}
			n, err := strconv.Atoi(strings.TrimPrefix(arg, "--limit="))
			if err != nil || n < 1 {
				return opts, fmt.Errorf("limit must be a positive integer")
			}
			opts.Limit = n
			gotLimit = true
			continue
		}
		if strings.HasPrefix(arg, "--session=") {
			opts.SessionFilter = strings.TrimSpace(strings.TrimPrefix(arg, "--session="))
			if opts.SessionFilter == "" {
				return opts, fmt.Errorf("session filter must not be empty")
			}
			continue
		}
		if arg == "--session" {
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return opts, fmt.Errorf("session filter must not be empty")
			}
			opts.SessionFilter = strings.TrimSpace(args[i])
			continue
		}
		if strings.HasPrefix(arg, "--since=") {
			since, err := parseWSSSocketSince(strings.TrimPrefix(arg, "--since="), time.Now())
			if err != nil {
				return opts, err
			}
			opts.Since = since
			continue
		}
		if arg == "--since" {
			i++
			if i >= len(args) {
				return opts, fmt.Errorf("since must be RFC3339 time or duration")
			}
			since, err := parseWSSSocketSince(args[i], time.Now())
			if err != nil {
				return opts, err
			}
			opts.Since = since
			continue
		}
		if arg == "--fail-on-actionable" {
			opts.MaxActionableSockets = 0
			continue
		}
		if arg == "--fail-on-full-history" {
			opts.MaxReconnectFullHistoryRequests = 0
			continue
		}
		if strings.HasPrefix(arg, "--max-actionable=") {
			n, err := parseNonNegativeWSSSocketLimit("max-actionable", strings.TrimPrefix(arg, "--max-actionable="))
			if err != nil {
				return opts, err
			}
			opts.MaxActionableSockets = n
			continue
		}
		if strings.HasPrefix(arg, "--max-reconnect-full-history=") {
			n, err := parseNonNegativeWSSSocketLimit("max-reconnect-full-history", strings.TrimPrefix(arg, "--max-reconnect-full-history="))
			if err != nil {
				return opts, err
			}
			opts.MaxReconnectFullHistoryRequests = n
			continue
		}
		if strings.HasPrefix(arg, "--max-reconnect-full-history-input=") {
			n, err := parseNonNegativeWSSSocketLimit("max-reconnect-full-history-input", strings.TrimPrefix(arg, "--max-reconnect-full-history-input="))
			if err != nil {
				return opts, err
			}
			opts.MaxReconnectFullHistoryInputTokens = n
			continue
		}
		if arg == "--limit" || arg == "-limit" {
			return opts, fmt.Errorf("usage: slimference debug wss-sockets [limit|--limit=N] [--json] [--session ID] [--since TIME] [gate flags]")
		}
		if strings.HasPrefix(arg, "-") {
			return opts, fmt.Errorf("unknown flag: %s", arg)
		}
		if gotLimit {
			return opts, fmt.Errorf("unexpected extra argument: %s", arg)
		}
		n, err := strconv.Atoi(arg)
		if err != nil || n < 1 {
			return opts, fmt.Errorf("limit must be a positive integer")
		}
		opts.Limit = n
		gotLimit = true
	}
	if opts.Limit > maxWSSSocketRequestLimit {
		opts.Limit = maxWSSSocketRequestLimit
	}
	return opts, nil
}

func buildWSSSocketReport(path string, limit int, summaries []dbg.RequestSummary) wssSocketReport {
	return buildWSSSocketReportWithOptions(path, summaries, wssSocketDebugArgs{Limit: limit})
}

func buildWSSSocketReportWithOptions(path string, summaries []dbg.RequestSummary, opts wssSocketDebugArgs) wssSocketReport {
	report := wssSocketReport{
		DecisionsLog:    path,
		RequestLimit:    opts.Limit,
		SessionFilter:   opts.SessionFilter,
		Since:           opts.Since,
		RequestsScanned: len(summaries),
		CloseInitiators: make(map[string]int),
		CauseClasses:    make(map[string]int),
		Sockets:         make([]wssSocketSummary, 0),
	}
	wssSummaries := make([]dbg.RequestSummary, 0, len(summaries))
	for _, summary := range summaries {
		if _, ok := wssSocketSeq(summary.DebugFacts); ok {
			if !wssSummaryMatchesFilters(summary, opts) {
				report.RequestsFiltered++
				continue
			}
			wssSummaries = append(wssSummaries, summary)
		}
	}
	sort.SliceStable(wssSummaries, func(i, j int) bool {
		return wssSummaries[i].Timestamp.Before(wssSummaries[j].Timestamp)
	})
	byBaseKey := make(map[string][]*wssSocketSummary)
	for _, summary := range wssSummaries {
		seq, _ := wssSocketSeq(summary.DebugFacts)
		report.WSSRequests++
		baseKey := wssSocketBaseKey(summary.SessionID, seq)
		groups := byBaseKey[baseKey]
		var socket *wssSocketSummary
		if len(groups) > 0 {
			socket = groups[len(groups)-1]
		}
		if socket == nil || wssSummaryStartsNewSocketInstance(socket, summary) {
			socket = &wssSocketSummary{
				SocketSeq:      seq,
				SocketInstance: len(groups) + 1,
				SessionID:      summary.SessionID,
				RequestShapes:  make(map[string]int),
			}
			socket.SocketKey = wssSocketKey(socket)
			groups = append(groups, socket)
			byBaseKey[baseKey] = groups
		}
		mergeWSSSocketSummary(socket, summary)
	}
	for _, groups := range byBaseKey {
		for _, socket := range groups {
			finalizeWSSSocketSummary(socket)
			classifyWSSSocketSummary(socket)
			report.ProviderInputTokens += socket.ProviderInputTokens
			report.ProviderCachedTokens += socket.ProviderCachedTokens
			report.LocalSavedTokens += socket.LocalSavedTokens
			report.FullHistoryRequests += socket.FullHistoryRequests
			report.FullHistoryProviderInputTokens += socket.FullHistoryProviderInputTokens
			if socket.ReconnectFullHistory {
				report.ReconnectFullHistoryRequests += socket.FullHistoryRequests
				report.ReconnectFullHistoryProviderInputTokens += socket.ReconnectFullHistoryProviderInput
			}
			if socket.Closed {
				report.ClosedSockets++
				initiator := socket.CloseInitiator
				if initiator == "" {
					initiator = "unknown"
				}
				report.CloseInitiators[initiator]++
			}
			report.CauseClasses[socket.Cause]++
			if socket.Actionable {
				report.ActionableSockets++
			}
			report.Sockets = append(report.Sockets, *socket)
		}
	}
	sort.Slice(report.Sockets, func(i, j int) bool {
		left := report.Sockets[i]
		right := report.Sockets[j]
		if !left.LastTimestamp.Equal(right.LastTimestamp) {
			return left.LastTimestamp.After(right.LastTimestamp)
		}
		return left.SocketSeq > right.SocketSeq
	})
	report.SocketCount = len(report.Sockets)
	if len(report.CloseInitiators) == 0 {
		report.CloseInitiators = nil
	}
	if len(report.CauseClasses) == 0 {
		report.CauseClasses = nil
	}
	return report
}

func wssSummaryMatchesFilters(summary dbg.RequestSummary, opts wssSocketDebugArgs) bool {
	if opts.SessionFilter != "" && !strings.HasPrefix(summary.SessionID, opts.SessionFilter) {
		return false
	}
	if !opts.Since.IsZero() {
		if summary.Timestamp.IsZero() {
			return false
		}
		return !summary.Timestamp.Before(opts.Since)
	}
	return true
}

func mergeWSSSocketSummary(socket *wssSocketSummary, summary dbg.RequestSummary) {
	if socket.SessionID == "" {
		socket.SessionID = summary.SessionID
	}
	if socket.FirstRequestID == "" || (!summary.Timestamp.IsZero() && summary.Timestamp.Before(socket.FirstTimestamp)) {
		socket.FirstRequestID = summary.RequestID
		socket.FirstTimestamp = summary.Timestamp
	}
	if socket.LastRequestID == "" || summary.Timestamp.After(socket.LastTimestamp) {
		socket.LastRequestID = summary.RequestID
		socket.LastTimestamp = summary.Timestamp
	}
	socket.Requests++
	shape := strings.TrimSpace(summary.DebugFacts["wss.request_shape"])
	if shape == "" {
		shape = "unknown"
	}
	socket.RequestShapes[shape]++
	socket.ProviderInputTokens += positiveInt(summary.ProviderInputTokens)
	socket.ProviderCachedTokens += positiveInt(summary.ProviderCachedTokens)
	socket.LocalSavedTokens += positiveInt(summary.Tokens.Saved)
	if shape == "full_history" {
		socket.FullHistoryProviderInputTokens += positiveInt(summary.ProviderInputTokens)
	}
	mergeWSSSocketCloseFacts(socket, summary.DebugFacts)
}

func finalizeWSSSocketSummary(socket *wssSocketSummary) {
	socket.SocketKey = wssSocketKey(socket)
	socket.RootRequests = socket.RequestShapes["root"]
	socket.DeltaRequests = socket.RequestShapes["delta"]
	socket.FullHistoryRequests = socket.RequestShapes["full_history"]
	socket.UnknownShapeRequests = socket.RequestShapes["unknown"]
	if socket.ProviderInputTokens > 0 {
		socket.ProviderCachedRatio = float64(socket.ProviderCachedTokens) / float64(socket.ProviderInputTokens)
	}
	if (socket.SocketSeq > 1 || socket.SocketInstance > 1) && socket.FullHistoryRequests > 0 {
		socket.ReconnectFullHistory = true
		socket.ReconnectFullHistoryProviderInput = socket.FullHistoryProviderInputTokens
	}
}

func wssSummaryStartsNewSocketInstance(socket *wssSocketSummary, summary dbg.RequestSummary) bool {
	if socket == nil || summary.Timestamp.IsZero() || socket.estimatedClosedAt.IsZero() {
		return false
	}
	return summary.Timestamp.After(socket.estimatedClosedAt.Add(2 * time.Second))
}

func classifyWSSSocketSummary(socket *wssSocketSummary) {
	switch {
	case socket.ReconnectFullHistory:
		classifyWSSFullHistoryReconnect(socket)
	case socket.CloseInitiator == "our_error" || socket.CloseInitiator == "context_cancel":
		socket.Cause = "local_lifecycle_close"
		socket.CauseReason = "local bridge ended the socket without a full-history resend in the observed window"
		socket.Actionable = true
		socket.RecommendedAction = "inspect local lifecycle path first; keep mutation fail-open until root cause is proven"
	case socket.CloseInitiator == "upstream_error":
		socket.Cause = "upstream_error_close"
		socket.CauseReason = "upstream-side transport error closed the socket without a full-history resend in the observed window"
		socket.Actionable = true
		socket.RecommendedAction = "correlate with upstream error frames and retry/recovery telemetry before changing reducers"
	case socket.CloseInitiator == "client_error":
		socket.Cause = "client_transport_error"
		socket.CauseReason = "client-side transport error closed the socket without a full-history resend in the observed window"
		socket.Actionable = true
		socket.RecommendedAction = "correlate with Codex process lifetime and app-server shim logs"
	case socket.CloseInitiator == "upstream_eof":
		socket.Cause = "upstream_clean_close"
		socket.CauseReason = "upstream closed cleanly and no full-history reconnect cost was observed"
		socket.RecommendedAction = "monitor idle lifetime distribution; consider keepalive only if later samples show full-history reconnect cost"
	case socket.CloseInitiator == "client_eof" && socketHasOnlyRootDeltaShapes(socket):
		socket.Cause = "client_delta_safe_close"
		socket.CauseReason = "client closed cleanly and subsequent observed requests stayed root/delta, not full-history"
		socket.RecommendedAction = "no transport fix from this sample; continue collecting longer Desktop sessions"
	case socket.CloseInitiator == "client_eof":
		socket.Cause = "client_clean_close"
		socket.CauseReason = "client closed cleanly without observed full-history reconnect cost"
		socket.RecommendedAction = "monitor shape mix before attributing savings loss to reconnects"
	case !socket.Closed:
		socket.Cause = "open_or_missing_close"
		socket.CauseReason = "socket has request rows but no persisted close facts yet"
		socket.RecommendedAction = "rerun after socket close or verify lifecycle fact persistence"
	default:
		socket.Cause = "unclassified_close"
		socket.CauseReason = "socket close facts did not match a known class"
		socket.Actionable = true
		socket.RecommendedAction = "inspect raw content-free debug facts and extend classifier before changing transport behavior"
	}
}

func classifyWSSFullHistoryReconnect(socket *wssSocketSummary) {
	socket.Actionable = true
	switch socket.CloseInitiator {
	case "client_eof":
		socket.Cause = "client_full_history_reconnect"
		socket.CauseReason = "client closed or re-opened the socket and the later socket carried full-history input"
		socket.RecommendedAction = "verify whether previous_response_id is lost across client reconnect; preserve delta path before considering compression"
	case "upstream_eof":
		socket.Cause = "upstream_full_history_reconnect"
		socket.CauseReason = "upstream clean close was followed by full-history input on a later socket"
		socket.RecommendedAction = "evaluate protocol-legal ping/pong keepalive only if repeated samples confirm this cost"
	case "our_error", "context_cancel":
		socket.Cause = "local_full_history_reconnect"
		socket.CauseReason = "local lifecycle ended the socket and the later socket carried full-history input"
		socket.RecommendedAction = "fix local close/error path before changing reducers; this is the highest-signal zero-drawdown savings candidate"
	case "client_error":
		socket.Cause = "client_error_full_history_reconnect"
		socket.CauseReason = "client-side transport error coincided with full-history input on a later socket"
		socket.RecommendedAction = "correlate with Codex process/app-server lifecycle before transport changes"
	case "upstream_error":
		socket.Cause = "upstream_error_full_history_reconnect"
		socket.CauseReason = "upstream transport error coincided with full-history input on a later socket"
		socket.RecommendedAction = "correlate with upstream error frames and recovery retry decisions"
	default:
		socket.Cause = "full_history_reconnect"
		socket.CauseReason = "a later socket carried full-history input after a reconnect-like boundary"
		socket.RecommendedAction = "classify close initiator before implementing a fix"
	}
}

func socketHasOnlyRootDeltaShapes(socket *wssSocketSummary) bool {
	if socket.Requests == 0 || len(socket.RequestShapes) == 0 {
		return false
	}
	for shape := range socket.RequestShapes {
		if shape != "root" && shape != "delta" {
			return false
		}
	}
	return true
}

func mergeWSSSocketCloseFacts(socket *wssSocketSummary, facts map[string]string) {
	if facts["wss.socket_closed"] == "true" {
		socket.Closed = true
	}
	if v := strings.TrimSpace(facts["wss.socket_close_initiator"]); v != "" {
		socket.Closed = true
		socket.CloseInitiator = v
	}
	if v := strings.TrimSpace(facts["wss.socket_close_error"]); v != "" {
		socket.CloseError = v
	}
	socket.AgeMillis = maxInt64(socket.AgeMillis, parseDebugFactInt64(facts, "wss.socket_age_ms"))
	socket.C2SFrames = maxInt64(socket.C2SFrames, parseDebugFactInt64(facts, "wss.socket_c2s_frames"))
	socket.S2CFrames = maxInt64(socket.S2CFrames, parseDebugFactInt64(facts, "wss.socket_s2c_frames"))
	socket.C2SBytes = maxInt64(socket.C2SBytes, parseDebugFactInt64(facts, "wss.socket_c2s_bytes"))
	socket.S2CBytes = maxInt64(socket.S2CBytes, parseDebugFactInt64(facts, "wss.socket_s2c_bytes"))
	socket.TurnsCompleted = maxInt64(socket.TurnsCompleted, parseDebugFactInt64(facts, "wss.socket_turns_completed"))
	if socket.AgeMillis > 0 && !socket.FirstTimestamp.IsZero() {
		socket.estimatedClosedAt = socket.FirstTimestamp.Add(time.Duration(socket.AgeMillis) * time.Millisecond)
	}
}

func wssSocketBaseKey(sessionID string, seq uint64) string {
	return strings.TrimSpace(sessionID) + "#" + strconv.FormatUint(seq, 10)
}

func wssSocketKey(socket *wssSocketSummary) string {
	session := emptyDash(socket.SessionID)
	return fmt.Sprintf("%s#%d.%d", session, socket.SocketSeq, socket.SocketInstance)
}

func wssSocketSeq(facts map[string]string) (uint64, bool) {
	if facts == nil {
		return 0, false
	}
	seq, err := strconv.ParseUint(strings.TrimSpace(facts["wss.socket_seq"]), 10, 64)
	if err != nil || seq == 0 {
		return 0, false
	}
	return seq, true
}

func parseDebugFactInt64(facts map[string]string, key string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(facts[key]), 10, 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

func parseWSSSocketSince(raw string, now time.Time) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, fmt.Errorf("since must be RFC3339 time or duration")
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return time.Time{}, fmt.Errorf("since must be RFC3339 time or positive duration")
	}
	return now.Add(-duration), nil
}

func parseNonNegativeWSSSocketLimit(name string, raw string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return n, nil
}

func evaluateWSSSocketGate(report wssSocketReport, opts wssSocketDebugArgs) []string {
	var violations []string
	if opts.MaxActionableSockets >= 0 && report.ActionableSockets > opts.MaxActionableSockets {
		violations = append(violations, fmt.Sprintf("actionable_sockets=%d > %d", report.ActionableSockets, opts.MaxActionableSockets))
	}
	if opts.MaxReconnectFullHistoryRequests >= 0 &&
		report.ReconnectFullHistoryRequests > opts.MaxReconnectFullHistoryRequests {
		violations = append(violations, fmt.Sprintf("reconnect_full_history_requests=%d > %d",
			report.ReconnectFullHistoryRequests, opts.MaxReconnectFullHistoryRequests))
	}
	if opts.MaxReconnectFullHistoryInputTokens >= 0 &&
		report.ReconnectFullHistoryProviderInputTokens > opts.MaxReconnectFullHistoryInputTokens {
		violations = append(violations, fmt.Sprintf("reconnect_full_history_provider_input_tokens=%d > %d",
			report.ReconnectFullHistoryProviderInputTokens, opts.MaxReconnectFullHistoryInputTokens))
	}
	return violations
}

func positiveInt(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func maxInt64(a, b int64) int64 {
	if b > a {
		return b
	}
	return a
}

func printWSSSocketReport(report wssSocketReport, jsonOut bool) {
	if jsonOut {
		b, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(b))
		return
	}
	if report.SocketCount == 0 {
		fmt.Println("No WSS socket records found.")
		return
	}
	fmt.Printf("WSS socket lifecycle (%d socket(s), %d request(s))\n", report.SocketCount, report.WSSRequests)
	fmt.Println(strings.Repeat("-", 50))
	if report.SessionFilter != "" || !report.Since.IsZero() || report.RequestsFiltered > 0 {
		fmt.Printf("filters=session:%s since:%s filtered:%d scanned:%d\n",
			emptyDash(report.SessionFilter), formatWSSSocketSince(report.Since), report.RequestsFiltered, report.RequestsScanned)
	}
	fmt.Printf("closed=%d provider_input=%d provider_cached=%d local_saved=%d full_history=%d reconnect_full_history=%d\n",
		report.ClosedSockets,
		report.ProviderInputTokens,
		report.ProviderCachedTokens,
		report.LocalSavedTokens,
		report.FullHistoryRequests,
		report.ReconnectFullHistoryRequests)
	if len(report.CloseInitiators) > 0 {
		fmt.Printf("close_initiators=%s\n", formatWSSSocketShapeCounts(report.CloseInitiators))
	}
	if len(report.CauseClasses) > 0 {
		fmt.Printf("causes=%s actionable=%d\n", formatWSSSocketShapeCounts(report.CauseClasses), report.ActionableSockets)
	}
	for _, socket := range report.Sockets {
		fmt.Printf("socket=%s seq=%d session=%s close=%s cause=%s age_ms=%d requests=%d shapes=%s input=%d cached=%d full_history_input=%d turns=%d\n",
			socket.SocketKey,
			socket.SocketSeq,
			emptyDash(socket.SessionID),
			emptyDash(socket.CloseInitiator),
			socket.Cause,
			socket.AgeMillis,
			socket.Requests,
			formatWSSSocketShapeCounts(socket.RequestShapes),
			socket.ProviderInputTokens,
			socket.ProviderCachedTokens,
			socket.FullHistoryProviderInputTokens,
			socket.TurnsCompleted)
	}
}

func formatWSSSocketSince(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format(time.RFC3339)
}

func formatWSSSocketShapeCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", key, counts[key]))
	}
	return strings.Join(parts, ",")
}

func emptyDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
)

func TestParseWSSSocketDebugArgs(t *testing.T) {
	opts, err := parseWSSSocketDebugArgs([]string{"25", "--json"})
	if err != nil {
		t.Fatalf("parse args: %v", err)
	}
	if opts.Limit != 25 || !opts.JSONOut {
		t.Fatalf("opts=%+v", opts)
	}
	opts, err = parseWSSSocketDebugArgs([]string{"--limit=999999"})
	if err != nil {
		t.Fatalf("parse clamp: %v", err)
	}
	if opts.Limit != maxWSSSocketRequestLimit {
		t.Fatalf("limit=%d want %d", opts.Limit, maxWSSSocketRequestLimit)
	}
	opts, err = parseWSSSocketDebugArgs([]string{
		"--session", "codex-wss:abc",
		"--since=2026-06-11T10:00:00Z",
		"--fail-on-actionable",
		"--fail-on-full-history",
		"--max-reconnect-full-history-input=123",
	})
	if err != nil {
		t.Fatalf("parse filters/gates: %v", err)
	}
	if opts.SessionFilter != "codex-wss:abc" ||
		opts.Since.Format(time.RFC3339) != "2026-06-11T10:00:00Z" ||
		opts.MaxActionableSockets != 0 ||
		opts.MaxReconnectFullHistoryRequests != 0 ||
		opts.MaxReconnectFullHistoryInputTokens != 123 {
		t.Fatalf("filters/gates mismatch: %+v", opts)
	}
	sinceFile := filepath.Join(t.TempDir(), "since.txt")
	if err := os.WriteFile(sinceFile, []byte("2026-06-11T10:01:02Z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts, err = parseWSSSocketDebugArgs([]string{"--since-file", sinceFile})
	if err != nil {
		t.Fatalf("parse since-file: %v", err)
	}
	if opts.SinceFile != sinceFile || opts.Since.Format(time.RFC3339) != "2026-06-11T10:01:02Z" {
		t.Fatalf("since-file mismatch: %+v", opts)
	}
	for _, args := range [][]string{
		{"--bad"},
		{"0"},
		{"--limit=0"},
		{"--limit"},
		{"1", "2"},
		{"--session="},
		{"--session"},
		{"--since=bad"},
		{"--since=-1h"},
		{"--since-file"},
		{"--since-file="},
		{"--max-actionable=-1"},
	} {
		if _, err := parseWSSSocketDebugArgs(args); err == nil {
			t.Fatalf("args %v should fail", args)
		}
	}
}

func TestParseWSSSocketSinceDuration(t *testing.T) {
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	got, err := parseWSSSocketSince("2h", now)
	if err != nil {
		t.Fatalf("parse duration: %v", err)
	}
	if want := now.Add(-2 * time.Hour); !got.Equal(want) {
		t.Fatalf("since duration=%s want %s", got, want)
	}
}

func TestParseWSSSocketSinceFileErrors(t *testing.T) {
	if _, err := parseWSSSocketSinceFile(filepath.Join(t.TempDir(), "missing.txt")); err == nil || !strings.Contains(err.Error(), "read --since-file") {
		t.Fatalf("missing since-file error=%v", err)
	}
	empty := filepath.Join(t.TempDir(), "empty.txt")
	if err := os.WriteFile(empty, []byte(" \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := parseWSSSocketSinceFile(empty); err == nil || !strings.Contains(err.Error(), "must contain RFC3339") {
		t.Fatalf("empty since-file error=%v", err)
	}
	bad := filepath.Join(t.TempDir(), "bad.txt")
	if err := os.WriteFile(bad, []byte("2h\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := parseWSSSocketSinceFile(bad); err == nil || !strings.Contains(err.Error(), "must contain RFC3339") {
		t.Fatalf("bad since-file error=%v", err)
	}
}

func TestBuildWSSSocketReportCorrelatesReconnectFullHistory(t *testing.T) {
	base := time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)
	report := buildWSSSocketReport("decisions.jsonl", 100, []dbg.RequestSummary{
		wssSocketTestSummary("req-3", "codex-wss:thread", 2, "full_history", base.Add(3*time.Second), 9000, 0, 300, map[string]string{
			"wss.socket_closed":          "true",
			"wss.socket_close_initiator": "upstream_eof",
			"wss.socket_age_ms":          "4500",
			"wss.socket_turns_completed": "1",
		}),
		wssSocketTestSummary("req-2", "codex-wss:thread", 1, "delta", base.Add(2*time.Second), 700, 500, 50, nil),
		wssSocketTestSummary("req-1", "codex-wss:thread", 1, "root", base.Add(time.Second), 1100, 0, 100, map[string]string{
			"wss.socket_closed":          "true",
			"wss.socket_close_initiator": "client_eof",
			"wss.socket_age_ms":          "3100",
			"wss.socket_c2s_frames":      "2",
			"wss.socket_s2c_frames":      "29",
			"wss.socket_c2s_bytes":       "600",
			"wss.socket_s2c_bytes":       "12000",
			"wss.socket_turns_completed": "2",
		}),
		{RequestID: "non-wss", Timestamp: base, ProviderInputTokens: 9999},
	})
	if report.RequestsScanned != 4 || report.WSSRequests != 3 || report.SocketCount != 2 || report.ClosedSockets != 2 {
		t.Fatalf("counts mismatch: %+v", report)
	}
	if report.ProviderInputTokens != 10800 || report.ProviderCachedTokens != 500 || report.LocalSavedTokens != 450 {
		t.Fatalf("tokens mismatch: %+v", report)
	}
	if report.FullHistoryRequests != 1 || report.ReconnectFullHistoryRequests != 1 || report.ReconnectFullHistoryProviderInputTokens != 9000 {
		t.Fatalf("reconnect full-history mismatch: %+v", report)
	}
	if report.CloseInitiators["client_eof"] != 1 || report.CloseInitiators["upstream_eof"] != 1 {
		t.Fatalf("initiators mismatch: %+v", report.CloseInitiators)
	}
	if report.CauseClasses["client_full_history_reconnect"] != 1 ||
		report.CauseClasses["client_delta_safe_close"] != 1 ||
		report.ActionableSockets != 1 {
		t.Fatalf("cause classes mismatch: causes=%+v actionable=%d", report.CauseClasses, report.ActionableSockets)
	}
	if report.Sockets[0].SocketSeq != 2 || !report.Sockets[0].ReconnectFullHistory {
		t.Fatalf("newest reconnect socket first: %+v", report.Sockets)
	}
	if report.Sockets[0].ReconnectPreviousCloseInitiator != "client_eof" ||
		report.Sockets[0].ReconnectPreviousSocketKey != "codex-wss:thread#1.1" ||
		report.Sockets[0].ReconnectAttribution != "observed_previous_socket" {
		t.Fatalf("reconnect attribution mismatch: %+v", report.Sockets[0])
	}
	if report.Sockets[1].RootRequests != 1 || report.Sockets[1].DeltaRequests != 1 || report.Sockets[1].C2SFrames != 2 {
		t.Fatalf("socket 1 aggregation mismatch: %+v", report.Sockets[1])
	}
	if len(report.ReconnectFullHistoryByCause) != 1 {
		t.Fatalf("missing reconnect cause summary: %+v", report.ReconnectFullHistoryByCause)
	}
	cause := report.ReconnectFullHistoryByCause[0]
	if cause.Cause != "client_full_history_reconnect" ||
		cause.Requests != 1 ||
		cause.ReconnectInputTokens != 9000 ||
		cause.RetryResendCost != 9000 ||
		!containsString(cause.PreviousInitiators, "client_eof") ||
		!containsString(cause.Candidates, "t417_stateless_or_lineage_reroute") {
		t.Fatalf("reconnect cause summary mismatch: %+v", cause)
	}
	if len(report.T417ReconnectHandoff) != 1 {
		t.Fatalf("missing T417 reconnect handoff: %+v", report.T417ReconnectHandoff)
	}
	handoff := report.T417ReconnectHandoff[0]
	if handoff.SocketKey != "codex-wss:thread#2.1" ||
		handoff.PreviousSocketKey != "codex-wss:thread#1.1" ||
		handoff.PreviousClose != "client_eof" ||
		handoff.ReconnectInputTokens != 9000 ||
		handoff.RetryResendCost != 9000 ||
		handoff.ContinuationCandidate != "t417_stateless_or_lineage_reroute" ||
		handoff.RequestShapes["full_history"] != 1 {
		t.Fatalf("T417 reconnect handoff mismatch: %+v", handoff)
	}
}

func TestBuildWSSSocketReportSplitsReusedSocketSeqAfterClose(t *testing.T) {
	base := time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)
	report := buildWSSSocketReport("decisions.jsonl", 100, []dbg.RequestSummary{
		wssSocketTestSummary("req-1", "codex-wss:thread", 1, "root", base, 1000, 0, 0, map[string]string{
			"wss.socket_closed":          "true",
			"wss.socket_close_initiator": "client_eof",
			"wss.socket_age_ms":          "1000",
		}),
		wssSocketTestSummary("req-2", "codex-wss:thread", 1, "full_history", base.Add(10*time.Second), 7000, 0, 0, map[string]string{
			"wss.socket_closed":          "true",
			"wss.socket_close_initiator": "client_eof",
			"wss.socket_age_ms":          "1000",
		}),
	})
	if report.SocketCount != 2 || report.ClosedSockets != 2 {
		t.Fatalf("reused seq should split into two closed sockets: %+v", report)
	}
	if report.ReconnectFullHistoryRequests != 1 || report.ReconnectFullHistoryProviderInputTokens != 7000 {
		t.Fatalf("second instance full-history should count as reconnect cost: %+v", report)
	}
	if report.CauseClasses["client_full_history_reconnect"] != 1 || report.ActionableSockets != 1 {
		t.Fatalf("second instance should classify as actionable client full-history reconnect: %+v", report)
	}
	if report.Sockets[0].SocketInstance != 2 || !strings.Contains(report.Sockets[0].SocketKey, "#1.2") {
		t.Fatalf("newest socket should be second instance: %+v", report.Sockets)
	}
	if report.Sockets[0].ReconnectGapMillis != 9000 ||
		report.Sockets[0].ReconnectPreviousCloseInitiator != "client_eof" {
		t.Fatalf("reused seq reconnect attribution mismatch: %+v", report.Sockets[0])
	}
}

func TestBuildWSSSocketReportDoesNotAttributeReconnectToCurrentClose(t *testing.T) {
	base := time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)
	report := buildWSSSocketReport("decisions.jsonl", 100, []dbg.RequestSummary{
		wssSocketTestSummary("req-2", "codex-wss:thread", 2, "full_history", base.Add(time.Second), 7000, 0, 0, map[string]string{
			"wss.socket_closed":          "true",
			"wss.socket_close_initiator": "client_eof",
			"wss.socket_age_ms":          "1000",
		}),
	})
	if report.ReconnectFullHistoryRequests != 1 || report.ActionableSockets != 1 {
		t.Fatalf("reconnect should still be gated: %+v", report)
	}
	if report.CauseClasses["full_history_reconnect"] != 1 {
		t.Fatalf("unobserved previous close should stay unattributed: %+v", report.CauseClasses)
	}
	got := report.Sockets[0]
	if got.ReconnectPreviousCloseInitiator != "" ||
		got.ReconnectAttribution != "unobserved_previous_socket" ||
		got.Cause == "client_full_history_reconnect" {
		t.Fatalf("current close was incorrectly used as reconnect cause: %+v", got)
	}
}

func TestBuildWSSSocketReportFiltersBySessionAndSince(t *testing.T) {
	base := time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)
	report := buildWSSSocketReportWithOptions("decisions.jsonl", []dbg.RequestSummary{
		wssSocketTestSummary("old", "codex-wss:keep", 1, "root", base, 1000, 0, 0, nil),
		wssSocketTestSummary("other", "codex-wss:drop", 2, "full_history", base.Add(2*time.Hour), 9000, 0, 0, nil),
		wssSocketTestSummary("new", "codex-wss:keep", 3, "delta", base.Add(2*time.Hour), 700, 100, 10, nil),
	}, wssSocketDebugArgs{
		Limit:         100,
		SessionFilter: "codex-wss:keep",
		Since:         base.Add(time.Hour),
	})
	if report.RequestsScanned != 3 || report.RequestsFiltered != 2 || report.WSSRequests != 1 || report.SocketCount != 1 {
		t.Fatalf("filter counts mismatch: %+v", report)
	}
	if report.ProviderInputTokens != 700 || report.ProviderCachedTokens != 100 {
		t.Fatalf("filter token mismatch: %+v", report)
	}
	if report.Sockets[0].LastRequestID != "new" || report.Sockets[0].Cause != "open_or_missing_close" {
		t.Fatalf("filtered socket mismatch: %+v", report.Sockets[0])
	}
}

func TestClassifyWSSSocketSummaryBranches(t *testing.T) {
	base := wssClassifySocketBase()
	tests := []struct {
		name       string
		mutate     func(*wssSocketSummary)
		wantCause  string
		actionable bool
	}{
		{
			name: "local lifecycle close",
			mutate: func(s *wssSocketSummary) {
				s.CloseInitiator = "our_error"
			},
			wantCause:  "local_lifecycle_close",
			actionable: true,
		},
		{
			name: "context cancel local lifecycle",
			mutate: func(s *wssSocketSummary) {
				s.CloseInitiator = "context_cancel"
			},
			wantCause:  "local_lifecycle_close",
			actionable: true,
		},
		{
			name: "upstream error",
			mutate: func(s *wssSocketSummary) {
				s.CloseInitiator = "upstream_error"
			},
			wantCause:  "upstream_error_close",
			actionable: true,
		},
		{
			name: "client error",
			mutate: func(s *wssSocketSummary) {
				s.CloseInitiator = "client_error"
			},
			wantCause:  "client_transport_error",
			actionable: true,
		},
		{
			name: "upstream clean",
			mutate: func(s *wssSocketSummary) {
				s.CloseInitiator = "upstream_eof"
			},
			wantCause: "upstream_clean_close",
		},
		{
			name: "client clean unknown shape",
			mutate: func(s *wssSocketSummary) {
				s.CloseInitiator = "client_eof"
				s.RequestShapes = map[string]int{"unknown": 1}
			},
			wantCause: "client_clean_close",
		},
		{
			name: "open socket",
			mutate: func(s *wssSocketSummary) {
				s.Closed = false
				s.CloseInitiator = ""
			},
			wantCause: "open_or_missing_close",
		},
		{
			name: "unclassified closed",
			mutate: func(s *wssSocketSummary) {
				s.CloseInitiator = ""
			},
			wantCause:  "unclassified_close",
			actionable: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			socket := base
			socket.RequestShapes = cloneWSSSocketShapes(base.RequestShapes)
			tt.mutate(&socket)
			classifyWSSSocketSummary(&socket)
			if socket.Cause != tt.wantCause || socket.Actionable != tt.actionable || socket.CauseReason == "" {
				t.Fatalf("classification mismatch: got cause=%q actionable=%v reason=%q", socket.Cause, socket.Actionable, socket.CauseReason)
			}
		})
	}
}

func TestClassifyWSSFullHistoryReconnectBranches(t *testing.T) {
	tests := []struct {
		initiator string
		wantCause string
	}{
		{initiator: "client_eof", wantCause: "client_full_history_reconnect"},
		{initiator: "upstream_eof", wantCause: "upstream_full_history_reconnect"},
		{initiator: "our_error", wantCause: "local_full_history_reconnect"},
		{initiator: "context_cancel", wantCause: "local_full_history_reconnect"},
		{initiator: "client_error", wantCause: "client_error_full_history_reconnect"},
		{initiator: "upstream_error", wantCause: "upstream_error_full_history_reconnect"},
		{initiator: "", wantCause: "full_history_reconnect"},
	}
	for _, tt := range tests {
		t.Run(tt.wantCause, func(t *testing.T) {
			socket := wssClassifySocketBase()
			socket.SocketSeq = 2
			socket.FullHistoryRequests = 1
			socket.FullHistoryProviderInputTokens = 7000
			socket.ReconnectFullHistory = true
			socket.CloseInitiator = tt.initiator
			classifyWSSSocketSummary(&socket)
			if socket.Cause != tt.wantCause || !socket.Actionable || socket.RecommendedAction == "" {
				t.Fatalf("classification mismatch: %+v", socket)
			}
		})
	}
}

func TestWSSSocketSmallHelpers(t *testing.T) {
	if _, ok := wssSocketSeq(nil); ok {
		t.Fatal("nil facts should not have socket seq")
	}
	if _, ok := wssSocketSeq(map[string]string{"wss.socket_seq": "bad"}); ok {
		t.Fatal("bad socket seq should not parse")
	}
	if positiveInt(-1) != 0 {
		t.Fatal("negative token count should clamp to zero")
	}
	empty := wssSocketSummary{Requests: 0, RequestShapes: map[string]int{"root": 1}}
	if socketHasOnlyRootDeltaShapes(&empty) {
		t.Fatal("empty request count should not be root/delta safe")
	}
	mixed := wssSocketSummary{Requests: 2, RequestShapes: map[string]int{"root": 1, "full_history": 1}}
	if socketHasOnlyRootDeltaShapes(&mixed) {
		t.Fatal("full_history shape should not be root/delta safe")
	}
	if got := cloneWSSSocketShapeCounts(nil); got != nil {
		t.Fatalf("nil shape clone should stay nil: %+v", got)
	}
	if got := formatWSSSocketSince(time.Time{}); got != "-" {
		t.Fatalf("zero since formatting=%q", got)
	}
	when := time.Date(2026, 6, 20, 12, 34, 56, 0, time.UTC)
	if got := formatWSSSocketSince(when); got != "2026-06-20T12:34:56Z" {
		t.Fatalf("since formatting=%q", got)
	}
	if got := formatWSSReconnectCauseSummaries(nil); got != "-" {
		t.Fatalf("empty reconnect cause formatting=%q", got)
	}
}

func TestBuildWSSReconnectHandoffRanksCausesAndCandidates(t *testing.T) {
	sockets := []wssSocketSummary{
		{
			SocketKey:                         "local#3.1",
			SessionID:                         "local",
			RequestShapes:                     map[string]int{"full_history": 2},
			Requests:                          2,
			FullHistoryRequests:               2,
			ProviderInputTokens:               15000,
			ProviderCachedTokens:              1000,
			LocalSavedTokens:                  250,
			ReconnectFullHistory:              true,
			ReconnectFullHistoryProviderInput: 15000,
			ReconnectPreviousSocketKey:        "local#2.1",
			ReconnectPreviousCloseInitiator:   "our_error",
			ReconnectGapMillis:                42,
			ReconnectAttribution:              "observed_previous_socket",
			Cause:                             "local_full_history_reconnect",
			RecommendedAction:                 "fix local close path",
		},
		{
			SocketKey:                         "upstream#2.1",
			SessionID:                         "upstream",
			RequestShapes:                     map[string]int{"full_history": 1},
			Requests:                          1,
			FullHistoryRequests:               1,
			ProviderInputTokens:               7000,
			ProviderCachedTokens:              0,
			LocalSavedTokens:                  70,
			ReconnectFullHistory:              true,
			ReconnectFullHistoryProviderInput: 7000,
			ReconnectPreviousCloseInitiator:   "upstream_error",
			Cause:                             "upstream_error_full_history_reconnect",
		},
		{
			SocketKey:                         "unknown#2.1",
			SessionID:                         "unknown",
			RequestShapes:                     map[string]int{"full_history": 1},
			Requests:                          1,
			FullHistoryRequests:               1,
			ProviderInputTokens:               100,
			ReconnectFullHistory:              true,
			ReconnectFullHistoryProviderInput: 100,
			Cause:                             "unexpected_future_class",
		},
		{
			SocketKey: "delta#1.1",
			Cause:     "client_delta_safe_close",
		},
	}

	summaries := buildWSSReconnectCauseSummaries(sockets)
	if len(summaries) != 3 {
		t.Fatalf("summaries=%+v", summaries)
	}
	if summaries[0].Cause != "local_full_history_reconnect" ||
		summaries[0].Requests != 2 ||
		summaries[0].RetryResendCost != 15000 ||
		!containsString(summaries[0].PreviousInitiators, "our_error") ||
		!containsString(summaries[0].Candidates, "t420_local_lifecycle_fix") {
		t.Fatalf("local summary mismatch: %+v", summaries[0])
	}
	if summaries[1].Cause != "upstream_error_full_history_reconnect" ||
		!containsString(summaries[1].Candidates, "t420_upstream_keepalive_or_recovery") {
		t.Fatalf("upstream summary mismatch: %+v", summaries[1])
	}
	if summaries[2].Cause != "unexpected_future_class" ||
		!containsString(summaries[2].Candidates, "classify_before_reroute") {
		t.Fatalf("unknown summary mismatch: %+v", summaries[2])
	}
	if got := formatWSSReconnectCauseSummaries(summaries); !strings.Contains(got, "local_full_history_reconnect:2/15000") {
		t.Fatalf("formatted summaries missing local mass: %q", got)
	}

	handoff := buildWSSReconnectT417Handoff(sockets)
	if len(handoff) != 3 {
		t.Fatalf("handoff=%+v", handoff)
	}
	if handoff[0].ContinuationCandidate != "t420_local_lifecycle_fix" ||
		handoff[0].RetryResendCost != 15000 ||
		handoff[0].RequestShapes["full_history"] != 2 ||
		handoff[0].RecommendedAction != "fix local close path" {
		t.Fatalf("local handoff mismatch: %+v", handoff[0])
	}
	handoff[0].RequestShapes["full_history"] = 99
	if sockets[0].RequestShapes["full_history"] != 2 {
		t.Fatalf("handoff mutated source request shapes: %+v", sockets[0].RequestShapes)
	}
}

func TestEvaluateWSSSocketGate(t *testing.T) {
	report := wssSocketReport{
		ActionableSockets:                       2,
		ReconnectFullHistoryRequests:            1,
		ReconnectFullHistoryProviderInputTokens: 7000,
	}
	violations := evaluateWSSSocketGate(report, wssSocketDebugArgs{
		MaxActionableSockets:               1,
		MaxReconnectFullHistoryRequests:    0,
		MaxReconnectFullHistoryInputTokens: 6000,
	})
	if len(violations) != 3 {
		t.Fatalf("violations=%v", violations)
	}
	if got := evaluateWSSSocketGate(report, wssSocketDebugArgs{
		MaxActionableSockets:               2,
		MaxReconnectFullHistoryRequests:    1,
		MaxReconnectFullHistoryInputTokens: 7000,
	}); len(got) != 0 {
		t.Fatalf("unexpected gate violations: %v", got)
	}
}

func TestHandleDebugWSSSocketsTextAndJSON(t *testing.T) {
	tmp := t.TempDir()
	decisionsPath := filepath.Join(tmp, "decisions.jsonl")
	lines := []string{
		mustJSONLine(t, wssSocketTestSummary("req-1", "codex-wss:thread", 1, "root", time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC), 1000, 100, 20, map[string]string{
			"wss.socket_closed":          "true",
			"wss.socket_close_initiator": "client_eof",
			"wss.socket_age_ms":          "500",
		})),
		mustJSONLine(t, wssSocketTestSummary("req-2", "codex-wss:thread", 2, "full_history", time.Date(2026, 6, 11, 10, 0, 1, 0, time.UTC), 8000, 0, 120, map[string]string{
			"wss.socket_closed":          "true",
			"wss.socket_close_initiator": "client_eof",
			"wss.socket_age_ms":          "2500",
		})),
	}
	if err := os.WriteFile(decisionsPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", decisionsPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(tmp, "missing.toml"))

	text := captureWSSSocketStdout(t, func() { handleDebugWSSSockets([]string{"20"}) })
	for _, want := range []string{
		"WSS socket lifecycle",
		"seq=2",
		"reconnect_full_history=1",
		"cause=client_full_history_reconnect",
		"t417_handoff rows=1 reconnect_input=8000",
		"handoff socket=codex-wss:thread#2.1 cause=client_full_history_reconnect",
		"candidate=t417_stateless_or_lineage_reroute",
		"reconnect_prev=codex-wss:thread#1.1",
		"shapes=full_history:1",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text output missing %q:\n%s", want, text)
		}
	}

	jsonOut := captureWSSSocketStdout(t, func() { handleDebugWSSSockets([]string{"20", "--json"}) })
	var report wssSocketReport
	if err := json.Unmarshal([]byte(jsonOut), &report); err != nil {
		t.Fatalf("json output: %v\n%s", err, jsonOut)
	}
	if report.SocketCount != 2 || report.ReconnectFullHistoryRequests != 1 || report.ProviderInputTokens != 9000 {
		t.Fatalf("json report mismatch: %+v", report)
	}
	if report.Sockets[0].Cause != "client_full_history_reconnect" || !report.Sockets[0].Actionable {
		t.Fatalf("json cause mismatch: %+v", report.Sockets[0])
	}
	if report.Sockets[0].ReconnectPreviousCloseInitiator != "client_eof" {
		t.Fatalf("json reconnect attribution mismatch: %+v", report.Sockets[0])
	}
	if len(report.ReconnectFullHistoryByCause) != 1 ||
		report.ReconnectFullHistoryByCause[0].ReconnectInputTokens != 8000 {
		t.Fatalf("json reconnect cause summary mismatch: %+v", report.ReconnectFullHistoryByCause)
	}
	if len(report.T417ReconnectHandoff) != 1 ||
		report.T417ReconnectHandoff[0].ContinuationCandidate != "t417_stateless_or_lineage_reroute" {
		t.Fatalf("json T417 handoff mismatch: %+v", report.T417ReconnectHandoff)
	}
}

func TestHandleDebugWSSSocketsSinceFileFiltersProofWindow(t *testing.T) {
	tmp := t.TempDir()
	decisionsPath := filepath.Join(tmp, "decisions.jsonl")
	lines := []string{
		mustJSONLine(t, wssSocketTestSummary("old-root", "codex-wss:thread", 1, "root", time.Date(2026, 6, 11, 9, 59, 59, 0, time.UTC), 1000, 100, 20, map[string]string{
			"wss.socket_closed":          "true",
			"wss.socket_close_initiator": "client_eof",
		})),
		mustJSONLine(t, wssSocketTestSummary("new-full", "codex-wss:thread", 2, "full_history", time.Date(2026, 6, 11, 10, 0, 1, 0, time.UTC), 8000, 0, 120, map[string]string{
			"wss.socket_closed":          "true",
			"wss.socket_close_initiator": "client_eof",
		})),
	}
	if err := os.WriteFile(decisionsPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sinceFile := filepath.Join(tmp, "proof-since.txt")
	if err := os.WriteFile(sinceFile, []byte("2026-06-11T10:00:00Z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", decisionsPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(tmp, "missing.toml"))

	text := captureWSSSocketStdout(t, func() { handleDebugWSSSockets([]string{"20", "--since-file", sinceFile}) })
	if !strings.Contains(text, "since_file:"+sinceFile) || !strings.Contains(text, "filtered:1") || strings.Contains(text, "old-root") {
		t.Fatalf("since-file text output mismatch:\n%s", text)
	}

	jsonOut := captureWSSSocketStdout(t, func() { handleDebugWSSSockets([]string{"20", "--since-file=" + sinceFile, "--json"}) })
	var report wssSocketReport
	if err := json.Unmarshal([]byte(jsonOut), &report); err != nil {
		t.Fatalf("json output: %v\n%s", err, jsonOut)
	}
	if report.SinceFile != sinceFile ||
		report.Since.Format(time.RFC3339) != "2026-06-11T10:00:00Z" ||
		report.RequestsFiltered != 1 ||
		report.WSSRequests != 1 ||
		report.FullHistoryRequests != 1 {
		t.Fatalf("since-file report mismatch: %+v", report)
	}
}

func TestHandleDebugWSSSocketsNoConfiguredLog(t *testing.T) {
	isolateDebugNoConfig(t)
	text := captureWSSSocketStdout(t, func() { handleDebugWSSSockets(nil) })
	if !strings.Contains(text, "No decisions_log configured") {
		t.Fatalf("missing no-config message: %q", text)
	}
}

func TestHandleDebugWSSSocketsGateExits(t *testing.T) {
	tmp := t.TempDir()
	decisionsPath := filepath.Join(tmp, "decisions.jsonl")
	line := mustJSONLine(t, wssSocketTestSummary("req-1", "codex-wss:thread", 2, "full_history",
		time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC), 8000, 0, 0, map[string]string{
			"wss.socket_closed":          "true",
			"wss.socket_close_initiator": "client_eof",
			"wss.socket_age_ms":          "2500",
		}))
	if err := os.WriteFile(decisionsPath, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", decisionsPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(tmp, "missing.toml"))
	code, exited := captureExit(func() { handleDebugWSSSockets([]string{"--fail-on-actionable"}) })
	if !exited || code != 1 {
		t.Fatalf("gate should exit 1, exited=%v code=%d", exited, code)
	}
}

func wssSocketTestSummary(reqID, sessionID string, socketSeq uint64, shape string, ts time.Time, input, cached, saved int, extra map[string]string) dbg.RequestSummary {
	facts := map[string]string{
		"wss.socket_seq":    strconvFormatUint(socketSeq),
		"wss.request_shape": shape,
	}
	maps.Copy(facts, extra)
	return dbg.RequestSummary{
		RequestID:            reqID,
		Timestamp:            ts,
		SessionID:            sessionID,
		RouteMode:            "websocket_phasef",
		Provider:             "chatgpt",
		ProviderInputTokens:  input,
		ProviderCachedTokens: cached,
		Tokens:               dbg.TokenCounts{Saved: saved},
		DebugFacts:           facts,
	}
}

func mustJSONLine(t *testing.T, summary dbg.RequestSummary) string {
	t.Helper()
	b, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func captureWSSSocketStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func wssClassifySocketBase() wssSocketSummary {
	return wssSocketSummary{
		SocketKey:           "session#1.1",
		SocketSeq:           1,
		SocketInstance:      1,
		SessionID:           "session",
		Requests:            2,
		RequestShapes:       map[string]int{"root": 1, "delta": 1},
		Closed:              true,
		CloseInitiator:      "client_eof",
		TurnsCompleted:      2,
		ProviderInputTokens: 1000,
	}
}

func cloneWSSSocketShapes(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	maps.Copy(out, in)
	return out
}

func containsString(values []string, want string) bool {
	return slices.Contains(values, want)
}

func strconvFormatUint(v uint64) string {
	return strconv.FormatUint(v, 10)
}

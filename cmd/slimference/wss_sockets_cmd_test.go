package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
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
	for _, args := range [][]string{
		{"--bad"},
		{"0"},
		{"--limit=0"},
		{"--limit"},
		{"1", "2"},
	} {
		if _, err := parseWSSSocketDebugArgs(args); err == nil {
			t.Fatalf("args %v should fail", args)
		}
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
	if report.CauseClasses["upstream_full_history_reconnect"] != 1 ||
		report.CauseClasses["client_delta_safe_close"] != 1 ||
		report.ActionableSockets != 1 {
		t.Fatalf("cause classes mismatch: causes=%+v actionable=%d", report.CauseClasses, report.ActionableSockets)
	}
	if report.Sockets[0].SocketSeq != 2 || !report.Sockets[0].ReconnectFullHistory {
		t.Fatalf("newest reconnect socket first: %+v", report.Sockets)
	}
	if report.Sockets[1].RootRequests != 1 || report.Sockets[1].DeltaRequests != 1 || report.Sockets[1].C2SFrames != 2 {
		t.Fatalf("socket 1 aggregation mismatch: %+v", report.Sockets[1])
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
}

func TestHandleDebugWSSSocketsTextAndJSON(t *testing.T) {
	tmp := t.TempDir()
	decisionsPath := filepath.Join(tmp, "decisions.jsonl")
	lines := []string{
		mustJSONLine(t, wssSocketTestSummary("req-1", "codex-wss:thread", 1, "root", time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC), 1000, 100, 20, nil)),
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
}

func TestHandleDebugWSSSocketsNoConfiguredLog(t *testing.T) {
	isolateDebugNoConfig(t)
	text := captureWSSSocketStdout(t, func() { handleDebugWSSSockets(nil) })
	if !strings.Contains(text, "No decisions_log configured") {
		t.Fatalf("missing no-config message: %q", text)
	}
}

func wssSocketTestSummary(reqID, sessionID string, socketSeq uint64, shape string, ts time.Time, input, cached, saved int, extra map[string]string) dbg.RequestSummary {
	facts := map[string]string{
		"wss.socket_seq":    strconvFormatUint(socketSeq),
		"wss.request_shape": shape,
	}
	for key, value := range extra {
		facts[key] = value
	}
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
	for key, value := range in {
		out[key] = value
	}
	return out
}

func strconvFormatUint(v uint64) string {
	return strconv.FormatUint(v, 10)
}

package proxy

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/evidence"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func TestWSSABReplayCapture_Record(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "ab.jsonl")
	capture := newWSSABReplayCapture(path)
	if capture == nil {
		t.Fatal("newWSSABReplayCapture returned nil")
	}

	// nil capture -> no-op.
	var nilCapture *wssABReplayCapture
	nilCapture.record(wsmitm.DirClientToServer, nil, false, 0)

	// nil env -> no-op.
	capture.record(wsmitm.DirClientToServer, nil, false, 0)

	// valid env, not mutated, with Raw.
	validPayload := json.RawMessage(`{"type":"response","result":"ok"}`)
	env := &wsmitm.Envelope{
		Kind: wsmitm.FrameKindResponseCompleted,
		Raw:  validPayload,
	}
	capture.record(wsmitm.DirClientToServer, env, false, 1)

	// valid env, not mutated, empty Raw -> Marshal fallback.
	env2 := &wsmitm.Envelope{
		Kind: wsmitm.FrameKindResponseCompleted,
	}
	capture.record(wsmitm.DirServerToClient, env2, false, 2)

	// valid env, mutated -> Marshal.
	env3 := &wsmitm.Envelope{
		Kind: wsmitm.FrameKindResponseCompleted,
		Body: json.RawMessage(`{"data":"mutated"}`),
	}
	capture.record(wsmitm.DirClientToServer, env3, true, 3)

	// Verify file was written.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("capture file should exist: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("capture file should not be empty")
	}
}

func TestWrapRawScopedWSSListener(t *testing.T) {
	t.Parallel()
	// nil proxy -> returns original listener.
	var nilProxy *Proxy
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	if got := nilProxy.wrapRawScopedWSSListener(ln); got != ln {
		t.Fatal("nil proxy should return original listener")
	}

	// proxy without webSocketTunnel -> returns original listener.
	p := &Proxy{}
	if got := p.wrapRawScopedWSSListener(ln); got != ln {
		t.Fatal("proxy without tunnel should return original listener")
	}
}

func TestRecordRawScopedWSS(t *testing.T) {
	t.Parallel()
	// nil proxy -> no-op.
	var nilProxy *Proxy
	nilProxy.recordRawScopedWSS("/path", []byte("header"))

	// proxy without debugRecorder -> no-op.
	p := &Proxy{}
	p.recordRawScopedWSS("/path", []byte("header"))
}

func TestShortChunkID(t *testing.T) {
	t.Parallel()
	if got := shortChunkID("short"); got != "short" {
		t.Fatalf("shortChunkID(\"short\") = %q", got)
	}
	if got := shortChunkID("exactly12ch"); got != "exactly12ch" {
		t.Fatalf("shortChunkID(\"exactly12ch\") = %q", got)
	}
	if got := shortChunkID("thisisaverylongid"); got != "thisisaveryl" {
		t.Fatalf("shortChunkID(\"thisisaverylongid\") = %q, want \"thisisaveryl\"", got)
	}
}

func TestUptimeSeconds(t *testing.T) {
	t.Parallel()
	// nil proxy -> 0.
	var nilProxy *Proxy
	if got := nilProxy.uptimeSeconds(); got != 0 {
		t.Fatalf("nil proxy uptime = %d, want 0", got)
	}
	// zero startedAt -> 0.
	p := &Proxy{}
	if got := p.uptimeSeconds(); got != 0 {
		t.Fatalf("zero startedAt uptime = %d, want 0", got)
	}
	// valid startedAt -> positive.
	p2 := &Proxy{startedAt: time.Now().Add(-5 * time.Second)}
	if got := p2.uptimeSeconds(); got < 1 {
		t.Fatalf("valid startedAt uptime = %d, want >= 1", got)
	}
}

func TestCodexHostBudgetExceeded(t *testing.T) {
	t.Parallel()
	// nil proxy -> false.
	var nilProxy *Proxy
	if nilProxy.codexHostBudgetExceeded() {
		t.Fatal("nil proxy should return false")
	}
	// proxy with flag not set -> false.
	p := &Proxy{}
	if p.codexHostBudgetExceeded() {
		t.Fatal("unset flag should return false")
	}
	// proxy with flag set -> true.
	p2 := &Proxy{}
	p2.hostBudgetExceeded.Store(true)
	if !p2.codexHostBudgetExceeded() {
		t.Fatal("set flag should return true")
	}
}

func TestCodexRuntimeBudgetExceeded(t *testing.T) {
	t.Parallel()
	// nil proxy -> false.
	var nilProxy *Proxy
	if nilProxy.codexRuntimeBudgetExceeded() {
		t.Fatal("nil proxy should return false")
	}
	// proxy with no flags -> false.
	p := &Proxy{}
	if p.codexRuntimeBudgetExceeded() {
		t.Fatal("no flags should return false")
	}
	// proxy with hostBudgetExceeded -> true.
	p2 := &Proxy{}
	p2.hostBudgetExceeded.Store(true)
	if !p2.codexRuntimeBudgetExceeded() {
		t.Fatal("hostBudgetExceeded should return true")
	}
	// proxy with codexLayer0LatencyExceeded -> true.
	p3 := &Proxy{}
	p3.codexLayer0LatencyExceeded.Store(true)
	if !p3.codexRuntimeBudgetExceeded() {
		t.Fatal("codexLayer0LatencyExceeded should return true")
	}
}

func TestRawScopedWSSRouteMode(t *testing.T) {
	t.Parallel()
	// Bridge path -> websocket_raw_bridge.
	if got := rawScopedWSSRouteMode("/backend-api/codex-bridge/responses"); got != "websocket_raw_bridge" {
		t.Fatalf("rawScopedWSSRouteMode(bridge) = %q", got)
	}
	// Non-bridge path -> websocket_raw_phasef.
	if got := rawScopedWSSRouteMode("/backend-api/codex/responses"); got != "websocket_raw_phasef" {
		t.Fatalf("rawScopedWSSRouteMode(non-bridge) = %q", got)
	}
}

func TestWSSLayer0EvidenceHasFullPassReason(t *testing.T) {
	t.Parallel()
	// empty reason -> false.
	if wssLayer0EvidenceHasFullPassReason(nil, "") {
		t.Fatal("empty reason should return false")
	}
	// nil decisions -> false.
	if wssLayer0EvidenceHasFullPassReason(nil, "reason") {
		t.Fatal("nil decisions should return false")
	}
	// matching decision -> true.
	decisions := []evidence.BlockDecision{
		{Action: evidence.ActionFullPass, Reason: "match"},
		{Action: evidence.ActionSkipped, Reason: "other"},
	}
	if !wssLayer0EvidenceHasFullPassReason(decisions, "match") {
		t.Fatal("matching full_pass decision should return true")
	}
	// no matching decision -> false.
	if wssLayer0EvidenceHasFullPassReason(decisions, "nomatch") {
		t.Fatal("non-matching reason should return false")
	}
	// wrong action -> false.
	if wssLayer0EvidenceHasFullPassReason(decisions, "other") {
		t.Fatal("non-full_pass action should return false")
	}
}

func TestWSSStatefulPrefixElisionTokensSaved(t *testing.T) {
	t.Parallel()
	// nil facts -> 0.
	if got := wssStatefulPrefixElisionTokensSaved(nil); got != 0 {
		t.Fatalf("nil facts should return 0, got %d", got)
	}
	// missing key -> 0.
	if got := wssStatefulPrefixElisionTokensSaved(map[string]string{}); got != 0 {
		t.Fatalf("missing key should return 0, got %d", got)
	}
	// key not "true" -> 0.
	if got := wssStatefulPrefixElisionTokensSaved(map[string]string{"wss.stateful_prefix_elision_changed": "false"}); got != 0 {
		t.Fatalf("non-true value should return 0, got %d", got)
	}
	// valid with bytes saved -> positive.
	if got := wssStatefulPrefixElisionTokensSaved(map[string]string{
		"wss.stateful_prefix_elision_changed":     "true",
		"wss.stateful_prefix_elision_bytes_saved": "1000",
	}); got <= 0 {
		t.Fatalf("valid facts should return positive, got %d", got)
	}
	// invalid bytes -> 0.
	if got := wssStatefulPrefixElisionTokensSaved(map[string]string{
		"wss.stateful_prefix_elision_changed":     "true",
		"wss.stateful_prefix_elision_bytes_saved": "not-a-number",
	}); got != 0 {
		t.Fatalf("invalid bytes should return 0, got %d", got)
	}
}

func TestWSSRequestShape(t *testing.T) {
	t.Parallel()
	historyMsgs := []types.Message{{Role: "assistant"}}
	userMsgs := []types.Message{{Role: "user"}}
	cases := []struct {
		name     string
		meta     wssRequestMeta
		messages []types.Message
		want     string
	}{
		{
			name:     "history shape",
			messages: historyMsgs,
			want:     "full_history",
		},
		{
			name: "raw input with assistant messages",
			meta: wssRequestMeta{
				InputShape: wssRawInputShapeFacts{Items: 1, AssistantMessages: 1},
			},
			want: "full_history",
		},
		{
			name: "raw input root without previous response",
			meta: wssRequestMeta{
				InputShape: wssRawInputShapeFacts{Items: 1, MessageItems: 1},
			},
			want: "root",
		},
		{
			name: "raw input delta with previous response",
			meta: wssRequestMeta{
				PreviousResponseID: "resp-123",
				InputShape:         wssRawInputShapeFacts{Items: 1, MessageItems: 1},
			},
			want: "delta",
		},
		{
			name: "raw input full history fallback",
			meta: wssRequestMeta{
				PreviousResponseID: "resp-123",
				InputShape:         wssRawInputShapeFacts{Items: 1},
			},
			want: "full_history",
		},
		{
			name: "root without previous response",
			meta: wssRequestMeta{
				PreviousResponseID: "",
			},
			messages: userMsgs,
			want:     "root",
		},
		{
			name: "delta with previous response",
			meta: wssRequestMeta{
				PreviousResponseID: "resp-123",
			},
			messages: userMsgs,
			want:     "delta",
		},
		{
			name: "full history fallback with previous response and history",
			meta: wssRequestMeta{
				PreviousResponseID: "resp-123",
			},
			messages: historyMsgs,
			want:     "full_history",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := wssRequestShape(tc.meta, tc.messages); got != tc.want {
				t.Fatalf("wssRequestShape() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWSSRequestShapeSource(t *testing.T) {
	t.Parallel()
	historyMsgs := []types.Message{{Role: "assistant"}}
	userMsgs := []types.Message{{Role: "user"}}
	cases := []struct {
		name     string
		meta     wssRequestMeta
		messages []types.Message
		want     string
	}{
		{
			name:     "history shape",
			messages: historyMsgs,
			want:     "message_history",
		},
		{
			name: "raw input history",
			meta: wssRequestMeta{
				InputShape: wssRawInputShapeFacts{Items: 1, FunctionCalls: 1},
			},
			want: "raw_input_history",
		},
		{
			name: "raw input root without previous response",
			meta: wssRequestMeta{
				InputShape: wssRawInputShapeFacts{Items: 1, MessageItems: 1},
			},
			want: "raw_input_root_without_previous_response",
		},
		{
			name: "raw input delta with previous response",
			meta: wssRequestMeta{
				PreviousResponseID: "resp-123",
				InputShape:         wssRawInputShapeFacts{Items: 1, FunctionCallOutputs: 1},
			},
			want: "raw_input_previous_response_delta_shape",
		},
		{
			name: "raw input full history fallback",
			meta: wssRequestMeta{
				PreviousResponseID: "resp-123",
				InputShape:         wssRawInputShapeFacts{Items: 1},
			},
			want: "raw_input_previous_response_full_history_fallback",
		},
		{
			name: "root without previous response",
			meta: wssRequestMeta{
				PreviousResponseID: "",
			},
			messages: userMsgs,
			want:     "root_without_previous_response",
		},
		{
			name: "delta with previous response",
			meta: wssRequestMeta{
				PreviousResponseID: "resp-123",
			},
			messages: userMsgs,
			want:     "previous_response_delta_shape",
		},
		{
			name: "full history fallback with previous response",
			meta: wssRequestMeta{
				PreviousResponseID: "resp-123",
			},
			messages: historyMsgs,
			want:     "message_history",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := wssRequestShapeSource(tc.meta, tc.messages); got != tc.want {
				t.Fatalf("wssRequestShapeSource() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCodexTurnMetadataSessionID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", "", ""},
		{"invalid json", "not json", ""},
		{"no turn metadata", `{"other":"value"}`, ""},
		{"turn metadata not a string", `{"x-codex-turn-metadata":123}`, ""},
		{"turn metadata empty string", `{"x-codex-turn-metadata":""}`, ""},
		{"turn metadata invalid json", `{"x-codex-turn-metadata":"not json"}`, ""},
		{"thread_id", `{"x-codex-turn-metadata":"{\"thread_id\":\"thread-123\"}"}`, "thread-123"},
		{"session_id", `{"x-codex-turn-metadata":"{\"session_id\":\"sess-456\"}"}`, "sess-456"},
		{"thread_id wins over session_id", `{"x-codex-turn-metadata":"{\"thread_id\":\"thread\",\"session_id\":\"sess\"}"}`, "thread"},
		{"empty thread_id falls to session_id", `{"x-codex-turn-metadata":"{\"thread_id\":\"\",\"session_id\":\"sess\"}"}`, "sess"},
		{"both empty", `{"x-codex-turn-metadata":"{\"thread_id\":\"\",\"session_id\":\"\"}"}`, ""},
		{"neither key", `{"x-codex-turn-metadata":"{\"other\":\"value\"}"}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := codexTurnMetadataSessionID(json.RawMessage(tc.raw)); got != tc.want {
				t.Fatalf("codexTurnMetadataSessionID(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestProxyLayer0DependencySensitiveCommand(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cmd  string
		want bool
	}{
		{"empty", "", false},
		{"unknown", "echo hello", false},
		{"go", "go build", true},
		{"cargo", "cargo build", true},
		{"npm", "npm install", true},
		{"pnpm", "pnpm install", true},
		{"yarn", "yarn install", true},
		{"bun", "bun install", true},
		{"pytest", "pytest -x", true},
		{"tox", "tox", true},
		{"uv", "uv pip install", true},
		{"pip", "pip install", true},
		{"python", "python script.py", true},
		{"python3", "python3 script.py", true},
		{"node", "node script.js", true},
		{"jest", "jest", true},
		{"vitest", "vitest run", true},
		{"tsc", "tsc --noEmit", true},
		{"eslint", "eslint .", true},
		{"npx with tool", "npx tsc --noEmit", true},
		{"npx with unknown", "npx echo hello", false},
		{"pnpm exec", "pnpm exec tsc", true},
		{"yarn dlx", "yarn dlx tsc", true},
		{"bun exec", "bun exec tsc", true},
		{"full path go", "/usr/local/go/bin/go build", true},
		{"ls", "ls -la", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := proxyLayer0DependencySensitiveCommand(tc.cmd); got != tc.want {
				t.Fatalf("proxyLayer0DependencySensitiveCommand(%q) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}

func TestWSSCompactedSARIFZeroResults(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		compacted string
		want      bool
	}{
		{"empty", "", false},
		{"no sarif prefix", "some output", false},
		{"sarif with results", "[sarif: eslint] 5 results", false},
		{"sarif zero results", "[sarif: eslint] 0 results", true},
		{"sarif zero results with whitespace", "  [sarif: eslint] 0 results  ", true},
		{"sarif no close bracket", "[sarif: eslint 0 results", false},
		{"sarif empty after bracket", "[sarif: eslint]", false},
		{"sarif only whitespace after bracket", "[sarif: eslint]   ", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := wssCompactedSARIFZeroResults([]byte(tc.compacted)); got != tc.want {
				t.Fatalf("wssCompactedSARIFZeroResults(%q) = %v, want %v", tc.compacted, got, tc.want)
			}
		})
	}
}

func TestCompactProxyInferredPlainPathList(t *testing.T) {
	t.Parallel()
	// No envelope -> false.
	if _, ok := compactProxyInferredPlainPathList("no envelope here"); ok {
		t.Fatal("text without envelope should return false")
	}
	// Envelope with non-path-list payload -> false.
	envelope := "Process exited with code 0\nOutput:\nhello world"
	if _, ok := compactProxyInferredPlainPathList(envelope); ok {
		t.Fatal("non-path-list payload should return false")
	}
	// Envelope with path list -> true (needs >= 8 paths, no spaces,
	// and grouping must produce shorter output than original).
	pathList := "very/long/deep/nested/path/a.go\nvery/long/deep/nested/path/b.go\nvery/long/deep/nested/path/c.go\nvery/long/deep/nested/path/d.go\nvery/long/deep/nested/path/e.go\nvery/long/deep/nested/path/f.go\nvery/long/deep/nested/path/g.go\nvery/long/deep/nested/path/h.go"
	envelopeWithPathList := "Process exited with code 0\nOutput:\n" + pathList
	out, ok := compactProxyInferredPlainPathList(envelopeWithPathList)
	if !ok {
		t.Fatal("path list payload should return true")
	}
	if !strings.Contains(out, "Process exited with code 0") {
		t.Fatal("output should contain envelope header")
	}
}

func TestWSSCacheBustDemotedMechanisms(t *testing.T) {
	t.Parallel()
	// nil adapter -> 0.
	var nilAdapter *wsPhaseFAdapter
	if got := nilAdapter.wssCacheBustDemotedMechanisms("session"); got != 0 {
		t.Fatalf("nil adapter should return 0, got %d", got)
	}
	// empty session -> 0.
	a := &wsPhaseFAdapter{}
	if got := a.wssCacheBustDemotedMechanisms(""); got != 0 {
		t.Fatalf("empty session should return 0, got %d", got)
	}
	// nil cacheBustSessions -> 0.
	if got := a.wssCacheBustDemotedMechanisms("session"); got != 0 {
		t.Fatalf("nil cacheBustSessions should return 0, got %d", got)
	}
	// unknown session -> 0.
	a.cacheBustSessions = map[string]*wssProviderCacheBustSession{}
	if got := a.wssCacheBustDemotedMechanisms("unknown"); got != 0 {
		t.Fatalf("unknown session should return 0, got %d", got)
	}
	// known session with demoted mask -> returns mask.
	a.cacheBustSessions["known"] = &wssProviderCacheBustSession{demoted: 42}
	if got := a.wssCacheBustDemotedMechanisms("known"); got != 42 {
		t.Fatalf("known session should return 42, got %d", got)
	}
}

func TestProxyGoRunInvokesReconc(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		argv []string
		want bool
	}{
		{"empty", []string{}, false},
		{"too short", []string{"go", "run"}, false},
		{"not go", []string{"node", "run", "reconc"}, false},
		{"not run", []string{"go", "build", "reconc"}, false},
		{"reconc", []string{"go", "run", "reconc"}, true},
		{"reconc with args", []string{"go", "run", "reconc", "--flag"}, true},
		{"cmd/reconc", []string{"go", "run", "cmd/reconc"}, true},
		{"path to reconc", []string{"go", "run", "./cmd/reconc"}, true},
		{"full path to reconc", []string{"go", "run", "/home/user/repo/cmd/reconc"}, true},
		{"not reconc", []string{"go", "run", "main.go"}, false},
		{"with flags before reconc", []string{"go", "run", "-v", "reconc"}, true},
		{"double dash stops", []string{"go", "run", "--", "reconc"}, false},
		{"empty args skipped", []string{"go", "run", "", "reconc"}, true},
		{"GO uppercase", []string{"GO", "run", "reconc"}, true},
		{"go with spaces", []string{" go ", "run", "reconc"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := proxyGoRunInvokesReconc(tc.argv); got != tc.want {
				t.Fatalf("proxyGoRunInvokesReconc(%v) = %v, want %v", tc.argv, got, tc.want)
			}
		})
	}
}

func TestScanWindowBlockFilePath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"no key", `{"data":"value"}`, ""},
		{"path key", `{"path":"/src/main.go"}`, "/src/main.go"},
		{"file_path key", `{"file_path":"test.go"}`, "test.go"},
		{"filename key", `{"filename":"config.json"}`, "config.json"},
		{"filepath key", `{"filepath":"/etc/hosts"}`, "/etc/hosts"},
		{"file key", `{"file":"readme.md"}`, "readme.md"},
		{"no colon", `{"path" "/src/main.go"}`, ""},
		{"no quote", `{"path": /src/main.go}`, ""},
		{"unclosed quote", `{"path":"/src/main.go`, ""},
		{"empty after colon", `{"path":  `, ""},
		{"whitespace after colon", `{"path":   "/src/main.go"}`, "/src/main.go"},
		{"first key wins", `{"path":"/first","file":"/second"}`, "/first"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := scanWindowBlockFilePath(tc.input); got != tc.want {
				t.Fatalf("scanWindowBlockFilePath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestSetWSSABCapture(t *testing.T) {
	t.Parallel()
	// nil proxy -> error.
	var nilProxy *Proxy
	_, err := nilProxy.SetWSSABCapture("/path", 0)
	if err == nil {
		t.Fatal("nil proxy should return error")
	}

	// proxy without wssABCapture -> error.
	p := &Proxy{}
	_, err = p.SetWSSABCapture("/path", 0)
	if err == nil {
		t.Fatal("proxy without wssABCapture should return error")
	}
}

func TestClearWSSABCapture(t *testing.T) {
	t.Parallel()
	// nil proxy -> empty status.
	var nilProxy *Proxy
	got := nilProxy.ClearWSSABCapture()
	if got.Enabled {
		t.Fatal("nil proxy should return disabled status")
	}

	// proxy without wssABCapture -> empty status.
	p := &Proxy{}
	got = p.ClearWSSABCapture()
	if got.Enabled {
		t.Fatal("proxy without wssABCapture should return disabled status")
	}
}

func TestWSSABCaptureStatus(t *testing.T) {
	t.Parallel()
	// nil proxy -> empty status.
	var nilProxy *Proxy
	got := nilProxy.WSSABCaptureStatus()
	if got.Enabled {
		t.Fatal("nil proxy should return disabled status")
	}

	// proxy without wssABCapture -> empty status.
	p := &Proxy{}
	got = p.WSSABCaptureStatus()
	if got.Enabled {
		t.Fatal("proxy without wssABCapture should return disabled status")
	}
}

func TestWSSNPXCommandClassSuffix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		argv      []string
		want      []string
		wantFound bool
	}{
		{"empty", []string{}, nil, false},
		{"not npx", []string{"node", "script.js"}, nil, false},
		{"npx bare", []string{"npx"}, nil, true},
		{"npx with package", []string{"npx", "create-react-app"}, []string{"create-react-app"}, true},
		{"npx -y", []string{"npx", "-y", "create-react-app"}, []string{"create-react-app"}, true},
		{"npx --yes", []string{"npx", "--yes", "create-react-app"}, []string{"create-react-app"}, true},
		{"npx -p package", []string{"npx", "-p", "pkg", "cmd"}, []string{"cmd"}, true},
		{"npx --package", []string{"npx", "--package", "pkg", "cmd"}, []string{"cmd"}, true},
		{"npx -c call", []string{"npx", "-c", "call", "cmd"}, []string{"cmd"}, true},
		{"npx --call", []string{"npx", "--call", "call", "cmd"}, []string{"cmd"}, true},
		{"npx -- separator", []string{"npx", "--", "cmd", "arg"}, []string{"cmd", "arg"}, true},
		{"npx -- alone", []string{"npx", "--"}, nil, true},
		{"npx unknown flag", []string{"npx", "--unknown", "cmd"}, []string{"cmd"}, true},
		{"npx only flags", []string{"npx", "-y", "--yes"}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, found := wssNPXCommandClassSuffix(tc.argv)
			if found != tc.wantFound {
				t.Fatalf("wssNPXCommandClassSuffix(%v) found=%v, want %v", tc.argv, found, tc.wantFound)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("wssNPXCommandClassSuffix(%v) = %v, want %v", tc.argv, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("wssNPXCommandClassSuffix(%v)[%d] = %q, want %q", tc.argv, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestProxyCommandLineContainsSearchTool(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cmd  string
		want bool
	}{
		{"empty", "", false},
		{"plain text", "echo hello world", false},
		{"rg", "rg pattern", true},
		{"grep", "grep pattern file", true},
		{"ggrep", "ggrep pattern", true},
		{"ag", "ag pattern", true},
		{"ack", "ack pattern", true},
		{"ack.pl", "ack.pl pattern", true},
		{"ug", "ug pattern", true},
		{"ugrep", "ugrep pattern", true},
		{"sift", "sift pattern", true},
		{"git grep", "git grep pattern", true},
		{"git log", "git log", false},
		{"git without grep arg", "git status", false},
		{"quoted rg", `"rg" pattern`, true},
		{"full path rg", "/usr/local/bin/rg pattern", true},
		{"quoted full path", `"/usr/bin/grep" pattern file`, true},
		{"rg in middle", "sudo rg pattern", true},
		{"unknown command", "ls -la", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := proxyCommandLineContainsSearchTool(tc.cmd); got != tc.want {
				t.Fatalf("proxyCommandLineContainsSearchTool(%q) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}

func TestProxyLayer0RouteCounters_SnapshotNil(t *testing.T) {
	t.Parallel()
	var c *proxyLayer0RouteCounters
	got := c.snapshot()
	if got.ToolResultBlocks != 0 {
		t.Fatalf("nil snapshot should be zero value, got %+v", got)
	}
}

func TestSaveCodexLayer0LatencyBudgetState(t *testing.T) {
	t.Parallel()
	// nil proxy -> no-op.
	var p *Proxy
	p.saveCodexLayer0LatencyBudgetState()

	// home error -> no-op.
	origHome := proxyUserHomeDir
	t.Cleanup(func() { proxyUserHomeDir = origHome })
	proxyUserHomeDir = func() (string, error) { return "", os.ErrNotExist }
	p2 := &Proxy{}
	p2.saveCodexLayer0LatencyBudgetState()

	// empty home -> no-op.
	proxyUserHomeDir = func() (string, error) { return "", nil }
	p3 := &Proxy{}
	p3.saveCodexLayer0LatencyBudgetState()

	// valid home with strikes clamping.
	tmp := t.TempDir()
	proxyUserHomeDir = func() (string, error) { return tmp, nil }
	p4 := &Proxy{}
	p4.codexLayer0LatencyStrikes.Store(-5)
	p4.codexLayer0LatencyExceeded.Store(true)
	p4.saveCodexLayer0LatencyBudgetState()

	// Verify file was written.
	statePath := filepath.Join(tmp, ".slimference", "runtime-budget", "codex-layer0-latency.json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state file should exist: %v", err)
	}

	// Strikes above limit -> clamped.
	p5 := &Proxy{}
	p5.codexLayer0LatencyStrikes.Store(codexLayer0LatencyStrikeLimit + 100)
	p5.saveCodexLayer0LatencyBudgetState()
}

func TestProxyChunkDedupUniformPriorityScore(t *testing.T) {
	t.Parallel()
	const base = 1_000_000_000
	cases := []struct {
		name  string
		order int
		want  int
	}{
		{"negative", -1, base},
		{"zero", 0, base},
		{"small", 5, base - 5},
		{"large", 999999, base - 999999},
		{"at base", base, 1},
		{"above base", base + 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := proxyChunkDedupUniformPriorityScore(tc.order); got != tc.want {
				t.Fatalf("proxyChunkDedupUniformPriorityScore(%d) = %d, want %d", tc.order, got, tc.want)
			}
		})
	}
}

func TestProxyChunkDedupCandidateBudgetBytes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		outputBytes   int
		maxRefPercent int
		want          int
	}{
		{"zero output", 0, 50, 1},
		{"negative output", -10, 50, 1},
		{"zero percent", 1000, 0, 1000},
		{"negative percent", 1000, -5, 1000},
		{"over 100 percent", 1000, 150, 1000},
		{"normal", 1000, 50, 500},
		{"100 percent", 1000, 100, 1000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := proxyChunkDedupCandidateBudgetBytes(tc.outputBytes, tc.maxRefPercent)
			if got != tc.want {
				t.Fatalf("proxyChunkDedupCandidateBudgetBytes(%d, %d) = %d, want %d", tc.outputBytes, tc.maxRefPercent, got, tc.want)
			}
		})
	}
}

func TestInputItemUserText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"not a map", "string", ""},
		{"not a map number", 42, ""},
		{"nil", nil, ""},
		{"non-user role", map[string]any{"role": "assistant", "content": "hi"}, ""},
		{"empty role with content string", map[string]any{"content": "hello"}, "hello"},
		{"user role with content string", map[string]any{"role": "user", "content": "hello"}, "hello"},
		{"user role with content array", map[string]any{"role": "user", "content": []any{map[string]any{"text": "part1"}, map[string]any{"text": "part2"}}}, "part1 part2"},
		{"user role with text field", map[string]any{"role": "user", "text": "direct text"}, "direct text"},
		{"user role no content no text", map[string]any{"role": "user"}, ""},
		{"empty role with text field", map[string]any{"text": "fallback text"}, "fallback text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := inputItemUserText(tc.value); got != tc.want {
				t.Fatalf("inputItemUserText(%v) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

func TestContentValueString(t *testing.T) {
	t.Parallel()
	if got := contentValueString("plain string"); got != "plain string" {
		t.Fatalf("contentValueString(string) = %q", got)
	}
	if got := contentValueString([]any{map[string]any{"text": "a"}, map[string]any{"text": "b"}}); got != "a b" {
		t.Fatalf("contentValueString(array) = %q, want \"a b\"", got)
	}
	if got := contentValueString([]any{map[string]any{"type": "image"}, map[string]any{"text": "b"}}); got != "b" {
		t.Fatalf("contentValueString(array mixed) = %q, want \"b\"", got)
	}
	if got := contentValueString(42); got != "42" {
		t.Fatalf("contentValueString(number) = %q, want \"42\"", got)
	}
}

func TestInputValueFirstUserText(t *testing.T) {
	t.Parallel()
	// String input -> returned directly.
	if got := inputValueFirstUserText("plain string"); got != "plain string" {
		t.Fatalf("inputValueFirstUserText(string) = %q", got)
	}
	// Array with user message -> returns text.
	arr := []any{
		map[string]any{"role": "assistant", "content": "skip"},
		map[string]any{"role": "user", "content": "found"},
	}
	if got := inputValueFirstUserText(arr); got != "found" {
		t.Fatalf("inputValueFirstUserText(array) = %q, want \"found\"", got)
	}
	// Empty array -> empty.
	if got := inputValueFirstUserText([]any{}); got != "" {
		t.Fatalf("inputValueFirstUserText(empty array) = %q, want empty", got)
	}
	// Non-string, non-array -> empty.
	if got := inputValueFirstUserText(42); got != "" {
		t.Fatalf("inputValueFirstUserText(number) = %q, want empty", got)
	}
}

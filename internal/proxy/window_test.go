package proxy

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/evidence"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
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

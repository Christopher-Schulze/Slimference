package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/chunkdedup"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/contentarchive"
	"github.com/slimference/slimference/internal/filter"
	"github.com/slimference/slimference/internal/savingspolicy"
	"github.com/slimference/slimference/internal/sessions"
	"github.com/slimference/slimference/internal/types"
)

func TestApplyProxyLayer0Branches(t *testing.T) {
	t.Parallel()

	unchanged := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "not a tool result"}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", Text: "plain output"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-echo", ToolName: "shell", ToolInput: `{"command":"echo ok"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-echo", Text: "ok\n"}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolUseID: "missing", Text: "ok\n"}}},
	}
	out, saved := applyProxyLayer0(unchanged)
	if saved != 0 || &out[0] != &unchanged[0] {
		t.Fatalf("unchanged messages should be returned as-is, saved=%d", saved)
	}
	_, stats := applyProxyLayer0WithSessionAndToolUsesDetailed(unchanged, "", nil)
	if stats.TokensSaved != 0 || stats.BlocksModified != 0 || stats.ToolResultBlocks != 3 ||
		stats.CommandResolvedBlocks != 1 || stats.CommandUnresolvedBlocks != 2 ||
		stats.ToolUseUnresolvedBlocks != 2 || stats.LedgerCommandCapsules != 1 {
		t.Fatalf("unchanged stats mismatch: %+v", stats)
	}

	var status strings.Builder
	for i := 0; i < 80; i++ {
		status.WriteString(" M file")
		status.WriteString(string(rune('a' + i%26)))
		status.WriteString(".go\n")
	}
	changed := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-status", ToolName: "shell", ToolInput: `{"command":"git status --short"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-status", Text: status.String()}}},
	}
	out, saved = applyProxyLayer0(changed)
	if saved <= 0 {
		t.Fatalf("expected savings, got %d", saved)
	}
	if out[1].Content[0].Text == changed[1].Content[0].Text || !strings.Contains(out[1].Content[0].Text, "[git status]") {
		t.Fatalf("tool result not compacted: %q", out[1].Content[0].Text)
	}
	if changed[1].Content[0].Text == out[1].Content[0].Text {
		t.Fatal("original message slice should not be mutated")
	}
	_, stats = applyProxyLayer0WithSessionAndToolUsesDetailed(changed, "", nil)
	if stats.ToolResultBlocks != 1 || stats.CommandResolvedBlocks != 1 || stats.TokensSaved <= 0 ||
		stats.BlocksModified != 1 || stats.CapturedOutputBlocks != 1 ||
		stats.ReadDeltaBlocks != 0 || stats.CodexExecEnvelopeBlocks != 0 ||
		stats.LedgerCommandCapsules != 1 {
		t.Fatalf("captured-output stats mismatch: %+v", stats)
	}
}

func TestApplyProxyLayer0WithRememberedToolUse(t *testing.T) {
	t.Parallel()

	var status strings.Builder
	for i := 0; i < 80; i++ {
		status.WriteString("?? synthetic_")
		status.WriteString(string(rune('a' + i%26)))
		status.WriteString(".go\n")
	}
	messages := []types.Message{
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-status", Text: status.String()}}},
	}
	remembered := map[string]types.ContentBlock{
		"call-status": {Type: "tool_use", ToolUseID: "call-status", ToolName: "exec_command", ToolInput: `{"cmd":"git -C /tmp/slimf status --short"}`},
	}
	out, saved := applyProxyLayer0WithSessionAndToolUses(messages, "sess", remembered)
	if saved <= 0 {
		t.Fatalf("expected remembered tool use to produce savings, got %d", saved)
	}
	if !strings.Contains(out[0].Content[0].Text, "[git status]") || strings.Contains(out[0].Content[0].Text, "synthetic_z.go") {
		t.Fatalf("remembered tool use did not compact status output: %q", out[0].Content[0].Text)
	}
}

func TestReduceCodexLayer0WSSCapturedOutputCarriesArchiveReference(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var status strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&status, "?? synthetic_%02d.go\n", i)
	}
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-status", ToolName: "shell", ToolInput: `{"command":"git status --short"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-status", Text: status.String()}}},
	}
	result := reduceCodexLayer0(codexLayer0Request{
		Route:     codexLayer0RouteWSSPhaseF,
		Messages:  messages,
		SessionID: "sess-wss-captured",
	})
	text := result.Messages[1].Content[0].Text
	if result.Stats.CapturedOutputBlocks != 1 || result.Stats.TokensSaved <= 0 {
		t.Fatalf("expected WSS captured-output savings, stats=%+v text=%q", result.Stats, text)
	}
	if !strings.Contains(text, "[git status]") || !strings.Contains(text, "[context-archive kind=tool-output uri=local-archive://") {
		t.Fatalf("WSS captured output must be compact and recoverable: %q", text)
	}
}

func TestArchiveProxyCapturedOutputArchivesCodexPayload(t *testing.T) {
	home := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	payload := strings.Repeat("src/example.go:42:stable match\n", 40)
	original := "Chunk ID: volatile\nWall time: 0.1234 seconds\nProcess exited with code 0\nOriginal token count: 500\nOutput:\n" + payload
	compacted := "Chunk ID: volatile\nWall time: 0.1234 seconds\nProcess exited with code 0\nOriginal token count: 500\nOutput:\n[rg] 40 match(es)"
	out, ok := archiveProxyCapturedOutput("sess-archive-payload", "rg -n stable src", compacted, original)
	if !ok {
		t.Fatal("expected captured output archive")
	}
	const marker = "uri=local-archive://"
	idx := strings.LastIndex(out, marker)
	if idx < 0 {
		t.Fatalf("archive marker missing: %q", out)
	}
	id := strings.TrimSpace(strings.TrimSuffix(out[idx+len("uri="):], "]"))
	_, archived, err := contentarchive.Get(contentarchive.DefaultDir(home), id)
	if err != nil {
		t.Fatal(err)
	}
	if string(archived) != payload {
		t.Fatalf("Codex exec archive should store stable payload only, got %q", string(archived[:min(len(archived), 120)]))
	}
}

func TestReduceCodexLayer0WSSCapturedOutputFailsOpenWithoutSession(t *testing.T) {
	t.Parallel()

	var status strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&status, "?? synthetic_%02d.go\n", i)
	}
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-status", ToolName: "shell", ToolInput: `{"command":"git status --short"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-status", Text: status.String()}}},
	}
	result := reduceCodexLayer0(codexLayer0Request{
		Route:    codexLayer0RouteWSSPhaseF,
		Messages: messages,
	})
	if result.Stats.BlocksModified != 0 || result.Messages[1].Content[0].Text != status.String() {
		t.Fatalf("missing WSS session must fail open, stats=%+v text=%q", result.Stats, result.Messages[1].Content[0].Text)
	}
}

func TestProxyResolveToolUseBranches(t *testing.T) {
	t.Parallel()

	block := types.ContentBlock{ToolResultID: "r1", ToolUseID: "u1"}
	if got := proxyResolveToolUse(block, nil); got != block {
		t.Fatal("nil index should return original block")
	}
	if got := proxyResolveToolUse(types.ContentBlock{}, map[string]types.ContentBlock{"x": {ToolName: "shell"}}); got.ToolName != "" {
		t.Fatal("missing id should return original empty block")
	}
	if got := proxyResolveToolUse(types.ContentBlock{ToolResultID: "missing"}, map[string]types.ContentBlock{"x": {ToolName: "shell"}}); got.ToolName != "" {
		t.Fatal("unknown id should return original block")
	}
	use := types.ContentBlock{ToolName: "shell", ToolInput: `{"command":"pwd"}`}
	if got := proxyResolveToolUse(types.ContentBlock{ToolUseID: "u1"}, map[string]types.ContentBlock{"u1": use}); got.ToolName != "shell" {
		t.Fatalf("fallback ToolUseID did not resolve: %#v", got)
	}
}

func TestApplyProxyLayer0LedgerObservationKinds(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	readOutput := "Process exited with code 0\nOutput:\n" + strings.Repeat("package proxy\nfunc ledgerObservation() {}\n", 20)
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-read", ToolName: "exec_command", ToolInput: `{"cmd":"sed -n '1,20p' internal/proxy/layer0_proxy.go","workdir":"/repo"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-read", Text: readOutput}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-search", ToolName: "exec_command", ToolInput: `{"cmd":"rg -n TODO .","workdir":"/repo"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-search", Text: "Process exited with code 0\nOutput:\na.go:1:TODO\n"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-fail", ToolName: "exec_command", ToolInput: `{"cmd":"go test ./pkg","workdir":"/repo"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-fail", Text: "Process exited with code 1\nOutput:\n--- FAIL: TestThing\n"}}},
	}
	_, stats := applyProxyLayer0WithSessionAndToolUsesDetailed(messages, "sess-ledger", nil)
	if stats.LedgerCommandCapsules != 3 ||
		stats.LedgerFileCapsules != 1 ||
		stats.LedgerSearchCapsules != 1 ||
		stats.LedgerFailureCapsules != 1 {
		t.Fatalf("ledger counters mismatch: %+v", stats)
	}
}

func TestProxyLayer0CommandLineVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   types.ContentBlock
		want string
	}{
		{"empty", types.ContentBlock{ToolName: "shell"}, ""},
		{"json_string_shell", types.ContentBlock{ToolName: "shell", ToolInput: `"git status --short"`}, "git status --short"},
		{"command", types.ContentBlock{ToolName: "shell", ToolInput: `{"command":"go test ./..."}`}, "go test ./..."},
		{"cmd", types.ContentBlock{ToolName: "shell", ToolInput: `{"cmd":"cargo test"}`}, "cargo test"},
		{"command_line", types.ContentBlock{ToolName: "shell", ToolInput: `{"command_line":"pnpm test"}`}, "pnpm test"},
		{"cmdline", types.ContentBlock{ToolName: "local_shell_call", ToolInput: `{"cmdline":"go vet ./..."}`}, "go vet ./..."},
		{"shell_command", types.ContentBlock{ToolName: "bash_command", ToolInput: `{"shell_command":"git status --short"}`}, "git status --short"},
		{"shellCommand", types.ContentBlock{ToolName: "bash_command", ToolInput: `{"shellCommand":"git diff --stat"}`}, "git diff --stat"},
		{"commandLine", types.ContentBlock{ToolName: "terminal.exec", ToolInput: `{"commandLine":"rg TODO docs"}`}, "rg TODO docs"},
		{"git_workdir_string", types.ContentBlock{ToolName: "exec_command", ToolInput: `{"cmd":"git status --short","workdir":"/repo/project"}`}, "git -C /repo/project status --short"},
		{"git_workdir_array", types.ContentBlock{ToolName: "exec_command", ToolInput: `{"command":["git","diff","--stat"],"cwd":"/repo/project"}`}, "git -C /repo/project diff --stat"},
		{"bash_lc_wrapper", types.ContentBlock{ToolName: "shell", ToolInput: `{"command":"/opt/homebrew/bin/bash -lc 'git status --short .'"}`}, "git status --short ."},
		{"slimference_filter_wrapper", types.ContentBlock{ToolName: "shell", ToolInput: `{"command":"slimference filter -- git status --short ."}`}, "git status --short ."},
		{"slimference_filter_stream_wrapper", types.ContentBlock{ToolName: "shell", ToolInput: `{"command":"slimference filter --stream -- rg TODO docs"}`}, "rg TODO docs"},
		{"command_array", types.ContentBlock{ToolName: "terminal.exec", ToolInput: `{"command":["/bin/sh","-c","git status --short"]}`}, "git status --short"},
		{"cmd_args", types.ContentBlock{ToolName: "terminal.exec", ToolInput: `{"cmd_args":["sh","-c","go test ./..."]}`}, "go test ./..."},
		{"command_args", types.ContentBlock{ToolName: "terminal.exec", ToolInput: `{"command_args":["rg","needle","path with space"]}`}, `rg needle "path with space"`},
		{"bash_lc_array_read", types.ContentBlock{ToolName: "container.exec", ToolInput: `{"command":["bash","-lc","cat /tmp/t248-target.md"]}`}, "cat /tmp/t248-target.md"},
		{"bash_lc_array_relative_read_workdir", types.ContentBlock{ToolName: "exec_command", ToolInput: `{"command":["bash","-lc","cat docs/todo.md"],"workdir":"/repo/project"}`}, "cat /repo/project/docs/todo.md"},
		{"bash_lc_array_relative_read_workingDirectory", types.ContentBlock{ToolName: "exec_command", ToolInput: `{"command":["bash","-lc","cat docs/todo.md"],"workingDirectory":"/repo/project"}`}, "cat /repo/project/docs/todo.md"},
		{"bash_lc_cd_relative_sed", types.ContentBlock{ToolName: "exec_command", ToolInput: `{"command":["bash","-lc","cd /repo/project && sed -n '10,20p' docs/todo.md"]}`}, "sed -n 10,20p /repo/project/docs/todo.md"},
		{"bash_lc_cd_git", types.ContentBlock{ToolName: "exec_command", ToolInput: `{"command":["bash","-lc","cd /repo/project && git status --short"]}`}, "git -C /repo/project status --short"},
		{"head_relative_read_workdir", types.ContentBlock{ToolName: "exec_command", ToolInput: `{"cmd":"head -n 20 internal/proxy/layer0_proxy.go","workdir":"/repo/project"}`}, "head -n 20 /repo/project/internal/proxy/layer0_proxy.go"},
		{"argv", types.ContentBlock{ToolName: "exec", ToolInput: `{"argv":["go","test","./pkg with space"]}`}, `go test "./pkg with space"`},
		{"args", types.ContentBlock{ToolName: "run_command", ToolInput: `{"args":["rg","needle","path with space"]}`}, `rg needle "path with space"`},
		{"read_path", types.ContentBlock{ToolName: "Read", ToolInput: `{"path":"pkg/file with space.go"}`}, `cat "pkg/file with space.go"`},
		{"read_path_workdir", types.ContentBlock{ToolName: "Read", ToolInput: `{"path":"pkg/file with space.go","cwd":"/repo/project"}`}, `cat "/repo/project/pkg/file with space.go"`},
		{"read_uri", types.ContentBlock{ToolName: "file.read", ToolInput: `{"uri":"docs/todo.md","workingDir":"/repo/project"}`}, `cat /repo/project/docs/todo.md`},
		{"read_target", types.ContentBlock{ToolName: "mcp.read_file", ToolInput: `{"target":"docs/todo.md","current_working_directory":"/repo/project"}`}, `cat /repo/project/docs/todo.md`},
		{"read_source_path", types.ContentBlock{ToolName: "local_file_read", ToolInput: `{"source_path":"docs/todo.md"}`}, `cat docs/todo.md`},
		{"read_file_path", types.ContentBlock{ToolName: "read_file", ToolInput: `{"file_path":"internal/proxy/provider.go"}`}, `cat internal/proxy/provider.go`},
		{"view_absolute_path", types.ContentBlock{ToolName: "view_file", ToolInput: `{"absolute_path":"/tmp/file with space.go"}`}, `cat "/tmp/file with space.go"`},
		{"raw_read_path", types.ContentBlock{ToolName: "open", ToolInput: `"docs/todo.md"`}, `cat docs/todo.md`},
		{"non_shell_raw", types.ContentBlock{ToolName: "other", ToolInput: "git status"}, ""},
		{"invalid_json_shell", types.ContentBlock{ToolName: "terminal", ToolInput: "git status --short"}, "git status --short"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := proxyLayer0CommandLine(tc.in); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestCompactProxyLayer0TextCodexExecEnvelope(t *testing.T) {
	t.Parallel()

	var status strings.Builder
	for i := 0; i < 80; i++ {
		status.WriteString(" M internal/proxy/file_")
		status.WriteString(string(rune('a' + i%26)))
		status.WriteString(".go\n")
	}
	envelope := "Chunk ID: abc123\nWall time: 0.0000 seconds\nProcess exited with code 0\nOriginal token count: 800\nOutput:\n" + status.String()
	out, changed := compactProxyLayer0Text("git status --short .", envelope, filter.FileReadContext{Mode: "scan"})
	if !changed {
		t.Fatal("expected Codex exec envelope to compact")
	}
	if !strings.Contains(out, "Process exited with code 0") || !strings.Contains(out, "Output:\n[git status]") {
		t.Fatalf("envelope header or compacted body missing: %q", out)
	}
	if _, changed, mechanism := compactProxyLayer0TextDetailed("git status --short .", envelope, filter.FileReadContext{Mode: "scan"}); !changed || mechanism != proxyLayer0MechanismCodexEnvelope {
		t.Fatalf("expected codex envelope mechanism, changed=%v mechanism=%q", changed, mechanism)
	}
	if strings.Contains(out, "file_z.go") {
		t.Fatalf("uncompacted payload leaked: %q", out)
	}
	if _, changed := compactProxyLayer0Text("git status --short .", "plain\nOutput:\n M one.go\n", filter.FileReadContext{Mode: "scan"}); changed {
		t.Fatal("non-Codex envelope should not compact via envelope fallback")
	}
	if header, payload, ok := splitCodexExecEnvelope("Process exited with code 0\nOutput:\nbody"); !ok || header != "Process exited with code 0\nOutput:\n" || payload != "body" {
		t.Fatalf("splitCodexExecEnvelope mismatch header=%q payload=%q ok=%v", header, payload, ok)
	}
	if header, payload, ok := splitCodexExecEnvelope("Process exited with code 0\r\nOutput:\r\nbody"); !ok || header != "Process exited with code 0\r\nOutput:\r\n" || payload != "body" {
		t.Fatalf("splitCodexExecEnvelope CRLF mismatch header=%q payload=%q ok=%v", header, payload, ok)
	}
	if _, changed := compactCodexExecEnvelope("echo ok", "Process exited with code 0\nOutput:\nok\n", filter.FileReadContext{Mode: "scan"}); changed {
		t.Fatal("envelope with unchanged payload should not compact")
	}
	if _, _, ok := splitCodexExecEnvelope("Process exited with code 0\nNo output marker"); ok {
		t.Fatal("missing output marker should not split")
	}
	if _, _, ok := splitCodexExecEnvelope("Process exited with code 0\nOutput:\n"); ok {
		t.Fatal("empty payload should not split")
	}
}

func TestApplyProxyLayer0WithSessionReadDelta(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	first := proxyReadMessages(strings.Repeat("line one\n", 80))
	out, saved := applyProxyLayer0WithSession(first, "sess-read")
	if strings.Contains(out[1].Content[0].Text, "status=unchanged") {
		t.Fatalf("first read must not become a read-cache reference, saved=%d", saved)
	}

	second := proxyReadMessages(strings.Repeat("line one\n", 80))
	out, saved = applyProxyLayer0WithSession(second, "sess-read")
	if saved <= 0 || !strings.Contains(out[1].Content[0].Text, "status=unchanged") {
		t.Fatalf("unchanged reread should become reference, saved=%d text=%q", saved, out[1].Content[0].Text)
	}
	_, stats := applyProxyLayer0WithSessionAndToolUsesDetailed(second, "sess-read", nil)
	if stats.ToolResultBlocks != 1 || stats.CommandResolvedBlocks != 1 || stats.ReadDeltaAttempts != 1 ||
		stats.ReadDeltaMisses != 0 || stats.TokensSaved <= 0 || stats.BlocksModified != 1 || stats.ReadDeltaBlocks != 1 ||
		stats.LedgerFileCapsules != 1 {
		t.Fatalf("read-delta stats mismatch: %+v", stats)
	}
	if len(stats.CacheEvents) != 1 || stats.CacheEvents[0].Action != proxyLayer0CacheHit || stats.CacheEvents[0].Reason != "unchanged" {
		t.Fatalf("read-delta cache hit event mismatch: %+v", stats.CacheEvents)
	}

	changed := proxyReadMessages(strings.Repeat("line one\n", 80) + "line two\n")
	out, saved = applyProxyLayer0WithSession(changed, "sess-read")
	if saved <= 0 || !strings.Contains(out[1].Content[0].Text, "+line two") || !strings.Contains(out[1].Content[0].Text, "uri=local-archive://") {
		t.Fatalf("changed reread should become delta, saved=%d text=%q", saved, out[1].Content[0].Text)
	}
}

func TestReduceCodexLayer0SuppressesCollapsedReadKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var payload strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&payload, "suppression unique payload line %03d with nonrepeating value %08x\n", i, i*7919+17)
	}
	messages := proxyReadMessages(payload.String())
	if result := reduceCodexLayer0(codexLayer0Request{
		Messages:  messages,
		SessionID: "sess-suppress",
	}); result.Stats.ReadDeltaMisses != 1 || result.Stats.TokensSaved != 0 {
		t.Fatalf("first read should seed readcache without savings: %+v", result.Stats)
	}

	suppressed := reduceCodexLayer0(codexLayer0Request{
		Messages:          messages,
		SessionID:         "sess-suppress",
		SuppressedToolKey: map[string]struct{}{"read:main.go": {}},
	})
	if suppressed.Stats.TokensSaved != 0 || suppressed.Stats.ReadDeltaAttempts != 0 ||
		suppressed.Stats.ReadDeltaBlocks != 0 || suppressed.Stats.BlocksModified != 0 {
		t.Fatalf("suppressed read key should full-pass without read-delta: %+v", suppressed.Stats)
	}
	if suppressed.Messages[1].Content[0].Text != messages[1].Content[0].Text {
		t.Fatalf("suppressed read changed model-facing text: %q", suppressed.Messages[1].Content[0].Text)
	}

	unsuppressed := reduceCodexLayer0(codexLayer0Request{
		Messages:  messages,
		SessionID: "sess-suppress",
	})
	if unsuppressed.Stats.ReadDeltaBlocks != 1 || unsuppressed.Stats.TokensSaved <= 0 ||
		!strings.Contains(unsuppressed.Messages[1].Content[0].Text, "archive=local-archive://") {
		t.Fatalf("unsuppressed reread should still collapse: %+v text=%q",
			unsuppressed.Stats, unsuppressed.Messages[1].Content[0].Text)
	}
}

func TestApplyProxyLayer0WithSessionRepeatedNonFileOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	body := strings.Repeat("deterministic report row with unchanged non-file data\n", 80)
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-report", ToolName: "exec_command", ToolInput: `{"cmd":"python generate_report.py"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-report", Text: body}}},
	}
	out, saved := applyProxyLayer0WithSession(messages, "sess-output")
	if saved != 0 || strings.Contains(out[1].Content[0].Text, "previous emitted output") {
		t.Fatalf("first non-file output must not collapse, saved=%d text=%q", saved, out[1].Content[0].Text)
	}

	out, saved = applyProxyLayer0WithSession(messages, "sess-output")
	if saved <= 0 || !strings.Contains(out[1].Content[0].Text, "kind=tool-output") ||
		!strings.Contains(out[1].Content[0].Text, "archive=local-archive://") {
		t.Fatalf("repeated non-file output should collapse, saved=%d text=%q", saved, out[1].Content[0].Text)
	}
	_, stats := applyProxyLayer0WithSessionAndToolUsesDetailed(messages, "sess-output", nil)
	if stats.ToolResultBlocks != 1 || stats.CommandResolvedBlocks != 1 || stats.TokensSaved <= 0 ||
		stats.BlocksModified != 1 || stats.RepeatedOutputBlocks != 1 ||
		stats.ReadDeltaBlocks != 0 || stats.CapturedOutputBlocks != 0 || stats.CodexExecEnvelopeBlocks != 0 ||
		stats.LedgerCommandCapsules != 1 {
		t.Fatalf("repeated-output stats mismatch: %+v", stats)
	}
	if len(stats.CacheEvents) != 1 || stats.CacheEvents[0].Mechanism != "repeated_output" ||
		stats.CacheEvents[0].Action != proxyLayer0CacheHit || stats.CacheEvents[0].Reason != "unchanged" {
		t.Fatalf("repeated-output cache hit event mismatch: %+v", stats.CacheEvents)
	}
}

func TestReduceCodexLayer0RepeatedSearchSameMatchSetBeforeGrouping(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	command := `cd /repo/search && rg -n needle src`
	output := func(reverse bool) string {
		var lines []string
		for i := 1; i <= 30; i++ {
			lines = append(lines, fmt.Sprintf("src/b.go:%d:needle beta context %s", i+100, strings.Repeat("detail ", 30)))
			lines = append(lines, fmt.Sprintf("src/a.go:%d:needle alpha context %s", i, strings.Repeat("detail ", 30)))
		}
		if reverse {
			for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
				lines[i], lines[j] = lines[j], lines[i]
			}
			lines = append([]string{"Chunk ID: second", "Wall time: 0.0001 seconds"}, lines...)
		} else {
			lines = append([]string{"Chunk ID: first", "Wall time: 0.0003 seconds"}, lines...)
		}
		return strings.Join(lines, "\n") + "\n"
	}
	messagesFor := func(callID, text string) []types.Message {
		return []types.Message{
			{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: callID, ToolName: "exec_command", ToolInput: `{"cmd":"` + command + `"}`}}},
			{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: callID, Text: text}}},
		}
	}
	seed := reduceCodexLayer0(codexLayer0Request{
		Route:     codexLayer0RouteWSSPhaseF,
		Messages:  messagesFor("search-a", output(false)),
		SessionID: "sess-search-match-set",
	})
	if seed.Stats.RepeatedOutputBlocks != 0 || seed.Stats.CapturedOutputBlocks != 1 || seed.Stats.TokensSaved <= 0 {
		t.Fatalf("first search should group and seed raw match identity: %+v", seed.Stats)
	}
	if len(seed.Stats.CacheEvents) != 1 || seed.Stats.CacheEvents[0].Mechanism != savingspolicy.CodexMechanismRepeatedOutput ||
		seed.Stats.CacheEvents[0].Action != proxyLayer0CacheMiss {
		t.Fatalf("first search should record repeated-output seed miss: %+v", seed.Stats.CacheEvents)
	}
	out := reduceCodexLayer0(codexLayer0Request{
		Route:     codexLayer0RouteWSSPhaseF,
		Messages:  messagesFor("search-b", output(true)),
		SessionID: "sess-search-match-set",
	})
	text := out.Messages[1].Content[0].Text
	if out.Stats.RepeatedOutputBlocks != 1 || out.Stats.CapturedOutputBlocks != 0 || out.Stats.TokensSaved <= 0 ||
		!strings.Contains(text, "kind=search-output") ||
		!strings.Contains(text, "status=same-match-set") {
		t.Fatalf("same search match-set should collapse before grouping: stats=%+v text=%q", out.Stats, text)
	}
}

func TestReduceCodexLayer0HostBudgetDemotesReducers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	body := strings.Repeat("deterministic report row with unchanged non-file data\n", 80)
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-report", ToolName: "exec_command", ToolInput: `{"cmd":"python generate_report.py"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-report", Text: body}}},
	}
	baseReq := codexLayer0Request{
		Messages:  messages,
		SessionID: "sess-host-budget",
	}
	seed := reduceCodexLayer0(baseReq)
	if seed.Stats.TokensSaved != 0 {
		t.Fatalf("first observation must only seed, stats=%+v", seed.Stats)
	}
	budgetReq := baseReq
	budgetReq.HostBudgetExceeded = true
	budgeted := reduceCodexLayer0(budgetReq)
	if budgeted.Stats.TokensSaved <= 0 || budgeted.Stats.RepeatedOutputBlocks != 1 ||
		!strings.Contains(budgeted.Messages[1].Content[0].Text, "[context-elided kind=tool-output status=unchanged") {
		t.Fatalf("host budget should keep cheap lossless cache hits, stats=%+v text=%q", budgeted.Stats, budgeted.Messages[1].Content[0].Text)
	}
	latencyReq := baseReq
	latencyReq.LatencyBudgetExceeded = true
	latencyBudgeted := reduceCodexLayer0(latencyReq)
	if latencyBudgeted.Stats.TokensSaved != 0 || latencyBudgeted.Stats.RepeatedOutputBlocks != 0 ||
		latencyBudgeted.Messages[1].Content[0].Text != body {
		t.Fatalf("latency budget must full-pass existing cache hit, stats=%+v text=%q", latencyBudgeted.Stats, latencyBudgeted.Messages[1].Content[0].Text)
	}
	unblocked := reduceCodexLayer0(baseReq)
	if unblocked.Stats.TokensSaved <= 0 || unblocked.Stats.RepeatedOutputBlocks != 1 {
		t.Fatalf("normal budget should still collapse repeated output, stats=%+v", unblocked.Stats)
	}
}

func TestApplyProxyLayer0WithSessionRepeatedPartialReadOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var bodyBuilder strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&bodyBuilder, "visible partial file range line %03d with stable value %d\n", i, i*i)
	}
	body := bodyBuilder.String()
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-head", ToolName: "exec_command", ToolInput: `{"cmd":"head -n 200 /tmp/range.data"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-head", Text: body}}},
	}
	out, stats := applyProxyLayer0WithSessionAndToolUsesDetailed(messages, "sess-partial-read", nil)
	if stats.ReadDeltaAttempts != 1 || stats.ReadDeltaMisses != 1 || stats.ReadDeltaBlocks != 0 || stats.RepeatedOutputBlocks != 0 ||
		strings.Contains(out[1].Content[0].Text, "archive=local-archive://") {
		t.Fatalf("first partial read should miss read-delta and avoid repeated-output, stats=%+v text=%q", stats, out[1].Content[0].Text)
	}

	out, stats = applyProxyLayer0WithSessionAndToolUsesDetailed(messages, "sess-partial-read", nil)
	if stats.ReadDeltaAttempts != 1 || stats.ReadDeltaBlocks != 1 ||
		stats.RepeatedOutputBlocks != 0 || stats.TokensSaved <= 0 ||
		!strings.Contains(out[1].Content[0].Text, "archive=local-archive://") {
		t.Fatalf("partial read should use ranged read-delta, not repeated-output, stats=%+v text=%q", stats, out[1].Content[0].Text)
	}
}

func TestApplyProxyLayer0WithSessionRepeatedAwkRangeReadOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var bodyBuilder strings.Builder
	for i := 10; i <= 40; i++ {
		fmt.Fprintf(&bodyBuilder, "awk range line %03d keeps exact context\n", i)
	}
	body := bodyBuilder.String()
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-awk", ToolName: "exec_command", ToolInput: `{"cmd":"awk 'NR>=10 && NR<=40 {print}' /tmp/range.data"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-awk", Text: body}}},
	}
	out, stats := applyProxyLayer0WithSessionAndToolUsesDetailed(messages, "sess-awk-range", nil)
	if stats.ReadDeltaAttempts != 1 || stats.ReadDeltaMisses != 1 || stats.ReadDeltaBlocks != 0 ||
		strings.Contains(out[1].Content[0].Text, "archive=local-archive://") {
		t.Fatalf("first awk range read should full-pass and seed readcache, stats=%+v text=%q", stats, out[1].Content[0].Text)
	}

	out, stats = applyProxyLayer0WithSessionAndToolUsesDetailed(messages, "sess-awk-range", nil)
	if stats.ReadDeltaAttempts != 1 || stats.ReadDeltaBlocks != 1 ||
		stats.RepeatedOutputBlocks != 0 || stats.TokensSaved <= 0 ||
		!strings.Contains(out[1].Content[0].Text, "archive=local-archive://") {
		t.Fatalf("second awk range read should use read-delta, stats=%+v text=%q", stats, out[1].Content[0].Text)
	}
}

func TestReduceCodexLayer0ChunkDedupPartialOverlap(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store := chunkdedup.NewStoreWithLimits(chunkdedup.Config{MinSize: 1024, AvgSize: 2048, MaxSize: 4096}, chunkdedup.StoreLimits{}, func(_, id string, chunk []byte) string {
		if len(chunk) == 0 || id == "" {
			return ""
		}
		return "local-archive://" + id
	})
	shared := uniqueProxyReadPayload("shared chunk dedup")
	first := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-a", ToolName: "Read", ToolInput: `{"path":"a.go"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-a", Text: shared + uniqueProxyReadPayload("tail a")}}},
	}
	second := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-b", ToolName: "Read", ToolInput: `{"path":"b.go"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-b", Text: shared + uniqueProxyReadPayload("tail b")}}},
	}

	seed := reduceCodexLayer0(codexLayer0Request{
		Messages:           first,
		SessionID:          "sess-chunks",
		ChunkDedupEnabled:  true,
		ChunkDedupMinBytes: 0,
		ChunkStore:         store,
		ArchiveRecovery:    true,
	})
	if seed.Stats.TokensSaved != 0 || seed.Stats.ChunkDedupBlocks != 0 {
		t.Fatalf("first partially-overlapped read should seed only: %+v", seed.Stats)
	}
	out := reduceCodexLayer0(codexLayer0Request{
		Messages:           second,
		SessionID:          "sess-chunks",
		ChunkDedupEnabled:  true,
		ChunkDedupMinBytes: 0,
		ChunkStore:         store,
		ArchiveRecovery:    true,
	})
	text := out.Messages[1].Content[0].Text
	if out.Stats.TokensSaved <= 0 || out.Stats.ChunkDedupBlocks != 1 ||
		!strings.Contains(text, "[context-chunk status=unchanged uri=local-archive://") ||
		!strings.Contains(text, "tail b") {
		t.Fatalf("second similar read should chunk-dedup shared regions: stats=%+v text=%q", out.Stats, text)
	}
}

func TestReduceCodexLayer0ChunkDedupInsideCodexExecEnvelope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store := chunkdedup.NewStoreWithLimits(chunkdedup.Config{MinSize: 1024, AvgSize: 2048, MaxSize: 4096}, chunkdedup.StoreLimits{}, func(_, id string, chunk []byte) string {
		if len(chunk) == 0 || id == "" {
			return ""
		}
		return "local-archive://" + id
	})
	shared := uniqueProxyReadPayload("shared codex exec envelope")
	firstText := "Chunk ID: aaa111\nWall time: 0ms\nProcess exited with code 0\nOutput:\n" + shared + uniqueProxyReadPayload("tail a")
	secondText := "Chunk ID: bbb222\nWall time: 0ms\nProcess exited with code 0\nOutput:\n" + shared + uniqueProxyReadPayload("tail b")
	first := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-a", ToolName: "exec_command", ToolInput: `{"cmd":"cat a.go"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-a", Text: firstText}}},
	}
	second := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-b", ToolName: "exec_command", ToolInput: `{"cmd":"cat b.go"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-b", Text: secondText}}},
	}

	seed := reduceCodexLayer0(codexLayer0Request{
		Messages:           first,
		SessionID:          "sess-envelope-chunks",
		ChunkDedupEnabled:  true,
		ChunkDedupMinBytes: 0,
		ChunkStore:         store,
		ArchiveRecovery:    true,
	})
	if seed.Stats.TokensSaved != 0 || seed.Stats.ChunkDedupBlocks != 0 {
		t.Fatalf("first envelope should seed payload chunks only: %+v", seed.Stats)
	}
	out := reduceCodexLayer0(codexLayer0Request{
		Messages:           second,
		SessionID:          "sess-envelope-chunks",
		ChunkDedupEnabled:  true,
		ChunkDedupMinBytes: 0,
		ChunkStore:         store,
		ArchiveRecovery:    true,
	})
	text := out.Messages[1].Content[0].Text
	if out.Stats.TokensSaved <= 0 || out.Stats.ChunkDedupBlocks != 1 ||
		!strings.Contains(text, "Chunk ID: bbb222") ||
		!strings.Contains(text, "Output:\n[context-chunk status=unchanged uri=local-archive://") ||
		!strings.Contains(text, "tail b") {
		t.Fatalf("second envelope should chunk-dedup payload while preserving header: stats=%+v text=%q", out.Stats, text)
	}
}

func TestReduceCodexLayer0ChunkDedupCodexTruncatedEnvelope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store := chunkdedup.NewStore(chunkdedup.Config{}, func(_, id string, chunk []byte) string {
		if len(chunk) == 0 || id == "" {
			return ""
		}
		return "local-archive://" + id
	})
	shared := strings.Repeat("codex truncated output stable shared line\n", 210)
	firstText := "Chunk ID: aaa111\nWall time: 0.0000 seconds\nProcess exited with code 0\nOriginal token count: 51204\nOutput:\nTotal output lines: 3201\n\n" + shared + "tail for file A\n"
	secondText := "Chunk ID: bbb222\nWall time: 0.0001 seconds\nProcess exited with code 0\nOriginal token count: 51204\nOutput:\nTotal output lines: 3201\n\n" + shared + "tail for file B\n"
	first := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-a", ToolName: "exec_command", ToolInput: `{"cmd":"cat /tmp/a.txt"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-a", Text: firstText}}},
	}
	second := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-b", ToolName: "exec_command", ToolInput: `{"cmd":"cat /tmp/b.txt"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-b", Text: secondText}}},
	}

	reduceCodexLayer0(codexLayer0Request{
		Messages:           first,
		SessionID:          "sess-truncated-envelope",
		ChunkDedupEnabled:  true,
		ChunkDedupMinBytes: 0,
		ChunkStore:         store,
		ArchiveRecovery:    true,
	})
	out := reduceCodexLayer0(codexLayer0Request{
		Messages:           second,
		SessionID:          "sess-truncated-envelope",
		ChunkDedupEnabled:  true,
		ChunkDedupMinBytes: 0,
		ChunkStore:         store,
		ArchiveRecovery:    true,
	})
	text := out.Messages[1].Content[0].Text
	if out.Stats.TokensSaved <= 0 || out.Stats.ChunkDedupBlocks != 1 ||
		!strings.Contains(text, "[context-chunk status=unchanged uri=local-archive://") ||
		!strings.Contains(text, "tail for file B") {
		t.Fatalf("Codex-truncated envelope should chunk-dedup stable payload prefix: stats=%+v text=%q", out.Stats, text)
	}
}

func TestReduceCodexLayer0ChunkDedupReferenceDensityGuard(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store := chunkdedup.NewStoreWithLimits(chunkdedup.Config{MinSize: 1024, AvgSize: 2048, MaxSize: 4096}, chunkdedup.StoreLimits{}, func(_, id string, chunk []byte) string {
		if len(chunk) == 0 || id == "" {
			return ""
		}
		return "local-archive://" + id
	})
	shared := strings.Repeat("stable repeated context line for chunk density\n", 220)
	first := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-a", ToolName: "Read", ToolInput: `{"path":"a.go"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-a", Text: shared + "fresh A\n"}}},
	}
	second := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-b", ToolName: "Read", ToolInput: `{"path":"b.go"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-b", Text: shared + "fresh B\n"}}},
	}

	reduceCodexLayer0(codexLayer0Request{
		Messages:            first,
		SessionID:           "sess-density-guard",
		ChunkDedupEnabled:   true,
		ChunkDedupMinBytes:  0,
		ChunkDedupMaxRefPct: 10,
		ChunkStore:          store,
		ArchiveRecovery:     true,
	})
	out := reduceCodexLayer0(codexLayer0Request{
		Messages:            second,
		SessionID:           "sess-density-guard",
		ChunkDedupEnabled:   true,
		ChunkDedupMinBytes:  0,
		ChunkDedupMaxRefPct: 10,
		ChunkStore:          store,
		ArchiveRecovery:     true,
	})
	if out.Stats.ChunkDedupBlocks != 0 || strings.Contains(out.Messages[1].Content[0].Text, "context-chunk") {
		t.Fatalf("high reference density should full-pass, stats=%+v text=%q", out.Stats, out.Messages[1].Content[0].Text)
	}
}

func TestReduceCodexLayer0ChunkDedupSkipsPatchAndDiffOutputs(t *testing.T) {
	store := chunkdedup.NewStoreWithLimits(chunkdedup.Config{}, chunkdedup.StoreLimits{MaxSessionRefPct: 100}, func(_, id string, chunk []byte) string {
		return "local-archive://" + id
	})
	largeDiff := strings.Repeat("diff --git a/a.go b/a.go\n+added context line that should stay fresh for patch reasoning\n", 900)
	messagesFor := func(id, command string) []types.Message {
		return []types.Message{
			{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: id, ToolName: "exec_command", ToolInput: `{"cmd":"` + command + `"}`}}},
			{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: id, Text: largeDiff}}},
		}
	}
	req := func(messages []types.Message) codexLayer0Request {
		return codexLayer0Request{
			Messages:            messages,
			SessionID:           "chunk-patch-guard",
			ChunkStore:          store,
			ArchiveRecovery:     true,
			ChunkDedupEnabled:   true,
			ChunkDedupMinBytes:  0,
			ChunkDedupMaxRefPct: 100,
			PolicyMode:          "max",
		}
	}

	seed := reduceCodexLayer0(req(messagesFor("diff-1", "git diff -- a.go")))
	second := reduceCodexLayer0(req(messagesFor("diff-2", "git diff -- a.go")))
	if seed.Stats.ChunkDedupBlocks != 0 || second.Stats.ChunkDedupBlocks != 0 ||
		strings.Contains(second.Messages[1].Content[0].Text, "context-chunk") {
		t.Fatalf("git diff outputs must not receive chunk refs: seed=%+v second=%+v text=%q", seed.Stats, second.Stats, second.Messages[1].Content[0].Text)
	}

	if !chunkDedupAllowedForCommand("cat a.go", true) ||
		chunkDedupAllowedForCommand("apply_patch <<'PATCH'\n*** Begin Patch\n*** Update File: a.go\nPATCH", false) ||
		chunkDedupAllowedForCommand("git -C /repo show HEAD -- a.go", false) {
		t.Fatal("chunk patch/diff guard classification mismatch")
	}
}

func TestReduceCodexLayer0ChunkDedupRequiresGateAndRecovery(t *testing.T) {
	t.Parallel()
	store := chunkdedup.NewStore(chunkdedup.Config{}, nil)
	body := uniqueProxyReadPayload("large output")
	msgs := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "cmd", ToolName: "exec_command", ToolInput: `{"cmd":"python report.py"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "cmd", Text: body}}},
	}
	reduceCodexLayer0(codexLayer0Request{Messages: msgs, SessionID: "sess-gate", ChunkDedupEnabled: true, ChunkDedupMinBytes: 0, ChunkStore: store, ArchiveRecovery: true})
	out := reduceCodexLayer0(codexLayer0Request{Messages: msgs, SessionID: "sess-gate", ChunkDedupEnabled: false, ChunkDedupMinBytes: 0, ChunkStore: store, ArchiveRecovery: false})
	if out.Stats.ChunkDedupBlocks != 0 || strings.Contains(out.Messages[1].Content[0].Text, "context-chunk") {
		t.Fatalf("disabled chunk dedup must stay byte-equal: %+v", out.Stats)
	}
}

func TestCodexChunkDedupSettingsAutoPolicyEnablesRecoverableChunks(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.ArchiveRecoveryNoteEnabled = false
	p := New(cfg)
	if store, enabled, minBytes, maxRefPct, explicit, mode, archive := p.codexChunkDedupSettings(); !enabled || store == nil || explicit || mode != "auto" || !archive || minBytes != cfg.Compression.OutputReduce.CodexChunkDedupMinBytes || maxRefPct != cfg.Compression.OutputReduce.CodexChunkDedupMaxReferencePercent {
		t.Fatalf("auto policy should make recoverable chunk dedup available: enabled=%v store=%v min=%d max_ref=%d explicit=%v mode=%q archive=%v", enabled, store, minBytes, maxRefPct, explicit, mode, archive)
	}
	cfg.Compression.OutputReduce.CodexSavingsPolicyMode = "conservative"
	p = New(cfg)
	if store, enabled, _, _, _, _, archive := p.codexChunkDedupSettings(); enabled || store != nil || archive {
		t.Fatalf("conservative policy without explicit recovery should not enable chunk dedup: enabled=%v store=%v archive=%v", enabled, store, archive)
	}
	cfg.Compression.OutputReduce.CodexChunkDedupEnabled = true
	cfg.Compression.OutputReduce.ArchiveRecoveryNoteEnabled = true
	p = New(cfg)
	if store, enabled, minBytes, maxRefPct, explicit, mode, archive := p.codexChunkDedupSettings(); !enabled || store == nil || !explicit || mode != "conservative" || !archive || minBytes != cfg.Compression.OutputReduce.CodexChunkDedupMinBytes || maxRefPct != cfg.Compression.OutputReduce.CodexChunkDedupMaxReferencePercent {
		t.Fatalf("explicit chunk dedup settings not enabled with recovery note: enabled=%v store=%v min=%d max_ref=%d explicit=%v mode=%q archive=%v", enabled, store, minBytes, maxRefPct, explicit, mode, archive)
	}
}

func TestCodexHTTPChunkDedupSettingsStayConservative(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.CodexSavingsPolicyMode = "max"
	cfg.Compression.OutputReduce.CodexChunkDedupEnabled = true
	cfg.Compression.OutputReduce.ArchiveRecoveryNoteEnabled = true
	p := New(cfg)
	if store, enabled, minBytes, maxRefPct, explicit, mode, archive := p.codexHTTPChunkDedupSettings(); enabled || store != nil || explicit || archive || mode != "max" || minBytes != cfg.Compression.OutputReduce.CodexChunkDedupMinBytes || maxRefPct != cfg.Compression.OutputReduce.CodexChunkDedupMaxReferencePercent {
		t.Fatalf("http route must not emit chunk/archive refs without route recovery wiring: enabled=%v store=%v min=%d max_ref=%d explicit=%v mode=%q archive=%v", enabled, store, minBytes, maxRefPct, explicit, mode, archive)
	}
}

func TestApplyProxyLayer0ReadDeltaMissTelemetry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-1", ToolName: "Read", ToolInput: `{"path":"notes.txt"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-1", Text: strings.Repeat("plain archived seed line\n", 4)}}},
	}
	_, stats := applyProxyLayer0WithSessionAndToolUsesDetailed(messages, "sess-miss", nil)
	if stats.ToolResultBlocks != 1 || stats.CommandResolvedBlocks != 1 || stats.ReadDeltaAttempts != 1 ||
		stats.ReadDeltaMisses != 1 || stats.TokensSaved != 0 || stats.ReadDeltaBlocks != 0 {
		t.Fatalf("read-delta miss stats mismatch: %+v", stats)
	}
	if len(stats.CacheEvents) != 1 || stats.CacheEvents[0].Action != proxyLayer0CacheMiss ||
		stats.CacheEvents[0].Reason != "first_observation_seeded" {
		t.Fatalf("read-delta cache miss event mismatch: %+v", stats.CacheEvents)
	}
}

func TestProxyReadDeltaWorkdirSeparatesRelativePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dirA := t.TempDir()
	dirB := t.TempDir()
	command := func(workdir string) string {
		input := `{"cmd":"cat shared.txt","workdir":` + strconv.Quote(workdir) + `}`
		return proxyLayer0CommandLine(types.ContentBlock{ToolName: "exec_command", ToolInput: input})
	}
	largeA := uniqueProxyReadPayload("alpha")
	if out, changed := compactProxyReadDelta("sess-workdir", "", command(dirA), largeA, filter.FileReadContext{Mode: "scan"}, 0); changed || out != "" {
		t.Fatalf("first workdir A read must not delta, changed=%v out=%q", changed, out)
	}
	if out, changed := compactProxyReadDelta("sess-workdir", "", command(dirA), largeA, filter.FileReadContext{Mode: "scan"}, 0); !changed || !strings.Contains(out, dirA) {
		t.Fatalf("second workdir A read should delta against A path, changed=%v out=%q", changed, out)
	}
	largeB := uniqueProxyReadPayload("beta")
	if out, changed := compactProxyReadDelta("sess-workdir", "", command(dirB), largeB, filter.FileReadContext{Mode: "scan"}, 0); changed || out != "" {
		t.Fatalf("first workdir B read must not reuse workdir A cache, changed=%v out=%q", changed, out)
	}
}

func TestProxyReadDeltaIgnoresCodexExecEnvelopeVolatileHeader(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	payload := uniqueProxyReadPayload("envelope")
	first := "Chunk ID: aaa111\nWall time: 0.0000 seconds\nProcess exited with code 0\nOriginal token count: 900\nOutput:\n" + payload
	second := "Chunk ID: bbb222\nWall time: 0.1234 seconds\nProcess exited with code 0\nOriginal token count: 901\nOutput:\n" + payload

	if out, changed := compactProxyReadDelta("sess-envelope", "turn-1", "cat AGENTS.md", first, filter.FileReadContext{Mode: "scan"}, 0); changed || out != "" {
		t.Fatalf("first envelope read must seed without mutation, changed=%v out=%q", changed, out)
	}
	out, changed := compactProxyReadDelta("sess-envelope", "turn-2", "cat AGENTS.md", second, filter.FileReadContext{Mode: "scan"}, 0)
	if !changed {
		t.Fatalf("second envelope read should delta despite volatile header")
	}
	if strings.Contains(out, payload) {
		t.Fatalf("unchanged envelope read should not repeat payload: %q", out)
	}
	if !strings.Contains(out, "Chunk ID: bbb222") || !strings.Contains(out, "[context-elided kind=file-read status=unchanged") {
		t.Fatalf("envelope header and unchanged marker not preserved: %q", out)
	}
}

func TestProxyRepeatedOutputIgnoresCodexExecEnvelopeVolatileHeader(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	payload := strings.Repeat("internal/proxy/example.go:42:stable search result\n", 40)
	first := "Chunk ID: aaa111\nWall time: 0.0000 seconds\nProcess exited with code 0\nOriginal token count: 900\nOutput:\n" + payload
	second := "Chunk ID: bbb222\nWall time: 0.1234 seconds\nProcess exited with code 0\nOriginal token count: 901\nOutput:\n" + payload
	key := "search:rg\t-n\tstable\t/Users/christopher/CODE/Slimference/internal/proxy"
	command := "rg -n stable /Users/christopher/CODE/Slimference/internal/proxy"

	if out, ok := compactProxyRepeatedToolOutputWithKey("sess-repeated-envelope", key, command, first); ok || out != "" {
		t.Fatalf("first envelope output must seed without mutation, ok=%v out=%q", ok, out)
	}
	out, ok := compactProxyRepeatedToolOutputWithKey("sess-repeated-envelope", key, command, second)
	if !ok {
		t.Fatalf("second envelope output should collapse despite volatile header")
	}
	if strings.Contains(out, payload) {
		t.Fatalf("unchanged repeated output should not repeat payload: %q", out)
	}
	if !strings.Contains(out, "Chunk ID: bbb222") ||
		!strings.Contains(out, "[context-elided kind=search-output status=same-match-set") {
		t.Fatalf("envelope header and unchanged output marker not preserved: %q", out)
	}
}

func uniqueProxyReadPayload(prefix string) string {
	var b strings.Builder
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&b, "%s unique payload line %03d with nonrepeating value %08x\n", prefix, i, i*7919+17)
	}
	return b.String()
}

func TestApplyProxyLayer0WithSessionRecentEditBypassesReadDeltaAndCommentStrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := sessions.ObserveHookFile(sessions.DefaultHookStateDir(home), "sess-edit", "main.go", "edit"); err != nil {
		t.Fatal(err)
	}
	source := strings.Repeat("// keep this recent edit comment\n", 20) + "package main\n"
	msgs := proxyReadMessages(source)
	out, saved := applyProxyLayer0WithSession(msgs, "sess-edit")
	if strings.Contains(out[1].Content[0].Text, "uri=local-archive://") ||
		strings.Contains(out[1].Content[0].Text, "Read delta") ||
		!strings.Contains(out[1].Content[0].Text, "// keep this recent edit comment") {
		t.Fatalf("recent edit should bypass read delta and preserve content signal, saved=%d text=%q", saved, out[1].Content[0].Text)
	}
	if _, err := os.Stat(sessions.DefaultHookStateDir(home)); err != nil {
		t.Fatal(err)
	}
}

func TestProxyEditedPathsFromMessages(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Role: "tool", Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "edit-1",
			Text:         "patch applied",
		}}},
	}
	remembered := map[string]types.ContentBlock{
		"edit-1": {
			Type:      "tool_use",
			ToolUseID: "edit-1",
			ToolName:  "apply_patch",
			ToolInput: `{"workdir":"/repo","patch":"*** Begin Patch\n*** Update File: src/main.go\n*** Add File: src/new.go\n*** End Patch"}`,
		},
	}
	paths := proxyEditedPathsFromMessages(msgs, remembered)
	got := strings.Join(paths, "\n")
	for _, want := range []string{"/repo/src/main.go", "/repo/src/new.go"} {
		if !strings.Contains(got, want) {
			t.Fatalf("edited paths missing %s: %#v", want, paths)
		}
	}

	rawPatch := types.ContentBlock{
		Type:      "tool_use",
		ToolName:  "apply_patch",
		ToolInput: `"*** Update File: a.go\n--- a/old.go\t2026-05-30\n+++ b/new.go\t2026-05-30\n"`,
	}
	paths = proxyEditedPathsFromMessages([]types.Message{{Content: []types.ContentBlock{rawPatch}}}, nil)
	got = strings.Join(paths, ",")
	if got != "a.go,old.go,new.go" {
		t.Fatalf("raw patch paths = %#v", paths)
	}
}

func proxyReadMessages(text string) []types.Message {
	return []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-1", ToolName: "Read", ToolInput: `{"path":"main.go"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-1", Text: text}}},
	}
}

func TestProxyLayer0SmallHelpers(t *testing.T) {
	t.Parallel()

	if got := proxyStringArray(nil); got != nil {
		t.Fatalf("nil raw array=%#v", got)
	}
	if got := proxyStringArray(json.RawMessage(`{"no":"array"}`)); got != nil {
		t.Fatalf("invalid array=%#v", got)
	}
	if got := proxyStringArray(json.RawMessage(`["go","test"]`)); len(got) != 2 || got[1] != "test" {
		t.Fatalf("array=%#v", got)
	}
	if !looksLikeShellTool(" Bash ") || !looksLikeShellTool("terminal.exec") || looksLikeShellTool("read") {
		t.Fatal("shell tool classifier mismatch")
	}
	if !looksLikeShellExecutable("/opt/homebrew/bin/bash") || looksLikeShellExecutable("fish") {
		t.Fatal("shell executable classifier mismatch")
	}
	if normalizeLayer0CommandLine("/bin/sh -c 'git status --short'") != "git status --short" ||
		normalizeLayer0CommandLine("git status --short") != "git status --short" {
		t.Fatal("shell wrapper normalization mismatch")
	}
	if got := normalizeLayer0CommandLine("cd /repo/project && rg needle docs"); got != "rg needle /repo/project/docs" {
		t.Fatalf("cd-wrapped search must normalize to repo-scoped key: %q", got)
	}
	if got := applyWorkdirToLayer0Command("rg needle docs", "/repo/project"); got != "rg needle /repo/project/docs" {
		t.Fatalf("workdir search must normalize to repo-scoped key: %q", got)
	}
	if got := normalizeLayer0CommandLine("cd /repo/project && awk 'NR>=10 && NR<=20 {print}' src/main.go"); !strings.Contains(got, "/repo/project/src/main.go") {
		t.Fatalf("cd-wrapped awk read must normalize to repo path: %q", got)
	}
	if got := applyWorkdirToLayer0Command("awk 'NR>=10 && NR<=20 {print}' src/main.go", "/repo/project"); !strings.Contains(got, "/repo/project/src/main.go") {
		t.Fatalf("workdir awk read must normalize to repo path: %q", got)
	}
	if got := applyWorkdirToLayer0Command("awk 'NR>=10&&NR<=20{print $0}' src/main.go", "/repo/project"); readRequestFromCommandLine(got).FilePath != "/repo/project/src/main.go" {
		t.Fatalf("workdir awk $0 read must remain parseable after normalization: %q", got)
	}
	if got := applyWorkdirToGitCommand("git -C /other status --short", "/repo/project"); got != "git -C /other status --short" {
		t.Fatalf("git -C should not be rewritten: %q", got)
	}
	if stripSlimferenceFilterWrapper([]string{"slimference", "filter", "--bad", "git"}) != "" ||
		stripSlimferenceFilterWrapper([]string{"other", "filter", "git"}) != "" ||
		stripSlimferenceFilterWrapper([]string{"slimference", "filter"}) != "" ||
		stripSlimferenceFilterWrapper([]string{"slimference", "filter", "--"}) != "" {
		t.Fatal("slimference wrapper rejection mismatch")
	}
	if !looksLikeReadTool("open_file") || !looksLikeReadTool("read_file") || looksLikeReadTool("shell") {
		t.Fatal("read tool classifier mismatch")
	}
	if joinShellArgs([]string{"", "plain", "two words"}) != `"" plain "two words"` {
		t.Fatal("joinShellArgs quoting mismatch")
	}
	if quoteShellArg("plain") != "plain" || quoteShellArg("two words") != `"two words"` ||
		quoteShellArg("NR>=10&&NR<=20{print $0}") != `'NR>=10&&NR<=20{print $0}'` {
		t.Fatal("quoteShellArg mismatch")
	}
}

func TestProxyRepeatedSearchOutputKeepsRepoScopedKeys(t *testing.T) {
	tmp := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	sessionID := "search-repo-scope"
	outputA := strings.Repeat("src/a.go:10:TODO repo A context\n", 30)
	outputB := strings.Repeat("src/a.go:10:TODO repo B context\n", 30)
	cmdA := applyWorkdirToLayer0Command("rg -n TODO src", "/repo/a")
	cmdB := applyWorkdirToLayer0Command("rg -n TODO src", "/repo/b")
	if cmdA == cmdB || !strings.Contains(cmdA, "/repo/a/src") || !strings.Contains(cmdB, "/repo/b/src") {
		t.Fatalf("repo-scoped search commands not distinct: A=%q B=%q", cmdA, cmdB)
	}
	if out, ok := compactProxyRepeatedToolOutput(sessionID, cmdA, outputA); ok || out != "" {
		t.Fatalf("first repo A search should seed without collapse: ok=%v out=%q", ok, out)
	}
	if out, ok := compactProxyRepeatedToolOutput(sessionID, cmdB, outputB); ok || out != "" {
		t.Fatalf("first repo B search must not reuse repo A key: ok=%v out=%q", ok, out)
	}
	if out, ok := compactProxyRepeatedToolOutput(sessionID, cmdB, outputB); !ok ||
		!strings.Contains(out, "kind=search-output") ||
		!strings.Contains(out, "status=same-match-set") ||
		!strings.Contains(out, "/repo/b/src") {
		t.Fatalf("second repo B search should collapse on its own key: ok=%v out=%q", ok, out)
	}
}

func TestProxyRepeatedSearchOutputRejectsImplicitCwdKey(t *testing.T) {
	tmp := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	sessionID := "search-implicit-cwd"
	command := "rg -n TODO src"
	output := strings.Repeat("src/a.go:10:TODO repo context\n", 30)
	if key := proxyLayer0QualityToolKey(command); key != "" {
		t.Fatalf("implicit-cwd search must not receive a reusable cache key: %q", key)
	}
	if out, ok := compactProxyRepeatedToolOutput(sessionID, command, output); ok || out != "" {
		t.Fatalf("implicit-cwd search must full-pass instead of seeding/collapsing: ok=%v out=%q", ok, out)
	}
}

func TestProxyRepeatedGenericOutputKeepsWorkdirScopedKeys(t *testing.T) {
	tmp := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	sessionID := "generic-repo-scope"
	output := strings.Repeat("ok  pkg/example  cached test output\n", 30)
	messagesFor := func(id, workdir string) []types.Message {
		return []types.Message{
			{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: id, ToolName: "exec_command", ToolInput: `{"cmd":"go test ./...","workdir":"` + workdir + `"}`}}},
			{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: id, Text: output}}},
		}
	}

	firstA := reduceCodexLayer0(codexLayer0Request{Messages: messagesFor("call-a", "/repo/a"), SessionID: sessionID})
	if firstA.Stats.RepeatedOutputBlocks != 0 || firstA.Stats.TokensSaved != 0 {
		t.Fatalf("first repo A generic command should seed only: %+v", firstA.Stats)
	}
	firstB := reduceCodexLayer0(codexLayer0Request{Messages: messagesFor("call-b", "/repo/b"), SessionID: sessionID})
	if firstB.Stats.RepeatedOutputBlocks != 0 || firstB.Stats.TokensSaved != 0 {
		t.Fatalf("first repo B generic command must not reuse repo A key: %+v text=%q", firstB.Stats, firstB.Messages[1].Content[0].Text)
	}
	secondB := reduceCodexLayer0(codexLayer0Request{Messages: messagesFor("call-b2", "/repo/b"), SessionID: sessionID})
	if secondB.Stats.RepeatedOutputBlocks != 1 || secondB.Stats.TokensSaved <= 0 ||
		!strings.Contains(secondB.Messages[1].Content[0].Text, "kind=tool-output") {
		t.Fatalf("second repo B generic command should collapse on workdir key: %+v text=%q", secondB.Stats, secondB.Messages[1].Content[0].Text)
	}

	useA := messagesFor("key-a", "/repo/a")[0].Content[0]
	useB := messagesFor("key-b", "/repo/b")[0].Content[0]
	keyA := proxyLayer0QualityToolKeyForUse(useA, proxyLayer0CommandLine(useA))
	keyB := proxyLayer0QualityToolKeyForUse(useB, proxyLayer0CommandLine(useB))
	if keyA == keyB || !strings.Contains(keyA, "cwd:/repo/a") || !strings.Contains(keyB, "cwd:/repo/b") {
		t.Fatalf("workdir-scoped keys not distinct: A=%q B=%q", keyA, keyB)
	}
}

func TestProxyLayer0GenericOutputKeyIncludesDependencyFingerprint(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.test/a\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	use := types.ContentBlock{
		Type:      "tool_use",
		ToolName:  "exec_command",
		ToolInput: `{"cmd":"go test ./...","workdir":"` + repo + `"}`,
	}
	commandLine := proxyLayer0CommandLine(use)
	first := proxyLayer0QualityToolKeyForUse(use, commandLine)
	if !strings.Contains(first, "cwd:"+repo) || !strings.Contains(first, ":deps:") {
		t.Fatalf("dependency-sensitive key missing cwd/deps: %q", first)
	}

	if err := os.WriteFile(filepath.Join(repo, "go.sum"), []byte("example.test/dep v1.0.0 h1:abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := proxyLayer0QualityToolKeyForUse(use, commandLine)
	if first == second || !strings.Contains(second, ":deps:") {
		t.Fatalf("dependency fingerprint should change after lockfile update: first=%q second=%q", first, second)
	}

	plain := types.ContentBlock{
		Type:      "tool_use",
		ToolName:  "exec_command",
		ToolInput: `{"cmd":"date","workdir":"` + repo + `"}`,
	}
	plainKey := proxyLayer0QualityToolKeyForUse(plain, proxyLayer0CommandLine(plain))
	if strings.Contains(plainKey, ":deps:") || !strings.Contains(plainKey, "cwd:"+repo) {
		t.Fatalf("non-sensitive command should stay cwd-scoped without dependency hash: %q", plainKey)
	}
}

func TestProxyReadDeltaFailOpenBranches(t *testing.T) {
	t.Parallel()
	if out, changed := compactProxyReadDelta("", "", "cat main.go", "content", filter.FileReadContext{Mode: "scan"}, 0); changed || out != "" {
		t.Fatalf("empty session should fail open, out=%q changed=%v", out, changed)
	}
	if out, changed := compactProxyReadDelta("sess", "", "echo nope", "content", filter.FileReadContext{Mode: "scan"}, 0); changed || out != "" {
		t.Fatalf("non-read command should fail open, out=%q changed=%v", out, changed)
	}
	if out, changed := compactProxyRepeatedToolOutput("", "python report.py", "content"); changed || out != "" {
		t.Fatalf("empty session repeated-output should fail open, out=%q changed=%v", out, changed)
	}
	ctx := proxyReadFileContext("", "cat main.go")
	if ctx.RecentlyEdited {
		t.Fatal("empty session should not mark recent edit")
	}
	ctx = proxyReadFileContext("sess", "echo nope")
	if ctx.RecentlyEdited {
		t.Fatal("non-read command should not mark recent edit")
	}
}

func TestProxyReadDeltaHomeErrorBranches(t *testing.T) {
	orig := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return "", errors.New("home") }
	defer func() { proxyUserHomeDir = orig }()

	if out, changed := compactProxyReadDelta("sess", "", "cat main.go", strings.Repeat("content\n", 20), filter.FileReadContext{Mode: "scan"}, 0); changed || out != "" {
		t.Fatalf("home error should fail open, out=%q changed=%v", out, changed)
	}
	if out, changed := compactProxyRepeatedToolOutput("sess", "python report.py", strings.Repeat("content\n", 80)); changed || out != "" {
		t.Fatalf("home error should fail open for repeated-output, out=%q changed=%v", out, changed)
	}
	ctx := proxyReadFileContext("sess", "cat main.go")
	if ctx.RecentlyEdited {
		t.Fatal("home error should not mark recent edit")
	}
}

func TestToolPruneRetryHelpers(t *testing.T) {
	if resolveToolPruneSessionKey("session", "req") != "session" || resolveToolPruneSessionKey("", "req") != "req" {
		t.Fatal("tool-prune session key fallback mismatch")
	}
	cfg := config.Defaults()
	cfg.Proxy.ServerStateEnabled = true
	p := New(cfg)
	p.serverState.Set("conv", "resp_prev")
	body := []byte(`{"messages":[{"role":"user","content":"old"},{"role":"assistant","content":"ok"},{"role":"user","content":"latest"}]}`)
	rewritten := p.rewriteToolPruneRetryBody(types.OpenAI, body, true, "conv")
	if string(rewritten) == string(body) || !strings.Contains(string(rewritten), `"previous_response_id":"resp_prev"`) || strings.Contains(string(rewritten), "old") {
		t.Fatalf("retry body was not server-state rewritten: %s", rewritten)
	}
	if got := p.rewriteToolPruneRetryBody(types.OpenAI, body, false, "conv"); string(got) != string(body) {
		t.Fatalf("unused server state should keep body: %s", got)
	}
}

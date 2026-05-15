package proxy

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/filter"
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
		{"bash_lc_wrapper", types.ContentBlock{ToolName: "shell", ToolInput: `{"command":"/opt/homebrew/bin/bash -lc 'git status --short .'"}`}, "git status --short ."},
		{"slimference_filter_wrapper", types.ContentBlock{ToolName: "shell", ToolInput: `{"command":"slimference filter -- git status --short ."}`}, "git status --short ."},
		{"slimference_filter_stream_wrapper", types.ContentBlock{ToolName: "shell", ToolInput: `{"command":"slimference filter --stream -- rg TODO docs"}`}, "rg TODO docs"},
		{"command_array", types.ContentBlock{ToolName: "terminal.exec", ToolInput: `{"command":["/bin/sh","-c","git status --short"]}`}, `/bin/sh -c "git status --short"`},
		{"argv", types.ContentBlock{ToolName: "exec", ToolInput: `{"argv":["go","test","./pkg with space"]}`}, `go test "./pkg with space"`},
		{"args", types.ContentBlock{ToolName: "run_command", ToolInput: `{"args":["rg","needle","path with space"]}`}, `rg needle "path with space"`},
		{"read_path", types.ContentBlock{ToolName: "Read", ToolInput: `{"path":"pkg/file with space.go"}`}, `cat "pkg/file with space.go"`},
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
	if strings.Contains(out[1].Content[0].Text, "unchanged since previous full read") {
		t.Fatalf("first read must not become a read-cache reference, saved=%d", saved)
	}

	second := proxyReadMessages(strings.Repeat("line one\n", 80))
	out, saved = applyProxyLayer0WithSession(second, "sess-read")
	if saved <= 0 || !strings.Contains(out[1].Content[0].Text, "unchanged since previous full read") {
		t.Fatalf("unchanged reread should become reference, saved=%d text=%q", saved, out[1].Content[0].Text)
	}

	changed := proxyReadMessages(strings.Repeat("line one\n", 80) + "line two\n")
	out, saved = applyProxyLayer0WithSession(changed, "sess-read")
	if saved <= 0 || !strings.Contains(out[1].Content[0].Text, "+ line two") || !strings.Contains(out[1].Content[0].Text, "Full content: local-archive://") {
		t.Fatalf("changed reread should become delta, saved=%d text=%q", saved, out[1].Content[0].Text)
	}
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
	if strings.Contains(out[1].Content[0].Text, "Full content: local-archive://") ||
		strings.Contains(out[1].Content[0].Text, "Slimference delta") ||
		!strings.Contains(out[1].Content[0].Text, "// keep this recent edit comment") {
		t.Fatalf("recent edit should bypass read delta and preserve content signal, saved=%d text=%q", saved, out[1].Content[0].Text)
	}
	if _, err := os.Stat(sessions.DefaultHookStateDir(home)); err != nil {
		t.Fatal(err)
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
	if quoteShellArg("plain") != "plain" || quoteShellArg("two words") != `"two words"` {
		t.Fatal("quoteShellArg mismatch")
	}
}

func TestProxyReadDeltaFailOpenBranches(t *testing.T) {
	t.Parallel()
	if out, changed := compactProxyReadDelta("", "cat main.go", "content", filter.FileReadContext{Mode: "scan"}); changed || out != "" {
		t.Fatalf("empty session should fail open, out=%q changed=%v", out, changed)
	}
	if out, changed := compactProxyReadDelta("sess", "echo nope", "content", filter.FileReadContext{Mode: "scan"}); changed || out != "" {
		t.Fatalf("non-read command should fail open, out=%q changed=%v", out, changed)
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

	if out, changed := compactProxyReadDelta("sess", "cat main.go", strings.Repeat("content\n", 20), filter.FileReadContext{Mode: "scan"}); changed || out != "" {
		t.Fatalf("home error should fail open, out=%q changed=%v", out, changed)
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

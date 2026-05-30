package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
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
	_, stats := applyProxyLayer0WithSessionAndToolUsesDetailed(unchanged, "", nil)
	if stats.TokensSaved != 0 || stats.BlocksModified != 0 || stats.ToolResultBlocks != 3 ||
		stats.CommandResolvedBlocks != 1 || stats.CommandUnresolvedBlocks != 2 ||
		stats.ToolUseUnresolvedBlocks != 2 {
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
		stats.ReadDeltaBlocks != 0 || stats.CodexExecEnvelopeBlocks != 0 {
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
		{"shellCommand", types.ContentBlock{ToolName: "bash_command", ToolInput: `{"shellCommand":"git diff --stat"}`}, "git diff --stat"},
		{"commandLine", types.ContentBlock{ToolName: "terminal.exec", ToolInput: `{"commandLine":"rg TODO docs"}`}, "rg TODO docs"},
		{"bash_lc_wrapper", types.ContentBlock{ToolName: "shell", ToolInput: `{"command":"/opt/homebrew/bin/bash -lc 'git status --short .'"}`}, "git status --short ."},
		{"slimference_filter_wrapper", types.ContentBlock{ToolName: "shell", ToolInput: `{"command":"slimference filter -- git status --short ."}`}, "git status --short ."},
		{"slimference_filter_stream_wrapper", types.ContentBlock{ToolName: "shell", ToolInput: `{"command":"slimference filter --stream -- rg TODO docs"}`}, "rg TODO docs"},
		{"command_array", types.ContentBlock{ToolName: "terminal.exec", ToolInput: `{"command":["/bin/sh","-c","git status --short"]}`}, "git status --short"},
		{"cmd_args", types.ContentBlock{ToolName: "terminal.exec", ToolInput: `{"cmd_args":["sh","-c","go test ./..."]}`}, "go test ./..."},
		{"command_args", types.ContentBlock{ToolName: "terminal.exec", ToolInput: `{"command_args":["rg","needle","path with space"]}`}, `rg needle "path with space"`},
		{"bash_lc_array_read", types.ContentBlock{ToolName: "container.exec", ToolInput: `{"command":["bash","-lc","cat /tmp/t248-target.md"]}`}, "cat /tmp/t248-target.md"},
		{"bash_lc_array_relative_read_workdir", types.ContentBlock{ToolName: "exec_command", ToolInput: `{"command":["bash","-lc","cat docs/todo.md"],"workdir":"/repo/project"}`}, "cat /repo/project/docs/todo.md"},
		{"bash_lc_array_relative_read_workingDirectory", types.ContentBlock{ToolName: "exec_command", ToolInput: `{"command":["bash","-lc","cat docs/todo.md"],"workingDirectory":"/repo/project"}`}, "cat /repo/project/docs/todo.md"},
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
		stats.ReadDeltaMisses != 0 || stats.TokensSaved <= 0 || stats.BlocksModified != 1 || stats.ReadDeltaBlocks != 1 {
		t.Fatalf("read-delta stats mismatch: %+v", stats)
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
		stats.ReadDeltaBlocks != 0 || stats.CapturedOutputBlocks != 0 || stats.CodexExecEnvelopeBlocks != 0 {
		t.Fatalf("repeated-output stats mismatch: %+v", stats)
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

func TestApplyProxyLayer0ReadDeltaMissTelemetry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-1", ToolName: "Read", ToolInput: `{"path":"notes.txt"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-1", Text: "plain line\n"}}},
	}
	_, stats := applyProxyLayer0WithSessionAndToolUsesDetailed(messages, "sess-miss", nil)
	if stats.ToolResultBlocks != 1 || stats.CommandResolvedBlocks != 1 || stats.ReadDeltaAttempts != 1 ||
		stats.ReadDeltaMisses != 1 || stats.TokensSaved != 0 || stats.ReadDeltaBlocks != 0 {
		t.Fatalf("read-delta miss stats mismatch: %+v", stats)
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
	if out, changed := compactProxyReadDelta("sess-workdir", command(dirA), largeA, filter.FileReadContext{Mode: "scan"}); changed || out != "" {
		t.Fatalf("first workdir A read must not delta, changed=%v out=%q", changed, out)
	}
	if out, changed := compactProxyReadDelta("sess-workdir", command(dirA), largeA, filter.FileReadContext{Mode: "scan"}); !changed || !strings.Contains(out, dirA) {
		t.Fatalf("second workdir A read should delta against A path, changed=%v out=%q", changed, out)
	}
	largeB := uniqueProxyReadPayload("beta")
	if out, changed := compactProxyReadDelta("sess-workdir", command(dirB), largeB, filter.FileReadContext{Mode: "scan"}); changed || out != "" {
		t.Fatalf("first workdir B read must not reuse workdir A cache, changed=%v out=%q", changed, out)
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

	if out, changed := compactProxyReadDelta("sess", "cat main.go", strings.Repeat("content\n", 20), filter.FileReadContext{Mode: "scan"}); changed || out != "" {
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

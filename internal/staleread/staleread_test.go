package staleread

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func readUse(id, path string) types.ContentBlock {
	return types.ContentBlock{
		Type:      "tool_use",
		ToolUseID: id,
		ToolName:  "Read",
		ToolInput: `{"path":"` + path + `"}`,
	}
}

func readResult(id, text string) types.ContentBlock {
	return types.ContentBlock{
		Type:         "tool_result",
		ToolResultID: id,
		Text:         text,
	}
}

func shellReadUse(id, command, workdir string) types.ContentBlock {
	payload := map[string]string{"cmd": command}
	if workdir != "" {
		payload["workdir"] = workdir
	}
	input, _ := json.Marshal(payload)
	return types.ContentBlock{
		Type:      "tool_use",
		ToolUseID: id,
		ToolName:  "exec_command",
		ToolInput: string(input),
	}
}

func TestEmptyMessages(t *testing.T) {
	out, stats := AgeMessages(nil, Options{})
	if out != nil {
		t.Errorf("out should be nil for nil input")
	}
	if stats.BlocksReplaced != 0 {
		t.Errorf("stats=%+v", stats)
	}
}

func TestNoReadToolUses(t *testing.T) {
	msgs := []types.Message{
		{Content: []types.ContentBlock{{Type: "text", Text: "hi"}}},
		{Content: []types.ContentBlock{{Type: "text", Text: "world"}}},
	}
	out, stats := AgeMessages(msgs, Options{})
	if stats.BlocksReplaced != 0 {
		t.Errorf("no reads should yield no replacements, got %+v", stats)
	}
	if &out[0] == &msgs[0] {
		// passthrough returns the slice as-is; that's fine
	}
}

func TestOlderReadAgedWhenLaterReadExists(t *testing.T) {
	longContent := strings.Repeat("file body line. ", 50)
	msgs := []types.Message{
		// Turn 0: tool_use Read(x.go)
		{Content: []types.ContentBlock{readUse("tu1", "src/x.go")}},
		// Turn 1: tool_result for tu1 with full content
		{Content: []types.ContentBlock{readResult("tu1", longContent)}},
		// Turns 2-4: filler
		{Content: []types.ContentBlock{{Type: "text", Text: "msg2"}}},
		{Content: []types.ContentBlock{{Type: "text", Text: "msg3"}}},
		{Content: []types.ContentBlock{{Type: "text", Text: "msg4"}}},
		// Turn 5: tool_use Read(x.go) again
		{Content: []types.ContentBlock{readUse("tu2", "src/x.go")}},
		// Turn 6: tool_result for tu2 with updated content
		{Content: []types.ContentBlock{readResult("tu2", "updated content")}},
	}
	out, stats := AgeMessages(msgs, Options{MinTurnGap: 3})
	if stats.BlocksReplaced != 1 {
		t.Fatalf("expected 1 block replaced, got %d", stats.BlocksReplaced)
	}
	if stats.PathsAged != 1 {
		t.Errorf("paths_aged=%d want 1", stats.PathsAged)
	}
	if stats.BytesReplaced <= 0 {
		t.Errorf("bytes_replaced=%d should be positive", stats.BytesReplaced)
	}
	// Old tool_result content must be replaced.
	if out[1].Content[0].Text == longContent {
		t.Errorf("old read content not replaced")
	}
	if !strings.Contains(out[1].Content[0].Text, "kind=stale-read") {
		t.Errorf("marker missing in old read: %q", out[1].Content[0].Text)
	}
	if !strings.Contains(out[1].Content[0].Text, "src/x.go") {
		t.Errorf("path missing in marker: %q", out[1].Content[0].Text)
	}
	// Latest read must stay intact.
	if out[6].Content[0].Text != "updated content" {
		t.Errorf("latest read mutated: %q", out[6].Content[0].Text)
	}
	// Input slice must not be mutated.
	if msgs[1].Content[0].Text != longContent {
		t.Errorf("input slice mutated")
	}
}

func TestMinTurnGapGuard(t *testing.T) {
	// Reads only 1 turn apart; default MinTurnGap=3 prevents aging.
	msgs := []types.Message{
		{Content: []types.ContentBlock{readUse("tu1", "x.go")}},
		{Content: []types.ContentBlock{readResult("tu1", "v1 content goes here and is sufficiently long.")}},
		{Content: []types.ContentBlock{readUse("tu2", "x.go")}},
		{Content: []types.ContentBlock{readResult("tu2", "v2 content")}},
	}
	_, stats := AgeMessages(msgs, Options{})
	if stats.BlocksReplaced != 0 {
		t.Errorf("reads too close should not be aged, got %d replaced", stats.BlocksReplaced)
	}
}

func TestLatestReadNotAged(t *testing.T) {
	msgs := []types.Message{
		{Content: []types.ContentBlock{readUse("tu1", "x.go")}},
		{Content: []types.ContentBlock{readResult("tu1", strings.Repeat("v1 ", 100))}},
		{Content: []types.ContentBlock{{Type: "text", Text: "filler1"}}},
		{Content: []types.ContentBlock{{Type: "text", Text: "filler2"}}},
		{Content: []types.ContentBlock{{Type: "text", Text: "filler3"}}},
		{Content: []types.ContentBlock{readUse("tu2", "x.go")}},
		{Content: []types.ContentBlock{readResult("tu2", "FRESH")}},
	}
	out, _ := AgeMessages(msgs, Options{MinTurnGap: 2})
	if out[6].Content[0].Text != "FRESH" {
		t.Errorf("most-recent read was aged, got %q", out[6].Content[0].Text)
	}
}

func TestDifferentPathsIndependent(t *testing.T) {
	msgs := []types.Message{
		{Content: []types.ContentBlock{readUse("tu1", "a.go")}},
		{Content: []types.ContentBlock{readResult("tu1", strings.Repeat("a-body ", 50))}},
		{Content: []types.ContentBlock{readUse("tu2", "b.go")}},
		{Content: []types.ContentBlock{readResult("tu2", strings.Repeat("b-body ", 50))}},
		{Content: []types.ContentBlock{readUse("tu3", "a.go")}},
		{Content: []types.ContentBlock{readResult("tu3", "a-fresh")}},
	}
	out, stats := AgeMessages(msgs, Options{MinTurnGap: 2})
	if stats.BlocksReplaced != 1 {
		t.Errorf("expected only a.go's older read aged, got %d replacements", stats.BlocksReplaced)
	}
	if !strings.Contains(out[1].Content[0].Text, "a.go") {
		t.Errorf("a.go old read not aged")
	}
	// b.go has no newer read → must stay intact
	if !strings.HasPrefix(out[3].Content[0].Text, "b-body") {
		t.Errorf("b.go read mutated: %q", out[3].Content[0].Text)
	}
}

func TestToolResultWithoutMatchingToolUseIgnored(t *testing.T) {
	msgs := []types.Message{
		// orphan tool_result with no matching tool_use - common when
		// the prior turn was trimmed by a sliding window.
		{Content: []types.ContentBlock{readResult("tu_orphan", "stray content")}},
	}
	_, stats := AgeMessages(msgs, Options{})
	if stats.BlocksReplaced != 0 {
		t.Errorf("orphan tool_result should not be aged, got %d", stats.BlocksReplaced)
	}
}

func TestToolUseWithoutToolUseID(t *testing.T) {
	msgs := []types.Message{
		{Content: []types.ContentBlock{{
			Type: "tool_use", ToolName: "Read", ToolInput: `{"path":"x.go"}`,
			// ToolUseID intentionally empty - we can't link results
		}}},
	}
	_, stats := AgeMessages(msgs, Options{})
	if stats.BlocksReplaced != 0 {
		t.Errorf("tool_use without ID should not enable aging, got %d", stats.BlocksReplaced)
	}
}

func TestToolUseWithUnparseableInput(t *testing.T) {
	msgs := []types.Message{
		{Content: []types.ContentBlock{{
			Type: "tool_use", ToolName: "Read", ToolUseID: "tu1",
			ToolInput: "not_json",
		}}},
	}
	_, stats := AgeMessages(msgs, Options{})
	if stats.BlocksReplaced != 0 {
		t.Errorf("non-JSON input should yield no aging, got %d", stats.BlocksReplaced)
	}
}

func TestToolUseWithoutPathInInput(t *testing.T) {
	msgs := []types.Message{
		{Content: []types.ContentBlock{{
			Type: "tool_use", ToolName: "Read", ToolUseID: "tu1",
			ToolInput: `{"limit": 100}`,
		}}},
	}
	_, stats := AgeMessages(msgs, Options{})
	if stats.BlocksReplaced != 0 {
		t.Errorf("input without path should yield no aging")
	}
}

func TestCustomReadToolNames(t *testing.T) {
	msgs := []types.Message{
		{Content: []types.ContentBlock{{
			Type: "tool_use", ToolName: "cat", ToolUseID: "tu1",
			ToolInput: `{"file":"x.go"}`,
		}}},
		{Content: []types.ContentBlock{readResult("tu1", strings.Repeat("old ", 100))}},
		{Content: []types.ContentBlock{{Type: "text", Text: "filler"}}},
		{Content: []types.ContentBlock{{Type: "text", Text: "filler"}}},
		{Content: []types.ContentBlock{{Type: "text", Text: "filler"}}},
		{Content: []types.ContentBlock{{
			Type: "tool_use", ToolName: "cat", ToolUseID: "tu2",
			ToolInput: `{"file":"x.go"}`,
		}}},
		{Content: []types.ContentBlock{readResult("tu2", "fresh")}},
	}
	_, stats := AgeMessages(msgs, Options{ReadToolNames: []string{"cat"}, MinTurnGap: 2})
	if stats.BlocksReplaced != 1 {
		t.Errorf("custom tool name should age, got %d", stats.BlocksReplaced)
	}
}

func TestShellReadCommandAged(t *testing.T) {
	msgs := []types.Message{
		{Content: []types.ContentBlock{shellReadUse("r1", "sed -n '1,20p' src/x.go", "/repo")}},
		{Content: []types.ContentBlock{readResult("r1", strings.Repeat("old shell read ", 80))}},
		{Content: []types.ContentBlock{{Type: "text", Text: "filler"}}},
		{Content: []types.ContentBlock{{Type: "text", Text: "filler"}}},
		{Content: []types.ContentBlock{{Type: "text", Text: "filler"}}},
		{Content: []types.ContentBlock{shellReadUse("r2", "sed -n '1,20p' src/x.go", "/repo")}},
		{Content: []types.ContentBlock{readResult("r2", "fresh shell read")}},
	}
	out, stats := AgeMessages(msgs, Options{MinTurnGap: 2})
	if stats.BlocksReplaced != 1 {
		t.Fatalf("shell read should age exactly once, got %+v", stats)
	}
	if !strings.Contains(out[1].Content[0].Text, `/repo/src/x.go:range:1:20`) {
		t.Fatalf("marker missing shell read identity: %q", out[1].Content[0].Text)
	}
	if out[6].Content[0].Text != "fresh shell read" {
		t.Fatalf("fresh shell read mutated: %q", out[6].Content[0].Text)
	}
}

func TestShellPipelineReadNotAged(t *testing.T) {
	msgs := []types.Message{
		{Content: []types.ContentBlock{shellReadUse("r1", "cat src/x.go | head -20", "/repo")}},
		{Content: []types.ContentBlock{readResult("r1", strings.Repeat("old ", 80))}},
		{Content: []types.ContentBlock{{Type: "text", Text: "filler"}}},
		{Content: []types.ContentBlock{{Type: "text", Text: "filler"}}},
		{Content: []types.ContentBlock{{Type: "text", Text: "filler"}}},
		{Content: []types.ContentBlock{shellReadUse("r2", "cat src/x.go | head -20", "/repo")}},
		{Content: []types.ContentBlock{readResult("r2", "fresh")}},
	}
	_, stats := AgeMessages(msgs, Options{MinTurnGap: 2})
	if stats.BlocksReplaced != 0 {
		t.Fatalf("ambiguous shell pipeline must full-pass, got %+v", stats)
	}
}

func TestExtractPathVariants(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"path key", `{"path":"a.go"}`, "a.go"},
		{"file_path key", `{"file_path":"b.go"}`, "b.go"},
		{"filename key", `{"filename":"c.go"}`, "c.go"},
		{"file key", `{"file":"d.go"}`, "d.go"},
		{"empty path string", `{"path":""}`, ""},
		{"non-string path", `{"path":123}`, ""},
		{"empty", ``, ""},
		{"malformed", `{`, ""},
		{"no recognised key", `{"other":"x"}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractPath(c.in); got != c.want {
				t.Errorf("got=%q want=%q", got, c.want)
			}
		})
	}
}

func TestMixedToolUsesAndOrphanResults(t *testing.T) {
	// Exercise the `!isRead` branch (non-Read tool_use) AND
	// orphan-result branches in passes 2 + 3 by mixing a Read
	// session with unrelated Bash tool_uses and orphan results.
	msgs := []types.Message{
		// Read use + matching result
		{Content: []types.ContentBlock{readUse("tu1", "x.go")}},
		{Content: []types.ContentBlock{readResult("tu1", strings.Repeat("old ", 100))}},
		// Non-Read tool_use (Bash) - must be ignored
		{Content: []types.ContentBlock{{
			Type: "tool_use", ToolName: "Bash", ToolUseID: "bash1",
			ToolInput: `{"command":"ls"}`,
		}}},
		// Orphan tool_result for the Bash use - no matching Read
		{Content: []types.ContentBlock{readResult("bash1", "ls output")}},
		// More filler
		{Content: []types.ContentBlock{{Type: "text", Text: "f"}}},
		// Fresh Read use + result
		{Content: []types.ContentBlock{readUse("tu2", "x.go")}},
		{Content: []types.ContentBlock{readResult("tu2", "fresh")}},
	}
	_, stats := AgeMessages(msgs, Options{MinTurnGap: 2})
	if stats.BlocksReplaced != 1 {
		t.Errorf("expected only the Read tool_result to age, got %d", stats.BlocksReplaced)
	}
}

func TestAgingPreservesCacheControlAndMetadata(t *testing.T) {
	// A tool_result block with cache_control set must keep the
	// hint after aging - layer 1/2 caching decisions depend on it.
	longBody := strings.Repeat("body ", 100)
	staleBlock := types.ContentBlock{
		Type:         "tool_result",
		ToolResultID: "r1",
		Text:         longBody,
		CacheControl: &types.CacheControl{Type: "ephemeral"},
		ArchiveID:    "arch-123",
	}
	msgs := []types.Message{
		{Content: []types.ContentBlock{readUse("r1", "x.go")}},
		{Content: []types.ContentBlock{staleBlock}},
		{Content: []types.ContentBlock{{Type: "text", Text: "f"}}},
		{Content: []types.ContentBlock{{Type: "text", Text: "f"}}},
		{Content: []types.ContentBlock{readUse("r2", "x.go")}},
		{Content: []types.ContentBlock{readResult("r2", "fresh")}},
	}
	out, stats := AgeMessages(msgs, Options{MinTurnGap: 2})
	if stats.BlocksReplaced != 1 {
		t.Fatalf("expected 1 replaced, got %d", stats.BlocksReplaced)
	}
	aged := out[1].Content[0]
	if aged.CacheControl == nil || aged.CacheControl.Type != "ephemeral" {
		t.Errorf("CacheControl lost during aging: %+v", aged.CacheControl)
	}
	if aged.ArchiveID != "arch-123" {
		t.Errorf("ArchiveID lost during aging: %q", aged.ArchiveID)
	}
	if aged.ToolResultID != "r1" {
		t.Errorf("ToolResultID lost: %q", aged.ToolResultID)
	}
	if !strings.Contains(aged.Text, "kind=stale-read") {
		t.Errorf("marker missing")
	}
}

func TestZeroMinTurnGapDefaults(t *testing.T) {
	// Two reads exactly DefaultMinTurnGap=3 messages apart should fire.
	msgs := []types.Message{
		{Content: []types.ContentBlock{readUse("tu1", "x.go")}},
		{Content: []types.ContentBlock{readResult("tu1", strings.Repeat("body ", 100))}},
		{Content: []types.ContentBlock{{Type: "text", Text: "filler"}}},
		{Content: []types.ContentBlock{{Type: "text", Text: "filler"}}},
		{Content: []types.ContentBlock{readUse("tu2", "x.go")}},
		{Content: []types.ContentBlock{readResult("tu2", "fresh")}},
	}
	_, stats := AgeMessages(msgs, Options{}) // MinTurnGap=0 → default 3
	if stats.BlocksReplaced != 1 {
		t.Errorf("expected aging with default gap, got %d", stats.BlocksReplaced)
	}
}

func TestShellCommandInput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		rawInput    string
		wantCommand string
		wantWorkdir string
	}{
		{"empty", "", "", ""},
		{"whitespace_only", "   \n\t  ", "", ""},
		{"plain_json_string", `"ls -la"`, "ls -la", ""},
		{"plain_text_fallback", "ls -la /tmp", "ls -la /tmp", ""},
		{"invalid_json_fallback", `{not valid json`, "{not valid json", ""},
		{"json_with_command", `{"command":"git status","workdir":"/repo"}`, "git status", "/repo"},
		{"json_with_cmd", `{"cmd":"npm test","workdir":"/project"}`, "npm test", "/project"},
		{"json_with_command_line", `{"command_line":"make build"}`, "make build", ""},
		{"json_with_cmdline", `{"cmdline":"cargo test"}`, "cargo test", ""},
		{"json_with_commandLine", `{"commandLine":"go vet"}`, "go vet", ""},
		{"json_with_shell_command", `{"shell_command":"rg pattern"}`, "rg pattern", ""},
		{"json_with_shellCommand", `{"shellCommand":"grep foo"}`, "grep foo", ""},
		{"json_with_workdir_only", `{"workdir":"/repo"}`, "", "/repo"},
		{"json_empty_command", `{"command":"","workdir":"/repo"}`, "", "/repo"},
		{"json_whitespace_command", `{"command":"   ","workdir":"/repo"}`, "", "/repo"},
		{"json_first_key_wins", `{"command":"first","cmd":"second"}`, "first", ""},
		{"json_no_keys", `{"other":"value"}`, "", ""},
		{"json_with_empty_workdir", `{"command":"ls","workdir":""}`, "ls", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd, workdir := shellCommandInput(tc.rawInput)
			if cmd != tc.wantCommand || workdir != tc.wantWorkdir {
				t.Fatalf("shellCommandInput(%q) = (%q, %q), want (%q, %q)",
					tc.rawInput, cmd, workdir, tc.wantCommand, tc.wantWorkdir)
			}
		})
	}
}

func TestLooksLikeShellToolName(t *testing.T) {
	t.Parallel()
	shellTools := []string{"bash", "shell", "sh", "zsh", "exec", "exec_command",
		"run_command", "terminal", "local_shell"}
	for _, name := range shellTools {
		if !looksLikeShellToolName(name) {
			t.Fatalf("looksLikeShellToolName(%q) = false, want true", name)
		}
	}
	nonShell := []string{"read", "write", "edit", "search", "", "unknown"}
	for _, name := range nonShell {
		if looksLikeShellToolName(name) {
			t.Fatalf("looksLikeShellToolName(%q) = true, want false", name)
		}
	}
}

func TestPathWithWorkdir(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		path    string
		workdir string
		want    string
	}{
		{"empty_path", "", "/repo", "."},
		{"absolute_path", "/usr/bin/file", "/repo", "/usr/bin/file"},
		{"relative_with_workdir", "src/file.go", "/repo", "/repo/src/file.go"},
		{"relative_empty_workdir", "src/file.go", "", "src/file.go"},
		{"relative_whitespace_workdir", "src/file.go", "  ", "src/file.go"},
		{"dot_path", ".", "/repo", "/repo"},
		{"dotdot_path", "..", "/repo", "/"},
		{"path_with_trailing_slash", "dir/", "/repo", "/repo/dir"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := pathWithWorkdir(tc.path, tc.workdir); got != tc.want {
				t.Fatalf("pathWithWorkdir(%q, %q) = %q, want %q",
					tc.path, tc.workdir, got, tc.want)
			}
		})
	}
}

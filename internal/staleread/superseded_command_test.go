package staleread

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func makeToolUse(id, name, input string) types.ContentBlock {
	return types.ContentBlock{
		Type:      "tool_use",
		ToolUseID: id,
		ToolName:  name,
		ToolInput: input,
	}
}

func makeToolResult(id, text string) types.ContentBlock {
	return types.ContentBlock{
		Type:         "tool_result",
		ToolResultID: id,
		Text:         text,
	}
}

func makeExecCommandToolUse(id, cmd string) types.ContentBlock {
	input, _ := json.Marshal(map[string]string{"cmd": cmd})
	return types.ContentBlock{
		Type:      "tool_use",
		ToolUseID: id,
		ToolName:  "exec_command",
		ToolInput: string(input),
	}
}

// TestSupersededCommand_PruneGitStatus proves that when git status is run
// twice, the earlier output is replaced with a marker and the later output
// is preserved.
func TestSupersededCommand_PruneGitStatus(t *testing.T) {
	messages := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "check status"}}},
		{Role: "assistant", Content: []types.ContentBlock{makeExecCommandToolUse("call_1", "git status --short")}},
		{Role: "tool", Content: []types.ContentBlock{makeToolResult("call_1", " M file1.go\n M file2.go\n")}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "I see modifications"}}},
		{Role: "assistant", Content: []types.ContentBlock{makeExecCommandToolUse("call_2", "git status --short")}},
		{Role: "tool", Content: []types.ContentBlock{makeToolResult("call_2", " M file1.go\n M file2.go\n M file3.go\n")}},
	}

	pruned, stats := PruneSupersededCommandOutputs(messages, ObsoleteOptions{})
	if stats.BlocksReplaced != 1 {
		t.Fatalf("expected 1 block replaced, got %d", stats.BlocksReplaced)
	}
	if stats.CommandsPruned != 1 {
		t.Fatalf("expected 1 command pruned, got %d", stats.CommandsPruned)
	}
	// First result should be replaced with marker
	if !strings.Contains(pruned[2].Content[0].Text, "[context-elided kind=superseded-command") {
		t.Fatalf("first git status output should be replaced with marker: %s", pruned[2].Content[0].Text)
	}
	// Second result should be preserved
	if pruned[5].Content[0].Text != " M file1.go\n M file2.go\n M file3.go\n" {
		t.Fatalf("second git status output should be preserved: %s", pruned[5].Content[0].Text)
	}
}

// TestSupersededCommand_PreserveMostRecent proves that the most recent
// command output is always preserved, even when there are 3+ runs.
func TestSupersededCommand_PreserveMostRecent(t *testing.T) {
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{makeExecCommandToolUse("c1", "go test ./...")}},
		{Role: "tool", Content: []types.ContentBlock{makeToolResult("c1", "FAIL: test1\n")}},
		{Role: "assistant", Content: []types.ContentBlock{makeExecCommandToolUse("c2", "go test ./...")}},
		{Role: "tool", Content: []types.ContentBlock{makeToolResult("c2", "PASS\n")}},
		{Role: "assistant", Content: []types.ContentBlock{makeExecCommandToolUse("c3", "go test ./...")}},
		{Role: "tool", Content: []types.ContentBlock{makeToolResult("c3", "ok example.com/pkg 0.5s\n")}},
	}

	pruned, stats := PruneSupersededCommandOutputs(messages, ObsoleteOptions{})
	if stats.BlocksReplaced != 2 {
		t.Fatalf("expected 2 blocks replaced (first two), got %d", stats.BlocksReplaced)
	}
	// First two should be markers
	if !strings.Contains(pruned[1].Content[0].Text, "[context-elided") {
		t.Fatalf("first go test output should be marker: %s", pruned[1].Content[0].Text)
	}
	if !strings.Contains(pruned[3].Content[0].Text, "[context-elided") {
		t.Fatalf("second go test output should be marker: %s", pruned[3].Content[0].Text)
	}
	// Third (most recent) should be preserved
	if pruned[5].Content[0].Text != "ok example.com/pkg 0.5s\n" {
		t.Fatalf("third go test output should be preserved: %s", pruned[5].Content[0].Text)
	}
}

// TestSupersededCommand_NeverTouchUserMessages proves that user messages
// are never touched by the pruning mechanism.
func TestSupersededCommand_NeverTouchUserMessages(t *testing.T) {
	userText := "please run git status and tell me what changed"
	messages := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: userText}}},
		{Role: "assistant", Content: []types.ContentBlock{makeExecCommandToolUse("c1", "git status --short")}},
		{Role: "tool", Content: []types.ContentBlock{makeToolResult("c1", " M file.go\n")}},
		{Role: "assistant", Content: []types.ContentBlock{makeExecCommandToolUse("c2", "git status --short")}},
		{Role: "tool", Content: []types.ContentBlock{makeToolResult("c2", " M file.go\n M other.go\n")}},
	}

	pruned, _ := PruneSupersededCommandOutputs(messages, ObsoleteOptions{})
	if pruned[0].Content[0].Text != userText {
		t.Fatalf("user message was modified: %s", pruned[0].Content[0].Text)
	}
}

// TestSupersededCommand_NeverTouchReasoning proves that model reasoning
// (assistant text blocks) are never touched.
func TestSupersededCommand_NeverTouchReasoning(t *testing.T) {
	reasoning := "Let me analyze the test results and determine the root cause..."
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: reasoning}}},
		{Role: "assistant", Content: []types.ContentBlock{makeExecCommandToolUse("c1", "go test ./...")}},
		{Role: "tool", Content: []types.ContentBlock{makeToolResult("c1", "FAIL\n")}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "The test failed because..."}}},
		{Role: "assistant", Content: []types.ContentBlock{makeExecCommandToolUse("c2", "go test ./...")}},
		{Role: "tool", Content: []types.ContentBlock{makeToolResult("c2", "PASS\n")}},
	}

	pruned, _ := PruneSupersededCommandOutputs(messages, ObsoleteOptions{})
	if pruned[0].Content[0].Text != reasoning {
		t.Fatalf("reasoning was modified: %s", pruned[0].Content[0].Text)
	}
	if pruned[3].Content[0].Text != "The test failed because..." {
		t.Fatalf("model reasoning was modified: %s", pruned[3].Content[0].Text)
	}
}

// TestSupersededCommand_NonDeterministicNotPruned proves that
// non-deterministic commands (tail -f, watch, top) are NOT pruned.
func TestSupersededCommand_NonDeterministicNotPruned(t *testing.T) {
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{makeExecCommandToolUse("c1", "tail -f /var/log/syslog")}},
		{Role: "tool", Content: []types.ContentBlock{makeToolResult("c1", "line1\nline2\n")}},
		{Role: "assistant", Content: []types.ContentBlock{makeExecCommandToolUse("c2", "tail -f /var/log/syslog")}},
		{Role: "tool", Content: []types.ContentBlock{makeToolResult("c2", "line3\nline4\n")}},
	}

	_, stats := PruneSupersededCommandOutputs(messages, ObsoleteOptions{})
	if stats.BlocksReplaced != 0 {
		t.Fatalf("non-deterministic commands should not be pruned, got %d blocks replaced", stats.BlocksReplaced)
	}
}

// TestSupersededCommand_DifferentCommandsNotPruned proves that different
// commands (even similar ones) are NOT pruned — only exact same command line.
func TestSupersededCommand_DifferentCommandsNotPruned(t *testing.T) {
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{makeExecCommandToolUse("c1", "git status --short")}},
		{Role: "tool", Content: []types.ContentBlock{makeToolResult("c1", " M file.go\n")}},
		{Role: "assistant", Content: []types.ContentBlock{makeExecCommandToolUse("c2", "git status")}},
		{Role: "tool", Content: []types.ContentBlock{makeToolResult("c2", "On branch main\nChanges not staged...\n")}},
	}

	_, stats := PruneSupersededCommandOutputs(messages, ObsoleteOptions{})
	if stats.BlocksReplaced != 0 {
		t.Fatalf("different commands (git status --short vs git status) should not be pruned, got %d", stats.BlocksReplaced)
	}
}

// TestSupersededCommand_SingleRunNotPruned proves that a command run only
// once is never pruned (it's the most recent by definition).
func TestSupersededCommand_SingleRunNotPruned(t *testing.T) {
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{makeExecCommandToolUse("c1", "git status --short")}},
		{Role: "tool", Content: []types.ContentBlock{makeToolResult("c1", " M file.go\n")}},
	}

	_, stats := PruneSupersededCommandOutputs(messages, ObsoleteOptions{})
	if stats.BlocksReplaced != 0 {
		t.Fatalf("single run should not be pruned, got %d", stats.BlocksReplaced)
	}
}

// TestSupersededCommand_EmptyMessages proves fail-open on empty input.
func TestSupersededCommand_EmptyMessages(t *testing.T) {
	_, stats := PruneSupersededCommandOutputs(nil, ObsoleteOptions{})
	if stats.BlocksReplaced != 0 || stats.CommandsPruned != 0 {
		t.Fatalf("empty messages should produce zero stats")
	}
}

// TestSupersededCommand_NoToolUseMetadata proves fail-open when tool_use
// metadata is missing (no tool_use blocks → no pruning).
func TestSupersededCommand_NoToolUseMetadata(t *testing.T) {
	messages := []types.Message{
		{Role: "tool", Content: []types.ContentBlock{makeToolResult("c1", "output1\n")}},
		{Role: "tool", Content: []types.ContentBlock{makeToolResult("c2", "output2\n")}},
	}

	_, stats := PruneSupersededCommandOutputs(messages, ObsoleteOptions{})
	if stats.BlocksReplaced != 0 {
		t.Fatalf("no tool_use metadata → no pruning, got %d", stats.BlocksReplaced)
	}
}

// TestIsPrunableCommand verifies the command classification.
func TestIsPrunableCommand(t *testing.T) {
	prunable := []string{
		"git status --short", "git diff", "git log --oneline -5",
		"go test ./...", "go build ./...", "go vet ./...",
		"cargo test", "cargo build",
		"npm test", "npm run build",
		"rg pattern src/", "grep -r foo .",
		"ls -la", "wc -l file.go",
		"docker ps", "kubectl get pods",
		"df -h", "du -sh .",
	}
	for _, cmd := range prunable {
		if !isPrunableCommand(cmd) {
			t.Errorf("command %q should be prunable", cmd)
		}
	}
	notPrunable := []string{
		"tail -f /var/log/syslog", "watch -n 1 date",
		"top", "htop",
		"less file.go", "vim file.go",
		"ssh user@host", "nc -l 8080",
		"some-unknown-command --flag",
		"", "  ",
	}
	for _, cmd := range notPrunable {
		if isPrunableCommand(cmd) {
			t.Errorf("command %q should NOT be prunable", cmd)
		}
	}
}

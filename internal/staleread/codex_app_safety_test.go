package staleread

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// TestCodexAppComputerUseUntouched proves staleread aging does not
// fire on Anthropic computer-use tool calls. The proxy ships
// computer-use traffic through the same /v1/messages pipeline, so
// the allowlist-based design must demonstrably not touch screenshots,
// mouse moves, or other computer-use tool_results.
func TestCodexAppComputerUseUntouched(t *testing.T) {
	bigScreenshot := strings.Repeat("base64-blob-bytes-go-here-and-here. ", 30)
	msgs := []types.Message{
		// turn 0: computer-use screenshot tool_use
		{Content: []types.ContentBlock{{
			Type: "tool_use", ToolUseID: "c1", ToolName: "computer_20241022",
			ToolInput: `{"action":"screenshot"}`,
		}}},
		// turn 1: tool_result with screenshot payload (we model as text
		// here since real screenshots are base64 binary in image blocks)
		{Content: []types.ContentBlock{readResult("c1", bigScreenshot)}},
		{Content: []types.ContentBlock{{Type: "text", Text: "filler"}}},
		{Content: []types.ContentBlock{{Type: "text", Text: "filler"}}},
		{Content: []types.ContentBlock{{Type: "text", Text: "filler"}}},
		// turn 5: another screenshot
		{Content: []types.ContentBlock{{
			Type: "tool_use", ToolUseID: "c2", ToolName: "computer_20241022",
			ToolInput: `{"action":"screenshot"}`,
		}}},
		{Content: []types.ContentBlock{readResult("c2", "fresh screenshot")}},
	}
	out, stats := AgeMessages(msgs, Options{MinTurnGap: 2})
	if stats.BlocksReplaced != 0 {
		t.Errorf("computer-use tool calls should not trigger aging, got %d replaced", stats.BlocksReplaced)
	}
	// Verify the original screenshot content survives byte-for-byte.
	if out[1].Content[0].Text != bigScreenshot {
		t.Errorf("computer-use tool_result mutated")
	}
}

// TestCodexAppBrowserUseUntouched mirrors the above for web_search /
// browser tool calls.
func TestCodexAppBrowserUseUntouched(t *testing.T) {
	searchResult := strings.Repeat("search snippet content. ", 50)
	msgs := []types.Message{
		{Content: []types.ContentBlock{{
			Type: "tool_use", ToolUseID: "s1", ToolName: "web_search_20250305",
			ToolInput: `{"query":"foo"}`,
		}}},
		{Content: []types.ContentBlock{readResult("s1", searchResult)}},
		{Content: []types.ContentBlock{{Type: "text", Text: "f"}}},
		{Content: []types.ContentBlock{{Type: "text", Text: "f"}}},
		{Content: []types.ContentBlock{{Type: "text", Text: "f"}}},
		{Content: []types.ContentBlock{{
			Type: "tool_use", ToolUseID: "s2", ToolName: "web_search_20250305",
			ToolInput: `{"query":"foo"}`,
		}}},
		{Content: []types.ContentBlock{readResult("s2", "fresh")}},
	}
	out, stats := AgeMessages(msgs, Options{MinTurnGap: 2})
	if stats.BlocksReplaced != 0 {
		t.Errorf("web_search should not trigger aging, got %d", stats.BlocksReplaced)
	}
	if out[1].Content[0].Text != searchResult {
		t.Errorf("web_search tool_result mutated")
	}
}

// TestComputerUseMutationsDoNotTriggerObsoletePrune confirms the
// T174 prune list does not accidentally include computer-use mouse/
// key actions even though they "mutate" UI state.
func TestComputerUseMutationsDoNotTriggerObsoletePrune(t *testing.T) {
	readContent := strings.Repeat("doc content. ", 60)
	msgs := []types.Message{
		// Read a file
		{Content: []types.ContentBlock{readUse("r1", "doc.md")}},
		{Content: []types.ContentBlock{readResult("r1", readContent)}},
		// computer_20241022 left_click: "mutates" the screen but
		// not the file system. Must NOT prune the prior read.
		{Content: []types.ContentBlock{{
			Type: "tool_use", ToolUseID: "c1", ToolName: "computer_20241022",
			ToolInput: `{"action":"left_click","coordinate":[10,20]}`,
		}}},
	}
	out, stats := PruneObsoleteReads(msgs, ObsoleteOptions{})
	if stats.BlocksReplaced != 0 {
		t.Errorf("computer-use click should not trigger prune, got %d", stats.BlocksReplaced)
	}
	if out[1].Content[0].Text != readContent {
		t.Errorf("read mutated by computer-use side-effect")
	}
}

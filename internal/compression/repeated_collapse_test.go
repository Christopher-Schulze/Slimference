package compression

import (
	"strings"
	"testing"

	"github.com/tokenproxy/tokenproxy/internal/types"
)

func TestToolCallIndex_Reset(t *testing.T) {
	t.Parallel()
	idx := NewToolCallIndex()
	idx.callFirst[[32]byte{}] = 1
	idx.Reset()
	if len(idx.callFirst) != 0 {
		t.Error("Reset should clear callFirst")
	}
}

func TestCollapseRepeated_IdenticalCallAndResult(t *testing.T) {
	t.Parallel()

	// Two identical Bash calls with identical results
	toolInput := `{"cmd":"ls -la"}`
	// Content must be longer than the collapse marker "[Identical to Bash result in message N]"
	toolResult := "total 128\n" + strings.Repeat("-rw-r--r-- 1 user staff 1234 Jan 1 12:00 file.go\n", 10)

	msgs := []types.Message{
		{
			Index: 0, Role: "user",
			Content: []types.ContentBlock{
				{Type: "tool_use", ToolName: "Bash", ToolInput: toolInput, ToolUseID: "use-1"},
			},
		},
		{
			Index: 1, Role: "user",
			Content: []types.ContentBlock{
				{Type: "tool_result", Text: toolResult, ToolResultID: "use-1"},
			},
		},
		{
			Index: 2, Role: "user",
			Content: []types.ContentBlock{
				{Type: "tool_use", ToolName: "Bash", ToolInput: toolInput, ToolUseID: "use-2"},
			},
		},
		{
			Index: 3, Role: "user",
			Content: []types.ContentBlock{
				{Type: "tool_result", Text: toolResult, ToolResultID: "use-2"},
			},
		},
	}

	idx := NewToolCallIndex()
	saved := idx.CollapseRepeated(msgs, 4)

	if saved <= 0 {
		t.Errorf("expected bytes saved > 0, got %d", saved)
	}
	collapsed := msgs[3].Content[0].Text
	if !strings.Contains(collapsed, "Identical to") {
		t.Errorf("expected collapse marker, got %q", collapsed)
	}
	if !strings.Contains(collapsed, "Bash") {
		t.Errorf("collapse marker should mention tool name, got %q", collapsed)
	}
}

func TestCollapseRepeated_DifferentResults_NoCollapse(t *testing.T) {
	t.Parallel()

	toolInput := `{"cmd":"git status"}`
	msgs := []types.Message{
		{
			Index: 0, Role: "user",
			Content: []types.ContentBlock{
				{Type: "tool_use", ToolName: "Bash", ToolInput: toolInput, ToolUseID: "use-1"},
			},
		},
		{
			Index: 1, Role: "user",
			Content: []types.ContentBlock{
				{Type: "tool_result", Text: "On branch main\nnothing to commit", ToolResultID: "use-1"},
			},
		},
		{
			Index: 2, Role: "user",
			Content: []types.ContentBlock{
				{Type: "tool_use", ToolName: "Bash", ToolInput: toolInput, ToolUseID: "use-2"},
			},
		},
		{
			Index: 3, Role: "user",
			Content: []types.ContentBlock{
				// Different result (file was modified)
				{Type: "tool_result", Text: "On branch main\nChanges not staged", ToolResultID: "use-2"},
			},
		},
	}

	idx := NewToolCallIndex()
	saved := idx.CollapseRepeated(msgs, 4)

	if saved != 0 {
		t.Errorf("different results should not be collapsed, got saved=%d", saved)
	}
	// Original content should be preserved
	if msgs[3].Content[0].Text != "On branch main\nChanges not staged" {
		t.Errorf("content should not be changed when results differ")
	}
}

func TestCollapseRepeated_TooFewMessages_NoOp(t *testing.T) {
	t.Parallel()
	idx := NewToolCallIndex()
	msgs := []types.Message{{Index: 0, Role: "user"}}
	saved := idx.CollapseRepeated(msgs, 1)
	if saved != 0 {
		t.Errorf("single message should save nothing, got %d", saved)
	}
}

func TestCollapseRepeated_NoToolUseID_NoCollapse(t *testing.T) {
	t.Parallel()
	// tool_result without a corresponding tool_use_id should not be collapsed
	msgs := []types.Message{
		{
			Index: 0, Role: "user",
			Content: []types.ContentBlock{
				{Type: "tool_result", Text: "some output"},
			},
		},
		{
			Index: 1, Role: "user",
			Content: []types.ContentBlock{
				{Type: "tool_result", Text: "some output"},
			},
		},
	}
	idx := NewToolCallIndex()
	saved := idx.CollapseRepeated(msgs, 2)
	if saved != 0 {
		t.Errorf("tool_results without IDs should not be collapsed, got %d", saved)
	}
}

func TestHashToolCall_Stable(t *testing.T) {
	t.Parallel()
	h1 := hashToolCall("Bash", `{"cmd":"ls -la","path":"/tmp"}`)
	h2 := hashToolCall("Bash", `{"path":"/tmp","cmd":"ls -la"}`) // key order differs
	// JSON normalization should make hashes equal
	if h1 != h2 {
		t.Errorf("hashToolCall should be order-independent for JSON inputs")
	}
}

func TestHashToolCall_DifferentTools_DifferentHashes(t *testing.T) {
	t.Parallel()
	h1 := hashToolCall("Read", `{"path":"foo.go"}`)
	h2 := hashToolCall("Write", `{"path":"foo.go"}`)
	if h1 == h2 {
		t.Errorf("different tool names should produce different hashes")
	}
}

func TestNormalizeJSON_InvalidJSON(t *testing.T) {
	t.Parallel()
	// Invalid JSON should return the original string unchanged
	input := `{"incomplete`
	got := normalizeJSON(input)
	if got != input {
		t.Errorf("invalid JSON should return original, got %q", got)
	}
}

func TestNormalizeJSON_EmptyString(t *testing.T) {
	t.Parallel()
	got := normalizeJSON("")
	if got != "" {
		t.Errorf("empty string should return empty, got %q", got)
	}
}

func TestCollapseRepeated_NoPrefixEnd_NoOp(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: "data"}}},
		{Index: 1, Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: "data"}}},
	}
	idx := NewToolCallIndex()
	saved := idx.CollapseRepeated(msgs, 0) // prefixEnd=0 -> no-op
	if saved != 0 {
		t.Errorf("prefixEnd=0 should save nothing, got %d", saved)
	}
}

// TestCollapseRepeated_SameMessageTwoResultsSameID covers the "first == i" guard (line 101):
// two tool_result blocks in the same message referencing the same tool_use_id → same callHash
// → second block finds first == i → continue (no panic, no collapse).
func TestCollapseRepeated_SameMessageTwoResultsSameID(t *testing.T) {
	t.Parallel()
	toolInput := `{"path":"file.go"}`
	toolResult := strings.Repeat("file content here\n", 5)

	msgs := []types.Message{
		{
			Index: 0, Role: "user",
			Content: []types.ContentBlock{
				{Type: "tool_use", ToolName: "Read", ToolInput: toolInput, ToolUseID: "use-1"},
			},
		},
		{
			// Two tool_result blocks with same ToolResultID in the same message.
			// Second block triggers the "first == i" guard.
			Index: 1, Role: "user",
			Content: []types.ContentBlock{
				{Type: "tool_result", Text: toolResult, ToolResultID: "use-1"},
				{Type: "tool_result", Text: toolResult, ToolResultID: "use-1"},
			},
		},
	}

	idx := NewToolCallIndex()
	saved := idx.CollapseRepeated(msgs, 2)
	// No collapse expected: second block hits the "first == i" guard and continues.
	if saved != 0 {
		t.Errorf("same-message duplicate should not be collapsed, got saved=%d", saved)
	}
}

// TestCollapseRepeated_ReplacementLongerThanOrig covers the "len(replacement) >= len(orig)" guard
// (line 112): when the original result is very short, the collapse marker is longer → no savings.
func TestCollapseRepeated_ReplacementLongerThanOrig(t *testing.T) {
	t.Parallel()
	toolInput := `{"cmd":"true"}`
	// Short result: 2 chars. Replacement "[Identical to Bash result in message N]" >> 2 chars.
	shortResult := "ok"

	msgs := []types.Message{
		{
			Index: 0, Role: "user",
			Content: []types.ContentBlock{
				{Type: "tool_use", ToolName: "Bash", ToolInput: toolInput, ToolUseID: "use-1"},
			},
		},
		{
			Index: 1, Role: "user",
			Content: []types.ContentBlock{
				{Type: "tool_result", Text: shortResult, ToolResultID: "use-1"},
			},
		},
		{
			Index: 2, Role: "user",
			Content: []types.ContentBlock{
				{Type: "tool_use", ToolName: "Bash", ToolInput: toolInput, ToolUseID: "use-2"},
			},
		},
		{
			Index: 3, Role: "user",
			Content: []types.ContentBlock{
				// Same short result → replacement marker is longer → no savings.
				{Type: "tool_result", Text: shortResult, ToolResultID: "use-2"},
			},
		},
	}

	idx := NewToolCallIndex()
	saved := idx.CollapseRepeated(msgs, 4)
	if saved != 0 {
		t.Errorf("short result: replacement longer than orig, expected saved=0, got %d", saved)
	}
	// Content at message 3 should be unchanged.
	if msgs[3].Content[0].Text != shortResult {
		t.Errorf("short result should be unchanged, got %q", msgs[3].Content[0].Text)
	}
}

package compression

import (
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/types"
)

func TestFileOpGraph_Reset(t *testing.T) {
	t.Parallel()
	g := NewFileOpGraph()
	g.files["foo.go"] = []FileOp{{Type: FileOpRead, MsgIdx: 0}}
	g.Reset()
	if len(g.files) != 0 {
		t.Error("Reset should clear files map")
	}
}

func TestPruneRedundant_ReadEditReadPattern(t *testing.T) {
	t.Parallel()

	// Pattern: Read@0, Edit@2, Read@4 -> Read@0 is prunable
	msgs := []types.Message{
		{
			Index: 0, Role: "user",
			Content: []types.ContentBlock{
				{
					Type:      "tool_result",
					Text:      strings.Repeat("file content v1\n", 50),
					ToolInput: `{"path": "pkg/foo.go"}`,
				},
			},
		},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "ok"}}},
		{
			Index: 2, Role: "user",
			Content: []types.ContentBlock{
				{
					Type:      "tool_use",
					ToolName:  "Edit",
					ToolInput: `{"path": "pkg/foo.go", "old": "v1", "new": "v2"}`,
					ToolUseID: "edit-1",
				},
			},
		},
		{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "edited"}}},
		{
			Index: 4, Role: "user",
			Content: []types.ContentBlock{
				{
					Type:      "tool_result",
					Text:      strings.Repeat("file content v2\n", 50),
					ToolInput: `{"path": "pkg/foo.go"}`,
				},
			},
		},
		{Index: 5, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "next"}}},
	}

	g := NewFileOpGraph()
	saved := g.PruneRedundant(msgs, 5)

	if saved <= 0 {
		t.Errorf("expected bytes saved from graph pruning, got %d", saved)
	}
	pruned := msgs[0].Content[0].Text
	if !strings.Contains(pruned, "superseded") {
		t.Errorf("pruned message should contain superseded marker: %q", pruned)
	}
	if !strings.Contains(pruned, "pkg/foo.go") {
		t.Errorf("pruned message should mention file path: %q", pruned)
	}
}

func TestPruneRedundantWithArchive_FullPassesWhenArchiveMissing(t *testing.T) {
	t.Parallel()

	original := strings.Repeat("file content v1\n", 50)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{
			Type:      "tool_result",
			Text:      original,
			ToolInput: `{"path": "pkg/foo.go"}`,
		}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "ok"}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{
			Type:      "tool_use",
			ToolName:  "Edit",
			ToolInput: `{"path": "pkg/foo.go"}`,
			ToolUseID: "edit-1",
		}}},
		{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "edited"}}},
		{Index: 4, Role: "user", Content: []types.ContentBlock{{
			Type:      "tool_result",
			Text:      strings.Repeat("file content v2\n", 50),
			ToolInput: `{"path": "pkg/foo.go"}`,
		}}},
	}

	g := NewFileOpGraph()
	saved := g.PruneRedundantWithArchive(msgs, 5, func(int, int, string) string { return "" })
	if saved != 0 {
		t.Fatalf("missing archive must full-pass, saved=%d", saved)
	}
	if msgs[0].Content[0].Text != original {
		t.Fatalf("missing archive changed original text: %q", msgs[0].Content[0].Text)
	}
	if msgs[0].Content[0].ArchiveID != "" {
		t.Fatalf("missing archive stamped id %q", msgs[0].Content[0].ArchiveID)
	}
}

func TestPruneRedundant_NoEditBetweenReads_NoPrune(t *testing.T) {
	t.Parallel()

	// Two reads of same file but no edit between them -> first read is NOT redundant
	msgs := []types.Message{
		{
			Index: 0, Role: "user",
			Content: []types.ContentBlock{
				{Type: "tool_result", Text: "content v1", ToolInput: `{"path": "foo.go"}`},
			},
		},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "ok"}}},
		{
			Index: 2, Role: "user",
			Content: []types.ContentBlock{
				{Type: "tool_result", Text: "content v1 again", ToolInput: `{"path": "foo.go"}`},
			},
		},
		{Index: 3, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "next"}}},
	}

	g := NewFileOpGraph()
	saved := g.PruneRedundant(msgs, 3)

	if saved != 0 {
		t.Errorf("without edit between reads, should not prune; got saved=%d", saved)
	}
}

func TestPruneRedundant_TooFewMessages_NoOp(t *testing.T) {
	t.Parallel()
	g := NewFileOpGraph()
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "hi"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "hey"}}},
	}
	saved := g.PruneRedundant(msgs, 2)
	if saved != 0 {
		t.Errorf("too few messages should save nothing, got %d", saved)
	}
}

func TestPruneRedundant_SafetyCheck_MessageReferenced(t *testing.T) {
	t.Parallel()

	// Read@0, Edit@2, Read@4, but message 5 explicitly references "message 0" -> no prune
	msgs := []types.Message{
		{
			Index: 0, Role: "user",
			Content: []types.ContentBlock{
				{Type: "tool_result", Text: strings.Repeat("file content\n", 50), ToolInput: `{"path": "pkg/bar.go"}`},
			},
		},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "ok"}}},
		{
			Index: 2, Role: "user",
			Content: []types.ContentBlock{
				{Type: "tool_use", ToolName: "Edit", ToolInput: `{"path": "pkg/bar.go"}`, ToolUseID: "e1"},
			},
		},
		{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "edited"}}},
		{
			Index: 4, Role: "user",
			Content: []types.ContentBlock{
				{Type: "tool_result", Text: strings.Repeat("file content v2\n", 50), ToolInput: `{"path": "pkg/bar.go"}`},
			},
		},
		{
			// Explicitly references message 0
			Index: 5, Role: "assistant",
			Content: []types.ContentBlock{
				{Type: "text", Text: "As I noted in message 0, the original content was..."},
			},
		},
	}

	g := NewFileOpGraph()
	saved := g.PruneRedundant(msgs, 6)

	if saved != 0 {
		t.Errorf("referenced message should not be pruned, got saved=%d", saved)
	}
	// Content at message 0 should be unchanged
	if msgs[0].Content[0].Text != strings.Repeat("file content\n", 50) {
		t.Error("referenced message content should not be modified")
	}
}

func TestMessageReferencesIndex_Detected(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "original"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "see message 0 for context"}}},
	}
	if !messageReferencesIndex(msgs, 0, 2) {
		t.Error("should detect reference to message 0")
	}
}

func TestMessageReferencesIndex_NotDetected(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "original"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "no reference here"}}},
	}
	if messageReferencesIndex(msgs, 0, 2) {
		t.Error("should not detect reference when none exists")
	}
}

func TestExtractFileOp_WriteTool(t *testing.T) {
	t.Parallel()
	block := types.ContentBlock{
		Type:      "tool_use",
		ToolName:  "Write",
		ToolInput: `{"path": "pkg/new.go", "content": "package main"}`,
	}
	path, opType, _ := extractFileOp(block)
	if path != "pkg/new.go" {
		t.Errorf("expected pkg/new.go, got %q", path)
	}
	if opType != FileOpWrite {
		t.Errorf("Write tool should give FileOpWrite, got %d", opType)
	}
}

func TestExtractFileOp_ReadTool(t *testing.T) {
	t.Parallel()
	block := types.ContentBlock{
		Type:      "tool_use",
		ToolName:  "Read",
		ToolInput: `{"path": "pkg/foo.go"}`,
	}
	path, opType, _ := extractFileOp(block)
	if path != "pkg/foo.go" {
		t.Errorf("expected pkg/foo.go, got %q", path)
	}
	if opType != FileOpRead {
		t.Errorf("Read tool should give FileOpRead, got %d", opType)
	}
}

func TestExtractFileOp_UnknownTool_NoPath(t *testing.T) {
	t.Parallel()
	block := types.ContentBlock{
		Type:      "tool_use",
		ToolName:  "Bash",
		ToolInput: `{"cmd": "ls"}`,
	}
	path, _, _ := extractFileOp(block)
	if path != "" {
		t.Errorf("Bash without path should return empty path, got %q", path)
	}
}

func TestContainsIgnoreCase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		s, sub string
		want   bool
	}{
		{"Hello World", "hello", true},
		{"Hello World", "WORLD", true},
		{"Hello World", "xyz", false},
		{"", "a", false},
		{"a", "", true},
	}
	for _, tc := range tests {
		got := containsIgnoreCase(tc.s, tc.sub)
		if got != tc.want {
			t.Errorf("containsIgnoreCase(%q, %q) = %v, want %v", tc.s, tc.sub, got, tc.want)
		}
	}
}

// TestPruneFileRead_AllBranches covers three previously uncovered paths in pruneFileRead:
// 1. block.Type != "tool_result" -> continue (line 138-139)
// 2. fp != path -> continue (line 143-144)
// 3. len(stub) >= len(orig) -> no replacement (original content too short)
func TestPruneFileRead_AllBranches(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{
			Index: 0, Role: "user",
			Content: []types.ContentBlock{
				// Block 0: not a tool_result -> triggers "continue" in block.Type check.
				{Type: "text", Text: "plain text block"},
				// Block 1: tool_result but wrong path -> triggers "continue" in fp != path check.
				{Type: "tool_result", Text: "other file content", ToolInput: `{"path": "other.go"}`},
				// Block 2: correct path but orig shorter than stub -> no replacement, saved=0.
				{Type: "tool_result", Text: "x", ToolInput: `{"path": "p"}`},
			},
		},
	}
	saved := pruneFileRead(msgs, 0, "p", 5, nil)
	if saved != 0 {
		t.Errorf("expected 0 saved (stub longer than orig), got %d", saved)
	}
	if msgs[0].Content[2].Text != "x" {
		t.Errorf("content should be unchanged when stub >= orig, got %q", msgs[0].Content[2].Text)
	}
}

// TestExtractPathFromInput covers the non-empty toolInput branch of extractPathFromInput.
func TestExtractPathFromInput(t *testing.T) {
	t.Parallel()
	// Empty input returns empty
	if got := extractPathFromInput(""); got != "" {
		t.Errorf("empty input: want empty, got %q", got)
	}
	// Valid JSON with "path" key returns the path
	got := extractPathFromInput(`{"path": "/src/main.go"}`)
	if got != "/src/main.go" {
		t.Errorf("path extraction: want /src/main.go, got %q", got)
	}
	// Valid JSON with "file_path" key
	got2 := extractPathFromInput(`{"file_path": "/internal/foo.go"}`)
	if got2 != "/internal/foo.go" {
		t.Errorf("file_path extraction: want /internal/foo.go, got %q", got2)
	}
	// JSON without any path key returns empty
	got3 := extractPathFromInput(`{"cmd": "ls"}`)
	if got3 != "" {
		t.Errorf("no path key: want empty, got %q", got3)
	}
}

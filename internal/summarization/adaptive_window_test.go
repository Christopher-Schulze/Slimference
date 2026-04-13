package summarization

import (
	"testing"

	"github.com/tokenproxy/tokenproxy/internal/types"
)

func TestAdaptiveWindowSize_NotEnoughMessages(t *testing.T) {
	t.Parallel()
	// Fewer messages than baseWindow+2: return baseWindow unchanged
	msgs := makeWindowTestMsgs(4)
	got := AdaptiveWindowSize(msgs, 5)
	if got != 5 {
		t.Errorf("not enough messages: want 5, got %d", got)
	}
}

func TestAdaptiveWindowSize_SimpleSession_ShrinkWindow(t *testing.T) {
	t.Parallel()
	// Simple Q&A: no tools, no anchors, no files -> low complexity -> window shrinks
	msgs := makeWindowTestMsgs(15) // 15 plain text messages, no tools
	base := 5
	got := AdaptiveWindowSize(msgs, base)
	// Score ~0: adjusted = 5 + round(0.0 * 4) - 2 = 3
	// Clamped to [max(3, 3), 7] = [3, 7] -> 3
	if got > base {
		t.Errorf("simple session should not increase window: base=%d, got=%d", base, got)
	}
	if got < 3 {
		t.Errorf("window should not go below 3, got %d", got)
	}
}

func TestAdaptiveWindowSize_ComplexSession_ExpandWindow(t *testing.T) {
	t.Parallel()
	// Complex session: many tool calls and edit operations -> high complexity -> window expands
	msgs := makeComplexSessionMsgs(20)
	base := 5
	got := AdaptiveWindowSize(msgs, base)
	if got < base {
		t.Errorf("complex session should not shrink window: base=%d, got=%d", base, got)
	}
	if got > base+2 {
		t.Errorf("window should not exceed baseWindow+2=%d, got %d", base+2, got)
	}
}

func TestAdaptiveWindowSize_MinimumThree(t *testing.T) {
	t.Parallel()
	// Even with base=3 and simple session, minimum is always 3
	msgs := makeWindowTestMsgs(20)
	got := AdaptiveWindowSize(msgs, 3)
	if got < 3 {
		t.Errorf("window should never go below 3, got %d", got)
	}
}

func TestAdaptiveWindowSize_MaximumBaseWindowPlusTwo(t *testing.T) {
	t.Parallel()
	msgs := makeComplexSessionMsgs(30)
	base := 5
	got := AdaptiveWindowSize(msgs, base)
	if got > base+2 {
		t.Errorf("window should not exceed baseWindow+2=%d, got %d", base+2, got)
	}
}

func TestNormalizeLinear(t *testing.T) {
	t.Parallel()
	tests := []struct {
		v, lo, hi float64
		want      float64
	}{
		{1, 1, 15, 0.0},
		{15, 1, 15, 1.0},
		{8, 1, 15, 0.5},
		{0, 1, 15, 0.0},  // clamped below lo
		{20, 1, 15, 1.0}, // clamped above hi
	}
	for _, tc := range tests {
		got := normalizeLinear(tc.v, tc.lo, tc.hi)
		if abs64(got-tc.want) > 0.01 {
			t.Errorf("normalizeLinear(%v, %v, %v) = %v, want %v", tc.v, tc.lo, tc.hi, got, tc.want)
		}
	}
}

func TestCountUniqueFilePaths(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Content: []types.ContentBlock{
			{Type: "tool_use", ToolInput: `{"path": "foo.go"}`},
			{Type: "tool_use", ToolInput: `{"path": "bar.go"}`},
		}},
		{Content: []types.ContentBlock{
			{Type: "tool_use", ToolInput: `{"path": "foo.go"}`}, // duplicate
		}},
	}
	got := countUniqueFilePaths(msgs)
	if got != 2 {
		t.Errorf("expected 2 unique paths, got %d", got)
	}
}

func TestCountToolDiversity(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Content: []types.ContentBlock{
			{Type: "tool_use", ToolName: "Read"},
			{Type: "tool_use", ToolName: "Edit"},
		}},
		{Content: []types.ContentBlock{
			{Type: "tool_use", ToolName: "read"}, // same as Read (case-insensitive)
			{Type: "tool_use", ToolName: "Bash"},
		}},
	}
	got := countToolDiversity(msgs)
	if got != 3 { // Read, Edit, Bash
		t.Errorf("expected 3 unique tools, got %d", got)
	}
}

// helpers

func makeWindowTestMsgs(n int) []types.Message {
	msgs := make([]types.Message, n)
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = types.Message{
			Index: i,
			Role:  role,
			Content: []types.ContentBlock{
				{Type: "text", Text: "plain text message"},
			},
		}
	}
	return msgs
}

func makeComplexSessionMsgs(n int) []types.Message {
	msgs := make([]types.Message, n)
	tools := []string{"Read", "Edit", "Bash", "Grep", "Write", "Glob", "Search", "List"}
	paths := []string{
		"pkg/foo.go", "pkg/bar.go", "internal/types/types.go",
		"cmd/main.go", "internal/config/config.go", "tests/foo_test.go",
		"internal/proxy/proxy.go", "internal/compression/layer1.go",
	}
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		tool := tools[i%len(tools)]
		path := paths[i%len(paths)]
		msgs[i] = types.Message{
			Index: i,
			Role:  role,
			Content: []types.ContentBlock{
				{
					Type:      "tool_use",
					ToolName:  tool,
					ToolInput: `{"path": "` + path + `"}`,
				},
			},
		}
	}
	return msgs
}

func TestExtractBlockFilePath_NoMatch(t *testing.T) {
	t.Parallel()
	block := types.ContentBlock{
		Type:      "tool_use",
		ToolInput: `{"cmd": "ls -la"}`,
	}
	got := extractBlockFilePath(block)
	if got != "" {
		t.Errorf("no path key should return empty, got %q", got)
	}
}

func TestExtractBlockFilePath_EmptyInput(t *testing.T) {
	t.Parallel()
	block := types.ContentBlock{Type: "tool_use"}
	got := extractBlockFilePath(block)
	if got != "" {
		t.Errorf("empty ToolInput should return empty, got %q", got)
	}
}

func TestNormalizeLinear_EqualBounds(t *testing.T) {
	t.Parallel()
	// lo == hi -> always return 0
	got := normalizeLinear(5, 5, 5)
	if got != 0 {
		t.Errorf("equal bounds should return 0, got %v", got)
	}
}

func TestAnchorDensity_Empty(t *testing.T) {
	t.Parallel()
	score := anchorDensity(nil)
	if score != 0 {
		t.Errorf("empty messages should return 0, got %v", score)
	}
}

func TestAdaptiveWindowSize_ZeroMsgs(t *testing.T) {
	t.Parallel()
	got := AdaptiveWindowSize(nil, 5)
	if got != 5 {
		t.Errorf("zero messages should return base window, got %d", got)
	}
}

func abs64(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// TestAdaptiveWindowSize_recentStartClamped covers the recentStart<0 → recentStart=0
// branch, which fires when len(messages) is between baseWindow+2 and 9 (< 10).
func TestAdaptiveWindowSize_recentStartClamped(t *testing.T) {
	t.Parallel()
	// base=5, need len >= 7 (>= baseWindow+2) AND len < 10 (so len-10 < 0 → start=0).
	// Use 8 plain text messages so recentStart = 8-10 = -2 → clamped to 0.
	msgs := makeWindowTestMsgs(8)
	got := AdaptiveWindowSize(msgs, 5)
	if got < 3 {
		t.Errorf("clamped start: window should be ≥ 3, got %d", got)
	}
	if got > 7 {
		t.Errorf("clamped start: window should be ≤ 7, got %d", got)
	}
}

// TestComputeComplexityScore_Empty covers the len(msgs)==0 branch (returns 0.5).
func TestComputeComplexityScore_Empty(t *testing.T) {
	t.Parallel()
	got := computeComplexityScore(nil)
	if got != 0.5 {
		t.Errorf("empty msgs: want 0.5, got %v", got)
	}
}

// TestExtractBlockFilePath_ValidPath covers the successful extraction path.
func TestExtractBlockFilePath_ValidPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
		want  string
	}{
		{`{"path": "/src/main.go"}`, "/src/main.go"},
		{`{"file_path": "/internal/foo.go"}`, "/internal/foo.go"},
		{`{"filename": "bar.py"}`, "bar.py"},
		{`{"filepath": "/tmp/x.ts"}`, "/tmp/x.ts"},
		{`{"file": "script.sh"}`, "script.sh"},
	}
	for _, c := range cases {
		block := types.ContentBlock{Type: "tool_use", ToolInput: c.input}
		got := extractBlockFilePath(block)
		if got != c.want {
			t.Errorf("extractBlockFilePath(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// TestExtractBlockFilePath_MalformedValue covers edge cases where key exists but value is malformed.
func TestExtractBlockFilePath_MalformedValue(t *testing.T) {
	t.Parallel()
	cases := []struct {
		desc  string
		input string
	}{
		{"no colon after key", `{"path" "value"}`},
		{"non-string value", `{"path": 42}`},
		{"unclosed string", `{"path": "unterminated}`},
	}
	for _, c := range cases {
		block := types.ContentBlock{Type: "tool_use", ToolInput: c.input}
		got := extractBlockFilePath(block)
		if got != "" {
			t.Errorf("%s: want empty, got %q", c.desc, got)
		}
	}
}

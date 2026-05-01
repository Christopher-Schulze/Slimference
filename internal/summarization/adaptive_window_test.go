package summarization

import (
	"testing"

	"github.com/slimference/slimference/internal/types"
)

func TestResolveWindow_Disabled(t *testing.T) {
	t.Parallel()
	msgs := makeWindowTestMsgs(4)
	wd := ResolveWindow(msgs, 5, false, 3, 12)
	if wd.Size != 5 {
		t.Errorf("disabled: want 5, got %d", wd.Size)
	}
	if wd.Reason != "adaptive disabled" {
		t.Errorf("reason: got %q", wd.Reason)
	}
}

func TestResolveWindow_NotEnoughMessages(t *testing.T) {
	t.Parallel()
	msgs := makeWindowTestMsgs(4)
	wd := ResolveWindow(msgs, 5, true, 3, 12)
	if wd.Size != 5 {
		t.Errorf("not enough messages: want 5, got %d", wd.Size)
	}
	if wd.Reason != "too few messages" {
		t.Errorf("reason: got %q", wd.Reason)
	}
}

func TestResolveWindow_SimpleSession_ShrinkWindow(t *testing.T) {
	t.Parallel()
	msgs := makeWindowTestMsgs(15)
	wd := ResolveWindow(msgs, 5, true, 3, 12)
	if wd.Size > 5 {
		t.Errorf("simple session should not increase window: got=%d", wd.Size)
	}
	if wd.Size < 3 {
		t.Errorf("window should not go below min=3, got %d", wd.Size)
	}
}

func TestResolveWindow_ComplexSession_ExpandWindow(t *testing.T) {
	t.Parallel()
	msgs := makeComplexSessionMsgs(20)
	wd := ResolveWindow(msgs, 5, true, 3, 12)
	if wd.Size < 5 {
		t.Errorf("complex session should not shrink window: got=%d", wd.Size)
	}
}

func TestResolveWindow_MinBound(t *testing.T) {
	t.Parallel()
	msgs := makeWindowTestMsgs(20)
	wd := ResolveWindow(msgs, 3, true, 3, 12)
	if wd.Size < 3 {
		t.Errorf("window should never go below min=3, got %d", wd.Size)
	}
}

func TestResolveWindow_MaxBound(t *testing.T) {
	t.Parallel()
	msgs := makeComplexSessionMsgs(30)
	wd := ResolveWindow(msgs, 5, true, 3, 12)
	if wd.Size > 12 {
		t.Errorf("window should not exceed max=12, got %d", wd.Size)
	}
}

func TestResolveWindow_ClampedToMax(t *testing.T) {
	t.Parallel()
	msgs := makeComplexSessionMsgs(30)
	wd := ResolveWindow(msgs, 10, true, 3, 4)
	if wd.Size > 4 {
		t.Errorf("should not exceed max=4, got %d", wd.Size)
	}
}

func TestResolveWindow_NoChange(t *testing.T) {
	t.Parallel()
	msgs := makeWindowTestMsgs(20)
	for i := range msgs {
		msgs[i].Content = []types.ContentBlock{
			{Type: "text", Text: "some text"},
			{Type: "tool_use", ToolName: "Read", ToolInput: `{"path": "file.go"}`},
		}
	}
	wd := ResolveWindow(msgs, 5, true, 3, 12)
	_ = wd
}

func TestResolveWindow_String(t *testing.T) {
	t.Parallel()
	wd := ResolveWindow(nil, 5, false, 3, 12)
	s := wd.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

func TestResolveWindow_DefaultBounds(t *testing.T) {
	t.Parallel()
	msgs := makeWindowTestMsgs(20)
	wd := ResolveWindow(msgs, 5, true, 0, 0)
	if wd.Min != 3 {
		t.Errorf("default min should be 3, got %d", wd.Min)
	}
	if wd.Max != 12 {
		t.Errorf("default max should be 12, got %d", wd.Max)
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

func TestResolveWindow_ZeroMsgs(t *testing.T) {
	t.Parallel()
	wd := ResolveWindow(nil, 5, true, 3, 12)
	if wd.Size != 5 {
		t.Errorf("zero messages should return base window, got %d", wd.Size)
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
func TestResolveWindow_RecentStartClamped(t *testing.T) {
	t.Parallel()
	msgs := makeWindowTestMsgs(8)
	wd := ResolveWindow(msgs, 5, true, 3, 12)
	if wd.Size < 3 {
		t.Errorf("clamped start: window should be >= 3, got %d", wd.Size)
	}
	if wd.Size > 7 {
		t.Errorf("clamped start: window should be <= 7, got %d", wd.Size)
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

package summarization

import (
	"testing"

	"github.com/slimference/slimference/internal/types"
)

// msg builds a Message with a single text block.
func msg(t *testing.T, index int, role, text string) types.Message {
	t.Helper()
	return types.Message{
		Index: index,
		Role:  role,
		Content: []types.ContentBlock{
			{Type: "text", Text: text},
		},
	}
}

// toolUseMsg builds a Message with a tool_use block.
func toolUseMsg(t *testing.T, index int, toolName string) types.Message {
	t.Helper()
	return types.Message{
		Index: index,
		Role:  "assistant",
		Content: []types.ContentBlock{
			{Type: "tool_use", ToolName: toolName},
		},
	}
}

// toolResultMsg builds a Message with a tool_result block.
func toolResultMsg(t *testing.T, index int, text string) types.Message {
	t.Helper()
	return types.Message{
		Index: index,
		Role:  "user",
		Content: []types.ContentBlock{
			{Type: "tool_result", Text: text},
		},
	}
}

// TestAnchorDetector_EditTool verifies that a tool_use with an edit tool name is an anchor.
func TestAnchorDetector_EditTool(t *testing.T) {
	t.Parallel()

	d := NewAnchorDetector()
	tests := []struct {
		name       string
		toolName   string
		wantAnchor bool
	}{
		{"edit tool", "edit_file", true},
		{"write tool", "write_file", true},
		{"create tool", "create_file", true},
		{"delete tool", "delete_file", true},
		{"read tool", "read_file", false},
		{"bash tool", "bash", false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := toolUseMsg(t, 0, tc.toolName)
			got := d.IsAnchor(m, []types.Message{m})
			if got != tc.wantAnchor {
				t.Errorf("IsAnchor(%q) = %v, want %v", tc.toolName, got, tc.wantAnchor)
			}
		})
	}
}

// TestAnchorDetector_ErrorContent verifies that error/stack trace content is an anchor.
func TestAnchorDetector_ErrorContent(t *testing.T) {
	t.Parallel()

	d := NewAnchorDetector()
	tests := []struct {
		name     string
		text     string
		isAnchor bool
	}{
		{
			name:     "error prefix",
			text:     "error: connection refused\nat main.go:42\nat runtime.go:100",
			isAnchor: true,
		},
		{
			name:     "panic message",
			text:     "panic: runtime error: index out of range",
			isAnchor: true,
		},
		{
			name:     "normal assistant message",
			text:     "I have updated the file as requested.",
			isAnchor: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := msg(t, 0, "assistant", tc.text)
			got := d.IsAnchor(m, []types.Message{m})
			if got != tc.isAnchor {
				t.Errorf("IsAnchor for %q = %v, want %v", tc.name, got, tc.isAnchor)
			}
		})
	}
}

// TestAnchorDetector_DecisionYes verifies that short user decision messages are anchors.
func TestAnchorDetector_DecisionYes(t *testing.T) {
	t.Parallel()

	d := NewAnchorDetector()
	tests := []struct {
		name     string
		role     string
		text     string
		isAnchor bool
	}{
		{"user yes", "user", "yes", true},
		{"user go ahead", "user", "go ahead", true},
		{"user approved", "user", "approved", true},
		{"user no", "user", "no", true},
		{"user cancel", "user", "cancel", true},
		{"assistant yes - not a decision", "assistant", "yes", false},
		{"user long message - not a decision", "user",
			"Yes I agree with your approach and I think it is the best way to handle the situation given the constraints we have discussed at length in our previous conversations and I am confident the team will execute well on this important strategic plan over the coming weeks and months ahead",
			false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := msg(t, 0, tc.role, tc.text)
			got := d.IsAnchor(m, []types.Message{m})
			if got != tc.isAnchor {
				t.Errorf("IsAnchor(%q, %q) = %v, want %v", tc.role, tc.text, got, tc.isAnchor)
			}
		})
	}
}

// TestAnchorDetector_ConfigFile verifies that tool results referencing config file paths are anchors.
func TestAnchorDetector_ConfigFile(t *testing.T) {
	t.Parallel()

	d := NewAnchorDetector()
	tests := []struct {
		name     string
		text     string
		isAnchor bool
	}{
		{"toml file reference", "/project/config.toml", true},
		{"yaml file reference", "app.yaml", true},
		{"env file reference", ".env", true},
		{"Makefile reference", "Makefile", true},
		{"regular text", "just some output text", false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := toolResultMsg(t, 0, tc.text)
			got := d.IsAnchor(m, []types.Message{m})
			if got != tc.isAnchor {
				t.Errorf("IsAnchor for config %q = %v, want %v", tc.text, got, tc.isAnchor)
			}
		})
	}
}

// TestAnchorDetector_NormalMessage verifies that an unremarkable message is not an anchor.
func TestAnchorDetector_NormalMessage(t *testing.T) {
	t.Parallel()

	d := NewAnchorDetector()
	m := msg(t, 0, "assistant", "The function returns the sum of two integers.")
	if d.IsAnchor(m, []types.Message{m}) {
		t.Error("normal message incorrectly flagged as anchor")
	}
}

// TestFilterNonAnchored verifies that anchored messages are excluded from the result.
func TestFilterNonAnchored(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		msg(t, 0, "user", "first"),
		msg(t, 1, "assistant", "second"),
		msg(t, 2, "user", "third"),
		msg(t, 3, "assistant", "fourth"),
	}

	// Mark indices 1 and 3 as anchors.
	anchorIndices := []int{1, 3}
	result := filterNonAnchored(messages, anchorIndices)

	if len(result) != 2 {
		t.Fatalf("filterNonAnchored returned %d messages, want 2", len(result))
	}
	if result[0].Content[0].Text != "first" {
		t.Errorf("result[0] text = %q, want %q", result[0].Content[0].Text, "first")
	}
	if result[1].Content[0].Text != "third" {
		t.Errorf("result[1] text = %q, want %q", result[1].Content[0].Text, "third")
	}
}

func TestAnchorDetector_Detect_editChain(t *testing.T) {
	t.Parallel()
	d := NewAnchorDetector()
	messages := []types.Message{
		toolUseMsg(t, 0, "edit_file"),
		toolResultMsg(t, 1, "ok"),
	}
	got := d.Detect(messages)
	if len(got) < 1 {
		t.Fatalf("expected anchors: %v", got)
	}
}

// TestAnchorDetector_StackTrace covers the stackTrace branch in isAnchorError (lines 108-110).
func TestAnchorDetector_StackTrace(t *testing.T) {
	t.Parallel()
	d := NewAnchorDetector()
	// Contains a Go goroutine stack trace signature but no plain "error" keyword.
	stackText := "goroutine 1 [running]:\nmain.foo()\n\t/app/main.go:42 +0x24\n"
	m := msg(t, 0, "assistant", stackText)
	if !d.IsAnchor(m, []types.Message{m}) {
		t.Error("stack trace message should be an anchor")
	}
}

// TestAnchorDetector_ArchitectBullets covers isAnchorArchitect lines 137-138.
// An assistant message with architecture keywords AND > 3 bullet items must be anchored.
func TestAnchorDetector_ArchitectBullets(t *testing.T) {
	t.Parallel()
	d := NewAnchorDetector()
	// Has "architecture" keyword and 4 bullet points -> anchor.
	architectText := "This is the architecture plan:\n- item one\n- item two\n- item three\n- item four\nApproach is solid."
	m := msg(t, 0, "assistant", architectText)
	if !d.IsAnchor(m, []types.Message{m}) {
		t.Error("architect message with >3 bullets should be anchor")
	}
	// Only 3 bullets -> not an anchor via architect (may still pass other checks).
	threeBullets := "This is the architecture plan:\n- item one\n- item two\n- item three\nApproach is solid."
	m2 := msg(t, 0, "assistant", threeBullets)
	// Only test that the architect rule alone doesn't fire; use isAnchorArchitect directly.
	if d.isAnchorArchitect(m2) {
		t.Error("3 bullets should not trigger architect anchor (needs >3)")
	}
}

// TestFullText_MultipleBlocks covers the newline-join branch in fullText (lines 183-185).
func TestFullText_MultipleBlocks(t *testing.T) {
	t.Parallel()
	m := types.Message{
		Index: 0,
		Role:  "assistant",
		Content: []types.ContentBlock{
			{Type: "text", Text: "first block"},
			{Type: "text", Text: "second block"},
		},
	}
	got := fullText(m)
	const want = "first block\nsecond block"
	if got != want {
		t.Errorf("fullText = %q, want %q", got, want)
	}
}

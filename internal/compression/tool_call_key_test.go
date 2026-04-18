package compression

import (
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/types"
)

// TestExtractToolCallKey_preferFilepath prefers a filepath when present.
func TestExtractToolCallKey_preferFilepath(t *testing.T) {
	t.Parallel()
	block := types.ContentBlock{
		ToolName:  "Read",
		ToolInput: `{"path":"/tmp/a.go"}`,
		Text:      "package a",
	}
	if got := ExtractToolCallKey(block); got != "file:/tmp/a.go" {
		t.Fatalf("key: %q", got)
	}
}

// TestExtractToolCallKey_emptyBlock yields empty string.
func TestExtractToolCallKey_emptyBlock(t *testing.T) {
	t.Parallel()
	if got := ExtractToolCallKey(types.ContentBlock{}); got != "" {
		t.Fatalf("empty block must produce empty key, got %q", got)
	}
}

// TestExtractToolCallKey_toolOnly returns tool:<name> when no topic is
// recoverable.
func TestExtractToolCallKey_toolOnly(t *testing.T) {
	t.Parallel()
	block := types.ContentBlock{ToolName: "Bash"}
	if got := ExtractToolCallKey(block); got != "tool:bash" {
		t.Fatalf("key: %q", got)
	}
}

// TestExtractToolCallKey_fromToolInputCommand uses the first structured
// argument as a topic when no filepath is present.
func TestExtractToolCallKey_fromToolInputCommand(t *testing.T) {
	t.Parallel()
	block := types.ContentBlock{
		ToolName:  "Bash",
		ToolInput: `{"command":"git status"}`,
	}
	if got := ExtractToolCallKey(block); got != "tool:bash|git status" {
		t.Fatalf("key: %q", got)
	}
}

// TestExtractToolCallKey_fromToolInputKnownKeys covers each preferred key.
func TestExtractToolCallKey_fromToolInputKnownKeys(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
		want  string
	}{
		{`{"cmd":"ls /tmp"}`, "tool:bash|ls /tmp"},
		{`{"pattern":"TODO"}`, "tool:bash|TODO"},
		{`{"query":"how?"}`, "tool:bash|how?"},
		{`{"url":"https://x"}`, "tool:bash|https://x"},
		{`{"target":"build"}`, "tool:bash|build"},
		{`{"name":"something"}`, "tool:bash|something"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			block := types.ContentBlock{ToolName: "Bash", ToolInput: tc.input}
			if got := ExtractToolCallKey(block); got != tc.want {
				t.Errorf("input=%s: got %q want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestExtractToolCallKey_fromTextFallback uses first line of text when the
// structured input has no usable field.
func TestExtractToolCallKey_fromTextFallback(t *testing.T) {
	t.Parallel()
	block := types.ContentBlock{
		ToolName: "Grep",
		Text:     "first line\nsecond line",
	}
	if got := ExtractToolCallKey(block); got != "tool:grep|first line" {
		t.Fatalf("key: %q", got)
	}
}

// TestExtractToolCallKey_invalidToolInputSkipped falls back to text.
func TestExtractToolCallKey_invalidToolInputSkipped(t *testing.T) {
	t.Parallel()
	block := types.ContentBlock{
		ToolName:  "Grep",
		ToolInput: "{not json",
		Text:      "result line",
	}
	if got := ExtractToolCallKey(block); got != "tool:grep|result line" {
		t.Fatalf("key: %q", got)
	}
}

// TestExtractToolCallKey_topicTruncated limits the topic length.
func TestExtractToolCallKey_topicTruncated(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", 200)
	block := types.ContentBlock{ToolName: "Grep", Text: long}
	got := ExtractToolCallKey(block)
	if !strings.HasPrefix(got, "tool:grep|") {
		t.Fatalf("prefix: %s", got)
	}
	tail := strings.TrimPrefix(got, "tool:grep|")
	if len(tail) != 64 {
		t.Fatalf("tail len %d", len(tail))
	}
}

// TestExtractToolCallKey_emptyLineFallback returns tool: when text is only
// whitespace.
func TestExtractToolCallKey_emptyLineFallback(t *testing.T) {
	t.Parallel()
	block := types.ContentBlock{ToolName: "Grep", Text: "   \t\n   "}
	if got := ExtractToolCallKey(block); got != "tool:grep" {
		t.Fatalf("key: %q", got)
	}
}

// TestTopicFromToolInput_noKnownKeys covers the "no known key matched" path.
func TestTopicFromToolInput_noKnownKeys(t *testing.T) {
	t.Parallel()
	if got := topicFromToolInput(`{"path":"/x","extra":"y"}`); got != "" {
		t.Fatalf("path is handled by extractFilepath, topicFromToolInput must return empty when only unknown keys remain: %q", got)
	}
	if got := topicFromToolInput(`{"unknown":"value"}`); got != "" {
		t.Fatalf("unknown-only input must return empty, got %q", got)
	}
}

// TestTruncateRunes covers the zero/negative, short, and long branches.
func TestTruncateRunes(t *testing.T) {
	t.Parallel()
	if got := truncateRunes("abc", 0); got != "" {
		t.Fatalf("zero: %q", got)
	}
	if got := truncateRunes("abc", -1); got != "" {
		t.Fatalf("negative: %q", got)
	}
	if got := truncateRunes("abc", 10); got != "abc" {
		t.Fatalf("short: %q", got)
	}
	if got := truncateRunes("abcde", 3); got != "abc" {
		t.Fatalf("long: %q", got)
	}
	// Multibyte: ensure we cut at rune boundary, not byte.
	if got := truncateRunes("äöü", 2); got != "äö" {
		t.Fatalf("multibyte: %q", got)
	}
}

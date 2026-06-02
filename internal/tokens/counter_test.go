package tokens

import (
	"strconv"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/types"
)

// TestCount_NonEmpty verifies that Count returns > 0 for a non-empty string.
func TestCount_NonEmpty(t *testing.T) {
	t.Parallel()
	got := Count("Hello, world!")
	if got <= 0 {
		t.Errorf("Count(\"Hello, world!\") = %d, want > 0", got)
	}
}

// TestCount_Empty verifies that Count returns 0 for the empty string.
func TestCount_Empty(t *testing.T) {
	t.Parallel()
	got := Count("")
	if got != 0 {
		t.Errorf("Count(\"\") = %d, want 0", got)
	}
}

// TestCount_LongerTextMoreTokens verifies the monotonicity property:
// a longer string should produce a strictly larger token count.
func TestCount_LongerTextMoreTokens(t *testing.T) {
	t.Parallel()
	short := "Hello"
	long := "Hello, this is a substantially longer sentence with many more words."
	ts := Count(short)
	tl := Count(long)
	if tl <= ts {
		t.Errorf("Count(long) = %d <= Count(short) = %d, want strictly greater", tl, ts)
	}
}

// TestCountString_Alias verifies that CountString returns the same result as Count.
func TestCountString_Alias(t *testing.T) {
	t.Parallel()
	text := "The quick brown fox jumps over the lazy dog."
	if CountString(text) != Count(text) {
		t.Errorf("CountString(%q) != Count(%q)", text, text)
	}
}

// TestEstimate verifies the byteLen/4 approximation.
func TestEstimate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input int
		want  int
	}{
		{0, 0},
		{3, 0},
		{4, 1},
		{100, 25},
		{1000, 250},
	}
	for _, tt := range tests {
		got := Estimate(tt.input)
		if got != tt.want {
			t.Errorf("Estimate(%d) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

// TestCountMessages_SingleBlock verifies token counting for a single-message, single-block slice.
func TestCountMessages_SingleBlock(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{
			Index: 0,
			Role:  "user",
			Content: []types.ContentBlock{
				{Type: "text", Text: "Hello, world!"},
			},
		},
	}
	got := CountMessages(msgs)
	if got <= 0 {
		t.Errorf("CountMessages() = %d, want > 0", got)
	}
}

// TestCountMessages_MultipleBlocks verifies that all text, tool_input, and tool_name are counted.
func TestCountMessages_MultipleBlocks(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{
			Content: []types.ContentBlock{
				{Type: "text", Text: "Some text"},
				{Type: "tool_use", ToolName: "read_file", ToolInput: `{"path":"/foo.go"}`},
			},
		},
	}
	got := CountMessages(msgs)
	// We expect at least as many tokens as just "Some text" alone.
	textOnly := Count("Some text")
	if got <= textOnly {
		t.Errorf("CountMessages() = %d, want > Count(text only) = %d", got, textOnly)
	}
}

// TestCountMessages_EmptyMessages verifies that an empty slice returns 0.
func TestCountMessages_EmptyMessages(t *testing.T) {
	t.Parallel()
	got := CountMessages(nil)
	if got != 0 {
		t.Errorf("CountMessages(nil) = %d, want 0", got)
	}
}

// TestCountMessages_EmptyBlocks verifies that a message with empty content returns 0.
func TestCountMessages_EmptyBlocks(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Content: []types.ContentBlock{{Type: "text", Text: ""}}},
	}
	got := CountMessages(msgs)
	if got != 0 {
		t.Errorf("CountMessages with empty text = %d, want 0", got)
	}
}

// TestCount_LargeText verifies that Count handles large inputs without panicking.
func TestCount_LargeText(t *testing.T) {
	t.Parallel()
	large := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 10000)
	got := Count(large)
	if got <= 0 {
		t.Errorf("Count(large text) = %d, want > 0", got)
	}
}

// TestCounter_NilEncoder covers the enc==nil branch in Count (counter.go:38-40).
// We exhaust the sync.Once without setting enc, then Count must return 0.
func TestCounter_NilEncoder(t *testing.T) {
	t.Parallel()
	c := &Counter{}
	// Exhaust the once with a no-op, leaving c.enc == nil.
	c.once.Do(func() {})
	if got := c.Count("hello world"); got != 0 {
		t.Errorf("Count with nil encoder = %d, want 0", got)
	}
}

func TestTokenCountCache_LargeTextExactHit(t *testing.T) {
	resetTokenCountCacheForTest()
	text := strings.Repeat("package tokens\nfunc cached() {}\n", 300)
	want := Count(text)
	if want <= 0 {
		t.Fatalf("Count(large) = %d, want > 0", want)
	}
	if len(tokenCountCache.values) != 1 {
		t.Fatalf("cache entries after first count = %d, want 1", len(tokenCountCache.values))
	}
	if got := Count(text); got != want {
		t.Fatalf("cached Count(large) = %d, want %d", got, want)
	}
	if len(tokenCountCache.values) != 1 {
		t.Fatalf("cache entries after second count = %d, want 1", len(tokenCountCache.values))
	}
}

func TestTokenCountCache_EncodingScoped(t *testing.T) {
	resetTokenCountCacheForTest()
	text := strings.Repeat("encoding scoped token cache\n", 300)
	cl100k := (&Counter{}).Count(text)
	o200k := (&Counter{encoding: "o200k_base"}).Count(text)
	if cl100k <= 0 || o200k <= 0 {
		t.Fatalf("counts = %d/%d, want both > 0", cl100k, o200k)
	}
	if len(tokenCountCache.values) != 2 {
		t.Fatalf("cache entries = %d, want 2", len(tokenCountCache.values))
	}
}

func TestTokenCountCache_Bounded(t *testing.T) {
	resetTokenCountCacheForTest()
	for i := 0; i < tokenCountCacheMaxItems+8; i++ {
		text := "bounded token cache entry " + strconv.Itoa(i)
		tokenCountCachePut("cl100k_base", text, i+1)
	}
	if len(tokenCountCache.values) != tokenCountCacheMaxItems {
		t.Fatalf("cache entries = %d, want %d", len(tokenCountCache.values), tokenCountCacheMaxItems)
	}
	if len(tokenCountCache.order) != tokenCountCacheMaxItems {
		t.Fatalf("cache order = %d, want %d", len(tokenCountCache.order), tokenCountCacheMaxItems)
	}
}

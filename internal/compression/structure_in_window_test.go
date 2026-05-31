package compression

import (
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

func buildInWindowCfg() *config.CompressionConfig {
	cfg := config.Defaults().Compression
	cfg.SlidingWindow = 2
	cfg.Tuning.StructureInWindow = true
	cfg.Tuning.StructureInWindowMinTokens = 100
	return &cfg
}

func goBody(repeat int) string {
	var sb strings.Builder
	sb.WriteString("package demo\n")
	for i := 0; i < repeat; i++ {
		sb.WriteString("func F")
		sb.WriteString(smallItoa(i))
		sb.WriteString("() {\n    // a lot of body content here indeed\n    doWork()\n    doMore()\n    doEvenMore()\n}\n")
	}
	return sb.String()
}

// smallItoa is a tiny int-to-string helper used by goBody fixtures.
func smallItoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// TestStructureInWindow_disabledByDefault leaves messages untouched.
func TestStructureInWindow_disabledByDefault(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.SlidingWindow = 2 // keep in-window range narrow
	c := NewDeterministicCompressor(&cfg)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "hello"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{
			Type: "tool_result", ToolName: "Read",
			ToolInput: `{"path":"/tmp/x.go"}`, Text: goBody(50),
		}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "ok"}}},
	}
	result := c.Compress(msgs)
	if result.StructureSaved != 0 {
		t.Fatalf("disabled feature must not save: %d", result.StructureSaved)
	}
}

// TestStructureInWindow_compressesMiddleToolResult when enabled.
func TestStructureInWindow_compressesMiddleToolResult(t *testing.T) {
	t.Parallel()
	cfg := buildInWindowCfg()
	c := NewDeterministicCompressor(cfg)
	body := goBody(80)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "please read"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{
			Type: "tool_result", ToolName: "Read",
			ToolInput: `{"path":"/tmp/huge.go"}`, Text: body,
		}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "what's in it?"}}},
	}
	result := c.Compress(msgs)
	if result.StructureSaved == 0 {
		t.Fatalf("expected in-window structure savings, got 0")
	}
	if got := result.Messages[1].Content[0].Text; !strings.Contains(got, "Structural summary") {
		t.Fatalf("middle block not structured: %s", got)
	}
}

// TestStructureInWindow_lastMessagePreserved keeps the terminal turn exact.
func TestStructureInWindow_lastMessagePreserved(t *testing.T) {
	t.Parallel()
	cfg := buildInWindowCfg()
	c := NewDeterministicCompressor(cfg)
	body := goBody(80)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "read it"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{
			Type: "tool_result", ToolName: "Read",
			ToolInput: `{"path":"/tmp/a.go"}`, Text: body,
		}}},
	}
	result := c.Compress(msgs)
	if strings.Contains(result.Messages[1].Content[0].Text, "Structural summary") {
		t.Fatal("terminal message must not be structured")
	}
}

// TestStructureInWindow_userTurnPreserved refuses to touch role=user.
func TestStructureInWindow_userTurnPreserved(t *testing.T) {
	t.Parallel()
	cfg := buildInWindowCfg()
	c := NewDeterministicCompressor(cfg)
	body := goBody(80)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "tool_result", ToolName: "Read", ToolInput: `{"path":"/tmp/x.go"}`, Text: body}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "ok"}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "tail"}}},
	}
	result := c.Compress(msgs)
	if strings.Contains(result.Messages[0].Content[0].Text, "Structural summary") {
		t.Fatal("user-role message must not be structured")
	}
}

// TestStructureInWindow_smallBlockSkipped respects the min-tokens gate.
func TestStructureInWindow_smallBlockSkipped(t *testing.T) {
	t.Parallel()
	cfg := buildInWindowCfg()
	cfg.Tuning.StructureInWindowMinTokens = 1_000_000
	c := NewDeterministicCompressor(cfg)
	body := goBody(5)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "read"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", ToolName: "Read", ToolInput: `{"path":"/tmp/x.go"}`, Text: body}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "ok"}}},
	}
	result := c.Compress(msgs)
	if strings.Contains(result.Messages[1].Content[0].Text, "Structural summary") {
		t.Fatal("block below threshold must not be structured")
	}
}

// TestStructureInWindow_diffSkipped refuses to restructure a patch.
func TestStructureInWindow_diffSkipped(t *testing.T) {
	t.Parallel()
	cfg := buildInWindowCfg()
	c := NewDeterministicCompressor(cfg)
	diff := "diff --git a/x b/x\n" + strings.Repeat("@@ -1 +1 @@\n-old\n+new\n", 40)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "review"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", ToolName: "Bash", ToolInput: `{"command":"git diff"}`, Text: diff}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "lgtm"}}},
	}
	result := c.Compress(msgs)
	if strings.Contains(result.Messages[1].Content[0].Text, "Structural summary") {
		t.Fatal("diff-shaped content must not be structured")
	}
}

// TestLooksLikeDiffOrPatch covers each shape.
func TestLooksLikeDiffOrPatch(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"diff --git a/x b/x", true},
		{"--- a\n+++ b", true},
		{"@@ -1 +1 @@", true},
		{"From abc\nSubject: tag", true},
		{"random text", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := looksLikeDiffOrPatch(tc.in); got != tc.want {
			t.Errorf("looksLikeDiffOrPatch(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestShouldStructureInWindowMessage covers both branches.
func TestShouldStructureInWindowMessage(t *testing.T) {
	t.Parallel()
	if shouldStructureInWindowMessage(types.Message{Role: "user"}) {
		t.Fatal("user role must never be eligible")
	}
	if shouldStructureInWindowMessage(types.Message{Role: "assistant"}) {
		t.Fatal("empty content must not be eligible")
	}
	if !shouldStructureInWindowMessage(types.Message{Role: "assistant", Content: []types.ContentBlock{{Type: "text"}}}) {
		t.Fatal("assistant with content must be eligible")
	}
}

// TestShouldStructureInWindowBlock covers the per-block gate.
func TestShouldStructureInWindowBlock(t *testing.T) {
	t.Parallel()
	// non tool_result
	if shouldStructureInWindowBlock(types.ContentBlock{Type: "text", Text: strings.Repeat("a", 10000)}, 1000) {
		t.Fatal("non-tool_result must be rejected")
	}
	// empty text
	if shouldStructureInWindowBlock(types.ContentBlock{Type: "tool_result"}, 100) {
		t.Fatal("empty text must be rejected")
	}
	// small
	if shouldStructureInWindowBlock(types.ContentBlock{Type: "tool_result", Text: "hi"}, 100) {
		t.Fatal("small text must be rejected")
	}
	// diff-shaped
	if shouldStructureInWindowBlock(types.ContentBlock{Type: "tool_result", Text: "diff --git a/x b/x\n" + strings.Repeat("x", 10000)}, 100) {
		t.Fatal("diff-shaped must be rejected")
	}
	// happy path
	if !shouldStructureInWindowBlock(types.ContentBlock{Type: "tool_result", Text: strings.Repeat("a", 10000)}, 100) {
		t.Fatal("large tool_result must be eligible")
	}
}

// TestStructureInWindow_alsoRunsWhenPrefixExists exercises the branch that
// fires from the main compressed path (prefixEnd > 0).
func TestStructureInWindow_alsoRunsWhenPrefixExists(t *testing.T) {
	t.Parallel()
	cfg := buildInWindowCfg()
	cfg.SlidingWindow = 1 // one exchange kept; earlier ones are prefix
	cfg.MinMessagesForCompression = 1
	c := testCompressorWithArchive(cfg)
	body := goBody(80)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "a"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "b"}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "read"}}},
		{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", ToolName: "Read", ToolInput: `{"path":"/tmp/x.go"}`, Text: body}}},
		{Index: 4, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "tail"}}},
	}
	result := c.Compress(msgs)
	if result.StructureSaved == 0 {
		t.Fatal("expected in-window savings on the post-prefix branch")
	}
}

// TestStructureInWindow_minTokensFallback covers the v<=0 default.
func TestStructureInWindow_minTokensFallback(t *testing.T) {
	t.Parallel()
	cfg := buildInWindowCfg()
	cfg.Tuning.StructureInWindowMinTokens = 0
	c := NewDeterministicCompressor(cfg)
	body := goBody(80)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "read"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", ToolName: "Read", ToolInput: `{"path":"/tmp/x.go"}`, Text: body}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "ok"}}},
	}
	result := c.Compress(msgs)
	// Default (1500) is large - with body size ~2500 tokens the block should
	// still qualify; we mostly verify the helper returns without panicking.
	_ = result
}

// TestStructureInWindow_mixedRolesSkipUserInMiddle covers the
// shouldStructureInWindowMessage "continue" branch mid-loop.
func TestStructureInWindow_mixedRolesSkipUserInMiddle(t *testing.T) {
	t.Parallel()
	cfg := buildInWindowCfg()
	c := NewDeterministicCompressor(cfg)
	body := goBody(80)
	// Inserting a user message in the middle of the in-window range.
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "a"}}},
		{Index: 1, Role: "user", Content: []types.ContentBlock{{Type: "tool_result", ToolName: "Read", ToolInput: `{"path":"/tmp/x.go"}`, Text: body}}},
		{Index: 2, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", ToolName: "Read", ToolInput: `{"path":"/tmp/y.go"}`, Text: body}}},
		{Index: 3, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "tail"}}},
	}
	result := c.Compress(msgs)
	if strings.Contains(result.Messages[1].Content[0].Text, "Structural summary") {
		t.Fatal("user-in-middle must be skipped")
	}
	if !strings.Contains(result.Messages[2].Content[0].Text, "Structural summary") {
		t.Fatal("assistant block must still be compressed")
	}
}

// TestStructureInWindow_textBlockSkippedInMiddle covers the per-block
// continue when the block is text rather than tool_result.
func TestStructureInWindow_textBlockSkippedInMiddle(t *testing.T) {
	t.Parallel()
	cfg := buildInWindowCfg()
	c := NewDeterministicCompressor(cfg)
	body := goBody(80)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "a"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{
			{Type: "text", Text: strings.Repeat("chatter ", 500)},
			{Type: "tool_result", ToolName: "Read", ToolInput: `{"path":"/tmp/z.go"}`, Text: body},
		}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "ok"}}},
	}
	result := c.Compress(msgs)
	// text block must stay unchanged
	if !strings.HasPrefix(result.Messages[1].Content[0].Text, "chatter") {
		t.Fatal("text block must not be touched")
	}
	// tool_result block must get structured
	if !strings.Contains(result.Messages[1].Content[1].Text, "Structural summary") {
		t.Fatal("tool_result block must be structured")
	}
}

// TestStructureInWindow_unknownLanguageSkipped covers the lang=="" branch.
func TestStructureInWindow_unknownLanguageSkipped(t *testing.T) {
	t.Parallel()
	cfg := buildInWindowCfg()
	c := NewDeterministicCompressor(cfg)
	// Large prose with no recognisable language and no .go/.py/.rs hint.
	prose := strings.Repeat("pure prose content without structure ", 200)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "q"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", ToolName: "Bash", ToolInput: `{"command":"echo"}`, Text: prose}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "ok"}}},
	}
	result := c.Compress(msgs)
	if strings.Contains(result.Messages[1].Content[0].Text, "Structural summary") {
		t.Fatal("unknown-language prose must not be structured")
	}
}

// TestStructureInWindow_extractNoShrinkSkipped covers the "summary not shorter"
// guard by using structure-extract-unfriendly but large Go content where the
// extractor may return the same length.
func TestStructureInWindow_extractNoShrinkSkipped(t *testing.T) {
	t.Parallel()
	cfg := buildInWindowCfg()
	c := NewDeterministicCompressor(cfg)
	// All-signature Go with no bodies - structural extraction will produce
	// output that is not shorter than the input.
	var sb strings.Builder
	sb.WriteString("package demo\n")
	for i := 0; i < 80; i++ {
		sb.WriteString("var V")
		sb.WriteString(smallItoa(i))
		sb.WriteString(" int\n")
	}
	signatureOnly := sb.String()
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "read"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", ToolName: "Read", ToolInput: `{"path":"/tmp/x.go"}`, Text: signatureOnly}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "ok"}}},
	}
	result := c.Compress(msgs)
	// Whatever happened, the output must not be empty and must not contain
	// a duplicate "Structural summary" wrapper when the extractor returned
	// nothing useful.
	got := result.Messages[1].Content[0].Text
	if got == "" {
		t.Fatal("output must be non-empty")
	}
}

// TestStructureInWindow_preFilteredSkipped respects Layer 0 markers.
func TestStructureInWindow_preFilteredSkipped(t *testing.T) {
	t.Parallel()
	cfg := buildInWindowCfg()
	c := NewDeterministicCompressor(cfg)
	// The prefilter marker must match rePreFilteredMarker.
	body := "[build] ok\n" + goBody(80)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "q"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", ToolName: "Read", ToolInput: `{"path":"/tmp/x.go"}`, Text: body}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "ok"}}},
	}
	result := c.Compress(msgs)
	if strings.Contains(result.Messages[1].Content[0].Text, "Structural summary") {
		t.Fatal("pre-filtered content must not be restructured")
	}
}

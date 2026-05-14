package compression

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/tokens"
	"github.com/slimference/slimference/internal/types"
)

// buildMessage is a test helper for constructing a Message with content blocks.
func buildMessage(t *testing.T, index int, role string, blocks ...types.ContentBlock) types.Message {
	t.Helper()
	return types.Message{
		Index:   index,
		Role:    role,
		Content: blocks,
	}
}

// textBlock builds a plain text ContentBlock.
func textBlock(text string) types.ContentBlock {
	return types.ContentBlock{Type: "text", Text: text}
}

// toolResultBlock builds a tool_result ContentBlock.
func toolResultBlock(text string) types.ContentBlock {
	return types.ContentBlock{Type: "tool_result", Text: text}
}

// defaultTestCfg returns a CompressionConfig with a sliding window of 2 for testing.
func defaultTestCfg(slidingWindow int) *config.CompressionConfig {
	return &config.CompressionConfig{
		SlidingWindow:             slidingWindow,
		MinMessagesForCompression: 1,
		MinTokensForLayer2:        30000,
		StructureMinTokens:        500,
		StructureLanguages:        []string{"go", "typescript", "python"},
		DedupSimilarityThreshold:  0.85,
		Layer1Enabled:             true,
	}
}

// TestCompress_BelowWindow verifies that messages below the sliding window pass through unchanged.
func TestCompress_BelowWindow(t *testing.T) {
	t.Parallel()

	cfg := defaultTestCfg(5)
	c := NewDeterministicCompressor(cfg)

	msgs := []types.Message{
		buildMessage(t, 0, "user", textBlock("hello")),
		buildMessage(t, 1, "assistant", textBlock("hi")),
		buildMessage(t, 2, "user", textBlock("how are you")),
	}

	result := c.Compress(msgs)

	// All 3 messages are inside the window of 5 -> nothing compressed.
	if result.TokensSaved != 0 {
		t.Errorf("TokensSaved = %d, want 0 (all messages inside window)", result.TokensSaved)
	}
	if len(result.Messages) != len(msgs) {
		t.Errorf("len(Messages) = %d, want %d", len(result.Messages), len(msgs))
	}
}

// TestCompress_JSONToolResult verifies that a tool_result with JSON content gets compacted.
func TestCompress_JSONToolResult(t *testing.T) {
	t.Parallel()

	cfg := defaultTestCfg(1) // window=1 so all but last message is eligible
	c := NewDeterministicCompressor(cfg)

	// Structural whitespace only (inside strings json.Compact cannot remove).
	var jb strings.Builder
	jb.WriteString("{\n  \"items\": [\n")
	for i := 0; i < 400; i++ {
		if i > 0 {
			jb.WriteString(",\n")
		}
		jb.WriteString("    ")
		jb.WriteString(`{"i":`)
		jb.WriteString(strconv.Itoa(i))
		jb.WriteString(`}`)
	}
	jb.WriteString("\n  ]\n}\n")
	largeJSON := jb.String()

	// Need ≥2 user turns so with window=1 the compressible prefix is non-empty (exchange_window.go).
	msgs := []types.Message{
		buildMessage(t, 0, "user", toolResultBlock(largeJSON)),
		buildMessage(t, 1, "assistant", textBlock("done")),
		buildMessage(t, 2, "user", textBlock("latest exchange")),
	}

	result := c.Compress(msgs)

	if result.JSONSaved <= 0 {
		t.Errorf("JSONSaved = %d, want > 0 for whitespace-heavy JSON", result.JSONSaved)
	}
	// The compressed text must not contain unescaped newlines.
	compressed := result.Messages[0].Content[0].Text
	if strings.Contains(compressed, "\n") {
		t.Errorf("compressed tool_result still contains newline: %q", compressed)
	}
}

// TestCompress_RepeatedContent verifies that a second identical tool_result gets a dedup marker.
func TestCompress_RepeatedContent(t *testing.T) {
	t.Parallel()

	cfg := defaultTestCfg(1) // last 1 user exchange stays uncompressed (spec: user-started exchanges)
	c := NewDeterministicCompressor(cfg)

	// Use pre-compacted JSON so JSON sub-layer won't fire first.
	content := `{"result":"identical content for dedup test","x":1}`

	// Three user turns at indices 0, 2, 4 → with window=1 only index 4+ is protected; 0–3 compress.
	msgs := []types.Message{
		buildMessage(t, 0, "user", toolResultBlock(content)),
		buildMessage(t, 1, "assistant", textBlock("a")),
		buildMessage(t, 2, "user", toolResultBlock(content)),
		buildMessage(t, 3, "assistant", textBlock("b")),
		buildMessage(t, 4, "user", textBlock("latest turn")),
	}

	result := c.Compress(msgs)

	if result.DedupSaved <= 0 {
		t.Errorf("DedupSaved = %d, want > 0 for duplicate content", result.DedupSaved)
	}
	dupText := result.Messages[2].Content[0].Text
	if !strings.HasPrefix(dupText, "[Duplicate of message") {
		t.Errorf("duplicate user message text = %q, want dedup marker prefix", dupText)
	}
}

// TestCompress_PreservesWindow verifies that messages inside the sliding window are untouched.
func TestCompress_PreservesWindow(t *testing.T) {
	t.Parallel()

	window := 3
	cfg := defaultTestCfg(window)
	c := NewDeterministicCompressor(cfg)

	windowMsgs := []types.Message{
		buildMessage(t, 3, "user", textBlock("window msg 1")),
		buildMessage(t, 4, "assistant", textBlock("window msg 2")),
		buildMessage(t, 5, "user", textBlock("window msg 3")),
	}
	outsideMsgs := []types.Message{
		buildMessage(t, 0, "user", toolResultBlock(`{"k":"outside window content here"}`)),
		buildMessage(t, 1, "assistant", textBlock("old response")),
		buildMessage(t, 2, "user", textBlock("old user message")),
	}

	msgs := append(outsideMsgs, windowMsgs...)
	result := c.Compress(msgs)

	for i := len(msgs) - window; i < len(msgs); i++ {
		orig := msgs[i].Content[0].Text
		got := result.Messages[i].Content[0].Text
		if got != orig {
			t.Errorf("window message[%d] text changed: got %q, want %q", i, got, orig)
		}
	}
}

func TestDeterministicCompressor_Reset(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	c := NewDeterministicCompressor(cfg)
	msgs := []types.Message{
		buildMessage(t, 0, "user", textBlock("hello")),
		buildMessage(t, 1, "assistant", textBlock("hi")),
		buildMessage(t, 2, "user", textBlock("tail")),
	}
	_ = c.Compress(msgs)
	c.Reset()
	_ = c.Compress(msgs)
}

func TestFormatDeltaHeader(t *testing.T) {
	t.Parallel()
	h := formatDeltaHeader("pkg/a.go", 1, 4)
	want := "[Delta from message 1 to 4 for pkg/a.go]\n"
	if h != want {
		t.Fatalf("got %q want %q", h, want)
	}
}

func TestFormatNearDupeReference(t *testing.T) {
	t.Parallel()
	s := formatNearDupeReference(2, 11)
	if !strings.Contains(s, "2") || !strings.Contains(s, "11") || !strings.Contains(s, "Near-duplicate") {
		t.Fatalf("got %q", s)
	}
}

func TestCompress_ANSIInToolResult(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	c := NewDeterministicCompressor(cfg)
	// Not valid JSON → skip JSON compact; no path → no comment/structure/delta.
	plain := "\x1b[31m" + strings.Repeat("noise ", 80) + "\x1b[0m"
	msgs := []types.Message{
		buildMessage(t, 0, "user", toolResultBlock(plain)),
		buildMessage(t, 1, "assistant", textBlock("ok")),
		buildMessage(t, 2, "user", textBlock("tail")),
	}
	result := c.Compress(msgs)
	if result.ANSISaved <= 0 {
		t.Fatalf("ANSISaved=%d", result.ANSISaved)
	}
	if strings.Contains(result.Messages[0].Content[0].Text, "\x1b[") {
		t.Fatal("ANSI codes should be stripped")
	}
}

func TestCompress_FileDeltaSecondVersion(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	cfg.StructureMinTokens = 10000
	cfg.StructureLanguages = []string{}
	cfg.DedupSimilarityThreshold = 2.0
	c := NewDeterministicCompressor(cfg)
	v1 := strings.Repeat("same line\n", 30) + "line thirty\n"
	v2 := strings.Repeat("same line\n", 30) + "line CHANGED\n"
	tool := func(body string) types.ContentBlock {
		return types.ContentBlock{
			Type:      "tool_result",
			Text:      body,
			ToolInput: `{"path": "pkg/file.go"}`,
		}
	}
	msgs := []types.Message{
		buildMessage(t, 0, "user", tool(v1)),
		buildMessage(t, 1, "assistant", textBlock("a")),
		buildMessage(t, 2, "user", tool(v2)),
		buildMessage(t, 3, "assistant", textBlock("b")),
		buildMessage(t, 4, "user", textBlock("latest")),
	}
	result := c.Compress(msgs)
	if result.DeltaSaved <= 0 {
		t.Fatalf("DeltaSaved=%d want > 0", result.DeltaSaved)
	}
	got := result.Messages[2].Content[0].Text
	if !strings.HasPrefix(got, "[Delta from message") {
		t.Fatalf("want delta header, got %q", got)
	}
}

func TestCompress_ToolResultDeltaUsesResolvedToolCallKey(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	cfg.StructureMinTokens = 10000
	cfg.StructureLanguages = []string{}
	cfg.DedupSimilarityThreshold = 2.0
	c := NewDeterministicCompressor(cfg)
	var stable strings.Builder
	stable.WriteString("On branch main\n")
	for i := 0; i < 24; i++ {
		stable.WriteString(fmt.Sprintf(" M pkg/file-%02d.go\n", i))
	}
	v1 := stable.String() + "?? tmp.txt\n"
	v2 := stable.String() + " M pkg/other.go\n?? tmp.txt\n"
	msgs := []types.Message{
		buildMessage(t, 0, "assistant", types.ContentBlock{
			Type:      "tool_use",
			ToolName:  "Bash",
			ToolInput: `{"command":"git status"}`,
			ToolUseID: "use-1",
		}),
		buildMessage(t, 1, "user", types.ContentBlock{
			Type:         "tool_result",
			Text:         v1,
			ToolResultID: "use-1",
		}),
		buildMessage(t, 2, "assistant", types.ContentBlock{
			Type:      "tool_use",
			ToolName:  "Bash",
			ToolInput: `{"command":"git status"}`,
			ToolUseID: "use-2",
		}),
		buildMessage(t, 3, "user", types.ContentBlock{
			Type:         "tool_result",
			Text:         v2,
			ToolResultID: "use-2",
		}),
		buildMessage(t, 4, "assistant", textBlock("done")),
		buildMessage(t, 5, "user", textBlock("latest")),
	}

	result := c.Compress(msgs)
	if result.DeltaSaved <= 0 {
		t.Fatalf("DeltaSaved=%d want > 0", result.DeltaSaved)
	}
	got := result.Messages[3].Content[0].Text
	if !strings.Contains(got, "tool:bash|git status") {
		t.Fatalf("delta header must use resolved tool key, got %q", got)
	}
}

func TestCompress_ToolCompressorUsesResolvedCodexToolInput(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	c := NewDeterministicCompressor(cfg)
	var output strings.Builder
	for i := 0; i < 120; i++ {
		output.WriteString(fmt.Sprintf("internal/pkg/file_%02d.go:%d:TODO marker %02d\n", i, i+1, i))
	}
	msgs := []types.Message{
		buildMessage(t, 0, "user", textBlock("find TODOs")),
		buildMessage(t, 0, "assistant", types.ContentBlock{
			Type:      "tool_use",
			ToolName:  "exec_command",
			ToolInput: `{"command":"rg -n TODO internal"}`,
			ToolUseID: "call-rg",
		}),
		buildMessage(t, 1, "tool", types.ContentBlock{
			Type:         "tool_result",
			Text:         output.String(),
			ToolResultID: "call-rg",
		}),
		buildMessage(t, 2, "assistant", textBlock("done")),
		buildMessage(t, 3, "user", textBlock("summarize")),
	}

	result := c.Compress(msgs)
	if result.ToolCompressorSaved <= 0 {
		t.Fatalf("ToolCompressorSaved=%d want > 0", result.ToolCompressorSaved)
	}
	got := result.Messages[2].Content[0].Text
	if !strings.Contains(got, "more matches omitted") || strings.Contains(got, "file_119.go") {
		t.Fatalf("resolved command should compact search output, got %q", got)
	}
}

func TestCompress_DeltaTracksNormalizedCommentStrippedSource(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	cfg.StructureMinTokens = 10000
	cfg.StructureLanguages = []string{}
	cfg.DedupSimilarityThreshold = 2.0
	c := NewDeterministicCompressor(cfg)

	var comments strings.Builder
	for i := 0; i < 40; i++ {
		comments.WriteString("// noisy comment line that should be stripped\n")
	}
	var body strings.Builder
	body.WriteString("package main\n\nfunc main() {\n")
	for i := 0; i < 200; i++ {
		body.WriteString(fmt.Sprintf("\tprintln(\"stable-%02d\")\n", i))
	}
	body.WriteString("}\n")
	v1 := comments.String() + body.String()
	v2 := comments.String() + strings.Replace(body.String(), "}\n", "\tprintln(\"delta\")\n}\n", 1)
	msgs := []types.Message{
		buildMessage(t, 0, "user", types.ContentBlock{
			Type:      "tool_result",
			Text:      v1,
			ToolInput: `{"path":"pkg/main.go"}`,
		}),
		buildMessage(t, 1, "assistant", textBlock("done once")),
		buildMessage(t, 2, "user", types.ContentBlock{
			Type:      "tool_result",
			Text:      v2,
			ToolInput: `{"path":"pkg/main.go"}`,
		}),
		buildMessage(t, 3, "assistant", textBlock("done twice")),
		buildMessage(t, 4, "user", textBlock("latest")),
	}

	result := c.Compress(msgs)
	if result.CommentSaved <= 0 {
		t.Fatalf("CommentSaved=%d want > 0", result.CommentSaved)
	}
	if result.DeltaSaved <= 0 {
		t.Fatalf("DeltaSaved=%d want > 0 after comment stripping", result.DeltaSaved)
	}
	if !strings.HasPrefix(result.Messages[2].Content[0].Text, "[Delta from message") {
		t.Fatalf("want delta header, got %q", result.Messages[2].Content[0].Text)
	}
}

func TestCompress_NearDupeToolResult(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	// Lower threshold so a one-token tail change stays above MinHash Jaccard estimate.
	cfg.DedupSimilarityThreshold = 0.45
	c := NewDeterministicCompressor(cfg)
	prefix := strings.Repeat("alpha beta gamma delta ", 25)
	body1 := prefix + "suffixaaaa"
	body2 := prefix + "suffixaaab"
	ci := NewContentIndex()
	ci.CheckAndRecord(body1, 0, cfg.DedupSimilarityThreshold)
	_, near, _ := ci.CheckAndRecord(body2, 1, cfg.DedupSimilarityThreshold)
	if !near {
		t.Fatal("sanity: expected MinHash near-duplicate for similar tails")
	}
	msgs := []types.Message{
		buildMessage(t, 0, "user", toolResultBlock(body1)),
		buildMessage(t, 1, "assistant", textBlock("x")),
		buildMessage(t, 2, "user", toolResultBlock(body2)),
		buildMessage(t, 3, "assistant", textBlock("y")),
		buildMessage(t, 4, "user", textBlock("z")),
	}
	result := c.Compress(msgs)
	if result.DedupSaved <= 0 {
		t.Fatalf("DedupSaved=%d want near-dupe", result.DedupSaved)
	}
	got := result.Messages[2].Content[0].Text
	if !strings.Contains(got, "Near-duplicate") {
		t.Fatalf("want near-dupe marker, got %q", got)
	}
}

// TestCompress_ZeroMessages verifies Compress handles an empty message slice without panic.
// TestCompress_UnchangedToolResult verifies that tool_result content with nothing to compress
// passes through unchanged (exercises the `if len(text) < originalLen || text != block.Text` else path).
func TestCompress_UnchangedToolResult(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	c := NewDeterministicCompressor(cfg)
	// Unique, non-JSON, no-ANSI, non-code content - no compressor can reduce it.
	unique := strings.Repeat("xyzzy ", 25) + "uniquetoken_abc99"
	msgs := []types.Message{
		buildMessage(t, 0, "user", toolResultBlock(unique)),
		buildMessage(t, 1, "assistant", textBlock("ok")),
		buildMessage(t, 2, "user", textBlock("tail")),
	}
	result := c.Compress(msgs)
	got := result.Messages[0].Content[0].Text
	if got != unique {
		t.Errorf("unchanged content should pass through verbatim; got len=%d want len=%d", len(got), len(unique))
	}
}

func TestCompress_ZeroMessages(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(2)
	c := NewDeterministicCompressor(cfg)
	result := c.Compress(nil)
	if result.TokensSaved != 0 || len(result.Messages) != 0 {
		t.Errorf("nil messages: TokensSaved=%d len=%d", result.TokensSaved, len(result.Messages))
	}
	result = c.Compress([]types.Message{})
	if result.TokensSaved != 0 || len(result.Messages) != 0 {
		t.Errorf("empty messages: TokensSaved=%d len=%d", result.TokensSaved, len(result.Messages))
	}
}

// TestCompress_NonToolResultBlocks verifies that text/image blocks are skipped (only tool_result compressed).
func TestCompress_NonToolResultBlocks(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	c := NewDeterministicCompressor(cfg)
	msgs := []types.Message{
		buildMessage(t, 0, "user", textBlock("hello world - this is plain text")),
		buildMessage(t, 1, "assistant", textBlock("response text")),
		buildMessage(t, 2, "user", textBlock("latest exchange")),
	}
	result := c.Compress(msgs)
	if result.TokensSaved != 0 {
		t.Errorf("text-only blocks: TokensSaved=%d, want 0", result.TokensSaved)
	}
	if result.Messages[0].Content[0].Text != "hello world - this is plain text" {
		t.Error("text block content should be unchanged")
	}
}

// TestCompress_EmptyToolResult verifies that a tool_result with empty text is skipped.
func TestCompress_EmptyToolResult(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	c := NewDeterministicCompressor(cfg)
	msgs := []types.Message{
		buildMessage(t, 0, "user", types.ContentBlock{Type: "tool_result", Text: ""}),
		buildMessage(t, 1, "assistant", textBlock("response")),
		buildMessage(t, 2, "user", textBlock("latest")),
	}
	result := c.Compress(msgs)
	if result.TokensSaved != 0 {
		t.Errorf("empty tool_result: TokensSaved=%d, want 0", result.TokensSaved)
	}
}

// TestCompress_TwoSimilarBlocksSameMsgIdx verifies that two similar tool_result blocks
// within the SAME message index are NOT flagged as near-duplicates of each other.
// This exercises the `if e.idx == msgIdx { continue }` guard in CheckAndRecord.
func TestCompress_TwoSimilarBlocksSameMsgIdx(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	cfg.DedupSimilarityThreshold = 0.85
	c := NewDeterministicCompressor(cfg)

	// Two very similar long blocks inside the same message.
	prefix := strings.Repeat("word pair common ", 50)
	b1 := prefix + "suffix_alpha"
	b2 := prefix + "suffix_beta"

	msgs := []types.Message{
		{
			Index: 0,
			Role:  "user",
			Content: []types.ContentBlock{
				{Type: "tool_result", Text: b1},
				{Type: "tool_result", Text: b2},
			},
		},
		buildMessage(t, 1, "assistant", textBlock("ok")),
		buildMessage(t, 2, "user", textBlock("tail")),
	}
	result := c.Compress(msgs)
	// b2 must NOT be marked as near-duplicate of b1 from the same message.
	if strings.Contains(result.Messages[0].Content[1].Text, "Near-duplicate") {
		t.Error("blocks within the same message should not near-match each other (same msgIdx guard)")
	}
}

// TestCompress_CommentStripViaPath verifies that a tool_result whose ToolInput contains a
// ".go" path gets language-detected and its // comments stripped (commentSaved > 0).
func TestCompress_CommentStripViaPath(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	c := NewDeterministicCompressor(cfg)

	// Non-JSON Go code with // comments - path detection yields "go".
	code := "package main\n\n// Top-level comment to be stripped\nfunc Hello() {}\n// Another comment\nvar x = 1\n"
	msgs := []types.Message{
		buildMessage(t, 0, "user", types.ContentBlock{
			Type:      "tool_result",
			Text:      code,
			ToolInput: `{"path": "pkg/foo.go"}`,
		}),
		buildMessage(t, 1, "assistant", textBlock("ok")),
		buildMessage(t, 2, "user", textBlock("done")),
	}
	result := c.Compress(msgs)
	if result.CommentSaved <= 0 {
		t.Fatalf("CommentSaved=%d want > 0 for Go code with comments", result.CommentSaved)
	}
	got := result.Messages[0].Content[0].Text
	if strings.Contains(got, "// Top-level comment") {
		t.Error("// comment should be stripped from output")
	}
}

// TestCompress_SuccessShortCircuit verifies that a tool_result matching build/test success
// patterns gets replaced by a one-liner (successShortSaved > 0).
func TestCompress_SuccessShortCircuit(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	c := NewDeterministicCompressor(cfg)

	// Text matching reTestsOK, > 80 chars, no error patterns, non-JSON.
	buildOutput := "Running test suite...\nInitializing runner\nLoading fixtures\n\nAll tests passed\n\nElapsed: 1.2s\nDone.\n"
	msgs := []types.Message{
		buildMessage(t, 0, "user", toolResultBlock(buildOutput)),
		buildMessage(t, 1, "assistant", textBlock("ok")),
		buildMessage(t, 2, "user", textBlock("done")),
	}
	result := c.Compress(msgs)
	if result.SuccessShortSaved <= 0 {
		t.Fatalf("SuccessShortSaved=%d want > 0 for success output", result.SuccessShortSaved)
	}
	got := result.Messages[0].Content[0].Text
	if !strings.Contains(got, "[ok]") {
		t.Errorf("want success short-circuit marker, got %q", got)
	}
}

func TestCompress_structureExtractLargeGoFile(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	cfg.StructureMinTokens = 100
	cfg.StructureLanguages = []string{"go"}
	c := NewDeterministicCompressor(cfg)
	body := "package p\n\nfunc Big() {\n" + strings.Repeat("\t_ = 1\n", 120) + "}\n"
	msgs := []types.Message{
		buildMessage(t, 0, "user", types.ContentBlock{
			Type:      "tool_result",
			Text:      body,
			ToolInput: `{"path": "big.go"}`,
		}),
		buildMessage(t, 1, "assistant", textBlock("ok")),
		buildMessage(t, 2, "user", textBlock("done")),
	}
	result := c.Compress(msgs)
	if result.StructureSaved <= 0 {
		t.Fatalf("StructureSaved=%d", result.StructureSaved)
	}
	if !result.Messages[0].Metadata.WasStructured {
		t.Fatal("WasStructured should be true")
	}
}

func TestShouldRunStructureExtraction_UsesTokenizerThreshold(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("token rich content ", 30)
	counted := tokens.CountString(text)
	if counted <= 0 {
		t.Fatal("tokenizer returned zero")
	}
	if !shouldRunStructureExtraction(text, counted) {
		t.Fatal("threshold equal to token count should run")
	}
	if shouldRunStructureExtraction(text, counted+1) {
		t.Fatal("threshold above token count should not run")
	}
	if shouldRunStructureExtraction("", 0) {
		t.Fatal("empty text should not run")
	}
	if !shouldRunStructureExtraction("small", 0) {
		t.Fatal("zero threshold should run non-empty text")
	}
}

// TestCompress_GitDiffToolCompressor covers lines 240-248 in compressMessage: the tool
// compressor body executes when classifyToolResult returns a compressible type (git diff)
// and compressToolOutput returns a shorter string.
func TestCompress_GitDiffToolCompressor(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	c := NewDeterministicCompressor(cfg)

	// Build a git diff with >60 diff lines so filterGitCompact truncates them.
	var sb strings.Builder
	sb.WriteString("diff --git a/main.go b/main.go\n")
	sb.WriteString("--- a/main.go\n")
	sb.WriteString("+++ b/main.go\n")
	for i := 0; i < 100; i++ {
		sb.WriteString(fmt.Sprintf("+added line %d: some code that was added here in this commit\n", i))
	}
	sb.WriteString("1 file changed, 100 insertions(+)\n")

	msgs := []types.Message{
		buildMessage(t, 0, "user", types.ContentBlock{
			Type: "tool_result",
			Text: sb.String(),
		}),
		buildMessage(t, 1, "assistant", textBlock("looks good")),
		buildMessage(t, 2, "user", textBlock("continue")),
	}

	result := c.Compress(msgs)
	if result.ToolCompressorSaved <= 0 {
		t.Errorf("git diff tool result: expected ToolCompressorSaved > 0, got %d", result.ToolCompressorSaved)
	}
}

func TestCompressMessage_UsesScalarDedupThresholdFallback(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	cfg.DedupSimilarityThreshold = 0.5
	c := NewDeterministicCompressor(cfg)
	msg := buildMessage(t, 0, "user", types.ContentBlock{
		Type: "tool_result",
		Text: strings.Repeat("same words ", 80),
	})
	_, _, _, _, _, _, _, _, _, _, _ = c.compressMessage(msg, 0, 2, nil)
	dupe := buildMessage(t, 1, "user", types.ContentBlock{
		Type: "tool_result",
		Text: strings.Repeat("same words ", 80),
	})
	_, _, dedupSaved, _, _, _, _, _, _, _, _ := c.compressMessage(dupe, 1, 2, nil)
	if dedupSaved <= 0 {
		t.Fatalf("dedup fallback did not trigger, saved=%d", dedupSaved)
	}
}

func TestCompress_SemanticDictionaryForRepeatedPaths(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	c := NewDeterministicCompressor(cfg)
	path := "/Users/example/workspace/slimference/internal/proxy/handler.go"
	body := "panic stack\n" + strings.Repeat(path+":123: failure in handler\n", 12)
	msgs := []types.Message{
		buildMessage(t, 0, "user", types.ContentBlock{
			Type:     "tool_result",
			ToolName: "Bash",
			Text:     body,
		}),
		buildMessage(t, 1, "assistant", textBlock("ok")),
		buildMessage(t, 2, "user", textBlock("tail")),
	}

	result := c.Compress(msgs)
	if result.DictionarySaved <= 0 {
		t.Fatalf("DictionarySaved=%d", result.DictionarySaved)
	}
	got := result.Messages[0].Content[0].Text
	if !strings.Contains(got, "[Slimference path dictionary]") || !strings.Contains(got, "[P1]="+path) {
		t.Fatalf("dictionary legend missing: %s", got)
	}
	if strings.Count(got, path) != 1 {
		t.Fatalf("path should only remain in legend, got %d occurrences in %s", strings.Count(got, path), got)
	}
}

// TestCompress_InlineBase64ImageInToolResult covers lines 268-271 in compressMessage:
// a tool_result with >500 chars of text containing an inline data URI gets its image data
// replaced and imageSaved > 0.
func TestCompress_InlineBase64ImageInToolResult(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	c := NewDeterministicCompressor(cfg)

	// Build text > 500 chars with an embedded data URI (matching reBase64DataURI).
	b64Payload := strings.Repeat("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/", 8)[:480]
	toolText := "Result: data:image/png;base64," + b64Payload + " done"
	if len(toolText) <= 500 {
		t.Fatalf("test setup: need > 500 chars, got %d", len(toolText))
	}

	msgs := []types.Message{
		buildMessage(t, 0, "user", types.ContentBlock{
			Type: "tool_result",
			Text: toolText,
		}),
		buildMessage(t, 1, "assistant", textBlock("processed")),
		buildMessage(t, 2, "user", textBlock("next step")),
	}

	result := c.Compress(msgs)
	if result.ImageSaved <= 0 {
		t.Errorf("inline base64 in tool_result: expected ImageSaved > 0, got %d", result.ImageSaved)
	}
	// Verify the data URI is gone from the compressed message.
	if strings.Contains(result.Messages[0].Content[0].Text, "data:image/png;base64,") {
		t.Error("data URI should be replaced in compressed output")
	}
}

// TestCompress_ImageBlock covers the block.Type == "image" path in compressMessage.
// The image block in message 0 falls outside the sliding window (2 user turns, window=1),
// so it is replaced with a descriptive text label.
func TestCompress_ImageBlock(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	c := NewDeterministicCompressor(cfg)

	// buildPNGHeader is defined in image_replace_test.go (same package).
	pngData := buildPNGHeader(640, 480)
	encoded := base64.StdEncoding.EncodeToString(pngData)

	msgs := []types.Message{
		{
			Index: 0, Role: "user",
			Content: []types.ContentBlock{
				{Type: "image", ImageData: encoded},
			},
		},
		buildMessage(t, 1, "assistant", textBlock("I see the image")),
		buildMessage(t, 2, "user", textBlock("describe it further")),
	}

	result := c.Compress(msgs)
	if result.ImageSaved <= 0 {
		t.Errorf("image block outside window: expected imageSaved > 0, got %d", result.ImageSaved)
	}
	// Image block should have been replaced by a text block.
	if result.Messages[0].Content[0].Type != "text" {
		t.Errorf("image block should become text after compression, got %q", result.Messages[0].Content[0].Type)
	}
}

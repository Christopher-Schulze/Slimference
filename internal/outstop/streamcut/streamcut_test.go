package streamcut

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// feed emits a sequence of Anthropic-flavoured SSE lines for the given
// text segments and reports the index where the cutter fired (or -1
// if it never did).
func feedAnthropic(c *Cutter, segments ...string) int {
	for i, seg := range segments {
		line := fmt.Appendf(nil, `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":%q}}`, seg)
		if c.Observe(line) {
			return i
		}
	}
	return -1
}

func TestCutterFiresOnAnthropicTrailingCommentary(t *testing.T) {
	c := NewCutter("anthropic")
	// 90 chars of substantive content, then commentary opener.
	body := strings.Repeat("Sample line of substantive code content. ", 3)
	if got := feedAnthropic(c, body, "\nLet me know if you need more."); got != 1 {
		t.Fatalf("expected fire on second segment, got=%d, buf=%q", got, c.buf.String())
	}
	if !c.Fired() {
		t.Errorf("Fired()=false after trigger")
	}
}

func TestCutterDoesNotFireBeforeMinThreshold(t *testing.T) {
	c := NewCutter("anthropic")
	// Commentary opener but accumulator <80 chars total - guard suppresses.
	if got := feedAnthropic(c, "Short.", "\nLet me know"); got != -1 {
		t.Errorf("expected no fire, got=%d (accumulator too short)", got)
	}
}

func TestCutterIgnoresMidSentenceMatch(t *testing.T) {
	c := NewCutter("anthropic")
	body := "I will let you know about the result. " + strings.Repeat("More content here. ", 5)
	// 'let you know' contains 'let' but not '\nLet me know'. Should not fire.
	if got := feedAnthropic(c, body); got != -1 {
		t.Errorf("false-positive fire on mid-sentence: got=%d", got)
	}
}

func TestCutterIgnoresOldMatchOutsideTailWindow(t *testing.T) {
	c := NewCutter("anthropic")
	c.minBeforeFire = 10
	c.tailWindow = 50
	// Single delta with the match early followed by enough padding
	// to push it out of the tail window. The cutter inspects the
	// running buffer once after each Observe call - tailWindow=50 and
	// match position 0 means matchTail sees only the trailing pad.
	seg := "\nLet me know" + strings.Repeat(" pad word ", 12) // ~ 12 + 120 chars
	if got := feedAnthropic(c, seg); got != -1 {
		t.Errorf("expected no fire (match outside tail window), got=%d buf_len=%d", got, c.buf.Len())
	}
}

func TestCutterReportsFiredAfterTrigger(t *testing.T) {
	c := NewCutter("anthropic")
	body := strings.Repeat("Substantive content here.\n", 5)
	feedAnthropic(c, body, "\nHope this helps.")
	if !c.Fired() {
		t.Fatalf("Fired()=false after trigger")
	}
	// Further Observe calls short-circuit and return true.
	if !c.Observe([]byte(`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"more"}}`)) {
		t.Errorf("post-fire Observe should return true")
	}
}

func TestCutterOpenAIChatDelta(t *testing.T) {
	c := NewCutter("openai")
	long := strings.Repeat("Substantive content. ", 5)
	line1 := fmt.Appendf(nil, `data: {"choices":[{"delta":{"content":%q}}]}`, long)
	if c.Observe(line1) {
		t.Fatal("fired prematurely")
	}
	line2 := []byte(`data: {"choices":[{"delta":{"content":"\nHope this helps with your task."}}]}`)
	if !c.Observe(line2) {
		t.Errorf("expected fire on commentary opener")
	}
}

func TestCutterCodexChatGPTRouting(t *testing.T) {
	c := NewCutter("codex_chatgpt")
	long := strings.Repeat("Substantive content. ", 5)
	c.Observe(fmt.Appendf(nil, `data: {"choices":[{"delta":{"content":%q}}]}`, long))
	if !c.Observe([]byte(`data: {"choices":[{"delta":{"content":"\nWould you like more detail?"}}]}`)) {
		t.Errorf("codex_chatgpt should match like openai")
	}
}

func TestCutterOpenAIResponsesAPIDelta(t *testing.T) {
	c := NewCutter("openai")
	long := strings.Repeat("Substantive content. ", 5)
	c.Observe(fmt.Appendf(nil, `data: {"type":"response.output_text.delta","delta":%q}`, long))
	if !c.Observe([]byte(`data: {"type":"response.output_text.delta","delta":"\nFeel free to ask."}`)) {
		t.Errorf("Responses-API delta shape not recognised")
	}
}

func TestExtractDeltaTextNonDataLine(t *testing.T) {
	if extractDeltaText("anthropic", []byte("event: message_start")) != "" {
		t.Errorf("non-data line should yield empty text")
	}
}

func TestExtractDeltaTextDoneMarker(t *testing.T) {
	if extractDeltaText("openai", []byte("data: [DONE]")) != "" {
		t.Errorf("[DONE] should yield empty text")
	}
}

func TestExtractDeltaTextMalformedJSON(t *testing.T) {
	if extractDeltaText("anthropic", []byte(`data: {not json`)) != "" {
		t.Errorf("malformed JSON should yield empty text")
	}
	if extractDeltaText("openai", []byte(`data: {not json`)) != "" {
		t.Errorf("malformed JSON should yield empty text")
	}
}

func TestExtractDeltaTextUnknownProvider(t *testing.T) {
	if extractDeltaText("gemini", []byte(`data: {"x":1}`)) != "" {
		t.Errorf("unknown provider should yield empty text")
	}
}

func TestExtractAnthropicDeltaNonTextEvent(t *testing.T) {
	// message_start, not a text delta
	got := extractAnthropicDelta([]byte(`{"type":"message_start","message":{"id":"x"}}`))
	if got != "" {
		t.Errorf("non-text event yielded %q want empty", got)
	}
}

func TestExtractAnthropicDeltaNonTextDeltaType(t *testing.T) {
	// content_block_delta with input_json_delta (tool call), not text_delta
	got := extractAnthropicDelta([]byte(`{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{"}}`))
	if got != "" {
		t.Errorf("non-text delta yielded %q want empty", got)
	}
}

func TestExtractOpenAIDeltaEmptyChoices(t *testing.T) {
	if extractOpenAIDelta([]byte(`{"choices":[]}`)) != "" {
		t.Errorf("empty choices should yield empty")
	}
}

func TestExtractOpenAIDeltaNoContent(t *testing.T) {
	if extractOpenAIDelta([]byte(`{"choices":[{"delta":{}}]}`)) != "" {
		t.Errorf("delta without content should yield empty")
	}
}

func TestSyntheticTerminatorAnthropic(t *testing.T) {
	c := NewCutter("anthropic")
	out := string(c.SyntheticTerminator())
	if !strings.Contains(out, "message_stop") {
		t.Errorf("terminator missing message_stop: %q", out)
	}
	if !strings.Contains(out, "message_delta") {
		t.Errorf("terminator missing message_delta with stop_reason: %q", out)
	}
}

func TestSyntheticTerminatorOpenAI(t *testing.T) {
	c := NewCutter("openai")
	out := string(c.SyntheticTerminator())
	if out != "data: [DONE]\n\n" {
		t.Errorf("terminator=%q want OpenAI [DONE]", out)
	}
}

func TestSyntheticTerminatorCodex(t *testing.T) {
	c := NewCutter("codex_chatgpt")
	out := string(c.SyntheticTerminator())
	if out != "data: [DONE]\n\n" {
		t.Errorf("terminator=%q want [DONE]", out)
	}
}

func TestSyntheticTerminatorUnknown(t *testing.T) {
	c := NewCutter("unknown")
	if c.SyntheticTerminator() != nil {
		t.Errorf("unknown provider terminator should be nil")
	}
}

// --- Delay-buffer (T184) ---

func deltaLine(text string) []byte {
	b, _ := json.Marshal(text)
	return fmt.Appendf(nil, `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":%s}}`, b)
}

func TestForwardHoldbackQueuesAndEmitsOldest(t *testing.T) {
	c := NewCutterWithHoldback("anthropic", 3)
	// First three lines are held back; nothing emitted yet.
	for i := range 3 {
		emit, term := c.Forward(deltaLine(fmt.Sprintf("delta-%d ", i)))
		if term {
			t.Fatalf("unexpected terminate at i=%d", i)
		}
		if emit != nil {
			t.Errorf("expected no emit while queue<=holdback, got %q at i=%d", emit, i)
		}
	}
	// Fourth line pushes the first one out.
	emit, term := c.Forward(deltaLine("delta-3 "))
	if term {
		t.Fatal("unexpected terminate")
	}
	if emit == nil {
		t.Fatal("expected delta-0 to be emitted")
	}
	if !bytes.Contains(emit, []byte("delta-0")) {
		t.Errorf("oldest line not emitted; got %q", emit)
	}
}

func TestForwardHoldbackPassesNonTextEventsImmediately(t *testing.T) {
	c := NewCutterWithHoldback("anthropic", 3)
	control := []byte(`event: message_start`)
	emit, term := c.Forward(control)
	if term {
		t.Fatal("unexpected terminate")
	}
	if !bytes.Equal(emit, control) {
		t.Errorf("control event not passed verbatim, got %q want %q", emit, control)
	}
}

func TestForwardHoldbackDropsQueueOnFire(t *testing.T) {
	c := NewCutterWithHoldback("anthropic", 3)
	c.minBeforeFire = 10
	// Build up enough content to enable matching.
	c.Forward(deltaLine(strings.Repeat("Filler content. ", 5))) // pushes into accum
	c.Forward(deltaLine("more substantive content here. "))
	// Commentary opener fires the cutter.
	emit, term := c.Forward(deltaLine("\nHope this helps."))
	if !term {
		t.Fatal("expected terminate on commentary opener")
	}
	if !bytes.Contains(emit, []byte("message_stop")) {
		t.Errorf("synthetic terminator missing: %q", emit)
	}
	// Subsequent Forward calls return terminate=true with nil emit.
	emit2, term2 := c.Forward(deltaLine("more"))
	if !term2 || emit2 != nil {
		t.Errorf("post-fire Forward should be terminate=true emit=nil, got emit=%q term=%v", emit2, term2)
	}
}

func TestFlushReturnsHoldbackOnNaturalEnd(t *testing.T) {
	c := NewCutterWithHoldback("anthropic", 3)
	c.Forward(deltaLine("a "))
	c.Forward(deltaLine("b "))
	c.Forward(deltaLine("c "))
	leftover := c.Flush()
	if len(leftover) != 3 {
		t.Fatalf("expected 3 lines flushed, got %d", len(leftover))
	}
	for i, want := range []string{"a", "b", "c"} {
		if !bytes.Contains(leftover[i], []byte(want)) {
			t.Errorf("flush[%d] missing %q: %q", i, want, leftover[i])
		}
	}
	// Second Flush returns nil (queue cleared).
	if got := c.Flush(); got != nil {
		t.Errorf("second flush should be nil, got %v", got)
	}
}

func TestFlushReturnsNilAfterFire(t *testing.T) {
	c := NewCutterWithHoldback("anthropic", 3)
	c.minBeforeFire = 5
	c.Forward(deltaLine(strings.Repeat("Content. ", 5)))
	c.Forward(deltaLine("\nHope this helps with everything."))
	if !c.Fired() {
		t.Fatal("expected fire")
	}
	if got := c.Flush(); got != nil {
		t.Errorf("post-fire flush should be nil, got %v", got)
	}
}

func TestForwardLegacyPassthroughWhenHoldbackZero(t *testing.T) {
	c := NewCutter("anthropic")
	line := deltaLine("first ")
	emit, term := c.Forward(line)
	if term {
		t.Fatal("unexpected terminate")
	}
	if !bytes.Equal(emit, line) {
		t.Errorf("legacy mode should return line verbatim, got %q", emit)
	}
}

func TestForwardLegacyFireEmitsTerminator(t *testing.T) {
	c := NewCutter("anthropic")
	c.minBeforeFire = 10
	c.Forward(deltaLine(strings.Repeat("Subst. ", 10)))
	emit, term := c.Forward(deltaLine("\nLet me know if you need anything."))
	if !term {
		t.Fatal("expected fire")
	}
	if !bytes.Contains(emit, []byte("message_stop")) {
		t.Errorf("terminator missing in legacy fire: %q", emit)
	}
}

func TestNewCutterUnknownProviderObserveNoop(t *testing.T) {
	c := NewCutter("gemini")
	// Cannot extract delta for unknown provider; Observe never accumulates.
	for range 5 {
		if c.Observe([]byte(`data: {"choices":[{"delta":{"content":"x"}}]}`)) {
			t.Errorf("unknown provider cutter should never fire")
		}
	}
}

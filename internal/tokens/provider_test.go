package tokens

import (
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/types"
)

func TestForProvider_Routing(t *testing.T) {
	if got := ForProvider(types.Anthropic).Name(); got != "anthropic-calibrated" {
		t.Fatalf("anthropic: %s", got)
	}
	if got := ForProvider(types.OpenAI).Name(); got != "openai-tiktoken" {
		t.Fatalf("openai: %s", got)
	}
	if got := ForProvider(types.Provider(999)).Name(); got != "universal-fallback" {
		t.Fatalf("universal: %s", got)
	}
}

func TestAnthropicTokenizer_CountString(t *testing.T) {
	defer resetForTest()
	a := anthropic
	if got := a.CountString(""); got != 0 {
		t.Fatalf("empty: %d", got)
	}
	// A 350-byte ASCII string at 3.5 bytes/token ~= 100 tokens.
	s := strings.Repeat("x", 350)
	got := a.CountString(s)
	if got < 90 || got > 110 {
		t.Fatalf("got %d tokens, want ~100", got)
	}
}

func TestAnthropicTokenizer_CountStringCJK(t *testing.T) {
	defer resetForTest()
	a := anthropic
	// 10 Han characters: each is 3 UTF-8 bytes, so byteLen=30. Base
	// estimate ~30*1000/3500 = 8, plus CJK correction 10/3 = 3, = 11.
	got := a.CountString(strings.Repeat("你", 10))
	if got < 8 {
		t.Fatalf("CJK count too low: %d", got)
	}
}

func TestAnthropicTokenizer_CountStringSmallYieldsAtLeastOne(t *testing.T) {
	defer resetForTest()
	a := anthropic
	if got := a.CountString("a"); got < 1 {
		t.Fatalf("got %d, want >= 1", got)
	}
}

func TestAnthropicTokenizer_CountMessages(t *testing.T) {
	defer resetForTest()
	a := anthropic
	msgs := []types.Message{{Role: "user", Content: []types.ContentBlock{
		{Type: "text", Text: strings.Repeat("x", 350)},
		{Type: "tool_use", ToolName: "Read", ToolInput: `{"path":"/tmp"}`},
	}}}
	total := a.CountMessages(msgs)
	if total < 90 {
		t.Fatalf("total %d too low", total)
	}
}

func TestObserveUpstreamUsage_CalibratesAnthropic(t *testing.T) {
	defer resetForTest()
	before := anthropic.BytesPerTokenX1000ForModel("claude-sonnet")
	for i := 0; i < 30; i++ {
		ObserveUpstreamUsage(types.Anthropic, "claude-sonnet", 80, 100)
	}
	after := anthropic.BytesPerTokenX1000ForModel("claude-sonnet")
	if after <= before {
		t.Fatalf("ratio did not rise after repeated over-estimates: %d -> %d", before, after)
	}
}

func TestObserveUpstreamUsage_ConvergesDownwards(t *testing.T) {
	defer resetForTest()
	anthropic.bytesPerTokenX1000.Store(5000)
	val, _ := anthropic.perModel.LoadOrStore("sonnet", &modelRatio{})
	val.(*modelRatio).value.Store(5000)
	for i := 0; i < 40; i++ {
		ObserveUpstreamUsage(types.Anthropic, "claude-sonnet", 120, 100)
	}
	after := anthropic.BytesPerTokenX1000ForModel("claude-sonnet")
	if after >= 5000 {
		t.Fatalf("ratio did not fall after repeated under-estimates: %d", after)
	}
}

func TestObserveUpstreamUsage_ClampsToRange(t *testing.T) {
	defer resetForTest()
	for i := 0; i < 200; i++ {
		ObserveUpstreamUsage(types.Anthropic, "claude-sonnet", 1, 1_000_000)
	}
	if r := anthropic.BytesPerTokenX1000ForModel("claude-sonnet"); r > 6000 {
		t.Fatalf("ratio exceeded cap: %d", r)
	}
	for i := 0; i < 200; i++ {
		ObserveUpstreamUsage(types.Anthropic, "claude-sonnet", 1_000_000, 1)
	}
	if r := anthropic.BytesPerTokenX1000ForModel("claude-sonnet"); r < 1500 {
		t.Fatalf("ratio below floor: %d", r)
	}
}

func TestObserveUpstreamUsage_NoOpOnBadInput(t *testing.T) {
	defer resetForTest()
	before := anthropic.BytesPerTokenX1000()
	ObserveUpstreamUsage(types.Anthropic, "", 0, 100)
	ObserveUpstreamUsage(types.Anthropic, "", 100, 0)
	ObserveUpstreamUsage(types.OpenAI, "", 100, 100)
	if after := anthropic.BytesPerTokenX1000(); after != before {
		t.Fatalf("invalid inputs must not mutate ratio: %d -> %d", before, after)
	}
}

func TestAnthropicTokenizer_FallbackRatio(t *testing.T) {
	defer resetForTest()
	anthropic.bytesPerTokenX1000.Store(0)
	if got := anthropic.CountString("hello world"); got <= 0 {
		t.Fatalf("zero ratio must fall back to default, got %d", got)
	}
}

func TestAnthropicTokenizer_ObserveFallbackRatio(t *testing.T) {
	defer resetForTest()
	anthropic.bytesPerTokenX1000.Store(-1)
	ObserveUpstreamUsage(types.Anthropic, "", 100, 110)
	if r := anthropic.BytesPerTokenX1000(); r <= 0 {
		t.Fatal("observe must restore a positive ratio")
	}
}

func TestOpenAITokenizer_Roundtrip(t *testing.T) {
	if got := openaiTokenizer.CountString(""); got != 0 {
		t.Fatalf("empty: %d", got)
	}
	if got := openaiTokenizer.CountString("hello world"); got < 1 {
		t.Fatalf("short: %d", got)
	}
}

func TestOpenAITokenizer_CountMessages(t *testing.T) {
	msgs := []types.Message{{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "hi"}}}}
	if got := openaiTokenizer.CountMessages(msgs); got < 1 {
		t.Fatalf("count: %d", got)
	}
}

func TestUniversalFallback_DelegatesToOpenAI(t *testing.T) {
	if universal.Name() != "universal-fallback" {
		t.Fatal("name")
	}
	// Same output as openai for the same input.
	a := universal.CountString("hello world")
	b := openaiTokenizer.CountString("hello world")
	if a != b {
		t.Fatalf("delegate mismatch: universal=%d openai=%d", a, b)
	}
	msgs := []types.Message{{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "x"}}}}
	if universal.CountMessages(msgs) != openaiTokenizer.CountMessages(msgs) {
		t.Fatal("messages delegate")
	}
}

func TestModelEncoder(t *testing.T) {
	if got := ModelEncoder("gpt-4o"); got != "o200k_base" {
		t.Fatalf("gpt-4o: %s", got)
	}
	if got := ModelEncoder("o1-preview"); got != "o200k_base" {
		t.Fatalf("o1: %s", got)
	}
	if got := ModelEncoder("o3"); got != "o200k_base" {
		t.Fatalf("o3: %s", got)
	}
	if got := ModelEncoder("claude-3.5-sonnet"); got != "cl100k_base" {
		t.Fatalf("default: %s", got)
	}
}

func TestCountCJKRunes(t *testing.T) {
	if got := countCJKRunes("abc"); got != 0 {
		t.Fatalf("ascii: %d", got)
	}
	if got := countCJKRunes("你好"); got != 2 {
		t.Fatalf("han: %d", got)
	}
	if got := countCJKRunes("ひら"); got != 2 {
		t.Fatalf("hiragana: %d", got)
	}
	if got := countCJKRunes("가나"); got != 2 {
		t.Fatalf("hangul: %d", got)
	}
}

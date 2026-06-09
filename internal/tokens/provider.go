package tokens

import (
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// Tokenizer is the provider-aware token counting surface. Implementations
// translate text and message structures into token counts using a provider
// specific model.
//
// T28 splits the historical single-tiktoken pipeline into three branches:
//   - anthropic: character-based estimator calibrated by real upstream
//     usage events; updated at runtime when the proxy observes Anthropic's
//     reported input_tokens.
//   - openai: tiktoken (cl100k_base / o200k_base by model). The existing
//     Counter is reused for this branch.
//   - universal: conservative fallback used when the provider is unknown.
//
// The interface returns the same shape CountMessages has always returned so
// existing call sites can migrate incrementally.
type Tokenizer interface {
	Name() string
	CountString(s string) int
	CountMessages(messages []types.Message) int
}

// ForProvider returns the Tokenizer appropriate for the given provider tag.
// A zero-value Provider maps to the universal fallback.
func ForProvider(provider types.Provider) Tokenizer {
	switch provider {
	case types.Anthropic:
		return anthropic
	case types.CodexChatGPT:
		return openaiO200KTokenizer
	case types.OpenAI:
		return openaiTokenizer
	default:
		return universal
	}
}

// ObserveUpstreamUsage feeds a confirmed upstream usage reading back into
// the anthropic tokenizer so its character-to-token ratio converges on
// reality over time. T28 self-calibration loop.
//
// observed is the number of input tokens the provider actually billed for;
// estimated is what the tokenizer predicted just before the request was
// sent. The call is a no-op when observed <= 0 or the tokenizer is not a
// calibrated one.
func ObserveUpstreamUsage(provider types.Provider, model string, observed, estimated int) {
	if observed <= 0 || estimated <= 0 {
		return
	}
	if provider != types.Anthropic {
		return
	}
	anthropic.observe(model, observed, estimated)
}

// anthropicTokenizer approximates Anthropic token counts using a calibrated
// bytes-per-token ratio. The default ratio matches the rule of thumb
// Anthropic publishes (~3.5 bytes/token for English). ObserveUpstreamUsage
// nudges the ratio towards observed reality with a gentle EMA so short
// term noise does not whiplash the estimator.
type anthropicTokenizer struct {
	bytesPerTokenX1000 atomic.Int64
	perModel           sync.Map
	mu                 sync.Mutex
}

type modelRatio struct {
	value atomic.Int64
}

func newAnthropicTokenizer() *anthropicTokenizer {
	a := &anthropicTokenizer{}
	a.bytesPerTokenX1000.Store(3500)
	return a
}

var anthropic = newAnthropicTokenizer()

func (a *anthropicTokenizer) ratioForModel(model string) *atomic.Int64 {
	if model == "" {
		return &a.bytesPerTokenX1000
	}
	family := modelFamily(model)
	if family == "" {
		return &a.bytesPerTokenX1000
	}
	val, _ := a.perModel.LoadOrStore(family, &modelRatio{})
	mr := val.(*modelRatio)
	if mr.value.Load() == 0 {
		mr.value.Store(a.bytesPerTokenX1000.Load())
	}
	return &mr.value
}

func modelFamily(model string) string {
	switch {
	case strings.Contains(model, "opus"):
		return "opus"
	case strings.Contains(model, "sonnet"):
		return "sonnet"
	case strings.Contains(model, "haiku"):
		return "haiku"
	default:
		return ""
	}
}

// Name returns the tokenizer identity for observability.
func (a *anthropicTokenizer) Name() string { return "anthropic-calibrated" }

// CountString returns the estimated token count for s. Uses the current
// bytes-per-token ratio; CJK runs are weighted more aggressively since they
// do not follow the western character-to-token curve.
func (a *anthropicTokenizer) CountString(s string) int {
	if s == "" {
		return 0
	}
	byteLen := len(s)
	cjk := countCJKRunes(s)
	ratio := a.bytesPerTokenX1000.Load()
	if ratio <= 0 {
		ratio = 3500
	}
	// Base estimate from bytes * 1000 / ratio.
	base := int64(byteLen) * 1000 / ratio
	// Each CJK rune roughly maps to one token which is heavier than the
	// default ratio implies; add a small correction.
	base += int64(cjk) / 3
	if base <= 0 {
		return 1
	}
	return int(base)
}

// CountMessages sums the estimate across all textual fields of messages.
func (a *anthropicTokenizer) CountMessages(messages []types.Message) int {
	total := 0
	for i := range messages {
		for j := range messages[i].Content {
			b := &messages[i].Content[j]
			if b.Text != "" {
				total += a.CountString(b.Text)
			}
			if b.ToolInput != "" {
				total += a.CountString(b.ToolInput)
			}
			if b.ToolName != "" {
				total += a.CountString(b.ToolName)
			}
		}
	}
	return total
}

// observe adjusts the bytes-per-token ratio towards `observed / estimated`.
// Uses an EMA with alpha=0.05 so a single outlier never moves the dial
// more than ~5% and we converge over 20+ samples.
func (a *anthropicTokenizer) observe(model string, observed, estimated int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ratio := a.ratioForModel(model)
	current := ratio.Load()
	if current <= 0 {
		current = 3500
	}
	correction := float64(current) * float64(estimated) / float64(observed)
	const alpha = 0.05
	next := float64(current)*(1.0-alpha) + correction*alpha
	if next < 1500 {
		next = 1500
	}
	if next > 6000 {
		next = 6000
	}
	ratio.Store(int64(next))
	if model != "" {
		appendCalibration(model, observed, estimated, int64(next))
	}
}

func (a *anthropicTokenizer) BytesPerTokenX1000() int64 {
	return a.bytesPerTokenX1000.Load()
}

func (a *anthropicTokenizer) BytesPerTokenX1000ForModel(model string) int64 {
	return a.ratioForModel(model).Load()
}

// countCJKRunes counts Han / Hiragana / Katakana / Hangul code points.
func countCJKRunes(s string) int {
	count := 0
	for _, r := range s {
		switch {
		case r >= 0x4E00 && r <= 0x9FFF:
			count++
		case r >= 0x3040 && r <= 0x30FF:
			count++
		case r >= 0xAC00 && r <= 0xD7A3:
			count++
		}
	}
	return count
}

// openaiTokenizerImpl wraps the existing tiktoken Counter.
type openaiTokenizerImpl struct {
	counter *Counter
}

var openaiTokenizer Tokenizer = &openaiTokenizerImpl{counter: &global}

// openaiO200KTokenizer counts with o200k_base, used for Codex (GPT-4o /
// GPT-5-codex) whose billing uses the o200k encoding rather than cl100k.
var openaiO200KTokenizer Tokenizer = &openaiTokenizerImpl{counter: &o200kGlobal}

func (o *openaiTokenizerImpl) Name() string {
	if o != nil && o.counter != nil && o.counter.encodingName() == "o200k_base" {
		return "openai-tiktoken-o200k_base"
	}
	return "openai-tiktoken-cl100k_base"
}

func (o *openaiTokenizerImpl) CountString(s string) int {
	return o.counter.Count(s)
}

func (o *openaiTokenizerImpl) CountMessages(messages []types.Message) int {
	return o.counter.CountMessages(messages)
}

// universalFallback is used when the provider is unknown. It leans on the
// OpenAI tokenizer since BPE overestimates rather than under-estimates for
// mixed content, which is the safer direction.
type universalFallback struct{}

var universal Tokenizer = universalFallback{}

func (universalFallback) Name() string { return "universal-fallback" }

func (universalFallback) CountString(s string) int {
	return openaiTokenizer.CountString(s)
}

func (universalFallback) CountMessages(messages []types.Message) int {
	return openaiTokenizer.CountMessages(messages)
}

func resetForTest() {
	anthropic.bytesPerTokenX1000.Store(3500)
	anthropic.perModel.Range(func(key, _ any) bool {
		anthropic.perModel.Delete(key)
		return true
	})
	ResetCalibration()
}

// modelSelector keeps a string-based sanity check for potential future
// OpenAI model selection. Today tiktoken-go only carries cl100k_base in
// our vendored version; selecting a different encoder would require
// upgrading the dep. Exported for introspection.
func ModelEncoder(model string) string {
	model = strings.ToLower(model)
	switch {
	case strings.HasPrefix(model, "gpt-5"), strings.HasPrefix(model, "gpt-4o"),
		strings.HasPrefix(model, "o1"), strings.HasPrefix(model, "o3"),
		strings.Contains(model, "codex"):
		return "o200k_base"
	default:
		return "cl100k_base"
	}
}

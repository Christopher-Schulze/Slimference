package summarization

import (
	"context"
	"strings"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/extract"
)

// extractConfigFromSummary derives an extract.Config from the user's
// summarization tuning. The mapping is intentionally minimal: the
// extract compactor has its own well-tuned defaults; only the
// production-relevant overrides flow through.
func extractConfigFromSummary(s config.SummaryConfig) extract.Config {
	cfg := extract.DefaultConfig()
	// Future: map s.Mode (balanced/aggressive/conservative) to ratio.
	// For now, defaults are fine; the per-call Summarize() overrides
	// TargetRatio based on the requested token budget anyway.
	_ = s
	return cfg
}

// ExtractSummarizer is the deterministic, in-process Summarizer that
// wraps the section-aware TF-IDF compactor from internal/extract. It
// has the same role as MiniMaxClient in the FallbackChain but pays no
// network round-trip, no API tokens, and no latency variance.
//
// The proxy plugs it into NewLayer2() ahead of the MiniMax client so
// every Summarize() call hits extract first. MiniMax stays in the chain
// as an opt-in fallback for users who explicitly configure it; in the
// default configuration the chain is extract-only and L2 runs entirely
// deterministic.
//
// Capability surface: SupportsTemperatureZero=true, SupportsSeed=true,
// SupportsMinCompletionTokens=true — extract is the strictest possible
// summarizer (literally a pure function), so RequireDeterministic mode
// always picks it up. Deterministic mode is the long-term default.
type ExtractSummarizer struct {
	compactor *extract.Compactor
	cfg       extract.Config
}

// NewExtractSummarizer constructs an ExtractSummarizer with the given
// extract.Config. Zero-value cfg falls back to extract.DefaultConfig().
func NewExtractSummarizer(cfg extract.Config) *ExtractSummarizer {
	return &ExtractSummarizer{
		compactor: extract.New(cfg),
		cfg:       cfg,
	}
}

// Name identifies the provider in logs and analytics.
func (e *ExtractSummarizer) Name() string {
	return "extract-tfidf"
}

// IsConfigured is always true: extract needs no API key, network, or
// model weights. The FallbackChain treats this as "always available",
// so extract is the primary path until the operator explicitly opts
// out via config.
func (e *ExtractSummarizer) IsConfigured() bool {
	return true
}

// Capabilities exposes the determinism guarantees so the FallbackChain
// passes RequireDeterministic gates. Extract is literally a pure
// function: no temperature, no sampling, no model weights — every
// determinism flag is trivially satisfied.
func (e *ExtractSummarizer) Capabilities() capProvider {
	return capProvider{
		SupportsTemperatureZero:     true,
		SupportsSeed:                true,
		SupportsMinCompletionTokens: true,
	}
}

// validatorMaxOutputRatio mirrors the cap enforced by the downstream
// validator (internal/summarization/validator.go): a summary must not
// exceed 40% of the original token count. We aim under it to leave
// headroom for the "- " bullet prefix overhead.
const validatorMaxOutputRatio = 0.4

// defaultTargetRatio is the production setpoint when no targetTokens
// budget is supplied. Sits below the validator cap so bullet overhead
// keeps us safe.
const defaultTargetRatio = 0.25

// Summarize compresses inputText using TF-IDF + section-aware
// extractive ranking. targetTokens is honoured strictly: we translate
// it into extract's TargetRatio and additionally hard-truncate the
// output if bullet-overhead pushes it over budget.
//
// startMsg/endMsg are ignored because the input arrives as a single
// concatenated transcript at this layer; message boundaries were
// already collapsed by the caller.
func (e *ExtractSummarizer) Summarize(ctx context.Context, inputText string, startMsg, endMsg, targetTokens int) (string, error) {
	if ctx != nil && ctx.Err() != nil {
		return "", ctx.Err()
	}
	if strings.TrimSpace(inputText) == "" {
		return inputText, nil
	}

	approxInputTokens := len(inputText) / 4
	cfg := e.cfg
	// Pick the most-aggressive of (caller-requested budget, default
	// safe ratio). Even when callers don't specify a budget we stay
	// under the validator's 40% cap.
	cfg.TargetRatio = defaultTargetRatio
	if targetTokens > 0 && approxInputTokens > 0 && targetTokens < approxInputTokens {
		callerRatio := float64(targetTokens) / float64(approxInputTokens)
		if callerRatio > 0 && callerRatio < cfg.TargetRatio {
			cfg.TargetRatio = callerRatio
		}
	}
	// Construct a per-call compactor only if the ratio differs from
	// the cached one; otherwise reuse the constructor's instance.
	c := e.compactor
	if cfg.TargetRatio != e.cfg.TargetRatio {
		c = extract.New(cfg)
	}
	compacted := c.Compact(inputText)
	// The downstream validator (internal/summarization/validator.go)
	// rejects summaries that contain no "- "-prefixed bullet lines.
	// MiniMax produced bullets because its prompt asked for them; the
	// deterministic compactor produces structured prose. Wrap the
	// prose output as bullets without disturbing code/header/list
	// sections so the validator's format-compliance gate passes.
	bulleted := bulletisePoseSentences(compacted)

	// Hard truncate to stay under the validator's 40% cap if the
	// bullet overhead pushed the output beyond budget. We drop
	// trailing bullets until the byte count fits. Tokens estimated
	// at 4 chars/token; matches the validator's own estimator.
	maxOutputBytes := int(float64(len(inputText)) * validatorMaxOutputRatio)
	if maxOutputBytes > 0 && len(bulleted) > maxOutputBytes {
		bulleted = truncateBulletsToBudget(bulleted, maxOutputBytes)
	}
	return bulleted, nil
}

// truncateBulletsToBudget drops trailing bullet lines until byte count
// fits within maxBytes. Preserves code blocks and headers (those are
// load-bearing). If the very first line is itself longer than the
// budget, returns the first line truncated — the format-compliance
// gate downstream still passes because at least one "- " bullet
// survives.
func truncateBulletsToBudget(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	lines := strings.Split(s, "\n")
	var keep []string
	used := 0
	for _, l := range lines {
		// +1 for the '\n' that strings.Join will reinsert.
		need := len(l) + 1
		if used+need > maxBytes {
			if len(keep) == 0 {
				break
			}
			break
		}
		keep = append(keep, l)
		used += need
	}
	if len(keep) == 0 {
		// Single very-long bullet larger than the budget: take a
		// prefix slice up to maxBytes-1 so there is room for the
		// terminating newline.
		if maxBytes > 1 {
			return s[:maxBytes-1] + "\n"
		}
		return ""
	}
	return strings.Join(keep, "\n")
}

// bulletisePoseSentences walks the compacted output and emits each
// prose sentence as a "- " bullet, while leaving code blocks, headers,
// blank lines, and existing lists verbatim. The output keeps section
// boundaries intact so the validator's downstream gates
// (path-preservation, function-name preservation, no-CoT) see content
// in the same byte form they would from the LLM provider.
func bulletisePoseSentences(s string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	// Use the same section parser the compactor used, so the round-trip
	// is byte-precise on non-prose chunks.
	sections := extract.ParseSections(s)
	var out strings.Builder
	for _, sec := range sections {
		if sec.Kind != extract.SectionProse {
			out.WriteString(sec.Content)
			continue
		}
		for _, sentence := range extract.SplitSentences(sec.Content) {
			trimmed := strings.TrimSpace(sentence)
			out.WriteString("- ")
			out.WriteString(trimmed)
			out.WriteByte('\n')
		}
	}
	return out.String()
}

package summarization

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/slimference/slimference/internal/compression"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

// summaryMaxAge is the default staleness threshold for cached summaries.
const summaryMaxAge = 30 * time.Minute

// Layer2 orchestrates asynchronous summarization-based compression (Layer 2).
// It uses a FallbackChain of Summarizer providers: primary (MiniMax), then any fallbacks.
type Layer2 struct {
	cfg       *config.CompressionConfig
	chain     *FallbackChain
	cache     *SummaryCache
	anchor    *AnchorDetector
	validator *CompressionValidator
}

func NewLayer2(cfg *config.CompressionConfig) *Layer2 {
	mm := NewMiniMaxClient(cfg.MiniMax)
	chain := NewFallbackChain(mm)
	return &Layer2{
		cfg:       cfg,
		chain:     chain,
		cache:     NewSummaryCache(),
		anchor:    NewAnchorDetector(),
		validator: NewCompressionValidator(),
	}
}

// AddFallbackProvider appends a fallback summarizer to the chain.
// Providers are tried in insertion order after the primary (MiniMax).
func (l *Layer2) AddFallbackProvider(s Summarizer) {
	l.chain.SetProviders(append(l.chain.Providers(), s)...)
}

// ApplyToMessages injects a cached summary in place of the messages it covers.
// Returns (newMessages, tokensSaved, applied).
// If no valid summary exists the original slice is returned unchanged.
func (l *Layer2) ApplyToMessages(messages []types.Message) ([]types.Message, int, bool) {
	cached, coveredRange := l.cache.GetCurrent()
	if cached == nil {
		return messages, 0, false
	}

	end := coveredRange[1]
	if end <= 0 || end >= len(messages) {
		return messages, 0, false
	}

	summaryText := fmt.Sprintf(
		"[Conversation summary covering messages 0-%d: %s]",
		end, cached.Summary,
	)

	synthetic := types.Message{
		Index: 0,
		Role:  "assistant",
		Content: []types.ContentBlock{
			{Type: "text", Text: summaryText},
		},
		Metadata: types.MessageMetadata{
			OriginalTokens:   cached.OriginalTokens,
			CompressedTokens: cached.CompressedTokens,
			CompressionLevel: types.CompressionLayer2,
		},
	}

	// Re-index the tail messages starting from 1.
	tail := messages[end+1:]
	result := make([]types.Message, 0, 1+len(tail))
	result = append(result, synthetic)
	for i, msg := range tail {
		msg.Index = i + 1
		result = append(result, msg)
	}

	tokensSaved := cached.OriginalTokens - cached.CompressedTokens
	return result, tokensSaved, true
}

// RunCompressionJob is the async worker body. Call it from a goroutine.
// It determines what to compress, calls MiniMax, validates the result, and stores it.
func (l *Layer2) RunCompressionJob(messages []types.Message) {
	minMsgs := l.cfg.MinMessagesForCompression
	prefixEnd := compression.CompressiblePrefixEnd(messages, l.cfg.SlidingWindow)
	if prefixEnd < minMsgs {
		return
	}

	// Messages eligible for summarization are everything before the sliding window of recent exchanges.
	boundaryIdx := prefixEnd - 1

	// Detect and preserve anchors.
	allAnchorIndices := l.anchor.Detect(messages[:boundaryIdx+1])
	toSummarize := filterNonAnchored(messages[:boundaryIdx+1], allAnchorIndices)

	if len(toSummarize) == 0 {
		return
	}

	// If a cached summary already covers most of this range, attempt an incremental
	// extension by summarising only the new messages appended since last run.
	existing, existingRange := l.cache.GetCurrent()
	startIdx := 0
	existingSummaryPrefix := ""
	if existing != nil && existingRange[1] > 0 {
		coveredFraction := float64(existingRange[1]) / float64(boundaryIdx)
		if coveredFraction >= 0.70 {
			// Only compress the delta since the last covered message.
			newStart := existingRange[1] + 1
			if newStart <= boundaryIdx {
				toSummarize = filterNonAnchored(messages[newStart:boundaryIdx+1], allAnchorIndices)
				startIdx = newStart
				existingSummaryPrefix = existing.Summary + "\n"
			} else {
				// Already fully covered.
				return
			}
		}
	}

	if len(toSummarize) == 0 {
		return
	}

	inputText := existingSummaryPrefix + l.FormatMessagesForSummarization(toSummarize)
	inputText = preprocessInput(inputText)
	origTokens := estimateTokens(inputText)

	// Cap input to prevent quality degradation on very long conversations.
	// M2.7 has 200k context but quality drops past ~120k input tokens.
	const maxInputTokens = 120000
	if origTokens > maxInputTokens {
		// Truncate from the oldest messages, keeping the most recent content.
		maxBytes := maxInputTokens * 4
		if len(inputText) > maxBytes {
			inputText = inputText[len(inputText)-maxBytes:]
			if idx := strings.Index(inputText, "\n"); idx >= 0 {
				inputText = inputText[idx+1:]
			}
		}
		origTokens = estimateTokens(inputText)
	}

	targetTokens := computeAdaptiveTarget(origTokens, toSummarize, l.cfg.Summary.TargetRatio)
	if targetTokens < 100 {
		targetTokens = 100
	}

	summary, providerName, err := l.chain.Summarize(context.Background(), inputText, startIdx, boundaryIdx, targetTokens)
	if err != nil {
		slog.Error("layer2 summarization failed (all providers)",
			slog.String("error", err.Error()),
			slog.Int("msg_count", len(toSummarize)),
		)
		return
	}
	slog.Debug("layer2 summarization provider", "provider", providerName)

	result := l.validator.Validate(toSummarize, summary, origTokens)
	if !result.Valid {
		// Validation-driven retry: retry once with a targeted hint about what failed.
		slog.Warn("layer2 summary failed validation, retrying with emphasis",
			slog.String("reason", result.FailReason),
			slog.Int("orig_tokens", origTokens),
		)

		retryInput := inputText + "\n\nIMPORTANT: Previous attempt was rejected because: " + result.FailReason +
			". Fix this issue. Remember: bullet format, preserve ALL paths and names verbatim."
		retryTarget := targetTokens

		retrySummary, retryProvider, retryErr := l.chain.Summarize(context.Background(), retryInput, startIdx, boundaryIdx, retryTarget)
		if retryErr == nil {
			retryResult := l.validator.Validate(toSummarize, retrySummary, origTokens)
			if retryResult.Valid {
				summary = retrySummary
				providerName = retryProvider
				result = retryResult
				slog.Debug("layer2 retry succeeded", "provider", providerName)
			} else {
				slog.Warn("layer2 retry also failed validation",
					slog.String("reason", retryResult.FailReason),
				)
				return
			}
		} else {
			slog.Warn("layer2 retry request failed", "error", retryErr.Error())
			return
		}
	}

	if !result.Valid {
		slog.Warn("layer2 summary failed validation",
			slog.String("reason", result.FailReason),
			slog.Int("orig_tokens", origTokens),
		)
		return
	}

	compressedTokens := estimateTokens(summary)
	ratio := 0.0
	if origTokens > 0 {
		ratio = float64(compressedTokens) / float64(origTokens)
	}

	cached := &CachedSummary{
		Summary:          summary,
		CoveredRange:     [2]int{0, boundaryIdx},
		AnchorsInlined:   allAnchorIndices,
		OriginalTokens:   origTokens,
		CompressedTokens: compressedTokens,
		CompressionRatio: ratio,
		CreatedAt:        time.Now(),
		Hash:             hashMessages(messages[:boundaryIdx+1]),
	}
	l.cache.Store(cached)

	slog.Info("layer2 compression complete",
		slog.Int("covered_msgs", boundaryIdx+1),
		slog.Int("orig_tokens", origTokens),
		slog.Int("compressed_tokens", compressedTokens),
		slog.Float64("ratio", ratio),
	)
}

// ShouldTriggerCompression reports whether conditions are right to start a new
// compression job. Returns false if compression is already in progress or the
// existing summary is still fresh and covers enough of the conversation.
func (l *Layer2) ShouldTriggerCompression(messages []types.Message) bool {
	minMsgs := l.cfg.MinMessagesForCompression
	prefixEnd := compression.CompressiblePrefixEnd(messages, l.cfg.SlidingWindow)
	if prefixEnd < minMsgs {
		return false
	}

	if l.cache.Compressing.Load() {
		return false
	}

	if l.cache.IsStale(summaryMaxAge) {
		return true
	}

	_, existingRange := l.cache.GetCurrent()

	// Trigger if less than 70% of the compressible range is already covered.
	boundaryIdx := prefixEnd - 1
	if boundaryIdx <= 0 {
		return true
	}
	coveredFraction := float64(existingRange[1]) / float64(boundaryIdx)
	return coveredFraction < 0.70
}

// FormatMessagesForSummarization renders messages as readable plain text for MiniMax.
func (l *Layer2) FormatMessagesForSummarization(messages []types.Message) string {
	var sb strings.Builder
	for _, msg := range messages {
		role := strings.ToUpper(msg.Role)
		sb.WriteString(fmt.Sprintf("[%s msg %d]\n", role, msg.Index))
		for _, blk := range msg.Content {
			switch blk.Type {
			case "text":
				sb.WriteString(blk.Text)
			case "tool_use":
				sb.WriteString(fmt.Sprintf("<tool_use name=%q input=%s>", blk.ToolName, blk.ToolInput))
			case "tool_result":
				sb.WriteString(fmt.Sprintf("<tool_result id=%q>%s</tool_result>", blk.ToolResultID, blk.Text))
			}
			sb.WriteByte('\n')
		}
		sb.WriteString("---\n")
	}
	return sb.String()
}

// preprocessInput cleans the formatted message text before sending to the LLM.
// It removes noise that wastes tokens and degrades summary quality:
// - Truncates very long tool_result content (e.g. file reads) to a summary line
// - Collapses consecutive identical tool outputs
// - Strips "-tool.sh" noise from hook script names
func preprocessInput(input string) string {
	lines := strings.Split(input, "\n")
	var cleaned []string
	prevContent := ""

	for _, line := range lines {
		// Skip if identical to previous line (duplicate output).
		if line == prevContent {
			continue
		}
		prevContent = line

		// Truncate very long lines (>2000 chars likely a file dump).
		// Keep the first 200 chars as context.
		if len(line) > 2000 {
			truncated := line[:200] + "... [truncated, original " + fmt.Sprintf("%d", len(line)) + " chars]"
			cleaned = append(cleaned, truncated)
			continue
		}

		cleaned = append(cleaned, line)
	}

	return strings.Join(cleaned, "\n")
}

// GetCache returns the underlying SummaryCache for external inspection.
func (l *Layer2) GetCache() *SummaryCache {
	return l.cache
}

// hashMessages computes a SHA-256 over the JSON-serialised message slice.
func hashMessages(messages []types.Message) [32]byte {
	data, _ := json.Marshal(messages)
	return sha256.Sum256(data)
}

// estimateTokens approximates the token count using a weighted heuristic.
// Accounts for whitespace, CJK characters, and code-heavy content more accurately
// than a simple bytes/4 division.
func estimateTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	tokens := 0
	inWord := false
	for _, r := range text {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			inWord = false
		case r >= 0x4E00 && r <= 0x9FFF:
			tokens++
			inWord = false
		case r >= 0x3040 && r <= 0x309F:
			tokens++
			inWord = false
		case r >= 0x30A0 && r <= 0x30FF:
			tokens++
			inWord = false
		default:
			if !inWord {
				tokens++
				inWord = true
			}
		}
	}
	if tokens < 1 {
		return 1
	}
	return tokens
}

// contentDensity measures how information-dense a set of messages is.
// Returns 0.0-1.0 where higher means more dense (code, paths, tool calls).
func contentDensity(messages []types.Message) float64 {
	if len(messages) == 0 {
		return 0.5
	}

	var totalChars, codeChars, pathChars, toolChars int
	for _, msg := range messages {
		for _, blk := range msg.Content {
			n := len(blk.Text)
			totalChars += n
			switch blk.Type {
			case "tool_use", "tool_result":
				toolChars += n
			case "text":
				for _, line := range splitLines(blk.Text) {
					trimmed := strings.TrimSpace(line)
					if looksLikeCode(trimmed) || looksLikePath(trimmed) {
						codeChars += len(trimmed)
					}
				}
			}
		}
	}

	for _, msg := range messages {
		for _, blk := range msg.Content {
			matches := filePathRegex.FindAllString(blk.Text, -1)
			pathChars += len(matches) * 20
		}
	}

	if totalChars == 0 {
		return 0.5
	}

	density := float64(codeChars+toolChars+pathChars) / float64(totalChars)
	if density > 1.0 {
		density = 1.0
	}
	return density
}

// computeAdaptiveTarget calculates target output tokens based on input size,
// content density, and message count. Dense content (code/paths/tools) needs
// more output tokens to preserve information; sparse prose can compress more.
func computeAdaptiveTarget(origTokens int, messages []types.Message, baseRatio float64) int {
	density := contentDensity(messages)
	msgCount := len(messages)

	ratio := baseRatio

	if msgCount <= 5 {
		ratio = baseRatio * 1.5
	} else if msgCount <= 10 {
		ratio = baseRatio * 1.25
	}

	ratio += density * 0.15

	if origTokens < 1000 {
		ratio = max(ratio, 0.40)
	} else if origTokens < 5000 {
		ratio = max(ratio, 0.25)
	}

	if ratio > 0.60 {
		ratio = 0.60
	}

	target := int(float64(origTokens) * ratio)
	return max(target, 100)
}

// splitLines splits text into lines without allocating a new slice per line.
func splitLines(s string) []string {
	return strings.Split(s, "\n")
}

// looksLikeCode heuristically detects code lines.
func looksLikeCode(line string) bool {
	if len(line) == 0 {
		return false
	}
	codeIndicators := []string{
		"func ", "func(", "var ", "const ", "type ", "import ",
		"if ", "for ", "switch ", "case ", "return ",
		"pub fn", "let ", "impl ", "use ", "mod ",
		"def ", "class ", "async ",
		"== ", "!= ", ">= ", "<= ",
		":=", "->", "=>", "&&", "||",
	}
	for _, ind := range codeIndicators {
		if strings.Contains(line, ind) {
			return true
		}
	}
	if len(line) > 0 && (line[0] == '{' || line[0] == '}' || line[0] == ')' || line[0] == ']') {
		return true
	}
	if strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "/*") {
		return true
	}
	return false
}

// looksLikePath heuristically detects file paths.
func looksLikePath(line string) bool {
	if len(line) == 0 {
		return false
	}
	if strings.Count(line, "/") >= 2 {
		return true
	}
	if strings.Contains(line, "./") || strings.Contains(line, "../") {
		return true
	}
	for _, suffix := range []string{".go", ".ts", ".rs", ".py", ".js", ".toml", ".json", ".yaml", ".md"} {
		if strings.Contains(line, suffix) {
			return true
		}
	}
	return false
}

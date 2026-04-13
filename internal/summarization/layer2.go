package summarization

import (
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

// Layer2 orchestrates asynchronous MiniMax-based compression (Layer 2).
type Layer2 struct {
	cfg       *config.CompressionConfig
	minimax   *MiniMaxClient
	cache     *SummaryCache
	anchor    *AnchorDetector
	validator *CompressionValidator
}

// NewLayer2 constructs a Layer2 coordinator from the supplied configuration.
func NewLayer2(cfg *config.CompressionConfig) *Layer2 {
	return &Layer2{
		cfg:       cfg,
		minimax:   NewMiniMaxClient(cfg.MiniMax),
		cache:     NewSummaryCache(),
		anchor:    NewAnchorDetector(),
		validator: NewCompressionValidator(),
	}
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
	origTokens := estimateTokens(inputText)

	targetTokens := int(float64(origTokens) * l.cfg.Summary.TargetRatio)
	if targetTokens < 100 {
		targetTokens = 100
	}

	summary, err := l.minimax.Summarize(inputText, startIdx, boundaryIdx, targetTokens)
	if err != nil {
		slog.Error("layer2 minimax summarize failed",
			slog.String("error", err.Error()),
			slog.Int("msg_count", len(toSummarize)),
		)
		return
	}

	result := l.validator.Validate(toSummarize, summary, origTokens)
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

// GetCache returns the underlying SummaryCache for external inspection.
func (l *Layer2) GetCache() *SummaryCache {
	return l.cache
}

// hashMessages computes a SHA-256 over the JSON-serialised message slice.
func hashMessages(messages []types.Message) [32]byte {
	data, _ := json.Marshal(messages)
	return sha256.Sum256(data)
}

// estimateTokens approximates the token count using a 4-bytes-per-token heuristic.
func estimateTokens(text string) int {
	n := len(text) / 4
	if n < 1 && len(text) > 0 {
		return 1
	}
	return n
}

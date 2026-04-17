package summarization

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/slimference/slimference/internal/types"
)

// CompressionTier describes one contiguous range of messages and its target ratio.
type CompressionTier struct {
	// Name is a human-readable label for logging.
	Name string

	// MsgRange is the inclusive [start, end] index range covered by this tier.
	MsgRange [2]int

	// TargetRatio is the desired compressed/original token ratio (0.0-1.0).
	TargetRatio float64
}

// DetermineCompressionTiers produces a tier plan for the given message count.
//
// Tier layout:
//   - Sessions with fewer than 20 compressible messages use a single tier at 20%.
//   - Sessions >= 20 messages use up to four tiers with increasing ratios toward
//     the recency window, which is always kept at 100% (uncompressed).
func DetermineCompressionTiers(totalMessages, windowSize int) []CompressionTier {
	compressible := totalMessages - windowSize
	if compressible <= 0 {
		return nil
	}

	// Short sessions: single tier.
	if compressible < 20 {
		return []CompressionTier{
			{
				Name:        "tier-single",
				MsgRange:    [2]int{0, compressible - 1},
				TargetRatio: 0.20,
			},
		}
	}

	// Multi-tier layout.
	// Tier 1: msgs 0-19 (oldest, most aggressively compressed).
	// Tier 2: msgs 20-34 (if available).
	// Tier 3: msgs 35 through end of compressible range (softer compression).
	// Window: kept verbatim by the caller - not included in tier output.
	var tiers []CompressionTier

	// Tier 1 always present for sessions >= 20 messages.
	t1End := min(19, compressible-1)
	tiers = append(tiers, CompressionTier{
		Name:        "tier-1",
		MsgRange:    [2]int{0, t1End},
		TargetRatio: 0.10,
	})

	if compressible > 20 {
		t2End := min(34, compressible-1)
		tiers = append(tiers, CompressionTier{
			Name:        "tier-2",
			MsgRange:    [2]int{20, t2End},
			TargetRatio: 0.20,
		})

		if compressible > 35 {
			tiers = append(tiers, CompressionTier{
				Name:        "tier-3",
				MsgRange:    [2]int{35, compressible - 1},
				TargetRatio: 0.60,
			})
		}
	}

	// Append a descriptor for the uncompressed window (informational only).
	tiers = append(tiers, CompressionTier{
		Name:        "window",
		MsgRange:    [2]int{compressible, totalMessages - 1},
		TargetRatio: 1.0,
	})

	return tiers
}

// ApplyProgressiveTiers compresses each tier independently and reassembles the
// message array. Tier entries with TargetRatio == 1.0 are kept verbatim.
// Compression failures for a tier are logged and that tier is kept verbatim.
func (l *Layer2) ApplyProgressiveTiers(messages []types.Message, tiers []CompressionTier) []types.Message {
	ctx, cancel := l.withJobTimeout(context.Background())
	defer cancel()
	return l.applyProgressiveTiersWithContext(ctx, messages, tiers)
}

func (l *Layer2) applyProgressiveTiersWithContext(ctx context.Context, messages []types.Message, tiers []CompressionTier) []types.Message {
	if len(tiers) == 0 {
		return messages
	}

	result := make([]types.Message, 0, len(messages))
	nextIndex := 0

	for _, tier := range tiers {
		start := tier.MsgRange[0]
		end := tier.MsgRange[1]
		if ctx.Err() != nil {
			return appendVerbatimTail(result, messages, start, nextIndex)
		}

		if start >= len(messages) {
			break
		}
		if end >= len(messages) {
			end = len(messages) - 1
		}

		slice := messages[start : end+1]

		// Keep verbatim if ratio is 1.0 or no client configured.
		if tier.TargetRatio >= 1.0 || l.chain.ActiveProviderName() == "" {
			for _, msg := range slice {
				msg.Index = nextIndex
				nextIndex++
				result = append(result, msg)
			}
			continue
		}

		// Anchor detection: always preserve anchor messages verbatim within the tier.
		anchorIndices := l.anchor.Detect(slice)
		anchoredSet := make(map[int]bool, len(anchorIndices))
		for _, ai := range anchorIndices {
			anchoredSet[ai] = true
		}

		toSummarize := filterNonAnchored(slice, anchorIndices)

		if len(toSummarize) == 0 {
			// All messages in this tier are anchors - keep verbatim.
			for _, msg := range slice {
				msg.Index = nextIndex
				nextIndex++
				result = append(result, msg)
			}
			continue
		}

		inputText := l.FormatMessagesForSummarization(toSummarize)
		origTokens := estimateTokens(inputText)
		targetTokens := int(float64(origTokens) * tier.TargetRatio)
		if targetTokens < 50 {
			targetTokens = 50
		}

		summary, _, err := l.chain.Summarize(ctx, inputText, start, end, targetTokens)
		if err != nil {
			if ctx.Err() != nil {
				return appendVerbatimTail(result, messages, start, nextIndex)
			}
			slog.Warn("progressive tier compression failed, keeping verbatim",
				slog.String("tier", tier.Name),
				slog.String("error", err.Error()),
			)
			for _, msg := range slice {
				msg.Index = nextIndex
				nextIndex++
				result = append(result, msg)
			}
			continue
		}

		validation := l.validator.Validate(toSummarize, summary, origTokens)
		if ctx.Err() != nil {
			return appendVerbatimTail(result, messages, start, nextIndex)
		}
		if !validation.Valid {
			slog.Warn("progressive tier summary invalid, keeping verbatim",
				slog.String("tier", tier.Name),
				slog.String("reason", validation.FailReason),
			)
			for _, msg := range slice {
				msg.Index = nextIndex
				nextIndex++
				result = append(result, msg)
			}
			continue
		}

		// Insert anchors that appeared in this tier before the summary block.
		for ai, msg := range slice {
			if anchoredSet[ai] {
				msg.Index = nextIndex
				nextIndex++
				result = append(result, msg)
			}
		}

		// Append the summary as a synthetic assistant message.
		compressedTokens := estimateTokens(summary)
		summaryMsg := types.Message{
			Index: nextIndex,
			Role:  "assistant",
			Content: []types.ContentBlock{
				{
					Type: "text",
					Text: fmt.Sprintf(
						"[%s summary (msgs %d-%d): %s]",
						tier.Name, start, end, summary,
					),
				},
			},
			Metadata: types.MessageMetadata{
				OriginalTokens:   origTokens,
				CompressedTokens: compressedTokens,
				CompressionLevel: types.CompressionLayer2,
			},
		}
		nextIndex++
		result = append(result, summaryMsg)

		slog.Info("progressive tier compressed",
			slog.String("tier", tier.Name),
			slog.Int("orig_msgs", len(slice)),
			slog.Int("orig_tokens", origTokens),
			slog.Int("compressed_tokens", compressedTokens),
			slog.String("ratio", ratioStr(tier.TargetRatio)),
		)
	}

	return result
}

func appendVerbatimTail(result []types.Message, messages []types.Message, start, nextIndex int) []types.Message {
	if start < 0 {
		start = 0
	}
	if start >= len(messages) {
		return result
	}
	for _, msg := range messages[start:] {
		msg.Index = nextIndex
		nextIndex++
		result = append(result, msg)
	}
	return result
}

// ratioStr formats a compression ratio as a compact string for logging.
func ratioStr(r float64) string {
	pct := int(r * 100)
	var sb strings.Builder
	sb.WriteString(itoa(pct))
	sb.WriteByte('%')
	return sb.String()
}

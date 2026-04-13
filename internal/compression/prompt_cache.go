package compression

import (
	"github.com/slimference/slimference/internal/types"
)

const (
	maxCacheBreakpoints    = 4
	minStablePrefixTokens  = 1024
	// Rough estimate: 4 characters per token (conservative).
	charsPerToken = 4
)

// OptimizeCacheBreakpoints injects Anthropic prompt cache breakpoints into messages.
// It places cache_control: {type: "ephemeral"} on the last content block of the last
// stable message (index < stableBoundary). A maximum of maxCacheBreakpoints are injected
// across the full message list, and only when the stable prefix is >= 1024 estimated tokens.
//
// messages is modified by value - the caller receives a new slice with the injected markers.
func OptimizeCacheBreakpoints(messages []types.Message, stableBoundary int) []types.Message {
	if len(messages) == 0 || stableBoundary <= 0 {
		return messages
	}

	// Count estimated tokens in the stable prefix.
	stableChars := 0
	for i := 0; i < stableBoundary && i < len(messages); i++ {
		for _, block := range messages[i].Content {
			stableChars += len(block.Text) + len(block.ToolInput)
		}
	}

	if stableChars/charsPerToken < minStablePrefixTokens {
		return messages
	}

	// Work on a shallow copy of the slice so we do not mutate the caller's slice header.
	result := make([]types.Message, len(messages))
	copy(result, messages)

	// Find candidate injection points: last content block of each message in the stable
	// prefix. We inject from the end backwards, up to maxCacheBreakpoints.
	type candidate struct {
		msgIdx   int
		blockIdx int
	}

	var candidates []candidate
	for i := stableBoundary - 1; i >= 0 && i < len(result); i-- {
		if len(result[i].Content) == 0 {
			continue
		}
		candidates = append(candidates, candidate{msgIdx: i, blockIdx: len(result[i].Content) - 1})
		if len(candidates) >= maxCacheBreakpoints {
			break
		}
	}

	if len(candidates) == 0 {
		return result
	}

	// Apply breakpoints. Each message that needs modification gets its content slice deep-copied
	// so we do not mutate the original message's content array.
	ephemeral := &types.CacheControl{Type: "ephemeral"}

	for _, c := range candidates {
		msg := result[c.msgIdx]
		newContent := make([]types.ContentBlock, len(msg.Content))
		copy(newContent, msg.Content)
		newContent[c.blockIdx].CacheControl = ephemeral
		msg.Content = newContent
		result[c.msgIdx] = msg
	}

	return result
}

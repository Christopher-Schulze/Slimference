package compression

import (
	"sync/atomic"

	"github.com/slimference/slimference/internal/types"
)

const (
	maxCacheBreakpoints = 4
	minStablePrefixTokens = 1024
	// Rough estimate: 4 characters per token (conservative).
	charsPerToken = 4
)

// promptCacheBreakpointsInjected counts total breakpoints injected across all
// Compress calls. Exposed via PromptCacheBreakpointsInjected() for the admin
// surface. Reset via ResetPromptCacheBreakpointsCounter() in tests.
var promptCacheBreakpointsInjected atomic.Int64

// PromptCacheBreakpointsInjected returns the cumulative count of cache
// breakpoints injected since process start.
func PromptCacheBreakpointsInjected() int64 {
	return promptCacheBreakpointsInjected.Load()
}

// ResetPromptCacheBreakpointsCounter zeroes the counter. Test-only helper.
func ResetPromptCacheBreakpointsCounter() {
	promptCacheBreakpointsInjected.Store(0)
}

// OptimizeCacheBreakpoints injects Anthropic prompt cache breakpoints into
// messages. It places `cache_control: {type: "ephemeral"}` on the last content
// block of up to maxCacheBreakpoints messages inside the stable prefix
// (index < stableBoundary). Breakpoints are **spread evenly** across the
// stable prefix (T45) rather than clustered at the tail, which produces
// overlapping cache layers at multiple depths: a small edit near the tail
// still hits the earlier layers, and a large prefix change only invalidates
// the layers it spans.
//
// Only runs when the stable prefix is >= minStablePrefixTokens estimated
// tokens - below that the caching overhead outweighs the win.
//
// messages is never mutated; a shallow slice copy + per-touched-message
// deep content copy is returned.
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

	// Collect indices of messages with content within the stable prefix.
	// Only these can carry a breakpoint.
	eligible := make([]int, 0, stableBoundary)
	for i := 0; i < stableBoundary && i < len(messages); i++ {
		if len(messages[i].Content) > 0 {
			eligible = append(eligible, i)
		}
	}
	if len(eligible) == 0 {
		return messages
	}

	// T45: spread-evenly placement.
	// If we have up to maxCacheBreakpoints eligible messages, mark them all.
	// Otherwise, pick maxCacheBreakpoints positions evenly across eligible:
	// segment k (1..N) ends at eligible[floor(len*k/N) - 1].
	pickCount := maxCacheBreakpoints
	if pickCount > len(eligible) {
		pickCount = len(eligible)
	}
	selected := make(map[int]struct{}, pickCount)
	for k := 1; k <= pickCount; k++ {
		idx := (len(eligible)*k)/pickCount - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(eligible) {
			idx = len(eligible) - 1
		}
		selected[eligible[idx]] = struct{}{}
	}

	// Work on a shallow copy of the slice so we do not mutate the caller's
	// slice header.
	result := make([]types.Message, len(messages))
	copy(result, messages)

	ephemeral := &types.CacheControl{Type: "ephemeral"}
	injected := 0
	for msgIdx := range selected {
		msg := result[msgIdx]
		newContent := make([]types.ContentBlock, len(msg.Content))
		copy(newContent, msg.Content)
		lastIdx := len(newContent) - 1
		newContent[lastIdx].CacheControl = ephemeral
		msg.Content = newContent
		result[msgIdx] = msg
		injected++
	}
	if injected > 0 {
		promptCacheBreakpointsInjected.Add(int64(injected))
	}

	return result
}

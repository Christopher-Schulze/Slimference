package compression

import (
	"sort"
	"sync/atomic"

	"github.com/slimference/slimference/internal/types"
)

const (
	maxCacheBreakpoints   = 4
	minStablePrefixTokens = 1024
	// Rough estimate: 4 characters per token (conservative).
	charsPerToken = 4
	// toolResultCacheThreshold marks large tool-result blocks as high-value
	// cache boundaries. Anthropic charges cache reads far below fresh input,
	// so large stable tool output should outrank tiny conversational turns.
	toolResultCacheThreshold = 5 * 1024
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
// (index < stableBoundary). Breakpoints are selected by expected cache value:
// large tool results first, then late stable assistant/user turns, with
// deterministic tie-breaking. This keeps T45's "multiple depth" intent while
// avoiding uniform placement on low-value tiny messages.
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

	selected := selectCacheBreakpointIndices(messages, stableBoundary)

	// Work on a shallow copy of the slice so we do not mutate the caller's
	// slice header.
	result := make([]types.Message, len(messages))
	copy(result, messages)

	ephemeral := &types.CacheControl{Type: "ephemeral"}
	injected := 0
	for _, msgIdx := range selected {
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

type cacheBreakpointCandidate struct {
	index int
	score int
}

func selectCacheBreakpointIndices(messages []types.Message, stableBoundary int) []int {
	candidates := make([]cacheBreakpointCandidate, 0, stableBoundary)
	for i := 0; i < stableBoundary && i < len(messages); i++ {
		if len(messages[i].Content) == 0 {
			continue
		}
		candidates = append(candidates, cacheBreakpointCandidate{
			index: i,
			score: cacheBreakpointScore(messages[i], i, stableBoundary),
		})
	}
	if len(candidates) == 0 {
		return nil
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].index > candidates[j].index
	})

	pickCount := maxCacheBreakpoints
	if pickCount > len(candidates) {
		pickCount = len(candidates)
	}
	selected := make([]int, pickCount)
	for i := 0; i < pickCount; i++ {
		selected[i] = candidates[i].index
	}
	sort.Ints(selected)
	return selected
}

func cacheBreakpointScore(message types.Message, index, stableBoundary int) int {
	score := 0
	if stableBoundary > 1 {
		score += (index * 100) / (stableBoundary - 1)
	}
	switch message.Role {
	case "assistant":
		score += 90
	case "user":
		score += 70
	case "tool":
		score += 80
	case "system":
		score += 40
	}
	for _, block := range message.Content {
		size := len(block.Text) + len(block.ToolInput)
		if block.Type == "tool_result" && size >= toolResultCacheThreshold {
			score += 1000 + size/1024
			continue
		}
		if size > 1024 {
			score += size / 2048
		}
	}
	return score
}

package compression

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// makeStableMessages builds N messages with enough bytes to pass the
// minStablePrefixTokens gate (each message ~1.5kB of text).
func makeStableMessages(n int) []types.Message {
	msgs := make([]types.Message, n)
	body := strings.Repeat("word ", 400) // ~2000 chars/4 = ~500 tokens per msg
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = types.Message{
			Index:   i,
			Role:    role,
			Content: []types.ContentBlock{{Type: "text", Text: body}},
		}
	}
	return msgs
}

func cacheControlCount(msgs []types.Message) int {
	n := 0
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.CacheControl != nil && b.CacheControl.Type == "ephemeral" {
				n++
			}
		}
	}
	return n
}

func hotBreakpointIndices(msgs []types.Message) []int {
	var idx []int
	for i, m := range msgs {
		for _, b := range m.Content {
			if b.CacheControl != nil && b.CacheControl.Type == "ephemeral" {
				idx = append(idx, i)
				break
			}
		}
	}
	return idx
}

func TestOptimizeCacheBreakpoints_HintHotPushesLater(t *testing.T) {
	msgs := makeStableMessages(8)
	stableBoundary := 8

	cold := OptimizeCacheBreakpointsHint(msgs, stableBoundary, PromptCacheHintCold)
	hot := OptimizeCacheBreakpointsHint(msgs, stableBoundary, PromptCacheHintHot)

	coldIdx := hotBreakpointIndices(cold)
	hotIdx := hotBreakpointIndices(hot)
	if len(hotIdx) == 0 {
		t.Fatalf("hot path should emit breakpoints, got none")
	}

	// Average position of breakpoints under hot must be later (higher
	// index) than under cold.
	sum := func(s []int) int {
		x := 0
		for _, v := range s {
			x += v
		}
		return x
	}
	avgCold := float64(sum(coldIdx)) / float64(len(coldIdx))
	avgHot := float64(sum(hotIdx)) / float64(len(hotIdx))
	if avgHot <= avgCold {
		t.Fatalf("expected hot breakpoints later than cold: cold=%v hot=%v", coldIdx, hotIdx)
	}
}

func TestOptimizeCacheBreakpoints_HintHotPicksMaxCount(t *testing.T) {
	msgs := makeStableMessages(10)
	out := OptimizeCacheBreakpointsHint(msgs, 10, PromptCacheHintHot)
	got := cacheControlCount(out)
	if got != maxCacheBreakpoints {
		t.Fatalf("expected %d breakpoints, got %d", maxCacheBreakpoints, got)
	}
}

func TestOptimizeCacheBreakpoints_HintHotRespectsBoundary(t *testing.T) {
	// stableBoundary < len(messages): we must not place breakpoints
	// in messages beyond the boundary.
	msgs := makeStableMessages(10)
	out := OptimizeCacheBreakpointsHint(msgs, 6, PromptCacheHintHot)
	for i, m := range out {
		if i >= 6 {
			for _, b := range m.Content {
				if b.CacheControl != nil {
					t.Fatalf("breakpoint leaked past stableBoundary at index %d", i)
				}
			}
		}
	}
}

func TestOptimizeCacheBreakpoints_HintHotSkipsEmptyContent(t *testing.T) {
	msgs := makeStableMessages(6)
	// Wipe content on a middle message so the late-picker must skip it.
	msgs[4].Content = nil
	out := OptimizeCacheBreakpointsHint(msgs, 6, PromptCacheHintHot)
	for _, b := range out[4].Content {
		if b.CacheControl != nil {
			t.Fatalf("empty-content message must not receive a breakpoint")
		}
	}
}

func TestOptimizeCacheBreakpoints_BackwardsCompatibleAlias(t *testing.T) {
	msgs := makeStableMessages(8)
	defaultRun := OptimizeCacheBreakpoints(msgs, 8)
	coldRun := OptimizeCacheBreakpointsHint(msgs, 8, PromptCacheHintCold)
	// Same breakpoint placement under default (cold) and explicit cold.
	if !equalIntSlices(hotBreakpointIndices(defaultRun), hotBreakpointIndices(coldRun)) {
		t.Fatalf("default alias must equal cold-hint behaviour\n default=%v\n cold=%v",
			hotBreakpointIndices(defaultRun), hotBreakpointIndices(coldRun))
	}
}

func TestOptimizeCacheBreakpoints_HintHotShortPrefixSkipsGate(t *testing.T) {
	// Below the minStablePrefixTokens floor → no breakpoints even
	// under hot hint.
	msgs := []types.Message{{
		Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "tiny"}},
	}}
	out := OptimizeCacheBreakpointsHint(msgs, 1, PromptCacheHintHot)
	if cacheControlCount(out) != 0 {
		t.Fatalf("tiny prefix must skip the gate even when hot")
	}
}

func equalIntSlices(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

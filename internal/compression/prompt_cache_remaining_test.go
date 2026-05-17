package compression

import (
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/types"
)

func TestOptimizeCacheBreakpointsHotStableBoundaryClamped(t *testing.T) {
	ResetPromptCacheBreakpointsCounter()
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("a", 5000)}}},
		{Role: "assistant", Content: nil},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("b", 5000)}}},
	}
	got := OptimizeCacheBreakpointsHint(msgs, 99, PromptCacheHintHot)
	if got[0].Content[0].CacheControl == nil || got[2].Content[0].CacheControl == nil {
		t.Fatalf("hot breakpoint clamp did not tag non-empty stable messages: %+v", got)
	}
	if PromptCacheBreakpointsInjected() != 2 {
		t.Fatalf("injected=%d want 2", PromptCacheBreakpointsInjected())
	}
}

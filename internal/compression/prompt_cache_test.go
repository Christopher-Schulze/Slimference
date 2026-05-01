package compression

import (
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/types"
)

func TestOptimizeCacheBreakpoints_noOp(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "hello"}}},
	}
	if out := OptimizeCacheBreakpoints(nil, 1); out != nil {
		t.Fatalf("nil messages")
	}
	if out := OptimizeCacheBreakpoints(msgs, 0); len(out) != 1 {
		t.Fatal()
	}
	if out := OptimizeCacheBreakpoints(msgs, 1); out[0].Content[0].CacheControl != nil {
		t.Fatal("short prefix should not inject breakpoints")
	}
}

func TestOptimizeCacheBreakpoints_injectsEphemeral(t *testing.T) {
	t.Parallel()
	// >= minStablePrefixTokens estimated tokens in stable prefix (chars/4 >= 1024).
	big := strings.Repeat("x", 2500)
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: big}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: big}}},
	}
	out := OptimizeCacheBreakpoints(msgs, 2)
	if len(out) != 2 {
		t.Fatal(len(out))
	}
	found := false
	for _, m := range out {
		for _, b := range m.Content {
			if b.CacheControl != nil && b.CacheControl.Type == "ephemeral" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected ephemeral cache_control on a block")
	}
}

func TestSelectCacheBreakpointIndices_NoCandidates(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{{Role: "user"}}
	if got := selectCacheBreakpointIndices(msgs, 1); got != nil {
		t.Fatalf("expected no candidates, got %v", got)
	}
}

func TestSelectCacheBreakpointIndices_TieBreaksByLaterIndex(t *testing.T) {
	t.Parallel()
	msgs := make([]types.Message, 200)
	for i := range msgs {
		msgs[i] = types.Message{
			Role:    "user",
			Content: []types.ContentBlock{{Type: "text", Text: "x"}},
		}
	}
	got := selectCacheBreakpointIndices(msgs, len(msgs))
	if len(got) != maxCacheBreakpoints {
		t.Fatalf("selected %d breakpoints, want %d: %v", len(got), maxCacheBreakpoints, got)
	}
	for _, idx := range got {
		if idx < 196 {
			t.Fatalf("tie-break should prefer latest candidates, got %v", got)
		}
	}
}

func TestCacheBreakpointScore_SystemRole(t *testing.T) {
	t.Parallel()
	msg := types.Message{Role: "system", Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("x", 2048)}}}
	if got := cacheBreakpointScore(msg, 0, 1); got <= 40 {
		t.Fatalf("expected system role plus size score, got %d", got)
	}
}

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

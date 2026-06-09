package compression

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func TestCompress_SkipsCacheControlledBlocks(t *testing.T) {
	t.Parallel()

	cfg := defaultTestCfg(1)
	c := testCompressorWithArchive(cfg)
	largeToolOutput := strings.Repeat("stable prompt-cache protected output line\n", 200)
	msgs := []types.Message{
		buildMessage(t, 0, "user", textBlock("seed")),
		buildMessage(t, 1, "tool", types.ContentBlock{
			Type:         "tool_result",
			Text:         largeToolOutput,
			CacheControl: &types.CacheControl{Type: "ephemeral"},
		}),
		buildMessage(t, 2, "tool", types.ContentBlock{
			Type:         "tool_result",
			Text:         largeToolOutput,
			CacheControl: &types.CacheControl{Type: "ephemeral"},
		}),
		buildMessage(t, 3, "user", textBlock("latest")),
	}

	result := c.Compress(msgs)
	if result.TokensSaved != 0 {
		t.Fatalf("cache-controlled blocks must not contribute Layer-1 savings, got %d", result.TokensSaved)
	}
	for _, idx := range []int{1, 2} {
		got := result.Messages[idx].Content[0]
		if got.Text != largeToolOutput {
			t.Fatalf("message %d cache-controlled text mutated", idx)
		}
		if got.ArchiveID != "" {
			t.Fatalf("message %d cache-controlled block should not be archived/mutated, archive=%q", idx, got.ArchiveID)
		}
		if got.CacheControl == nil || got.CacheControl.Type != "ephemeral" {
			t.Fatalf("message %d cache_control lost: %+v", idx, got.CacheControl)
		}
	}
}

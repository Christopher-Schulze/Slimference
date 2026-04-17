package summarization

import (
	"context"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

func layer2SkipMessages() []types.Message {
	return []types.Message{
		{
			Index: 0,
			Role:  "user",
			Content: []types.ContentBlock{
				{Type: "text", Text: strings.Repeat("context ", 200)},
			},
		},
		{
			Index: 1,
			Role:  "assistant",
			Content: []types.ContentBlock{
				{Type: "text", Text: strings.Repeat("reply ", 200)},
			},
		},
	}
}

func TestLayer2_ShouldTriggerCompression_skipsWithoutConfiguredProvider(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "")

	cfg := config.Defaults()
	cfg.Compression.MinMessagesForCompression = 1
	cfg.Compression.MinTokensForLayer2 = 0

	layer := NewLayer2(&cfg.Compression)
	if layer.ShouldTriggerCompression(layer2SkipMessages()) {
		t.Fatal("ShouldTriggerCompression must stay false when no summarizer is configured")
	}
}

func TestLayer2_RunCompressionJobContext_skipsWithoutConfiguredProvider(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "")

	cfg := config.Defaults()
	cfg.Compression.MinMessagesForCompression = 1
	cfg.Compression.MinTokensForLayer2 = 0

	layer := NewLayer2(&cfg.Compression)
	layer.RunCompressionJobContext(context.Background(), layer2SkipMessages())

	if got, _ := layer.GetCache().GetCurrent(); got != nil {
		t.Fatal("summary cache should remain empty when no summarizer is configured")
	}
}

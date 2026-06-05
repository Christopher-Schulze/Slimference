package summarization

import (
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/types"
)

func BenchmarkRedactorOffModeLargeHistory(b *testing.B) {
	r := NewRedactor(RedactOptions{Mode: RedactionModeOff})
	messages := make([]types.Message, 80)
	text := strings.Repeat("unchanged tool output with paths and tokens ", 64)
	for i := range messages {
		messages[i] = types.Message{
			Index: i,
			Role:  "user",
			Content: []types.ContentBlock{
				{Type: "text", Text: text},
				{Type: "tool_result", Text: text},
			},
		}
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		out, stats := r.Redact(messages)
		if len(out) != len(messages) || stats != (RedactStats{}) {
			b.Fatalf("unexpected off-mode result: len=%d stats=%+v", len(out), stats)
		}
	}
}

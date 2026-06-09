package compression

import (
	"strconv"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func largeJSONPayload() string {
	var sb strings.Builder
	sb.WriteString("{")
	for i := 0; i < 40; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`"k`)
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString(`":"`)
		sb.WriteString(strings.Repeat("v", 200))
		sb.WriteString(`"`)
	}
	sb.WriteString("}")
	return sb.String()
}

func TestStructurePreviewPass_disabledByDefault(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	c := NewDeterministicCompressor(&cfg)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "please analyse"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", ToolName: "Bash", Text: largeJSONPayload()}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "now summarise"}}},
	}
	result := c.Compress(msgs)
	if result.PreviewSaved != 0 {
		t.Fatalf("disabled preview must not save: %d", result.PreviewSaved)
	}
}

func TestStructurePreviewPass_fires(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.SlidingWindow = 1
	cfg.Tuning.StructurePreview = true
	c := testCompressorWithArchive(&cfg)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "analyse"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", ToolName: "Bash", Text: largeJSONPayload()}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "tail"}}},
	}
	result := c.Compress(msgs)
	if result.PreviewSaved == 0 {
		t.Fatal("expected preview savings")
	}
	if !strings.Contains(result.Messages[1].Content[0].Text, "JSON object") {
		t.Fatalf("preview not applied: %s", result.Messages[1].Content[0].Text)
	}
}

func TestStructurePreviewPass_skipsUserRoles(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.Tuning.StructurePreview = true
	cfg.SlidingWindow = 1
	c := NewDeterministicCompressor(&cfg)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: largeJSONPayload()}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "ok"}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "tail"}}},
	}
	result := c.Compress(msgs)
	// User turn must be preserved verbatim.
	if strings.Contains(result.Messages[0].Content[0].Text, "JSON object") {
		t.Fatal("user role must not be previewed")
	}
	_ = result.PreviewSaved
}

func TestShouldPreviewMessage(t *testing.T) {
	t.Parallel()
	if shouldPreviewMessage(types.Message{Role: "user"}) {
		t.Fatal("user must not be eligible")
	}
	if shouldPreviewMessage(types.Message{Role: "assistant", Metadata: types.MessageMetadata{WasDeduped: true}}) {
		t.Fatal("deduped must not be eligible")
	}
	if !shouldPreviewMessage(types.Message{Role: "assistant"}) {
		t.Fatal("assistant must be eligible")
	}
}

func TestShouldPreviewBlock(t *testing.T) {
	t.Parallel()
	if shouldPreviewBlock(types.ContentBlock{Type: "text", Text: strings.Repeat("x", 10000)}) {
		t.Fatal("non-tool_result must be ineligible")
	}
	if shouldPreviewBlock(types.ContentBlock{Type: "tool_result", Text: ""}) {
		t.Fatal("empty text must be ineligible")
	}
	if shouldPreviewBlock(types.ContentBlock{Type: "tool_result", Text: "short"}) {
		t.Fatal("too small must be ineligible")
	}
	if shouldPreviewBlock(types.ContentBlock{Type: "tool_result", Text: "[build] ok\n" + strings.Repeat("x", 10000)}) {
		t.Fatal("pre-filtered must be ineligible")
	}
	if !shouldPreviewBlock(types.ContentBlock{Type: "tool_result", Text: largeJSONPayload()}) {
		t.Fatal("large tool_result must be eligible")
	}
}

func TestCompressApplyLoopNudge_viaTuning(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.Tuning.LoopDetection = true
	c := NewDeterministicCompressor(&cfg)
	text := "run the proxy benchmark and report per-layer savings"
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: text}}},
		{Index: 1, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: text}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: text + " now"}}},
		{Index: 3, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: text + " please"}}},
	}
	result := c.Compress(msgs)
	if result.LoopNudgeSaved == 0 {
		t.Fatal("expected loop nudge savings")
	}
	last := result.Messages[len(result.Messages)-1]
	if !strings.Contains(last.Content[0].Text, LoopNudgeMarker) {
		t.Fatal("nudge missing from last user")
	}
}

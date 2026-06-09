package compression

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/contentarchive"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

type toolOutputWindowRecorder struct {
	seen contentarchive.Input
}

func (r *toolOutputWindowRecorder) Record(input contentarchive.Input) (string, error) {
	r.seen = input
	return "archive-tool-output", nil
}

func TestToolOutputInWindowPass_CompactsLargeCurrentSearchOutput(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.SlidingWindow = 5
	cfg.Tuning.ToolOutputInWindow = true
	cfg.Tuning.ToolOutputInWindowMinTokens = 100
	c := NewDeterministicCompressor(&cfg).WithRecorder(&toolOutputWindowRecorder{})

	var output strings.Builder
	for i := 0; i < 300; i++ {
		output.WriteString(fmt.Sprintf("internal/proxy/file_%03d.go:%d:func example%d() {}\n", i, i+1, i))
	}
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "search symbols"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-rg", ToolName: "shell", ToolInput: `{"command":"rg -n \"func\" internal/proxy"}`}}},
		{Index: 2, Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-rg", Text: output.String()}}},
		{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "read results"}}},
		{Index: 4, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "final short"}}},
	}

	result := c.Compress(msgs)
	if result.ToolCompressorSaved <= 0 {
		t.Fatalf("ToolCompressorSaved=%d want > 0", result.ToolCompressorSaved)
	}
	got := result.Messages[2].Content[0].Text
	if !strings.Contains(got, "more matches omitted") || strings.Contains(got, "file_299.go") {
		t.Fatalf("in-window search output was not compacted correctly: %q", got)
	}
}

func TestToolOutputInWindowPass_DisabledAndSafetyBranches(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.SlidingWindow = 5
	cfg.Tuning.ToolOutputInWindow = false
	cfg.Tuning.ToolOutputInWindowMinTokens = 1
	c := NewDeterministicCompressor(&cfg)

	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "x"}}},
		{Index: 1, Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolName: "Read", Text: strings.Repeat("package main\n", 400)}}},
		{Index: 2, Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolName: "shell", ToolInput: `{"command":"git diff"}`, Text: "diff --git a/a b/a\n--- a/a\n+++ b/a\n" + strings.Repeat("+x\n", 300)}}},
		{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "done"}}},
		{Index: 4, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "tail"}}},
	}
	result := c.Compress(msgs)
	if result.ToolCompressorSaved != 0 {
		t.Fatalf("disabled in-window tool compression saved %d", result.ToolCompressorSaved)
	}
	if !shouldToolOutputInWindowMessage(types.Message{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", Text: "x"}}}) {
		t.Fatal("assistant tool message should be eligible")
	}
	if shouldToolOutputInWindowMessage(types.Message{Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: "x"}}}) {
		t.Fatal("user messages must not be eligible")
	}
	if shouldToolOutputInWindowBlock(types.ContentBlock{Type: "tool_result", Text: "[search] 2 results\n"}, 1) {
		t.Fatal("pre-filtered blocks must not be eligible")
	}
	if toolOutputInWindowTypeAllowed(types.ToolTypeFileRead) {
		t.Fatal("file reads must not be compacted by tool-output in-window pass")
	}
}

func TestToolOutputInWindowPass_MinTokenFallbackAndNoShrink(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.SlidingWindow = 5
	cfg.Tuning.ToolOutputInWindow = true
	cfg.Tuning.ToolOutputInWindowMinTokens = 0
	c := NewDeterministicCompressor(&cfg)

	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "build"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-build", ToolName: "shell", ToolInput: `{"command":"tsc"}`}}},
		{Index: 2, Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-build", Text: strings.Repeat("ok build line\n", 1000)}}},
		{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "done"}}},
		{Index: 4, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "tail"}}},
	}

	result := c.Compress(msgs)
	if result.ToolCompressorSaved != 0 {
		t.Fatalf("non-shrinking build output should be skipped, saved=%d", result.ToolCompressorSaved)
	}
	if got := result.Messages[2].Content[0].Text; got != msgs[2].Content[0].Text {
		t.Fatal("non-shrinking output should remain byte-identical")
	}
}

func TestToolOutputInWindowPass_ArchivesOriginalWhenRecorderPresent(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.SlidingWindow = 5
	cfg.Tuning.ToolOutputInWindow = true
	cfg.Tuning.ToolOutputInWindowMinTokens = 100
	recorder := &toolOutputWindowRecorder{}
	c := NewDeterministicCompressor(&cfg).WithRecorder(recorder)

	var output strings.Builder
	for i := 0; i < 300; i++ {
		output.WriteString(fmt.Sprintf("internal/filter/file_%03d.go:%d:needle\n", i, i+1))
	}
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "search"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-search", ToolName: "shell", ToolInput: `{"command":"rg needle internal/filter"}`}}},
		{Index: 2, Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-search", Text: output.String()}}},
		{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "done"}}},
		{Index: 4, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "tail"}}},
	}

	result := c.CompressWithSession("session-tool-output", msgs)
	block := result.Messages[2].Content[0]
	if block.ArchiveID != "archive-tool-output" {
		t.Fatalf("archive id=%q", block.ArchiveID)
	}
	if recorder.seen.SessionID != "session-tool-output" || recorder.seen.SubLayer != "tool_output_in_window" {
		t.Fatalf("archive input=%#v", recorder.seen)
	}
}

func TestClassifyJavaScriptPackageCommand_requiresScriptName(t *testing.T) {
	t.Parallel()
	if got := classifyJavaScriptPackageCommand([]string{"npm"}); got != types.ToolTypeUnknown {
		t.Fatalf("npm without subcommand classified as %v", got)
	}
}

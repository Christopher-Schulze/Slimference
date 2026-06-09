package proxy

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/chunkdedup"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// TestReduceCodexLayer0NeverElidesFirstRead proves the product invariant:
// Codex first-read file contents are not replaced by scan/signature output in
// any policy mode. Savings must come from repeat/cache/delta mechanisms, not
// from giving the model less file content on first sight.
func TestReduceCodexLayer0NeverElidesFirstRead(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := chunkdedup.NewStoreWithLimits(chunkdedup.Config{MinSize: 1024, AvgSize: 2048, MaxSize: 4096}, chunkdedup.StoreLimits{}, func(_, id string, chunk []byte) string {
		if len(chunk) == 0 || id == "" {
			return ""
		}
		return "local-archive://" + id
	})
	var body strings.Builder
	body.WriteString("Process exited with code 0\nOutput:\n")
	body.WriteString("package x\n\n")
	for i := 0; i < 40; i++ {
		body.WriteString(fmt.Sprintf("func F%d(a int) int {\n", i))
		for j := 0; j < 15; j++ {
			body.WriteString(fmt.Sprintf("\ta += %d\n", j))
		}
		body.WriteString("\treturn a\n}\n\n")
	}
	mk := func() []types.Message {
		return []types.Message{
			{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-read", ToolName: "exec_command", ToolInput: `{"cmd":"cat /tmp/x.go"}`}}},
			{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-read", Text: body.String()}}},
		}
	}

	for _, mode := range []string{"auto", "max", "conservative"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			res := reduceCodexLayer0(codexLayer0Request{
				Route: codexLayer0RouteWSSPhaseF, Messages: mk(), SessionID: "s-" + mode,
				PolicyMode: mode, ChunkDedupEnabled: true, ChunkDedupMinBytes: 1 << 30,
				ChunkStore: store, ArchiveRecovery: true,
			})
			if res.Stats.BlocksModified != 0 || res.Stats.CapturedOutputBlocks != 0 {
				t.Fatalf("first read must full-pass in %s mode, stats=%+v", mode, res.Stats)
			}
			if got := res.Messages[1].Content[0].Text; got != body.String() {
				t.Fatalf("first read text changed in %s mode", mode)
			}
		})
	}
}

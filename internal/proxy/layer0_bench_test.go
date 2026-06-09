package proxy

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func BenchmarkReduceCodexLayer0_WSSRepeatedGitStatus80Files(b *testing.B) {
	b.Setenv("HOME", b.TempDir())
	var status strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&status, " M internal/pkg/file_%03d.go\n", i)
	}
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{
			Type:      "tool_use",
			ToolUseID: "call-status",
			ToolName:  "exec_command",
			ToolInput: `{"cmd":"git status --short","workdir":"/repo"}`,
		}}},
		{Role: "tool", Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "call-status",
			Text:         status.String(),
		}}},
	}
	req := codexLayer0Request{
		Route:     codexLayer0RouteWSSPhaseF,
		Messages:  messages,
		SessionID: "bench-layer0-status",
	}
	warm := reduceCodexLayer0(req)
	if warm.Stats.TokensSaved <= 0 {
		b.Fatalf("warm git-status reduction should save: %+v", warm.Stats)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		result := reduceCodexLayer0(req)
		if result.Stats.TokensSaved <= 0 {
			b.Fatal("expected git-status savings")
		}
	}
}

func BenchmarkReduceCodexLayer0_WSSRepeatedRead64KB(b *testing.B) {
	b.Setenv("HOME", b.TempDir())
	body := "package proxy\n" + strings.Repeat("func benchmarkRepeatedRead() {}\n", 2400)
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{
			Type:      "tool_use",
			ToolUseID: "call-read",
			ToolName:  "exec_command",
			ToolInput: `{"cmd":"sed -n '1,2400p' internal/proxy/layer0_proxy.go","workdir":"/repo"}`,
		}}},
		{Role: "tool", Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "call-read",
			Text:         body,
		}}},
	}
	req := codexLayer0Request{
		Route:     codexLayer0RouteWSSPhaseF,
		Messages:  messages,
		SessionID: "bench-layer0-read",
	}
	seed := reduceCodexLayer0(req)
	if seed.Stats.TokensSaved != 0 {
		b.Fatalf("first read should seed without savings: %+v", seed.Stats)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		result := reduceCodexLayer0(req)
		if result.Stats.TokensSaved <= 0 {
			b.Fatal("expected repeated-read savings")
		}
	}
}

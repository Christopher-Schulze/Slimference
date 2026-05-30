package proxy

import (
	"fmt"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/chunkdedup"
	"github.com/slimference/slimference/internal/filter"
	"github.com/slimference/slimference/internal/tokens"
	"github.com/slimference/slimference/internal/types"
)

// TestRecordScanModeShadow_GatedTelemetryOnly proves the scan-mode read shadow is
// env-gated, measures a positive would-save on a large Go read, and only returns
// a number (it never mutates the read; the caller full-passes it).
func TestRecordScanModeShadow_GatedTelemetryOnly(t *testing.T) {
	tok := tokens.ForProvider(types.CodexChatGPT)
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
	text := body.String()
	cmd := "cat /tmp/x.go"
	ctx := filter.FileReadContext{Mode: "scan"}
	before := tok.CountString(text)

	t.Setenv(scanShadowEnv, "")
	if got := recordScanModeShadow(cmd, text, ctx, before, tok); got != 0 {
		t.Fatalf("env unset must be a no-op, got %d", got)
	}
	t.Setenv(scanShadowEnv, "1")
	if got := recordScanModeShadow(cmd, text, ctx, before, tok); got <= 0 {
		t.Fatalf("scan shadow should measure positive would-save on a large Go read, got %d", got)
	}
}

// TestReduceCodexLayer0ScanReadApplyGatedAndRecoverable proves scan-apply is
// default-off (read full-passes) and, when enabled, compacts the read while
// triple-covering recovery: discoverable note, archive reference, and read-key
// registration so a re-read full-passes.
func TestReduceCodexLayer0ScanReadApplyGatedAndRecoverable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
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

	t.Setenv(scanApplyEnv, "")
	off := reduceCodexLayer0(codexLayer0Request{Route: codexLayer0RouteWSSPhaseF, Messages: mk(), SessionID: "s1"})
	if off.Stats.BlocksModified != 0 {
		t.Fatalf("scan-apply default-off must full-pass the read, stats=%+v", off.Stats)
	}

	t.Setenv(scanApplyEnv, "1")
	on := reduceCodexLayer0(codexLayer0Request{Route: codexLayer0RouteWSSPhaseF, Messages: mk(), SessionID: "s2"})
	text := on.Messages[1].Content[0].Text
	if on.Stats.TokensSaved <= 0 || on.Stats.BlocksModified != 1 {
		t.Fatalf("scan-apply enabled must compact the read, stats=%+v", on.Stats)
	}
	if !strings.Contains(text, "re-run the read to see the full file") {
		t.Fatalf("scan output must carry the recovery note: %q", text[:min(len(text), 200)])
	}
	if !strings.Contains(text, "context-archive kind=tool-output uri=local-archive://") {
		t.Fatalf("scan output must carry an archive reference: %q", text[:min(len(text), 200)])
	}
	if len(on.Stats.ReadDeltaKeys) == 0 {
		t.Fatalf("scan-apply must register the read key for re-read recovery, stats=%+v", on.Stats)
	}
}

// TestReduceCodexLayer0ScanReadViaMaxPolicy proves scan-mode is driven by the
// savings policy (no env flag): auto mode never scan-compacts a first read, max
// mode does, and the max path keeps the full triple recovery (note, archive
// reference, read-key registration).
func TestReduceCodexLayer0ScanReadViaMaxPolicy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(scanApplyEnv, "") // env OFF: prove the policy alone drives scan-mode.
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

	autoRes := reduceCodexLayer0(codexLayer0Request{
		Route: codexLayer0RouteWSSPhaseF, Messages: mk(), SessionID: "s-auto",
		PolicyMode: "auto", ChunkDedupEnabled: true, ChunkStore: store, ArchiveRecovery: true,
	})
	if autoRes.Stats.CapturedOutputBlocks != 0 {
		t.Fatalf("auto mode must not scan-compact a first read (not promoted yet), stats=%+v", autoRes.Stats)
	}

	maxRes := reduceCodexLayer0(codexLayer0Request{
		Route: codexLayer0RouteWSSPhaseF, Messages: mk(), SessionID: "s-max",
		PolicyMode: "max", ChunkDedupEnabled: true, ChunkStore: store, ArchiveRecovery: true,
	})
	text := maxRes.Messages[1].Content[0].Text
	if maxRes.Stats.TokensSaved <= 0 || maxRes.Stats.CapturedOutputBlocks != 1 {
		t.Fatalf("max policy must scan-compact the first read, stats=%+v", maxRes.Stats)
	}
	if !strings.Contains(text, "re-run the read to see the full file") ||
		!strings.Contains(text, "context-archive kind=tool-output uri=local-archive://") ||
		len(maxRes.Stats.ReadDeltaKeys) == 0 {
		t.Fatalf("max-policy scan must keep triple recovery: text=%q stats=%+v", text[:min(len(text), 200)], maxRes.Stats)
	}
}

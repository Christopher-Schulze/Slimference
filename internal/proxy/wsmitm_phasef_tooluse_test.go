package proxy

import (
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// TestWSPhaseF_ToolUsePersistenceSurvivesReconnect proves item 15: the per-socket
// tool-use resolution map is persisted and rehydrated, so a re-read after a WSS
// reconnect still resolves its command (and read-delta can fire) instead of going
// to an unresolved command with no savings.
func TestWSPhaseF_ToolUsePersistenceSurvivesReconnect(t *testing.T) {
	home := t.TempDir()
	orig := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { proxyUserHomeDir = orig })

	const sid = "codex-wss:reconnect-1"

	// Adapter 1: a fresh socket learns a tool_use and persists it.
	a1 := &wsPhaseFAdapter{}
	a1.hydrateToolUses(sid)
	a1.mu.Lock()
	a1.toolUses = map[string]types.ContentBlock{
		"call_9": {Type: "tool_use", ToolUseID: "call_9", ToolName: "exec_command", ToolInput: `{"cmd":"cat x.go"}`},
	}
	a1.mu.Unlock()
	a1.persistToolUses()

	// Adapter 2: a reconnect (new socket, empty in-memory map) rehydrates.
	a2 := &wsPhaseFAdapter{}
	a2.hydrateToolUses(sid)
	got := a2.loadToolUses()
	e, ok := got["call_9"]
	if !ok {
		t.Fatalf("reconnect did not rehydrate the tool use: %+v", got)
	}
	if e.ToolName != "exec_command" || e.ToolInput != `{"cmd":"cat x.go"}` {
		t.Fatalf("rehydrated tool use wrong: %+v", e)
	}

	// A different session must not see it.
	a3 := &wsPhaseFAdapter{}
	a3.hydrateToolUses("codex-wss:other")
	if got := a3.loadToolUses(); len(got) != 0 {
		t.Fatalf("different session leaked tool uses: %+v", got)
	}
}

// TestWSPhaseF_ToolUseInferenceFallbackForFirstDelta proves that when
// ResponseOutputItemDone is not emitted for function_call items, the
// inference fallback (proxyInferCommandLineFromToolResult) still allows
// toolOutputKnown to become true, enabling delta stateless recovery.
func TestWSPhaseF_ToolUseInferenceFallbackForFirstDelta(t *testing.T) {
	// Simulate a tool_result with git status output wrapped in codex exec envelope
	gitStatusOutput := "Process exited with code 0\nOutput:\n M file1.go\n M file2.go\n?? new_file.go\n"
	messages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{{
			Type:         "tool_result",
			Text:         gitStatusOutput,
			ToolResultID: "call_1",
		}},
	}}

	// No tool_use metadata in the map (simulates missing ResponseOutputItemDone)
	toolUses := map[string]types.ContentBlock{}

	total, resolved, inferred := wssToolOutputResolutionStatsWithToolUses(messages, toolUses)
	if total != 1 {
		t.Fatalf("expected total=1, got %d", total)
	}
	if resolved != 0 {
		t.Fatalf("expected resolved=0 (no metadata), got %d", resolved)
	}
	if inferred != 1 {
		t.Fatalf("expected inferred=1 (git status pattern matched), got %d", inferred)
	}

	toolOutputKnown := total > 0 && resolved+inferred == total
	if !toolOutputKnown {
		t.Fatalf("expected toolOutputKnown=true via inference fallback, got false")
	}
}

// TestWSPhaseF_ToolUseInferenceFallbackForSearchOutput proves that search
// output (rg) is inferred correctly, enabling delta stateless recovery
// even when tool_use metadata is missing.
func TestWSPhaseF_ToolUseInferenceFallbackForSearchOutput(t *testing.T) {
	// Simulate rg search output wrapped in codex exec envelope
	searchOutput := "Process exited with code 0\nOutput:\ninternal/filter/builtin_system.go:16:func TryCompactHistory\ninternal/filter/builtin_system.go:60:func TryCompactDmesg\ninternal/filter/builtin_system.go:103:func TryCompactMount\n"
	messages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{{
			Type:         "tool_result",
			Text:         searchOutput,
			ToolResultID: "call_2",
		}},
	}}

	toolUses := map[string]types.ContentBlock{} // no metadata

	total, resolved, inferred := wssToolOutputResolutionStatsWithToolUses(messages, toolUses)
	if total != 1 {
		t.Fatalf("expected total=1, got %d", total)
	}
	if resolved != 0 {
		t.Fatalf("expected resolved=0, got %d", resolved)
	}
	if inferred != 1 {
		t.Fatalf("expected inferred=1 (path list pattern matched), got %d", inferred)
	}

	toolOutputKnown := total > 0 && resolved+inferred == total
	if !toolOutputKnown {
		t.Fatalf("expected toolOutputKnown=true via path list inference, got false")
	}
}

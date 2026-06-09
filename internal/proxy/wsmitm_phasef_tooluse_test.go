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

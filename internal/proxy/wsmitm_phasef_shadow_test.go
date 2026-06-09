package proxy

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/servermirror"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func shadowMsg(text string) []types.Message {
	return []types.Message{{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", Text: text}}}}
}

func TestRecordShadowMirror_PredictsRepeatAfterObserve(t *testing.T) {
	old := wssShadowMirror
	t.Cleanup(func() { wssShadowMirror = old })
	wssShadowMirror = servermirror.New()

	content := strings.Repeat("repeated tool output line\n", 40)
	frame := shadowMsg(content)

	// First frame: nothing referenceable yet, content recorded.
	if rep := recordShadowMirror("sess-x", frame, frame); rep.ReferenceableBlocks != 0 {
		t.Fatalf("first frame should have nothing referenceable: %+v", rep)
	}
	// Second frame with the same content: now the server already holds it.
	rep := recordShadowMirror("sess-x", frame, frame)
	if rep.ReferenceableBlocks != 1 || rep.PotentialSavedBytes != len(content) {
		t.Fatalf("repeated frame should be referenceable in shadow: %+v", rep)
	}
	// Empty session is a no-op (never panics, never references).
	if rep := recordShadowMirror("", frame, frame); rep.Blocks != 0 || rep.ReferenceableBlocks != 0 {
		t.Fatalf("empty session must be a no-op: %+v", rep)
	}
}

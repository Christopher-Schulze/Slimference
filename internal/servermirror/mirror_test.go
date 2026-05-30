package servermirror

import (
	"fmt"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/types"
)

func msg(texts ...string) []types.Message {
	blocks := make([]types.ContentBlock, len(texts))
	for i, t := range texts {
		blocks[i] = types.ContentBlock{Type: "tool_result", Text: t}
	}
	return []types.Message{{Role: "tool", Content: blocks}}
}

func TestMirror_ObserveThenPredictReferenceable(t *testing.T) {
	t.Parallel()
	m := New()
	content := strings.Repeat("server has this\n", 50)
	m.Observe("s1", msg(content))
	rep := m.Predict("s1", msg(content))
	if rep.Blocks != 1 || rep.ReferenceableBlocks != 1 || rep.PotentialSavedBytes != len(content) {
		t.Fatalf("forwarded content should be referenceable: %+v", rep)
	}
	if !rep.Predictions[0].AlreadyForwarded {
		t.Fatal("block should be AlreadyForwarded")
	}
}

func TestMirror_NovelContentNotReferenceable(t *testing.T) {
	t.Parallel()
	m := New()
	m.Observe("s", msg("alpha content here"))
	rep := m.Predict("s", msg("totally different beta content"))
	if rep.ReferenceableBlocks != 0 || rep.PotentialSavedBytes != 0 {
		t.Fatalf("novel content must not be referenceable: %+v", rep)
	}
	if rep.Predictions[0].AlreadyForwarded {
		t.Fatal("novel block must not be AlreadyForwarded")
	}
}

// TestMirror_NoFalseElisionProperty is the catastrophic-bug guard: content the
// mirror NEVER observed must NEVER be predicted referenceable, even after
// observing many other blocks.
func TestMirror_NoFalseElisionProperty(t *testing.T) {
	t.Parallel()
	m := New()
	for i := 0; i < 200; i++ {
		m.Observe("s", msg(fmt.Sprintf("observed block number %d with filler", i)))
	}
	for i := 0; i < 200; i++ {
		never := fmt.Sprintf("NEVER-FORWARDED unique content %d", i)
		rep := m.Predict("s", msg(never))
		if rep.ReferenceableBlocks != 0 || rep.Predictions[0].AlreadyForwarded {
			t.Fatalf("false elision: un-observed content predicted referenceable: %q", never)
		}
	}
}

func TestMirror_SessionIsolation(t *testing.T) {
	t.Parallel()
	m := New()
	content := "session A private content xyz"
	m.Observe("a", msg(content))
	if rep := m.Predict("b", msg(content)); rep.ReferenceableBlocks != 0 {
		t.Fatalf("session b must not reference session a content: %+v", rep)
	}
	if rep := m.Predict("a", msg(content)); rep.ReferenceableBlocks != 1 {
		t.Fatal("session a should reference its own content")
	}
}

func TestMirror_ResetAndNilNoOps(t *testing.T) {
	t.Parallel()
	var nilM *Mirror
	nilM.Observe("s", msg("x")) // must not panic
	if rep := nilM.Predict("s", msg("x")); rep.ReferenceableBlocks != 0 {
		t.Fatal("nil mirror predicts nothing referenceable")
	}
	nilM.Reset("s")

	m := New()
	c := "reset me content"
	m.Observe("s", msg(c))
	m.Reset("s")
	if rep := m.Predict("s", msg(c)); rep.ReferenceableBlocks != 0 {
		t.Fatal("after reset, content must not be referenceable")
	}
	if rep := m.Predict("", msg(c)); rep.ReferenceableBlocks != 0 {
		t.Fatal("empty session predicts nothing referenceable")
	}
	if rep := m.Predict("s", msg("")); rep.Blocks != 0 {
		t.Fatal("empty text blocks are not counted")
	}
}

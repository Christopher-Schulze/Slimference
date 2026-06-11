package servermirror

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/types"
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

func TestMirror_NormalizedCodexExecPayloadPredictsThroughVolatileHeader(t *testing.T) {
	t.Parallel()
	m := New()
	payload := strings.Repeat("stable command payload\n", 20)
	first := "Chunk ID: first\nWall time: 0.0001 seconds\nProcess exited with code 0\nOriginal token count: 900\nOutput:\n" + payload
	second := "Chunk ID: second\nWall time: 9.9999 seconds\nProcess exited with code 0\nOriginal token count: 901\nOutput:\n" + payload
	m.Observe("s", msg(first))

	rep := m.Predict("s", msg(second))
	if rep.BlockBytes != len(second) || rep.ReferenceableBlocks != 0 || rep.PotentialSavedBytes != 0 {
		t.Fatalf("exact mirror must not match volatile envelopes: %+v", rep)
	}
	if rep.NormalizedSegments != 1 ||
		rep.NormalizedBytes != len(payload) ||
		rep.NormalizedReferenceableSegments != 1 ||
		rep.NormalizedPotentialSavedBytes != len(payload) {
		t.Fatalf("normalized payload should be referenceable in shadow: %+v", rep)
	}
	kind := rep.NormalizedPotentialSavedBytesByKind["codex_exec_payload"]
	if kind.Segments != 1 || kind.Bytes != len(payload) || kind.ReferenceableSegments != 1 || kind.PotentialSavedBytes != len(payload) {
		t.Fatalf("normalized kind accounting wrong: %+v", rep.NormalizedPotentialSavedBytesByKind)
	}
	if got := rep.NormalizedPredictions[0]; got.Kind != "codex_exec_payload" || !got.AlreadyForwarded || got.Bytes != len(payload) {
		t.Fatalf("normalized prediction wrong: %+v", got)
	}
}

func TestMirror_NormalizedNovelPayloadNotReferenceable(t *testing.T) {
	t.Parallel()
	m := New()
	firstPayload := strings.Repeat("first payload\n", 20)
	secondPayload := strings.Repeat("second payload\n", 20)
	first := "Chunk ID: first\nProcess exited with code 0\nOutput:\n" + firstPayload
	second := "Chunk ID: second\nProcess exited with code 0\nOutput:\n" + secondPayload
	m.Observe("s", msg(first))

	rep := m.Predict("s", msg(second))
	if rep.NormalizedReferenceableSegments != 0 || rep.NormalizedPotentialSavedBytes != 0 {
		t.Fatalf("novel normalized payload must not be referenceable: %+v", rep)
	}
}

func TestMirror_NormalizedHelpersCoverFallbacksAndMalformedEnvelopes(t *testing.T) {
	t.Parallel()

	if _, _, ok := splitCodexExecEnvelope("plain output without exit marker"); ok {
		t.Fatal("plain output must not parse as a Codex exec envelope")
	}
	if _, _, ok := splitCodexExecEnvelope("Process exited with code 0\nno output marker"); ok {
		t.Fatal("exec envelope without output marker must not parse")
	}
	if _, _, ok := splitCodexExecEnvelope("Process exited with code 0\nOutput:\n"); ok {
		t.Fatal("empty exec payload must not parse as referenceable")
	}
	_, payload, ok := splitCodexExecEnvelope("Process exited with code 0\r\nOutput:\r\nstable\r\n")
	if !ok || payload != "stable\r\n" {
		t.Fatalf("CRLF exec envelope parsed incorrectly: ok=%v payload=%q", ok, payload)
	}

	roleOnly := normalizedSegmentKind(types.Message{Role: "assistant"}, types.ContentBlock{})
	if roleOnly != "assistant" {
		t.Fatalf("role fallback kind wrong: %q", roleOnly)
	}
	textFallback := normalizedSegmentKind(types.Message{}, types.ContentBlock{})
	if textFallback != "text" {
		t.Fatalf("text fallback kind wrong: %q", textFallback)
	}

	m := New()
	m.Observe("", msg("must not attach to an empty session"))
	if rep := m.Predict("non-empty", msg("must not attach to an empty session")); rep.ReferenceableBlocks != 0 {
		t.Fatalf("empty-session observe must not seed another session: %+v", rep)
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

package abharness

import (
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

func TestCompare_NoCompressionNoElisions(t *testing.T) {
	t.Parallel()
	full := strings.Repeat("line of output\n", 40)
	rep := Compare([]Turn{
		{Before: msg(full), After: msg(full)},
		{Before: msg(full), After: msg(full)},
	})
	if len(rep.Elisions) != 0 || rep.Lost() != 0 {
		t.Fatalf("identity compression should have no elisions: %+v", rep.Elisions)
	}
	if rep.Saved() != 0 {
		t.Fatalf("identity compression saved %d, want 0", rep.Saved())
	}
}

func TestCompare_RepeatReadIsRecoverable(t *testing.T) {
	t.Parallel()
	full := strings.Repeat("FILE CONTENT LINE\n", 50)
	rep := Compare([]Turn{
		{Before: msg(full), After: msg(full)},          // sent full first
		{Before: msg(full), After: msg("[unchanged]")}, // collapsed re-read
	})
	if rep.Lost() != 0 {
		t.Fatalf("re-read of previously-full content must not be lost: %+v", rep.Elisions)
	}
	if len(rep.Elisions) != 1 || rep.Elisions[0].Severity != SeverityRecoverable {
		t.Fatalf("want 1 recoverable elision, got %+v", rep.Elisions)
	}
	if rep.Saved() <= 0 {
		t.Fatalf("collapse should save bytes, got %d", rep.Saved())
	}
}

func TestCompare_CollapseWithoutPriorFullIsLost(t *testing.T) {
	t.Parallel()
	secret := strings.Repeat("NEVER SENT FULL\n", 50)
	rep := Compare([]Turn{
		{Before: msg(secret), After: msg("[gone]")}, // collapsed, never full, no ref
	})
	if rep.Lost() != 1 {
		t.Fatalf("content collapsed without prior full or reference must be lost: %+v", rep.Elisions)
	}
}

func TestCompare_ChangedSameLengthWithoutReferenceIsLost(t *testing.T) {
	t.Parallel()
	before := strings.Repeat("abcde", 120)
	after := strings.Repeat("vwxyz", 120)
	rep := Compare([]Turn{
		{Before: msg(before), After: msg(after)},
	})
	if rep.Lost() != 1 {
		t.Fatalf("same-length content change must be a lost comprehension issue: %+v", rep.Elisions)
	}
	if len(rep.Elisions) != 1 || rep.Elisions[0].Severity != SeverityChanged {
		t.Fatalf("want changed severity, got %+v", rep.Elisions)
	}
}

func TestCompare_ExtraAfterBlockIsAuditedAsLost(t *testing.T) {
	t.Parallel()
	rep := Compare([]Turn{
		{Before: msg("kept"), After: msg("kept", "unexpected injected instruction")},
	})
	if rep.Lost() != 1 {
		t.Fatalf("extra model-facing text must be audited as a lost issue: %+v", rep.Elisions)
	}
	if len(rep.Elisions) != 1 || rep.Elisions[0].Severity != SeverityExtra {
		t.Fatalf("want extra severity, got %+v", rep.Elisions)
	}
	if rep.Saved() >= 0 {
		t.Fatalf("extra text should produce negative savings, got %d", rep.Saved())
	}
}

func TestCompare_ReferencedElisionIsNotLost(t *testing.T) {
	t.Parallel()
	content := strings.Repeat("X", 500)
	rep := Compare([]Turn{
		{Before: msg(content), After: msg("[delta for f: local-archive://abc123]")},
	})
	if rep.Lost() != 0 {
		t.Fatalf("referenced elision should not be lost: %+v", rep.Elisions)
	}
	if len(rep.Elisions) != 1 || rep.Elisions[0].Severity != SeverityReferenced {
		t.Fatalf("want 1 referenced elision, got %+v", rep.Elisions)
	}
}

func TestCompare_BytesAndTurns(t *testing.T) {
	t.Parallel()
	a := strings.Repeat("a", 100)
	b := strings.Repeat("b", 100)
	rep := Compare([]Turn{
		{Before: msg(a, b), After: msg(a, "[ref]")},
	})
	if rep.Turns != 1 {
		t.Fatalf("turns = %d", rep.Turns)
	}
	if rep.BytesBefore != 200 {
		t.Fatalf("bytes before = %d, want 200", rep.BytesBefore)
	}
	if rep.BytesAfter != 100+len("[ref]") {
		t.Fatalf("bytes after = %d", rep.BytesAfter)
	}
}

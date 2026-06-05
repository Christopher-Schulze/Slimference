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

func TestCompare_CodexExecEnvelopeRepeatReadIsRecoverableByPayload(t *testing.T) {
	t.Parallel()
	payload := strings.Repeat("FILE CONTENT LINE\n", 50)
	first := "Chunk ID: aaa111\nWall time: 0.0000 seconds\nProcess exited with code 0\nOriginal token count: 900\nOutput:\n" + payload
	secondBefore := "Chunk ID: bbb222\nWall time: 0.1234 seconds\nProcess exited with code 0\nOriginal token count: 901\nOutput:\n" + payload
	secondAfter := "Chunk ID: bbb222\nWall time: 0.1234 seconds\nProcess exited with code 0\nOriginal token count: 901\nOutput:\n[context-elided kind=file-read status=unchanged path=\"AGENTS.md\"]"
	rep := Compare([]Turn{
		{Before: msg(first), After: msg(first)},
		{Before: msg(secondBefore), After: msg(secondAfter)},
	})
	if rep.Lost() != 0 {
		t.Fatalf("codex envelope re-read should be recoverable through stable payload: %+v", rep.Elisions)
	}
	if len(rep.Elisions) != 1 || rep.Elisions[0].Severity != SeverityRecoverable {
		t.Fatalf("want recoverable envelope elision, got %+v", rep.Elisions)
	}
	if rep.Saved() <= 0 {
		t.Fatalf("collapse should save bytes, got %d", rep.Saved())
	}
}

func TestCompare_CodexExecEnvelopeArchiveReferenceCanResolvePayload(t *testing.T) {
	t.Parallel()
	payload := strings.Repeat("internal/proxy/example.go:42:stable search result\n", 40)
	before := "Chunk ID: bbb222\nWall time: 0.1234 seconds\nProcess exited with code 0\nOriginal token count: 901\nOutput:\n" + payload
	after := "Chunk ID: bbb222\nWall time: 0.1234 seconds\nProcess exited with code 0\nOriginal token count: 901\nOutput:\n[context-elided kind=tool-output status=unchanged command=\"rg -n stable internal\" archive=local-archive://payload]"
	rep := CompareWithArchiveExpansion([]Turn{
		{Before: msg(before), After: msg(after)},
	}, func(id string) ([]byte, error) {
		if id != "payload" {
			t.Fatalf("unexpected archive id %q", id)
		}
		return []byte(payload), nil
	})
	if rep.Lost() != 0 {
		t.Fatalf("payload archive should recover Codex envelope elision: %+v", rep.Elisions)
	}
	if len(rep.Elisions) != 1 || rep.Elisions[0].Severity != SeverityReferenced {
		t.Fatalf("want referenced envelope elision, got %+v", rep.Elisions)
	}
}

func TestCompare_CodexExecEnvelopeNestedArchiveReferenceCanResolvePayload(t *testing.T) {
	t.Parallel()
	payload := strings.Repeat("internal/proxy/example.go:42:stable search result\n", 40)
	before := "Chunk ID: ccc333\nWall time: 0.5678 seconds\nProcess exited with code 0\nOriginal token count: 901\nOutput:\n" + payload
	compactedWithRecovery := "Chunk ID: aaa111\nWall time: 0.0000 seconds\nProcess exited with code 0\nOriginal token count: 901\nOutput:\n[rg] 40 match(es)\n[context-archive kind=tool-output uri=local-archive://full-payload]"
	after := "Chunk ID: ccc333\nWall time: 0.5678 seconds\nProcess exited with code 0\nOriginal token count: 901\nOutput:\n[context-elided kind=tool-output status=unchanged command=\"rg -n stable internal\" archive=local-archive://prior-compact]"
	rep := CompareWithArchiveExpansion([]Turn{
		{Before: msg(before), After: msg(after)},
	}, func(id string) ([]byte, error) {
		switch id {
		case "prior-compact":
			return []byte(compactedWithRecovery), nil
		case "full-payload":
			return []byte(payload), nil
		default:
			t.Fatalf("unexpected archive id %q", id)
			return nil, errArchiveMissing{}
		}
	})
	if rep.Lost() != 0 {
		t.Fatalf("nested payload archive should recover Codex envelope elision: %+v", rep.Elisions)
	}
	if len(rep.Elisions) != 1 || rep.Elisions[0].Severity != SeverityReferenced {
		t.Fatalf("want referenced nested envelope elision, got %+v", rep.Elisions)
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

func TestCompare_InsertedLeadingBlockDoesNotCreateFalseChangedLoss(t *testing.T) {
	t.Parallel()
	rep := Compare([]Turn{
		{Before: msg("continue"), After: msg("expected recovery note", "continue")},
	})
	if rep.Lost() != 1 {
		t.Fatalf("only the inserted note should be audited as lost, got %+v", rep.Elisions)
	}
	if len(rep.Elisions) != 1 || rep.Elisions[0].Severity != SeverityExtra || rep.Elisions[0].Preview != "expected recovery note" {
		t.Fatalf("want one leading extra note, got %+v", rep.Elisions)
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

func TestCompareWithArchiveExpansion_VerifiesChunkReferencesByReconstruction(t *testing.T) {
	t.Parallel()
	chunk := strings.Repeat("chunk body\n", 80)
	content := "prefix\n" + chunk + "\nsuffix\n"
	after := "prefix\n[context-chunk status=unchanged uri=local-archive://chunk1 bytes=880]\nsuffix\n"
	rep := CompareWithArchiveExpansion([]Turn{
		{Before: msg(content), After: msg(after)},
	}, func(id string) ([]byte, error) {
		if id != "chunk1" {
			t.Fatalf("unexpected archive id %q", id)
		}
		return []byte(chunk), nil
	})
	if rep.Lost() != 0 {
		t.Fatalf("chunk reference expansion should reconstruct the source: %+v", rep.Elisions)
	}
	if len(rep.Elisions) != 1 || rep.Elisions[0].Severity != SeverityReferenced {
		t.Fatalf("want referenced severity, got %+v", rep.Elisions)
	}
}

func TestCompareWithArchiveExpansion_VerifiesReferencedBytes(t *testing.T) {
	t.Parallel()
	content := strings.Repeat("exact archived source\n", 40)
	rep := CompareWithArchiveExpansion([]Turn{
		{Before: msg(content), After: msg("[context-archive uri=local-archive://good]")},
	}, func(id string) ([]byte, error) {
		if id != "good" {
			t.Fatalf("unexpected archive id %q", id)
		}
		return []byte(content), nil
	})
	if rep.Lost() != 0 {
		t.Fatalf("exact archive expansion should be recoverable: %+v", rep.Elisions)
	}
	if len(rep.Elisions) != 1 || rep.Elisions[0].Severity != SeverityReferenced {
		t.Fatalf("want referenced severity, got %+v", rep.Elisions)
	}
}

func TestCompareWithArchiveExpansion_FailsMissingOrMismatchedReference(t *testing.T) {
	t.Parallel()
	content := strings.Repeat("expected source\n", 40)
	missing := CompareWithArchiveExpansion([]Turn{
		{Before: msg(content), After: msg("[context-archive uri=local-archive://missing]")},
	}, func(string) ([]byte, error) {
		return nil, errArchiveMissing{}
	})
	if missing.Lost() != 1 || len(missing.Elisions) != 1 || missing.Elisions[0].Severity != SeverityReferenceMissing {
		t.Fatalf("missing archive must be a lost issue: %+v", missing.Elisions)
	}

	mismatch := CompareWithArchiveExpansion([]Turn{
		{Before: msg(content), After: msg("[context-archive uri=local-archive://wrong]")},
	}, func(string) ([]byte, error) {
		return []byte("different source"), nil
	})
	if mismatch.Lost() != 1 || len(mismatch.Elisions) != 1 || mismatch.Elisions[0].Severity != SeverityReferenceMismatch {
		t.Fatalf("mismatched archive must be a lost issue: %+v", mismatch.Elisions)
	}

}

func TestCompareWithArchiveExpansion_PriorFullWinsBeforeArchiveLookup(t *testing.T) {
	t.Parallel()
	content := strings.Repeat("prior full content\n", 40)
	called := false
	rep := CompareWithArchiveExpansion([]Turn{
		{Before: msg(content), After: msg(content)},
		{Before: msg(content), After: msg("[context-archive uri=local-archive://missing]")},
	}, func(string) ([]byte, error) {
		called = true
		return nil, errArchiveMissing{}
	})
	if called {
		t.Fatal("prior full content should not need archive lookup")
	}
	if rep.Lost() != 0 || len(rep.Elisions) != 1 || rep.Elisions[0].Severity != SeverityRecoverable {
		t.Fatalf("prior full collapse should stay recoverable: %+v", rep.Elisions)
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

type errArchiveMissing struct{}

func (errArchiveMissing) Error() string { return "missing archive" }

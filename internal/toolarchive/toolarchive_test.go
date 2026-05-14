package toolarchive

import (
	"os"
	"strings"
	"testing"
)

func TestArchiveAndExpand(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry, err := Archive(dir, Input{
		ToolName:  "Bash",
		ToolUseID: "tool_123",
		SessionID: "sess_1",
		Command:   "npm test",
		Output:    strings.Repeat("line\n", 800),
		Preview:   "short preview",
	})
	if err != nil {
		t.Fatal(err)
	}
	if entry == nil || entry.ID != "tool_123" {
		t.Fatalf("entry=%+v", entry)
	}

	meta, body, err := Expand(dir, "slim://archive/tool_123")
	if err != nil {
		t.Fatal(err)
	}
	if meta.ID != "tool_123" || !strings.Contains(string(body), "line") {
		t.Fatalf("meta=%+v body-len=%d", meta, len(body))
	}

	stats, err := Snapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Count != 1 || stats.Expanded != 1 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestRenderContextIncludesBodyExpandHintForASTPreview(t *testing.T) {
	t.Parallel()
	got := RenderContext(Entry{
		ID:      "tool-ast",
		Command: "cat service.go",
		Preview: `package demo

func Huge() int { /* body omitted: 40 lines */ }

/* AST-compacted by Slimference. Re-read the file for full bodies when editing/debugging. */`,
	})
	if !strings.Contains(got, "Body expand: slimference expand-body tool-ast <symbol>") {
		t.Fatalf("missing body expand hint:\n%s", got)
	}
}

func TestEligibleRequiresRealMetadata(t *testing.T) {
	t.Parallel()

	if Eligible(Input{Output: strings.Repeat("x", 4000)}) {
		t.Fatal("metadata-free payload should not be archived")
	}
	if !Eligible(Input{ToolName: "Bash", Output: strings.Repeat("x", 4000)}) {
		t.Fatal("real tool payload should be archived")
	}
}

func TestExpandMissing(t *testing.T) {
	t.Parallel()

	_, _, err := Expand(t.TempDir(), "missing")
	if !os.IsNotExist(err) {
		t.Fatalf("err=%v", err)
	}
}

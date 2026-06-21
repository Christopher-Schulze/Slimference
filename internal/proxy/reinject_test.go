package proxy

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/contentarchive"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func writeArchiveEntry(t *testing.T, home, original string) string {
	t.Helper()
	return writeArchiveEntryForSession(t, home, "sess", original)
}

func writeArchiveEntryForSession(t *testing.T, home, sessionID, original string) string {
	t.Helper()
	entry, err := contentarchive.Put(contentarchive.DefaultDir(home), contentarchive.Input{
		SessionID:    sessionID,
		MessageIndex: 1,
		BlockIndex:   0,
		SubLayer:     "test",
		Original:     original,
	}, contentarchive.Limits{})
	if err != nil || entry == nil {
		t.Fatalf("seed archive: entry=%#v err=%v", entry, err)
	}
	return entry.ID
}

func withArchiveHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
}

func TestHasArchiveReference(t *testing.T) {
	t.Parallel()
	if !hasArchiveReference("see local-archive://abc123") {
		t.Fatal("expected match for local-archive uri")
	}
	if !hasArchiveReference("see slim://archive/legacy-id") {
		t.Fatal("expected match for legacy uri")
	}
	if hasArchiveReference("nothing here") {
		t.Fatal("must not match unrelated text")
	}
}

func TestExtractArchiveIDs(t *testing.T) {
	t.Parallel()
	if got := extractArchiveIDs(""); got != nil {
		t.Fatalf("empty input: %v", got)
	}
	if got := extractArchiveIDs("plain prose"); got != nil {
		t.Fatalf("no match input: %v", got)
	}
	got := extractArchiveIDs("see local-archive://aaa and slim://archive/bbb and local-archive://aaa again")
	if len(got) != 2 || got[0] != "aaa" || got[1] != "bbb" {
		t.Fatalf("dedup order broken: %+v", got)
	}
}

func TestReinjectArchivedContent_NoMessages(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	if got := p.reinjectArchivedContent(nil); got != nil {
		t.Fatalf("nil input must round-trip: %+v", got)
	}
}

func TestReinjectArchivedContent_HappyPath(t *testing.T) {
	home := t.TempDir()
	withArchiveHome(t, home)
	original := strings.Repeat("archived line content that is definitely long enough to be eligible\n", 4)
	id := writeArchiveEntry(t, home, original)

	p := New(config.Defaults())
	msgs := []types.Message{
		{
			Role: "user",
			Content: []types.ContentBlock{
				{Type: "text", Text: "please look at local-archive://" + id + " carefully"},
			},
		},
	}
	out := p.reinjectArchivedContent(msgs)
	if len(out[0].Content) != 2 {
		t.Fatalf("expected one expansion appended, got %d blocks", len(out[0].Content))
	}
	if !strings.Contains(out[0].Content[1].Text, "archived line content") {
		t.Fatalf("expansion missing archived body:\n%s", out[0].Content[1].Text)
	}
	stats, err := contentarchive.LoadStats(contentarchive.DefaultDir(home))
	if err != nil {
		t.Fatal(err)
	}
	if stats.ReInjectCount == 0 {
		t.Fatal("re-inject counter did not advance")
	}
	if stats.ReInjectBytesRaw != int64(len(original)) || stats.ReInjectTokensEstimate != int64(len(original)/4) {
		t.Fatalf("re-inject recovery cost not recorded: %+v want bytes=%d tokens=%d", stats, len(original), len(original)/4)
	}
}

func TestReinjectArchivedContent_DuplicateRefDedupedPerMessage(t *testing.T) {
	home := t.TempDir()
	withArchiveHome(t, home)
	id := writeArchiveEntry(t, home, strings.Repeat("body bytes for dedup ", 8))
	p := New(config.Defaults())
	msgs := []types.Message{{
		Role: "user",
		Content: []types.ContentBlock{
			{Type: "text", Text: "look at local-archive://" + id + " then local-archive://" + id + " again"},
		},
	}}
	out := p.reinjectArchivedContent(msgs)
	if len(out[0].Content) != 2 {
		t.Fatalf("dup ref must collapse to one expansion: %d blocks", len(out[0].Content))
	}
}

func TestReinjectArchivedContent_MissingEntryLeavesMarker(t *testing.T) {
	home := t.TempDir()
	withArchiveHome(t, home)
	p := New(config.Defaults())
	msgs := []types.Message{{
		Role: "user",
		Content: []types.ContentBlock{
			{Type: "text", Text: "ref to local-archive://missing-id"},
		},
	}}
	out := p.reinjectArchivedContent(msgs)
	if len(out[0].Content) != 1 {
		t.Fatalf("missing archive must not append: %d blocks", len(out[0].Content))
	}
	if !strings.Contains(out[0].Content[0].Text, "local-archive://missing-id") {
		t.Fatal("original marker must remain on miss")
	}
}

func TestReinjectArchivedContentForSession_StaleSessionFailsOpen(t *testing.T) {
	home := t.TempDir()
	withArchiveHome(t, home)
	id := writeArchiveEntryForSession(t, home, "sess-live", strings.Repeat("same-session body\n", 5))
	p := New(config.Defaults())
	msgs := []types.Message{{
		Role: "user",
		Content: []types.ContentBlock{
			{Type: "text", Text: "need local-archive://" + id},
		},
	}}
	out := p.reinjectArchivedContentForSession("sess-other", msgs)
	if len(out[0].Content) != 1 {
		t.Fatalf("stale session archive must not append expansion: %d blocks", len(out[0].Content))
	}
	if !strings.Contains(out[0].Content[0].Text, "local-archive://"+id) {
		t.Fatal("stale session marker must remain visible")
	}
	stats, err := contentarchive.LoadStats(contentarchive.DefaultDir(home))
	if err != nil {
		t.Fatal(err)
	}
	if stats.Expanded != 0 || stats.ReInjectCount != 0 {
		t.Fatalf("stale session must not count expansion/reinject: %+v", stats)
	}
}

func TestReinjectArchivedContent_DoesNotRecursivelyExpandArchiveReferences(t *testing.T) {
	home := t.TempDir()
	withArchiveHome(t, home)
	nestedBody := strings.Repeat("nested archive body\n", 5)
	nestedID := writeArchiveEntry(t, home, nestedBody)
	outerBody := "outer archived body refers to local-archive://" + nestedID + "\n" + strings.Repeat("outer body line\n", 5)
	outerID := writeArchiveEntry(t, home, outerBody)
	p := New(config.Defaults())
	msgs := []types.Message{{
		Role: "user",
		Content: []types.ContentBlock{
			{Type: "text", Text: "need local-archive://" + outerID},
		},
	}}
	out := p.reinjectArchivedContent(msgs)
	if len(out[0].Content) != 2 {
		t.Fatalf("only the directly requested archive should expand, got %d blocks", len(out[0].Content))
	}
	if !strings.Contains(out[0].Content[1].Text, "outer archived body") ||
		!strings.Contains(out[0].Content[1].Text, "local-archive://"+nestedID) {
		t.Fatalf("outer expansion missing expected body/reference: %s", out[0].Content[1].Text)
	}
	if strings.Contains(out[0].Content[1].Text, nestedBody) {
		t.Fatalf("nested archive must not recursively expand in the same request: %s", out[0].Content[1].Text)
	}
}

func TestReinjectArchivedContent_RespectsMaxBudget(t *testing.T) {
	home := t.TempDir()
	withArchiveHome(t, home)
	ids := make([]string, 0, maxReinjectsPerRequest+3)
	for range maxReinjectsPerRequest + 3 {
		id := writeArchiveEntry(t, home, strings.Repeat("budget filler\n", 6))
		// Each entry needs unique content for the archive id to differ.
		ids = append(ids, id)
	}
	p := New(config.Defaults())
	var sb strings.Builder
	for _, id := range ids {
		sb.WriteString("see local-archive://" + id + " ")
	}
	msgs := []types.Message{{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: sb.String()}}}}
	out := p.reinjectArchivedContent(msgs)
	expansions := len(out[0].Content) - 1 // minus the original block
	if expansions > maxReinjectsPerRequest {
		t.Fatalf("budget breached: %d expansions vs cap %d", expansions, maxReinjectsPerRequest)
	}
}

func TestReinjectArchivedContent_HomeUnavailableReturnsInput(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	cfg := config.Defaults()
	cfg.Analytics.LogDir = ""
	p := New(cfg)
	msgs := []types.Message{{Content: []types.ContentBlock{{Text: "irrelevant"}}}}
	got := p.reinjectArchivedContent(msgs)
	if len(got) != len(msgs) {
		t.Fatalf("home-unavailable path must return input unchanged")
	}
}

func TestReinjectArchivedContent_EmptyTextBlockSkipped(t *testing.T) {
	home := t.TempDir()
	withArchiveHome(t, home)
	p := New(config.Defaults())
	msgs := []types.Message{{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: ""}}}}
	out := p.reinjectArchivedContent(msgs)
	if len(out[0].Content) != 1 {
		t.Fatal("empty text must not produce expansions")
	}
}

func TestReinjectArchivedContent_BudgetBreaksInnerLoop(t *testing.T) {
	home := t.TempDir()
	withArchiveHome(t, home)
	// Force the inner per-block break by supplying maxReinjectsPerRequest+1
	// distinct ids inside a single content block.
	ids := make([]string, 0, maxReinjectsPerRequest+1)
	for i := range maxReinjectsPerRequest + 1 {
		id := writeArchiveEntry(t, home, strings.Repeat("inner-content-payload-", 8)+strings.Repeat("x", i+1))
		ids = append(ids, id)
	}
	p := New(config.Defaults())
	var sb strings.Builder
	for _, id := range ids {
		sb.WriteString("local-archive://" + id + " ")
	}
	msgs := []types.Message{{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: sb.String()}}}}
	out := p.reinjectArchivedContent(msgs)
	expansions := len(out[0].Content) - 1
	if expansions > maxReinjectsPerRequest {
		t.Fatalf("inner break should cap expansions: got %d", expansions)
	}
}

func TestReinjectArchivedContent_BudgetBreaksOuterLoop(t *testing.T) {
	home := t.TempDir()
	withArchiveHome(t, home)
	// Seed maxReinjectsPerRequest distinct ids and reference all of them
	// in the first message so the cross-message budget is exhausted
	// before the next message is touched.
	ids := make([]string, 0, maxReinjectsPerRequest)
	for i := range maxReinjectsPerRequest {
		id := writeArchiveEntry(t, home, strings.Repeat("uniq-content-", 8)+strings.Repeat("x", i+1))
		ids = append(ids, id)
	}
	p := New(config.Defaults())
	var first strings.Builder
	for _, id := range ids {
		first.WriteString("see local-archive://" + id + " ")
	}
	tail := writeArchiveEntry(t, home, strings.Repeat("tail-content-", 6))
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: first.String()}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "see local-archive://" + tail}}},
	}
	out := p.reinjectArchivedContent(msgs)
	if len(out[1].Content) != 1 {
		t.Fatalf("budget exhaustion must skip later messages, got %d blocks", len(out[1].Content))
	}
}

func TestReinjectArchivedContent_PathFromDefaultDir(t *testing.T) {
	t.Parallel()
	// Indirect coverage of the entries-dir branch via DefaultDir.
	home := t.TempDir()
	if got := contentarchive.DefaultDir(home); !strings.HasSuffix(got, filepath.Join(".slimference", "content-archive")) {
		t.Fatalf("default dir wrong: %s", got)
	}
}

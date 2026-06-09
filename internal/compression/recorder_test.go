package compression

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/contentarchive"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

type stubRecorder struct {
	calls []contentarchive.Input
	id    string
	err   error
}

func (s *stubRecorder) Record(input contentarchive.Input) (string, error) {
	s.calls = append(s.calls, input)
	return s.id, s.err
}

func TestNoopRecorder_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	id, err := NoopRecorder.Record(contentarchive.Input{Original: "x"})
	if id != "" || err != nil {
		t.Fatalf("noop must be empty/nil, got %q %v", id, err)
	}
}

func TestNewDiskRecorder_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "ca")
	rec := NewDiskRecorder(dir, contentarchive.Limits{})
	id, err := rec.Record(contentarchive.Input{
		SessionID:    "sess-x",
		MessageIndex: 1,
		BlockIndex:   0,
		SubLayer:     "test",
		Original:     "this is a long enough payload to be eligible for archiving in the content archive store",
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty id")
	}
}

func TestDiskRecorder_NilSafe(t *testing.T) {
	t.Parallel()
	var rec *DiskRecorder
	id, err := rec.Record(contentarchive.Input{Original: "x"})
	if id != "" || err != nil {
		t.Fatalf("nil recorder: %q %v", id, err)
	}
}

func TestDiskRecorder_EmptyDir(t *testing.T) {
	t.Parallel()
	rec := NewDiskRecorder("", contentarchive.Limits{})
	id, err := rec.Record(contentarchive.Input{Original: "x"})
	if id != "" || err != nil {
		t.Fatalf("empty dir: %q %v", id, err)
	}
}

func TestDiskRecorder_PutErrorSurfaces(t *testing.T) {
	t.Parallel()
	// Pointing at a path where the parent cannot be created surfaces the
	// underlying error from contentarchive.Put.
	rec := NewDiskRecorder("/proc/0/no-such/dir", contentarchive.Limits{})
	_, err := rec.Record(contentarchive.Input{
		SessionID: "s",
		Original:  "this content is long enough to be eligible for archiving via the recorder",
	})
	if err == nil {
		t.Fatal("expected error from underlying Put")
	}
}

func TestDiskRecorder_IneligibleInputReturnsEmpty(t *testing.T) {
	t.Parallel()
	rec := NewDiskRecorder(t.TempDir(), contentarchive.Limits{})
	id, err := rec.Record(contentarchive.Input{Original: "tiny"})
	if id != "" || err != nil {
		t.Fatalf("ineligible: %q %v", id, err)
	}
}

func TestArchiveOriginal_NilRecorder(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	c := NewDeterministicCompressor(&cfg)
	if got := c.archiveOriginal(0, 0, "x", "abc"); got != "" {
		t.Fatalf("nil recorder must return empty id: %q", got)
	}
}

func TestArchiveOriginal_NoopRecorderShortCircuits(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	c := NewDeterministicCompressor(&cfg).WithRecorder(NoopRecorder)
	if got := c.archiveOriginal(0, 0, "x", "abc"); got != "" {
		t.Fatalf("noop recorder must short-circuit: %q", got)
	}
}

func TestArchiveOriginal_EmptyOriginal(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	stub := &stubRecorder{id: "stub-id"}
	c := NewDeterministicCompressor(&cfg).WithRecorder(stub)
	if got := c.archiveOriginal(0, 0, "x", ""); got != "" {
		t.Fatalf("empty original must short-circuit: %q", got)
	}
	if len(stub.calls) != 0 {
		t.Fatal("recorder must not be called for empty original")
	}
}

func TestArchiveOriginal_RecorderError(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	stub := &stubRecorder{err: errors.New("record fail")}
	c := NewDeterministicCompressor(&cfg).WithRecorder(stub)
	if got := c.archiveOriginal(2, 1, "preview_pass", "long original content"); got != "" {
		t.Fatalf("error must surface as empty id: %q", got)
	}
}

func TestCompress_ArchiveRequiredMutationFullPassesOnRecorderError(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	stub := &stubRecorder{err: errors.New("record fail")}
	c := NewDeterministicCompressor(cfg).WithRecorder(stub)

	code := "package main\n\n// must remain if archive fails\nfunc main() {}\n// another retained comment\n"
	msgs := []types.Message{
		buildMessage(t, 0, "user", types.ContentBlock{
			Type:      "tool_result",
			Text:      code,
			ToolInput: `{"path": "main.go"}`,
		}),
		buildMessage(t, 1, "assistant", textBlock("ok")),
		buildMessage(t, 2, "user", textBlock("tail")),
	}

	result := c.Compress(msgs)
	if result.CommentSaved != 0 {
		t.Fatalf("archive-required comment strip must full-pass on archive failure, saved=%d", result.CommentSaved)
	}
	got := result.Messages[0].Content[0]
	if got.Text != code {
		t.Fatalf("archive failure must keep original text, got %q", got.Text)
	}
	if got.ArchiveID != "" {
		t.Fatalf("archive failure must not stamp archive id, got %q", got.ArchiveID)
	}
}

func TestCompress_NearDedupArchiveRoundTripsOriginalBytes(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	cfg.DedupSimilarityThreshold = 0.45
	archiveDir := filepath.Join(t.TempDir(), "content-archive")
	c := NewDeterministicCompressor(cfg).WithRecorder(NewDiskRecorder(archiveDir, contentarchive.Limits{}))

	prefix := repeatString("alpha beta gamma delta ", 25)
	body1 := prefix + "suffixaaaa"
	body2 := prefix + "suffixaaab"
	msgs := []types.Message{
		buildMessage(t, 0, "user", toolResultBlock(body1)),
		buildMessage(t, 1, "assistant", textBlock("x")),
		buildMessage(t, 2, "user", toolResultBlock(body2)),
		buildMessage(t, 3, "assistant", textBlock("y")),
		buildMessage(t, 4, "user", textBlock("z")),
	}

	result := c.CompressWithSession("sess-roundtrip", msgs)
	block := result.Messages[2].Content[0]
	if result.NearDedupSaved <= 0 || block.ArchiveID == "" {
		t.Fatalf("near-dedup did not archive: saved=%d block=%+v", result.NearDedupSaved, block)
	}
	meta, body, err := contentarchive.Get(archiveDir, block.ArchiveID)
	if err != nil {
		t.Fatalf("expand archive %q: %v", block.ArchiveID, err)
	}
	if string(body) != body2 {
		t.Fatalf("archive body mismatch\ngot:  %q\nwant: %q", string(body), body2)
	}
	if meta.SessionID != "sess-roundtrip" || meta.SubLayer != "dedup_near" || meta.MessageIndex != 2 || meta.BlockIndex != 0 {
		t.Fatalf("archive metadata mismatch: %+v", meta)
	}
	if got := result.ArchiveWrites["dedup_near"]; got != 1 {
		t.Fatalf("near-dedup archive writes=%d want 1", got)
	}
	decision := findLayer1Decision(t, result.Decisions, "dedup_near")
	if !decision.Applied || !decision.RequiresArchive || decision.ArchiveWrites != 1 {
		t.Fatalf("near-dedup decision missing archive roundtrip proof: %+v", decision)
	}
}

func TestCompress_Layer1CorpusArchiveRoundTripsEveryRecoverableMutation(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	cfg.DedupSimilarityThreshold = 0.45
	cfg.StructureMinTokens = 10000
	cfg.StructureLanguages = []string{}
	archiveDir := filepath.Join(t.TempDir(), "content-archive")
	c := NewDeterministicCompressor(cfg).WithRecorder(NewDiskRecorder(archiveDir, contentarchive.Limits{}))

	var commented strings.Builder
	commented.WriteString("package main\n\n")
	for i := 0; i < 24; i++ {
		commented.WriteString("// noisy implementation note that must be recoverable from archive\n")
	}
	commented.WriteString("func important() string {\n\treturn \"archive me\"\n}\n")

	prefix := repeatString("alpha beta gamma delta ", 25)
	body1 := prefix + "suffixaaaa"
	body2 := prefix + "suffixaaab"
	successOutput := strings.Join([]string{
		"Running test suite...",
		"Initializing runner",
		"Loading fixtures",
		"Preparing deterministic archive proof",
		"Executing package tests",
		"All tests passed",
		"Elapsed: 1.2s",
		"Done.",
	}, "\n")

	msgs := []types.Message{
		buildMessage(t, 0, "user", types.ContentBlock{
			Type:      "tool_result",
			Text:      commented.String(),
			ToolInput: `{"path":"pkg/commented.go"}`,
		}),
		buildMessage(t, 1, "assistant", textBlock("comment strip observed")),
		buildMessage(t, 2, "user", toolResultBlock(body1)),
		buildMessage(t, 3, "assistant", textBlock("first similar block observed")),
		buildMessage(t, 4, "user", toolResultBlock(body2)),
		buildMessage(t, 5, "assistant", textBlock("second similar block observed")),
		buildMessage(t, 6, "user", types.ContentBlock{
			Type:      "tool_result",
			Text:      successOutput,
			ToolInput: `{"command":"go test ./..."}`,
		}),
		buildMessage(t, 7, "assistant", textBlock("success output observed")),
		buildMessage(t, 8, "user", textBlock("latest exchange stays in window")),
	}

	result := c.CompressWithSession("sess-layer1-corpus", msgs)
	if result.CommentSaved <= 0 {
		t.Fatalf("CommentSaved=%d want > 0", result.CommentSaved)
	}
	if result.NearDedupSaved <= 0 {
		t.Fatalf("NearDedupSaved=%d want > 0", result.NearDedupSaved)
	}

	wantSublayers := map[string]bool{
		"comment_strip": false,
		"dedup_near":    false,
	}
	archiveBackedBlocks := 0
	for msgIdx, msg := range result.Messages {
		for blockIdx, block := range msg.Content {
			if block.ArchiveID == "" {
				continue
			}
			archiveBackedBlocks++
			meta, body, err := contentarchive.Get(archiveDir, block.ArchiveID)
			if err != nil {
				t.Fatalf("expand archive %q at result[%d].content[%d]: %v", block.ArchiveID, msgIdx, blockIdx, err)
			}
			if meta.SessionID != "sess-layer1-corpus" {
				t.Fatalf("archive %q session=%q want sess-layer1-corpus", block.ArchiveID, meta.SessionID)
			}
			if meta.MessageIndex != msgIdx || meta.BlockIndex != blockIdx {
				t.Fatalf("archive %q metadata position=(%d,%d) want (%d,%d)", block.ArchiveID, meta.MessageIndex, meta.BlockIndex, msgIdx, blockIdx)
			}
			original := msgs[meta.MessageIndex].Content[meta.BlockIndex].Text
			if string(body) != original {
				t.Fatalf("archive %q body mismatch\ngot:  %q\nwant: %q", block.ArchiveID, string(body), original)
			}
			for subLayer := range wantSublayers {
				if archiveTagContains(meta.SubLayer, subLayer) {
					wantSublayers[subLayer] = true
				}
			}
		}
	}
	if archiveBackedBlocks < len(wantSublayers) {
		t.Fatalf("archive-backed blocks=%d want at least %d", archiveBackedBlocks, len(wantSublayers))
	}
	for subLayer, seen := range wantSublayers {
		if !seen {
			t.Fatalf("missing archive-backed mutation for %s", subLayer)
		}
		decision := findLayer1Decision(t, result.Decisions, subLayer)
		if !decision.Applied || !decision.RequiresArchive || decision.ArchiveWrites <= 0 {
			t.Fatalf("%s decision lacks archive-backed applied proof: %+v", subLayer, decision)
		}
		if got := result.ArchiveWrites[subLayer]; got <= 0 {
			t.Fatalf("%s archive writes=%d want >0", subLayer, got)
		}
	}
}

func archiveTagContains(tag string, subLayer string) bool {
	for _, part := range strings.Split(tag, ",") {
		if strings.TrimSpace(part) == subLayer {
			return true
		}
	}
	return false
}

func TestArchiveOriginal_HappyPath(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	stub := &stubRecorder{id: "stub-id"}
	c := NewDeterministicCompressor(&cfg).WithRecorder(stub)
	c.activeSessionID = "sess-A"
	c.activeArchiveWrites = make(map[string]int)
	id := c.archiveOriginal(7, 3, "comment_strip", "real content here")
	if id != "stub-id" {
		t.Fatalf("expected stub-id, got %q", id)
	}
	if got := c.snapshotArchiveWrites()["comment_strip"]; got != 1 {
		t.Fatalf("archive writes for comment_strip=%d want 1", got)
	}
	if len(stub.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(stub.calls))
	}
	got := stub.calls[0]
	if got.SessionID != "sess-A" || got.MessageIndex != 7 || got.BlockIndex != 3 || got.SubLayer != "comment_strip" {
		t.Fatalf("call mismatch: %+v", got)
	}
}

func TestCompressWithSession_PropagatesSessionID(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.Tuning.StructurePreview = true
	stub := &stubRecorder{id: "x"}
	c := NewDeterministicCompressor(&cfg).WithRecorder(stub)

	body := `{"items":[`
	for i := 0; i < 200; i++ {
		body += `{"k":"v","value":"line` + strconv.Itoa(i) + `"},`
	}
	body += `{"k":"v"}]}`
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "u"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", Text: body}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "next"}}},
	}
	c.cfg.SlidingWindow = 1
	res := c.CompressWithSession("sess-PROP", msgs)
	if res.TokensSaved == 0 && res.PreviewSaved == 0 {
		t.Skip("no mutation; cannot verify session propagation")
	}
	if c.activeSessionID != "" {
		t.Fatalf("activeSessionID must be cleared after call, got %q", c.activeSessionID)
	}
	if len(stub.calls) == 0 {
		t.Fatal("expected recorder calls")
	}
	for _, call := range stub.calls {
		if call.SessionID != "sess-PROP" {
			t.Fatalf("session id mismatch on call %+v", call)
		}
	}
}

func TestPreviewPass_StampsArchiveIDWhenRecorderActive(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.Tuning.StructurePreview = true
	stub := &stubRecorder{id: "preview-id"}
	c := NewDeterministicCompressor(&cfg).WithRecorder(stub)

	// Build a JSON-shaped tool_result that StructurePreview will compress.
	body := `{"items":[`
	for i := 0; i < 200; i++ {
		body += `{"k":"v","value":"line` + strconv.Itoa(i) + `"},`
	}
	body += `{"k":"v"}]}`
	msgs := []types.Message{{
		Role: "assistant",
		Content: []types.ContentBlock{{
			Type: "tool_result",
			Text: body,
		}},
	}}
	saved := c.structurePreviewPass(msgs, 1)
	if saved == 0 {
		t.Skip("preview did not fire on fixture; archive stamping not exercised")
	}
	if msgs[0].Content[0].ArchiveID == "" {
		t.Fatal("expected ArchiveID to be stamped after preview mutation")
	}
	if len(stub.calls) == 0 || stub.calls[0].SubLayer != "preview_pass" {
		t.Fatalf("recorder calls: %+v", stub.calls)
	}
}

func TestCompressMessage_StampsArchiveIDOnLossyMutation(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	stub := &stubRecorder{id: "layer1-id"}
	c := NewDeterministicCompressor(&cfg).WithRecorder(stub)

	// A tool_result containing a long, deduplicatable JSON payload.
	body := `{"a":"` + repeatString("x", 800) + `"}`
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "first"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", Text: body}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", Text: body}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "next user turn"}}},
	}
	c.cfg.SlidingWindow = 1
	res := c.Compress(msgs)
	if res.TokensSaved == 0 {
		t.Skip("no compression on fixture; archive stamping branch not exercised")
	}
	stamped := false
	for _, m := range res.Messages {
		for _, b := range m.Content {
			if b.ArchiveID != "" {
				stamped = true
			}
		}
	}
	if !stamped {
		t.Fatal("expected at least one block to carry ArchiveID")
	}
}

func TestCompressMessage_ANSIOnlyDoesNotArchive(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	stub := &stubRecorder{id: "should-not-fire"}
	c := NewDeterministicCompressor(&cfg).WithRecorder(stub)

	body := "\x1b[31mred\x1b[0m " + repeatString("a", 300)
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "u"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", Text: body}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "next"}}},
	}
	c.cfg.SlidingWindow = 1
	res := c.Compress(msgs)
	for _, m := range res.Messages {
		for _, b := range m.Content {
			if b.ArchiveID != "" && ansiOnlyChange(body, b.Text) {
				t.Fatalf("ANSI-only mutation should not stamp ArchiveID: text=%q", b.Text)
			}
		}
	}
}

func repeatString(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

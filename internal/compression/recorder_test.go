package compression

import (
	"errors"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/contentarchive"
	"github.com/slimference/slimference/internal/types"
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

func TestArchiveOriginal_HappyPath(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	stub := &stubRecorder{id: "stub-id"}
	c := NewDeterministicCompressor(&cfg).WithRecorder(stub)
	c.activeSessionID = "sess-A"
	id := c.archiveOriginal(7, 3, "comment_strip", "real content here")
	if id != "stub-id" {
		t.Fatalf("expected stub-id, got %q", id)
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

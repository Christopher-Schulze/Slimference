package compression

import (
	"strings"
	"sync"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/contentarchive"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func TestCoordinatorSubsume_SkipsHeavySubLayers(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	c := NewDeterministicCompressor(&cfg)

	// Build messages with content that would normally trigger dedup +
	// structure extract; with subsume on, those passes must skip.
	body := strings.Repeat("repeated tool output line\n", 80)
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "u"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", Text: body}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", Text: body}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "next"}}},
	}
	c.cfg.SlidingWindow = 1
	c.SetCoordinatorSubsume(true)

	res := c.Compress(msgs)
	if c.CoordinatorSkipped() == 0 {
		t.Fatal("coordinator skip counter must advance")
	}
	if res.DedupSaved != 0 {
		t.Fatalf("dedup must be skipped under subsume; got dedupSaved=%d", res.DedupSaved)
	}
}

func TestCoordinatorSubsume_JSONCompactArchivesOriginal(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	stub := &capturingRecorder{}
	c := NewDeterministicCompressor(&cfg).WithRecorder(stub)

	// JSON content with redundant whitespace gets compacted by the
	// cheap pass; under subsume the archive should still record the
	// original since the change isn't ANSI-only.
	jsonBody := "{\n" +
		strings.Repeat("    \"key\": \"value with lots of padding\",\n", 40) +
		"    \"last\": true\n}"
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "u"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", Text: jsonBody}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "next"}}},
	}
	c.cfg.SlidingWindow = 1
	c.SetCoordinatorSubsume(true)
	c.Compress(msgs)
	if len(stub.calls) == 0 {
		t.Fatal("subsume + JSON compact must archive original")
	}
}

type capturingRecorder struct {
	calls []contentarchive.Input
}

func (r *capturingRecorder) Record(in contentarchive.Input) (string, error) {
	r.calls = append(r.calls, in)
	return "stub-id", nil
}

func TestCoordinatorSubsume_PreservesCheapPasses(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	c := NewDeterministicCompressor(&cfg)

	// ANSI codes get stripped by the cheap pass even with subsume on.
	body := "\x1b[31mred line\x1b[0m\n" + strings.Repeat("plain content line\n", 60)
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "u"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", Text: body}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "next"}}},
	}
	c.cfg.SlidingWindow = 1
	c.SetCoordinatorSubsume(true)
	res := c.Compress(msgs)
	if res.ANSISaved == 0 {
		t.Fatal("ANSI strip must run even under subsume")
	}
	// Verify the rewritten text actually made it back (cheap-pass path).
	for _, m := range res.Messages {
		for _, b := range m.Content {
			if strings.Contains(b.Text, "\x1b[") {
				t.Fatalf("ANSI codes leaked through: %q", b.Text)
			}
		}
	}
}

func TestCoordinatorSubsume_OffPreservesHeavyPasses(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	c := NewDeterministicCompressor(&cfg)
	body := strings.Repeat("repeated tool output line\n", 80)
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "u"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", Text: body}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", Text: body}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "next"}}},
	}
	c.cfg.SlidingWindow = 1
	res := c.Compress(msgs)
	if c.CoordinatorSkipped() != 0 {
		t.Fatal("counter must stay at zero when subsume is off")
	}
	if res.DedupSaved == 0 {
		t.Fatal("dedup should fire when coordinator is off")
	}
}

func TestCompressWithSessionOptions_RequestScopedCoordinator(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.SlidingWindow = 1
	body := strings.Repeat("repeated tool output line\n", 80)
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "u"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", Text: body}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", Text: body}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "next"}}},
	}

	legacyTrue := NewDeterministicCompressor(&cfg)
	legacyTrue.SetCoordinatorSubsume(true)
	res := legacyTrue.CompressWithSessionOptions("sess", msgs, Layer1CompressOptions{})
	if res.DedupSaved == 0 {
		t.Fatal("request-scoped options must not inherit legacy coordinator subsume")
	}

	scopedTrue := NewDeterministicCompressor(&cfg)
	res = scopedTrue.CompressWithSessionOptions("sess", msgs, Layer1CompressOptions{CoordinatorSubsume: true})
	if res.DedupSaved != 0 || scopedTrue.CoordinatorSkipped() == 0 {
		t.Fatalf("request-scoped coordinator subsume did not skip heavy passes: dedup=%d skipped=%d", res.DedupSaved, scopedTrue.CoordinatorSkipped())
	}
}

func TestCompressWithSessionOptions_ConcurrentSessionScope(t *testing.T) {
	cfg := config.Defaults().Compression
	cfg.SlidingWindow = 1
	rec := &sessionCapturingRecorder{}
	c := NewDeterministicCompressor(&cfg).WithRecorder(rec)
	body := "{\n" + strings.Repeat("    \"key\": \"value with lots of padding\",\n", 40) + "    \"last\": true\n}"
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "u"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", Text: body}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "next"}}},
	}

	var wg sync.WaitGroup
	for _, sessionID := range []string{"sess-A", "sess-B"} {
		wg.Add(1)
		go func(sessionID string) {
			defer wg.Done()
			c.CompressWithSessionOptions(sessionID, msgs, Layer1CompressOptions{CoordinatorSubsume: true})
		}(sessionID)
	}
	wg.Wait()

	got := rec.sessions()
	if len(got) != 2 {
		t.Fatalf("recorded sessions=%v, want two archive writes", got)
	}
	if got["sess-A"] != 1 || got["sess-B"] != 1 {
		t.Fatalf("archive session scope leaked across concurrent calls: %+v", got)
	}
}

type sessionCapturingRecorder struct {
	mu    sync.Mutex
	calls []contentarchive.Input
}

func (r *sessionCapturingRecorder) Record(in contentarchive.Input) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, in)
	return "stub-" + in.SessionID, nil
}

func (r *sessionCapturingRecorder) sessions() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]int, len(r.calls))
	for _, call := range r.calls {
		out[call.SessionID]++
	}
	return out
}

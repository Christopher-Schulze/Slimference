package compression

import (
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/contentarchive"
	"github.com/slimference/slimference/internal/types"
)

// TestJoinSubLayers covers all branches of the helper.
func TestJoinSubLayers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"empty falls back to layer1", nil, "layer1"},
		{"single tag", []string{"dedup"}, "dedup"},
		{"multiple tags", []string{"json_compact", "tool_compressor"}, "json_compact,tool_compressor"},
		{"three tags", []string{"comment_strip", "structure_extract", "delta"}, "comment_strip,structure_extract,delta"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := joinSubLayers(tc.in); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

// captureSubLayerRecorder is a shared archive sink that records every
// Record call so tests can assert on the T76b sub_layer tags.
type captureSubLayerRecorder struct {
	calls []contentarchive.Input
}

func (r *captureSubLayerRecorder) Record(in contentarchive.Input) (string, error) {
	r.calls = append(r.calls, in)
	return "stub-id-" + in.SubLayer, nil
}

// TestCompress_PerSubLayerAttribution_JSONCompact verifies the archive
// sub_layer tag carries the specific pass that mutated the block, not
// the generic "layer1" placeholder. T76b.
func TestCompress_PerSubLayerAttribution_JSONCompact(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	rec := &captureSubLayerRecorder{}
	c := NewDeterministicCompressor(cfg).WithRecorder(rec)

	jsonBody := "{\n" + strings.Repeat("  \"k\": \"v\",\n", 40) + "  \"last\": true\n}"
	msgs := []types.Message{
		buildMessage(t, 0, "user", toolResultBlock(jsonBody)),
		buildMessage(t, 1, "assistant", textBlock("ok")),
		buildMessage(t, 2, "user", textBlock("tail")),
	}
	c.Compress(msgs)

	if len(rec.calls) == 0 {
		t.Fatal("expected at least one archive call")
	}
	found := false
	for _, in := range rec.calls {
		if strings.Contains(in.SubLayer, "json_compact") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected sub_layer to mention json_compact, got tags %s",
			tagListOf(rec.calls))
	}
}

// TestCompress_PerSubLayerAttribution_DedupExact verifies the
// "dedup" sub-layer tag fires when a duplicated tool_result is
// recognised. T76b.
func TestCompress_PerSubLayerAttribution_DedupExact(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	rec := &captureSubLayerRecorder{}
	c := NewDeterministicCompressor(cfg).WithRecorder(rec)

	body := "{" + strings.Repeat("\"k\":\"v\",", 50) + "\"last\":true}"
	msgs := []types.Message{
		buildMessage(t, 0, "user", toolResultBlock(body)),
		buildMessage(t, 1, "assistant", textBlock("a")),
		buildMessage(t, 2, "user", toolResultBlock(body)),
		buildMessage(t, 3, "assistant", textBlock("b")),
		buildMessage(t, 4, "user", toolResultBlock(body)),
		buildMessage(t, 5, "assistant", textBlock("c")),
		buildMessage(t, 6, "user", textBlock("tail")),
	}
	c.Compress(msgs)

	if !anySubLayer(rec.calls, "dedup") &&
		!anyContains(rec.calls, "dedup") {
		t.Fatalf("expected dedup sub-layer tag, got %s",
			tagListOf(rec.calls))
	}
}

// TestCompress_PerSubLayerAttribution_CoordinatorSubsumeFallback
// verifies that the coordinator-subsume path keeps the legacy "layer1"
// sub-layer tag (unattributed) since heavy passes are skipped.
func TestCompress_PerSubLayerAttribution_CoordinatorSubsumeFallback(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	rec := &captureSubLayerRecorder{}
	c := NewDeterministicCompressor(cfg).WithRecorder(rec)
	c.SetCoordinatorSubsume(true)

	jsonBody := "{\n" + strings.Repeat("  \"k\": \"v\",\n", 40) + "  \"last\": true\n}"
	msgs := []types.Message{
		buildMessage(t, 0, "user", toolResultBlock(jsonBody)),
		buildMessage(t, 1, "assistant", textBlock("ok")),
		buildMessage(t, 2, "user", textBlock("tail")),
	}
	c.Compress(msgs)

	if !anySubLayer(rec.calls, "layer1") {
		t.Fatalf("coordinator subsume must keep coarse layer1 tag, got %s",
			tagListOf(rec.calls))
	}
}

func anySubLayer(calls []contentarchive.Input, want string) bool {
	for _, c := range calls {
		if c.SubLayer == want {
			return true
		}
	}
	return false
}

func anyContains(calls []contentarchive.Input, sub string) bool {
	for _, c := range calls {
		if strings.Contains(c.SubLayer, sub) {
			return true
		}
	}
	return false
}

func tagListOf(calls []contentarchive.Input) string {
	parts := make([]string, 0, len(calls))
	for _, c := range calls {
		parts = append(parts, c.SubLayer)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

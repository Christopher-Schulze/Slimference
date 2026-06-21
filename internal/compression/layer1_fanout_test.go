package compression

import (
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/contentarchive"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func TestShouldUseCoordinatorParallelAutoGate(t *testing.T) {
	t.Parallel()
	small := []types.Message{
		buildMessage(t, 0, "user", toolResultBlock("small")),
		buildMessage(t, 1, "assistant", textBlock("ok")),
		buildMessage(t, 2, "user", textBlock("tail")),
	}
	if shouldUseCoordinatorParallel(false, small, 2) {
		t.Fatal("disabled coordinator must not fan out")
	}
	if shouldUseCoordinatorParallel(true, small, 1) {
		t.Fatal("single-prefix message must stay sequential")
	}
	if shouldUseCoordinatorParallel(true, small, 2) {
		t.Fatal("tiny prefix must stay sequential to avoid goroutine overhead")
	}

	large := []types.Message{
		buildMessage(t, 0, "user", toolResultBlock(strings.Repeat("large ", 1500))),
		buildMessage(t, 1, "assistant", textBlock("ok")),
		buildMessage(t, 2, "user", textBlock("tail")),
	}
	if !shouldUseCoordinatorParallel(true, large, 2) {
		t.Fatal("large prefix should use coordinator fan-out")
	}

	many := make([]types.Message, runtime.GOMAXPROCS(0)*2)
	for i := range many {
		many[i] = buildMessage(t, i, "user", toolResultBlock("x"))
	}
	if !shouldUseCoordinatorParallel(true, many, len(many)) {
		t.Fatal("many prefix messages should use coordinator fan-out")
	}
}

func TestCompress_ParallelFanOut_CompressesAllMessages(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	cfg.Tuning.CoordinatorParallel = true
	c := NewDeterministicCompressor(cfg)

	msgs := []types.Message{
		buildMessage(t, 0, "user", toolResultBlock("{\n  \"a\": \""+strings.Repeat("x", 100)+"\"\n}")),
		buildMessage(t, 1, "assistant", textBlock("ok")),
		buildMessage(t, 2, "user", toolResultBlock("\x1b[31m"+strings.Repeat("beta ", 100)+"\x1b[0m")),
		buildMessage(t, 3, "assistant", textBlock("ok")),
		buildMessage(t, 4, "user", toolResultBlock("{\n  \"c\": \""+strings.Repeat("y", 100)+"\"\n}")),
		buildMessage(t, 5, "assistant", textBlock("ok")),
		buildMessage(t, 6, "user", textBlock("latest")),
	}

	result := c.Compress(msgs)
	if result.TokensSaved <= 0 {
		t.Fatalf("expected savings with parallel fan-out, got %d", result.TokensSaved)
	}
	if len(result.Messages) != len(msgs) {
		t.Fatalf("message count: got %d want %d", len(result.Messages), len(msgs))
	}
}

func TestCompress_ParallelFanOff_SameAsSequential(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	c := NewDeterministicCompressor(cfg)

	msgs := []types.Message{
		buildMessage(t, 0, "user", toolResultBlock(strings.Repeat("unique-a ", 80))),
		buildMessage(t, 1, "assistant", textBlock("r1")),
		buildMessage(t, 2, "user", toolResultBlock(strings.Repeat("unique-b ", 80))),
		buildMessage(t, 3, "assistant", textBlock("r2")),
		buildMessage(t, 4, "user", textBlock("tail")),
	}

	seq := c.Compress(msgs)

	cfg2 := defaultTestCfg(1)
	cfg2.Tuning.CoordinatorParallel = true
	c2 := NewDeterministicCompressor(cfg2)
	par := c2.Compress(msgs)

	if seq.TokensSaved != par.TokensSaved {
		t.Fatalf("sequential saved %d, parallel saved %d", seq.TokensSaved, par.TokensSaved)
	}
}

func TestCompress_ParallelFanOut_DedupAcrossMessages(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	cfg.Tuning.CoordinatorParallel = true
	c := NewDeterministicCompressor(cfg)

	content := `{"result":"identical for dedup","x":1}`
	msgs := []types.Message{
		buildMessage(t, 0, "user", toolResultBlock(content)),
		buildMessage(t, 1, "assistant", textBlock("a")),
		buildMessage(t, 2, "user", toolResultBlock(content)),
		buildMessage(t, 3, "assistant", textBlock("b")),
		buildMessage(t, 4, "user", toolResultBlock(content)),
		buildMessage(t, 5, "assistant", textBlock("c")),
		buildMessage(t, 6, "user", textBlock("tail")),
	}

	result := c.Compress(msgs)
	if result.DedupSaved <= 0 {
		t.Fatalf("dedup must fire in parallel mode; got dedupSaved=%d", result.DedupSaved)
	}
}

func TestCompress_ParallelFanOut_SingleMessage_FallsBack(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	cfg.Tuning.CoordinatorParallel = true
	c := NewDeterministicCompressor(cfg)

	msgs := []types.Message{
		buildMessage(t, 0, "user", toolResultBlock(strings.Repeat("x ", 50))),
		buildMessage(t, 1, "assistant", textBlock("ok")),
		buildMessage(t, 2, "user", textBlock("tail")),
	}

	result := c.Compress(msgs)
	if len(result.Messages) != 3 {
		t.Fatalf("got %d messages, want 3", len(result.Messages))
	}
}

func TestCompress_ParallelFanOut_CoordinatorSubsume(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	cfg.Tuning.CoordinatorParallel = true
	c := NewDeterministicCompressor(cfg)

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
		t.Fatal("coordinator skip counter must advance even in parallel mode")
	}
	if res.DedupSaved != 0 {
		t.Fatalf("dedup must be skipped under subsume; got dedupSaved=%d", res.DedupSaved)
	}
}

type concurrentRecorder struct {
	calls atomic.Int64
}

func (r *concurrentRecorder) Record(_ contentarchive.Input) (string, error) {
	r.calls.Add(1)
	return "stub-id", nil
}

func TestCompress_ParallelFanOut_RecorderConcurrentSafe(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	cfg.Tuning.CoordinatorParallel = true
	rec := &concurrentRecorder{}
	c := NewDeterministicCompressor(cfg).WithRecorder(rec)

	jsonBody := "{\n" + strings.Repeat("  \"k\": \"v\",\n", 40) + "  \"last\": true\n}"
	msgs := make([]types.Message, 10)
	for i := range 9 {
		msgs[i] = buildMessage(t, i, "user", toolResultBlock(jsonBody))
	}
	msgs[9] = buildMessage(t, 9, "user", textBlock("tail"))

	c.Compress(msgs)
	if rec.calls.Load() == 0 {
		t.Fatal("recorder should have been called for JSON-compact mutations")
	}
}

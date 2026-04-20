package compression

import (
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/types"
)

// buildUniformMsgs returns n user messages each with `big`-sized text so the
// stable-prefix token threshold is cleared.
func buildUniformMsgs(n int, big string) []types.Message {
	msgs := make([]types.Message, n)
	for i := range msgs {
		msgs[i] = types.Message{
			Role:    "user",
			Content: []types.ContentBlock{{Type: "text", Text: big}},
		}
	}
	return msgs
}

// breakpointIndices returns the sorted list of message indices that carry at
// least one cache_control marker.
func breakpointIndices(msgs []types.Message) []int {
	var out []int
	for i, m := range msgs {
		for _, b := range m.Content {
			if b.CacheControl != nil {
				out = append(out, i)
				break
			}
		}
	}
	return out
}

func TestT45_BreakpointsSpreadEvenly(t *testing.T) {
	big := strings.Repeat("x", 2500)
	msgs := buildUniformMsgs(20, big)
	out := OptimizeCacheBreakpoints(msgs, 20)

	got := breakpointIndices(out)
	// Spread-evenly formula: indices (len*k/N)-1 for k in 1..N=4
	// len=20, N=4 -> 4, 9, 14, 19
	want := []int{4, 9, 14, 19}
	if len(got) != len(want) {
		t.Fatalf("bp count = %d, want %d (got indices %v)", len(got), len(want), got)
	}
	for i, idx := range got {
		if idx != want[i] {
			t.Errorf("bp[%d] = %d, want %d (all=%v)", i, idx, want[i], got)
		}
	}
}

func TestT45_BreakpointsFewerThanMax(t *testing.T) {
	// 3 eligible messages; should get 3 breakpoints (not capped).
	big := strings.Repeat("x", 4100) // each message alone passes the token floor
	msgs := buildUniformMsgs(3, big)
	out := OptimizeCacheBreakpoints(msgs, 3)

	got := breakpointIndices(out)
	if len(got) != 3 {
		t.Fatalf("bp count = %d, want 3 (one per message)", len(got))
	}
}

func TestT45_NeverExceedsMax(t *testing.T) {
	big := strings.Repeat("y", 2000)
	msgs := buildUniformMsgs(50, big)
	out := OptimizeCacheBreakpoints(msgs, 50)
	got := breakpointIndices(out)
	if len(got) > maxCacheBreakpoints {
		t.Fatalf("bp count = %d, want <= %d", len(got), maxCacheBreakpoints)
	}
	if len(got) != maxCacheBreakpoints {
		t.Fatalf("bp count = %d, want exactly %d (enough eligible)", len(got), maxCacheBreakpoints)
	}
}

func TestT45_SkipsEmptyContent(t *testing.T) {
	big := strings.Repeat("z", 2000)
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: big}}},
		{Role: "assistant"}, // empty content -> not eligible
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: big}}},
		{Role: "assistant"}, // empty
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: big}}},
	}
	out := OptimizeCacheBreakpoints(msgs, 5)
	got := breakpointIndices(out)
	// All 3 eligible messages should carry a breakpoint (< max).
	if len(got) != 3 {
		t.Fatalf("bp count = %d, want 3 (only eligible slots)", len(got))
	}
	for _, idx := range got {
		if len(msgs[idx].Content) == 0 {
			t.Errorf("breakpoint on empty msg at %d", idx)
		}
	}
}

func TestT45_StabilityAcrossRuns(t *testing.T) {
	// Same input -> same breakpoint positions -> cache key stable.
	big := strings.Repeat("w", 2500)
	msgs := buildUniformMsgs(16, big)

	run1 := breakpointIndices(OptimizeCacheBreakpoints(msgs, 16))
	run2 := breakpointIndices(OptimizeCacheBreakpoints(msgs, 16))
	if len(run1) != len(run2) {
		t.Fatalf("non-stable count: %v vs %v", run1, run2)
	}
	for i := range run1 {
		if run1[i] != run2[i] {
			t.Fatalf("non-stable placement at %d: %d vs %d (run1=%v run2=%v)",
				i, run1[i], run2[i], run1, run2)
		}
	}
}

func TestT45_CounterObservesInjections(t *testing.T) {
	ResetPromptCacheBreakpointsCounter()
	big := strings.Repeat("m", 2100)
	msgs := buildUniformMsgs(16, big)
	_ = OptimizeCacheBreakpoints(msgs, 16)
	got := PromptCacheBreakpointsInjected()
	if got != int64(maxCacheBreakpoints) {
		t.Fatalf("counter = %d, want %d", got, maxCacheBreakpoints)
	}

	// Second call adds to the counter.
	_ = OptimizeCacheBreakpoints(msgs, 16)
	got = PromptCacheBreakpointsInjected()
	if got != int64(2*maxCacheBreakpoints) {
		t.Fatalf("counter after 2nd call = %d, want %d", got, 2*maxCacheBreakpoints)
	}
}

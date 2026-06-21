package types

import (
	"sync"
	"testing"
)

func TestProviderString(t *testing.T) {
	t.Parallel()
	if Anthropic.String() != "anthropic" || OpenAI.String() != "openai" || CodexChatGPT.String() != "codex_chatgpt" {
		t.Fatalf("anthropic=%q openai=%q codex=%q", Anthropic.String(), OpenAI.String(), CodexChatGPT.String())
	}
	if Provider(99).String() != "unknown" {
		t.Fatalf("got %q", Provider(99).String())
	}
}

func TestMessage_TextContent(t *testing.T) {
	t.Parallel()
	m := Message{
		Content: []ContentBlock{
			{Type: "text", Text: "a"},
			{Type: "text", Text: "b"},
			{Type: "tool_use", Text: "ignored"},
		},
	}
	if got := m.TextContent(); got != "a\nb" {
		t.Fatalf("got %q", got)
	}
}

func TestMessage_HasToolUseAndResult(t *testing.T) {
	t.Parallel()
	use := Message{Content: []ContentBlock{{Type: "tool_use", ToolName: "x"}}}
	if !use.HasToolUse() || use.HasToolResult() {
		t.Fatal("tool_use")
	}
	res := Message{Content: []ContentBlock{{Type: "tool_result", Text: "z"}}}
	if !res.HasToolResult() || res.HasToolUse() {
		t.Fatal("tool_result")
	}
}

// TestRingBuffer_PushAndLast verifies that items pushed are retrievable via Last.
func TestRingBuffer_PushAndLast(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cap      int
		pushVals []int
		lastN    int
		wantLen  int
		wantVals []int
	}{
		{
			name:     "single push last 1",
			cap:      5,
			pushVals: []int{10},
			lastN:    1,
			wantLen:  1,
			wantVals: []int{10},
		},
		{
			name:     "three pushes last 2",
			cap:      5,
			pushVals: []int{1, 2, 3},
			lastN:    2,
			wantLen:  2,
			wantVals: []int{2, 3},
		},
		{
			name:     "last N larger than size returns all",
			cap:      5,
			pushVals: []int{7, 8},
			lastN:    10,
			wantLen:  2,
			wantVals: []int{7, 8},
		},
		{
			name:     "last 0 returns empty",
			cap:      5,
			pushVals: []int{1, 2, 3},
			lastN:    0,
			wantLen:  0,
			wantVals: []int{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rb := NewRingBuffer[int](tc.cap)
			for _, v := range tc.pushVals {
				rb.Push(v)
			}
			got := rb.Last(tc.lastN)
			if len(got) != tc.wantLen {
				t.Fatalf("Last(%d) returned %d items, want %d", tc.lastN, len(got), tc.wantLen)
			}
			for i, v := range tc.wantVals {
				if got[i] != v {
					t.Errorf("got[%d] = %d, want %d", i, got[i], v)
				}
			}
		})
	}
}

// TestRingBuffer_Overflow verifies that overflow wraps around and overwrites oldest items.
func TestRingBuffer_Overflow(t *testing.T) {
	t.Parallel()

	rb := NewRingBuffer[int](3)
	// Push 5 items into cap-3 buffer; oldest two (1,2) should be gone.
	for i := 1; i <= 5; i++ {
		rb.Push(i)
	}

	if rb.Len() != 3 {
		t.Fatalf("Len() = %d, want 3 after overflow", rb.Len())
	}

	got := rb.Last(3)
	want := []int{3, 4, 5}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("got[%d] = %d, want %d", i, got[i], w)
		}
	}
}

// TestRingBuffer_Concurrent exercises Push and Last from multiple goroutines concurrently.
func TestRingBuffer_Concurrent(t *testing.T) {
	t.Parallel()

	rb := NewRingBuffer[int](64)
	const goroutines = 20
	const pushesPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := range goroutines {
		go func() {
			defer wg.Done()
			for i := range pushesPerGoroutine {
				rb.Push(g*1000 + i)
				_ = rb.Last(5)
			}
		}()
	}
	wg.Wait()

	// After all writes the buffer should have exactly cap items (capacity 64).
	if rb.Len() != 64 {
		t.Errorf("Len() = %d, want 64 after concurrent pushes", rb.Len())
	}
}

// TestRingBuffer_Len verifies Len at various fill levels.
func TestRingBuffer_Len(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cap     int
		pushN   int
		wantLen int
	}{
		{"empty", 5, 0, 0},
		{"half full", 5, 2, 2},
		{"exactly full", 5, 5, 5},
		{"overflowed", 5, 9, 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rb := NewRingBuffer[int](tc.cap)
			for i := 0; i < tc.pushN; i++ {
				rb.Push(i)
			}
			if got := rb.Len(); got != tc.wantLen {
				t.Errorf("Len() = %d, want %d", got, tc.wantLen)
			}
		})
	}
}

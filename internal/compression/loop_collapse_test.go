package compression

import (
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/types"
)

func makeLoopMessages(streakLen int) []types.Message {
	msgs := make([]types.Message, 0, streakLen*2+2)
	msgs = append(msgs, types.Message{Index: 0, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "initial response"}}})
	for i := 0; i < streakLen; i++ {
		msgs = append(msgs, types.Message{
			Index: len(msgs),
			Role:  "user",
			Content: []types.ContentBlock{
				{Type: "text", Text: strings.Repeat("please fix the build error in main.go the tests are failing ", 3)},
			},
		})
		msgs = append(msgs, types.Message{
			Index: len(msgs),
			Role:  "assistant",
			Content: []types.ContentBlock{
				{Type: "text", Text: "I fixed the error"},
			},
		})
	}
	return msgs
}

func TestCollapseLoopMessages_Streak(t *testing.T) {
	msgs := makeLoopMessages(5)
	out, saved := CollapseLoopMessages(msgs)
	if saved <= 0 {
		t.Fatal("should save tokens on collapse")
	}
	collapsedCount := 0
	for _, m := range out {
		for _, b := range m.Content {
			if strings.Contains(b.Text, "Near-duplicate") {
				collapsedCount++
			}
		}
	}
	if collapsedCount == 0 {
		t.Fatal("expected at least one collapsed message")
	}
	for i, m := range out {
		if m.Index != i {
			t.Fatalf("index mismatch at %d: got %d", i, m.Index)
		}
	}
}

func TestCollapseLoopMessages_BrokenStreak(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "please fix the build error in main.go the tests are failing again and again"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "please fix the build error in main.go the tests are failing again and again"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "please fix the build error in main.go the tests are failing again and again"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "please fix the build error in main.go the tests are failing again and again"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "completely different message about something else entirely no overlap here"}}},
	}
	out, saved := CollapseLoopMessages(msgs)
	if saved <= 0 {
		t.Fatal("should collapse the first streak")
	}
	_ = out
}

func TestCollapseLoopMessages_NoStreakAfterBreak(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "completely different message about something else"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "another different message with unique content here"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "yet another different message with no overlap"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "fourth unique message without any similarity"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "fifth completely different text in the sequence"}}},
	}
	out, saved := CollapseLoopMessages(msgs)
	if saved != 0 {
		t.Fatalf("no streak: saved=%d", saved)
	}
	if len(out) != 5 {
		t.Fatal("should pass through unchanged")
	}
}

func TestCollapseLoopMessages_NoStreak(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "hello world this is a test"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "response"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "different message entirely"}}},
	}
	out, saved := CollapseLoopMessages(msgs)
	if saved != 0 {
		t.Fatalf("no streak: saved=%d want 0", saved)
	}
	if len(out) != len(msgs) {
		t.Fatal("messages should be unchanged")
	}
}

func TestResolveLoopStrategy_Additive(t *testing.T) {
	r := ResolveLoopStrategy(StrategyConfig{LoopDetection: true, LoopStrategy: "additive"})
	if r.Strategy != "additive" {
		t.Fatalf("got %q", r.Strategy)
	}
}

func TestResolveLoopStrategy_Subtractive(t *testing.T) {
	r := ResolveLoopStrategy(StrategyConfig{LoopDetection: true, LoopStrategy: "subtractive"})
	if r.Strategy != "subtractive" {
		t.Fatalf("got %q", r.Strategy)
	}
}

func TestResolveLoopStrategy_Off(t *testing.T) {
	r := ResolveLoopStrategy(StrategyConfig{LoopDetection: true, LoopStrategy: "off"})
	if r.Strategy != "off" {
		t.Fatalf("got %q", r.Strategy)
	}
	msgs := []types.Message{{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "test"}}}}
	out, saved := r.Apply(msgs)
	if saved != 0 {
		t.Fatal("off strategy should save 0")
	}
	if len(out) != 1 {
		t.Fatal("off should pass through")
	}
}

func TestResolveLoopStrategy_DefaultIsAdditive(t *testing.T) {
	r := ResolveLoopStrategy(StrategyConfig{LoopDetection: true, LoopStrategy: ""})
	if r.Strategy != "additive" {
		t.Fatalf("default should be additive, got %q", r.Strategy)
	}
}

func TestApplyOff(t *testing.T) {
	msgs := []types.Message{{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "hello"}}}}
	out, saved := applyOff(msgs)
	if saved != 0 {
		t.Fatal("should save 0")
	}
	if len(out) != 1 {
		t.Fatal("should pass through")
	}
}

func TestCollectUserIndices(t *testing.T) {
	msgs := []types.Message{
		{Role: "user"},
		{Role: "assistant"},
		{Role: "user"},
	}
	idx := collectUserIndices(msgs)
	if len(idx) != 2 || idx[0] != 0 || idx[1] != 2 {
		t.Fatalf("got %v", idx)
	}
}

func TestCollapseLoopMessages_TrailingStreak(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "please fix the build error in main.go the tests are failing again and again"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "fixed"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "please fix the build error in main.go the tests are failing again and again"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "fixed again"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "please fix the build error in main.go the tests are failing again and again"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "fixed once more"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "please fix the build error in main.go the tests are failing again and again"}}},
	}
	out, saved := CollapseLoopMessages(msgs)
	if saved <= 0 {
		t.Fatal("should save tokens on trailing streak")
	}
	collapsedCount := 0
	for _, m := range out {
		for _, b := range m.Content {
			if strings.Contains(b.Text, "Near-duplicate") {
				collapsedCount++
			}
		}
	}
	if collapsedCount == 0 {
		t.Fatal("expected collapsed messages")
	}
	_ = out
}

func TestApplySubtractive(t *testing.T) {
	msgs := makeLoopMessages(5)
	out, saved := applySubtractive(msgs)
	if saved <= 0 {
		t.Fatal("should save tokens")
	}
	_ = out
}

func TestFirstStreakUserIdx_Empty(t *testing.T) {
	got := firstStreakUserIdx(nil, nil, 42)
	if got != 42 {
		t.Fatalf("empty streaks: got %d want 42", got)
	}
}

func TestMsgTextLen(t *testing.T) {
	m := types.Message{Content: []types.ContentBlock{
		{Type: "text", Text: "hello"},
		{Type: "text", Text: " world"},
	}}
	if msgTextLen(m) != 11 {
		t.Fatalf("got %d", msgTextLen(m))
	}
}

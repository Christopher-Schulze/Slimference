package proxy

import (
	"testing"

	"github.com/slimference/slimference/internal/toolprune"
	"github.com/slimference/slimference/internal/types"
)

// TestMessageMentionsAnyPrunedTool_NilTracker covers the early-return
// guard when no usage tracker is present.
func TestMessageMentionsAnyPrunedTool_NilTracker(t *testing.T) {
	t.Parallel()
	out := messageMentionsAnyPrunedTool(nil, nil, "session-id")
	if out != nil {
		t.Fatalf("expected nil, got %v", out)
	}
}

// TestMessageMentionsAnyPrunedTool_EmptySession covers the early-return
// guard when the session id is empty.
func TestMessageMentionsAnyPrunedTool_EmptySession(t *testing.T) {
	t.Parallel()
	tracker := toolprune.NewUsageTracker(20)
	out := messageMentionsAnyPrunedTool(nil, tracker, "")
	if out != nil {
		t.Fatalf("expected nil, got %v", out)
	}
}

// TestMessageMentionsAnyPrunedTool_NoCandidates covers the path where
// the tracker has no pruned tools recorded for this session.
func TestMessageMentionsAnyPrunedTool_NoCandidates(t *testing.T) {
	t.Parallel()
	tracker := toolprune.NewUsageTracker(20)
	msgs := []types.Message{{Content: []types.ContentBlock{{Type: "text", Text: "hello"}}}}
	out := messageMentionsAnyPrunedTool(msgs, tracker, "fresh-session")
	if out != nil {
		t.Fatalf("expected nil, got %v", out)
	}
}

func TestMessageMentionsAnyPrunedTool_UserInstructionOnly(t *testing.T) {
	t.Parallel()
	tracker := toolprune.NewUsageTracker(20)
	tracker.RememberPrunedDef("session-id", "GetWeather", []byte(`{"name":"GetWeather"}`))

	historyOnly := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "I may need GetWeather later"}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "text", Text: "GetWeather unavailable in this log"}}},
	}
	if got := messageMentionsAnyPrunedTool(historyOnly, tracker, "session-id"); got != nil {
		t.Fatalf("historical assistant/tool text must not reattach, got %v", got)
	}

	userIntent := append(historyOnly, types.Message{
		Role:    "user",
		Content: []types.ContentBlock{{Type: "text", Text: "please check the weather now"}},
	})
	got := messageMentionsAnyPrunedTool(userIntent, tracker, "session-id")
	if len(got) != 1 || got[0] != "GetWeather" {
		t.Fatalf("user intent should reattach GetWeather, got %v", got)
	}
}

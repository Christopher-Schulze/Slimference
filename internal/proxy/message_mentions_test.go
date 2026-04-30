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

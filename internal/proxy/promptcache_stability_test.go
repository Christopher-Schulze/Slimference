package proxy

import (
	"testing"

	"github.com/slimference/slimference/internal/promptcache"
	"github.com/slimference/slimference/internal/types"
)

func TestObservePromptCacheStability_NilProxy(t *testing.T) {
	var p *Proxy
	o := p.observePromptCacheStability("s", nil, 0)
	if o.Confidence != promptcache.ConfidenceCold || o.HitCount != 0 {
		t.Fatalf("nil proxy must return zero observation, got %+v", o)
	}
}

func TestObservePromptCacheStability_NilTracker(t *testing.T) {
	p := &Proxy{}
	o := p.observePromptCacheStability("s", nil, 0)
	if o.Confidence != promptcache.ConfidenceCold || o.HitCount != 0 {
		t.Fatalf("proxy without tracker must return zero observation, got %+v", o)
	}
}

func TestObservePromptCacheStability_EmptySession(t *testing.T) {
	p := &Proxy{promptCacheStability: promptcache.NewTracker(10, 0)}
	o := p.observePromptCacheStability("", nil, 0)
	if o.Confidence != promptcache.ConfidenceCold {
		t.Fatalf("empty session must yield cold, got %+v", o)
	}
}

func TestObservePromptCacheStability_StreakProducesHot(t *testing.T) {
	p := &Proxy{promptCacheStability: promptcache.NewTracker(10, 0)}
	msgs := []types.Message{
		{Role: "system", Content: []types.ContentBlock{{Type: "text", Text: "you are helpful"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "hi"}}},
	}
	first := p.observePromptCacheStability("s1", msgs, len(msgs))
	if first.Confidence != promptcache.ConfidenceCold {
		t.Fatalf("first observation must be cold, got %+v", first)
	}
	second := p.observePromptCacheStability("s1", msgs, len(msgs))
	if second.Confidence != promptcache.ConfidenceHot {
		t.Fatalf("second identical observation must be hot, got %+v", second)
	}
}

func TestObservePromptCacheStability_ChangedPrefixResets(t *testing.T) {
	p := &Proxy{promptCacheStability: promptcache.NewTracker(10, 0)}
	a := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "alpha"}}},
	}
	b := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "beta"}}},
	}
	_ = p.observePromptCacheStability("s", a, 1)
	_ = p.observePromptCacheStability("s", a, 1)
	o := p.observePromptCacheStability("s", b, 1)
	if o.Confidence != promptcache.ConfidenceCold || o.HitCount != 1 {
		t.Fatalf("changed prefix must reset hit count, got %+v", o)
	}
}

func TestHashStablePrefix_StableAcrossCalls(t *testing.T) {
	msgs := []types.Message{
		{Role: "system", Content: []types.ContentBlock{{Type: "text", Text: "be brief"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "Read", ToolInput: `{"path":"x"}`, ToolUseID: "1"}}},
	}
	a := hashStablePrefix(msgs, 2)
	b := hashStablePrefix(msgs, 2)
	if a != b {
		t.Fatalf("hash must be stable across calls")
	}
}

func TestHashStablePrefix_BoundarySensitive(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "a"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "b"}}},
	}
	if hashStablePrefix(msgs, 1) == hashStablePrefix(msgs, 2) {
		t.Fatalf("hash must depend on boundary index")
	}
}

func TestHashStablePrefix_FieldDiscrimination(t *testing.T) {
	// Tool name vs text with same payload must hash differently.
	withText := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "xxx"}}},
	}
	withTool := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "xxx"}}},
	}
	if hashStablePrefix(withText, 1) == hashStablePrefix(withTool, 1) {
		t.Fatalf("text and tool-name fields must produce distinct hashes")
	}
}

func TestHashStablePrefix_LengthDelimitedNoCollision(t *testing.T) {
	// "ab"+"" vs "a"+"b" — without length delimiting these would collide.
	a := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "ab"}}},
	}
	b := []types.Message{
		{Role: "user", Content: []types.ContentBlock{
			{Type: "text", Text: "a"},
			{Type: "text", Text: "b"},
		}},
	}
	if hashStablePrefix(a, 1) == hashStablePrefix(b, 1) {
		t.Fatalf("split-content collision: length delimiting failed")
	}
}

func TestHashStablePrefix_BoundaryClampsToLen(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "x"}}},
	}
	// stableBoundary > len(messages) must not panic; the function
	// clamps the loop to len. After clamping both inputs produce the
	// same hash (boundary=1 effective on both calls).
	a := hashStablePrefix(msgs, 5)
	b := hashStablePrefix(msgs, 1)
	if a != b {
		t.Fatalf("boundary above len must clamp to len and produce identical hash")
	}
}

func TestHashStablePrefix_EmptyMessages(t *testing.T) {
	// Empty input with any boundary must not panic and must produce
	// a stable hash.
	a := hashStablePrefix(nil, 0)
	b := hashStablePrefix(nil, 0)
	if a != b {
		t.Fatalf("empty input hash must be deterministic")
	}
}

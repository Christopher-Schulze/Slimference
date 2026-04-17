package caching

import (
	"testing"
	"time"

	"github.com/slimference/slimference/internal/types"
)

func TestResponseCache_ComputeRequestKey_canonicalizesFullBody(t *testing.T) {
	t.Parallel()

	cache := NewResponseCache(10, time.Minute)

	bodyA := []byte(`{
	  "messages":[{"content":"hello","role":"user"}],
	  "model":"claude-3-5-sonnet-20241022",
	  "system":"keep system",
	  "tools":[{"name":"Read","input_schema":{"properties":{"path":{"type":"string"}},"type":"object"}}]
	}`)
	bodyB := []byte(`{"tools":[{"input_schema":{"type":"object","properties":{"path":{"type":"string"}}},"name":"Read"}],"system":"keep system","model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":"hello"}]}`)
	bodyC := []byte(`{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":"hello"}],"system":"changed","tools":[{"name":"Read","input_schema":{"properties":{"path":{"type":"string"}},"type":"object"}}]}`)
	bodyD := []byte(`{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":"hello"}],"system":"keep system","tools":[{"name":"Read","input_schema":{"properties":{"path":{"type":"number"}},"type":"object"}}]}`)

	keyA := cache.ComputeRequestKey(types.Anthropic, bodyA)
	keyB := cache.ComputeRequestKey(types.Anthropic, bodyB)
	keyC := cache.ComputeRequestKey(types.Anthropic, bodyC)
	keyD := cache.ComputeRequestKey(types.Anthropic, bodyD)
	keyOpenAI := cache.ComputeRequestKey(types.OpenAI, bodyA)

	if keyA != keyB {
		t.Fatal("semantically identical request bodies should produce the same cache key")
	}
	if keyA == keyC {
		t.Fatal("changing system prompt must change the cache key")
	}
	if keyA == keyD {
		t.Fatal("changing tool schema must change the cache key")
	}
	if keyA == keyOpenAI {
		t.Fatal("provider must participate in the cache key")
	}
}

func TestExtractDependencyPaths_scansWholeBody(t *testing.T) {
	t.Parallel()

	body := []byte(`{
	  "system":"Read docs/spec+.md before editing internal/proxy/handler.go",
	  "messages":[
	    {"role":"user","content":"Open internal/hooks/claude.go and src/app.tsx"},
	    {"role":"assistant","content":[{"type":"text","text":"Check ./scripts/coverage/main.go and ../shared/types.ts too"}]}
	  ]
	}`)

	paths := ExtractDependencyPaths(body)
	want := map[string]bool{
		"docs/spec+.md":              true,
		"internal/proxy/handler.go":  true,
		"internal/hooks/claude.go":   true,
		"src/app.tsx":                true,
		"./scripts/coverage/main.go": false,
		"scripts/coverage/main.go":   true,
		"../shared/types.ts":         true,
	}

	got := make(map[string]bool, len(paths))
	for _, path := range paths {
		got[path] = true
	}

	for path, expected := range want {
		if got[path] != expected {
			t.Fatalf("path %q present=%v want=%v; all=%v", path, got[path], expected, paths)
		}
	}
}

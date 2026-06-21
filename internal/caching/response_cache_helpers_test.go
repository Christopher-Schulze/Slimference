package caching

import "testing"

func TestCanonicalizeJSON_InvalidInputFallsBackToTrimmedText(t *testing.T) {
	t.Parallel()

	got := string(canonicalizeJSON([]byte("  not-json  ")))
	if got != "not-json" {
		t.Fatalf("canonicalizeJSON fallback: got %q", got)
	}
}

func TestExtractDependencyHelpers(t *testing.T) {
	t.Parallel()

	paths := extractDependencyPathsFromString("edit src/main.go then src/main.go and ./pkg/handler_test.go")
	if len(paths) != 2 {
		t.Fatalf("expected deduped dependency paths, got %#v", paths)
	}

	if normalizeDependencyPath("   ") != "" {
		t.Fatal("empty path should normalize to empty string")
	}
	if normalizeDependencyPath("/") != "" {
		t.Fatal("root path should normalize to empty string")
	}

	entry := &CacheEntry{DependencyPaths: []string{"src/main.go", "/Users/me/repo/internal/app.go"}}
	if !cacheEntryDependsOnPath(entry, "/Users/me/repo/src/main.go") {
		t.Fatal("absolute changed path should match stored relative dependency")
	}
	if !cacheEntryDependsOnPath(entry, "internal/app.go") {
		t.Fatal("relative changed path should match stored absolute dependency suffix")
	}
	if cacheEntryDependsOnPath(entry, "docs/readme.md") {
		t.Fatal("unrelated path should not match dependencies")
	}
}

func TestNonEmptyJSONValue(t *testing.T) {
	t.Parallel()
	if nonEmptyJSONValue(nil) {
		t.Fatal("nil should be false")
	}
	if nonEmptyJSONValue("") {
		t.Fatal("empty string should be false")
	}
	if nonEmptyJSONValue("  ") {
		t.Fatal("whitespace string should be false")
	}
	if nonEmptyJSONValue("none") {
		t.Fatal("\"none\" should be false")
	}
	if nonEmptyJSONValue("valid") != true {
		t.Fatal("\"valid\" should be true")
	}
	if nonEmptyJSONValue([]any{}) {
		t.Fatal("empty array should be false")
	}
	if nonEmptyJSONValue([]any{"x"}) != true {
		t.Fatal("non-empty array should be true")
	}
	if nonEmptyJSONValue(map[string]any{}) {
		t.Fatal("empty map should be false")
	}
	if nonEmptyJSONValue(map[string]any{"a": 1}) != true {
		t.Fatal("non-empty map should be true")
	}
	if nonEmptyJSONValue(42) != true {
		t.Fatal("number should be true")
	}
}

func TestContainsToolRole(t *testing.T) {
	t.Parallel()
	// nil -> false.
	if containsToolRole(nil) {
		t.Fatal("nil should be false")
	}
	// String -> false.
	if containsToolRole("hello") {
		t.Fatal("string should be false")
	}
	// Array with tool role message.
	if !containsToolRole([]any{map[string]any{"role": "tool", "content": "result"}}) {
		t.Fatal("array with tool role should be true")
	}
	// Array with function role.
	if !containsToolRole([]any{map[string]any{"role": "function", "content": "result"}}) {
		t.Fatal("array with function role should be true")
	}
	// Array with tool_call type.
	if !containsToolRole([]any{map[string]any{"type": "tool_call"}}) {
		t.Fatal("array with tool_call type should be true")
	}
	// Array with function_call_output type.
	if !containsToolRole([]any{map[string]any{"type": "function_call_output"}}) {
		t.Fatal("array with function_call_output type should be true")
	}
	// Array with nested tool role.
	if !containsToolRole([]any{map[string]any{"messages": []any{map[string]any{"role": "tool"}}}}) {
		t.Fatal("array with nested tool role should be true")
	}
	// Array with no tool roles.
	if containsToolRole([]any{map[string]any{"role": "user", "content": "hi"}}) {
		t.Fatal("array with no tool roles should be false")
	}
	// Empty array.
	if containsToolRole([]any{}) {
		t.Fatal("empty array should be false")
	}
	// Map with tool role.
	if !containsToolRole(map[string]any{"role": "tool"}) {
		t.Fatal("map with tool role should be true")
	}
	// Map with no tool role and no children.
	if containsToolRole(map[string]any{"role": "user"}) {
		t.Fatal("map with user role should be false")
	}
}

func TestRequestCanProduceToolCalls(t *testing.T) {
	t.Parallel()
	// Empty root -> false.
	if requestCanProduceToolCalls(map[string]any{}) {
		t.Fatal("empty root should be false")
	}
	// tools key with non-empty array.
	if !requestCanProduceToolCalls(map[string]any{"tools": []any{"tool1"}}) {
		t.Fatal("tools key with array should be true")
	}
	// functions key with non-empty map.
	if !requestCanProduceToolCalls(map[string]any{"functions": map[string]any{"f1": "x"}}) {
		t.Fatal("functions key with map should be true")
	}
	// tool_choice key.
	if !requestCanProduceToolCalls(map[string]any{"tool_choice": "auto"}) {
		t.Fatal("tool_choice key should be true")
	}
	// function_call key.
	if !requestCanProduceToolCalls(map[string]any{"function_call": "auto"}) {
		t.Fatal("function_call key should be true")
	}
	// messages with tool role.
	if !requestCanProduceToolCalls(map[string]any{"messages": []any{map[string]any{"role": "tool"}}}) {
		t.Fatal("messages with tool role should be true")
	}
	// input with tool role.
	if !requestCanProduceToolCalls(map[string]any{"input": []any{map[string]any{"role": "tool"}}}) {
		t.Fatal("input with tool role should be true")
	}
	// tools key with empty array -> false.
	if requestCanProduceToolCalls(map[string]any{"tools": []any{}}) {
		t.Fatal("tools key with empty array should be false")
	}
}

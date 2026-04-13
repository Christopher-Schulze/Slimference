package filter

import "testing"

func TestExtractCommandFromHookJSON(t *testing.T) {
	t.Parallel()
	s, err := ExtractCommandFromHookJSON([]byte(`{"command":"git status"}`))
	if err != nil || s != "git status" {
		t.Fatalf("got %q %v", s, err)
	}
}

func TestExtractCommandFromHookJSON_Nested(t *testing.T) {
	t.Parallel()
	s, err := ExtractCommandFromHookJSON([]byte(`{"tool_input":{"command":"npm test"}}`))
	if err != nil || s != "npm test" {
		t.Fatalf("got %q %v", s, err)
	}
}

func TestExtractCommandFromHookJSON_Error(t *testing.T) {
	t.Parallel()
	_, err := ExtractCommandFromHookJSON([]byte(`{}`))
	if err == nil {
		t.Fatal("want error")
	}
}

// TestExtractCommandFromHookJSON_InvalidJSON covers the json.Unmarshal error path.
func TestExtractCommandFromHookJSON_InvalidJSON(t *testing.T) {
	t.Parallel()
	_, err := ExtractCommandFromHookJSON([]byte(`not-json`))
	if err == nil {
		t.Fatal("want error for invalid JSON")
	}
}

// TestExtractCommandFromHookJSON_Array covers the []interface{} branch in findStringForKey.
func TestExtractCommandFromHookJSON_Array(t *testing.T) {
	t.Parallel()
	s, err := ExtractCommandFromHookJSON([]byte(`[{"command":"git log"}]`))
	if err != nil || s != "git log" {
		t.Fatalf("array: got %q %v", s, err)
	}
}

// TestFindStringForKey_direct covers the helper directly for edge cases.
func TestFindStringForKey_direct(t *testing.T) {
	t.Parallel()
	// empty string value should not match
	if s, ok := findStringForKey(map[string]interface{}{"command": ""}, "command"); ok || s != "" {
		t.Fatalf("empty string: got %q %v", s, ok)
	}
	// nested array
	v := []interface{}{map[string]interface{}{"command": "npm test"}}
	if s, ok := findStringForKey(v, "command"); !ok || s != "npm test" {
		t.Fatalf("nested array: got %q %v", s, ok)
	}
	// non-matching type (int) should return false
	if _, ok := findStringForKey(42, "command"); ok {
		t.Fatal("int: should return false")
	}
}

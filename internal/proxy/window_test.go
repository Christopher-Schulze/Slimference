package proxy

import "testing"

func TestInputItemUserText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"not a map", "string", ""},
		{"not a map number", 42, ""},
		{"nil", nil, ""},
		{"non-user role", map[string]any{"role": "assistant", "content": "hi"}, ""},
		{"empty role with content string", map[string]any{"content": "hello"}, "hello"},
		{"user role with content string", map[string]any{"role": "user", "content": "hello"}, "hello"},
		{"user role with content array", map[string]any{"role": "user", "content": []any{map[string]any{"text": "part1"}, map[string]any{"text": "part2"}}}, "part1 part2"},
		{"user role with text field", map[string]any{"role": "user", "text": "direct text"}, "direct text"},
		{"user role no content no text", map[string]any{"role": "user"}, ""},
		{"empty role with text field", map[string]any{"text": "fallback text"}, "fallback text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := inputItemUserText(tc.value); got != tc.want {
				t.Fatalf("inputItemUserText(%v) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

func TestContentValueString(t *testing.T) {
	t.Parallel()
	if got := contentValueString("plain string"); got != "plain string" {
		t.Fatalf("contentValueString(string) = %q", got)
	}
	if got := contentValueString([]any{map[string]any{"text": "a"}, map[string]any{"text": "b"}}); got != "a b" {
		t.Fatalf("contentValueString(array) = %q, want \"a b\"", got)
	}
	if got := contentValueString([]any{map[string]any{"type": "image"}, map[string]any{"text": "b"}}); got != "b" {
		t.Fatalf("contentValueString(array mixed) = %q, want \"b\"", got)
	}
	if got := contentValueString(42); got != "42" {
		t.Fatalf("contentValueString(number) = %q, want \"42\"", got)
	}
}

func TestInputValueFirstUserText(t *testing.T) {
	t.Parallel()
	// String input -> returned directly.
	if got := inputValueFirstUserText("plain string"); got != "plain string" {
		t.Fatalf("inputValueFirstUserText(string) = %q", got)
	}
	// Array with user message -> returns text.
	arr := []any{
		map[string]any{"role": "assistant", "content": "skip"},
		map[string]any{"role": "user", "content": "found"},
	}
	if got := inputValueFirstUserText(arr); got != "found" {
		t.Fatalf("inputValueFirstUserText(array) = %q, want \"found\"", got)
	}
	// Empty array -> empty.
	if got := inputValueFirstUserText([]any{}); got != "" {
		t.Fatalf("inputValueFirstUserText(empty array) = %q, want empty", got)
	}
	// Non-string, non-array -> empty.
	if got := inputValueFirstUserText(42); got != "" {
		t.Fatalf("inputValueFirstUserText(number) = %q, want empty", got)
	}
}

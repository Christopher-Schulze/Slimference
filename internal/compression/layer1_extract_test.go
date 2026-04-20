package compression

import (
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/types"
)

func TestExtractFilepathFromToolResult(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{`{"path": "/src/main.go"}`, "/src/main.go"},
		{`{"file_path": "a.py"}`, "a.py"},
		{`{"filename": "x.rs"}`, "x.rs"},
		{`{"filepath": "/tmp/t"}`, "/tmp/t"},
		{`{"file": "z.rb"}`, "z.rb"},
		{`not json`, ""},
		{``, ""},
	}
	for _, tc := range tests {
		got := extractFilepathFromToolResult(types.ContentBlock{ToolInput: tc.input})
		if got != tc.want {
			t.Errorf("input %q: got %q want %q", tc.input, got, tc.want)
		}
	}
}

func TestDeterministicCompressor_detectLanguage(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	c := NewDeterministicCompressor(cfg)

	blk := types.ContentBlock{ToolInput: `{"path": "pkg/foo.go"}`}
	if got := c.detectLanguage(blk, " "); got != "go" {
		t.Fatalf("from path: %q", got)
	}
	if got := c.detectLanguage(types.ContentBlock{}, "package main\nfunc x() {}"); got != "go" {
		t.Fatalf("go heuristics: %q", got)
	}
	if got := c.detectLanguage(types.ContentBlock{}, "import os\nfrom x import y"); got != "python" {
		t.Fatalf("python: %q", got)
	}
	if got := c.detectLanguage(types.ContentBlock{}, "#!/bin/sh\necho hi"); got != "shell" {
		t.Fatalf("shell: %q", got)
	}
}

func TestStructureLangAllowed(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	cfg.StructureLanguages = []string{"go"}
	c := NewDeterministicCompressor(cfg)
	if !c.structureLangAllowed("go") || c.structureLangAllowed("zig") {
		t.Fatal("allowlist")
	}
	emptyCfg := defaultTestCfg(1)
	emptyCfg.StructureLanguages = nil
	empty := NewDeterministicCompressor(emptyCfg)
	if !empty.structureLangAllowed("anything") {
		t.Fatal("empty allowlist => all")
	}
}

// TestDeterministicCompressor_detectLanguage_extended covers heuristics not hit by the
// base test: java (package + semicolon), rust (use + ::), c (#include), ruby (require),
// html (<html), and the go \nfunc branch (no package prefix).
func TestDeterministicCompressor_detectLanguage_extended(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	c := NewDeterministicCompressor(cfg)

	cases := []struct {
		name string
		text string
		want string
	}{
		{
			name: "java via package+semicolon",
			text: "package com.example;\n\npublic class Foo {}",
			want: "java",
		},
		{
			name: "rust via use+::",
			text: "use std::io;\nfn main() {}",
			want: "rust",
		},
		{
			name: "c via #include",
			text: "#include <stdio.h>\nint main() { return 0; }",
			want: "c",
		},
		{
			name: "c via #ifndef",
			text: "#ifndef FOO_H\n#define FOO_H\n#endif",
			want: "c",
		},
		{
			name: "ruby via require double-quote",
			text: `require "json"\nclass Foo; end`,
			want: "ruby",
		},
		{
			name: "ruby via require single-quote",
			text: "require 'json'\nclass Foo; end",
			want: "ruby",
		},
		{
			name: "html via <html>",
			text: "<html>\n<body>hello</body>\n</html>",
			want: "html",
		},
		{
			name: "html via <!DOCTYPE html",
			text: "<!DOCTYPE html>\n<html><body></body></html>",
			want: "html",
		},
		{
			name: "go via \\nfunc without package prefix",
			text: "var x = 1\nfunc helper() {}",
			want: "go",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := c.detectLanguage(types.ContentBlock{}, tc.text)
			if got != tc.want {
				t.Errorf("detectLanguage(%q) = %q, want %q", tc.text[:min(len(tc.text), 40)], got, tc.want)
			}
		})
	}
}

// min is a local helper for Go < 1.21 compat in this package.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestSignatureOnly_noBrace verifies that a line without an opening brace is returned unchanged.
func TestSignatureOnly_noBrace(t *testing.T) {
	t.Parallel()
	line := "func Foo(x int) int"
	got := signatureOnly(line)
	if got != line {
		t.Errorf("no-brace line should be returned unchanged: got %q want %q", got, line)
	}
}

// TestItoa_negative verifies the negative-number branch of itoa.
func TestItoa_negative(t *testing.T) {
	t.Parallel()
	if got := itoa(-7); got != "-7" {
		t.Errorf("itoa(-7) = %q, want \"-7\"", got)
	}
	if got := itoa(-42); got != "-42" {
		t.Errorf("itoa(-42) = %q, want \"-42\"", got)
	}
}

// TestExtractFilepathFromToolResult_extended covers non-string and empty-string values.
func TestExtractFilepathFromToolResult_extended(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{`{"path": 42}`, ""},     // non-string value → type assertion fails
		{`{"path": ""}`, ""},     // empty string → s != "" guard skips it
		{`{"other": "val"}`, ""}, // no matching key → return ""
	}
	for _, tc := range tests {
		got := extractFilepathFromToolResult(types.ContentBlock{ToolInput: tc.input})
		if got != tc.want {
			t.Errorf("input %q: got %q want %q", tc.input, got, tc.want)
		}
	}
}

// TestOptimizeCacheBreakpoints_emptyContentMessage verifies that a message with no content
// blocks is skipped (len(result[i].Content) == 0 continue branch).
func TestOptimizeCacheBreakpoints_emptyContentMessage(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("x", 5000) // > minStablePrefixTokens
	msgs := []types.Message{
		{Role: "user", Content: nil},           // no content → skipped
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: big}}},
	}
	out := OptimizeCacheBreakpoints(msgs, 2)
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
	// Only the second message (with content) should get a breakpoint.
	found := false
	for _, b := range out[1].Content {
		if b.CacheControl != nil && b.CacheControl.Type == "ephemeral" {
			found = true
		}
	}
	if !found {
		t.Error("expected breakpoint on the message with content")
	}
}

// TestOptimizeCacheBreakpoints_allEmptyContent verifies the len(candidates)==0 early return.
func TestOptimizeCacheBreakpoints_allEmptyContent(t *testing.T) {
	t.Parallel()
	// All messages in the stable prefix have no content → candidates is empty → early return.
	msgs := []types.Message{
		{Role: "user", Content: nil},
		{Role: "user", Content: nil},
	}
	out := OptimizeCacheBreakpoints(msgs, 2)
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
}

// TestOptimizeCacheBreakpoints_boundaryExceedsLen verifies that when
// stableBoundary > len(messages) the placement is capped at len(messages):
// the eligible set is clamped and a breakpoint is still placed on the one
// actual stable message. T45 corrected the pre-existing behaviour that
// silently did nothing in this case.
func TestOptimizeCacheBreakpoints_boundaryExceedsLen(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("x", 5000) // > minStablePrefixTokens * charsPerToken
	msgs := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: big}}},
	}
	// stableBoundary=100 >> len(msgs)=1; clamped to 1 eligible -> 1 breakpoint.
	out := OptimizeCacheBreakpoints(msgs, 100)
	got := 0
	for _, b := range out[0].Content {
		if b.CacheControl != nil {
			got++
		}
	}
	if got != 1 {
		t.Errorf("breakpoints = %d, want 1 (clamped to eligible count)", got)
	}
}

// TestOptimizeCacheBreakpoints_moreThanMaxBreakpoints verifies that at most maxCacheBreakpoints
// breakpoints are injected (break branch inside the candidate collection loop).
func TestOptimizeCacheBreakpoints_moreThanMaxBreakpoints(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("z", 2000)
	// Build more than maxCacheBreakpoints (4) messages in the prefix.
	msgs := make([]types.Message, 8)
	for i := range msgs {
		msgs[i] = types.Message{
			Role:    "user",
			Content: []types.ContentBlock{{Type: "text", Text: big}},
		}
	}
	out := OptimizeCacheBreakpoints(msgs, 8)
	count := 0
	for _, m := range out {
		for _, b := range m.Content {
			if b.CacheControl != nil {
				count++
			}
		}
	}
	if count > maxCacheBreakpoints {
		t.Errorf("injected %d breakpoints, want at most %d", count, maxCacheBreakpoints)
	}
}

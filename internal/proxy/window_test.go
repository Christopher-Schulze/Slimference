package proxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProxyCommandLineContainsSearchTool(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cmd  string
		want bool
	}{
		{"empty", "", false},
		{"plain text", "echo hello world", false},
		{"rg", "rg pattern", true},
		{"grep", "grep pattern file", true},
		{"ggrep", "ggrep pattern", true},
		{"ag", "ag pattern", true},
		{"ack", "ack pattern", true},
		{"ack.pl", "ack.pl pattern", true},
		{"ug", "ug pattern", true},
		{"ugrep", "ugrep pattern", true},
		{"sift", "sift pattern", true},
		{"git grep", "git grep pattern", true},
		{"git log", "git log", false},
		{"git without grep arg", "git status", false},
		{"quoted rg", `"rg" pattern`, true},
		{"full path rg", "/usr/local/bin/rg pattern", true},
		{"quoted full path", `"/usr/bin/grep" pattern file`, true},
		{"rg in middle", "sudo rg pattern", true},
		{"unknown command", "ls -la", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := proxyCommandLineContainsSearchTool(tc.cmd); got != tc.want {
				t.Fatalf("proxyCommandLineContainsSearchTool(%q) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}

func TestProxyLayer0RouteCounters_SnapshotNil(t *testing.T) {
	t.Parallel()
	var c *proxyLayer0RouteCounters
	got := c.snapshot()
	if got.ToolResultBlocks != 0 {
		t.Fatalf("nil snapshot should be zero value, got %+v", got)
	}
}

func TestSaveCodexLayer0LatencyBudgetState(t *testing.T) {
	t.Parallel()
	// nil proxy -> no-op.
	var p *Proxy
	p.saveCodexLayer0LatencyBudgetState()

	// home error -> no-op.
	origHome := proxyUserHomeDir
	t.Cleanup(func() { proxyUserHomeDir = origHome })
	proxyUserHomeDir = func() (string, error) { return "", os.ErrNotExist }
	p2 := &Proxy{}
	p2.saveCodexLayer0LatencyBudgetState()

	// empty home -> no-op.
	proxyUserHomeDir = func() (string, error) { return "", nil }
	p3 := &Proxy{}
	p3.saveCodexLayer0LatencyBudgetState()

	// valid home with strikes clamping.
	tmp := t.TempDir()
	proxyUserHomeDir = func() (string, error) { return tmp, nil }
	p4 := &Proxy{}
	p4.codexLayer0LatencyStrikes.Store(-5)
	p4.codexLayer0LatencyExceeded.Store(true)
	p4.saveCodexLayer0LatencyBudgetState()

	// Verify file was written.
	statePath := filepath.Join(tmp, ".slimference", "runtime-budget", "codex-layer0-latency.json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state file should exist: %v", err)
	}

	// Strikes above limit -> clamped.
	p5 := &Proxy{}
	p5.codexLayer0LatencyStrikes.Store(codexLayer0LatencyStrikeLimit + 100)
	p5.saveCodexLayer0LatencyBudgetState()
}

func TestProxyChunkDedupUniformPriorityScore(t *testing.T) {
	t.Parallel()
	const base = 1_000_000_000
	cases := []struct {
		name  string
		order int
		want  int
	}{
		{"negative", -1, base},
		{"zero", 0, base},
		{"small", 5, base - 5},
		{"large", 999999, base - 999999},
		{"at base", base, 1},
		{"above base", base + 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := proxyChunkDedupUniformPriorityScore(tc.order); got != tc.want {
				t.Fatalf("proxyChunkDedupUniformPriorityScore(%d) = %d, want %d", tc.order, got, tc.want)
			}
		})
	}
}

func TestProxyChunkDedupCandidateBudgetBytes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		outputBytes   int
		maxRefPercent int
		want          int
	}{
		{"zero output", 0, 50, 1},
		{"negative output", -10, 50, 1},
		{"zero percent", 1000, 0, 1000},
		{"negative percent", 1000, -5, 1000},
		{"over 100 percent", 1000, 150, 1000},
		{"normal", 1000, 50, 500},
		{"100 percent", 1000, 100, 1000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := proxyChunkDedupCandidateBudgetBytes(tc.outputBytes, tc.maxRefPercent)
			if got != tc.want {
				t.Fatalf("proxyChunkDedupCandidateBudgetBytes(%d, %d) = %d, want %d", tc.outputBytes, tc.maxRefPercent, got, tc.want)
			}
		})
	}
}

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

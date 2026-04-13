package compression

import (
	"testing"

	"github.com/slimference/slimference/internal/types"
)

func TestIsPreFiltered_GitMarkers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"git status clean", "[git status] clean\n", true},
		{"git status paths", "[git status] 3 paths (staged:1 worktree:2 untracked:0)\n", true},
		{"git diff empty", "[git diff] empty\n", true},
		{"git log empty", "[git log] empty\n", true},
		{"git push ok", "[git push] ok\n", true},
		{"git rebase ok", "[git rebase] up to date\n", true},
		{"not pre-filtered", "On branch main\nnothing to commit\n", false},
		{"empty", "", false},
		{"random text", "some output from a tool", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isPreFiltered(tc.content)
			if got != tc.want {
				t.Errorf("isPreFiltered(%q) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}

func TestIsPreFiltered_TOMLMarkers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		content string
		want    bool
	}{
		{"[ok]", true},
		{"[ok: build succeeded]\n", true},
		{"[3 matches]\n", true},
		{"[12 matches] in 5 files\n", true},
		{"[full output: /tmp/tee-123.txt]\n", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.content, func(t *testing.T) {
			t.Parallel()
			if got := isPreFiltered(tc.content); got != tc.want {
				t.Errorf("isPreFiltered(%q) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}

func TestIsPreFiltered_DuplicateMarker(t *testing.T) {
	t.Parallel()
	content := "[×3] same log line\n"
	if !isPreFiltered(content) {
		t.Errorf("duplicate collapse marker should be recognized as pre-filtered")
	}
}

func TestIsPreFiltered_BuildTestMarkers(t *testing.T) {
	t.Parallel()
	if !isPreFiltered("[build] ok\n") {
		t.Error("[build] should be pre-filtered")
	}
	if !isPreFiltered("[test] passed\n") {
		t.Error("[test] should be pre-filtered")
	}
	if !isPreFiltered("[search] 5 results\n") {
		t.Error("[search] should be pre-filtered")
	}
	if !isPreFiltered("[grep] 2 matches\n") {
		t.Error("[grep] should be pre-filtered")
	}
}

func TestIsPreFiltered_OnlyChecksFirstLine(t *testing.T) {
	t.Parallel()
	// Marker only on second line should NOT trigger (we check first line only)
	content := "normal output here\n[git status] clean\n"
	if isPreFiltered(content) {
		t.Error("marker on second line should not trigger pre-filter detection")
	}
}

// TestLayer1_SkipsProcessingForPreFilteredContent verifies that the compressor
// respects the pre-filter flag and does not apply JSON compact / comment strip /
// structure extraction on pre-filtered content.
func TestLayer1_SkipsProcessingForPreFilteredContent(t *testing.T) {
	t.Parallel()
	// A git-status compact output - isPreFiltered fires on first line.
	// Build enough messages so some are in the compressible window.
	preFiltered := "[git status] 2 paths (staged:1 worktree:1 untracked:0)\n// Go comment\nfunc foo() {}"

	cfg := defaultTestCfg(2)
	c := NewDeterministicCompressor(cfg)

	// Build 5 messages: first 3 are in compressible window, last 2 are in sliding window.
	msgs := make([]types.Message, 5)
	for i := range msgs {
		msgs[i] = types.Message{
			Index: i,
			Role:  "user",
			Content: []types.ContentBlock{
				{Type: "tool_result", Text: preFiltered, ToolName: "Bash"},
			},
		}
	}

	result := c.Compress(msgs)

	// No JSON saved, no comment saved (pre-filtered content bypasses both)
	if result.JSONSaved > 0 {
		t.Errorf("pre-filtered content should not have JSON savings, got %d", result.JSONSaved)
	}
	if result.CommentSaved > 0 {
		t.Errorf("pre-filtered content should not have comment savings, got %d", result.CommentSaved)
	}
}

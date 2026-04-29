package summarization

import (
	"strings"
	"testing"
)

func TestHasLineageMarker(t *testing.T) {

	cases := []struct {
		line string
		want bool
	}{
		{"- fact text [msg:0]", true},
		{"- multi-msg fact [msg:1,2,3]", true},
		{"- trailing whitespace [msg:7]   ", true},
		{"- bullet without marker", false},
		{"- bullet with [msg:abc]", false},
		{"- bullet with bracket [foo:1]", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := hasLineageMarker(tc.line); got != tc.want {
			t.Fatalf("hasLineageMarker(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

func TestStripLineageMarker(t *testing.T) {

	cases := []struct {
		in, want string
	}{
		{"- fact [msg:0]", "- fact"},
		{"- compound [msg:1,2,3]", "- compound"},
		{"- no marker", "- no marker"},
		{"- trailing space [msg:7]  ", "- trailing space"},
	}
	for _, tc := range cases {
		if got := StripLineageMarker(tc.in); got != tc.want {
			t.Fatalf("StripLineageMarker(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRecordLineageStats_TracksRate(t *testing.T) {

	ResetLineageMarkerStats()
	summary := strings.Join([]string{
		"- bullet without marker",
		"- bullet with marker [msg:0]",
		"- another with marker [msg:1,2]",
		"plain prose line",
		"- last marker [msg:9]",
	}, "\n")
	RecordLineageStats(summary)
	marked, total := LineageMarkerCounts()
	if total != 4 {
		t.Fatalf("total bullets = %d, want 4", total)
	}
	if marked != 3 {
		t.Fatalf("marked bullets = %d, want 3", marked)
	}
	rate := LineageMarkerRate()
	if rate < 0.74 || rate > 0.76 {
		t.Fatalf("rate = %.4f, want ~0.75", rate)
	}
}

func TestLineageMarkerRate_EmptyReturnsZero(t *testing.T) {

	ResetLineageMarkerStats()
	if got := LineageMarkerRate(); got != 0 {
		t.Fatalf("empty rate must be 0, got %f", got)
	}
}

func TestSystemPromptIncludesLineageInstruction(t *testing.T) {

	if !strings.Contains(systemPrompt, "[msg:") {
		t.Fatal("system prompt must instruct the model to emit [msg:N] markers")
	}
	if !strings.Contains(systemPrompt, "LINEAGE MARKERS") {
		t.Fatal("system prompt must contain a LINEAGE MARKERS section header")
	}
}

package compression

import (
	"testing"
)

const testDedupThreshold = 0.85

// TestContentIndex_NoDupe verifies that the first occurrence is never flagged as duplicate.
func TestContentIndex_NoDupe(t *testing.T) {
	t.Parallel()

	ci := NewContentIndex()
	exact, near, firstIdx := ci.CheckAndRecord("unique content here", 0, testDedupThreshold)
	if exact || near {
		t.Error("first occurrence should not be a duplicate")
	}
	if firstIdx != -1 {
		t.Errorf("firstIdx = %d, want -1 for fresh content", firstIdx)
	}
}

// TestContentIndex_ExactDupe verifies that a second identical occurrence is detected.
func TestContentIndex_ExactDupe(t *testing.T) {
	t.Parallel()

	ci := NewContentIndex()
	content := "the exact same content"

	exact1, near1, _ := ci.CheckAndRecord(content, 0, testDedupThreshold)
	if exact1 || near1 {
		t.Fatal("first occurrence incorrectly flagged as duplicate")
	}

	exact2, _, firstIdx := ci.CheckAndRecord(content, 1, testDedupThreshold)
	if !exact2 {
		t.Error("second identical occurrence should be flagged as exact duplicate")
	}
	if firstIdx != 0 {
		t.Errorf("firstIdx = %d, want 0", firstIdx)
	}
}

// TestContentIndex_NormalizedMatch verifies that content differing only in whitespace
// or line endings is treated as the same content.
func TestContentIndex_NormalizedMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		first  string
		second string
	}{
		{
			name:   "trailing whitespace differs",
			first:  "hello world   ",
			second: "hello world",
		},
		{
			name:   "CRLF vs LF",
			first:  "line1\r\nline2\r\n",
			second: "line1\nline2",
		},
		{
			name:   "leading and trailing whitespace",
			first:  "\n  content  \n",
			second: "content",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ci := NewContentIndex()

			ci.CheckAndRecord(tc.first, 0, testDedupThreshold)
			exact, near, firstIdx := ci.CheckAndRecord(tc.second, 1, testDedupThreshold)

			if !exact && !near {
				t.Error("normalized match should be flagged as duplicate")
			}
			if firstIdx != 0 {
				t.Errorf("firstIdx = %d, want 0", firstIdx)
			}
		})
	}
}

// TestContentIndex_Reset verifies that after Reset, previously seen content is treated as new.
func TestContentIndex_Reset(t *testing.T) {
	t.Parallel()

	ci := NewContentIndex()
	content := "content that will be reset"

	ci.CheckAndRecord(content, 0, testDedupThreshold)
	exact, _, _ := ci.CheckAndRecord(content, 1, testDedupThreshold)
	if !exact {
		t.Fatal("second occurrence should be a dupe before reset")
	}

	ci.Reset()

	exactAfter, _, firstIdx := ci.CheckAndRecord(content, 2, testDedupThreshold)
	if exactAfter {
		t.Error("after Reset, content should be treated as new (not a dupe)")
	}
	if firstIdx != -1 {
		t.Errorf("firstIdx = %d, want -1 after Reset", firstIdx)
	}
}

// TestContentIndex_DifferentContent verifies that two distinct contents are not confused.
func TestContentIndex_DifferentContent(t *testing.T) {
	t.Parallel()

	ci := NewContentIndex()
	ci.CheckAndRecord("content A", 0, testDedupThreshold)

	exact, near, _ := ci.CheckAndRecord("content B", 1, testDedupThreshold)
	if exact || near {
		t.Error("distinct content B should not match content A")
	}
}

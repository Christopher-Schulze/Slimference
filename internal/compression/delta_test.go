package compression

import (
	"strings"
	"testing"
)

func TestFileVersionTracker_RecordGetDeltaReset(t *testing.T) {
	t.Parallel()
	tr := NewFileVersionTracker()
	_, _, ok := tr.GetDelta("f.go", "second\n")
	if ok {
		t.Fatal("no prior version")
	}
	base := strings.Repeat("stable line\n", 40)
	tr.RecordVersion("f.go", base, 0)
	tr.RecordVersion("f.go", base, 1) // duplicate hash skipped
	newContent := base + "new tail line\n"
	delta, prevIdx, ok := tr.GetDelta("f.go", newContent)
	if !ok || prevIdx != 0 {
		t.Fatalf("delta len=%d prev=%d ok=%v", len(delta), prevIdx, ok)
	}
	if !strings.Contains(delta, "new tail") {
		t.Fatalf("diff body: %q", delta)
	}
	tr.Reset()
	_, _, ok = tr.GetDelta("f.go", "x")
	if ok {
		t.Fatal("after reset")
	}
}

func TestFileVersionTracker_GetDelta_noChange(t *testing.T) {
	t.Parallel()
	tr := NewFileVersionTracker()
	tr.RecordVersion("x", "same\n", 0)
	_, prev, ok := tr.GetDelta("x", "same\n")
	if ok || prev != 0 {
		t.Fatalf("ok=%v prev=%d", ok, prev)
	}
}

// TestFileVersionTracker_GetDelta_tooLarge verifies that when the diff exceeds 50% of
// newContent, no delta is returned (not worth encoding as a delta).
func TestFileVersionTracker_GetDelta_tooLarge(t *testing.T) {
	t.Parallel()
	tr := NewFileVersionTracker()
	// old is small; new is entirely different - diff ~ size of new (>50%).
	old := "line one\n"
	newContent := strings.Repeat("completely different line\n", 20)
	tr.RecordVersion("f.go", old, 0)
	_, _, ok := tr.GetDelta("f.go", newContent)
	if ok {
		t.Fatal("diff >= 50% of new content should not return a delta")
	}
}

// TestUnifiedDiff_additionOnly verifies that a diff with only additions (no context before
// first change) is handled - exercises the firstOld/firstNew edge case in buildHunks.
func TestUnifiedDiff_additionOnly(t *testing.T) {
	t.Parallel()
	diff := unifiedDiff("", "added line one\nadded line two\n")
	if !strings.Contains(diff, "+added line one") {
		t.Errorf("expected addition lines in diff: %q", diff)
	}
}

// TestBuildHunks_emptyEdits verifies that buildHunks returns nil immediately when edits is empty.
// This path is unreachable from unifiedDiff (strings.Split always returns ≥1 element) but is
// defensive code that we cover by calling buildHunks directly.
func TestBuildHunks_emptyEdits(t *testing.T) {
	t.Parallel()
	result := buildHunks(nil, 0, 0, 3)
	if result != nil {
		t.Errorf("expected nil for empty edits, got %v", result)
	}
	result = buildHunks([]lineEdit{}, 0, 0, 3)
	if result != nil {
		t.Errorf("expected nil for empty edits slice, got %v", result)
	}
}

// TestUnifiedDiff_noChange verifies that identical old and new content produces an empty diff.
// This exercises the len(changedIdx)==0 early return in buildHunks.
func TestUnifiedDiff_noChange(t *testing.T) {
	t.Parallel()
	diff := unifiedDiff("line one\nline two\n", "line one\nline two\n")
	if diff != "" {
		t.Errorf("expected empty diff for identical content, got %q", diff)
	}
}

// TestUnifiedDiff_multiHunk verifies that far-apart changes produce two separate hunks.
func TestUnifiedDiff_multiHunk(t *testing.T) {
	t.Parallel()
	// Changes at the very start and very end, far enough apart that they form separate hunks.
	oldLines := append([]string{"CHANGE_TOP"}, append(make([]string, 30, 30), "CHANGE_BOTTOM")...)
	newLines := append([]string{"modified_top"}, append(make([]string, 30, 30), "modified_bottom")...)
	for i := range oldLines {
		if oldLines[i] == "" {
			oldLines[i] = "stable line"
		}
	}
	for i := range newLines {
		if newLines[i] == "" {
			newLines[i] = "stable line"
		}
	}
	old := strings.Join(oldLines, "\n")
	newC := strings.Join(newLines, "\n")
	diff := unifiedDiff(old, newC)
	// Expect at least two @@ hunk headers.
	if strings.Count(diff, "@@") < 2 {
		t.Errorf("expected multiple hunks for far-apart changes, got:\n%s", diff)
	}
}

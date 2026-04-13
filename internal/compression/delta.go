package compression

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"time"
)

// FileVersion records a snapshot of a file at a specific message index.
type FileVersion struct {
	MessageIdx int
	Hash       [32]byte
	Content    string
	Timestamp  time.Time
}

// FileVersionTracker tracks per-file revision history for delta encoding.
type FileVersionTracker struct {
	mu       sync.Mutex
	versions map[string][]FileVersion // filepath -> ordered versions
}

// NewFileVersionTracker returns an initialized FileVersionTracker.
func NewFileVersionTracker() *FileVersionTracker {
	return &FileVersionTracker{
		versions: make(map[string][]FileVersion),
	}
}

// RecordVersion records a new version of a file. Thread-safe.
func (t *FileVersionTracker) RecordVersion(filepath, content string, msgIdx int) {
	hash := sha256.Sum256([]byte(content))

	t.mu.Lock()
	defer t.mu.Unlock()

	versions := t.versions[filepath]
	// Avoid recording the identical content twice in a row.
	if len(versions) > 0 && versions[len(versions)-1].Hash == hash {
		return
	}

	t.versions[filepath] = append(versions, FileVersion{
		MessageIdx: msgIdx,
		Hash:       hash,
		Content:    content,
		Timestamp:  time.Now(),
	})
}

// GetDelta computes a unified diff from the most recent recorded version of filepath
// to newContent. Returns (diff, prevMsgIdx, true) only when the diff is shorter than
// 50% of newContent length. Thread-safe.
func (t *FileVersionTracker) GetDelta(filepath, newContent string) (delta string, prevMsgIdx int, hasDelta bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	versions := t.versions[filepath]
	if len(versions) == 0 {
		return "", -1, false
	}

	prev := versions[len(versions)-1]
	diff := unifiedDiff(prev.Content, newContent)

	if len(diff) == 0 {
		return "", prev.MessageIdx, false
	}

	if float64(len(diff)) >= float64(len(newContent))*0.5 {
		return "", prev.MessageIdx, false
	}

	return diff, prev.MessageIdx, true
}

// Reset clears all tracked file versions. Called on cache flush.
func (t *FileVersionTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.versions = make(map[string][]FileVersion)
}

// unifiedDiff produces a minimal unified diff between old and new content.
// Uses line-by-line comparison with context lines.
func unifiedDiff(oldContent, newContent string) string {
	const contextLines = 3

	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	// Compute the edit script using a simple LCS-based diff.
	edits := diffLines(oldLines, newLines)

	// Group edits into hunks with context.
	hunks := buildHunks(edits, len(oldLines), len(newLines), contextLines)
	if len(hunks) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("--- a\n")
	sb.WriteString("+++ b\n")

	for _, h := range hunks {
		sb.WriteString(h)
	}

	return sb.String()
}

// lineEdit represents a single line operation in a diff.
type lineEdit struct {
	op      byte // ' ' = context, '+' = add, '-' = remove
	oldLine int  // 0-based index in old file (-1 for additions)
	newLine int  // 0-based index in new file (-1 for removals)
	text    string
}

// diffLines computes a sequence of line edits between old and new slices.
func diffLines(old, new []string) []lineEdit {
	m := len(old)
	n := len(new)

	// Build LCS table.
	// Use space-efficient approach: only two rows.
	// For large files this stays reasonable in the under-1ms budget for typical inputs.
	type cell struct{ length int }
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}

	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if old[i] == new[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				a := dp[i+1][j]
				b := dp[i][j+1]
				if a >= b {
					dp[i][j] = a
				} else {
					dp[i][j] = b
				}
			}
		}
	}

	// Trace back through the LCS table.
	var edits []lineEdit
	i, j := 0, 0
	for i < m || j < n {
		if i < m && j < n && old[i] == new[j] {
			edits = append(edits, lineEdit{op: ' ', oldLine: i, newLine: j, text: old[i]})
			i++
			j++
		} else if j < n && (i >= m || dp[i][j+1] >= dp[i+1][j]) {
			edits = append(edits, lineEdit{op: '+', oldLine: -1, newLine: j, text: new[j]})
			j++
		} else {
			edits = append(edits, lineEdit{op: '-', oldLine: i, newLine: -1, text: old[i]})
			i++
		}
	}

	return edits
}

// buildHunks groups edits into unified diff hunk strings.
func buildHunks(edits []lineEdit, oldTotal, newTotal, context int) []string {
	if len(edits) == 0 {
		return nil
	}

	// Find indices of changed edits.
	var changedIdx []int
	for i, e := range edits {
		if e.op != ' ' {
			changedIdx = append(changedIdx, i)
		}
	}
	if len(changedIdx) == 0 {
		return nil
	}

	var hunks []string

	i := 0
	for i < len(changedIdx) {
		// Start of hunk: context lines before first change.
		start := changedIdx[i] - context
		if start < 0 {
			start = 0
		}

		// Extend end to cover all changes within context reach.
		end := changedIdx[i] + context
		for i < len(changedIdx) && changedIdx[i] <= end {
			end = changedIdx[i] + context
			i++
		}
		if end >= len(edits) {
			end = len(edits) - 1
		}

		// Compute old/new line ranges.
		oldStart, oldCount, newStart, newCount := 0, 0, 0, 0
		firstOld, firstNew := true, true

		for _, e := range edits[start : end+1] {
			if e.op != '+' {
				if firstOld {
					oldStart = e.oldLine + 1
					firstOld = false
				}
				oldCount++
			}
			if e.op != '-' {
				if firstNew {
					newStart = e.newLine + 1
					firstNew = false
				}
				newCount++
			}
		}

		var hunk strings.Builder
		hunk.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount))
		for _, e := range edits[start : end+1] {
			hunk.WriteByte(e.op)
			hunk.WriteString(e.text)
			hunk.WriteByte('\n')
		}
		hunks = append(hunks, hunk.String())
	}

	return hunks
}

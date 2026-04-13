package compression

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"sync"

	"github.com/slimference/slimference/internal/types"
)

// FileOpType classifies a file operation.
type FileOpType int

const (
	FileOpRead  FileOpType = iota
	FileOpWrite            // write or create
	FileOpEdit             // in-place edit
)

// FileOp records a single file operation within a conversation.
type FileOp struct {
	Type   FileOpType
	MsgIdx int
	Hash   [32]byte // hash of content (for Read: content read; for Write/Edit: new content)
}

// FileOpGraph tracks file operations across a conversation to find prunable reads.
type FileOpGraph struct {
	mu    sync.RWMutex
	files map[string][]FileOp
}

// NewFileOpGraph returns a ready FileOpGraph.
func NewFileOpGraph() *FileOpGraph {
	return &FileOpGraph{files: make(map[string][]FileOp)}
}

// Reset clears all tracked file operations.
func (g *FileOpGraph) Reset() {
	g.mu.Lock()
	g.files = make(map[string][]FileOp)
	g.mu.Unlock()
}

// PruneRedundant identifies and prunes stale file reads in messages[0:prefixEnd].
// A read is prunable when: the same file has a newer read AND an edit/write between
// the old read and the newer read, AND no later message references the old message index.
// Returns total bytes saved.
func (g *FileOpGraph) PruneRedundant(messages []types.Message, prefixEnd int) int {
	if prefixEnd <= 2 {
		return 0
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Build file operation graph from conversation
	fileOps := make(map[string][]FileOp)
	for i := 0; i < prefixEnd; i++ {
		for _, block := range messages[i].Content {
			path, opType, content := extractFileOp(block)
			if path == "" {
				continue
			}
			fileOps[path] = append(fileOps[path], FileOp{
				Type:   opType,
				MsgIdx: i,
				Hash:   sha256.Sum256([]byte(content)),
			})
		}
	}

	// Identify prunable reads: Read@i, (Edit|Write)@j (j>i), Read@k (k>j)
	type pruneCandidate struct {
		msgIdx  int
		path    string
		newerAt int
	}
	var candidates []pruneCandidate

	for path, ops := range fileOps {
		for oi := 0; oi < len(ops); oi++ {
			if ops[oi].Type != FileOpRead {
				continue
			}
			readIdx := ops[oi].MsgIdx
			// Look for Edit/Write after this read
			hasEditAfter := false
			for _, op := range ops[oi+1:] {
				if op.Type == FileOpWrite || op.Type == FileOpEdit {
					hasEditAfter = true
					break
				}
			}
			if !hasEditAfter {
				continue
			}
			// Look for a newer Read after the edit
			for _, op := range ops[oi+1:] {
				if op.Type == FileOpRead && op.MsgIdx > readIdx {
					candidates = append(candidates, pruneCandidate{
						msgIdx:  readIdx,
						path:    path,
						newerAt: op.MsgIdx,
					})
					break
				}
			}
		}
	}

	if len(candidates) == 0 {
		return 0
	}

	// Safety check: ensure no later message references the candidate message by index
	saved := 0
	for _, c := range candidates {
		if messageReferencesIndex(messages, c.msgIdx, prefixEnd) {
			continue
		}
		// Safe to prune: find the tool_result block in the candidate message
		// that corresponds to the file read and replace its content
		saved += pruneFileRead(messages, c.msgIdx, c.path, c.newerAt)
	}

	return saved
}

// pruneFileRead replaces the file content in a tool_result block with a reference.
func pruneFileRead(messages []types.Message, msgIdx int, path string, newerMsgIdx int) int {
	newContent := make([]types.ContentBlock, len(messages[msgIdx].Content))
	copy(newContent, messages[msgIdx].Content)
	saved := 0

	for bi, block := range newContent {
		if block.Type != "tool_result" {
			continue
		}
		// Check if this block is a file read for the target path
		fp := extractFilepathFromToolResult(block)
		if fp != path {
			continue
		}
		orig := block.Text
		stub := fmt.Sprintf(
			"[File %s was read here but superseded by read in message %d]",
			path, newerMsgIdx)
		if len(stub) < len(orig) {
			newContent[bi].Text = stub
			saved += len(orig) - len(stub)
		}
	}

	if saved > 0 {
		messages[msgIdx].Content = newContent
	}
	return saved
}

// extractFileOp extracts file path, operation type, and content from a content block.
func extractFileOp(block types.ContentBlock) (path string, op FileOpType, content string) {
	switch block.Type {
	case "tool_use":
		name := block.ToolName
		path = extractPathFromInput(block.ToolInput)
		if path == "" {
			return "", 0, ""
		}
		switch {
		case reEditTool.MatchString(name):
			return path, FileOpEdit, block.ToolInput
		case reWriteTool.MatchString(name):
			return path, FileOpWrite, block.ToolInput
		case reReadTool.MatchString(name):
			return path, FileOpRead, ""
		}
	case "tool_result":
		path = extractFilepathFromToolResult(block)
		if path != "" {
			return path, FileOpRead, block.Text
		}
	}
	return "", 0, ""
}

// extractPathFromInput extracts a file path from a tool_input JSON string.
func extractPathFromInput(toolInput string) string {
	if toolInput == "" {
		return ""
	}
	// Reuse the existing logic from extractFilepathFromToolResult via a synthetic block
	b := types.ContentBlock{Type: "tool_result", ToolInput: toolInput}
	return extractFilepathFromToolResult(b)
}

// messageReferencesIndex checks whether any message after msgIdx contains a textual
// reference to that message index (e.g., "message 5", "msg 5", "[5]").
func messageReferencesIndex(messages []types.Message, targetIdx, limit int) bool {
	patterns := []string{
		fmt.Sprintf("message %d", targetIdx),
		fmt.Sprintf("msg %d", targetIdx),
		fmt.Sprintf("[%d]", targetIdx),
	}

	for i := targetIdx + 1; i < limit; i++ {
		for _, block := range messages[i].Content {
			text := block.Text
			for _, p := range patterns {
				if containsIgnoreCase(text, p) {
					return true
				}
			}
		}
	}
	return false
}

// containsIgnoreCase reports whether s contains substr case-insensitively.
func containsIgnoreCase(s, substr string) bool {
	if len(s) < len(substr) {
		return false
	}
	s2 := make([]byte, len(s))
	for i, c := range []byte(s) {
		if c >= 'A' && c <= 'Z' {
			s2[i] = c + 32
		} else {
			s2[i] = c
		}
	}
	lower := make([]byte, len(substr))
	for i, c := range []byte(substr) {
		if c >= 'A' && c <= 'Z' {
			lower[i] = c + 32
		} else {
			lower[i] = c
		}
	}
	return reContains(s2, lower)
}

// reContains does a simple byte-slice substring search.
func reContains(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		match := true
		for j, b := range needle {
			if haystack[i+j] != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

var (
	reEditTool  = regexp.MustCompile(`(?i)^(edit|str_replace|replace|patch|update_file)$`)
	reWriteTool = regexp.MustCompile(`(?i)^(write|create|write_file|new_file)$`)
	reReadTool  = regexp.MustCompile(`(?i)^(read|view|cat|readfile|read_file)$`)
)

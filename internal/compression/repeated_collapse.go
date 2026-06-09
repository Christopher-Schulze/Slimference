package compression

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// ToolCallIndex tracks tool_use calls to detect repeated identical calls in old messages.
type ToolCallIndex struct {
	mu          sync.Mutex
	callFirst   map[[32]byte]int      // call_hash -> first message index
	callResults map[[32]byte][32]byte // call_hash -> result_hash of first occurrence
}

// NewToolCallIndex returns a ready ToolCallIndex.
func NewToolCallIndex() *ToolCallIndex {
	return &ToolCallIndex{
		callFirst:   make(map[[32]byte]int),
		callResults: make(map[[32]byte][32]byte),
	}
}

// Reset clears the index. Call between sessions.
func (idx *ToolCallIndex) Reset() {
	idx.mu.Lock()
	idx.callFirst = make(map[[32]byte]int)
	idx.callResults = make(map[[32]byte][32]byte)
	idx.mu.Unlock()
}

// CollapseRepeated scans messages[0:prefixEnd] for repeated identical tool call+result pairs
// and replaces the duplicate results with a compact reference.
// Returns total bytes saved.
func (idx *ToolCallIndex) CollapseRepeated(messages []types.Message, prefixEnd int) int {
	if prefixEnd <= 1 {
		return 0
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	toolUses := buildToolUseIndex(messages, prefixEnd)

	// Second pass: for each tool_result, look up its tool_use and check for repeats.
	saved := 0
	for i := 0; i < prefixEnd; i++ {
		newContent := make([]types.ContentBlock, len(messages[i].Content))
		copy(newContent, messages[i].Content)
		changed := false

		for bi, block := range newContent {
			if block.Type != "tool_result" {
				continue
			}
			use, useOK := resolveToolUseInfo(block, toolUses)
			callKey := ExtractToolCallKeyWithIndex(block, toolUses)
			if !useOK && callKey == "" {
				continue
			}

			callHash := hashToolCallKey(callKey)
			if useOK {
				callHash = hashToolCall(use.name, use.input)
			}
			resultHash := sha256.Sum256([]byte(block.Text))

			first, exists := idx.callFirst[callHash]
			if !exists {
				// First time we see this call: record it
				idx.callFirst[callHash] = i
				idx.callResults[callHash] = resultHash
				continue
			}

			// Seen before: check if result is identical
			if first == i {
				continue // same message (shouldn't happen, but guard)
			}
			if idx.callResults[callHash] != resultHash {
				// Different result -> meaningful change, don't collapse
				continue
			}

			// Identical call + result: collapse only when replacement is shorter
			orig := block.Text
			label := "tool"
			if useOK && use.name != "" {
				label = use.name
			}
			replacement := fmt.Sprintf("[Identical to %s result in message %d]", label, first)
			if len(replacement) >= len(orig) {
				continue // no byte savings
			}
			newContent[bi].Text = replacement
			saved += len(orig) - len(replacement)
			changed = true
		}

		if changed {
			messages[i].Content = newContent
		}
	}

	return saved
}

// hashToolCall returns a SHA-256 hash of the tool name and input.
func hashToolCall(name, input string) [32]byte {
	// Normalize input JSON for stable comparison
	normalized := normalizeJSON(input)
	return hashToolCallKey(strings.ToLower(name) + "|" + normalized)
}

func hashToolCallKey(key string) [32]byte {
	return sha256.Sum256([]byte(key))
}

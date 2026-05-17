package staleread

import (
	"fmt"

	"github.com/slimference/slimference/internal/types"
)

// DefaultMutateTools lists the tool names that mutate a file's
// on-disk state. tool_use blocks with these names are treated as
// mutations for T174 obsolete-read detection.
var DefaultMutateTools = []string{"apply_patch", "Write", "Edit", "MultiEdit"}

// ObsoleteOptions tunes the multi-turn obsolete-read prune. Zero
// values fall back to safe defaults.
type ObsoleteOptions struct {
	// ReadToolNames lists the tool names treated as file reads (same
	// semantics as Options.ReadToolNames). Defaults to {"Read"}.
	ReadToolNames []string
	// MutateToolNames lists the tool names treated as file
	// mutations. Defaults to DefaultMutateTools.
	MutateToolNames []string
}

// ObsoleteStats summarises what PruneObsoleteReads did.
type ObsoleteStats struct {
	BlocksReplaced int
	BytesReplaced  int
	PathsPruned    int
}

// PruneObsoleteReads walks the message slice, identifies reads that
// happened before a subsequent mutation of the same file, and
// replaces those obsolete reads with a compact marker. The model
// retains the post-mutation state via later messages; the older
// read content is wrong-now and dropped to save input tokens.
//
// The function is independent from AgeMessages; both can run in
// sequence on the same slice. PruneObsoleteReads must run after
// AgeMessages so it sees the aged markers and does not pointlessly
// re-rewrite them.
func PruneObsoleteReads(messages []types.Message, opts ObsoleteOptions) ([]types.Message, ObsoleteStats) {
	if len(messages) == 0 {
		return messages, ObsoleteStats{}
	}
	readTools := opts.ReadToolNames
	if len(readTools) == 0 {
		readTools = []string{"Read"}
	}
	mutTools := opts.MutateToolNames
	if len(mutTools) == 0 {
		mutTools = DefaultMutateTools
	}
	readSet := make(map[string]struct{}, len(readTools))
	for _, n := range readTools {
		readSet[n] = struct{}{}
	}
	mutSet := make(map[string]struct{}, len(mutTools))
	for _, n := range mutTools {
		mutSet[n] = struct{}{}
	}

	// First pass: tool_use_id → path for Read uses;
	// path → earliest mutation turn for Mutate uses.
	idToPath := map[string]string{}
	firstMutationTurn := map[string]int{}
	for i, msg := range messages {
		for _, blk := range msg.Content {
			if blk.Type != "tool_use" {
				continue
			}
			if _, isRead := readSet[blk.ToolName]; isRead {
				if blk.ToolUseID != "" {
					if path := extractPath(blk.ToolInput); path != "" {
						idToPath[blk.ToolUseID] = path
					}
				}
				continue
			}
			if _, isMut := mutSet[blk.ToolName]; isMut {
				if path := extractPath(blk.ToolInput); path != "" {
					// Iteration is monotonic in i, so the first
					// time we observe a mutation of `path` is
					// also the earliest. Skip later mutations.
					if _, ok := firstMutationTurn[path]; !ok {
						firstMutationTurn[path] = i
					}
				}
			}
		}
	}
	if len(idToPath) == 0 || len(firstMutationTurn) == 0 {
		return messages, ObsoleteStats{}
	}

	// Second pass: rewrite obsolete reads.
	stats := ObsoleteStats{}
	prunedPaths := map[string]struct{}{}
	out := make([]types.Message, len(messages))
	copy(out, messages)
	for i, msg := range out {
		var newContent []types.ContentBlock
		mutated := false
		for j, blk := range msg.Content {
			if blk.Type != "tool_result" {
				continue
			}
			refID := toolResultRefID(blk)
			path, ok := idToPath[refID]
			if !ok {
				continue
			}
			mutTurn, hasMut := firstMutationTurn[path]
			if !hasMut || mutTurn <= i {
				continue
			}
			if !mutated {
				newContent = make([]types.ContentBlock, len(msg.Content))
				copy(newContent, msg.Content)
				mutated = true
			}
			origLen := len(blk.Text)
			marker := fmt.Sprintf("[obsolete: %s edited at turn %d]", path, mutTurn)
			// Preserve all metadata (CacheControl, ArchiveID, the
			// caller-set id field) by copying the block and only
			// substituting the text body.
			rewritten := blk
			rewritten.Text = marker
			rewritten.RawBlock = nil
			newContent[j] = rewritten
			stats.BlocksReplaced++
			stats.BytesReplaced += origLen - len(marker)
			prunedPaths[path] = struct{}{}
		}
		if mutated {
			out[i].Content = newContent
		}
	}
	stats.PathsPruned = len(prunedPaths)
	return out, stats
}

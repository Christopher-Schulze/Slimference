package staleread

import (
	"fmt"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/types"
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

	// First pass: tool_use_id -> read identity for Read/safe shell reads;
	// path -> mutation turns for explicit file mutations. Keep every mutation
	// turn so reads between two edits can still be pruned by the later edit.
	idToPath := map[string]string{}
	mutationTurns := map[string][]int{}
	for i, msg := range messages {
		for _, blk := range msg.Content {
			if blk.Type != "tool_use" {
				continue
			}
			if blk.ToolUseID != "" {
				if path := extractReadIdentity(blk, readSet); path != "" {
					idToPath[blk.ToolUseID] = path
				}
			}
			if _, isMut := mutSet[blk.ToolName]; isMut {
				if path := extractPath(blk.ToolInput); path != "" {
					mutationTurns[path] = append(mutationTurns[path], i)
				}
				continue
			}
			if looksLikeShellToolName(blk.ToolName) {
				for _, path := range shellMutationPaths(blk.ToolInput) {
					mutationTurns[path] = append(mutationTurns[path], i)
				}
			}
		}
	}
	if len(idToPath) == 0 || len(mutationTurns) == 0 {
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
			mutTurn, hasMut := firstMutationAfter(mutationTurns[path], i)
			if !hasMut || mutTurn <= i {
				continue
			}
			if !mutated {
				newContent = make([]types.ContentBlock, len(msg.Content))
				copy(newContent, msg.Content)
				mutated = true
			}
			origLen := len(blk.Text)
			marker := fmt.Sprintf("[context-elided kind=obsolete-read path=%q edited_turn=%d]", path, mutTurn)
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

func firstMutationAfter(turns []int, readTurn int) (int, bool) {
	for _, turn := range turns {
		if turn > readTurn {
			return turn, true
		}
	}
	return 0, false
}

func shellMutationPaths(rawInput string) []string {
	commandLine, workdir := shellCommandInput(rawInput)
	if commandLine == "" || !strings.Contains(commandLine, "apply_patch") {
		return nil
	}
	var out []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || path == "/dev/null" {
			return
		}
		path = strings.TrimPrefix(path, "a/")
		path = strings.TrimPrefix(path, "b/")
		out = append(out, pathWithWorkdir(path, workdir))
	}
	for _, line := range strings.Split(commandLine, "\n") {
		for _, prefix := range []string{"*** Update File: ", "*** Add File: ", "*** Delete File: ", "+++ ", "--- "} {
			if strings.HasPrefix(line, prefix) {
				add(strings.TrimPrefix(line, prefix))
			}
		}
	}
	return compactPaths(out)
}

func compactPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

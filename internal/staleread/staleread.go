// Package staleread replaces redundant older file-read tool_results
// with compact markers when the same file has been read more
// recently in the same conversation. The latest read of any given
// path stays verbatim; older reads collapse to one-line markers
// referencing the newer read. The model retains the current file
// content (via the latest read) and can request a re-read by name
// if it needs the historical version.
//
// Scope: T170 aging policy. Lossless in the common case where the
// model only cares about the current state of each file. Pairs with
// staleread.Pruner / future filetracker hooks for cross-turn
// mutation awareness (T174).
package staleread

import (
	"encoding/json"
	"fmt"

	"github.com/slimference/slimference/internal/types"
)

// DefaultMinTurnGap is the minimum number of messages between an old
// read and its superseding fresh read before aging fires. Smaller
// values are aggressive (drop reads only one turn apart); larger
// values are conservative.
const DefaultMinTurnGap = 3

// Options tunes the aging engine. Zero values fall back to safe
// defaults.
type Options struct {
	// MinTurnGap is the minimum message-index distance between an
	// older read and its newer superseding read. Defaults to
	// DefaultMinTurnGap when zero.
	MinTurnGap int
	// ReadToolNames lists the tool names treated as file reads. When
	// empty, defaults to {"Read"} (Codex + Claude convention).
	ReadToolNames []string
}

// Stats summarises what AgeMessages did. Reported via slog and
// counter telemetry.
type Stats struct {
	BlocksReplaced int
	BytesReplaced  int
	PathsAged      int
}

// AgeMessages walks the message slice, identifies tool_use blocks for
// file-read operations, finds older reads of the same file, and
// returns a new slice where those older reads are replaced with
// `[stale read: <path> superseded by turn N]` markers. The input
// slice and its inner ContentBlock slices are not mutated.
func AgeMessages(messages []types.Message, opts Options) ([]types.Message, Stats) {
	if len(messages) == 0 {
		return messages, Stats{}
	}
	if opts.MinTurnGap <= 0 {
		opts.MinTurnGap = DefaultMinTurnGap
	}
	readTools := opts.ReadToolNames
	if len(readTools) == 0 {
		readTools = []string{"Read"}
	}
	readSet := map[string]struct{}{}
	for _, n := range readTools {
		readSet[n] = struct{}{}
	}

	// First pass: tool_use_id → path. Only Read-family tool uses.
	idToPath := map[string]string{}
	for _, msg := range messages {
		for _, blk := range msg.Content {
			if blk.Type != "tool_use" {
				continue
			}
			if _, isRead := readSet[blk.ToolName]; !isRead {
				continue
			}
			if blk.ToolUseID == "" {
				continue
			}
			if path := extractPath(blk.ToolInput); path != "" {
				idToPath[blk.ToolUseID] = path
			}
		}
	}
	if len(idToPath) == 0 {
		return messages, Stats{}
	}

	// Second pass: find the latest message index that carries a
	// tool_result for each file path. tool_result blocks store the
	// originating tool_use_id in ToolResultID (not ToolUseID -
	// ToolUseID is set on the tool_use block itself).
	type readSite struct {
		msgIdx, blockIdx int
	}
	latestRead := map[string]readSite{}
	for i, msg := range messages {
		for j, blk := range msg.Content {
			if blk.Type != "tool_result" {
				continue
			}
			refID := toolResultRefID(blk)
			path, ok := idToPath[refID]
			if !ok {
				continue
			}
			latestRead[path] = readSite{msgIdx: i, blockIdx: j}
		}
	}

	// Third pass: rewrite older reads.
	stats := Stats{}
	agedPaths := map[string]struct{}{}
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
			latest, hasLatest := latestRead[path]
			if !hasLatest || latest.msgIdx <= i {
				continue
			}
			if latest.msgIdx-i < opts.MinTurnGap {
				continue
			}
			if !mutated {
				newContent = make([]types.ContentBlock, len(msg.Content))
				copy(newContent, msg.Content)
				mutated = true
			}
			origLen := len(blk.Text)
			marker := fmt.Sprintf("[stale read: %s superseded by turn %d]", path, latest.msgIdx)
			// Preserve every metadata field except the text body so
			// CacheControl, ArchiveID, and whichever id field the
			// caller set survive the rewrite.
			rewritten := blk
			rewritten.Text = marker
			rewritten.RawBlock = nil // force re-marshal from our shape
			newContent[j] = rewritten
			stats.BlocksReplaced++
			stats.BytesReplaced += origLen - len(marker)
			agedPaths[path] = struct{}{}
		}
		if mutated {
			out[i].Content = newContent
		}
	}
	stats.PathsAged = len(agedPaths)
	return out, stats
}

// toolResultRefID returns the tool_use_id this tool_result is
// answering. Anthropic responses land it in ToolResultID; other
// shapes (e.g. some Codex wire variants) may populate ToolUseID
// directly. Try both so the matcher works regardless.
func toolResultRefID(blk types.ContentBlock) string {
	if blk.ToolResultID != "" {
		return blk.ToolResultID
	}
	return blk.ToolUseID
}

// extractPath inspects a tool_use's serialized JSON input for the
// most common file-path keys. Returns "" when no recognised path is
// present.
func extractPath(rawInput string) string {
	if rawInput == "" {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(rawInput), &m); err != nil {
		return ""
	}
	for _, key := range []string{"path", "file_path", "filename", "file"} {
		if v, ok := m[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

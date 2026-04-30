package summarization

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/slimference/slimference/internal/types"
)

// RepetitionStaircase describes how the Nth occurrence of the same
// (tool_name, topic) should be discounted relative to the first one. Index 0
// maps to the first occurrence (always 1.0 by construction), index 1 to the
// second, index 2+ to any later occurrence. The summarization hint uses the
// staircase to communicate the discount to MiniMax.
var RepetitionStaircase = []float64{1.0, 0.5, 0.25}

// toolRepetition captures one tool/topic key plus every message index where
// it was observed, in chronological order.
type toolRepetition struct {
	key     string
	indices []int
}

// BuildRepetitionIndex walks the conversation in order and returns a slice
// of toolRepetition entries describing every (tool_name, topic) group that
// occurred more than once. Groups with a single occurrence are omitted -
// they add no information to the hint.
//
// The "topic" component is derived from the tool call id, tool name, and
// the first few leading characters of the result content, which is a
// deliberately coarse signal. False merges are preferred over false splits:
// a shared key causes MiniMax to lean on the repetition hint; a split
// simply means the hint stays silent.
func BuildRepetitionIndex(messages []types.Message) []toolRepetition {
	buckets := make(map[string]*toolRepetition)
	var order []string

	for _, msg := range messages {
		for _, blk := range msg.Content {
			if blk.Type != "tool_result" && blk.Type != "tool_use" {
				continue
			}
			key := extractToolKey(blk)
			if key == "" {
				continue
			}
			rep, ok := buckets[key]
			if !ok {
				rep = &toolRepetition{key: key}
				buckets[key] = rep
				order = append(order, key)
			}
			rep.indices = append(rep.indices, msg.Index)
		}
	}

	result := make([]toolRepetition, 0, len(order))
	for _, key := range order {
		rep := buckets[key]
		if len(rep.indices) < 2 {
			continue
		}
		// Deduplicate consecutive equal indices that can happen when the
		// same block appears twice in one message.
		uniq := rep.indices[:0]
		last := -1
		for _, idx := range rep.indices {
			if idx != last {
				uniq = append(uniq, idx)
				last = idx
			}
		}
		if len(uniq) < 2 {
			continue
		}
		result = append(result, toolRepetition{key: rep.key, indices: uniq})
	}
	return result
}

// extractToolKey builds a coarse (tool_name, topic) key for a tool_result
// or tool_use block. The key is stable across identical tool calls so
// repeats can be detected.
func extractToolKey(block types.ContentBlock) string {
	name := strings.ToLower(strings.TrimSpace(block.ToolName))
	if name == "" && block.ToolUseID != "" {
		// Fall back to the tool_use id as the name when no explicit name is
		// attached (rare).
		name = "call:" + block.ToolUseID
	}
	if name == "" {
		return ""
	}
	topic := extractTopicSignal(block.Text)
	if topic == "" {
		return name
	}
	return name + "|" + topic
}

// extractTopicSignal returns a stable short signature for the content: the
// first non-empty line's leading word(s) truncated to at most 48 runes.
// Matches are intentionally loose; the goal is to recognise "same tool,
// same target" without requiring byte-for-byte equivalence.
func extractTopicSignal(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	if nl := strings.IndexByte(trimmed, '\n'); nl >= 0 {
		trimmed = trimmed[:nl]
	}
	// Keep at most the first 48 runes.
	if len(trimmed) > 48 {
		trimmed = trimmed[:48]
	}
	return trimmed
}

// RepetitionHint builds a human-readable directive that tells the downstream
// summariser how to treat repeated tool calls. It stays silent when no
// repetition was detected.
func RepetitionHint(messages []types.Message) string {
	groups := BuildRepetitionIndex(messages)
	if len(groups) == 0 {
		return ""
	}
	// Sort deterministically so the emitted hint is stable.
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].indices) != len(groups[j].indices) {
			return len(groups[i].indices) > len(groups[j].indices)
		}
		return groups[i].key < groups[j].key
	})

	var sb strings.Builder
	sb.WriteString("Repetition guidance:\n")
	for _, g := range groups {
		sb.WriteString("- ")
		sb.WriteString(g.key)
		sb.WriteString(" appears ")
		sb.WriteString(strconv.Itoa(len(g.indices)))
		sb.WriteString("x (messages ")
		for i, idx := range g.indices {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(strconv.Itoa(idx))
			sb.WriteString(describeDiscount(i))
		}
		sb.WriteString(")\n")
	}
	sb.WriteString("For each repeated tool call, preserve the latest occurrence fully and compress earlier ones per the discount factor shown in parentheses.\n")
	return sb.String()
}

// describeDiscount returns a human-readable suffix for the Nth occurrence.
// First occurrence: no suffix. Later occurrences: the staircase factor.
func describeDiscount(occurrenceIdx int) string {
	if occurrenceIdx == 0 {
		return ""
	}
	factor := repetitionFactor(occurrenceIdx)
	return fmt.Sprintf("@%d%%", int(factor*100))
}

// repetitionFactor returns the staircase factor for the Nth occurrence
// (0-indexed). Out-of-range indices clamp to the last configured step.
func repetitionFactor(occurrenceIdx int) float64 {
	if occurrenceIdx < 0 {
		return 1.0
	}
	if occurrenceIdx >= len(RepetitionStaircase) {
		return RepetitionStaircase[len(RepetitionStaircase)-1]
	}
	return RepetitionStaircase[occurrenceIdx]
}

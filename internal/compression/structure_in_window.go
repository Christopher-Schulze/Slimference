package compression

import (
	"log/slog"
	"strings"

	"github.com/slimference/slimference/internal/types"
)

// structureInWindowPass walks messages in [startIdx, len(messages)-1) and
// replaces large tool_result code bodies with their structural signature.
// The very last message is always preserved verbatim because the model
// is still reasoning about it.
//
// Safety invariants (T24):
//   - role="user" messages are never modified (user just typed them).
//   - tool_result blocks smaller than StructureInWindowMinTokens are left
//     untouched.
//   - blocks that look like PR / diff / patch context are skipped.
//   - zero-downside: if the structural summary is not strictly shorter
//     than the original, the block is unchanged.
func (c *DeterministicCompressor) structureInWindowPass(messages []types.Message, startIdx int) int {
	saved := 0
	minTokens := c.cfg.Tuning.StructureInWindowMinTokens
	if minTokens <= 0 {
		minTokens = 1500
	}
	lastIdx := len(messages) - 1
	for i := startIdx; i < lastIdx; i++ {
		msg := messages[i]
		if !shouldStructureInWindowMessage(msg) {
			continue
		}
		blocks := msg.Content
		for bi, block := range blocks {
			if !shouldStructureInWindowBlock(block, minTokens) {
				continue
			}
			lang := c.detectLanguage(block, block.Text)
			if lang == "" || !c.structureLangAllowed(lang) {
				continue
			}
			summary, changed := c.structExtractor.Extract(block.Text, lang)
			if !changed || len(summary) >= len(block.Text) {
				continue
			}
			delta := len(block.Text) - len(summary)
			blocks[bi].Text = summary
			messages[i].Metadata.WasStructured = true
			saved += delta
			slog.Debug("structure_in_window applied",
				slog.Int("msg_idx", i),
				slog.String("lang", lang),
				slog.Int("saved_bytes", delta),
			)
		}
	}
	return saved
}

// shouldStructureInWindowMessage decides whether a message is a candidate
// for in-window compression. User turns are always preserved because they
// carry the most recent operator intent.
func shouldStructureInWindowMessage(msg types.Message) bool {
	switch msg.Role {
	case "user":
		return false
	}
	if len(msg.Content) == 0 {
		return false
	}
	return true
}

// shouldStructureInWindowBlock applies per-block safety rules. Only
// tool_result blocks large enough to carry real signature value, and not
// already compressed by another Layer 1 sub-layer, are eligible.
func shouldStructureInWindowBlock(block types.ContentBlock, minTokens int) bool {
	if block.Type != "tool_result" {
		return false
	}
	if block.Text == "" {
		return false
	}
	if isPreFiltered(block.Text) {
		return false
	}
	if looksLikeDiffOrPatch(block.Text) {
		return false
	}
	estimatedTokens := len(block.Text) / 4
	return estimatedTokens >= minTokens
}

// looksLikeDiffOrPatch is a conservative check for content the model would
// expect byte-for-byte: unified diffs, git patches, PR body text.
func looksLikeDiffOrPatch(text string) bool {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "diff --git") {
		return true
	}
	if strings.HasPrefix(trimmed, "--- ") && strings.Contains(trimmed, "\n+++ ") {
		return true
	}
	if strings.HasPrefix(trimmed, "@@") {
		return true
	}
	if strings.HasPrefix(trimmed, "From ") && strings.Contains(trimmed, "Subject:") {
		return true
	}
	return false
}

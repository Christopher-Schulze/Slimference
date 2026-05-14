package compression

import (
	"log/slog"

	"github.com/slimference/slimference/internal/tokens"
	"github.com/slimference/slimference/internal/types"
)

func (c *DeterministicCompressor) toolOutputInWindowPass(messages []types.Message, startIdx int, toolUses map[string]toolUseInfo) int {
	minTokens := c.cfg.Tuning.ToolOutputInWindowMinTokens
	if minTokens <= 0 {
		minTokens = 800
	}
	saved := 0
	lastIdx := len(messages) - 1
	for i := startIdx; i < lastIdx; i++ {
		msg := messages[i]
		if !shouldToolOutputInWindowMessage(msg) {
			continue
		}
		for bi, block := range msg.Content {
			if !shouldToolOutputInWindowBlock(block, minTokens) {
				continue
			}
			resolvedBlock := block
			if use, ok := resolveToolUseInfo(block, toolUses); ok {
				resolvedBlock.ToolName = use.name
				resolvedBlock.ToolInput = use.input
			}
			toolType := classifyToolResultWithInput(resolvedBlock.ToolName, resolvedBlock.ToolInput, block.Text)
			if !toolOutputInWindowTypeAllowed(toolType) {
				continue
			}
			compressed := compressToolOutput(toolType, block.Text, minTokens*3, c.cfg.SlidingWindow)
			if len(compressed) >= len(block.Text) {
				continue
			}
			if id := c.archiveOriginal(i, bi, "tool_output_in_window", block.Text); id != "" {
				block.ArchiveID = id
			}
			block.Text = compressed
			msg.Content[bi] = block
			messages[i] = msg
			delta := len(resolvedBlock.Text) - len(compressed)
			saved += delta
			slog.Debug("tool_output_in_window applied",
				slog.Int("msg_idx", i),
				slog.Int("tool_type", int(toolType)),
				slog.Int("saved_bytes", delta),
			)
		}
	}
	return saved
}

func shouldToolOutputInWindowMessage(msg types.Message) bool {
	if msg.Role == "user" || msg.Metadata.WasDeduped {
		return false
	}
	return len(msg.Content) > 0
}

func shouldToolOutputInWindowBlock(block types.ContentBlock, minTokens int) bool {
	if block.Type != "tool_result" || block.Text == "" {
		return false
	}
	if isPreFiltered(block.Text) || looksLikeDiffOrPatch(block.Text) {
		return false
	}
	return tokens.CountString(block.Text) >= minTokens
}

func toolOutputInWindowTypeAllowed(toolType types.ToolResultType) bool {
	switch toolType {
	case types.ToolTypeSearchResult,
		types.ToolTypeTestOutput,
		types.ToolTypeBuildOutput,
		types.ToolTypeLintOutput,
		types.ToolTypeLogOutput,
		types.ToolTypeDirListing:
		return true
	default:
		return false
	}
}

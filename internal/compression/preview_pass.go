package compression

import (
	"log/slog"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// structurePreviewPass walks compressible-prefix messages and replaces the
// text of each tool_result block with a shape-aware preview (StructurePreview)
// whenever the preview is strictly shorter than the current text. Other
// sub-layers run first, so this only fires when they left the text unchanged
// (or only marginally compressed). Returns the total bytes saved.
//
// T38: opt-in via [compression.tuning] structure_preview.
func (c *DeterministicCompressor) structurePreviewPass(messages []types.Message, prefixEnd int) int {
	saved := 0
	for i := 0; i < prefixEnd && i < len(messages); i++ {
		msg := messages[i]
		if !shouldPreviewMessage(msg) {
			continue
		}
		blocks := msg.Content
		for bi, block := range blocks {
			if !shouldPreviewBlock(block) {
				continue
			}
			c.recordLayer1Attempt("preview_pass")
			preview, ok := StructurePreview(block.Text)
			if !ok {
				continue
			}
			// StructurePreview's contract guarantees preview is strictly
			// shorter whenever ok==true, so delta is always > 0 here.
			delta := len(block.Text) - len(preview)
			// Archive-required preview is fail-closed: without a valid
			// archive id the original block stays model-facing.
			id := c.archiveOriginal(i, bi, "preview_pass", block.Text)
			if id == "" {
				continue
			}
			blocks[bi].ArchiveID = id
			blocks[bi].Text = preview
			saved += delta
			slog.Debug("structure_preview applied",
				slog.Int("msg_idx", i),
				slog.Int("saved_bytes", delta),
			)
		}
	}
	return saved
}

// shouldPreviewMessage gates per-message eligibility. User turns are
// always preserved; deduped messages are already minimal.
func shouldPreviewMessage(msg types.Message) bool {
	if msg.Role == "user" {
		return false
	}
	if msg.Metadata.WasDeduped {
		return false
	}
	return true
}

// shouldPreviewBlock gates per-block eligibility: tool_result only,
// non-empty, not pre-filtered, and large enough to be worth a preview.
func shouldPreviewBlock(block types.ContentBlock) bool {
	if block.Type != "tool_result" {
		return false
	}
	if block.Text == "" || len(block.Text) < PreviewThresholdBytes {
		return false
	}
	if isPreFiltered(block.Text) {
		return false
	}
	return true
}

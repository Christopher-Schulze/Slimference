package compression

import (
	"encoding/json"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/tokens"
	"github.com/slimference/slimference/internal/types"
)

// Layer1Result carries the compressed message list and per-sub-layer savings metrics.
type Layer1Result struct {
	Messages              []types.Message
	TokensSaved           int
	ANSISaved             int
	JSONSaved             int
	DedupSaved            int
	CommentSaved          int
	StructureSaved        int
	DeltaSaved            int
	SuccessShortSaved     int
	ToolCompressorSaved   int
	ImageSaved            int
	RepeatedCollapseSaved int
	GraphPruningSaved     int
	DictionarySaved       int
	// PreviewSaved is T38: shape-aware preview of large tool_result blocks.
	PreviewSaved int
	// LoopNudgeSaved is T37: injected retry-loop nudge estimate.
	LoopNudgeSaved int
}

// DeterministicCompressor runs Layer 1 sub-layers deterministically and synchronously.
type DeterministicCompressor struct {
	cfg             *config.CompressionConfig
	contentIndex    *ContentIndex
	fileTracker     *FileVersionTracker
	structExtractor *StructureExtractor
	toolCallIndex   *ToolCallIndex
	fileOpGraph     *FileOpGraph
	// activeDedupThreshold holds the staircase-resolved dedup similarity
	// threshold for the current Compress() call. Computed once per call
	// so every compressMessage invocation uses the same value.
	activeDedupThreshold float64
	// activeSessionID scopes archive entries for the current Compress()
	// invocation. Set at the top of CompressWithSession so per-call value
	// flows through compressMessage and the cross-message passes without
	// racy concurrent mutation: callers must hold one compressor per
	// in-flight session, or serialize their CompressWithSession calls.
	activeSessionID string
	// recorder archives original block content before lossy sub-layers
	// mutate it. Optional; nil means "no archiving" and every helper
	// short-circuits cheaply. T76.
	recorder MutationRecorder
	// coordinatorSubsume signals that Layer 2 will summarise the prefix
	// being processed. T100: when true, heavy L1 sub-layers (dedup,
	// structure, delta, tool-compressor, success-short, image-replace)
	// are skipped on the prefix because L2 will replace it anyway.
	// Cheap idempotent passes (ANSI strip, JSON compact) still run.
	coordinatorSubsume bool
	// recordMu serialises recorder.Record calls and coordinatorSkipped
	// updates so compressMessage is safe to call from multiple goroutines
	// when CoordinatorParallel is on (T104).
	recordMu sync.Mutex
	// coordinatorSkipped counts how often the coordinator skipped a
	// per-block heavy pass for /admin/status.compression.coordinator.
	coordinatorSkipped atomic.Int64
}

// resolveDedupThreshold applies the T53 staircase: the first step whose
// MsgCountLE covers msgCount wins. Zero / invalid entries fall back to the
// scalar Compression.DedupSimilarityThreshold so legacy configs keep their
// behaviour byte-equal.
func (c *DeterministicCompressor) resolveDedupThreshold(msgCount int) float64 {
	for _, step := range c.cfg.Tuning.DedupStaircase {
		if msgCount <= step.MsgCountLE {
			if step.Threshold <= 0 || step.Threshold > 1 {
				return c.cfg.DedupSimilarityThreshold
			}
			return step.Threshold
		}
	}
	return c.cfg.DedupSimilarityThreshold
}

// NewDeterministicCompressor returns a fully initialized DeterministicCompressor.
func NewDeterministicCompressor(cfg *config.CompressionConfig) *DeterministicCompressor {
	return &DeterministicCompressor{
		cfg:             cfg,
		contentIndex:    NewContentIndex(),
		fileTracker:     NewFileVersionTracker(),
		structExtractor: NewStructureExtractor(),
		toolCallIndex:   NewToolCallIndex(),
		fileOpGraph:     NewFileOpGraph(),
	}
}

// Compress applies Layer 1 to messages that fall outside the sliding window
// of recent user exchanges. Equivalent to CompressWithSession with an empty
// sessionID; archive entries written during this call are not session-tagged.
// See CompressiblePrefixEnd for the exact boundary.
func (c *DeterministicCompressor) Compress(messages []types.Message) Layer1Result {
	return c.CompressWithSession("", messages)
}

// SetCoordinatorSubsume tells the compressor that Layer 2 will replace
// the messages being passed in this call, so heavy L1 sub-layers can
// skip on the prefix. T100. The flag is reset at the end of each
// CompressWithSession call so callers must set it per-request.
func (c *DeterministicCompressor) SetCoordinatorSubsume(v bool) { c.coordinatorSubsume = v }

// CoordinatorSkipped returns the cumulative count of per-block heavy
// passes the coordinator decided to skip. T100 telemetry.
func (c *DeterministicCompressor) CoordinatorSkipped() int { return int(c.coordinatorSkipped.Load()) }

// CompressWithSession is the session-aware Compress entry point. The
// sessionID is stamped on every archive entry produced during this call so
// the proxy can later filter or attribute by session. Callers that share a
// single compressor across concurrent requests MUST serialize calls or use
// per-session compressors; the active session id is held on the receiver
// for the duration of the call.
func (c *DeterministicCompressor) CompressWithSession(sessionID string, messages []types.Message) Layer1Result {
	c.activeSessionID = sessionID
	defer func() { c.activeSessionID = "" }()

	result := Layer1Result{
		Messages: messages,
	}

	if len(messages) == 0 {
		return result
	}

	// T53: resolve the dedup similarity threshold once per call. The
	// staircase lowers the threshold for longer conversations, where
	// near-duplicates accumulate naturally (retry build output, repeated
	// log tails, etc.). An empty staircase keeps the scalar fallback.
	c.activeDedupThreshold = c.resolveDedupThreshold(len(messages))

	// T37 loop nudge runs first so any downstream compression sees the
	// nudged text. Opt-in via [compression.tuning] loop_detection.
	if c.cfg.Tuning.LoopDetection {
		strategy := ResolveLoopStrategy(StrategyConfig{
			LoopDetection: c.cfg.Tuning.LoopDetection,
			LoopStrategy:  c.cfg.Tuning.LoopStrategy,
		})
		if newMsgs, saved := strategy.Apply(messages); saved > 0 {
			messages = newMsgs
			result.LoopNudgeSaved = saved
			result.Messages = messages
		}
	}

	prefixEnd := CompressiblePrefixEnd(messages, c.cfg.SlidingWindow)
	if prefixEnd <= 0 {
		if c.cfg.Tuning.ToolOutputInWindow && len(messages) > 1 {
			out := make([]types.Message, len(messages))
			copy(out, messages)
			toolUses := buildToolUseIndex(messages, len(messages))
			saved := c.toolOutputInWindowPass(out, 0, toolUses)
			if saved > 0 {
				result.ToolCompressorSaved = saved
				result.TokensSaved = saved
				result.Messages = out
			}
		}
		// T24: even when the compressible prefix is empty, in-window
		// structure extraction (opt-in) can still compress large tool
		// outputs in the middle of the conversation.
		if c.cfg.Tuning.StructureInWindow && len(messages) >= 2 {
			out := result.Messages
			saved := c.structureInWindowPass(out, 0)
			if saved > 0 {
				result.StructureSaved = saved
				result.TokensSaved += saved
				result.Messages = out
			}
		}
		return result
	}

	out := make([]types.Message, len(messages))
	copy(out, messages)
	toolUses := buildToolUseIndex(messages, len(messages))

	// T104: message-level fan-out. The original spec asked for
	// sub-layer-level concurrency (ANSI/image/JSON-compact in parallel
	// per block); shipping at message granularity is strictly cheaper
	// to reason about (no shared state inside compressMessage except
	// the recorder + counters, both protected) and saturates the same
	// CPU budget. Bounded by GOMAXPROCS so a 4-core machine spawns at
	// most 4 in-flight compressMessage calls.
	if c.cfg.Tuning.CoordinatorParallel && prefixEnd > 1 {
		type fanOut struct {
			msg                                       types.Message
			js, ds, cs, ss, d2, as, sc, ts, ims, dict int
		}
		fan := make([]fanOut, prefixEnd)
		var wg sync.WaitGroup
		sem := make(chan struct{}, runtime.GOMAXPROCS(0))
		for i := 0; i < prefixEnd; i++ {
			wg.Add(1)
			sem <- struct{}{}
			go func(idx int) {
				defer wg.Done()
				defer func() { <-sem }()
				m := out[idx]
				m, js, ds, cs, ss, d2, as, sc, ts, ims, dict := c.compressMessage(m, idx, prefixEnd, toolUses)
				fan[idx] = fanOut{m, js, ds, cs, ss, d2, as, sc, ts, ims, dict}
			}(i)
		}
		wg.Wait()
		for i, r := range fan {
			out[i] = r.msg
			result.JSONSaved += r.js
			result.DedupSaved += r.ds
			result.CommentSaved += r.cs
			result.StructureSaved += r.ss
			result.DeltaSaved += r.d2
			result.ANSISaved += r.as
			result.SuccessShortSaved += r.sc
			result.ToolCompressorSaved += r.ts
			result.ImageSaved += r.ims
			result.DictionarySaved += r.dict
		}
	} else {
		for i := 0; i < prefixEnd; i++ {
			msg := out[i]
			msg, js, ds, cs, ss, ds2, as, sc, ts, ims, dict := c.compressMessage(msg, i, prefixEnd, toolUses)
			out[i] = msg
			result.JSONSaved += js
			result.DedupSaved += ds
			result.CommentSaved += cs
			result.StructureSaved += ss
			result.DeltaSaved += ds2
			result.ANSISaved += as
			result.SuccessShortSaved += sc
			result.ToolCompressorSaved += ts
			result.ImageSaved += ims
			result.DictionarySaved += dict
		}
	}

	// Cross-message optimizations (L1.12 and L1.13)
	result.RepeatedCollapseSaved = c.toolCallIndex.CollapseRepeated(out, prefixEnd)
	result.GraphPruningSaved = c.fileOpGraph.PruneRedundantWithArchive(out, prefixEnd, func(msgIdx, blockIdx int, original string) string {
		return c.archiveOriginal(msgIdx, blockIdx, "graph_pruning", original)
	})

	// T38 structure-aware preview. Runs after main sub-layers so preview
	// only fires when no other transformation replaced the raw text.
	if c.cfg.Tuning.StructurePreview {
		result.PreviewSaved = c.structurePreviewPass(out, prefixEnd)
	}

	// Short Codex CLI turns often keep the largest command output inside the
	// sliding window. Type-aware compaction is still safe there for large,
	// classified tool outputs because it leaves explicit omission markers.
	if c.cfg.Tuning.ToolOutputInWindow && prefixEnd < len(out)-1 {
		result.ToolCompressorSaved += c.toolOutputInWindowPass(out, prefixEnd, toolUses)
	}

	// T24: opt-in in-window structure extraction. Walks the tail of the
	// sliding window (excluding the very last message) and signatures large
	// tool_result blocks. Disabled by default; safety invariants are
	// enforced inside shouldStructureInWindow.
	if c.cfg.Tuning.StructureInWindow && prefixEnd < len(out)-1 {
		result.StructureSaved += c.structureInWindowPass(out, prefixEnd)
	}

	result.TokensSaved = result.JSONSaved + result.CommentSaved + result.StructureSaved + result.DeltaSaved +
		result.DedupSaved + result.ANSISaved + result.SuccessShortSaved +
		result.ToolCompressorSaved + result.ImageSaved +
		result.RepeatedCollapseSaved + result.GraphPruningSaved +
		result.DictionarySaved + result.PreviewSaved + result.LoopNudgeSaved
	result.Messages = out

	if result.TokensSaved > 0 {
		slog.Debug("layer1 compression complete",
			slog.Int("ansi_saved", result.ANSISaved),
			slog.Int("json_saved", result.JSONSaved),
			slog.Int("dedup_saved", result.DedupSaved),
			slog.Int("comment_saved", result.CommentSaved),
			slog.Int("structure_saved", result.StructureSaved),
			slog.Int("delta_saved", result.DeltaSaved),
			slog.Int("success_short_saved", result.SuccessShortSaved),
			slog.Int("tool_compressor_saved", result.ToolCompressorSaved),
			slog.Int("image_saved", result.ImageSaved),
			slog.Int("repeated_collapse_saved", result.RepeatedCollapseSaved),
			slog.Int("graph_pruning_saved", result.GraphPruningSaved),
			slog.Int("dictionary_saved", result.DictionarySaved),
			slog.Int("total_saved", result.TokensSaved),
		)
	}

	return result
}

// Pipeline order (spec+.md §5): ANSI → JSON compact OR comment strip → dedup (exact + MinHash)
// → regex structure → delta → classifier → tool compressor → success short-circuit → image.
// L1.12 (repeated collapse) and L1.13 (graph pruning) run cross-message after this loop.
func (c *DeterministicCompressor) compressMessage(
	msg types.Message, msgIdx, prefixEnd int, toolUses map[string]toolUseInfo,
) (out types.Message, jsonSaved, dedupSaved, commentSaved, structSaved, deltaSaved, ansiSaved, successShortSaved, toolSaved, imageSaved, dictionarySaved int) {
	out = msg
	newContent := make([]types.ContentBlock, len(msg.Content))
	copy(newContent, msg.Content)

	// messageAge: distance from compressible boundary; older = higher.
	messageAge := prefixEnd - msgIdx

	for bi, block := range newContent {
		// L1.11: Image replacement (applies to explicit image-type blocks)
		if block.Type == "image" {
			updated, saved := replaceImageBase64(block, msgIdx, prefixEnd)
			if saved > 0 {
				id := c.archiveOriginal(msgIdx, bi, "image_replace", block.ImageData)
				if id == "" {
					continue
				}
				updated.ArchiveID = id
				newContent[bi] = updated
				imageSaved += saved
			}
			continue
		}

		if block.Type != "tool_result" {
			continue
		}

		origText := block.Text
		if origText == "" {
			continue
		}

		originalLen := len(origText)
		metadataBefore := msg.Metadata
		jsonSavedBefore := jsonSaved
		dedupSavedBefore := dedupSaved
		commentSavedBefore := commentSaved
		structSavedBefore := structSaved
		deltaSavedBefore := deltaSaved
		ansiSavedBefore := ansiSaved
		successShortSavedBefore := successShortSaved
		toolSavedBefore := toolSaved
		imageSavedBefore := imageSaved
		dictionarySavedBefore := dictionarySaved

		// L1.14: Pre-Filtered Content Tagging - detect Layer 0 compact markers early.
		// When content was already filtered by "slimference filter", skip JSON compact,
		// comment strip, and structure extraction (redundant / could mangle compact format).
		preFiltered := isPreFiltered(origText)

		// L1.7: ANSI strip
		text := StripANSICodes(origText)
		if len(text) < len(origText) {
			ansiSaved += len(origText) - len(text)
			// ansi_strip is intentionally NOT recorded in
			// appliedSubLayers because it is treated as a
			// non-lossy normalisation by ansiOnlyChange below.
		}

		// L1.1/L1.2: JSON compact OR comment strip (mutually exclusive).
		// Skipped for pre-filtered content (already compact, mangling risk).
		jsonCompacted := false
		// T76b: pre-stage attribution carries forward into the post-dedup
		// stage's appliedSubLayers slice via this temporary list.
		var earlySubLayers []string
		if !preFiltered {
			if compacted, saved := compactJSONContent(text); saved > 0 {
				text = compacted
				jsonSaved += saved
				jsonCompacted = true
				earlySubLayers = append(earlySubLayers, "json_compact")
			} else {
				lang := c.detectLanguage(block, text)
				if lang != "" {
					if stripped := StripComments(text, lang); len(stripped) < len(text) {
						commentSaved += len(text) - len(stripped)
						text = stripped
						earlySubLayers = append(earlySubLayers, "comment_strip")
						slog.Debug("comment_strip applied",
							slog.String("lang", lang),
							slog.Int("msg_idx", msgIdx),
							slog.Int("saved_bytes", len(text)),
						)
					}
				}
			}
		}

		// T100: when the cross-direction coordinator decides Layer 2
		// will subsume this prefix, skip every heavy sub-layer below
		// (dedup, structure, delta, tool-compressor, success-short,
		// image-replace) and let the cheap ANSI/JSON passes above
		// stand. Counter is bumped per skipped block.
		if c.coordinatorSubsume {
			c.coordinatorSkipped.Add(1)
			if len(text) < originalLen || text != block.Text {
				if !ansiOnlyChange(origText, text) {
					tag := joinSubLayers(earlySubLayers)
					if layer1MutationRequiresArchive(earlySubLayers) {
						id := c.archiveOriginal(msgIdx, bi, tag, origText)
						if id == "" {
							jsonSaved = jsonSavedBefore
							dedupSaved = dedupSavedBefore
							commentSaved = commentSavedBefore
							structSaved = structSavedBefore
							deltaSaved = deltaSavedBefore
							ansiSaved = ansiSavedBefore
							successShortSaved = successShortSavedBefore
							toolSaved = toolSavedBefore
							imageSaved = imageSavedBefore
							dictionarySaved = dictionarySavedBefore
							msg.Metadata = metadataBefore
							continue
						}
						newContent[bi].ArchiveID = id
					} else if id := c.archiveOriginal(msgIdx, bi, tag, origText); id != "" {
						newContent[bi].ArchiveID = id
					}
				}
				newContent[bi].Text = text
			}
			continue
		}

		// L1.3: Dedup before structure / delta (spec order).
		// T53: activeDedupThreshold is staircase-resolved once per Compress().
		// Fall back to the scalar threshold for direct compressMessage calls
		// (tests) that bypass Compress().
		threshold := c.activeDedupThreshold
		if threshold <= 0 {
			threshold = c.cfg.DedupSimilarityThreshold
		}
		// T96/T107: per-session namespace prevents cross-session false-
		// positive duplicate references. activeSessionID is empty when
		// callers use the legacy Compress() entry point; in that case
		// the global namespace is used, preserving historical behaviour.
		exactDupe, nearDupe, firstIdx := c.contentIndex.CheckAndRecordForSession(c.activeSessionID, text, msgIdx, threshold)
		textTransformed := false // tracks whether delta/structure already rewrote text
		// T76b: track which sub-layers actually mutated this block so the
		// archive entry's sub_layer tag identifies the culprit (or the
		// chain) instead of the generic "layer1". Carries forward the
		// json_compact / comment_strip attribution from the early stage.
		appliedSubLayers := append([]string(nil), earlySubLayers...)
		if exactDupe {
			dedupSaved += len(text)
			text = formatDupeReference(firstIdx, msgIdx)
			msg.Metadata.WasDeduped = true
			appliedSubLayers = append(appliedSubLayers, "dedup")
		} else if nearDupe {
			dedupSaved += len(text)
			text = formatNearDupeReference(firstIdx, msgIdx)
			msg.Metadata.WasDeduped = true
			appliedSubLayers = append(appliedSubLayers, "dedup")
		} else if !jsonCompacted && !preFiltered {
			// L1.4: Structure extraction (skipped for pre-filtered content).
			lang := c.detectLanguage(block, text)
			if lang != "" && c.structureLangAllowed(lang) {
				if shouldRunStructureExtraction(text, c.cfg.StructureMinTokens) {
					if summary, changed := c.structExtractor.Extract(text, lang); changed {
						structSaved += len(text) - len(summary)
						text = summary
						textTransformed = true
						msg.Metadata.WasStructured = true
						appliedSubLayers = append(appliedSubLayers, "structure_extract")
						slog.Debug("structure_extract applied",
							slog.String("lang", lang),
							slog.Int("msg_idx", msgIdx),
						)
					}
				}
			}

			// L1.5 / T29: Delta encoding across tool calls. The key is a
			// generalised tool-call identity (filepath when present, else
			// tool_name|topic) so repeated `git status`, `grep <pattern>`
			// or `ls <dir>` invocations also benefit.
			deltaSource := text
			toolKey := ExtractToolCallKeyWithIndex(block, toolUses)
			if toolKey != "" {
				if delta, prevIdx, hasDelta := c.fileTracker.GetDelta(toolKey, deltaSource); hasDelta {
					deltaSaved += len(deltaSource) - len(delta)
					header := formatDeltaHeader(toolKey, prevIdx, msgIdx)
					text = header + delta
					textTransformed = true
					appliedSubLayers = append(appliedSubLayers, "delta")
					slog.Debug("delta applied",
						slog.String("key", toolKey),
						slog.Int("prev_msg_idx", prevIdx),
						slog.Int("msg_idx", msgIdx),
					)
				}
				c.fileTracker.RecordVersion(toolKey, deltaSource, msgIdx)
			}
		}

		// L1.8+L1.9: Classifier + Tool Output Compressor.
		// Skip when text was already replaced by dedup/delta/structure (content is a reference
		// or transformed diff, not the raw tool output the classifier expects).
		if !msg.Metadata.WasDeduped && !textTransformed {
			resolvedBlock := block
			if use, ok := resolveToolUseInfo(block, toolUses); ok {
				resolvedBlock.ToolName = use.name
				resolvedBlock.ToolInput = use.input
			}
			toolType := classifyToolResultWithInput(resolvedBlock.ToolName, resolvedBlock.ToolInput, text)
			if toolType != types.ToolTypeUnknown && toolType != types.ToolTypeFileRead &&
				toolType != types.ToolTypeJSONData {
				if compressed := compressToolOutput(toolType, text, messageAge, c.cfg.SlidingWindow); len(compressed) < len(text) {
					toolSaved += len(text) - len(compressed)
					text = compressed
					appliedSubLayers = append(appliedSubLayers, "tool_compressor")
					slog.Debug("tool_compressor applied",
						slog.Int("tool_type", int(toolType)),
						slog.Int("msg_idx", msgIdx),
						slog.Int("saved_bytes", toolSaved),
					)
				}
			}
		}

		// L1.10: Success short-circuit
		if t2, ok := MaybeSuccessShortCircuit(text); ok {
			successShortSaved += len(text) - len(t2)
			text = t2
			appliedSubLayers = append(appliedSubLayers, "success_short_circuit")
		}

		// L1.11: Inline base64 image data in tool_result text
		if len(text) > 500 {
			syntheticBlock := types.ContentBlock{
				Type:         "tool_result",
				Text:         text,
				ToolName:     block.ToolName,
				ToolInput:    block.ToolInput,
				ToolResultID: block.ToolResultID,
			}
			updated, imgSaved := replaceImageBase64(syntheticBlock, msgIdx, prefixEnd)
			if imgSaved > 0 {
				text = updated.Text
				imageSaved += imgSaved
				appliedSubLayers = append(appliedSubLayers, "image_replace")
			}
		}

		if dictText, saved, ok := applySemanticDictionary(text); ok {
			text = dictText
			dictionarySaved += saved
			appliedSubLayers = append(appliedSubLayers, "semantic_dictionary")
		}

		if len(text) < originalLen || text != block.Text {
			// T76: archive the original block text before stamping the
			// mutated value so the proxy can re-inject the original on
			// reference detection. ANSI strip alone is treated as
			// non-lossy (no archive) because it is a pure normalisation
			// of escape codes; archiving only fires when content beyond
			// ANSI was modified.
			if !ansiOnlyChange(origText, text) {
				// T76b: emit a comma-joined sub_layer tag listing every
				// pass that actually mutated this block. Falls back to
				// "layer1" if nothing tagged itself (preserves coarse
				// attribution from before T76b for unattributed paths
				// like comment_strip / json_compact done above).
				tag := joinSubLayers(appliedSubLayers)
				if layer1MutationRequiresArchive(appliedSubLayers) {
					id := c.archiveOriginal(msgIdx, bi, tag, origText)
					if id == "" {
						jsonSaved = jsonSavedBefore
						dedupSaved = dedupSavedBefore
						commentSaved = commentSavedBefore
						structSaved = structSavedBefore
						deltaSaved = deltaSavedBefore
						ansiSaved = ansiSavedBefore
						successShortSaved = successShortSavedBefore
						toolSaved = toolSavedBefore
						imageSaved = imageSavedBefore
						dictionarySaved = dictionarySavedBefore
						msg.Metadata = metadataBefore
						continue
					}
					newContent[bi].ArchiveID = id
				} else if id := c.archiveOriginal(msgIdx, bi, tag, origText); id != "" {
					newContent[bi].ArchiveID = id
				}
			}
			newContent[bi].Text = text
		}
	}

	out.Content = newContent
	out.Metadata = msg.Metadata
	return out, jsonSaved, dedupSaved, commentSaved, structSaved, deltaSaved, ansiSaved, successShortSaved, toolSaved, imageSaved, dictionarySaved
}

func shouldRunStructureExtraction(text string, minTokens int) bool {
	if text == "" {
		return false
	}
	if minTokens <= 0 {
		return true
	}
	return tokens.CountString(text) >= minTokens
}

// ansiOnlyChange reports whether the only difference between original and
// final is ANSI escape removal. ANSI strip is lossless for the model's
// understanding (the rendered text is identical) so it does not deserve
// an archive entry.
func ansiOnlyChange(original, final string) bool {
	stripped := StripANSICodes(original)
	return stripped == final
}

// joinSubLayers returns a comma-joined sub_layer tag for the archive
// entry (T76b). Empty input falls back to "layer1" to preserve the
// coarse attribution behaviour from before T76b for paths that do
// not list themselves (e.g. early ANSI-only-then-coordinator-skip
// hand-offs).
func joinSubLayers(layers []string) string {
	if len(layers) == 0 {
		return "layer1"
	}
	if len(layers) == 1 {
		return layers[0]
	}
	out := layers[0]
	for _, l := range layers[1:] {
		out += "," + l
	}
	return out
}

// Reset resets all stateful sub-components. Call on cache flush.
func (c *DeterministicCompressor) Reset() {
	c.contentIndex.Reset()
	c.fileTracker.Reset()
	c.toolCallIndex.Reset()
	c.fileOpGraph.Reset()
}

// extractFilepathFromToolResult attempts to extract a file path from a tool_result block.
func extractFilepathFromToolResult(block types.ContentBlock) string {
	if block.ToolInput == "" {
		return ""
	}

	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(block.ToolInput), &raw); err != nil {
		return ""
	}

	for _, key := range []string{"path", "file_path", "filename", "filepath", "file"} {
		if v, ok := raw[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}

	return ""
}

// detectLanguage infers the language for a tool_result block.
func (c *DeterministicCompressor) detectLanguage(block types.ContentBlock, text string) string {
	if fp := extractFilepathFromToolResult(block); fp != "" {
		if lang := LanguageFromPath(fp); lang != "" {
			return lang
		}
	}

	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "package ") && strings.Contains(trimmed, ";") {
		return "java"
	}
	if strings.HasPrefix(trimmed, "package ") || strings.Contains(trimmed, "\nfunc ") {
		return "go"
	}
	if strings.HasPrefix(trimmed, "import ") && strings.Contains(trimmed, "from ") {
		return "python"
	}
	if strings.HasPrefix(trimmed, "use ") && strings.Contains(trimmed, "::") {
		return "rust"
	}
	if strings.HasPrefix(trimmed, "#include") || strings.HasPrefix(trimmed, "#ifndef") {
		return "c"
	}
	if strings.HasPrefix(trimmed, "#!/") {
		return "shell"
	}
	if strings.Contains(trimmed, "require \"") || strings.Contains(trimmed, "require '") {
		return "ruby"
	}
	if strings.HasPrefix(trimmed, "<!DOCTYPE html") || strings.HasPrefix(trimmed, "<html") {
		return "html"
	}

	return ""
}

func (c *DeterministicCompressor) structureLangAllowed(lang string) bool {
	if len(c.cfg.StructureLanguages) == 0 {
		return true
	}
	for _, l := range c.cfg.StructureLanguages {
		if l == lang {
			return true
		}
	}
	return false
}

func formatDupeReference(firstIdx, currentIdx int) string {
	return "[Duplicate of message " + strconv.Itoa(firstIdx) + " - omitted at message " + strconv.Itoa(currentIdx) + "]"
}

func formatNearDupeReference(firstIdx, currentIdx int) string {
	return "[Near-duplicate of message " + strconv.Itoa(firstIdx) + " - omitted at message " + strconv.Itoa(currentIdx) + "]"
}

func formatDeltaHeader(filepath string, prevIdx, currentIdx int) string {
	return "[Delta from message " + strconv.Itoa(prevIdx) + " to " + strconv.Itoa(currentIdx) + " for " + filepath + "]\n"
}

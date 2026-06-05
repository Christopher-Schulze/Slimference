package summarization

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/slimference/slimference/internal/compression"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/security"
	"github.com/slimference/slimference/internal/tokens"
	"github.com/slimference/slimference/internal/types"
)

// summaryMaxAge is the default staleness threshold for cached summaries.
const summaryMaxAge = 30 * time.Minute

// Layer2 orchestrates asynchronous summarization-based compression (Layer 2).
// It uses a FallbackChain of Summarizer providers: primary (MiniMax), then any fallbacks.
type Layer2 struct {
	cfg       *config.CompressionConfig
	chain     *FallbackChain
	cache     *SummaryCache
	sessions  *SessionCache
	anchor    *AnchorDetector
	validator *CompressionValidator
	// redactor (T109) sanitises every outbound message slice before it
	// reaches the FallbackChain. Always non-nil; under
	// outbound_redaction=off it is a deep-copy passthrough.
	redactor *Redactor
	// Cumulative redaction counters (T109). Surfaced via
	// RedactionCounters() and the proxy admin status handler so
	// operators can see what gets stripped from L2 traffic over time.
	redSecrets atomic.Int64
	redPaths   atomic.Int64
	redHeaders atomic.Int64
	redJSON    atomic.Int64
	redInputs  atomic.Int64
}

func NewLayer2(cfg *config.CompressionConfig) *Layer2 {
	// Deterministic in-process extractive summarizer. Zero network
	// round-trip, zero API tokens, sub-millisecond on typical inputs.
	// Capability surface signals strict determinism so
	// RequireDeterministic gates always pass.
	es := NewExtractSummarizer(extractConfigFromSummary(cfg.Summary))

	chain := NewFallbackChain(es)
	chain.SetRequireDeterministic(cfg.Summary.RequireDeterministic)
	sc := NewSessionCache(defaultMaxSessions)
	return &Layer2{
		cfg:       cfg,
		chain:     chain,
		cache:     &SummaryCache{inner: sc},
		sessions:  sc,
		anchor:    NewAnchorDetector(),
		validator: NewCompressionValidator(),
		redactor:  buildRedactor(cfg),
	}
}

// buildRedactor constructs a fresh Redactor from cfg. Extracted so test
// helpers can build one without paying for the rest of NewLayer2.
func buildRedactor(cfg *config.CompressionConfig) *Redactor {
	mode := cfg.Summary.OutboundRedaction
	if mode == "" {
		mode = RedactionModeDefault
	}
	home, _ := os.UserHomeDir()
	// Reuse the inbound detector pattern set so we don't ship divergent
	// secret inventories between proxy ingress and L2 egress.
	det := security.NewDetector("redact", nil, nil)
	return NewRedactor(RedactOptions{
		Mode:           mode,
		HomeDir:        home,
		Detector:       det,
		DropToolInputs: cfg.Summary.OutboundDropToolInputs,
	})
}

// RedactionCounters reports the cumulative outbound-redaction
// telemetry. T109. Snapshotted atomically so the surface is consistent
// across concurrent reads.
type RedactionCounters struct {
	Secrets  int64  `json:"secrets_redacted"`
	Paths    int64  `json:"paths_normalised"`
	Headers  int64  `json:"headers_stripped"`
	JSONKeys int64  `json:"json_keys_redacted"`
	Inputs   int64  `json:"tool_inputs_dropped"`
	Mode     string `json:"mode"`
}

// RedactionCounters returns a snapshot of the per-stage redaction
// counters. Reads are lock-free; values may drift slightly across
// fields under heavy contention but are always individually consistent.
func (l *Layer2) RedactionCounters() RedactionCounters {
	mode := ""
	if l.redactor != nil {
		mode = l.redactor.opts.Mode
	}
	return RedactionCounters{
		Secrets:  l.redSecrets.Load(),
		Paths:    l.redPaths.Load(),
		Headers:  l.redHeaders.Load(),
		JSONKeys: l.redJSON.Load(),
		Inputs:   l.redInputs.Load(),
		Mode:     mode,
	}
}

// recordRedactionStats accumulates RedactStats into the per-Layer2
// atomic counters. Safe for concurrent use.
func (l *Layer2) recordRedactionStats(s RedactStats) {
	l.redSecrets.Add(int64(s.SecretsRedacted))
	l.redPaths.Add(int64(s.PathsNormalised))
	l.redHeaders.Add(int64(s.HeadersStripped))
	l.redJSON.Add(int64(s.JSONKeyRedacted))
	l.redInputs.Add(int64(s.ToolInputsDropped))
}

// applyOutboundRedaction is the single entry point both summarisation
// paths use to sanitise messages before the FallbackChain sees them.
// Returns the redacted slice and accumulates stats into the receiver.
// When the redactor is nil (defensive) or running in off-mode the slice
// is returned as-is.
func (l *Layer2) applyOutboundRedaction(messages []types.Message) []types.Message {
	if l.redactor == nil {
		return messages
	}
	redacted, stats := l.redactor.Redact(messages)
	l.recordRedactionStats(stats)
	return redacted
}

// ApplyMidExchange runs the T99 mid-exchange rewrite using the live
// FallbackChain for the summary body. Falls back to the deterministic
// stub when the chain has no configured provider, returns an error,
// or hands back an empty string. T99b.
//
// `targetTokens` is computed from the threshold so the chain knows how
// much budget the in-progress summary may consume; the local splice
// then applies its own length clamp.
func (l *Layer2) ApplyMidExchange(ctx context.Context, messages []types.Message, threshold int) ([]types.Message, int, bool) {
	pt, ok := DetectMidExchangePoint(messages, threshold)
	if !ok {
		return messages, 0, false
	}
	// T109: outbound redaction on the mid-exchange range. We redact a
	// scoped sub-slice so the splice math (Start/End indices) remains
	// referentially valid against the *original* `messages` arg the
	// downstream `applyMidExchangeWith` re-indexes against. The renderer
	// only reads from the redacted copy.
	redactedRange := l.applyOutboundRedaction(append([]types.Message(nil), messages[pt.Start:pt.End+1]...))
	body := renderRangeForSummarization(redactedRange, 0, len(redactedRange)-1)
	// renderRangeForSummarization always returns non-empty here:
	// DetectMidExchangePoint only fires when the cumulative Text in
	// the range exceeds the threshold, so at least one block has
	// non-empty text. No defensive branch needed.
	target := threshold / 5
	if target < 64 {
		target = 64
	}
	if l.chain == nil {
		return ApplyMidExchange(messages, threshold)
	}
	summary, _, err := l.chain.Summarize(ctx, body, pt.Start, pt.End, target)
	if err != nil || strings.TrimSpace(summary) == "" {
		slog.Debug("mid_exchange live summary fell back to stub",
			slog.Int("start", pt.Start),
			slog.Int("end", pt.End),
		)
		return ApplyMidExchange(messages, threshold)
	}
	return applyMidExchangeWith(messages, threshold, summary)
}

// AddFallbackProvider appends a fallback summarizer to the chain.
// Providers are tried in insertion order after the primary (MiniMax).
func (l *Layer2) AddFallbackProvider(s Summarizer) {
	l.chain.SetProviders(append(l.chain.Providers(), s)...)
}

// ApplyToMessages is the old sessionless compatibility wrapper. It now
// fail-closes for model-facing replacement because T262 requires an explicit
// session namespace before any summary can replace conversation history.
// Session-aware callers must use ApplyToMessagesSession.
func (l *Layer2) ApplyToMessages(messages []types.Message) ([]types.Message, int, bool) {
	return l.ApplyToMessagesSession(legacySessionID, messages)
}

// RunCompressionJob is the async worker body. Call it from a goroutine.
// It determines what to compress, calls MiniMax, validates the result, and stores it.
func (l *Layer2) RunCompressionJob(messages []types.Message) {
	l.RunCompressionJobContext(context.Background(), messages)
}

// RunCompressionJobContext is the async worker body with explicit cancellation.
// Call it when a caller context should bound summarization work.
func (l *Layer2) RunCompressionJobContext(ctx context.Context, messages []types.Message) {
	l.runCompressionJob(ctx, legacySessionID, messages)
}

func (l *Layer2) runCompressionJob(ctx context.Context, sessionID string, messages []types.Message) {
	ctx, cancel := l.withJobTimeout(ctx)
	defer cancel()

	if !l.hasConfiguredProvider() {
		return
	}
	if ctx.Err() != nil {
		return
	}
	minMsgs := l.cfg.MinMessagesForCompression
	prefixEnd := compression.CompressiblePrefixEnd(messages, l.cfg.SlidingWindow)
	if prefixEnd < minMsgs {
		return
	}
	if l.cfg.MinTokensForLayer2 > 0 {
		if !l.passesLayer2TokenGate(messages, prefixEnd, l.cfg.SlidingWindow) {
			return
		}
	}

	// Messages eligible for summarization are everything before the sliding window of recent exchanges.
	boundaryIdx := prefixEnd - 1

	// Detect and preserve anchors.
	allAnchorIndices := l.anchor.Detect(messages[:boundaryIdx+1])
	toSummarize := filterNonAnchored(messages[:boundaryIdx+1], allAnchorIndices)

	if len(toSummarize) == 0 {
		return
	}

	// If a cached summary already covers most of this range, attempt an incremental
	// extension by summarising only the new messages appended since last run.
	existing, existingRange := l.sessions.GetCurrent(sessionID)
	startIdx := 0
	existingSummaryPrefix := ""
	if existing != nil && existingRange[1] > 0 {
		coveredFraction := float64(existingRange[1]) / float64(boundaryIdx)
		if coveredFraction >= l.incrementalOverlapThreshold(len(messages)) {
			// Only compress the delta since the last covered message.
			newStart := existingRange[1] + 1
			if newStart <= boundaryIdx {
				toSummarize = filterNonAnchoredRange(messages[newStart:boundaryIdx+1], allAnchorIndices, newStart)
				startIdx = newStart
				existingSummaryPrefix = existing.Summary + "\n"
			} else {
				// Already fully covered.
				return
			}
		}
	}

	if len(toSummarize) == 0 {
		return
	}

	toSummarizeForInput := capMessageTextsForSummarization(toSummarize, maxLayer2InputTokens)

	// T109: outbound redaction. Sanitise the slice before any of the
	// downstream rendering or chain calls can read it. The receiver's
	// counters accumulate across calls so /admin/status and
	// `slimference doctor` can surface what's been stripped over time.
	toSummarizeForInput = l.applyOutboundRedaction(toSummarizeForInput)

	inputText := existingSummaryPrefix + l.FormatMessagesForSummarization(toSummarizeForInput)
	inputText = capSummarizationInput(inputText, maxLayer2InputTokens)
	inputText = preprocessInput(inputText)
	origTokens := estimateTokens(inputText)

	targetTokens := computeAdaptiveTargetFromText(origTokens, inputText, len(toSummarize), l.cfg.Summary.TargetRatio)

	summary, providerName, err := l.chain.Summarize(ctx, inputText, startIdx, boundaryIdx, targetTokens)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		slog.Error("layer2 summarization failed (all providers)",
			slog.String("error", err.Error()),
			slog.Int("msg_count", len(toSummarize)),
		)
		return
	}
	slog.Debug("layer2 summarization provider", "provider", providerName)

	result := l.validator.Validate(toSummarize, summary, origTokens)
	if !result.Valid {
		if ctx.Err() != nil {
			return
		}
		// T90: try deterministic repair before paying an API round-trip
		// for a retry. Most validator rejects come from preamble or
		// alternative bullet styles which a small local rewrite can fix.
		if repaired, changed := RepairSummary(summary); changed {
			repairResult := l.validator.Validate(toSummarize, repaired, origTokens)
			if repairResult.Valid {
				summary = repaired
				result = repairResult
				slog.Debug("layer2 deterministic repair succeeded")
				goto applySummary
			}
		}
		// Validation-driven retry: retry once with a targeted hint about what failed.
		slog.Warn("layer2 summary failed validation, retrying with emphasis",
			slog.String("reason", result.FailReason),
			slog.Int("orig_tokens", origTokens),
		)

		retryInput := inputText + "\n\nIMPORTANT: Previous attempt was rejected because: " + result.FailReason + "."
		if l.cfg.Summary.Strict {
			retryInput += " Fix this issue. Remember: bullet format, preserve ALL paths, function names, errors, tool details, and decisions verbatim."
		}
		retryTarget := targetTokens

		retrySummary, retryProvider, retryErr := l.chain.Summarize(ctx, retryInput, startIdx, boundaryIdx, retryTarget)
		if retryErr == nil {
			if ctx.Err() != nil {
				return
			}
			retryResult := l.validator.Validate(toSummarize, retrySummary, origTokens)
			if retryResult.Valid {
				summary = retrySummary
				providerName = retryProvider
				result = retryResult
				slog.Debug("layer2 retry succeeded", "provider", providerName)
			} else {
				slog.Warn("layer2 retry also failed validation",
					slog.String("reason", retryResult.FailReason),
				)
				return
			}
		} else {
			slog.Warn("layer2 retry request failed", "error", retryErr.Error())
			return
		}
	}
applySummary:
	if ctx.Err() != nil {
		return
	}
	// T92 telemetry: record per-bullet lineage-marker presence on the
	// validated, accepted summary. Captures shipped output only.
	RecordLineageStats(summary)

	compressedTokens := estimateTokens(summary)
	ratio := 0.0
	if origTokens > 0 {
		ratio = float64(compressedTokens) / float64(origTokens)
	}

	anchorMsgs := make([]types.Message, 0, len(allAnchorIndices))
	for _, idx := range allAnchorIndices {
		anchorMsgs = append(anchorMsgs, deepCopyMessage(messages[idx]))
	}

	cached := &CachedSummary{
		Summary:          summary,
		CoveredRange:     [2]int{0, boundaryIdx},
		AnchorsInlined:   allAnchorIndices,
		OriginalTokens:   origTokens,
		CompressedTokens: compressedTokens,
		CompressionRatio: ratio,
		CreatedAt:        time.Now(),
		Hash:             hashMessages(messages[:boundaryIdx+1]),
		AnchorMessages:   anchorMsgs,
	}
	l.sessions.Store(sessionID, cached)

	slog.Info("layer2 compression complete",
		slog.Int("covered_msgs", boundaryIdx+1),
		slog.Int("orig_tokens", origTokens),
		slog.Int("compressed_tokens", compressedTokens),
		slog.Float64("ratio", ratio),
	)
}

func (l *Layer2) withJobTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, l.jobTimeout())
}

// ShouldTriggerCompression reports whether conditions are right to start a new
// compression job. Returns false if compression is already in progress or the
// existing summary is still fresh and covers enough of the conversation.
func (l *Layer2) ShouldTriggerCompression(messages []types.Message) bool {
	return l.shouldTriggerCompressionWithWindow(messages, 0)
}

func (l *Layer2) ShouldTriggerCompressionWindow(messages []types.Message, window int) bool {
	return l.shouldTriggerCompressionWithWindow(messages, window)
}

func (l *Layer2) shouldTriggerCompressionWithWindow(messages []types.Message, overrideWindow int) bool {
	if !l.hasConfiguredProvider() {
		return false
	}
	window := l.cfg.SlidingWindow
	if overrideWindow > 0 {
		window = overrideWindow
	}
	minMsgs := l.cfg.MinMessagesForCompression
	prefixEnd := compression.CompressiblePrefixEnd(messages, window)
	if prefixEnd < minMsgs {
		return false
	}
	if l.cfg.MinTokensForLayer2 > 0 {
		if !l.passesLayer2TokenGate(messages, prefixEnd, window) {
			return false
		}
	}

	if l.cache.Compressing.Load() {
		return false
	}

	if l.cache.IsStale(summaryMaxAge) {
		return true
	}

	_, existingRange := l.cache.GetCurrent()

	boundaryIdx := prefixEnd - 1
	if boundaryIdx <= 0 {
		return true
	}
	coveredFraction := float64(existingRange[1]) / float64(boundaryIdx)
	return coveredFraction < l.incrementalOverlapThreshold(len(messages))
}

func (l *Layer2) hasConfiguredProvider() bool {
	return l.chain != nil && l.chain.ActiveProviderName() != ""
}

// incrementalOverlapThreshold reads the configured tuning knob. If a
// conversation-size-keyed staircase is configured, it is consulted first
// (first matching step wins). Otherwise the scalar fallback is used. Zero
// values fall back to the historical 0.70 for legacy configs.
func (l *Layer2) incrementalOverlapThreshold(msgCount int) float64 {
	for _, step := range l.cfg.Tuning.IncrementalStaircase {
		if msgCount <= step.MsgCountLE {
			if step.Threshold <= 0 {
				return 0.70
			}
			return step.Threshold
		}
	}
	v := l.cfg.Tuning.IncrementalOverlapThreshold
	if v <= 0 {
		return 0.70
	}
	return v
}

// FormatMessagesForSummarization renders messages as readable plain text for MiniMax.
func (l *Layer2) FormatMessagesForSummarization(messages []types.Message) string {
	var sb strings.Builder
	for _, msg := range messages {
		role := strings.ToUpper(msg.Role)
		sb.WriteString(fmt.Sprintf("[%s msg %d]\n", role, msg.Index))
		for _, blk := range msg.Content {
			switch blk.Type {
			case "text":
				sb.WriteString(formatBlockForSummarization(blk.Text, l.cfg.Summary.Strict))
			case "tool_use":
				sb.WriteString(fmt.Sprintf("<tool_use name=%q input=%s>", blk.ToolName, blk.ToolInput))
			case "tool_result":
				sb.WriteString(fmt.Sprintf("<tool_result id=%q>\n%s\n</tool_result>", blk.ToolResultID, formatBlockForSummarization(blk.Text, l.cfg.Summary.Strict)))
			}
			sb.WriteByte('\n')
		}
		sb.WriteString("---\n")
	}
	return sb.String()
}

// preprocessInput cleans the formatted message text before sending to the LLM.
// It removes noise that wastes tokens and degrades summary quality:
// - Truncates very long tool_result content (e.g. file reads) to a summary line
// - Collapses consecutive identical tool outputs
// - Strips "-tool.sh" noise from hook script names
func preprocessInput(input string) string {
	lines := strings.Split(input, "\n")
	var cleaned []string
	prevContent := ""

	for _, line := range lines {
		// Skip if identical to previous line (duplicate output).
		if line == prevContent {
			continue
		}
		prevContent = line

		// Truncate very long lines (>2000 chars likely a file dump).
		// Keep the first 200 chars as context.
		if len(line) > 2000 {
			truncated := line[:200] + "... [truncated, original " + fmt.Sprintf("%d", len(line)) + " chars]"
			cleaned = append(cleaned, truncated)
			continue
		}

		cleaned = append(cleaned, line)
	}

	return strings.Join(cleaned, "\n")
}

// GetCache returns the underlying SummaryCache for external inspection.
func (l *Layer2) GetCache() *SummaryCache {
	return l.cache
}

// hashMessages computes a SHA-256 over the JSON-serialised message slice.
func hashMessages(messages []types.Message) [32]byte {
	data, _ := json.Marshal(messages)
	return sha256.Sum256(data)
}

// estimateTokens approximates the token count using a weighted heuristic.
// Accounts for whitespace, CJK characters, and code-heavy content more accurately
// than a simple bytes/4 division.
func estimateTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	tokens := 0
	inWord := false
	for _, r := range text {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			inWord = false
		case r >= 0x4E00 && r <= 0x9FFF:
			tokens++
			inWord = false
		case r >= 0x3040 && r <= 0x309F:
			tokens++
			inWord = false
		case r >= 0x30A0 && r <= 0x30FF:
			tokens++
			inWord = false
		default:
			if !inWord {
				tokens++
				inWord = true
			}
		}
	}
	if tokens < 1 {
		return 1
	}
	return tokens
}

// contentDensity measures how information-dense a set of messages is.
// Returns 0.0-1.0 where higher means more dense (code, paths, tool calls).
func contentDensity(messages []types.Message) float64 {
	if len(messages) == 0 {
		return 0.5
	}

	var totalChars, codeChars, pathChars, toolChars int
	for _, msg := range messages {
		for _, blk := range msg.Content {
			n := len(blk.Text)
			totalChars += n
			switch blk.Type {
			case "tool_use", "tool_result":
				toolChars += n
			case "text":
				for _, line := range splitLines(blk.Text) {
					trimmed := strings.TrimSpace(line)
					if looksLikeCode(trimmed) || looksLikePath(trimmed) {
						codeChars += len(trimmed)
					}
				}
			}
		}
	}

	for _, msg := range messages {
		for _, blk := range msg.Content {
			matches := filePathRegex.FindAllString(blk.Text, -1)
			pathChars += len(matches) * 20
		}
	}

	if totalChars == 0 {
		return 0.5
	}

	density := float64(codeChars+toolChars+pathChars) / float64(totalChars)
	if density > 1.0 {
		density = 1.0
	}
	return density
}

func contentDensityText(text string) float64 {
	if text == "" {
		return 0.5
	}

	original := text
	totalChars := len(text)
	codeChars := 0
	pathChars := 0
	toolChars := 0
	for _, marker := range []string{"<tool_use", "<tool_result"} {
		toolChars += strings.Count(original, marker) * len(marker)
	}

	for len(text) > 0 {
		line := text
		if idx := strings.IndexByte(text, '\n'); idx >= 0 {
			line = text[:idx]
			text = text[idx+1:]
		} else {
			text = ""
		}
		trimmed := strings.TrimSpace(line)
		if looksLikeCode(trimmed) || looksLikePath(trimmed) {
			codeChars += len(trimmed)
		}
	}

	matches := filePathRegex.FindAllString(original, -1)
	pathChars += len(matches) * 20
	density := float64(codeChars+toolChars+pathChars) / float64(totalChars)
	if density > 1.0 {
		density = 1.0
	}
	return density
}

// computeAdaptiveTarget calculates target output tokens based on input size,
// content density, and message count. Dense content (code/paths/tools) needs
// more output tokens to preserve information; sparse prose can compress more.
func computeAdaptiveTarget(origTokens int, messages []types.Message, baseRatio float64) int {
	return computeAdaptiveTargetWithDensity(origTokens, len(messages), baseRatio, contentDensity(messages))
}

func computeAdaptiveTargetFromText(origTokens int, inputText string, msgCount int, baseRatio float64) int {
	return computeAdaptiveTargetWithDensity(origTokens, msgCount, baseRatio, contentDensityText(inputText))
}

func computeAdaptiveTargetWithDensity(origTokens int, msgCount int, baseRatio float64, density float64) int {
	ratio := baseRatio

	if msgCount <= 5 {
		ratio = baseRatio * 1.5
	} else if msgCount <= 10 {
		ratio = baseRatio * 1.25
	}

	ratio += density * 0.15

	if origTokens < 1000 {
		ratio = max(ratio, 0.40)
	} else if origTokens < 5000 {
		ratio = max(ratio, 0.25)
	}

	if ratio > 0.60 {
		ratio = 0.60
	}

	target := int(float64(origTokens) * ratio)
	return max(target, 100)
}

const maxLayer2InputTokens = 120000

func capMessageTextsForSummarization(messages []types.Message, maxTokens int) []types.Message {
	if maxTokens <= 0 {
		return messages
	}
	maxBytes := maxTokens * 4
	var capped []types.Message
	for i := range messages {
		for j := range messages[i].Content {
			text := messages[i].Content[j].Text
			if text == "" || (len(text) <= maxBytes && estimateTokens(text) <= maxTokens) {
				continue
			}
			if capped == nil {
				capped = make([]types.Message, len(messages))
				for k := range messages {
					capped[k] = deepCopyMessage(messages[k])
				}
			}
			capped[i].Content[j].Text = capSummarizationInput(text, maxTokens)
		}
	}
	if capped == nil {
		return messages
	}
	return capped
}

func capSummarizationInput(input string, maxTokens int) string {
	if maxTokens <= 0 {
		return input
	}
	maxBytes := maxTokens * 4
	if len(input) <= maxBytes && estimateTokens(input) <= maxTokens {
		return input
	}
	if len(input) > maxBytes {
		input = tailUTF8Bytes(input, maxBytes)
	}
	if idx := strings.IndexByte(input, '\n'); idx >= 0 {
		input = input[idx+1:]
	}
	if estimateTokens(input) <= maxTokens {
		return input
	}
	return tailEstimatedTokens(input, maxTokens)
}

func tailUTF8Bytes(input string, maxBytes int) string {
	if maxBytes <= 0 || len(input) <= maxBytes {
		return input
	}
	start := len(input) - maxBytes
	for start < len(input) && !utf8.RuneStart(input[start]) {
		start++
	}
	return input[start:]
}

func tailEstimatedTokens(input string, maxTokens int) string {
	if maxTokens <= 0 || input == "" {
		return ""
	}
	starts := make([]int, 0, 4096)
	inWord := false
	for i, r := range input {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			inWord = false
		case r >= 0x4E00 && r <= 0x9FFF:
			starts = append(starts, i)
			inWord = false
		case r >= 0x3040 && r <= 0x309F:
			starts = append(starts, i)
			inWord = false
		case r >= 0x30A0 && r <= 0x30FF:
			starts = append(starts, i)
			inWord = false
		default:
			if !inWord {
				starts = append(starts, i)
				inWord = true
			}
		}
	}
	if len(starts) == 0 {
		return ""
	}
	if len(starts) < maxTokens {
		return input
	}
	return input[starts[len(starts)-maxTokens]:]
}

// splitLines splits text into lines without allocating a new slice per line.
func splitLines(s string) []string {
	return strings.Split(s, "\n")
}

// looksLikeCode heuristically detects code lines.
func looksLikeCode(line string) bool {
	if len(line) == 0 {
		return false
	}
	codeIndicators := []string{
		"func ", "func(", "var ", "const ", "type ", "import ",
		"if ", "for ", "switch ", "case ", "return ",
		"pub fn", "let ", "impl ", "use ", "mod ",
		"def ", "class ", "async ",
		"== ", "!= ", ">= ", "<= ",
		":=", "->", "=>", "&&", "||",
	}
	for _, ind := range codeIndicators {
		if strings.Contains(line, ind) {
			return true
		}
	}
	if len(line) > 0 && (line[0] == '{' || line[0] == '}' || line[0] == ')' || line[0] == ']') {
		return true
	}
	if strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "/*") {
		return true
	}
	return false
}

// looksLikePath heuristically detects file paths.
func looksLikePath(line string) bool {
	if len(line) == 0 {
		return false
	}
	if strings.Count(line, "/") >= 2 {
		return true
	}
	if strings.Contains(line, "./") || strings.Contains(line, "../") {
		return true
	}
	for _, suffix := range []string{".go", ".ts", ".rs", ".py", ".js", ".toml", ".json", ".yaml", ".md"} {
		if strings.Contains(line, suffix) {
			return true
		}
	}
	return false
}

func formatBlockForSummarization(text string, strict bool) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return text
	}
	if !strict {
		return text
	}
	if shouldFenceSummarizationBlock(trimmed) {
		return "```text\n" + text + "\n```"
	}
	return text
}

func shouldFenceSummarizationBlock(text string) bool {
	lines := splitLines(text)
	if len(lines) <= 1 {
		return looksLikeCode(text)
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if looksLikeCode(trimmed) || looksLikePath(trimmed) {
			return true
		}
	}
	return false
}

func (l *Layer2) jobTimeout() time.Duration {
	// Deterministic in-process compactor: no network hops. A small
	// fixed budget covers the per-call extract pipeline plus
	// metadata bookkeeping. Keep enough headroom for huge Unicode-heavy
	// contexts where race builds and CJK token accounting are materially
	// slower than byte-oriented ASCII paths.
	return 15 * time.Second
}

const (
	adaptiveLayer2MinFraction       = 0.55
	adaptiveLayer2FloorTokens       = 6000
	adaptiveLayer2MinToolTokens     = 3000
	adaptiveLayer2ToolTokenShareMin = 0.35
)

type BackgroundCandidate struct {
	Eligible               bool
	Reason                 string
	PrefixEnd              int
	PrefixTokens           int
	ToolTokens             int
	ToolTokenShare         float64
	ProjectedSavingsTokens int
	ExistingCovered        float64
}

func (l *Layer2) passesLayer2TokenGate(messages []types.Message, prefixEnd int, window int) bool {
	if l.cfg.MinTokensForLayer2 <= 0 {
		return true
	}
	if prefixEnd <= 0 || prefixEnd > len(messages) {
		return false
	}
	prefixTokens := tokens.CountMessages(messages[:prefixEnd])
	if prefixTokens >= l.cfg.MinTokensForLayer2 {
		return true
	}
	return l.adaptiveLayer2ROICandidate(messages, prefixEnd, window, prefixTokens)
}

func (l *Layer2) adaptiveLayer2ROICandidate(messages []types.Message, prefixEnd int, window int, prefixTokens int) bool {
	if prefixTokens < adaptiveLayer2MinTokens(l.cfg.MinTokensForLayer2) {
		return false
	}
	if l.recentSensitiveAnchor(messages, prefixEnd, window) {
		return false
	}
	toolTokens := layer2ToolResultTokens(messages[:prefixEnd])
	if toolTokens < adaptiveLayer2MinToolTokens || prefixTokens <= 0 {
		return false
	}
	share := float64(toolTokens) / float64(prefixTokens)
	return share >= adaptiveLayer2ToolTokenShareMin
}

func adaptiveLayer2MinTokens(configured int) int {
	if configured <= 0 {
		return 0
	}
	adaptive := int(float64(configured) * adaptiveLayer2MinFraction)
	if adaptive < adaptiveLayer2FloorTokens {
		return adaptiveLayer2FloorTokens
	}
	return adaptive
}

func (l *Layer2) recentSensitiveAnchor(messages []types.Message, _ int, window int) bool {
	if l.anchor == nil {
		return false
	}
	start := len(messages) - max(window, 1)
	if start < 0 {
		start = 0
	}
	for _, msg := range messages[start:] {
		if l.anchor.isAnchorEdit(msg) || l.anchor.isAnchorError(msg) {
			return true
		}
	}
	return false
}

func layer2ToolResultTokens(messages []types.Message) int {
	total := 0
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type == "tool_result" {
				total += tokens.CountString(block.Text)
			}
		}
	}
	return total
}

func (l *Layer2) ScoreBackgroundCandidateSession(sessionID string, messages []types.Message, window int) BackgroundCandidate {
	if !l.hasConfiguredProvider() {
		return BackgroundCandidate{Reason: "provider_unconfigured"}
	}
	if window <= 0 {
		window = l.cfg.SlidingWindow
	}
	prefixEnd := compression.CompressiblePrefixEnd(messages, window)
	c := BackgroundCandidate{PrefixEnd: prefixEnd}
	if prefixEnd < l.cfg.MinMessagesForCompression {
		c.Reason = "below_min_messages"
		return c
	}
	if l.sessions.Compressing(sessionID) {
		c.Reason = "already_compressing"
		return c
	}
	if prefixEnd == 0 {
		c.Eligible = true
		c.Reason = "empty_existing_boundary"
		return c
	}
	if l.recentSensitiveAnchor(messages, prefixEnd, window) {
		c.Reason = "recent_sensitive_anchor"
		return c
	}

	c.PrefixTokens = tokens.CountMessages(messages[:prefixEnd])
	c.ToolTokens = layer2ToolResultTokens(messages[:prefixEnd])
	if c.PrefixTokens > 0 {
		c.ToolTokenShare = float64(c.ToolTokens) / float64(c.PrefixTokens)
	}
	if l.cfg.MinTokensForLayer2 > 0 && !l.passesLayer2TokenGate(messages, prefixEnd, window) {
		c.Reason = "below_token_roi_gate"
		return c
	}

	targetRatio := l.cfg.Summary.TargetRatio
	if targetRatio <= 0 || targetRatio >= 1 {
		targetRatio = 0.20
	}
	c.ProjectedSavingsTokens = c.PrefixTokens - int(float64(c.PrefixTokens)*targetRatio)
	if c.ProjectedSavingsTokens < l.minProjectedLayer2Savings() {
		c.Reason = "projected_savings_too_low"
		return c
	}

	if l.sessions.IsStale(sessionID, summaryMaxAge) {
		c.Eligible = true
		c.Reason = "stale_or_missing_summary"
		return c
	}
	_, existingRange := l.sessions.GetCurrent(sessionID)
	boundaryIdx := prefixEnd - 1
	if boundaryIdx <= 0 {
		c.Eligible = true
		c.Reason = "empty_existing_boundary"
		return c
	}
	c.ExistingCovered = float64(existingRange[1]) / float64(boundaryIdx)
	if c.ExistingCovered < l.incrementalOverlapThreshold(len(messages)) {
		c.Eligible = true
		c.Reason = "coverage_below_threshold"
		return c
	}
	c.Reason = "existing_summary_sufficient"
	return c
}

func (l *Layer2) minProjectedLayer2Savings() int {
	if l.cfg.MinTokensForLayer2 <= 1 {
		return 1
	}
	v := l.cfg.MinTokensForLayer2 / 8
	if v < 256 {
		return 256
	}
	if v > 2048 {
		return 2048
	}
	return v
}

// --- Session-keyed API (T110) ---

func (l *Layer2) ApplyToMessagesSession(sessionID string, messages []types.Message) ([]types.Message, int, bool) {
	if l == nil || l.cfg == nil || !l.cfg.Summary.AllowModelFacingReplacement {
		return messages, 0, false
	}
	if !modelFacingSessionIDTrusted(sessionID) {
		return messages, 0, false
	}
	cached, coveredRange := l.sessions.GetCurrentMatchingPrefix(sessionID, messages)
	if cached == nil {
		return messages, 0, false
	}
	if strings.TrimSpace(cached.Summary) == "" {
		return messages, 0, false
	}
	tokensSaved := cached.OriginalTokens - cached.CompressedTokens
	if tokensSaved <= 0 {
		return messages, 0, false
	}
	end := coveredRange[1]
	if end <= 0 || end >= len(messages) {
		return messages, 0, false
	}

	anchorIndices := cached.AnchorsInlined
	summaryText := buildSummaryText(end, anchorIndices, cached.Summary)

	synthetic := types.Message{
		Index: 0,
		Role:  "assistant",
		Content: []types.ContentBlock{
			{Type: "text", Text: summaryText},
		},
		Metadata: types.MessageMetadata{
			OriginalTokens:   cached.OriginalTokens,
			CompressedTokens: cached.CompressedTokens,
			CompressionLevel: types.CompressionLayer2,
		},
	}

	budget := l.maxAnchorsInlined()
	anchorMsgs := selectAnchors(cached.AnchorMessages, anchorIndices, budget)

	totalAnchors := len(cached.AnchorMessages)
	verbatimCount := 0
	demotedCount := 0
	if totalAnchors > 0 {
		if totalAnchors <= budget {
			verbatimCount = totalAnchors
		} else {
			verbatimCount = budget
			demotedCount = totalAnchors - budget
		}
	}
	l.sessions.anchorsTotal.Add(int64(totalAnchors))
	l.sessions.anchorsVerbatim.Add(int64(verbatimCount))
	l.sessions.anchorsDemoted.Add(int64(demotedCount))

	tail := messages[end+1:]
	result := make([]types.Message, 0, 1+len(anchorMsgs)+len(tail))
	result = append(result, synthetic)
	result = append(result, anchorMsgs...)
	for _, msg := range tail {
		result = append(result, msg)
	}
	for i := range result {
		result[i].Index = i
	}

	if len(anchorIndices) > 0 {
		v := NewCompressionValidator()
		vr := v.ValidateApply(messages, result, anchorIndices, budget)
		if !vr.Valid {
			slog.Warn("layer2.anchor_loss", "reason", vr.FailReason, "session", sessionID)
			return messages, 0, false
		}
	}

	return result, tokensSaved, true
}

func modelFacingSessionIDTrusted(sessionID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	return sessionID != "" && sessionID != "empty" && !strings.HasPrefix(sessionID, "fh:")
}

func buildSummaryText(end int, anchorIndices []int, summary string) string {
	if len(anchorIndices) == 0 {
		return fmt.Sprintf("[Conversation summary covering messages 0-%d: %s]", end, summary)
	}
	idxStrs := make([]string, len(anchorIndices))
	for i, idx := range anchorIndices {
		idxStrs[i] = fmt.Sprintf("%d", idx)
	}
	return fmt.Sprintf("[Conversation summary covering messages 0-%d excluding anchors at %s: %s]",
		end, strings.Join(idxStrs, ", "), summary)
}

func selectAnchors(stored []types.Message, indices []int, budget int) []types.Message {
	if len(stored) == 0 {
		return nil
	}

	type entry struct {
		origOrder int
		msg       types.Message
		cat       anchorCategory
	}

	entries := make([]entry, len(stored))
	for i, m := range stored {
		cat := anchorUnknown
		if i < len(indices) {
			cat = classifyStoredAnchor(m)
		}
		entries[i] = entry{origOrder: i, msg: m, cat: cat}
	}

	priorityOrder := make([]int, len(entries))
	for i := range priorityOrder {
		priorityOrder[i] = i
	}
	sort.Slice(priorityOrder, func(a, b int) bool {
		return entries[priorityOrder[a]].cat < entries[priorityOrder[b]].cat
	})

	verbatim := make(map[int]bool, budget)
	for i := 0; i < budget && i < len(priorityOrder); i++ {
		verbatim[priorityOrder[i]] = true
	}

	sort.Slice(entries, func(a, b int) bool {
		return entries[a].origOrder < entries[b].origOrder
	})

	result := make([]types.Message, 0, len(stored))
	for i, e := range entries {
		if verbatim[i] {
			result = append(result, deepCopyMessage(e.msg))
		} else {
			text := fullText(e.msg)
			if len(text) > 80 {
				text = text[:77] + "..."
			}
			catName := anchorCategoryString(e.cat)
			idx := -1
			if i < len(indices) {
				idx = indices[i]
			}
			result = append(result, types.Message{
				Role: e.msg.Role,
				Content: []types.ContentBlock{
					{Type: "text", Text: fmt.Sprintf("[anchor: %s at msg %d - %s]", catName, idx, text)},
				},
			})
		}
	}
	return result
}

func classifyStoredAnchor(m types.Message) anchorCategory {
	d := NewAnchorDetector()
	if d.isAnchorError(m) {
		return anchorError
	}
	if d.isAnchorEdit(m) {
		return anchorEdit
	}
	if d.isAnchorDecision(m) {
		return anchorDecision
	}
	if d.isAnchorConfig(m) {
		return anchorConfig
	}
	if d.isAnchorArchitect(m) {
		return anchorArchitect
	}
	return anchorUnknown
}

func anchorCategoryString(c anchorCategory) string {
	switch c {
	case anchorError:
		return "error"
	case anchorEdit:
		return "edit"
	case anchorDecision:
		return "decision"
	case anchorConfig:
		return "config"
	case anchorArchitect:
		return "architect"
	default:
		return "generic"
	}
}

func (l *Layer2) ShouldTriggerCompressionSession(sessionID string, messages []types.Message) bool {
	return l.shouldTriggerCompressionSessionWithWindow(sessionID, messages, 0)
}

func (l *Layer2) ShouldTriggerCompressionSessionWindow(sessionID string, messages []types.Message, window int) bool {
	return l.shouldTriggerCompressionSessionWithWindow(sessionID, messages, window)
}

func (l *Layer2) shouldTriggerCompressionSessionWithWindow(sessionID string, messages []types.Message, overrideWindow int) bool {
	window := l.cfg.SlidingWindow
	if overrideWindow > 0 {
		window = overrideWindow
	}
	return l.ScoreBackgroundCandidateSession(sessionID, messages, window).Eligible
}

func (l *Layer2) RunCompressionJobSession(ctx context.Context, sessionID string, messages []types.Message) {
	l.runCompressionJob(ctx, sessionID, messages)
}

func (l *Layer2) SetCompressingSession(sessionID string, v bool) {
	l.sessions.SetCompressing(sessionID, v)
}

func (l *Layer2) CompressionCandidateHash(messages []types.Message, window int) ([32]byte, bool) {
	if window <= 0 {
		window = l.cfg.SlidingWindow
	}
	prefixEnd := compression.CompressiblePrefixEnd(messages, window)
	if prefixEnd <= 0 || prefixEnd > len(messages) {
		return [32]byte{}, false
	}
	return hashMessages(messages[:prefixEnd]), true
}

func (l *Layer2) MarkCompressionCandidate(sessionID string, inputHash [32]byte) {
	l.sessions.SetCandidateHash(sessionID, inputHash)
}

func (l *Layer2) IsCurrentCompressionCandidate(sessionID string, inputHash [32]byte) bool {
	return l.sessions.CandidateHashMatches(sessionID, inputHash)
}

func (l *Layer2) RecordStaleCompressionJobSkip() {
	l.sessions.RecordStaleJobSkip()
}

func (l *Layer2) InvalidateSession(sessionID string) {
	l.sessions.Invalidate(sessionID)
}

func (l *Layer2) InvalidateAllSessions() {
	l.sessions.InvalidateAll()
}

func (l *Layer2) CacheStats() CacheStats {
	return l.sessions.Stats()
}

func (l *Layer2) GetSessionCache() *SessionCache {
	return l.sessions
}

func deepCopyMessage(m types.Message) types.Message {
	cp := m
	cp.Content = make([]types.ContentBlock, len(m.Content))
	copy(cp.Content, m.Content)
	return cp
}

const defaultMaxAnchorsInlined = 8

func (l *Layer2) maxAnchorsInlined() int {
	if v := l.cfg.Summary.MaxAnchorsInlined; v > 0 {
		return v
	}
	return defaultMaxAnchorsInlined
}

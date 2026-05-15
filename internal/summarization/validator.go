package summarization

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/slimference/slimference/internal/types"
)

// filePathRegex matches relative and absolute file paths without chopping
// leading path segments from values such as src/lib/util.go.
var filePathRegex = regexp.MustCompile(`[a-zA-Z0-9_./\\-]+\.[a-zA-Z]{1,8}`)

// funcNameRegex matches function declarations across Go, Python, and Rust.
var funcNameRegex = regexp.MustCompile(`func\s+\w+|def\s+\w+|fn\s+\w+`)

// errorStringRegex matches common error patterns to extract as key phrases.
var errorStringRegex = regexp.MustCompile(`(?i)(error|panic|fatal|exception|fail)[^\n]{0,120}`)

// ValidationResult records whether a summary passed quality checks.
type ValidationResult struct {
	Valid      bool
	FailReason string
}

// CompressionValidator checks that MiniMax summaries preserve critical information.
type CompressionValidator struct{}

// NewCompressionValidator constructs a CompressionValidator.
func NewCompressionValidator() *CompressionValidator {
	return &CompressionValidator{}
}

// Validate runs quality checks on the summary against the original messages.
// Checks: format compliance, file path preservation, function name preservation,
// error string preservation, minimum/maximum length, no CoT artifacts.
func (v *CompressionValidator) Validate(original []types.Message, summary string, origTokens int) ValidationResult {
	// 0. Format compliance: summary must contain bullet points starting with "- ".
	lines := strings.Split(summary, "\n")
	bulletCount := 0
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "- ") {
			bulletCount++
		}
	}
	if bulletCount == 0 {
		return ValidationResult{
			Valid:      false,
			FailReason: "format violation: no bullet points found, output must be \"- \" prefixed lines",
		}
	}

	// 0.5. No CoT artifacts: reject if thinking blocks leaked through.
	if strings.Contains(summary, "<think") || strings.Contains(summary, "</think") {
		return ValidationResult{
			Valid:      false,
			FailReason: "format violation: chain-of-thought artifacts detected in output",
		}
	}
	sourceText := joinMessages(original)
	// 1. FilePathPreservation: >90% of paths from original must appear in summary.
	paths := extractFilePaths(sourceText)
	if len(paths) > 0 {
		found := 0
		for _, p := range paths {
			if strings.Contains(summary, p) {
				found++
			}
		}
		ratio := float64(found) / float64(len(paths))
		if ratio < 0.90 {
			return ValidationResult{
				Valid:      false,
				FailReason: "file path preservation below 90%: " + percentStr(ratio),
			}
		}
	}
	if invented := inventedSummaryPaths(sourceText, paths, extractFilePaths(summary)); len(invented) > 0 {
		return ValidationResult{
			Valid:      false,
			FailReason: "summary invented file path absent from source: " + invented[0],
		}
	}

	// 2. FunctionNamePreservation: >80% of function names must appear in summary.
	funcNames := extractFunctionNames(sourceText)
	if len(funcNames) > 0 {
		found := 0
		for _, fn := range funcNames {
			// Extract just the name part after "func "/"def "/"fn ".
			name := extractIdentifier(fn)
			if name != "" && strings.Contains(summary, name) {
				found++
			}
		}
		ratio := float64(found) / float64(len(funcNames))
		if ratio < 0.80 {
			return ValidationResult{
				Valid:      false,
				FailReason: "function name preservation below 80%: " + percentStr(ratio),
			}
		}
	}

	// 3. ErrorPreservation: key error strings from original must appear in summary.
	errorStrings := extractErrorStrings(original)
	if len(errorStrings) > 0 {
		// Require at least half of detected error strings to appear.
		found := 0
		for _, es := range errorStrings {
			// Use the first 40 chars as the key fragment to avoid whitespace issues.
			fragment := trimToLen(es, 40)
			if strings.Contains(summary, fragment) {
				found++
			}
		}
		ratio := float64(found) / float64(len(errorStrings))
		if ratio < 0.50 {
			return ValidationResult{
				Valid:      false,
				FailReason: "error string preservation below 50%: " + percentStr(ratio),
			}
		}
	}

	// 4. MinimumLength: summary must be >5% of origTokens.
	summaryTokenEst := estimateTokens(summary)
	if origTokens > 0 && summaryTokenEst < origTokens/20 {
		return ValidationResult{
			Valid:      false,
			FailReason: "summary too short: below 5% of original token count",
		}
	}

	// 5. MaximumLength: summary must be <40% of origTokens.
	if origTokens > 0 && summaryTokenEst > (origTokens*40)/100 {
		return ValidationResult{
			Valid:      false,
			FailReason: "summary too long: exceeds 40% of original token count",
		}
	}

	return ValidationResult{Valid: true}
}

func inventedSummaryPaths(sourceText string, sourcePaths, summaryPaths []string) []string {
	if len(summaryPaths) == 0 {
		return nil
	}
	source := make(map[string]struct{}, len(sourcePaths))
	for _, p := range sourcePaths {
		source[p] = struct{}{}
	}
	var invented []string
	for _, p := range summaryPaths {
		if _, ok := source[p]; ok {
			continue
		}
		if !pathSeenInSource(sourceText, p, sourcePaths) {
			invented = append(invented, p)
		}
	}
	return invented
}

func pathSeenInSource(sourceText string, path string, sourcePaths []string) bool {
	needle := normalizeSummaryPath(path)
	if needle == "" {
		return true
	}
	if strings.Contains(sourceText, needle) || strings.Contains(sourceText, "/"+needle) || strings.Contains(sourceText, "./"+needle) {
		return true
	}
	for _, sourcePath := range sourcePaths {
		source := normalizeSummaryPath(sourcePath)
		if source == "" {
			continue
		}
		if source == needle || strings.HasSuffix(source, "/"+needle) || strings.HasSuffix(needle, "/"+source) {
			return true
		}
	}
	return false
}

func normalizeSummaryPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, "`'\",;:()[]{}")
	for strings.HasPrefix(path, "./") {
		path = strings.TrimPrefix(path, "./")
	}
	path = strings.TrimLeft(path, "/")
	path = strings.TrimRight(path, ".")
	return path
}

// extractFilePaths returns all unique file path strings found in text.
func extractFilePaths(text string) []string {
	raw := filePathRegex.FindAllString(text, -1)
	return dedupStrings(raw)
}

// extractFunctionNames returns all unique function declaration strings found in text.
// Text is extracted from code blocks only (between ``` fences).
func extractFunctionNames(text string) []string {
	codeContent := extractCodeBlocks(text)
	raw := append(funcNameRegex.FindAllString(codeContent, -1), funcNameRegex.FindAllString(text, -1)...)
	return dedupStrings(raw)
}

// extractErrorStrings returns key error phrases found across all messages.
func extractErrorStrings(messages []types.Message) []string {
	var combined strings.Builder
	for _, msg := range messages {
		for _, blk := range msg.Content {
			if blk.Text != "" {
				combined.WriteString(blk.Text)
				combined.WriteByte('\n')
			}
		}
	}
	raw := errorStringRegex.FindAllString(combined.String(), -1)
	return dedupStrings(raw)
}

// joinMessages concatenates all text content from all messages.
func joinMessages(messages []types.Message) string {
	var sb strings.Builder
	for _, msg := range messages {
		for _, blk := range msg.Content {
			switch blk.Type {
			case "text", "tool_result":
				if blk.Text != "" {
					sb.WriteString(blk.Text)
					sb.WriteByte('\n')
				}
			case "tool_use":
				if blk.ToolName != "" {
					sb.WriteString(blk.ToolName)
					sb.WriteByte('\n')
				}
				if blk.ToolInput != "" {
					sb.WriteString(blk.ToolInput)
					sb.WriteByte('\n')
				}
			}
		}
	}
	return sb.String()
}

// extractCodeBlocks returns content inside ``` fences.
func extractCodeBlocks(text string) string {
	var sb strings.Builder
	inBlock := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inBlock = !inBlock
			continue
		}
		if inBlock {
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// extractIdentifier pulls the function identifier from a "func Name" match.
func extractIdentifier(match string) string {
	parts := strings.Fields(match)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// dedupStrings returns a slice with duplicate strings removed, preserving order.
func dedupStrings(ss []string) []string {
	seen := make(map[string]bool, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// trimToLen returns s trimmed to at most n runes.
func trimToLen(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// ValidateApply confirms that anchor messages survive into the post-apply slice.
// Returns false if any anchor that should be present (within budget) is missing
// verbatim, and reports which anchors were lost.
func (v *CompressionValidator) ValidateApply(originalMsgs []types.Message, postApplyMsgs []types.Message, anchorIndices []int, budget int) ValidationResult {
	if len(anchorIndices) == 0 {
		return ValidationResult{Valid: true}
	}

	limit := budget
	if limit > len(anchorIndices) {
		limit = len(anchorIndices)
	}

	for i := 0; i < limit; i++ {
		origIdx := anchorIndices[i]
		if origIdx >= len(originalMsgs) {
			continue
		}
		orig := originalMsgs[origIdx]
		origText := fullText(orig)
		if origText == "" {
			continue
		}
		found := false
		for _, post := range postApplyMsgs {
			if containsVerbatimAnchor(post, orig) {
				found = true
				break
			}
		}
		if !found {
			return ValidationResult{
				Valid:      false,
				FailReason: "anchor lost: message at index " + strconv.Itoa(origIdx) + " not found verbatim in post-apply output",
			}
		}
	}
	return ValidationResult{Valid: true}
}

func containsVerbatimAnchor(post types.Message, orig types.Message) bool {
	postText := fullText(post)
	origText := fullText(orig)
	if origText == "" {
		return true
	}
	return strings.Contains(postText, origText)
}
func percentStr(ratio float64) string {
	pct := int(ratio * 100)
	s := ""
	if pct < 10 {
		s = "0"
	}
	s += strconv.Itoa(pct) + "%"
	return s
}

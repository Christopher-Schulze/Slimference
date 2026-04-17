package summarization

import (
	"regexp"
	"strings"

	"github.com/slimference/slimference/internal/types"
)

// filePathRegex matches file paths that contain a dot or slash followed by a name.
var filePathRegex = regexp.MustCompile(`[./][a-zA-Z0-9_\-/]+\.[a-zA-Z]{1,6}`)

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
	// 1. FilePathPreservation: >90% of paths from original must appear in summary.
	paths := extractFilePaths(joinMessages(original))
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

	// 2. FunctionNamePreservation: >80% of function names must appear in summary.
	funcNames := extractFunctionNames(joinMessages(original))
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

// percentStr formats a ratio as a human-readable percentage string.
func percentStr(ratio float64) string {
	pct := int(ratio * 100)
	s := ""
	if pct < 10 {
		s = "0"
	}
	s += itoa(pct) + "%"
	return s
}

// itoa is a minimal int-to-string converter avoiding fmt import.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

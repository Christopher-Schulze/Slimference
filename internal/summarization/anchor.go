// Package summarization implements Layer 2 MiniMax-based abstractive compression.
package summarization

import (
	"regexp"
	"strings"

	"github.com/tokenproxy/tokenproxy/internal/types"
)

// editToolNames matches tool names that perform file mutations.
var editToolNames = regexp.MustCompile(`(?i)edit|write|create|delete`)

// errorPatterns matches common error indicators in message content.
var errorPatterns = regexp.MustCompile(`error|Error|ERROR|panic|FAIL|traceback|exception|fatal`)

// stackTracePatterns matches stack trace signatures across Go, Python, and Java.
var stackTracePatterns = regexp.MustCompile(`goroutine \d+|(\bat \b.+\n){2,}|File ".*", line \d+`)

// configFilePatterns matches file paths that indicate configuration files.
var configFilePatterns = regexp.MustCompile(`\.(json|toml|yaml|yml|env|conf)$|^Makefile$|^Dockerfile$`)

// architectureKeywords that signal an architecture-level decision message.
var architectureKeywords = regexp.MustCompile(`architecture|design|approach|strategy|plan|trade-off`)

// bulletLine matches a markdown bullet or numbered list line.
var bulletLine = regexp.MustCompile(`(?m)^\s*[-*+]\s+.+|^\s*\d+\.\s+.+`)

// decisionWords matches short affirmative or negative user decision words.
var decisionWords = regexp.MustCompile(`(?i)\byes\b|\bja\b|\bdo it\b|\bgo ahead\b|\bapproved\b|\bno\b|\bnein\b|\bdon't\b|\bstop\b|\bnicht\b|\bcancel\b`)

// AnchorDetector identifies messages that must never be summarized.
type AnchorDetector struct {
	editTool     *regexp.Regexp
	errorPat     *regexp.Regexp
	stackTrace   *regexp.Regexp
	configFile   *regexp.Regexp
	archKeywords *regexp.Regexp
	bullet       *regexp.Regexp
	decision     *regexp.Regexp
}

// NewAnchorDetector constructs an AnchorDetector with pre-compiled patterns.
func NewAnchorDetector() *AnchorDetector {
	return &AnchorDetector{
		editTool:     editToolNames,
		errorPat:     errorPatterns,
		stackTrace:   stackTracePatterns,
		configFile:   configFilePatterns,
		archKeywords: architectureKeywords,
		bullet:       bulletLine,
		decision:     decisionWords,
	}
}

// Detect returns the indices of messages in the slice that must not be summarized.
func (d *AnchorDetector) Detect(messages []types.Message) []int {
	// Build a set of indices where a tool_use with edit/write/create/delete name appears,
	// so the immediately following tool_result can also be marked.
	editToolUseAt := make(map[int]bool, len(messages))
	for i, msg := range messages {
		for _, blk := range msg.Content {
			if blk.Type == "tool_use" && d.editTool.MatchString(blk.ToolName) {
				editToolUseAt[i] = true
			}
		}
	}

	var anchors []int
	for i, msg := range messages {
		if d.IsAnchor(msg, messages) {
			anchors = append(anchors, i)
			continue
		}
		// AnchorEdit: tool_result that follows an edit tool_use at index i-1.
		if i > 0 && editToolUseAt[i-1] && msg.HasToolResult() {
			anchors = append(anchors, i)
		}
	}
	return anchors
}

// IsAnchor reports whether a single message qualifies as an anchor.
func (d *AnchorDetector) IsAnchor(msg types.Message, allMessages []types.Message) bool {
	return d.isAnchorEdit(msg) ||
		d.isAnchorError(msg) ||
		d.isAnchorDecision(msg) ||
		d.isAnchorArchitect(msg) ||
		d.isAnchorConfig(msg)
}

// isAnchorEdit returns true when the message contains a file-mutation tool_use.
func (d *AnchorDetector) isAnchorEdit(msg types.Message) bool {
	for _, blk := range msg.Content {
		if blk.Type == "tool_use" && d.editTool.MatchString(blk.ToolName) {
			return true
		}
	}
	return false
}

// isAnchorError returns true when the message content shows failure signals.
func (d *AnchorDetector) isAnchorError(msg types.Message) bool {
	text := fullText(msg)
	if d.errorPat.MatchString(text) {
		return true
	}
	if d.stackTrace.MatchString(text) {
		return true
	}
	return false
}

// isAnchorDecision returns true for short user messages that confirm or reject.
func (d *AnchorDetector) isAnchorDecision(msg types.Message) bool {
	if msg.Role != "user" {
		return false
	}
	text := fullText(msg)
	words := strings.Fields(text)
	if len(words) >= 50 {
		return false
	}
	return d.decision.MatchString(text)
}

// isAnchorArchitect returns true for assistant messages with architecture decisions
// that include a meaningful bullet list (more than 3 items).
func (d *AnchorDetector) isAnchorArchitect(msg types.Message) bool {
	if msg.Role != "assistant" {
		return false
	}
	text := fullText(msg)
	if !d.archKeywords.MatchString(text) {
		return false
	}
	matches := d.bullet.FindAllString(text, -1)
	return len(matches) > 3
}

// isAnchorConfig returns true when a tool_result references a config file path.
func (d *AnchorDetector) isAnchorConfig(msg types.Message) bool {
	for _, blk := range msg.Content {
		if blk.Type != "tool_result" {
			continue
		}
		// Check both the tool name field and the content text for config file paths.
		combined := blk.Text + " " + blk.ToolName
		lines := strings.Fields(combined)
		for _, word := range lines {
			base := word
			if idx := strings.LastIndex(word, "/"); idx >= 0 {
				base = word[idx+1:]
			}
			if d.configFile.MatchString(base) || d.configFile.MatchString(word) {
				return true
			}
		}
	}
	return false
}

// filterNonAnchored returns only the messages whose index is NOT in anchorIndices.
func filterNonAnchored(messages []types.Message, anchorIndices []int) []types.Message {
	anchored := make(map[int]bool, len(anchorIndices))
	for _, idx := range anchorIndices {
		anchored[idx] = true
	}
	result := make([]types.Message, 0, len(messages))
	for i, msg := range messages {
		if !anchored[i] {
			result = append(result, msg)
		}
	}
	return result
}

// fullText concatenates all text content blocks in a message.
func fullText(msg types.Message) string {
	var sb strings.Builder
	for _, blk := range msg.Content {
		if blk.Text != "" {
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(blk.Text)
		}
	}
	return sb.String()
}

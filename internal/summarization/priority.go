package summarization

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/slimference/slimference/internal/types"
)

// ClassifyPriority returns the summarization priority for a tool result.
// HIGH: preserve verbatim (errors, edits, decisions)
// MEDIUM: preserve key facts, may paraphrase (file reads, search results)
// LOW: may reduce to one-liner (successful builds, clean tests, dir listings)
func ClassifyPriority(toolType types.ToolResultType, content string, isAnchor bool) types.ToolResultPriority {
	// Anchor messages always get HIGH priority
	if isAnchor {
		return types.PriorityHigh
	}

	switch toolType {
	case types.ToolTypeFileRead:
		return types.PriorityMedium // model may need to recall file content
	case types.ToolTypeSearchResult:
		return types.PriorityMedium // model may need to recall findings
	case types.ToolTypeGitOutput:
		if containsGitDiff(content) {
			return types.PriorityMedium // diff contains change info
		}
		return types.PriorityLow // git status / log summaries
	case types.ToolTypeTestOutput:
		if hasTestFailures(content) {
			return types.PriorityHigh // test failures are critical
		}
		return types.PriorityLow // all-passing test runs
	case types.ToolTypeBuildOutput:
		if hasBuildErrors(content) {
			return types.PriorityHigh // build errors are critical
		}
		return types.PriorityLow // clean builds
	case types.ToolTypeLintOutput:
		if hasLintErrors(content) {
			return types.PriorityMedium // lint issues need attention
		}
		return types.PriorityLow // no violations
	case types.ToolTypeDirListing:
		return types.PriorityLow // model can re-run ls
	case types.ToolTypeLogOutput:
		if hasLogErrors(content) {
			return types.PriorityHigh // error logs are critical
		}
		return types.PriorityLow
	case types.ToolTypeJSONData:
		return types.PriorityMedium // API responses may be referenced later
	case types.ToolTypeCommandOutput:
		return types.PriorityLow // generic output
	default:
		return types.PriorityMedium
	}
}

// PriorityLabel returns the human-readable label for a priority level.
func PriorityLabel(p types.ToolResultPriority) string {
	switch p {
	case types.PriorityHigh:
		return "HIGH"
	case types.PriorityMedium:
		return "MEDIUM"
	case types.PriorityLow:
		return "LOW"
	default:
		return "MEDIUM"
	}
}

// SummarizationHint builds the priority hint injected into the MiniMax summarization prompt.
// It lists HIGH/MEDIUM/LOW priority messages so MiniMax can compress accordingly.
func SummarizationHint(messages []types.Message) string {
	if len(messages) == 0 {
		return ""
	}

	d := NewAnchorDetector()
	var highIdxs, lowIdxs []int

	for _, msg := range messages {
		isAnchor := d.IsAnchor(msg, messages)
		for _, block := range msg.Content {
			if block.Type != "tool_result" {
				continue
			}
			from_classifier := classifyFromBlock(block)
			p := ClassifyPriority(from_classifier, block.Text, isAnchor)
			switch p {
			case types.PriorityHigh:
				highIdxs = append(highIdxs, msg.Index)
			case types.PriorityLow:
				lowIdxs = append(lowIdxs, msg.Index)
			}
		}
	}

	var sb strings.Builder
	sb.WriteString("Priority guidance:\n")
	if len(highIdxs) > 0 {
		sb.WriteString("HIGH priority (preserve verbatim): messages ")
		for i, idx := range highIdxs {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(strconv.Itoa(idx))
		}
		sb.WriteString("\n")
	}
	if len(lowIdxs) > 0 {
		sb.WriteString("LOW priority (may reduce to one-liner): messages ")
		for i, idx := range lowIdxs {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(strconv.Itoa(idx))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("MEDIUM priority items: preserve key facts, may paraphrase.\n")

	// T26: add repetition guidance for repeated tool calls. Silent when no
	// tool call appears more than once.
	if hint := RepetitionHint(messages); hint != "" {
		sb.WriteString("\n")
		sb.WriteString(hint)
	}

	return sb.String()
}

// classifyFromBlock classifies a tool_result block using the tool name and content.
func classifyFromBlock(block types.ContentBlock) types.ToolResultType {
	name := strings.ToLower(block.ToolName)
	switch name {
	case "read", "view", "readfile", "cat":
		return types.ToolTypeFileRead
	case "grep", "glob", "search", "find":
		return types.ToolTypeSearchResult
	case "ls", "list":
		return types.ToolTypeDirListing
	}
	// Fallback to content-based classification
	return classifyByContent(block.Text)
}

// classifyByContent classifies tool_result content without a known tool_name.
func classifyByContent(content string) types.ToolResultType {
	if content == "" {
		return types.ToolTypeCommandOutput
	}
	trimmed := strings.TrimSpace(content)

	if rePriorityGitBranch.MatchString(trimmed) || rePriorityGitDiff.MatchString(content) {
		return types.ToolTypeGitOutput
	}
	if rePriorityTestOutput.MatchString(content) {
		return types.ToolTypeTestOutput
	}
	if rePriorityBuildError.MatchString(content) {
		return types.ToolTypeBuildOutput
	}
	if rePriorityDirListing.MatchString(trimmed) {
		return types.ToolTypeDirListing
	}
	return types.ToolTypeCommandOutput
}

// Content-based detection helpers

func containsGitDiff(content string) bool {
	return rePriorityGitDiff.MatchString(content)
}

func hasTestFailures(content string) bool {
	return rePriorityTestFail.MatchString(content)
}

func hasBuildErrors(content string) bool {
	return rePriorityBuildError.MatchString(content)
}

func hasLintErrors(content string) bool {
	return rePriorityLintError.MatchString(content)
}

func hasLogErrors(content string) bool {
	return rePriorityLogError.MatchString(content)
}

var (
	rePriorityGitBranch  = regexp.MustCompile(`(?m)^(On branch|HEAD detached|nothing to commit)`)
	rePriorityGitDiff    = regexp.MustCompile(`(?m)^(diff --git|@@\s+-\d+)`)
	rePriorityTestOutput = regexp.MustCompile(`(?m)(^(?:PASS|FAIL|ok\s)|test result:|\d+ (?:passed|failed))`)
	rePriorityTestFail   = regexp.MustCompile(`(?m)(^FAIL\s|^---\s+FAIL:|FAILED\b|[1-9]\d* failed)`)
	rePriorityBuildError = regexp.MustCompile(`(?i)(error:|: error |^error\[)`)
	rePriorityLintError  = regexp.MustCompile(`(?i):\d+:\d+:?\s+(?:error|warning)`)
	rePriorityDirListing = regexp.MustCompile(`(?m)^(total \d+$|[-dlcbsp][rwx-]{9})`)
	rePriorityLogError   = regexp.MustCompile(`(?i)(error|fatal|panic|exception)`)
)

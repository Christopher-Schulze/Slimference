package compression

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tokenproxy/tokenproxy/internal/types"
)

// compressToolOutput applies type-specific compression to old tool_result content.
// messageAge is the distance from the message to the compressible boundary:
// age 1 = newest compressible message, higher = older.
// aggressive mode activates when messageAge > 2 * slidingWindow.
func compressToolOutput(toolType types.ToolResultType, content string, messageAge, slidingWindow int) string {
	if content == "" {
		return content
	}
	aggressive := messageAge > 2*slidingWindow
	switch toolType {
	case types.ToolTypeGitOutput:
		return filterGitCompact(content, aggressive)
	case types.ToolTypeTestOutput:
		return filterTestCompact(content, aggressive)
	case types.ToolTypeBuildOutput:
		return filterBuildCompact(content, aggressive)
	case types.ToolTypeLintOutput:
		return filterLintCompact(content, aggressive)
	case types.ToolTypeLogOutput:
		return filterLogCompact(content, aggressive)
	case types.ToolTypeDirListing:
		return filterDirCompact(content, aggressive)
	case types.ToolTypeSearchResult:
		return filterSearchCompact(content, aggressive)
	default:
		return content
	}
}

// filterGitCompact reduces git output: keeps branch/commit info, stats, and (moderate) diff.
func filterGitCompact(content string, aggressive bool) string {
	lines := strings.Split(content, "\n")
	var inDiff bool
	var header, stats, fileSummary, diffLines []string
	var diffCount int

	const moderateDiffLimit = 60

	for _, line := range lines {
		stripped := strings.TrimRight(line, "\r")
		// Always keep: branch/commit header lines
		if reGitCommitHeader.MatchString(stripped) ||
			reGitBranchLine.MatchString(stripped) {
			header = append(header, stripped)
			inDiff = false
			continue
		}
		// Always keep: summary stats
		if reGitStats.MatchString(stripped) {
			stats = append(stats, stripped)
			continue
		}
		// Always keep: file mode / rename lines
		if reGitFileSummary.MatchString(stripped) {
			fileSummary = append(fileSummary, stripped)
			inDiff = false
			continue
		}
		// Diff file header
		if strings.HasPrefix(stripped, "diff --git") ||
			strings.HasPrefix(stripped, "--- ") ||
			strings.HasPrefix(stripped, "+++ ") {
			inDiff = true
			if !aggressive && diffCount < moderateDiffLimit {
				diffLines = append(diffLines, stripped)
				diffCount++
			}
			continue
		}
		if inDiff {
			if !aggressive && diffCount < moderateDiffLimit {
				diffLines = append(diffLines, stripped)
				diffCount++
			}
		}
	}

	var out []string
	out = append(out, header...)
	out = append(out, fileSummary...)
	if !aggressive {
		out = append(out, diffLines...)
		if diffCount >= moderateDiffLimit {
			out = append(out, fmt.Sprintf("[diff truncated, %d lines shown]", moderateDiffLimit))
		}
	}
	out = append(out, stats...)

	result := strings.Join(out, "\n")
	if result == "" || len(result) >= len(content) {
		return content
	}
	return result
}

// filterTestCompact reduces test output: keeps failures and summary, drops verbose pass output.
func filterTestCompact(content string, aggressive bool) string {
	lines := strings.Split(content, "\n")
	var failures, summary []string
	var inFailure bool
	failureLineCount := 0
	const maxFailureLines = 40

	for _, line := range lines {
		stripped := strings.TrimRight(line, "\r")
		// Summary lines: always keep
		if reTestSummary.MatchString(stripped) {
			summary = append(summary, stripped)
			inFailure = false
			continue
		}
		// Failure markers: always keep + following context
		if reTestFail.MatchString(stripped) {
			inFailure = true
			failures = append(failures, stripped)
			failureLineCount++
			continue
		}
		// Pass lines: only keep in non-aggressive mode brief form
		if reTestPass.MatchString(stripped) {
			inFailure = false
			if !aggressive {
				failures = append(failures, stripped)
			}
			continue
		}
		// Failure context: keep up to limit
		if inFailure && failureLineCount < maxFailureLines {
			failures = append(failures, stripped)
			failureLineCount++
		}
	}

	var out []string
	out = append(out, failures...)
	out = append(out, summary...)
	result := strings.Join(out, "\n")
	if result == "" || len(result) >= len(content) {
		return content
	}
	return result
}

// filterBuildCompact reduces build output: keeps errors and warnings, drops verbose info.
func filterBuildCompact(content string, aggressive bool) string {
	lines := strings.Split(content, "\n")
	var errors, infos []string
	maxErrors := 50
	if aggressive {
		maxErrors = 20
	}

	for _, line := range lines {
		stripped := strings.TrimRight(line, "\r")
		if reBuildErrorLine.MatchString(stripped) || reBuildWarningLine.MatchString(stripped) {
			if len(errors) < maxErrors {
				errors = append(errors, stripped)
			}
			continue
		}
		if !aggressive && reBuildInfoLine.MatchString(stripped) {
			infos = append(infos, stripped)
		}
	}

	if len(errors) == maxErrors {
		errors = append(errors, "[...additional errors omitted]")
	}

	var out []string
	out = append(out, errors...)
	if !aggressive {
		out = append(out, infos...)
	}
	result := strings.Join(out, "\n")
	if result == "" || len(result) >= len(content) {
		return content
	}
	return result
}

// filterLintCompact reduces lint output: keeps violations, drops verbose rule explanations.
func filterLintCompact(content string, aggressive bool) string {
	lines := strings.Split(content, "\n")
	var violations, summaries []string
	maxViolations := 60
	if aggressive {
		maxViolations = 20
	}

	for _, line := range lines {
		stripped := strings.TrimRight(line, "\r")
		if reLintViolation.MatchString(stripped) {
			if len(violations) < maxViolations {
				violations = append(violations, stripped)
			}
			continue
		}
		if reLintSummary.MatchString(stripped) {
			summaries = append(summaries, stripped)
		}
	}

	if len(violations) == maxViolations {
		violations = append(violations, fmt.Sprintf("[%d violations shown, remainder omitted]", maxViolations))
	}

	var out []string
	out = append(out, violations...)
	out = append(out, summaries...)
	result := strings.Join(out, "\n")
	if result == "" || len(result) >= len(content) {
		return content
	}
	return result
}

// filterLogCompact reduces log output: deduplicates repeated lines, applies line limits.
func filterLogCompact(content string, aggressive bool) string {
	lines := strings.Split(content, "\n")
	seen := make(map[string]int)  // normalized line -> first occurrence index
	var deduplicated []string
	repeatCounts := make(map[int]int) // index in deduplicated -> repeat count

	for _, line := range lines {
		stripped := strings.TrimRight(line, "\r")
		// Normalize timestamps out for dedup comparison
		normalized := reLogNormalize.ReplaceAllString(stripped, "<T>")
		if idx, exists := seen[normalized]; exists {
			repeatCounts[idx]++
		} else {
			seen[normalized] = len(deduplicated)
			deduplicated = append(deduplicated, stripped)
		}
	}

	limit := 80
	if aggressive {
		limit = 30
	}

	var out []string
	for i, line := range deduplicated {
		if i >= limit {
			out = append(out, fmt.Sprintf("[...%d more unique log lines omitted]", len(deduplicated)-limit))
			break
		}
		if count := repeatCounts[i]; count > 0 {
			out = append(out, fmt.Sprintf("%s [x%d]", line, count+1))
		} else {
			out = append(out, line)
		}
	}

	result := strings.Join(out, "\n")
	if result == "" || len(result) >= len(content) {
		return content
	}
	return result
}

// filterDirCompact reduces directory listings to a summary.
func filterDirCompact(content string, aggressive bool) string {
	lines := strings.Split(content, "\n")
	var files, dirs, others int
	var kept []string

	for _, line := range lines {
		stripped := strings.TrimRight(line, "\r")
		if stripped == "" {
			continue
		}
		if strings.HasPrefix(stripped, "total ") {
			continue
		}
		if len(stripped) > 0 && stripped[0] == 'd' && reDirEntry.MatchString(stripped) {
			dirs++
		} else if reDirEntry.MatchString(stripped) || reSimpleEntry.MatchString(stripped) {
			files++
		} else {
			kept = append(kept, stripped)
			others++
		}
	}

	if aggressive || (files+dirs > 20) {
		summary := fmt.Sprintf("[directory: %d files, %d dirs]", files, dirs)
		if len(summary) < len(content) {
			return summary
		}
		return content
	}

	// Moderate: keep entries but without permission columns
	if !aggressive && len(kept) == 0 {
		return content // already simple format, no savings
	}
	return content
}

// filterSearchCompact reduces search results to match lines only, limiting count for old messages.
func filterSearchCompact(content string, aggressive bool) string {
	lines := strings.Split(content, "\n")
	var matchLines []string
	var contextLines []string

	for _, line := range lines {
		stripped := strings.TrimRight(line, "\r")
		if stripped == "" {
			continue
		}
		if reSearchMatch.MatchString(stripped) {
			matchLines = append(matchLines, stripped)
		} else {
			contextLines = append(contextLines, stripped)
		}
	}

	limit := 80
	if aggressive {
		limit = 30
	}

	var out []string
	if len(matchLines) > limit {
		out = matchLines[:limit]
		out = append(out, fmt.Sprintf("[%d more matches omitted]", len(matchLines)-limit))
	} else {
		out = matchLines
		if !aggressive {
			out = append(out, contextLines...)
		}
	}

	result := strings.Join(out, "\n")
	if result == "" || len(result) >= len(content) {
		return content
	}
	return result
}

// Regexes for filter functions.
var (
	reGitCommitHeader = regexp.MustCompile(`^(commit [0-9a-f]{6,40}|Author:|Date:|Merge:)`)
	reGitBranchLine   = regexp.MustCompile(`^(On branch|HEAD detached|Your branch|nothing to commit|Changes)`)
	reGitStats        = regexp.MustCompile(`\d+ files? changed`)
	reGitFileSummary  = regexp.MustCompile(`^(create mode|delete mode|rename |mode change )`)

	reTestSummary = regexp.MustCompile(
		`(?i)(^\s*\d+\s+tests?\s+(?:passed|failed|skipped)|^ok\s+|^FAIL\s+|^test result:|\d+\s+passed,\s+\d+\s+failed)`)
	reTestFail = regexp.MustCompile(`(?m)(^---\s+FAIL:|^FAIL\t|^\s+Error:|panic:|\bFAILED\b)`)
	reTestPass = regexp.MustCompile(`(?m)^---\s+PASS:`)

	reBuildErrorLine   = regexp.MustCompile(`(?i)(error:|: error |^error\[)`)
	reBuildWarningLine = regexp.MustCompile(`(?i)(warning:|: warning )`)
	reBuildInfoLine    = regexp.MustCompile(`(?i)(^Compiling|^Finished|^Running|^Building)`)

	reLintViolation = regexp.MustCompile(
		`(?m)[a-zA-Z0-9_./\\]+:\d+:\d+.*(?:error|warning|info)`)
	reLintSummary = regexp.MustCompile(
		`(?i)(\d+\s+(?:error|warning|problem)|✖|✓)`)

	reLogNormalize = regexp.MustCompile(
		`\d{4}[-/]\d{2}[-/]\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})?`)

	reDirEntry    = regexp.MustCompile(`^[-dlcbsp][rwx-]{9}\s`)
	reSimpleEntry = regexp.MustCompile(`^\S`)

	reSearchMatch = regexp.MustCompile(`^\S.*:\d+:`)
)

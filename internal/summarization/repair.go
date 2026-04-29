package summarization

import (
	"regexp"
	"strings"
)

// alternativeBulletRegex matches lines that begin with `*`, `-`, or a
// digit-dot bullet style and need to be normalised to `- `.
var alternativeBulletRegex = regexp.MustCompile(`^\s*(?:\*|\d+\.)\s+`)

// repairCounters tracks how often each deterministic repair fired so the
// operator can monitor compression quality without an A/B harness. T90.
var repairCounters = struct {
	deterministicTotal int64
	bulletNormalised   int64
	preambleTrimmed    int64
	headerStripped     int64
}{}

// RepairCounts returns the current repair counter snapshot.
func RepairCounts() (deterministicTotal, bulletNormalised, preambleTrimmed, headerStripped int64) {
	return repairCounters.deterministicTotal,
		repairCounters.bulletNormalised,
		repairCounters.preambleTrimmed,
		repairCounters.headerStripped
}

// ResetRepairCounts clears repair counters. Test helper.
func ResetRepairCounts() {
	repairCounters.deterministicTotal = 0
	repairCounters.bulletNormalised = 0
	repairCounters.preambleTrimmed = 0
	repairCounters.headerStripped = 0
}

// RepairSummary applies a small set of deterministic transformations
// that convert near-valid summaries into valid ones. Returns the
// possibly-rewritten summary plus a bool indicating whether any
// transformation was applied. Designed to run before the retry path in
// layer2.go so cheap fixes do not pay an API round-trip. T90.
//
// Order of operations matters: bullets are normalised first so the
// preamble-trim step sees the same `- ` anchor regardless of the
// original style, and headers are stripped before preamble-trim so a
// `# Summary` header above a bullet does not mask the actual preamble.
func RepairSummary(s string) (string, bool) {
	original := s

	// 1. Strip markdown headers first so they do not survive as preamble.
	out := mdHeaderRegex.ReplaceAllString(s, "")
	if out != s {
		repairCounters.headerStripped++
	}

	// 2. Normalise alternative bullet styles per line.
	lines := strings.Split(out, "\n")
	bulletRewrites := 0
	for i, line := range lines {
		if alternativeBulletRegex.MatchString(line) {
			lines[i] = alternativeBulletRegex.ReplaceAllString(line, "- ")
			bulletRewrites++
		}
	}
	if bulletRewrites > 0 {
		repairCounters.bulletNormalised += int64(bulletRewrites)
		out = strings.Join(lines, "\n")
	}

	// 3. Trim leading non-bullet preamble. Only fires when the summary
	//    does not already start with `- `; otherwise the first bullet is
	//    the genuine first fact.
	trimmedLeft := strings.TrimLeft(out, " \t\n\r")
	if !strings.HasPrefix(trimmedLeft, "- ") {
		idx := strings.Index(out, "\n- ")
		if idx >= 0 {
			out = out[idx+1:]
			repairCounters.preambleTrimmed++
		}
	} else if trimmedLeft != out {
		// Leading whitespace before first bullet is benign but we need
		// the `- ` to be the first character per the format contract.
		out = trimmedLeft
	}

	// 4. Collapse runs of blank lines and trim outer whitespace.
	out = multiBlankLineRegex.ReplaceAllString(out, "\n\n")
	out = strings.TrimSpace(out)

	if out != original {
		repairCounters.deterministicTotal++
		return out, true
	}
	return original, false
}

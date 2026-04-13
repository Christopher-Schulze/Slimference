package compression

import (
	"regexp"
	"strings"
)

var (
	reBuildOK = regexp.MustCompile(`(?i)(?:^|\n)\s*(?:BUILD SUCCESSFUL|Build succeeded|Compiling[^\n]*\n[^\n]*successfully|✓\s*Built in)`)
	reTestsOK = regexp.MustCompile(`(?i)(?:^|\n)\s*(?:All tests passed|Tests?:\s*\d+\s+passed(?:,\s*0\s+failed)?|OK\s*\(\d+\s+tests?\)|PASS\s*$)`)
	reLintOK  = regexp.MustCompile(`(?i)(?:^|\n)\s*(?:0 errors?|0 warnings?|No issues found|eslint:\s*no problems)`)
	reErrLine = regexp.MustCompile(`(?i)(?:^|\n)\s*(?:error|errors?|failed|FAIL|FAILED)\b[^\n]*[1-9]`)
)

// MaybeSuccessShortCircuit replaces verbose success-only tool output with a one-liner.
// It is conservative: if any failure/error signal is present, the text is unchanged.
func MaybeSuccessShortCircuit(text string) (string, bool) {
	if len(text) < 80 {
		return text, false
	}
	if reErrLine.MatchString(text) || strings.Contains(strings.ToLower(text), " error:") {
		return text, false
	}
	switch {
	case reBuildOK.MatchString(text) && !strings.Contains(strings.ToLower(text), "error"):
		return "[ok] Build succeeded (output omitted)", true
	case reTestsOK.MatchString(text):
		return "[ok] All tests passed (output omitted)", true
	case reLintOK.MatchString(text):
		return "[ok] Lint clean (output omitted)", true
	default:
		return text, false
	}
}

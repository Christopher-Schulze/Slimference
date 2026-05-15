package compression

import (
	"fmt"
	"regexp"
	"strings"
)

func compactSemanticTestFailure(content string, aggressive bool) string {
	lines := strings.Split(content, "\n")
	if len(lines) < 25 || !looksLikeStacktrace(content) {
		return content
	}

	maxAppFrames := 10
	maxDiffLines := 16
	maxContextLines := 18
	if aggressive {
		maxAppFrames = 6
		maxDiffLines = 10
		maxContextLines = 10
	}

	kept := make([]string, 0, maxAppFrames+maxDiffLines+maxContextLines+8)
	seen := make(map[string]struct{})
	frameworkFrames := 0
	appFrames := 0
	diffLines := 0
	contextLines := 0
	inFailure := false

	add := func(line string) {
		line = strings.TrimRight(line, "\r")
		if _, ok := seen[line]; ok {
			return
		}
		seen[line] = struct{}{}
		kept = append(kept, line)
	}

	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		switch {
		case isTestFailureAnchor(trimmed):
			inFailure = true
			add(line)
		case reTestSummary.MatchString(trimmed):
			inFailure = false
			add(line)
		case isAssertionOrDiffLine(trimmed):
			if diffLines < maxDiffLines {
				add(line)
			}
			diffLines++
		case isFrameworkFrame(trimmed):
			frameworkFrames++
		case isApplicationFrame(trimmed):
			if appFrames < maxAppFrames {
				add(line)
			}
			appFrames++
		case isErrorSignalLine(trimmed):
			inFailure = true
			add(line)
		case inFailure && contextLines < maxContextLines && !isLowValueTestNoise(trimmed):
			add(line)
			contextLines++
		}
	}

	if frameworkFrames > 0 {
		add(fmt.Sprintf("[semantic-test-compact: %d framework/vendor stack frame(s) omitted]", frameworkFrames))
	}
	if appFrames > maxAppFrames {
		add(fmt.Sprintf("[semantic-test-compact: %d additional application frame(s) omitted]", appFrames-maxAppFrames))
	}
	if diffLines > maxDiffLines {
		add(fmt.Sprintf("[semantic-test-compact: %d additional assertion/diff line(s) omitted]", diffLines-maxDiffLines))
	}
	if len(kept) == 0 {
		return content
	}

	result := "[semantic-test-compact]\n" + strings.Join(kept, "\n")
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	if len(result) >= len(content) {
		return content
	}
	return result
}

func looksLikeStacktrace(content string) bool {
	return reStackTraceSignal.MatchString(content)
}

func isTestFailureAnchor(line string) bool {
	return reTestFail.MatchString(line) ||
		strings.HasPrefix(line, "FAIL ") ||
		strings.HasPrefix(line, "FAILED ") ||
		strings.HasPrefix(line, "ERROR ") ||
		strings.HasPrefix(line, "thread '") ||
		strings.HasPrefix(line, "Traceback (most recent call last):")
}

func isAssertionOrDiffLine(line string) bool {
	if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
		return true
	}
	return reAssertionSignal.MatchString(line)
}

func isErrorSignalLine(line string) bool {
	return reErrorSignal.MatchString(line)
}

func isApplicationFrame(line string) bool {
	if isFrameworkFrame(line) {
		return false
	}
	return reAppFrame.MatchString(line)
}

func isFrameworkFrame(line string) bool {
	return reFrameworkFrame.MatchString(line)
}

func isLowValueTestNoise(line string) bool {
	return strings.HasPrefix(line, "=== RUN ") ||
		strings.HasPrefix(line, "--- PASS:") ||
		strings.HasPrefix(line, "PASS ") ||
		strings.HasPrefix(line, "ok  ") ||
		strings.Contains(line, "npm notice") ||
		strings.Contains(line, "running 0 tests")
}

var (
	reStackTraceSignal = regexp.MustCompile(
		`(?m)(Traceback \(most recent call last\):|^\s+at\s+|^\s*goroutine \d+ \[|^\s*stack backtrace:|^\s*at [^ ]+\.(?:go|rs|py|tsx?|jsx?|java|kt|c|cpp):\d+)`)
	reAssertionSignal = regexp.MustCompile(
		`(?i)(assert|expected|actual|received|got:|got |want:|want |diff|snapshot|AssertionError|panic:)`)
	reErrorSignal = regexp.MustCompile(
		`(?i)(panic:|fatal:|AssertionError|TypeError|ReferenceError|Error:|failed|failure|exception)`)
	reAppFrame = regexp.MustCompile(
		`(?m)([A-Za-z0-9_./\\-]+\.(?:go|rs|py|tsx?|jsx?|java|kt|c|cc|cpp|h|hpp):\d+(?::\d+)?|File "([^"]+)\.(?:py)", line \d+|at .*\(([^)]+)\.(?:tsx?|jsx?):\d+:\d+\)|at [^ ]+\.(?:tsx?|jsx?):\d+:\d+)`)
	reFrameworkFrame = regexp.MustCompile(
		`(?i)(node_modules/|/pkg/mod/|\.cargo/registry|site-packages/|\.venv/|/vendor/|runtime/panic\.go|runtime/asm_|testing\.go:|jest-|vitest|pytest|_pytest|org\.junit|junit\.framework|react-dom|@testing-library)`)
)

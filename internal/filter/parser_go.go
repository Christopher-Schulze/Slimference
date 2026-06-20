package filter

import (
	"regexp"
	"strconv"
	"strings"
)

var reGoError = regexp.MustCompile(`^\.?/?[^\s:]+\.go:\d+(:\d+)?\s*:`)
var reGoTestFail = regexp.MustCompile(`^--- FAIL: `)
var reGoTestPanic = regexp.MustCompile(`^panic: `)

func parseGoErrors(stdout string) (string, bool, bool) {
	if stdout == "" {
		return "", false, false
	}
	lines := strings.Split(stdout, "\n")
	type errEntry struct {
		lineNo  int
		content string
	}
	var errs []errEntry

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if reGoError.MatchString(trimmed) {
			errs = append(errs, errEntry{i, trimmed})
		} else if reGoTestFail.MatchString(trimmed) {
			errs = append(errs, errEntry{i, trimmed})
		} else if reGoTestPanic.MatchString(trimmed) {
			errs = append(errs, errEntry{i, trimmed})
		}
	}

	if len(errs) == 0 {
		if detectBuildSuccess(stdout) {
			if buildOutputHasNonZeroWarning(stdout) {
				return "", false, false
			}
			return "[go build] ok\n", false, true
		}
		return "", false, false
	}

	kept := map[int]bool{}
	for _, e := range errs {
		for j := e.lineNo - failureContextLines; j <= e.lineNo+failureContextLines; j++ {
			if j >= 0 && j < len(lines) {
				kept[j] = true
			}
		}
	}

	var out strings.Builder
	out.WriteString("[go build] FAILED\n")
	lastLine := ""
	repeatCount := 0
	hasLast := false
	flushLast := func() {
		if !hasLast {
			return
		}
		out.WriteString(lastLine)
		if lastLine != "" && repeatCount > 1 {
			out.WriteString(" [x")
			out.WriteString(strconv.Itoa(repeatCount))
			out.WriteByte(']')
		}
		out.WriteByte('\n')
	}
	for i, line := range lines {
		if kept[i] {
			trimmed := strings.TrimSpace(line)
			if trimmed == lastLine {
				repeatCount++
				continue
			}
			flushLast()
			lastLine = trimmed
			repeatCount = 1
			hasLast = true
		}
	}
	flushLast()
	result := out.String()
	if len(result) >= len(stdout) {
		return "", false, false
	}
	return result, true, true
}

type failureBlock struct {
	lines   []string
	context []string
	isError bool
}

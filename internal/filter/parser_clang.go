package filter

import (
	"regexp"
	"strings"
)

var reGccError = regexp.MustCompile(`^[^\s:]+:\d+(:\d+)?:\s*(error|fatal error|warning):\s`)
var reGccNote = regexp.MustCompile(`^[^\s:]+:\d+(:\d+)?:\s*(note|remark):\s`)

func parseGccClangErrors(stdout string) (string, bool, bool) {
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
		if reGccError.MatchString(trimmed) {
			errs = append(errs, errEntry{i, trimmed})
		}
	}

	if len(errs) == 0 {
		if detectBuildSuccess(stdout) {
			return "[gcc/clang] ok\n", false, true
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
	out.WriteString("[gcc] FAILED\n")
	for i, line := range lines {
		if kept[i] {
			out.WriteString(strings.TrimSpace(line))
			out.WriteByte('\n')
		}
	}
	result := out.String()
	if len(result) >= len(stdout) {
		return "", false, false
	}
	return result, true, true
}

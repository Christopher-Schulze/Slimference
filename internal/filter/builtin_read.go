package filter

import (
	"path/filepath"
	"regexp"
	"strings"
)

// FileReadContext carries request/session signals used by readcache safety
// decisions. First file reads full-pass; this context must not enable lossy
// first-read file compaction.
type FileReadContext struct {
	Mode           string
	RecentlyEdited bool
}

// ReadRequest describes the single file/range read represented by a simple
// cat/head/tail/sed command. Offset/Limit are line-oriented for shell range
// reads; full-file cat uses Offset=0, Limit=0.
type ReadRequest struct {
	Path   string
	Offset int
	Limit  int
}

func (r ReadRequest) IsFull() bool {
	return strings.TrimSpace(r.Path) != "" && r.Offset == 0 && r.Limit == 0
}

// ReadPathFromCommandLine returns the single file path read by a simple
// cat/head/tail command line. Compound commands intentionally return empty.
func ReadPathFromCommandLine(commandLine string) string {
	req, ok := ReadRequestFromCommandLine(commandLine)
	if !ok {
		return ""
	}
	return req.Path
}

// FullReadPathFromCommandLine returns the single file path read by a full-file
// `cat` command. Partial reads such as head/tail intentionally return empty so
// callers do not treat range output as a full file snapshot.
func FullReadPathFromCommandLine(commandLine string) string {
	req, ok := ReadRequestFromCommandLine(commandLine)
	if !ok || !req.IsFull() {
		return ""
	}
	return req.Path
}

// ReadRequestFromCommandLine returns the file/range read represented by a simple
// cat/head/tail/sed/awk command line. Compound commands intentionally return false.
func ReadRequestFromCommandLine(commandLine string) (ReadRequest, bool) {
	for _, tok := range tokenize(commandLine) {
		if tok.Kind == TokenOperator || tok.Kind == TokenPipe || tok.Kind == TokenRedirect || tok.Kind == TokenShellism {
			return ReadRequest{}, false
		}
	}
	argv := primaryArgvForCapturedOutput(commandLine)
	return readRequestFromArgv(argv)
}

func readRequestFromArgv(argv []string) (ReadRequest, bool) {
	if len(argv) == 0 {
		return ReadRequest{}, false
	}
	switch strings.ToLower(filepath.Base(argv[0])) {
	case "cat":
		if countReadPaths(argv) != 1 {
			return ReadRequest{}, false
		}
		if !isFullFileCat(argv) {
			return ReadRequest{}, false
		}
		return ReadRequest{Path: lastReadFilePath(argv)}, true
	case "head":
		if countReadPaths(argv) != 1 {
			return ReadRequest{}, false
		}
		limit, ok := headLineLimit(argv)
		if !ok {
			return ReadRequest{}, false
		}
		return ReadRequest{Path: lastReadFilePath(argv), Offset: 1, Limit: limit}, true
	case "tail":
		if countReadPaths(argv) != 1 {
			return ReadRequest{}, false
		}
		offset, limit, ok := tailLineRange(argv)
		if !ok {
			return ReadRequest{}, false
		}
		return ReadRequest{Path: lastReadFilePath(argv), Offset: offset, Limit: limit}, true
	case "sed":
		if countReadPaths(argv) != 1 {
			return ReadRequest{}, false
		}
		offset, limit, ok := sedLineRange(argv)
		if !ok {
			return ReadRequest{}, false
		}
		return ReadRequest{Path: lastReadFilePath(argv), Offset: offset, Limit: limit}, true
	case "awk":
		return awkReadRequest(argv)
	default:
		return ReadRequest{}, false
	}
}

func isFullFileCat(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	return strings.EqualFold(filepath.Base(argv[0]), "cat")
}

func countReadPaths(argv []string) int {
	n := 0
	for i := 1; i < len(argv); i++ {
		a := argv[i]
		if strings.HasPrefix(a, "-") {
			switch a {
			case "-n", "-c", "--bytes", "--lines":
				if i+1 < len(argv) {
					i++
				}
			}
			continue
		}
		n++
	}
	return n
}

func lastReadFilePath(argv []string) string {
	var last string
	for i := 1; i < len(argv); i++ {
		a := argv[i]
		if strings.HasPrefix(a, "-") {
			switch a {
			case "-n", "-c", "--bytes", "--lines":
				if i+1 < len(argv) {
					i++
				}
			}
			continue
		}
		last = a
	}
	return last
}

func headLineLimit(argv []string) (int, bool) {
	for i := 1; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "-n" || a == "--lines":
			if i+1 >= len(argv) {
				return 0, false
			}
			return parsePositiveLineCount(argv[i+1])
		case strings.HasPrefix(a, "-n") && len(a) > len("-n"):
			return parsePositiveLineCount(strings.TrimPrefix(a, "-n"))
		case strings.HasPrefix(a, "--lines="):
			return parsePositiveLineCount(strings.TrimPrefix(a, "--lines="))
		case strings.HasPrefix(a, "-") && len(a) > 1 && allDigits(a[1:]):
			return parsePositiveLineCount(a[1:])
		case a == "-c" || a == "--bytes" || strings.HasPrefix(a, "-c") || strings.HasPrefix(a, "--bytes="):
			return 0, false
		}
	}
	return 10, true
}

func tailLineRange(argv []string) (offset int, limit int, ok bool) {
	for i := 1; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "-n" || a == "--lines":
			if i+1 >= len(argv) {
				return 0, 0, false
			}
			return parseTailLineRange(argv[i+1])
		case strings.HasPrefix(a, "-n") && len(a) > len("-n"):
			return parseTailLineRange(strings.TrimPrefix(a, "-n"))
		case strings.HasPrefix(a, "--lines="):
			return parseTailLineRange(strings.TrimPrefix(a, "--lines="))
		case strings.HasPrefix(a, "-") && len(a) > 1 && allDigits(a[1:]):
			n, ok := parsePositiveLineCount(a[1:])
			if !ok {
				return 0, 0, false
			}
			return -n, n, true
		case a == "-c" || a == "--bytes" || strings.HasPrefix(a, "-c") || strings.HasPrefix(a, "--bytes="):
			return 0, 0, false
		}
	}
	return -10, 10, true
}

func sedLineRange(argv []string) (int, int, bool) {
	if len(argv) < 3 || argv[1] != "-n" {
		return 0, 0, false
	}
	expr := strings.TrimSpace(argv[2])
	expr = strings.Trim(expr, `'"`)
	if !strings.HasSuffix(expr, "p") {
		return 0, 0, false
	}
	expr = strings.TrimSuffix(expr, "p")
	if strings.ContainsAny(expr, "$/") {
		return 0, 0, false
	}
	parts := strings.Split(expr, ",")
	if len(parts) == 1 {
		start, ok := parsePositiveLineCount(parts[0])
		if !ok {
			return 0, 0, false
		}
		return start, 1, true
	}
	if len(parts) != 2 {
		return 0, 0, false
	}
	start, ok := parsePositiveLineCount(parts[0])
	if !ok {
		return 0, 0, false
	}
	end, ok := parsePositiveLineCount(parts[1])
	if !ok || end < start {
		return 0, 0, false
	}
	return start, end - start + 1, true
}

var (
	awkSingleLinePattern = regexp.MustCompile(`^NR==([0-9]+)$`)
	awkRangePattern      = regexp.MustCompile(`^NR>=([0-9]+)&&NR<=([0-9]+)$`)
	awkFromPattern       = regexp.MustCompile(`^NR>=([0-9]+)$`)
	awkUntilPattern      = regexp.MustCompile(`^NR<=([0-9]+)$`)
)

func awkReadRequest(argv []string) (ReadRequest, bool) {
	if len(argv) != 3 || strings.TrimSpace(argv[2]) == "" || strings.TrimSpace(argv[2]) == "-" {
		return ReadRequest{}, false
	}
	offset, limit, ok := awkLineRange(argv[1])
	if !ok {
		return ReadRequest{}, false
	}
	return ReadRequest{Path: argv[2], Offset: offset, Limit: limit}, true
}

func awkLineRange(expr string) (int, int, bool) {
	expr = strings.TrimSpace(strings.Trim(expr, `'"`))
	expr = strings.NewReplacer(" ", "", "\t", "", "\n", "", "\r", "").Replace(expr)
	var selector string
	switch {
	case strings.HasSuffix(expr, "{print}"):
		selector = strings.TrimSuffix(expr, "{print}")
	case strings.HasSuffix(expr, "{print$0}"):
		selector = strings.TrimSuffix(expr, "{print$0}")
	default:
		return 0, 0, false
	}
	if match := awkSingleLinePattern.FindStringSubmatch(selector); len(match) == 2 {
		line, ok := parsePositiveLineCount(match[1])
		return line, 1, ok
	}
	if match := awkRangePattern.FindStringSubmatch(selector); len(match) == 3 {
		start, ok := parsePositiveLineCount(match[1])
		if !ok {
			return 0, 0, false
		}
		end, ok := parsePositiveLineCount(match[2])
		if !ok || end < start {
			return 0, 0, false
		}
		return start, end - start + 1, true
	}
	if match := awkFromPattern.FindStringSubmatch(selector); len(match) == 2 {
		start, ok := parsePositiveLineCount(match[1])
		if !ok {
			return 0, 0, false
		}
		return start, 0, true
	}
	if match := awkUntilPattern.FindStringSubmatch(selector); len(match) == 2 {
		end, ok := parsePositiveLineCount(match[1])
		if !ok {
			return 0, 0, false
		}
		return 1, end, true
	}
	return 0, 0, false
}

func parseTailLineRange(raw string) (int, int, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "+") {
		n, ok := parsePositiveLineCount(strings.TrimPrefix(raw, "+"))
		if !ok {
			return 0, 0, false
		}
		return n, 0, true
	}
	n, ok := parsePositiveLineCount(strings.TrimPrefix(raw, "-"))
	if !ok {
		return 0, 0, false
	}
	return -n, n, true
}

func parsePositiveLineCount(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !allDigits(raw) {
		return 0, false
	}
	n := 0
	for _, r := range raw {
		n = n*10 + int(r-'0')
	}
	return n, n > 0
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

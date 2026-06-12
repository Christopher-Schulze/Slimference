package filter

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// FileReadContext carries request/session signals used by readcache safety
// decisions. First file reads full-pass; this context must not enable lossy
// first-read file compaction.
type FileReadContext struct {
	Mode                 string
	RecentlyEdited       bool
	SearchCompactOptions SearchCompactOptions
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
	toks := tokenize(commandLine)
	if req, ok := nlSedReadRequestFromTokens(toks); ok {
		return req, true
	}
	for _, tok := range toks {
		if tok.Kind == TokenOperator || tok.Kind == TokenPipe || tok.Kind == TokenRedirect || tok.Kind == TokenShellism {
			return ReadRequest{}, false
		}
	}
	argv := primaryArgvForCapturedOutput(commandLine)
	return readRequestFromArgv(argv)
}

// NormalizeReadCommandLine returns a canonical read command with a relative
// path resolved against workdir. It only rewrites commands that are already
// accepted by ReadRequestFromCommandLine.
func NormalizeReadCommandLine(commandLine, workdir string) string {
	workdir = cleanReadWorkdir(workdir)
	if workdir == "" {
		return ""
	}
	req, ok := ReadRequestFromCommandLine(commandLine)
	if !ok || strings.TrimSpace(req.Path) == "" || filepath.IsAbs(req.Path) {
		return ""
	}
	absPath := filepath.Clean(filepath.Join(workdir, req.Path))
	if out := normalizeNLSedReadCommand(commandLine, req, absPath); out != "" {
		return out
	}
	argv := primaryArgvForCapturedOutput(commandLine)
	if len(argv) == 0 {
		return ""
	}
	out := append([]string(nil), argv...)
	for i := len(out) - 1; i >= 1; i-- {
		if out[i] == req.Path {
			out[i] = absPath
			return joinReadArgs(out)
		}
	}
	return ""
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
	return parseSedLineRangeExpr(argv[2])
}

func parseSedLineRangeExpr(raw string) (int, int, bool) {
	expr := strings.TrimSpace(raw)
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

func nlSedReadRequestFromTokens(toks []ParsedToken) (ReadRequest, bool) {
	pipe := -1
	for i, tok := range toks {
		switch tok.Kind {
		case TokenArg:
		case TokenPipe:
			if pipe >= 0 {
				return ReadRequest{}, false
			}
			pipe = i
		default:
			return ReadRequest{}, false
		}
	}
	if pipe <= 0 || pipe >= len(toks)-1 {
		return ReadRequest{}, false
	}
	left := tokenArgs(toks[:pipe])
	right := tokenArgs(toks[pipe+1:])
	path, ok := nlBodyAllPath(left)
	if !ok {
		return ReadRequest{}, false
	}
	offset, limit, ok := pipedSedLineRange(right)
	if !ok {
		return ReadRequest{}, false
	}
	return ReadRequest{Path: path, Offset: offset, Limit: limit}, true
}

func tokenArgs(toks []ParsedToken) []string {
	out := make([]string, 0, len(toks))
	for _, tok := range toks {
		if tok.Kind == TokenArg {
			out = append(out, tok.Value)
		}
	}
	return out
}

func nlBodyAllPath(argv []string) (string, bool) {
	if len(argv) < 3 || strings.ToLower(filepath.Base(argv[0])) != "nl" {
		return "", false
	}
	bodyAll := false
	var paths []string
	for i := 1; i < len(argv); i++ {
		arg := argv[i]
		if arg == "--" {
			paths = append(paths, argv[i+1:]...)
			break
		}
		switch {
		case arg == "-ba" || arg == "--body-numbering=a":
			bodyAll = true
		case arg == "-b" || arg == "--body-numbering":
			if i+1 >= len(argv) {
				return "", false
			}
			i++
			bodyAll = argv[i] == "a"
		case strings.HasPrefix(arg, "-b") && len(arg) > len("-b"):
			bodyAll = strings.TrimPrefix(arg, "-b") == "a"
		case nlOptionConsumesNext(arg):
			if i+1 >= len(argv) {
				return "", false
			}
			i++
		case nlInlineOption(arg):
		case strings.HasPrefix(arg, "-"):
			return "", false
		default:
			paths = append(paths, arg)
		}
	}
	if !bodyAll || len(paths) != 1 || strings.TrimSpace(paths[0]) == "" || paths[0] == "-" {
		return "", false
	}
	return paths[0], true
}

func nlOptionConsumesNext(arg string) bool {
	switch arg {
	case "-d", "-f", "-h", "-i", "-l", "-n", "-s", "-v", "-w",
		"--section-delimiter", "--footer-numbering", "--header-numbering",
		"--line-increment", "--join-blank-lines", "--number-format",
		"--number-separator", "--starting-line-number", "--number-width":
		return true
	default:
		return false
	}
}

func nlInlineOption(arg string) bool {
	if arg == "-p" || arg == "--no-renumber" {
		return true
	}
	for _, prefix := range []string{
		"-d", "-f", "-h", "-i", "-l", "-n", "-s", "-v", "-w",
		"--section-delimiter=", "--footer-numbering=", "--header-numbering=",
		"--line-increment=", "--join-blank-lines=", "--number-format=",
		"--number-separator=", "--starting-line-number=", "--number-width=",
	} {
		if strings.HasPrefix(arg, prefix) && len(arg) > len(prefix) {
			return true
		}
	}
	return false
}

func pipedSedLineRange(argv []string) (int, int, bool) {
	if len(argv) != 3 || strings.ToLower(filepath.Base(argv[0])) != "sed" || argv[1] != "-n" {
		return 0, 0, false
	}
	return parseSedLineRangeExpr(argv[2])
}

func normalizeNLSedReadCommand(commandLine string, req ReadRequest, absPath string) string {
	if _, ok := nlSedReadRequestFromTokens(tokenize(commandLine)); !ok {
		return ""
	}
	end := req.Offset
	if req.Limit > 0 {
		end = req.Offset + req.Limit - 1
	}
	expr := strconv.Itoa(req.Offset) + "p"
	if req.Limit != 1 {
		expr = strconv.Itoa(req.Offset) + "," + strconv.Itoa(end) + "p"
	}
	return "nl -ba " + quoteReadArg(absPath) + " | sed -n " + quoteReadArg(expr)
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

func cleanReadWorkdir(workdir string) string {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" || !filepath.IsAbs(workdir) {
		return ""
	}
	return filepath.Clean(workdir)
}

func joinReadArgs(argv []string) string {
	parts := make([]string, 0, len(argv))
	for _, arg := range argv {
		parts = append(parts, quoteReadArg(arg))
	}
	return strings.Join(parts, " ")
}

func quoteReadArg(arg string) string {
	if arg == "" {
		return `""`
	}
	if strings.Contains(arg, "$") && !strings.Contains(arg, "'") {
		return "'" + arg + "'"
	}
	if strings.IndexFunc(arg, func(r rune) bool {
		return r == '"' || r == '\\' || r <= ' ' || r == '\'' || r == '$' || r == '`' ||
			r == '|' || r == '&' || r == ';' || r == '<' || r == '>' || r == '*' ||
			r == '?' || r == '(' || r == ')'
	}) < 0 {
		return arg
	}
	return strconv.Quote(arg)
}

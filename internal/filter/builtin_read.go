package filter

import (
	"path/filepath"
	"strings"

	"github.com/slimference/slimference/internal/codecompact"
	"github.com/slimference/slimference/internal/compression"
)

// signatureOnlyThreshold is the byte size above which we try structure extraction
// (functions/types only) instead of comment strip alone.
const signatureOnlyThreshold = 3000

// FileReadContext carries request/session signals that decide whether a file
// read can be safely compacted. Empty context preserves legacy scan behaviour.
type FileReadContext struct {
	Mode            string
	RecentlyEdited  bool
	ForceFull       bool
	RelevantSymbols []string
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

// TryStripCommentsFileRead compacts cat/head/tail stdout (F06).
// Single file: strips comments. Large single file: also attempts signature extraction.
// Multiple files with known extensions: applies comment strip to each section.
func TryStripCommentsFileRead(argv []string, stdout []byte) ([]byte, bool) {
	return TryStripCommentsFileReadWithContext(argv, stdout, FileReadContext{Mode: "scan"})
}

// TryStripCommentsFileReadWithContext is the context-aware variant used by
// PostToolUse/session-aware paths. It bypasses compaction for recently edited
// or edit/debug reads so the model receives exact file contents when it is
// likely to modify or inspect details.
func TryStripCommentsFileReadWithContext(argv []string, stdout []byte, ctx FileReadContext) ([]byte, bool) {
	if len(argv) < 2 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "cat" && b != "head" && b != "tail" {
		return stdout, false
	}

	nPaths := countReadPaths(argv)
	if nPaths == 0 {
		return stdout, false
	}
	if ctx.ForceFull || ctx.RecentlyEdited || isEditOrDebugReadMode(ctx.Mode) {
		return stdout, false
	}

	if nPaths == 1 {
		// Single-file path: strip comments, optionally extract signatures.
		return compactSingleFileReadWithContext(argv, lastReadFilePath(argv), stdout, ctx)
	}

	// Multi-file: all file paths must have recognized extensions.
	// We strip comments per file using the last file's extension as a hint
	// (all files assumed to be the same language for simplicity).
	lang := compression.LanguageFromPath(lastReadFilePath(argv))
	if lang == "" {
		return stdout, false
	}
	s := string(stdout)
	out := compression.StripComments(s, lang)
	if len(out) >= len(s) {
		return stdout, false
	}
	return []byte(out), true
}

// compactSingleFileRead applies comment strip and optionally structure extraction to one file.
func compactSingleFileRead(argv []string, path string, stdout []byte) ([]byte, bool) {
	return compactSingleFileReadWithContext(argv, path, stdout, FileReadContext{Mode: "scan"})
}

func compactSingleFileReadWithContext(argv []string, path string, stdout []byte, ctx FileReadContext) ([]byte, bool) {
	if ctx.ForceFull || ctx.RecentlyEdited || isEditOrDebugReadMode(ctx.Mode) {
		return stdout, false
	}
	lang := compression.LanguageFromPath(path)
	if lang == "" {
		return stdout, false
	}
	s := string(stdout)

	// For large Go files read via full `cat`, use the AST compactor before
	// regex structure extraction. head/tail are already partial reads and must
	// stay literal.
	if isFullFileCat(argv) && lang == "go" {
		if out, _, ok, err := codecompact.Compact(path, stdout, codecompact.Options{
			Mode:                 fileReadMode(ctx.Mode),
			RecentlyEdited:       ctx.RecentlyEdited,
			ForceFull:            ctx.ForceFull,
			RelevantSymbols:      append([]string(nil), ctx.RelevantSymbols...),
			MaxIncludedBodyLines: 12,
		}); err == nil && ok {
			return out, true
		}
	}

	// For large files, try structure (signature) extraction first.
	if len(stdout) >= signatureOnlyThreshold {
		if extracted, ok := compression.ExtractStructure(s, lang); ok && len(extracted) < len(s) {
			return []byte(extracted), true
		}
	}

	// Fallback: comment strip.
	out := compression.StripComments(s, lang)
	if len(out) >= len(s) {
		return stdout, false
	}
	return []byte(out), true
}

func isEditOrDebugReadMode(mode string) bool {
	switch fileReadMode(mode) {
	case "edit", "debug":
		return true
	default:
		return false
	}
}

func fileReadMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return "scan"
	}
	return mode
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
// cat/head/tail/sed command line. Compound commands intentionally return false.
func ReadRequestFromCommandLine(commandLine string) (ReadRequest, bool) {
	for _, tok := range tokenize(commandLine) {
		if tok.Kind == TokenOperator || tok.Kind == TokenPipe || tok.Kind == TokenRedirect || tok.Kind == TokenShellism {
			return ReadRequest{}, false
		}
	}
	argv := primaryArgvForCapturedOutput(commandLine)
	if len(argv) == 0 || countReadPaths(argv) != 1 {
		return ReadRequest{}, false
	}
	switch strings.ToLower(filepath.Base(argv[0])) {
	case "cat":
		if !isFullFileCat(argv) {
			return ReadRequest{}, false
		}
		return ReadRequest{Path: lastReadFilePath(argv)}, true
	case "head":
		limit, ok := headLineLimit(argv)
		if !ok {
			return ReadRequest{}, false
		}
		return ReadRequest{Path: lastReadFilePath(argv), Offset: 1, Limit: limit}, true
	case "tail":
		offset, limit, ok := tailLineRange(argv)
		if !ok {
			return ReadRequest{}, false
		}
		return ReadRequest{Path: lastReadFilePath(argv), Offset: offset, Limit: limit}, true
	case "sed":
		offset, limit, ok := sedLineRange(argv)
		if !ok {
			return ReadRequest{}, false
		}
		return ReadRequest{Path: lastReadFilePath(argv), Offset: offset, Limit: limit}, true
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

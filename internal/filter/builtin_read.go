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
	for _, tok := range tokenize(commandLine) {
		if tok.Kind == TokenOperator || tok.Kind == TokenPipe || tok.Kind == TokenRedirect || tok.Kind == TokenShellism {
			return ""
		}
	}
	argv := primaryArgvForCapturedOutput(commandLine)
	if len(argv) == 0 || countReadPaths(argv) != 1 {
		return ""
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "cat" && b != "head" && b != "tail" {
		return ""
	}
	return lastReadFilePath(argv)
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

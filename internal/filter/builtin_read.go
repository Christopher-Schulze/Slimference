package filter

import (
	"path/filepath"
	"strings"

	"github.com/tokenproxy/tokenproxy/internal/compression"
)

// signatureOnlyThreshold is the byte size above which we try structure extraction
// (functions/types only) instead of comment strip alone.
const signatureOnlyThreshold = 3000

// TryStripCommentsFileRead compacts cat/head/tail stdout (F06).
// Single file: strips comments. Large single file: also attempts signature extraction.
// Multiple files with known extensions: applies comment strip to each section.
func TryStripCommentsFileRead(argv []string, stdout []byte) ([]byte, bool) {
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

	if nPaths == 1 {
		// Single-file path: strip comments, optionally extract signatures.
		return compactSingleFileRead(lastReadFilePath(argv), stdout)
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
func compactSingleFileRead(path string, stdout []byte) ([]byte, bool) {
	lang := compression.LanguageFromPath(path)
	if lang == "" {
		return stdout, false
	}
	s := string(stdout)

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

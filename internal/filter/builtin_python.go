package filter

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
)

// TryCompactPythonTraceback compresses Python traceback output. It keeps the
// final stack frame (most relevant) plus the exception message and drops
// intermediate frames that come from library paths. It is safe on any stdout
// by detecting the classic traceback anchor; non-traceback input is passed
// through unchanged.
//
// Behaviour:
//   - only emits a shorter output when the result is strictly smaller than
//     the input (zero-downside guarantee).
//   - preserves the user's own last frame verbatim, including file path,
//     line number, function name, and the source line shown by the
//     interpreter.
//   - handles chained exceptions ("During handling of the above exception,
//     another exception occurred:" / "The above exception was the direct
//     cause of the following exception:") by keeping each chain boundary
//     plus its terminal frame and exception line.
//
// Rationale: Python tracebacks are a disproportionately common token sink in
// interactive coding sessions. The meaningful information is in the final
// frame and the exception message; the library-frame spam between them is
// redundant once the exception type is visible.
func TryCompactPythonTraceback(stdout []byte) ([]byte, bool) {
	if !tracebackAnchor.Match(stdout) {
		return stdout, false
	}
	compressed := compressPythonTraceback(stdout)
	if len(compressed) >= len(stdout) {
		return stdout, false
	}
	return compressed, true
}

var (
	tracebackAnchor      = regexp.MustCompile(`(?m)^Traceback \(most recent call last\):`)
	tracebackFrameHeader = regexp.MustCompile(`^\s*File "([^"]+)", line \d+, in .+`)
	// Library-path heuristic: frames from these roots are considered library
	// frames and may be dropped.
	libraryPathMarkers = []string{
		"/site-packages/",
		"/dist-packages/",
		"/.venv/",
		"/venv/",
		"/env/",
		"/usr/lib/",
		"/usr/local/lib/",
		"/Library/Frameworks/",
		"/opt/homebrew/",
		// Windows-style site-packages
		`\site-packages\`,
		`\dist-packages\`,
		`\.venv\`,
		`\venv\`,
		`\Python\Python`,
	}
)

// compressPythonTraceback walks stdout in order and returns a compressed
// variant. It preserves non-traceback surrounding text and rewrites each
// traceback block to its essential frames.
func compressPythonTraceback(stdout []byte) []byte {
	lines := strings.Split(string(stdout), "\n")
	var out []string
	i := 0
	for i < len(lines) {
		if strings.HasPrefix(lines[i], "Traceback (most recent call last):") {
			end := findTracebackEnd(lines, i+1)
			compressed := compressSingleTraceback(lines[i:end])
			out = append(out, compressed...)
			i = end
			continue
		}
		out = append(out, lines[i])
		i++
	}
	return []byte(strings.Join(out, "\n"))
}

// findTracebackEnd returns the index one past the last line of a traceback
// block starting at start. A block ends at the first line that is neither a
// frame header, a source line, nor the terminal exception line. The outer
// caller handles chained-exception anchors - a single invocation here covers
// exactly one block.
func findTracebackEnd(lines []string, start int) int {
	sawException := false
	for i := start; i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			if sawException {
				return i
			}
			continue
		}
		if tracebackFrameHeader.MatchString(line) {
			sawException = false
			continue
		}
		if strings.HasPrefix(line, "    ") {
			// source line inside a frame
			continue
		}
		trimmed := strings.TrimSpace(line)
		// Terminal exception line like `ValueError: invalid literal`.
		if !sawException && looksLikeExceptionLine(trimmed) {
			sawException = true
			continue
		}
		// Anything else ends the block (either after the exception line or
		// when the interpreter output transitioned to unrelated content).
		return i
	}
	return len(lines)
}

// compressSingleTraceback rewrites one traceback block.
func compressSingleTraceback(block []string) []string {
	var result []string
	var frames []tracebackFrame
	flushFrames := func() {
		if len(frames) == 0 {
			return
		}
		// Drop library frames that are sandwiched between user frames.
		kept := keepInformativeFrames(frames)
		droppedLib := len(frames) - len(kept)
		if droppedLib > 0 {
			result = append(result, indentedNote(droppedLib))
		}
		for _, fr := range kept {
			result = append(result, fr.header)
			result = append(result, fr.body...)
		}
		frames = nil
	}

	for _, line := range block {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Traceback (most recent call last):") {
			flushFrames()
			result = append(result, line)
			continue
		}
		if tracebackFrameHeader.MatchString(line) {
			frames = append(frames, tracebackFrame{header: line, isLib: isLibraryFrameHeader(line)})
			continue
		}
		if strings.HasPrefix(line, "    ") && len(frames) > 0 {
			frames[len(frames)-1].body = append(frames[len(frames)-1].body, line)
			continue
		}
		// Terminal exception line or any other trailing content inside the
		// block. Flush accumulated frames and pass the line through.
		flushFrames()
		result = append(result, line)
	}
	flushFrames()
	return result
}

// keepInformativeFrames retains the first frame and every non-library frame
// plus the final frame. When a long library run is collapsed, one elided
// marker per run is added.
func keepInformativeFrames(frames []tracebackFrame) []tracebackFrame {
	if len(frames) <= 2 {
		return frames
	}
	kept := make([]tracebackFrame, 0, len(frames))
	kept = append(kept, frames[0])
	for _, fr := range frames[1 : len(frames)-1] {
		if !fr.isLib {
			kept = append(kept, fr)
		}
	}
	kept = append(kept, frames[len(frames)-1])
	return kept
}

// tracebackFrame is a single traceback frame plus its source-line body and
// a library-origin flag used to decide whether it may be elided.
type tracebackFrame struct {
	header string
	body   []string
	isLib  bool
}

func isLibraryFrameHeader(line string) bool {
	m := tracebackFrameHeader.FindStringSubmatch(line)
	if len(m) < 2 {
		return false
	}
	path := m[1]
	for _, marker := range libraryPathMarkers {
		if strings.Contains(path, marker) {
			return true
		}
	}
	return false
}

func looksLikeExceptionLine(trimmed string) bool {
	// Exception lines follow the pattern "QualifiedName: message" where the
	// qualified name is mostly letters, digits, underscores and dots. A
	// colon at position 0 (empty name) is rejected by the index check, so
	// `name` is always non-empty after this point.
	colon := strings.Index(trimmed, ":")
	if colon <= 0 {
		return false
	}
	name := trimmed[:colon]
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '.':
		default:
			return false
		}
	}
	// First character must be an uppercase letter for a canonical exception.
	return name[0] >= 'A' && name[0] <= 'Z'
}

func indentedNote(count int) string {
	if count == 1 {
		return "  [... 1 library frame elided]"
	}
	return "  [... " + strconv.Itoa(count) + " library frames elided]"
}

// bytesHasPythonTraceback is a package-private helper used by tests to assert
// the anchor check without touching regex internals.
func bytesHasPythonTraceback(b []byte) bool {
	return bytes.Contains(b, []byte("Traceback (most recent call last):"))
}

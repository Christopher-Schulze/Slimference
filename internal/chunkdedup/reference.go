package chunkdedup

import (
	"fmt"
	"regexp"
	"strconv"
)

var referencePattern = regexp.MustCompile(`\[context-chunk status=unchanged uri=(local-archive://[A-Za-z0-9_\-]+) bytes=([0-9]+)\]`)

// FormatReference returns the neutral model-facing marker used for a repeated
// chunk. The URI stays a normal local-archive reference, so the existing
// recovery path can expand it if the model asks for the full region.
func FormatReference(uri string, size int) string {
	if size < 0 {
		size = 0
	}
	return fmt.Sprintf("[context-chunk status=unchanged uri=%s bytes=%d]", uri, size)
}

// DecodeReferences expands context-chunk references in text using expand. It is
// the inverse of FormatReference for fixtures and future replay tooling; live
// request reinjection still uses the generic local-archive scanner.
func DecodeReferences(text string, expand func(uri string) ([]byte, bool)) (string, bool) {
	if text == "" || expand == nil {
		return text, false
	}
	changed := false
	out := referencePattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := referencePattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		body, ok := expand(parts[1])
		if !ok {
			return match
		}
		if want, err := strconv.Atoi(parts[2]); err == nil && want >= 0 && len(body) != want {
			return match
		}
		changed = true
		return string(body)
	})
	return out, changed
}

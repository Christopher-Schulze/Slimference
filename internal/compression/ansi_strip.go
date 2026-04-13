package compression

import (
	"regexp"
	"strings"
)

// ansiSeq matches common ANSI escape sequences and OSC sequences.
var ansiSeq = regexp.MustCompile(
	`\x1b\[[0-9;:<>?]*[ -/]*[@-~]` + // CSI
		`|\x1b\][^\x07]*(?:\x07|\x1b\\)` + // OSC
		`|\x1b[PX^_][^\x1b]*\x1b\\` + // SOS/DCS/APC terminated by ST
		`|\x1b[@-_]` + // 2-char sequences
		`|\x1b.`,
)

// StripANSICodes removes ANSI escape codes and normalizes carriage-return overwrites.
func StripANSICodes(s string) string {
	if s == "" {
		return s
	}
	s = ansiSeq.ReplaceAllString(s, "")
	// Collapse lone \r (progress-bar style overwrites) without a following \n.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}

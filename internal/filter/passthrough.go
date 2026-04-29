package filter

import (
	"fmt"
	"unicode/utf8"
)

// TruncateStdoutWithHint caps stdout to maxRunes Unicode code points; if shorter, returns stdout unchanged.
// maxRunes <= 0 disables truncation.
func TruncateStdoutWithHint(stdout []byte, maxRunes int) []byte {
	if maxRunes <= 0 {
		return stdout
	}
	s := string(stdout)
	if utf8.RuneCountInString(s) <= maxRunes {
		return stdout
	}
	runes := []rune(s)
	cut := string(runes[:maxRunes])
	hint := fmt.Sprintf("\n[output truncated to %d characters]\n", maxRunes)
	return []byte(cut + hint)
}

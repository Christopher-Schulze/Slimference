package compression

import (
	"bytes"
	"encoding/json"
)

// compactJSONContent compacts valid JSON content and returns (result, bytesSaved).
// Returns the original text and 0 if content is not valid JSON or savings are < 10%.
func compactJSONContent(text string) (string, int) {
	if len(text) == 0 {
		return text, 0
	}

	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(text)); err != nil {
		return text, 0
	}

	compacted := buf.String()
	saved := len(text) - len(compacted)

	// Require at least 10% savings to justify the change.
	if saved <= 0 || float64(saved)/float64(len(text)) < 0.10 {
		return text, 0
	}

	return compacted, saved
}

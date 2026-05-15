package filter

import (
	"encoding/json"
	"fmt"
)

// ExtractPostToolPayloadFromHookJSON extracts the command and best-known tool
// output strings from hook JSON. Both fields are best-effort because Codex hook
// payload shapes vary by tool and version.
func ExtractPostToolPayloadFromHookJSON(b []byte) (command string, toolResponse string, err error) {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return "", "", fmt.Errorf("filter: JSON: %w", err)
	}
	command, _ = findStringForKey(v, "command")
	toolResponse, _ = findPostToolResponse(v)
	return command, toolResponse, nil
}

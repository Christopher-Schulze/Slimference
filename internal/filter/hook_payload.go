package filter

import (
	"encoding/json"
	"fmt"
)

// ExtractPostToolPayloadFromHookJSON extracts the command and tool_response strings
// from hook JSON. command is best-effort and may be empty; tool_response is required.
func ExtractPostToolPayloadFromHookJSON(b []byte) (command string, toolResponse string, err error) {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return "", "", fmt.Errorf("filter: JSON: %w", err)
	}
	command, _ = findStringForKey(v, "command")
	toolResponse, _ = findStringForKey(v, "tool_response")
	if toolResponse == "" {
		return "", "", fmt.Errorf("filter: no string field \"tool_response\" in JSON")
	}
	return command, toolResponse, nil
}

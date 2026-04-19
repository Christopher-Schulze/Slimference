package filter

import (
	"encoding/json"
	"fmt"
)

type PostToolPayload struct {
	CommandLine  string
	ToolResponse string
	ToolName     string
	ToolUseID    string
	SessionID    string
}

func ExtractPostToolDetailsFromHookJSON(b []byte) (PostToolPayload, error) {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return PostToolPayload{}, fmt.Errorf("filter: JSON: %w", err)
	}
	toolResponse, _ := findStringForKey(v, "tool_response")
	if toolResponse == "" {
		return PostToolPayload{}, fmt.Errorf("filter: no string field %q in JSON", "tool_response")
	}
	command, _ := findStringForKey(v, "command")
	toolName, ok := findStringForKey(v, "tool_name")
	if !ok {
		toolName, _ = findStringForKey(v, "toolName")
	}
	toolUseID, ok := findStringForKey(v, "tool_use_id")
	if !ok {
		toolUseID, _ = findStringForKey(v, "toolUseID")
	}
	sessionID, ok := findStringForKey(v, "session_id")
	if !ok {
		sessionID, _ = findStringForKey(v, "conversation_id")
	}
	return PostToolPayload{
		CommandLine:  command,
		ToolResponse: toolResponse,
		ToolName:     toolName,
		ToolUseID:    toolUseID,
		SessionID:    sessionID,
	}, nil
}

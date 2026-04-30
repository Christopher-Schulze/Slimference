package toolprune

import (
	"encoding/json"

	"github.com/slimference/slimference/internal/types"
)

// ExtractToolNames parses the request body and returns the names of all
// tool definitions found. Provider-specific shapes are handled:
//   - Anthropic: tools[].name
//   - OpenAI: tools[].function.name
//   - CodexChatGPT: same as OpenAI when tools[] is present
//
// Returns nil (not empty) when no tools field exists or the body is
// not valid JSON.
func ExtractToolNames(body []byte, provider types.Provider) []string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}
	toolsRaw, ok := raw["tools"]
	if !ok {
		return nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(toolsRaw, &entries); err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		name := extractToolName(entry, provider)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// extractToolName picks the tool name from a single tool definition entry
// according to the provider's wire shape.
func extractToolName(entry json.RawMessage, provider types.Provider) string {
	switch provider {
	case types.Anthropic:
		var v struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(entry, &v); err == nil {
			return v.Name
		}
	default:
		// OpenAI / CodexChatGPT / unknown
		var v struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(entry, &v); err == nil {
			if v.Function.Name != "" {
				return v.Function.Name
			}
			return v.Name
		}
	}
	return ""
}

// PruneToolDefinitions removes tool definitions whose names appear in
// toPrune from the request body. Returns the modified body and a map
// of pruned tool name -> original definition (for archiving). The
// original body is returned unchanged when no tools match or on any
// parse error (fail-open).
func PruneToolDefinitions(body []byte, provider types.Provider, toPrune map[string]bool) ([]byte, map[string]json.RawMessage, error) {
	if len(toPrune) == 0 {
		return body, nil, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return body, nil, nil
	}
	toolsRaw, ok := raw["tools"]
	if !ok {
		return body, nil, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(toolsRaw, &entries); err != nil {
		return body, nil, nil
	}

	kept := make([]json.RawMessage, 0, len(entries))
	pruned := make(map[string]json.RawMessage)
	for _, entry := range entries {
		name := extractToolName(entry, provider)
		if name != "" && toPrune[name] {
			pruned[name] = entry
		} else {
			kept = append(kept, entry)
		}
	}
	if len(pruned) == 0 {
		return body, nil, nil
	}

	keptJSON, _ := json.Marshal(kept)
	raw["tools"] = keptJSON
	out, _ := json.Marshal(raw)
	return out, pruned, nil
}

package toolprune

import (
	"encoding/json"
	"strings"

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

// MentionedTools returns the subset of `candidates` whose name appears
// in `text`. Pure function used by T103b reattach to decide which
// previously-pruned tools should be restored on the next turn. The
// match is case-sensitive and word-boundary friendly: occurrences
// inside a longer identifier (e.g. "Bashful") do NOT match "Bash".
func MentionedTools(text string, candidates []string) []string {
	if text == "" || len(candidates) == 0 {
		return nil
	}
	out := make([]string, 0, len(candidates))
	for _, name := range candidates {
		if name == "" {
			continue
		}
		if containsToolName(text, name) {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// containsToolName tests whether `name` appears in `text` as a word
// (delimited by non-letter / non-digit characters or string ends).
func containsToolName(text, name string) bool {
	idx := 0
	for {
		hit := strings.Index(text[idx:], name)
		if hit == -1 {
			return false
		}
		start := idx + hit
		end := start + len(name)
		if isWordBoundary(text, start-1) && isWordBoundary(text, end) {
			return true
		}
		idx = end
	}
}

func isWordBoundary(s string, pos int) bool {
	if pos < 0 || pos >= len(s) {
		return true
	}
	c := s[pos]
	switch {
	case c >= 'a' && c <= 'z':
	case c >= 'A' && c <= 'Z':
	case c >= '0' && c <= '9':
	case c == '_':
	default:
		return true
	}
	return false
}

// ReattachToolDefinitions adds the supplied definitions back into the
// request body's `tools[]` array. Definitions whose tool name already
// exists in `tools[]` are skipped so reattaching twice in the same
// request is a no-op. Returns the modified body and the number of
// definitions that were actually re-attached. T103b.
func ReattachToolDefinitions(body []byte, provider types.Provider, defs map[string]json.RawMessage) ([]byte, int, error) {
	if len(defs) == 0 || len(body) == 0 {
		return body, 0, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return body, 0, nil
	}
	var entries []json.RawMessage
	if rawTools, ok := raw["tools"]; ok {
		if err := json.Unmarshal(rawTools, &entries); err != nil {
			entries = nil
		}
	}
	existing := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if name := extractToolName(e, provider); name != "" {
			existing[name] = struct{}{}
		}
	}
	added := 0
	for name, def := range defs {
		if _, dup := existing[name]; dup {
			continue
		}
		entries = append(entries, def)
		existing[name] = struct{}{}
		added++
	}
	if added == 0 {
		return body, 0, nil
	}
	raw["tools"], _ = json.Marshal(entries)
	out, _ := json.Marshal(raw)
	return out, added, nil
}

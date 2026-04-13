package filter

import (
	"encoding/json"
	"fmt"
)

// ExtractCommandFromHookJSON finds the first non-empty string value for key "command"
// in a JSON object (recursive). Used for Claude Code PreToolUse hook stdin.
func ExtractCommandFromHookJSON(b []byte) (string, error) {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return "", fmt.Errorf("filter: JSON: %w", err)
	}
	if s, ok := findStringForKey(v, "command"); ok {
		return s, nil
	}
	return "", fmt.Errorf("filter: no string field \"command\" in JSON")
}

func findStringForKey(v interface{}, key string) (string, bool) {
	switch t := v.(type) {
	case map[string]interface{}:
		if s, ok := t[key].(string); ok && s != "" {
			return s, true
		}
		for _, vv := range t {
			if s, ok := findStringForKey(vv, key); ok {
				return s, true
			}
		}
	case []interface{}:
		for _, e := range t {
			if s, ok := findStringForKey(e, key); ok {
				return s, true
			}
		}
	}
	return "", false
}

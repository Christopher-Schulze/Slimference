package readcache

import (
	"encoding/json"
	"fmt"
)

func ExtractRequest(raw []byte) (Request, error) {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Request{}, fmt.Errorf("readhook: JSON: %w", err)
	}

	req := Request{
		SessionID: findString(payload, "session_id"),
		TurnID:    findString(payload, "turn_id"),
		FilePath:  findString(payload, "file_path"),
		Offset:    findInt(payload, "offset"),
		Limit:     findInt(payload, "limit"),
	}
	if req.FilePath == "" {
		return Request{}, fmt.Errorf("readhook: no string field %q in JSON", "file_path")
	}
	return req, nil
}

func findString(v any, key string) string {
	switch x := v.(type) {
	case map[string]any:
		if val, ok := x[key].(string); ok && val != "" {
			return val
		}
		for _, child := range x {
			if found := findString(child, key); found != "" {
				return found
			}
		}
	case []any:
		for _, child := range x {
			if found := findString(child, key); found != "" {
				return found
			}
		}
	}
	return ""
}

func findInt(v any, key string) int {
	switch x := v.(type) {
	case map[string]any:
		if val, ok := x[key]; ok {
			if n, ok := numericValue(val); ok {
				return n
			}
		}
		for _, child := range x {
			if found, ok := nestedInt(child, key); ok {
				return found
			}
		}
	case []any:
		for _, child := range x {
			if found, ok := nestedInt(child, key); ok {
				return found
			}
		}
	}
	return 0
}

func nestedInt(v any, key string) (int, bool) {
	switch x := v.(type) {
	case map[string]any, []any:
		found := findInt(x, key)
		return found, found != 0
	default:
		return 0, false
	}
}

func numericValue(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	default:
		return 0, false
	}
}

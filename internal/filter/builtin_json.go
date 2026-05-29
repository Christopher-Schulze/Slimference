package filter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// jsonSchemaThreshold is the minimum byte size at which we attempt schema extraction.
const jsonSchemaThreshold = 1500

// TryCompactJSONMinify minifies valid JSON stdout when it strictly shrinks byte size (F14).
// For large JSON it also tries schema extraction and returns the shorter result.
func TryCompactJSONMinify(stdout []byte) ([]byte, bool) {
	if len(stdout) == 0 {
		return stdout, false
	}
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return stdout, false
	}

	// Schema extraction for large JSON: produces structural overview.
	if len(trimmed) >= jsonSchemaThreshold && !jsonHasDiagnosticKeys(trimmed) {
		if schema, ok := extractJSONSchema(trimmed); ok && len(schema) < len(trimmed) {
			return schema, true
		}
	}

	// Fallback: compact whitespace only.
	var buf bytes.Buffer
	buf.Grow(len(trimmed))
	_ = json.Compact(&buf, trimmed)
	compact := buf.Bytes()
	if len(compact) >= len(stdout) {
		return stdout, false
	}
	return compact, true
}

func jsonHasDiagnosticKeys(data []byte) bool {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return false
	}
	return valueHasDiagnosticKeys(v, 0)
}

func valueHasDiagnosticKeys(v interface{}, depth int) bool {
	if depth > 8 {
		return false
	}
	switch val := v.(type) {
	case map[string]interface{}:
		for k, child := range val {
			if isDiagnosticJSONKey(k) {
				return true
			}
			if valueHasDiagnosticKeys(child, depth+1) {
				return true
			}
		}
	case []interface{}:
		for _, child := range val {
			if valueHasDiagnosticKeys(child, depth+1) {
				return true
			}
		}
	}
	return false
}

func isDiagnosticJSONKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "error", "errors", "message", "messages", "stderr", "stdout", "stack", "trace", "exception", "reason", "details", "diagnostic", "diagnostics":
		return true
	default:
		return false
	}
}

// extractJSONSchema produces a compact structural representation of a JSON value.
// Objects show key→type pairs; arrays show element type and length.
func extractJSONSchema(data []byte) ([]byte, bool) {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, false
	}
	var sb strings.Builder
	schemaOf(&sb, v, 0, 3)
	return []byte(sb.String()), true
}

// schemaOf writes a compact schema description for v at the given indent depth.
// maxDepth limits recursion to keep output compact.
func schemaOf(sb *strings.Builder, v interface{}, depth, maxDepth int) {
	indent := strings.Repeat("  ", depth)
	switch val := v.(type) {
	case map[string]interface{}:
		if depth == 0 {
			sb.WriteString(fmt.Sprintf("{object, %d keys}\n", len(val)))
		}
		// Sort keys for deterministic output.
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		limit := 40 // max keys shown per object
		for i, k := range keys {
			if i >= limit {
				sb.WriteString(fmt.Sprintf("%s  [+%d more keys]\n", indent, len(keys)-limit))
				break
			}
			child := val[k]
			childType := jsonTypeName(child)
			sb.WriteString(fmt.Sprintf("%s  %q: %s", indent, k, childType))
			if depth < maxDepth {
				switch cv := child.(type) {
				case map[string]interface{}:
					sb.WriteString(fmt.Sprintf(" {%d keys}", len(cv)))
					if len(cv) > 0 && depth+1 < maxDepth {
						sb.WriteByte('\n')
						schemaOf(sb, cv, depth+1, maxDepth)
						continue
					}
				case []interface{}:
					sb.WriteString(fmt.Sprintf(" [%d]", len(cv)))
					if len(cv) > 0 {
						sb.WriteString(fmt.Sprintf(" elem:%s", jsonTypeName(cv[0])))
					}
				case string:
					if len(cv) > 80 {
						sb.WriteString(fmt.Sprintf(" %q...", cv[:40]))
					} else if len(cv) > 0 {
						sb.WriteString(fmt.Sprintf(" %q", cv))
					}
				case float64:
					sb.WriteString(fmt.Sprintf(" %v", cv))
				case bool:
					sb.WriteString(fmt.Sprintf(" %v", cv))
				}
			}
			sb.WriteByte('\n')
		}
	case []interface{}:
		if depth == 0 {
			elemType := "?"
			if len(val) > 0 {
				elemType = jsonTypeName(val[0])
			}
			sb.WriteString(fmt.Sprintf("[array, %d items, elem:%s]\n", len(val), elemType))
			if len(val) > 0 {
				if obj, ok := val[0].(map[string]interface{}); ok {
					schemaOf(sb, obj, depth, maxDepth)
				}
			}
		}
	case string:
		if depth == 0 {
			sb.WriteString(fmt.Sprintf("[string, len=%d]\n", len(val)))
		}
	case float64:
		if depth == 0 {
			sb.WriteString(fmt.Sprintf("[number: %v]\n", val))
		}
	case bool:
		if depth == 0 {
			sb.WriteString(fmt.Sprintf("[bool: %v]\n", val))
		}
	case nil:
		if depth == 0 {
			sb.WriteString("[null]\n")
		}
	}
}

// jsonTypeName returns a short type label for a JSON-decoded value.
func jsonTypeName(v interface{}) string {
	switch v.(type) {
	case map[string]interface{}:
		return "object"
	case []interface{}:
		return "array"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "bool"
	case nil:
		return "null"
	}
	return "unknown"
}

package filter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
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

func isJQArgv(argv []string) bool {
	return len(argv) > 0 && strings.EqualFold(filepath.Base(argv[0]), "jq")
}

// TryCompactJQJSONExact exact-minifies jq JSON output and otherwise full-passes
// it so the jq TOML fallback cannot truncate inspected JSON payloads.
func TryCompactJQJSONExact(argv []string, stdout []byte) ([]byte, bool) {
	if !isJQArgv(argv) {
		return stdout, false
	}
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return stdout, true
	}
	var buf bytes.Buffer
	buf.Grow(len(trimmed))
	if err := json.Compact(&buf, trimmed); err != nil {
		return stdout, true
	}
	compact := buf.Bytes()
	if len(compact) < len(stdout) {
		return compact, true
	}
	return stdout, true
}

func jsonHasDiagnosticKeys(data []byte) bool {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return false
	}
	return valueHasDiagnosticKeys(v, 0)
}

func valueHasDiagnosticKeys(v any, depth int) bool {
	if depth > 8 {
		return false
	}
	switch val := v.(type) {
	case map[string]any:
		for k, child := range val {
			if isDiagnosticJSONKey(k) {
				return true
			}
			if valueHasDiagnosticKeys(child, depth+1) {
				return true
			}
		}
	case []any:
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
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, false
	}
	var sb strings.Builder
	schemaOf(&sb, v, 0, 3)
	return []byte(sb.String()), true
}

// schemaOf writes a compact schema description for v at the given indent depth.
// maxDepth limits recursion to keep output compact.
func schemaOf(sb *strings.Builder, v any, depth, maxDepth int) {
	indent := strings.Repeat("  ", depth)
	switch val := v.(type) {
	case map[string]any:
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
				case map[string]any:
					sb.WriteString(fmt.Sprintf(" {%d keys}", len(cv)))
					if len(cv) > 0 && depth+1 < maxDepth {
						sb.WriteByte('\n')
						schemaOf(sb, cv, depth+1, maxDepth)
						continue
					}
				case []any:
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
	case []any:
		if depth == 0 {
			elemType := "?"
			if len(val) > 0 {
				elemType = jsonTypeName(val[0])
			}
			sb.WriteString(fmt.Sprintf("[array, %d items, elem:%s]\n", len(val), elemType))
			if len(val) > 0 {
				if obj, ok := val[0].(map[string]any); ok {
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
func jsonTypeName(v any) string {
	switch v.(type) {
	case map[string]any:
		return "object"
	case []any:
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

// TryCompactGoListJSON compacts `go list -json` NDJSON output by minifying
// each JSON object on each line. go list -json produces multiple JSON objects
// separated by newlines (NDJSON), not a JSON array, so TryCompactJSONMinify
// (which expects a single valid JSON document) cannot handle it.
//
// This function minifies each JSON object independently and concatenates the
// results. For large outputs with many modules, this can produce significant
// savings by removing pretty-printing whitespace.
//
// Drawdown vector: the model loses whitespace formatting only. All data
// (module path, imports, dependencies, etc.) is preserved. Fail-open on
// malformed JSON lines.
func TryCompactGoListJSON(argv []string, stdout []byte) ([]byte, bool) {
	if !isGoListJSONArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return stdout, false
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 2 {
		// Single object — try TryCompactJSONMinify instead
		return TryCompactJSONMinify(stdout)
	}

	// go list -json outputs one JSON object per module, but each object
	// spans multiple lines. We need to find object boundaries by tracking
	// brace depth.
	var result bytes.Buffer
	result.Grow(len(stdout))
	changed := false
	var currentObj bytes.Buffer
	depth := 0
	inString := false
	escaped := false

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if currentObj.Len() > 0 {
			currentObj.WriteByte('\n')
		}
		currentObj.WriteString(line)

		for _, r := range line {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inString = !inString
				continue
			}
			if inString {
				continue
			}
			if r == '{' {
				depth++
			} else if r == '}' {
				depth--
				if depth == 0 {
					// Complete object — minify it
					obj := currentObj.Bytes()
					var compacted bytes.Buffer
					compacted.Grow(len(obj))
					if err := json.Compact(&compacted, bytes.TrimSpace(obj)); err == nil {
						if compacted.Len() < currentObj.Len() {
							result.Write(compacted.Bytes())
							result.WriteByte('\n')
							changed = true
						} else {
							result.Write(obj)
							result.WriteByte('\n')
						}
					} else {
						// Fail-open: write original
						result.Write(obj)
						result.WriteByte('\n')
					}
					currentObj.Reset()
				}
			}
		}
	}

	// Write any remaining content
	if currentObj.Len() > 0 {
		result.Write(currentObj.Bytes())
		result.WriteByte('\n')
	}

	if !changed {
		return stdout, false
	}

	out := bytes.TrimRight(result.Bytes(), "\n")
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return out, true
}

func isGoListJSONArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	b = strings.TrimSuffix(b, ".exe")
	if b != "go" {
		return false
	}
	hasList := false
	hasJSON := false
	for _, arg := range argv[1:] {
		if arg == "list" {
			hasList = true
		}
		if arg == "-json" || strings.HasPrefix(arg, "-json=") {
			hasJSON = true
		}
	}
	return hasList && hasJSON
}

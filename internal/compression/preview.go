package compression

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Structure-aware preview (T38, inspired by token-optimizer's
// archive_result.py _compress_mcp_preview).
//
// Large tool_result blocks frequently fall into one of four shapes:
// JSON object/array, newline-separated file paths, ASCII-bordered tables,
// or free text. The raw-text pipeline forwards everything verbatim, which
// is wasteful when the model only needs the shape plus a few representative
// entries.
//
// StructurePreview detects the shape cheaply (first 100k chars) and returns
// a compact preview only when it is strictly shorter than the input. It is
// intentionally orthogonal to the existing delta/structure-extract paths:
// delta needs a prior version, structure-extract needs a recognised
// programming language. Preview covers everything else.

// PreviewThresholdBytes is the minimum input length before preview is
// attempted. Below this, the original is already cheap.
const PreviewThresholdBytes = 4096

// PreviewMaxOutputBytes caps the preview so it cannot itself become
// oversized.
const PreviewMaxOutputBytes = 1500

// outputShape classifies the detected structure of the input.
type outputShape int

const (
	shapeUnknown outputShape = iota
	shapeJSON
	shapePaths
	shapeTable
)

// StructurePreview returns a shorter structured summary of raw tool output.
// Returns (preview, true) only when a shorter, same-shape summary could be
// emitted. Otherwise returns (raw, false).
func StructurePreview(raw string) (string, bool) {
	if len(raw) < PreviewThresholdBytes {
		return raw, false
	}
	switch detectOutputShape(raw) {
	case shapeJSON:
		if p, ok := previewJSON(raw); ok {
			return p, true
		}
	case shapePaths:
		if p, ok := previewPaths(raw); ok {
			return p, true
		}
	case shapeTable:
		if p, ok := previewTable(raw); ok {
			return p, true
		}
	}
	return raw, false
}

// detectOutputShape samples the input to decide which preview strategy to
// apply. Intentionally cheap: only the first lines are examined.
func detectOutputShape(raw string) outputShape {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return shapeUnknown
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		sample := trimmed
		if len(sample) > 100_000 {
			sample = sample[:100_000]
		}
		var probe any
		if err := json.Unmarshal([]byte(sample), &probe); err == nil {
			return shapeJSON
		}
	}
	lines := strings.SplitN(trimmed, "\n", 51)
	if len(lines) > 50 {
		lines = lines[:50]
	}
	if len(lines) >= 5 {
		pathLike := 0
		for _, ln := range lines {
			if strings.Contains(ln, "/") || strings.Contains(ln, `\`) {
				pathLike++
			}
		}
		if float64(pathLike) >= 0.6*float64(len(lines)) {
			return shapePaths
		}
		sep := 0
		for _, ln := range lines {
			t := strings.TrimSpace(ln)
			if t == "" {
				continue
			}
			onlySep := true
			for _, r := range t {
				switch r {
				case '-', '=', '|', '+', ' ':
				default:
					onlySep = false
				}
				if !onlySep {
					break
				}
			}
			if onlySep {
				sep++
			}
		}
		if sep >= 1 {
			return shapeTable
		}
	}
	return shapeUnknown
}

// previewJSON renders "JSON object (N keys)" / "JSON array (N items)"
// with the first few keys / sample items.
func previewJSON(raw string) (string, bool) {
	sample := raw
	if len(sample) > 500_000 {
		sample = sample[:500_000]
	}
	var data any
	if err := json.Unmarshal([]byte(sample), &data); err != nil {
		return "", false
	}
	var parts []string
	switch v := data.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts = append(parts, fmt.Sprintf("JSON object (%d keys):", len(v)))
		limit := min(len(keys), 15)
		for _, k := range keys[:limit] {
			parts = append(parts, formatJSONKey(k, v[k]))
		}
		if len(keys) > limit {
			parts = append(parts, fmt.Sprintf("  ... (%d more keys)", len(keys)-limit))
		}
	case []any:
		parts = append(parts, fmt.Sprintf("JSON array (%d items):", len(v)))
		limit := min(len(v), 5)
		for _, item := range v[:limit] {
			parts = append(parts, "  "+sketchJSONItem(item))
		}
		if len(v) > limit {
			parts = append(parts, fmt.Sprintf("  ... (%d more items)", len(v)-limit))
		}
	default:
		return "", false
	}
	out := strings.Join(parts, "\n")
	if len(out) > PreviewMaxOutputBytes {
		out = out[:PreviewMaxOutputBytes]
	}
	if len(out) >= len(raw) {
		return "", false
	}
	return out, true
}

// formatJSONKey renders one key/value pair for the preview.
func formatJSONKey(k string, v any) string {
	switch vt := v.(type) {
	case []any:
		return fmt.Sprintf("  %s: [%d items]", k, len(vt))
	case map[string]any:
		subkeys := make([]string, 0, len(vt))
		for sk := range vt {
			subkeys = append(subkeys, sk)
		}
		sort.Strings(subkeys)
		if len(subkeys) > 5 {
			subkeys = subkeys[:5]
			return fmt.Sprintf("  %s: {%s, ...}", k, strings.Join(subkeys, ", "))
		}
		return fmt.Sprintf("  %s: {%s}", k, strings.Join(subkeys, ", "))
	case string:
		if len(vt) > 80 {
			return fmt.Sprintf("  %s: %q...", k, vt[:77])
		}
		return fmt.Sprintf("  %s: %q", k, vt)
	default:
		raw, _ := json.Marshal(v)
		s := string(raw)
		if len(s) > 80 {
			s = s[:80]
		}
		return fmt.Sprintf("  %s: %s", k, s)
	}
}

// sketchJSONItem renders one array element.
func sketchJSONItem(v any) string {
	switch vt := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(vt))
		for k := range vt {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) > 5 {
			keys = keys[:5]
			return fmt.Sprintf("{%s, ...}", strings.Join(keys, ", "))
		}
		return fmt.Sprintf("{%s}", strings.Join(keys, ", "))
	default:
		raw, _ := json.Marshal(v)
		s := string(raw)
		if len(s) > 80 {
			s = s[:80]
		}
		return s
	}
}

// previewPaths groups path-shaped lines by directory.
func previewPaths(raw string) (string, bool) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	dirs := map[string]int{}
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if !strings.Contains(t, "/") && !strings.Contains(t, `\`) {
			continue
		}
		sep := "/"
		if strings.Contains(t, "\\") && !strings.Contains(t, "/") {
			sep = "\\"
		}
		idx := strings.LastIndex(t, sep)
		dir := "."
		if idx >= 0 {
			dir = t[:idx]
		}
		dirs[dir]++
	}
	if len(dirs) == 0 {
		return "", false
	}
	type kv struct {
		dir   string
		count int
	}
	sorted := make([]kv, 0, len(dirs))
	for d, c := range dirs {
		sorted = append(sorted, kv{d, c})
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].count != sorted[j].count {
			return sorted[i].count > sorted[j].count
		}
		return sorted[i].dir < sorted[j].dir
	})
	parts := []string{fmt.Sprintf("%d paths across %d directories:", len(lines), len(dirs))}
	limit := min(len(sorted), 10)
	for _, e := range sorted[:limit] {
		parts = append(parts, fmt.Sprintf("  %s/ (%d files)", e.dir, e.count))
	}
	if len(sorted) > limit {
		parts = append(parts, fmt.Sprintf("  ... (%d more directories)", len(sorted)-limit))
	}
	out := strings.Join(parts, "\n")
	if len(out) > PreviewMaxOutputBytes {
		out = out[:PreviewMaxOutputBytes]
	}
	if len(out) >= len(raw) {
		return "", false
	}
	return out, true
}

// previewTable keeps the header + separator row and the first 10 data rows.
func previewTable(raw string) (string, bool) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) < 2 {
		return "", false
	}
	header := lines[:2]
	data := make([]string, 0, len(lines))
	for _, ln := range lines[2:] {
		if strings.TrimSpace(ln) != "" {
			data = append(data, ln)
		}
	}
	tail := data
	truncated := 0
	if len(tail) > 10 {
		tail = data[:10]
		truncated = len(data) - 10
	}
	result := append([]string{}, header...)
	result = append(result, tail...)
	if truncated > 0 {
		result = append(result, fmt.Sprintf("... (%d more rows, %d total)", truncated, len(data)))
	}
	out := strings.Join(result, "\n")
	if len(out) > PreviewMaxOutputBytes {
		out = out[:PreviewMaxOutputBytes]
	}
	if len(out) >= len(raw) {
		return "", false
	}
	return out, true
}

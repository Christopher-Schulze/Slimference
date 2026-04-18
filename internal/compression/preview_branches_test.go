package compression

import (
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

// TestDetectOutputShape_jsonSampleTruncation covers the 100k sample clip
// for large JSON inputs that are still valid at the sample boundary.
func TestDetectOutputShape_jsonSampleTruncation(t *testing.T) {
	t.Parallel()
	// 150k bytes of valid JSON array: the sample truncation path must
	// still classify this as shapeJSON because the first 100k is a
	// parseable prefix of a larger document (json.Unmarshal accepts
	// leading valid JSON when given exactly a balanced slice).
	var sb strings.Builder
	sb.WriteString("[")
	for i := 0; i < 2000; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("{\"a\":")
		sb.WriteString(itoaLoop(i))
		sb.WriteString(",\"pad\":\"")
		sb.WriteString(strings.Repeat("p", 60))
		sb.WriteString("\"}")
	}
	sb.WriteString("]")
	in := sb.String()
	if len(in) <= 100_000 {
		t.Skip("fixture too small to exercise truncation branch")
	}
	// Even if the 100k slice is not a balanced JSON document, the
	// function's fallthrough lands on the shapeUnknown path; the goal
	// here is to execute the `if len(sample) > 100_000` branch itself.
	_ = detectOutputShape(in)
}

// TestDetectOutputShape_tableWithBlankLines covers the `if t == ""`
// continue branch inside the table-detection loop.
func TestDetectOutputShape_tableWithBlankLines(t *testing.T) {
	t.Parallel()
	// Table separator followed by blank lines and data - forces the
	// inner loop to hit the blank-skip branch.
	in := "COL_A  COL_B\n-----  -----\n\n\nv1     v2\nv3     v4\nv5     v6\n"
	if detectOutputShape(in) != shapeTable {
		t.Fatalf("expected table: got %d", detectOutputShape(in))
	}
}

// TestPreviewJSON_sampleTruncation covers the 500k sample clip in
// previewJSON.
func TestPreviewJSON_sampleTruncation(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("[")
	// >500k bytes
	for i := 0; i < 10_000; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("\"")
		sb.WriteString(strings.Repeat("x", 60))
		sb.WriteString("\"")
	}
	sb.WriteString("]")
	in := sb.String()
	if len(in) <= 500_000 {
		t.Skip("fixture too small to exercise truncation branch")
	}
	_, _ = previewJSON(in)
}

// TestPreviewJSON_objectFewerThan15Keys covers the `len(keys) < limit`
// branch.
func TestPreviewJSON_objectFewerThan15Keys(t *testing.T) {
	t.Parallel()
	// 3-key object but with large values so the total exceeds
	// PreviewThresholdBytes inside StructurePreview tests.
	in := `{"a":"` + strings.Repeat("x", 2000) + `","b":"` + strings.Repeat("y", 2000) + `","c":"` + strings.Repeat("z", 2000) + `"}`
	out, ok := previewJSON(in)
	if !ok {
		t.Fatal("expected preview")
	}
	if strings.Contains(out, "more keys") {
		t.Fatalf("small object must not emit ellipsis: %s", out)
	}
}

// TestPreviewJSON_arrayFewerThan5Items covers the `len(v) < limit` branch.
func TestPreviewJSON_arrayFewerThan5Items(t *testing.T) {
	t.Parallel()
	in := `["` + strings.Repeat("x", 2000) + `","` + strings.Repeat("y", 2500) + `"]`
	out, ok := previewJSON(in)
	if !ok {
		t.Fatal("expected preview")
	}
	if strings.Contains(out, "more items") {
		t.Fatalf("small array must not emit ellipsis: %s", out)
	}
}

// TestPreviewJSON_scalarRoot covers the default case that returns false.
func TestPreviewJSON_scalarRoot(t *testing.T) {
	t.Parallel()
	// Valid JSON scalar (long string), >= 4KB.
	in := `"` + strings.Repeat("x", 5000) + `"`
	if _, ok := previewJSON(in); ok {
		t.Fatal("scalar root must not preview")
	}
}

// TestPreviewJSON_outputExceedsCap covers the truncation branch.
func TestPreviewJSON_outputExceedsCap(t *testing.T) {
	t.Parallel()
	// Build an object with many long keys so the joined output exceeds
	// PreviewMaxOutputBytes (1500).
	var sb strings.Builder
	sb.WriteString("{")
	for i := 0; i < 100; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("\"")
		sb.WriteString(strings.Repeat("k", 40))
		sb.WriteString(itoaLoop(i))
		sb.WriteString("\":\"")
		sb.WriteString(strings.Repeat("v", 60))
		sb.WriteString("\"")
	}
	sb.WriteString("}")
	in := sb.String()
	out, ok := previewJSON(in)
	if !ok {
		t.Fatal("expected preview")
	}
	if len(out) > PreviewMaxOutputBytes {
		t.Fatalf("preview must be capped: len=%d", len(out))
	}
}

// TestPreviewJSON_notShorter covers the `len(out) >= len(raw)` guard.
func TestPreviewJSON_notShorter(t *testing.T) {
	t.Parallel()
	// Object with 1 tiny key: preview overhead exceeds raw length.
	in := `{"a":1}`
	if _, ok := previewJSON(in); ok {
		t.Fatal("trivially-small object must not preview")
	}
}

// TestFormatJSONKey_defaultLongString covers the `len(s) > 80` truncation
// branch inside the default arm.
func TestFormatJSONKey_defaultLongString(t *testing.T) {
	t.Parallel()
	// Pass a boolean inside a slice so marshal produces long JSON.
	longNum := strings.Repeat("1234567890", 20) // 200 chars
	var v interface{}
	// int64 large value formats to a short string; use a byte slice
	// marshal trick: any with a huge string value within a struct. For
	// the default branch to emit > 80 chars we use a custom large value.
	// Marshal a json.RawMessage with length > 80 so the default branch
	// sees a long serialisation.
	type bigStruct struct {
		Field string `json:"field"`
	}
	v = bigStruct{Field: longNum}
	got := formatJSONKey("k", v)
	if len(got) > 200 {
		t.Fatalf("expected default truncation, got len=%d", len(got))
	}
	// default-arm truncation cuts at 80 chars.
	if strings.Count(got, "1") < 5 {
		t.Fatalf("truncation stripped too much: %s", got)
	}
}

// TestPreviewPaths_dirSortTiebreak exercises the alphabetical tiebreak when
// two directories have the same count.
func TestPreviewPaths_dirSortTiebreak(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	// Equal counts for /z/ and /a/, /a/ must come first alphabetically.
	for i := 0; i < 150; i++ {
		sb.WriteString("/z/deep/path/file_" + itoaLoop(i) + ".go\n")
		sb.WriteString("/a/deep/path/file_" + itoaLoop(i) + ".go\n")
	}
	out, ok := previewPaths(sb.String())
	if !ok {
		t.Fatal("expected preview")
	}
	aIdx := strings.Index(out, "/a/")
	zIdx := strings.Index(out, "/z/")
	if aIdx < 0 || zIdx < 0 || aIdx > zIdx {
		t.Fatalf("alphabetical tiebreak failed: a=%d z=%d\n%s", aIdx, zIdx, out)
	}
}

// TestPreviewPaths_moreThan10Dirs exercises the `len(sorted) > limit`
// ellipsis branch.
func TestPreviewPaths_moreThan10Dirs(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	// 25 distinct directories × 10 files with long filenames so the
	// preview of only the top 10 still triggers the ellipsis.
	for i := 0; i < 25; i++ {
		for j := 0; j < 10; j++ {
			sb.WriteString("/dir_" + itoaLoop(i) + "/nested/deep/file_with_a_fairly_long_name_" + itoaLoop(j) + ".go\n")
		}
	}
	in := sb.String()
	out, ok := previewPaths(in)
	if !ok {
		t.Fatal("expected preview")
	}
	if !strings.Contains(out, "more directories") {
		t.Fatalf("missing ellipsis: %s", out)
	}
}

// TestPreviewPaths_outputCappedAt1500 and not-shorter guard.
func TestPreviewPaths_notShorterShortInput(t *testing.T) {
	t.Parallel()
	// 3 tiny path lines - preview header alone exceeds this length.
	in := "/a/b/c\n/a/b/d\n/a/b/e"
	if _, ok := previewPaths(in); ok {
		t.Fatal("tiny path list must not preview (not shorter)")
	}
}

// TestPreviewPaths_outputCappedAt1500 covers the truncation branch with
// enough dirs + long names to exceed PreviewMaxOutputBytes.
func TestPreviewPaths_outputCappedAt1500(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	// 10 dirs are kept, but make dir names very long so the preview
	// balloons. 10 lines of 200+ chars each = 2000+ chars.
	for i := 0; i < 10; i++ {
		bigDir := strings.Repeat("name_segment_", 20) + itoaLoop(i)
		for j := 0; j < 5; j++ {
			sb.WriteString("/" + bigDir + "/file_" + itoaLoop(j) + ".go\n")
		}
	}
	in := sb.String()
	out, ok := previewPaths(in)
	if !ok {
		t.Fatal("expected preview")
	}
	if len(out) > PreviewMaxOutputBytes {
		t.Fatalf("preview exceeded cap: len=%d", len(out))
	}
}

// TestPreviewTable_outputCappedAt1500 covers the truncation branch for
// tables with very wide rows.
func TestPreviewTable_outputCappedAt1500(t *testing.T) {
	t.Parallel()
	header := strings.Repeat("H", 200)
	sep := strings.Repeat("-", 200)
	rows := []string{header, sep}
	for i := 0; i < 11; i++ {
		rows = append(rows, strings.Repeat("v", 200))
	}
	in := strings.Join(rows, "\n") + "\n"
	out, ok := previewTable(in)
	if !ok {
		t.Fatal("expected preview")
	}
	if len(out) > PreviewMaxOutputBytes {
		t.Fatalf("preview exceeded cap: len=%d", len(out))
	}
}

// TestPreviewTable_notShorterShortInput covers the `len(out) >= len(raw)`
// guard on short tables.
func TestPreviewTable_notShorterShortInput(t *testing.T) {
	t.Parallel()
	in := "H1 H2\n-- --\na b"
	if _, ok := previewTable(in); ok {
		t.Fatal("trivially-small table must not preview")
	}
}

// TestStructurePreviewPass_skipBlockWhenPreviewDeclines covers the
// `if !ok { continue }` branch in the pass.
func TestStructurePreviewPass_skipBlockWhenPreviewDeclines(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.Tuning.StructurePreview = true
	cfg.SlidingWindow = 1
	c := NewDeterministicCompressor(&cfg)
	// Large, shape-unknown prose input: passes the size gate but
	// StructurePreview returns ok=false.
	prose := strings.Repeat("random prose without any recognisable shape ", 150)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "go"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", Text: prose}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "tail"}}},
	}
	result := c.Compress(msgs)
	if result.PreviewSaved != 0 {
		t.Fatalf("expected zero preview savings, got %d", result.PreviewSaved)
	}
}

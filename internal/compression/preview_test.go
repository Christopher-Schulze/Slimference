package compression

import (
	"strconv"
	"strings"
	"testing"
)

func TestStructurePreview_tooSmall(t *testing.T) {
	t.Parallel()
	if _, ok := StructurePreview("short input"); ok {
		t.Fatal("small input must not be previewed")
	}
}

func TestStructurePreview_json(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("{")
	for i := 0; i < 30; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`"k`)
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString(`":"`)
		sb.WriteString(strings.Repeat("v", 200))
		sb.WriteString(`"`)
	}
	sb.WriteString("}")
	in := sb.String()
	out, ok := StructurePreview(in)
	if !ok {
		t.Fatal("expected preview")
	}
	if !strings.Contains(out, "JSON object") {
		t.Fatalf("missing JSON shape: %s", out)
	}
	if len(out) >= len(in) {
		t.Fatalf("output not shorter: in=%d out=%d", len(in), len(out))
	}
}

func TestStructurePreview_jsonArray(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("[")
	for i := 0; i < 50; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"id":`)
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString(`,"data":"`)
		sb.WriteString(strings.Repeat("x", 100))
		sb.WriteString(`"}`)
	}
	sb.WriteString("]")
	in := sb.String()
	out, ok := StructurePreview(in)
	if !ok {
		t.Fatal("expected preview")
	}
	if !strings.Contains(out, "JSON array") {
		t.Fatalf("missing array shape: %s", out)
	}
}

func TestStructurePreview_paths(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	dirs := []string{"/a/b", "/a/b", "/a/b", "/c/d", "/c/d", "/e/f"}
	// Need >= PreviewThresholdBytes raw input to exercise preview at all.
	for i := 0; i < 400; i++ {
		sb.WriteString(dirs[i%len(dirs)])
		sb.WriteString("/nested/deep/file_with_a_fairly_long_name_")
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString(".go\n")
	}
	in := sb.String()
	out, ok := StructurePreview(in)
	if !ok {
		t.Fatal("expected preview")
	}
	if !strings.Contains(out, "directories") {
		t.Fatalf("missing directory preview: %s", out)
	}
}

func TestStructurePreview_table(t *testing.T) {
	t.Parallel()
	rows := []string{
		"COLUMN_ONE  COLUMN_TWO  COLUMN_THREE  COLUMN_FOUR",
		"----------  ----------  ------------  ----------",
	}
	// Need >= PreviewThresholdBytes raw input.
	for i := 0; i < 250; i++ {
		rows = append(rows, "value_one   value_two   value_three   value_four")
	}
	in := strings.Join(rows, "\n") + "\n"
	out, ok := StructurePreview(in)
	if !ok {
		t.Fatal("expected preview")
	}
	if !strings.Contains(out, "more rows") {
		t.Fatalf("missing truncation hint: %s", out)
	}
}

func TestStructurePreview_unknownShape(t *testing.T) {
	t.Parallel()
	in := strings.Repeat("random prose without structure ", 200)
	if _, ok := StructurePreview(in); ok {
		t.Fatal("prose must not be previewed")
	}
}

func TestStructurePreview_invalidJSONFallsThrough(t *testing.T) {
	t.Parallel()
	in := "{ this is not json at all " + strings.Repeat("x", 5000)
	if _, ok := StructurePreview(in); ok {
		t.Fatal("invalid JSON must not be previewed as JSON")
	}
}

func TestDetectOutputShape(t *testing.T) {
	t.Parallel()
	if detectOutputShape("") != shapeUnknown {
		t.Fatal("empty must be unknown")
	}
	if detectOutputShape(`{"a":1}`) != shapeJSON {
		t.Fatal("json object")
	}
	if detectOutputShape(`[1,2,3]`) != shapeJSON {
		t.Fatal("json array")
	}
	if detectOutputShape("a/b\nc/d\ne/f\ng/h\ni/j\n") != shapePaths {
		t.Fatal("paths")
	}
	if detectOutputShape("h1 h2 h3\n--- --- ---\na b c\nd e f\ng h i\n") != shapeTable {
		t.Fatal("table")
	}
}

func TestPreviewJSON_nonContainerRoot(t *testing.T) {
	t.Parallel()
	in := strings.Repeat(`"hello"`, 500)
	if _, ok := previewJSON(in); ok {
		t.Fatal("non-container root must not preview")
	}
}

func TestPreviewTable_tooShort(t *testing.T) {
	t.Parallel()
	if _, ok := previewTable("only one line"); ok {
		t.Fatal("single line must not be table")
	}
}

func TestPreviewPaths_noPathLines(t *testing.T) {
	t.Parallel()
	if _, ok := previewPaths("no\npaths\nhere"); ok {
		t.Fatal("no paths must return false")
	}
}

func TestFormatJSONKey_longString(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 200)
	out := formatJSONKey("big", long)
	if !strings.Contains(out, "...") {
		t.Fatalf("long string must be truncated: %s", out)
	}
}

func TestFormatJSONKey_nestedObject(t *testing.T) {
	t.Parallel()
	out := formatJSONKey("obj", map[string]interface{}{"a": 1, "b": 2, "c": 3, "d": 4, "e": 5, "f": 6})
	if !strings.Contains(out, "...") {
		t.Fatalf("expected more-than-5 ellipsis: %s", out)
	}
}

func TestFormatJSONKey_shortNested(t *testing.T) {
	t.Parallel()
	out := formatJSONKey("obj", map[string]interface{}{"a": 1})
	if strings.Contains(out, "...") {
		t.Fatalf("short nested must not have ellipsis: %s", out)
	}
}

func TestSketchJSONItem(t *testing.T) {
	t.Parallel()
	s := sketchJSONItem(map[string]interface{}{"a": 1, "b": 2})
	if !strings.HasPrefix(s, "{") {
		t.Fatalf("obj sketch: %s", s)
	}
	s = sketchJSONItem(map[string]interface{}{"a": 1, "b": 2, "c": 3, "d": 4, "e": 5, "f": 6})
	if !strings.Contains(s, "...") {
		t.Fatalf("long obj sketch must have ...: %s", s)
	}
	s = sketchJSONItem([]interface{}{1, 2, 3})
	if len(s) == 0 {
		t.Fatal("array sketch empty")
	}
	s = sketchJSONItem(strings.Repeat("y", 200))
	if len(s) > 81 {
		t.Fatalf("long scalar not truncated: %d", len(s))
	}
}

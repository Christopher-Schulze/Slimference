package filter

import "testing"

func TestExtractPostToolDetailsFromHookJSON_Metadata(t *testing.T) {
	t.Parallel()

	got, err := ExtractPostToolDetailsFromHookJSON([]byte(`{
		"session_id":"sess-1",
		"tool_name":"Bash",
		"tool_use_id":"tool-1",
		"tool_input":{"command":"npm test"},
		"tool_response":"ok"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != "sess-1" || got.ToolName != "Bash" || got.ToolUseID != "tool-1" || got.CommandLine != "npm test" || !got.HasToolResponse {
		t.Fatalf("got=%+v", got)
	}
}

func TestExtractPostToolDetailsFromHookJSON_FilePaths(t *testing.T) {
	t.Parallel()

	got, err := ExtractPostToolDetailsFromHookJSON([]byte(`{
		"session_id":"sess-1",
		"tool_name":"Edit",
		"cwd":"/repo",
		"tool_input":{"path":"src/main.go","nested":{"file_path":"src/lib.go"}},
		"command":"apply_patch <<'PATCH'\n*** Begin Patch\n*** Update File: src/main.go\n*** Add File: src/new.go\n*** End Patch\nPATCH",
		"tool_response":"ok"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.CWD != "/repo" {
		t.Fatalf("cwd=%q", got.CWD)
	}
	want := []string{"src/lib.go", "src/main.go", "src/new.go"}
	if len(got.FilePaths) != len(want) {
		t.Fatalf("paths=%v", got.FilePaths)
	}
	for i := range want {
		if got.FilePaths[i] != want[i] {
			t.Fatalf("paths=%v want=%v", got.FilePaths, want)
		}
	}
}

func TestCollectPostToolFilePaths_Edges(t *testing.T) {
	t.Parallel()
	v := map[string]any{
		"path": "bad\npath",
		"items": []any{
			map[string]any{"filePath": "."},
			map[string]any{"filepath": "z.go"},
			map[string]any{"file_path": "a.go"},
		},
	}
	got := collectPostToolFilePaths(v, "*** Delete File: m.go")
	want := []string{"a.go", "m.go", "z.go"}
	if len(got) != len(want) {
		t.Fatalf("paths=%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("paths=%v want=%v", got, want)
		}
	}
}

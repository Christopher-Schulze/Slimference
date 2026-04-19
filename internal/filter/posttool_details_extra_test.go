package filter

import "testing"

func TestExtractPostToolDetailsFromHookJSON_Fallbacks(t *testing.T) {
	t.Parallel()

	got, err := ExtractPostToolDetailsFromHookJSON([]byte(`{
		"session_id":"sess-2",
		"tool_input":{"input":{"command":"go test ./..."}, "file_path":"ignored.txt"},
		"tool_response":"ok"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.CommandLine != "go test ./..." || got.SessionID != "sess-2" {
		t.Fatalf("got=%+v", got)
	}

	got, err = ExtractPostToolDetailsFromHookJSON([]byte(`{
		"toolName":"Read",
		"conversation_id":"conv-1",
		"tool_input":{"command":"cat main.go"},
		"tool_response":"ok"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.CommandLine != "cat main.go" || got.ToolName != "Read" || got.SessionID != "conv-1" {
		t.Fatalf("got=%+v", got)
	}

	if _, err := ExtractPostToolDetailsFromHookJSON([]byte(`{`)); err == nil {
		t.Fatal("expected invalid JSON error")
	}
	if _, err := ExtractPostToolDetailsFromHookJSON([]byte(`{"command":"x"}`)); err == nil {
		t.Fatal("expected missing tool_response error")
	}
}

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
	if got.SessionID != "sess-1" || got.ToolName != "Bash" || got.ToolUseID != "tool-1" || got.CommandLine != "npm test" {
		t.Fatalf("got=%+v", got)
	}
}

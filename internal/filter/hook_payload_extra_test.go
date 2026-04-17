package filter

import (
	"strings"
	"testing"
)

func TestExtractPostToolPayloadFromHookJSON_NestedCommand(t *testing.T) {
	command, toolResponse, err := ExtractPostToolPayloadFromHookJSON([]byte(`{
		"event": {
			"tool_input": {"command": "go test ./..."},
			"tool_response": "ok"
		},
		"tool_response": "captured output"
	}`))
	if err != nil {
		t.Fatalf("ExtractPostToolPayloadFromHookJSON: %v", err)
	}
	if command != "go test ./..." {
		t.Fatalf("unexpected command: %q", command)
	}
	if toolResponse != "captured output" {
		t.Fatalf("unexpected tool response: %q", toolResponse)
	}
}

func TestExtractPostToolPayloadFromHookJSON_ToolResponseMustBeString(t *testing.T) {
	_, _, err := ExtractPostToolPayloadFromHookJSON([]byte(`{"tool_response": 42}`))
	if err == nil || !strings.Contains(err.Error(), "tool_response") {
		t.Fatalf("expected missing string tool_response error, got %v", err)
	}
}

func TestExtractPostToolPayloadFromHookJSON_InvalidJSON(t *testing.T) {
	_, _, err := ExtractPostToolPayloadFromHookJSON([]byte("{"))
	if err == nil || !strings.Contains(err.Error(), "filter: JSON") {
		t.Fatalf("expected JSON error, got %v", err)
	}
}

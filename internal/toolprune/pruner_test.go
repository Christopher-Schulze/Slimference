package toolprune

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/types"
)

func TestExtractToolNames_Anthropic(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"claude-3","tools":[{"name":"Read","description":"read"},{"name":"Write","description":"write"},{"name":"Bash","description":"run"}],"messages":[]}`)
	names := ExtractToolNames(body, types.Anthropic)
	if len(names) != 3 {
		t.Fatalf("got %d names: %v", len(names), names)
	}
	if names[0] != "Read" || names[1] != "Write" || names[2] != "Bash" {
		t.Fatalf("names: %v", names)
	}
}

func TestExtractToolNames_AnthropicBadEntry(t *testing.T) {
	t.Parallel()
	body := []byte(`{"tools":[123,true,{"name":"OK"}]}`)
	names := ExtractToolNames(body, types.Anthropic)
	if len(names) != 1 || names[0] != "OK" {
		t.Fatalf("expected only OK, got %v", names)
	}
}

func TestExtractToolNames_OpenAIBadEntry(t *testing.T) {
	t.Parallel()
	body := []byte(`{"tools":["bad",{"function":{"name":"good"}}]}`)
	names := ExtractToolNames(body, types.OpenAI)
	if len(names) != 1 || names[0] != "good" {
		t.Fatalf("expected only good, got %v", names)
	}
}

func TestExtractToolNames_UnknownProvider(t *testing.T) {
	t.Parallel()
	body := []byte(`{"tools":[{"function":{"name":"via-func"}}]}`)
	names := ExtractToolNames(body, types.Provider(99))
	if len(names) != 1 || names[0] != "via-func" {
		t.Fatalf("unknown provider should use OpenAI shape, got %v", names)
	}
}

func TestExtractToolNames_OpenAITopLevelName(t *testing.T) {
	t.Parallel()
	body := []byte(`{"tools":[{"name":"top-level"}]}`)
	names := ExtractToolNames(body, types.OpenAI)
	if len(names) != 1 || names[0] != "top-level" {
		t.Fatalf("expected top-level name, got %v", names)
	}
}

func TestExtractToolNames_OpenAI(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"gpt-4","tools":[{"type":"function","function":{"name":"get_weather"}},{"type":"function","function":{"name":"send_email"}}],"messages":[]}`)
	names := ExtractToolNames(body, types.OpenAI)
	if len(names) != 2 {
		t.Fatalf("got %d names: %v", len(names), names)
	}
	if names[0] != "get_weather" || names[1] != "send_email" {
		t.Fatalf("names: %v", names)
	}
}

func TestExtractToolNames_NoTools(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"gpt-4","messages":[]}`)
	names := ExtractToolNames(body, types.OpenAI)
	if names != nil {
		t.Fatalf("expected nil, got %v", names)
	}
}

func TestExtractToolNames_ToolsNotArray(t *testing.T) {
	t.Parallel()
	body := []byte(`{"tools":"not-array"}`)
	names := ExtractToolNames(body, types.Anthropic)
	if names != nil {
		t.Fatalf("expected nil for non-array tools, got %v", names)
	}
}

func TestExtractToolNames_InvalidJSON(t *testing.T) {
	t.Parallel()
	names := ExtractToolNames([]byte(`not json`), types.Anthropic)
	if names != nil {
		t.Fatalf("expected nil, got %v", names)
	}
}

func TestPruneToolDefinitions_Anthropic(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"claude-3","tools":[{"name":"Read","description":"read"},{"name":"Write","description":"write"},{"name":"Bash","description":"run"}],"messages":[]}`)
	toPrune := map[string]bool{"Write": true}
	out, removed, err := PruneToolDefinitions(body, types.Anthropic, toPrune)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 {
		t.Fatalf("removed: %d want 1", len(removed))
	}
	if _, ok := removed["Write"]; !ok {
		t.Fatal("Write should be in removed")
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	var tools []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(parsed["tools"], &tools); err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 {
		t.Fatalf("tools after prune: %d want 2", len(tools))
	}
	for _, t2 := range tools {
		if t2.Name == "Write" {
			t.Fatal("Write should have been pruned")
		}
	}
}

func TestPruneToolDefinitions_OpenAI(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"gpt-4","tools":[{"type":"function","function":{"name":"get_weather"}},{"type":"function","function":{"name":"send_email"}}],"messages":[]}`)
	toPrune := map[string]bool{"send_email": true}
	out, removed, err := PruneToolDefinitions(body, types.OpenAI, toPrune)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 {
		t.Fatalf("removed: %d want 1", len(removed))
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	var tools []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(parsed["tools"], &tools); err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Function.Name != "get_weather" {
		t.Fatalf("tools after prune: %v", tools)
	}
}

func TestPruneToolDefinitions_NoMatch(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"claude-3","tools":[{"name":"Read","description":"read"}],"messages":[]}`)
	toPrune := map[string]bool{"NonExistent": true}
	out, removed, err := PruneToolDefinitions(body, types.Anthropic, toPrune)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 {
		t.Fatalf("removed: %d want 0", len(removed))
	}
	if string(out) != string(body) {
		t.Fatal("body should be unchanged when no tools match")
	}
}

func TestPruneToolDefinitions_EmptyPrune(t *testing.T) {
	t.Parallel()
	body := []byte(`{"tools":[]}`)
	out, removed, err := PruneToolDefinitions(body, types.Anthropic, nil)
	if err != nil {
		t.Fatal(err)
	}
	if removed != nil {
		t.Fatalf("removed should be nil, got %v", removed)
	}
	if string(out) != string(body) {
		t.Fatal("body should be unchanged")
	}
}

func TestPruneToolDefinitions_InvalidJSON(t *testing.T) {
	t.Parallel()
	out, removed, err := PruneToolDefinitions([]byte(`bad`), types.Anthropic, map[string]bool{"x": true})
	if err != nil {
		t.Fatal(err)
	}
	if removed != nil {
		t.Fatalf("removed should be nil")
	}
	if string(out) != "bad" {
		t.Fatal("body should be returned unchanged on parse error")
	}
}

func TestPruneToolDefinitions_InvalidToolsArray(t *testing.T) {
	t.Parallel()
	body := []byte(`{"tools":"not an array"}`)
	out, removed, err := PruneToolDefinitions(body, types.Anthropic, map[string]bool{"x": true})
	if err != nil {
		t.Fatal(err)
	}
	if removed != nil {
		t.Fatalf("removed should be nil")
	}
	if string(out) != string(body) {
		t.Fatal("body should be unchanged when tools is not an array")
	}
}

func TestPruneToolDefinitions_NoToolsField(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"claude-3","messages":[]}`)
	out, removed, err := PruneToolDefinitions(body, types.Anthropic, map[string]bool{"x": true})
	if err != nil {
		t.Fatal(err)
	}
	if removed != nil {
		t.Fatalf("removed should be nil")
	}
	if string(out) != string(body) {
		t.Fatal("body should be unchanged when no tools field")
	}
}

func TestPruneToolDefinitions_LargeBody_Savings(t *testing.T) {
	t.Parallel()
	var tools []map[string]interface{}
	for i := 0; i < 20; i++ {
		tools = append(tools, map[string]interface{}{
			"name":        "Tool" + strings.Repeat("x", i+1),
			"description": strings.Repeat("d", 500),
		})
	}
	bodyMap := map[string]interface{}{
		"model":    "claude-3",
		"tools":    tools,
		"messages": []interface{}{},
	}
	body, _ := json.Marshal(bodyMap)
	toPrune := map[string]bool{}
	for i := 10; i < 20; i++ {
		toPrune["Tool"+strings.Repeat("x", i+1)] = true
	}
	out, removed, err := PruneToolDefinitions(body, types.Anthropic, toPrune)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 10 {
		t.Fatalf("removed: %d want 10", len(removed))
	}
	if len(out) >= len(body) {
		t.Fatalf("pruned body (%d) should be smaller than original (%d)", len(out), len(body))
	}
}

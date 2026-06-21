package toolprune

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/types"
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

func TestExtractToolNamesForPruningRejectsUnknownSchema(t *testing.T) {
	t.Parallel()
	names, safe := ExtractToolNamesForPruning([]byte(`{"tools":[{"name":"Read"},{"description":"missing name"}]}`), types.Anthropic)
	if safe || names != nil {
		t.Fatalf("mixed unknown schema must be unsafe, safe=%v names=%v", safe, names)
	}
	names, safe = ExtractToolNamesForPruning([]byte(`{"tools":[{"name":"Read"},{"name":"Write"}]}`), types.Anthropic)
	if !safe || len(names) != 2 {
		t.Fatalf("known schema should be safe, safe=%v names=%v", safe, names)
	}
	names, safe = ExtractToolNamesForPruning([]byte(`{"messages":[]}`), types.Anthropic)
	if !safe || names != nil {
		t.Fatalf("no tools should be safe no-op, safe=%v names=%v", safe, names)
	}
	if _, safe = ExtractToolNamesForPruning([]byte(`{"tools":"bad"}`), types.Anthropic); safe {
		t.Fatal("malformed tools shape must be unsafe")
	}
}

func TestExtractToolNamesForPruningCodexDesktopSpecialTools(t *testing.T) {
	t.Parallel()
	body := []byte(`{"tools":[{"type":"function","name":"exec_command"},{"type":"custom","name":"apply_patch"},{"type":"tool_search","parameters":{"type":"object"}},{"type":"web_search","external_web_access":true},{"type":"image_generation","output_format":"png"}]}`)
	names, safe := ExtractToolNamesForPruning(body, types.CodexChatGPT)
	if !safe {
		t.Fatalf("known Codex Desktop special tools should be schema-safe, names=%v", names)
	}
	want := []string{"exec_command", "apply_patch", "tool_search", "web_search", "image_generation"}
	if len(names) != len(want) {
		t.Fatalf("got names %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names[%d]=%q, want %q; all=%v", i, names[i], want[i], names)
		}
	}
}

func TestExtractToolNamesForPruningCodexUnknownNamelessToolIsUnsafe(t *testing.T) {
	t.Parallel()
	names, safe := ExtractToolNamesForPruning([]byte(`{"tools":[{"type":"function","name":"exec_command"},{"type":"unknown_tool_family","parameters":{"type":"object"}}]}`), types.CodexChatGPT)
	if safe || names != nil {
		t.Fatalf("unknown nameless Codex tool must full-pass, safe=%v names=%v", safe, names)
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
	var tools []map[string]any
	for i := range 20 {
		tools = append(tools, map[string]any{
			"name":        "Tool" + strings.Repeat("x", i+1),
			"description": strings.Repeat("d", 500),
		})
	}
	bodyMap := map[string]any{
		"model":    "claude-3",
		"tools":    tools,
		"messages": []any{},
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

func TestDecideWithOptions_AlwaysKeepAndCooldown(t *testing.T) {
	t.Parallel()
	u := NewUsageTracker(2)
	const session = "s"
	u.ObserveTurn(session, []string{"Bash", "ColdTool", "CustomKeep"})
	u.ObserveTurn(session, []string{"Other"})
	u.ObserveTurn(session, []string{"Other"})
	u.ObserveTurn(session, []string{"Other"})

	decision := u.DecideWithOptions(session, []string{"Bash", "ColdTool", "CustomKeep"}, DecisionOptions{
		MinKeep:    1,
		AlwaysKeep: []string{"customkeep"},
	})
	if !containsString(decision.Keep, "Bash") || !containsString(decision.Keep, "CustomKeep") {
		t.Fatalf("always-keep tools missing: %+v", decision)
	}
	if !containsString(decision.Pruned, "ColdTool") {
		t.Fatalf("cold tool should be pruned: %+v", decision)
	}
	if decision.AlwaysKept != 2 {
		t.Fatalf("always kept = %d want 2", decision.AlwaysKept)
	}

	u.MarkMiss(session)
	cooldown := u.DecideWithOptions(session, []string{"ColdTool"}, DecisionOptions{MinKeep: 1})
	if cooldown.Reason != "quality_cooldown" || len(cooldown.Pruned) != 0 || !containsString(cooldown.Keep, "ColdTool") {
		t.Fatalf("cooldown decision: %+v", cooldown)
	}
	snap := u.Snapshot()
	if snap.MissTotal != 1 || snap.DisabledSessions != 0 {
		t.Fatalf("snapshot after miss: %+v", snap)
	}
}

func TestLooksLikeMissingToolError(t *testing.T) {
	t.Parallel()
	if !LooksLikeMissingToolError(400, []byte(`{"error":"unknown tool GetWeather"}`)) {
		t.Fatal("expected unknown tool error to match")
	}
	if !LooksLikeMissingToolError(422, []byte(`tool_use id not found in tools`)) {
		t.Fatal("expected not found in tools to match")
	}
	if !LooksLikeMissingToolError(400, []byte(`{"error":"no such tool: GetWeather"}`)) {
		t.Fatal("expected no such tool error to match")
	}
	if !LooksLikeMissingToolError(400, []byte(`{"error":"requested tool is not available"}`)) {
		t.Fatal("expected unavailable tool error to match")
	}
	if !LooksLikeMissingToolError(400, []byte(`{"error":"tool was not provided in this request"}`)) {
		t.Fatal("expected not-provided tool error to match")
	}
	if !LooksLikeMissingToolError(400, []byte(`{"error":"no tool named GetWeather is available"}`)) {
		t.Fatal("expected no-tool-named error to match")
	}
	if !LooksLikeMissingToolError(400, []byte(`{"error":"tool GetWeather does not exist"}`)) {
		t.Fatal("expected does-not-exist tool error to match")
	}
	if !LooksLikeMissingToolError(400, []byte(`{"error":"GetWeather is not in the list of available tools"}`)) {
		t.Fatal("expected available-tools list error to match")
	}
	if !LooksLikeMissingToolError(400, []byte(`{"error":"function not found: get_weather"}`)) {
		t.Fatal("expected function-not-found error to match")
	}
	if !LooksLikeMissingToolError(400, []byte(`{"error":"not a valid function: get_weather"}`)) {
		t.Fatal("expected invalid-function error to match")
	}
	if LooksLikeMissingToolError(500, []byte(`unknown tool`)) {
		t.Fatal("5xx should not trigger tool fallback")
	}
	if LooksLikeMissingToolError(400, []byte(`invalid prompt_cache_key`)) {
		t.Fatal("unrelated 4xx should not trigger tool fallback")
	}
}

func containsString(values []string, needle string) bool {
	return slices.Contains(values, needle)
}

func TestCommandFamilyAliases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"bash", "Bash", []string{"bash", "shell", "exec", "terminal", "command"}},
		{"shell", "ShellTool", []string{"bash", "shell", "exec", "terminal", "command"}},
		{"exec", "ExecTool", []string{"bash", "shell", "exec", "terminal", "command"}},
		{"terminal", "Terminal", []string{"bash", "shell", "exec", "terminal", "command"}},
		{"grep", "GrepTool", []string{"grep", "rg", "search"}},
		{"search", "SearchTool", []string{"grep", "rg", "search"}},
		{"rg", "RGTool", []string{"grep", "rg", "search"}},
		{"read", "ReadTool", []string{"read", "open", "view"}},
		{"open", "OpenFile", []string{"read", "open", "view"}},
		{"view", "ViewTool", []string{"read", "open", "view"}},
		{"write", "WriteTool", []string{"write", "edit", "patch", "apply_patch"}},
		{"edit", "EditTool", []string{"write", "edit", "patch", "apply_patch"}},
		{"patch", "PatchTool", []string{"write", "edit", "patch", "apply_patch"}},
		{"unknown", "UnknownTool", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := commandFamilyAliases(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("commandFamilyAliases(%q) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("commandFamilyAliases(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestContainsToolAlias_NormalizedMatch(t *testing.T) {
	t.Parallel()
	// Test the normalizedAlias path (line 189-191): a long alias (>=6 chars)
	// that matches in normalized text.
	if !containsToolAlias("running bash command", "running bash command", "Bash") {
		t.Fatal("containsToolAlias should find 'bash' in text")
	}
	// No match at all.
	if containsToolAlias("nothing relevant here", "nothing relevant here", "Bash") {
		t.Fatal("containsToolAlias should not find 'bash' in unrelated text")
	}
}

package proxy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tokenproxy/tokenproxy/internal/types"
)

// TestDetectProvider_ByPath verifies provider detection from URL path.
func TestDetectProvider_ByPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want types.Provider
	}{
		{"Anthropic messages path", "/v1/messages", types.Anthropic},
		{"OpenAI chat completions path", "/v1/chat/completions", types.OpenAI},
		{"Anthropic messages nested", "/anthropic/v1/messages", types.Anthropic},
		{"unknown path falls back to OpenAI", "/v1/unknown", types.OpenAI},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := detectProvider(tc.path, nil)
			if got != tc.want {
				t.Errorf("detectProvider(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestDetectProvider_ByBodyMaxTokens(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"x","max_tokens":10,"messages":[]}`)
	if got := detectProvider("/v1/unknown-endpoint", body); got != types.Anthropic {
		t.Fatalf("path fallback + max_tokens without frequency_penalty => Anthropic, got %v", got)
	}
}

// TestExtractMessages_Anthropic_StringContent verifies parsing of string-content messages.
func TestExtractMessages_Anthropic_StringContent(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "Hello, world!"},
			{"role": "assistant", "content": "Hi there!"}
		]
	}`)

	msgs, _, err := extractMessages(types.Anthropic, body)
	if err != nil {
		t.Fatalf("extractMessages returned error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("msgs[0].Role = %q, want %q", msgs[0].Role, "user")
	}
	if msgs[0].Content[0].Text != "Hello, world!" {
		t.Errorf("msgs[0].Content[0].Text = %q, want %q", msgs[0].Content[0].Text, "Hello, world!")
	}
	if msgs[0].Content[0].Type != "text" {
		t.Errorf("msgs[0].Content[0].Type = %q, want %q", msgs[0].Content[0].Type, "text")
	}
	if msgs[1].Role != "assistant" {
		t.Errorf("msgs[1].Role = %q, want %q", msgs[1].Role, "assistant")
	}
}

// TestExtractMessages_Anthropic_ContentBlocks verifies parsing of array content blocks.
func TestExtractMessages_Anthropic_ContentBlocks(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"max_tokens": 1024,
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "first block"},
					{"type": "text", "text": "second block"}
				]
			}
		]
	}`)

	msgs, _, err := extractMessages(types.Anthropic, body)
	if err != nil {
		t.Fatalf("extractMessages returned error: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if len(msgs[0].Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(msgs[0].Content))
	}
	if msgs[0].Content[0].Text != "first block" {
		t.Errorf("Content[0].Text = %q, want %q", msgs[0].Content[0].Text, "first block")
	}
	if msgs[0].Content[1].Text != "second block" {
		t.Errorf("Content[1].Text = %q, want %q", msgs[0].Content[1].Text, "second block")
	}
}

// TestExtractMessages_Anthropic_ToolUse verifies parsing of tool_use content blocks.
func TestExtractMessages_Anthropic_ToolUse(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"max_tokens": 1024,
		"messages": [
			{
				"role": "assistant",
				"content": [
					{
						"type": "tool_use",
						"id": "tu_abc123",
						"name": "read_file",
						"input": {"path": "/tmp/file.go"}
					}
				]
			}
		]
	}`)

	msgs, _, err := extractMessages(types.Anthropic, body)
	if err != nil {
		t.Fatalf("extractMessages returned error: %v", err)
	}
	if len(msgs) != 1 || len(msgs[0].Content) != 1 {
		t.Fatalf("expected 1 message with 1 block, got msgs=%d", len(msgs))
	}
	block := msgs[0].Content[0]
	if block.Type != "tool_use" {
		t.Errorf("Type = %q, want %q", block.Type, "tool_use")
	}
	if block.ToolName != "read_file" {
		t.Errorf("ToolName = %q, want %q", block.ToolName, "read_file")
	}
	if block.ToolUseID != "tu_abc123" {
		t.Errorf("ToolUseID = %q, want %q", block.ToolUseID, "tu_abc123")
	}
	if !strings.Contains(block.ToolInput, "/tmp/file.go") {
		t.Errorf("ToolInput = %q, want path /tmp/file.go", block.ToolInput)
	}
}

// TestExtractMessages_Anthropic_ToolResult verifies parsing of tool_result content blocks.
func TestExtractMessages_Anthropic_ToolResult(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"max_tokens": 1024,
		"messages": [
			{
				"role": "user",
				"content": [
					{
						"type": "tool_result",
						"tool_use_id": "tu_abc123",
						"content": "file contents here"
					}
				]
			}
		]
	}`)

	msgs, _, err := extractMessages(types.Anthropic, body)
	if err != nil {
		t.Fatalf("extractMessages returned error: %v", err)
	}
	if len(msgs) != 1 || len(msgs[0].Content) != 1 {
		t.Fatalf("expected 1 message with 1 block, got msgs=%d", len(msgs))
	}
	block := msgs[0].Content[0]
	if block.Type != "tool_result" {
		t.Errorf("Type = %q, want %q", block.Type, "tool_result")
	}
	if block.ToolResultID != "tu_abc123" {
		t.Errorf("ToolResultID = %q, want %q", block.ToolResultID, "tu_abc123")
	}
	if block.Text != "file contents here" {
		t.Errorf("Text = %q, want %q", block.Text, "file contents here")
	}
}

// TestReconstructBody_Anthropic verifies that reconstructBody produces valid JSON with the correct messages.
func TestReconstructBody_Anthropic(t *testing.T) {
	t.Parallel()

	originalBody := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "original text"}
		]
	}`)

	compressed := []types.Message{
		{
			Index: 0,
			Role:  "user",
			Content: []types.ContentBlock{
				{Type: "text", Text: "compressed text"},
			},
		},
	}

	out, err := reconstructBody(types.Anthropic, originalBody, compressed)
	if err != nil {
		t.Fatalf("reconstructBody returned error: %v", err)
	}

	// Must be valid JSON.
	var result map[string]json.RawMessage
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("reconstructed body is not valid JSON: %v", err)
	}

	// Model field must be preserved.
	var model string
	if err := json.Unmarshal(result["model"], &model); err != nil || model != "claude-3-5-sonnet-20241022" {
		t.Errorf("model = %q, want %q", model, "claude-3-5-sonnet-20241022")
	}

	// Messages must contain the compressed text.
	if !strings.Contains(string(out), "compressed text") {
		t.Errorf("reconstructed body does not contain compressed text; body: %s", out)
	}
	// Original text must be gone.
	if strings.Contains(string(out), "original text") {
		t.Errorf("reconstructed body still contains original text; body: %s", out)
	}
}

// TestReconstructBody_PreservesExtraFields verifies that fields beyond "messages" are preserved.
func TestReconstructBody_PreservesExtraFields(t *testing.T) {
	t.Parallel()

	originalBody := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"max_tokens": 2048,
		"temperature": 0.7,
		"system": "You are a helpful assistant.",
		"stream": true,
		"messages": [
			{"role": "user", "content": "hello"}
		]
	}`)

	compressed := []types.Message{
		{
			Index: 0,
			Role:  "user",
			Content: []types.ContentBlock{
				{Type: "text", Text: "hello"},
			},
		},
	}

	out, err := reconstructBody(types.Anthropic, originalBody, compressed)
	if err != nil {
		t.Fatalf("reconstructBody returned error: %v", err)
	}

	var result map[string]json.RawMessage
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("reconstructed body is not valid JSON: %v", err)
	}

	extraFields := []string{"temperature", "system", "stream", "max_tokens"}
	for _, field := range extraFields {
		if _, ok := result[field]; !ok {
			t.Errorf("extra field %q missing from reconstructed body", field)
		}
	}
}

func TestDetectProvider_ByBody(t *testing.T) {
	t.Parallel()
	anthropicBody := []byte(`{"model":"x","max_tokens":100,"messages":[]}`)
	if got := detectProvider("/v1/unknown", anthropicBody); got != types.Anthropic {
		t.Fatalf("got %v want Anthropic", got)
	}
	openaiBody := []byte(`{"model":"x","max_completion_tokens":100,"frequency_penalty":0,"messages":[]}`)
	if got := detectProvider("/v1/unknown", openaiBody); got != types.OpenAI {
		t.Fatalf("got %v want OpenAI", got)
	}
}

func TestExtractTextFromContent(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`"hello"`)
	if extractTextFromContent(raw) != "hello" {
		t.Fatalf("string json: %q", extractTextFromContent(raw))
	}
	arr := json.RawMessage(`[{"type":"text","text":"a"},{"type":"text","text":"b"}]`)
	if got := extractTextFromContent(arr); got != "a\nb" {
		t.Fatalf("array: %q", got)
	}
	fallback := json.RawMessage(`not-valid-json`)
	if extractTextFromContent(fallback) == "" {
		t.Fatal("fallback should return raw")
	}
}

func TestMessagesToAnthropicJSON_singleText(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{{
		Role: "user",
		Content: []types.ContentBlock{
			{Type: "text", Text: "compact"},
		},
	}}
	raw, err := messagesToAnthropicJSON(msgs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "compact") {
		t.Fatalf("%s", raw)
	}
}

func TestContentBlocksToAnthropicJSON_multipleBlocks(t *testing.T) {
	t.Parallel()
	blocks := []types.ContentBlock{
		{Type: "text", Text: "first"},
		{Type: "text", Text: "second"},
	}
	raw, err := contentBlocksToAnthropicJSON(blocks)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 20 {
		t.Fatalf("short: %s", raw)
	}
}

func TestContentBlocksToAnthropicJSON_toolUseEmptyInput(t *testing.T) {
	t.Parallel()
	blocks := []types.ContentBlock{{
		Type:      "tool_use",
		ToolUseID: "toolu_1",
		ToolName:  "bash",
		ToolInput: "",
	}}
	raw, err := contentBlocksToAnthropicJSON(blocks)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"input":{}`) {
		t.Fatalf("want empty input object: %s", raw)
	}
}

func TestContentBlocksToAnthropicJSON_imageBlock(t *testing.T) {
	t.Parallel()
	blocks := []types.ContentBlock{{
		Type:        "image",
		ImageSource: map[string]any{"type": "url", "url": "https://example.com/x.png"},
	}}
	raw, err := contentBlocksToAnthropicJSON(blocks)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "example.com") {
		t.Fatalf("%s", raw)
	}
}

func TestContentBlocksToAnthropicJSON_rawBlockWithCacheControl(t *testing.T) {
	t.Parallel()
	blocks := []types.ContentBlock{{
		Type: "text",
		Text: "edited surface text",
		RawBlock: anthropicContentBlock{
			Type: "text",
			Text: "original from wire",
		},
		CacheControl: &types.CacheControl{Type: "ephemeral"},
	}}
	raw, err := contentBlocksToAnthropicJSON(blocks)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "edited surface text") || !strings.Contains(s, "cache_control") {
		t.Fatalf("%s", raw)
	}
}

func TestContentBlocksToAnthropicJSON_toolResultBlock(t *testing.T) {
	t.Parallel()
	blocks := []types.ContentBlock{{
		Type:         "tool_result",
		ToolResultID: "toolu_99",
		Text:         "stdout here",
	}}
	raw, err := contentBlocksToAnthropicJSON(blocks)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "tool_use_id") || !strings.Contains(s, "stdout here") {
		t.Fatalf("%s", raw)
	}
}

func TestContentBlocksToAnthropicJSON_imageBlockNoSource(t *testing.T) {
	t.Parallel()
	blocks := []types.ContentBlock{{Type: "image"}}
	raw, err := contentBlocksToAnthropicJSON(blocks)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"type":"image"`) {
		t.Fatalf("%s", raw)
	}
}

func TestContentBlocksToAnthropicJSON_rawBlockToolUse(t *testing.T) {
	t.Parallel()
	blocks := []types.ContentBlock{{
		Type:      "tool_use",
		ToolUseID: "tu_1",
		ToolName:  "read_file",
		ToolInput: `{"path":"/x"}`,
		RawBlock: anthropicContentBlock{
			Type:  "tool_use",
			ID:    "tu_1",
			Name:  "read_file",
			Input: json.RawMessage(`{"path":"/src"}`),
		},
		CacheControl: &types.CacheControl{Type: "ephemeral"},
	}}
	raw, err := contentBlocksToAnthropicJSON(blocks)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "read_file") || !strings.Contains(s, "cache_control") {
		t.Fatalf("%s", raw)
	}
}

func TestMessagesToOpenAIJSON_userMessages(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{
			Role: "user",
			Content: []types.ContentBlock{
				{Type: "text", Text: "hello"},
			},
		},
	}
	raw, err := messagesToOpenAIJSON(msgs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "hello") {
		t.Fatalf("%s", raw)
	}
}

func TestReconstructBody_openai(t *testing.T) {
	t.Parallel()
	originalBody := []byte(`{
		"model": "gpt-4",
		"messages": [{"role":"user","content":"old"}]
	}`)
	compressed := []types.Message{
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "new"}}},
	}
	out, err := reconstructBody(types.OpenAI, originalBody, compressed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "new") {
		t.Fatalf("%s", out)
	}
}

func TestReconstructBody_invalidOriginalJSON(t *testing.T) {
	t.Parallel()
	_, err := reconstructBody(types.OpenAI, []byte(`{`), nil)
	if err == nil {
		t.Fatal("expected error for invalid original body")
	}
}

func TestExtractOpenAIMessages_invalidJSON(t *testing.T) {
	t.Parallel()
	_, _, err := extractOpenAIMessages(json.RawMessage(`{`), nil)
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestExtractOpenAIMessages_toolRoleAndArrayContent(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`[
		{"role":"user","content":[{"type":"text","text":"hi"}]},
		{"role":"tool","content":"tool output","tool_call_id":"call_abc"}
	]`)
	msgs, _, err := extractOpenAIMessages(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len=%d", len(msgs))
	}
	if msgs[0].Content[0].Type != "text" || msgs[0].Content[0].Text == "" {
		t.Fatalf("user array-as-string: %#v", msgs[0].Content[0])
	}
	if msgs[1].Role != "tool" || msgs[1].Content[0].Type != "tool_result" || msgs[1].Content[0].ToolResultID != "call_abc" {
		t.Fatalf("tool msg: %#v", msgs[1])
	}
}

func TestMessagesToOpenAIJSON_toolRole(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{
			Role: "tool",
			Content: []types.ContentBlock{
				{Type: "tool_result", Text: "compact-out", ToolResultID: "call_xyz"},
			},
		},
	}
	raw, err := messagesToOpenAIJSON(msgs)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "call_xyz") || !strings.Contains(s, "compact-out") {
		t.Fatalf("%s", s)
	}
}

func TestMessagesToOpenAIJSON_rawOpenAIMessageBranch(t *testing.T) {
	t.Parallel()
	orig := OpenAIMessage{
		Role:    "assistant",
		Content: json.RawMessage(`"original"`),
	}
	msgs := []types.Message{
		{
			Role: "assistant",
			Content: []types.ContentBlock{
				{Type: "text", Text: "updated-text", RawBlock: orig},
			},
		},
	}
	raw, err := messagesToOpenAIJSON(msgs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "updated-text") {
		t.Fatalf("%s", raw)
	}
}

// TestExtractMessages_noMessagesKey covers the branch where "messages" key is absent.
func TestExtractMessages_noMessagesKey(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"x","max_tokens":100}`)
	msgs, raw, err := extractMessages(types.Anthropic, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msgs != nil {
		t.Fatalf("expected nil messages, got %v", msgs)
	}
	if raw == nil {
		t.Fatal("expected non-nil raw map")
	}
}

// TestExtractMessages_unknownProvider covers the default branch for an unknown provider.
func TestExtractMessages_unknownProvider(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"x","messages":[{"role":"user","content":"hi"}]}`)
	_, _, err := extractMessages(types.Provider(99), body)
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("error = %q, want 'unknown provider'", err)
	}
}

// TestExtractMessages_invalidBodyJSON covers the unmarshal error in extractMessages.
func TestExtractMessages_invalidBodyJSON(t *testing.T) {
	t.Parallel()
	_, _, err := extractMessages(types.Anthropic, []byte(`not-json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// TestExtractAnthropicMessages_unmarshalError covers the unmarshal error branch.
func TestExtractAnthropicMessages_unmarshalError(t *testing.T) {
	t.Parallel()
	_, _, err := extractAnthropicMessages(json.RawMessage(`{not-array}`), nil)
	if err == nil {
		t.Fatal("expected error for non-array messages")
	}
}

// TestExtractAnthropicMessages_contentParseError covers the propagated parse error for malformed content.
func TestExtractAnthropicMessages_contentParseError(t *testing.T) {
	t.Parallel()
	// The content value is a number (123) - it is valid JSON, passes AnthropicMessage unmarshal
	// since Content is json.RawMessage, but then parseAnthropicContent fails (not string, not array).
	body := []byte(`{"model":"x","max_tokens":1,"messages":[{"role":"user","content":123}]}`)
	_, _, err := extractMessages(types.Anthropic, body)
	if err == nil {
		t.Fatal("expected error for numeric content (not string or array)")
	}
}

// TestParseAnthropicContent_empty covers the empty raw branch (returns nil, nil).
func TestParseAnthropicContent_empty(t *testing.T) {
	t.Parallel()
	blocks, err := parseAnthropicContent(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if blocks != nil {
		t.Fatalf("expected nil blocks for empty input, got %v", blocks)
	}
	// Also test explicitly empty RawMessage
	blocks2, err2 := parseAnthropicContent(json.RawMessage(""))
	if err2 != nil {
		t.Fatalf("unexpected error for empty RawMessage: %v", err2)
	}
	if blocks2 != nil {
		t.Fatalf("expected nil blocks for empty RawMessage, got %v", blocks2)
	}
}

// TestParseAnthropicContent_malformedString covers the unmarshal error for a string that starts with '"' but is invalid JSON.
func TestParseAnthropicContent_malformedString(t *testing.T) {
	t.Parallel()
	// Starts with '"' but is not a valid JSON string.
	_, err := parseAnthropicContent(json.RawMessage(`"unterminated`))
	if err == nil {
		t.Fatal("expected error for malformed JSON string content")
	}
}

// TestParseAnthropicContent_malformedArray covers the unmarshal error for an invalid array.
func TestParseAnthropicContent_malformedArray(t *testing.T) {
	t.Parallel()
	// Does not start with '"', so treated as array - but invalid JSON.
	_, err := parseAnthropicContent(json.RawMessage(`[{`))
	if err == nil {
		t.Fatal("expected error for malformed JSON array content")
	}
}

// TestParseAnthropicContent_imageBlock covers the image branch in parseAnthropicContent.
func TestParseAnthropicContent_imageBlock(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"abc"}}]`)
	blocks, err := parseAnthropicContent(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Type != "image" {
		t.Fatalf("expected image type, got %q", blocks[0].Type)
	}
}

// TestParseAnthropicContent_cacheControl covers the CacheControl branch in parseAnthropicContent.
func TestParseAnthropicContent_cacheControl(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`[{"type":"text","text":"hello","cache_control":{"type":"ephemeral"}}]`)
	blocks, err := parseAnthropicContent(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].CacheControl == nil {
		t.Fatal("expected CacheControl to be set")
	}
	if blocks[0].CacheControl.Type != "ephemeral" {
		t.Fatalf("expected ephemeral cache control, got %q", blocks[0].CacheControl.Type)
	}
}

// TestParseAnthropicContent_toolResultArrayContent covers the tool_result branch with array content.
func TestParseAnthropicContent_toolResultArrayContent(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`[{"type":"tool_result","tool_use_id":"tu_1","content":[{"type":"text","text":"result text"}]}]`)
	blocks, err := parseAnthropicContent(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Type != "tool_result" {
		t.Fatalf("expected tool_result type, got %q", blocks[0].Type)
	}
	if blocks[0].Text != "result text" {
		t.Fatalf("expected 'result text', got %q", blocks[0].Text)
	}
}

// TestExtractTextFromContent_empty covers the empty/blank raw branch.
func TestExtractTextFromContent_empty(t *testing.T) {
	t.Parallel()
	if got := extractTextFromContent(json.RawMessage("")); got != "" {
		t.Fatalf("empty: got %q", got)
	}
	if got := extractTextFromContent(json.RawMessage("   ")); got != "" {
		t.Fatalf("whitespace: got %q", got)
	}
}

// TestMessagesToOpenAIJSON_noContent covers the branch where a non-tool message has no content blocks.
func TestMessagesToOpenAIJSON_noContent(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Role: "user", Content: nil},
	}
	raw, err := messagesToOpenAIJSON(msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("expected non-empty JSON")
	}
}

// TestContentBlockToAnthropicWire_toolUseWithInput covers the tool_use branch (lines 341-343)
// where b.ToolInput is non-empty and there is no RawBlock (direct wire path).
func TestContentBlockToAnthropicWire_toolUseWithInput(t *testing.T) {
	t.Parallel()
	blocks := []types.ContentBlock{{
		Type:      "tool_use",
		ToolUseID: "tu_42",
		ToolName:  "run_bash",
		ToolInput: `{"cmd":"ls"}`,
		// No RawBlock - uses direct wire path.
	}}
	raw, err := contentBlocksToAnthropicJSON(blocks)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "run_bash") {
		t.Fatalf("missing tool name: %s", s)
	}
	if !strings.Contains(s, `"cmd":"ls"`) {
		t.Fatalf("missing tool input: %s", s)
	}
}

// TestContentBlockToAnthropicWire_toolResultWithCacheControl covers the cache_control branch (lines 358-361)
// on the non-RawBlock path (b.Type == "tool_result" exits the raw-block branch).
func TestContentBlockToAnthropicWire_toolResultWithCacheControl(t *testing.T) {
	t.Parallel()
	blocks := []types.ContentBlock{{
		Type:         "tool_result",
		ToolResultID: "tu_99",
		Text:         "output here",
		CacheControl: &types.CacheControl{Type: "ephemeral"},
		// No RawBlock - uses direct wire path.
	}}
	raw, err := contentBlocksToAnthropicJSON(blocks)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "tool_use_id") {
		t.Fatalf("missing tool_use_id: %s", s)
	}
	if !strings.Contains(s, "cache_control") {
		t.Fatalf("missing cache_control: %s", s)
	}
}

// TestContentBlocksToAnthropicJSON_singleTextWithCacheControl covers the multi-block path
// for a single text block that has CacheControl set (bypasses the simple-string shortcut).
func TestContentBlocksToAnthropicJSON_singleTextWithCacheControl(t *testing.T) {
	t.Parallel()
	// A single text block WITH CacheControl must NOT go through the simple string path.
	blocks := []types.ContentBlock{{
		Type:         "text",
		Text:         "hello",
		CacheControl: &types.CacheControl{Type: "ephemeral"},
	}}
	raw, err := contentBlocksToAnthropicJSON(blocks)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "cache_control") {
		t.Fatalf("cache_control missing: %s", s)
	}
	if !strings.Contains(s, "hello") {
		t.Fatalf("text missing: %s", s)
	}
}

// TestContentBlockToAnthropicWire_textNoRawBlock covers the "text" case (line 333-335)
// on the non-RawBlock wire path.
func TestContentBlockToAnthropicWire_textNoRawBlock(t *testing.T) {
	t.Parallel()
	blocks := []types.ContentBlock{
		// Two text blocks - bypasses single-text shortcut; no RawBlock.
		{Type: "text", Text: "first"},
		{Type: "text", Text: "second", CacheControl: &types.CacheControl{Type: "ephemeral"}},
	}
	raw, err := contentBlocksToAnthropicJSON(blocks)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "first") || !strings.Contains(s, "second") {
		t.Fatalf("text missing: %s", s)
	}
}

// TestReconstructBody_unknownProvider covers the default switch case (no messages set)
// when provider is neither Anthropic nor OpenAI.
func TestReconstructBody_unknownProvider(t *testing.T) {
	t.Parallel()
	originalBody := []byte(`{"model":"x","messages":[{"role":"user","content":"hi"}]}`)
	out, err := reconstructBody(types.Provider(99), originalBody, nil)
	// Should not error - the switch has no default, so messages key stays from original.
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected non-empty output")
	}
}

// TestMessagesToOpenAIJSON_nonRawBlockNoRawBlock covers the branch where content has blocks but no OpenAIMessage raw block.
func TestMessagesToOpenAIJSON_nonRawBlockNoRawBlock(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{
			Role: "user",
			Content: []types.ContentBlock{
				{Type: "text", Text: "hello", RawBlock: nil},
			},
		},
	}
	raw, err := messagesToOpenAIJSON(msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(raw), "hello") {
		t.Fatalf("expected 'hello' in output: %s", raw)
	}
}

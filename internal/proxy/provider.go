// Package proxy implements the HTTP reverse proxy with multi-layer compression.
package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// detectProvider determines the upstream provider from the HTTP request.
// T66: Codex CLI (ChatGPT subscription product) posts to /backend-api/*
// against chatgpt.com - we recognise that path prefix FIRST so Codex app
// and connector routes do not fall through to api.openai.com. When the path
// is ambiguous (Codex also sends /v1/responses through openai_base_url), the
// User-Agent header disambiguates: Codex's native UA contains "codex",
// Claude Code's UA contains "claude-code".
func detectProvider(path string, body []byte) types.Provider {
	return detectProviderWithUA(path, body, "")
}

// detectProviderWithUA is the UA-aware variant used on the hot path. The
// public detectProvider wraps it with an empty UA for tests that don't care.
func detectProviderWithUA(path string, body []byte, userAgent string) types.Provider {
	if path == "/backend-api" || strings.HasPrefix(path, "/backend-api/") {
		return types.CodexChatGPT
	}
	if strings.Contains(path, "/messages") {
		return types.Anthropic
	}

	// UA disambiguation before the generic /chat/completions match so that
	// a Codex request through openai_base_url (which Codex sends to
	// /v1/responses) routes to chatgpt.com, not api.openai.com.
	uaLower := strings.ToLower(userAgent)
	if strings.Contains(uaLower, "codex") {
		return types.CodexChatGPT
	}

	if strings.Contains(path, "/chat/completions") {
		return types.OpenAI
	}
	// Fallback: check body structure. Anthropic uses max_tokens, OpenAI may use max_completion_tokens.
	if len(body) > 0 {
		var probe map[string]json.RawMessage
		if json.Unmarshal(body, &probe) == nil {
			_, hasMaxTokens := probe["max_tokens"]
			_, hasFreqPenalty := probe["frequency_penalty"]
			if hasMaxTokens && !hasFreqPenalty {
				return types.Anthropic
			}
		}
	}
	return types.OpenAI
}

// AnthropicRequest is the wire format for Anthropic Messages API.
type AnthropicRequest struct {
	Model     string             `json:"model"`
	Messages  []AnthropicMessage `json:"messages"`
	System    json.RawMessage    `json:"system,omitempty"`
	MaxTokens int                `json:"max_tokens"`
	Stream    bool               `json:"stream,omitempty"`
	// All other fields preserved via Extra.
	extra map[string]json.RawMessage
}

// AnthropicMessage is an individual message in an Anthropic request.
type AnthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string or []ContentBlock
}

// OpenAIRequest is the wire format for OpenAI Chat Completions API.
type OpenAIRequest struct {
	Model    string          `json:"model"`
	Messages []OpenAIMessage `json:"messages"`
	Stream   bool            `json:"stream,omitempty"`
	extra    map[string]json.RawMessage
}

// OpenAIMessage is an individual message in an OpenAI request.
type OpenAIMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type codexInputItemRaw struct {
	Fields    map[string]json.RawMessage
	ItemIndex int
	TextPath  string
	TextIndex int
}

// anthropicContentBlock mirrors the JSON structure for Anthropic content blocks.
type anthropicContentBlock struct {
	Type         string          `json:"type"`
	Text         string          `json:"text,omitempty"`
	ID           string          `json:"id,omitempty"`
	Name         string          `json:"name,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
	ToolUseID    string          `json:"tool_use_id,omitempty"`
	Content      json.RawMessage `json:"content,omitempty"`
	CacheControl *struct {
		Type string `json:"type"`
	} `json:"cache_control,omitempty"`
	Source json.RawMessage `json:"source,omitempty"` // for image blocks
}

// extractMessages converts a raw request body into normalized internal Messages.
// It handles Anthropic, OpenAI, and Codex wire formats.
func extractMessages(provider types.Provider, body []byte) ([]types.Message, map[string]json.RawMessage, error) {
	// Parse as generic map first to preserve all extra fields.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil, fmt.Errorf("unmarshal body: %w", err)
	}

	if provider == types.CodexChatGPT {
		return extractCodexMessages(raw)
	}

	messagesRaw, ok := raw["messages"]
	if !ok {
		return nil, raw, nil
	}

	switch provider {
	case types.Anthropic:
		return extractAnthropicMessages(messagesRaw, raw)
	case types.OpenAI:
		return extractOpenAIMessages(messagesRaw, raw)
	default:
		return nil, raw, fmt.Errorf("unknown provider")
	}
}

func extractAnthropicMessages(messagesRaw json.RawMessage, extra map[string]json.RawMessage) ([]types.Message, map[string]json.RawMessage, error) {
	var rawMsgs []AnthropicMessage
	if err := json.Unmarshal(messagesRaw, &rawMsgs); err != nil {
		return nil, extra, fmt.Errorf("unmarshal anthropic messages: %w", err)
	}

	messages := make([]types.Message, 0, len(rawMsgs))
	for i, rm := range rawMsgs {
		msg := types.Message{
			Index: i,
			Role:  rm.Role,
		}
		blocks, err := parseAnthropicContent(rm.Content)
		if err != nil {
			return nil, extra, fmt.Errorf("parse message %d content: %w", i, err)
		}
		msg.Content = blocks
		messages = append(messages, msg)
	}
	return messages, extra, nil
}

func extractCodexMessages(raw map[string]json.RawMessage) ([]types.Message, map[string]json.RawMessage, error) {
	if messagesRaw, ok := raw["messages"]; ok {
		return extractOpenAIMessages(messagesRaw, raw)
	}

	inputRaw, ok := raw["input"]
	if !ok {
		return nil, raw, nil
	}
	return extractCodexInputMessages(inputRaw, raw)
}

func extractCodexInputMessages(inputRaw json.RawMessage, extra map[string]json.RawMessage) ([]types.Message, map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(inputRaw)
	if len(trimmed) == 0 {
		return nil, extra, nil
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(inputRaw, &text); err != nil {
			return nil, extra, fmt.Errorf("unmarshal codex input string: %w", err)
		}
		return []types.Message{{
			Index:   0,
			Role:    "user",
			Content: []types.ContentBlock{{Type: "text", Text: text}},
		}}, extra, nil
	}

	var items []json.RawMessage
	if err := json.Unmarshal(inputRaw, &items); err != nil {
		return nil, extra, fmt.Errorf("unmarshal codex input items: %w", err)
	}

	messages := make([]types.Message, 0, len(items))
	for i, itemRaw := range items {
		msg, ok, err := codexInputItemToMessage(i, itemRaw)
		if err != nil {
			return nil, extra, err
		}
		if !ok {
			continue
		}
		messages = append(messages, msg)
	}
	if len(messages) == 0 {
		return nil, extra, nil
	}
	return messages, extra, nil
}

func codexInputItemToMessage(index int, itemRaw json.RawMessage) (types.Message, bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(itemRaw, &fields); err != nil {
		return types.Message{}, false, fmt.Errorf("unmarshal codex input item %d: %w", index, err)
	}

	itemType := rawJSONString(fields["type"])
	role := rawJSONString(fields["role"])
	raw := codexInputItemRaw{Fields: fields, ItemIndex: index, TextIndex: -1}

	if itemType == "response_item" {
		nested, ok, err := codexResponseItemPayloadToMessage(index, fields)
		if err != nil || !ok {
			return nested, ok, err
		}
		return nested, true, nil
	}

	if codexLooksLikeToolOutput(itemType, fields) {
		text, textPath := codexToolOutputText(fields)
		raw.TextPath = textPath
		msg := types.Message{Index: index, Role: "tool"}
		msg.Content = []types.ContentBlock{{
			Type:         "tool_result",
			Text:         text,
			ToolResultID: firstNonEmpty(rawJSONString(fields["call_id"]), rawJSONString(fields["id"])),
			ToolName:     codexToolName(fields),
			ToolInput:    codexToolInput(fields),
			RawBlock:     raw,
		}}
		return msg, true, nil
	}
	if codexLooksLikeToolCall(itemType, fields) {
		msg := types.Message{Index: index, Role: "assistant"}
		msg.Content = []types.ContentBlock{{
			Type:      "tool_use",
			ToolUseID: firstNonEmpty(rawJSONString(fields["call_id"]), rawJSONString(fields["id"])),
			ToolName:  codexToolName(fields),
			ToolInput: codexToolInput(fields),
			RawBlock:  raw,
		}}
		return msg, true, nil
	}

	if role == "" {
		role = codexRoleForInputType(itemType)
	}
	if role == "" {
		return types.Message{}, false, nil
	}

	msg := types.Message{Index: index, Role: role}

	if contentRaw, ok := fields["content"]; ok {
		text, textPath, textIndex := codexTextFromContent(contentRaw)
		if textPath == "" {
			return types.Message{}, false, nil
		}
		raw.TextPath = textPath
		raw.TextIndex = textIndex
		msg.Content = []types.ContentBlock{{Type: "text", Text: text, RawBlock: raw}}
		return msg, true, nil
	}

	return types.Message{}, false, nil
}

func codexResponseItemPayloadToMessage(index int, fields map[string]json.RawMessage) (types.Message, bool, error) {
	payload := fields["payload"]
	if len(payload) == 0 {
		return types.Message{}, false, nil
	}
	msg, ok, err := codexInputItemToMessage(index, payload)
	if err != nil || !ok {
		return msg, ok, err
	}
	for i := range msg.Content {
		raw, ok := msg.Content[i].RawBlock.(codexInputItemRaw)
		if !ok {
			continue
		}
		raw.Fields = fields
		raw.ItemIndex = index
		if raw.TextPath != "" {
			raw.TextPath = "payload:" + raw.TextPath
		}
		msg.Content[i].RawBlock = raw
	}
	return msg, true, nil
}

func codexLooksLikeToolCall(itemType string, fields map[string]json.RawMessage) bool {
	switch itemType {
	case "function_call", "custom_tool_call", "local_shell_call", "shell_call", "tool_call", "mcp_call", "computer_call":
		return true
	}
	if rawJSONString(fields["call_id"]) == "" && rawJSONString(fields["id"]) == "" {
		return false
	}
	if len(fields["arguments"]) != 0 || len(fields["action"]) != 0 || len(fields["input"]) != 0 || len(fields["parameters"]) != 0 {
		return true
	}
	return codexCommandLineFromFields(fields) != ""
}

func codexLooksLikeToolOutput(itemType string, fields map[string]json.RawMessage) bool {
	switch itemType {
	case "function_call_output", "custom_tool_call_output", "local_shell_call_output", "shell_call_output", "tool_result", "tool_output", "mcp_call_output", "computer_call_output":
		return true
	}
	if !strings.HasSuffix(itemType, "_output") && itemType != "output" {
		return false
	}
	_, textPath := codexToolOutputText(fields)
	return textPath != ""
}

func codexToolName(fields map[string]json.RawMessage) string {
	if name := rawJSONString(fields["name"]); name != "" {
		return name
	}
	if codexCommandLineFromFields(fields) != "" {
		return "shell"
	}
	return ""
}

func codexToolInput(fields map[string]json.RawMessage) string {
	for _, key := range []string{"arguments", "input", "action", "parameters"} {
		if len(fields[key]) != 0 {
			return rawJSONText(fields[key])
		}
	}
	obj := make(map[string]json.RawMessage)
	for _, key := range []string{
		"command", "cmd", "command_line", "cmdline", "shell_command", "argv", "args",
		"path", "file_path", "filepath", "absolute_path",
		"workdir", "cwd", "working_directory", "directory",
	} {
		if len(fields[key]) != 0 {
			obj[key] = fields[key]
		}
	}
	if commandLine := codexCommandLineFromFields(fields); commandLine != "" {
		data, _ := json.Marshal(commandLine)
		obj["command"] = data
	}
	if len(obj) == 0 {
		return ""
	}
	data, _ := json.Marshal(obj)
	return string(data)
}

func codexCommandLineFromFields(fields map[string]json.RawMessage) string {
	for _, key := range []string{"cmd", "command_line", "cmdline", "shell_command"} {
		if s := strings.TrimSpace(rawJSONString(fields[key])); s != "" {
			return s
		}
	}
	if s := strings.TrimSpace(rawJSONString(fields["command"])); s != "" {
		return s
	}
	if argv := rawJSONStringArray(fields["command"]); len(argv) > 0 {
		return joinShellArgs(argv)
	}
	for _, key := range []string{"argv", "args"} {
		if argv := rawJSONStringArray(fields[key]); len(argv) > 0 {
			return joinShellArgs(argv)
		}
	}
	for _, key := range []string{"action", "input", "parameters"} {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(fields[key], &obj); err == nil {
			if commandLine := codexCommandLineFromFields(obj); commandLine != "" {
				return commandLine
			}
		}
	}
	return ""
}

func rawJSONStringArray(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	return values
}

func codexToolOutputText(fields map[string]json.RawMessage) (string, string) {
	for _, key := range []string{"output", "aggregated_output", "stdout", "stderr", "text", "content", "result", "tool_response"} {
		raw := fields[key]
		if len(raw) == 0 {
			continue
		}
		if key == "output" {
			text, textPath := codexOutputText(raw)
			if textPath != "" {
				return text, textPath
			}
		}
		if s := rawJSONString(raw); s != "" {
			return s, "field:" + key
		}
		if text, textPath := codexSingleTextPart(raw, key); textPath != "" {
			return text, textPath
		}
		if field, ok := singleCodexOutputTextField(raw); ok {
			return rawJSONString(field.value), "field_object:" + key + ":" + field.name
		}
		if text, textPath, ambiguous := singleCodexNestedTextPart(raw, key); textPath != "" {
			return text, textPath
		} else if ambiguous {
			continue
		}
		if rawJSONArray(raw) {
			continue
		}
		if text := rawJSONText(raw); text != "" {
			return text, "field:" + key
		}
	}
	return "", ""
}

func codexRoleForInputType(itemType string) string {
	switch itemType {
	case "", "message":
		return "user"
	default:
		return ""
	}
}

func codexTextFromContent(raw json.RawMessage) (text string, textPath string, textIndex int) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "", "", -1
	}
	if trimmed[0] == '"' {
		return rawJSONString(raw), "content_string", -1
	}

	var parts []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return string(raw), "", -1
	}

	var texts []string
	rewriteIndex := -1
	for i, part := range parts {
		partType := rawJSONString(part["type"])
		switch partType {
		case "input_text", "output_text", "text":
			if s := rawJSONString(part["text"]); s != "" {
				texts = append(texts, s)
				if rewriteIndex == -1 {
					rewriteIndex = i
				} else {
					rewriteIndex = -2
				}
			}
		}
	}
	if rewriteIndex >= 0 && len(texts) == 1 {
		return texts[0], "content_part_text", rewriteIndex
	}
	return strings.Join(texts, "\n"), "", -1
}

func rawJSONString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func rawJSONText(raw json.RawMessage) string {
	if s := rawJSONString(raw); s != "" {
		return s
	}
	if len(raw) == 0 {
		return ""
	}
	return string(raw)
}

func codexOutputText(raw json.RawMessage) (string, string) {
	if s := rawJSONString(raw); s != "" {
		return s, "output"
	}
	if text, textPath := codexSingleTextPart(raw, "output"); textPath != "" {
		return text, textPath
	}
	fields, ok := singleCodexOutputTextField(raw)
	if ok {
		return rawJSONString(fields.value), "output_field:" + fields.name
	}
	if text, textPath, ambiguous := singleCodexNestedTextPart(raw, "output"); textPath != "" {
		return text, textPath
	} else if ambiguous {
		return "", ""
	}
	if rawJSONArray(raw) {
		return "", ""
	}
	return rawJSONText(raw), "output"
}

func codexSingleTextPart(raw json.RawMessage, field string) (string, string) {
	selected := selectCodexSingleTextPart(raw)
	if !selected.ok {
		return "", ""
	}
	return selected.text, "field_part_text:" + field + ":" + strconv.Itoa(selected.index)
}

type codexTextPartSelection struct {
	text      string
	index     int
	ok        bool
	ambiguous bool
}

func selectCodexSingleTextPart(raw json.RawMessage) codexTextPartSelection {
	if !rawJSONArray(raw) {
		return codexTextPartSelection{}
	}
	var parts []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return codexTextPartSelection{ambiguous: true}
	}
	selectedIndex := -1
	selectedText := ""
	for i, part := range parts {
		partType := rawJSONString(part["type"])
		if partType != "output_text" && partType != "text" && partType != "input_text" {
			continue
		}
		text := rawJSONString(part["text"])
		if strings.TrimSpace(text) == "" {
			continue
		}
		if selectedIndex >= 0 {
			return codexTextPartSelection{ambiguous: true}
		}
		selectedIndex = i
		selectedText = text
	}
	if selectedIndex < 0 {
		return codexTextPartSelection{ambiguous: true}
	}
	return codexTextPartSelection{text: selectedText, index: selectedIndex, ok: true}
}

func singleCodexNestedTextPart(raw json.RawMessage, field string) (string, string, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", "", false
	}
	selectedText := ""
	selectedPath := ""
	ambiguous := false
	for _, nested := range []string{"output", "stdout", "text", "content", "stderr", "result", "tool_response"} {
		value, ok := obj[nested]
		if !ok {
			continue
		}
		selected := selectCodexSingleTextPart(value)
		if selected.ambiguous {
			ambiguous = true
			continue
		}
		if !selected.ok {
			continue
		}
		if selectedPath != "" {
			return "", "", true
		}
		selectedText = selected.text
		selectedPath = "field_object_part_text:" + field + ":" + nested + ":" + strconv.Itoa(selected.index)
	}
	return selectedText, selectedPath, ambiguous
}

func rawJSONArray(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '['
}

type codexOutputField struct {
	name  string
	value json.RawMessage
}

func singleCodexOutputTextField(raw json.RawMessage) (codexOutputField, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return codexOutputField{}, false
	}
	var selected codexOutputField
	for _, key := range []string{"output", "stdout", "text", "content", "stderr", "result", "tool_response"} {
		value, ok := obj[key]
		if !ok {
			continue
		}
		if strings.TrimSpace(rawJSONString(value)) == "" {
			continue
		}
		if selected.name != "" {
			return codexOutputField{}, false
		}
		selected = codexOutputField{name: key, value: value}
	}
	return selected, selected.name != ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func parseAnthropicContent(raw json.RawMessage) ([]types.ContentBlock, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	// Content is either a string or an array of content blocks.
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, err
		}
		return []types.ContentBlock{{Type: "text", Text: text}}, nil
	}

	var blocks []anthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}

	result := make([]types.ContentBlock, 0, len(blocks))
	for _, b := range blocks {
		cb := types.ContentBlock{Type: b.Type}
		switch b.Type {
		case "text":
			cb.Text = b.Text
		case "tool_use":
			cb.ToolName = b.Name
			cb.ToolUseID = b.ID
			if b.Input != nil {
				cb.ToolInput = string(b.Input)
			}
		case "tool_result":
			cb.ToolResultID = b.ToolUseID
			// Content of tool_result can be string or array.
			if b.Content != nil {
				cb.Text = extractTextFromContent(b.Content)
			}
		case "image":
			// Preserve raw source for passthrough.
			cb.ImageSource = b.Source
		}
		if b.CacheControl != nil {
			cb.CacheControl = &types.CacheControl{Type: b.CacheControl.Type}
		}
		// Stash the original block for exact reconstruction.
		cb.RawBlock = b
		result = append(result, cb)
	}
	return result, nil
}

func extractTextFromContent(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return ""
	}
	if trimmed[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
	}
	var blocks []anthropicContentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(raw)
}

func extractOpenAIMessages(messagesRaw json.RawMessage, extra map[string]json.RawMessage) ([]types.Message, map[string]json.RawMessage, error) {
	var rawMsgs []OpenAIMessage
	if err := json.Unmarshal(messagesRaw, &rawMsgs); err != nil {
		return nil, extra, fmt.Errorf("unmarshal openai messages: %w", err)
	}

	messages := make([]types.Message, 0, len(rawMsgs))
	for i, rm := range rawMsgs {
		msg := types.Message{
			Index: i,
			Role:  rm.Role,
		}
		// Content can be string or array of content parts.
		if len(rm.Content) > 0 {
			cb := types.ContentBlock{RawBlock: rm}
			trimmed := bytes.TrimSpace(rm.Content)
			if len(trimmed) > 0 && trimmed[0] == '"' {
				var text string
				if json.Unmarshal(rm.Content, &text) == nil {
					cb.Type = "text"
					cb.Text = text
				}
			} else {
				// Array of OpenAI content parts - treat entire thing as text for compression.
				cb.Type = "text"
				cb.Text = string(rm.Content)
			}
			msg.Content = []types.ContentBlock{cb}
		}
		if len(rm.ToolCalls) > 0 {
			if len(msg.Content) == 0 {
				msg.Content = []types.ContentBlock{{Type: "text", RawBlock: rm}}
			}
			msg.Content = append(msg.Content, parseOpenAIToolCalls(rm.ToolCalls)...)
		}
		// Tool result role.
		if rm.Role == "tool" {
			if len(msg.Content) > 0 {
				msg.Content[0].Type = "tool_result"
				msg.Content[0].ToolResultID = rm.ToolCallID
			}
		}
		messages = append(messages, msg)
	}
	return messages, extra, nil
}

func parseOpenAIToolCalls(raw json.RawMessage) []types.ContentBlock {
	var calls []openAIToolCall
	if err := json.Unmarshal(raw, &calls); err != nil {
		return nil
	}
	blocks := make([]types.ContentBlock, 0, len(calls))
	for _, call := range calls {
		if call.ID == "" || call.Function.Name == "" {
			continue
		}
		blocks = append(blocks, types.ContentBlock{
			Type:      "tool_use",
			ToolUseID: call.ID,
			ToolName:  call.Function.Name,
			ToolInput: call.Function.Arguments,
		})
	}
	return blocks
}

// reconstructBody rebuilds the wire-format request body with compressed messages.
func reconstructBody(provider types.Provider, originalBody []byte, compressed []types.Message) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(originalBody, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal for reconstruct: %w", err)
	}

	switch provider {
	case types.Anthropic:
		msgs, err := messagesToAnthropicJSON(compressed)
		if err != nil {
			return nil, fmt.Errorf("reconstruct anthropic messages: %w", err)
		}
		raw["messages"] = msgs
	case types.OpenAI:
		msgs, err := messagesToOpenAIJSON(compressed)
		if err != nil {
			return nil, fmt.Errorf("reconstruct openai messages: %w", err)
		}
		raw["messages"] = msgs
	case types.CodexChatGPT:
		if _, ok := raw["messages"]; ok {
			msgs, err := messagesToOpenAIJSON(compressed)
			if err != nil {
				return nil, fmt.Errorf("reconstruct codex messages: %w", err)
			}
			raw["messages"] = msgs
			break
		}
		if _, ok := raw["input"]; ok {
			input, err := messagesToCodexInputJSON(raw["input"], compressed)
			if err != nil {
				return nil, fmt.Errorf("reconstruct codex input: %w", err)
			}
			raw["input"] = input
		}
	}

	return json.Marshal(raw)
}

func messagesToAnthropicJSON(messages []types.Message) (json.RawMessage, error) {
	type wireMsg struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}

	wireMsgs := make([]wireMsg, 0, len(messages))
	for _, msg := range messages {
		content, _ := contentBlocksToAnthropicJSON(msg.Content)
		wireMsgs = append(wireMsgs, wireMsg{Role: msg.Role, Content: content})
	}
	return json.Marshal(wireMsgs)
}

func messagesToCodexInputJSON(originalInput json.RawMessage, messages []types.Message) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(originalInput)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		if len(messages) == 0 {
			return originalInput, nil
		}
		return json.Marshal(messages[0].TextContent())
	}

	var originalItems []json.RawMessage
	if err := json.Unmarshal(originalInput, &originalItems); err != nil {
		return nil, err
	}
	items := make([]json.RawMessage, len(originalItems))
	copy(items, originalItems)
	changed := false
	for _, msg := range messages {
		rawItem, ok := firstCodexInputRaw(msg)
		if !ok {
			var err error
			rawItem, ok, err = recoverCodexInputRawFromOriginal(msg, originalItems)
			_ = err // originalItems came from a successfully decoded JSON array.
		}
		if !ok {
			continue
		}
		if rawItem.ItemIndex < 0 || rawItem.ItemIndex >= len(items) {
			continue
		}
		item, err := codexMessageToInputItem(msg, rawItem)
		if err != nil {
			return nil, err
		}
		items[rawItem.ItemIndex] = item
		changed = true
	}
	if !changed {
		return originalInput, nil
	}
	return json.Marshal(items)
}

func recoverCodexInputRawFromOriginal(msg types.Message, originalItems []json.RawMessage) (codexInputItemRaw, bool, error) {
	if msg.Index < 0 || msg.Index >= len(originalItems) {
		return codexInputItemRaw{}, false, nil
	}
	original, ok, err := codexInputItemToMessage(msg.Index, originalItems[msg.Index])
	if err != nil || !ok {
		return codexInputItemRaw{}, false, err
	}
	rawItem, found := firstCodexInputRaw(original)
	return rawItem, found, nil
}

func firstCodexInputRaw(msg types.Message) (codexInputItemRaw, bool) {
	for _, block := range msg.Content {
		if raw, ok := block.RawBlock.(codexInputItemRaw); ok {
			return raw, true
		}
	}
	return codexInputItemRaw{}, false
}

func codexMessageToInputItem(msg types.Message, raw codexInputItemRaw) (json.RawMessage, error) {
	fields := cloneRawMap(raw.Fields)
	text := msg.TextContent()
	if text == "" {
		for _, block := range msg.Content {
			if block.Type == "tool_result" && block.Text != "" {
				text = block.Text
				break
			}
		}
	}

	if nestedPath, ok := strings.CutPrefix(raw.TextPath, "payload:"); ok && nestedPath != "" {
		var payloadFields map[string]json.RawMessage
		if err := json.Unmarshal(fields["payload"], &payloadFields); err != nil {
			return nil, err
		}
		nestedRaw := codexInputItemRaw{
			Fields:    payloadFields,
			ItemIndex: raw.ItemIndex,
			TextPath:  nestedPath,
			TextIndex: raw.TextIndex,
		}
		payload, err := codexMessageToInputItem(msg, nestedRaw)
		if err != nil {
			return nil, err
		}
		fields["payload"] = payload
		return json.Marshal(fields)
	}

	switch raw.TextPath {
	case "content_string":
		data, _ := json.Marshal(text)
		fields["content"] = data
	case "content_part_text":
		var parts []map[string]json.RawMessage
		if err := json.Unmarshal(fields["content"], &parts); err != nil {
			return nil, err
		}
		if raw.TextIndex >= 0 && raw.TextIndex < len(parts) {
			data, _ := json.Marshal(text)
			parts[raw.TextIndex]["text"] = data
			content, _ := json.Marshal(parts)
			fields["content"] = content
		}
	case "output":
		data, _ := json.Marshal(text)
		fields["output"] = data
	default:
		if key, ok := strings.CutPrefix(raw.TextPath, "field:"); ok && key != "" {
			data, _ := json.Marshal(text)
			fields[key] = data
		}
		if rest, ok := strings.CutPrefix(raw.TextPath, "field_object:"); ok && rest != "" {
			key, nested, ok := strings.Cut(rest, ":")
			if ok && key != "" && nested != "" {
				var obj map[string]json.RawMessage
				if err := json.Unmarshal(fields[key], &obj); err != nil {
					return nil, err
				}
				data, _ := json.Marshal(text)
				obj[nested] = data
				output, _ := json.Marshal(obj)
				fields[key] = output
			}
		}
		if key, ok := strings.CutPrefix(raw.TextPath, "output_field:"); ok && key != "" {
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(fields["output"], &obj); err != nil {
				return nil, err
			}
			data, _ := json.Marshal(text)
			obj[key] = data
			output, _ := json.Marshal(obj)
			fields["output"] = output
		}
		if rest, ok := strings.CutPrefix(raw.TextPath, "field_part_text:"); ok && rest != "" {
			key, indexText, ok := strings.Cut(rest, ":")
			if ok && key != "" && indexText != "" {
				index, err := strconv.Atoi(indexText)
				if err != nil {
					return nil, err
				}
				var parts []map[string]json.RawMessage
				if err := json.Unmarshal(fields[key], &parts); err != nil {
					return nil, err
				}
				if index >= 0 && index < len(parts) {
					data, _ := json.Marshal(text)
					parts[index]["text"] = data
					output, _ := json.Marshal(parts)
					fields[key] = output
				}
			}
		}
		if rest, ok := strings.CutPrefix(raw.TextPath, "field_object_part_text:"); ok && rest != "" {
			key, nestedAndIndex, ok := strings.Cut(rest, ":")
			if ok && key != "" && nestedAndIndex != "" {
				nested, indexText, ok := strings.Cut(nestedAndIndex, ":")
				if ok && nested != "" && indexText != "" {
					index, err := strconv.Atoi(indexText)
					if err != nil {
						return nil, err
					}
					var obj map[string]json.RawMessage
					if err := json.Unmarshal(fields[key], &obj); err != nil {
						return nil, err
					}
					var parts []map[string]json.RawMessage
					if err := json.Unmarshal(obj[nested], &parts); err != nil {
						return nil, err
					}
					if index >= 0 && index < len(parts) {
						data, _ := json.Marshal(text)
						parts[index]["text"] = data
						nestedData, _ := json.Marshal(parts)
						obj[nested] = nestedData
						output, _ := json.Marshal(obj)
						fields[key] = output
					}
				}
			}
		}
	}

	return json.Marshal(fields)
}

func cloneRawMap(in map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(in))
	for key, value := range in {
		copied := make([]byte, len(value))
		copy(copied, value)
		out[key] = copied
	}
	return out
}

func contentBlocksToAnthropicJSON(blocks []types.ContentBlock) (json.RawMessage, error) {
	if len(blocks) == 1 && blocks[0].Type == "text" && blocks[0].CacheControl == nil {
		// Simple string content - keep as string for clean output.
		return json.Marshal(blocks[0].Text)
	}

	wireBlocks := make([]map[string]json.RawMessage, 0, len(blocks))
	for _, b := range blocks {
		wb, _ := contentBlockToAnthropicWire(b)
		wireBlocks = append(wireBlocks, wb)
	}
	return json.Marshal(wireBlocks)
}

func contentBlockToAnthropicWire(b types.ContentBlock) (map[string]json.RawMessage, error) {
	// If we have the original raw block and the text hasn't been modified, use it directly.
	if ab, ok := b.RawBlock.(anthropicContentBlock); ok && b.Type != "tool_result" {
		// Reconstruct from the original block ensuring text changes are applied.
		if b.Type == "text" {
			ab.Text = b.Text
		}
		data, _ := json.Marshal(ab)
		var m map[string]json.RawMessage
		_ = json.Unmarshal(data, &m)
		// Apply cache control if set.
		if b.CacheControl != nil {
			ccData, _ := json.Marshal(b.CacheControl)
			m["cache_control"] = ccData
		}
		return m, nil
	}

	m := map[string]json.RawMessage{}
	typeData, _ := json.Marshal(b.Type)
	m["type"] = typeData

	switch b.Type {
	case "text":
		textData, _ := json.Marshal(b.Text)
		m["text"] = textData
	case "tool_use":
		idData, _ := json.Marshal(b.ToolUseID)
		nameData, _ := json.Marshal(b.ToolName)
		m["id"] = idData
		m["name"] = nameData
		if b.ToolInput != "" {
			m["input"] = json.RawMessage(b.ToolInput)
		} else {
			m["input"] = json.RawMessage("{}")
		}
	case "tool_result":
		idData, _ := json.Marshal(b.ToolResultID)
		m["tool_use_id"] = idData
		contentData, _ := json.Marshal(b.Text)
		m["content"] = contentData
	case "image":
		if b.ImageSource != nil {
			srcData, _ := json.Marshal(b.ImageSource)
			m["source"] = srcData
		}
	}

	if b.CacheControl != nil {
		ccData, _ := json.Marshal(b.CacheControl)
		m["cache_control"] = ccData
	}
	return m, nil
}

func messagesToOpenAIJSON(messages []types.Message) (json.RawMessage, error) {
	type wireMsg struct {
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content"`
		ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
		ToolCallID string          `json:"tool_call_id,omitempty"`
		Name       string          `json:"name,omitempty"`
	}

	wireMsgs := make([]wireMsg, 0, len(messages))
	for _, msg := range messages {
		wm := wireMsg{Role: msg.Role}

		if msg.Role == "tool" {
			// Reconstruct tool result message.
			var toolCallID string
			var text string
			for _, b := range msg.Content {
				if b.ToolResultID != "" {
					toolCallID = b.ToolResultID
				}
				if b.Text != "" {
					text = b.Text
				}
			}
			contentData, _ := json.Marshal(text)
			wm.Content = contentData
			wm.ToolCallID = toolCallID
		} else {
			// Use raw block if available.
			if len(msg.Content) > 0 {
				if rawMsg, ok := msg.Content[0].RawBlock.(OpenAIMessage); ok {
					// Reconstruct from original, applying text changes only when the
					// original content shape was a plain JSON string. Array/object
					// content is preserved verbatim so multimodal inputs do not degrade
					// into stringified JSON.
					var textContent string
					for _, b := range msg.Content {
						if b.Text != "" {
							textContent = b.Text
						}
					}
					rawMsg.Role = msg.Role
					trimmed := bytes.TrimSpace(rawMsg.Content)
					if len(trimmed) == 0 || trimmed[0] == '"' {
						rawMsg.Content = json.RawMessage(strconv.AppendQuote(nil, textContent))
					}
					wireMsgs = append(wireMsgs, wireMsg{
						Role:       rawMsg.Role,
						Content:    rawMsg.Content,
						ToolCalls:  rawMsg.ToolCalls,
						ToolCallID: rawMsg.ToolCallID,
						Name:       rawMsg.Name,
					})
					continue
				}
				var parts []string
				for _, b := range msg.Content {
					if b.Text != "" {
						parts = append(parts, b.Text)
					}
				}
				contentData, _ := json.Marshal(strings.Join(parts, "\n"))
				wm.Content = contentData
			}
		}
		wireMsgs = append(wireMsgs, wm)
	}
	return json.Marshal(wireMsgs)
}

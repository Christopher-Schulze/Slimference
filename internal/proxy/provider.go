// Package proxy implements the HTTP reverse proxy with multi-layer compression.
package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/slimference/slimference/internal/types"
)

// detectProvider determines the upstream provider from the HTTP request.
// T66: Codex CLI (ChatGPT subscription product) posts to /backend-api/codex/*
// against chatgpt.com - we recognise that path prefix FIRST so it takes
// precedence over the generic OpenAI fallback. When the path is ambiguous
// (Codex also sends /v1/responses through openai_base_url), the
// User-Agent header disambiguates: Codex's native UA contains "codex",
// Claude Code's UA contains "claude-code".
func detectProvider(path string, body []byte) types.Provider {
	return detectProviderWithUA(path, body, "")
}

// detectProviderWithUA is the UA-aware variant used on the hot path. The
// public detectProvider wraps it with an empty UA for tests that don't care.
func detectProviderWithUA(path string, body []byte, userAgent string) types.Provider {
	if strings.Contains(path, "/backend-api/codex/") {
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
			return nil, extra, nil
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
	if role == "" {
		role = codexRoleForInputType(itemType)
	}
	if role == "" {
		return types.Message{}, false, nil
	}

	raw := codexInputItemRaw{Fields: fields, TextIndex: -1}
	msg := types.Message{Index: index, Role: role}

	switch itemType {
	case "function_call":
		msg.Content = []types.ContentBlock{{
			Type:      "tool_use",
			ToolUseID: firstNonEmpty(rawJSONString(fields["call_id"]), rawJSONString(fields["id"])),
			ToolName:  rawJSONString(fields["name"]),
			ToolInput: rawJSONText(fields["arguments"]),
			RawBlock:  raw,
		}}
		return msg, true, nil
	case "function_call_output":
		raw.TextPath = "output"
		msg.Role = "tool"
		msg.Content = []types.ContentBlock{{
			Type:         "tool_result",
			Text:         rawJSONText(fields["output"]),
			ToolResultID: firstNonEmpty(rawJSONString(fields["call_id"]), rawJSONString(fields["id"])),
			RawBlock:     raw,
		}}
		return msg, true, nil
	}

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

func codexRoleForInputType(itemType string) string {
	switch itemType {
	case "", "message":
		return "user"
	case "function_call":
		return "assistant"
	case "function_call_output":
		return "tool"
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
		msgs, _ := messagesToAnthropicJSON(compressed)
		raw["messages"] = msgs
	case types.OpenAI:
		msgs, _ := messagesToOpenAIJSON(compressed)
		raw["messages"] = msgs
	case types.CodexChatGPT:
		if _, ok := raw["messages"]; ok {
			msgs, _ := messagesToOpenAIJSON(compressed)
			raw["messages"] = msgs
			break
		}
		if _, ok := raw["input"]; ok {
			input, _ := messagesToCodexInputJSON(raw["input"], compressed)
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

	items := make([]json.RawMessage, 0, len(messages))
	for _, msg := range messages {
		rawItem, ok := firstCodexInputRaw(msg)
		if !ok {
			continue
		}
		item, err := codexMessageToInputItem(msg, rawItem)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return originalInput, nil
	}
	return json.Marshal(items)
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

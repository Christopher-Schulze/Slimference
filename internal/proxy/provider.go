// Package proxy implements the HTTP reverse proxy with multi-layer compression.
package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tokenproxy/tokenproxy/internal/types"
)

// detectProvider determines the upstream provider from the HTTP request.
func detectProvider(path string, body []byte) types.Provider {
	if strings.Contains(path, "/messages") {
		return types.Anthropic
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
	Model     string              `json:"model"`
	Messages  []AnthropicMessage  `json:"messages"`
	System    json.RawMessage     `json:"system,omitempty"`
	MaxTokens int                 `json:"max_tokens"`
	Stream    bool                `json:"stream,omitempty"`
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
// It handles both Anthropic and OpenAI wire formats.
func extractMessages(provider types.Provider, body []byte) ([]types.Message, map[string]json.RawMessage, error) {
	// Parse as generic map first to preserve all extra fields.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil, fmt.Errorf("unmarshal body: %w", err)
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
					// Reconstruct from original, applying text changes.
					var textContent string
					for _, b := range msg.Content {
						if b.Text != "" {
							textContent = b.Text
						}
					}
					rawMsg.Content, _ = json.Marshal(textContent)
					rawMsg.Role = msg.Role
					data, _ := json.Marshal(rawMsg)
					var m map[string]json.RawMessage
					_ = json.Unmarshal(data, &m)
					wireData, _ := json.Marshal(m)
					wireMsgs = append(wireMsgs, wireMsg{})
					_ = json.Unmarshal(wireData, &wireMsgs[len(wireMsgs)-1])
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

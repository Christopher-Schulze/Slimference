package outputreduce

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/slimference/slimference/internal/types"
)

type Options struct {
	Enabled             bool
	Profile             string
	CustomDirectivePath string
	SignatureMarker     string
	MaxAddedBytes       int
	TaskShape           TaskShape
}

type Stats struct {
	Applied     bool
	Profile     string
	AddedBytes  int
	AddedTokens int
	Reason      string
	TaskShape   TaskShape
}

func InjectBody(provider types.Provider, body []byte, opts Options) ([]byte, Stats, error) {
	stats := Stats{Reason: "disabled"}
	if !opts.Enabled {
		return body, stats, nil
	}
	configured, err := ParseProfile(opts.Profile)
	if err != nil {
		return body, stats, err
	}
	profile := ResolveProfile(provider, configured)
	stats.Profile = string(profile)
	stats.TaskShape = opts.TaskShape
	if stats.TaskShape == "" {
		stats.TaskShape = DetectTaskShape(provider, body)
	}
	if stats.TaskShape == ShapeExactReply {
		stats.Reason = "exact_reply"
		return body, stats, nil
	}
	if profile == ProfileOff {
		stats.Reason = "noop_profile"
		return body, stats, nil
	}

	directive, err := directiveFromOptions(profile, opts)
	if err != nil {
		return body, stats, err
	}
	if directive == "" {
		stats.Reason = "empty_directive"
		return body, stats, nil
	}
	if opts.MaxAddedBytes > 0 && len(directive) > opts.MaxAddedBytes {
		stats.Reason = "directive_over_cap"
		return body, stats, nil
	}
	if bytes.Contains(body, []byte(markerFromOptions(opts))) {
		stats.Reason = "already_present"
		return body, stats, nil
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return body, stats, fmt.Errorf("parse output-reduce request: %w", err)
	}

	var changed bool
	switch provider {
	case types.Anthropic:
		changed, err = injectAnthropic(root, directive)
	case types.OpenAI:
		changed, err = injectOpenAI(root, directive)
	case types.CodexChatGPT:
		changed, err = injectCodex(root, directive)
	default:
		stats.Reason = "unsupported_provider"
		return body, stats, nil
	}
	if err != nil {
		return body, stats, err
	}
	if !changed {
		stats.Reason = "unsupported_shape"
		return body, stats, nil
	}
	out, _ := json.Marshal(root)
	stats.Applied = true
	stats.Reason = "applied"
	stats.AddedBytes = max(len(out)-len(body), len(directive))
	stats.AddedTokens = estimateTokens(stats.AddedBytes)
	return out, stats, nil
}

func directiveFromOptions(profile Profile, opts Options) (string, error) {
	if strings.TrimSpace(opts.CustomDirectivePath) != "" {
		data, err := os.ReadFile(opts.CustomDirectivePath)
		if err != nil {
			return "", fmt.Errorf("read output-reduce directive: %w", err)
		}
		text := strings.TrimSpace(string(data))
		if text == "" {
			return "", nil
		}
		marker := markerFromOptions(opts)
		if !strings.Contains(text, marker) {
			text = marker + "\n" + text
		}
		return text, nil
	}
	return DirectiveForShape(profile, opts.TaskShape, markerFromOptions(opts)), nil
}

func markerFromOptions(opts Options) string {
	if strings.TrimSpace(opts.SignatureMarker) == "" {
		return DefaultMarker
	}
	return strings.TrimSpace(opts.SignatureMarker)
}

func injectAnthropic(root map[string]json.RawMessage, directive string) (bool, error) {
	if raw, ok := root["system"]; ok && len(bytes.TrimSpace(raw)) > 0 {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			root["system"] = mustJSON(s + "\n\n" + directive)
			return true, nil
		}
		var blocks []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &blocks); err != nil {
			return false, fmt.Errorf("unsupported anthropic system shape: %w", err)
		}
		blocks = append(blocks, textBlock(directive))
		root["system"] = mustJSON(blocks)
		return true, nil
	}
	root["system"] = mustJSON(directive)
	return true, nil
}

func injectOpenAI(root map[string]json.RawMessage, directive string) (bool, error) {
	raw, ok := root["messages"]
	if !ok {
		return false, nil
	}
	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &messages); err != nil {
		return false, fmt.Errorf("parse openai messages: %w", err)
	}
	messages = injectMessageList(messages, directive, "system")
	root["messages"] = mustJSON(messages)
	return true, nil
}

func injectCodex(root map[string]json.RawMessage, directive string) (bool, error) {
	if _, ok := root["messages"]; ok {
		return injectOpenAI(root, directive)
	}
	raw, ok := root["input"]
	if !ok {
		return false, nil
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false, nil
	}
	if trimmed[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		root["input"] = mustJSON([]map[string]json.RawMessage{
			codexMessage("system", directive),
			codexMessage("user", s),
		})
		return true, nil
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return false, fmt.Errorf("parse codex input items: %w", err)
	}
	items = injectCodexMessageList(items, directive)
	root["input"] = mustJSON(items)
	return true, nil
}

func injectMessageList(messages []map[string]json.RawMessage, directive, roleName string) []map[string]json.RawMessage {
	if len(messages) > 0 {
		if role := rawString(messages[0]["role"]); role == "system" || role == "developer" {
			messages[0] = appendToMessageContent(messages[0], directive)
			return messages
		}
	}
	system := map[string]json.RawMessage{
		"role":    mustJSON(roleName),
		"content": mustJSON(directive),
	}
	out := make([]map[string]json.RawMessage, 0, len(messages)+1)
	out = append(out, system)
	out = append(out, messages...)
	return out
}

func appendToMessageContent(msg map[string]json.RawMessage, directive string) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(msg)+1)
	for k, v := range msg {
		out[k] = v
	}
	if raw, ok := msg["content"]; ok {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			out["content"] = mustJSON(s + "\n\n" + directive)
			return out
		}
		var blocks []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &blocks); err == nil {
			blocks = append(blocks, textBlock(directive))
			out["content"] = mustJSON(blocks)
			return out
		}
	}
	out["content"] = mustJSON(directive)
	return out
}

func injectCodexMessageList(messages []map[string]json.RawMessage, directive string) []map[string]json.RawMessage {
	if len(messages) > 0 {
		if role := rawString(messages[0]["role"]); role == "system" || role == "developer" {
			messages[0] = appendToCodexMessageContent(messages[0], directive)
			return messages
		}
	}
	out := make([]map[string]json.RawMessage, 0, len(messages)+1)
	out = append(out, codexMessage("system", directive))
	out = append(out, messages...)
	return out
}

func appendToCodexMessageContent(msg map[string]json.RawMessage, directive string) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(msg)+1)
	for k, v := range msg {
		out[k] = v
	}
	if raw, ok := msg["content"]; ok {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			out["content"] = mustJSON(s + "\n\n" + directive)
			return out
		}
		var blocks []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &blocks); err == nil {
			blocks = append(blocks, codexTextBlock(directive))
			out["content"] = mustJSON(blocks)
			return out
		}
	}
	out["content"] = mustJSON([]map[string]json.RawMessage{codexTextBlock(directive)})
	return out
}

func codexMessage(role, text string) map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"type":    mustJSON("message"),
		"role":    mustJSON(role),
		"content": mustJSON([]map[string]json.RawMessage{codexTextBlock(text)}),
	}
}

func codexTextBlock(text string) map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"type": mustJSON("input_text"),
		"text": mustJSON(text),
	}
}

func textBlock(text string) map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"type": mustJSON("text"),
		"text": mustJSON(text),
	}
}

func rawString(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

func estimateTokens(bytesCount int) int {
	if bytesCount <= 0 {
		return 0
	}
	return (bytesCount + 3) / 4
}

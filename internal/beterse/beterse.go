// Package beterse implements the T169 be-terse system-prompt hint.
// A curated one-line directive ("Reply concisely. No preambles, no
// closing remarks.") is appended to the outbound system prompt for
// sessions assigned to the qualityab treatment cohort. The harness
// auto-rolls-back if treatment-side upstream failures exceed control
// by a configurable delta, so the feature is shippable default-on
// when an operator opts in (default off in v1).
//
// Cohort routing lives in internal/qualityab. This package owns the
// per-provider body rewrite: Anthropic uses `system` (string or
// array of content blocks), OpenAI uses a `system` role message at
// the head of `messages`, and Codex also supports a `system` item at
// the head of Responses-API `input`. These shapes accept an appended
// hint without changing the rest of the payload.
package beterse

import (
	"bytes"
	"encoding/json"

	"github.com/slimference/slimference/internal/types"
)

// DefaultHint is the curated be-terse instruction. Operators can
// override via config; this constant captures the wording we
// validated as having the smallest quality drawdown.
const DefaultHint = "Reply concisely. No preambles, no closing remarks. Show your work directly."

// Result describes the outcome of Inject. Applied=true means the
// outbound body was mutated; FieldUsed names the JSON field we
// edited so callers can log it.
type Result struct {
	Applied   bool
	Provider  types.Provider
	FieldUsed string
	Bytes     int // size of injected hint text
}

// Inject prepends/appends the be-terse hint to the outbound body
// for the given provider. Idempotent: if the hint text is already
// present in the system prompt, the body is returned unchanged.
// Any JSON parse / marshal failure falls back to returning the
// original body with Applied=false.
//
// Pass hint="" to use DefaultHint.
func Inject(provider types.Provider, body []byte, hint string) ([]byte, Result) {
	if hint == "" {
		hint = DefaultHint
	}
	res := Result{Provider: provider}
	if len(body) == 0 {
		return body, res
	}
	switch provider {
	case types.Anthropic:
		out, ok := injectAnthropic(body, hint)
		if ok {
			res.Applied = true
			res.FieldUsed = "system"
			res.Bytes = len(hint)
			return out, res
		}
		return body, res
	case types.OpenAI:
		out, ok := injectOpenAI(body, hint)
		if ok {
			res.Applied = true
			res.FieldUsed = "messages[system]"
			res.Bytes = len(hint)
			return out, res
		}
		return body, res
	case types.CodexChatGPT:
		out, ok := injectOpenAI(body, hint)
		if ok {
			res.Applied = true
			res.FieldUsed = "messages[system]"
			res.Bytes = len(hint)
			return out, res
		}
		out, ok = injectCodexInput(body, hint)
		if ok {
			res.Applied = true
			res.FieldUsed = "input[system]"
			res.Bytes = len(hint)
			return out, res
		}
		return body, res
	}
	return body, res
}

func injectAnthropic(body []byte, hint string) ([]byte, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return body, false
	}
	systemRaw, has := raw["system"]
	if !has {
		// Insert as a new top-level system field.
		s, _ := json.Marshal(hint)
		raw["system"] = s
		out, _ := json.Marshal(raw)
		return out, true
	}
	// Existing system field can be a string or an array of content
	// blocks. Detect via first non-whitespace byte. A non-empty
	// json.RawMessage from a valid JSON object can't be all
	// whitespace, so we treat the byte slice as guaranteed-non-empty.
	trimmed := bytes.TrimSpace(systemRaw)
	switch trimmed[0] {
	case '"':
		var existing string
		// systemRaw came from a successful Unmarshal of a JSON object
		// so re-unmarshalling a string-shaped value cannot fail.
		_ = json.Unmarshal(systemRaw, &existing)
		if bytes.Contains([]byte(existing), []byte(hint)) {
			return body, false
		}
		updated, _ := json.Marshal(existing + "\n\n" + hint)
		raw["system"] = updated
	case '[':
		var blocks []map[string]json.RawMessage
		if err := json.Unmarshal(systemRaw, &blocks); err != nil {
			return body, false
		}
		// Skip if any existing text block already carries the hint.
		for _, blk := range blocks {
			if t, ok := blk["text"]; ok {
				var s string
				if json.Unmarshal(t, &s) == nil && bytes.Contains([]byte(s), []byte(hint)) {
					return body, false
				}
			}
		}
		hintText, _ := json.Marshal(hint)
		appended := map[string]json.RawMessage{
			"type": json.RawMessage(`"text"`),
			"text": hintText,
		}
		blocks = append(blocks, appended)
		newBlocks, _ := json.Marshal(blocks)
		raw["system"] = newBlocks
	default:
		return body, false
	}
	out, _ := json.Marshal(raw)
	return out, true
}

func injectOpenAI(body []byte, hint string) ([]byte, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return body, false
	}
	messagesRaw, ok := raw["messages"]
	if !ok {
		return body, false
	}
	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(messagesRaw, &messages); err != nil {
		return body, false
	}
	hintMsg := map[string]json.RawMessage{
		"role":    json.RawMessage(`"system"`),
		"content": mustMarshalString(hint),
	}
	if len(messages) > 0 {
		// Inspect the head. If it's already a system message we
		// append to its content; otherwise we prepend a new system
		// message.
		head := messages[0]
		if roleRaw, ok := head["role"]; ok {
			var role string
			if json.Unmarshal(roleRaw, &role) == nil && role == "system" {
				if contentRaw, ok := head["content"]; ok {
					var content string
					if err := json.Unmarshal(contentRaw, &content); err == nil {
						if bytes.Contains([]byte(content), []byte(hint)) {
							return body, false
						}
						head["content"] = mustMarshalString(content + "\n\n" + hint)
						newHead, _ := json.Marshal(head)
						messages[0] = map[string]json.RawMessage{}
						_ = json.Unmarshal(newHead, &messages[0])
						newMessages, _ := json.Marshal(messages)
						raw["messages"] = newMessages
						out, _ := json.Marshal(raw)
						return out, true
					}
				}
			}
		}
	}
	// Prepend new system message.
	prepended := append([]map[string]json.RawMessage{hintMsg}, messages...)
	newMessages, _ := json.Marshal(prepended)
	raw["messages"] = newMessages
	out, _ := json.Marshal(raw)
	return out, true
}

func mustMarshalString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

func injectCodexInput(body []byte, hint string) ([]byte, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return body, false
	}
	inputRaw, ok := raw["input"]
	if !ok {
		return body, false
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(inputRaw, &items); err != nil {
		return body, false
	}
	for i, item := range items {
		role := rawString(item["role"])
		if role != "system" {
			continue
		}
		content, ok := rawStringOK(item["content"])
		if !ok {
			continue
		}
		if bytes.Contains([]byte(content), []byte(hint)) {
			return body, false
		}
		item["content"] = mustMarshalString(content + "\n\n" + hint)
		items[i] = item
		nextInput, _ := json.Marshal(items)
		raw["input"] = nextInput
		out, _ := json.Marshal(raw)
		return out, true
	}
	system := map[string]json.RawMessage{
		"type":    json.RawMessage(`"message"`),
		"role":    json.RawMessage(`"system"`),
		"content": mustMarshalString(hint),
	}
	prepended := append([]map[string]json.RawMessage{system}, items...)
	nextInput, _ := json.Marshal(prepended)
	raw["input"] = nextInput
	out, _ := json.Marshal(raw)
	return out, true
}

func rawString(raw json.RawMessage) string {
	s, _ := rawStringOK(raw)
	return s
}

func rawStringOK(raw json.RawMessage) (string, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

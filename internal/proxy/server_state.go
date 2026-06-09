package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// peekUnknownPreviousIDError inspects a 4xx upstream response for the
// T78 recovery signal without losing the body for the caller. The body
// is buffered (capped at 64 KiB so a malicious upstream cannot make us
// allocate unboundedly) and reattached as an io.NopCloser so downstream
// passthrough still works when no recovery is required.
func peekUnknownPreviousIDError(resp *http.Response) (bool, []byte) {
	if resp == nil || resp.StatusCode < 400 || resp.StatusCode >= 500 {
		return false, nil
	}
	const maxPeek = 64 * 1024
	buf, _ := io.ReadAll(io.LimitReader(resp.Body, maxPeek))
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(buf))
	return isUnknownPreviousIDError(resp.StatusCode, buf), buf
}

// extractServerStateKey returns a stable session key from the request
// body for providers that support server-side state (T78). Returns ""
// if nothing usable was found, which signals "do not apply server-state
// rewrite to this request".
func extractServerStateKey(prov types.Provider, body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(body, &generic); err != nil {
		return ""
	}
	switch prov {
	case types.OpenAI:
		if v := nestedString(generic, "metadata", "session_id"); v != "" {
			return v
		}
		if v := nestedString(generic, "metadata", "conversation_id"); v != "" {
			return v
		}
		if v := topString(generic, "previous_response_id"); v != "" {
			return v
		}
	case types.CodexChatGPT:
		if v := topString(generic, "conversation_id"); v != "" {
			return v
		}
		if v := nestedString(generic, "metadata", "conversation_id"); v != "" {
			return v
		}
	}
	return ""
}

// rewriteWithPreviousID strips the message history (except the last
// user turn) and inserts previous_response_id (T78). Returns the
// rewritten body and true on success; returns the original body and
// false if the rewrite cannot be performed safely.
func rewriteWithPreviousID(prov types.Provider, body []byte, prevID string) ([]byte, bool) {
	if prevID == "" || len(body) == 0 {
		return body, false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return body, false
	}
	switch prov {
	case types.OpenAI, types.CodexChatGPT:
		if !shrinkOpenAIToLastUserTurn(m) {
			return body, false
		}
		raw, _ := json.Marshal(prevID)
		m["previous_response_id"] = raw
	default:
		return body, false
	}
	out, _ := json.Marshal(m)
	return out, true
}

// shrinkOpenAIToLastUserTurn rewrites either `messages` (chat
// completions shape) or `input` (Responses shape) to keep only the
// final user turn. Returns false when no usable user turn was found.
func shrinkOpenAIToLastUserTurn(m map[string]json.RawMessage) bool {
	if raw, ok := m["messages"]; ok {
		var arr []json.RawMessage
		if err := json.Unmarshal(raw, &arr); err == nil {
			last := lastUserTurn(arr)
			if last == nil {
				return false
			}
			out, _ := json.Marshal([]json.RawMessage{last})
			m["messages"] = out
			return true
		}
	}
	if raw, ok := m["input"]; ok {
		var arr []json.RawMessage
		if err := json.Unmarshal(raw, &arr); err == nil {
			last := lastUserTurn(arr)
			if last == nil {
				return false
			}
			out, _ := json.Marshal([]json.RawMessage{last})
			m["input"] = out
			return true
		}
		// Plain-string `input` is already minimal: keep it.
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return true
		}
	}
	return false
}

func lastUserTurn(arr []json.RawMessage) json.RawMessage {
	for i := len(arr) - 1; i >= 0; i-- {
		var entry map[string]json.RawMessage
		if err := json.Unmarshal(arr[i], &entry); err != nil {
			continue
		}
		var role string
		if r, ok := entry["role"]; ok {
			_ = json.Unmarshal(r, &role)
		}
		if role == "user" {
			return arr[i]
		}
	}
	return nil
}

// extractResponseID parses an upstream response body for the response
// identifier we should remember as the next anchor (T78).
func extractResponseID(prov types.Provider, body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(body, &generic); err != nil {
		return ""
	}
	switch prov {
	case types.OpenAI:
		if v := topString(generic, "id"); v != "" {
			return v
		}
		if v := topString(generic, "response_id"); v != "" {
			return v
		}
	case types.CodexChatGPT:
		if v := topString(generic, "conversation_id"); v != "" {
			return v
		}
		if v := topString(generic, "id"); v != "" {
			return v
		}
	}
	return ""
}

// isUnknownPreviousIDError detects upstream rejections caused by a
// stale previous_response_id (T78 recovery path). Conservative: only
// 4xx with a clearly identifying signal counts.
func isUnknownPreviousIDError(status int, body []byte) bool {
	if status < 400 || status >= 500 {
		return false
	}
	if len(body) == 0 {
		return false
	}
	low := strings.ToLower(string(body))
	if !strings.Contains(low, "previous_response_id") &&
		!strings.Contains(low, "response not found") &&
		!strings.Contains(low, "conversation not found") &&
		!strings.Contains(low, "unknown response") {
		return false
	}
	return bytes.Contains(body, []byte("error")) ||
		strings.Contains(low, "not found") ||
		strings.Contains(low, "invalid") ||
		strings.Contains(low, "unknown")
}

// topString reads a top-level string field from a generic JSON map.
func topString(m map[string]json.RawMessage, key string) string {
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// nestedString reads a 2-level nested string from a generic JSON map.
func nestedString(m map[string]json.RawMessage, parent, key string) string {
	raw, ok := m[parent]
	if !ok {
		return ""
	}
	var inner map[string]json.RawMessage
	if err := json.Unmarshal(raw, &inner); err != nil {
		return ""
	}
	return topString(inner, key)
}

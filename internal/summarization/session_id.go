package summarization

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/slimference/slimference/internal/types"
)

// ExtractSessionID derives a stable session identifier from request headers
// and body. The strategy is provider-specific with a content-hash fallback
// so two distinct conversations never collide accidentally.
func ExtractSessionID(provider types.Provider, body []byte, headers http.Header) string {
	switch {
	case isAnthropicProvider(provider):
		if id := anthropicSessionID(headers, body); id != "" {
			return "anthropic:" + id
		}
	case isOpenAIProvider(provider):
		if id := openAISessionID(headers, body); id != "" {
			return "openai:" + id
		}
	}
	return contentHashFallback(body)
}

func anthropicSessionID(headers http.Header, body []byte) string {
	if org := headers.Get("anthropic-organization-id"); org != "" {
		if uid := extractMetadataUserID(body); uid != "" {
			return org + ":" + uid
		}
		return org
	}
	if trace := headers.Get("anthropic-trace-id"); trace != "" {
		return trace
	}
	return ""
}

func openAISessionID(headers http.Header, body []byte) string {
	if cid := headers.Get("openai-conversation-id"); cid != "" {
		return cid
	}
	if rid := extractPreviousResponseID(body); rid != "" {
		return rid
	}
	return ""
}

func contentHashFallback(body []byte) string {
	if len(body) == 0 {
		return "empty"
	}
	text := extractFirstUserText(body)
	if text == "" {
		return "empty"
	}
	if len(text) > 200 {
		text = text[:200]
	}
	h := sha256.Sum256([]byte(text))
	return fmt.Sprintf("fh:%x", h[:8])
}

func extractFirstUserText(body []byte) string {
	var req struct {
		Messages []struct {
			Role    string      `json:"role"`
			Content interface{} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}
	for _, m := range req.Messages {
		if m.Role == "user" {
			return contentToString(m.Content)
		}
	}
	return ""
}

func contentToString(v interface{}) string {
	switch c := v.(type) {
	case string:
		return c
	case []interface{}:
		var parts []string
		for _, item := range c {
			if m, ok := item.(map[string]interface{}); ok {
				if t, ok := m["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprintf("%v", v)
	}
}

func extractMetadataUserID(body []byte) string {
	var req struct {
		Metadata struct {
			UserID string `json:"user_id"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}
	return req.Metadata.UserID
}

func extractPreviousResponseID(body []byte) string {
	var req struct {
		PreviousResponseID string `json:"previous_response_id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}
	return req.PreviousResponseID
}

func isAnthropicProvider(p types.Provider) bool {
	return p == types.Anthropic || p == types.CodexChatGPT
}

func isOpenAIProvider(p types.Provider) bool {
	return p == types.OpenAI || p == types.CodexChatGPT
}

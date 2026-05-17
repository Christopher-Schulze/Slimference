// Package upstream resolves the upstream provider and URL for an inbound
// request. The package is intentionally tiny and dependency-light so the
// fail-open sidecar (t164) can import it without dragging the full proxy
// stack (compression, summarization, sessions, tui) into its build.
//
// The detection rules mirror internal/proxy/provider.go.detectProviderWithUA
// and internal/proxy/proxy.go.upstreamURL; once t164 lands and the sidecar
// is wired in, those two sites delegate here instead of duplicating logic.
package upstream

import (
	"encoding/json"
	"strings"

	"github.com/slimference/slimference/internal/types"
)

// Bases holds the per-provider upstream base URLs. The sidecar constructs
// this struct from minimal config; the proxy hands it a slice of its own
// config.UpstreamConfig. Empty strings fall back to the documented
// production defaults.
type Bases struct {
	Anthropic    string
	OpenAI       string
	CodexChatGPT string
}

// DefaultBases mirrors internal/config/defaults.go upstream block. Kept
// here in sync so the sidecar has a safe baseline even before any config
// is loaded.
var DefaultBases = Bases{
	Anthropic:    "https://api.anthropic.com",
	OpenAI:       "https://api.openai.com",
	CodexChatGPT: "https://chatgpt.com",
}

// Detect determines the upstream provider from request shape. The rules
// match internal/proxy/provider.go.detectProviderWithUA verbatim so the
// sidecar and the proxy never disagree on routing.
//
//   - Path-prefix /backend-api[/...] wins (Codex CLI + Codex Desktop).
//   - Path containing /messages routes to Anthropic.
//   - UA containing "codex" pre-empts the generic /chat/completions match,
//     so Codex requests through openai_base_url (which Codex sends to
//     /v1/responses) route to chatgpt.com, not api.openai.com.
//   - Path containing /chat/completions routes to OpenAI.
//   - Body shape fallback: presence of max_tokens without frequency_penalty
//     indicates Anthropic.
//   - Default: OpenAI.
func Detect(path string, body []byte, userAgent string) types.Provider {
	if path == "/backend-api" || strings.HasPrefix(path, "/backend-api/") {
		return types.CodexChatGPT
	}
	if strings.Contains(path, "/messages") {
		return types.Anthropic
	}
	if strings.Contains(strings.ToLower(userAgent), "codex") {
		return types.CodexChatGPT
	}
	if strings.Contains(path, "/chat/completions") {
		return types.OpenAI
	}
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

// BaseURL returns the upstream base URL for the provider, falling back to
// DefaultBases when the user-supplied Bases entry is empty. Trailing
// slashes are stripped so callers can concatenate cleanly.
func BaseURL(provider types.Provider, bases Bases) string {
	base := selectBase(provider, bases)
	if base == "" {
		base = selectBase(provider, DefaultBases)
	}
	return strings.TrimSuffix(base, "/")
}

func selectBase(provider types.Provider, bases Bases) string {
	switch provider {
	case types.Anthropic:
		return bases.Anthropic
	case types.OpenAI:
		return bases.OpenAI
	case types.CodexChatGPT:
		return bases.CodexChatGPT
	default:
		return ""
	}
}

// URL builds the full upstream URL for the given provider, joining the
// base, request path, and raw query. Path is normalised to a leading
// slash. Empty rawQuery omits the query separator entirely.
func URL(provider types.Provider, path, rawQuery string, bases Bases) string {
	base := BaseURL(provider, bases)
	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	full := base + path
	if rawQuery != "" {
		full += "?" + rawQuery
	}
	return full
}

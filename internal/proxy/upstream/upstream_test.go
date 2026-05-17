package upstream

import (
	"testing"

	"github.com/slimference/slimference/internal/types"
)

func TestDetect(t *testing.T) {
	cases := []struct {
		name string
		path string
		body []byte
		ua   string
		want types.Provider
	}{
		{"backend-api codex", "/backend-api/codex/responses", nil, "", types.CodexChatGPT},
		{"backend-api bare", "/backend-api", nil, "", types.CodexChatGPT},
		{"backend-api root slash", "/backend-api/", nil, "", types.CodexChatGPT},
		{"backend-api connector", "/backend-api/connector/sessions", nil, "", types.CodexChatGPT},
		{"anthropic messages", "/v1/messages", nil, "", types.Anthropic},
		{"anthropic batched messages", "/v1/messages/batches", nil, "", types.Anthropic},
		{"codex via openai-shaped responses ua", "/v1/responses", nil, "codex-cli/0.5.2", types.CodexChatGPT},
		{"codex desktop ua wins", "/v1/responses", nil, "OpenAI-Codex-Desktop/1.0", types.CodexChatGPT},
		{"openai chat", "/v1/chat/completions", nil, "claude-code/0.1", types.OpenAI},
		{"openai default no path no body", "/", nil, "", types.OpenAI},
		{"anthropic via body shape", "/x", []byte(`{"max_tokens":100,"model":"claude"}`), "", types.Anthropic},
		{"openai via body shape (max_tokens + freq)", "/x", []byte(`{"max_tokens":100,"frequency_penalty":0.5}`), "", types.OpenAI},
		{"openai via body shape garbage", "/x", []byte(`not-json`), "", types.OpenAI},
		{"codex ua mixed case", "/v1/responses", nil, "OpenAI-CODEX-CLI/1.0", types.CodexChatGPT},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Detect(tc.path, tc.body, tc.ua)
			if got != tc.want {
				t.Fatalf("Detect(%q, %q, %q) = %v, want %v", tc.path, string(tc.body), tc.ua, got, tc.want)
			}
		})
	}
}

func TestBaseURL_DefaultFallback(t *testing.T) {
	empty := Bases{}
	cases := []struct {
		provider types.Provider
		want     string
	}{
		{types.Anthropic, "https://api.anthropic.com"},
		{types.OpenAI, "https://api.openai.com"},
		{types.CodexChatGPT, "https://chatgpt.com"},
	}
	for _, tc := range cases {
		got := BaseURL(tc.provider, empty)
		if got != tc.want {
			t.Fatalf("BaseURL(%v, empty) = %q, want %q", tc.provider, got, tc.want)
		}
	}
}

func TestBaseURL_UserOverride(t *testing.T) {
	bases := Bases{
		Anthropic:    "https://example-anthropic.test/",
		OpenAI:       "https://example-openai.test/",
		CodexChatGPT: "https://example-codex.test/",
	}
	if got, want := BaseURL(types.Anthropic, bases), "https://example-anthropic.test"; got != want {
		t.Fatalf("Anthropic override: got %q, want %q", got, want)
	}
	if got, want := BaseURL(types.OpenAI, bases), "https://example-openai.test"; got != want {
		t.Fatalf("OpenAI override: got %q, want %q", got, want)
	}
	if got, want := BaseURL(types.CodexChatGPT, bases), "https://example-codex.test"; got != want {
		t.Fatalf("CodexChatGPT override: got %q, want %q", got, want)
	}
}

func TestBaseURL_UnknownProviderFallsBackToDefault(t *testing.T) {
	got := BaseURL(types.Provider(99), Bases{})
	if got != "" {
		t.Fatalf("unknown provider should yield empty, got %q", got)
	}
}

func TestURL_JoinsParts(t *testing.T) {
	bases := DefaultBases
	cases := []struct {
		name     string
		provider types.Provider
		path     string
		query    string
		want     string
	}{
		{"codex responses", types.CodexChatGPT, "/backend-api/codex/responses", "", "https://chatgpt.com/backend-api/codex/responses"},
		{"codex with query", types.CodexChatGPT, "/backend-api/codex/x", "foo=bar&baz=1", "https://chatgpt.com/backend-api/codex/x?foo=bar&baz=1"},
		{"anthropic messages", types.Anthropic, "/v1/messages", "", "https://api.anthropic.com/v1/messages"},
		{"openai chat", types.OpenAI, "/v1/chat/completions", "", "https://api.openai.com/v1/chat/completions"},
		{"path missing leading slash", types.OpenAI, "v1/chat/completions", "", "https://api.openai.com/v1/chat/completions"},
		{"empty path", types.OpenAI, "", "", "https://api.openai.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := URL(tc.provider, tc.path, tc.query, bases)
			if got != tc.want {
				t.Fatalf("URL(%v, %q, %q) = %q, want %q", tc.provider, tc.path, tc.query, got, tc.want)
			}
		})
	}
}

func TestDefaultBases_MatchesConfigDefaults(t *testing.T) {
	// Acceptance: DefaultBases must mirror internal/config/defaults.go upstream
	// block. If those drift, the sidecar starts routing to the wrong host
	// when run before any config is loaded.
	if DefaultBases.Anthropic != "https://api.anthropic.com" {
		t.Fatalf("Anthropic default drift: %q", DefaultBases.Anthropic)
	}
	if DefaultBases.OpenAI != "https://api.openai.com" {
		t.Fatalf("OpenAI default drift: %q", DefaultBases.OpenAI)
	}
	if DefaultBases.CodexChatGPT != "https://chatgpt.com" {
		t.Fatalf("CodexChatGPT default drift: %q", DefaultBases.CodexChatGPT)
	}
}

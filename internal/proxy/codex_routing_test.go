package proxy

import (
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func TestDetectProvider_CodexBackendPath(t *testing.T) {
	cases := []struct {
		path string
		want types.Provider
	}{
		{"/backend-api/codex/responses", types.CodexChatGPT},
		{"/backend-api/codex/conversations", types.CodexChatGPT},
		{"/backend-api/codex/", types.CodexChatGPT},
		{"/backend-api", types.CodexChatGPT},
		{"/backend-api/", types.CodexChatGPT},
		{"/backend-api/mcp", types.CodexChatGPT},
		{"/backend-api/connectors/tools", types.CodexChatGPT},
		{"/v1/messages", types.Anthropic},
		{"/v1/chat/completions", types.OpenAI},
		{"/unknown", types.OpenAI}, // fallback
	}
	for _, tc := range cases {
		if got := detectProvider(tc.path, nil); got != tc.want {
			t.Errorf("path=%q: got %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestUpstreamURL_CodexRoutesToChatGPT(t *testing.T) {
	p := &Proxy{
		config: &config.Config{
			Upstream: config.UpstreamConfig{
				Anthropic:    config.ProviderUpstream{BaseURL: "https://api.anthropic.com"},
				OpenAI:       config.ProviderUpstream{BaseURL: "https://api.openai.com"},
				CodexChatGPT: config.ProviderUpstream{BaseURL: "https://chatgpt.com"},
			},
		},
	}
	got := p.upstreamURL(types.CodexChatGPT, "/backend-api/codex/responses", "")
	want := "https://chatgpt.com/backend-api/codex/responses"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUpstreamURL_CodexFallbackBaseWhenEmpty(t *testing.T) {
	p := &Proxy{
		config: &config.Config{
			Upstream: config.UpstreamConfig{
				CodexChatGPT: config.ProviderUpstream{BaseURL: ""},
			},
		},
	}
	got := p.upstreamURL(types.CodexChatGPT, "/backend-api/codex/x", "")
	if got != "https://chatgpt.com/backend-api/codex/x" {
		t.Fatalf("empty base did not fall back: %q", got)
	}
}

func TestUpstreamURL_CodexPreservesQuery(t *testing.T) {
	p := &Proxy{
		config: &config.Config{
			Upstream: config.UpstreamConfig{
				CodexChatGPT: config.ProviderUpstream{BaseURL: "https://chatgpt.com"},
			},
		},
	}
	got := p.upstreamURL(types.CodexChatGPT, "/backend-api/codex/x", "foo=bar&baz=1")
	if got != "https://chatgpt.com/backend-api/codex/x?foo=bar&baz=1" {
		t.Fatalf("query not preserved: %q", got)
	}
}

func TestT66_CodexProviderStringAndToggle(t *testing.T) {
	if types.CodexChatGPT.String() != "codex_chatgpt" {
		t.Fatalf("String mismatch: %q", types.CodexChatGPT.String())
	}
	p := &Proxy{}
	p.providerEnabled[types.CodexChatGPT].Store(true)
	if !p.isProviderEnabled(types.CodexChatGPT) {
		t.Fatal("enabled Codex not reported as enabled")
	}
	p.SetProviderEnabled(types.CodexChatGPT, false)
	if p.isProviderEnabled(types.CodexChatGPT) {
		t.Fatal("disabled Codex still reported as enabled")
	}
}

func TestT66_ConfigDefaultCodexBaseURL(t *testing.T) {
	cfg := config.Defaults()
	if cfg.Upstream.CodexChatGPT.BaseURL != "https://chatgpt.com" {
		t.Fatalf("default Codex URL = %q", cfg.Upstream.CodexChatGPT.BaseURL)
	}
}

func TestT66_UserAgentCodexRoutesToChatGPTEvenWithGenericPath(t *testing.T) {
	// Codex via openai_base_url hits /v1/responses. Without UA inspection
	// we would route this to api.openai.com and 401 the OAuth token.
	cases := []struct {
		path string
		ua   string
		want types.Provider
	}{
		// Path alone is ambiguous; UA decides.
		{"/v1/responses", "codex/0.121.0 (rust)", types.CodexChatGPT},
		{"/v1/responses", "Codex-Native/0.121", types.CodexChatGPT},
		// Explicit chat/completions from plain OpenAI client.
		{"/v1/chat/completions", "openai-python/1.25.0", types.OpenAI},
		// Claude UA preserved.
		{"/v1/messages", "claude-code/2.1.114", types.Anthropic},
		// Empty UA falls back to path/body heuristic.
		{"/v1/chat/completions", "", types.OpenAI},
		// Backend-api path still wins regardless of UA (path is the
		// strongest signal).
		{"/backend-api/codex/responses", "curl/8.4", types.CodexChatGPT},
	}
	for _, tc := range cases {
		if got := detectProviderWithUA(tc.path, nil, tc.ua); got != tc.want {
			t.Errorf("path=%q ua=%q: got %v, want %v", tc.path, tc.ua, got, tc.want)
		}
	}
}

func TestT66_EnvOverrideForCodex(t *testing.T) {
	t.Setenv("SLIMFERENCE_UPSTREAM_CODEX_CHATGPT_BASE_URL", "https://example.test")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("SLIMFERENCE_CONFIG", "")
	t.Setenv("HOME", t.TempDir())

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Upstream.CodexChatGPT.BaseURL != "https://example.test" {
		t.Fatalf("env override did not apply: %q", cfg.Upstream.CodexChatGPT.BaseURL)
	}
}

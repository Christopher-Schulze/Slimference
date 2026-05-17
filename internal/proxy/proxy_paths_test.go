package proxy

import (
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

func TestIsCompressiblePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want bool
	}{
		{"/v1/messages", true},
		{"/v1/chat/completions", true},
		{"/v1/responses", true},
		{"/backend-api/codex/responses", true},
		{"/backend-api/codex/responses/", true},
		{"/v1/messages/", true},
		{"/v1/chat/completions/", true},
		{"/v1/messages/batches", false},
		{"/health", false},
		// Voice / audio / realtime / vision paths must NOT be intercepted:
		// Codex desktop app + Claude voice/computer-use should pass through
		// byte-equal so we never break those flows.
		{"/v1/audio/transcriptions", false},
		{"/v1/audio/speech", false},
		{"/v1/audio/translations", false},
		{"/v1/realtime", false},
		{"/v1/images/generations", false},
		{"/v1/embeddings", false},
		{"/v1/files", false},
		{"/v1/threads", false},
		{"/v1/assistants", false},
		// Anthropic non-message endpoints
		{"/v1/messages/count_tokens", false},
		{"/v1/complete", false}, // legacy completion API
		// Codex Desktop App specific non-conversation endpoints under
		// /backend-api/codex/* — these must NOT be compressed because
		// they carry non-message shapes (realtime call setup,
		// model listings, memories, plugin manifests, image gen).
		{"/backend-api/codex/realtime/calls", false},
		{"/backend-api/codex/realtime/calls/abc-123", false},
		{"/backend-api/codex/models", false},
		{"/backend-api/codex/memories/trace_summarize", false},
		{"/backend-api/codex/responses/compact", false},
		{"/backend-api/codex/plugins", false},
		{"/backend-api/codex/plugins/install", false},
		{"/backend-api/codex/images/generations", false},
		// Codex Desktop App WebSocket control plane:
		{"/backend-api/wham/remote/control/server", false},
	}
	for _, tc := range tests {
		if got := isCompressiblePath(tc.path); got != tc.want {
			t.Errorf("%q: got %v want %v", tc.path, got, tc.want)
		}
	}
}

func TestUpstreamURL(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	p := New(cfg)

	u := p.upstreamURL(types.Anthropic, "/v1/messages", "foo=bar")
	if u != "https://api.anthropic.com/v1/messages?foo=bar" {
		t.Fatalf("anthropic url: %q", u)
	}
	u2 := p.upstreamURL(types.OpenAI, "/v1/chat/completions", "")
	if u2 != "https://api.openai.com/v1/chat/completions" {
		t.Fatalf("openai url: %q", u2)
	}

	// Invalid Provider value hits default branch (defensive).
	u3 := p.upstreamURL(types.Provider(99), "/v1/messages", "x=1")
	want := "https://api.anthropic.com/v1/messages?x=1"
	if u3 != want {
		t.Fatalf("unknown provider default base: got %q want %q", u3, want)
	}
}

func TestIsProviderCompressiblePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		provider types.Provider
		path     string
		want     bool
	}{
		{types.Anthropic, "/v1/messages", true},
		{types.Anthropic, "/v1/responses", false},
		{types.OpenAI, "/v1/chat/completions", true},
		{types.OpenAI, "/v1/responses", false},
		{types.CodexChatGPT, "/v1/responses", true},
		{types.CodexChatGPT, "/backend-api/codex/responses", true},
		{types.CodexChatGPT, "/backend-api/codex/responses/", true},
		{types.CodexChatGPT, "/v1/chat/completions", false},
		// Codex Desktop App sidebands - all untouched:
		{types.CodexChatGPT, "/backend-api/codex/realtime/calls", false},
		{types.CodexChatGPT, "/backend-api/codex/models", false},
		{types.CodexChatGPT, "/backend-api/codex/memories/trace_summarize", false},
		{types.CodexChatGPT, "/backend-api/codex/responses/compact", false},
		{types.CodexChatGPT, "/backend-api/codex/plugins", false},
		{types.CodexChatGPT, "/backend-api/codex/plugins/install", false},
		{types.CodexChatGPT, "/backend-api/codex/images/generations", false},
		{types.Provider(99), "/v1/messages", false},
	}
	for _, tc := range tests {
		if got := isProviderCompressiblePath(tc.provider, tc.path); got != tc.want {
			t.Errorf("%v %q: got %v want %v", tc.provider, tc.path, got, tc.want)
		}
	}
}

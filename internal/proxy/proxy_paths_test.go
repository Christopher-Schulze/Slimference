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
		{"/v1/messages/", true},
		{"/v1/chat/completions/", true},
		{"/v1/messages/batches", false},
		{"/health", false},
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

package sniroute

import (
	"testing"

	"github.com/slimference/slimference/internal/control/apps"
)

func newResolverWithDefaults(t *testing.T) *Resolver {
	t.Helper()
	m, err := apps.NewManager("")
	if err != nil {
		t.Fatalf("apps manager: %v", err)
	}
	return New(m)
}

func newResolverWithAllOff(t *testing.T) *Resolver {
	t.Helper()
	m, err := apps.NewManager("")
	if err != nil {
		t.Fatal(err)
	}
	_ = m.SetEnabled(apps.AppCodexCLI, false)
	_ = m.SetEnabled(apps.AppCodexDesktop, false)
	_ = m.SetEnabled(apps.AppClaudeCode, false)
	return New(m)
}

func TestRoutesChatGPTConversationPOSTToMITM(t *testing.T) {
	r := newResolverWithDefaults(t)
	d := r.Resolve(Request{
		SNI:       "chatgpt.com",
		Path:      "/backend-api/codex/responses",
		Method:    "POST",
		UserAgent: "codex_cli_rs/0.130.0 (Mac)",
	})
	if d != MITMConversation {
		t.Errorf("got %s want MITM", d)
	}
}

func TestRoutesChatGPTConversationWebSocketToMITM(t *testing.T) {
	r := newResolverWithDefaults(t)
	d := r.Resolve(Request{
		SNI:         "chatgpt.com",
		Path:        "/backend-api/codex/responses",
		Method:      "GET",
		IsWebSocket: true,
		Subprotocol: "responses_websockets=2026-02-06",
		UserAgent:   "codex_cli_rs/0.130.0",
	})
	if d != MITMConversation {
		t.Errorf("WS upgrade should MITM, got %s", d)
	}
}

func TestRoutesChatGPTUnknownSubprotocolToPassthrough(t *testing.T) {
	r := newResolverWithDefaults(t)
	d := r.Resolve(Request{
		SNI:         "chatgpt.com",
		Path:        "/backend-api/codex/responses",
		Method:      "GET",
		IsWebSocket: true,
		Subprotocol: "some_new_protocol_v3",
		UserAgent:   "codex_cli_rs/0.130.0",
	})
	if d != PassthroughTLS {
		t.Errorf("unknown subprotocol should passthrough, got %s", d)
	}
}

func TestRoutesChatGPTConversationGETNoBodyToPassthrough(t *testing.T) {
	r := newResolverWithDefaults(t)
	d := r.Resolve(Request{
		SNI:       "chatgpt.com",
		Path:      "/backend-api/codex/responses",
		Method:    "GET",
		UserAgent: "codex_cli_rs/0.130.0",
	})
	if d != PassthroughTLS {
		t.Errorf("GET on /responses should passthrough, got %s", d)
	}
}

func TestRoutesChatGPTConversationDefaultMethodToMITM(t *testing.T) {
	// Empty method (some callers may not set it on the first
	// request line if we're emitted before parsing) defaults to POST.
	r := newResolverWithDefaults(t)
	d := r.Resolve(Request{
		SNI:       "chatgpt.com",
		Path:      "/backend-api/codex/responses",
		UserAgent: "codex_cli_rs/0.130.0",
	})
	if d != MITMConversation {
		t.Errorf("empty method should default to POST/MITM, got %s", d)
	}
}

func TestRoutesChatGPTSidebandsToPassthrough(t *testing.T) {
	r := newResolverWithDefaults(t)
	sidebands := []string{
		"/backend-api/codex/realtime/calls",
		"/backend-api/codex/realtime/calls/abc-123",
		"/backend-api/codex/models",
		"/backend-api/codex/memories/trace_summarize",
		"/backend-api/codex/responses/compact",
		"/backend-api/codex/images/generations",
		"/backend-api/codex/plugins",
		"/backend-api/codex/plugins/install",
		"/backend-api/codex/analytics-events/events",
		"/backend-api/codex/skills/loader",
	}
	for _, p := range sidebands {
		d := r.Resolve(Request{SNI: "chatgpt.com", Path: p, Method: "POST",
			UserAgent: "codex_cli_rs/0.130.0"})
		if d != PassthroughTLS {
			t.Errorf("%s: got %s, want passthrough", p, d)
		}
	}
}

func TestRoutesChatGPTNonCodexBackendApiToPassthrough(t *testing.T) {
	r := newResolverWithDefaults(t)
	paths := []string{
		"/backend-api/wham/remote/control/server",
		"/backend-api/v1/me",
		"/backend-api/auth/session",
	}
	for _, p := range paths {
		d := r.Resolve(Request{SNI: "chatgpt.com", Path: p, Method: "GET"})
		if d != PassthroughTLS {
			t.Errorf("%s: got %s want passthrough", p, d)
		}
	}
}

func TestRoutesChatGPTWebUIToPassthrough(t *testing.T) {
	r := newResolverWithDefaults(t)
	d := r.Resolve(Request{
		SNI:       "chatgpt.com",
		Path:      "/api/auth/session",
		Method:    "GET",
		UserAgent: "Mozilla/5.0 (Macintosh; Intel)",
	})
	if d != PassthroughTLS {
		t.Errorf("browser web UI should passthrough, got %s", d)
	}
}

func TestRoutesOpenAIChatCompletionsToMITM(t *testing.T) {
	r := newResolverWithDefaults(t)
	d := r.Resolve(Request{
		SNI:       "api.openai.com",
		Path:      "/v1/chat/completions",
		Method:    "POST",
		UserAgent: "codex_cli_rs/0.130.0",
	})
	if d != MITMConversation {
		t.Errorf("got %s want MITM", d)
	}
}

func TestRoutesOpenAIResponsesToMITM(t *testing.T) {
	r := newResolverWithDefaults(t)
	d := r.Resolve(Request{
		SNI:       "api.openai.com",
		Path:      "/v1/responses",
		Method:    "POST",
		UserAgent: "codex_cli_rs/0.130.0",
	})
	if d != MITMConversation {
		t.Errorf("got %s want MITM", d)
	}
}

func TestRoutesOpenAISidebandsToPassthrough(t *testing.T) {
	r := newResolverWithDefaults(t)
	paths := []string{
		"/v1/audio/transcriptions",
		"/v1/audio/speech",
		"/v1/images/generations",
		"/v1/embeddings",
		"/v1/files",
		"/v1/models",
	}
	for _, p := range paths {
		d := r.Resolve(Request{
			SNI: "api.openai.com", Path: p, Method: "POST",
			UserAgent: "codex_cli_rs/0.130.0",
		})
		if d != PassthroughTLS {
			t.Errorf("%s: got %s want passthrough", p, d)
		}
	}
}

func TestRoutesOpenAIGETToPassthrough(t *testing.T) {
	r := newResolverWithDefaults(t)
	d := r.Resolve(Request{
		SNI: "api.openai.com", Path: "/v1/chat/completions", Method: "GET",
		UserAgent: "codex_cli_rs/0.130.0",
	})
	if d != PassthroughTLS {
		t.Errorf("GET should passthrough, got %s", d)
	}
}

func TestRoutesAnthropicMessagesAlwaysPassthrough(t *testing.T) {
	// Claude Code is parked while Slimference is Codex-only. Even an
	// external/stale attempt to enable claude_code must not MITM
	// Anthropic traffic.
	m, _ := apps.NewManager("")
	if err := m.SetEnabled(apps.AppClaudeCode, true); err == nil {
		t.Fatal("precondition: Claude Code enable should be rejected")
	}
	r := New(m)
	d := r.Resolve(Request{
		SNI:       "api.anthropic.com",
		Path:      "/v1/messages",
		Method:    "POST",
		UserAgent: "claude-code/0.18",
	})
	if d != PassthroughTLS {
		t.Errorf("got %s want passthrough", d)
	}
}

func TestRoutesAnthropicMessagesDefaultDisabledPassthrough(t *testing.T) {
	r := newResolverWithDefaults(t)
	d := r.Resolve(Request{
		SNI:       "api.anthropic.com",
		Path:      "/v1/messages",
		Method:    "POST",
		UserAgent: "claude-code/0.18",
	})
	if d != PassthroughTLS {
		t.Errorf("Claude Code off by default → passthrough, got %s", d)
	}
}

func TestRoutesAnthropicBatchesToPassthrough(t *testing.T) {
	r := newResolverWithDefaults(t)
	d := r.Resolve(Request{
		SNI: "api.anthropic.com", Path: "/v1/messages/batches", Method: "POST",
		UserAgent: "claude-code/0.18",
	})
	if d != PassthroughTLS {
		t.Errorf("batches endpoint should passthrough, got %s", d)
	}
}

func TestRoutesAnthropicGETToPassthrough(t *testing.T) {
	r := newResolverWithDefaults(t)
	d := r.Resolve(Request{
		SNI: "api.anthropic.com", Path: "/v1/messages", Method: "GET",
		UserAgent: "claude-code/0.18",
	})
	if d != PassthroughTLS {
		t.Errorf("GET should passthrough, got %s", d)
	}
}

func TestRoutesUnknownHostToPassthrough(t *testing.T) {
	r := newResolverWithDefaults(t)
	d := r.Resolve(Request{SNI: "example.com", Path: "/anything"})
	if d != PassthroughTLS {
		t.Errorf("unknown host should passthrough, got %s", d)
	}
}

func TestRoutesEmptySNIToPassthrough(t *testing.T) {
	r := newResolverWithDefaults(t)
	d := r.Resolve(Request{Path: "/backend-api/codex/responses", Method: "POST"})
	if d != PassthroughTLS {
		t.Errorf("empty SNI should passthrough, got %s", d)
	}
}

func TestRoutesSNICaseInsensitive(t *testing.T) {
	r := newResolverWithDefaults(t)
	d := r.Resolve(Request{
		SNI: "ChatGPT.com", Path: "/backend-api/codex/responses",
		Method: "POST", UserAgent: "codex_cli_rs/0.130.0",
	})
	if d != MITMConversation {
		t.Errorf("uppercase SNI should still match, got %s", d)
	}
}

func TestPerAppToggleDisablesCodexCLI(t *testing.T) {
	m, _ := apps.NewManager("")
	_ = m.SetEnabled(apps.AppCodexCLI, false)
	r := New(m)
	d := r.Resolve(Request{
		SNI: "chatgpt.com", Path: "/backend-api/codex/responses",
		Method: "POST", UserAgent: "codex_cli_rs/0.130.0",
	})
	if d != PassthroughTLS {
		t.Errorf("disabled CLI should passthrough, got %s", d)
	}
}

func TestPerAppToggleDisablesDesktopApp(t *testing.T) {
	m, _ := apps.NewManager("")
	_ = m.SetEnabled(apps.AppCodexDesktop, false)
	r := New(m)
	d := r.Resolve(Request{
		SNI: "chatgpt.com", Path: "/backend-api/codex/responses",
		Method: "POST", UserAgent: "codex_desktop_app/2026.05",
	})
	if d != PassthroughTLS {
		t.Errorf("disabled Desktop App should passthrough, got %s", d)
	}
}

func TestUnknownUAPassthroughEvenOnConversationPath(t *testing.T) {
	r := newResolverWithDefaults(t)
	d := r.Resolve(Request{
		SNI: "chatgpt.com", Path: "/backend-api/codex/responses",
		Method: "POST", UserAgent: "curl/8.0",
	})
	if d != PassthroughTLS {
		t.Errorf("unknown UA must passthrough, got %s", d)
	}
}

func TestNilPolicyAllowsMITM(t *testing.T) {
	// In-process test mode: a Resolver constructed with nil policy
	// shouldn't block conversation routes (callers in tests want to
	// exercise the path without standing up a Manager).
	r := New(nil)
	d := r.Resolve(Request{
		SNI: "chatgpt.com", Path: "/backend-api/codex/responses", Method: "POST",
	})
	if d != MITMConversation {
		t.Errorf("nil policy should not block MITM, got %s", d)
	}
}

func TestAppForViaResolver(t *testing.T) {
	r := newResolverWithDefaults(t)
	id, ok := r.AppFor("codex_cli_rs/0.130.0")
	if !ok || id != apps.AppCodexCLI {
		t.Errorf("AppFor codex UA: (%q,%v)", id, ok)
	}
	_, ok = r.AppFor("Mozilla/5.0")
	if ok {
		t.Errorf("browser UA should not match an app")
	}
}

func TestAppForNilPolicy(t *testing.T) {
	r := New(nil)
	if _, ok := r.AppFor("codex_cli_rs/0.130.0"); ok {
		t.Errorf("nil policy should refuse app attribution")
	}
}

func TestAllOffMakesEverythingPassthrough(t *testing.T) {
	r := newResolverWithAllOff(t)
	cases := []Request{
		{SNI: "chatgpt.com", Path: "/backend-api/codex/responses", Method: "POST",
			UserAgent: "codex_cli_rs/0.130.0"},
		{SNI: "api.openai.com", Path: "/v1/chat/completions", Method: "POST",
			UserAgent: "codex_cli_rs/0.130.0"},
		{SNI: "api.anthropic.com", Path: "/v1/messages", Method: "POST",
			UserAgent: "claude-code/0.18"},
	}
	for _, req := range cases {
		if d := r.Resolve(req); d != PassthroughTLS {
			t.Errorf("%+v: got %s want passthrough", req, d)
		}
	}
}

func TestNormalisePath(t *testing.T) {
	cases := map[string]string{
		"":                              "/",
		"/":                             "/",
		"/a/b":                          "/a/b",
		"/a/b/":                         "/a/b",
		"//a//b//":                      "/a/b",
		"/backend-api/codex/responses/": "/backend-api/codex/responses",
		"/backend-api//codex/responses": "/backend-api/codex/responses",
	}
	for in, want := range cases {
		if got := normalisePath(in); got != want {
			t.Errorf("normalisePath(%q)=%q want %q", in, got, want)
		}
	}
}

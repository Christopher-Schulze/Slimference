package proxy

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/caching"
	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func TestCtxReader_ContextCanceledBeforeRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cr := &ctxReader{ctx: ctx, r: strings.NewReader("hello")}
	_, err := cr.Read(make([]byte, 8))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestProxy_GetProviderHealth_NoMonitor(t *testing.T) {
	p := New(config.Defaults())
	p.healthMon = nil

	info := p.GetProviderHealth(types.Anthropic)
	if info.Status != types.ProviderHealthIdle {
		t.Fatalf("expected idle without monitor, got %v", info.Status)
	}
}

func TestHealthMonitor_idleAfterStaleActivity(t *testing.T) {
	h := newHealthMonitor()
	h.record(types.Anthropic, true)
	h.mu.Lock()
	h.results[types.Anthropic].lastSuccess = time.Now().Add(-6 * time.Minute)
	h.mu.Unlock()

	info := h.getStatus(types.Anthropic)
	if info.Status != types.ProviderHealthIdle {
		t.Fatalf("expected idle after stale activity, got %v", info.Status)
	}
}

func TestProxy_HasListener(t *testing.T) {
	p := New(config.Defaults())
	if p.HasListener() {
		t.Fatal("expected false without bound listener")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	p.listenerMu.Lock()
	p.listener = ln
	p.listenerMu.Unlock()
	if !p.HasListener() {
		t.Fatal("expected true with bound listener")
	}
}

func fillAnalyticsQueue(p *Proxy) {
	for range cap(p.analyticsQueue) {
		p.analyticsQueue <- types.AnalyticsEvent{Type: types.EventRequestProcessed}
	}
}

func TestExtractSessionIDCodexHTTPUsesTurnMetadata(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","previous_response_id":"resp_prev","client_metadata":{"x-codex-turn-metadata":"{\"thread_id\":\"019ea6ca-5279-7200-868e-2efda5e6731d\",\"source\":\"cli\"}"},"input":[]}`)
	got := extractSessionID(types.CodexChatGPT, body, http.Header{})
	if got != "codex-http:019ea6ca-5279-7200-868e-2efda5e6731d" {
		t.Fatalf("session id=%q", got)
	}
	if family := extractClientFamily(types.CodexChatGPT, body, http.Header{}); family != "codex_cli" {
		t.Fatalf("client family=%q", family)
	}
}

func TestExtractSessionIDCodexHTTPUsesStrongThreadFields(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "top level thread id",
			body: `{"thread_id":"thread-top","conversation_id":"conv-worse","client_metadata":{"x-codex-turn-metadata":"{\"thread_id\":\"thread-client\"}"}}`,
			want: "codex-http:thread-top",
		},
		{
			name: "metadata thread id",
			body: `{"metadata":{"thread_id":"thread-meta"},"client_metadata":{"x-codex-turn-metadata":"{\"thread_id\":\"thread-client\"}"}}`,
			want: "codex-http:thread-meta",
		},
		{
			name: "metadata turn metadata",
			body: `{"metadata":{"x-codex-turn-metadata":"{\"thread_id\":\"thread-turn-meta\"}"}}`,
			want: "codex-http:thread-turn-meta",
		},
		{
			name: "client metadata direct session id",
			body: `{"client_metadata":{"session_id":"session-client"}}`,
			want: "codex-http:session-client",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSessionID(types.CodexChatGPT, []byte(tt.body), http.Header{})
			if got != tt.want {
				t.Fatalf("session id=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractSessionIDCodexHTTPUsesStrongThreadHeaders(t *testing.T) {
	headers := http.Header{"X-Codex-Session-Id": []string{"thread-from-header"}}
	body := []byte(`{"model":"gpt-5.5","input":"check the repo"}`)
	got := extractSessionID(types.CodexChatGPT, body, headers)
	if got != "codex-http:thread-from-header" {
		t.Fatalf("session id=%q", got)
	}
}

func TestExtractSessionIDCodexHTTPDoesNotUseUserIDAsThread(t *testing.T) {
	for _, body := range [][]byte{
		[]byte(`{"user_id":"user-top","messages":[{"role":"user","content":"top user only"}]}`),
		[]byte(`{"metadata":{"user_id":"user-meta"},"messages":[{"role":"user","content":"metadata user only"}]}`),
		[]byte(`{"client_metadata":{"user_id":"user-client"},"messages":[{"role":"user","content":"client user only"}]}`),
	} {
		if got := extractCodexHTTPThreadSessionID(body); got != "" {
			t.Fatalf("user_id must not be used as Codex HTTP thread id for %s: %q", body, got)
		}
		if got := extractSessionID(types.CodexChatGPT, body, http.Header{}); strings.HasPrefix(got, "codex-http:") {
			t.Fatalf("user_id-only Codex HTTP body must fall back to non-thread session id for %s: %q", body, got)
		}
	}
}

func TestExtractSessionIDCodexHTTPResponsesInputFallbackIsNotEmpty(t *testing.T) {
	for _, body := range [][]byte{
		[]byte(`{"model":"gpt-5.5","input":"check the repo"}`),
		[]byte(`{"model":"gpt-5.5","input":[{"type":"message","role":"user","content":"check the repo"}]}`),
		[]byte(`{"model":"gpt-5.5","input":[{"type":"message","role":"assistant","content":"ignored"},{"type":"message","role":"user","content":[{"type":"input_text","text":"check the repo"}]}]}`),
	} {
		got := extractSessionID(types.CodexChatGPT, body, http.Header{})
		if !strings.HasPrefix(got, "fh:") {
			t.Fatalf("Codex Responses API fallback session id=%q for %s", got, body)
		}
	}
}

func TestExtractClientFamilyCodexHTTPFallbacks(t *testing.T) {
	if family := extractClientFamily(types.CodexChatGPT, []byte(`{"metadata":{"source":"desktop"}}`), http.Header{}); family != "codex_desktop_app" {
		t.Fatalf("metadata family=%q", family)
	}
	headers := http.Header{"User-Agent": []string{"OpenAI-Codex-Desktop/1.0"}}
	if family := extractClientFamily(types.CodexChatGPT, []byte(`{}`), headers); family != "codex_desktop_app" {
		t.Fatalf("ua family=%q", family)
	}
	cliHeaders := http.Header{"User-Agent": []string{"codex/0.137.0"}}
	if family := extractClientFamily(types.CodexChatGPT, []byte(`{}`), cliHeaders); family != "codex_cli" {
		t.Fatalf("cli ua family=%q", family)
	}
	if family := extractClientFamily(types.CodexChatGPT, []byte(`{`), http.Header{}); family != "codex" {
		t.Fatalf("fallback family=%q", family)
	}
	if family := extractClientFamily(types.OpenAI, []byte(`{}`), headers); family != "" {
		t.Fatalf("non-codex family=%q", family)
	}
}

func TestAdaptiveWindowHeuristics(t *testing.T) {
	if got := resolveWindow(nil, 7, false, 0, 0); got.Size != 7 || got.Min != 3 || got.Max != 12 || !strings.Contains(got.String(), "adaptive disabled") {
		t.Fatalf("disabled adaptive window decision = %+v", got)
	}
	if got := resolveWindow([]types.Message{{Role: "user"}}, 4, true, 2, 9); got.Size != 4 || got.Reason != "too few messages" {
		t.Fatalf("too-few adaptive window decision = %+v", got)
	}

	low := make([]types.Message, 8)
	for i := range low {
		low[i] = types.Message{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "plain output"}}}
	}
	if got := resolveWindow(low, 3, true, 4, 12); got.Size != 4 || got.Reason != "clamped to min" {
		t.Fatalf("low-complexity adaptive window decision = %+v", got)
	}

	high := make([]types.Message, 12)
	for i := range high {
		high[i] = types.Message{
			Role: "assistant",
			Content: []types.ContentBlock{
				{Type: "tool_use", ToolName: "edit_file_" + string(rune('a'+i)), ToolInput: `{"path":"config/file_` + string(rune('a'+i)) + `.toml"}`},
				{Type: "text", Text: "panic in config loader"},
			},
		}
	}
	if got := resolveWindow(high, 8, true, 3, 9); got.Size != 9 || got.Reason != "clamped to max" || got.Score <= 0.5 {
		t.Fatalf("high-complexity adaptive window decision = %+v", got)
	}
}

func TestWindowComplexityHelpers(t *testing.T) {
	messages := []types.Message{
		{
			Role: "user",
			Content: []types.ContentBlock{
				{Type: "text", Text: "yes"},
				{Type: "tool_use", ToolName: "Read", ToolInput: `{"path":"AGENTS.md"}`},
			},
		},
		{
			Role: "assistant",
			Content: []types.ContentBlock{
				{Type: "tool_use", ToolName: "Edit", ToolInput: `{"file_path":"config/app.yaml"}`},
				{Type: "text", Text: "fatal config error"},
			},
		},
	}
	if got := windowComplexityScore(nil); got != 0.5 {
		t.Fatalf("empty complexity score = %v", got)
	}
	if got := normalizeWindowScore(10, 5, 5); got != 0 {
		t.Fatalf("invalid normalize score = %v", got)
	}
	if got := countWindowFilePaths(messages); got != 2 {
		t.Fatalf("file path count = %d", got)
	}
	if got := countWindowToolDiversity(messages); got != 2 {
		t.Fatalf("tool diversity count = %d", got)
	}
	if got := windowAnchorDensity(messages); got != 1 {
		t.Fatalf("anchor density = %v", got)
	}
	if !looksConfigPath("Dockerfile") || !looksConfigPath(".env") || looksConfigPath("src/main.go") {
		t.Fatal("config path detection mismatch")
	}
	for _, block := range []types.ContentBlock{
		{ToolInput: `{"filename":"src/main.go"}`},
		{ToolInput: `{"filepath":"src/other.go"}`},
		{ToolInput: `{"file":"README.md"}`},
	} {
		if path := windowBlockFilePath(block); path == "" {
			t.Fatalf("expected path from block %+v", block)
		}
	}
	if path := windowBlockFilePath(types.ContentBlock{ToolInput: `{"path":123}`}); path != "" {
		t.Fatalf("malformed path parse = %q", path)
	}
	if path := windowBlockFilePath(types.ContentBlock{ToolInput: `{"cmd":"rg \"path\":\"config/app.yaml\" internal"}`}); path != "" {
		t.Fatalf("structured parser must ignore path-looking strings inside command values: %q", path)
	}
	if path := windowBlockFilePath(types.ContentBlock{
		ToolName:  "exec_command",
		ToolInput: `{"command":["bash","-lc","cat docs/todo.md"],"workdir":"/repo/project"}`,
	}); path != "/repo/project/docs/todo.md" {
		t.Fatalf("structured read command path = %q", path)
	}
	if path := windowBlockFilePath(types.ContentBlock{
		ToolName:  "exec_command",
		ToolInput: `{"cmd":"nl -ba internal/proxy/window.go | sed -n '10,20p'","workdir":"/repo/project"}`,
	}); path != "/repo/project/internal/proxy/window.go" {
		t.Fatalf("structured nl/sed read command path = %q", path)
	}
	if path := windowBlockFilePath(types.ContentBlock{
		ToolName:  "Read",
		ToolInput: `{"path":"docs/spec.md","cwd":"/repo/project"}`,
	}); path != "/repo/project/docs/spec.md" {
		t.Fatalf("structured read-tool path = %q", path)
	}
	if path := windowBlockFilePath(types.ContentBlock{ToolInput: `legacy "path": "config/app.toml"`}); path != "config/app.toml" {
		t.Fatalf("legacy scanner fallback path = %q", path)
	}
	if text := messageText(messages[1]); !strings.Contains(text, "fatal config error") {
		t.Fatalf("message text = %q", text)
	}
}

func TestServeHTTP_AnalyticsQueueFullBranches(t *testing.T) {
	t.Run("cache hit", func(t *testing.T) {
		p := New(config.Defaults())
		fillAnalyticsQueue(p)
		body := []byte(`{"model":"claude","temperature":0,"messages":[{"role":"user","content":"cache"}]}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
		sessionID := extractSessionID(types.Anthropic, body, req.Header)
		key := p.responseCache.ComputeRequestKeyWithRoute(types.Anthropic, p.responseCacheEffectiveRouteKey(req, sessionID), body, req.Header)
		p.responseCache.Set(key, &caching.CacheEntry{
			Response:    []byte(`{"ok":true}`),
			Headers:     map[string][]string{"Content-Type": {"application/json"}},
			StatusCode:  http.StatusOK,
			CreatedAt:   time.Now(),
			TokensSaved: 1,
		})
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("cache-hit status = %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("secret redaction", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"id":"ok","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
		}))
		defer upstream.Close()

		cfg := config.Defaults()
		cfg.Upstream.Anthropic.BaseURL = upstream.URL
		cfg.Compression.Layer1Enabled = false
		cfg.Compression.Layer2Enabled = false
		cfg.Secrets.Mode = "redact"

		p := New(cfg)
		fillAnalyticsQueue(p)
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude","messages":[{"role":"user","content":"key AKIAIOSFODNN7EXAMPLE"}]}`))
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("secret status = %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("upstream error and request processed", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Upstream.Anthropic.BaseURL = "http://127.0.0.1:1"
		cfg.Compression.Layer1Enabled = false
		cfg.Compression.Layer2Enabled = false
		cfg.Secrets.Mode = "off"

		p := New(cfg)
		fillAnalyticsQueue(p)
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude","messages":[{"role":"user","content":"x"}]}`))
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("upstream error status = %d body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestDoUpstreamRequest_ContextDoneAndOverflowAnalyticsQueueFull(t *testing.T) {
	t.Run("rate limit context done", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer srv.Close()

		cfg := config.Defaults()
		cfg.Upstream.Anthropic.BaseURL = srv.URL
		p := New(cfg)
		p.httpClients[types.Anthropic] = srv.Client()
		fillAnalyticsQueue(p)

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()
		r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(context.WithValue(ctx, origBodyKey{}, []byte(`{}`)))

		_, err := p.doUpstreamRequest(r, types.Anthropic, []byte(`{}`))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	})

	t.Run("overflow retry analytics queue full", func(t *testing.T) {
		var calls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls++
			if calls == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `context_length_exceeded`)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"ok":true}`)
		}))
		defer srv.Close()

		cfg := config.Defaults()
		cfg.Upstream.Anthropic.BaseURL = srv.URL
		p := New(cfg)
		p.httpClients[types.Anthropic] = srv.Client()
		fillAnalyticsQueue(p)

		orig := []byte(`{"model":"claude","messages":[{"role":"user","content":"hello"}]}`)
		r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(context.WithValue(context.Background(), origBodyKey{}, orig))

		resp, err := p.doUpstreamRequest(r, types.Anthropic, []byte(`{"model":"claude","messages":[]}`))
		if err != nil {
			t.Fatalf("overflow retry failed: %v", err)
		}
		_ = resp.Body.Close()
		if calls != 2 {
			t.Fatalf("expected overflow retry path, got %d calls", calls)
		}
	})
}

func TestHandleCompressibleRequest_ReconstructBodyError(t *testing.T) {
	p := New(config.Defaults())
	orig := reconstructBodyFn
	reconstructBodyFn = func(types.Provider, []byte, []types.Message) ([]byte, error) {
		return nil, errors.New("rebuild boom")
	}
	defer func() {
		reconstructBodyFn = orig
	}()

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude","messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected reconstruct error 500, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDoUpstreamRequest_OverflowFallbackBuildError(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `context_length_exceeded`)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = srv.URL
	p := New(cfg)
	p.httpClients[types.Anthropic] = srv.Client()

	origNewRequest := newRequestWithContextFn
	callCount := 0
	newRequestWithContextFn = func(ctx context.Context, method string, url string, body io.Reader) (*http.Request, error) {
		callCount++
		if callCount >= 2 {
			return nil, errors.New("new request boom")
		}
		return http.NewRequestWithContext(ctx, method, url, body)
	}
	defer func() {
		newRequestWithContextFn = origNewRequest
	}()

	orig := []byte(`{"model":"claude","messages":[{"role":"user","content":"hello"}]}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(context.WithValue(context.Background(), origBodyKey{}, orig))

	_, err := p.doUpstreamRequest(r, types.Anthropic, []byte(`{"model":"claude","messages":[]}`))
	if err == nil || !strings.Contains(err.Error(), "build overflow fallback request") {
		t.Fatalf("expected overflow fallback build error, got %v", err)
	}
}

func TestDoUpstreamRequest_ContextAlreadyCanceled(t *testing.T) {
	p := New(config.Defaults())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
	_, err := p.doUpstreamRequest(r, types.Anthropic, []byte(`{}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected immediate context cancellation, got %v", err)
	}
}

func TestParseRetryAfter_FutureDateCapped(t *testing.T) {
	header := time.Now().Add(2 * time.Minute).UTC().Format(http.TimeFormat)
	got := parseRetryAfter(header)
	if got != 30*time.Second {
		t.Fatalf("expected 30s cap for future http-date, got %v", got)
	}
}

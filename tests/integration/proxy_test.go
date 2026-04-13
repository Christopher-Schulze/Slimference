//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/proxy"
)

const mockAnthropicResponse = `{"id":"msg_01","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude-3-5-sonnet-20241022","stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":2}}`

// newTestProxy creates a Proxy configured against the given upstream URL.
func newTestProxy(t *testing.T, upstreamURL string) *proxy.Proxy {
	t.Helper()
	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstreamURL
	cfg.Upstream.OpenAI.BaseURL = upstreamURL
	cfg.Compression.Layer1Enabled = true
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Secrets.Mode = "off"
	return proxy.New(cfg)
}

// newTestServer creates an httptest.Server that mirrors the real server mux:
// /health -> JSON ok; everything else -> proxy.ServeHTTP.
func newTestServer(t *testing.T, p *proxy.Proxy) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","service":"slimference"}`))
	})
	mux.Handle("/", p)
	return httptest.NewServer(mux)
}

// TestProxy_CompressesLargeConversation verifies that a 15-message request
// results in a shorter body reaching the upstream after Layer 1 compression.
func TestProxy_CompressesLargeConversation(t *testing.T) {
	var receivedBodyLen atomic.Int64

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("upstream: read body: %v", err)
		}
		receivedBodyLen.Store(int64(len(body)))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(mockAnthropicResponse))
	}))
	defer upstream.Close()

	p := newTestProxy(t, upstream.URL)
	srv := newTestServer(t, p)
	defer srv.Close()

	// Build a 15-message payload using tool_result blocks.
	// Layer 1 only compresses tool_result blocks; repeating the same large
	// content across multiple user messages triggers dedup in the compressible
	// prefix (all messages outside the sliding window of 5 user turns).
	filler := strings.Repeat("x", 500)
	type toolResultBlock struct {
		Type      string `json:"type"`
		ToolUseID string `json:"tool_use_id"`
		Content   string `json:"content"`
	}
	type textBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	type request struct {
		Model     string    `json:"model"`
		MaxTokens int       `json:"max_tokens"`
		Messages  []message `json:"messages"`
	}
	msgs := make([]message, 15)
	for i := range msgs {
		if i%2 == 0 {
			// User message: tool_result with identical large content - triggers dedup
			// for repeated occurrences in the compressible prefix.
			raw, _ := json.Marshal([]toolResultBlock{{
				Type:      "tool_result",
				ToolUseID: "toolu_01",
				Content:   filler,
			}})
			msgs[i] = message{Role: "user", Content: raw}
		} else {
			raw, _ := json.Marshal([]textBlock{{Type: "text", Text: "ok"}})
			msgs[i] = message{Role: "assistant", Content: raw}
		}
	}
	payload := request{
		Model:     "claude-3-5-sonnet-20241022",
		MaxTokens: 1024,
		Messages:  msgs,
	}
	originalBody, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/messages", bytes.NewReader(originalBody))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("got status %d: %s", resp.StatusCode, body)
	}

	upstream.Close() // ensure upstream captured the body before we check

	got := receivedBodyLen.Load()
	if got == 0 {
		t.Fatal("upstream received no body")
	}
	if got >= int64(len(originalBody)) {
		t.Errorf("expected compressed body shorter than original (%d), got %d", len(originalBody), got)
	}
}

// TestProxy_PassthroughNonCompressiblePath verifies that requests to non-message
// paths (e.g. /v1/models) are forwarded unchanged to the upstream.
func TestProxy_PassthroughNonCompressiblePath(t *testing.T) {
	const fixedBody = `{"model":"claude-3-5-sonnet-20241022"}`
	var receivedBody []byte

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("upstream: read body: %v", err)
		}
		receivedBody = b
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer upstream.Close()

	p := newTestProxy(t, upstream.URL)
	srv := newTestServer(t, p)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/models", strings.NewReader(fixedBody))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("got status %d: %s", resp.StatusCode, body)
	}

	if string(receivedBody) != fixedBody {
		t.Errorf("body modified: want %q, got %q", fixedBody, receivedBody)
	}
}

// TestProxy_HealthEndpoint verifies that GET /health returns 200 with status ok.
func TestProxy_HealthEndpoint(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p := newTestProxy(t, upstream.URL)
	srv := newTestServer(t, p)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("get /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	status, ok := result["status"].(string)
	if !ok || status != "ok" {
		t.Errorf("want status=ok, got %v", result["status"])
	}
}

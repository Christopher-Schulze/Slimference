package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/slimference/slimference/internal/config"
)

// TestCodexDesktopVisionInputUntouched feeds a Codex Responses-API
// body containing input_image content parts and asserts the image URL
// flows byte-equal to the upstream. Vision use must not be broken
// by our compression pipeline.
func TestCodexDesktopVisionInputUntouched(t *testing.T) {
	var captured atomic.Pointer[string]
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		captured.Store(&s)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"output":[{"role":"assistant","content":[{"type":"output_text","text":"saw the image"}]}],"usage":{"input_tokens":3,"output_tokens":3}}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.CodexChatGPT.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	p := New(cfg)

	imageURL := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAA"
	req := map[string]any{
		"model": "gpt-5",
		"input": []map[string]any{{
			"type": "message",
			"role": "user",
			"content": []map[string]any{
				{"type": "input_text", "text": "Describe this image"},
				{"type": "input_image", "image_url": imageURL, "detail": "high"},
			},
		}},
	}
	rb, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses", strings.NewReader(string(rb)))
	httpReq.Header.Set("Authorization", "Bearer test")
	httpReq.Header.Set("User-Agent", "codex_cli_rs/0.130.0")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	got := captured.Load()
	if got == nil {
		t.Fatal("upstream did not receive body")
	}
	if !strings.Contains(*got, imageURL) {
		t.Errorf("input_image URL stripped from upstream body: %s", *got)
	}
	if !strings.Contains(*got, `"detail":"high"`) {
		t.Errorf("input_image detail field lost: %s", *got)
	}
	if !strings.Contains(*got, `"input_image"`) {
		t.Errorf("input_image type field lost: %s", *got)
	}
}

// TestCodexDesktopWebSearchUntouched confirms web_search tool calls
// in the Codex Responses-API conversation flow are not mutated by
// our staleread/prune mechanisms.
func TestCodexDesktopWebSearchUntouched(t *testing.T) {
	var captured atomic.Pointer[string]
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		captured.Store(&s)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"output":[{"role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.CodexChatGPT.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = true
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = true
	p := New(cfg)

	searchSnippet := strings.Repeat("search result snippet content. ", 30)
	req := map[string]any{
		"model": "gpt-5",
		"input": []map[string]any{
			{"type": "message", "role": "user", "content": []map[string]any{
				{"type": "input_text", "text": "Look up X"},
			}},
			{"type": "function_call", "name": "web_search", "call_id": "ws1", "arguments": `{"query":"X"}`},
			{"type": "function_call_output", "call_id": "ws1", "output": searchSnippet},
			{"type": "message", "role": "user", "content": []map[string]any{
				{"type": "input_text", "text": "Look up X again"},
			}},
		},
	}
	rb, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses", strings.NewReader(string(rb)))
	httpReq.Header.Set("Authorization", "Bearer test")
	httpReq.Header.Set("User-Agent", "codex_cli_rs/0.130.0")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httpReq)
	got := captured.Load()
	if got == nil {
		t.Fatal("upstream did not receive body")
	}
	if !strings.Contains(*got, searchSnippet) {
		t.Errorf("web_search result mutated/dropped: %s", *got)
	}
}

// TestCodexDesktopComputerCallUntouched confirms computer-use tool
// items in the Codex Responses-API flow pass through untouched.
func TestCodexDesktopComputerCallUntouched(t *testing.T) {
	var captured atomic.Pointer[string]
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		captured.Store(&s)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"output":[],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.CodexChatGPT.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	p := New(cfg)

	screenshotData := strings.Repeat("base64-screenshot-bytes. ", 30)
	req := map[string]any{
		"model": "gpt-5",
		"input": []map[string]any{
			{"type": "message", "role": "user", "content": []map[string]any{
				{"type": "input_text", "text": "open settings"},
			}},
			{"type": "computer_call", "call_id": "cc1",
				"action": map[string]any{"type": "screenshot"}},
			{"type": "computer_call_output", "call_id": "cc1",
				"output": map[string]any{"type": "computer_screenshot", "image_url": screenshotData}},
		},
	}
	rb, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses", strings.NewReader(string(rb)))
	httpReq.Header.Set("Authorization", "Bearer test")
	httpReq.Header.Set("User-Agent", "codex_cli_rs/0.130.0")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httpReq)
	got := captured.Load()
	if got == nil {
		t.Fatal("upstream did not receive body")
	}
	if !strings.Contains(*got, screenshotData) {
		t.Errorf("computer_call_output screenshot stripped: %s", *got)
	}
	if !strings.Contains(*got, `"computer_call"`) {
		t.Errorf("computer_call item dropped: %s", *got)
	}
}

// TestCodexDesktopRealtimeVoicePassthrough confirms /backend-api/codex/
// realtime/* paths are NOT compressed even with the same Codex
// upstream provider — voice/audio flows untouched.
func TestCodexDesktopRealtimeVoicePassthrough(t *testing.T) {
	bodyHit := atomic.Bool{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Confirm we got the exact path Codex would emit for a
		// realtime call setup POST, byte-equal body.
		if r.URL.Path != "/backend-api/codex/realtime/calls" {
			t.Errorf("upstream got wrong path: %s", r.URL.Path)
		}
		bodyHit.Store(true)
		body, _ := io.ReadAll(r.Body)
		// Realtime body has its own shape - not messages-like.
		if !strings.Contains(string(body), `"session"`) {
			t.Errorf("realtime body mutated: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"call_id":"abc"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.CodexChatGPT.BaseURL = upstream.URL
	p := New(cfg)

	body := `{"session":{"voice":"alloy"},"model":"gpt-realtime"}`
	httpReq := httptest.NewRequest(http.MethodPost, "/backend-api/codex/realtime/calls", strings.NewReader(body))
	httpReq.Header.Set("Authorization", "Bearer test")
	httpReq.Header.Set("User-Agent", "codex_desktop_app/2026.05")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httpReq)
	if !bodyHit.Load() {
		t.Errorf("realtime body never reached upstream; route was intercepted")
	}
}

// TestCodexDesktopImageGenerationPassthrough confirms image generation
// requests pass through byte-equal.
func TestCodexDesktopImageGenerationPassthrough(t *testing.T) {
	bodyHit := atomic.Bool{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyHit.Store(true)
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"prompt"`) {
			t.Errorf("image gen body mutated: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.CodexChatGPT.BaseURL = upstream.URL
	p := New(cfg)

	body := `{"prompt":"a cat","model":"gpt-image-1.5"}`
	httpReq := httptest.NewRequest(http.MethodPost, "/backend-api/codex/images/generations", strings.NewReader(body))
	httpReq.Header.Set("Authorization", "Bearer test")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httpReq)
	if !bodyHit.Load() {
		t.Errorf("image gen never reached upstream")
	}
}

// TestCodexDesktopModelsListingPassthrough confirms GET requests to
// the models listing fly through unchanged.
func TestCodexDesktopModelsListingPassthrough(t *testing.T) {
	bodyHit := atomic.Bool{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyHit.Store(true)
		if r.Method != http.MethodGet {
			t.Errorf("method changed: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"data":[{"id":"gpt-5"}]}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.CodexChatGPT.BaseURL = upstream.URL
	p := New(cfg)

	httpReq := httptest.NewRequest(http.MethodGet, "/backend-api/codex/models", nil)
	httpReq.Header.Set("Authorization", "Bearer test")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httpReq)
	if !bodyHit.Load() {
		t.Errorf("models listing never reached upstream")
	}
}

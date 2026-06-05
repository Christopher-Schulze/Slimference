package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
)

// TestOutstopWiredIntoAnthropicUpstream proves the T165 injection
// reaches the upstream HTTP request when the toggle is on and
// disappears when the toggle is off. The upstream stub captures the
// request body so we can assert on stop_sequences.
func TestOutstopWiredIntoAnthropicUpstream(t *testing.T) {
	var captured atomic.Pointer[string]
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		captured.Store(&s)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"x","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"model":"claude","stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":1}}`)
	}))
	defer upstream.Close()

	t.Run("enabled adds stop_sequences", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Upstream.Anthropic.BaseURL = upstream.URL
		cfg.Compression.Layer1Enabled = false
		cfg.Compression.OutputReduce.StopSequencesEnabled = true
		p := New(cfg)
		req := httptest.NewRequest(http.MethodPost, "/v1/messages",
			strings.NewReader(`{"model":"claude","messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("x-api-key", "test")
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		got := captured.Load()
		if got == nil {
			t.Fatal("upstream did not receive body")
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(*got), &raw); err != nil {
			t.Fatalf("unmarshal upstream body: %v", err)
		}
		seq, ok := raw["stop_sequences"]
		if !ok {
			t.Fatalf("upstream body missing stop_sequences: %s", *got)
		}
		var arr []string
		if err := json.Unmarshal(seq, &arr); err != nil {
			t.Fatalf("decode stop_sequences: %v", err)
		}
		if len(arr) == 0 {
			t.Fatal("stop_sequences empty")
		}
		hasNewlineAnchored := false
		for _, e := range arr {
			if strings.HasPrefix(e, "\n") {
				hasNewlineAnchored = true
				break
			}
		}
		if !hasNewlineAnchored {
			t.Errorf("stop_sequences lacks \\n-anchored phrase: %v", arr)
		}
	})

	t.Run("disabled leaves body alone", func(t *testing.T) {
		captured.Store(nil)
		cfg := config.Defaults()
		cfg.Upstream.Anthropic.BaseURL = upstream.URL
		cfg.Compression.Layer1Enabled = false
		cfg.Compression.OutputReduce.StopSequencesEnabled = false
		p := New(cfg)
		req := httptest.NewRequest(http.MethodPost, "/v1/messages",
			strings.NewReader(`{"model":"claude","messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("x-api-key", "test")
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		got := captured.Load()
		if got == nil {
			t.Fatal("upstream did not receive body")
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(*got), &raw); err != nil {
			t.Fatalf("unmarshal upstream body: %v", err)
		}
		if _, present := raw["stop_sequences"]; present {
			t.Errorf("disabled toggle still injected stop_sequences: %s", *got)
		}
	})
}

// TestOutstopWiredIntoOpenAIUpstream proves T165 injection works on the
// OpenAI / Codex wire too, using the `stop` field instead of
// `stop_sequences`.
func TestOutstopWiredIntoOpenAIUpstream(t *testing.T) {
	var captured atomic.Pointer[string]
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		captured.Store(&s)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"x","object":"chat.completion","model":"gpt-5","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.OutputReduce.StopSequencesEnabled = true
	p := New(cfg)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer test")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	got := captured.Load()
	if got == nil {
		t.Fatal("upstream did not receive body")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(*got), &raw); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, *got)
	}
	stopRaw, ok := raw["stop"]
	if !ok {
		t.Fatalf("openai body missing `stop` field: %s", *got)
	}
	if _, dupe := raw["stop_sequences"]; dupe {
		t.Errorf("openai body should not carry anthropic-shaped stop_sequences: %s", *got)
	}
	var arr []string
	if err := json.Unmarshal(stopRaw, &arr); err != nil {
		t.Fatalf("decode stop: %v raw=%s", err, stopRaw)
	}
	if len(arr) == 0 {
		t.Errorf("stop is empty: %s", stopRaw)
	}
}

func TestOutstopWireSkipsOpenAIResponsesShape(t *testing.T) {
	var captured atomic.Pointer[string]
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		captured.Store(&s)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"resp_test","object":"response","output":[]}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.OutputReduce.StopSequencesEnabled = true
	p := New(cfg)
	body := `{"model":"gpt-5","input":"hi"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("User-Agent", "openai-python/2.0")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	got := captured.Load()
	if got == nil {
		t.Fatal("upstream did not receive body")
	}
	if *got != body {
		t.Fatalf("Responses body mutated: got %s want %s", *got, body)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(*got), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := raw["stop"]; present {
		t.Fatalf("Responses body must not carry stop: %s", *got)
	}
	if got := p.OutputReduceCountersSnapshot().StopSeqRequestsModified; got != 0 {
		t.Fatalf("stop counter=%d want 0", got)
	}
}

func TestOutstopWireSkipsCodexResponsesPath(t *testing.T) {
	var captured atomic.Pointer[string]
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		captured.Store(&s)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"resp_codex","object":"response","output":[]}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.CodexChatGPT.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.OutputReduce.StopSequencesEnabled = true
	p := New(cfg)
	body := `{"model":"gpt-5-codex","input":[{"type":"message","role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("User-Agent", "codex/0.130.0")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	got := captured.Load()
	if got == nil {
		t.Fatal("upstream did not receive body")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(*got), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := raw["input"]; !present {
		t.Fatalf("Codex Responses body lost input: %s", *got)
	}
	if _, present := raw["stop"]; present {
		t.Fatalf("Codex Responses body must not carry stop: %s", *got)
	}
	if got := p.OutputReduceCountersSnapshot().StopSeqRequestsModified; got != 0 {
		t.Fatalf("stop counter=%d want 0", got)
	}
}

// TestStreamcutWiredClosesUpstreamOnCommentary proves T166 actually
// closes the upstream body after detecting a trailing-commentary opener
// in the SSE stream. The upstream emits substantial content followed by
// the opener and then a long tail of further commentary; the proxy must
// terminate after the opener.
func TestStreamcutWiredClosesUpstreamOnCommentary(t *testing.T) {
	bodyClosed := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		writeAnthropicDelta := func(text string) {
			b, _ := json.Marshal(text)
			fmt.Fprintf(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", b)
			if flusher != nil {
				flusher.Flush()
			}
		}
		// Realistic: 15-20 small deltas of substantive content before
		// the opener. Holdback drops the last few; the bulk of the
		// substantive content reaches the client.
		for i := 0; i < 18; i++ {
			writeAnthropicDelta("Substantive content line. ")
		}
		writeAnthropicDelta("\nHope this helps with your question.")
		for i := 0; i < 50; i++ {
			writeAnthropicDelta(" more trailing chatter here.")
			time.Sleep(2 * time.Millisecond)
		}
		fmt.Fprintf(w, "data: {\"type\":\"message_stop\"}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		select {
		case bodyClosed <- struct{}{}:
		default:
		}
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.OutputReduce.StreamCutEnabled = true
	p := New(cfg)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"claude","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("x-api-key", "test")
	rec := httptest.NewRecorder()
	start := time.Now()
	p.ServeHTTP(rec, req)
	elapsed := time.Since(start)
	out := rec.Body.String()

	// T184 delay-buffer: opener should NOT reach client (holdback
	// queue drops it on fire).
	if strings.Contains(out, "Hope this helps") {
		t.Errorf("delay-buffer leaked opener to client: %q", out)
	}
	if strings.Count(out, "more trailing chatter") > 2 {
		t.Errorf("trailing chatter not suppressed; cutter never fired. count=%d body=%q", strings.Count(out, "more trailing chatter"), out)
	}
	if !strings.Contains(out, "[DONE]") && !strings.Contains(out, "message_stop") {
		t.Errorf("no synthetic terminator emitted to client: %q", out)
	}
	// Substantive content emitted before the holdback queue filled
	// must reach the client (proves we don't drop too much).
	if !strings.Contains(out, "Substantive content line") {
		t.Errorf("substantive content lost: %q", out)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("relay took %v; cutter likely failed to short-circuit", elapsed)
	}
}

// TestStreamcutDisabledLetsTailThrough confirms the streamcut toggle
// gates the new behaviour: with StreamCutEnabled=false the full tail
// reaches the client.
func TestStreamcutDisabledLetsTailThrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		writeAnthropicDelta := func(text string) {
			b, _ := json.Marshal(text)
			fmt.Fprintf(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", b)
			if flusher != nil {
				flusher.Flush()
			}
		}
		writeAnthropicDelta(strings.Repeat("Substantive content line. ", 4))
		writeAnthropicDelta("\nHope this helps with your question.")
		for i := 0; i < 10; i++ {
			writeAnthropicDelta(" more trailing chatter here.")
		}
		fmt.Fprintf(w, "data: {\"type\":\"message_stop\"}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.OutputReduce.StreamCutEnabled = false
	p := New(cfg)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"claude","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("x-api-key", "test")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	out := rec.Body.String()
	if strings.Count(out, "more trailing chatter") < 5 {
		t.Errorf("disabled toggle still suppressed tail. count=%d", strings.Count(out, "more trailing chatter"))
	}
}

// TestRepdetWiredRewritesAnthropicResponse proves T167 rewrites the
// non-streaming Anthropic response when the prompt contains a
// tool_result block that the model echoes verbatim.
func TestRepdetWiredRewritesAnthropicResponse(t *testing.T) {
	// 400-char tool_result content we'll echo verbatim from the model.
	echoed := strings.Repeat("Echo line content unchanged. ", 14) // ~ 14 * 29 = 406 chars

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Anthropic response containing a text block where the model
		// pastes the same content back.
		resp := map[string]any{
			"id":      "x",
			"type":    "message",
			"role":    "assistant",
			"model":   "claude",
			"content": []map[string]any{{"type": "text", "text": "Here is the file:\n" + echoed + "\nDone."}},
			"usage":   map[string]int{"input_tokens": 5, "output_tokens": 100},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.OutputReduce.RepetitionDetectionEnabled = true
	p := New(cfg)

	// Build a prompt that carries the to-be-echoed block as a tool_result.
	reqBody := map[string]any{
		"model": "claude",
		"messages": []map[string]any{
			{"role": "user", "content": "show me the file"},
			{"role": "assistant", "content": []map[string]any{
				{"type": "tool_use", "id": "tu1", "name": "Read", "input": map[string]any{"path": "x.go"}},
			}},
			{"role": "user", "content": []map[string]any{
				{"type": "tool_result", "tool_use_id": "tu1", "content": echoed},
			}},
		},
	}
	rb, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(rb)))
	req.Header.Set("x-api-key", "test")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, "[unchanged:") {
		t.Errorf("expected [unchanged: …] marker in response, got: %q", body)
	}
	if strings.Contains(body, echoed) {
		t.Errorf("echoed content not replaced; body still contains full block")
	}
}

// TestRepdetWiredRewritesOpenAIResponse proves T183 rewrites the
// non-streaming OpenAI / Codex response when the prompt contains a
// tool_result block that the model echoes verbatim.
func TestRepdetWiredRewritesOpenAIResponse(t *testing.T) {
	echoed := strings.Repeat("Echo line content unchanged. ", 14)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"id":     "x",
			"object": "chat.completion",
			"model":  "gpt-5",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "Here it is:\n" + echoed + "\nDone."},
				"finish_reason": "stop",
			}},
			"usage": map[string]int{"prompt_tokens": 5, "completion_tokens": 100, "total_tokens": 105},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.OutputReduce.RepetitionDetectionEnabled = true
	p := New(cfg)

	reqBody := map[string]any{
		"model": "gpt-5",
		"messages": []map[string]any{
			{"role": "user", "content": "show me the file"},
			{"role": "tool", "tool_call_id": "tu1", "content": echoed},
		},
	}
	rb, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(rb)))
	req.Header.Set("Authorization", "Bearer test")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "[unchanged:") {
		t.Errorf("expected [unchanged: …] marker in response, got: %q", body)
	}
	if strings.Contains(body, echoed) {
		t.Errorf("echoed content not replaced in OpenAI response")
	}
}

// TestRepdetDisabledLeavesBodyIntact confirms the toggle gates the
// rewrite path: with the feature off, the echoed block survives.
func TestRepdetDisabledLeavesBodyIntact(t *testing.T) {
	echoed := strings.Repeat("Echo line content unchanged. ", 14)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"id":      "x",
			"type":    "message",
			"role":    "assistant",
			"model":   "claude",
			"content": []map[string]any{{"type": "text", "text": echoed}},
			"usage":   map[string]int{"input_tokens": 1, "output_tokens": 100},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.OutputReduce.RepetitionDetectionEnabled = false
	p := New(cfg)
	reqBody := map[string]any{
		"model": "claude",
		"messages": []map[string]any{
			{"role": "user", "content": []map[string]any{
				{"type": "tool_result", "tool_use_id": "tu1", "content": echoed},
			}},
		},
	}
	rb, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(rb)))
	req.Header.Set("x-api-key", "test")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, echoed) {
		t.Errorf("disabled toggle still rewrote response: %q", body)
	}
}

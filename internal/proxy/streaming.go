package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

var errUpstreamResponseBodyTooLarge = errors.New("upstream response body too large")

const maxUpstreamResponseBodySize = 10 * 1024 * 1024
const maxSSELineSize = 8 * 1024 * 1024

// ctxReader wraps an io.Reader so that Read respects context cancellation.
// When ctx is cancelled, Read returns ctx.Err() without waiting for the underlying reader.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (cr *ctxReader) Read(p []byte) (n int, err error) {
	select {
	case <-cr.ctx.Done():
		return 0, cr.ctx.Err()
	default:
	}
	type readResult struct {
		n   int
		err error
	}
	ch := make(chan readResult, 1)
	go func() {
		nn, e := cr.r.Read(p)
		ch <- readResult{nn, e}
	}()
	select {
	case <-cr.ctx.Done():
		return 0, cr.ctx.Err()
	case res := <-ch:
		return res.n, res.err
	}
}

// streamingRelay copies a Server-Sent Events stream from upstream to the client.
// It counts output tokens by parsing content delta events and returns the total.
// ctx is the request context; when it is cancelled (e.g. client disconnect) the
// relay exits early without waiting for the upstream to finish.
//
// See streamingRelayWithUsage for a variant that also extracts provider-side
// prompt-cache usage. This thin wrapper keeps the existing call sites and
// tests unchanged.
func streamingRelay(ctx context.Context, w http.ResponseWriter, upstreamResp *http.Response, provider string) (outputTokens int) {
	outputTokens, _ = streamingRelayWithUsage(ctx, w, upstreamResp, provider)
	return outputTokens
}

// cacheUsage captures provider-reported prompt-cache accounting for a single
// request. Anthropic reports cache_read/cache_creation input tokens; OpenAI
// and Codex expose cached input tokens through usage token-details fields.
type cacheUsage struct {
	ReadTokens   int
	CreateTokens int
	// InputTokens is the provider-reported total input token count. Used
	// by T28 to self-calibrate the per-provider tokenizer. Zero when
	// absent.
	InputTokens int
}

// streamingRelayWithUsage is streamingRelay augmented with prompt-cache usage
// extraction. Returns the scanned output token count alongside the cache
// usage. T23: prompt-cache observability.
func streamingRelayWithUsage(ctx context.Context, w http.ResponseWriter, upstreamResp *http.Response, provider string) (outputTokens int, usage cacheUsage) {
	defer upstreamResp.Body.Close()

	// Copy all upstream response headers to client.
	for k, vv := range upstreamResp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(upstreamResp.StatusCode)

	flusher, canFlush := w.(http.Flusher)

	// Wrap the body in a ctxReader so scanner.Scan() unblocks on context cancellation.
	cr := &ctxReader{ctx: ctx, r: upstreamResp.Body}
	scanner := bufio.NewScanner(cr)
	// Allow large SSE events for big tool outputs while still keeping a hard cap.
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineSize)

	for scanner.Scan() {
		line := scanner.Bytes()

		// Write to client immediately.
		if _, err := w.Write(line); err != nil {
			slog.Debug("stream write error", "error", err)
			return outputTokens, usage
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			slog.Debug("stream write error", "error", err)
			return outputTokens, usage
		}
		if canFlush {
			flusher.Flush()
		}

		// Count output tokens from SSE data events.
		outputTokens += extractOutputTokensFromSSE(line, provider)
		switch provider {
		case "anthropic":
			if r, c, in := extractAnthropicCacheUsage(line); r > 0 || c > 0 || in > 0 {
				usage.ReadTokens += r
				usage.CreateTokens += c
				if in > usage.InputTokens {
					usage.InputTokens = in
				}
			}
		case "openai", "codex_chatgpt":
			if r, in := extractOpenAICacheUsage(line); r > 0 || in > 0 {
				usage.ReadTokens += r
				if in > usage.InputTokens {
					usage.InputTokens = in
				}
			}
		}
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			slog.Debug("stream relay stopped: client context done", "reason", err)
		} else if errors.Is(err, bufio.ErrTooLong) {
			slog.Warn("stream scanner: SSE line exceeded buffer limit, relay truncated", "limit_bytes", maxSSELineSize)
		} else {
			slog.Debug("stream scanner error", "error", err)
		}
	}

	return outputTokens, usage
}

// extractAnthropicCacheUsage parses a single SSE line and returns any
// prompt-cache read/create token counts reported by Anthropic alongside
// the total input_tokens (used by T28 for tokenizer self-calibration).
// Fields are surfaced on message_start (initial usage) and message_delta
// (running totals). Zero is returned for lines without usage data.
func extractAnthropicCacheUsage(line []byte) (read, create, input int) {
	if !bytes.HasPrefix(line, []byte("data: ")) {
		return 0, 0, 0
	}
	data := line[6:]
	if bytes.Equal(data, []byte("[DONE]")) {
		return 0, 0, 0
	}
	var ev struct {
		Type    string `json:"type"`
		Message *struct {
			Usage *struct {
				InputTokens              int `json:"input_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			} `json:"usage,omitempty"`
		} `json:"message,omitempty"`
		Usage *struct {
			InputTokens              int `json:"input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return 0, 0, 0
	}
	if ev.Message != nil && ev.Message.Usage != nil {
		read += ev.Message.Usage.CacheReadInputTokens
		create += ev.Message.Usage.CacheCreationInputTokens
		if ev.Message.Usage.InputTokens > input {
			input = ev.Message.Usage.InputTokens
		}
	}
	if ev.Usage != nil {
		read += ev.Usage.CacheReadInputTokens
		create += ev.Usage.CacheCreationInputTokens
		if ev.Usage.InputTokens > input {
			input = ev.Usage.InputTokens
		}
	}
	return read, create, input
}

func extractOpenAICacheUsage(line []byte) (read, input int) {
	if !bytes.HasPrefix(line, []byte("data: ")) {
		return 0, 0
	}
	data := line[6:]
	if bytes.Equal(data, []byte("[DONE]")) {
		return 0, 0
	}
	return extractOpenAICacheUsageFromData(data)
}

func extractOpenAICacheUsageFromData(data []byte) (read, input int) {
	var chunk struct {
		Usage *openAIUsage `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(data, &chunk); err != nil || chunk.Usage == nil {
		return 0, 0
	}
	return chunk.Usage.cachedTokens(), chunk.Usage.inputTokens()
}

type openAIUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	InputTokens         int `json:"input_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	OutputTokens        int `json:"output_tokens"`
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
	InputTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details,omitempty"`
}

func (u openAIUsage) cachedTokens() int {
	cached := 0
	if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens > cached {
		cached = u.PromptTokensDetails.CachedTokens
	}
	if u.InputTokensDetails != nil && u.InputTokensDetails.CachedTokens > cached {
		cached = u.InputTokensDetails.CachedTokens
	}
	return cached
}

func (u openAIUsage) inputTokens() int {
	if u.InputTokens > 0 {
		return u.InputTokens
	}
	return u.PromptTokens
}

func (u openAIUsage) outputTokens() int {
	if u.OutputTokens > 0 {
		return u.OutputTokens
	}
	return u.CompletionTokens
}

// extractAnthropicCacheUsageFromBody parses a non-streaming JSON response
// body and returns the provider-reported cache usage + input_tokens. Zero
// on failure.
func extractAnthropicCacheUsageFromBody(body []byte) cacheUsage {
	if len(body) == 0 {
		return cacheUsage{}
	}
	var resp struct {
		Usage *struct {
			InputTokens              int `json:"input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.Usage == nil {
		return cacheUsage{}
	}
	return cacheUsage{
		ReadTokens:   resp.Usage.CacheReadInputTokens,
		CreateTokens: resp.Usage.CacheCreationInputTokens,
		InputTokens:  resp.Usage.InputTokens,
	}
}

func extractOpenAICacheUsageFromBody(body []byte) cacheUsage {
	if len(body) == 0 {
		return cacheUsage{}
	}
	read, input := extractOpenAICacheUsageFromData(body)
	return cacheUsage{
		ReadTokens:  read,
		InputTokens: input,
	}
}

func extractCacheUsageFromBody(provider string, body []byte) cacheUsage {
	switch provider {
	case "anthropic":
		return extractAnthropicCacheUsageFromBody(body)
	case "openai", "codex_chatgpt":
		return extractOpenAICacheUsageFromBody(body)
	default:
		return cacheUsage{}
	}
}

// passthrough copies a non-streaming response body from upstream to the client.
func passthrough(w http.ResponseWriter, upstreamResp *http.Response) (responseBody []byte) {
	defer upstreamResp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(upstreamResp.Body, maxUpstreamResponseBodySize+1))
	if err != nil {
		slog.Error("read upstream response", "error", err)
		http.Error(w, "upstream read error", http.StatusBadGateway)
		return nil
	}
	if len(body) > maxUpstreamResponseBodySize {
		slog.Error("read upstream response", "error", errUpstreamResponseBodyTooLarge)
		http.Error(w, errUpstreamResponseBodyTooLarge.Error(), http.StatusBadGateway)
		return nil
	}
	for k, vv := range upstreamResp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}

	w.WriteHeader(upstreamResp.StatusCode)
	w.Write(body) //nolint:errcheck
	return body
}

// extractOutputTokensFromSSE parses an SSE line and returns output token delta count.
// It handles both Anthropic and OpenAI streaming formats.
func extractOutputTokensFromSSE(line []byte, provider string) int {
	// SSE lines are: "data: {...}" or "event: ..." or empty.
	if !bytes.HasPrefix(line, []byte("data: ")) {
		return 0
	}
	data := line[6:]
	if bytes.Equal(data, []byte("[DONE]")) {
		return 0
	}

	switch provider {
	case "anthropic":
		return extractAnthropicOutputTokens(data)
	case "openai", "codex_chatgpt":
		return extractOpenAIOutputTokens(data)
	}
	return 0
}

// anthropicUsageEvent is the structure of Anthropic's usage reporting in SSE.
type anthropicUsageEvent struct {
	Type  string `json:"type"`
	Usage *struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage,omitempty"`
	Delta *struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"delta,omitempty"`
}

func extractAnthropicOutputTokens(data []byte) int {
	var ev anthropicUsageEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return 0
	}
	// message_delta event contains the final usage stats.
	if ev.Type == "message_delta" && ev.Usage != nil {
		return ev.Usage.OutputTokens
	}
	// content_block_delta: count text length as approximate token estimate.
	if ev.Type == "content_block_delta" && ev.Delta != nil && ev.Delta.Type == "text_delta" {
		return estimateTokensFromText(ev.Delta.Text)
	}
	return 0
}

// openAIChunk is the structure of an OpenAI streaming chunk.
type openAIChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *openAIUsage `json:"usage,omitempty"`
}

func extractOpenAIOutputTokens(data []byte) int {
	var chunk openAIChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return 0
	}
	// Final chunk may have usage stats.
	if chunk.Usage != nil && chunk.Usage.outputTokens() > 0 {
		return chunk.Usage.outputTokens()
	}
	// Approximate from delta content.
	for _, c := range chunk.Choices {
		if c.Delta.Content != "" {
			return estimateTokensFromText(c.Delta.Content)
		}
	}
	return 0
}

// estimateTokensFromText gives a fast approximation: roughly 4 bytes per token.
func estimateTokensFromText(s string) int {
	n := len(s) / 4
	if n == 0 && len(s) > 0 {
		return 1
	}
	return n
}

// isStreamingRequest checks whether a request body has stream:true.
func isStreamingRequest(body []byte) bool {
	// Fast path: look for "stream":true without full JSON parse.
	if bytes.Contains(body, []byte(`"stream":true`)) {
		return true
	}
	// Full parse fallback.
	var req struct {
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return false
	}
	return req.Stream
}

// extractModel returns the model name from a request body.
func extractModel(body []byte) string {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}
	return req.Model
}

// shortModel returns a short display name for a model.
func shortModel(model string) string {
	switch {
	case strings.Contains(model, "opus"):
		return "opus"
	case strings.Contains(model, "sonnet"):
		return "sonnet"
	case strings.Contains(model, "haiku"):
		return "haiku"
	case strings.Contains(model, "o3"):
		return "o3"
	case strings.Contains(model, "o1"):
		return "o1"
	case strings.Contains(model, "gpt-4"):
		return "gpt4"
	default:
		if len(model) > 12 {
			return model[:12]
		}
		return model
	}
}

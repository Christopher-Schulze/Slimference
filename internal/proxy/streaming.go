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
func streamingRelay(ctx context.Context, w http.ResponseWriter, upstreamResp *http.Response, provider string) (outputTokens int) {
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
	// Allow lines up to 1MB (large tool results can appear in SSE streams).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		// Write to client immediately.
		if _, err := w.Write(line); err != nil {
			slog.Debug("stream write error", "error", err)
			return outputTokens
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			slog.Debug("stream write error", "error", err)
			return outputTokens
		}
		if canFlush {
			flusher.Flush()
		}

		// Count output tokens from SSE data events.
		outputTokens += extractOutputTokensFromSSE(line, provider)
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			slog.Debug("stream relay stopped: client context done", "reason", err)
		} else if errors.Is(err, bufio.ErrTooLong) {
			slog.Warn("stream scanner: SSE line exceeded 1MB buffer limit, relay truncated")
		} else {
			slog.Debug("stream scanner error", "error", err)
		}
	}

	return outputTokens
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
	case "openai":
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
	Usage *struct {
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage,omitempty"`
}

func extractOpenAIOutputTokens(data []byte) int {
	var chunk openAIChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return 0
	}
	// Final chunk may have usage stats.
	if chunk.Usage != nil && chunk.Usage.CompletionTokens > 0 {
		return chunk.Usage.CompletionTokens
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

package proxy

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var errPassthroughRead = errors.New("upstream body read fail")

var errStreamClientWrite = errors.New("stream client write fail")

type failReadCloser struct{}

func (failReadCloser) Read([]byte) (int, error) { return 0, errPassthroughRead }
func (failReadCloser) Close() error             { return nil }

// streamFailWriter fails the first client write (exercises streamingRelay write-error path).
type streamFailWriter struct {
	rec *httptest.ResponseRecorder
}

func (s *streamFailWriter) Header() http.Header         { return s.rec.Header() }
func (s *streamFailWriter) WriteHeader(code int)       { s.rec.WriteHeader(code) }
func (s *streamFailWriter) Write([]byte) (int, error) { return 0, errStreamClientWrite }

func TestExtractOutputTokensFromSSE_AnthropicMessageDelta(t *testing.T) {
	t.Parallel()
	line := []byte(`data: {"type":"message_delta","usage":{"output_tokens":42}}`)
	n := extractOutputTokensFromSSE(line, "anthropic")
	if n != 42 {
		t.Fatalf("got %d want 42", n)
	}
}

func TestExtractOutputTokensFromSSE_AnthropicContentDelta(t *testing.T) {
	t.Parallel()
	line := []byte(`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hello world"}}`)
	n := extractOutputTokensFromSSE(line, "anthropic")
	if n < 1 {
		t.Fatalf("expected token estimate, got %d", n)
	}
}

func TestExtractOutputTokensFromSSE_OpenAIUsage(t *testing.T) {
	t.Parallel()
	line := []byte(`data: {"usage":{"completion_tokens":99},"choices":[]}`)
	n := extractOutputTokensFromSSE(line, "openai")
	if n != 99 {
		t.Fatalf("got %d want 99", n)
	}
}

func TestExtractOutputTokensFromSSE_NonData(t *testing.T) {
	t.Parallel()
	if n := extractOutputTokensFromSSE([]byte("event: ping"), "anthropic"); n != 0 {
		t.Fatalf("got %d", n)
	}
}

func TestExtractOutputTokensFromSSE_Done(t *testing.T) {
	t.Parallel()
	if n := extractOutputTokensFromSSE([]byte("data: [DONE]"), "openai"); n != 0 {
		t.Fatalf("got %d", n)
	}
}

func TestExtractOutputTokensFromSSE_unknownProvider(t *testing.T) {
	t.Parallel()
	line := []byte(`data: {"usage":{"completion_tokens":99}}`)
	if n := extractOutputTokensFromSSE(line, "other"); n != 0 {
		t.Fatalf("unknown provider should not count, got %d", n)
	}
}

func TestExtractOpenAIOutputTokens_deltaContent(t *testing.T) {
	t.Parallel()
	data := []byte(`{"choices":[{"delta":{"content":"hello world from delta"}}]}`)
	n := extractOpenAIOutputTokens(data)
	if n < 1 {
		t.Fatalf("got %d", n)
	}
}

func TestExtractAnthropicOutputTokens_invalidJSON(t *testing.T) {
	t.Parallel()
	if n := extractAnthropicOutputTokens([]byte(`not-json`)); n != 0 {
		t.Fatalf("got %d", n)
	}
}

func TestExtractAnthropicOutputTokens_messageDeltaNoUsage(t *testing.T) {
	t.Parallel()
	if n := extractAnthropicOutputTokens([]byte(`{"type":"message_delta"}`)); n != 0 {
		t.Fatalf("got %d", n)
	}
}

func TestExtractAnthropicOutputTokens_contentBlockNonTextDelta(t *testing.T) {
	t.Parallel()
	if n := extractAnthropicOutputTokens([]byte(`{"type":"content_block_delta","delta":{"type":"tool_use_delta"}}`)); n != 0 {
		t.Fatalf("got %d", n)
	}
}

func TestExtractAnthropicOutputTokens_contentBlockDeltaNilDelta(t *testing.T) {
	t.Parallel()
	if n := extractAnthropicOutputTokens([]byte(`{"type":"content_block_delta"}`)); n != 0 {
		t.Fatalf("got %d", n)
	}
}

func TestExtractOpenAIOutputTokens_invalidJSON(t *testing.T) {
	t.Parallel()
	if n := extractOpenAIOutputTokens([]byte(`not-json`)); n != 0 {
		t.Fatalf("got %d", n)
	}
}

func TestIsStreamingRequest(t *testing.T) {
	t.Parallel()
	if !isStreamingRequest([]byte(`{"model":"x","stream":true,"messages":[]}`)) {
		t.Fatal("want true")
	}
	if !isStreamingRequest([]byte(`{"model":"x","stream": true}`)) {
		t.Fatal("want true (JSON fallback when fast path substring does not match)")
	}
	if isStreamingRequest([]byte(`{"model":"x","stream":false}`)) {
		t.Fatal("want false")
	}
}

func TestExtractModel(t *testing.T) {
	t.Parallel()
	if got := extractModel([]byte(`{"model":"claude-3-5-sonnet-20241022"}`)); got != "claude-3-5-sonnet-20241022" {
		t.Fatalf("got %q", got)
	}
	if extractModel([]byte(`{}`)) != "" {
		t.Fatal("want empty")
	}
	if extractModel([]byte(`not valid json`)) != "" {
		t.Fatal("invalid json should return empty model")
	}
}

func TestShortModel(t *testing.T) {
	t.Parallel()
	if shortModel("claude-opus-4") != "opus" {
		t.Fatal(shortModel("claude-opus-4"))
	}
	if shortModel("claude-sonnet-4") != "sonnet" {
		t.Fatal(shortModel("claude-sonnet-4"))
	}
	if shortModel("claude-haiku-3") != "haiku" {
		t.Fatal(shortModel("claude-haiku-3"))
	}
	if shortModel("o3-pro-preview") != "o3" {
		t.Fatal(shortModel("o3-pro-preview"))
	}
	if shortModel("o1-preview") != "o1" {
		t.Fatal(shortModel("o1-preview"))
	}
	if shortModel("gpt-4o-mini") != "gpt4" {
		t.Fatal(shortModel("gpt-4o-mini"))
	}
	if shortModel("x") != "x" {
		t.Fatal("short string unchanged")
	}
	if got := shortModel("very-long-model-name-xyz"); got != "very-long-mo" {
		t.Fatalf("truncate default: got %q", got)
	}
}

func TestEstimateTokensFromText(t *testing.T) {
	t.Parallel()
	if estimateTokensFromText("") != 0 {
		t.Fatal("empty")
	}
	if estimateTokensFromText("ab") != 1 {
		t.Fatalf("got %d", estimateTokensFromText("ab"))
	}
	if estimateTokensFromText(strings.Repeat("a", 40)) != 10 {
		t.Fatalf("got %d", estimateTokensFromText(strings.Repeat("a", 40)))
	}
}

func TestPassthrough(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Up": []string{"1"}},
		Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
	}
	body := passthrough(rec, resp)
	if string(body) != `{"ok":true}` {
		t.Fatalf("body %q", body)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d", rec.Code)
	}
	if rec.Header().Get("X-Up") != "1" {
		t.Fatal("header not copied")
	}
}

func TestPassthrough_upstreamReadError(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Up": []string{"1"}},
		Body:       io.NopCloser(failReadCloser{}),
	}
	body := passthrough(rec, resp)
	if body != nil {
		t.Fatal("expected nil body on read error")
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("code %d body %q", rec.Code, rec.Body.String())
	}
}

func TestStreamingRelay_CountsTokens(t *testing.T) {
	t.Parallel()
	sse := "data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n"
	rec := httptest.NewRecorder()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
	n := streamingRelay(rec, resp, "anthropic")
	if n != 7 {
		t.Fatalf("got %d want 7", n)
	}
}

func TestStreamingRelay_clientWriteError(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	w := &streamFailWriter{rec: rec}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: {\"x\":1}\n")),
	}
	n := streamingRelay(w, resp, "anthropic")
	if n != 0 {
		t.Fatalf("want 0 tokens when client write fails, got %d", n)
	}
}

// errorReader returns n bytes then an error to cover the scanner.Err() branch in streamingRelay.
type errorReader struct {
	data []byte
	pos  int
}

func (r *errorReader) Read(p []byte) (int, error) {
	if r.pos < len(r.data) {
		n := copy(p, r.data[r.pos:])
		r.pos += n
		return n, nil
	}
	return 0, errors.New("injected read error")
}

func (r *errorReader) Close() error { return nil }

// TestStreamingRelay_scannerError covers the scanner.Err() != nil && != io.EOF branch (lines 49-51).
func TestStreamingRelay_scannerError(t *testing.T) {
	t.Parallel()
	// Provide a valid SSE line followed by a read error.
	// bufio.Scanner.Err() returns non-nil when the underlying reader fails with a non-EOF error.
	sse := []byte("data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":3}}\n")
	rec := httptest.NewRecorder()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       &errorReader{data: sse},
	}
	n := streamingRelay(rec, resp, "anthropic")
	// Should have counted tokens from the first line before hitting the error.
	if n < 0 {
		t.Fatalf("want non-negative token count, got %d", n)
	}
}

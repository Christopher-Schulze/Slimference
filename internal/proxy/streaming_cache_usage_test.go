package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestExtractAnthropicCacheUsage covers every branch of the SSE-line
// prompt-cache usage parser.
func TestExtractAnthropicCacheUsage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		line       string
		wantRead   int
		wantCreate int
	}{
		{"non-data line", "event: message_start\n", 0, 0},
		{"done sentinel", "data: [DONE]", 0, 0},
		{"invalid json", "data: {not json", 0, 0},
		{"message_start usage", `data: {"type":"message_start","message":{"usage":{"cache_read_input_tokens":120,"cache_creation_input_tokens":30}}}`, 120, 30},
		{"message_delta usage", `data: {"type":"message_delta","usage":{"cache_read_input_tokens":5,"cache_creation_input_tokens":11}}`, 5, 11},
		{"combined fields", `data: {"type":"message_start","message":{"usage":{"cache_read_input_tokens":1,"cache_creation_input_tokens":2}},"usage":{"cache_read_input_tokens":3,"cache_creation_input_tokens":4}}`, 4, 6},
		{"no usage", `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}}`, 0, 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, c, _ := extractAnthropicCacheUsage([]byte(tc.line))
			if r != tc.wantRead || c != tc.wantCreate {
				t.Errorf("got read=%d create=%d, want read=%d create=%d", r, c, tc.wantRead, tc.wantCreate)
			}
		})
	}
}

// TestExtractAnthropicCacheUsageFromBody covers all branches of the
// non-streaming JSON response parser.
func TestExtractAnthropicCacheUsageFromBody(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		body       string
		wantRead   int
		wantCreate int
	}{
		{"empty", "", 0, 0},
		{"invalid json", "{oops", 0, 0},
		{"no usage", `{"id":"m1","type":"message"}`, 0, 0},
		{"with usage", `{"id":"m1","usage":{"cache_read_input_tokens":42,"cache_creation_input_tokens":7}}`, 42, 7},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := extractAnthropicCacheUsageFromBody([]byte(tc.body))
			if u.ReadTokens != tc.wantRead || u.CreateTokens != tc.wantCreate {
				t.Errorf("got %+v, want read=%d create=%d", u, tc.wantRead, tc.wantCreate)
			}
		})
	}
}

// TestExtractAnthropicCacheUsage_InputTokens returns the provider-reported
// input_token total alongside cache fields.
func TestExtractAnthropicCacheUsage_InputTokens(t *testing.T) {
	t.Parallel()
	line := `data: {"type":"message_start","message":{"usage":{"input_tokens":1234,"cache_read_input_tokens":5,"cache_creation_input_tokens":7}}}`
	_, _, in := extractAnthropicCacheUsage([]byte(line))
	if in != 1234 {
		t.Fatalf("input_tokens: %d", in)
	}
	// message_delta variant with larger input_tokens.
	line2 := `data: {"type":"message_delta","usage":{"input_tokens":5000}}`
	_, _, in2 := extractAnthropicCacheUsage([]byte(line2))
	if in2 != 5000 {
		t.Fatalf("delta input_tokens: %d", in2)
	}
}

// TestStreamingRelayWithUsage_AggregatesAnthropicCacheFields exercises the
// cache-usage path in the streaming relay itself so the branch is hit during
// a real scan loop.
func TestStreamingRelayWithUsage_AggregatesAnthropicCacheFields(t *testing.T) {
	t.Parallel()
	sse := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"usage":{"cache_read_input_tokens":100,"cache_creation_input_tokens":20}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","usage":{"cache_read_input_tokens":50,"cache_creation_input_tokens":5,"output_tokens":10}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       readCloser(sse),
	}
	rec := httptest.NewRecorder()
	_, usage := streamingRelayWithUsage(context.Background(), rec, resp, "anthropic")
	if usage.ReadTokens != 150 || usage.CreateTokens != 25 {
		t.Fatalf("expected aggregated usage (150, 25), got %+v", usage)
	}
}

// TestStreamingRelayWithUsage_InputTokensMaxWins keeps the larger of two
// input_tokens readings across events.
func TestStreamingRelayWithUsage_InputTokensMaxWins(t *testing.T) {
	t.Parallel()
	sse := strings.Join([]string{
		`data: {"type":"message_start","message":{"usage":{"input_tokens":100}}}`,
		``,
		`data: {"type":"message_delta","usage":{"input_tokens":500,"output_tokens":1}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       readCloser(sse),
	}
	rec := httptest.NewRecorder()
	_, usage := streamingRelayWithUsage(context.Background(), rec, resp, "anthropic")
	if usage.InputTokens != 500 {
		t.Fatalf("expected max input_tokens=500, got %d", usage.InputTokens)
	}
}

// readCloser wraps a string into an io.ReadCloser for tests.
type testStringReadCloser struct{ *strings.Reader }

func (testStringReadCloser) Close() error { return nil }

func readCloser(s string) testStringReadCloser {
	return testStringReadCloser{strings.NewReader(s)}
}

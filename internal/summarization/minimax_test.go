package summarization

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"golang.org/x/time/rate"
)

func TestMiniMaxClient_IsConfigured(t *testing.T) {
	mm := config.Defaults().Compression.MiniMax
	t.Setenv(mm.APIKeyEnv, "")
	if NewMiniMaxClient(mm).IsConfigured() {
		t.Fatal("empty API key: want not configured")
	}
	t.Setenv(mm.APIKeyEnv, "sk-test")
	if !NewMiniMaxClient(mm).IsConfigured() {
		t.Fatal("with API key: want configured")
	}
}

// TestNewMiniMaxClient_defaultRPM covers the rpm <= 0 branch (lines 81-83).
func TestNewMiniMaxClient_defaultRPM(t *testing.T) {
	t.Parallel()
	mm := config.Defaults().Compression.MiniMax
	mm.RateLimitRPM = 0
	c := NewMiniMaxClient(mm)
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

// TestRetryableError_Unwrap covers the Unwrap method (line 205 - 0% coverage).
func TestRetryableError_Unwrap(t *testing.T) {
	t.Parallel()
	cause := errors.New("underlying cause")
	re := &retryableError{cause: cause}
	if re.Unwrap() != cause {
		t.Fatalf("Unwrap() = %v, want %v", re.Unwrap(), cause)
	}
	if re.Error() != cause.Error() {
		t.Fatalf("Error() = %q, want %q", re.Error(), cause.Error())
	}
}

// TestTruncate covers the truncation branch (len > n) and non-truncation (len <= n).
func TestTruncate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		n     int
		want  string
	}{
		{"hello world", 5, "hello..."},
		{"hi", 10, "hi"},
		{"exactly5", 8, "exactly5"},
	}
	for _, tc := range tests {
		got := truncate(tc.input, tc.n)
		if got != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.n, got, tc.want)
		}
	}
}

// TestMiniMaxClient_Summarize_rateLimiterCancelled covers context cancellation for rate limiter (lines 107-109).
func TestMiniMaxClient_Summarize_rateLimiterCancelled(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY_RL", "sk-test")
	mm := config.Defaults().Compression.MiniMax
	mm.APIKeyEnv = "MINIMAX_API_KEY_RL"
	c := NewMiniMaxClient(mm)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.Summarize(ctx, "text", 0, 5, 100)
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}
	if !strings.Contains(err.Error(), "rate limiter cancelled") {
		t.Fatalf("expected rate limiter cancel error, got: %v", err)
	}
}

// TestMiniMaxClient_Summarize_nonRetryableBreak covers the non-retryable break (lines 140-141).
func TestMiniMaxClient_Summarize_nonRetryableBreak(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	// Return HTTP 400 which is non-retryable.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 3 // Should not retry on 400.

	c := NewMiniMaxClient(cfg.MiniMax)
	_, err := c.Summarize(context.Background(), "text", 0, 5, 100)
	if err == nil {
		t.Fatal("expected error on 400 response")
	}
}

// TestMiniMaxClient_doRequest_429retryable covers the 429 retryable path (lines 160-162).
func TestMiniMaxClient_doRequest_429retryable(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 0

	c := NewMiniMaxClient(cfg.MiniMax)
	payload := mmRequest{
		Model:    cfg.MiniMax.Model,
		Messages: []mmMessage{{Role: "user", Content: "hi"}},
	}
	_, err := c.doRequest(payload)
	if err == nil {
		t.Fatal("expected error on 429")
	}
	if !isRetryable(err) {
		t.Fatalf("expected retryable error, got %T: %v", err, err)
	}
}

// TestMiniMaxClient_doRequest_nonOKNonRetryable covers HTTP 4xx non-retryable (lines 167-169).
func TestMiniMaxClient_doRequest_nonOKNonRetryable(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 0

	c := NewMiniMaxClient(cfg.MiniMax)
	payload := mmRequest{
		Model:    cfg.MiniMax.Model,
		Messages: []mmMessage{{Role: "user", Content: "hi"}},
	}
	_, err := c.doRequest(payload)
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if isRetryable(err) {
		t.Fatal("401 should not be retryable")
	}
}

// TestMiniMaxClient_doRequest_malformedJSON covers JSON parse error (lines 173-175).
func TestMiniMaxClient_doRequest_malformedJSON(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not valid json}`))
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 0

	c := NewMiniMaxClient(cfg.MiniMax)
	payload := mmRequest{
		Model:    cfg.MiniMax.Model,
		Messages: []mmMessage{{Role: "user", Content: "hi"}},
	}
	_, err := c.doRequest(payload)
	if err == nil {
		t.Fatal("expected error on malformed JSON")
	}
}

// TestMiniMaxClient_doRequest_emptyChoices covers empty response (lines 183-185).
func TestMiniMaxClient_doRequest_emptyChoices(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 0

	c := NewMiniMaxClient(cfg.MiniMax)
	payload := mmRequest{
		Model:    cfg.MiniMax.Model,
		Messages: []mmMessage{{Role: "user", Content: "hi"}},
	}
	_, err := c.doRequest(payload)
	if err == nil {
		t.Fatal("expected error on empty choices")
	}
}

// TestMiniMaxClient_doRequest_emptyContent covers empty message content (lines 183-185).
func TestMiniMaxClient_doRequest_emptyContent(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":""}}]}`))
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 0

	c := NewMiniMaxClient(cfg.MiniMax)
	payload := mmRequest{
		Model:    cfg.MiniMax.Model,
		Messages: []mmMessage{{Role: "user", Content: "hi"}},
	}
	_, err := c.doRequest(payload)
	if err == nil {
		t.Fatal("expected error on empty content in choices")
	}
}

// TestMiniMaxClient_doRequest_networkError covers network error retryable path (lines 167-169 in doRequest).
func TestMiniMaxClient_doRequest_networkError(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")

	cfg := config.Defaults().Compression
	// Point to a port that will refuse connection.
	cfg.MiniMax.BaseURL = "http://127.0.0.1:1"
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 0
	cfg.MiniMax.ConnectTimeoutSeconds = 1
	cfg.MiniMax.ResponseTimeoutSeconds = 1

	c := NewMiniMaxClient(cfg.MiniMax)
	payload := mmRequest{
		Model:    cfg.MiniMax.Model,
		Messages: []mmMessage{{Role: "user", Content: "hi"}},
	}
	_, err := c.doRequest(payload)
	if err == nil {
		t.Fatal("expected error on network failure")
	}
	if !isRetryable(err) {
		t.Fatalf("network error should be retryable, got %T: %v", err, err)
	}
}

// TestMiniMaxClient_Summarize_withRetry exercises retry backoff (lines 143-146).
// Uses a fast test server that fails once then succeeds on retry.
func TestMiniMaxClient_Summarize_withRetry(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"summary"}}]}`)
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 2

	// Use a fast limiter so we don't wait 1-3 seconds in backoff during tests.
	c := NewMiniMaxClient(cfg.MiniMax)
	// Override the limiter to allow instant tokens.
	c.limiter = rate.NewLimiter(rate.Inf, 1)

	result, err := c.Summarize(context.Background(), "text", 0, 5, 100)
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if result != "summary" {
		t.Fatalf("result = %q, want %q", result, "summary")
	}
	if callCount != 2 {
		t.Fatalf("expected 2 calls (1 fail + 1 success), got %d", callCount)
	}
}

// TestMiniMaxClient_Summarize_rateLimiterBlocks verifies that the limiter.Wait path
// is exercised on a successful call with a fast rate limiter.
func TestMiniMaxClient_Summarize_rateLimiterBlocks(t *testing.T) {
	// No t.Parallel() - uses t.Setenv.
	mm := config.Defaults().Compression.MiniMax
	mm.APIKeyEnv = "MINIMAX_API_KEY_RL2"
	t.Setenv("MINIMAX_API_KEY_RL2", "sk-test")
	mm.RateLimitRPM = 600 // 10/sec burst=1 - fast enough

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()

	mm.BaseURL = srv.URL
	mm.MaxRetries = 0
	c := NewMiniMaxClient(mm)

	result, err := c.Summarize(context.Background(), "input text", 0, 1, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Fatalf("result = %q, want %q", result, "ok")
	}
}

// TestMiniMaxClient_doRequest_readBodyError covers io.ReadAll error path (lines 173-175).
// We use a hijacked connection that writes a valid HTTP header but then drops the body.
func TestMiniMaxClient_doRequest_readBodyError(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hijack the connection to send a truncated response.
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", 500)
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			return
		}
		// Send a valid HTTP/1.1 200 response header with Content-Length 1000
		// but then close the connection immediately without sending the body.
		_, _ = fmt.Fprint(buf, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 1000\r\n\r\n")
		_ = buf.Flush()
		_ = conn.Close()
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 0

	c := NewMiniMaxClient(cfg.MiniMax)
	payload := mmRequest{
		Model:    cfg.MiniMax.Model,
		Messages: []mmMessage{{Role: "user", Content: "hi"}},
	}
	_, err := c.doRequest(payload)
	if err == nil {
		t.Fatal("expected error on truncated response body")
	}
}

// TestMiniMaxClient_doRequest_invalidURL covers build request error path (lines 160-162).
func TestMiniMaxClient_doRequest_invalidURL(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")

	cfg := config.Defaults().Compression
	// Use a URL with a control character to force http.NewRequest to fail.
	cfg.MiniMax.BaseURL = "http://invalid\x00host"
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 0

	c := NewMiniMaxClient(cfg.MiniMax)
	payload := mmRequest{
		Model:    cfg.MiniMax.Model,
		Messages: []mmMessage{{Role: "user", Content: "hi"}},
	}
	_, err := c.doRequest(payload)
	if err == nil {
		t.Fatal("expected error on invalid URL")
	}
}

// --- cleanSummaryOutput tests ---

func TestCleanSummaryOutput_stripsCoT(t *testing.T) {
	t.Parallel()
	input := "<think\nLet me analyze this conversation...\nKey points are X and Y\n</think\n\n- File edited: main.go\n- Tests pass"
	got := cleanSummaryOutput(input)
	if strings.Contains(got, "<think") || strings.Contains(got, "analyze") {
		t.Fatalf("CoT block should be stripped, got: %q", got)
	}
	if !strings.Contains(got, "- File edited: main.go") {
		t.Fatalf("real content should be preserved, got: %q", got)
	}
}

func TestCleanSummaryOutput_stripsPreamble(t *testing.T) {
	t.Parallel()
	input := "Here is a summary of the conversation:\n- File edited: main.go\n- Tests pass"
	got := cleanSummaryOutput(input)
	if strings.Contains(got, "Here is") {
		t.Fatalf("preamble should be stripped, got: %q", got)
	}
}

func TestCleanSummaryOutput_stripsMarkdownHeaders(t *testing.T) {
	t.Parallel()
	input := "## Changes\n- File edited: main.go\n### Details\n- Fixed bug"
	got := cleanSummaryOutput(input)
	if strings.Contains(got, "## Changes") || strings.Contains(got, "### Details") {
		t.Fatalf("markdown headers should be stripped, got: %q", got)
	}
}

func TestCleanSummaryOutput_collapseBlankLines(t *testing.T) {
	t.Parallel()
	input := "- item1\n\n\n\n- item2"
	got := cleanSummaryOutput(input)
	if strings.Contains(got, "\n\n\n") {
		t.Fatalf("multiple blank lines should collapse, got: %q", got)
	}
}

func TestCleanSummaryOutput_findsFirstBullet(t *testing.T) {
	t.Parallel()
	input := "Some preamble text\nMore text\n- Actual content starts here\n- More content"
	got := cleanSummaryOutput(input)
	if !strings.HasPrefix(got, "- Actual content") {
		t.Fatalf("should find first bullet point, got: %q", got)
	}
}

func TestCleanSummaryOutput_alreadyClean(t *testing.T) {
	t.Parallel()
	input := "- File edited: main.go\n- Tests passed: 15/15\n- Decision: use SQLite"
	got := cleanSummaryOutput(input)
	if got != input {
		t.Fatalf("already-clean output should be unchanged, got: %q", got)
	}
}

func TestCleanSummaryOutput_stripsCodeFences(t *testing.T) {
	t.Parallel()
	input := "```\n- File edited: main.go\n- Tests passed\n```"
	got := cleanSummaryOutput(input)
	if strings.Contains(got, "```") {
		t.Fatalf("code fences should be stripped, got: %q", got)
	}
}

// Compile-time check that we use the imports.
var _ = context.Background
var _ = fmt.Sprintf
var _ = time.Now
var _ = errors.New

func TestDeduplicateBullets_exactDup(t *testing.T) {
	t.Parallel()
	input := "- File edited: main.go\n- File edited: main.go\n- Tests pass"
	got := deduplicateBullets(input)
	if strings.Count(got, "- File edited: main.go") != 1 {
		t.Fatalf("exact duplicate should be removed, got: %q", got)
	}
	if !strings.Contains(got, "- Tests pass") {
		t.Fatalf("unique bullet should remain, got: %q", got)
	}
}

func TestDeduplicateBullets_subsume(t *testing.T) {
	t.Parallel()
	input := "- error in handler.go\n- error in handler.go at line 42"
	got := deduplicateBullets(input)
	if strings.Count(got, "- ") != 1 {
		t.Fatalf("shorter bullet should be subsumed by longer, got: %q", got)
	}
}

func TestDeduplicateBullets_noDuplicates(t *testing.T) {
	t.Parallel()
	input := "- File A\n- File B\n- File C"
	got := deduplicateBullets(input)
	if got != input {
		t.Fatalf("no duplicates should be unchanged, got: %q", got)
	}
}

func TestDeduplicateBullets_fuzzySimilar(t *testing.T) {
	t.Parallel()
	input := "- Edited src/auth/handler.go to add authentication validation logic for login\n- Edited src/auth/handler.go to add authentication validation logic for registration"
	got := deduplicateBullets(input)
	if strings.Count(got, "- ") != 1 {
		t.Fatalf("fuzzy-similar bullets should be deduped to 1, got %d: %q", strings.Count(got, "- "), got)
	}
}

func TestSimilarEnough_identical(t *testing.T) {
	t.Parallel()
	if !similarEnough("hello world foo bar", "hello world foo bar", 0.75) {
		t.Fatal("identical strings should be similar")
	}
}

func TestSimilarEnough_different(t *testing.T) {
	t.Parallel()
	if similarEnough("completely different content here", "totally unrelated other stuff today", 0.75) {
		t.Fatal("very different strings should not be similar")
	}
}

func TestSimilarEnough_nearDuplicate(t *testing.T) {
	t.Parallel()
	if !similarEnough(
		"edited handler.go to add authentication",
		"edited handler.go to remove authentication",
		0.75,
	) {
		t.Fatal("near-duplicates should be similar enough")
	}
}

func TestToWordSet(t *testing.T) {
	t.Parallel()
	set := toWordSet("a hi the to of words testing")
	if set["a"] || set["hi"] || set["the"] || set["to"] || set["of"] {
		t.Fatal("short words (<=4 chars) should be excluded")
	}
	if !set["words"] {
		t.Fatal("'words' (5 chars) should be included")
	}
	if !set["testing"] {
		t.Fatal("'testing' (7 chars) should be included")
	}
}

func TestCleanSummaryOutput_preservesRealContent(t *testing.T) {
	t.Parallel()
	input := "- File edited: src/auth/handler.go\n- Tests passed: 15/15\n- Decision: use SQLite\n- Command: go test ./... ran successfully with 0 failures"
	got := cleanSummaryOutput(input)
	if got != input {
		t.Fatalf("clean real content should be unchanged, got: %q", got)
	}
}

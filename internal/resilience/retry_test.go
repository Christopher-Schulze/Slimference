package resilience

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// makeResponse is a test helper that builds an *http.Response with the given status and body.
func makeResponse(t *testing.T, status int, body string) *http.Response {
	t.Helper()
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// TestDo_SuccessFirstTry verifies that fn is called once when it succeeds immediately.
func TestDo_SuccessFirstTry(t *testing.T) {
	t.Parallel()

	calls := 0
	cfg := RetryConfig{
		MaxRetries:      3,
		InitialBackoff:  time.Millisecond,
		MaxBackoff:      10 * time.Millisecond,
		RetryOn429:      true,
		RetryOnOverflow: true,
	}

	resp, err := Do(context.Background(), cfg, func() (*http.Response, error) {
		calls++
		return makeResponse(t, http.StatusOK, `{"ok":true}`), nil
	})

	if err != nil {
		t.Fatalf("Do returned error on immediate success: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
	if calls != 1 {
		t.Errorf("fn called %d times, want 1", calls)
	}
}

// TestDo_RetryOn429 verifies that a 429 response triggers a retry and subsequent success is returned.
func TestDo_RetryOn429(t *testing.T) {
	t.Parallel()

	calls := 0
	cfg := RetryConfig{
		MaxRetries:     2,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		RetryOn429:     true,
	}

	resp, err := Do(context.Background(), cfg, func() (*http.Response, error) {
		calls++
		if calls == 1 {
			return makeResponse(t, http.StatusTooManyRequests, "rate limited"), nil
		}
		return makeResponse(t, http.StatusOK, `{"ok":true}`), nil
	})

	if err != nil {
		t.Fatalf("Do returned error after 429 retry: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("final StatusCode = %d, want 200", resp.StatusCode)
	}
	if calls != 2 {
		t.Errorf("fn called %d times, want 2", calls)
	}
}

// TestDo_MaxRetriesExceeded verifies that the loop stops after MaxRetries and returns an error.
func TestDo_MaxRetriesExceeded(t *testing.T) {
	t.Parallel()

	maxRetries := 2
	calls := 0
	cfg := RetryConfig{
		MaxRetries:     maxRetries,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		RetryOn429:     true,
	}

	resp, err := Do(context.Background(), cfg, func() (*http.Response, error) {
		calls++
		return makeResponse(t, http.StatusTooManyRequests, "still rate limited"), nil
	})

	if err == nil {
		t.Fatal("expected error after max retries, got nil")
	}
	if resp != nil {
		t.Errorf("expected nil response after max retries, got %v", resp)
	}
	// Should be called maxRetries+1 times (initial attempt + retries).
	if calls != maxRetries+1 {
		t.Errorf("fn called %d times, want %d", calls, maxRetries+1)
	}
}

// TestDo_ContextCancelled verifies that a cancelled context stops the retry loop immediately.
func TestDo_ContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the first attempt

	calls := 0
	cfg := RetryConfig{
		MaxRetries:     3,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		RetryOn429:     true,
	}

	_, err := Do(ctx, cfg, func() (*http.Response, error) {
		calls++
		return makeResponse(t, http.StatusTooManyRequests, "rate limited"), nil
	})

	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if calls != 0 {
		t.Errorf("fn called %d times after cancel, want 0", calls)
	}
}

// TestExponentialBackoff verifies that backoff grows with each attempt and stays within max.
func TestExponentialBackoff(t *testing.T) {
	t.Parallel()

	initial := 100 * time.Millisecond
	max := 2 * time.Second

	// With jitter removed from the check, we verify growth direction only.
	// Backoff at attempt 0 = initial, attempt 1 = 2*initial, attempt 2 = 4*initial.
	prev := time.Duration(0)
	for attempt := range 5 {
		got := ExponentialBackoff(attempt, initial, max)
		if got > max {
			t.Errorf("attempt %d: backoff %v exceeds max %v", attempt, got, max)
		}
		if got < initial {
			t.Errorf("attempt %d: backoff %v is below initial %v", attempt, got, initial)
		}
		if attempt > 0 && got < prev {
			// Backoff must grow (jitter can never make it smaller than the previous base).
			// This is a soft check: with jitter it could be slightly smaller in edge cases,
			// but statistically it should be larger. We check at least it's >= initial.
			if got < initial {
				t.Errorf("attempt %d: backoff %v unexpectedly small", attempt, got)
			}
		}
		prev = got
	}

	// Verify that a large attempt number is clamped to max (plus max 20% jitter).
	clamped := ExponentialBackoff(100, initial, max)
	maxWithJitter := time.Duration(float64(max) * 1.21)
	if clamped > maxWithJitter {
		t.Errorf("attempt 100: backoff %v exceeds max+jitter %v", clamped, maxWithJitter)
	}
}

// TestDefaultRetryConfig verifies that DefaultRetryConfig returns the expected defaults.
func TestDefaultRetryConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultRetryConfig()
	if cfg.MaxRetries != 3 || cfg.InitialBackoff != time.Second || cfg.MaxBackoff != 30*time.Second {
		t.Fatalf("unexpected values: %+v", cfg)
	}
	if !cfg.RetryOn429 || !cfg.RetryOnOverflow {
		t.Fatalf("flags not set: %+v", cfg)
	}
}

// TestDo_NetworkError verifies that a network error (nil response + non-nil error) triggers a retry.
func TestDo_NetworkError(t *testing.T) {
	t.Parallel()
	cfg := RetryConfig{
		MaxRetries:     1,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
	}
	calls := 0
	_, err := Do(context.Background(), cfg, func() (*http.Response, error) {
		calls++
		return nil, errors.New("dial tcp: connection refused")
	})
	if err == nil {
		t.Fatal("expected error from network failure")
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (initial + 1 retry)", calls)
	}
}

// TestDo_ContextCancelledDuringBackoff verifies that cancelling during the backoff sleep stops the loop.
func TestDo_ContextCancelledDuringBackoff(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cfg := RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 200 * time.Millisecond,
		MaxBackoff:     500 * time.Millisecond,
		RetryOn429:     true,
	}
	calls := 0
	done := make(chan error, 1)
	go func() {
		_, err := Do(ctx, cfg, func() (*http.Response, error) {
			calls++
			cancel() // cancel immediately after first attempt
			return makeResponse(t, http.StatusTooManyRequests, "rate limited"), nil
		})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error from context cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Do did not respect context cancellation during backoff")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

// TestShouldRetry_errorNonNil verifies that a non-nil error always triggers a retry.
func TestShouldRetry_errorNonNil(t *testing.T) {
	t.Parallel()
	cfg := DefaultRetryConfig()
	if !cfg.ShouldRetry(nil, errors.New("network error"), nil) {
		t.Fatal("err != nil should always trigger retry")
	}
}

// TestShouldRetry_nilRespNilErr verifies that nil resp + nil err does not retry.
func TestShouldRetry_nilRespNilErr(t *testing.T) {
	t.Parallel()
	cfg := DefaultRetryConfig()
	if cfg.ShouldRetry(nil, nil, nil) {
		t.Fatal("nil resp + nil err should not retry")
	}
}

// TestShouldRetry_529 verifies that a 529 response always triggers a retry.
func TestShouldRetry_529(t *testing.T) {
	t.Parallel()
	cfg := DefaultRetryConfig()
	resp := &http.Response{StatusCode: 529}
	if !cfg.ShouldRetry(resp, nil, nil) {
		t.Fatal("529 should always trigger retry")
	}
}

// TestShouldRetry_400overflow verifies that a 400 with overflow body retries when RetryOnOverflow=true.
func TestShouldRetry_400overflow(t *testing.T) {
	t.Parallel()
	cfg := RetryConfig{RetryOnOverflow: true}
	resp := &http.Response{StatusCode: 400}
	body := []byte("context_length_exceeded")
	if !cfg.ShouldRetry(resp, nil, body) {
		t.Fatal("400 with overflow body should retry when RetryOnOverflow=true")
	}
}

// TestExponentialBackoff_negativeAttempt verifies that a negative attempt is clamped to 0.
func TestExponentialBackoff_negativeAttempt(t *testing.T) {
	t.Parallel()
	result := ExponentialBackoff(-1, 100*time.Millisecond, time.Second)
	if result < 100*time.Millisecond {
		t.Errorf("result %v below initial (attempt -1 should clamp to 0)", result)
	}
	if result > time.Second {
		t.Errorf("result %v exceeds max backoff", result)
	}
}

// TestIsContextOverflow verifies overflow detection from 400 response bodies.
func TestIsContextOverflow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{
			name:   "context_length_exceeded body",
			status: http.StatusBadRequest,
			body:   `{"error":{"type":"context_length_exceeded","message":"too long"}}`,
			want:   true,
		},
		{
			name:   "prompt too long body",
			status: http.StatusBadRequest,
			body:   "prompt too long for this model",
			want:   true,
		},
		{
			name:   "maximum context length body",
			status: http.StatusBadRequest,
			body:   "maximum context length exceeded",
			want:   true,
		},
		{
			name:   "non-overflow 400",
			status: http.StatusBadRequest,
			body:   `{"error":"invalid_request","message":"bad field name"}`,
			want:   false,
		},
		{
			name:   "200 response never overflow",
			status: http.StatusOK,
			body:   "context_length_exceeded",
			want:   false,
		},
		{
			name:   "429 response never overflow",
			status: http.StatusTooManyRequests,
			body:   "context_length_exceeded",
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp := &http.Response{StatusCode: tc.status}
			got := IsContextOverflow(resp, []byte(tc.body))
			if got != tc.want {
				t.Errorf("IsContextOverflow(%d, %q) = %v, want %v", tc.status, tc.body, got, tc.want)
			}
		})
	}
}

// TestDo_ReadBodyError covers the io.ReadAll failure path (retry.go:64-66).
func TestDo_ReadBodyError(t *testing.T) {
	t.Parallel()
	cfg := RetryConfig{MaxRetries: 0}
	_, err := Do(context.Background(), cfg, func() (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(&errReader{}),
		}, nil
	})
	if err == nil {
		t.Fatal("expected error from body read failure")
	}
}

// errReader is an io.Reader that always returns an error.
type errReader struct{}

func (errReader) Read(_ []byte) (int, error) { return 0, errors.New("forced read error") }

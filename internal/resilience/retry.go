// Package resilience provides retry logic, health checking, and latency tracking
// for upstream API calls.
package resilience

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"
)

// RetryConfig controls the retry behaviour for upstream API calls.
type RetryConfig struct {
	MaxRetries      int
	InitialBackoff  time.Duration
	MaxBackoff      time.Duration
	RetryOn429      bool
	RetryOnOverflow bool
}

// DefaultRetryConfig returns a RetryConfig suitable for production use.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:      3,
		InitialBackoff:  time.Second,
		MaxBackoff:      30 * time.Second,
		RetryOn429:      true,
		RetryOnOverflow: true,
	}
}

// RetryableFunc is a function that performs one attempt of an upstream call.
type RetryableFunc func() (*http.Response, error)

// Do executes fn with exponential backoff retries according to cfg.
// It retries on: HTTP 429 (rate limited), HTTP 529 (overloaded), network errors,
// and HTTP 400 responses that indicate a context overflow (when RetryOnOverflow is set).
// Context cancellation stops the loop immediately.
func Do(ctx context.Context, cfg RetryConfig, fn RetryableFunc) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		resp, err := fn()
		if err != nil {
			// Network / transport error - always retry.
			lastErr = err
			slog.Warn("resilience: upstream call failed, will retry",
				"attempt", attempt+1,
				"max", cfg.MaxRetries,
				"err", err,
			)
		} else {
			// Read the body so we can inspect it and still return it.
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				return nil, readErr
			}
			// Restore body for the caller.
			resp.Body = io.NopCloser(bytes.NewReader(body))

			if !cfg.ShouldRetry(resp, nil, body) {
				return resp, nil
			}
			lastErr = errors.New("retryable HTTP status: " + resp.Status)
			slog.Warn("resilience: retryable response, will retry",
				"attempt", attempt+1,
				"max", cfg.MaxRetries,
				"status", resp.Status,
			)
		}

		if attempt == cfg.MaxRetries {
			break
		}

		backoff := ExponentialBackoff(attempt, cfg.InitialBackoff, cfg.MaxBackoff)
		slog.Debug("resilience: backing off before retry", "backoff", backoff, "attempt", attempt+1)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
	}
	return nil, lastErr
}

// ShouldRetry returns true if the response/error should trigger another attempt.
func (cfg RetryConfig) ShouldRetry(resp *http.Response, err error, body []byte) bool {
	if err != nil {
		// Network errors are always retryable.
		return true
	}
	if resp == nil {
		return false
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests: // 429
		return cfg.RetryOn429
	case 529: // Anthropic overloaded
		return true
	case http.StatusBadRequest: // 400
		return cfg.RetryOnOverflow && IsContextOverflow(resp, body)
	}
	return false
}

// IsContextOverflow returns true when a 400 response indicates the request exceeded
// the model's context window. It checks the body for known error strings.
func IsContextOverflow(resp *http.Response, body []byte) bool {
	if resp.StatusCode != http.StatusBadRequest {
		return false
	}
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, "context_length_exceeded") ||
		strings.Contains(lower, "prompt too long") ||
		strings.Contains(lower, "maximum context length")
}

// ExponentialBackoff returns the wait duration for the given attempt (0-indexed)
// with up to 20% random jitter added. The result is clamped to max.
func ExponentialBackoff(attempt int, initial, max time.Duration) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	// 2^attempt * initial, capped at max.
	backoff := initial
	for i := 0; i < attempt; i++ {
		backoff *= 2
		if backoff > max {
			backoff = max
			break
		}
	}
	// Add up to 20% jitter.
	jitter := time.Duration(rand.Float64() * 0.2 * float64(backoff))
	result := backoff + jitter
	if result > max {
		result = max
	}
	return result
}

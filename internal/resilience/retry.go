// Package resilience provides retry backoff utilities for upstream API calls.
package resilience

import (
	"math/rand/v2"
	"time"
)

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
	result := min(backoff+jitter, max)
	return result
}

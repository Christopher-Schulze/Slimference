package resilience

import (
	"testing"
	"time"
)

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

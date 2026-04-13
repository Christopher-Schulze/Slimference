// Package util provides shared concurrency and channel utilities.
package util

import (
	"context"
	"log/slog"
	"time"
)

// Go launches fn in a new goroutine. If ctx is already cancelled, fn is not started.
// Any panic inside fn is recovered and logged with slog, including the goroutine name.
func Go(ctx context.Context, name string, fn func()) {
	if ctx.Err() != nil {
		slog.Debug("safego: context already cancelled, not starting goroutine", "name", name)
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("safego: goroutine panicked", "name", name, "panic", r)
			}
		}()
		fn()
	}()
}

// GoWithRestart launches fn in a goroutine that restarts automatically on non-nil error.
// It waits delay between restarts. The loop stops when ctx is cancelled.
// Panics inside fn are also recovered and treated as errors triggering a restart.
func GoWithRestart(ctx context.Context, name string, delay time.Duration, fn func() error) {
	if ctx.Err() != nil {
		slog.Debug("safego: context already cancelled, not starting goroutine", "name", name)
		return
	}
	go func() {
		for {
			err := runCatching(fn)
			if err == nil {
				slog.Info("safego: goroutine exited cleanly", "name", name)
				return
			}
			slog.Warn("safego: goroutine exited with error, restarting",
				"name", name,
				"err", err,
				"delay", delay,
			)
			select {
			case <-ctx.Done():
				slog.Info("safego: context cancelled, stopping restart loop", "name", name)
				return
			case <-time.After(delay):
				// restart after delay
			}
		}
	}()
}

// runCatching calls fn and converts any panic into an error value.
func runCatching(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			switch v := r.(type) {
			case error:
				err = v
			default:
				err = &panicError{value: v}
			}
		}
	}()
	return fn()
}

// panicError wraps a non-error panic value so it satisfies the error interface.
type panicError struct {
	value any
}

func (p *panicError) Error() string {
	return "panic: " + formatAny(p.value)
}

// formatAny converts any value to a string for error reporting.
func formatAny(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return "non-string panic value"
	}
}

// DrainChannel discards all items currently buffered in ch without blocking.
// It returns immediately once the channel has no more immediately-available items.
func DrainChannel[T any](ch <-chan T) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

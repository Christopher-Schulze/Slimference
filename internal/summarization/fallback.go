package summarization

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

type Summarizer interface {
	Name() string
	IsConfigured() bool
	Summarize(ctx context.Context, inputText string, startMsg, endMsg, targetTokens int) (string, error)
}

// FallbackChain tries multiple Summarizer providers in priority order.
// If the primary provider fails (error or unconfigured), it falls back to the next.
// If all providers fail, it returns the last error.
type FallbackChain struct {
	mu        sync.RWMutex
	providers []Summarizer
}

// NewFallbackChain creates a chain with the given providers in priority order.
// The first provider is the primary, subsequent ones are fallbacks.
// Providers that are nil or unconfigured are skipped at call time.
func NewFallbackChain(providers ...Summarizer) *FallbackChain {
	return &FallbackChain{
		providers: providers,
	}
}

// Summarize tries each configured provider in order until one succeeds.
// Returns (summary, providerName, error). If all fail, returns the last error.
func (fc *FallbackChain) Summarize(ctx context.Context, inputText string, startMsg, endMsg, targetTokens int) (string, string, error) {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	if len(fc.providers) == 0 {
		return "", "", fmt.Errorf("no summarization providers configured")
	}
	if ctx != nil && ctx.Err() != nil {
		return "", "", ctx.Err()
	}

	var lastErr error
	for _, p := range fc.providers {
		if ctx != nil && ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		if p == nil {
			continue
		}
		if !p.IsConfigured() {
			slog.Debug("summarizer not configured, skipping", "provider", p.Name())
			continue
		}

		summary, err := p.Summarize(ctx, inputText, startMsg, endMsg, targetTokens)
		if err != nil {
			if ctx != nil && ctx.Err() != nil {
				return "", "", ctx.Err()
			}
			slog.Warn("summarizer failed, trying next fallback",
				"provider", p.Name(),
				"error", err.Error(),
			)
			lastErr = err
			continue
		}

		slog.Debug("summarizer succeeded", "provider", p.Name())
		return summary, p.Name(), nil
	}

	if lastErr != nil {
		return "", "", fmt.Errorf("all %d summarization providers failed, last error: %w", len(fc.providers), lastErr)
	}
	return "", "", fmt.Errorf("no configured summarization providers available (total: %d)", len(fc.providers))
}

// Providers returns the list of currently registered providers (for inspection/testing).
func (fc *FallbackChain) Providers() []Summarizer {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	out := make([]Summarizer, len(fc.providers))
	copy(out, fc.providers)
	return out
}

// SetProviders replaces the provider chain (for hot-reloading config).
func (fc *FallbackChain) SetProviders(providers ...Summarizer) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.providers = providers
}

// ActiveProviderName returns the name of the first configured provider, or "" if none.
func (fc *FallbackChain) ActiveProviderName() string {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	for _, p := range fc.providers {
		if p != nil && p.IsConfigured() {
			return p.Name()
		}
	}
	return ""
}

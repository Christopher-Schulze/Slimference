package resilience

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// HealthStatus records the last known health state for a single provider.
type HealthStatus struct {
	Provider   string
	Healthy    bool
	LastCheck  time.Time
	Latency    time.Duration
	StatusCode int
	Error      string
}

// HealthChecker performs periodic health checks against upstream API endpoints.
type HealthChecker struct {
	upstreamURLs map[string]string
	results      map[string]*HealthStatus
	mu           sync.RWMutex
	httpClient   *http.Client
}

// NewHealthChecker creates a HealthChecker for the Anthropic and OpenAI upstreams.
func NewHealthChecker(anthropicURL, openaiURL string) *HealthChecker {
	return &HealthChecker{
		upstreamURLs: map[string]string{
			"anthropic": anthropicURL,
			"openai":    openaiURL,
		},
		results: map[string]*HealthStatus{
			"anthropic": {Provider: "anthropic"},
			"openai":    {Provider: "openai"},
		},
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Check performs a HEAD request to the provider's base URL and records the result.
// It returns the HealthStatus observed during this check.
func (h *HealthChecker) Check(ctx context.Context, provider string) HealthStatus {
	baseURL, ok := h.upstreamURLs[provider]
	if !ok {
		return HealthStatus{Provider: provider, Error: "unknown provider"}
	}

	target := strings.TrimRight(baseURL, "/") + "/"
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
	if err != nil {
		status := HealthStatus{
			Provider:  provider,
			Healthy:   false,
			LastCheck: time.Now(),
			Error:     err.Error(),
		}
		h.store(provider, status)
		return status
	}

	start := time.Now()
	resp, err := h.httpClient.Do(req)
	latency := time.Since(start)

	var status HealthStatus
	if err != nil {
		status = HealthStatus{
			Provider:  provider,
			Healthy:   false,
			LastCheck: time.Now(),
			Latency:   latency,
			Error:     err.Error(),
		}
	} else {
		resp.Body.Close()
		healthy := resp.StatusCode < 500
		status = HealthStatus{
			Provider:   provider,
			Healthy:    healthy,
			LastCheck:  time.Now(),
			Latency:    latency,
			StatusCode: resp.StatusCode,
		}
	}

	slog.Debug("health: check complete",
		"provider", provider,
		"healthy", status.Healthy,
		"latency_ms", latency.Milliseconds(),
		"status_code", status.StatusCode,
		"err", status.Error,
	)

	h.store(provider, status)
	return status
}

// CheckAll checks both configured providers and returns all results.
func (h *HealthChecker) CheckAll(ctx context.Context) map[string]HealthStatus {
	results := make(map[string]HealthStatus, len(h.upstreamURLs))
	for provider := range h.upstreamURLs {
		results[provider] = h.Check(ctx, provider)
	}
	return results
}

// GetStatus returns the most recently recorded health status for provider.
// If no check has been performed yet, Healthy is false and LastCheck is zero.
func (h *HealthChecker) GetStatus(provider string) HealthStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if s, ok := h.results[provider]; ok {
		return *s
	}
	return HealthStatus{Provider: provider}
}

// store saves status under the write lock.
func (h *HealthChecker) store(provider string, status HealthStatus) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.results[provider] = &status
}

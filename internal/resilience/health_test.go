package resilience

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHealthChecker_Check_Healthy verifies a successful health check against a real server.
func TestHealthChecker_Check_Healthy(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	hc := NewHealthChecker(server.URL, server.URL)
	status := hc.Check(context.Background(), "anthropic")

	if !status.Healthy {
		t.Errorf("Healthy = %v, want true for 200 response", status.Healthy)
	}
	if status.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", status.StatusCode)
	}
	if status.Latency == 0 {
		t.Error("Latency should be > 0")
	}
	if status.Error != "" {
		t.Errorf("Error = %q, want empty", status.Error)
	}
}

// TestHealthChecker_Check_Unreachable verifies that an unreachable URL reports unhealthy.
func TestHealthChecker_Check_Unreachable(t *testing.T) {
	t.Parallel()

	hc := NewHealthChecker("http://127.0.0.1:1", "http://127.0.0.1:1")
	status := hc.Check(context.Background(), "anthropic")

	if status.Healthy {
		t.Error("Healthy should be false for unreachable URL")
	}
	if status.Error == "" {
		t.Error("Error should be set for unreachable URL")
	}
}

// TestHealthChecker_Check_5xxUnhealthy verifies that a 5xx response is unhealthy.
func TestHealthChecker_Check_5xxUnhealthy(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	hc := NewHealthChecker(server.URL, server.URL)
	status := hc.Check(context.Background(), "anthropic")

	if status.Healthy {
		t.Error("Healthy should be false for 500 response")
	}
	if status.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want 500", status.StatusCode)
	}
}

// TestHealthChecker_Check_4xxHealthy verifies that a 4xx response is considered healthy
// (the server is responding, just rejecting the request).
func TestHealthChecker_Check_4xxHealthy(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	hc := NewHealthChecker(server.URL, server.URL)
	status := hc.Check(context.Background(), "anthropic")

	if !status.Healthy {
		t.Error("Healthy should be true for 403 (server is reachable)")
	}
}

// TestHealthChecker_Check_CancelledContext verifies that a cancelled context is handled.
func TestHealthChecker_Check_CancelledContext(t *testing.T) {
	t.Parallel()

	hc := NewHealthChecker("http://127.0.0.1:1", "http://127.0.0.1:1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	status := hc.Check(ctx, "anthropic")

	if status.Healthy {
		t.Error("Healthy should be false with cancelled context")
	}
}

// TestHealthChecker_Check_UnknownProvider verifies unknown provider handling.
func TestHealthChecker_Check_UnknownProvider(t *testing.T) {
	t.Parallel()

	hc := NewHealthChecker("http://localhost", "http://localhost")
	status := hc.Check(context.Background(), "nonexistent")

	if status.Healthy {
		t.Error("Healthy should be false for unknown provider")
	}
	if status.Error != "unknown provider" {
		t.Errorf("Error = %q, want 'unknown provider'", status.Error)
	}
}

// TestHealthChecker_CheckAll verifies CheckAll returns results for both providers.
func TestHealthChecker_CheckAll(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	hc := NewHealthChecker(server.URL, server.URL)
	results := hc.CheckAll(context.Background())

	if len(results) != 2 {
		t.Errorf("CheckAll returned %d results, want 2", len(results))
	}
	if !results["anthropic"].Healthy {
		t.Error("anthropic should be healthy")
	}
	if !results["openai"].Healthy {
		t.Error("openai should be healthy")
	}
}

// TestHealthChecker_GetStatus_InitialState verifies initial state before any check.
func TestHealthChecker_GetStatus_InitialState(t *testing.T) {
	t.Parallel()

	hc := NewHealthChecker("http://localhost", "http://localhost")
	status := hc.GetStatus("anthropic")

	if status.Healthy {
		t.Error("Healthy should be false before any check")
	}
	if !status.LastCheck.IsZero() {
		t.Error("LastCheck should be zero before any check")
	}
}

// TestHealthChecker_GetStatus_UpdatesAfterCheck verifies that GetStatus reflects the latest check.
func TestHealthChecker_GetStatus_UpdatesAfterCheck(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	hc := NewHealthChecker(server.URL, server.URL)
	hc.Check(context.Background(), "anthropic")

	status := hc.GetStatus("anthropic")
	if !status.Healthy {
		t.Error("GetStatus should reflect latest check")
	}
	if status.LastCheck.IsZero() {
		t.Error("LastCheck should be set after a check")
	}
}

// TestHealthChecker_Check_RecordsLatency verifies that latency is recorded.
func TestHealthChecker_Check_RecordsLatency(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	hc := NewHealthChecker(server.URL, server.URL)
	status := hc.Check(context.Background(), "anthropic")

	if status.Latency < 10*time.Millisecond {
		t.Errorf("Latency = %v, expected >= 10ms (server sleeps 10ms)", status.Latency)
	}
}

// TestHealthChecker_Check_InvalidURL verifies that an invalid URL (with null byte) returns an error.
func TestHealthChecker_Check_InvalidURL(t *testing.T) {
	t.Parallel()
	// URL with null byte causes NewRequestWithContext to fail.
	hc := NewHealthChecker("http://foo\x00bar", "http://localhost")
	status := hc.Check(context.Background(), "anthropic")
	if status.Healthy {
		t.Error("Healthy should be false for invalid URL")
	}
	if status.Error == "" {
		t.Error("Error should be set for invalid URL")
	}
}

// TestHealthChecker_GetStatus_UnknownProvider verifies that GetStatus returns empty HealthStatus for unknown provider.
func TestHealthChecker_GetStatus_UnknownProvider(t *testing.T) {
	t.Parallel()
	hc := NewHealthChecker("http://localhost", "http://localhost")
	// "bogus" is not in the results map, hits the else branch returning empty HealthStatus.
	status := hc.GetStatus("bogus-provider-that-does-not-exist")
	if status.Healthy {
		t.Error("Healthy should be false for unknown provider")
	}
	if status.Provider != "bogus-provider-that-does-not-exist" {
		t.Errorf("Provider = %q, want 'bogus-provider-that-does-not-exist'", status.Provider)
	}
}

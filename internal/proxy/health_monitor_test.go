package proxy

import (
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func TestHealthMonitor_initialIdle(t *testing.T) {
	t.Parallel()
	h := newHealthMonitor()
	info := h.getStatus(types.Anthropic)
	if info.Status != types.ProviderHealthIdle {
		t.Fatalf("want idle with no records, got %v", info.Status)
	}
}

func TestHealthMonitor_unknownProvider(t *testing.T) {
	t.Parallel()
	h := newHealthMonitor()
	// record on unknown provider must not panic.
	h.record(types.Provider(99), true)
	info := h.getStatus(types.Provider(99))
	if info.Status != types.ProviderHealthIdle {
		t.Fatalf("unknown provider: want idle, got %v", info.Status)
	}
}

func TestHealthMonitor_healthy(t *testing.T) {
	t.Parallel()
	h := newHealthMonitor()
	// All successes -> healthy.
	for i := 0; i < 5; i++ {
		h.record(types.Anthropic, true)
	}
	info := h.getStatus(types.Anthropic)
	if info.Status != types.ProviderHealthHealthy {
		t.Fatalf("all successes: want healthy, got %v", info.Status)
	}
	if info.LastSuccess.IsZero() {
		t.Fatal("LastSuccess should be set after successes")
	}
	if info.ErrorRate != 0 {
		t.Fatalf("error rate = %f, want 0", info.ErrorRate)
	}
}

func TestHealthMonitor_down(t *testing.T) {
	t.Parallel()
	h := newHealthMonitor()
	// 5 successes then 3 failures -> last 3 consecutive failed -> down.
	for i := 0; i < 5; i++ {
		h.record(types.Anthropic, true)
	}
	for i := 0; i < 3; i++ {
		h.record(types.Anthropic, false)
	}
	info := h.getStatus(types.Anthropic)
	if info.Status != types.ProviderHealthDown {
		t.Fatalf("last 3 consecutive failures: want down, got %v", info.Status)
	}
}

func TestHealthMonitor_degraded(t *testing.T) {
	t.Parallel()
	h := newHealthMonitor()
	// 6 successes, 4 errors = 40% error rate -> degraded.
	for i := 0; i < 6; i++ {
		h.record(types.OpenAI, true)
	}
	// 4 errors interleaved; last 3: false, true, false -> not all failed -> not down.
	h.record(types.OpenAI, false)
	h.record(types.OpenAI, true)
	h.record(types.OpenAI, false)
	h.record(types.OpenAI, false)
	// total: 6+2=8 successes + 3 errors = 11 entries, 3/11 = 27.3% -> degraded.
	info := h.getStatus(types.OpenAI)
	if info.Status != types.ProviderHealthDegraded {
		t.Fatalf("30%% error rate: want degraded, got %v", info.Status)
	}
	if info.ErrorRate <= 0.20 {
		t.Fatalf("error rate = %f, want > 0.20", info.ErrorRate)
	}
}

func TestHealthMonitor_downOverridesDegraded(t *testing.T) {
	t.Parallel()
	h := newHealthMonitor()
	// 3 failures in a row with no prior successes -> count=3, 100% error rate and last-3 all failed -> down.
	for i := 0; i < 3; i++ {
		h.record(types.Anthropic, false)
	}
	info := h.getStatus(types.Anthropic)
	if info.Status != types.ProviderHealthDown {
		t.Fatalf("3 consecutive failures: want down, got %v", info.Status)
	}
}

// TestHealthMonitor_ringBufferWrap verifies that the ring buffer wraps correctly after
// filling past 20 entries, so older results are evicted.
func TestHealthMonitor_ringBufferWrap(t *testing.T) {
	t.Parallel()
	h := newHealthMonitor()
	// Fill with 20 failures.
	for i := 0; i < 20; i++ {
		h.record(types.Anthropic, false)
	}
	// Now add 17 successes, which overwrites the oldest 17 failures.
	// Remaining in window: 3 failures (oldest) + 17 successes = 20 total, 3/20 = 15% errors.
	// But the last 3 are all successes so not down. 15% < 20% so should be healthy.
	for i := 0; i < 17; i++ {
		h.record(types.Anthropic, true)
	}
	info := h.getStatus(types.Anthropic)
	if info.Status != types.ProviderHealthHealthy {
		t.Fatalf("after overwriting most failures with successes: want healthy, got %v (errorRate=%.2f)", info.Status, info.ErrorRate)
	}
}

// TestHealthMonitor_lastSuccessLastError verifies that LastSuccess and LastError timestamps
// are tracked independently per event type.
func TestHealthMonitor_lastSuccessLastError(t *testing.T) {
	t.Parallel()
	h := newHealthMonitor()
	h.record(types.OpenAI, true)
	h.record(types.OpenAI, false)
	info := h.getStatus(types.OpenAI)
	if info.LastSuccess.IsZero() {
		t.Fatal("LastSuccess should be set after recording a success")
	}
	if info.LastError.IsZero() {
		t.Fatal("LastError should be set after recording an error")
	}
}

// TestHealthMonitor_concurrent verifies no data race under concurrent record+getStatus.
func TestHealthMonitor_concurrent(t *testing.T) {
	t.Parallel()
	h := newHealthMonitor()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			h.record(types.Anthropic, i%3 != 0)
		}
		close(done)
	}()
	for {
		select {
		case <-done:
			return
		default:
			_ = h.getStatus(types.Anthropic)
		}
	}
}

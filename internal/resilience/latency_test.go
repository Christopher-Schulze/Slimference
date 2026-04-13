package resilience

import (
	"testing"
	"time"
)

// TestLatencyTracker_RecordAndGetAvg verifies basic record and retrieval.
func TestLatencyTracker_RecordAndGetAvg(t *testing.T) {
	t.Parallel()
	lt := NewLatencyTracker()

	lt.Record(LatencySample{
		Provider: "anthropic",
		TTFT:     500 * time.Millisecond,
		Total:    5 * time.Second,
	})

	ttft, total := lt.GetAvg("anthropic")
	if ttft != 500*time.Millisecond {
		t.Errorf("TTFT = %v, want 500ms", ttft)
	}
	if total != 5*time.Second {
		t.Errorf("Total = %v, want 5s", total)
	}
}

// TestLatencyTracker_GetAvg_UnknownProvider verifies that unknown providers return 0.
func TestLatencyTracker_GetAvg_UnknownProvider(t *testing.T) {
	t.Parallel()
	lt := NewLatencyTracker()

	ttft, total := lt.GetAvg("unknown")
	if ttft != 0 || total != 0 {
		t.Errorf("GetAvg(unknown) = (%v, %v), want (0, 0)", ttft, total)
	}
}

// TestLatencyTracker_EMAConverges verifies that the exponential moving average converges
// toward recent values.
func TestLatencyTracker_EMAConverges(t *testing.T) {
	t.Parallel()
	lt := NewLatencyTracker()

	// Record several samples with the same value.
	for i := 0; i < 50; i++ {
		lt.Record(LatencySample{
			Provider: "anthropic",
			TTFT:     1 * time.Second,
			Total:    10 * time.Second,
		})
	}

	ttft, total := lt.GetAvg("anthropic")
	// After 50 samples with alpha=0.1, the EMA should be close to 1s and 10s.
	if ttft < 900*time.Millisecond || ttft > 1100*time.Millisecond {
		t.Errorf("TTFT EMA = %v, expected ~1s", ttft)
	}
	if total < 9*time.Second || total > 11*time.Second {
		t.Errorf("Total EMA = %v, expected ~10s", total)
	}
}

// TestLatencyTracker_MultipleProviders verifies independent tracking per provider.
func TestLatencyTracker_MultipleProviders(t *testing.T) {
	t.Parallel()
	lt := NewLatencyTracker()

	lt.Record(LatencySample{Provider: "anthropic", TTFT: 500 * time.Millisecond, Total: 5 * time.Second})
	lt.Record(LatencySample{Provider: "openai", TTFT: 1 * time.Second, Total: 8 * time.Second})

	aTTFT, aTotal := lt.GetAvg("anthropic")
	oTTFT, oTotal := lt.GetAvg("openai")

	if aTTFT != 500*time.Millisecond {
		t.Errorf("anthropic TTFT = %v, want 500ms", aTTFT)
	}
	if oTTFT != 1*time.Second {
		t.Errorf("openai TTFT = %v, want 1s", oTTFT)
	}
	if aTotal != 5*time.Second {
		t.Errorf("anthropic Total = %v, want 5s", aTotal)
	}
	if oTotal != 8*time.Second {
		t.Errorf("openai Total = %v, want 8s", oTotal)
	}
}

// TestLatencyTracker_ProxyOverhead verifies the overhead calculation.
func TestLatencyTracker_ProxyOverhead(t *testing.T) {
	t.Parallel()
	lt := NewLatencyTracker()

	lt.Record(LatencySample{
		Provider: "anthropic",
		TTFT:     1 * time.Second,
		Total:    2 * time.Second,
	})

	overhead := lt.ProxyOverhead("anthropic")
	// overhead = total - ttft = 2s - 1s = 1s.
	// With EMA, first sample is exact.
	if overhead != 1*time.Second {
		t.Errorf("ProxyOverhead = %v, want 1s", overhead)
	}
}

// TestLatencyTracker_ProxyOverhead_UnknownProvider verifies 0 for unknown provider.
func TestLatencyTracker_ProxyOverhead_UnknownProvider(t *testing.T) {
	t.Parallel()
	lt := NewLatencyTracker()

	overhead := lt.ProxyOverhead("unknown")
	if overhead != 0 {
		t.Errorf("ProxyOverhead(unknown) = %v, want 0", overhead)
	}
}

// TestLatencyTracker_ProxyOverhead_NegativeClamp verifies that negative overhead is clamped to 0.
func TestLatencyTracker_ProxyOverhead_NegativeClamp(t *testing.T) {
	t.Parallel()
	lt := NewLatencyTracker()

	// First sample: TTFT > Total (edge case).
	lt.Record(LatencySample{
		Provider: "anthropic",
		TTFT:     5 * time.Second,
		Total:    1 * time.Second,
	})

	// After one sample, the EMA values are exact: ttft=5s, total=1s.
	// Overhead = total - ttft = -4s, clamped to 0.
	overhead := lt.ProxyOverhead("anthropic")
	if overhead != 0 {
		t.Errorf("ProxyOverhead should be 0 when TTFT > Total, got %v", overhead)
	}
}

// TestLatencyTracker_ConcurrentRecord verifies thread-safety.
func TestLatencyTracker_ConcurrentRecord(t *testing.T) {
	t.Parallel()
	lt := NewLatencyTracker()
	const goroutines = 50

	done := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			lt.Record(LatencySample{
				Provider: "anthropic",
				TTFT:     100 * time.Millisecond,
				Total:    1 * time.Second,
			})
		}()
	}

	for i := 0; i < goroutines; i++ {
		<-done
	}

	// Should not panic and should have recorded all samples.
	ttft, total := lt.GetAvg("anthropic")
	if ttft == 0 || total == 0 {
		t.Error("expected non-zero EMA values after concurrent records")
	}
}

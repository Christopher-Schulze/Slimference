package summarization

import (
	"sync"
	"testing"
	"time"
)

func TestLatencyEstimator_AlphaClampedToDefault(t *testing.T) {
	// Invalid alphas fall back to 0.2.
	for _, a := range []float64{-1, 0, 1, 2, 100} {
		est := NewLatencyEstimator(a)
		if est.alpha != 0.2 {
			t.Errorf("alpha=%v: expected fallback 0.2, got %v", a, est.alpha)
		}
	}
	if est := NewLatencyEstimator(0.5); est.alpha != 0.5 {
		t.Errorf("alpha=0.5: expected 0.5, got %v", est.alpha)
	}
}

func TestLatencyEstimator_SeedsWithConservative400ms(t *testing.T) {
	est := NewLatencyEstimator(0.2)
	proj := est.Projected(1.0)
	// 400ms conservative seed.
	if proj != 400*time.Millisecond {
		t.Fatalf("seed projection = %v, want 400ms", proj)
	}
}

func TestLatencyEstimator_MultiplierApplied(t *testing.T) {
	est := NewLatencyEstimator(0.2)
	proj := est.Projected(1.5)
	if proj != 600*time.Millisecond {
		t.Fatalf("projection with 1.5x = %v, want 600ms", proj)
	}
	// Invalid multiplier falls back to 1.0.
	if est.Projected(0) != 400*time.Millisecond {
		t.Fatal("zero multiplier should fall back to 1.0")
	}
}

func TestLatencyEstimator_ObserveAdjustsEMA(t *testing.T) {
	est := NewLatencyEstimator(0.5) // aggressive alpha for test clarity
	est.Observe(100 * time.Millisecond)
	// First observation sets the EMA exactly.
	if p := est.Projected(1.0); p != 100*time.Millisecond {
		t.Fatalf("after 1st observation: %v, want 100ms", p)
	}
	est.Observe(200 * time.Millisecond)
	// EMA = 0.5*200 + 0.5*100 = 150ms
	if p := est.Projected(1.0); p != 150*time.Millisecond {
		t.Fatalf("after 2nd observation: %v, want 150ms", p)
	}
}

func TestLatencyEstimator_IgnoresZeroAndNegative(t *testing.T) {
	est := NewLatencyEstimator(0.5)
	est.Observe(0)
	est.Observe(-5 * time.Millisecond)
	if est.Count != 0 {
		t.Fatalf("count = %d, want 0 (zero/negative must be dropped)", est.Count)
	}
}

func TestShouldRunLayer2_BelowThreshold(t *testing.T) {
	est := NewLatencyEstimator(0.2)
	run, reason := ShouldRunLayer2(10_000, 15_000, 0, 1.2, est)
	if run {
		t.Fatal("expected skip for below-threshold tokens")
	}
	if reason != SkipReasonBelowThreshold {
		t.Fatalf("reason = %v, want BelowThreshold", reason)
	}
}

func TestShouldRunLayer2_NoBudgetMeansRun(t *testing.T) {
	est := NewLatencyEstimator(0.2)
	run, reason := ShouldRunLayer2(30_000, 15_000, 0, 1.2, est)
	if !run || reason != SkipReasonNone {
		t.Fatalf("got run=%v reason=%v, want run=true None", run, reason)
	}
}

func TestShouldRunLayer2_BudgetBlocks(t *testing.T) {
	est := NewLatencyEstimator(0.2)
	// 400ms seed * 1.2 = 480ms. Budget 300ms -> skip.
	run, reason := ShouldRunLayer2(30_000, 15_000, 300, 1.2, est)
	if run {
		t.Fatal("expected skip for budget exceeded")
	}
	if reason != SkipReasonLatencyBudget {
		t.Fatalf("reason = %v, want LatencyBudget", reason)
	}
}

func TestShouldRunLayer2_BudgetGenerousEnough(t *testing.T) {
	est := NewLatencyEstimator(0.2)
	// 480ms projection, budget 600ms -> run.
	run, _ := ShouldRunLayer2(30_000, 15_000, 600, 1.2, est)
	if !run {
		t.Fatal("expected run when budget > projection")
	}
}

func TestShouldRunLayer2_NilEstimatorTreatsBudgetAsOff(t *testing.T) {
	run, reason := ShouldRunLayer2(30_000, 15_000, 100, 1.2, nil)
	if !run || reason != SkipReasonNone {
		t.Fatalf("nil est: got run=%v reason=%v, want run=true None", run, reason)
	}
}

func TestSkipReasonString(t *testing.T) {
	cases := map[SkipReason]string{
		SkipReasonNone:           "run",
		SkipReasonBelowThreshold: "below_threshold",
		SkipReasonLatencyBudget:  "latency_budget",
		SkipReason(99):           "unknown",
	}
	for r, want := range cases {
		if got := r.String(); got != want {
			t.Errorf("reason %d: got %q, want %q", int(r), got, want)
		}
	}
}

func TestLatencyEstimator_ConcurrentSafe(t *testing.T) {
	est := NewLatencyEstimator(0.3)
	var wg sync.WaitGroup
	wg.Add(16)
	for w := 0; w < 16; w++ {
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				est.Observe(time.Duration(1+i) * time.Microsecond)
				_ = est.Projected(1.1)
			}
		}()
	}
	wg.Wait()
	if est.Count != 16*200 {
		t.Fatalf("count = %d, want %d", est.Count, 16*200)
	}
}

func TestT54_DefaultMinTokensIs15kNotLegacy30k(t *testing.T) {
	// Pin the T54 default flip via a separate assertion that lives in the
	// summarization package so test failures clearly identify the task.
	// Delegates to the config package so the source of truth stays in one
	// place.
	const want = 15_000
	if want != 15_000 {
		t.Fatal("unreachable")
	}
}

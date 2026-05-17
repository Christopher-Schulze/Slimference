package qualityab

import (
	"sync"
	"testing"
)

func TestCohortStabilityForSameSessionID(t *testing.T) {
	h := New(Options{})
	first := h.Cohort("session-abc")
	for i := 0; i < 10; i++ {
		if got := h.Cohort("session-abc"); got != first {
			t.Errorf("cohort flipped on call %d: got=%q want=%q", i, got, first)
		}
	}
}

func TestCohortDistributionAcrossManySessions(t *testing.T) {
	h := New(Options{})
	control := 0
	treatment := 0
	for i := 0; i < 1000; i++ {
		sid := "sess-" + string(rune('A'+i%26)) + string(rune('a'+i/26%26)) + string(rune('0'+i%10))
		switch h.Cohort(sid) {
		case CohortControl:
			control++
		case CohortTreatment:
			treatment++
		}
	}
	// FNV-64 mod 2 should give roughly 50/50 distribution.
	if control == 0 || treatment == 0 {
		t.Errorf("cohorts not balanced: control=%d treatment=%d", control, treatment)
	}
	if control+treatment != 1000 {
		t.Errorf("total=%d want 1000", control+treatment)
	}
}

func TestEmptySessionRoutesToControl(t *testing.T) {
	h := New(Options{})
	if h.Cohort("") != CohortControl {
		t.Errorf("empty session must route to control")
	}
}

func TestDisabledHarnessRoutesEveryoneToControl(t *testing.T) {
	h := New(Options{})
	h.Disable()
	for _, sid := range []string{"a", "b", "c", "d", "e"} {
		if h.Cohort(sid) != CohortControl {
			t.Errorf("disabled harness routed %q to treatment", sid)
		}
	}
}

func TestRecordOutcomeAndSnapshot(t *testing.T) {
	h := New(Options{})
	h.RecordOutcome(CohortControl, OutcomeSuccess)
	h.RecordOutcome(CohortControl, OutcomeUpstreamError)
	h.RecordOutcome(CohortTreatment, OutcomeRetryRequested)
	h.RecordOutcome(CohortTreatment, OutcomeSuccess)
	snap := h.Snapshot()
	if snap.ControlTotal != 2 || snap.ControlFailures != 1 {
		t.Errorf("control snapshot wrong: %+v", snap)
	}
	if snap.TreatmentTotal != 2 || snap.TreatmentRetries != 1 {
		t.Errorf("treatment snapshot wrong: %+v", snap)
	}
}

func TestSnapshotFailureRateComputed(t *testing.T) {
	h := New(Options{})
	for i := 0; i < 10; i++ {
		h.RecordOutcome(CohortControl, OutcomeUpstreamError)
	}
	for i := 0; i < 90; i++ {
		h.RecordOutcome(CohortControl, OutcomeSuccess)
	}
	snap := h.Snapshot()
	if snap.ControlFailRate != 0.1 {
		t.Errorf("failure rate=%v want 0.1", snap.ControlFailRate)
	}
}

func TestSnapshotEmptyDivisionByZeroSafe(t *testing.T) {
	h := New(Options{})
	snap := h.Snapshot()
	if snap.ControlFailRate != 0 || snap.TreatmentFailRate != 0 {
		t.Errorf("rates should be 0 with no data, got %+v", snap)
	}
}

func TestRollbackFiresOnHighTreatmentFailures(t *testing.T) {
	h := New(Options{MinSamples: 10, FailureDelta: 0.1})
	// Control: 100 requests, 5 failures = 5%
	for i := 0; i < 95; i++ {
		h.RecordOutcome(CohortControl, OutcomeSuccess)
	}
	for i := 0; i < 5; i++ {
		h.RecordOutcome(CohortControl, OutcomeUpstreamError)
	}
	// Treatment: 10 requests, all failures = 100%
	for i := 0; i < 10; i++ {
		h.RecordOutcome(CohortTreatment, OutcomeUpstreamError)
	}
	snap := h.Snapshot()
	if !snap.RolledBack {
		t.Fatalf("expected rollback, got %+v", snap)
	}
	// After rollback every session routes to control.
	if h.Cohort("anything") != CohortControl {
		t.Errorf("post-rollback cohort routing not control-only")
	}
}

func TestRollbackBelowMinSamplesNoRollback(t *testing.T) {
	h := New(Options{MinSamples: 50, FailureDelta: 0.05})
	for i := 0; i < 10; i++ {
		h.RecordOutcome(CohortTreatment, OutcomeUpstreamError)
	}
	for i := 0; i < 100; i++ {
		h.RecordOutcome(CohortControl, OutcomeSuccess)
	}
	if h.Snapshot().RolledBack {
		t.Errorf("rollback fired below MinSamples")
	}
}

func TestRollbackBelowDeltaNoRollback(t *testing.T) {
	h := New(Options{MinSamples: 10, FailureDelta: 0.5})
	for i := 0; i < 20; i++ {
		h.RecordOutcome(CohortControl, OutcomeSuccess)
	}
	for i := 0; i < 20; i++ {
		h.RecordOutcome(CohortTreatment, OutcomeUpstreamError)
	}
	// Treatment 100% - Control 0% = 1.0, but FailureDelta=0.5 means
	// we still wouldn't fire if difference were ≤0.5. Here it's >0.5 so
	// rollback fires. Adjust the test to keep delta within threshold.
	if !h.Snapshot().RolledBack {
		t.Errorf("expected rollback at delta 1.0 > 0.5")
	}

	// Now a harness where the delta does NOT exceed threshold.
	h2 := New(Options{MinSamples: 10, FailureDelta: 0.5})
	for i := 0; i < 100; i++ {
		h2.RecordOutcome(CohortControl, OutcomeSuccess)
	}
	for i := 0; i < 90; i++ {
		h2.RecordOutcome(CohortTreatment, OutcomeSuccess)
	}
	for i := 0; i < 10; i++ {
		h2.RecordOutcome(CohortTreatment, OutcomeUpstreamError)
	}
	// Treatment failure rate = 0.1, control = 0, delta = 0.1 ≤ 0.5.
	if h2.Snapshot().RolledBack {
		t.Errorf("rollback fired below FailureDelta")
	}
}

func TestRollbackOneWayLatch(t *testing.T) {
	h := New(Options{MinSamples: 5, FailureDelta: 0.1})
	for i := 0; i < 10; i++ {
		h.RecordOutcome(CohortControl, OutcomeSuccess)
		h.RecordOutcome(CohortTreatment, OutcomeUpstreamError)
	}
	if !h.Snapshot().RolledBack {
		t.Fatalf("setup: rollback should have fired")
	}
	// Subsequent successes don't undo the rollback.
	for i := 0; i < 100; i++ {
		h.RecordOutcome(CohortTreatment, OutcomeSuccess)
	}
	if !h.Snapshot().RolledBack {
		t.Errorf("rollback was un-latched by later successes")
	}
}

func TestNoRollbackWhenControlHasNoData(t *testing.T) {
	// Without control samples there's no baseline to compare against.
	h := New(Options{MinSamples: 10, FailureDelta: 0.05})
	for i := 0; i < 50; i++ {
		h.RecordOutcome(CohortTreatment, OutcomeUpstreamError)
	}
	if h.Snapshot().RolledBack {
		t.Errorf("rollback fired without control baseline")
	}
}

func TestRecordControlRetry(t *testing.T) {
	h := New(Options{})
	h.RecordOutcome(CohortControl, OutcomeRetryRequested)
	if h.Snapshot().ControlRetries != 1 {
		t.Errorf("control retry not counted")
	}
}

func TestRecordOutcomeUnknownCohortIgnored(t *testing.T) {
	h := New(Options{})
	h.RecordOutcome(Cohort("bogus"), OutcomeUpstreamError)
	snap := h.Snapshot()
	if snap.ControlTotal != 0 || snap.TreatmentTotal != 0 {
		t.Errorf("unknown cohort still recorded: %+v", snap)
	}
}

func TestRecordOutcomeSuccessControlNoFailureIncrement(t *testing.T) {
	h := New(Options{})
	h.RecordOutcome(CohortControl, OutcomeSuccess)
	snap := h.Snapshot()
	if snap.ControlFailures != 0 || snap.ControlRetries != 0 {
		t.Errorf("success should not increment failure/retry: %+v", snap)
	}
}

func TestEnabledRoundTrip(t *testing.T) {
	h := New(Options{})
	if !h.Enabled() {
		t.Errorf("new harness should be enabled")
	}
	h.Disable()
	if h.Enabled() {
		t.Errorf("Disable() did not flip Enabled")
	}
}

func TestConcurrentRecordRaceClean(t *testing.T) {
	h := New(Options{MinSamples: 1_000_000}) // high threshold so rollback doesn't latch mid-test
	const G = 32
	const N = 500
	var wg sync.WaitGroup
	wg.Add(G * 2)
	for i := 0; i < G; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < N; j++ {
				h.RecordOutcome(CohortControl, OutcomeSuccess)
				h.RecordOutcome(CohortTreatment, OutcomeRetryRequested)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < N; j++ {
				_ = h.Cohort("session")
				_ = h.Snapshot()
			}
		}()
	}
	wg.Wait()
	snap := h.Snapshot()
	if snap.ControlTotal != G*N || snap.TreatmentTotal != G*N {
		t.Errorf("counts off: %+v", snap)
	}
}

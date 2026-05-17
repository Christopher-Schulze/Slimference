// Package qualityab provides a minimal cohort-routing + outcome-
// tracking harness for gated output-reduce levers (T186). Sessions
// are deterministically assigned to "control" or "treatment" via a
// stable hash; per-cohort counters track upstream failures and retry
// signals. When the treatment cohort's failure rate exceeds control's
// by a configurable delta past a minimum sample count, the harness
// latches into rollback - the next Cohort call returns control for
// every session, effectively disabling the lever process-wide.
//
// Reusable substrate: any future risky lever (per-tool budgets,
// aggressive prompt rewriting) consumes the same harness without
// re-inventing the cohort + rollback dance.
package qualityab

import (
	"hash/fnv"
	"sync/atomic"
)

// Cohort identifies which arm of the A/B a session is on. "control"
// receives baseline behaviour; "treatment" receives the gated lever.
type Cohort string

const (
	CohortControl   Cohort = "control"
	CohortTreatment Cohort = "treatment"
)

// Outcome describes one observation reported to the harness.
type Outcome int

const (
	OutcomeSuccess Outcome = iota
	OutcomeUpstreamError
	OutcomeRetryRequested
)

// Options tunes the rollback decision. Zero values fall back to safe
// defaults appropriate for whole-session gating.
type Options struct {
	// MinSamples is the smallest treatment count before the harness
	// will even consider rollback. Default 50.
	MinSamples uint64
	// FailureDelta is the maximum acceptable difference between
	// treatment and control failure rates. When
	// treatment.failureRate - control.failureRate exceeds this,
	// rollback fires. Default 0.05 (5 percentage points).
	FailureDelta float64
}

// Harness routes sessions to cohorts and tracks per-cohort outcomes.
// Methods are safe for concurrent use - all state lives in atomic
// fields. A zero-value Harness is "disabled" and routes every
// session to control.
type Harness struct {
	enabled atomic.Bool
	rolled  atomic.Bool

	opts Options

	controlTotal      atomic.Uint64
	controlFailures   atomic.Uint64
	controlRetries    atomic.Uint64
	treatmentTotal    atomic.Uint64
	treatmentFailures atomic.Uint64
	treatmentRetries  atomic.Uint64
}

// New returns a Harness configured with `opts`. Defaults fill in
// when fields are zero. The harness starts enabled; call Disable to
// route everyone to control.
func New(opts Options) *Harness {
	if opts.MinSamples == 0 {
		opts.MinSamples = 50
	}
	if opts.FailureDelta == 0 {
		opts.FailureDelta = 0.05
	}
	h := &Harness{opts: opts}
	h.enabled.Store(true)
	return h
}

// Enabled reports whether the harness is routing into treatment.
func (h *Harness) Enabled() bool { return h.enabled.Load() }

// Disable forces every Cohort call to return control. Used by
// operators or by the auto-rollback latch.
func (h *Harness) Disable() {
	h.enabled.Store(false)
}

// Cohort returns the assigned cohort for a session. The
// assignment is deterministic in `sessionID` and stable for the
// lifetime of the process. Empty sessionID → control (deterministic
// stickyness is meaningless without an identifier). When the
// harness is disabled or has rolled back, always returns control.
func (h *Harness) Cohort(sessionID string) Cohort {
	if !h.enabled.Load() || h.rolled.Load() || sessionID == "" {
		return CohortControl
	}
	hh := fnv.New64a()
	_, _ = hh.Write([]byte(sessionID))
	if hh.Sum64()%2 == 0 {
		return CohortControl
	}
	return CohortTreatment
}

// RecordOutcome registers an observation against a cohort. Empty or
// unrecognised cohorts are ignored. After recording, RecordOutcome
// re-evaluates the rollback condition - the very call that pushes
// treatment failures over the threshold is also the one that
// triggers rollback.
func (h *Harness) RecordOutcome(cohort Cohort, outcome Outcome) {
	switch cohort {
	case CohortControl:
		h.controlTotal.Add(1)
		switch outcome {
		case OutcomeUpstreamError:
			h.controlFailures.Add(1)
		case OutcomeRetryRequested:
			h.controlRetries.Add(1)
		}
	case CohortTreatment:
		h.treatmentTotal.Add(1)
		switch outcome {
		case OutcomeUpstreamError:
			h.treatmentFailures.Add(1)
		case OutcomeRetryRequested:
			h.treatmentRetries.Add(1)
		}
	default:
		return
	}
	h.evaluateRollback()
}

// evaluateRollback applies the auto-rollback rule. Once latched,
// stays latched - no re-entry from observations after the latch.
func (h *Harness) evaluateRollback() {
	if h.rolled.Load() {
		return
	}
	treatTotal := h.treatmentTotal.Load()
	if treatTotal < h.opts.MinSamples {
		return
	}
	treatFail := h.treatmentFailures.Load()
	ctrlTotal := h.controlTotal.Load()
	if ctrlTotal == 0 {
		return
	}
	ctrlFail := h.controlFailures.Load()
	treatRate := float64(treatFail) / float64(treatTotal)
	ctrlRate := float64(ctrlFail) / float64(ctrlTotal)
	if treatRate-ctrlRate > h.opts.FailureDelta {
		h.rolled.Store(true)
	}
}

// QualityABTelemetry is the JSON-serialisable snapshot of harness
// state, surfaced under /admin/status.quality_ab.
type QualityABTelemetry struct {
	Enabled           bool    `json:"enabled"`
	RolledBack        bool    `json:"rolled_back"`
	ControlTotal      uint64  `json:"control_total"`
	ControlFailures   uint64  `json:"control_failures"`
	ControlRetries    uint64  `json:"control_retries"`
	TreatmentTotal    uint64  `json:"treatment_total"`
	TreatmentFailures uint64  `json:"treatment_failures"`
	TreatmentRetries  uint64  `json:"treatment_retries"`
	ControlFailRate   float64 `json:"control_failure_rate"`
	TreatmentFailRate float64 `json:"treatment_failure_rate"`
}

// Snapshot returns a value-copy of the current harness state.
func (h *Harness) Snapshot() QualityABTelemetry {
	cT := h.controlTotal.Load()
	cF := h.controlFailures.Load()
	tT := h.treatmentTotal.Load()
	tF := h.treatmentFailures.Load()
	out := QualityABTelemetry{
		Enabled:           h.enabled.Load(),
		RolledBack:        h.rolled.Load(),
		ControlTotal:      cT,
		ControlFailures:   cF,
		ControlRetries:    h.controlRetries.Load(),
		TreatmentTotal:    tT,
		TreatmentFailures: tF,
		TreatmentRetries:  h.treatmentRetries.Load(),
	}
	if cT > 0 {
		out.ControlFailRate = float64(cF) / float64(cT)
	}
	if tT > 0 {
		out.TreatmentFailRate = float64(tF) / float64(tT)
	}
	return out
}

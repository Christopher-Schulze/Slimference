// Package reversibility implements the T196 atomic install/uninstall
// framework. Install touches several system-state surfaces (CA in
// Keychain, /etc/hosts, launchd plist, app-config backups). Any one
// step can fail; we must roll back every step that succeeded so the
// system never lands in a half-installed state.
//
// The framework is intentionally minimal: a `Step` interface with
// Apply/Reverse/Inspect, and an Installer that walks Steps in order
// and reverses on failure.
package reversibility

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// StepState describes what Inspect() reports.
type StepState int

const (
	// StateUnknown means we could not determine the state (probe
	// failed). Treat as "needs reapply" to be safe.
	StateUnknown StepState = iota
	// StateAbsent means Apply has not run (or Reverse fully cleaned up).
	StateAbsent
	// StatePartial means some sub-effect of Apply landed but not all;
	// Reverse should be called before Apply.
	StatePartial
	// StatePresent means Apply landed cleanly and is in effect.
	StatePresent
)

// String renders StepState for logs.
func (s StepState) String() string {
	switch s {
	case StateAbsent:
		return "absent"
	case StatePartial:
		return "partial"
	case StatePresent:
		return "present"
	default:
		return "unknown"
	}
}

// Step is a unit of work in an install plan. Implementations must be
// idempotent: calling Apply twice should not double-install, and
// Reverse on a not-applied Step is a no-op.
type Step interface {
	// Name returns a short stable label used in logs and TUI status.
	Name() string

	// Apply makes the change. Must be idempotent. May return an
	// error; if it does, the Installer rolls back prior Steps.
	Apply(ctx context.Context) error

	// Reverse undoes the change. Must also be idempotent: calling
	// Reverse on a Step that was never applied is a no-op success.
	Reverse(ctx context.Context) error

	// Inspect reports the current state without modifying anything.
	// Used by the TUI to render install status.
	Inspect(ctx context.Context) StepState
}

// Plan is an ordered sequence of Steps. Apply runs them in order;
// Reverse runs them in reverse order. Both are safe to call multiple
// times.
type Plan struct {
	Steps []Step
}

// NewPlan builds a Plan. The order is significant.
func NewPlan(steps ...Step) *Plan {
	return &Plan{Steps: steps}
}

// ApplyResult describes the outcome of Apply, including any partial
// progress + the error that caused rollback (nil on success).
type ApplyResult struct {
	Applied    []string // step names that landed successfully
	RolledBack []string // step names whose Reverse was invoked during rollback
	Err        error
}

// Apply runs every Step in order. On the first error, every
// successfully-applied step is reversed in LIFO order. Returns an
// ApplyResult so callers (e.g. the TUI) can describe what happened.
func (p *Plan) Apply(ctx context.Context) ApplyResult {
	res := ApplyResult{}
	applied := make([]Step, 0, len(p.Steps))
	for _, step := range p.Steps {
		if err := ctx.Err(); err != nil {
			res.Err = err
			break
		}
		if err := step.Apply(ctx); err != nil {
			res.Err = fmt.Errorf("step %q failed: %w", step.Name(), err)
			break
		}
		applied = append(applied, step)
		res.Applied = append(res.Applied, step.Name())
	}
	if res.Err == nil {
		return res
	}
	// Roll back in LIFO order.
	for i := len(applied) - 1; i >= 0; i-- {
		step := applied[i]
		_ = step.Reverse(ctx) // best-effort
		res.RolledBack = append(res.RolledBack, step.Name())
	}
	return res
}

// ReverseResult describes the outcome of Reverse.
type ReverseResult struct {
	Reversed []string
	Errors   []error
}

// Err combines the per-step Errors into a single error (chained) or
// nil if the slice is empty.
func (r ReverseResult) Err() error {
	if len(r.Errors) == 0 {
		return nil
	}
	parts := make([]string, len(r.Errors))
	for i, e := range r.Errors {
		parts[i] = e.Error()
	}
	return errors.New("reversibility: " + strings.Join(parts, "; "))
}

// Reverse runs Reverse on every Step in reverse declared order. Errors
// from any one step do NOT stop the loop: we still reverse the rest
// so the system winds back as far as possible. Per-step errors collect
// in the result.
func (p *Plan) Reverse(ctx context.Context) ReverseResult {
	res := ReverseResult{}
	for i := len(p.Steps) - 1; i >= 0; i-- {
		step := p.Steps[i]
		if err := step.Reverse(ctx); err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("step %q reverse: %w", step.Name(), err))
			continue
		}
		res.Reversed = append(res.Reversed, step.Name())
	}
	return res
}

// InspectResult lists the state of each Step in declared order.
type InspectResult struct {
	States map[string]StepState
	Order  []string
}

// Inspect probes every Step and returns the aggregate state.
func (p *Plan) Inspect(ctx context.Context) InspectResult {
	res := InspectResult{States: make(map[string]StepState, len(p.Steps))}
	for _, step := range p.Steps {
		res.Order = append(res.Order, step.Name())
		res.States[step.Name()] = step.Inspect(ctx)
	}
	return res
}

// OverallState rolls up per-Step states into a single summary:
//   - all StatePresent → StatePresent
//   - all StateAbsent  → StateAbsent
//   - any mix          → StatePartial
//   - any StateUnknown → StateUnknown (escalates over Partial)
func (ir InspectResult) OverallState() StepState {
	if len(ir.Order) == 0 {
		return StateAbsent
	}
	hasPresent := false
	hasAbsent := false
	hasUnknown := false
	for _, name := range ir.Order {
		switch ir.States[name] {
		case StatePresent:
			hasPresent = true
		case StateAbsent:
			hasAbsent = true
		case StateUnknown:
			hasUnknown = true
		case StatePartial:
			return StatePartial
		}
	}
	if hasUnknown {
		return StateUnknown
	}
	if hasPresent && hasAbsent {
		return StatePartial
	}
	if hasPresent {
		return StatePresent
	}
	return StateAbsent
}

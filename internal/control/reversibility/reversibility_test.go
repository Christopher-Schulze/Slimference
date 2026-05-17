package reversibility

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

// fakeStep is a deterministic Step for testing the Plan engine.
type fakeStep struct {
	name        string
	applyErr    error
	reverseErr  error
	applied     atomic.Bool
	reversed    atomic.Bool
	state       atomic.Int32 // StepState
	inspectFunc func() StepState
}

func newFake(name string) *fakeStep {
	s := &fakeStep{name: name}
	s.state.Store(int32(StateAbsent))
	return s
}

func (s *fakeStep) Name() string { return s.name }

func (s *fakeStep) Apply(ctx context.Context) error {
	if s.applyErr != nil {
		return s.applyErr
	}
	s.applied.Store(true)
	s.state.Store(int32(StatePresent))
	return nil
}

func (s *fakeStep) Reverse(ctx context.Context) error {
	if s.reverseErr != nil {
		return s.reverseErr
	}
	s.reversed.Store(true)
	s.state.Store(int32(StateAbsent))
	return nil
}

func (s *fakeStep) Inspect(ctx context.Context) StepState {
	if s.inspectFunc != nil {
		return s.inspectFunc()
	}
	return StepState(s.state.Load())
}

func TestPlanApplyAllSucceeds(t *testing.T) {
	a, b, c := newFake("a"), newFake("b"), newFake("c")
	plan := NewPlan(a, b, c)
	res := plan.Apply(context.Background())
	if res.Err != nil {
		t.Fatalf("err=%v", res.Err)
	}
	want := []string{"a", "b", "c"}
	if !equalSlice(res.Applied, want) {
		t.Errorf("applied=%v want %v", res.Applied, want)
	}
	if !a.applied.Load() || !b.applied.Load() || !c.applied.Load() {
		t.Errorf("not all steps applied")
	}
	if len(res.RolledBack) != 0 {
		t.Errorf("rollback fired on success: %v", res.RolledBack)
	}
}

func TestPlanApplyRollsBackOnFailure(t *testing.T) {
	a, b, c := newFake("a"), newFake("b"), newFake("c")
	b.applyErr = errors.New("bang")
	plan := NewPlan(a, b, c)
	res := plan.Apply(context.Background())
	if res.Err == nil {
		t.Fatalf("expected error from step b")
	}
	if !a.applied.Load() {
		t.Errorf("step a should have applied before rollback")
	}
	if !a.reversed.Load() {
		t.Errorf("step a should have been rolled back")
	}
	if b.applied.Load() || c.applied.Load() {
		t.Errorf("steps after failure should not have applied")
	}
	if c.reversed.Load() {
		t.Errorf("step c was never applied; should not be reversed")
	}
	if !contains(res.RolledBack, "a") {
		t.Errorf("rollback list missing a: %v", res.RolledBack)
	}
}

func TestPlanApplyContextCancelBeforeStart(t *testing.T) {
	a := newFake("a")
	plan := NewPlan(a)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := plan.Apply(ctx)
	if res.Err == nil {
		t.Errorf("expected context error")
	}
	if a.applied.Load() {
		t.Errorf("step should not have applied with cancelled context")
	}
}

func TestPlanApplyContextCancelMidway(t *testing.T) {
	a, b := newFake("a"), newFake("b")
	plan := NewPlan(a, b)
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel between step a and step b by inserting a step that cancels.
	canceller := &fakeStep{
		name: "cancel",
	}
	canceller.applyErr = nil
	plan = NewPlan(a, &cancelStep{cancel: cancel, fake: canceller}, b)
	res := plan.Apply(ctx)
	if res.Err == nil {
		t.Errorf("expected error from context cancellation")
	}
	if b.applied.Load() {
		t.Errorf("step b should not apply after cancel")
	}
}

type cancelStep struct {
	cancel context.CancelFunc
	fake   *fakeStep
}

func (s *cancelStep) Name() string { return s.fake.Name() }
func (s *cancelStep) Apply(ctx context.Context) error {
	if err := s.fake.Apply(ctx); err != nil {
		return err
	}
	s.cancel()
	return nil
}
func (s *cancelStep) Reverse(ctx context.Context) error { return s.fake.Reverse(ctx) }
func (s *cancelStep) Inspect(ctx context.Context) StepState {
	return s.fake.Inspect(ctx)
}

func TestPlanReverseAllSteps(t *testing.T) {
	a, b, c := newFake("a"), newFake("b"), newFake("c")
	plan := NewPlan(a, b, c)
	plan.Apply(context.Background())
	res := plan.Reverse(context.Background())
	if res.Err() != nil {
		t.Errorf("Reverse err=%v", res.Err())
	}
	want := []string{"c", "b", "a"}
	if !equalSlice(res.Reversed, want) {
		t.Errorf("reverse order=%v want %v", res.Reversed, want)
	}
	if !a.reversed.Load() || !b.reversed.Load() || !c.reversed.Load() {
		t.Errorf("not all steps reversed")
	}
}

func TestPlanReverseCollectsErrorsButContinues(t *testing.T) {
	a, b, c := newFake("a"), newFake("b"), newFake("c")
	b.reverseErr = errors.New("b broke")
	plan := NewPlan(a, b, c)
	plan.Apply(context.Background())
	res := plan.Reverse(context.Background())
	if res.Err() == nil {
		t.Fatalf("expected aggregate err")
	}
	if len(res.Errors) != 1 {
		t.Errorf("errors=%d want 1", len(res.Errors))
	}
	// Even though b failed, a and c reversed.
	if !a.reversed.Load() || !c.reversed.Load() {
		t.Errorf("good steps did not reverse: a=%v c=%v", a.reversed.Load(), c.reversed.Load())
	}
	if !contains(res.Reversed, "c") || !contains(res.Reversed, "a") {
		t.Errorf("reversed list missing entries: %v", res.Reversed)
	}
}

func TestReverseResultErrNilWhenEmpty(t *testing.T) {
	if (ReverseResult{}).Err() != nil {
		t.Errorf("empty ReverseResult should have nil Err")
	}
}

func TestPlanInspectReportsEachState(t *testing.T) {
	a := newFake("a")
	a.state.Store(int32(StatePresent))
	b := newFake("b")
	b.state.Store(int32(StateAbsent))
	c := newFake("c")
	c.state.Store(int32(StatePartial))
	plan := NewPlan(a, b, c)
	res := plan.Inspect(context.Background())
	if res.States["a"] != StatePresent {
		t.Errorf("a not present")
	}
	if res.States["b"] != StateAbsent {
		t.Errorf("b not absent")
	}
	if res.States["c"] != StatePartial {
		t.Errorf("c not partial")
	}
	if !equalSlice(res.Order, []string{"a", "b", "c"}) {
		t.Errorf("order wrong: %v", res.Order)
	}
}

func TestOverallStateAllPresent(t *testing.T) {
	a, b := newFake("a"), newFake("b")
	a.state.Store(int32(StatePresent))
	b.state.Store(int32(StatePresent))
	plan := NewPlan(a, b)
	if s := plan.Inspect(context.Background()).OverallState(); s != StatePresent {
		t.Errorf("got %s want present", s)
	}
}

func TestOverallStateAllAbsent(t *testing.T) {
	plan := NewPlan(newFake("a"), newFake("b"))
	if s := plan.Inspect(context.Background()).OverallState(); s != StateAbsent {
		t.Errorf("got %s want absent", s)
	}
}

func TestOverallStateMixedYieldsPartial(t *testing.T) {
	a, b := newFake("a"), newFake("b")
	a.state.Store(int32(StatePresent))
	plan := NewPlan(a, b)
	if s := plan.Inspect(context.Background()).OverallState(); s != StatePartial {
		t.Errorf("got %s want partial", s)
	}
}

func TestOverallStatePartialEarlyReturn(t *testing.T) {
	a := newFake("a")
	a.state.Store(int32(StatePartial))
	plan := NewPlan(a)
	if s := plan.Inspect(context.Background()).OverallState(); s != StatePartial {
		t.Errorf("got %s want partial", s)
	}
}

func TestOverallStateUnknownEscalates(t *testing.T) {
	a, b := newFake("a"), newFake("b")
	a.state.Store(int32(StatePresent))
	b.state.Store(int32(StateUnknown))
	plan := NewPlan(a, b)
	if s := plan.Inspect(context.Background()).OverallState(); s != StateUnknown {
		t.Errorf("got %s want unknown", s)
	}
}

func TestOverallStateEmptyPlan(t *testing.T) {
	plan := NewPlan()
	if s := plan.Inspect(context.Background()).OverallState(); s != StateAbsent {
		t.Errorf("empty plan should be absent, got %s", s)
	}
}

func TestStepStateString(t *testing.T) {
	cases := map[StepState]string{
		StateAbsent:  "absent",
		StatePartial: "partial",
		StatePresent: "present",
		StateUnknown: "unknown",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("%d→%s want %s", s, got, want)
		}
	}
}

func TestInspectFuncWired(t *testing.T) {
	calls := 0
	s := &fakeStep{
		name: "x",
		inspectFunc: func() StepState {
			calls++
			return StatePartial
		},
	}
	if s.Inspect(context.Background()) != StatePartial {
		t.Errorf("inspectFunc not honoured")
	}
	if calls != 1 {
		t.Errorf("inspectFunc called %d times", calls)
	}
}

// utilities

func equalSlice[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

func contains[T comparable](a []T, v T) bool {
	for _, x := range a {
		if x == v {
			return true
		}
	}
	return false
}

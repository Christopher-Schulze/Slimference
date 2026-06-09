package proxy

import (
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func TestAnyProviderDegraded_NoSignals(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	if p.AnyProviderDegraded() {
		t.Fatal("idle providers must not register as degraded")
	}
}

func TestAnyProviderDegraded_NilMonitorReturnsFalse(t *testing.T) {
	t.Parallel()
	p := &Proxy{}
	if p.AnyProviderDegraded() {
		t.Fatal("nil monitor must report false")
	}
}

func TestAnyProviderDegraded_SignalsDownAfterFailures(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	// Three consecutive failures on one provider trigger Down.
	for i := 0; i < 3; i++ {
		p.healthMon.record(types.Anthropic, false)
	}
	if !p.AnyProviderDegraded() {
		t.Fatal("expected degradation after 3 failures")
	}
}

func TestAnyProviderDegraded_HealthyDoesNotTrigger(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	for i := 0; i < 5; i++ {
		p.healthMon.record(types.Anthropic, true)
	}
	if p.AnyProviderDegraded() {
		t.Fatal("healthy provider must not register as degraded")
	}
}

func TestHealthMonitor_CodexProviderTracked(t *testing.T) {
	t.Parallel()
	mon := newHealthMonitor()
	if _, ok := mon.results[types.CodexChatGPT]; !ok {
		t.Fatal("Codex should be tracked")
	}
}

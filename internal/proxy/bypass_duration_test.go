package proxy

import (
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
)

func TestSetBypassFor_AutoRevertsOnDeadline(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	p.SetBypassFor(20 * time.Millisecond)
	if !p.Bypass() {
		t.Fatal("bypass must be on immediately after SetBypassFor")
	}
	deadline := p.BypassExpiresAt()
	if deadline.IsZero() {
		t.Fatal("BypassExpiresAt must be non-zero")
	}
	time.Sleep(40 * time.Millisecond)
	if p.Bypass() {
		t.Fatal("bypass must auto-revert past the deadline")
	}
	if c := p.BypassAutoRevertCount(); c != 1 {
		t.Fatalf("auto-revert counter must be 1, got %d", c)
	}
	if !p.BypassExpiresAt().IsZero() {
		t.Fatal("BypassExpiresAt must reset after auto-revert")
	}
}

func TestSetBypassFor_NonPositiveTreatedAsInfinite(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	p.SetBypassFor(0)
	if !p.Bypass() {
		t.Fatal("bypass must be on")
	}
	if !p.BypassExpiresAt().IsZero() {
		t.Fatal("zero duration must not set deadline")
	}
	p.SetBypassFor(-time.Second)
	if !p.Bypass() {
		t.Fatal("bypass must stay on for negative duration")
	}
	if !p.BypassExpiresAt().IsZero() {
		t.Fatal("negative duration must not set deadline")
	}
}

func TestSetBypass_FalseClearsTimer(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	p.SetBypassFor(time.Hour)
	if p.BypassExpiresAt().IsZero() {
		t.Fatal("expected non-zero deadline")
	}
	p.SetBypass(false)
	if !p.BypassExpiresAt().IsZero() {
		t.Fatal("SetBypass(false) must clear the deadline")
	}
}

func TestBypassExpiresAt_OffWhenBypassOff(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	if !p.BypassExpiresAt().IsZero() {
		t.Fatal("default deadline must be zero")
	}
}

func TestSetBypassForNextRequests_ConsumeFlipsOff(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	p.SetBypassForNextRequests(2)
	if !p.Bypass() {
		t.Fatal("bypass must be on after SetBypassForNextRequests")
	}
	if p.BypassNextRequestCount() != 2 {
		t.Fatalf("counter: %d", p.BypassNextRequestCount())
	}
	if flipped := p.ConsumeBypassRequest(); flipped {
		t.Fatal("first consume should not flip yet")
	}
	if !p.Bypass() {
		t.Fatal("bypass should still be on after one consume")
	}
	if flipped := p.ConsumeBypassRequest(); !flipped {
		t.Fatal("second consume must flip bypass off")
	}
	if p.Bypass() {
		t.Fatal("bypass must be off after final consume")
	}
}

func TestSetBypassForNextRequests_ZeroTreatedAsOne(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	p.SetBypassForNextRequests(0)
	if p.BypassNextRequestCount() != 1 {
		t.Fatalf("expected 1, got %d", p.BypassNextRequestCount())
	}
	p.SetBypassForNextRequests(-3)
	if p.BypassNextRequestCount() != 1 {
		t.Fatalf("expected 1, got %d", p.BypassNextRequestCount())
	}
}

func TestConsumeBypassRequest_NoOpWhenOff(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	if p.ConsumeBypassRequest() {
		t.Fatal("consume on inactive bypass must not flip")
	}
}

func TestConsumeBypassRequest_NoOpWhenNoCounter(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	p.SetBypassFor(time.Hour)
	if p.ConsumeBypassRequest() {
		t.Fatal("consume on duration-only bypass must not flip")
	}
	if !p.Bypass() {
		t.Fatal("bypass should still be on")
	}
}

func TestBypass_NeverOnReturnsFalse(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	if p.Bypass() {
		t.Fatal("default bypass must be off")
	}
	if c := p.BypassAutoRevertCount(); c != 0 {
		t.Fatalf("default counter must be 0, got %d", c)
	}
}

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

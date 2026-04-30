package proxy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
)

func TestApplyDrainTimeout_NilConfig(t *testing.T) {
	t.Parallel()
	p := &Proxy{}
	ctx, cancel := p.applyDrainTimeout(context.Background())
	defer cancel()
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("nil config must not add a deadline")
	}
}

func TestApplyDrainTimeout_ZeroDuration(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Proxy.DrainTimeoutSeconds = 0
	p := &Proxy{config: cfg}
	ctx, cancel := p.applyDrainTimeout(context.Background())
	defer cancel()
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("zero duration must not add a deadline")
	}
}

func TestApplyDrainTimeout_AddsDeadline(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Proxy.DrainTimeoutSeconds = 5
	p := &Proxy{config: cfg}
	ctx, cancel := p.applyDrainTimeout(context.Background())
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("expected deadline")
	}
}

func TestApplyDrainTimeout_PreservesExistingDeadline(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Proxy.DrainTimeoutSeconds = 60
	p := &Proxy{config: cfg}
	parent, parentCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer parentCancel()
	ctx, cancel := p.applyDrainTimeout(parent)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected deadline")
	}
	parentDeadline, _ := parent.Deadline()
	if !deadline.Equal(parentDeadline) {
		t.Fatalf("must preserve parent deadline; got %v want %v", deadline, parentDeadline)
	}
}

func TestShutdown_DrainTimeoutTriggersErrShutdownTimeout(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Proxy.DrainTimeoutSeconds = 1
	p := New(cfg)
	// Block worker goroutine so wg.Wait can't complete in time.
	p.wg.Add(1)
	defer p.wg.Done()
	prev := shutdownDumpWriterFn
	shutdownDumpWriterFn = func() (string, error) { return "", nil }
	defer func() { shutdownDumpWriterFn = prev }()
	err := p.Shutdown(context.Background())
	if !errors.Is(err, ErrShutdownTimeout) {
		t.Fatalf("expected ErrShutdownTimeout, got %v", err)
	}
}

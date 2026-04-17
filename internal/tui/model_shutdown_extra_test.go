package tui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type shutdownCtxProxy struct {
	*mockProxy
	gotDeadline bool
}

func (p *shutdownCtxProxy) Shutdown(ctx context.Context) error {
	p.shutdownCalled = true
	_, p.gotDeadline = ctx.Deadline()
	return nil
}

func TestUpdate_CtrlC_UsesTimedShutdownContext(t *testing.T) {
	t.Parallel()

	orig := shutdownTimeout
	shutdownTimeout = 250 * time.Millisecond
	defer func() { shutdownTimeout = orig }()

	p := &shutdownCtxProxy{mockProxy: newMockProxy()}
	m := NewModel(p)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	if !p.shutdownCalled {
		t.Fatal("Shutdown should have been called on ctrl+c")
	}
	if !p.gotDeadline {
		t.Fatal("Shutdown should receive a timed context")
	}
	if cmd == nil {
		t.Fatal("expected a quit command on ctrl+c")
	}
}

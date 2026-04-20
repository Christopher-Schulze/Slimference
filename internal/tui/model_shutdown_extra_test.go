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
	// Intentionally NOT t.Parallel: mutates package-level shutdownTimeout
	// which TestUpdate_CtrlC reads concurrently. Running in parallel races
	// under -race. The test is fast (<50 ms) so serialising is harmless.

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

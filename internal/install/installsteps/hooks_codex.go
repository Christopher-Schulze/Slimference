package installsteps

import (
	"context"
	"errors"
	"os"

	"github.com/Christopher-Schulze/Slimference/internal/control/reversibility"
	"github.com/Christopher-Schulze/Slimference/internal/hooks"
)

// HooksCodex wraps internal/hooks.InstallCodex / RemoveCodex into a
// reversibility.Step. Apply writes hook scripts under
// ~/.slimference/hooks/ and merges hook entries into ~/.codex/
// hooks.json. Reverse removes those entries and scripts.
type HooksCodex struct {
	// Home is the user home directory.
	Home string
	// BinaryPath is the absolute path to the slimference binary that
	// the hook scripts will invoke. Empty falls back to "slimference"
	// (assumes PATH).
	BinaryPath string
}

const hooksCodexStepName = "hooks.codex"

// Name implements reversibility.Step.
func (s *HooksCodex) Name() string { return hooksCodexStepName }

// Apply installs the Codex hook scripts + hooks.json entries.
func (s *HooksCodex) Apply(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Home == "" {
		return errors.New("hooks.codex: Home empty")
	}
	return hooks.InstallCodex(s.Home, s.BinaryPath)
}

// Reverse removes the Codex hook scripts + hooks.json entries.
func (s *HooksCodex) Reverse(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Home == "" {
		return errors.New("hooks.codex: Home empty")
	}
	return hooks.RemoveCodex(s.Home)
}

// Inspect reports whether the post-tool hook script is present on
// disk. That single marker is enough because InstallCodex writes
// scripts + hooks.json atomically together.
func (s *HooksCodex) Inspect(ctx context.Context) reversibility.StepState {
	if s.Home == "" {
		return reversibility.StateUnknown
	}
	scriptPath := hooks.CodexHookScriptPath(s.Home)
	if _, err := os.Stat(scriptPath); err == nil {
		return reversibility.StatePresent
	}
	return reversibility.StateAbsent
}

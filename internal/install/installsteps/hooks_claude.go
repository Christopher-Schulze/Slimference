package installsteps

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/slimference/slimference/internal/control/reversibility"
	"github.com/slimference/slimference/internal/hooks"
)

// HooksClaude wraps internal/hooks.InstallClaude / RemoveClaude into a
// reversibility.Step. Apply writes ~/.claude/hooks/slimference-*.sh
// and merges PreToolUse / PreCompact entries into settings.json.
// Reverse strips those entries and removes the scripts.
type HooksClaude struct {
	Home       string
	BinaryPath string
}

const hooksClaudeStepName = "hooks.claude"

// Name implements reversibility.Step.
func (s *HooksClaude) Name() string { return hooksClaudeStepName }

// Apply installs Claude hook scripts + settings merge.
func (s *HooksClaude) Apply(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Home == "" {
		return errors.New("hooks.claude: Home empty")
	}
	return hooks.InstallClaude(s.Home, s.BinaryPath)
}

// Reverse removes the Claude hook scripts + settings entries.
func (s *HooksClaude) Reverse(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Home == "" {
		return errors.New("hooks.claude: Home empty")
	}
	return hooks.RemoveClaude(s.Home)
}

// Inspect reports whether the rewrite hook script is on disk.
func (s *HooksClaude) Inspect(ctx context.Context) reversibility.StepState {
	if s.Home == "" {
		return reversibility.StateUnknown
	}
	scriptPath := filepath.Join(s.Home, ".claude", "hooks", "slimference-rewrite.sh")
	if _, err := os.Stat(scriptPath); err == nil {
		return reversibility.StatePresent
	}
	return reversibility.StateAbsent
}

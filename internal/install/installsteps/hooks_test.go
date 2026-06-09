package installsteps

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/control/reversibility"
	"github.com/Christopher-Schulze/Slimference/internal/hooks"
)

func TestHooksCodexApplyAndReverse(t *testing.T) {
	home := t.TempDir()
	step := &HooksCodex{Home: home, BinaryPath: "/usr/local/bin/slimference"}

	if got := step.Inspect(context.Background()); got != reversibility.StateAbsent {
		t.Errorf("pre-apply Inspect = %v, want StateAbsent", got)
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := os.Stat(hooks.CodexHookScriptPath(home)); err != nil {
		t.Errorf("post-tool script missing: %v", err)
	}
	if got := step.Inspect(context.Background()); got != reversibility.StatePresent {
		t.Errorf("post-apply Inspect = %v, want StatePresent", got)
	}
	if err := step.Reverse(context.Background()); err != nil {
		t.Fatalf("Reverse: %v", err)
	}
}

func TestHooksCodexEmptyHomeRejected(t *testing.T) {
	step := &HooksCodex{}
	if err := step.Apply(context.Background()); err == nil {
		t.Error("Apply with empty Home should error")
	}
	if err := step.Reverse(context.Background()); err == nil {
		t.Error("Reverse with empty Home should error")
	}
	if got := step.Inspect(context.Background()); got != reversibility.StateUnknown {
		t.Errorf("Inspect with empty Home = %v, want StateUnknown", got)
	}
}

func TestHooksCodexContextCancelled(t *testing.T) {
	step := &HooksCodex{Home: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := step.Apply(ctx); err == nil {
		t.Error("Apply with cancelled ctx should error")
	}
	if err := step.Reverse(ctx); err == nil {
		t.Error("Reverse with cancelled ctx should error")
	}
}

func TestHooksCodexNameStable(t *testing.T) {
	if (&HooksCodex{}).Name() != "hooks.codex" {
		t.Error("Name mismatch")
	}
}

func TestHooksClaudeApplyAndReverse(t *testing.T) {
	home := t.TempDir()
	step := &HooksClaude{Home: home, BinaryPath: "/usr/local/bin/slimference"}

	if got := step.Inspect(context.Background()); got != reversibility.StateAbsent {
		t.Errorf("pre-apply Inspect = %v, want StateAbsent", got)
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	scriptPath := filepath.Join(home, ".claude", "hooks", "slimference-rewrite.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Errorf("rewrite script missing: %v", err)
	}
	if got := step.Inspect(context.Background()); got != reversibility.StatePresent {
		t.Errorf("post-apply Inspect = %v, want StatePresent", got)
	}
	if err := step.Reverse(context.Background()); err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	if _, err := os.Stat(scriptPath); err == nil {
		t.Error("rewrite script still present after Reverse")
	}
}

func TestHooksClaudeEmptyHomeRejected(t *testing.T) {
	step := &HooksClaude{}
	if err := step.Apply(context.Background()); err == nil {
		t.Error("Apply with empty Home should error")
	}
	if err := step.Reverse(context.Background()); err == nil {
		t.Error("Reverse with empty Home should error")
	}
	if got := step.Inspect(context.Background()); got != reversibility.StateUnknown {
		t.Errorf("Inspect with empty Home = %v, want StateUnknown", got)
	}
}

func TestHooksClaudeContextCancelled(t *testing.T) {
	step := &HooksClaude{Home: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := step.Apply(ctx); err == nil {
		t.Error("Apply with cancelled ctx should error")
	}
	if err := step.Reverse(ctx); err == nil {
		t.Error("Reverse with cancelled ctx should error")
	}
}

func TestHooksClaudeNameStable(t *testing.T) {
	if (&HooksClaude{}).Name() != "hooks.claude" {
		t.Error("Name mismatch")
	}
}

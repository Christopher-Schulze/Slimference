package installsteps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/control/reversibility"
)

// Notice writes a README file into a third-party folder we just
// modified (typically ~/.codex/ or ~/.claude/), explaining what
// Slimference changed, why, and how to revert. Idempotent: Apply
// overwrites with the latest content; Reverse removes the file.
//
// The README is marker-fenced via its own filename + a magic header
// line so an agent can recognise it unambiguously and so concurrent
// edits by humans cannot corrupt our content.
type Notice struct {
	// Path is the absolute path of the README to write.
	Path string
	// Title is the file's H1 (e.g. "Slimference touched this folder").
	Title string
	// Body is the prose between the H1 and the auto-footer. Markdown.
	Body string
	// AppName labels the section under "How to revert" — e.g.
	// "Codex CLI / Desktop App" or "Claude Code".
	AppName string
	// StepName overrides the reversibility.Step name. Defaults to
	// "notice.<basename-without-ext>".
	StepName string
	// Now overrides time.Now for the footer; tests inject deterministic.
	Now func() time.Time
	// Version is rendered into the footer; defaults to "unknown".
	Version string
}

const noticeMarker = "<!-- slimference:notice -->"

// Name implements reversibility.Step.
func (s *Notice) Name() string {
	if s.StepName != "" {
		return s.StepName
	}
	base := strings.TrimSuffix(filepath.Base(s.Path), filepath.Ext(s.Path))
	if base == "" {
		return "notice"
	}
	return "notice." + strings.ToLower(base)
}

// Apply writes the README file. If the directory does not exist we
// do NOT create it - the Notice is meant to land alongside hooks that
// the prior Step (HooksCodex / HooksClaude) installed, which already
// guarantees the directory exists. Missing directory is a real error.
func (s *Notice) Apply(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.validate(); err != nil {
		return err
	}
	now := s.clock()
	content := s.render(now)
	return writeAtomic(s.Path, []byte(content), 0o644)
}

// Reverse removes the README. Idempotent: missing file is success.
// The file is removed UNCONDITIONALLY when our marker is present, so
// a human edit that doesn't strip the marker still gets cleaned up.
// If the marker is missing (someone replaced the entire file), we
// leave it alone to avoid clobbering user content.
func (s *Notice) Reverse(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Path == "" {
		return errors.New("notice: Path empty")
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !strings.Contains(string(data), noticeMarker) {
		// Not ours anymore. Don't delete.
		return nil
	}
	if err := os.Remove(s.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Inspect reports whether our notice is on disk. We check the marker
// so a human-edited replacement reads as "absent" (we are not
// installed there).
func (s *Notice) Inspect(ctx context.Context) reversibility.StepState {
	if s.Path == "" {
		return reversibility.StateUnknown
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return reversibility.StateAbsent
		}
		return reversibility.StateUnknown
	}
	if strings.Contains(string(data), noticeMarker) {
		return reversibility.StatePresent
	}
	return reversibility.StateAbsent
}

func (s *Notice) validate() error {
	if s.Path == "" {
		return errors.New("notice: Path empty")
	}
	if s.Title == "" {
		return errors.New("notice: Title empty")
	}
	if s.AppName == "" {
		return errors.New("notice: AppName empty")
	}
	dir := filepath.Dir(s.Path)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("notice: parent dir %q: %w", dir, err)
	}
	return nil
}

func (s *Notice) clock() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

// render assembles the README content. The marker is on the first
// line so any agent reading the file can pattern-match immediately.
func (s *Notice) render(now time.Time) string {
	version := s.Version
	if version == "" {
		version = "unknown"
	}
	body := s.Body
	if body == "" {
		body = "(no extra detail)"
	}
	return fmt.Sprintf(`%s
# %s

%s

## How to revert

The cleanest path:

    slimference uninstall

This reverses every change Slimference made (this folder included).
Backups land under ~/.slimference/backups/ and are preserved across
uninstalls so you can recover any pre-change state.

Manual revert for %s only:

1. Inspect the entries this folder owns that point at ~/.slimference/.
2. Remove those entries; they will not be re-added unless you run
   `+"`slimference install`"+` again.

## Why these changes

Slimference is a local proxy that sits between this client and the
upstream LLM. The hooks installed here let the proxy:

- Detect compaction boundaries (PreCompact / PostCompact) so the
  prompt-cache state and tool-result archives stay in sync.
- Apply output-token reductions (T165-T186 family) at the right
  moment in your conversation lifecycle.

Without these hooks, the proxy still intercepts traffic — but it loses
some optimization opportunities.

## Verifying

    slimference status

Look for "Hooks: ✓" rows in the table for this app.

## Where to learn more

- Install / uninstall SSOT: docs/install.md inside your local Slimference repo.
- Agent rules: agents.md inside your local Slimference repo.
- This file is generated; do not edit by hand.

---

Installed by Slimference %s at %s.
`,
		noticeMarker,
		s.Title,
		body,
		s.AppName,
		version,
		now.Format(time.RFC3339),
	)
}

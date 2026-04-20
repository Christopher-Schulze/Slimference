# T64 - TUI Keybindings + Error-Modal ESC-Path Dokumentieren und Härten

Status: todo
Priority: P2
Scope: `internal/tui/`, `docs/documentation.md`, `docs/tui-keybindings.md` (new)
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

TUI keybindings are not discoverable:

1. No in-TUI help overlay (`?` does nothing in some views).
2. Error modal is blocking; ESC handling is inconsistent (some modals
   trap ESC, others ignore).
3. Quit key varies across views.
4. No documentation file listing keys.

Result: new users stare at the UI and try `Ctrl+C`, breaking the
process rather than dismissing a modal.

## Current State

- Bindings exist per view but are not consolidated.
- `q` quits in some views, others require `Ctrl+C`.
- Error modal renders but traps all input.
- No help overlay.

## Target State

### Global keys (every view)

| Key | Action |
|-----|--------|
| `?` or `F1` | toggle help overlay |
| `q` | leave current view → previous, or quit if at root |
| `Ctrl+C` | quit immediately (graceful) |
| `Esc` | dismiss any modal / overlay / menu, else same as `q` |
| `Tab` / `Shift-Tab` | cycle focused view |
| `r` | context-specific refresh OR (in Stats) redaction toggle (T59) |

### Per-view keys

| View | Key | Action |
|------|-----|--------|
| Dashboard | `1-6` | switch layer toggles |
| Stats | `j/k` | scroll phase table |
| Debug | `/` | filter decision chain |
| Debug | `Enter` | expand entry |

### Error modal

- `Esc` always dismisses.
- `Enter` dismisses and copies error text to clipboard
  (via `golang.design/x/clipboard` already in use elsewhere; if not,
  fall back to slog).
- Max width 80 cols; long stacks are scrollable.
- Auto-dismiss after 30 s if untouched (configurable).

### Help overlay

Renders a table of keys; dismissed with `?`, `Esc`, or `q`. Available
from every view.

### Documentation

`docs/tui-keybindings.md` - canonical list. Rendered also as the help
overlay content (single source).

## Design

### Single-source keybindings

`internal/tui/keys.go`:

```go
type Binding struct {
    Keys        []string
    Description string
    Scope       string  // "global" | view name
}

var Bindings = []Binding{
    {Keys: []string{"?", "f1"}, Description: "Toggle help overlay", Scope: "global"},
    {Keys: []string{"q"},        Description: "Back / quit",         Scope: "global"},
    {Keys: []string{"ctrl+c"},   Description: "Quit immediately",    Scope: "global"},
    // ...
}

func RenderHelpTable() string
func RenderMarkdown() string   // used to regenerate docs/tui-keybindings.md
```

Doc-lint CI step regenerates `docs/tui-keybindings.md` from
`Bindings` and fails if checked-in file is stale.

### Modal handling

A shared `modalManager` ensures Esc is always handled uniformly:

```go
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    if m.modal != nil {
        switch msg := msg.(type) {
        case tea.KeyMsg:
            if msg.String() == "esc" { m.modal = nil; return m, nil }
            // ...
        }
    }
    // ...
}
```

Every modal registers through the manager; no ad-hoc modals allowed.

### Auto-dismiss timer

Modal struct carries `DismissAfter time.Duration`. `tea.Tick` messages
count down and send `DismissMsg`.

### Clipboard on error modal

Use `clipboard.Write(clipboard.FmtText, []byte(errText))` when
available; on failure, just log.

## Implementation Plan

### WP1 - Single-source `keys.go`.
### WP2 - Modal manager + Esc uniformity.
### WP3 - Help overlay renderer.
### WP4 - Error modal enhancements (copy, auto-dismiss).
### WP5 - Regenerate `docs/tui-keybindings.md`.
### WP6 - Doc-lint CI gate.
### WP7 - Tests
- Model update with Esc + modal dismisses correctly.
- Help overlay toggles.
- Auto-dismiss fires on tick.

---

## Subtasks

- [ ] `internal/tui/keys.go` single-source bindings.
- [ ] Modal manager refactor.
- [ ] Help overlay view.
- [ ] Error modal: clipboard + auto-dismiss.
- [ ] Per-view bindings normalised (q, r, j/k, /, Enter).
- [ ] Regenerate `docs/tui-keybindings.md`.
- [ ] Doc-lint CI gate for keybindings.
- [ ] Unit tests for modal manager.

## Risks

- Refactoring modals may surface latent bugs in existing views.
  Mitigation: thorough test coverage before and after.
- Clipboard access may prompt macOS security permission; fall back
  silently.

## Acceptance Criteria

- [ ] Every view responds to `?` with help overlay.
- [ ] Esc dismisses any modal in every view.
- [ ] `docs/tui-keybindings.md` auto-generated and checked in.
- [ ] `go test -race ./internal/tui/...` green.
- [ ] Error modal supports copy + auto-dismiss.

## Out of Scope

- Theme customisation.
- Full mouse support beyond what BubbleTea already provides.

---

## Validation

```
go test -race ./internal/tui/...
./slimference         # press ?, q, esc across views
cat docs/tui-keybindings.md
```

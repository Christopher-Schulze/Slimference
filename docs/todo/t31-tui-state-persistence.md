# T31 - TUI State Persistence Across Restarts

Status: open
Priority: low
Scope: internal/tui, internal/config (state file)

---

## Problem

The TUI holds user-facing preferences that reset on every start:

- Provider toggles (anthropic/openai enabled).
- Layer toggles (L1/L2/L3).
- Active view (dashboard, analytics, debug, hooks).
- Filter / sort / focus positions.

Every start is a clean slate. A small annoyance in isolation; in practice,
repeatedly toggling back to the same configuration.

---

## Desired End State

- TUI reads and writes a state file on top of the existing config dir, e.g.
  `~/.slimference/tui_state.json`.
- State includes: provider toggles, layer toggles, current view, sort/filter
  choices, pinned panels.
- File is written on quit and on explicit "save" key press.
- File is read on startup, merged with config defaults.
- Human-readable JSON, small (<1 KB typical).

---

## Work Packages

### WP1 - State schema

- Define `type TUIState struct{...}` in `internal/tui`.
- Serialize with `encoding/json`.

### WP2 - Persistence hooks

- Load state at `NewModel` time.
- Save on `tea.Quit` and on `ctrl+s` keybinding.
- Tolerate missing file, parse errors fall back to defaults.

### WP3 - Apply on init

- After load, project toggles onto the proxy (same API the TUI already uses
  to flip toggles at runtime).

### WP4 - Tests

- Save then load: roundtrip preserves fields.
- Corrupt file: falls back to defaults without crashing.
- New field added later: missing field -> default, no migration needed.

---

## Subtasks

- [ ] Define `TUIState` and JSON encoding.
- [ ] Hook load on init, save on quit / `ctrl+s`.
- [ ] Apply toggles to proxy after load.
- [ ] Tests: roundtrip, corrupt file, forward-compat.
- [ ] Document in `docs/documentation.md` TUI section.

## Acceptance Criteria

- User toggles survive restarts.
- Zero crash on missing or corrupt state file.
- Coverage stays at 100 %.

# T67 - TUI Master Switch + Integration Status Panel

Status: todo
Priority: P1
Scope: `internal/tui/`, `internal/proxy/admin.go`, `cmd/slimference/`
Driver: Users need a hot-reload bypass and at-a-glance integration state.

---

## Problem

With T65 (auto-integration) and T66 (Codex routing) both landed, users will
have Slimference wired into two clients at once. They need one place to see:

- Is the daemon alive?
- Is Claude Code wired? Codex wired?
- Is the proxy actively compressing, or in pass-through?
- How do I kill compression fast if a request feels off?

## Target State

### Integration panel (top of dashboard)

```
╭─ Slimference 2.1.0 ─────────────────────────────────────╮
│ Daemon:  ● running   uptime 04:13:22   pid 78432         │
│ Bypass:  OFF          (B to toggle)                      │
│                                                          │
│ Claude Code: ● wired  hooks ✓  env ✓  traffic: 42 req    │
│ Codex:       ● wired  hooks ✓  cfg ✓  traffic: 18 req    │
╰──────────────────────────────────────────────────────────╯
```

Status colours:
- `● wired` green = everything installed and recent traffic seen.
- `○ partial` amber = binary found but at least one wiring missing.
- `○ absent` grey = client not detected on this machine.
- `◉ error` red = wiring present but proxy rejected last 3 requests.

### Master bypass switch

Hotkey: `B` (bypass). Confirmation modal:

```
╭─ Bypass mode ─────────────────────────────╮
│                                            │
│ Disable ALL compression layers?            │
│ Proxy will forward bytes unmodified.       │
│ Toggle off again with B.                   │
│                                            │
│    [ confirm ]     [ cancel ]              │
╰────────────────────────────────────────────╯
```

When bypass is on:

- Banner at top of TUI: `⚠ BYPASS ACTIVE - no compression`.
- All three layer toggles go grey/inactive in the dashboard.
- Admin `/admin/status.bypass = true` is surfaced.
- Analytics snapshot keeps counting requests but `tokens_saved = 0`.

Bypass is a **single atomic flag** on the proxy, not three separate layer
toggles, so the state is unambiguous. Internally it short-circuits
`isLayerEnabled` and `isProviderEnabled` to return false, leaving the existing
"all layers disabled" passthrough path to handle the forward.

Bypass survives daemon restarts (persisted in `~/.slimference/state.json`).

### Integration status source

Pulled via `/admin/status.integration`:

```json
"integration": {
  "claude": {"wired": true,  "hooks": true,  "env": true,
             "last_seen_unix": 1713623112, "request_count": 42},
  "codex":  {"wired": true,  "hooks": true,  "config": true,
             "last_seen_unix": 1713623130, "request_count": 18},
  "bypass": false
}
```

Dashboard renders this block every tick (500 ms). The TUI does NOT call the
integrate detection itself - that lives server-side on the daemon so the admin
API is the single source of truth.

## Design

### Proxy-side

- New `internal/integrate/detect.go` (delivered by T65) exposes a
  `Snapshot()` that the admin endpoint calls.
- Bypass flag is `atomic.Bool` on `Proxy`. Getter returns pre-computed ANDed
  result with the per-layer flags.
- `Proxy.SetBypass(bool)` writes through to `state.json` asynchronously.
- Admin endpoint `POST /admin/bypass` with `{enabled: bool}`.

### TUI-side

- New `views.renderIntegrationPanel(m)`.
- Key binding `B` (added to `KeyMap`) with a confirm modal.
- Modal reuses the existing confirm-modal plumbing (T64 help overlay work lives
  in the same modalManager).
- Traffic counters ticker updates the `request_count` values from the
  rolling-window totals in `AdminStatus.Analytics`.

### State persistence

- `~/.slimference/state.json` already holds provider / layer toggles from T31.
- Add `"bypass": true` key alongside; load on startup, write on every toggle.
- On config validation failure, default bypass=false, log warn.

## Implementation Plan

### WP1 - Bypass atomic + admin endpoint.
### WP2 - Integration snapshot wired to admin status.
### WP3 - TUI integration panel renderer.
### WP4 - `B` keybinding + confirm modal.
### WP5 - State persistence across restarts.
### WP6 - Tests
- Atomic bypass toggle covers all three layers.
- Admin POST sets state + persists.
- TUI key handler enters modal then confirms then renders banner.
- Integration snapshot reflects detect results.

---

## Acceptance Criteria

- [ ] `B` toggles bypass in TUI; proxy forwards bytes unmodified while on.
- [ ] Bypass state survives daemon restart.
- [ ] Dashboard shows per-client wiring state with colour.
- [ ] `/admin/status.integration` returns the expected shape.
- [ ] `go test -race ./internal/tui/... ./internal/proxy/...` green.

## Out of Scope

- Per-client bypass (bypass is global - fine-grained control stays on the
  existing layer + provider toggles).
- Programmatic bypass scheduling.
- Per-project integration status.

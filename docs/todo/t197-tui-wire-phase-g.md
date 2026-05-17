# TASK 197: TUI rewire to Phase H 2-surface model

Status: PARTIAL 2026-05-17 (visible Phase H surface done; deeper API cleanup optional)
Priority: P1 — visible surface for the 2-surface architecture
Scope: `internal/tui/` (existing 7 800 LOC, edit in place; no rewrite),
       `cmd/slimference/main.go` (DI of `internal/install`)

## Why (revised)

Per Phase H (T200) the operative surface is now **two buttons** plus a
toggle and a status table:

```
[ Install ]   [ Uninstall ]
[ Enable / Disable transparent MITM ]
[ Apps: codex_cli on/off | codex_desktop_app on/off | claude_code on/off ]
[ Status table — auto-refreshes from /admin/state ]
```

The old setup-wizard with check/confirm/apply steps is replaced by a
single Install button that runs the reversibility.Plan and shows
live progress. No multi-step wizard. No "set OPENAI_API_BASE" option.
No "configure HTTPS_PROXY" option.

## Current reality 2026-05-17

The user-visible TUI surface has been corrected in T207:

- Apps view exists and is visible.
- Dashboard and setup copy point at `slimference install`, `enable`,
  `disable`, `uninstall`, and `status`.
- The TUI service adapter delegates to the top-level Phase H lifecycle
  command functions, not to legacy `proxy install` command plumbing.
- Claude Code is shown as off / opt-in later and pressing the old
  Claude shortcut no longer changes proxy routing.

The deeper internal interface cleanup below is still useful, but it is
no longer a blocker for the Phase H user flow. Existing method names
such as `InstallTransparent` remain as compatibility shims.

## Concrete gaps after T200/T201/T202

1. `internal/tui/state.go` has a hand-rolled `TransparentStatus`
   struct. **Replace** with `control.SetupState` (consumed via
   `/admin/state` HTTP call — TUI is a remote proxy adapter).
2. `internal/tui/model.go` `ProxyInterface` has 6 install-related
   methods (`InstallTransparent`, `EnableTransparent`,
   `DisableTransparent`, `UninstallTransparent`, `InstallService`,
   `UninstallService`). **Collapse** to:
   ```go
   Install(opts InstallOptions) error
   Uninstall(opts UninstallOptions) error
   Enable() error
   Disable() error
   ```
3. No "Apps" view exists. Add `ViewApps` as a new top-level view
   between Stats and Setup. Three rows, one per AppID, with `space`
   to toggle. POST `/admin/apps` on change.
4. Setup view becomes "Status" view: renders the SetupState table
   verbatim, with action hints at the bottom (`i = install`,
   `u = uninstall`, `e = enable/disable`).
5. MiniMax references in views.go must be removed (user explicitly
   ordered weeks ago). Stats view loses the "MiniMax (async)" latency
   row, the "Layer 2 MiniMax" label becomes just "Layer 2".

## Target state

### `internal/tui/model.go` — slimmed ProxyInterface

```go
type ProxyInterface interface {
    // Phase F / state introspection
    SetupState(ctx context.Context) (control.SetupState, error)
    GetAnalytics() analytics.AnalyticsSnapshot
    GetRecentRequests(n int) []types.RequestMetrics
    // … (existing read-only methods unchanged)

    // Phase H install/enable surface (new)
    Install(ctx context.Context) error
    Uninstall(ctx context.Context) error
    Enable(ctx context.Context) error
    Disable(ctx context.Context) error

    // Per-app toggle (Phase G T193)
    SetAppEnabled(id apps.AppID, enabled bool) error
}
```

### New view: `ViewApps`

ASCII mock:
```
┌─ Apps ────────────────────────────────────────────────────────────────┐
│                                                                       │
│  codex_cli                  [✓] Enabled    routed   12 453            │
│                              │             bypassed     0             │
│                              │                                        │
│  codex_desktop_app          [✓] Enabled    routed       43            │
│                              │             bypassed     0             │
│                              │                                        │
│  claude_code                [ ] Disabled   routed        0            │
│                              │             bypassed   847             │
│                                                                       │
│                                                                       │
│  space toggle   ↑↓ select   ? help                                   │
└───────────────────────────────────────────────────────────────────────┘
```

### Revised Status view

```
┌─ Status ──────────────────────────────────────────────────────────────┐
│                                                                       │
│  CA         ✓ installed, in Keychain                                  │
│             fingerprint a3:f2:9e:…:4c                                 │
│             not after 2027-05-16                                      │
│                                                                       │
│  Daemon     ✓ running (pid 12483, RSS 18 MB, uptime 2h14m)            │
│             HTTPS:     127.0.0.1:8990                                 │
│             SNI-peek:  127.0.0.1:8443                                 │
│                                                                       │
│  Network    ✗ hosts file: CLEAN (transparent mode disarmed)           │
│             run `e` to arm                                            │
│                                                                       │
│  Hooks      ✓ codex     installed (~/.codex/config.toml)              │
│             ✓ claude    installed (~/.claude.json)                    │
│                                                                       │
│  Savings    output tokens saved 421 932 (≈ $2.53)                     │
│             streamcut 1 243   repdet 387   beterse 91                 │
│             quality A/B  ctl 1.2%  treat 1.1%  healthy                │
│                                                                       │
│  i install   u uninstall   e enable/disable   r refresh   q quit      │
└───────────────────────────────────────────────────────────────────────┘
```

### No more setup-wizard

The check/confirm/apply step list is gone. The Install button runs
the reversibility.Plan in one go, showing per-Step progress in a
streaming overlay:

```
Installing Slimference…
  [✓] ca.generate
  [✓] ca.keychain
  [✓] launchd.install
  [⠧] hooks.codex
  [ ] hooks.claude
```

On error, the overlay shows the failing Step + a "Roll back" prompt.

## Implementation plan

1. **Slim `ProxyInterface`**:
   - Remove `InstallTransparent`, `EnableTransparent`, `DisableTransparent`,
     `UninstallTransparent`, `InstallService`, `UninstallService`.
   - Add `Install`, `Uninstall`, `Enable`, `Disable`, `SetAppEnabled`,
     `SetupState`.
   - Update `cmd/slimference/main.go` `serviceControlAdapter` to
     implement the new methods by delegating to `internal/install`.

2. **Add `ViewApps`**:
   - New file `internal/tui/view_apps.go` with the render function.
   - New keybind: `a` = switch to ViewApps from anywhere.
   - Three rows; arrow keys + space; emits a `tea.Cmd` that POSTs
     `/admin/apps` and refreshes state.

3. **Rewrite Status view** (rename Setup view → Status):
   - Pure SetupState renderer; no embedded wizard logic.
   - Footer actions: `i u e r q`.

4. **Install/Uninstall flow**:
   - When user hits `i`, switch to a transient overlay that streams
     Plan.Apply progress. Plan.Apply runs in a goroutine; emits
     per-Step events to the TUI via `tea.Cmd`.
   - On finish (success or error), return to Status view.

5. **Remove MiniMax references**:
   - `internal/tui/views.go:110, 126, 250, 251, 699, 829, 992-994` —
     delete the labels, rows, and the trustOverride lookup.
   - `internal/tui/model.go:163` — remove `GetMiniMaxTrustClass()` from
     `ProxyConfigInterface`.

6. **Tests**:
   - `view_apps_test.go`: render returns expected lines for given
     SetupState fixtures.
   - `model_install_flow_test.go`: hitting `i` invokes Install adapter;
     overlay shows per-Step events; error path returns to Status with
     red banner.
   - `view_status_test.go`: SetupState fixture renders the expected
     table.
   - Remove tests asserting MiniMax labels.

## Acceptance

- `internal/tui/model.go` `ProxyInterface` has 4 install-related
  methods (Install, Uninstall, Enable, Disable) — not 6.
- `slimference` TUI launches and `a` switches to ViewApps. Toggling a
  row POSTs `/admin/apps` and refreshes the row text.
- `i` from Status view runs Install, streaming Step events.
- No MiniMax references survive `grep -i minimax internal/tui/`.
- 100% test coverage on new view files.
- Pre-existing tests still pass after API slim-down.

## Sub-Tasks

- [ ] Slim ProxyInterface in `model.go`.
- [ ] Update `serviceControlAdapter` in `main.go` to delegate to
      `internal/install`.
- [ ] Add `ViewApps` (new file).
- [ ] Rewrite Status view (was Setup view).
- [ ] Install/Uninstall streaming overlay.
- [ ] Remove MiniMax references in views.go + model.go.
- [ ] Tests (view_apps, model_install_flow, view_status).
- [ ] Update keybindings doc.

## Dependencies

- **T201 must land first**: this task depends on `internal/install.
  Plan()` existing and being callable from the TUI adapter.
- **T203 (README)**: TUI's help text points at `slimference help install`
  which references `docs/install.md`.
- **T204 (defaults consolidation)**: TUI must not show the legacy
  URL-redirect / HTTPS_PROXY toggles. T204 nukes them globally; T197
  removes the UI affordances.

## Deviations

- The original T197 plan envisioned reusing the existing setup-wizard.
  Phase H reduces operative surface to 4 actions — no wizard needed.
  The wizard's check/confirm/apply flow is replaced by a single
  Plan.Apply progress overlay.

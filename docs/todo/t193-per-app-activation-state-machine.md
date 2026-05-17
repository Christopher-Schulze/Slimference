# TASK 193: Per-app activation state machine

Status: PLANNING 2026-05-16
Priority: P0 (the explicit "per-app toggle" piece the user named)
Scope: new `internal/control/apps.go`, `~/.config/slimference/apps.toml`,
       wiring in `internal/proxy/sniroute/` (T189), TUI screen in T191

## Why

User wants explicit independent toggles for:
- Codex CLI
- Codex Desktop App
- (planned) Claude Code

Each app independently on/off. Default policy:
- Codex CLI: ON after install (the primary user case).
- Codex Desktop App: ON after install (also a primary case).
- Claude Code: OFF after install; user explicitly enables.

Per-app toggling lets the user run Slimference in a controlled rollout:
test on one app, leave others untouched while validating.

## App identification

Each app has a stable identifier and a detection heuristic:

| AppID                | Detection (in order)                                             |
|----------------------|------------------------------------------------------------------|
| `codex_cli`          | UA prefix `codex_cli_rs/`; binary at `~/.npm-global/bin/codex` or `/usr/local/bin/codex` |
| `codex_desktop_app`  | UA prefix `codex_desktop_app/`, `Codex/`, or `Codex.app/`; binary at `/Applications/Codex.app` |
| `claude_code`        | UA prefix `claude-code/`; binary at `~/.local/bin/claude` |

UA detection happens after our TLS handshake when we see the first HTTP
request line + headers. The routing decision is per-connection (T189).

## Policy state

`~/.config/slimference/apps.toml`:

```toml
# Per-app integration toggles (T193). Managed by the TUI; manual edits
# survive but must respect this schema. Daemon hot-reloads on SIGHUP.

[apps.codex_cli]
enabled = true

[apps.codex_desktop_app]
enabled = true

[apps.claude_code]
enabled = false
```

Schema versioned via `schema_version = 1` at file top so future
migrations are sane.

## Wiring into the SNI router (T189)

The router's `mitm_codex_conversation` decision becomes:

```
if !apps.policy.IsEnabled(detectedAppID) {
    return RoutePassthrough
}
```

Detection cascade for the `detectedAppID`:

1. Process-owner lookup (best-effort) - on macOS via `lsof -ni :<sport>`
   then `ps -p <pid> -o comm=`. Match `Codex` vs `codex` vs `claude`.
2. UA prefix on the HTTP/WS upgrade request.
3. If both fail, fall back to "unknown" → routed passthrough (safest
   default). Telemetry counter `apps_unknown_seen` increments.

## TUI screen (A key)

```
╔══════════════════════════════════════════════════════════════════╗
║ Slimference — Per-app integration                               ║
╠══════════════════════════════════════════════════════════════════╣
║                                                                  ║
║ Each toggle controls whether Slimference intercepts that app's   ║
║ traffic. Other apps' traffic passes through unmodified.          ║
║                                                                  ║
║   Codex CLI                                                      ║
║     Detected   ✓  /Users/.../bin/codex (v0.130.0)                ║
║     Status     [X] enabled                  routed: 412          ║
║     Press 1 to toggle                                            ║
║                                                                  ║
║   Codex Desktop App                                              ║
║     Detected   ✓  /Applications/Codex.app (v2026.05)             ║
║     Status     [X] enabled                  routed: 87           ║
║     Press 2 to toggle                                            ║
║                                                                  ║
║   Claude Code                                                    ║
║     Detected   ✓  /Users/.../local/bin/claude                    ║
║     Status     [ ] disabled                 routed: 0            ║
║     Press 3 to toggle                                            ║
║                                                                  ║
║ Changes take effect within ≤ 2 s (next connection).             ║
║                                                                  ║
║ [←] Back   [R] Refresh detection                                 ║
╚══════════════════════════════════════════════════════════════════╝
```

## Sub-Tasks

- [ ] `apps.toml` schema + loader + validator.
- [ ] App detection heuristics (binary on disk + UA prefix).
- [ ] Policy cache shared between SNI router and TUI; hot-reload on
      SIGHUP and on file-change watcher.
- [ ] TUI per-app screen + toggle keys.
- [ ] Counter telemetry: per-app `routed` vs `bypassed_disabled`.
- [ ] Tests: schema parsing, toggle semantics, default policy on first
      install, detection on synthetic UA strings.

## Acceptance

- Default policy on fresh install: CLI on, Desktop App on, Claude Code
  off.
- Toggle CLI off; CLI requests immediately route to passthrough (next
  connection ≤ 2 s later confirms).
- File-edit of `apps.toml` is picked up within 2 s (file-watcher) or
  via explicit SIGHUP.
- Per-app counters break out correctly across mixed traffic (CLI + App
  + browser simultaneously).
- 100 % coverage on the policy module.

## Notes

- The Codex Desktop App may also send sideband traffic on the same TLS
  connection pool as conversation traffic (HTTP/2 multiplexing). The SNI
  router decides per-request, not per-connection, so the toggle still
  works correctly even when CLI and Desktop App share an outbound TCP
  flow (unlikely but legal).
- Process-owner lookup on macOS is unreliable for sandboxed apps. We
  document this as "best effort"; UA detection is the primary signal.

## Deviations

(none yet)

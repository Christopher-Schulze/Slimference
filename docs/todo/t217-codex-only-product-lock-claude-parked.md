# TASK 217: Codex-only product lock / Claude parked

Status: DONE (2026-05-17)
Priority: P0 before T209 live Codex certification
Scope: product entrypoints, app policy, SNI routing, docs/tests; no Claude code deletion

## Why

The product target is now strict Codex-only: Codex CLI first, Codex Desktop App next. Claude Code must remain in the repository for history/reference and possible future work, but Slimference must not install, remove, toggle, or route Claude Code in the active binary. The user uses RTK for Claude Code, so Slimference must not touch `~/.claude` or `api.anthropic.com`.

## Acceptance

- `slimference install` installs Codex hooks/notice only.
- `slimference install --with-claude` and `uninstall --with-claude` remain accepted for old scripts but are no-ops for Claude.
- `slimference hook install claude` and `hook remove claude` exit with a parked message and do not touch `~/.claude`.
- `slimference integrate --client=claude` exits through a parked/no-write path; `--client=all` normalizes to Codex only.
- `slimference readhook claude` is rejected; `readhook codex` remains active.
- Top-level `slimference claudeposttool` is not exposed.
- `apps.Manager` forces `claude_code=false`, rejects enabling it, normalizes stale TOML reloads, and `/admin/apps` rejects `claude_code=true`.
- `sniroute` always passes `api.anthropic.com` through; inventory reflects that parked state.
- Default hosts remain `chatgpt.com` and `api.openai.com` only.
- Detached daemon `SIGHUP` reloads app policy + SNI-peek mode without
  exiting; `disable` keeps the daemon healthy and `:8443` off.
- Docs say Claude is parked, not opt-in-active.
- No live `cert-trust`, `root-arm`, `enable`, or real Codex traffic is run.

## Sub-Tasks

- [x] Park install/uninstall `--with-claude` as compatibility no-op.
- [x] Park public hook/integrate/readhook/claudeposttool entrypoints.
- [x] Force app policy and admin app endpoint to keep Claude disabled.
- [x] Route Anthropic SNI as passthrough and update routing inventory.
- [x] Fix detached daemon SIGHUP handling so `enable`/`disable` do not
  terminate the background daemon.
- [x] Replace hosts-cleanup non-nil heuristic with explicit armed state;
  `enable` after a disarmed start now applies hosts and starts the SNI
  engine, while `disable` cancels it.
- [x] Update docs/install.md, docs/documentation.md, and todo references.
- [x] Add/adjust tests for no-write parked behavior and Codex-only success paths.

## Verification

- `go test ./cmd/slimference ./internal/hooks ./internal/control/apps ./internal/proxy/sniroute ./internal/install ./docs -count=1 -timeout 180s`
- `go test ./internal/daemon ./cmd/slimference -race -run 'TestRunDaemon|TestReloadSNIPeek|TestEndToEndCLIDaemonSIGHUP|TestHandleDaemon|TestServiceControlAdapter' -count=1 -timeout 180s`
- `go run ./scripts/ci` passes all 8 gates, including total coverage 100.0%.
- Live-safe checks only: daemon running on `:8990`, `:8443` off,
  hosts inactive, DoH preflight OK for `chatgpt.com` +
  `api.openai.com`, and `disable` SIGHUP keeps the daemon alive.
- Temp-HOME no-write checks: `install --dry-run --with-claude` lists
  Codex steps only; `hook install claude` exits parked; `integrate
  install --client=claude` exits parked; no `.claude` directory is
  created.

## Notes

This task does not delete Claude Code code, tests, docs, or internal helpers. It changes only the public product routing/install surface so Slimference cannot accidentally affect Claude Code while the Codex CLI certification is prepared.

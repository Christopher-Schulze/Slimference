# TASK 210: Legacy surface retirement audit

Status: DONE (2026-05-17)
Priority: complete; deletion decisions remain deferred until after T209
Scope: classification + default-path guardrails only; no legacy code deleted

## Why

Phase H's target architecture is deliberately small: Codex hooks for
signal input plus transparent TLS-MITM for traffic input. The tree
still contains older URL-redirect, per-process proxy, system HTTPS
proxy, and config-patch paths. Some are useful as advanced/manual
fallbacks; some are only historical baggage. We should not delete any
of them until live Codex certification is done, but they must be
inventoried so future agents do not accidentally re-promote them into
the default flow.

## Acceptance

- Inventory every non-Phase-H traffic/input surface still present in
  code, help, tests, and docs.
- Classify each as `keep-advanced`, `hide-from-default`, `deprecate`,
  or `remove-after-certification`.
- Confirm no `slimference install`, TUI setup action, or default test
  path uses URL-redirect, env proxy, macOS system network proxy, or
  Claude Code by default.
- Do not delete or disable code in this task without a separate user
  approval.

## Current Inventory

| Surface | Files | Classification | Cleanup decision |
|---------|-------|----------------|------------------|
| Phase H Codex install | `cmd/slimference/help.go`, `cmd/slimference/install_cmd.go`, `docs/install.md`, `docs/documentation.md` | current default | Keep as the only promoted path: `install`, `cert-trust`, `root-arm`, `enable`, `disable`, `root-disarm`, `uninstall`, `status`. Per-app policy lives under the XDG config dir as `apps.toml`. |
| Config-patch integration | `cmd/slimference/integrate_cmd.go`, `internal/integrate/*` | keep-advanced + hide-from-default | Kept for manual fallback only. Help and post-install output now label it `LEGACY/ADVANCED`; no default command points to it. |
| Legacy proxy lifecycle + per-process env helpers | `cmd/slimference/proxy_cmd.go`, `cmd/slimference/help.go` | keep-advanced + hide-from-default | Kept for diagnostics/regression work. Subcommand help now states it is not the Phase H default path. |
| Legacy CONNECT flag | `internal/config.Transparent.Enabled` | keep-advanced | Keep for backwards config compatibility. `SNIPeekMode` is the Phase H traffic flag. |
| Historical System-HTTPS-Proxy doc | `docs/transparent-mode.md` | deprecate | Top-of-file warning already points to `docs/install.md`; do not use as normative setup doc. |
| Historical Codex URL-redirect note | `docs/codex-routing-status.md` | deprecate | Added top warning: investigation note only; current setup lives in `docs/install.md`. |
| Config loader debug helper | `scripts/utils/cfgdebug/` | remove-after-certification | Kept for now, explicitly marked debug-only. Remove only after T209 proves config path stability and user approves deletion. |
| Historical task notes | `docs/todo/t05*`, `t65*`, `t66*`, `t71*`, `t72*`, `t122*`, `t137*`, `t140*`, `t163*`, `t200*`, `t201*`, `t204*` | keep-history | Keep as immutable history. Current docs must point to `docs/install.md`. |

## Sub-Tasks

- [x] Run a code/help/doc grep for legacy surface terms.
- [x] Produce a keep/deprecate/remove table with exact file paths.
- [x] Add tests that top-level help cannot regress to legacy install
  surfaces.
- [x] Ask before deleting any legacy code: nothing was deleted in T210.

## Verification

- `go test ./cmd/slimference -run 'TestHelp|TestIntegrate|TestRunIntegrate' -count=1 -timeout 120s`
- `go test ./docs -count=1`
- `go test ./scripts/utils/cfgdebug -count=1`
- Full live Codex certification remains T209 and is intentionally not part
  of this task.

## Notes

This task is intentionally separate from T208. T208 ships product
value; T210 prevents old scaffolding from leaking back into the
operator path.

No `cert-trust`, `root-arm`, `enable`, hosts edit, Keychain edit, or
GitHub operation was run for this cleanup.

# TASK 240: Codex zero-drawdown release certification

Status: PLANNED
Priority: P0 after T238, T239, T241, T242, and T243 branch decisions
Scope: Final macOS arm64 product proof for Codex CLI and Codex Desktop UX

## Why

The final product claim is not "we have routing code". The final claim is:
Slimference can be installed, used, repaired, disabled, and uninstalled while
Codex stays at least as capable as native Codex, with measured savings where
the route is proven and no collateral impact on Browser ChatGPT, ChatGPT.app,
or Claude Code.

T238/T242 prove or reject the Desktop routing branch. T241 makes Codex CLI WSS
Phase-F certification automatically repairable after updates. T243 makes
`transport=auto` WSS-first with byte-equal WSS bridge before HTTP fallback.
T239 builds the human launch center and unified Codex install UX. T245 keeps CA
truth honest: not required for scoped CLI WSS, process-local custom CA env first
for Desktop proof, Keychain trust only for Desktop/Lab fallback branches that
prove they can use it. T240 certifies the whole user-facing system as one
release ceremony.

## Acceptance

- Fresh build is installed and daemon is restarted from the installed binary.
- `slimference` launch center opens and exposes exactly:
  Launch Codex CLI, Launch Codex App, Savings, Status, Manage Slimference.
- Install/Repair is unified for Codex. The default product flow prepares both
  CLI and Desktop support and does not show CLI/App install checkboxes. Desktop
  is capability-gated after install, not treated as a separate half-installed
  product.
- Launch Codex CLI runs through `transport=auto`, uses `wss_phasef` for the
  certified tuple, returns correct model output, records clean counters, and
  measures real WSS Phase-F savings.
- If Codex CLI updated, the guided recert path from T241 has either issued a
  new WSS cert automatically or Status clearly says WSS Phase-F savings are
  being repaired while T243 keeps the session on WSS byte-equal bridge when the
  bridge is safe.
- Launch Codex App follows the T238 branch:
  - if Desktop proxy mode is proven with process-local `CODEX_CA_CERTIFICATE`
    or an equivalent supported hook, it launches through Slimference and records
    clean WSS counters;
  - if Desktop proxy mode is rejected by T242, it reports Desktop direct-only or
    root-store blocked and makes no savings claim.
- Direct `codex` and Finder/Spotlight Codex.app launches remain available and
  native.
- Browser ChatGPT and ChatGPT.app remain direct during Slimference-launched
  sessions.
- Claude Code remains parked and untouched.
- Savings output is source-labelled and contains no fake Desktop numbers.
- Status output identifies daemon, binary SHA, CA trust, route mode, WSS cert,
  drift fallback, config path, global lab state, and last observation.
- Status proves missing CA env or Keychain trust is not a CLI WSS failure. CA
  state is reported as Desktop/Lab readiness only unless the active test is a
  Desktop/Lab TLS-MITM probe.
- Repair fixes missing/partial local Slimference state without mutating unrelated
  Codex, Browser, ChatGPT.app, or Claude state.
- Disable returns scoped Codex route to direct mode.
- Uninstall reverses Slimference-managed files and leaves Codex usable.
- Version drift test proves Codex/Slimference tuple mismatch follows the final
  ladder: `wss_phasef -> wss_bridge -> http -> direct`.
- CA test proves scoped CLI WSS works with CA absent, present, or removed. If
  Desktop proxy mode uses process-local custom CA env, prove no Keychain prompt
  is needed. If Desktop proxy mode needs Keychain trust, prove the T245 guided
  flow and removal path. If Desktop remains direct-only, prove the TUI does not
  ask for CA during normal CLI use.
- Full CI remains green with coverage >= 95.0% and behavior-critical tests
  covering the changed paths.
- Operation log contains the exact evidence and final branch decision.

## Sub-Tasks

- [ ] Build and install fresh stripped binary; verify `./slimference` and
  `~/.local/bin/slimference` SHA match.
- [ ] Restart daemon and verify PID, health, listener state, and disarmed global
  lab state.
- [ ] Run launch-center smoke from a terminal.
- [ ] Run unified Install/Repair state proof: one product install prepares CLI
  and Desktop support together, with no default CLI/App checkbox split.
- [ ] Run Launch Codex CLI exact-reply smoke.
- [ ] Run Launch Codex CLI mutation-triggering smoke and verify WSS mutation
  counters.
- [ ] Simulate Codex/Slimference cert drift and verify `transport=auto` selects
  WSS byte-equal bridge while T241 auto-recert runs.
- [ ] Verify successful T241 recert restores `wss_phasef` on the next
  Slimference-launched Codex CLI session.
- [ ] Force WSS bridge failure in a controlled test and verify HTTP fallback is
  used only after bridge is unsafe.
- [ ] Run Launch Codex App according to T238 final branch decision.
- [ ] If T242 is still open, run the Desktop custom-CA-env probe first:
  `launch-desktop --transport=proxy --with-ca-env`, verify
  `CODEX_CA_CERTIFICATE`, lsof, WSS counters, and direct controls.
- [ ] Prove direct fallback: normal `codex` and Finder/Spotlight Codex.app still
  work without Slimference env/config/global routing.
- [ ] Prove Browser ChatGPT direct while Slimference session is active.
- [ ] Prove ChatGPT.app direct while Slimference session is active, if installed
  and running.
- [ ] Verify Claude Code state is unchanged and disabled in Slimference product
  policy.
- [ ] Verify Savings output by source and period.
- [ ] Verify Status output and repair recommendations.
- [ ] Verify CA wording and behavior: CLI WSS independent of CA; Desktop/Lab CA
  branch explicit, reversible, and not advertised as savings proof.
- [ ] Run Repair against a controlled partial state and verify it fixes only
  Slimference-owned state.
- [ ] Run Disable and verify `~/.codex/config.toml` returns to baseline.
- [ ] Run Uninstall dry-run and real uninstall in a controlled window; verify
  byte-equal or explicitly documented reversible deltas.
- [ ] Reinstall after uninstall and verify the launch center works again.
- [ ] Run Codex version-drift and Slimference version-drift fallbacks.
- [ ] Run `go test ./... -count=1 -timeout 300s`, `go vet ./...`,
  `go run ./scripts/ci`, and `git diff --check`.
- [ ] Append operation-log release certification section with exact commands,
  hashes, counters, versions, and decision.

## Evidence Table

| Area | Evidence |
|---|---|
| Build | binary SHA, commit SHA, version |
| Daemon | PID, health endpoint, listener state |
| CLI | command, response sentinel, route mode, WSS counters |
| Desktop | launch mode, app version, app-server PID, lsof, WSS counters or direct-only reason |
| Browser | lsof/direct proof |
| ChatGPT.app | lsof/direct proof or not-running note |
| Savings | `savings`/`gain` output with source attribution |
| Status | JSON and human output snapshots |
| CA / Trust | absent/present/remove proof, custom-CA-env proof, CLI independence, Desktop/Lab branch need |
| Repair | before/after state diff |
| Disable | config hash before/after |
| Uninstall | managed-file diff and Codex direct smoke |
| Drift | fallback reason and transport |
| WSS ladder | `wss_phasef`, `wss_bridge`, `http`, `direct` branch evidence |
| CI | command list and pass/fail |

## Notes

This is a release gate, not a feature task. It should not introduce new
routing mechanisms. If T240 finds a product defect, open a new task with the
failure evidence instead of weakening acceptance.

The task can pass with Desktop direct-only if T238 proves no safe process-local
Desktop route exists. In that branch, the release claim is still honest:
Codex CLI gets Slimference savings, Codex Desktop remains native with no
drawbacks, and the launch center says so plainly.

The task can also pass with CA trust absent for the normal product path. CA is
not the thing that makes CLI WSS work. The release-critical thing is that CLI
WSS Phase-F and WSS bridge are scoped, certified, repairable, and reversible.
CA only matters for Desktop/Lab TLS termination if T242 proves a usable client
trust path. The preferred Desktop path uses process-local `CODEX_CA_CERTIFICATE`
before any Keychain trust prompt.

T238 pre-live code has narrowed the Desktop release branch to one explicit
live ceremony: process-local proxy launch plus lsof and `/admin/state.wss`.
T240 must not accept a cosmetic provider badge, sideband-only routing, or
base-URL env injection as Desktop savings evidence.

## Deviations

None yet.

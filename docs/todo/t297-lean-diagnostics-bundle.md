# TASK T297: Lean diagnostics bundle for field sessions

## Why

Users need a low-friction way to run Slimference for a few real sessions and
later hand an agent a useful evidence bundle. The bundle must stay compact and
content-free: no raw prompts, raw tool outputs, raw WSS frames, auth material,
or giant capture archives. It should collect the existing safe signals in one
place so analysis can distinguish route readiness, real savings, host budget,
proof-event loss, and recent decision/flight behavior.

## Acceptance

- `slimference debug bundle` writes a timestamped diagnostics directory under
  `~/.slimference/exports/` by default.
- Bundle contents are bounded and content-free: manifest, admin state/status
  snapshot, savings summary, debug paths, capped decision-flight export, capped
  filter tail, and capped daemon log tails.
- The command supports explicit output directory and row/log caps.
- Docs explain the command and how to use it after a few real sessions.
- Focused tests and full CI pass.
- Current binary is installed and daemon status is verified.

## Sub-Tasks

- [x] Implement `slimference debug bundle`.
- [x] Add tests for path parsing, bounded export, and missing-source behavior.
- [x] Update docs and help text.
- [x] Run focused tests and full CI.
- [x] Install binary, restart/check daemon, and commit.

## Notes

- No persistent Codex route changes. Normal Codex remains direct unless launched
  via Slimference mode.
- Focused bundle tests: `go test ./cmd/slimference -run 'Test(ParseDebugBundleArgs|DefaultDebugBundleAdminURLNormalizesWildcardHosts|HandleDebugBundleWritesBoundedContentFreeBundle|WriteDebugBundleMissingSourcesStillWritesBundle)' -count=1`.
- Package/docs tests: `go test ./cmd/slimference`; `go test ./docs`.
- Real bundle smoke: `go run ./cmd/slimference debug bundle --out /tmp/slimference-debug-bundle-t297 --flight-limit 20 --filter-limit 20 --log-lines 20` produced a 132K bundle with admin state, paths, savings, decision/flight tails, filter tail, daemon log tails, and no missing optional source.
- Full gate: `go run ./scripts/ci` PASS, 8/8 steps, aggregate coverage 96.3%.
- Installed via `go run ./scripts/build --restart`; daemon restarted on port 8990 as PID 16223.
- Installed checks: `slimference status --preflight` green; normal Codex direct; advanced shared route off; `slimference codex status` daemon reachable; installed `slimference debug bundle --out /tmp/slimference-debug-bundle-installed-t297 --flight-limit 20 --filter-limit 20 --log-lines 20` produced a 132K bundle.

## Deviations

- None.

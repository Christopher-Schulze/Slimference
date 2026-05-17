# TASK 215: DoH fallback + live-arm preflight

Status: DONE (2026-05-17)
Priority: P0 before T209 live certification
Scope: `internal/proxy/upstream_resolve.go`, `cmd/slimference status|doctor`, install/status tests, docs/install.md

## Why

When transparent MITM is armed, `/etc/hosts` maps Codex hosts to loopback. The daemon must bypass the system resolver when dialing the real upstream or it will self-loop. The current DoH resolver dials Cloudflare `1.1.1.1` directly. That is a good default, but a single hardcoded endpoint creates avoidable live-test risk: if `1.1.1.1` is blocked, slow, or temporarily unavailable, the first live Codex request can stall.

Before T209, the system should prove all external prerequisites before the user starts Codex traffic through it.

## Target State

- DoH has fallback endpoints, at minimum `1.1.1.1` then `1.0.0.1`.
- Resolver errors record which endpoint failed.
- `slimference status` or `doctor` can preflight:
  - CA file exists.
  - CA trust is present or clearly missing.
  - SNI listener is bound on configured port.
  - hosts block is active or clean as expected.
  - pfctl route appears active enough for the local mode, or status says how to recover.
  - DoH upstream resolution succeeds for `chatgpt.com` and `api.openai.com`.
  - apps policy enables Codex CLI and/or Codex Desktop and keeps Claude off by default.
- If hosts are active but SNI listener is down, status prints recovery: `slimference root-disarm`.
- If DoH is down, status warns before any Codex live traffic starts.

## Maximum-Possible Check

The preflight should answer: "Is it safe to run T209 now?"

It must not perform live Codex calls. It may perform local state checks and DoH DNS lookups only.

Required checks:

- Config path: canonical XDG / `SLIMFERENCE_CONFIG` resolver.
- Daemon: admin port reachable.
- Transparent engine: actual SNI listener, not admin port.
- Hosts: marker-fenced block only contains Codex hosts unless user explicitly opted into Claude in the future.
- IPv6: default remains IPv4-only unless design changes.
- DoH: fallback tested without system resolver.
- Upstream self-loop prevention: resolving `chatgpt.com` while hosts are active must produce a non-loopback upstream dial target.

## Acceptance

- Resolver falls back from primary DoH endpoint to secondary.
- Tests cover primary failure, secondary success, all failures, cache behavior, and IP literal passthrough.
- Status/doctor surfaces DoH preflight result and recovery hints.
- `docs/install.md` T209 runbook includes preflight output expectations.
- No live Codex traffic is run.

## Sub-Tasks

- [x] Add DoH endpoint list and fallback loop: `1.1.1.1:443`, then `1.0.0.1:443`.
- [x] Add endpoint-attribution to resolver errors.
- [x] Add `slimference status --preflight` and automatic preflight when routing is active.
- [x] Preserve existing hosts-active + SNI-listener-down warning path.
- [x] Add tests for fallback, loopback rejection, and status preflight output.
- [x] Update `docs/install.md` T209 checklist.

## Verification

- `go test ./internal/proxy -run 'Test.*Resolve|Test.*DoH|TestDefaultUpstreamDial' -count=1 -timeout 60s`
- `go test ./cmd/slimference -run 'Test.*Status|Test.*Doctor|Test.*Install' -count=1 -timeout 120s`
- `go test ./docs -count=1`
- `go run ./scripts/ci`

## Notes

This is the safest first implementation task before T209 because it lowers live-test risk without arming Codex or touching Keychain/hosts on the active machine.

Implemented files: `internal/proxy/upstream_resolve.go`, `internal/control/probes.go`, `internal/control/state.go`, `cmd/slimference/install_cmd.go`, docs and tests.

Final pre-live polish: `slimference start|stop|restart --help` now prints help instead of executing lifecycle actions, and unexpected lifecycle arguments fail with exit 2. This avoids accidental daemon starts while preparing the T209 runbook.

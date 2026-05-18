# TASK 226: WSS-first auto promotion

Status: PARTIAL - pre-live auto gate, status surfaces, and certify issuer implemented; live promotion pending
Priority: P0 after T224 CLI live proof
Scope: Codex CLI first; Codex Desktop only after T225/T228 proof

## Why

The final Codex product should use WebSockets whenever they are safe. WSS is
the native high-value transport for modern Codex conversation traffic: it keeps
streaming semantics, exposes frame boundaries to the T208 Phase-F adapter, and
avoids forcing Codex into a lower-fidelity HTTP fallback.

The current implementation is deliberately conservative: explicit
`--transport=wss` exists, and `--transport=auto` now reads local
certification state from `~/.slimference/codex-wss-cert.json`. Without a
green, version-matching proof it fails closed to HTTP. This task flips the
default to WSS only after the evidence gates pass.

## Target State

`slimference codex run --transport=auto` and the eventual top-level
`slimference enable` product route choose WSS first when all of these are true:

- daemon reachable on the scoped listener;
- raw scoped WSS frontdoor registered;
- upstream profile/fingerprint status is usable;
- last live certification for the current Codex CLI version is green or absent
  but the operator explicitly requested WSS;
- previous local session did not record parse failures or degraded WSS sessions;
- route is Codex-only and global lab mode is off.

Fallback order:

1. WSS scoped provider route.
2. HTTP scoped provider route.
3. Direct Codex only for one-shot `codex run` when the daemon is unreachable
   before process launch.

Persistent shared route fallback:

- `slimference enable` / `codex enable` cannot silently switch already-running
  Desktop/App-server processes mid-session.
- If WSS becomes unhealthy, status and TUI must show the fix: switch to HTTP or
  disable.
- Any auto-downgrade for persistent route must be marker-owned, reversible, and
  explicitly logged.

## Acceptance

- `--transport=auto` prefers WSS after T224 promotion criteria pass.
- `--transport=auto` still uses HTTP when T224 has not passed, when WSS
  preflight fails, or when recent WSS health counters show degradation.
- The promotion state is visible in `slimference codex status`, top-level
  `slimference status`, TUI Setup, and `/admin/state`.
- Promotion is version-aware: a Codex CLI version change invalidates the WSS
  default until a fast smoke or capture re-validates it.
- WSS failure never affects Browser ChatGPT, ChatGPT.app, Claude Code, or
  global network state.
- Tests cover auto-selection, downgrade conditions, version invalidation,
  telemetry wording, and direct fallback for one-shot CLI.

## Sub-Tasks

- [x] Add a small local certification state file under `~/.slimference/` keyed
  by Codex CLI version, Slimference build, transport, and route profile.
- [ ] Record WSS proof outcome after T224: native baseline hash, scoped WSS
  capture hash, `frames_reencoded`, `degraded_sessions`, `parse_failures`,
  timestamp, and operator decision.
- [x] Add `slimference codex certify wss` to issue the local proof only from
  a green live daemon observation, with `--dry-run`, `--operator`, `--notes`,
  host/port flags, Codex CLI version parsing, and no manual cert-file writes.
- [x] Teach `codex run --transport=auto` to consult certification state before
  choosing WSS. Recent daemon WSS health remains a live-certification input.
- [x] Teach `codex enable --transport=auto` to write WSS only when the shared
  route is certified for the current Codex version; otherwise write HTTP and
  explain why.
- [x] Add `slimference codex status` fields:
  `auto_transport`, `wss_certified`, `certified_codex_version`,
  `last_wss_error`, and `fallback_reason` via the JSON `auto` object and
  human `Auto` line.
- [x] Add TUI/status/admin visibility for the auto decision:
  `Codex Mode`, `transport`, `auto_transport`, `wss_certified`,
  `fallback_reason`, and daemon reachability.
- [x] Add tests for no-cert, green-cert, stale-version, Slimference-version
  drift, schema/transport/profile mismatch, parse-failure, degraded-session,
  daemon-down, explicit `--transport=wss` override, and every certify
  criterion failure.
- [ ] After implementation, run T224/T209 live proof before marking Done.

## Pre-Live Implementation Notes

- `internal/codexroute/certification.go` owns the certification schema.
- `cmd/slimference/codex_cmd.go` resolves default `auto` mode through the
  certification decision. No-cert, unreadable cert, stale Codex version,
  stale Slimference version, parse failures, or degraded sessions all select
  HTTP and explain the fallback.
- Explicit `--transport=wss` remains an operator override for T224/T209.
- `/admin/state.codex_route`, `slimference status`, `slimference codex
  status`, and TUI Setup now show the route mode and WSS certification state.
- `slimference codex certify wss` is the only supported writer for
  `~/.slimference/codex-wss-cert.json`. It refuses proof issuance unless the
  current `/admin/state` snapshot reports daemon reachability,
  `frames_reencoded>0`, `compressed_messages_mutated>0`, mutation active,
  byte-bridge-only false, and zero parse/degrade/compression errors.

## Benefits

Compared with current HTTP auto:

- Restores the intended native Codex WSS behavior without global routing.
- Gives T208 WSS Phase-F adapter real production value.
- Expected incremental savings: 0-15 percentage points over scoped HTTP on
  WSS-heavy Codex sessions, depending on how much request/response content is
  frame-visible.

Compared with direct native Codex baseline:

- Expected total savings remains corpus-dependent: near zero for tiny prompts,
  20-55% for tool/history-heavy sessions, higher only where output-reduce and
  cache layers trigger cleanly.

## Drawdowns and Guards

- WSS schema drift can remove savings. Guard: byte-equal degraded bridge plus
  health counters.
- Daemon death mid-WSS turn cannot be made invisible. Guard: one-shot fallback
  before launch, TUI/status disable path for persistent route.
- Do not claim invisibility. Guard: T224 evidence required; docs say
  `minimized drift` until captures prove otherwise.

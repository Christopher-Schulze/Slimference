# TASK 241: Codex automatic WSS recertification engine

Status: PARTIAL - status/JSON drift visibility landed; auto-recert engine
planned
Priority: P0 before T243 full WSS-first auto ladder and before T240 release seal
Scope: Codex CLI WSS Phase-F savings resilience across Codex CLI and
Slimference updates, with one shared recert core for CLI, TUI, and background
repair

## Why

The WSS cert deliberately binds to the Codex CLI version and Slimference
version. That is the correct zero-drawdown safety guard: after `codex-cli`
updated from 0.130.0 to 0.131.0, `transport=auto` fell back to HTTP instead of
silently running an uncertified WSS parser.

The missing product layer is not "ignore drift". The missing layer is an
automatic recertification engine that makes WSS Phase-F the practical steady
state again immediately after compatible updates. The user goal is MAXXED WSS
savings without manual babysitting, fake certs, unsafe mutation, or UX drift.

The version tuple remains a useful alarm, but not the final truth. The final
truth is a live capability proof: the current Codex/Slimference tuple must
negotiate scoped WSS, preserve `permessage-deflate`, trigger a real Phase-F
mutation, re-encode frames, and record zero parser/degrade/compression errors.
If that proof is green, WSS Phase-F is the standard. If it is not green, T243
keeps Codex on WSS byte-equal bridge before any HTTP fallback.

## Acceptance

- `slimference codex status` and the TUI clearly show version drift, the old
  tuple, the current tuple, `needs_recert`, last recert attempt, last failure,
  cooldown/backoff state, and the exact repair action.
- A single recert core powers all entry points: `slimference codex recertify
  wss`, TUI "Repair CLI WSS", and background auto-recert. No parallel recert
  logic is allowed.
- `slimference codex recertify wss` is a real product command with:
  `--dry-run`, `--force`, `--no-write`, `--operator`, `--notes`,
  `--host`, `--port`, `--timeout`, and `--json`.
- The recert core uses a deterministic live mutation trigger, not a synthetic
  cert bypass. It may create a temporary repo outside the Slimference checkout,
  run Codex through `slimference codex run --transport=wss -- exec ...`, then
  inspect `/admin/state`.
- Recert passes only when all Phase-F criteria are green in the current
  observation window:
  - daemon reachable;
  - Codex CLI version resolved;
  - Slimference version resolved;
  - WSS engine active;
  - `parse_failures=0`;
  - `degraded_sessions=0`;
  - `compression_errors=0`;
  - `frames_reencoded>0`;
  - `compressed_messages_mutated>0`;
  - `mutation_active=true`;
  - `byte_bridge_only=false`.
- Recert then calls the same certification writer as `slimference codex certify
  wss`; cert criteria are not weakened or duplicated.
- Auto-recert starts opportunistically when Slimference itself is launched, when
  the TUI Status/Launch Center sees `needs_recert=true`, and when `Launch Codex
  CLI` is selected. It must never trigger from normal direct `codex` launches
  outside Slimference.
- Auto-recert never blocks the user's immediate Codex work. While it runs, T243
  should keep `transport=auto` on WSS bridge if possible, then HTTP only if WSS
  itself is unhealthy.
- A per-tuple lock prevents duplicate recert runs. If another recert is running,
  callers attach to status or report "already running"; they do not launch a
  second Codex API call.
- Backoff prevents expensive loops: after a failed attempt, auto-recert waits a
  bounded cooldown before retrying unless the user explicitly presses Repair /
  passes `--force`.
- Recert state is persisted in a small local state file under
  `~/.slimference/` with current tuple, certified tuple, last attempt, last
  success, last failure reason, retry-after, and last counters.
- Logs are bounded and useful:
  - path: `~/.slimference/logs/recert.log`;
  - max active file size: 2 MiB;
  - one rotated backup is enough;
  - no prompt bodies, secrets, auth tokens, or large tool outputs;
  - include tuple, command id, start/end time, counters before/after, decision,
    cert path, and compact error class.
- The recert flow leaves `~/.codex/config.toml` bit-identical after every
  enable/disable cycle and does not touch Browser ChatGPT, ChatGPT.app, Claude
  Code, `/etc/hosts`, pfctl, macOS system proxy, or persistent shell env.
- Failure does not degrade Codex. It reports exact failure class and leaves T243
  to prefer WSS byte-equal bridge before HTTP fallback.
- Operation log records version before/after, cert path, counters, binary SHA,
  config hash, recert attempt ids, fallback ladder decision, and final auto
  decision.

## Sub-Tasks

- [x] Add a `needs_recert` field to Codex status JSON with current/expected
  Codex and Slimference versions.
- [ ] Add `slimference codex recertify wss` with dry-run, force, no-write,
  operator, notes, host, port, timeout, and JSON output.
- [ ] Extract a shared recert core that can be called from CLI, TUI, and
  background auto-repair without duplicating criteria or state transitions.
- [ ] Implement deterministic real Codex mutation trigger using a temporary
  repo and a long repeated `git status --short` / tool-output pattern proven to
  trip Phase-F on current Codex.
- [ ] Snapshot WSS counters before and after the trigger and evaluate only the
  delta window for pass/fail.
- [ ] Persist recert state under `~/.slimference/` with tuple, attempt id, last
  success, last failure, cooldown, counters, and cert path.
- [ ] Add a bounded recert logger with 2 MiB cap, one backup rotation, no
  secrets, no prompt dumps, and machine-readable event classes.
- [ ] Add per-tuple lock and cooldown/backoff so auto-recert cannot spam Codex
  API calls or overlap with itself.
- [ ] Add background auto-recert triggers from Slimference TUI startup, Launch
  Center refresh, and Launch Codex CLI selection; keep normal direct Codex
  launches completely untouched.
- [ ] Add TUI Status/Manage wording and a "Repair CLI WSS" action that calls
  the same recert core with explicit user intent.
- [ ] Keep certify criteria unchanged; do not lower the gate for convenience.
- [ ] Add tests for Codex drift, Slimference drift, missing daemon, mutation
  not observed, duplicate lock, cooldown, log rotation, no-write, dry-run,
  certify refusal, and successful recert.
- [ ] Add tests proving auto-recert failure leaves the next auto decision to
  T243's WSS bridge path rather than immediately forcing HTTP.
- [ ] Run one live recert against the current `codex-cli 0.131.0` and append
  evidence.

## Notes

This task makes updates boring. It must not weaken the version tuple guard.
The desired behavior is: updates never make Codex worse and almost never leave
WSS Phase-F savings off for long. If Codex changes compatibly, auto-recert
should prove it and restore WSS Phase-F. If Codex changes incompatibly, T243
keeps native WSS byte-equal bridge before falling back to HTTP.

The recert engine should spend engineering effort on the boring parts that make
it practically always work:

- deterministic trigger construction;
- per-attempt observation windows instead of lifetime counter guesses;
- lock/backoff so repair never storms;
- exact failure taxonomy so broken cases are fixable;
- bounded logs so debugging exists without disk growth;
- shared core so CLI/TUI/background cannot drift.

2026-05-19 status hardening landed:

- `codexroute.AutoDecision` now carries the current Codex/Slimference tuple,
  the certified tuple, `needs_recert`, and `recert_command`.
- `slimference codex status --json` exposes those fields through its existing
  `auto` object.
- Human `slimference codex status` prints the current tuple, certified tuple,
  fallback reason, and recert action when version drift pauses WSS savings.
  This keeps the strict tuple guard intact while making the repair path visible.

Open design constraints for implementation:

- Do not certify from unit tests, synthetic counters, or hand-written cert
  files.
- Do not auto-run recert outside a Slimference-launched context.
- Do not disable the guard just because the version changed.
- Do not claim Desktop savings from this task; Desktop is T242/T240.
- Do not optimize Audio/Realtime/Voice; passthrough only.
- Do not add a second TUI or a second launcher surface.

## Deviations

None yet.

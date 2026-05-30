# TASK 249: Codex comprehension safety net (A/B harness, recoverable archive, re-read auto-restore)

Status: [~] PARTIAL - core comparison engine, WSS reducer replay, live capture/report CLI, recovery note, and auto-restore landed; socket-lifecycle measurement remains open
Priority: P1 - must land before any aggressive/lossy savings layer is default-on
Scope: Codex-only WSS Phase-F. Build the measurement + recovery substrate that lets
aggressive compression be enabled with data instead of hope. No new savings by
itself; it is the gate that makes t253/t254/t255 safe to turn on.

## Why

"No drawback" on the WSS product path is currently asserted via token counters and
parse/degrade/upstream-error rates, never via comprehension. `qualityab.RecordOutcome`
on WSS (`internal/proxy/wsmitm_phasef.go:222-226`) records only Success vs
UpstreamError - a failure-rate proxy, blind to silent context/intelligence
degradation: a model that produces a confidently wrong answer because it lost a file
still counts as Success.

Mutations are permanent in server-side state: the Codex Responses API keeps history
server-side via `previous_response_id`, so anything we forward becomes the model's
permanent memory of the session, and over-compression compounds turn after turn. The
recovery path is broken: the `local-archive://<id>` marker is not resolvable by the
model unless the model itself re-emits the URI, and nothing tells it that it may
(`internal/proxy/reinject.go:25-81` only expands a model-emitted URI). Filtered tool
outputs have NO server-side full fallback at all (the first transmission is already
compacted), so a lossy filter loss is permanent and unrecoverable.

Before compressing more aggressively (first-read AST t253, server-state mirror t254,
chunk dedup t255), we need (a) a way to PROVE the model still behaves identically, and
(b) a recovery path that turns permanent loss into recoverable loss.

## Acceptance

- Offline comprehension A/B harness exists and runs in CI as a NON-gating report:
  replays a captured or synthetic multi-turn Codex session twice through the WSS
  reducer pipeline (compressed vs byte-equal/direct), diffs the model-facing
  reconstructed context, and flags any case where the compressed context loses
  information present in the direct context. Isolated via `t.TempDir`, content-free,
  no private data committed.
- A minimal, neutral, idempotent (exactly once per session) system/developer note can
  be injected stating that elided content is requestable via `local-archive://<id>`;
  `reinjectArchivedContent` expands such a model-emitted URI to the exact archived
  bytes. Behind a config flag with a documented default; fail-open; no persistent
  machine state; voice-neutral (no product name in model-facing text).
- Re-read-after-collapse auto-restore: when the re-read canary
  (`ObserveQualityToolKeyForTurn` / `re_read_count`) detects the model re-requesting a
  path it previously collapsed, the next collapse of that path is suppressed (full
  pass) for the rest of the session.
- Socket-lifecycle measurement: a documented real multi-turn CLI + Desktop run
  recording whether cross-user-turn re-reads still resolve (`CommandUnresolvedBlocks`
  vs mutation), recorded in `docs/operation-log.md`, deciding whether t251 toolUse
  persistence is required.
- Content-free; doctrine clean (AGENTS.md §9); tests beside code; coverage gate
  >=95% stays green; `go build/vet/test ./... && go run ./scripts/ci` pass.

## Sub-Tasks

- [x] Build comprehension A/B replay harness (scripts/ + internal test fixtures).
      Replay multi-turn Codex traffic compressed vs direct; diff model-facing context;
      report info-loss. Non-gating CI report first.
- [x] Once-per-session neutral archive-recovery note injection on the WSS request path;
      config-flag gated; verify `reinjectArchivedContent` expands model-emitted URIs
      (extend the reinject test).
- [x] Re-read-after-collapse auto-restore driven by the existing canary; deterministic
      table test simulating N re-reads of a collapsed path flips that path to full pass.
- [ ] Run + document socket-lifecycle measurement (lsof `127.0.0.1:8990`,
      `decisions.jsonl` `route_mode`, CommandUnresolved vs mutations across SEPARATE
      user turns). Record verdict; decide t251 toolUse persistence.

## Notes

- % impact: enabler (no direct savings). Unlocks t253/t254/t255 to be enabled safely;
  without it they must stay shadow/default-off.
- 2026-05-30: core comparison engine landed in `internal/abharness`: it now detects
  shortenings, same-length content changes, missing blocks, and extra model-facing
  blocks.
- 2026-05-30: `internal/proxy.RunWSSPhaseFABReplay` now bridges decompressed Codex
  WSS frames into the real Phase-F reducer and then into `abharness.Compare`.
  CI-covered fixtures prove repeat-read read-delta is recoverable and prove that
  enabling the archive recovery note is auditable as extra model-facing context.
  `SLIMFERENCE_WSS_AB_CAPTURE=/private/path/frames.jsonl` records local WSS replay
  frames before mutation, and `go run ./scripts/utils wss-ab-replay <frames.jsonl>`
  exposes this as a text/JSON report with `--fail-on-lost`.
- 2026-05-30: real captured-session replay is now proven on scoped Codex CLI WSS:
  a double-read run captured 147 frames, replayed 3 request turns, found 1 real
  mutation, saved 6096 model-facing bytes in the A/B replay, reported `lost=0`
  and `gate=PASS`, while live admin counters showed 1414 billable input tokens
  saved, 1 read-delta block, and zero parse/degraded/compression errors.
- 2026-05-30: `RunWSSPhaseFABReplay` now uses an isolated temporary home for each
  offline replay, so prior disk-backed readcache/tooluse/archive state cannot
  skew reported A/B savings.
- 2026-05-30: WSS archive-recovery note injection landed behind
  `archive_recovery_note_enabled`, default-off, once per session, and voice-neutral.
  It injects no product name and keeps recovery proof-gated instead of making a new
  model-facing instruction default.
- 2026-05-30: re-read-after-collapse auto-restore landed. When a collapsed read key is
  requested again in the same WSS session, that key is suppressed from further collapse
  for the rest of the session, so the model gets a fresh full pass instead of a stale
  pointer loop.
- Risk: the recovery note is itself a prompt mutation - keep it minimal, neutral,
  flagged, and validate it against the A/B harness before any default-on.
- Dependencies: none. This is the first task in the v2 arc. t253/t254/t255 depend on it.
- Session-key collision (former WO-1/2b) is already closed by the 2026-05-30 T248
  audit proving distinct session ids across two conversations; not re-opened here.
- Doctrine: scoped, fail-open, content-free measurement, no machine-wide changes.

## Deviations

(none)

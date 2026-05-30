# TASK 251: Codex savings stability + cross-turn resolution robustness

Status: [~] PARTIAL - tool-use persistence, archive-id hardening, and bounded readcache state landed
Priority: P1 - protects and multiplies existing savings; removes hot-path latency
Scope: Codex-only WSS Phase-F. Make the existing savings robust across socket
lifecycle, remove per-read disk I/O, fix archive-id collisions, add recency-adaptive
aggressiveness, prompt-cache-aware mutation, and bounded state.

## Why

The headline read-delta resolution lives in an in-memory per-socket map
(`wsPhaseFAdapter.toolUses`, populated by `rememberToolUsesFromResponse`). If Codex
opens a new WSS socket per user turn, the map resets and a re-read in a later turn
becomes `CommandUnresolvedBlocks` -> zero savings (safe but ineffective). This is a
plausible cause of the low proof hit-rate (1 positive of 23 phasef_requests in the
Desktop proof). The readcache does synchronous disk I/O per read
(`EvaluateObserved` -> `LoadSession` + `readCacheSaveSession`,
`internal/readcache/evaluate.go`), the only place that can add user-visible stream
latency. Archive IDs embed a timestamp + 4KB content prefix
(`internal/contentarchive`), allowing a rare silent collision/overwrite. There is no
recency policy (old and fresh content are treated equally), no bounded state, and no
guard against mutating items inside the server-cached prefix (net-negative billing).

## Acceptance

- (Gated by the t249 socket-lifecycle measurement) The `call_id -> {toolName, command
  metadata}` map is persisted bounded + TTL per session, content-free (NO raw tool
  output). A reconnect rehydrates resolution; a re-read after a simulated socket reset
  still mutates.
- readcache session state is served from memory with async/periodic flush; no
  per-read disk round-trip; crash-safe (flush on close + interval).
- Archive IDs are content-addressed (collision-free, idempotent, auto-dedup of
  identical content).
- Recency-adaptive aggressiveness: the last N user turns are kept full; older content
  is compressed harder. N configurable.
- Prompt-cache-aware mutation: never mutate items inside the server-cached prefix
  (on the pure delta wire the request carries only new items, but guard explicitly).
- readcache / content-archive / toolUse state are bounded (TTL/LRU); no unbounded
  disk growth across many sessions.
- Coverage gate green; doctrine clean.

## Sub-Tasks

- [x] (if t249 measurement shows reconnect cold misses) Persist toolUse map to disk,
      bounded + TTL, content-free; rehydrate test across a simulated socket reset.
- [ ] In-memory readcache session state + async/periodic flush; remove the per-read
      Load/Save disk round-trip; crash-safe flush.
- [x] Content-addressed archive IDs (replace timestamp + 4KB-prefix scheme);
      collision test for two large files sharing a prefix in the same second.
- [ ] Recency-adaptive aggressiveness (keep last N turns full); deterministic test.
- [~] Prompt-cache-aware mutation guard (only mutate past the cached prefix); test.
- [~] Bounded session/readcache/archive state with TTL/LRU eviction.

## Notes

- % impact: toolUse persistence is a MULTIPLIER (could 2-5x effective hit-rate if
  reconnects are the cause - measure in t249 first). Recency policy ~+5-10% on old
  content AND reduces lost-in-the-middle drawdown. The rest is latency/correctness/
  stability, not direct savings.
- 2026-05-30: `internal/toolusecache` now persists bounded tool-call resolution
  metadata and `wsmitm_phasef` rehydrates it across WSS reconnects. It stores tool
  metadata, not raw tool output; live efficacy still needs a multi-turn reconnect
  measurement.
- 2026-05-30: archive IDs now include a full-content hash and are idempotent for the
  same archive input, closing the timestamp plus 4KB-prefix collision class. This is
  collision hardening, not a global content-only dedup feature across unrelated
  sessions/positions.
- 2026-05-30: readcache and tool-use state now have bounded pruning. The planned
  in-memory readcache plus async flush is still open.
- 2026-05-30: prompt-cache-aware mutation is currently verified by WSS shape
  reasoning: Codex WSS sends delta `input` items rather than the cached
  instructions/tools prefix, so the active reducer cannot mutate that prefix. An
  explicit regression test remains open.
- Dependencies: toolUse persistence gated by t249 socket measurement. The other
  sub-tasks are independent.
- Doctrine: content-free persistence (metadata only, never raw output), fail-open,
  scoped.

## Deviations

(none)

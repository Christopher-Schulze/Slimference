# TASK 255: Codex content-defined chunk dedup (FastCDC, rsync-for-LLM-context)

Status: [x] DONE - default-off WSS chunk-dedup is live-proven; default-on remains policy-gated
Priority: P2 - innovative core for "maximal sparsam" on read/log-heavy sessions
Scope: Codex-only WSS Phase-F. Deduplicate tool outputs and file reads at content-
defined CHUNK granularity across the whole session.

## Why

Whole-output dedup (exact non-file dedup, read-delta) only fires on near-identical
whole blocks. It misses PARTIAL overlap, which is the common case: a file read after a
small edit, two similar files, logs that share most lines, search outputs with
overlapping hits. Content-defined chunking (FastCDC: a rolling hash picks chunk
boundaries by content, so boundaries are stable under insertions/deletions) plus
chunk-level dedup across the session catches all of it - this is rsync/restic applied
to LLM context. A read that shares 80% of its chunks with something already sent goes
out as the 20% novel chunks + references.

No content-defined chunking infrastructure exists today: the only related code is the
whole-block MinHash in the dead Layer-1 module (`internal/compression/dedup_minhash.go`),
which is not chunk-level and not on the WSS path. This is genuinely new infrastructure.

## Acceptance

- A FastCDC (or equivalent content-defined) chunker produces stable, content-addressed
  chunks.
- A session-scoped chunk store (content-addressed, bounded, TTL/LRU) records chunks
  already sent to the model this session.
- Tool outputs / file reads are emitted as novel chunks + references to already-sent
  chunks; every reference is recoverable via the archive/recovery contract.
- A chunk-reference notation is understood by the model via the t249 recovery note;
  the model can request the full content of any referenced chunk.
- The t249 A/B harness shows no comprehension regression.
- Never net-negative (len/token guard); fail-open on any chunking/reference ambiguity;
  coverage gate green; doctrine clean.

## Sub-Tasks

- [x] Implement a FastCDC content-defined chunker (rolling hash, min/avg/max chunk
      size), content-addressed chunk IDs.
- [x] Session-scoped chunk store (bounded, TTL/LRU), content-free identity.
- [x] Chunk-reference encode (emit novel chunks + references) and decode (reinject for
      chunk references); recoverable via the t249 contract.
- [x] Wire into the WSS reducer for tool outputs / file reads; route attribution.
- [x] Tests proving partial-overlap savings (similar files and repeated chunks) + A/B
      harness run proving no comprehension regression.

## Notes

- % impact: ~10-30% on read/log-heavy sessions (catches partial overlap that whole-
  output dedup cannot); high effort.
- TASK-SPLIT candidate: chunker, store, and reducer wiring can split into separate
  TASKs if scope grows.
- Dependencies: HARD on t249 (recovery contract + A/B harness). Overlaps conceptually
  with t254 (the mirror could consume chunk identities); coordinate so they share the
  content-addressed store rather than duplicate it.
- 2026-05-30: `internal/chunkdedup` now has a deterministic FastCDC-style chunker, a
  bounded TTL/LRU session chunk store, neutral `[context-chunk ...]` references, and
  decode support for replay tooling. The proxy wires a store into shared Codex Layer-0
  and WSS Phase-F, but only when both `codex_chunk_dedup_enabled=true` and
  `archive_recovery_note_enabled=true` are set. Default remains off. Tests prove partial
  overlap savings for similar reads and WSS route attribution. Remaining gate: run the
  t249 A/B harness on real captured chunk-dedup frames before any default-on promotion.
- 2026-05-30: `wss-ab-replay` now has a `--codex-chunk-dedup` proof switch. It
  enables the default-off chunk path in the isolated replay config, implies the
  archive-recovery note, and separates the expected once-per-session recovery-note
  extra block from true `--fail-on-lost` gate failures. Synthetic real-reducer replay
  now proves partial-overlap chunk references save bytes with no unreferenced loss;
  live captured Codex frames are still required before default-on.
- 2026-05-30: live scoped Codex WSS proof passed on real captured frames after
  tuning chunk defaults for Codex's truncated exec-output envelope. A final
  telemetry rerun on `/tmp/slimference-t255-chunk-telemetry-20260530T150436Z.jsonl`
  confirmed the new admin counters. Replay
  `--codex-chunk-dedup --chunk-dedup-min-bytes=0 --fail-on-lost --json` on
  that capture reported `frames=123`, `mutated_requests=2`, `bytes_saved=7757`, `lost=1`,
  `expected_extras=1`, and `gate_passed=true`; the only lost-class item was the
  expected recovery note, while the actual chunk elision was referenced. Live
  admin counters for the same run reported `input_tokens_saved=1707`,
  `proxy_layer0_chunk_dedup_blocks=1`,
  `proxy_layer0_routes.wss_phasef.chunk_dedup_blocks=1`,
  `compressed_messages_mutated=2`, `frames_reencoded=2`, and zero parse,
  degraded-session, or compression errors. The daemon was restarted normally
  afterward with no experimental env flags active. Default remains off until a
  separate product decision promotes it.
- Doctrine: content-free identity, fail-open, scoped; references always recoverable so
  no loss is permanent.

## Deviations

(none)

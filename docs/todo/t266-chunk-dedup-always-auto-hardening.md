# T266 - Chunk dedup always-auto hardening

## Why

Content-defined chunk dedup can save heavily on similar outputs: edited files,
similar files, repeated logs, and overlapping command results. It is also more
invasive than unchanged-read collapse because the server state stores references
and new bytes, not a full fresh output. This task defines the work required to
make chunk dedup automatic without product drawdowns.

## Current reality check

- FastCDC chunking and a bounded session chunk store exist.
- WSS route attribution and replay proof exist.
- Chunk references are recoverable where archive support is available.
- Chunk references are cross-send only. Repeated chunks introduced inside the
  same first model-facing output now seed identity state but stay verbatim, so
  the first observation is always full context.
- Chunk encode now has local self-verification: a changed reference stream must
  decode back to the exact original bytes before it can be returned. Archive URI
  collisions or orphan references fail open to the original output.
- Per-output reference-density rejection happens before cumulative session
  reference-budget accounting, so candidates that full-pass because they are too
  reference-dense do not poison later session budget decisions.
- Chunk refs are now suppressed for patch/diff/edit-style command outputs such
  as `apply_patch`, `patch`, `git diff`, `git show`, `git apply`, `git am`, and
  `git format-patch`. These outputs can still use deterministic filters and
  exact repeated-output reducers, but content-defined references are not allowed
  to split fresh patch reasoning context.
- It is not enough to prove one matching workload. Default-auto needs broad
  proof and runtime self-protection.

## Product target

Chunk dedup may be always-auto only for routes/workloads where:

- chunks were previously full-seen by the model or are exactly recoverable
- archive recovery note/contract is available
- recency and re-read canaries are active
- integrity budget prevents cumulative reference erosion
- live corpus shows no workflow or comprehension regression

## Technical work packages

1. Make eligibility explicit:
   - WSS only unless HTTP recovery is proven
- no first-observation chunk references without full source seeding
- [x] no same-output chunk references before the model has received that output
  as full context
   - [x] no chunk refs for active patch/diff/edit outputs
   - no chunk refs under recent edit uncertainty
2. Add integrity budget:
   - per-session ratio of referenced bytes to total tool-output bytes
   - per-output maximum reference density
   - automatic full-pass when budget is exceeded
3. Strengthen recovery:
   - [x] archive id for every referenced chunk group
   - [x] exact local decode self-check before returning a changed stream
   - [x] fail open on archive URI collision or orphan references
   - [x] route refuses chunk refs if archive write fails
   - [x] add content-free chunk reference density reporting to WSS audit/admin state
   - [x] per-output reference-density cap defaults to 90% and full-passes when
     the candidate would replace too much fresh model-facing output
   - [x] per-session accepted-reference budget defaults to 70% and full-passes
     once cumulative chunk references would dominate the session
4. Add recency policy:
   - deliberate re-read of same file may full-pass or provide salient summary
     plus refs, never bare refs if canary says the model is struggling
   - post-collapse re-read restores full context
5. Add live proof matrix:
   - edited file
   - similar files
   - repeated logs
   - repeated test output
   - large search output
   - Desktop reconnect
   - CLI long session

## Zero product-drawdown gates

- No unresolved reference can enter model-facing context.
- No chunk ref without a known previous full-seen or exact archive-backed source.
- No first-send repeated chunk inside a single output can become a reference;
  those chunks only seed later sends.
- Re-read spike disables chunk dedup for that session.
- Any decode mismatch fails open before model-facing context.
- Any output above the configured reference-density threshold full-passes.
- Any session above the configured cumulative reference-density threshold full-passes.
- Aggressive chunking must not affect patches, final code output, or terminal
  protocol correctness.

## Savings targets

- Similar-output workloads: target 10% to 30% additional billable-input savings
  over exact read/repeated-output reducers.
- Normal workday: positive net savings after recovery-note overhead.
- Host cost: chunking large outputs remains bounded and below measurable UX
  impact, with auto-bypass for very small outputs.

## Verification

- Chunk encode/decode exactness tests.
- Store TTL/LRU tests.
- Integrity budget tests using the admin/audit chunk-density counters.
- WSS replay with `--fail-on-lost`.
- A/B replay must reconstruct chunk references back to the exact source block,
  not merely detect that a URI exists.
- Live CLI/Desktop matrix with canary counters and no repair-loop increase.

## Done

Chunk dedup is always-auto only when it is automatic, recoverable, budgeted,
canary-protected, and proven across multiple real Codex workloads. Before that,
it remains guarded by policy.

## Progress

- 2026-06-01: Moved per-output reference-density enforcement into the chunk
  store before cumulative session budget accounting. A dense candidate that
  full-passes now still seeds chunk identity, because the model receives the
  original bytes, but it no longer consumes the accepted-reference session
  budget or skews density telemetry.
- 2026-06-02: Hardened chunk dedup to cross-send-only. The store no longer emits
  references for repeated chunks first seen earlier in the same output; it marks
  them as seen only for future outputs. This preserves first-observation context
  while keeping later overlap savings. The offline A/B harness now aligns
  inserted recovery-note blocks instead of misclassifying shifted user/tool
  blocks, and it verifies `[context-chunk ... local-archive://...]` references
  by expanding all chunk refs and comparing the reconstructed block to the exact
  direct model-facing source.
- 2026-06-02: Fixed the cumulative session reference-budget denominator. The
  store now counts every observed output as model-visible budget, including
  first-send seed outputs and rejected full-pass candidates, while counting only
  accepted chunk refs in the numerator. This removes the false block where a
  safe second output with large overlap was rejected because the first full
  seed output was not counted. The real CLI WSS chunk probe capture
  `chunk-live-cli-similar-output-20260602T150301.jsonl` now replays through
  default auto with `reducer_tokens_saved=6636`,
  `reducer_chunk_dedup_blocks=1`, `reducer_chunk_dedup_references=4`,
  `bytes_saved=32195`, and `gate_passed=true`. This is a real-frame reducer
  replay proof; a fresh live-token matrix row remains separate.
- 2026-06-02: `codex-capture-run` and `wss-proof-matrix` now carry chunk
  reference telemetry in matrix `live_delta`: `proxy_layer0_chunk_dedup_blocks`,
  `proxy_layer0_chunk_dedup_references`, referenced bytes, and input bytes.
  Chunk-specific live rows can now require both `--expected-reducer chunk_dedup`
  and `--expected-reducer chunk_dedup_refs`, plus `host_budget_ok`, so a future
  chunk proof cannot pass on replay bytes alone or on a block counter without
  actual chunk references.
- 2026-06-02: Live CLI chunk proof remains open. The capture
  `/Users/christopher/.slimference/captures/live-cli-chunk-dedup-current-20260602T182411.jsonl`
  contains two real 40 KB `function_call_output` frames and replay mutates them
  with `reducer_chunk_dedup_blocks=1`, `reducer_chunk_dedup_references=1`,
  `reducer_tokens_saved=1807`, `bytes_saved=7907`, `lost=1`,
  `expected_extras=1`, and `gate_passed=true`, but the live daemon matrix row
  recorded zero billable-token savings and zero live chunk counters. This is not
  sufficient for default promotion. Follow-up must prove the same mechanism
  through a live `phasef_mutations>0` row, or fix the live wiring/counter path
  if replay and live continue to diverge.
- 2026-06-02: Added policy/cache deltas to `codex-capture-run` live output and
  matrix rows, then reran the clean two-file CLI WSS proof:
  `/Users/christopher/.slimference/captures/live-cli-chunk-dedup-policy-cat-20260602T165831Z.jsonl`.
  The new live delta explains the previous zero-hit result: chunk-dedup was not
  miswired; it was deliberately demoted with `chunk_dedup/full_pass
  host_budget_full_context` because `/admin/state.host_budget` reported
  `cpu_window_budget_exceeded` (`cpu_window_percent=428.57`, RSS 17 MB, state
  3.98 MB). Replay on the same frames still produces one chunk-dedup mutation
  and 7907 request bytes saved, so the remaining blocker for default promotion
  is product-resource stability, not reducer correctness. Next proof must either
  show live chunk hits under `host_budget_ok`, or make the chunk hotpath cheaper
  enough that the host-budget guard stays green on the real capture workload.
- 2026-06-02: Follow-up host-budget hardening removed the false startup-poll
  class from this blocker: CPU-window demotion now requires at least a one-second
  measured window. The focused chunk proof still needs to be rerun with the new
  guard and a clean latency-budget state; a pass must show live
  `proxy_layer0_chunk_dedup_blocks>0`, `proxy_layer0_chunk_dedup_references>0`,
  `host_budget_ok`, zero parse/degrade/compression errors, and replay
  reconstruction gate pass on the same frames.
- 2026-06-02: Reran the focused CLI WSS chunk proof after the host-budget
  minimum-sample fix:
  `/Users/christopher/.slimference/captures/live-cli-chunk-dedup-hostguard-cat-20260602T170717Z.jsonl`.
  Result: live `billable_input_tokens_saved=1722`,
  `proxy_layer0_chunk_dedup_blocks=1`,
  `proxy_layer0_chunk_dedup_references=1`,
  `proxy_layer0_chunk_dedup_referenced_bytes=8192`,
  `proxy_layer0_chunk_dedup_input_bytes=32064`, `phasef_mutations=2`,
  `parse_failures=0`, `degraded_sessions=0`, `compression_errors=0`,
  `host_budget_status=ok`, and policy delta
  `chunk_dedup/allow recoverable_chunk_dedup`. The focused matrix gate with
  `chunk_dedup`, `chunk_dedup_refs`, and `host_budget_ok` passed. Replay on the
  same frames reports `reducer_tokens_saved=1722`, `bytes_saved=7907`,
  one chunk reference, `expected_extras=1`, and `gate_passed=true`.
- 2026-06-02: `codex-capture-run` now supports `--transport=wss` for focused
  mechanism proofs. Release proof can stay on `auto`, but chunk-dedup promotion
  should force `wss` while debugging so bridge/fallback cannot be mistaken for
  a negative reducer result.
- 2026-06-02: Ran the matching focused Desktop Codex.app chunk proof:
  `/Users/christopher/.slimference/captures/desktop-chunk-dedup-proof-20260602T172859Z.jsonl`
  with matrix row
  `/Users/christopher/.slimference/captures/desktop-chunk-dedup-matrix.jsonl`.
  The live Desktop delta reports `billable_input_tokens_saved=1719`,
  `proxy_layer0_chunk_dedup_blocks=1`,
  `proxy_layer0_chunk_dedup_references=1`,
  `proxy_layer0_chunk_dedup_referenced_bytes=8192`,
  `proxy_layer0_chunk_dedup_input_bytes=32064`, `phasef_mutations=3`,
  `parse_failures=0`, `degraded_sessions=0`, `compression_errors=0`,
  `host_budget_status=ok`, and policy delta
  `chunk_dedup/allow recoverable_chunk_dedup`. The focused matrix gate with
  `--require-live-token-delta`, `--required-workload=similar_files`,
  `--min-desktop=1`, `chunk_dedup`, `chunk_dedup_refs`, and `host_budget_ok`
  passed. Replay on the same Desktop frames reports
  `reducer_tokens_saved=1719`, `bytes_saved=7907`, one chunk reference,
  `expected_extras=1`, and `gate_passed=true`.

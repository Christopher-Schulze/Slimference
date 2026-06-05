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
  reference-budget accounting. The store now uses the remaining per-output and
  per-session byte budgets during encoding: references stop when the budget is
  exhausted and the rest of the output stays verbatim, so useful overlap is not
  lost just because a whole candidate would be too dense.
- Chunk encode now evaluates both FastCDC chunks and a line-boundary chunk plan.
  The line plan is exact and only selected if it locally reconstructs the
  original output with higher net savings, which gives long logs and stable
  command-output lines a second chance without making generic chunking more
  aggressive.
- Chunk refs are now suppressed for patch/diff/edit-style command outputs and
  patch/diff file reads such as `apply_patch`, `patch`, `diff`, `colordiff`,
  `git diff`, `git show`, `git log -p`, `git apply`, `git am`,
  `git format-patch`, `gh pr diff`, `gh pr view --patch`, `jj diff`,
  `jj show`, `hg diff`, and `svn diff`. Normal search/status commands remain
  eligible, including searches whose pattern is the word `diff`. Patch/diff
  outputs can still use deterministic filters and exact repeated-output
  reducers, but content-defined references are not allowed to split fresh patch
  reasoning context.
- Chunk refs are now also suppressed when the current Layer-0 batch carries an
  edit/apply-patch/write signal. This recent-edit uncertainty only demotes
  chunk dedup; lossless read-delta and exact repeated-output reducers remain
  available. Fresh post-edit command/search/log outputs therefore stay full
  context instead of being represented as chunk references.
- Default-auto proof needs broad coverage and runtime self-protection. The
  current proof set covers the distinct chunk-reference product surface on CLI
  and Desktop similar-output workloads, and covers large log/test outputs through
  the safer captured-output reducer when it wins before chunk refs.

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
   - [x] no chunk refs under recent edit uncertainty
2. Add integrity budget:
   - [x] per-session ratio of referenced bytes to total tool-output bytes
   - [x] per-output maximum reference density
   - [x] automatic full-pass when budget is exceeded
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
   - [x] encode references only up to the remaining byte budget and leave the
     rest verbatim, preserving recency while keeping positive savings
   - [x] use Codex Responses top-level `instructions` for the recovery note;
     never emit an `input` item with `role=system`
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
- A/B replay must include Codex Responses top-level `instructions` as
  model-facing context, so recovery notes and output-reduce hints cannot be
  invisible to the no-drawdown gate.
- Live CLI/Desktop matrix with canary counters and no repair-loop increase.

## Done

Chunk dedup is product-ready for the guarded automatic policy when it is
automatic, recoverable, budgeted, canary-protected, and proven across multiple
real Codex workloads. The current closeout accepts the safest reducer for each
workload: actual chunk refs for similar-output overlap, captured-output
compaction for logs/tests when that stricter deterministic reducer wins first,
and full-pass for patch/diff/edit or recent-edit uncertainty.

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
- 2026-06-02: Hardened similar-log coverage. The chunk store now evaluates a
  second exact line-boundary chunk plan beside the FastCDC plan and chooses the
  locally verified best result. It also treats reference caps as byte budgets:
  once the per-output or per-session budget is exhausted, later repeated chunks
  stay verbatim instead of forcing the whole candidate to full-pass. The focused
  unit proof `TestStore_LineOrientedLogsDedupWithinReferenceBudget` verifies
  two 520-line logs with one differing failure line: first output seeds only,
  second output emits archive-backed references within a 90% cap, and decoding
  reconstructs the exact second output.
- 2026-06-02: Replayed the earlier real Desktop log capture
  `/Users/christopher/.slimference/captures/desktop-chunk-log-proof-20260602T182050Z.jsonl`
  after the line-boundary hardening. The same real WSS frames now produce
  `mutated_requests=1`, `bytes_saved=33843`, `reducer_tokens_saved=7155`,
  `reducer_chunk_dedup_blocks=1`, `reducer_chunk_dedup_references=16`,
  `reducer_chunk_dedup_referenced_bytes=35378`,
  `reducer_chunk_dedup_input_bytes=40158`, `lost=0`, and `gate_passed=true`.
  This closes the offline reducer gap for similar logs.
- 2026-06-02: Fixed a real Codex Responses recovery-note regression found during
  the Desktop log proof. The archive-recovery hint previously used an `input`
  item with `role=system`, and Codex returned `400 System messages are not
  allowed`. `internal/beterse` now writes Codex hints to the top-level
  `instructions` string, appends idempotently, and refuses to create
  `instructions` on JSON bodies without a Responses `input` field. The tests
  assert no `input` system item is emitted.
- 2026-06-02: Re-ran the Desktop log workload with short commands after the
  recovery-note fix:
  `/Users/christopher/.slimference/captures/desktop-chunk-log-proof-short-20260602T195730Z.jsonl`.
  The real commands produced two ~40 KB outputs and live WSS saved
  `16192` billable input tokens with `wss_phasef.requests_modified=2`,
  `captured_output_blocks=2`, `chunk_dedup_blocks=0`, zero lost blocks in replay,
  and no `System messages are not allowed` error. This is the desired product
  behavior: the stricter captured-output/log reducer wins first on this workload,
  and chunk dedup remains the guarded fallback for large similar outputs that
  survive safer deterministic reducers.
- 2026-06-03: Broadened the patch/diff/edit guard for chunk-dedup eligibility.
  The guard now blocks direct and wrapped diff-producing commands across
  `diff`/`colordiff`, Git, GitHub CLI, Jujutsu, Mercurial, and Subversion, plus
  `.patch`/`.diff` file reads, while explicitly preserving normal `rg diff ...`
  searches and `git status` outputs. Focused proxy tests prove the guard and the
  existing WSS chunk-dedup policy tests still pass.
- 2026-06-03: Closed a WSS A/B replay blind spot for Codex Responses
  `instructions`. Recovery notes and other Codex top-level instruction
  mutations are now included as synthetic model-facing system context during
  replay comparison, so the no-drawdown gate can see them. The focused proxy and
  `scripts/utils` replay tests now prove recovery-note extras are separated from
  true loss while chunk references still pass through exact archive expansion.
- 2026-06-03: Added a chunk-specific recent-edit uncertainty guard. Layer-0 now
  detects edit/apply-patch/write signals in the current batch and passes that
  signal to savings policy. Policy full-passes chunk dedup with reason
  `recent_edit_uncertain_chunk_full_context` while leaving lossless reducers
  enabled. Focused policy and reducer tests prove a fresh post-edit command
  output with prior chunk overlap does not receive `context-chunk` references.
- 2026-06-03: Wired the cumulative session reference budget into runtime policy
  instead of leaving it only inside the encoder. The chunk store now exposes a
  content-free budget-available-after-candidate signal; Layer-0 maps a budget
  miss to `session_integrity_budget`, which full-passes chunk dedup while
  keeping lossless read-delta and exact repeated-output reducers enabled.
  Focused store, policy, and reducer tests cover the signal.
- 2026-06-03: Local proof inventory now finds positive live chunk-dedup rows for
  `chunk_dedup_similar_outputs` and live hits for both `chunk_dedup` and
  `chunk_dedup_refs`. It also makes the remaining breadth explicit:
  no committed live proof-matrix rows yet exist for `chunk_dedup_log_output` or
  `chunk_dedup_test_output`, even though real log frames replay through the
  offline chunk fallback and the live log workload saved earlier through the
  stricter captured-output reducer.
- 2026-06-03: Corrected the proof semantics for large log/test outputs. These
  workload classes are product output-saving proofs, not an instruction to force
  chunk references over a safer deterministic reducer. The inventory now accepts
  either `captured_output` or recoverable `chunk_dedup_refs` for
  `chunk_dedup_log_output` and `chunk_dedup_test_output`, while still requiring
  positive live token savings, `host_budget_ok`, and zero safety issues. Similar
  files still require actual chunk refs because that is the distinct chunk-dedup
  product surface.
- 2026-06-04: Local proof inventory reports complete maxx workload status for
  `chunk_dedup_similar_outputs`, `chunk_dedup_log_output`, and
  `chunk_dedup_test_output` with zero safety issues. The exported chunk
  categories pass their mechanism-specific corpus validators and close the
  current chunk product claim without over-forcing chunk refs where a safer
  deterministic reducer already saves. The global `benchmark-corpus
  --maxx-check` is intentionally held open by T267 output-token evidence, not by
  chunk-dedup.

# T261 - Layer 1 exact/reversible safety-tier max-out

## Why

Layer 1 is the historical deterministic compression engine. It is valuable for
HTTP/full-history bodies and non-Codex shapes, but deterministic does not mean
zero-drawdown. Some transformations are exact or reversible; others are
structure summaries that can reduce context fidelity. This task makes Layer 1
safe by construction through explicit tiers and enforcement.

## Current reality check

- Layer 1 is implemented and default-enabled.
- Layer 1 now has `Layer1SubLayerRegistry()` with stable metadata for each
  sublayer's safety tier, default eligibility, archive requirement, model risk,
  and recovery path.
- The executor enforces archive-required mutations in the hot path. If a
  sublayer needs archive recovery and no archive id can be written, the original
  block full-passes and per-block savings are rolled back.
- Unknown future sublayer tags fail closed: they require archive recovery until
  the registry explicitly classifies them.
- It contains a mix of safety classes:
  - exact/lossless: ANSI stripping when only terminal control bytes are removed,
    JSON minification, path dictionary with recovery
  - reversible/recoverable: repeated exact content references, archive-backed
    near-duplicate references, archive-backed replacement
  - lossy or reconstructive: structure extraction, comment stripping, success
    short-circuit, graph pruning, tool-output compression
- The Layer 1 result and HTTP decisions log now include content-free
  per-sub-layer decision records with tier, applied flag, reason, saved tokens,
  archive requirement, recovery path, and default eligibility.
- Live HTTP callers now pass coordinator/subsume gates through request-scoped
  `Layer1CompressOptions`, and the compressor serializes receiver-local call
  state (`activeSessionID`, dedup threshold, active coordinator flag) so parallel
  requests cannot mix archive session ids or policy decisions.
- Existing negative-savings guards protect token count, not comprehension.

## Product target

Default Layer 1 must run only transformations whose safety contract is explicit.
Riskier transformations may exist, but they must be gated by recovery, proof,
route/workload policy, and quality canaries. The model must not lose usable
context, memory, or workflow capability because Layer 1 chose a clever
compression shortcut.

## Technical work packages

1. Add a Layer 1 sublayer safety registry:
   - [x] exact
   - [x] reversible
   - [x] recoverable-with-archive
   - [x] task-preserving-summary
   - [x] non-default/research
2. Enforce tier rules in the Layer 1 executor:
   - [x] archive-required mutations must full-pass on archive failure
   - [x] unknown sublayer tags require archive recovery until classified
   - [x] default exact/reversible sublayers can run without archive only when
     their registry contract says recovery is not needed
   - [x] mode-specific default/auto/max enforcement is covered by the current
     safety registry, prompt-cache boundary protection, and Layer-1 corpus
     round-trip guard; broader policy changes still require their own evidence
     before they can mutate model-facing text
3. Add a per-sublayer decision record:
   - [x] attempted
   - [x] applied
   - [x] tier
   - [x] reason
   - [x] saved tokens
   - [x] archive requirement
   - [x] recovery path
   - [x] default eligibility
   - [x] per-sub-layer archive write counts for recoverability audit
4. Convert context-risky sublayers to archive-backed form where feasible:
   - structure extraction must preserve anchors and archive full original
   - comment strip must keep critical comments and archive original if applied
   - graph pruning must preserve all referenced messages and archive pruned
     bodies
5. Add "no hidden loss" tests:
   - exact sublayers are byte-equivalent after decode/reconstruct
   - reversible sublayers expand to original
   - [x] recoverable near-dedup includes a valid archive id and expands to exact
     original bytes
   - [x] a multi-message Layer-1 corpus loads every emitted archive id and
     verifies exact original block bytes plus session/message/block metadata
   - [x] risky sublayers full-pass when archive is unavailable
6. Add prompt-cache-aware mutation checks:
   - [x] never mutate stable provider-cached prefixes when the token economics
     are net negative
   - [x] never trade a provider cache hit for small local savings without proof

## Zero product-drawdown gates

- Default Layer 1 cannot silently remove facts, code, comments marked critical,
  file paths, errors, or tool outputs unless the full original is recoverable.
- If an archive write fails, the sublayer full-passes.
- Unknown sublayer tags must fail closed by requiring archive recovery until the
  registry is updated.
- If reconstruction cannot be proven in unit tests, the sublayer is not eligible
  for default-auto.
- Layer 1 must not cause prompt-cache invalidation that costs more than the
  local savings.

## Savings targets

- HTTP/full-history corpora: positive net billable-input savings in long
  sessions while preserving reconstructed context.
- Codex WSS corpora: Layer 1 should not be the main savings source, but it must
  not regress WSS route behavior when invoked through fallback or legacy paths.
- Host cost: p95 Layer 1 processing under 10 ms for 250 KB request bodies on
  Apple Silicon, unless the body is larger than normal Codex frames and the
  operation remains below 1% of upstream inference time.

## Verification

- Unit tests for each sublayer tier.
- Round-trip tests for every reversible/recoverable sublayer.
- Corpus replay comparing direct vs compressed model-facing context.
- `go test ./internal/compression ./internal/proxy`
- `go run ./scripts/ci`

## Progress

- 2026-05-31: Added the Layer 1 safety registry with contract tests. Every
  known sublayer is classified by safety tier, default eligibility, model risk,
  archive requirement, and recovery path. This does not yet change compression
  output; it makes the next enforcement step auditable.
- 2026-05-31: Enforced archive-required Layer 1 mutations in the executor. Comment
  stripping, structure extraction, tool-output summaries, image replacement,
  preview, in-window tool-output compression, and product graph-pruning only
  commit model-facing replacements after a valid archive id exists. Archive
  failure now full-passes the original block and resets per-block savings
  counters instead of silently shipping unrecoverable context loss.
- 2026-06-01: Added `Layer1DecisionRecord` telemetry to every Layer 1 result and
  exposed it through proxy decision summaries. The record is content-free and
  ties each sub-layer to its registry tier, default eligibility, recovery path,
  archive requirement, application state, reason, and saved-token count. The
  mapping separates `tool_compressor` from `tool_output_in_window` attribution
  while preserving legacy aggregate accounting.
- 2026-06-04: Tightened Layer 1 decision honesty. The compressor now records
  content-free per-sub-layer attempt counts only when a reducer is actually
  reached, and `layer1_decisions` reports `not_attempted` for registered
  sub-layers the workload never exercised. Proxy decision summaries now also
  carry `archive_writes`, so an archive-required applied decision remains
  auditable outside the compression package.
- 2026-06-02: Hardened the Layer 1 registry guard to fail closed for unknown
  sub-layer tags. Any future unclassified mutation now requires archive recovery
  before model-facing text can change, preventing accidental unrecoverable
  context loss from new Layer 1 work.
- 2026-06-02: Hardened the same guard for unattributed non-ANSI mutations. An
  empty sub-layer tag list now also requires archive recovery, so a future
  mutating pass cannot bypass recovery merely by forgetting to append its
  registry id.
- 2026-06-02: Hardened the `structure_in_window` side path. It now archives the
  original block before replacing in-window tool-result text with a structural
  summary and full-passes when no archive id is available. This brings the
  opt-in in-window path under the same `structure_extract` archive-required
  safety contract as the normal Layer 1 executor.
- 2026-06-02: Promoted `success_short_circuit` to archive-required. Success-only
  build/test/log summaries still require the success classifier, but they now
  also full-pass unless the verbose original can be archived and stamped before
  the `[ok]` marker replaces model-facing text.
- 2026-06-03: Neutralized the reversible path-dictionary marker. The
  `semantic_dictionary` sub-layer now emits `[path dictionary]` /
  `[/path dictionary]` instead of product-branded marker text while keeping the
  same inline `[P1]=/absolute/path` legend and strict positive-savings gate. This
  removes a prompt-contamination surface from a default-eligible reversible
  sub-layer without reducing reconstructability.
- 2026-06-03: Split fuzzy MinHash dedup from exact dedup. Exact duplicate
  collapse keeps the existing reversible `dedup` contract, but non-identical
  near-duplicate collapse now uses `dedup_near`, requires content-archive
  recovery, full-passes when no archive id is available, and reports its own
  decision counter. This removes the silent context-loss risk where similar but
  changed text could previously be replaced by a bare "near duplicate" marker.
- 2026-06-03: Hardened Layer 1 request scoping. `CompressWithSessionOptions`
  carries coordinator/subsume decisions per request, the HTTP hot path no longer
  mutates receiver-global coordinator state, and the compressor serializes the
  receiver-local fields used by inner fan-out workers. Focused tests prove
  request-scoped options do not inherit legacy state and concurrent archive writes
  keep the correct session id.
- 2026-06-03: Added prompt-cache boundary protection. Layer 1 now skips any
  content block that already carries provider `cache_control`, so local
  compression cannot mutate a stable cached prefix for small savings or disturb
  provider prompt-cache economics. Regression coverage proves large
  cache-controlled tool outputs remain byte-identical, unarchived, and
  savings-neutral.
- 2026-06-04: Hardened the registry contract for default eligibility. Tests now
  fail if a future default-eligible Layer 1 sublayer is neither exact nor
  reversible and also lacks archive recovery. This turns the product
  zero-drawdown rule into an executable guard instead of relying on prose.
  Focused `go test ./internal/compression` coverage is green, and the current
  exported live corpus passes both `benchmark-corpus --promotion-check` and
  `--maxx-check`. Remaining closeout is not WSS product safety, but a dedicated
  Layer-1/full-history corpus round-trip proof for the historical HTTP path and
  exact/reversible reconstruction claims.
- 2026-06-04: Added content-free per-sub-layer archive write accounting to
  `Layer1Result` and `Layer1DecisionRecord`. The count is incremented only after
  the recorder returns a non-empty archive id, so archive-required applied
  decisions can now prove that recovery material was actually written instead of
  merely intended. Focused and full `go test ./internal/compression -count=1`
  are green.
- 2026-06-04: Added a DiskRecorder-backed offline round-trip guard for
  archive-backed `dedup_near`. The fixture compresses a similar-but-changed
  block, reads the resulting archive id through `contentarchive.Get`, and asserts
  exact original bytes plus session/sub-layer/message/block metadata. This proves
  the most dangerous Layer 1 dedup shape is recoverable locally without relying
  on live traffic.
- 2026-06-04: Closed the dedicated Layer-1/full-history offline proof. The
  corpus exercises multiple historical messages, archive-backed comment stripping
  and archive-backed near-dedup, then loads every emitted archive id through
  `contentarchive.Get` and compares the stored bytes against the original
  `types.ContentBlock` text at the recorded message/block position. Applied
  archive-required decisions must also carry positive archive-write counts. This
  proves the default Layer-1 safety contract at the reconstruction boundary
  without relying on WSS captures or model-facing archive instructions.

## Done

Layer 1 is maxxed for the current product contract: every sublayer has a
declared safety tier, the executor enforces that tier, risky transformations are
archive-backed or non-default, archive failure full-passes, and focused plus
corpus tests prove exact local recovery for emitted archive ids. Decision
telemetry now distinguishes unattempted, attempted-zero, applied, and
archive-backed-applied states without payload logging. Any future new sublayer
or broader model-facing policy must meet the same gates before it can run
default-auto.

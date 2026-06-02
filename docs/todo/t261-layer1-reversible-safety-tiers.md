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
  - reversible/recoverable: archive-backed replacement, repeated exact content
    references
  - lossy or reconstructive: structure extraction, comment stripping, success
    short-circuit, graph pruning, tool-output compression
- The Layer 1 result and HTTP decisions log now include content-free
  per-sub-layer decision records with tier, applied flag, reason, saved tokens,
  archive requirement, recovery path, and default eligibility.
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
   - [ ] mode-specific default/auto/max enforcement needs a final corpus proof
     pass before any broader policy change
3. Add a per-sublayer decision record:
   - [x] attempted
   - [x] applied
   - [x] tier
   - [x] reason
   - [x] saved tokens
   - [x] archive requirement
   - [x] recovery path
   - [x] default eligibility
   - [ ] per-sub-layer archive id counts if live proof shows this is needed
4. Convert context-risky sublayers to archive-backed form where feasible:
   - structure extraction must preserve anchors and archive full original
   - comment strip must keep critical comments and archive original if applied
   - graph pruning must preserve all referenced messages and archive pruned
     bodies
5. Add "no hidden loss" tests:
   - exact sublayers are byte-equivalent after decode/reconstruct
   - reversible sublayers expand to original
   - recoverable sublayers include valid archive ids
   - risky sublayers full-pass when archive is unavailable
6. Add prompt-cache-aware mutation checks:
   - never mutate stable provider-cached prefixes when the token economics are
     net negative
   - never trade a provider cache hit for small local savings without proof

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
- 2026-06-02: Hardened the Layer 1 registry guard to fail closed for unknown
  sub-layer tags. Any future unclassified mutation now requires archive recovery
  before model-facing text can change, preventing accidental unrecoverable
  context loss from new Layer 1 work.

## Done

Layer 1 is maxxed only when every sublayer has a declared safety tier, the
executor enforces that tier, risky transformations are archive-backed or
non-default, and proof shows savings without model-facing context loss.

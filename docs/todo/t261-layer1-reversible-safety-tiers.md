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
  and recovery path. This is control-plane metadata; executor enforcement is
  still tracked below.
- It contains a mix of safety classes:
  - exact/lossless: ANSI stripping when only terminal control bytes are removed,
    JSON minification, path dictionary with recovery
  - reversible/recoverable: archive-backed replacement, repeated exact content
    references
  - lossy or reconstructive: structure extraction, comment stripping, success
    short-circuit, graph pruning, tool-output compression
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
   - default: exact + reversible + proven recoverable only
   - auto: default plus proof-gated task-preserving summaries
   - max: only mechanisms that still have recovery or live proof
   - off: byte-equivalent pass
3. Add a per-sublayer decision record:
   - attempted
   - applied
   - tier
   - reason
   - saved tokens
   - recovered archive id count
   - bypass reason
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

## Done

Layer 1 is maxxed only when every sublayer has a declared safety tier, the
executor enforces that tier, risky transformations are archive-backed or
non-default, and proof shows savings without model-facing context loss.

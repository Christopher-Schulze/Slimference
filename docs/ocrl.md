# OCRL - Old Context Replacement Layer

OCRL is the Layer 2 product direction. It replaces old inactive context with
deterministic, archive-backed capsules only when the replacement is proven
recoverable and cheaper than the original context. It is not an abstractive or
extractive summary layer.

## Product Contract

OCRL may become model-facing only when all of these are true:

- the route resends old full context and can actually save input tokens
- the current session id is known and matches every selected capsule
- selected context is old, inactive, outside the recent working set, and not
  under quality pressure
- selected context does not touch active files
- selected context is not a high-risk failure, active user instruction, active
  patch, unresolved decision, or recovery-sensitive block
- every selected capsule has complete deterministic facts
- every selected capsule carries archive ids for omitted raw content
- archive expansion succeeds before replacement
- token accounting proves positive net savings after capsule and recovery
  overhead
- any gate failure falls back to full-pass original context

The model must never be asked to trust a paraphrase. Capsules are indexes into
already observed, exactly recoverable reality.

## Modes

| Mode | Behavior |
|------|----------|
| `off` | no OCRL selection, no model-facing replacement |
| `shadow` | build and verify OCRL candidates, record proof data, keep original context |
| `auto` | apply only on route-safe full-history paths when all zero-drawdown gates pass |
| `max` | same safety gates as `auto`, but higher capsule budgets and broader old-context coverage |

`max` is not allowed to weaken safety. It may only raise savings by considering
more eligible old context.

Configured surface:

- TOML: `[compression.ocrl]`
- `mode = "shadow"` by default
- `max_capsules = 512`
- `min_net_saved_tokens = 1`
- `max_replacement_tokens = 0` means uncapped except by positive net savings
- Env overrides: `SLIMFERENCE_OCRL_MODE`,
  `SLIMFERENCE_OCRL_MAX_CAPSULES`,
  `SLIMFERENCE_OCRL_MIN_NET_SAVED_TOKENS`,
  `SLIMFERENCE_OCRL_MAX_REPLACEMENT_TOKENS`

The CLI exposes the effective OCRL policy through `slimference layer2 status`.

## Route Reality

Codex WSS currently uses server-side response state and does not resend the
whole old conversation on every turn. OCRL therefore cannot claim direct
model-facing savings on that route today. On Codex WSS, OCRL is shadow/proof
infrastructure unless a future protocol surface actually resends replaceable
old context and replay proof shows no product drawdown.

Full-history HTTP-style routes are the eligible product target because old
messages are present in the request body and can be replaced before upstream
delivery.

## Exact Message Apply Primitive

`internal/contextledger/message_apply.go` provides the first model-facing OCRL
apply primitive for full-history routes. It is deliberately stricter than the
pure renderer:

- callers must provide explicit `(message_index, block_index, capsule)` targets
- each target capsule must have exactly one archive id
- the archive payload must be byte-equal to the current target block text
- duplicate or out-of-range targets full-pass
- selected targets are rechecked through the normal session, active-path,
  recent-turn, quality-pressure, archive, route, and token gates
- final net savings count only selected targets, not verbatim or rejected
  candidates
- single-block covered messages keep a compact `covered_by` marker; multi-block
  covered blocks are deleted from their message
- replacement and marker token overhead are both included before mutation is
  accepted

This primitive does not infer context mapping from rendered text. If a future
route cannot prove exact old-message positions and exact archive equality, it
must not call the apply path.

For full-history routes that already have the old message blocks locally,
`ApplyOCRLToMessagesByArchiveMatch` can derive targets without guessing. It
loads each capsule's single archive payload and accepts a target only when that
payload is byte-equal to exactly one current message block. Ambiguous matches,
missing archives, archive read errors, unmatched payloads, and duplicate target
positions are omitted and counted in the derivation report.

## Capsule Rendering

The rendered OCRL block is deterministic and machine-readable:

- stable header
- one line per selected capsule
- stable kind/session/turn/source fields
- sorted archive ids
- sorted fact keys
- sorted hash keys

It contains no raw omitted content. Raw content is recovered only through the
archive expansion path.

The offline A/B harness understands this rendered form. Archive ids in OCRL
`archives=[...]` lists are replayed with the archive resolver, and old blocks
deleted under a covering OCRL block are treated as recoverable only when a listed
archive expands to the exact original block text. Missing or mismatched
expansion remains a lost-comprehension failure.

## Savings Accounting

Net savings are:

`original_old_context_tokens - rendered_ocrl_tokens - recovery_overhead_tokens`

OCRL applies only when net savings are positive and meet the configured minimum
threshold. Missing or unavailable token accounting means full-pass.

For shadow/proof telemetry, OCRL can count original tokens from the selected
archive payloads when the caller does not yet have an exact old-context slice.
Those values are reported as would-save telemetry only. They never contribute to
product `net_tokens` until a route actually applies OCRL.

## Runtime Shadow Proof

Codex WSS Phase-F records content-free OCRL shadow telemetry in debug request
summaries. The summary includes mode, route, reason, selected/verbatim/rejected
capsule counts, archive-expansion count, replacement tokens, original archive
tokens, and would-save tokens. It intentionally omits rendered OCRL text and
capsule facts.

Shadow archive verification uses a read-only content-archive peek path, so proof
telemetry does not increment real recovery/expansion counters.

The runtime shadow path uses the same configured OCRL policy as future eligible
routes. `off` emits no would-save claim, `shadow` computes proof only, and
`auto`/`max` still stay route-blocked on Codex WSS.

## Zero Drawdown Gates

OCRL full-passes on:

- missing session policy
- wrong session
- missing provenance
- active turn
- recent turn
- active path
- quality pressure
- high-risk failure
- missing facts
- missing archives
- archive expansion error
- invalid or duplicate message target
- target archive payload mismatch
- ambiguous archive-to-message target derivation
- unknown capsule kind
- capsule budget exhaustion
- non-positive net savings
- non-model-facing route

These are product-quality gates, not development gates. Development effort,
benchmarks, captures, and proof work are not drawdowns.

## Verification Requirements

The engine requires:

- pure deterministic unit tests for every gate
- golden-style deterministic rendering assertions
- archive expansion tests that prove copied exact bytes
- A/B harness tests that prove OCRL archive lists recover replaced and deleted
  old blocks, and fail on mismatched archive payloads
- benchmark coverage for large capsule sets
- route tests proving Codex WSS stays shadow-only
- full-history message-apply tests proving positive-saving application,
  archive-mismatch full-pass, route/shadow gates, marker overhead accounting,
  duplicate-target rejection, and selected-target-only token accounting
- archive-match target-derivation tests proving exact single-match apply and
  fail-closed behavior for ambiguous, unmatched, missing, errored, and duplicate
  target candidates
- benchmark-corpus OCRL validator coverage: `ocrl_full_history` requires
  applied full-history evidence, candidate capsules, archive expansions,
  positive OCRL savings, and no shadow-only rows
- max-out promotion coverage: `benchmark-corpus --maxx-check` requires a real,
  non-synthetic `ocrl_full_history` workload and fails if it lacks applied
  OCRL, full-history route rows, archive expansions, positive OCRL saved tokens,
  or if any OCRL row is shadow-only
- docs and TODO state updated before a task can be closed

Live Codex App/Desktop captures are a promotion gate for route claims, not a
reason to weaken offline safety.

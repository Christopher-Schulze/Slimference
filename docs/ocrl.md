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

## Route Reality

Codex WSS currently uses server-side response state and does not resend the
whole old conversation on every turn. OCRL therefore cannot claim direct
model-facing savings on that route today. On Codex WSS, OCRL is shadow/proof
infrastructure unless a future protocol surface actually resends replaceable
old context and replay proof shows no product drawdown.

Full-history HTTP-style routes are the eligible product target because old
messages are present in the request body and can be replaced before upstream
delivery.

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

## Savings Accounting

Net savings are:

`original_old_context_tokens - rendered_ocrl_tokens - recovery_overhead_tokens`

OCRL applies only when net savings are positive and meet the configured minimum
threshold. Missing or unavailable token accounting means full-pass.

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
- benchmark coverage for large capsule sets
- route tests proving Codex WSS stays shadow-only
- full-history route tests proving positive-saving application
- docs and TODO state updated before a task can be closed

Live Codex App/Desktop captures are a promotion gate for route claims, not a
reason to weaken offline safety.

# OCRL - Old Context Recovery Ledger

OCRL is no longer a product context-replacement layer. The implementation is
kept as deterministic shadow/proof and recovery infrastructure only. It can
build archive-backed capsules and would-save telemetry, but the product runtime
must keep original model-facing context unchanged.

## Product Contract

OCRL is not allowed to become model-facing in product runtime. It may only:

- build deterministic capsules from already observed context
- verify archive recoverability
- report content-free shadow would-save telemetry
- support offline A/B and recovery tests
- feed safer superseded/obsolete/repeated reducers that already have their own
  exact full-pass or recovery contracts

The model must never be asked to trust a paraphrase. Capsules are indexes into
already observed, exactly recoverable reality, not a replacement for visible
model memory.

## Modes

| Mode | Behavior |
|------|----------|
| `off` | no OCRL selection, no model-facing replacement |
| `shadow` | build and verify OCRL candidates, record proof data, keep original context |
| `auto` | product runtime still shadow-only; kept as an operator policy input for proof/reporting compatibility |
| `max` | product runtime still shadow-only; may consider broader candidates for would-save telemetry only |

`auto` and `max` do not authorize model-facing OCRL replacement. They only
change how much shadow evidence is collected.

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
infrastructure.

Full-history HTTP-style routes resend old messages and were the only route
where OCRL could have saved directly. That product target is now retired because
Slimference cannot prove that a real model will never need a hidden old detail.
`internal/proxy/ocrl_full_history.go` still archives old inactive
non-user/non-system blocks, builds deterministic capsules, verifies byte-equal
archive payloads, and records would-save telemetry, but it always keeps the
upstream request unchanged.

The `ocrl_full_history` live-corpus category is now a `full_history_http`
shadow-proof category. It must prove route evidence, candidate capsules, archive
expansions, and positive would-save telemetry while also proving zero applied
OCRL rows.

Current product status: OCRL is not a finished Layer 2 product savings lever and
must not be counted as product token savings. The reusable parts are capsule
builders, archive verification, exact message-apply tests, and would-save
telemetry for future safe reducers.

## Exact Message Apply Primitive

`internal/contextledger/message_apply.go` provides an exact OCRL apply primitive
for tests and offline proof. It is deliberately stricter than the pure renderer:

- callers must provide explicit `(message_index, block_index, capsule)` targets
- target selection runs before archive verification, so active/recent/risky
  targets stay original and cannot poison safe old-context replacements
- each selected target capsule must have exactly one archive id
- the selected target archive payload must be byte-equal to the current target
  block text
- duplicate or out-of-range selected targets full-pass
- selected targets are rechecked through the normal session, active-path,
  recent-turn, quality-pressure, archive, route, and token gates
- final net savings count only selected targets, not verbatim or rejected
  candidates
- single-block covered messages keep a compact `covered_by` marker; multi-block
  covered blocks are deleted from their message
- replacement and marker token overhead are both included before mutation is
  accepted
- the byte-equal selected-target archive proof is reused by the apply builder,
  so explicit apply does not load the same archive twice
- the archive-match convenience path tracks derivation proofs only internally,
  so public target derivation stays allocation-light

This primitive does not infer context mapping from rendered text. If an offline
test cannot prove exact old-message positions and exact archive equality for a
selected target, it must not replace that target. Explicit selected targets
normalize a single archive id with the same trim/sort rule as the derivation,
rendering, and archive verification paths; multiple archive ids for one
selected target still fail closed.

For full-history routes that already have the old message blocks locally,
`ApplyOCRLToMessagesByArchiveMatch` can derive targets without guessing. It
loads each capsule's single archive payload and accepts a target only when that
payload is byte-equal to exactly one current message block. Ambiguous matches,
missing archives, archive read errors, unmatched payloads, and duplicate target
positions are omitted and counted in the derivation report. Target bookkeeping
uses compact numeric keys instead of formatted strings, and explicit archive
payload checks compare bytes to current message text without allocating a
converted payload string.

The HTTP full-history shadow hook archives the post-Layer-1 block text, not an
earlier raw pre-compression version. That makes proof byte-equal to the exact
text the model still receives. It caps candidate creation at the configured
capsule budget, skips user/system roles, skips tiny blocks, and full-passes
under re-read or quality pressure.

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

The proxy-level Full-History HTTP test now drives the real OCRL shadow path and
checks that before/after model-facing messages remain byte-equal. Archive data
is still produced by the runtime path, so the test proves recoverability
evidence exists without hiding any context from the model.

## Savings Accounting

Net savings are:

`original_old_context_tokens - rendered_ocrl_tokens - recovery_overhead_tokens`

OCRL product runtime never applies. Net savings are reported only as shadow
would-save telemetry. Missing or unavailable token accounting means no
would-save claim.

For shadow/proof telemetry, OCRL can count original tokens from the selected
archive payloads when the caller does not yet have an exact old-context slice.
Those values are reported as would-save telemetry only. They never contribute to
product `net_tokens`.

## Runtime Shadow Proof

Codex WSS Phase-F records content-free OCRL shadow telemetry in debug request
summaries. The summary includes mode, route, reason, selected/verbatim/rejected
capsule counts, archive-expansion count, replacement tokens, original archive
tokens, and would-save tokens. It intentionally omits rendered OCRL text and
capsule facts.

Shadow archive verification uses a read-only content-archive peek path, so proof
telemetry does not increment real recovery/expansion counters.

The runtime shadow path uses the configured OCRL policy only to decide how much
proof data to collect. `off` emits no would-save claim; `shadow`, `auto`, and
`max` compute proof only.

Full-history HTTP requests attach OCRL shadow telemetry to the normal debug
request summary when candidates exist: `ocrl_route=full_history_http`,
`ocrl_reason=shadow_only`, selected/verbatim/rejected counts, archive
expansions, original tokens, replacement tokens, and would-save tokens. Rows
must stay `telemetry_only=true` and `ocrl_shadow_only=true`.

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
- proxy runtime tests proving Full-History HTTP OCRL never mutates the upstream
  request and records `full_history_http/shadow_only` telemetry
- proxy A/B recovery tests proving the real Full-History HTTP shadow path keeps
  old blocks visible with `lost=0`
- benchmark-corpus OCRL validator coverage: `ocrl_full_history` requires
  full-history route evidence, candidate capsules, archive expansions, positive
  OCRL would-save telemetry, zero applied rows, and at least one shadow-only row
- max-out promotion coverage: `benchmark-corpus --maxx-check` requires a real,
  non-synthetic `ocrl_full_history` workload captured on a full-history HTTP
  route and fails if it lacks shadow OCRL evidence or has any applied OCRL row
- docs and TODO state updated before a task can be closed

Live Codex App/Desktop captures are route evidence, not a reason to weaken
offline safety or revive model-facing OCRL replacement.

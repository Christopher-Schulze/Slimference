# TASK 254: Codex server-state mirror shadow infrastructure

Status: [x] CLOSED AS SHADOW/POLICY INFRA - no generalized model-facing mutation
Priority: P2 - retained only where it improves safe cache-hit decisions
Scope: Codex-only WSS Phase-F. Track exact content forwarded upstream so the policy can
measure safe cache-hit opportunities. Do not replace arbitrary future frames with
general novelty references.

## Why

Today compression is reactive per frame, and read-delta is a hand-built special case
of a broader observation: the Responses API keeps conversation state server-side via
`previous_response_id`. The safe part is tracking what Slimference actually forwarded
upstream and using that to measure or improve exact cache hits. The unsafe part is a
general differential transport that rewrites arbitrary future frames into references.
That would create a new model-facing reference language and can degrade attention or
comprehension even when the referenced bytes technically exist earlier in the session.

Final decision: keep the mirror only as shadow telemetry and policy/cache-hit
infrastructure. Do not build a generalized model-facing mutation layer.

## Acceptance

- A server-state mirror module tracks exact forwarded text hashes per WSS session.
- The mirror predicts referenceable blocks as shadow telemetry only and never changes
  a frame.
- Existing safe reducers remain the mutation owners: read-delta, ranged read-delta,
  exact repeated output, search delta/grouping, build/test/git/lint filters, and
  policy-gated WSS chunk dedup.
- No generalized differential-transport pass is built.
- Coverage gate green; doctrine clean.

## Sub-Tasks

- [x] Implement shadow mirror tracking from forwarded bytes, content-free identity only.
- [x] Prove no-false-elision for the shadow predictor: exact same-session hash only;
      eviction can only under-report.
- [x] Wire WSS Phase-F shadow observation without mutating frames.
- [x] Close generalized differential transport as not product-default safe.
- [x] Keep existing safe reducers independent; no read-delta migration required.

## Target Metrics

- Shadow-only: potential savings can be reported, but no billable-input savings are
  claimed from the mirror itself.
- Zero false-elision by construction: no frame mutation exists in this task.
- Zero hard failures: mirror ambiguity or missing session id reports no opportunity and
  full-passes the original pipeline.
- Useful only if it feeds safe cache-hit work; otherwise it remains a cheap observer.

## Required Architecture

- Mirror input is only the exact bytes Slimference forwarded upstream. The mirror must
  never infer server state from local files, client intent, or unforwarded archive
  entries.
- Mirror entries store content hashes only in the current shadow implementation. Raw
  payload retention is not required for this task.
- Model-facing output must remain owned by the proven reducers. The mirror may inform
  route/workload telemetry and future exact cache-hit keys, but not emit references.
- Semantic summaries do not belong in the mirror path.

## Execution Gates

- No mutation gate remains in this task. If future evidence shows a narrow exact
  cache-hit reducer worth building, open a new task for that specific reducer rather
  than reopening generalized differential transport.
- The mirror can continue to run as telemetry because it does not alter model-facing
  context.

## Notes

- The old ~15-40% estimate belonged to generalized mutation and is no longer a product
  claim. Shadow observations may reveal future exact-cache opportunities, but those
  must be built as specific default-safe reducers.
- Doctrine: content-free, fail-open, scoped, and no model-facing reconstruction layer.

## Closure decision (2026-05-31)

The mirror is retained because exact forwarded-state knowledge is useful for telemetry
and future cache-hit improvements. The generalized differential-transport ambition is
closed because it would turn Slimference into a context-rewriter with a new reference
language. That is outside the no-drawdown product bar.

## Deviations

(none)

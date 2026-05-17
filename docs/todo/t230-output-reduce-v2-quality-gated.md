# TASK 230: Output-reduce v2 quality-gated max savings

Status: PLANNED
Priority: P1 after T209/T224 live proof establishes transport baseline
Scope: Response/output side savings for Codex; provider-neutral where safe

## Why

Output-reduce is a major economic lever. Input compression reduces what the
model reads; output-reduce reduces what the model generates or what the client
has to receive and feed back into future context. The risk is quality: output
mutation can hide useful information if it is too aggressive.

This task expands output-reduce only where quality can be protected by
deterministic guards, schema awareness, A/B metrics, or exact fallback.

## Target State

Output-reduce v2 is a set of independently gated reducers:

1. Semantic repetition killer.
2. Tool-result echo suppression.
3. Diff/patch-aware answer budget.
4. JSON/code-block canonicalizer.
5. Streaming early-cut pattern engine.
6. Exact-reply and high-risk prompt bypass.
7. Quality A/B rollback.

Every reducer must be token-decreasing, observable, reversible in debug logs,
and disabled automatically if quality metrics degrade.

## Acceptance

- Reducers are individually configurable under `[compression.output_reduce]`.
- Each reducer has counters: attempts, applied, bytes/tokens saved, bypassed,
  quality rollback, and unsafe skip.
- Exact-answer prompts, legal/security-sensitive prompts, and user requests for
  exhaustive output bypass aggressive reducers.
- Tool-result echo suppression only replaces content already present in prior
  tool output or request context.
- Diff/patch-aware budgeting preserves file paths, line numbers, hunks, test
  failures, and error messages.
- Streaming early-cut closes upstream only after a high-confidence trailing
  commentary pattern, not during active answer content.
- Quality A/B can compare control/treatment and auto-disable a reducer on
  failure-rate regression.
- Tests include broken-code counterexamples where a reducer must not fire.

## Sub-Tasks

- [ ] Add reducer registry and per-reducer config/state.
- [ ] Implement semantic repetition detector:
  deterministic phrase/window similarity first; no embedding dependency.
- [ ] Implement tool-result echo suppression:
  build per-request index of prior tool outputs, rewrite verbatim/near-verbatim
  repeats into compact stable references.
- [ ] Implement diff/patch-aware output policy:
  preserve technical payloads, reduce surrounding prose, never remove actionable
  failure lines.
- [ ] Implement JSON/code-block canonicalizer:
  minify fenced JSON, normalize excessive blank lines, preserve syntax.
- [ ] Expand streaming early-cut:
  pattern groups for post-answer disclaimers, recap loops, unnecessary next-step
  boilerplate, and repeated "let me know" endings.
- [ ] Add exact-reply and high-risk bypass classifier.
- [ ] Wire all reducers into Codex HTTP and WSS response paths with the same
  fail-open behavior.
- [ ] Add `/admin/state.output_reduce_v2` and debug flight event fields.
- [ ] Add live-corpus gate with synthetic fixtures first, then real scrubbed
  corpus after T209.

## Benefits

Expected incremental savings over the current stack:

- 5-25% output-token savings in verbose agent sessions.
- Larger future-context savings because shorter outputs are fed back into later
  turns.
- Best ROI where models echo tool results, produce long summaries, or add
  trailing commentary.

Expected savings over direct baseline:

- Tiny exact replies: 0%.
- Normal coding sessions: 10-35% from output side alone when reducers trigger.
- Combined with input/WSS/cache layers: potentially 30-65% on favorable
  tool-heavy sessions, but only live corpus can certify that.

## Drawdowns and Guards

- Quality loss is the only serious risk. Guard: conservative defaults,
  per-reducer gates, exact-reply bypass, debug visibility, and auto rollback.
- Semantic reducers can overmatch. Guard: start deterministic/verbatim-near
  only; no embedding dependency in v1.
- Do not hide code/test details. Guard: diff/error preservation tests.


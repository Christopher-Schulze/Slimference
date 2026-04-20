# T52 - Prompt-Cache Hit-Rate Verifikation gegen Anthropic-API

Status: todo
Priority: P1
Scope: `scripts/benchmarks/prompt-cache-verify.ts`, `internal/analytics/`, `docs/benchmarks.md`
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

Slimference injects `cache_control: {type: "ephemeral"}` breakpoints and
relies on Anthropic's prompt-cache to save tokens on repeat requests.
T23 already measures `usage.cache_creation_input_tokens` and
`usage.cache_read_input_tokens` per response - but no test verifies
**against the real Anthropic API** that:

1. Breakpoint placement actually yields a cache hit on the next request.
2. Byte-identical history up to breakpoint produces `cache_read > 0`.
3. The Slimference-specific byte adjustments (structure extract, delta,
   etc.) do not break cache-stability.

Today's unit tests only verify **injection**. If Anthropic changes cache
semantics, or our L1 pipeline introduces non-determinism upstream of the
breakpoint, hit-rate silently drops to 0 - invisible regression.

## Current State

- T23 captures usage fields in analytics snapshot.
- T45 (pending) adds multi-breakpoint injection.
- No end-to-end verification test exists.
- `docs/benchmarks.md` reports aggregate savings but not hit-rate
  specifically.

## Target State

- `scripts/benchmarks/prompt-cache-verify.ts` sends a controlled
  request twice, through Slimference, to the real Anthropic API using a
  test API key (not committed, sourced from env `ANTHROPIC_API_KEY`).
- Verifies:
  - Request 1: `cache_read_input_tokens == 0`,
    `cache_creation_input_tokens > 0`.
  - Request 2 (identical up to breakpoint, one new user turn):
    `cache_read_input_tokens > 0`, `cache_read_input_tokens / input_tokens
    >= 0.5`.
  - Hit-rate per Slimference breakpoint reported.
- Script is **opt-in**; CI skips unless `ANTHROPIC_API_KEY` is set and
  `SLIM_VERIFY_PROMPT_CACHE=1`.
- Results appended to `docs/benchmarks.md` with date + SHA.
- TUI Stats shows a live moving-average hit-rate over last 100 requests.

## Design

### Test scenarios

1. **System-prompt cache**: long system, 3 user turns, tiny reply. Second
   run swaps the last user turn; expect system cache hit.
2. **Tools-array cache**: identical tools, different user turn. Expect
   tools cache hit.
3. **History cache**: identical first 5 turns, new 6th. Expect early
   breakpoint hit.
4. **Anti-test**: intentionally vary the system prompt by one byte;
   expect `cache_read == 0` (cache miss). Confirms no over-claiming.

### Script structure (Bun/TS)

```ts
// scripts/benchmarks/prompt-cache-verify.ts
import { fetch } from "bun";

const scenarios = [systemCacheScenario, toolsCacheScenario, ...];

for (const s of scenarios) {
  const r1 = await runThroughSlimference(s.request1);
  const r2 = await runThroughSlimference(s.request2);

  assert(r1.usage.cache_creation_input_tokens > 0);
  assert(r1.usage.cache_read_input_tokens === 0);
  assert(r2.usage.cache_read_input_tokens > 0);
  const hitRatio = r2.usage.cache_read_input_tokens /
                   (r2.usage.cache_read_input_tokens +
                    r2.usage.input_tokens);
  report(s.name, hitRatio);
}
```

### Slimference determinism check

Before sending request 2, record the byte-slice up to each breakpoint
via `/admin/last-request-breakpoints`. Compare with request 1 recorded
slices - must be byte-equal up to breakpoint. If not, Slimference is
the culprit, not Anthropic.

### Moving-average in TUI

`internal/analytics/moving_avg.go` with a ring buffer of last 100
responses. Each response adds (cache_read, cache_read + input) pair.
TUI Stats: `Prompt-Cache Hit (rolling-100): 72.4 %`.

### Cost guard

Script caps total tokens sent to a small budget (e.g. 50 k) to keep
per-run cost bounded. Abort if exceeded.

## Implementation Plan

### WP1 - Determinism endpoint
- `/admin/last-request-breakpoints` returns SHA256 of each byte slice
  up to each breakpoint for the most recent 10 requests.

### WP2 - Benchmark script
- Four scenarios above, Bun/TS, opt-in via env.

### WP3 - Moving-average
- Ring buffer in analytics, exposed in snapshot + TUI.

### WP4 - Report format
- Markdown table append to `docs/benchmarks.md` under `## Prompt-Cache
  Verification Runs`.

### WP5 - CI
- Job `prompt-cache-verify` gated on secret `ANTHROPIC_API_KEY`. Skips
  on fork PRs.

---

## Subtasks

- [ ] `/admin/last-request-breakpoints` endpoint with SHA256 hashes.
- [ ] `scripts/benchmarks/prompt-cache-verify.ts` with 4 scenarios.
- [ ] Cost-guard in script.
- [ ] Moving-average in analytics + TUI.
- [ ] Append report to `docs/benchmarks.md`.
- [ ] CI job gated on secret.
- [ ] Docs note in `docs/documentation.md` §8 Analytics.

## Risks

- Anthropic quota burn on CI runs. Mitigation: run only on release
  branches + weekly schedule, not every PR.
- API key leak risk. Mitigation: GitHub Actions secret scope to
  release env only.
- Flaky test due to Anthropic cache eviction timing. Mitigation: allow
  3 retries with backoff before failing.

## Acceptance Criteria

- [ ] Script runs end-to-end against real Anthropic API with `ANTHROPIC_API_KEY`.
- [ ] All 4 scenarios pass with expected hit-rate thresholds.
- [ ] Anti-test confirms hit-rate = 0 on intentional byte change.
- [ ] Determinism endpoint shows byte-equal slices for identical
      requests.
- [ ] TUI renders rolling hit-rate.
- [ ] `docs/benchmarks.md` appendable report section.

## Out of Scope

- OpenAI cache verification (different cache model, separate TASK).
- Automatic PR comments with hit-rate (nice-to-have).

---

## Validation

```
ANTHROPIC_API_KEY=sk-ant-... SLIM_VERIFY_PROMPT_CACHE=1 \
  bun run scripts/benchmarks/prompt-cache-verify.ts

curl -s 127.0.0.1:8990/admin/last-request-breakpoints | jq .
curl -s 127.0.0.1:8990/admin/status | jq .prompt_cache.rolling_hit_rate
```

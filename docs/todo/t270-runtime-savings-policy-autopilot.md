# T270 - Runtime savings policy autopilot

## Why

A policy engine is only worth its complexity if it makes runtime decisions that
humans should not have to make. "Only proven things on" is not enough to justify
a hot-path planner. The engine must be an autopilot: route-aware, recency-aware,
recovery-aware, proof-fed, and able to loosen compression before product
drawdowns show up.

## Current reality check

- A policy/planner exists and has route/mechanism counters.
- It can block closed candidates and gate risky mechanisms.
- The remaining value is dynamic runtime intelligence, not static feature flags.
- Offline hardening done:
  - Codex tool-output policy now has typed demotion inputs for quality spikes,
    archive recovery loops, missing-tool retries, degraded routes, host-budget
    pressure, and negative-savings history
  - any supplied demotion signal forces a full-pass decision for managed Codex
    tool-output reducers
  - mechanism telemetry reports the exact demotion reason, so the product can
    explain why savings loosened without logging content
  - planner Layer 2 `run` now requires the explicit legacy model-facing summary
    replacement gate; otherwise long-context Layer 2 remains shadow-only for the
    deterministic context-ledger path

## Product target

The policy engine controls the default product mode:

- safe mechanisms on automatically
- risky mechanisms only when route/workload/recovery/proof allows them
- automatic loosening on edit, recency, re-read, repair, missing-tool, or
  degraded-route signals
- automatic re-promotion only after cooldown and proof
- no manual experiment modes in normal UX

## Inputs

- route: WSS Phase-F, WSS bridge, HTTP, direct
- workload class: read, ranged read, search, git, test, build, log, tool schema,
  output
- recency: active file, recent edit, deliberate re-read
- recovery: archive available, archive note active, expansion success rate
- quality: re-read canary, repair turns, user re-asks, missing-tool retries
- proof: version tuple certified, corpus class passed, live workday passed
- economics: billable input, output-wire, prompt-cache hit/miss impact
- host cost: p95 latency, RSS, CPU, disk writes

## Technical work packages

1. Define typed policy facts and typed mechanism decisions.
2. Remove any hot-path policy branch that is not backed by real inputs.
3. Add cooldown buckets:
   - session
   - route
   - provider/model
   - workload class
   - mechanism
   - current offline state: typed signal inputs and full-pass demotion reasons
     exist; persistent bucket state for those signals remains live-telemetry
     gated
4. Add promotion rules:
   - proof gate passed
   - no quality spike
   - positive net savings
   - host budget below ceiling
5. Add demotion rules:
   - re-read spike
   - repair/reask spike
   - archive recovery loop
   - missing tool retry
   - degraded WSS tuple
   - host budget exceeded
   - current offline state: archive recovery loop, missing tool retry, degraded
     route, host budget exceeded, quality spike, negative-savings history,
     recent edit, post-collapse re-read, and session integrity budget all have
     deterministic full-pass policy outcomes; classical Layer 2 model-facing
     summary replacement cannot be promoted by `layer2_enabled` alone
6. Keep policy output explainable:
   - decision
   - reason
   - required proof
   - bypass reason
   - next promotion blocker

## Zero product-drawdown gates

- Policy must loosen before continuing with a mechanism that shows quality
  signals.
- Policy must fail open to less compression, never to broken routing.
- Policy cannot enable non-product experimental mechanisms.
- Manual max modes cannot be surfaced as the normal product recommendation.

## Savings targets

- Default mode should approach max safe savings by workload, not by global
  aggressiveness.
- Aggressive mechanisms should show positive net savings only in buckets where
  proof supports them.
- Policy overhead should be negligible compared to frame parsing and upstream
  inference.

## Verification

- Table-driven policy tests for every mechanism and every demotion signal.
- Corpus replay comparing expected vs actual policy decisions.
- Live workday proof with policy counters.
- Negative tests for closed candidates: they remain unreachable.

## Done

The policy engine is justified only when it is the automatic runtime brain that
maximizes savings while actively preventing product drawdowns. If a branch is
static bookkeeping, remove it from the hot path or move it to reporting.

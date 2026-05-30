# TASK 256: Codex savings policy engine

Status: [x] DONE - auto policy centralizes aggressive Codex reducer activation
Priority: P1 - make maximum savings automatic without a flag minefield
Scope: Codex-only WSS/HTTP Layer-0 reducer policy. Centralize which mechanisms
may run, when they must loosen, and which recovery prerequisites are required.

## Why

Default-off features are only useful if the product can safely promote them
without forcing operators to micromanage toggles. T255 proved recoverable
content-defined chunk dedup on real WSS frames, but hard-defaulting it would be
too blunt: Codex Responses API stores the mutated context permanently, deliberate
re-reads carry recency/attention intent, and recovery references must be
understood by the model. The correct target is `auto`: aggressive savings are on
by default only where recovery, recency, and safety signals make them drawdown
neutral.

## Acceptance

- A single policy layer decides Codex reducer permissions instead of scattering
  ad-hoc boolean checks through the reducer.
- Default policy mode is `auto`; `off`, `conservative`, and `max` remain
  operator-visible escape hatches.
- Safe lossless reducers remain enabled under auto.
- T255 chunk dedup becomes auto-eligible on recoverable large tool outputs, but
  automatically loosens for recent edits and post-collapse re-reads.
- Recovery-note injection is automatic only when a recoverable reference is
  actually emitted, not as global prompt noise.
- HTTP does not emit archive/chunk references until recovery-note support is
  implemented and proven for that route.
- Tests prove auto-on, conservative opt-in, recency loosening, config/env
  validation, and WSS wiring.
- Documentation states that "default" means policy-driven auto, not blind hard-on.

## Sub-Tasks

- [x] Add `internal/savingspolicy` as a pure Codex policy package.
- [x] Add `[compression.output_reduce].codex_savings_policy_mode`.
- [x] Wire WSS and HTTP Layer-0 through the policy.
- [x] Make T255 auto-eligible on WSS with automatic recovery-note injection only
      when chunk references are emitted.
- [x] Keep HTTP conservative until route-specific recovery-note support exists.
- [x] Add tests for policy decisions, WSS auto chunk dedup, conservative opt-in,
      and config/env validation.
- [x] Flush docs and operation log.

## Notes

- Default mode is `auto`. This makes proven high-value mechanisms usable without
  manual toggling while keeping the drawdown guardrails centralized.
- `codex_chunk_dedup_enabled` remains as an explicit override for conservative
  policy, but it is no longer the product-level way to make T255 useful.
- Auto policy currently enables:
  - read-delta and exact repeated-output reducers as safe lossless mechanisms;
  - recoverable chunk dedup when archive recovery is available and the block is
    large enough (`codex_chunk_dedup_min_bytes=4096`, deliberately below Codex's
    observed ~8 KiB truncated exec-output envelope);
  - automatic full-pass loosening on recent edits and post-collapse re-reads.
- Semantic reducers remain outside auto until the A/B harness proves
  comprehension preservation.

## Deviations

(none)

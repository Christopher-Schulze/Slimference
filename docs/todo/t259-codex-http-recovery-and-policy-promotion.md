# TASK 259: Codex HTTP recovery and policy promotion

Status: [ ] QUEUED - HTTP route remains conservative until recovery is proven
Priority: P1 - avoid a hidden drawdown channel outside WSS
Scope: Codex HTTP/scoped fallback path. Decide whether HTTP should support recoverable
chunk/archive references or remain permanently conservative.

## Why

T256 intentionally keeps HTTP conservative: it shares safe Layer-0 primitives but does
not emit chunk/archive references because WSS has the proven recovery-note injection
path and HTTP does not. That is correct for safety, but it leaves possible savings on
the table for fallback/legacy/hook surfaces. This task either proves and wires HTTP
recovery or explicitly locks HTTP out of archive-reference mechanisms.

## Acceptance

- HTTP route behavior is explicit: either (a) proven recoverable and policy-promotable,
  or (b) permanently conservative with docs/tests preventing archive refs.
- If promoted, HTTP injects a neutral recovery note exactly once per session/request
  scope when and only when a recoverable reference is emitted.
- If not promoted, tests prove HTTP cannot emit `[context-chunk ...]` or other
  archive refs even when `codex_savings_policy_mode=max` and explicit chunk flags are
  set.
- HTTP policy decisions are visible in admin/workday/audit output.

## Gates

- Fixture gate: HTTP Codex request/reconstruction fixtures prove note injection,
  reinjection, and byte-equal fallback.
- Replay gate: HTTP fallback capture, if available, shows no unexpected model-facing
  losses.
- Safety gate: no global proxy/system settings are used; scoped doctrine remains
  intact.
- Regression gate: full build/vet/test/CI green.

## Sub-Tasks

- [ ] Audit current HTTP Codex fallback usage and whether it still carries real product
      traffic or only legacy/testing traffic.
- [ ] Decide route strategy: implement HTTP recovery-note injection or permanently
      disallow archive refs on HTTP.
- [ ] Add tests that enforce the chosen strategy.
- [ ] Update `docs/documentation.md`, T248, and T256 wording.

## Notes

- WSS remains the standard product path. HTTP promotion is useful only if fallback
  traffic still matters and can be made equally recoverable.
- No savings gain is worth a second, weaker semantics surface.

## Deviations

(none)

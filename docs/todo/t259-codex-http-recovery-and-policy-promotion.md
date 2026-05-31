# TASK 259: Codex HTTP recovery and policy promotion

Status: [x] CLOSED - HTTP stays conservative; archive refs are WSS-only
Priority: P1 - avoid a hidden drawdown channel outside WSS
Scope: Codex HTTP/scoped fallback path. Decide whether HTTP should support recoverable
chunk/archive references or remain permanently conservative.

## Why

T256 intentionally keeps HTTP conservative: it shares safe Layer-0 primitives but does
not emit chunk/archive references because WSS has the proven recovery-note injection
path and HTTP does not. That is the final product decision. HTTP remains a robust
fallback/legacy route, not a second semantics surface for recoverable references.

## Acceptance

- HTTP route behavior is explicit: permanently conservative for archive refs.
- Tests prove HTTP cannot emit `[context-chunk ...]` or other archive refs even when
  `codex_savings_policy_mode=max` and explicit chunk flags are set.
- HTTP policy decisions are visible in admin/workday/audit output.
- WSS remains the standard path for recoverable archive/chunk mechanisms; HTTP remains
  fallback and must keep working byte-safe.

## Gates

- Policy gate: HTTP archive-backed mechanisms stay blocked in central policy.
- Safety gate: no global proxy/system settings are used; scoped doctrine remains
  intact.
- Regression gate: full build/vet/test/CI green.

## Sub-Tasks

- [x] Audit current HTTP route role: fallback/legacy route, not the WSS standard product
      path for recoverable savings.
- [x] Decide route strategy: implement HTTP recovery-note injection or permanently
      disallow archive refs on HTTP.
- [x] Add tests that enforce the chosen strategy.
- [x] Update product documentation and task wording.

## Notes

- WSS remains the standard product path. HTTP remains useful as fallback/legacy, but
  not for archive-backed references.
- No savings gain is worth a second, weaker semantics surface.
- 2026-05-31: chosen route strategy is "conservative lock" for now. T258 policy blocks
  HTTP archive-backed chunk references even in `max` with explicit chunk flags. Tests
  cover `codexHTTPChunkDedupSettings` and `DecideCodexToolOutput` on `CodexRouteHTTP`.
  Promotion is not planned for the default product mode.

## Closure decision (2026-05-31)

HTTP stays as fallback so Slimference remains robust. It does not get archive/chunk
references because that would create a second recovery contract with low fallback-only
savings upside and avoidable model-facing risk. Proven safe HTTP Layer-0 reducers remain
available.

## Deviations

(none)

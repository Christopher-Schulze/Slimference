# T333 Codex HTTP Session Savings Attribution

## Why

Codex CLI traffic can produce HTTP `/backend-api/codex/responses` requests that
carry the real Codex thread id in `client_metadata.x-codex-turn-metadata`.
Slimference already used that metadata on the WSS path, but HTTP-side savings
could still be grouped as `no-session:proxy`. That made per-thread savings
reports incomplete even though the reductions were measured.

## Acceptance

- Codex HTTP requests with turn metadata use the same Codex thread namespace as
  WSS requests.
- `slimference savings` enriches Codex HTTP/WSS thread rows from the local Codex
  thread DB when metadata is available.
- Existing no-session fallback remains for traffic that genuinely has no thread
  identity.
- Focused tests cover HTTP turn metadata extraction and savings enrichment.

## Sub-Tasks

- [x] Extract Codex HTTP thread ids from `client_metadata.x-codex-turn-metadata`.
- [x] Keep Codex HTTP and WSS traffic aggregatable by real thread id.
- [x] Extend thread-id normalization and savings enrichment coverage.
- [x] Add regression tests for the previously unattributed path.

## Notes

- Historical decision-log rows that were already written as `empty` cannot be
  rewritten without mutating evidence. They can still be measured by timestamp
  window, but future reports attribute them directly.
- The `codex-wss:` prefix remains the compatibility namespace for Codex thread
  sessions; transport is still visible via route/source fields.

## Deviations

- None.

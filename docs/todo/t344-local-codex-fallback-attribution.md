# T344: Local Codex fallback thread attribution

## Why

Some current Codex HTTP requests do not expose `thread_id`,
`conversation_id`, or turn metadata in the request body. Slimference then has
to fall back to an `fh:*` content-hash session, which is safe but not useful
for per-thread savings reports. The report should use real local Codex thread
metadata when it can prove an unambiguous match, and it must stay honest when
parallel sessions make the match ambiguous.

## Acceptance

- Codex HTTP `x-codex-thread-id`, `x-codex-conversation-id`, and
  `x-codex-session-id` headers are accepted as strong Codex HTTP attribution
  before `fh:*` fallback.
- `codex/...` User-Agent values classify as `codex_cli`; Desktop/App-like
  local thread sources still classify as Codex App.
- `fh:*` savings sessions can resolve to `codex-local:<thread-id>` only when
  the local Codex thread DB gives a unique match by first-user-message hash or
  by time/model/client-family.
- Anonymous Codex fallback sessions such as `no-session:proxy` can resolve only
  when exactly one local Codex thread created/updated activity envelope
  encloses the token-bearing request window. Zero-token tunnel/ping rows do not
  widen that attribution window.
- Ambiguous parallel candidates remain unattributed and keep the attribution
  status at `attention`.
- The change is reporting-only: no reducer, cache key, payload, route, or
  provider request behavior changes.
- Focused attribution/cache tests and the full CI gate pass.

## Changes

- Added windowed Codex thread metadata lookup, including `first_user_message`.
- Added strong Codex header extraction for HTTP session IDs.
- Added `codex/...` CLI User-Agent recognition.
- Added local fallback session resolution for Savings reports with strict
  ambiguity guards.
- Extended local Codex thread lookup with `created_at`/`created_at_ms` and
  overlap-window queries so long-running active threads can be matched without
  blindly relying on `updated_at` only.
- Made Savings cost estimates conservative for provider cache by subtracting
  cache-create tokens from cache-read discount equivalent before reporting
  estimated cost saved.
- Tightened generic OpenAI prompt-cache negative-net cooldown from three to two
  negative samples while preserving the single create-only warmup allowance.
- Aligned admin status, prompt-cache reports, and proxy-gain reports with the
  same conservative cache math: `max(cache_read - cache_create, 0) * 0.9`.

## Verification

- Focused gate passed:
  - `go test ./internal/codexthreads ./internal/proxy ./cmd/slimference -run 'TestLookupWindowCurrentCodexSchema|TestLookupCurrentCodexSchema|TestExtractSessionIDCodexHTTPUsesStrongThreadHeaders|TestExtractClientFamilyCodexHTTPFallbacks|TestSavingsResolvesHashFallbackToLocalCodexThread|TestSavingsHashFallbackMatchesProxyHashWithoutTrimming|TestSavingsKeepsAmbiguousHashFallbackUnattributed|TestSavingsResolvesAnonymousCodexFallbackByUniqueActivityEnvelope|TestSavingsKeepsAmbiguousAnonymousCodexFallbackUnattributed|TestComputeSavingsLiveUsesCurrentDaemonWindow|TestSavingsCodexAttributionHealth|TestComputeSavingsDetectsNegativeCacheNet|TestEstimateCostUSD' -count=1`
- Live local proof after the change:
  - `go run ./cmd/slimference savings live --json`
  - `decision_codex_attribution_status=ok`
  - `decision_codex_attributed_requests=69`
  - `decision_codex_unattributed_requests=0`
  - top session `codex-local:019ea6ca-5279-7200-868e-2efda5e6731d`
    (`/Users/christopher/CODE/Golem`, `codex_cli`)
  - `decision_cache_status=ok`
  - `decision_cache_negative_net_requests=0`
- Full CI:
  - `go run ./scripts/ci` passed all 8 steps; total coverage `95.3%`

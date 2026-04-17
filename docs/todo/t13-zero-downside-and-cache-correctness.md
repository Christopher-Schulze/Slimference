# T13 - Zero-Downside and Cache Correctness

Status: closed
Priority: critical
Scope: proxy hot path, request reconstruction, Layer 3 request identity, invalidation semantics

---

## Problem

Two central correctness promises are not yet fully secured:

1. the proxy claims zero downside
2. the response cache claims identical-request replay safety

The current implementation is too close, but not exact, on both.

---

## Desired End State

1. A negative-savings request can never be forwarded in compressed form.
2. Cache hits can only occur for truly equivalent effective requests.
3. File-change invalidation cannot falsely imply correctness that the key model
   does not support.

---

## Work Packages

### WP1 - Make zero-downside mechanical

- Move the negative-savings guard ahead of request reconstruction or rebuild the
  request body after the revert.
- Add a regression test that proves the upstream receives the original request
  body whenever compression expands or breaks even.
- Ensure metrics, analytics, and cache bookkeeping agree with the forwarded body.

### WP2 - Canonical effective-request fingerprint

Build a normalized cache key from the effective forwarded request, including:

- provider
- model
- normalized message roles and full content blocks
- system content where applicable
- tool call and tool result metadata
- request parameters that affect generation behavior

Do not rely on text-only content extraction.

### WP3 - Safer invalidation model

Current response-body substring invalidation is too heuristic. Replace it with
one of the following:

- a tracked dependency index from request/response metadata
- conservative whole-cache invalidation on watched file changes until a proper
  dependency model exists

The chosen model must prefer correctness over cache retention.

### WP4 - Layer 3 proof tests

- cache miss for different non-text blocks
- cache miss for different generation parameters
- cache miss when provider-specific request shape differs
- invalidation tests aligned with the new policy

---

## Architecture Notes

- Correctness beats hit rate.
- If a fully safe cache identity is not yet available, disable or constrain the
  cache rather than serve potentially wrong content.
- The cache key should reflect the forwarded request shape, not a simplified
  pre-forward sketch.

---

## Subtasks

- [x] Fix the hot-path zero-downside ordering bug.
- [x] Add a regression test that asserts the forwarded body after negative savings.
- [x] Design and implement a canonical effective-request normalizer.
- [x] Replace the current text-only cache key with the canonical fingerprint.
- [x] Replace substring invalidation with a correctness-first policy.
- [x] Add request-identity regression tests for text, tool, image, and param deltas.

Closure note:

- forwarded request bodies are rebuilt only after the kept message slice is final
- Layer 3 identity is now provider + canonical full request body
- cache invalidation is driven by extracted dependency paths instead of response
  substring guesses

---

## Acceptance Criteria

- No request with negative savings is ever forwarded as compressed.
- Cache replay is limited to truly equivalent forwarded requests.
- File-change invalidation is conservative enough that stale-or-wrong replay is
  not a credible production risk.

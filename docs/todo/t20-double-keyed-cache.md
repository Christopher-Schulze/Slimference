# T20 - Double-Keyed Response Cache (Pre-Compress Lookup)

Status: open
Priority: high
Scope: internal/caching, internal/proxy/handler.go

---

## Problem

The Layer 3 response cache currently computes its key from the **post-compression
body** (`handler.go:168-170`). Consequence: even on a cache hit, every request
pays the full cost of Layer 1 and, if triggered, the synchronous part of
Layer 2. On a modern M1 that cost is small in absolute ms but not free:

- L1 walks every message, runs ANSI strip, JSON compact, comment strip,
  MinHash dedup, structure extract, delta, success-shortcircuit,
  image-replace, repeated-collapse, graph-pruning, prefilter-tag, and the
  tool classifier/compressor.
- For a 30-50 message conversation that is measurable and entirely avoidable
  when the request is identical to a past one.

**Note on M1 CPU impact:** the raw proxy CPU usage is already effectively
imperceptible on an M1 under normal load (L1 runs in a few ms per request,
L2 is fully async). The goal here is not to "fix a lag", it is to avoid
doing any work at all when we already have the answer cached.

---

## Desired End State

Two-stage cache lookup in `handleCompressibleRequest`:

1. **Stage A (pre-compress):** hash the canonical normalized original body
   + provider + headers. If hit -> serve immediately, skip L1/L2/L3-store.
2. **Stage B (post-compress):** keep the existing hash on the compressed
   body as the **authoritative** cache identity. Populated on miss + success.

Stage A is a pointer into Stage B. When a Stage A hit resolves to an expired
or invalidated Stage B entry, fall through to the normal pipeline.

---

## Work Packages

### WP1 - Canonical original-body hash

- Extend `caching.ComputeRequestKeyWithHeaders` or add a sibling
  `ComputeOriginalKeyWithHeaders` that normalizes the *pre-compression*
  request body the same way.
- Ensure the canonicalization does not leak compression-order sensitivity.

### WP2 - Pointer table

- Add `originalKey -> compressedKey` map inside `responseCache`.
- Both maps share the same LRU/TTL semantics and the same invalidation
  signals (dependency watches, TTL).
- Writes: on Stage B store, also insert the Stage A pointer.
- Reads: on Stage A lookup, resolve pointer, load Stage B entry, verify
  freshness.

### WP3 - Handler integration

- Before L1/L2: compute `origKey`, look up via Stage A.
- On hit: short-circuit exactly like the current post-compress hit path
  (emit analytics, record debug summary, write response).
- On miss: continue with existing pipeline, store both keys on success.

### WP4 - Regression and proof tests

- Identical request twice: second one must show `layers_applied=[3]` only,
  L1 breakdown empty, and measurably lower latency.
- Dependency file change between calls: second one must miss cleanly.
- Header variation that affects generation (temperature, tools) must miss.
- Ensure coverage stays at 100 %.

---

## Subtasks

- [ ] Design Stage A key normalization (document in code doc-comment).
- [ ] Add pointer map + promote/evict logic.
- [ ] Wire Stage A lookup into `handleCompressibleRequest`.
- [ ] Add metric `cache.stage_a_hit` / `cache.stage_b_hit` / `cache.miss`.
- [ ] Add tests for: same-input hit, different-input miss, invalidated hit.
- [ ] Update `docs/documentation.md` (Layer 3 section) to describe the
      two-stage model.

## Acceptance Criteria

- On repeated-identical requests, Layer 1 and Layer 2 do not run.
- Cache hit latency drops by 30 %+ on conversations with >= 20 messages.
- Correctness: no stale or wrong response ever served.
- 100 % coverage preserved, race-clean.

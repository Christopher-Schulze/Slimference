# T266 - Chunk dedup always-auto hardening

## Why

Content-defined chunk dedup can save heavily on similar outputs: edited files,
similar files, repeated logs, and overlapping command results. It is also more
invasive than unchanged-read collapse because the server state stores references
and new bytes, not a full fresh output. This task defines the work required to
make chunk dedup automatic without product drawdowns.

## Current reality check

- FastCDC chunking and a bounded session chunk store exist.
- WSS route attribution and replay proof exist.
- Chunk references are recoverable where archive support is available.
- Chunk encode now has local self-verification: a changed reference stream must
  decode back to the exact original bytes before it can be returned. Archive URI
  collisions or orphan references fail open to the original output.
- Chunk refs are now suppressed for patch/diff/edit-style command outputs such
  as `apply_patch`, `patch`, `git diff`, `git show`, `git apply`, `git am`, and
  `git format-patch`. These outputs can still use deterministic filters and
  exact repeated-output reducers, but content-defined references are not allowed
  to split fresh patch reasoning context.
- It is not enough to prove one matching workload. Default-auto needs broad
  proof and runtime self-protection.

## Product target

Chunk dedup may be always-auto only for routes/workloads where:

- chunks were previously full-seen by the model or are exactly recoverable
- archive recovery note/contract is available
- recency and re-read canaries are active
- integrity budget prevents cumulative reference erosion
- live corpus shows no workflow or comprehension regression

## Technical work packages

1. Make eligibility explicit:
   - WSS only unless HTTP recovery is proven
   - no first-observation chunk references without full source seeding
   - [x] no chunk refs for active patch/diff/edit outputs
   - no chunk refs under recent edit uncertainty
2. Add integrity budget:
   - per-session ratio of referenced bytes to total tool-output bytes
   - per-output maximum reference density
   - automatic full-pass when budget is exceeded
3. Strengthen recovery:
   - [x] archive id for every referenced chunk group
   - [x] exact local decode self-check before returning a changed stream
   - [x] fail open on archive URI collision or orphan references
   - [x] route refuses chunk refs if archive write fails
   - [x] add content-free chunk reference density reporting to WSS audit/admin state
   - [x] per-output reference-density cap defaults to 90% and full-passes when
     the candidate would replace too much fresh model-facing output
   - [x] per-session accepted-reference budget defaults to 70% and full-passes
     once cumulative chunk references would dominate the session
4. Add recency policy:
   - deliberate re-read of same file may full-pass or provide salient summary
     plus refs, never bare refs if canary says the model is struggling
   - post-collapse re-read restores full context
5. Add live proof matrix:
   - edited file
   - similar files
   - repeated logs
   - repeated test output
   - large search output
   - Desktop reconnect
   - CLI long session

## Zero product-drawdown gates

- No unresolved reference can enter model-facing context.
- No chunk ref without a known previous full-seen or exact archive-backed source.
- Re-read spike disables chunk dedup for that session.
- Any decode mismatch fails open before model-facing context.
- Any output above the configured reference-density threshold full-passes.
- Any session above the configured cumulative reference-density threshold full-passes.
- Aggressive chunking must not affect patches, final code output, or terminal
  protocol correctness.

## Savings targets

- Similar-output workloads: target 10% to 30% additional billable-input savings
  over exact read/repeated-output reducers.
- Normal workday: positive net savings after recovery-note overhead.
- Host cost: chunking large outputs remains bounded and below measurable UX
  impact, with auto-bypass for very small outputs.

## Verification

- Chunk encode/decode exactness tests.
- Store TTL/LRU tests.
- Integrity budget tests using the admin/audit chunk-density counters.
- WSS replay with `--fail-on-lost`.
- Live CLI/Desktop matrix with canary counters and no repair-loop increase.

## Done

Chunk dedup is always-auto only when it is automatic, recoverable, budgeted,
canary-protected, and proven across multiple real Codex workloads. Before that,
it remains guarded by policy.

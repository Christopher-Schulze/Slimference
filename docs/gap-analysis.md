# Gap Analysis - Documentation Target vs Verified Reality

Date: 2026-04-17
Policy: documentation remains the target contract

---

## Framing

This file does not downgrade the documentation. It defines the gap between the
current implementation and the documented target so the implementation can be
lifted to the documented level with explicit work packages and hard proof.

---

## Gap Matrix

| Area | Documented target | Verified reality | Gap class | Target state | Owning plan |
|------|-------------------|------------------|-----------|--------------|-------------|
| Zero-downside | Compression never makes a request worse | Revert happens after body reconstruction in the hot path | correctness | Revert is mechanically enforced before forwarding | `docs/todo/t13-zero-downside-and-cache-correctness.md` |
| Layer 3 response cache | Safe replay for identical requests | Key is text-only and invalidation is response-substring-based | correctness | Canonical effective-request fingerprint and safe invalidation model | `docs/todo/t13-zero-downside-and-cache-correctness.md` |
| Claude Code hooks | Strict, transparent adoption path | Rewrite contract and config merge semantics are not yet robust enough | compatibility | Structured contract-correct hook flow and non-destructive merge/remove | `docs/todo/t12-hook-contract-hardening.md` |
| Codex hooks | Strong hook-based adoption | Current PostToolUse path routes output into the wrong execution surface | compatibility | Contract-correct post-tool filtering and authoritative verify path | `docs/todo/t12-hook-contract-hardening.md` |
| Hook verification | Install/verify can be trusted | Codex failures are not treated as hard verify failures | operability | `hook verify` fails whenever supported integrations are broken | `docs/todo/t12-hook-contract-hardening.md` |
| Layer 2 MiniMax strictness | Strong, low-drift summarization | Mostly async best-effort with weak cancellation and partial validation | product behavior | Explicit operating modes, stronger validator, full context propagation | `docs/todo/t14-layer2-strictness-and-cancellation.md` |
| Daemon/service | Local service path is production-worthy | Plaintext secrets in launchd plist and incomplete lifecycle handling | security/operability | No plaintext secret persistence, real lifecycle, tested behavior | `docs/todo/t15-daemon-service-productionization.md` |
| Coverage claim | 100% proof-level test coverage | Live baseline is 97.3% and CI gate can pass incorrectly | proof | 100% is either achieved and enforced or not claimed as complete | `docs/todo/t16-proof-gates-and-release-readiness.md` |
| CI proof | `scripts/ci` enforces the release bar | Coverage minimum is not parsed in the CI runner | proof | Release script blocks on real coverage and real regression checks | `docs/todo/t16-proof-gates-and-release-readiness.md` |
| Evidence trail | Repo history supports parity claims | Audit proof is fragmented and hard to compare later | traceability | Stable audit baseline, gap matrix, tracked remediation program | `docs/todo/t11-audit-remediation-program.md` |

---

## Tension That Must Be Resolved Explicitly

The repository currently wants all three of these at once:

1. zero downside
2. strongest possible MiniMax forcing
3. zero perceived latency

Those goals are not identical. The implementation needs explicit operating
modes and clear precedence rules:

- correctness before aggressiveness
- deterministic safety before summarization yield
- measurable proof before product claim expansion

That means the codebase should support a documented policy instead of relying on
implicit best-effort behavior.

---

## Delivery Sequence

### Phase 1 - Correctness floor

- Fix zero-downside in the hot path
- Repair cache correctness
- Repair hook contracts and verification

### Phase 2 - Safety and operability

- Remove daemon secret persistence risk
- Wire cancellation through all Layer 2 paths
- Strengthen validator inputs and acceptance checks

### Phase 3 - Proof and release gates

- Repair coverage gate
- Close remaining coverage gaps
- Build a release checklist that proves parity claims instead of asserting them

### Phase 4 - Claim closure

- Re-run the audit after implementation
- Compare against `docs/audit-1.md`
- Upgrade proof-bearing docs only after the code and gates support the claim

---

## Definition of Closure

This gap analysis is closed only when all of the following are true:

- every critical and high audit finding has an implemented fix
- the release gates fail when guarantees are broken
- hook compatibility is demonstrated against current supported CLI versions
- the repository can reproduce proof for the documented claims
- a follow-up audit shows the gap is closed, not merely reframed

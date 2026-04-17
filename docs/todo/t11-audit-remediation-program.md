# T11 - Audit Remediation Program

Status: open
Priority: critical
Scope: repository-wide production-readiness lift

---

## Problem

`docs/audit-1.md` established that the implementation is close in several core
areas, but it still has correctness, compatibility, security, and proof gaps
that prevent the current documentation target from being proven.

The project direction is explicit:

- keep the existing documentation/spec level as the target
- do not lower claims to match the current code
- raise the code, tests, hooks, and release proof until the target is real

---

## Program Goals

1. Make the documented guarantees mechanically true.
2. Remove correctness traps from the hot path.
3. Make supported hook integrations strict, verifiable, and non-destructive.
4. Convert current confidence claims into proof-backed release gates.
5. End with a second audit that can be compared against `docs/audit-1.md`.

---

## Workstreams

- T12: hook contract hardening
- T13: zero-downside and cache correctness
- T14: Layer 2 strictness and cancellation
- T15: daemon service productionization
- T16: proof gates and release readiness

The workstreams are not equal in urgency. The correct execution order is:

1. T13
2. T12
3. T14
4. T15
5. T16

Rationale:

- T13 protects the core request path and cached behavior.
- T12 protects CLI adoption and prevents destructive user-config mutations.
- T14 resolves the MiniMax policy tension and closes cancellation gaps.
- T15 cleans up the local service path.
- T16 should only lock down gates after the code path risks are reduced.

---

## Repository-Level Exit Criteria

- `go test ./...` green
- `go test -race ./...` green
- real coverage gate enforces the intended minimum
- zero-downside is proven with direct regression tests
- supported Claude Code and Codex hook paths are verified against current contracts
- service installation path no longer persists secrets in plaintext
- follow-up audit documents materially reduced risk

---

## Subtasks

- [ ] Land T13 and add hot-path regression tests.
- [ ] Land T12 and add install/remove/verify integration fixtures.
- [ ] Land T14 and decide the supported Layer 2 operating modes.
- [ ] Land T15 and prove launchd lifecycle behavior end-to-end.
- [ ] Land T16 and make release proof reproducible from a clean checkout.
- [ ] Re-run the deep audit and diff against `docs/audit-1.md`.

---

## Verification

```bash
go test ./...
go test -race ./...
go run ./scripts/ci
go run ./scripts/coverage -min=100
bun test
```

Additional manual verification required:

- Claude Code hook install -> verify -> remove on a temp home directory
- Codex hook install -> verify -> remove on a temp home directory
- local daemon install/uninstall on macOS without plaintext secret persistence

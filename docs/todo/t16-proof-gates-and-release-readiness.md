# T16 - Proof Gates and Release Readiness

Status: open
Priority: high
Scope: coverage enforcement, release verification, compatibility proof

---

## Problem

The repository has strong claims, but the proof stack is not yet strong enough:

- the current CI runner does not actually enforce the intended coverage minimum
- the documentation target needs a reproducible verification path
- production readiness needs explicit release gates, not confidence alone

---

## Desired End State

1. Release scripts fail whenever the documented proof bar is not met.
2. Coverage claims are backed by real gate behavior.
3. Supported CLI compatibility is part of release verification.
4. A future audit can be reproduced from commands in the repository.

---

## Work Packages

### WP1 - Repair the coverage gate

- remove the broken `-- -min=100` invocation pattern
- make the failure mode obvious and tested
- ensure `scripts/ci` blocks on the real minimum

### WP2 - Close coverage gaps

Priority targets from the latest audit baseline:

- `internal/daemon`
- `cmd/slimference`
- `internal/hooks`
- `internal/slogutil`
- `internal/summarization`
- `internal/tui`

### WP3 - Release verification bundle

Add or tighten repository-level proof commands for:

- unit tests
- race detector
- coverage gate
- TypeScript tests
- supported hook compatibility checks

### WP4 - Audit reproducibility

- codify the commands used in `docs/audit-1.md`
- make reruns comparable
- define a post-remediation audit checklist

---

## Subtasks

- [ ] Fix the `scripts/ci` coverage invocation.
- [ ] Add tests for the coverage script argument handling.
- [ ] Raise package coverage to the documented target.
- [ ] Add a repeatable release verification checklist.
- [ ] Add a follow-up audit checklist for parity proof.

---

## Acceptance Criteria

- `go run ./scripts/ci` fails whenever the intended coverage minimum is not met.
- Release verification can be run from a clean checkout with repository-native commands.
- The documentation target is supported by reproducible proof, not only narrative claims.

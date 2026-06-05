# TASK 286: Always-on safe product readiness rule

## Why

The product direction is now clear: new Slimference product code must be worth
shipping in the default runtime path. Existing legacy, lab, proof, and operator
surfaces may remain when they are already documented and isolated, but new
feature work must not add another mechanism that is planned to stay permanently
off, experimental, or manually promoted. This keeps engineering effort focused
on deterministic, drawdownless savings that can actually be enabled for users.

## Acceptance

- `AGENTS.md` states the new-product-code rule: new savings/product mechanisms
  must be designed for default-on or automatic safe enablement.
- The rule preserves existing isolated legacy/proof/operator code but forbids
  adding new always-off product mechanisms without explicit project override.
- Current docs/TODO surfaces record this rule so future agents do not reopen
  semantic summaries or build non-shipping feature flags.
- Repo audit distinguishes current release gaps from historical/deferred task
  text.
- Verification gates pass.

## Sub-Tasks

- [x] Add the always-on-safe rule to `AGENTS.md`.
- [x] Audit current docs for stale wording that conflicts with the rule.
- [x] Apply only docs/spec cleanup or always-on-safe code improvements found by
  the audit.
- [x] Run focused checks and full CI gate.

## Notes

- This task intentionally does not remove already-isolated legacy/lab features.
- New semantic summary, OCRL, context-ledger insertion, or external model
  compression remains forbidden product work.
- T286 promoted Layer-1 coordinator parallelism to default-on with an auto-gate
  that keeps tiny prefixes sequential and only fans out larger prefix work. This
  changes CPU scheduling only, not model-visible content.
- Current docs now state that `docs/todo.md` is the active task queue; unlisted
  detail files are historical records, not implicit open product work.
- Verification:
  - Focused `internal/compression` and `internal/config` tests passed.
  - `go test ./...` passed.
  - `go run ./scripts/ci` passed all 8 steps, including aggregate coverage
    96.6%, Codex smoke gate, live corpus gate, and leaf audit gate.

## Deviations

- None.

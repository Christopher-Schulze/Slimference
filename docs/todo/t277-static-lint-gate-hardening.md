# T277 - Static lint gate hardening

## Status

Done.

## Source

External model-review follow-up after validating the repo at commit `f0f96ed`.
The specific reported `go vet` bug is no longer present: `go vet ./...` is
clean. The broader finding remains valid: the project has no dedicated static
lint gate beyond the current CI steps.

## Why

The max-out bar values early detection of dead branches, unchecked errors,
copylock mistakes, unreachable code, and suspicious concurrency patterns. A
static lint gate can help, but only if it is reproducible, low-noise, and cheap
enough for the default local CI path.

## Scope

- Evaluate whether existing `go vet`, tests, race gates, and scripts already
  cover the practical defect classes.
- If adding a linter, prefer a Go-native script wrapper under `scripts/` that
  can run optional tools only when installed, or a documented pinned tool
  install path.
- Do not introduce a brittle local dependency that fails clean developer
  machines by default.
- Add focused tests or fixtures only when a real missed bug class is
  demonstrated.

## Non-goals

- Do not add a huge lint suite that duplicates CI coverage with high false
  positives.
- Do not require Docker, Node, or network during normal CI.
- Do not tune lint rules to hide real failures.

## Acceptance

- Decision document in this task: keep `go vet` only, add optional staticcheck,
  or add a pinned lint tool.
- If a new gate is added, `go run ./scripts/ci` remains green and the gate's
  runtime is measured.
- At least one concrete bug class is covered, or the task closes as no-op with
  evidence.
- Documentation states how developers run the gate locally.

## Verification

- `go vet ./...`
- Any new lint command.
- `go run ./scripts/ci`

## Notes

- Review models can overvalue "add golangci" as a reflex. The real criterion is
  useful signal under this repo's constraints.
- Decision: keep the default CI dependency-free and expand the existing
  `go vet` gate to `go vet ./...` instead of adding optional external lint
  tooling. This covers `cmd/`, `internal/`, `docs/`, `scripts/`, and tests; the
  historical reported bug class lived under `scripts/utils`, so the previous
  `./cmd/... ./internal/...` scope was too narrow.
- Local tool check on this machine: neither `staticcheck` nor `golangci-lint`
  is installed. Adding either to the default path would create a new local
  dependency without evidence that it catches a current repo defect.
- Verification:
  - `go vet ./...`
  - `go test ./scripts/ci ./docs -count=1`
  - `go run ./scripts/ci`

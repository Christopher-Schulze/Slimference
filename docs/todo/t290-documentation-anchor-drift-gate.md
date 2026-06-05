# T290 Documentation anchor drift gate

## Why

T288/T289 finished the current Layer 2/Layer 3 naming and RTK breadth work, but a deep repo scan found one live documentation drift: `docs/documentation.md` still linked the Layer 2 response-cache TOC entry to a stale `layer-3-response-cache` anchor. That is not a runtime drawdown, but it is an agent/operator re-entry risk because the docs are the main current-state reference.

## Acceptance

- [x] `docs/documentation.md` Table of Contents links resolve to real headings after the Layer 2/Layer 3 renumbering.
- [x] A docs-package test fails on future broken local `docs/documentation.md` heading anchors.
- [x] Scan results are recorded, including why historical unlisted task files were not bulk-edited.
- [x] `go test ./docs`, `go test ./...`, and `go run ./scripts/ci` pass.
- [x] Commit as `TASK 290: Documentation anchor drift gate`.

## Sub-Tasks

- [x] Fix the stale Layer 2 response-cache TOC anchor.
- [x] Add local documentation anchor test.
- [x] Record scan triage and run gates.
- [x] Commit task.

## Notes

- Production scan found no live `TODO`/`FIXME` markers outside help text and the intentional comment-strip whitelist.
- `cmd/slimference/failopen.go` contains `panic(r)` only for controlled-exit test/clean-exit unwinds inside the fail-open guard; not a product bug.
- Old unlisted `docs/todo/t*.md` files may still contain historical `Status: TODO` and old layer names. `docs/todo.md` explicitly declares unlisted detail files historical, so bulk-editing them would add churn without changing product truth.
- `staticcheck`, `golangci-lint`, and `govulncheck` are not installed in this local shell, so the authoritative local gate remains `go run ./scripts/ci`.
- `go test ./docs` passed.
- `go test ./...` passed.
- `go run ./scripts/ci` passed all 8 steps with aggregate coverage 96.5% and live-corpus gate PASS.

## Deviations

- None.

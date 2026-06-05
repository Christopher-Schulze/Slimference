# T273 - CLI/proxy God-file split

## Status

Open.

## Source

External model-review follow-up after validating Gemini, MiniMax, and Nemotron
claims against commit `f0f96ed`.

## Evidence

- `cmd/slimference/main.go`: 4602 lines.
- `cmd/slimference/codex_cmd.go`: 1446 lines.
- `internal/proxy/handler.go`: 2122 lines.
- The concern is maintainability and AI re-entry cost, not runtime model
  quality. It is not a product drawdown and must not be represented as one.

## Why

The files are functionally covered and CI-clean, but their size makes future
Maxx work slower and riskier. Small, domain-aligned files reduce review
surface, merge conflict risk, and accidental edits in unrelated command or
proxy phases. The split should improve engineering throughput without changing
runtime behavior or token-savings claims.

## Scope

Refactor in place, preserving existing packages and public behavior:

- `cmd/slimference/main.go`
  - keep process entry, command registry, top-level dispatch, and shared usage
    helpers
  - move command families into domain files that mirror existing test splits
    where possible
- `cmd/slimference/codex_cmd.go`
  - split by subcommand or responsibility: run, enable/disable/status,
    desktop launch, recert/proof helpers, shared parsing
- `internal/proxy/handler.go`
  - split by HTTP request phase, analytics worker, streaming/upstream relay,
    layer orchestration, and debug/decision recording
  - do not introduce new packages unless an existing boundary already supports
    it cleanly

## Non-goals

- No semantic rewrite of compression, WSS, routing, or proof accounting.
- No new abstraction just to make files small.
- No route, header, model-facing byte, or telemetry-counter change.
- No move into `research/`.

## Acceptance

- `git diff --function-context` shows moves/splits, not logic changes.
- `go test ./cmd/slimference ./internal/proxy -count=1` passes.
- `go test -race ./cmd/slimference ./internal/proxy -count=1` passes, or any
  race-only skip is documented with a root cause.
- `go run ./scripts/ci` passes.
- `git grep` for moved functions finds exactly one implementation each.
- No product proof counters or live-corpus fixtures change.

## Verification

- Pre-refactor snapshot: function list and selected `go test` baseline.
- Post-refactor: same focused tests, full CI, and `git diff --check`.
- For proxy handler splits, run focused WSS/output-reduce/readcache tests if
  any phase extraction touches those blocks.

## Notes

- This is a high-value maintainability task, but it is not a token-savings
  mechanism. It should not block product release proofs.

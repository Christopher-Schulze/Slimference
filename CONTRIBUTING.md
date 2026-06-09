# Contributing

## Development Setup

Slimference is a Go project. Use the Go version from `go.mod`.

```sh
go mod download
go test ./...
go run ./scripts/ci
```

## Product Boundaries

- Keep the default product path scoped to Codex CLI and Codex Desktop launches.
- Do not make transparent MITM, system proxy settings, hosts changes, or trusted
  local CA installation part of the default flow.
- Keep Claude Code support parked unless a change explicitly targets legacy or
  compatibility behavior.
- Do not add semantic summarization providers, OCRL/context-ledger insertion,
  or model-facing context replacement.

## Tests

New or changed Go behavior needs package-local `*_test.go` coverage. TypeScript
tests under `tests/ts/` may add E2E or contract coverage, but they do not replace
Go package tests.

Before opening a pull request, run:

```sh
go run ./scripts/ci
```

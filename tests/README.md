# `tests/` - Integration, fixtures, TypeScript suites

## Layout

| Path | Contents |
|------|--------|
| `integration/` | **Go** integration tests across packages, including optional `//go:build integration` suites. |
| `fixtures/` | Shared test data for Go and/or TypeScript tests. |
| `ts/` | **TypeScript** test suites, for example Vitest or Jest; see `AGENTS.md` Section 6.2. |

## Go vs. TypeScript

- The project coverage target for `internal/` and `cmd/` is enforced through
  package-local Go `*_test.go` files.
- `tests/ts/` is for additional TypeScript tests such as E2E or contract
  coverage. It does not replace Go package tests.

Small package-specific fixtures stay in `testdata/` next to the relevant Go
package.

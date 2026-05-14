# TASK 147: Layer 0 real-traffic parser frontier

Status: PENDING (planned 2026-05-13)
Priority: P1
Scope: `internal/filter/`, `internal/compression/tool_compressor.go`, `internal/analytics/`, `cmd/slimference/gain_cmd.go`, `tests/fixtures/l0_parsers/`, `docs/layer0-leaf-audit.md`.

## Why

Layer 0 is deterministic and cheap. The right expansion is not "50 languages"; it is the highest-volume tool outputs an agent actually sends back to the model. The current code already covers many broad categories and T124 added requested languages. This task goes after the next real-traffic frontier: ecosystem tools, framework CLIs, test runners, package managers, database outputs, and monorepo logs.

## Target State

Layer 0 parser coverage is driven by observed traffic:

1. Flight records show which tool outputs are large and frequent.
2. Parsers are added for high-volume shapes first.
3. Every parser is conservative, failable, and bypasses unknown shapes.
4. `gain --by-parser` shows real hit rates and savings.

## Priority Parser Families

### Go

- `go test -json`.
- plain `go test` failures.
- `go vet`.
- `staticcheck`.
- `golangci-lint`.

### Rust

- `cargo test`.
- `cargo nextest`.
- `cargo clippy`.
- `cargo build` diagnostics.
- `rustfmt --check`.

### TypeScript / JavaScript / Node / Bun

- `tsc --pretty false`.
- ESLint flat config output.
- `vitest`.
- `jest`.
- `bun test`.
- [x] `bun install` success/error summary compaction.
- [x] `npm/pnpm/yarn` install/update warnings and resolver errors.
- `vite`.
- `next build`.
- `turbo`.

### React / Svelte / Frontend

- React compiler/runtime diagnostic shapes.
- [x] Next.js route/build errors through the shared frontend diagnostic parser.
- [x] Svelte compiler errors through the existing Svelte diagnostic parser.
- [x] Vite HMR/build errors through the shared frontend diagnostic parser.
- [x] Playwright test failures through the shared frontend diagnostic parser.
- [x] Vitest/Jest/Bun test frontend failures through the shared frontend diagnostic parser.

### Python

- [x] `pytest` direct, wrapped, and `python -m pytest` matching in the
  shared diagnostic recognizer; existing test-output fallback remains the
  owner for pytest compaction labels.
- [x] `ruff`.
- [x] `mypy`.
- [x] `pyright` / `basedpyright`.
- [x] `pylint`, `flake8`, and `python -m unittest` matching in the shared
  diagnostic recognizer.
- [x] `uv` sync / pip install resolver errors.
- [x] `pip` resolver errors.

### Zig / C / C++

- `zig build/test`.
- GCC/Clang diagnostics already exist; extend for:
  - linker errors.
  - CMake configure/build output.
  - sanitizer traces.
  - make/ninja failure summaries.

### JVM / Mobile / Other High-Use Stacks

- Java `javac`, Maven, Gradle.
- Kotlin Gradle diagnostics.
- Swift build/test.
- Dart/Flutter analyzer/test.
- PHP Composer/PHPUnit/Psalm/PHPStan.

### Database and Data

- [x] PostgreSQL `psql` table and diagnostic/error matching.
- [x] SQLite shell empty-output and diagnostic/error matching.
- [x] MySQL/MariaDB table, empty-output, and diagnostic/error matching.
- semantic `EXPLAIN`/query-plan summarization.
- migration tool output.
- [x] Prisma/Drizzle migration diagnostic matching through the shared SQL
  diagnostic parser.
- [x] SQL lint/format diagnostics through existing SQLFluff/Sqruff matching.

### Infra / Monorepo

- Docker build/run errors.
- Kubernetes `kubectl describe/get/events`.
- Helm lint/template/install.
- Terraform/OpenTofu plan/apply already exists; extend real misses.
- GitHub CLI output.
- Nx/Turborepo/Lerna workspace output.

## Work Packages

### WP1 - Parser opportunity report

- Use flight records and leaf audit to rank:
  - total input tokens by command family.
  - average saving.
  - parser miss rate.
  - top passthrough large outputs.
- Do not build parsers with no observed traffic unless they are on the operator's requested core list.

### WP2 - Shared diagnostic model

- Normalize diagnostics to:
  - tool.
  - severity.
  - file.
  - line/column.
  - code.
  - message.
  - snippet.
  - repeated count.
  - command.
  - exit status.
- This prevents one-off parser sprawl.

### WP3 - Conservative parser implementation

- Parser returns compact output only when shape confidence is high.
- Unknown output returns original content.
- All compactors preserve exact actionable lines.
- Max elision caps prevent over-compression.

### WP4 - Fixtures

- Each parser gets:
  - typical success output.
  - one failure output.
  - one huge repeated output.
  - one malformed/unknown output that must bypass.
  - one regression fixture from live corpus if available.

### WP5 - Parser telemetry

- Persist parser name, in/out bytes/tokens, bypass reason.
- `gain --by-parser` distinguishes:
  - hits.
  - misses.
  - bypasses.
  - negative-savings prevented.

## Acceptance

- [ ] Parser priority is backed by observed corpus or explicit core-stack requirement.
- [ ] Shared diagnostic model is used where possible.
- [ ] Each parser has high-confidence bypass behavior.
- [ ] Core requested stacks are covered: sh, bash, zsh, Python, JS/TS, Bun/Node, Rust, Go, Zig, C, C++, React, Svelte, Markdown, SQL/DB.
- [ ] Next common stacks are covered or explicitly queued: Java, Kotlin, Swift, PHP, Dart/Flutter, Docker, Kubernetes, Terraform/OpenTofu, monorepo tools.
- [ ] Parser telemetry exposes real hit/saving rates.
- [ ] Live-corpus gate proves no parser causes quality loss.
- [ ] `go run ./scripts/ci` passes with 100% coverage for new Go code.

## Expected Upside

- 5-20% additional Layer 0 saving on tool-heavy coding sessions, depending on parser hit rate.
- Very low latency and privacy cost.
- High maintainability if built through shared diagnostic models rather than hundreds of unique ad hoc functions.

## Non-Goals

- Do not add obscure language parsers without traffic or operator need.
- Do not compact output intended for machine consumption in a later shell pipeline.
- Do not delete existing parsers.

## Notes

- 2026-05-14: T143b closed part of the user's requested stack coverage on the
  Layer 1 file/content side, not this Layer 0 tool-output side. Markdown, SQL,
  GraphQL, HCL, Dockerfile, and Makefile now have deterministic structure
  summaries for large tool-result content. T147 still owns command-output
  parsers and telemetry for real CLI output shapes.
- 2026-05-14: First T147 slice landed in the shared diagnostic model: `next`,
  `vite`, `vitest`, `jest`, `playwright`, `eslint`, `biome`, `oxlint`,
  `turbo`, `bun test`, and `bun build` route into the conservative
  `frontend` diagnostic compactor. It preserves only actionable diagnostic
  rows and short failure summaries, and bypasses when output would not shrink.
- 2026-05-14: Python diagnostic slice landed. The shared diagnostic parser now
  recognizes ruff, pylint, flake8, mypy, pyright/basedpyright, pytest,
  `python -m pytest`, `uv run pytest`, `poetry run pytest`, and
  `python -m unittest`. `lint_output` calls the shared parser after exact
  success compactors, so non-empty Python lint/type-check output is reduced
  without removing the older specialized success paths. `test_output` keeps
  its existing label-preserving fallback for pytest to avoid regressions.
- 2026-05-14: SQL/DB client diagnostic slice landed. The shared SQL
  diagnostic parser now recognizes `psql`, `sqlite3`/`sqlite`,
  `mysql`/`mariadb`, Prisma, Drizzle Kit, SQLFluff, and Sqruff, including
  package-manager wrappers such as `npm exec --`, `npm x`, `pnpm exec`, `bun x`,
  `bun run`, `yarn`, and `npx`. This deliberately covers diagnostic/error
  compaction only; rich table/explain summarization remains future work.
- 2026-05-14: Package-manager resolver-error slice landed. `npm install|ci|update`,
  `pnpm install|ci|update`, `yarn install|upgrade`, `bun install`,
  `pip install`, `uv pip install`, and `uv sync` now compact noisy install
  resolver failures to actionable error lines while still preserving existing
  success summaries. Error output is capped to avoid shipping repeated resolver
  traces back to the model.
- 2026-05-14: SQL shell table-output slice landed. The existing `psql` ASCII
  table compactor now also recognizes MySQL, MariaDB, SQLite, and SQLite3
  shells, strips border-only lines, normalizes padded `|` columns, and emits
  compact ok markers for empty SQL shell output. This is deliberately table
  shape compaction, not semantic query-plan interpretation.

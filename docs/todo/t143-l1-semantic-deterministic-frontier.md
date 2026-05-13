# TASK 143: Layer 1 semantic deterministic compaction frontier

Status: PENDING (planned 2026-05-13)
Priority: P0
Scope: `internal/compression/`, `internal/filter/`, `internal/codecompact/`, `internal/tokens/`, `internal/sessions/`, `internal/quality/`, `tests/fixtures/l1_frontier/`, `docs/savings-assessment.md`.

## Why

Layer 1 is the safest saving class because it stays local and deterministic. The current pipeline already handles JSON minify, duplicate collapse, comment/structure extraction, and some AST-style file-read compaction. The next frontier is semantic deterministic compaction: preserve what the model needs while removing repeated, mechanically reconstructable, or inactive detail.

The constraint is hard: no third-party calls, no hidden context loss, no aggressive mutation without reversible archive or quality gate.

## Target State

Layer 1 becomes a multi-pass semantic reducer with a central budget plan:

1. Detect content class and provider tokenizer.
2. Pick a safe compaction plan.
3. Apply deterministic reductions.
4. Record exact in/out tokens and reason codes.
5. Preserve lossless or recoverable state where required.
6. Bypass when expected net saving is negative or quality risk is high.

## Work Packages

### WP1 - Tokenizer-aware budget planner

- Reopen the useful part of T95 now that proxy/hook/session context exists.
- Replace character-only thresholds with provider-token budgets where provider context is available.
- Keep char thresholds as fallback.
- Budgets by content family:
  - tool output.
  - source file.
  - config file.
  - logs.
  - stack traces.
  - markdown.
  - SQL/database output.
- Acceptance requires no over-trimming from tokenizer mismatch.

### WP2 - Reversible per-request dictionaries

- Build a deterministic alias table for repeated high-cost strings:
  - absolute paths.
  - long package/module names.
  - repeated test names.
  - repeated stack frames.
  - repeated error prefixes.
  - repeated JSON keys in huge objects.
- Replace repeats with compact aliases only inside the same request body.
- Add a legend at the smallest safe scope.
- Do not alias code identifiers if it would make code edits ambiguous.

### WP3 - Multi-language symbol slicing

- Extend AST/symbol slicing beyond Go:
  - TypeScript/JavaScript/TSX/JSX.
  - Python.
  - Rust.
  - Svelte.
  - Zig.
  - C/C++.
  - SQL.
  - Markdown code fences.
- Prefer existing stdlib or lightweight parsers; tree-sitter only if default build stays clean or feature-gated.
- Output shape:
  - file header/imports.
  - symbol table.
  - exported/public signatures.
  - touched/recent bodies.
  - referenced error-line context.
  - elided body markers with exact recovery command.

### WP4 - Stacktrace and test-failure semantic compaction

- Recognize high-volume failure shapes:
  - Go `go test`.
  - Rust `cargo test` / `cargo nextest`.
  - Python `pytest`.
  - JS/TS `vitest`, `jest`, `bun test`.
  - Java/Kotlin Gradle/Maven.
  - C/C++ compiler and sanitizer traces.
- Preserve:
  - failing test name.
  - exact assertion diff.
  - top application frames.
  - command and exit status.
- Collapse:
  - vendor/framework frames.
  - repeated path prefixes.
  - repeated expected/actual boilerplate.

### WP5 - Config/schema-aware compaction

- For JSON/TOML/YAML/HCL/package manifests:
  - preserve referenced keys and dependency versions.
  - summarize unrelated blocks by key count and hash.
  - keep lockfile changes as package/version deltas.
- Never compact secrets into visible summaries; redaction runs first.

### WP6 - Markdown and documentation compaction

- Markdown is often text-heavy and currently underexploited.
- Detect:
  - headings.
  - code fences.
  - tables.
  - repeated generated sections.
  - changelog/task logs.
- Compact inactive sections into heading outlines while preserving active/current section verbatim.

### WP7 - Quality gates

- Per-content negative-saving guard.
- Per-content max elision ratio.
- If a file was recently edited or is likely to be edited, prefer full content unless T149 explicitly allows slicing.
- Every lossy reduction must include:
  - reason.
  - recoverability hint.
  - archive key when available.
- Add golden Q/A fixtures where the model must still answer line/path/error questions from compacted context.

## Acceptance

- [ ] Token budgets are provider-aware when context is available.
- [ ] Reversible dictionary compaction never aliases edit-critical identifiers.
- [ ] Multi-language slicing covers the requested high-volume stacks.
- [ ] Stacktrace/test compaction preserves exact actionable failure data.
- [ ] Markdown/SQL/config compaction has dedicated fixtures.
- [ ] No content class can produce negative token saving for more than the configured tolerance.
- [ ] Quality fixtures prove no loss on line/path/error/debug tasks.
- [ ] Live-corpus gate from T146 shows positive net saving before default-on.
- [ ] `go run ./scripts/ci` passes with 100% coverage for new Go code.

## Expected Upside

- Additional 10-25% input reduction on top of current L1 for code/tool-heavy sessions.
- 40-80% reduction on large read-only file inspections when symbol slicing applies.
- Strongest no-drawdown lever because all work is local and deterministic.

## Risks

- Over-slicing can force extra tool calls, which can cost more than saved input tokens.
- Multi-language parsing can become maintenance-heavy if implemented as too many bespoke parsers.
- Dictionary legends can add overhead on small inputs; planner must bypass small cases.


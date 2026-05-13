# TASK 143: Layer 1 semantic deterministic compaction frontier

Status: IN PROGRESS (T143a reversible path dictionary and T143b text/config structure extraction landed 2026-05-14)
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

- [x] Build a deterministic alias table for repeated high-cost strings:
  - absolute paths.
- Implemented first safe slice: repeated absolute local paths inside a
  tool-result block are replaced with `[P1]`, `[P2]`, ... only when a prepended
  legend keeps the transform reversible and produces positive net savings.
- Current guards: known local filesystem roots only, minimum path length,
  minimum occurrence count, maximum eight aliases, URL-style path rejection,
  and negative-saving bypass.
- Remaining dictionary classes:
  - long package/module names.
  - repeated test names.
  - repeated stack frames.
  - repeated error prefixes.
  - repeated JSON keys in huge objects.
- [x] Replace repeats with compact aliases at the smallest safe scope.
  T143a uses block-local scope because it is self-contained and reversible
  without session state.
- [x] Add a legend at the smallest safe scope.
- [x] Do not alias code identifiers if it would make code edits ambiguous.
  T143a aliases only absolute paths, never symbol names.

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
- [x] Land the first non-code structure slice for high-volume text/config
  formats: Markdown, SQL, GraphQL, HCL, Dockerfile, and Makefile now produce
  deterministic outlines through `structure_more.go`.
- Current T143b scope is structural, not body-on-demand AST slicing: it keeps
  headings, clauses, declarations, targets, and top-level blocks, while
  dropping inactive prose or command bodies only when the summary is shorter.
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
  - [x] headings.
  - [x] code fences.
  - [x] tables.
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
- [x] Reversible dictionary compaction never aliases edit-critical identifiers
  for the implemented absolute-path slice.
- [ ] Multi-language slicing covers the requested high-volume stacks.
- [ ] Stacktrace/test compaction preserves exact actionable failure data.
- [x] Markdown/SQL/config compaction has dedicated local fixtures for the
  landed T143b structure-extraction slice.
- [x] The T143a path dictionary has a strict negative-saving bypass.
- [ ] No content class can produce negative token saving for more than the configured tolerance across every future T143 slice.
- [ ] Quality fixtures prove no loss on line/path/error/debug tasks.
- [ ] Live-corpus gate from T146 shows positive net saving before default-on.
- [ ] `go run ./scripts/ci` passes with 100% coverage for new Go code.

## Implementation Notes

- 2026-05-14 T143a:
  - Added `internal/compression/semantic_dictionary.go`.
  - Layer 1 now reports `DictionarySaved` and the proxy exposes it as
    `semantic_dictionary` in Layer 1 breakdowns.
  - The dictionary is reversible by construction: the full original path stays
    in a local legend and body occurrences use compact aliases.
  - Focus tests: `go test ./internal/compression ./internal/proxy -cover` at
    100% for both packages.
- 2026-05-14 T143b:
  - Extended `StructureExtractor` to Markdown, SQL, GraphQL, HCL, Dockerfile,
    and Makefile.
  - Markdown keeps headings, task/list/quote markers, table rows, and code
    fences; SQL keeps DDL/DML/constraint clauses; GraphQL/HCL keep top-level
    blocks; Dockerfile keeps image/control/copy/cmd instructions and collapses
    `RUN` chains to a command count; Makefile keeps includes, variables,
    `.PHONY`, and targets.
  - All summaries remain guarded by the existing shorter-than-original bypass.
  - Focus test: `go test ./internal/compression -cover` at 100%.

## Expected Upside

- Additional 10-25% input reduction on top of current L1 for code/tool-heavy sessions.
- 40-80% reduction on large read-only file inspections when symbol slicing applies.
- Strongest no-drawdown lever because all work is local and deterministic.

## Risks

- Over-slicing can force extra tool calls, which can cost more than saved input tokens.
- Multi-language parsing can become maintenance-heavy if implemented as too many bespoke parsers.
- Dictionary legends can add overhead on small inputs; planner must bypass small cases.

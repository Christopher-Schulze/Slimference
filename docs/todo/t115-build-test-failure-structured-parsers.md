# TASK 115: Build / test failure detection - structured parsers

Status: PENDING (audit-driven mitigation 2026-04-30)
Priority: P1
Scope: `internal/filter/builtin_compact_helpers.go`, `internal/filter/builtin_build.go`, `internal/filter/builtin_testrun.go`, `internal/filter/builtin_lint.go`, `tests/fixtures/build_corpus/`
Driver: `extractBuildErrors` (`builtin_compact_helpers.go:62-88`) is a substring grep against `error|failed|fatal|cannot|undefined|unresolved|aborting`. False-positive examples observed in the wild: `"Successfully resolved errors: 0"`, `"Test 'test_undefined_handling' passed"`, `"Aborting on first failure: false"`. This emits noisy compaction output where lines that *describe* errors get mistaken for actual error lines, and the filtered tool_result confuses the model into thinking the build failed. Replace the substring heuristic with per-tool structured parsers (cargo, gcc/clang, go, rustc, msbuild, gradle, maven, npm/eslint, pylint, mypy, golangci-lint).

---

## Problem

`internal/filter/builtin_compact_helpers.go:62-88`:
```go
func extractBuildErrors(stdout string) (string, bool) {
    for _, line := range strings.Split(stdout, "\n") {
        l := strings.ToLower(line)
        if strings.Contains(l, "error:") || strings.Contains(l, "failed") || ... {
            kept = append(kept, line)
        }
    }
}
```

This is the catch-all fallback inside `TryCompactBuildOutput` and `TryCompactTestOutput` when no tool-specific compactor matches. Two failure modes:

1. **False positives**: success messages containing the trigger word are kept; the model sees a "compacted error list" that isn't.
2. **Missing context**: real error lines get extracted in isolation without the file-line-column context lines that the build tool emitted around them. Fix-instructions become useless.

`extractTestFailures` has the same shape with `--- FAIL:`, `FAIL\t`, `FAILED`, `● ` patterns; same FP risk.

## Target State

A registry of per-tool structured parsers:

| Tool | Output Shape | Parser strategy |
|---|---|---|
| `cargo build` | `error[E0xxx]: ...` followed by ` --> file:line:col`, multi-line spans | Block-mode parser bounded by blank lines |
| `cargo test` | `running N tests`, ` test foo ... ok / FAILED`, panic blocks | Status-line + panic-block extractor |
| `gcc/clang` | `file:line:col: error/warning: ...` | Line + 1-3 follow lines (caret + suggestion) |
| `go build` | `./file.go:line:col: ...` | Same line shape |
| `go test` (text) | `--- FAIL: TestName (0.00s)` + indented body until next `---` or `PASS\|FAIL\|ok\|FAIL` summary | Block parser on dashed delimiters |
| `go test -json` | NDJSON events | Already structured (`builtin_testrun.go::TryCompactGoTestJSON`) - keep |
| `rustc` | identical to cargo block shape | reuse cargo parser |
| `msbuild` | `file(line,col): error CSxxxx: ...` | line shape |
| `gradle` / `maven` | hierarchical task output, `BUILD FAILED` summary, `> Task :foo:bar FAILED` | block parser |
| `npm test` (jest) | `● Suite > test ... FAIL` + assertion frames | block parser bounded by `●` |
| `pylint` / `mypy` / `flake8` | `file:line[:col]: code [W/E] msg` | line shape with severity column |
| `golangci-lint` | `file:line:col: linter: msg` | line shape |
| `eslint` | `file\n  line:col  severity  rule  msg` | indented block parser |

The fallback substring grep stays as the **last-resort** mode for unrecognised tools but is downgraded to "kept lines plus ±2 context" so the model still has surrounding hint lines.

## Implementation Plan

### WP1 - Parser registry
- New `internal/filter/structured_parsers.go` exposing `ParseFailures(toolHint string, stdout string) (compact string, ok bool)`.
- `toolHint` derived from argv (already extracted by dispatch layer).

### WP2 - Per-tool parsers
- One sub-file per tool family: `parser_rust.go`, `parser_go.go`, `parser_clang.go`, etc.
- Each implements `Parse(stdout string) (compact string, hadFailures bool, ok bool)`.
- `ok=false` => fall through to the next parser; `ok=true` => use this output.

### WP3 - Wire-in
- `TryCompactBuildOutput` consults `ParseFailures(argv0, stdout)` first; if `ok`, returns its output.
- Falls back to today's `extractBuildErrors` (renamed `extractFailuresFallback` to make its lower-tier role explicit) only when no parser matches.
- Same for `TryCompactTestOutput`.

### WP4 - Context preservation
- Each parser keeps ±N context lines around extracted errors (N defaults to 2, configurable via `[filter.tuning] failure_context_lines`).
- Maximum total kept lines per failure: 30 (truncate with `[... N more lines truncated]`).

### WP5 - Build corpus
- `tests/fixtures/build_corpus/<tool>/<scenario>.txt` with paired `expected.txt` showing the post-compaction output.
- Scenarios per tool: `success.txt`, `single_error.txt`, `multiple_errors.txt`, `warnings_only.txt`, `mixed.txt`, `panic_in_test.txt`, `compile_then_link_error.txt`.

### WP6 - Regression test harness
- New `internal/filter/structured_parsers_corpus_test.go` walks the fixture tree, asserts `ParseFailures` output equals `expected.txt` byte-for-byte.
- CI integration via `scripts/ci`.

### WP7 - False-positive measurement
- New synthetic corpus `tests/fixtures/build_corpus/false_positive_traps/` with strings like `"Successfully resolved errors: 0"`, `"Test 'test_undefined_handling' passed"`.
- Assert all parsers return `hadFailures=false` on these inputs.
- The fallback substring grep is allowed to FP these but its output is gated to ≤30% of input lines so the model still sees most context.

## Acceptance Criteria

- [ ] All 12 parsers shipped with corpus coverage.
- [ ] False-positive corpus produces zero `hadFailures=true` from structured parsers.
- [ ] Recall ≥95% on real-failure corpus (each fixture's `expected.txt` matched).
- [ ] `extractFailuresFallback` (renamed) only runs when no parser matched.
- [ ] Coverage 100%; race tests green.
- [ ] `scripts/ci` runs the corpus regression as a blocking gate.

## Out of Scope

- Tools without recognisable error shapes (custom internal CLIs) - they still hit the fallback.
- Cross-locale error messages (we assume English; rerun with C locale would be a separate task).
- Auto-detection of which tool ran when argv hint is missing (heuristic; could be added later).

## Validation

```
go test -race ./internal/filter/...
go run ./scripts/ci
```

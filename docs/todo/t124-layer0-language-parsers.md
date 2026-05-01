# TASK 124: Layer 0 per-language structured parsers (full programming-language coverage)

Status: PENDING (planned 2026-05-01)
Priority: P1
Scope: `internal/filter/parser_*.go` (existing T115 path), `internal/filter/builtin_*.go`, `tests/fixtures/build_corpus/`, `tests/fixtures/lang_corpus/` (new).
Driver: T115 landed structured parsers for Go, Cargo, GCC/Clang. The leaf-audit (T119) showed 200+ TryCompact functions, mostly heuristic. Real coding sessions span far more languages and tools than that. Every per-language tool has a deterministic output shape that a parser can compact 80-95% lossless: error/warning blocks with line/column references, success summaries, test-result statistics, lint reports. Today most of those land in the generic build/test fallback which uses substring matching ("error", "failed", "panic") and produces noisy false-positive-prone compaction. T124 closes the gap by shipping a per-language parser for every mainstream language an LLM coding agent might invoke.

---

## Languages covered (and what each parser extracts)

Each parser is a single file under `internal/filter/parser_<lang>.go`, registered with the existing T115 dispatch table in `structured_parsers.go`. Tests under `_test.go` neighbour each parser; corpus under `tests/fixtures/lang_corpus/<lang>/`.

### Go (extends T115)
- **`go test`**: PASS/FAIL summary lines + failing-test names + `--- FAIL: TestX` blocks with the fail-context. Drops `=== RUN`, `=== PAUSE`, coverage lines unless `-cover` is in argv.
- **`go vet`**: `<file>:<line>:<col>: <message>` rows kept; everything else dropped.
- **`go build`**: T115 already covers; extends with `golang.org/x/tools/go/packages`-style multi-package error grouping.
- **`gopls`**: structured diagnostic JSON from LSP - parse + group by file.
- **`go mod tidy` / `go mod download`**: success summary + retract list; drops the verbose download list.
- **`go run`**: passes through unchanged (cannot be compacted; output is the program).

### Rust (extends T115)
- **`cargo build / cargo check`**: T115 covers; extend with `--message-format=json` mode if the tool was invoked that way.
- **`cargo test`**: per-test PASS/FAIL + failure-output groups.
- **`cargo clippy`**: lint diagnostics with file:line:col + suggestion.
- **`rustfmt --check`**: per-file diff or "ok"; full diff bodies replaced with line-count summaries.
- **`cargo doc` / `cargo bench`**: success + bench-result table.

### Python
- **`pytest`**: pass/fail summary + per-test FAIL block with assertion + traceback (existing builtin_python.go covers tracebacks; extend).
- **`mypy`**: `<file>:<line>: error: <msg>  [code]` rows.
- **`ruff check / ruff format`**: per-file finding rows + summary.
- **`pylint`**: convention/warning/error rows + score.
- **`black --check / isort --check-only`**: per-file ok/needs-reformatting + summary.
- **`pip install`**: dependency-resolution table compaction (already partially in builtin_pkg.go); extend for collected/installed lists.
- **`poetry install / pipenv install / uv sync`**: same.

### JavaScript / TypeScript
- **`tsc`**: `<file>(line,col): error TS<code>: <msg>` rows.
- **`eslint`**: per-file rows + summary; existing builtin_lint.go has stub - upgrade to full per-rule ID extraction.
- **`prettier --check`**: file-list summary.
- **`jest / vitest`**: existing builtin_testrun.go covers; extend.
- **`tsdoc / typedoc`**: success/warning summary.
- **`pnpm / yarn / npm install`**: existing in builtin_pkg.go; tighten output.
- **`webpack / vite / rollup / esbuild`**: bundle-size summary + warnings.

### C / C++ (extends T115)
- **`gcc / clang / clang++`**: T115 covers; extend with `-fdiagnostics-format=json` parsing when the flag is present.
- **`make`**: target-execution log + first failure.
- **`cmake`**: configure-stage summary; build-stage delegates to `make` / `ninja`.
- **`ninja`**: `[N/M]` progress trimmed; failures kept verbatim.
- **`cargo`-of-C-projects (Meson, Bazel, Buck2)**: same shape, single-line summary + failure block.

### Java / Kotlin / Scala
- **`mvn` / `gradle`**: BUILD SUCCESS / FAILURE + per-module timing table; extend the existing T115 generic build parser with structured `[ERROR]`/`[WARNING]` block extraction.
- **`javac`**: `<file>:<line>: error: <msg>` rows.
- **`kotlinc`**: same.
- **`scalac` / `sbt`**: same with sbt's `[error]`/`[warn]` prefixes.
- **`junit / testng`**: pass/fail summary + per-test failure with stack.

### Swift / Objective-C
- **`swift build / swift test`**: error/warning rows + summary.
- **`xcodebuild`**: per-target compile-time table + first failure.
- **`swift-format / swiftlint`**: per-file rows + summary.

### Ruby (extends builtin_ruby.go)
- **`rake`** / **`rspec`**: existing covers; extend with per-spec assertion failure context.
- **`rubocop`**: rule-id grouped lint output.
- **`bundle install`**: dependency table compaction.

### PHP
- **`composer install / update`**: package install summary.
- **`phpunit`**: pass/fail + per-test failure block.
- **`phpstan / psalm`**: error rows.
- **`php -l`**: syntax-check rows.

### Elixir
- **`mix compile / mix test`**: pass/fail summary + per-test failure context.
- **`mix format --check-formatted`**: per-file rows.
- **`credo`**: lint rows.

### Haskell
- **`cabal build / stack build`**: per-module compile + warning summary.
- **`cabal test / stack test`**: pass/fail + counterexample shrinking.
- **`hlint`**: rule-id grouped suggestions.

### Other
- **`dart pub get / flutter pub get`**: dependency table.
- **`dart test / flutter test`**: pass/fail + per-test failure.
- **`zig build / zig test`**: error rows + summary.
- **`nim c / nimble test`**: error rows + summary.
- **`crystal build / crystal spec`**: error rows + spec results.

## Implementation plan

### WP1 - Parser registry expansion

Existing `structured_parsers.go` has a small registry (go/cargo/clang). Expand to a multi-tier lookup:

```go
type LangParser struct {
    Name      string
    Detect    func(argv []string) bool
    Parse     func(stdout, stderr []byte) (compact []byte, hadFailures bool, ok bool)
    Lang      string
}

var langParsers = []LangParser{
    {Name: "go-test", Detect: isGoTestArgv, Parse: parseGoTest, Lang: "go"},
    {Name: "go-vet",  Detect: isGoVetArgv,  Parse: parseGoVet,  Lang: "go"},
    {Name: "rustc",   Detect: isRustcArgv,  Parse: parseRustc,  Lang: "rust"},
    // ... 50+ entries, one per parser
}
```

Detect runs in declaration order; first match wins. Most-specific arg patterns first (e.g. `cargo test --release` before `cargo` alone).

### WP2 - Per-parser implementation

Each parser is ~50-200 lines. Pattern:

```go
func parseMypy(stdout, stderr []byte) ([]byte, bool, bool) {
    diagnostics := mypyDiagnosticRE.FindAllStringSubmatch(string(stdout), -1)
    if len(diagnostics) == 0 {
        // Still compact: drop banner, find summary line.
        return mypySummaryOnly(stdout)
    }
    var sb strings.Builder
    for _, d := range diagnostics {
        sb.WriteString(d[0]) // full diagnostic line
        sb.WriteByte('\n')
    }
    if summary := mypySummaryRE.FindString(string(stdout)); summary != "" {
        sb.WriteString(summary)
        sb.WriteByte('\n')
    }
    return []byte(sb.String()), len(diagnostics) > 0, true
}
```

### WP3 - Corpus

`tests/fixtures/lang_corpus/<lang>/<scenario>.txt` paired with `expected.txt`. Scenarios per parser:
- `success_short`, `success_long`, `success_empty`
- `failure_one`, `failure_many`, `failure_with_warnings`
- `mixed_pass_fail`
- `pre-existing-warnings` (ignore, don't emit failure)
- `unicode-paths` (CJK, emoji in error messages)

50+ parsers x 7 scenarios = 350 corpus files. We do not need every cell; pragmatically each parser ships with its 4 most-distinctive scenarios.

### WP4 - Dispatch order tuning

Parsers register a priority. The dispatch loop in `applyLayer0Filters` walks specific-first. T115's dispatch is preserved; new parsers slot in between the existing per-tool entries and the generic build/test fallback.

Auto-detection precedence:
1. `argv[0]`-exact match (e.g. `mypy`, `pytest`).
2. `argv[0] == "node" && argv[1] ends with .js && script-content-detect` (rare, wrapped invocations).
3. Wrapper detection (`npx`, `pnpm exec`, `yarn`, `cargo run --`) - re-dispatch to inner argv.
4. `argv[0]` build-system + `argv[1]` subcommand (e.g. `cargo test`, `gradle build`).
5. Generic build/test fallback.

### WP5 - Stream-vs-buffered handling

Many of these tools are long-running and stream output. The T122 transparent-mode dispatch sees the full body before the L0 filter runs (the proxy buffers into the response writer). For native (non-transparent) mode where Slimference is invoked directly via PreToolUse hook, the output is already complete by the time the hook runs.

T108/T94 future stream-mode compaction is a separate task; T124 is buffered-only.

### WP6 - Telemetry

- Per-parser hit counter: `/admin/status.layer0.parsers[<name>].fired_total`.
- Per-parser bytes-saved counter: `/admin/status.layer0.parsers[<name>].bytes_saved_total`.
- `slimference gain --by-parser` ranks parsers by tokens saved over the rolling 7-day window.

### WP7 - Tests

- Per-parser `_test.go`: ~10 tests per parser. Total: 500+ tests.
- Corpus integration: a single `TestLangCorpus_AllParsers` walks `tests/fixtures/lang_corpus/`, runs each fixture through its declared parser, asserts byte-equal to `expected.txt`. Replaces 500+ individual test functions with one.
- Dispatch tests in `pipeline_test.go` confirm priority ordering and wrapper-detect behaviour.

## Acceptance criteria

- [ ] 50+ per-language parsers registered, dispatch-ordered correctly.
- [ ] Per parser: at least 3 corpus fixtures (success / failure / edge).
- [ ] Total Layer 0 corpus passes byte-equal end-to-end.
- [ ] No regression in existing T115 / T117 / T119c / generic-build coverage.
- [ ] Coverage 100%; race-clean; CI gate green.
- [ ] Leaf-audit ratio: empty-only ratio stays at 4.7% or drops further; real-parser count rises by ~50.
- [ ] `slimference gain --by-parser` reports per-parser saving on real corpus.

## Out of scope

- Stream-mode parsers for live tail output (T108/T94 own that).
- IDE-language-server output beyond `gopls` and `tsserver` (rare in agent tool calls).
- Localised tool output (most CLIs ship English; locale handling is operator-side).
- Custom enterprise build systems (Bazel covered partially, Buck2 partially; bespoke ones are operator-supplied via TOML).

## Validation

```
go test -race ./internal/filter/...
go run ./scripts/benchmarks benchmark-corpus tests/fixtures/lang_corpus/ --check
go run ./scripts/utils leaf-audit --root=. --check
slimference gain --by-parser
```

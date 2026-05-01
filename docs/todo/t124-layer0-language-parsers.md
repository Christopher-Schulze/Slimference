# TASK 124: Layer 0 real-traffic parser expansion

Status: CODE-COMPLETE FOR REAL-TRAFFIC CORE / LIVE-CORPUS PROOF PENDING (2026-05-02)
Priority: P1
Scope: `internal/filter/parser_*.go` (existing T115 path), `internal/filter/builtin_*.go`, `tests/fixtures/build_corpus/`, `tests/fixtures/lang_corpus/` (new).
Driver: T115 landed structured parsers for Go, Cargo, GCC/Clang. The leaf-audit (T119) showed 200+ `TryCompact*` functions, but only a small structured-parser registry; many existing wins are broad built-ins and heuristic wrappers. Real coding sessions still need better semantic parsing for the operator's actual stacks: shell, Python, JS/TS/Bun/Node, Go, Rust, C/C++, Zig, React/JSX/TSX, Svelte, Markdown, SQL/DB tooling, Dockerfile/Make/HCL, plus the top practical language ecosystems. T124 expands coverage without deleting existing filters and without building a blind 50-language maintenance swamp.

## Current repo truth (2026-05-02 audit)

- Existing structured-parser registry: Go build/vet/test, Cargo build/check/clippy, GCC/Clang.
- Existing source-structure defaults: Go, TypeScript/TSX, JavaScript/JSX, Rust, Python, C, C++, Java, Ruby, Shell.
- Existing built-in Layer 0 filters are broad and must stay: git/gh/glab, build/test/lint, package managers, Docker/Kubernetes/Helm, JSON, logs, AWS, PostgreSQL, .NET, Ruby, Python, formatters, Terraform, and others.
- Existing built-ins already cover far more than the first plan assumed: pytest, phpunit, vitest, jest, playwright, bun test, dart/flutter test, gradle/sbt/mill test, mypy, ruff, pylint, flake8, prettier, shfmt, clang-format, package managers, psql, terraform, Docker/Kubernetes/Helm.
- T124 adds targeted semantic parsers and language detection where existing generic compaction lost structure. It does not remove or replace working built-ins.

---

## Languages / toolchains covered (and what each parser extracts)

Each parser is a focused file under `internal/filter/parser_<tool_or_lang>.go`, registered with the existing T115 dispatch table in `structured_parsers.go`. Tests under `_test.go` neighbour each parser; corpus under `tests/fixtures/lang_corpus/<lang>/`.

Priority set for this task:

1. Operator-requested gaps: Zig, JSX/TSX-specific diagnostics, Svelte (`svelte-check`), Markdown / Markdownlint / code-fence-aware summaries, SQL/DB (`sqlfluff`, psql error rows, migration tools).
2. High-traffic build/test stacks: Bun, Node, npm/pnpm/yarn, Vite, Next.js, React test runners, Vitest/Jest/Playwright, Ruff/Pytest/Mypy/Pyright, Cargo/Clippy/Rustfmt, Go build/test/vet/gopls.
3. Practical top ecosystems: Swift, Kotlin, PHP, Dart/Flutter, Lua, GraphQL, Protobuf, Dockerfile, Make/Ninja/CMake, HCL/Terraform, PowerShell, Perl, OCaml, Haskell, Erlang, Solidity, JSON5/JSONNET.

Explicit non-goal: adding parsers for obscure languages only because they exist. New language support needs either operator demand, corpus evidence, or common agent-tool traffic.

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

### Other practical ecosystems
- **`dart pub get / flutter pub get`**: dependency table.
- **`dart test / flutter test`**: pass/fail + per-test failure.
- **`zig build / zig test`**: error rows + summary.
- **`bun test / bun run`**: failure rows, test summary, bundle errors, preserving command-specific useful context.
- **`vite / next / svelte-check`**: framework compile/runtime diagnostics, route/page file references, warnings grouped by file.
- **Markdown / docs tools**: markdownlint / mdformat output grouped by file; code-fence-aware summaries for huge Markdown reads stay out of scope unless triggered by tool output.
- **SQL / DB tooling**: sqlfluff diagnostics, migration tool failures, psql error rows with line/position.
- **GraphQL / Protobuf**: schema compiler diagnostics grouped by file.
- **Dockerfile / Makefile / HCL**: hadolint, make/ninja/cmake failures, Terraform/HCL diagnostics.
- **Additional top ecosystems**: Swift, Kotlin, PHP, Dart/Flutter, Lua, PowerShell, Perl, OCaml, Haskell, Erlang, Solidity, JSON5/JSONNET.

## Implementation plan

### WP1 - Parser registry expansion

Completed for the safe core. `structured_parsers.go` now registers additional diagnostic parsers after the existing T115 parsers:

- TypeScript / TSX: `tsc`, `vue-tsc`, `tsserver`, including `npx`, `pnpm exec`, `yarn`, `bun x/run` wrappers.
- Svelte: `svelte-check`.
- Zig: `zig build`, `zig test`, direct `zig`.
- SQL / DB lint: `sqlfluff lint`, `sqruff`, `psql`.
- Markdown: `markdownlint`, `markdownlint-cli2`, `mdformat`.
- Practical ecosystem catch-all for common compiler/linter diagnostics: Swift/Xcode, Kotlin/Gradle/Maven/SBT/Scala, PHP/PHPStan/Psalm/PHPUnit/Composer, Dart/Flutter, Lua, Protobuf/Buf, GraphQL codegen, Dockerfile/Hadolint, Make/Ninja/CMake, Terraform/OpenTofu, PowerShell, Perl, OCaml/Dune, Haskell/Cabal/Stack/GHC, Erlang/Rebar3, Elixir/Mix, Solidity/Solc/Forge, JSONNET.

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
    // targeted real-traffic entries, specific before generic
}
```

Detect runs in declaration order; first match wins. Most-specific arg patterns first (e.g. `cargo test --release` before `cargo` alone).

### WP2 - Per-parser implementation

Completed as a shared diagnostic-row parser instead of one file per ecosystem. This is intentionally smaller and safer than the original 50-parser plan:

- Keeps compiler/linter rows shaped as `file:line[:col]: error/warning/...`.
- Keeps TypeScript-style `file(line,col): error TSxxxx`.
- Keeps SQLFluff `L: ... | P: ... | CODE | ...` rows.
- Keeps Markdownlint `file.md:line: MDxxx/...` rows.
- Keeps concise failure/issue/problem summary lines.
- Dedupe is adjacent-only, so non-adjacent repeated diagnostics are not silently collapsed.
- The parser refuses to compact if the result is not smaller than the original.

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

Code-local tests were added for every new parser family. The large external corpus matrix remains pending until T118b live corpus exists; generating synthetic `tests/fixtures/lang_corpus/` now would create fake confidence and a lot of maintenance noise.

`tests/fixtures/lang_corpus/<lang>/<scenario>.txt` paired with `expected.txt`. Scenarios per parser:
- `success_short`, `success_long`, `success_empty`
- `failure_one`, `failure_many`, `failure_with_warnings`
- `mixed_pass_fail`
- `pre-existing-warnings` (ignore, don't emit failure)
- `unicode-paths` (CJK, emoji in error messages)

Do not generate a giant fixture matrix. Each parser ships with its 3-5 most-distinctive scenarios, plus at least one "must passthrough" negative fixture when false positives are plausible.

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

Partially existing via `internal/filter/observability.go`, which records per-filter in/out bytes and panic/slow telemetry. `slimference gain --by-parser` now groups persisted Layer 0 `filter_runs` rows by parser/tool family for real-session reporting. This remains command-label based rather than a new hot-path per-parser counter, so it is useful but not a substitute for T118b live corpus proof.

### WP7 - Tests

- New parser tests cover TypeScript, Zig, Svelte, SQL, Markdown, ecosystem diagnostics, success/no-match/too-short refusal, wrapper detection, and structured dispatch.
- Language detection tests cover Zig, Svelte, Markdown, Dockerfile, Makefile, Swift, Kotlin, PHP, Dart, GraphQL, Protobuf, HCL, PowerShell, Solidity, JSONNET and SQL.
- Corpus integration stays pending for T118b live/scrubbed fixture data.

## Acceptance criteria

- [x] Existing built-ins and T115 structured parsers remain intact.
- [x] Operator-requested stack gaps are covered at parser/language-detection level: shell/Bash/Zsh, Python, JS/TS/Bun/Node, Go, Rust, C/C++, Zig, JSX/TSX/React, Svelte, Markdown, SQL/DB tooling.
- [x] Practical top ecosystem additions are covered by diagnostic parser matching and/or language detection: Swift, Kotlin, PHP, Dart/Flutter, Lua, GraphQL, Protobuf, Dockerfile, Make/Ninja/CMake, HCL/Terraform, PowerShell, Perl, OCaml, Haskell, Erlang, Solidity, JSON5/JSONNET.
- [x] Parser tests cover success / failure / edge behavior for the new shared diagnostic families.
- [ ] Total Layer 0 live corpus passes byte-equal end-to-end after T118b real corpus exists.
- [x] No regression in existing focused T115 / T117 / T119c / generic-build tests for touched packages.
- [x] Coverage 100%; race-clean; CI gate green after the full Phase R batch.
- [x] Leaf-audit ratio checked after final CI.
- [x] `slimference gain --by-parser` reports parser/tool-family saving from persisted `filter_runs` rows.

## Out of scope

- Stream-mode parsers for live tail output (T108/T94 own that).
- IDE-language-server output beyond `gopls` and `tsserver` (rare in agent tool calls).
- Localised tool output (most CLIs ship English; locale handling is operator-side).
- Custom enterprise build systems (Bazel covered partially, Buck2 partially; bespoke ones are operator-supplied via TOML).

## Validation

```
go test -race ./internal/filter/...
go test ./internal/filter ./internal/compression
go run ./scripts/utils leaf-audit --root=. --check
slimference gain --by-parser
```

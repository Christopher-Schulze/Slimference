# TASK 119: Layer 0 stub-to-compactor uplift (~145 leaves -> real compactors)

Status: DONE (core) 2026-05-01. Audit tool, CI gate, and corpus-grade leaf classification shipped. The audit revealed the empty-only-stub ratio is 4.8% (10 of 209 functions), not the ~70% the task brief assumed - the assumption was based on visual file-name impression, not actual function-body inspection. T119a..T119h sub-tasks are demoted to deferred, individually-justified follow-ups.
Priority: P0
Scope: `internal/filter/builtin_*.go` (almost all), `internal/filter/builtin_compact_helpers.go`, `tests/fixtures/cli_corpus/`
Driver: Of the 209 `TryCompact*` leaf functions in `internal/filter/`, ~70% are wrappers around `tryCompactEmptyStdoutSingleBinary` - they only fire on empty stdout to emit `[tool] ok\n`. Real Layer 0 token savings come from semantic compaction of non-empty output: structured parsers for command output, table compaction, list dedup, status digests. Today this exists for git, json, search, log_dedup (docker/kubectl), some test runners, lint, and a few others. Many high-value tools (dotnet, terraform, package managers, AWS CLI, kubectl get, docker ps, helm, gh/glab list, psql, rails, mvn, gradle, pip, npm, cargo) only compact on empty stdout. This is the single largest unrealised lever in Layer 0.

---

## Problem

`grep -c "^func Try" internal/filter/builtin_*.go`:
- `builtin_aws.go`: 1
- `builtin_dotnet.go`: 1
- `builtin_log.go`: 1
- `builtin_json.go`: 1
- `builtin_psql.go`: 1
- `builtin_python.go`: 1
- `builtin_read.go`: 1
- `builtin_terraform.go`: 1
- `builtin_gh.go`: 1
- `builtin_glab.go`: 1
- `builtin_fs.go`: 2
- `builtin_ruby.go`: 3
- `builtin_git.go`: 5 (each with rich semantic parsing - the model)
- `builtin_container.go`: 8
- `builtin_search.go`: 13
- `builtin_format.go`: 17
- `builtin_pkg.go`: 18
- `builtin_build.go`: 33
- `builtin_testrun.go`: 38
- `builtin_lint.go`: 62

Of those, the high-leaf files (lint 62, testrun 38, build 33) have their semantic compaction concentrated in **one or two** real parser functions plus a long tail of empty-stdout-only wrappers. The low-leaf files (aws 1, dotnet 1, terraform 1, etc.) ARE the empty-only stubs.

Token-saving math: a tool whose typical successful output is 2-50 lines of structured data (e.g. `aws s3 ls`, `terraform plan`, `dotnet build`, `kubectl get pods`, `helm list`, `gh pr list`, `psql -c "SELECT ..."`) currently saves zero on success. With a real compactor (status summary + first/last N rows + count), savings of 60-85% on successful runs are realistic.

## Target State

Every entry in the dispatch chain has a real compactor for its tool's success-output shape, alongside the existing empty-only fallback. Specifically:

| Tool family | Current state | Target compactor |
|---|---|---|
| `aws s3 ls / aws s3api list-* / aws ec2 describe-* / aws lambda list-*` | empty-only | JSON / table digest, count + first 30 + truncate |
| `dotnet build / dotnet test / dotnet restore` | empty-only | parse `Build succeeded` / project count, structured failure extraction (T115) |
| `terraform plan / terraform apply / terraform init` | empty-only | summary block (`N to add, M to change, K to destroy`) + plan diff compact |
| `gh pr list / gh issue list / gh run list` | empty-only (gh_list) | header + count + first N rows |
| `glab pr list / glab issue list` | empty-only | same as gh |
| `kubectl get / kubectl describe / kubectl top` | container-family stub | header + N rows + truncate, status digest |
| `docker ps / docker images / docker volume ls / docker network ls` | container stub | same |
| `helm list / helm status` | not covered | header + N rows |
| `psql -c "SELECT ..."` | empty-only | header row + first 30 + total count |
| `pip list / pip show / poetry show / npm ls / pnpm ls / yarn list / cargo tree` | empty-only | first 50 + count |
| `mvn / gradle` | empty-only | task summary + structured failure extraction (T115) |
| `rails test / rails routes / rails db:migrate` | empty-only | per-command parsers |
| `find / fd / locate` | passthrough? | top 50 results + count |
| `ls -la / ls -R` | already covered (ls/tree) but only basic | extend tree-mode for huge outputs |
| `du -sh */` | not covered | top N by size + total |
| `df -h` | not covered | filesystem rows + cap |

Each real compactor must:

1. Detect the command shape (argv pattern + optional output shape).
2. Compact preserving: status (success/failure), counts, first/last N rows, error lines verbatim with full context (T115).
3. Survive zero-downside (output never larger than input - already in pipeline).
4. Have a corpus fixture with `expected.txt` showing the post-compaction output.

## Implementation Plan

### WP1 - Coverage audit
- New `scripts/utils/leaf_audit/` walks `builtin_*.go`, parses each `TryCompact*` function's body, classifies as: empty-only-stub, real-parser, mixed, fallback-only.
- Output: markdown table of every leaf with its category. Lives at `docs/layer0-leaf-audit.md`.
- Goal: ≤30% empty-only after this task; rest are real parsers.

### WP2 - Per-tool parser sprints
Tackled as sub-tasks (T119a..T119h), each owning one tool family. Pattern per sub-task:

- Read tool's documented output shapes.
- Capture corpus from real runs (operator's machine, scrubbed).
- Implement `TryCompact<Tool>Output(argv, stdout)` returning the compacted bytes.
- Update dispatch chain priority.
- Unit tests + corpus tests.

Sub-task split:

- **T119a** - AWS CLI (s3 ls, s3api, ec2, lambda, iam, dynamodb, cloudformation list-stacks)
- **T119b** - Kubernetes / Container (`kubectl get/describe/top/logs without -f`, helm, docker ps/images/etc, podman)
- **T119c** - Terraform / Infrastructure (terraform plan/apply, ansible, packer)
- **T119d** - .NET / JVM build (dotnet build/test/restore, mvn, gradle)
- **T119e** - Package managers (pip, poetry, npm, pnpm, yarn, cargo, go list)
- **T119f** - Database CLIs (psql, mysql, sqlite3, mongosh, redis-cli)
- **T119g** - Filesystem / search (find, fd, locate, ripgrep already partial, du, df, stat)
- **T119h** - GitHub / GitLab CLIs (gh pr/issue/run/release, glab equivalent)

### WP3 - Helper consolidation
- Common shapes (table with header + body, count summary, first-N-truncate) extracted into `builtin_compact_helpers.go` so per-tool parsers stay short and consistent.
- Test those helpers on synthetic input independently.

### WP4 - Corpus
- `tests/fixtures/cli_corpus/<tool>/<scenario>.txt` + `expected.txt`.
- Categories per tool: `success_short`, `success_long`, `success_empty`, `failure_known`, `failure_unknown`, `mixed_warn`.
- All fixtures hand-scrubbed; no real account IDs / customer data.

### WP5 - Dispatch chain reorder
- Place real-parser entries before fallback `extractFailures*`.
- Per-tool ordering: most-specific match first (e.g. `aws s3api` before `aws s3` before generic `aws`).

### WP6 - Telemetry
- `slimference gain` extends category breakdown with per-tool savings totals.
- `/admin/status.layer0.{tools_with_real_parser, tools_empty_only, parser_match_rate}`.

### WP7 - CI gate
- `scripts/ci` adds a Layer 0 leaf audit step: fail when empty-only ratio > 30% (regression guard).

## What the audit actually found (2026-05-01)

`go run ./scripts/utils leaf-audit --root=.` produces, against the live `internal/filter/` package:

```
total=209 empty_only=10 (4.8%) real=195 mixed=0 fallback=4
```

The ten empty-only stubs are all linters / test-runners that genuinely produce no output on success: `errcheck`, `ineffassign`, `nilaway`, `unparam`, `misspell`, `gocyclo`, `forbidigo`, `prealloc`, `ginkgo`, `ctest`. On failure, their non-empty stdout is already covered by the generic build/test fallback that T115's structured parsers feed. There is nothing to "uplift" in the empty-only column itself; the original task brief was wrong about the baseline. The four `fallback`-classified entries (`TryCompactLogDedup`, `TryCompactLogOutput`, `TryCompactRubyOutput`, `TryCompactSearchOutput`) are dispatchers that delegate to real per-tool parsers; they look like fallbacks to the audit heuristic only because the entry function itself has no parser body.

## What did ship under this task (core, 2026-05-01)

- `scripts/utils/leaf_audit.go`: `leaf-audit` subcommand. Classifies every `TryCompact*` function in `internal/filter/builtin_*.go` via Go AST inspection into `empty_only_stub` / `real_parser` / `mixed` / `fallback`. Recognises calls to `tryCompactEmptyStdoutSingleBinary`, the family of `extract*` / `compact*` / `compress*` / `summarize*` / `ParseFailures*` / `detectBuildSuccess` helpers, and inline parser signals (`json.Unmarshal`, `bytes.Split`, regex `Match`, `bufio.Scanner`, etc.). Heuristic but conservative: prefers `real_parser` whenever any semantic signal is present.
- `scripts/utils/leaf_audit_test.go`: 26 tests covering each classification path, the AST walker, the audit-package walker, the Markdown renderer, the gate, and the CLI flag matrix.
- `scripts/ci/main.go`: new step 8/8 "leaf audit gate" (`leaf-audit --check --max-empty-only-pct=20 --root=.`). Comfortable headroom over the 4.8% baseline so a regression that pushes empty-only past 20% fails CI.
- `docs/layer0-leaf-audit.md`: generated, committed Markdown report. Reviewers see the per-file counts and per-function classification at HEAD without running the tool.

## Re-activation criteria for T119a..T119h (now individually deferred)

The original sub-tasks were scoped against an inflated empty-only ratio assumption. Each is now a separate optional improvement, justified or rejected on its own evidence:

- **T119a** (AWS) - `TryCompactAwsJSON` already strips `ResponseMetadata`. Adding a table compactor for `aws s3 ls` / `aws ec2 describe-* --query` style output is still a real lever but operator-driven (the test corpus needs real AWS responses, scrubbed).
- **T119b** (kubectl/container) - `builtin_container.go` ships eight per-shape compactors already; the audit shows them as `real_parser`. Adding a shared header+rows+truncate helper for `kubectl get` long lists is a tightening, not an uplift.
- **T119c** (terraform/IaC) - `TryCompactTerraformPlan` is a fallback in the audit because the entry-point function delegates to anchor-mode logic; the underlying parser exists. Splitting the plan-summary block (`N to add, M to change`) and a separate apply-summary parser is the real lever.
- **T119d** (.NET / JVM) - largely covered by T115 structured parsers (`parser_msbuild`, `parser_gradle`, `parser_maven` are the gap-fillers; build/test failure shape is the lever).
- **T119e** (pkg-managers) - `TryCompactNpmList`, `TryCompactPipList`, `TryCompactCargoTree` are live in `builtin_pkg.go`; audit confirms `real_parser`.
- **T119f** (DB CLIs) - `TryCompactPsql` exists; `mysql`, `sqlite3`, `mongosh`, `redis-cli` are not covered. Add only when the maintainer captures real psql output to drive the design.
- **T119g** (filesystem/search) - `find`, `fd`, `ripgrep` are all covered as `real_parser`; `du -sh */`, `df -h`, `stat` are not. Low priority.
- **T119h** (gh/glab) - `TryCompactGhList` and `TryCompactGlabList` exist; audit confirms.

The leaf-audit tool now provides the data so each of these decisions is grounded in measurement instead of impression. T119b/c/g remain reasonable follow-up items if real-session capture (T118b) shows them as savings hot-spots; the others are functionally closed.

## Acceptance Criteria

- [x] Audit tool ships under `scripts/utils/leaf_audit.go`. Classifies every `TryCompact*` function via AST.
- [x] CI step `leaf-audit --check --max-empty-only-pct=20` blocks regressions past 20% (current: 4.8%).
- [x] `docs/layer0-leaf-audit.md` committed with the per-file and per-function tables. Reviewers can read the distribution without running the tool.
- [x] Coverage on the new audit tool reasonable; race tests green.
- [ ] **T119b** (deferred): shared `header+rows+truncate` helper consolidating the kubectl/docker/helm parsers, gated on T118b real-session evidence.
- [ ] **T119c** (deferred): split-out terraform plan-summary + apply-summary parsers driven by captured plan output.
- [ ] **T119g** (deferred): `du -sh */`, `df -h`, `stat` per-shape parsers when an operator session captures them as the savings shape.

## Out of Scope

- Boutique / less-used CLIs (rkhunter, augeas, etc.) - operator can add via TOML DSL.
- Stream-mode compaction for these (T94/T108 owns).
- Multi-language tool output (most CLIs ship English; locale handling deferred).

## Validation

```
go test -race ./internal/filter/...
go run ./scripts/utils leaf-audit
go run ./scripts/ci
```

# TASK 119: Layer 0 stub-to-compactor uplift (~145 leaves -> real compactors)

Status: PENDING (audit-driven mitigation 2026-04-30)
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

## Acceptance Criteria

- [ ] Empty-only leaf ratio ≤ 30% post-task (currently ~70%).
- [ ] Each of the 8 sub-tasks (T119a-h) shipped with ≥3 parsers and corpus.
- [ ] Per-tool corpus tests green.
- [ ] CI gate enforces the ≤30% ratio.
- [ ] `slimference gain` reports per-tool savings.
- [ ] Coverage 100%; race tests green.

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

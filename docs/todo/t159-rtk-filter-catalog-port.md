# TASK 159: Port RTK filter catalog (60 TOMLs) for L0 parity

Status: TODO (planning 2026-05-15)
Priority: P1
Scope: `internal/filter/builtins_toml/` (new), `internal/filter/`, `tests/fixtures/`, `docs/documentation.md`, `NOTICE.md` (new)

## Why

RTK ships 60+ embedded TOML filters covering ecosystems we currently do not (`gradle`, `mvn-build`, `dotnet-build`, `xcodebuild`, `swift-build`, `terraform-*`, `helm`, `gcloud`, `ansible`, `gcc`, `basedpyright`, `ty`, `biome`, `oxlint`, `hadolint`, `markdownlint`, `yamllint`, `shellcheck`, `make`, `just`, `task`, `turbo`, `nx`, `mise`, `mix-*`, `brew-install`, `composer-install`, `poetry-install`, `uv-sync`, `ollama`, `jj`, `jq`, `pre-commit`, `quarto-render`, `liquibase`, `fail2ban-client`, `iptables`, `systemctl-status`, `df`, `du`, `stat`, `ps`, `ping`, `rsync`, `ssh`, `skopeo`, `sops`, `shopify-theme`, `pio-run`, `trunk-build`, `jira`, `yadm`, and more). Our 8-stage TOML DSL (`internal/filter/filters_toml.go`) is feature-compatible with RTK's filter format. Porting is mechanical translation, not new design. Filter catalog depth is the single largest L0 gap vs. RTK and the most user-visible.

**Why:** L0 token savings are proportional to ecosystem coverage. Every unported filter = a tool whose raw output blasts the context. RTK has done the empirical work of identifying the 60 highest-value commands; reuse it.
**How to apply:** All ports embed via `//go:embed`, are gated by a built-in priority list (built-ins > TOML > passthrough), and ship with the same snapshot fixtures RTK uses (MIT-licensed input/output pairs).

## Target State

1. New directory `internal/filter/builtins_toml/` with one `.toml` file per filter, structure identical to RTK's `src/filters/*.toml`.
2. Files embedded at build time via `//go:embed builtins_toml/*.toml` into `internal/filter/builtins_loader.go`.
3. Dispatch order in `internal/filter/pipeline.go`:
   1. ANSI strip (`compression.StripANSICodes`)
   2. Built-in Go compactors (`TryCompactGitStatus` etc.) — highest specificity wins
   3. Embedded TOML filters (the 60 ports) — pattern-matched by command name
   4. User TOML filters (`~/.slimference/filters.toml`, project `.slimference/filters.toml`)
   5. Truncate fallback (`TruncateStdoutWithHint`)
4. Each ported filter has at minimum one fixture pair under `tests/fixtures/builtins_toml/<filter>/{input.txt,expected.txt}` and one Go snapshot test in `internal/filter/builtins_toml_snapshot_test.go` table-driven over all filter directories.
5. `NOTICE.md` at repo root credits RTK (MIT) for the filter catalog and references the upstream commit hash that was ported.
6. Telemetry: `filter_runs` SQLite row records which filter source matched (`builtin_go` | `embedded_toml:<name>` | `user_toml:<name>` | `passthrough`) so analytics can show coverage per session.

## Acceptance

- 60 filters from `research/rtk-ai/rtk/src/filters/*.toml` ported with byte-identical semantics on the RTK fixtures.
- All snapshot tests green; running `go test ./internal/filter/...` passes.
- `slimference filter -- <cmd>` for any of the 60 covered commands compacts output without panics on real-world runs (manual smoke-test list in the task notes).
- `gain` analytics surface per-filter coverage and savings.
- `NOTICE.md` present, MIT attribution clean.
- 100% statement coverage maintained on `internal/filter/`.

## Sub-Tasks

- [ ] Author `internal/filter/builtins_toml_loader.go` with `//go:embed` and lazy-once parsing.
- [ ] Extend `internal/filter/pipeline.go` to insert embedded-TOML stage between built-ins and user-TOML.
- [ ] Comment-strip parity audit: enumerate RTK's 38 supported source-file languages in `research/rtk-ai/rtk/src/core/filter.rs` (or wherever language tables live), diff against our `internal/compression/comment_strip.go` extension list, add the missing languages with the same comment-syntax rules. Snapshot tests per language under `tests/fixtures/comment_strip/<lang>/`.
- [ ] Language-aware AST-style filtering audit: examine RTK's `src/core/filter.rs` (Rust/Python/JS/Go aware filtering) and verify our `internal/compression/structure.go` reaches equivalent semantic coverage. Document gaps in this task's Notes; spin out follow-up tasks per language if needed.
- [ ] Port batch 1 — P0 build/test/lint (12 filters): `gradle`, `mvn-build`, `dotnet-build`, `xcodebuild`, `swift-build`, `gcc`, `basedpyright`, `ty`, `biome`, `oxlint`, `hadolint`, `make`.
- [ ] Port batch 2 — P0 infra/deploy (10 filters): `terraform-plan`, `tofu-{fmt,init,plan,validate}`, `helm`, `gcloud`, `ansible-playbook`, `kubectl-*` (if RTK has).
- [ ] Port batch 3 — P0 quality/format (8 filters): `markdownlint`, `yamllint`, `shellcheck`, `pre-commit`, `prettier-*` if missing, `eslint-*` if missing, `clippy-*` if missing, `ruff-*` if missing.
- [ ] Port batch 4 — P0 dev tooling (10 filters): `just`, `task`, `turbo`, `nx`, `mise`, `mix-compile`, `mix-format`, `quarto-render`, `liquibase`, `jq`.
- [ ] Port batch 5 — P1 system/network (10 filters): `df`, `du`, `stat`, `ps`, `ping`, `rsync`, `ssh`, `iptables`, `systemctl-status`, `fail2ban-client`.
- [ ] Port batch 6 — P1 package managers (5 filters): `brew-install`, `composer-install`, `poetry-install`, `uv-sync`, `bundle-install`.
- [ ] Port batch 7 — P1 misc (5 filters): `jj`, `sops`, `skopeo`, `ollama`, `shopify-theme`.
- [ ] Copy RTK fixtures into `tests/fixtures/builtins_toml/<filter>/` (MIT, attribution in `NOTICE.md`).
- [ ] Add `internal/filter/builtins_toml_snapshot_test.go` (table-driven over all filter dirs).
- [ ] Telemetry: extend `filter_runs` schema with `filter_source` column; migrations via `internal/filter/db_migrate.go`.
- [ ] Update `docs/documentation.md` with the full filter catalog (alphabetical table).
- [ ] Write `NOTICE.md` with MIT attribution and RTK commit hash.

## Notes

- Per `AGENTS.md` §2, `research/rtk-ai/rtk/` is read-only. We do not modify it; we copy filter `.toml` content and fixture content into our own tree.
- License: RTK is MIT; copying TOML + test fixtures is allowed with attribution.
- Some filter names may collide with Go built-ins (e.g., we already have `git-status` as Go built-in). Go built-ins win — TOML port is only registered if no Go built-in matches.
- Parser-tier integration: when t160 lands, each filter declares its `tier = 1|2|3` in TOML metadata so the runtime can fall back cleanly.

## Deviations

(none yet)

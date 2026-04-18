# RTK-Master Parity Audit (T18)

Date: 2026-04-18
Scope: `rtk-master/` vendored reference implementation vs. Slimference Go port.

Purpose: decide what to port, what we already have, what is out of scope,
and clear the folder from the repository.

---

## Scale

| | RTK (Rust) | Slimference (Go) |
|---|---|---|
| Production SLOC | ~23 500 | ~68 000 (includes HTTP proxy + TUI + MiniMax) |
| TOML filter rules | 58 | TOML DSL + 26 built-in compactors (now incl. T25 Python + Terraform) |
| Hook files | 11 | 3 (claude, codex, verify) |
| Tests | Implicit via `cargo test` | 100 % statement coverage enforced |

Scope difference: Slimference is a **superset** on everything that is not
Layer-0 filtering. RTK has deeper Layer-0 specialisation; Slimference has
the HTTP proxy, 3-layer compression pipeline, MiniMax summarisation,
response cache, TUI, operating modes, prompt-cache metrics.

---

## What RTK has that we did NOT have -> ported

| RTK module | Status | Note |
|---|---|---|
| `hooks/trust.rs` (504 LoC) | Ported as `internal/filter/trust.go` | Security-critical: prevents malicious repo-committed filters.toml. CLI `slimference trust {add,list,remove,status}`. |
| `filters/terraform-plan.toml` + tofu variants | Ported as `internal/filter/builtin_terraform.go` | See T25. |
| Python traceback heuristic (cmds/python + cmds/system/summary) | Ported as `internal/filter/builtin_python.go` | See T25. |

---

## What RTK has that we already had, often better

| RTK module | Slimference equivalent | Note |
|---|---|---|
| `core/runner.rs`, `core/tee.rs`, `core/tracking.rs`, `core/telemetry.rs`, `core/toml_filter.rs` | `internal/filter/{engine,pipeline,tee,tracking,filters_toml}.go` | Equivalent. We use `modernc.org/sqlite` so no CGO. |
| `hooks/init.rs` (3 103 LoC), `hooks/hook_cmd.rs`, `hooks/verify_cmd.rs` | `internal/hooks/{claude,codex,verify}.go` + `slimference hook {install,remove,verify}` | Smaller, contract-hardened (T12, T33). |
| `hooks/integrity.rs` | `internal/hooks/verify.go` + T33 drift watchdog | Equivalent SHA-256 checks. |
| `hooks/permissions.rs` | `internal/filter/permissions.go` | Equivalent deny-list + sudo-ask model. |
| `analytics/gain.rs` | `internal/analytics/gain.go` + `slimference gain` | Equivalent. |
| `analytics/cc_economics.rs` | T23 `CacheReadTokens` + `CacheCreateTokens` in AnalyticsEvent / RequestMetrics | We expose the same upstream data; RTK renders more economic formulas, we leave that to external tooling. |
| `parser/*.rs` (ANSI strip, formatting) | `internal/compression/ansi_strip.go` + `internal/filter/builtin_format.go` | Equivalent. |
| `cmds/*/` specialised handlers (go, rust, python, ruby, js, dotnet, cloud, system) | `internal/filter/builtin_{build,test,lint,search,format,log,pkg,container,git,ls,read,json,aws,psql,gh,glab,ruby,dotnet}.go` + generic `TryCompact{Build,Test,Lint,Package}Output` | Slimference collapses dozens of RTK-specialised binaries into generic category filters (build/test/lint) that detect the tool dynamically. Same coverage, fewer files. |

---

## What RTK has that is intentionally OUT of scope

| RTK module | Decision | Reason |
|---|---|---|
| `discover/` (~4 400 LoC) | Not ported | Scans Claude Code sessions for commands that would benefit from a new TOML rule. Advisory / research feature, not token-saving in the hot path. Users can do this ad hoc with `slimference stats`. |
| `learn/detector.rs` (629 LoC) + `learn/report.rs` | Not ported | Detects CLI error patterns (unknown flag, command not found, wrong syntax) and suggests corrections. Dev-productivity feature, orthogonal to Slimference's mission. |
| `hooks/hook_audit_cmd.rs` | Not ported | Historical audit log of every rewrite. Nice-to-have. `slimference debug tail` + `daemon logs` (T30) already give comparable visibility. |
| `openclaw/` subdirectory | Not ported | Separate companion tool. |
| 48 niche TOML filters (`ansible-playbook`, `fail2ban-client`, `iptables`, `jira`, `jj`, `shopify-theme`, `yadm`, ...) | Not ported | Long tail. Covered generically by our build/lint/format/log dispatchers; users add their own via `~/.slimference/filters.toml` when needed. |

---

## What Slimference has that RTK doesn't

Slimference scope is intentionally larger than RTK. The following are
entirely outside RTK:

- HTTP reverse proxy with 3-layer compression pipeline (L1 deterministic,
  L2 async MiniMax summarisation with anchors/priority/staircase, L3
  response cache with Stage A/B double keying - T20).
- Prompt-cache metrics, both injection (OptimizeCacheBreakpoints) and
  measurement (T23 `cache_read_input_tokens` aggregation).
- Operating modes (T36: strict / balanced / fast) with explicit
  precedence rules.
- TUI (BubbleTea) with live metrics, state persistence (T31), hook
  status, and toggles.
- Daemon service (launchd) with `daemon logs` subcommand (T30).
- Hook-drift watchdog (T33).
- Bash completion (T32).
- Tuning staircases (T26 repetition, T27 incremental overlap, T36 L2 modes).
- Overflow recover that is guaranteed to never call MiniMax synchronously
  (T21).

---

## Closure

- All RTK components that add unique, in-scope value have been ported.
- The `rtk-master/` folder has no further dependency in the Slimference
  code or tests. It is removed from HEAD and `.gitignore`d to prevent
  accidental re-add. History retains it in the initial commit.
- Any future interest in the non-ported modules (`discover/`, `learn/`)
  can consult git history at commit cb78774 or earlier.

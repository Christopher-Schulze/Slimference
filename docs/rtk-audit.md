# RTK Current Delta Audit (T211)

Date: 2026-05-17
Scope: current embedded RTK snapshot at `research/rtk-ai/rtk/` vs.
Slimference Go implementation.

Status: current. The older T18 audit below is historical and references the
removed `rtk-master/` path.

## Executive Result

RTK still wins on one thing conceptually: Claude Code adoption ergonomics.
Its Claude hook rewrites Bash `PreToolUse` with `updatedInput` and its docs
make the wrapper obvious. Slimference now matches or exceeds the equivalent
compression layer for Codex because it has the RTK TOML catalog, broader Go
compactors, proxy-side HTTP/WSS mutation, and Codex tool-output Layer-0
adoption.

No RTK compression rule is missing from Slimference's bundled TOML catalog:
both trees currently contain 59 `.toml` filter files, and the filename diff is
empty. RTK's 61 Rust `src/cmds/**/*.rs` command files map to Slimference's 47
Go `builtin_*.go` files plus generic build/test/lint/search/package/container
dispatchers.

## Current Matrix

| RTK capability | Evidence | Slimference status | Classification |
|---|---|---|---|
| Built-in TOML filters | `research/rtk-ai/rtk/src/filters/*.toml` = 59; `internal/filter/builtins_toml/*.toml` = 59; filename diff empty | Byte-for-byte catalog class is present under Slimference-owned tree | parity |
| Git, gh, glab, cargo, JS/TS, Python, Ruby, .NET, cloud, system command handlers | RTK `src/cmds/**`; Slimference `internal/filter/builtin_*.go` + category dispatchers | Slimference has fewer files but broader generic dispatch plus tests | already-better |
| Claude Bash `PreToolUse.updatedInput` rewrite | RTK `hooks/claude/rtk-rewrite.sh`; Slimference `internal/hooks/claude.go` | Slimference retains reference code, but product entrypoints are parked by T217. Use RTK for Claude Code now. | parked |
| Claude PostTool output replacement | RTK does not ship a dedicated `updatedToolOutput` posttool path in the inspected snapshot | Slimference retains `claudeposttool` handler code for reference, but the public command is not exposed in product mode. | parked |
| Codex adoption | RTK `hooks/codex/rtk-awareness.md` is instruction-based | Slimference has Codex hooks plus transparent SNI MITM, HTTP Responses mutation, WSS Phase-F mutation, and proxy Layer-0 tool-output adoption | already-better |
| Rewrite compound operators, pipes, env prefixes, absolute paths | RTK `src/discover/registry.rs` tests cover compounds, pipes, env prefixes, absolute paths, git global options | Slimference covers compounds, pipes, env prefixes, absolute paths via `filepath.Base`, explicit opt-out; lacks RTK transparent-prefix config | parity with one port-later gap |
| Transparent wrapper prefixes (`shadowenv exec --`, `direnv exec .`, `docker exec app`) | RTK `transparent_prefixes` registry support | Slimference does not expose configurable transparent prefixes | port-later |
| Explicit disabled env var | RTK `RTK_DISABLED=1`; Slimference `SLIMFERENCE_DISABLED=1` | Equivalent local opt-out under Slimference naming | parity |
| Read/Grep/Glob/LS built-in non-Bash tools | RTK README states Claude built-in Read/Grep/Glob bypass Bash hook | Slimference has Claude Read hook only; Grep/Glob/LS remain unimplemented until verified profitable/contract-stable | port-later |
| Observability | RTK SQLite tracking and gain; Slimference filter.db, gain/savings, admin state, WSS counters | Slimference has more surfaces, especially proxy/WSS counters | already-better |
| Fail-open | RTK raw proxy, tee, hook version guard; Slimference tee, panic guards, timeouts, schema-drift byte bridge, daemon lifecycle revert | Slimference stronger for Codex live traffic | already-better |
| Discover/learn/advisory tooling | RTK `discover/` and `learn/` | Not hot-path savings; Slimference has stats/gain and T210/T211 docs | not-needed |

## Port Queue

| Item | Decision | Reason |
|---|---|---|
| Claude `PostToolUse.updatedToolOutput` replacement | Parked by T217 | Useful reference code, but not part of the Slimference product path while RTK handles Claude Code. |
| Wrapper help/completion truth | Done in T214 | Keeps wrapper excellent but explicitly advanced-only; removed unimplemented flags from help/completion. |
| DoH fallback and status preflight | Done in T215 | RTK has no equivalent transparent-MITM self-loop problem; Slimference needed this for T209. |
| Configurable transparent rewrite prefixes | New future gap | Useful for Claude wrapper ergonomics (`shadowenv`, `direnv`, selected `docker exec`), but not needed for Codex CLI live certification and can alter command semantics if rushed. |
| Claude Grep/Glob/LS dedicated hooks | Future Claude phase | Built-in tools bypass Bash. Add only after local Claude hook payloads are verified with real examples. |
| RTK discover/learn | Do not port now | Advisory productivity feature, not core token-saving layer. |

## Answer To The RTK Question

RTK's "up to 3x" claim comes mainly from adoption position, not a magic
compressor Slimference lacks. It filters command output before the agent sees
it. Slimference now has the same class of Layer-0 filters, the complete RTK
TOML catalog, stronger Codex proxy/WSS mutation, and stronger fail-open
guardrails. For Claude Code, RTK's existing transparent Bash rewrite is the
active recommended path. Slimference keeps Claude experiments in tree, but T217
parks all public activation paths so the product can focus on Codex
CLI/Desktop.

For Codex CLI, RTK cannot replace Slimference's transparent MITM approach:
Codex does not reliably honor `updatedInput`, and Codex Desktop/WSS traffic is
not controllable by a shell wrapper. The correct Codex-max path remains
Slimference's two surfaces: Codex hooks for lifecycle/tool signals plus
transparent MITM for HTTP/WSS request mutation.

---

# Historical RTK-Master Parity Audit (T18)

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

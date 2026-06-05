# RTK Current Delta Audit (T211)

Date: 2026-06-06
Scope: current RTK upstream snapshot `0a630fe`, embedded read-only reference
at `research/rtk-ai/rtk/`, Codex CLI `0.137.0`, and Slimference Go
implementation.

Status: refreshed for the post-Layer-2 product. The older T18 audit below is
historical and references the removed `rtk-master/` path.

## Executive Result

RTK still wins on Claude Code adoption ergonomics. Its Claude hook rewrites
Bash `PreToolUse` with `updatedInput`, and its docs make wrapper usage
obvious. For Codex it does not have an equivalent programmatic hook path:
current RTK Codex support is prompt-level awareness. Slimference matches or
exceeds the in-scope compression layer for Codex because it has the RTK TOML
catalog, broader Go compactors, Codex hook signals, proxy-side HTTP/WSS
mutation, and Codex tool-output Layer-0 adoption.

No RTK compression rule is missing from Slimference's bundled TOML catalog:
both trees currently contain 59 `.toml` filter files, and the filename diff is
empty. RTK's Rust command files map to Slimference's `builtin_*.go` files plus
generic build/test/lint/search/package/container dispatchers. The current
accepted deltas from this refresh are `wc` compaction, safe large `find`/`fd`
path-list grouping, and stricter search output-shape refusal for NUL-delimited
or custom path-separator modes. The compaction deltas preserve requested
evidence, require a shorter result, and fail open on ambiguous shapes; the
shape-refusal delta is safety-only and prevents the colon parser from touching
formats it cannot prove.

T289 also closed the remaining safe registry-breadth gap: Slimference's hook
rewrite gate now reaches existing deterministic build/lint/format/search/package
reducers for the RTK-style direct commands that were previously only compacted
when users invoked `slimference filter` manually. `curl` and `wget` gained an
argv-aware exact network-response guard before generic reducers, so API bodies
cannot be lossy log-windowed or schema-summarized by default.

The remaining non-ported RTK surfaces are closed product decisions, not hidden
queue items: Claude-only rewrites stay parked, prompt-level advisory tooling
does not save tokens in Codex hot paths, lossy dependency-list summaries remain
rejected as default dependency evidence, and aggressive code-signature summaries
are rejected as defaults because they remove implementation bodies.

## Current Matrix

| RTK capability | Evidence | Slimference status | Classification |
|---|---|---|---|
| Built-in TOML filters | `research/rtk-ai/rtk/src/filters/*.toml` = 59; `internal/filter/builtins_toml/*.toml` = 59; filename diff empty | Byte-for-byte catalog class is present under Slimference-owned tree | parity |
| Git, gh, glab, cargo, JS/TS, Python, Ruby, .NET, cloud, system command handlers | RTK `src/cmds/**`; Slimference `internal/filter/builtin_*.go` + category dispatchers | Slimference has fewer files but broader generic dispatch plus tests | already-better |
| Claude Bash `PreToolUse.updatedInput` rewrite | RTK `hooks/claude/rtk-rewrite.sh`; Slimference `internal/hooks/claude.go` | Slimference retains reference code, but product entrypoints are parked by T217. Use RTK for Claude Code now. | parked |
| Claude PostTool output replacement | RTK does not ship a dedicated `updatedToolOutput` posttool path in the inspected snapshot | Slimference retains `claudeposttool` handler code for reference, but the public command is not exposed in product mode. | parked |
| Codex adoption | RTK `hooks/codex/rtk-awareness.md` is instruction-based | Slimference has Codex hooks plus transparent SNI MITM, HTTP Responses mutation, WSS Phase-F mutation, and proxy Layer-0 tool-output adoption | already-better |
| Codex hook event coverage | Codex CLI `0.137.0` exposes lifecycle events including `PreCompact`, `PostCompact`, `SubagentStart`, and `SubagentStop` | Slimference installs pre/post compact hooks and now normalizes those event names when migrating legacy flat `hooks.json` | parity |
| Rewrite compound operators, pipes, env prefixes, absolute paths | RTK `src/discover/registry.rs` tests cover compounds, pipes, env prefixes, absolute paths, git global options | Slimference covers Codex-relevant command extraction through `filepath.Base`, `cd &&` normalization, env-prefix handling, explicit opt-out, and proxy/WSS tool-output adoption | Codex parity |
| Transparent wrapper prefixes (`shadowenv exec --`, `direnv exec .`, `docker exec app`) | RTK `transparent_prefixes` registry support exists for command rewrite surfaces | Not a Codex product gap: current Codex hooks do not expose a proven command-mutation contract, and Slimference's Codex savings happen after tool output exists through hooks/proxy/WSS | not-codex-product |
| Explicit disabled env var | RTK `RTK_DISABLED=1`; Slimference `SLIMFERENCE_DISABLED=1` | Equivalent local opt-out under Slimference naming | parity |
| Read/Grep/Glob/LS built-in non-Bash tools | RTK README states Claude built-in Read/Grep/Glob bypass Bash hook | Claude-only gap, outside the current Codex product scope. Codex uses tool-output/proxy/WSS surfaces instead of Claude built-in tool hooks. | not-codex-product |
| Observability | RTK SQLite tracking and gain; Slimference filter.db, gain/savings, admin state, WSS counters | Slimference has more surfaces, especially proxy/WSS counters | already-better |
| Fail-open | RTK raw proxy, tee, hook version guard; Slimference tee, panic guards, timeouts, schema-drift byte bridge, daemon lifecycle revert | Slimference stronger for Codex live traffic | already-better |
| Discover/learn/advisory tooling | RTK `discover/` and `learn/` | Not hot-path savings; Slimference has stats/gain and T210/T211 docs | not-needed |
| `wc` compact output | RTK `src/cmds/system/wc_cmd.rs` strips alignment and common path prefixes | Slimference now has safe `TryCompactWc` Layer-0 reducer and rewrite coverage | ported |
| Large `find`/`fd` path-list output | RTK groups file search results by directory, but its command executes a new gitignore-aware walk | Slimference ports only the safe output half: group large actual path lists by repeated directory prefix, preserve every path component and order, fail-open on ambiguous lines | ported-safe-subset |
| NUL/custom-separator search output | RTK's registry treats output shape as a command-level safety boundary | Slimference refuses `rg -0`, GNU `grep -Z`, `--null`, `--null-data`, and `--path-separator` before match-line grouping | ported-guard |
| Registry-only direct command breadth | RTK `src/discover/rules.rs` routes additional direct tools such as `gt`, `diff`, `curl`, `wget`, `prisma`, plus many build/lint/format/search binaries | Slimference now routes these to existing safe reducers or exact full-pass guards; arbitrary runtime subcommands such as `deno run`, `dart run`, and `flutter run` stay unrewritten | ported-safe |
| `curl`/`wget` response handling | RTK preserves JSON and pipe outputs to avoid corrupting downstream consumers | Slimference's `network_response_exact` reducer exact-minifies JSON whitespace and otherwise full-passes before generic log/JSON reducers | ported-safer-for-codex |
| Package list/outdated/show summaries | RTK has broader package-manager list/outdated surfaces | Rejected as default because dependency versions and package names are requested facts; keep full-pass unless a future exact/recoverable table compactor owns the shape | reject-default |
| Aggressive code-signature summaries | RTK `rtk read -l aggressive` keeps imports/signatures and removes implementation bodies; default `rtk read` level is `none` | Rejected as default for Codex. It may save many tokens but can remove body details GPT-5.x needs later, so it violates Slimference's default drawdown bar. | reject-default |

## Port Decisions

| Item | Decision | Reason |
|---|---|---|
| Claude `PostToolUse.updatedToolOutput` replacement | Parked by T217 | Useful reference code, but not part of the Slimference product path while RTK handles Claude Code. |
| Wrapper help/completion truth | Done in T214 | Keeps wrapper excellent but explicitly advanced-only; removed unimplemented flags from help/completion. |
| DoH fallback and status preflight | Done in T215 | RTK has no equivalent transparent-MITM self-loop problem; Slimference needed this for T209. |
| Configurable transparent rewrite prefixes | Do not port for Codex product | Useful for Claude wrapper ergonomics (`shadowenv`, `direnv`, selected `docker exec`), but Codex does not expose the needed transparent command rewrite contract and Slimference already operates on Codex tool outputs. |
| Claude Grep/Glob/LS dedicated hooks | Parked outside Codex scope | Built-in Claude tools bypass Bash, but this project is Codex-focused and RTK remains the active Claude recommendation. |
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
Codex `updatedInput` is not a proven transparent rewrite contract in the
current Slimference evidence set, and Codex Desktop/WSS traffic is not
controllable by a shell wrapper. The correct Codex-max path remains
Slimference's two surfaces: Codex hooks for lifecycle/tool signals plus
scoped/proxy HTTP/WSS request mutation.

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
| Production SLOC | ~23 500 | Slimference Go codebase with HTTP proxy + TUI |
| TOML filter rules | 58 | TOML DSL + 26 built-in compactors (now incl. T25 Python + Terraform) |
| Hook files | 11 | 3 (claude, codex, verify) |
| Tests | Implicit via `cargo test` | 100 % statement coverage enforced |

Scope difference: Slimference is a **superset** on everything that is not
Layer-0 filtering. RTK has deeper Layer-0 specialisation; Slimference has
the HTTP proxy, active deterministic compression/cache/output-reduce stack,
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

- HTTP reverse proxy with active deterministic compression/cache/output-reduce
  pipeline: L0/WSS reducers, L1 deterministic compression, L2 response cache,
  and L3 output/tool-surface reduction.
- Prompt-cache metrics, both injection (OptimizeCacheBreakpoints) and
  measurement (T23 `cache_read_input_tokens` aggregation).
- Operating modes (T36: strict / balanced / fast) with explicit
  precedence rules.
- TUI (BubbleTea) with live metrics, state persistence (T31), hook
  status, and toggles.
- Daemon service (launchd) with `daemon logs` subcommand (T30).
- Hook-drift watchdog (T33).
- Bash completion (T32).
- Tuning staircases and overflow recovery that stay on deterministic product
  reducers.

---

## Closure

- All RTK components that add unique, in-scope value have been ported.
- The `rtk-master/` folder has no further dependency in the Slimference
  code or tests. It is removed from HEAD and `.gitignore`d to prevent
  accidental re-add. History retains it in the initial commit.
- Any future interest in the non-ported modules (`discover/`, `learn/`)
  can consult git history at commit cb78774 or earlier.

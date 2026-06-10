# Slimference - Technical Documentation

Version: 0.6.0
Last updated: 2026-06-07

Comprehensive reference for the Slimference token-savings proxy. This
document tracks the current v0.6.0 macOS-first product line; sections follow
current code layout, each with file:line pointers so readers can jump from
prose to source in one hop.

---

## Table of Contents

1. [Overview](#1-overview)
2. [Architecture](#2-architecture)
3. [Request Lifecycle](#3-request-lifecycle)
4. [Layer 0 - Pre-Entry Filter](#4-layer-0-pre-entry-filter)
5. [Layer 1 - Deterministic Compression](#5-layer-1-deterministic-compression)
6. [Retired Semantic Summary Path](#6-retired-semantic-summary-path)
7. [Layer 2 - Response Cache](#7-layer-2-response-cache)
8. [Provider Support](#8-provider-support)
9. [Install and integration](#9-install-and-integration)
10. [Bypass and Fallback](#10-bypass-and-fallback)
11. [Security](#11-security)
12. [Observability](#12-observability)
13. [TUI](#13-tui)
14. [Configuration Reference](#14-configuration-reference)
15. [CLI Reference](#15-cli-reference)
16. [Installation](#16-installation)
17. [Build and Release](#17-build-and-release)
18. [Testing Strategy](#18-testing-strategy)
19. [Package Map](#19-package-map)

---

## 1. Overview

Slimference is a Go reverse proxy plus local install tooling that reduces
token usage for Codex-first workflows. The current production target is Codex
CLI first and Codex Desktop next. Claude Code support remains in the tree for
reference, but the shipped product path parks it completely: no install, no
hooks, no `~/.claude` writes, and no Anthropic routing. The admin/control plane
listens on `127.0.0.1:8990`; the transparent
SNI listener defaults to `127.0.0.1:8443` when armed. Request bodies can run
through deterministic compression, output reduction, and cache layers before
the daemon re-dials the real upstream.

The proxy is **fail-open transparent**: request shape, headers, streaming
semantics, and unknown payloads are preserved. Known conversation bodies and
known output frames may shrink when a deterministic reducer proves the mutated
payload is shorter and schema-safe.

New product mechanisms must be default-on-safe or automatically safe-enabled.
Legacy, lab, proof, and operator surfaces may stay isolated in the tree, but new
normal-product code must not add another permanent manual experiment toggle.
If a lever cannot be made deterministic, recoverable/fail-open, and safe for
routine use, it stays out of the product path.

### Why it works

| Problem                                          | Slimference answer                          |
|--------------------------------------------------|---------------------------------------------|
| Large tool outputs repeated across turns         | Exact dedup plus archive-backed near-dedup  |
| Long sessions repeat tool/context surfaces        | Readcache, deltas, chunk recovery, cache leverage |
| Identical requests re-cost tokens                | Response cache + prompt-cache breakpoints   |
| Verbose shell / git / test output                | Built-in parser reducers + TOML DSL (Layer 0) |
| Compression costs latency on small requests      | Thresholds + latency-budget guard (T54)     |

### Client support

- **Codex CLI**: default scoped path is `slimference install`, `status
  --preflight`, then `slimference codex run -- <prompt>`.
  This affects only that Codex CLI process and leaves Browser ChatGPT and
  ChatGPT.app direct. Scoped launch strips stale Codex runtime/session env but
  preserves config-bearing env such as `CODEX_HOME`, so MCP server definitions
  remain visible.
- **Codex Desktop**: the app remains direct when launched normally from
  Finder/Spotlight. The Slimference path is
  `slimference codex launch-desktop --transport=app-server --replace-existing`,
  which sets only `CODEX_CLI_PATH` plus Slimference shim metadata on the spawned
  app process. Codex.app starts Slimference as its app-server; the hidden shim is
  a thin stdin JSON-RPC mediator that execs the real Codex app-server (provider
  block pointing at `http://127.0.0.1:8990/backend-api/codex`) and rewrites the
  one field that blocked routing: Codex Desktop opens conversations with
  `thread/start` carrying `modelProvider: null`, which resolves to the account
  default (chatgpt.com direct); the shim rewrites a default (null/absent)
  `modelProvider` to `slimference-codex`, byte-identical for everything else.
  The same scoped shim augments app-server responses shaped as `result.config`
  for older Desktop builds that still expose provider config in the UI. Current
  Codex Desktop builds do not expose a stable process-local text-chip contract
  through app-server response data, so Slimference does not fake the signal by
  mutating `model/list`, model IDs, display names, selected model values, or
  service-tier metadata. Current Desktop builds therefore have no external
  Slimference indicator. Route visibility stays in the TUI Activity/Status
  views, the app-server shim flight log, and daemon decisions.
  Realtime/voice threads and explicit provider choices are passed through; any
  parse ambiguity fails open. Unrelated stdout/stderr frames pass through
  untouched. This avoids the old proxy/CA/TLS root-store barrier entirely. Proof
  and TUI launches pass
  `--replace-existing` so an already running Codex.app is quit and verified gone
  before the scoped Slimference instance starts; raw CLI launch keeps a
  conservative refusal unless the same flag is explicit. Verified (2026-05-22):
  the spawned Desktop app-server holds loopback sockets to `:8990` with zero
  direct `chatgpt.com` sockets, and the daemon decisions log records the Desktop
  conversation as `route_mode=websocket_phasef` for `/backend-api/codex/responses`
  - the same Phase-F route the certified CLI uses, with byte-identical
  `permessage-deflate` frames. Desktop WSS routing is therefore proven;
  proof-fresh WSS can compact state-safe status output after the tool call is
  known; broader stateful WSS tool-output mutation remains lab/proof opt-in.
  Voice (`thread/realtime/*`), Browser ChatGPT,
  ChatGPT.app, computer-use, and Claude Code are untouched. Note: the sampled
  `desktop status` WSS counters lag and must not be used to claim or deny
  savings; the decisions-log `route_mode` is the reliable signal.
- **Global transparent lab**: `cert-trust`, `root-arm
  --global-chatgpt-hosts`, `enable`, `disable`, and `root-disarm` still
  exist for explicit lab certification. They route `chatgpt.com` and
  `api.openai.com` machine-wide and therefore include Browser ChatGPT and
  ChatGPT.app in the bridge.
- **Claude Code**: code remains available in the repository, but the product
  binary parks it. `slimference install`, `hook`, `integrate`, TUI apps, and
  `/admin/apps` do not enable or modify Claude Code.
- **Generic OpenAI API**: `api.openai.com/v1/chat/completions` routed by
  path detection when no Codex UA is present.

### Design invariants

- **Scoped before global**: the default Codex CLI path is per-process and
  does not use `/etc/hosts`, pfctl, System Proxy settings, or persistent
  env vars. Transparent MITM is explicit global lab mode only.
- **Local CA files are not global MITM**: default install may keep local CA
  material for isolated legacy/lab diagnostics, but Keychain trust, hosts,
  pfctl, and system-proxy routing are never part of the normal install path.
- **Autonomous WSS repair**: daemon startup checks Codex WSS proof drift and
  can launch the same lock/backoff-gated background recert used by scoped
  `codex run --transport=auto`, TUI startup/status refresh, and TUI repair.
  TUI Status shows the current/certified Codex tuple, and Setup can force a
  manual `CLI savings route` proof refresh even when the route is already green.
- **Passthrough on failure**: if any layer errors, the original body is
  forwarded. See section 10.
- **Bypass switch**: a single atomic flag collapses every provider + layer
  toggle to off, making the proxy a pure relay.
- **`encoding/json` only**: no third-party JSON library.
- **Hot path budget ≤ 5 ms**: Layer 0/WSS reducers, Layer 1, Layer 2,
  and Layer 3 must fail open and stay cheap enough for normal Codex use.

---

## 2. Architecture

```
┌─────────────┐       hooks        ┌─────────────────────────────────────┐
│ Codex CLI   │───────────────────▶│ slimference admin/control :8990     │
│ Codex App   │                    │                                     │
└─────────────┘       TLS/SNI      │ transparent SNI listener :8443      │
      │        chatgpt.com:443     │  ┌──── request/WSS pipeline ────┐   │
      └───────────────────────────▶│  │ detect → L0/WSS → L1 → L2/L3 │   │──HTTPS──▶ chatgpt.com
                                   │  └──────────────────────────────┘   │──HTTPS──▶ api.openai.com
                                   │  ┌──── response pipeline ───────┐   │
                                   │  │ streamcut/repdet + cache     │   │
                                   │  └──────────────────────────────┘   │
                                   │                                     │
                                   │  TUI, admin state, analytics, debug │
                                   └─────────────────────────────────────┘

Claude Code support stays in the codebase as parked legacy/reference plumbing.
It is not installed, toggled on, or routed by the Phase H path.
```

The proxy process also owns:

- launchd service (macOS) with `KeepAlive{Crashed=true,SuccessfulExit=false}`
  that restarts the binary in ≤ 2 s when it dies (T68).
- On-disk state: `~/.slimference/` (analytics, read-cache, tool-archive,
  checkpoints, session-logs, filter.db).
- Config: `$XDG_CONFIG_HOME/slimference/config.toml` by default, with
  legacy `~/.slimference/config.toml` as fallback (T46).

### Main goroutines

- `http.Server.Serve`: accepts connections.
- Analytics worker (1): drains `analyticsQueue`, persists JSONL.
- FileWatcher: invalidates response cache on file modifications.
- HealthMonitor: 20-slot ring of per-provider request outcomes.
- Optional TUI goroutines (BubbleTea).

---

## 3. Request Lifecycle

Entry: `internal/proxy/proxy.go::ServeHTTP` (line 347).

1. **Provider detect** (`detectProviderWithUA`): path prefix and
   `User-Agent` decide Anthropic, OpenAI, or CodexChatGPT.
2. **Passthrough fast path** for non-compressible URLs (health, admin,
   streaming endpoints we should not touch).
3. **Body read**, bounded by `maxRequestBodySize = 32 MiB`. Oversize → 413.
4. **Re-detect** with body available (lets the body-probe branch run).
5. **Provider toggle** — if disabled via admin or bypass, straight
   passthrough.
6. **Version negotiation** (T62): unknown `anthropic-version` downgrades
   to `PipelineConservative` or `PipelinePassthrough`.
7. **Layer 0 hooks** — handled *out of process* by Codex hooks before the
   HTTP request is ever sent, but the results appear as compressed tool
   outputs in the body we now receive. Claude hook code is parked and not part
   of the product path.
8. **Layer 1 compression** — deterministic, 15 sub-layers plus preview
   passes.
9. **Prompt-cache breakpoints** (T45) — up to 4 `ephemeral` markers
   spread evenly across the stable prefix.
10. **OpenAI prompt-cache steering** (T136/T285) — default-on hashed
    `prompt_cache_key` steering for generic OpenAI API requests only, using
    model-bound stable-prefix hashes. Optional model-gated
    `prompt_cache_retention` stays operator-controlled. Per-key negative-net
    cooldown suppresses keys that repeatedly cost more cache-create tokens than
    they read back. CodexChatGPT backend routes stay untouched until live proof.
11. **Layer 3 output/tool-surface reducers** — safe output discipline and
    tool-surface reductions are applied only when policy and proof gates allow.
12. **Upstream call** via the per-provider HTTP client. Streaming is
    preserved.
13. **Overflow recovery** (`docs/spec.md`): on HTTP 400 with context-
    too-large signal, retry with aggressive re-compression, then raw.
14. **Layer 2 response cache** — stores by request hash; `FileWatcher`
    invalidates on change.
15. **Analytics events** via non-blocking queue. Drops are counted +
    warn-logged (T42).

### Phase histograms (T58)

Every phase records into `internal/analytics/phase_hist.go` for live
p50/p95 via `/admin/status.pipeline`. Instrumentation overhead: ~15 ns
per phase.

### Panic recovery

`recoverMiddleware` catches any panic inside the handler: best-effort
passthrough using the original body stashed in the request context; if
the body was not yet stashed, returns 502.

---

## 4. Layer 0 - Pre-Entry Filter

Layer 0 runs **outside** the HTTP proxy. In product mode it is invoked by
Codex hooks only: `~/.codex/hooks.json` points at
`~/.slimference/hooks/*.sh`, which invoke `slimference filter`,
`slimference rewrite`, `slimference posttool`, `slimference codexhook`,
and `slimference readhook codex`. Claude hook code remains in-tree but no
public install path writes `~/.claude`.

Slimference hook install/remove is scoped to Slimference-owned hook entries and
preserves other repo-local Codex policy hooks in `.codex/hooks.json`.

### Pipeline

`internal/filter/pipeline.go::RunPipeline` runs:

1. Exec the tool command; capture stdout + stderr + exit code.
2. ANSI strip on stdout.
3. Built-in reducers tried in priority order from
   `filter.Layer0ReducerRegistry()`. The registry records each reducer's
   mechanism id, command family, safety class, default eligibility, and
   preserved-evidence contract before the reducer can participate in product
   dispatch. The default order covers git-status, git-diff, git-log, git-show,
   build-output, test-output, dotnet, ruby, search/path-list grouping, ls, tree, wc,
   exact network-response JSON, lint, log, format, psql, package-manager,
   container, gh list, glab list, AWS JSON, python traceback, Terraform
   plan/init/validate/show, structured JSON, and JSON minify. `curl`/`wget`
   network output is guarded before generic reducers: valid JSON may be
   whitespace-compacted exactly, while non-JSON and already-compact JSON
   full-pass so API bodies cannot be log-windowed, schema-summarized, or
   truncated by default. Long `terraform state list` and plain human-readable
   `terraform output` full-pass in the default package because resource
   addresses, output names, and output values are requested facts unless a
   future route-specific reducer owns exact archive recovery.
   `build-output` includes the shared diagnostic parser for Go, Cargo,
   GCC/Clang, TypeScript, Svelte, frontend tools (Next/Vite/Vitest/Jest/
   Playwright/ESLint/Biome/Oxlint/Turbo/Nx/Lerna/Bun), Python diagnostics
   (ruff/pylint/flake8/mypy/pyright/pytest/unittest matching), Zig, SQL/DB
   client diagnostics (psql/sqlite/mysql/mariadb/Prisma/Drizzle/SQLFluff/
   Sqruff), Markdown, and practical ecosystem compilers (Java/Kotlin/Swift/
   Dart/Flutter/PHP, container/orchestration tooling, and adjacent wrappers).
   `lint-output` also calls the shared parser after exact success compactors
   so non-empty Python lint/type-check output is reduced without losing the
   older ok-paths.
   `package-manager` compacts install/update success summaries and
   npm/pnpm/yarn/bun/pip/uv resolver-error noise to actionable lines.
   `psql` covers SQL-shell table-border compaction for psql, MySQL/MariaDB,
   SQLite, and SQLite3 outputs.
   The hook rewrite gate now reaches every safe built-in reducer family
   surfaced by the filter coverage audit, including direct build/lint/format/search
   binaries such as `ninja`, `cmake`, `next`, `vite`, `webpack`,
   `staticcheck`, `semgrep`, `stylelint`, `dprint`, `taplo`, `shfmt`,
   `sqlfmt`, `pipenv`, `prisma`, `gt`, `diff`, `curl`, and `wget`. Arbitrary
   runtime commands that can execute user programs remain guarded; for example
   `deno run`, `dart run`, and `flutter run` are not rewritten by default.
4. Fallback: `FirstMatchingTOMLRule` applies user-defined 8-stage
   rules from `~/.slimference/filters.toml`.
   The embedded default TOML catalog uses a product-safe application path:
   line caps preserve late error/fatal/warning/diagnostic evidence and emit an
   omitted-line marker. User and project TOML rules keep their literal DSL
   semantics, because they are operator-owned configuration.
5. Truncate with a short `[truncated …]` hint to
   `passthrough_max_chars` (default 2000; `docs/spec.md`).
6. Emit to stdout + write the raw bytes to the tee dir for recovery.
7. Record the run in `filter.db` (SQLite) with local-tokenizer counts for
   input/output tokens, falling back to byte/4 only if the tokenizer is
   unavailable.

### Exit-code contract

See `docs/layer0-exit-codes.md` for the complete matrix. The invariant:
Slimference **never** swallows a non-zero child exit. Filter failures
are degradation signals, not status translations.

### Token-saving reporting

`slimference gain [today|week|month|all]` aggregates rows from
`filter.db` into a summary with savings percentages. Layer 0 now stores
local-tokenizer counts rather than plain byte/4 estimates, so parser-family
comparisons are closer to what the provider will bill. `--by-command` breaks it
down per argv[0]; `--by-parser` groups persisted Layer-0 savings by parser/tool
family; `--cache` reports persisted provider prompt-cache read/create counters;
`--output` reports persisted T130 output-reduce telemetry without inventing a
savings baseline, including T141 profile tier and task-shape buckets plus a
provider/model/profile/task-shape profile row report for manual evolution;
`--proxy`
reads flight-recorder decision logs and reports provider-only proxied LLM
requests with input/cache/output accounting. `--csv` / `--json` for machine
consumption.

### Output-reduce quality governor

Output reduction is a runtime-governed output-wire layer, not a billable-input
savings claim. HTTP/SSE streamcut and stop-sequence injection remain separate
from Codex WSS Phase-F input reducers; WSS streamcut is deliberately disabled
until a terminal-safe WSS sequence is live-proven. Terminal WSS response payloads
stay byte-equal so code, patch, JSON, and final-answer text cannot be silently
rewritten after the model emits it.

The output directive injector is task-shape aware. Exact replies and repair
follow-ups skip injection; exact replies include `reply only`/`respond only`/
`return only`/`json only` style prompts and the German `gib/antworte/sage nur`
variants, not only `reply exactly`. Detail-sensitive task shapes full-pass
output-reduce injection by default until each shape has positive paired A/B
evidence: code edits, new-file generation, debugging, reviews, tool-result
reasoning, final summaries, read-only analysis, deep explanations, and
planning. This is stricter than the earlier standard-only cap and prevents
unproven directive text from changing model behavior on tasks where missing
detail is a product drawdown. Explicit command-output relay turns such as "show
the output", "full terminal output", or
German `gib die komplette Terminal-Ausgabe` skip output-reduce injection entirely
with `command_output_relay_exact_output`, preserving requested output, paths,
errors, exit codes, and line order. The proof gate runs before runtime cooldown
selection in the proxy and again inside the injector as defense in depth. The
central planner mirrors the same contract for product/audit output: exact
reply, command-output relay, repair follow-up, unproven detail shapes, and
low-ROI direct-answer turns are reported as bypassed. Repair signals such as
"you skipped", "too short",
missing detail, malformed patches, failed apply-patch
feedback, and German `fehlt`/`nochmal ausführlicher` style re-asks are stored by
session and immediately downgrade the affected
provider/model/profile/task-shape bucket without waiting for the normal sample
window.
Runtime controls live under `[compression.output_reduce]`. Directive injection
uses `enabled`, `profile`, `custom_directive_path`, `signature_marker`,
`max_added_bytes`, `min_input_tokens`, and auto-tune thresholds. The conservative
concise-chat hint is controlled by `concise_chat_enabled`,
`concise_chat_min_input_tokens`, and `concise_chat_text`. It applies only to
direct-answer/explanation turns, full-passes code, docs, JSON, logs, diffs,
repair, review, planning, and tool-output contexts, and is gated so tiny prompts
do not pay more input overhead than the hint can plausibly save in output. On
HTTP routes the established output-reduce profile keeps priority for requests at
or above `min_input_tokens`; concise-chat fills the guarded smaller-chat gap. On
Codex WSS, prompt-cache-prefix frames stay byte-equal. Stop sequences, HTTP/SSE
streamcut, repetition detection, stale-read aging, obsolete-read pruning, and
the default-off be-terse hint are independent toggles so operators can keep
deterministic response-side guards without forcing prompt-directive injection.
The output status/admin payload reports injected/skipped turns, directive input
overhead, observed output tokens, last skip reason, auto-tune downgrades,
stop-sequence additions, streamcut fires, repetition rewrites, stale/obsolete
read replacements, and be-terse injections. `slimference gain --output` reads
those persisted counters and deliberately reports observable telemetry only;
concrete output-token savings claims require paired A/B proof.
Task-shape detection reads only model instructions: user, system, developer,
top-level `instructions`, top-level `system`, and top-level prompt/input text.
It deliberately ignores prior Codex `function_call`, `function_call_output`,
tool stdout/stderr, and tool arguments, so old terminal output cannot make the
current user turn look like a patch, repair, or command-output relay request.
This keeps aggressive-profile caps focused on the actual current instruction
instead of silently sacrificing output savings because of historical tool text.
For Codex Responses bodies on non-WSS routes, output-reduce directives are
written only to the top-level `instructions` string. The injector does not
rewrite `input` and never creates `input` items with `role=system`, because Codex
rejects those and because output-reduce must not alter the model's task/tool
context while trying to save output tokens. On the Codex WSS Phase-F path,
model-facing output-reduce directive injection is disabled except for the
separate conservative concise-chat hint on eligible non-prefix chat frames. Live
scoped WSS sessions showed recurrent upstream `invalid_request_error` after a
prior WSS user-turn directive rewrite, while the directly rejected follow-up
frame was byte-equal.
The product rule wins: a speculative output-token reducer that can poison WSS
conversation state is not a default product path. Stateful Codex WSS request
bodies that carry tool output also full-pass by default, because live Desktop
sessions showed later `invalid_request_error` failures after earlier WSS
tool-output rewrites. HTTP, hook, and other non-WSS Codex routes keep the
deterministic read/git/test/repeated-output, tool-prune, stale-read,
archive-recovery, and chunk reducers. WSS debug telemetry records skipped
output-reduce candidates as `codex_wss_directive_disabled` and guarded
tool-output frames as `wss_tool_output_state_full_pass`.
Layer 3 product work follows a lower-savings, zero-drawdown profile: default-on
mechanisms must be deterministic, shape-bounded, recoverable or auto-demoted,
and must not require the model to reinterpret extra behavioral instructions.
Model-facing directive variants stay shadow/proof-gated until paired A/B rows
show positive net savings with no repair, re-ask, host-budget, or safety
regression for that exact workload shape.
Streaming provider usage is accounted by field semantics, not by blind addition:
if an OpenAI/Codex or Anthropic stream reports final `output_tokens`, that total
replaces earlier text estimates for the request; OpenAI/Codex `cached_tokens`
usage is likewise treated as a per-request total. Anthropic cache read/create
fields remain separate provider counters. This prevents output-wire or
OpenAI/Codex provider-cache claims from being inflated by counting an
intermediate usage event plus the final usage event.

### Codex read-compression (mechanisms and safety model)

Goal: maximum Codex token savings with no model-quality drawdown. A drawdown is
the model getting dumber, losing context, drifting from the real files, breaking
workflow, or hallucinating because Slimference removed information too early.
Mechanisms split into two product classes.

Default-auto tier (the proven product bulk): read-delta (repeat reads collapse to
references), exact repeated-output, content-defined chunk dedup, the Codex
exec-envelope stripper, and low-risk search-output grouping. The exact reducers
elide only what the model already holds or can reconstruct exactly; search grouping
keeps representative match context plus the recovery path of re-running the search.
On a real codebase-exploration session the default-auto reducers saved 36533
billable tokens with zero parse/compression/degraded errors.

Public savings claims are session-scoped, not universal averages. The realistic
slightly optimistic contribution ranges are: Layer 0 usually 15-45% with 50%+
bursts on repeated tool output; Layer 1 usually 3-15% with 20-30% on highly
structured/repeated context; Layer 2 usually 0-25% with 30-50% when provider
cache reuse is strong; Layer 3 usually 0-8% with 10-20% on exact-answer or
tool-heavy shapes. These ranges overlap and must not be added together. Combined
routed Codex sessions should be described as roughly 25-50% for normal
tool-heavy coding, 35-65% for long refactor/debug loops, 45-70% for
search/read/log-heavy loops, and 0-15% for short one-off prompts.

Retired path: first-read AST/structure scan-mode elision is not part of the Codex
product mode. It saved tokens in narrow probes, but it gave the model less file
information on first sight and relied on the model noticing the missing detail and
re-reading. Slimference's default instead keeps first file reads unelided and
spends engineering effort on better cache hits, repeat-output reuse, ranged-read
reuse, and search/build/test compaction.

Safety model: (1) first file reads full-pass; (2) repeat and ranged reads use exact
cache/delta decisions; (3) edit-target guards - recently-edited or edit/debug reads
full-pass; (4) archive-backed references are emitted only by mechanisms that keep a
deterministic recovery handle and pass the positive-token guard.

Current product status:

| Mechanism | Default status | Proof state | Drawdown position |
|---|---|---|---|
| WSS Phase-F routing for Codex CLI/Desktop | On when route proof is fresh; bridge/fallback on drift | CLI and Desktop route proofs recorded; auto-recert guards version drift | Fail-open; route-ready is distinct from state-safe WSS status compaction |
| Read-delta for repeated full-file reads | On | Proven in real CLI/Desktop repeat-read captures and A/B replay with `lost=0`; 2026-06-02 strict release matrix covered CLI + Desktop repeat reads and the mixed Desktop workday | Low risk: first read was already sent in full |
| Ranged read-delta for `head` / `tail` / `sed -n` | On | Covered by T250/T257 capture matrix; 2026-06-02 strict release matrix covered CLI + Desktop ranged `sed -n` repeat reads | Low risk: first range full-passes, later same range collapses only after exact observation |
| Exact repeated non-file output dedup | On | Implemented through the shared Codex Layer-0 reducer; 2026-06-02 automatic CLI replay covered Codex exec-envelope repeated-output recovery with `lost=0`; 2026-06-02 Desktop search-delta proof recorded a live repeated-output hit with 14,973 billable input tokens saved | Low risk: exact same command/output only, archive-backed, fail-open on changes; search uses a stricter match-set delta when visible evidence changes |
| Search-output grouping and repeated search delta | On | Real `rg` capture compacted about 40 KB to about 9 KB; T257 covers search workloads; 2026-06-02 strict release matrix covered CLI + Desktop search loops plus a mixed Desktop workday; 2026-06-02 Desktop search-delta proof passed live counters and replay `lost=0` | Low to medium: grouped first search keeps representative matches, changed repeated searches emit added/removed match evidence plus archive recovery, and ambiguous cwd full-passes for reusable keys |
| Build/test/git/lint/parser compactors | On where parser recognizes the command/output | Unit/integration covered; T252/T260 hardened caps and error priority | Low to medium: deterministic parser summaries only, positive-token guard |
| Content-defined chunk dedup | HTTP/non-WSS eligible through deterministic guards; WSS requires the state-safety gate or explicit lab/proof opt-in | T255/T266 live CLI+Desktop proof, T256 policy wiring, T258 route/risk/proof gate | Medium but guarded: archive recovery and `live` proof required, recent/re-read risk loosens |
| Archive recovery note | Default-off | Mechanism and replay support exist | Kept off by default until route/workload proof needs it |
| First-read AST/signature scan-mode | Removed | Removed by T253; tests enforce first file reads full-pass even in `max` | High drawdown, not product-safe |
| Predictive post-edit file state | Closed | T253 closed | Rejected for default-auto: first post-edit read full-passes to preserve recency/context; later repeats dedup normally |
| apply_patch context dedup | Closed | T253 closed | Rejected as standalone work: patch context is model working memory; exact repeated outputs remain covered |
| Reasoning-trace compaction | Closed | T253 closed | Rejected for default-auto: do not mutate reasoning/cognition surface for savings |
| Server-state mirror | Shadow/policy infra only | T254 closed as shadow | Tracks exact forwarded-state opportunities; no generalized model-facing mutation or reference language |
| Policy engine v2 | Foundation active | T258 in progress | Central route/workload/risk/recovery/proof decisions; unsafe candidates blocked or telemetry-only |
| HTTP archive recovery/promotion | Conservative lock active | T259 closed | HTTP fallback keeps safe Layer-0 reducers but cannot emit archive refs even in `max` |

Real-workload truth that shaped this: Codex reads files via `sed -n '1,Np'` partial
reads and searches via `rg`, never full `cat`, and truncates every exec output to a
token budget. So the original `cat`-only scan could not fire (extended to `sed`), and
search grouping was defeated by the truncation tail (made robust). The recurring
upstream `400 invalid_request` class is treated as a product safety signal, not
as a savings opportunity. Current Codex WSS guards full-pass stateful tool-output
request bodies, including search/path-list/source-like outputs, unless the
operator explicitly enables the lab/proof mutation switch for that exact
certification run.

Codex WSS and HTTP proxy-Layer-0 savings now share one explicit reducer entry
point and a central policy engine with route labels (`http`, `wss_phasef`),
workload classes, mechanism risk, recovery level, and proof level. Current policy
actions are `allow`, `shadow`, `full_pass`, and `block`: proven lossless reducers
are allowed, recoverable WSS chunk dedup is allowed only with archive recovery and
`codex_chunk_dedup_proof_level="live"` evidence, and recent/edit and
post-collapse re-read signals full-pass. First-read elision,
predictive post-edit synthesis, apply_patch context dedup, reasoning compaction,
and generalized server-state-mirror mutation are closed as non-product-default
surfaces. The server-state mirror remains telemetry/policy infrastructure only.
HTTP is explicitly blocked from archive-backed chunk references; WSS is the
product route for recoverable archive/chunk mechanisms.

The deterministic evidence selector is the block-level decision manifest used
by those reducers and reports. It classifies content as `test`, `log`,
`search`, `diff`, `stacktrace`, `json`, `code`, `plain`, or `unknown`, then
records content-free signals such as error keywords, stacktraces, outliers,
dedupe, changed hunks, recency, cache hot zone, first/last preservation,
exit/status, paths, counts, warnings, importance, and security. Keyword
detection is centralized in a deterministic registry; `token` is intentionally
not a security keyword because token-budget chatter would create false security
signals. The manifest stores safety class, action, reason, preserved-evidence
labels, recovery mode, cache impact, and net tokens, but never raw prompt/tool
payload. CLI savings and the TUI evidence card aggregate cache impact values so
provider-cache read, create/warmup, observed, and negative-net situations stay
visible. It does not summarize or retrieve content for the model; it only makes
deterministic reducer decisions auditable and catches negative-net/cache
regressions.
Research-derived compression ideas are limited to deterministic parser,
evidence, and reporting hardening. Slimference does not product-enable local
model compression, lossy code/text summaries, retrieve-on-demand memory
injection, or learning loops, because those require the model to recover omitted
context and do not meet the no-drawdown rule.
After any WSS upstream `error`, `response.failed`, or `response.incomplete`
frame, the current socket adapter quarantines itself and full-passes subsequent
request bodies until reconnect. That keeps the product fail-open after a proven
bad upstream response instead of attempting another mutation in the same WSS
state chain.

Layer 2 semantic context replacement is retired. Product savings now stay on
Layer 0 tool-output reducers, WSS route-safe guards, Layer 1 deterministic
compression, Layer 2 cache leverage, and Layer 3 output/tool-surface reduction.
No context ledger, OCRL capsule, or summary text is inserted into model-facing
context.

The offline A/B harness still proves archive-backed references by expanding
`local-archive://` markers to exact bytes, or by confirming the same bytes were
already sent verbatim earlier in the session. Missing or mismatched archive
expansion is counted as lost comprehension evidence. Codex exec envelopes are
normalized for prior-full checks so volatile `Chunk ID` and `Wall time` headers
do not turn safe repeat-output recovery into false failures.

The reducer telemetry includes mechanism attribution:
tool-result blocks seen, unresolved tool-use references, command-resolved
blocks, command-unresolved blocks, read-delta attempts, read-delta misses,
modified blocks, read-delta blocks, captured-output filter blocks, and Codex
exec-envelope blocks, exact repeated-output blocks, chunk-dedup blocks, and
content-free policy counters under `proxy_layer0_policy` keyed by route,
mechanism, action, reason, and block reason. Opportunity and miss fields make
hit-rate visible without claiming savings. The modified-block and mechanism-hit
fields are success counters and are only recorded with a positive token saving.
Cache-decision counters under `proxy_layer0_cache` separately record route,
mechanism, `hit`/`miss`, reason, and count for read-delta and exact
repeated-output. Those reasons make cold starts, first-seed full passes,
recent-edit bypasses, missing session/key state, non-shorter deltas, unavailable
archives, and unchanged hits visible without raw payload capture.
For product UI and low-noise status surfaces, `/admin/state` also exposes
`savings.product`: a content-free rollup with `status` (`idle`,
`active_no_savings`, `saving`, or `attention`), billable input tokens saved,
output-wire bytes saved, request-side bytes reduced, cost estimate, cache hit/miss
counts, read/repeated/chunk hits, tool-resolution misses, analytics drop
counters, and aggregate safety issues. Analytics drops are split into total,
proof-critical, and low-priority counters; any proof-critical analytics loss
sets the product status to `attention` because the release proof window is no
longer evidence-complete. The raw route, policy, parser, and cache counters
remain available under the existing debug fields, but product surfaces should
prefer this rollup instead of inventing their own mixed headline.
`/admin/state.host_budget` is the product resource guard. It reports `ok`,
`unknown`, or `attention`, daemon RSS, uptime, process CPU time, lifetime and
windowed CPU percentage, OS-reported lifetime and windowed disk read/write
operation counters, bounded state-directory size, the 200 MiB RSS budget, the
512 MiB state budget, WSS compression/parse/degrade health, mutation activity,
and content-free reason codes. The daemon/admin path uses an in-process
resource probe for PID, uptime, real RSS, process CPU time, disk I/O counters,
and state size where the platform can provide it, avoiding a loopback
self-health guess for the product budget. State-directory scans are entry-bound:
if a state tree exceeds the bounded scan limit, the host budget treats that as
resource pressure instead of reporting a partial undercount as healthy.
If the RSS resource probe is missing while WSS is otherwise active, the status
remains `unknown` instead of becoming `ok`; proof gates must not treat an
unmeasured host as a green resource budget. The TUI product panel renders that
as `host budget unknown` instead of `safety ok`.
CPU-window demotion ignores sub-second windows: tiny startup/admin-poll samples
remain visible in telemetry but cannot force managed reducers into full-pass.
Only a stable window of at least one second can trigger
`cpu_window_budget_exceeded`, which prevents false startup demotion while still
protecting the product path from sustained local CPU pressure.
Policy demotion uses the same concept through budget inputs. Host-budget
attention demotes recoverable or heavier mechanisms such as chunk references
while keeping cheap lossless/exact cache-hit reducers (`read_delta`,
`repeated_output`) available, so a transient local resource spike does not erase
the safest savings. Repeated Layer-0 latency budget breaches set a separate
`latency_budget_full_context` gate after three slow frames and recover after
cheap frames, so one spike does not disable savings but repeated local overhead
cannot degrade Codex UX. The latency demotion bucket is persisted under
`.slimference/runtime-budget` with a 30 minute TTL and capped strike debt, so a
daemon restart does not immediately forget recent local-overhead pressure, while
cheap frames still auto-recover without operator action. Readcache and WSS
tool-use/collapsed-key state use same-process memory plus short write-behind
flushes, so reconnect hydration is immediate while per-frame sync writes stay
out of the hot path. Readcache
write-behind flushes are version-guarded: a delayed disk flush can only mark the
same state revision clean, and it cannot overwrite a newer in-memory save made
while the flush was in flight. Windowed CPU and disk-write spikes also trip
host-budget attention, which demotes heavier managed Codex reducers until the
next healthy snapshot without turning off lossless exact repeat-read savings.
Exact token-count guards use the provider tokenizer, but large repeated texts
are cached by encoding, byte length, and SHA-256 content hash. This keeps
o200k/cl100k accounting exact while preventing repeated BPE regex passes from
becoming the local bottleneck on repeated Codex reads/searches.
These counters are emitted globally and under `proxy_layer0_routes.http` /
`proxy_layer0_routes.wss_phasef` through `/admin/state` and `aggregate-savings`,
so future cache or reducer work can measure which route and mechanism actually
saved tokens before broadening mutation surfaces. `workday-savings` carries the
same counters through start/finish deltas, and `wss-audit --admin-state-file`
can join a matching admin snapshot to show policy and cache decisions next to
the decisions-log route/session audit.
Codex tool metadata preserves `workdir` / `cwd` / `working_directory` /
`directory` when present. Relative single-file read commands are resolved
against that absolute workdir before readcache evaluation, which improves
repeat-read hit rate and prevents same-relative-path cache collisions across
repositories. Plain `git ...` commands with absolute workdir metadata become
`git -C <abs> ...`, so repeated git output keys stay repo-scoped. Safe shell
wrappers of the form `cd <abs> && <read>` are normalized to absolute `cat` /
`head` / `tail` / `sed -n` commands, and `cd <abs> && git ...` is normalized to
`git -C <abs> ...`. Search wrappers are not stripped until the path/key can be
made repo-safe. Codex tool outputs that arrive
as a single text part inside an `output` / `content` style array, or inside a
nested MCP-style object such as `result.content[0].text`, are extracted and
rewritten in place, preserving sibling non-text parts, metadata, and the
original object/array shape. Multi-text or otherwise ambiguous arrays fail open
instead of being stringified.

The readcache frontier is archive-backed for large observed tool reads. Full-file
and recognized ranged outputs (`cat`, `head`, `tail`, `sed -n`, and strict
single-file `awk` line ranges) are hashed and stored in the local content archive
while the session JSON keeps only the
hash/archive URI, avoiding unbounded session-state bloat. Ranged reads are keyed
by `path+offset+limit`, so `head -n 80 x` and `sed -n 120,180p x` never collide
with each other or with the full file. On a later same-session read, Slimference
expands the archive only when needed to build an exact delta; unchanged reads
use the stored content hash and archive URI without inflating the gzip payload.
If the archive is missing or the delta is not shorter, the original content is
sent unchanged. This keeps the savings path reconstructable and fail-open while
improving repeat-read hit-rate for both WSS and HTTP Codex traffic.

The same session state also tracks exact repeated non-file tool outputs. For
non-read commands, the reducer first applies deterministic captured-output
filters; if the candidate text is what will actually be sent upstream, its hash
is recorded under the resolved command key. A later identical command/output
pair in the same session can be replaced by a neutral archive-backed unchanged
output note. The mechanism is exact-only: changed output, short output,
unresolved commands, archive failures, and full-file reads fail open. Full-file
`cat` reads stay on the full-file read-delta path. Partial reads use their own
ranged read-delta key when the range is recognized; other deterministic commands
can still save through the exact command/output path when the same output repeats.
Repeated search outputs (`rg` / `grep` / `git grep`) also get position-aware
delta treatment, not just exact unchanged references, but only when the key is
repo-scoped through an absolute workdir, `cd <abs> && ...`, absolute search path,
or `git -C <abs> ...`. An implicit-cwd search can still use first-pass grouping,
but it does not seed cross-turn search collapse or search delta state. This keeps
repeat savings for commands such as repeated repo-scoped `rg`, `git status`,
build/test reports, partial file ranges, or custom deterministic tools without
introducing semantic summaries or cross-repo false hits. Codex WSS Phase-F
search-output reducer paths currently fail open before first-pass grouping and
repeated search delta, including grep-style search, path-list tools such as
`find` / `fd`, empty-result search tools, and output-inferred search payloads.
Fresh live scoped WSS sessions on 2026-06-07 and later Desktop retests showed
upstream `invalid_request_error` after broad WSS tool-output mutation even with
model-facing output-reduce disabled. Narrower search-key and
`previous_response_id`-only gates were insufficient because the next byte-equal
turn can still fail after the session state was already poisoned. HTTP, hook,
and non-WSS routes keep the deterministic search reducers; WSS allows only
proof-fresh, state-safe status compaction by default, while search/path-list and
broader tool-output savings must be re-certified with live captures before
returning to the default WSS product path.

Layer-0 reducer metadata is part of the safety contract. Every default reducer
declares its mechanism id, command family, safety class, required retained
fields, preserved evidence, and fail-open recovery path. The registry is used
for audits/control surfaces only; it does not add model-facing text.

Layer-0 cap handling is evidence-first, not first-N. Known default reducers that
truncate large outputs keep actionable rows before noise and sample the tail
inside important groups: test JSON keeps late failures, SARIF/ESLint JSON keep
late same-priority errors, kubectl JSON full-passes healthy lists and only
compacts unhealthy attention items, cargo metadata keeps late workspace members,
Terraform JSON keeps late destructive or state resource evidence, search grouping
keeps head/tail matches per file and file set, and the embedded default TOML
catalog keeps late diagnostic evidence when `max_lines`, `head_lines`, or
`tail_lines` would otherwise cut it away.
Omitted-count markers remain explicit, and malformed or non-shorter outputs
full-pass where the reducer owns that gate; operator-authored TOML rules remain
literal user configuration.

Git diff/show compaction is evidence-preserving: it keeps file paths, hunk
headers, added/removed lines, and structural diff metadata such as mode changes,
new/deleted files, renames/copies, similarity markers, and binary-file markers.
Context lines are stripped only after that metadata is retained. This avoids the
old silent loss where a rename-only or mode-only diff could collapse to a
`+0/-0` file entry without the reason for the change.

First-pass search outputs are grouped by `TryCompactSearchOutput` /
`groupSearchResults` (file -> match list with a `[tool] N match(es) in M file(s)`
header, capped at 30 files with `[+N more files]`) on HTTP and other non-WSS
routes. This grouping used to abandon the whole output on the FIRST colon-less
line, which a real-workload capture showed defeats it on every Codex search:
Codex truncates exec output to a token budget, so the captured `rg` payload
always ends in a cut-off line and carries a leading `Total output lines: N`
header - both colon-less. The grouper now SKIPS colon-less noise lines (header,
context separators, truncated tail) and only abandons grouping when nothing
parses or noise dominates (`skipped*2 > nonEmpty`). On the real captured `rg`
(402 matches, 79 files) this compacts 40 KB to ~9 KB (78%) on supported routes.
The same parser accepts normal `file:line:content`, Windows
`C:\path\file.go:line:content`, and dashed `file-line-content` match rows while
context/json/list/count/null separator modes still full-pass. When a file or
match list must be capped, first/last evidence stays visible and high-signal
rows (`error`, `fatal`, `timeout`, `rejected`, `warning`, `security`, `secret`,
`auth`, `todo`, `fixme`, etc.) are promoted into the visible window before
plain middle rows.
For Codex WSS Phase-F, search-output reducer paths are pass-through until the
WSS protocol shape is re-certified live. That includes grep-style output,
path-list output from tools such as `find` / `fd`, and search-looking output
inferred from shell wrappers or unresolved tool calls. The cost is lower WSS
search-token savings, but the product contract is stronger: no upstream 400s
and no model-facing context loss.

Stateful Codex WSS tool-output request bodies full-pass by default. The guard is
route- and shape-scoped, not a global savings kill switch: WSS routing,
byte-equal bridge/fallback, response-side diagnostics, concise-chat on eligible
non-tool chat frames, and HTTP/non-WSS source reducers keep their existing
gates. The guarded WSS tool-output shape stops claiming read-delta, search-delta,
exact repeated-output, or chunk savings until live evidence proves OpenAI's
current WSS contract accepts that mutation without 400s.

Repo-local policy command output is treated as workflow evidence and passes
through unchanged on every Codex Layer-0 route. The guard recognizes direct
policy-tool binaries, packaged variants, shell-wrapped invocations, leading
`cd <repo> && ...` forms, and Go development commands. These outputs are
intentionally not a savings surface: the token upside is small, while preserving
exact policy, audit, hook, and task evidence avoids confusing Codex or the
operator during workflow-state checks.

Slimference also preserves third-party Codex hooks during Slimference hook
install and removal, so repo-local policy gates can remain active while
Slimference handles scoped transport and safe output reduction.

Codex content-defined chunk dedup is available as a policy-gated extension of the
same Layer-0 reducer. A multi-plan chunker splits large tool outputs/file reads
into content-addressed regions: FastCDC handles general binary/text overlap, and
a line-boundary plan handles long logs and command outputs where line stability is
stronger than byte-window stability. A bounded in-memory session store tracks only
chunk identities, not raw payloads. When a later output shares chunks with content
already sent to the model in the same session, the reducer chooses the best locally
verified plan and emits the novel bytes plus neutral
`[context-chunk status=unchanged uri=local-archive://...]` references to archived
chunks. The product default is
`codex_savings_policy_mode="auto"`: safe lossless reducers stay on, and chunk
dedup is limited to recoverable WSS paths with archive support, cross-send
seeding, density budgets, local decode verification, and patch/diff/edit-output
guards. Same-batch edit uncertainty also demotes only chunk dedup: if the current
Layer-0 batch carries an edit/apply-patch/write signal, fresh command/search/log
outputs stay full context while lossless read-delta and exact repeated-output
reducers may still run. Cross-send seeding means repeated chunks first
encountered inside the same model-facing output stay verbatim and only seed
future overlap; references are emitted only for chunks that were known before the
current output started.
Commands such as
`apply_patch`, `patch`, `diff`, `colordiff`, patch/diff file reads,
`git diff`, `git show`, `git log -p`, `git apply`, `git am`,
`git format-patch`, `gh pr diff`, `gh pr view --patch`, `jj diff`,
`jj show`, `hg diff`, and `svn diff` do not receive chunk references, because
those outputs are fresh reasoning material for edits and reviews. Normal
search/status commands, including searches whose pattern is the word `diff`,
remain eligible. Patch/diff outputs can still use safer deterministic filters
or exact repeated-output collapse when eligible. Chunk dedup becomes eligible
only when the output is large enough and no recency/context-risk signal asks for
full text. The legacy
`codex_chunk_dedup_enabled=true` toggle remains as an explicit override for
conservative policy, not as the normal product path.
`codex_chunk_dedup_proof_level` records the content-free proof level that policy
may trust for automatic promotion (`none`, `unit`, `replay`, or `live`). The
current default is `live` because the release proof corpus contains positive CLI
and Desktop chunk-dedup evidence with zero safety issues. If an operator lowers
that value, `auto` shadows chunk dedup instead of emitting archive-backed
references; `max` still requires at least replay proof unless the explicit
operator override is set.
Runtime demotion inputs also cover quality spikes, archive recovery loops,
missing-tool retries, degraded routes, host-budget pressure, chunk
session-integrity budget pressure, and negative-savings history. Any supplied
demotion signal full-passes the affected managed Codex tool-output reducer and
records the exact content-free reason in mechanism telemetry.
Per-output and cumulative session reference-density caps are enforced as byte
budgets during encoding, not as a crude all-or-nothing rejection. A candidate can
replace repeated chunks only until the remaining budget is exhausted; repeated
chunks past that point stay verbatim in the model-facing output, preserving fresh
recency while still capturing the safe part of the overlap. The cumulative session
budget's denominator counts every observed output sent through the chunk store,
including first-send seed outputs and candidates that produced no accepted
references. Those bytes were visible to the model and therefore increase the safe
budget for later references instead of blocking the first useful overlap hit.
Layer-0 also asks the chunk store for a content-free "budget available after this
candidate" signal before policy evaluation. If the session budget cannot support
another useful chunk reference, the policy full-passes only chunk dedup with
reason `session_integrity_budget`; lossless read-delta and exact repeated-output
reducers remain eligible. This avoids spending hot-path CPU on a recoverable
reference mechanism that the integrity budget would reject while preserving the
safe cache hits.
The store is bounded by `codex_chunk_dedup_max_sessions`,
`codex_chunk_dedup_max_chunks_per_session`, and
`codex_chunk_dedup_ttl_seconds`; the default min block size is 4096 bytes so
auto mode catches Codex's observed ~8 KiB truncated exec-output envelope. It
fails open if archive recovery is unavailable or the token guard is not positive.
WSS Phase-F and HTTP share the same reducer primitives, but only WSS can
currently emit chunk/archive references because it can inject the recovery note
automatically when a reference is actually emitted. For Codex Responses bodies the
note is written to the top-level `instructions` string, never as a `system` item
inside `input`, because Codex's backend rejects `input` system messages. HTTP stays
conservative and does not emit chunk/archive references until that route has its
own proven recovery-note wiring. `/admin/state` reports chunk-dedup hits globally plus
under `proxy_layer0_routes.wss_phasef` / `.http`.
The 2026-05-30 T255 live proof used scoped Codex WSS frames with
`--codex-chunk-dedup --chunk-dedup-min-bytes=0 --fail-on-lost`: replay saved
7757 model-facing bytes with `gate_passed=true`, and live counters reported
1707 billable input tokens saved plus one global and WSS-route chunk-dedup hit
with zero parse, degraded-session, or compression errors. The follow-up T256
policy engine makes that proof usable by default through auto mode instead of
requiring operators to decide when a raw feature flag is safe.

Real log-heavy Codex.app workloads can be handled before chunk dedup by safer
deterministic filters. A 2026-06-02 Desktop proof with two ~40 KB similar log
outputs saved 16,192 billable input tokens through captured-output compaction,
`lost=0`, and zero safety counters; chunk dedup correctly recorded no live block
because the earlier reducer produced a stronger, lower-risk compaction. Replaying
the pre-filter log capture through the chunk store after the line-boundary plan
saved 7155 reducer tokens with 16 archive-backed references and exact
reconstruction. Product interpretation: chunk dedup is a fallback/overlap lever
for large similar outputs that survive stricter deterministic reducers, not a
reason to bypass safer log/search/test parsers.

First-read scan-mode (T253) is retired from Codex runtime. Earlier prototypes replaced
large first file reads with code signatures plus recovery notes. That mechanism is no
longer policy-reachable, has no apply environment flag, and has no live counters in
`/admin/state`. The invariant is enforced by tests: Codex first-read file outputs
full-pass in `auto`, `conservative`, and `max`. If a future design wants similar
savings, it must be rebuilt as a default-safe cache-hit mechanism that does not weaken
first-read information or cannibalize the lossless read-delta/chunk seed.

Model-facing readcache replacements use neutral `[context-* ...]` markers and
preserve the `local-archive://<id>` pattern without naming Slimference inside
tool output. This keeps the mechanical recovery handle while reducing prompt
contamination from product-specific marker text. Archive expansion remains
opportunistic: the proxy can expand a later incoming request that quotes a
stored URI. A neutral once-per-session WSS archive-recovery note exists behind
`archive_recovery_note_enabled`; it is default-off and injects no product name.
When enabled, it tells the model that `local-archive://<id>` may be requested if
full elided content is needed. `read_delta_recent_full_pass_turns` is also
default-off (`0`): operators can raise it after A/B proof to keep immediate
cross-turn re-reads full when recency matters more than dedup savings.
The auto policy adds a stronger runtime guard: after a collapsed key is
deliberately re-read, that key full-passes for the rest of the socket/session so
recency beats further savings. Recently edited reads also full-pass. These are
not manual toggles; they are the central policy's response to model-attention
signals.

`go run ./scripts/utils workday-savings start|finish` is the real-workday
measurement ceremony. `start` captures a baseline from `/admin/state` (and
optionally the filter DB); `finish` captures the current state and prints the
counter delta. Operators must close Codex CLI/Desktop sessions before `finish`
so WSS counters flush. The report keeps route-ready separate from
savings-proven: positive mutation/token counters are the proof, not the fact
that a WSS route was bridged. The same report also keeps the current Codex
route / auto-recert snapshot: auto mode, selected transport, WSS certification,
bridge availability, `needs_recert`, fallback reason, recert status, attempt id,
repair timestamps, last error, and bounded recert log path. Workday windows can
therefore explain whether savings were active, bridged, repaired, or in
fallback instead of only reporting token deltas. The finish report also carries
the host-resource budget snapshot: RSS, CPU-window percentage, disk-write delta,
state bytes, compression/degradation health, status, and reasons. Release proof
uses that snapshot as a promotion gate, because positive savings are not enough
if local resource cost would hurt normal Codex operation.

The 2026-06-02 strict T257 release gate completed with 14 local scoped WSS
captures. The capture matrix covered 9 CLI captures, 5 Desktop captures, all 10
required workload classes, 11 positive live-token-savings captures, 3
expected-zero controls, 43,113 live billable/input tokens saved, `lost=0` in
A/B replay, `captures_with_issues=0`, and safety counters `parse_failures=0`,
`degraded_sessions=0`, `compression_errors=0`. The clean CLI workday window
saved 372 billable
WSS-input tokens on an archive-backed `git status --short` workload with
`phasef_bridged=1`, `compressed_messages_mutated=1`, `frames_reencoded=1`, and
zero parse, degraded-session, or compression errors. The clean Desktop workday
window historically saved 382 billable WSS-input tokens on an archive-backed
`rg -n TODO` workload with `phasef_bridged=2`, `compressed_messages_mutated=1`,
`frames_reencoded=1`, and zero parse, degraded-session, or compression errors.
Fresh 2026-06-07 scoped WSS sessions later showed upstream 400s after WSS
search-output mutation, and later Desktop sessions showed the same class after
broader WSS tool-output mutation. Those rows are kept as historical replay/proof
evidence, not as broad default-WSS promotion claims. Current WSS allows only
proof-fresh, state-safe status compaction by default; search/path-list,
source-like, inferred search, and `find`/`fd` path-list payloads still fail open
until separately re-certified. The strict
matrix still proves reducer mechanics and route breadth; HTTP/non-WSS Codex
routes keep the deterministic read, ranged-read, git, exec-envelope, no-savings,
and mixed-workday reducers in the product path. The
2026-06-02 strict matrix additionally covered repeat reads, ranged reads, search
loops, git-status compaction, apply-patch/read safety, changed-file safety,
similar-file safe-zero behavior, test-failure safe-zero behavior, no-savings
controls, and a mixed Desktop workday through the same product path. The mixed
Desktop row alone saved 8,394 live billable/input
tokens with `read_delta=1`, `captured_output=1`, `codex_exec_envelope=1`,
`lost=0`, and zero safety errors. In this strict release proof,
`repeated_output` and `chunk_dedup` did not record live block hits and are not
part of the 43,113-token claim. The similar-files capture stayed expected-zero /
net-negative for default-auto.
After the session-budget denominator fix, the real CLI chunk probe capture
`chunk-live-cli-similar-output-20260602T150301.jsonl` replays through default
auto with `reducer_tokens_saved=6636`, `reducer_chunk_dedup_blocks=1`,
`reducer_chunk_dedup_references=4`, `bytes_saved=32195`, and `gate_passed=true`.
That is a reducer/replay proof on real WSS frames, not yet a fresh live-token
matrix claim.

`go run ./scripts/utils wss-audit` also reports a content-free re-read canary:
the number of WSS request summaries that repeated a resolved read/tool key and
the total repeated count. A non-zero canary is not automatically bad, because
repeat reads are also the highest-value savings workload. It is the live signal
to compare with positive savings, recent-edit guards, and future comprehension
A/B results when deciding whether a session needs looser compression. On the live
WSS reducer, a post-collapse deliberate re-read of the same read key suppresses
further collapse for that key for the rest of the session, restoring full recency
instead of fighting the model's attention signal.

Savings-proven and comprehension-preserved are intentionally separate claims.
Positive mutation and input-token counters prove Codex WSS savings for a
workload. They do not, by themselves, prove that every future response preserves
model comprehension. Deterministic reducers stay guarded by reconstruction,
token-decrease checks, recent-edit observations, content-free canaries, and
byte-equal fail-open; broader semantic compression or archive-instruction
recovery requires separate fixture and live proof before default-on promotion.
`internal/proxy.RunWSSPhaseFABReplay` is the offline bridge for that proof: it
replays decompressed Codex WSS frames through the real Phase-F reducer, extracts
the direct and compressed model-facing request context, and feeds both into
`internal/abharness.Compare`. The CI-covered fixture proves repeat-read
read-delta is recoverable because the first full read was already sent, and that
the recovery note is visibly audited as an expected extra model-facing context
change when a recoverable-reference mechanism needs it.
The replay extractor includes Codex Responses top-level `instructions` as
model-facing system context, so recovery-note or output-reduce hint injection
cannot bypass the no-drawdown comparison just because it does not live in
`input`.
Known output-reduce directive suffixes are audited separately as expected
instruction extras: the direct instructions must remain a prefix, the suffix
must contain the output-reduce marker, and unknown instruction rewrites still
count as lost context under `--fail-on-lost`.
The harness aligns inserted recovery-note/system blocks before comparing changed
tool-output blocks, so note insertion cannot create false content-loss findings.
For chunk dedup, the harness expands every `[context-chunk ... local-archive://...]`
reference and compares the reconstructed block to the exact direct model-facing
source; a URI by itself is not enough to pass the no-loss gate.
`go run ./scripts/utils wss-ab-replay <frames.jsonl> [--json|--fail-on-lost|--archive-recovery-note|--tool-output-mutation|--codex-chunk-dedup]`
is the operator-facing report wrapper. With default config it mirrors the
product WSS guard and keeps stateful tool-output request bodies byte-equal.
`--tool-output-mutation` enables the lab/proof replay path for historical and
focused mechanism proofs; `--codex-chunk-dedup` remains a force flag for
threshold experiments and implies tool-output mutation, the recovery note, and
separation of the expected once-per-session recovery-note extra block from true
loss-gate failures. The report separates two concepts: `bytes_saved` is the
comprehension A/B byte delta after archive expansion and note alignment, while
`reducer_tokens_saved`, `tool_output_mutation_enabled`, and the `reducer_*`
mechanism counters report the model-facing compressed request savings from that
specific replay mode. Its JSONL input is content-bearing by definition, so it
belongs in local/private
captures only; it does not read auth headers or WebSocket upgrade metadata. Each
replay uses an isolated temporary home directory so disk-backed
readcache/tooluse/archive state from prior live sessions cannot skew the A/B
result.
Set `SLIMFERENCE_WSS_AB_CAPTURE=/private/path/frames.jsonl` on the Slimference
daemon process to append those replay frames during a scoped Codex WSS session.
The capture hook records only decompressed JSON frame payloads and direction,
before any Phase-F mutation; it is disabled unless the env var is set.
`slimference start` preserves the caller environment when spawning the detached
daemon, so this capture env var works through the normal lifecycle command.

---

## 5. Layer 1 - Deterministic Compression

`internal/compression/layer1.go::DeterministicCompressor.Compress`
orchestrates 15 sub-layers. Execution order follows `docs/spec.md` plus the T143
semantic frontier:

| # | Sub-layer                          | File                           |
|---|-------------------------------------|--------------------------------|
| 1 | ANSI / control-char strip          | `ansi_strip.go`                |
| 2 | JSON minify                        | `json_minify.go`               |
| 3 | Comment strip (38 path languages)  | `comment_strip.go`             |
| 4 | Exact dedup + archive-backed near dedup | `dedup.go` + `dedup_minhash.go`|
| 5 | Structure extraction               | `structure.go`                 |
| 6 | Delta encoding (LCS unified diff)  | `delta.go`                     |
| 7 | Tool classifier                    | `tool_classifier.go`           |
| 8 | Tool-type-aware compression        | `tool_compressor.go`           |
| 9 | Success-shortcircuit               | `success_shortcircuit.go`      |
|10 | Image-block replace                | `image_replace.go`             |
|11 | Reversible path dictionary         | `semantic_dictionary.go`       |
|12 | Repeated-line collapse             | `repeated_collapse.go`         |
|13 | File-op graph pruning              | `graph_pruning.go`             |
|14 | Prefilter tag                      | `prefilter_tag.go`             |
|15 | Loop nudge (T37)                   | `loop_detect.go`               |

Plus:
- Structure-aware preview for oversized tool results (T38 / T55).
- Prompt-cache breakpoints (T23, T45).

### Layer 1 safety registry

`compression.Layer1SubLayerRegistry()` is the control-plane contract for Layer 1.
It classifies each sub-layer as exact, reversible, recoverable-with-archive,
task-preserving compaction, or non-default. The registry also records default
eligibility, whether an archive is required, the model-risk being controlled,
and the recovery path. The executor enforces that contract for archive-required
mutations: if the original block cannot be archived and stamped with a valid
archive id, the block full-passes and its per-block savings counters are reset.
Exact and reversible transforms can stay automatic; context-dropping compactions
must stay archive-backed or be bypassed. Exact dedup remains reversible without
an archive because the referenced block was already model-facing in the same
context. MinHash/near-dedup is not exact and is therefore classified separately
as `dedup_near`: it full-passes unless the current omitted block is archived and
stamped, and its decision telemetry is reported separately from exact `dedup`.
The same archive-required rule applies to side paths such as
`structure_in_window`: even when an in-window tool-result block is eligible for
structural extraction, the original must archive successfully before the compact view
can replace model-facing text.
Success short-circuit compactions follow the same rule: a verbose success-only
tool output can become an `[ok]` marker only when the original output has been
archived, so the marker never becomes an unrecoverable source of truth.

Every Layer 1 compression call also emits content-free `layer1_decisions`
telemetry in the proxy decisions log. Each record names the sub-layer, safety
tier, attempted flag, applied flag, reason, saved-token count, archive
requirement, recovery path, archive-write count, and default eligibility.
`attempted=false` means the workload never reached that sub-layer's reducer,
not merely that it saved zero tokens. Archive-write count is incremented only
after the recorder returns a non-empty archive id, so an archive-required
positive decision can prove that recovery material was actually written. This is
an audit/control surface only: it does not add model-facing text and does not
change compression output. It lets proofs separate "not attempted",
"attempted but not applicable", "full-passed because archive recovery was
unavailable", and "applied with positive savings and a concrete recovery
record" per sub-layer.
The near-dedup regression fixture uses `DiskRecorder` plus
`contentarchive.Get` to prove that a similar-but-changed omitted block expands
back to exact original bytes. A broader Layer-1 corpus guard exercises multiple
historical messages and archive-backed mutations, then expands every emitted
archive id and verifies exact original block bytes plus session, message, block,
and sub-layer metadata. This is the reconstruction boundary for default Layer 1:
future archive-required sub-layers must pass the same proof shape before they
can run default-auto.

Layer 1 also respects provider-cache boundaries. Any content block that already
carries `cache_control` is skipped by the Layer-1 block mutators, even if a
local sub-layer could save a few tokens. This prevents local compression from
rotating or weakening a provider-cached stable prefix and keeps prompt-cache
economics ahead of small local wins.

### Reversible path dictionary (T143a)

`semantic_dictionary.go` aliases repeated absolute local paths inside one
tool-result block only when the embedded legend plus aliases are strictly
shorter. It preserves reversibility by prepending a small dictionary such as
`[P1]=/Users/.../file.go`, then replacing repeated body occurrences with
`[P1]`. The marker is neutral (`[path dictionary]`) and product-name-free so the
model reads it as tool-output notation rather than a third-party speaker. It is
deliberately narrow: known local filesystem roots only,
minimum path length and occurrence gates, URL-style paths ignored, and no
application when the legend would create a negative saving.

### Structure extraction frontier (T143b)

Structure extraction now covers the main code stacks plus high-volume text and
config formats. `structure_more.go` adds Markdown, SQL, GraphQL, HCL,
container build files, and Makefile summaries on top of Go,
TypeScript/JavaScript, Rust, Python, C/C++, Java, Ruby, shell, Zig, Swift,
Kotlin, PHP, Dart, Scala, Elixir, Solidity, and Svelte.

The new text/config summaries are deliberately lossy but recoverable through
the existing content archive. They keep only structural markers: Markdown
headings/lists/tables/fences, SQL DDL/DML/constraint clauses, GraphQL/HCL
top-level blocks, container build image/control instructions with run chains
collapsed to a command count, and Makefile includes/variables/targets.
The `structure_min_tokens` gate is evaluated with the local tokenizer, falling
back to byte/4 only if tokenizer initialization fails. Negative-saving bypass
still applies before any compacted block is used.

### Semantic test-failure compaction (T143d)

`stacktrace_compact.go` runs inside the existing L1 tool-output compressor for
large `ToolTypeTestOutput` blocks that actually look like stack traces. It keeps
failing test anchors, assertion/diff lines, top application source frames, and
package/status summaries, then collapses framework/vendor frames and excessive
diff/context lines with explicit omitted-count markers. The existing
shorter-than-original guard still decides whether the compacted block is used.

### Adaptive dedup staircase (T53)

Jaccard threshold lowers as the conversation grows
(`[compression.tuning.dedup_staircase]`):

| Messages | Threshold | Rationale                                 |
|----------|-----------|-------------------------------------------|
| 0-10     | 0.88      | Short session; tighter to avoid collapse. |
| 11-20    | 0.85      | Pre-T53 default.                          |
| 21-40    | 0.82      | Near-duplicates accumulate.               |
| 41+      | 0.78      | Long session; aggressive dedup pays off.  |

Empty staircase or invalid step falls back to
`Compression.DedupSimilarityThreshold` scalar.

### Tool-compressor tuning (T61)

Tool-compressor heuristics now live in
`[compression.tuning.tool_compressor]`:

| Field                          | Default |
|--------------------------------|---------|
| `aggressive_after_multiplier`  | 2       |
| `git_moderate_diff_limit`      | 60      |
| `test_max_failure_lines`       | 40      |

`SetToolCompressorTuning` installs these at proxy boot; zero/negative
fields fall back to the compile-time defaults.

### Structure preview (T38 / T74 / T76)

`[compression.tuning] structure_preview = true` is the default after T76's
content-archive foundation. Oversized tool_result blocks with JSON /
path-list / ASCII-table shape can be replaced with a compact, shape-aware
preview when strictly shorter, while archive-backed recovery keeps the
original body locally retrievable.

---

## 6. Retired Semantic Summary Path

The old semantic summary path has been removed from the product and codebase.
Slimference no longer ships side-channel summarization, local LLM
summarization, OCRL full-history replacement, or context-ledger insertion as a
model-facing savings path.

The reason is product safety: any semantic replacement of old context can drop a
detail the model later needs, which violates the project drawdown rule. Savings
therefore stay on deterministic, recoverable, fail-open mechanisms: Layer 0
Codex/tool-output reducers, Layer 1 deterministic compression, Layer 2 cache
leverage, and Layer 3 output/tool-surface reduction.

There is no semantic-summary config surface, no summary CLI subcommand, no
background summary worker, no summary cache apply, and no model-facing context
ledger mutation in the current product. Historical task files may mention the
retired experiments, but current code and product docs must not require them.

---

## 7. Layer 2 - Response Cache

`internal/caching/response_cache.go` is an LRU keyed by the SHA-256 of
the provider, HTTP method plus route path/query, request-affecting Slimference policy
partition, canonical request body, and pertinent headers. Hits skip Layer 1 and serve the cached upstream response. Method and route keying
are deliberate: the same body on two methods or provider endpoints cannot alias. Policy keying
is equally deliberate: local response-cache hits do not cross request policies
such as stop-sequence injection or be-terse treatment cohorts, because those can
change the upstream model response even when the user body is identical.

The local response cache only serves explicitly deterministic, non-tool request
shapes. Missing sampling fields are treated as provider defaults, not as proof
that replay is safe. Streaming, non-zero temperature, top-p sampling, multiple
completions, tool definitions, explicit tool choices, function-call fields,
tool/function roles, and Responses function-call outputs all full-pass upstream.
Routes that can create or continue upstream server state are also fail-closed:
Responses requests must explicitly set `store:false`, and any
`previous_response_id`, conversation, thread, assistant, nested metadata
session/conversation/thread/assistant marker, or Codex turn-metadata marker
full-passes upstream. Local replay is allowed only for stateless deterministic
requests, because skipping upstream response-id creation or conversation-state
updates would change workflow state even if the visible text matched.
This keeps Layer 2 from replaying a cached tool workflow or a fresh model sample
where timing, tool state, or stochasticity matters.

Provider-cache accounting is intentionally split by provider shape. Anthropic
`cache_read_input_tokens` and `cache_creation_input_tokens` are recorded as
provider-cache read/create tokens. OpenAI and Codex `cached_tokens` are treated
as provider-cache read tokens for product/admin savings, while debug flight
records preserve the provider-specific cached-input shape so reports can avoid
double counting. HTTP and WSS `response.completed` usage both feed the same
admin-state savings rollup. Provider-cache numbers remain separate from local
response-cache hits and output-wire savings.

Output-reduce proof runs can override the runtime profile without editing the
config file via `SLIMFERENCE_OUTPUT_REDUCE_PROFILE` and
`SLIMFERENCE_OUTPUT_REDUCE_MIN_INPUT_TOKENS`. These env overrides are scoped to
the daemon process that receives them; they do not change product defaults.

### Invalidation

`internal/caching/file_watcher.go` watches every file referenced by
recently-cached tool calls (via fsnotify). A write invalidates every
cache entry whose key was computed from a body mentioning that path.

### Prompt-cache breakpoints (T45)

`internal/compression/prompt_cache.go::OptimizeCacheBreakpoints` places
up to 4 `cache_control: {type: "ephemeral"}` markers on the messages of
the stable prefix when the prefix is at least 1024 estimated tokens.
Breakpoints are selected by expected cache value: large stable
`tool_result` blocks first, then late stable user/assistant/tool turns,
with deterministic tie-breaking. The caller-owned message slice is not
mutated. This keeps Anthropic cache hints on stable content while avoiding
cache-control overhead on tiny one-shot requests.

Cumulative injection count:
`/admin/status.prompt_cache.breakpoints_injected_total`.

### Double-keyed pre-compress lookup (T20)

Cache is consulted *before* running Layer 1 (key = SHA256 of original
body) and *after* Layer 1 (key = SHA256 of compressed body). A pre-L1
hit avoids the pipeline entirely; a post-L1 hit short-circuits the
upstream call.

### Response-cache TTL

Default 5 min; configurable via `[cache] response_cache_ttl_seconds`.

---

## 8. Provider Support

`internal/types/types.go::Provider` has three values:

```go
Anthropic       // /v1/messages
OpenAI          // /v1/chat/completions (plain API)
CodexChatGPT    // /backend-api/codex/* OR any /v1/* with "codex" in UA
```

### Detection (T66)

`internal/proxy/provider.go::detectProviderWithUA`:

1. Path contains `/backend-api/codex/` → CodexChatGPT.
2. Path contains `/messages` → Anthropic.
3. User-Agent contains "codex" → CodexChatGPT (catches Codex's
   `/v1/responses` endpoint via `openai_base_url`).
4. Path contains `/chat/completions` → OpenAI.
5. Body probe: `max_tokens` + not `frequency_penalty` → Anthropic.
6. Fallback: OpenAI.

### Upstream routing

Config: `[upstream.*] base_url`:

| Provider      | Default                    | ENV override                                       |
|---------------|----------------------------|----------------------------------------------------|
| Anthropic     | `https://api.anthropic.com`| `SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL`          |
| OpenAI        | `https://api.openai.com`   | `SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL`             |
| CodexChatGPT  | `https://chatgpt.com`      | `SLIMFERENCE_UPSTREAM_CODEX_CHATGPT_BASE_URL`      |

### Header forwarding

All headers flow through verbatim except `Host` (rewritten to upstream
authority). `Authorization`, `User-Agent`, `OpenAI-Beta`, and `Cookie`
are preserved so Cloudflare + upstream see the same identity they
always see from this user. The proxy does not add an upstream-identifying
header.

### Codex request-body compression (T73)

Codex support is not only routing. Known Codex request shapes now enter
the same Layer 1-3 compression path:

- OpenAI-style `messages` bodies are parsed through the existing OpenAI
  normalizer.
- Responses-style `input` arrays map `message`, `function_call`,
  `function_call_output`, local-shell call/output variants, direct
  `command`/`args` arrays, `cmdline`/`shell_command` aliases, read path
  aliases, `aggregated_output`, `stdout`, and `stderr` into `types.Message`.
- Codex CLI exec envelopes (`Chunk ID`, exit code metadata, `Output:`)
  are treated as transport metadata: Layer 0 compacts the payload after
  `Output:` and preserves the header.
- `/v1/responses` is considered compressible only after User-Agent/body
  detection classifies it as `CodexChatGPT`; generic OpenAI Responses
  traffic remains passthrough.
- `/backend-api/codex/*` is a potential Codex compression path, but unknown
  body shapes return to byte-equal passthrough.
- Rebuild preserves body-level fields such as `conversation_id`, `metadata`,
  `stream`, and `store`, plus auth/session headers in the forwarded request.
  `x-codex-thread-id`, `x-codex-conversation-id`, and `x-codex-session-id`
  are also accepted as strong local reporting identities when the body lacks
  thread metadata; these headers are forwarded unchanged.

### Server-side state lever (T78)

`[proxy] server_state_enabled = true` activates the T78 lever for
providers whose capability map sets `SupportsResponseID = true`
(OpenAI, CodexChatGPT). On follow-up turns the proxy:

1. Pulls a session key from the body (OpenAI:
   `metadata.session_id` → `metadata.conversation_id` →
   `previous_response_id`; CodexChatGPT: top-level `conversation_id`).
2. Looks up the last upstream response id stored for that session.
3. Rewrites the request: history collapsed to the last user turn,
   `previous_response_id` injected.
4. Captures the upstream response id from the non-streaming response
   body and stores it for the next turn.
5. On a 4xx whose error mentions `previous_response_id`,
   `response not found`, or `conversation not found`, forgets the
   anchor, retries the original full body once, and continues.

Anthropic stays untouched (capability says no). Default off so traffic
shape stays identical until you opt in. Counters at
`/admin/status.server_state.{sessions,skip_total,recover_total}`.
Streaming response-id capture is deferred — SSE replies do not yet
seed the next-turn anchor.

### OpenAI prompt-cache steering (T136/T285)

`[proxy.openai_prompt_cache]` is a deterministic request-hint layer for generic
OpenAI API traffic. Slimference first builds a stable-prefix plan from
`messages` / Responses `input` arrays plus stable top-level fields
(`instructions`, `system`, `developer`, `tools`). Only content before the
final user turn is eligible; the latest user turn is excluded so normal prompt
edits do not rotate the cache key. The hint gate uses stable-prefix tokens,
not whole-request tokens, so one-turn requests do not pay cache-hint overhead
just because the latest prompt is large.

Generated `prompt_cache_key` values are privacy-safe hashes over stable-prefix
shape. The default strategy is `model_stable_prefix`, which reuses the same key
across sessions when the same model sees the same stable prefix, and rotates on
model, stable-prefix, or tool-schema changes. `stable_prefix`, `session`,
`model_session`, `static`, and `off` remain supported for operators that need a
different cache-key cardinality. Generated keys never contain raw prompt text,
session IDs, or full local paths.

`prompt_cache_retention` remains off by default. Operators may set `in_memory`
or model-gated `24h`; `auto` leaves the provider default untouched. Existing
caller-owned fields are preserved, and a per-key rate cap disables the hint
before it can create high-cardinality cache churn. If OpenAI rejects the fields
with a relevant 4xx response, the proxy retries once without those hints while
preserving any server-state rewrite, then suppresses prompt-cache steering for
that provider/model for 30 minutes. Debug/flight telemetry records only
content-free fields: applied/reason, retention, stable-prefix token estimate,
and stable-prefix hash.

Provider usage is also fed back into a per-key negative-net guard. After 2
negative samples and at least 1024 net-lost provider cache tokens, only that
generated key enters a 30-minute cooldown and future requests omit optional
cache hints with `reason=negative_net_cooldown`. A single create-only warmup
does not disable the key. Other keys keep working, and the model-facing prompt
content is unchanged. Savings cost estimates are conservative: cache-create
tokens are subtracted from the cache-read discount equivalent before estimated
cost saved is reported. Admin status, prompt-cache reports, proxy gain reports,
and savings summaries all use the same conservative net-read estimate so
cache-warmup cannot appear as a positive saving before read tokens have paid it
back.

CodexChatGPT backend routes do not receive these fields until T140 captures
live request acceptance.

`slimference gain --proxy` includes provider cache read/create/net accounting
and a content-free prompt-cache heat map grouped by stable-prefix hash. Each row
records request count, hint applied/skipped counts, maximum stable-prefix token
estimate, provider cached tokens, provider cache read tokens, cache create
tokens, and cache net tokens. JSON exposes the full fields, CSV includes
provider-cache read/create/net and negative-net request counts, and text output
prints the hottest five hashes. These rows explain cache behavior; provider
cache credits remain provider/accounting evidence, not claimed local deletion.

### Reversibility-by-default (T76)

Layer 1 archives original block content via `internal/contentarchive`
before any lossy mutation; the rewritten block carries an `archive_id`
reference and the proxy opportunistically re-injects archived content
when a follow-up request quotes a `local-archive://<id>` URI. T76
lets `structure_preview` ship default-on (T74 risk closed) and
unblocks T100 cross-direction coordinator + T103 tool-definition
pruning. Telemetry: `/admin/status.content_archive`.

### Quality calibration loop (T77)

Three lightweight signals run alongside existing analytics, none of
them require an LLM round-trip: a per-session re-read detector, a
rolling prompt-cache hit-ratio drop alarm, and a net-savings tally
that subtracts an estimate of cache-invalidation cost from raw
savings. Surfaced via `/admin/status.quality`.

### L1 message-level fan-out (T104)

`[compression.tuning] coordinator_parallel` runs `compressMessage`
concurrently per message in the compressible prefix when the prefix is large
enough to amortize goroutine overhead. Small prefixes stay sequential. Parallel
work is bounded by `runtime.GOMAXPROCS(0)`, output order is preserved, the
`archiveOriginal` recorder is mutex-protected, the `coordinator_skipped`
counter is atomic, and the receiver-local session/coordinator fields are
serialized per Compress call so the hot path stays race-clean. The auto-gate is
default-on because it changes CPU scheduling only, not model-visible content.
Note: shipped at message granularity, not the spec's stage-partitioned sub-layer
concurrency; reopens as T104b only if profiler evidence proves message-level
granularity is the wrong knob.

### Mid-exchange summary (T99)

`[compression.tuning] mid_exchange_enabled` is legacy configuration. Product
runtime does not inject in-progress summary blocks because that is model-facing
context replacement. The active product behavior keeps model-facing context
byte-equal except for the current deterministic reducer stack; there is no
context-ledger insertion path.

### Layer 3 tool-definition pruning (T103)

`[compression.tuning] tool_prune_enabled` activates the per-session
tool-usage tracker + body-rewrite pass. Tool definitions idle
beyond `tool_prune_idle_threshold_turns` (default 20) are removed from `tools[]` for Anthropic
(`tools[].name`) and OpenAI / CodexChatGPT (`tools[].function.name`
or top-level `tools[].name`). Telemetry at
`/admin/status.tool_prune.{sessions,pruned_total,reattach_total,
miss_total,retry_total,always_keep_total,disabled_sessions,
tokens_saved_sum}`. Default off.

T151/T268 make the pruner soak-safe enough for wider testing: shell,
edit, read, safety, browser, and MCP tool classes are always kept, and
`tool_prune_always_keep = []` can add project-specific exact tool names with
case-insensitive matching.
Focused tool-heavy proof runs can enable the pruner without editing the config
file via `SLIMFERENCE_TOOL_PRUNE_ENABLED=1`, shorten the proof-only idle window
via `SLIMFERENCE_TOOL_PRUNE_IDLE_THRESHOLD_TURNS=1`, and provide
comma-separated project keeps via `SLIMFERENCE_TOOL_PRUNE_ALWAYS_KEEP`.
The Codex WSS Phase-F path uses the same strict pruner for prompt/user-turn
request bodies. WSS tool-call frames feed tool-name usage into the session
tracker, but actual `tools[]` mutation only happens on prompt/user turns with a
known Codex tool schema. Unknown or mixed schemas stay byte-equal.
Pruned definitions are archived by session and tool name. A later tool-name
mention, safe alias (`GetWeather` -> "weather", `send_email` -> "email"), or
command-family hint in current user/system/developer instruction text reattaches
the definition before pruning runs again. Historical assistant text and tool
stdout/stderr do not trigger reattach, so old logs cannot silently add schema
tokens back to a later unrelated turn.
Reattached definitions are appended in deterministic tool-name order to avoid
avoidable prompt-cache churn, and the reattached tool names count as active for
the same prune decision so the idle pass cannot immediately remove the recovered
tool again. Reattach is schema-safe: cached pruned definitions are only consumed
after a successful safe reattach, and malformed or unnameable existing `tools`
entries full-pass unchanged instead of being rewritten. If the upstream returns
a conservative missing-tool 4xx, including common provider phrasings such as
unknown tool, no tool named, not in available tools, tool/function not found, or
not a valid function, the proxy retries once with the full pre-prune schema,
records miss/retry telemetry, and disables future pruning for that session
bucket. `slimference
gain --proxy` includes tool-prune saved-token, pruned-tool, reattach, miss, and
retry totals from the decision log.

### Posttool cross-session repetition marker (T93)

The `slimference posttool` hook records each `(session_id,
tool_name, command, output)` tuple in `~/.slimference/repetition.db`.
The row also keeps `first_turn_id` and `last_turn_id` for debug/provenance, but
the identity stays session-wide so useful cross-turn repetition detection is not
accidentally reduced to one turn.
On the third (and later) occurrence, the captured output is
replaced with `[tool output identical to msg #N (seen M times)]`
before the archive write. Counters at
`/admin/status.repetition`. The `slimference filter` subprocess
case is intentionally skipped because no `session_id` is available
there (would need extra hook plumbing).

### Codex evidence corpus and regression gate (T75)

`tests/fixtures/codex/` is the checked-in Codex evidence corpus directory:
synthetic request fixtures used by the proxy compression tests, a
`session-smoke.jsonl` log used by the reporting path, and a single
`codex-metadata.json` (schema_version=1) declaring the corpus provenance
(scrubbing method, Codex version, hooks/layers, scenarios) and a
`regression_gate` baseline.

`go run ./scripts/benchmarks codex-smoke-gate <dir>` aggregates the corpus
and asserts the baseline (min request count, min savings ratio, per-layer
min saved tokens, provider/route counts). It is wired as the final step of
`go run ./scripts/ci` so the smoke fixture cannot regress without failing
local CI. The synthetic numbers in this corpus are a regression backstop,
not a Codex savings claim; real claims still need a 10-20 session live
corpus that is intentionally not captured until the operator allows it.

### Anthropic version negotiation (T62)

`[proxy] anthropic_versions = ["2023-06-01", ...]` whitelists trusted
header values. Unknown versions downgrade via
`anthropic_unknown_behavior`:

- `conservative` (default): skip Layer 1 body mutation, still use L2 response
  cache.
- `passthrough`: no compression at all.
- `full`: trust the unknown version (opt-in risk).

Rate-limited warn fires ≤ 1× per minute on unknown versions. Count is
exposed at `/admin/status.anthropic_version.unknown_seen_total`.

---

## 9. Install and integration

`docs/install.md` is the install/uninstall SSOT. The current Phase H path is
Codex-first and scoped:

- hook callouts in `~/.codex/hooks.json`
- per-process Codex CLI traffic through
  `slimference codex run -- <prompt>`; explicit
  `--transport=wss` enables scoped Responses WebSockets through the raw
  local WSS frontdoor before live default promotion
- advanced shared Codex CLI/App traffic through
  `slimference codex enable` / `slimference codex disable`, which writes
  only a marker-owned `slimference-codex` provider block in
  `~/.codex/config.toml`; explicit `codex enable --transport=wss` writes
  `supports_websockets=true`

The normal user-facing commands are `slimference install`,
`status --preflight`, `codex run`, `codex status`, `codex certify wss`,
`uninstall`, and `status`. `codex enable` / `codex disable` are advanced
shared-route controls, not required for the default scoped workflow. Global
transparent lab commands are `cert-trust`,
`root-arm --global-chatgpt-hosts`, transparent `enable`, transparent `disable`, and
`root-disarm`.
Default install is Codex-only. Claude Code remains in tree, but is parked:
`--with-claude` is a compatibility no-op, the app policy forces
`claude_code=false`, `/admin/apps` rejects enabling it, and the SNI router
always passes `api.anthropic.com` through.

`internal/integrate` and `slimference integrate` are legacy/advanced
diagnostics for config-patch flows. `slimference codex run` is the current
scoped one-shot CLI path; `slimference codex enable` is the advanced shared
CLI/App route. The older proxy lifecycle and
transparent-proxied helpers remain advanced diagnostics. No default install,
TUI setup action, or primary certification path should depend on persistent
`OPENAI_API_BASE`, persistent `HTTPS_PROXY`, macOS System Network Proxy
settings, or persistent legacy `openai_base_url`.

The TUI exposes the same scoped lifecycle: Setup shows install/repair state for
the product path, daemon controls, and app routing with `[a]`, with Claude Code
parked. Savings counters come from `/admin/state`. Advanced shared-route and
global transparent controls are CLI-only; Setup does not advertise or execute
them.

### Legacy `integrate install`

`slimference integrate` is legacy/advanced config-patch mode and is not the
Phase H path. While Claude is parked, the command normalizes `all` to Codex,
rejects `--client=claude` without writing files, and never writes
`ANTHROPIC_BASE_URL`.

| Client / Surface | Wire point                                 | File            |
|------------------|--------------------------------------------|-----------------|
| Codex            | `openai_base_url` + `chatgpt_base_url`     | config.toml     |
| Hooks            | Optional Codex lifecycle + tool hooks      | hooks.json etc. |

Every edit uses fenced marker comments:

```
# >>> slimference integration >>>
openai_base_url = "http://127.0.0.1:8990/backend-api/codex"
chatgpt_base_url = "http://127.0.0.1:8990/backend-api/"
# <<< slimference integration <<<
```

First touch of an existing file leaves a timestamped backup
`.slim-backup-<ts>`.

### Optional Codex hook layer

`slimference hook install codex` is separate from transparent proxy setup and
legacy config-patch integration. It writes `~/.codex/hooks.json`, executable
scripts under `~/.slimference/hooks/`, and only the official
`[features] hooks = true` flag in `~/.codex/config.toml`; it does not
write `openai_base_url` or `chatgpt_base_url`.

The installed events are `SessionStart`, `PreToolUse`, `PermissionRequest`,
`PostToolUse`, `PreCompact`, `PostCompact`, `UserPromptSubmit`, and `Stop`.
The legacy flat `hooks.json` migration also recognizes current Codex lifecycle
events such as `SubagentStart` and `SubagentStop` so unrelated user hooks are
not lost during normalization. `PostToolUse` is Bash-only by default; write
tools (`apply_patch`, `Edit`, `Write`) and MCP calls are not post-processed
because their current ROI is negative without per-tool output contracts.
`SessionStart` records hook state without injecting context unless
`SLIMFERENCE_CODEX_HOOK_MODE=debug` is set, and `PreToolUse` does not block/retry
Bash commands unless `SLIMFERENCE_CODEX_HOOK_MODE=aggressive` is set
deliberately.
`PostToolUse` records turn state and archives raw Bash output. The default
`SLIMFERENCE_CODEX_HOOK_MODE=auto` emits visible `continue:false` replacement
only for Bash outputs with at least 600 original tokens, at least 400 saved
tokens, and at least 45% savings. `compact` / `aggressive` force visible
replacement for any changed output; `silent` keeps archive-only behaviour. It fail-opens on unknown
payload shapes so Codex never sees hook crashes for unsupported tool results:
missing or non-string tool-output fields are skipped and recorded as telemetry.
Tiny outputs below `hooks.codex_posttool_min_tokens` are skipped before the
heavy compaction/archive path. Both the generated shell hook and the Go
entrypoint enforce a fail-open watchdog via
`hooks.codex_posttool_timeout_seconds` / `SLIMFERENCE_CODEX_POSTTOOL_TIMEOUT_SECONDS`;
timeout telemetry is recorded as `timeout_fail_open`.
The primary no-chat-noise saving path is the Codex proxy route, not visible hook
feedback. The hook path also owns the safe T126
cross-tool mini path: raw `git status` path lists are recorded in the
file-backed current turn, and a later same-session/same-turn/same-CWD
`git diff --name-only` with the same exact path fingerprint is replaced by an
explicit marker. Diff hunks, `--name-status`, `git ls-files`, non-git output,
and standalone `slimference filter` runs are not touched. For large
AST-compacted Go file reads, the archive context also prints
`slimference expand-body <archive-id> <symbol>` so an omitted function or method
body can be recovered from the archived original. Unsupported/fail-open fields
such as `PreToolUse.updatedInput` remain disabled until a live Codex build
proves they are honored.

### Parked Claude Code hook code

Claude Code hook code is retained for audit/history, but no public product
entrypoint activates it. `slimference hook install claude`,
`slimference hook remove claude`, `slimference integrate --client=claude`,
`slimference readhook claude`, and top-level `slimference claudeposttool`
are parked/rejected. The TUI row is parked, the apps manager refuses
`claude_code=true`, and `api.anthropic.com` remains outside the hosts patch.
Claude Code optimization is intentionally out of scope for now.

Persistent hook/read-cache/repetition/tool-archive storage now uses the shared
`internal/sessions.SafeSessionID` convention for non-empty session ids and
`internal/sessions.SafeTurnID` for turn metadata. Blank sessions remain
no-op/anonymous depending on the store, and blank turn ids degrade to the prior
session-only behaviour so missing provider metadata never creates unbounded
cross-session state.

### TOML scope safety

The Codex fence is inserted **before the first `[table]` header** in
`config.toml`, not at EOF. TOML scoping rules make any key=value after
a `[header]` belong to that table — an EOF-append would silently nest
our keys inside the last `[projects.*]` section and Codex would never
see them at root. `insertBeforeFirstTable` guarantees top-level scope.

### Duplicate-key safety

TOML forbids a key appearing twice at the same table level. If the user
has a manual `openai_base_url` or `chatgpt_base_url` at unambiguous
top-level scope, `stripConflictingTopLevelKeys` removes it before
writing the fence. Keys nested inside tables are preserved (they are
dead from Codex's POV anyway; not our call to touch).

### Shell-rc flavour detection

`DetectRCFile` picks by `$SHELL` match + existence:

```
$SHELL=/bin/zsh   → ~/.zshrc
$SHELL=/bin/bash  → ~/.bashrc, or .bash_profile if present
$SHELL=.../fish   → ~/.config/fish/config.fish
otherwise         → ~/.zshrc (macOS default)
```

Fish uses `set -gx VAR value`; zsh / bash use `export VAR=value`.

### Verbs

```
slimference                         # TUI
slimference install                 # Codex-only install plan
slimference status --preflight      # scoped Codex readiness checks
slimference codex run -- <prompt>   # one-shot CLI with fail-open direct
slimference codex run --transport=wss -- <prompt>
slimference codex enable            # advanced shared Codex CLI/App route
slimference codex enable --transport=wss
slimference codex disable           # remove advanced shared route
slimference cert-trust              # global lab: open Keychain Access
slimference root-arm --global-chatgpt-hosts
slimference enable                  # global lab: enable SNI-peek daemon mode
slimference disable                 # global lab: disable SNI-peek daemon mode
slimference root-disarm             # global lab: remove hosts + pfctl
slimference status [--json]         # current setup state
slimference uninstall               # reverse install plan
slimference integrate status        # legacy/config-patch detection
```

### Doctor integration

`slimference doctor` appends an "Integration / Fallbacks" block with
the same detection output so a single command covers config, upstream
reachability, CLI drift, and the auto-integration state.

---

## 10. Bypass and Fallback

### Master bypass (T67)

`Proxy.bypassMode` (atomic.Bool) short-circuits every
`isLayerEnabled` and `isProviderEnabled` check. When on, the proxy
still accepts every connection and forwards bytes unchanged — a pure
transparent relay. Useful when a request feels off and you want to
rule Slimference out instantly.

Controls:

- CLI/admin bypass controls; the TUI header still shows `⚠ BYPASS` when bypass
  was enabled outside the TUI.
- `slimference bypass on|off|status` CLI (talks to the admin endpoint).
- Admin POST `/_slimference/admin/bypass {"enabled": true}`.

State is read via `AdminStatus.Bypass`.

### Daemon-down safety

`launchd` KeepAlive (T68) reshapes:

```
<key>KeepAlive</key>
<dict>
  <key>SuccessfulExit</key>    <false/>
  <key>Crashed</key>           <true/>
</dict>
<key>ThrottleInterval</key>    <integer>2</integer>
```

- Clean `service stop` → stays stopped.
- Process crash → restart in ≤ 2 s → SDK retry papers over the gap.
- Crash loop → throttled to 2 s minimum to avoid CPU burn.

### Post-install health probe

`slimference service install` polls `/admin/health` for up to 10 s and
reports `ok` or `degraded + troubleshooting hint`. Probe is
injectable in tests (`healthProbeFn`).

### Shutdown-timeout guard (T60)

`Proxy.Shutdown(ctx)` returns `ErrShutdownTimeout` when a worker
ignores context cancellation. A goroutine pprof dump is written to
`~/.slimference/shutdown-hang-<ts>.pprof`. Headless mode maps the
error to exit code 6 (T44). Nil `ctx` is tolerated
(`context.Background` substituted).

### Failure-mode matrix

| Scenario                    | Client impact                        | Recovery                          |
|-----------------------------|--------------------------------------|-----------------------------------|
| Daemon crashed              | 1× ECONNREFUSED, SDK retries         | none                              |
| Restart loop                | some requests fail                   | `slimference disable`, then restart or inspect daemon logs |
| Binary moved / deleted      | persistent ECONNREFUSED              | reinstall from source/release, then uninstall if needed |
| Want compression off        | —                                    | `bypass on` CLI                   |
| Legacy config-patch panic button | —                              | `integrate emergency-off`         |

---

## 11. Security

### Secrets detector

`internal/security` scans every request body + response body for 12
built-in patterns (AWS access key, GitHub token, Anthropic key, OpenAI
key, JWT, etc.) plus user patterns. Modes:

- `off`: scanning disabled.
- `warn`: matches logged, content unchanged.
- `redact`: matches replaced with `[REDACTED:<pattern>]`.
- `block`: matches cause 400 BadRequest from the proxy.

Config: `[security] mode`, `[security.allowlist] patterns`.

### Per-session suspend (T59)

`Detector.SuspendUntil(t)` + 1 h hard-cap disables scanning for a
bounded window without re-deploying. Surfaced via admin:

```
POST /_slimference/admin/security/suspend
Body: {"suspend_seconds": 600}
→ {"active": true, "until_unix_sec": ..., "mode": "redact"}
```

`GET` reports current state. Negative / zero seconds clear the
suspension (`time.Time{}`). Scanning is fully lazy-expired: the next
call after the deadline sees the cleared state.

### File permissions

- Config file written with `0o644`.
- launchd env file (`.env`) written with `0o600`; current product builds keep it
  as a restrictive service-environment stub and do not export retired
  side-channel summarization keys.
- SQLite files (`filter.db`) created with standard perms.

### What Slimference does NOT do

- No transparent interception unless the operator installs the local CA and
  arms the macOS System-HTTPS-Proxy.
- No modification of provider certificates. Transparent mode signs local leaf
  certificates from Slimference's local root CA, then validates upstream
  provider certificates on the outbound connection.
- No SOCKS/WebRTC interception; microphone/audio UDP paths are expected to
  bypass transparent mode.
- No traffic inspection for non-allowlisted hosts; those CONNECT requests are
  raw-relayed.

---

## 12. Observability

### Admin API

Base path `/_slimference/admin`:

| Endpoint              | Method   | Purpose                                       |
|-----------------------|----------|-----------------------------------------------|
| `/status`             | GET      | Full state snapshot.                          |
| `/provider`           | POST     | `{"provider": "anthropic", "enabled": false}` |
| `/layer`              | POST     | `{"layer": 2, "enabled": true}`               |
| `/flush`              | POST     | Flush caches.                                 |
| `/bypass`             | GET/POST | Master bypass (T67).                          |
| `/security/suspend`   | GET/POST | Per-session secrets-off (T59).                |
| `/health`             | GET      | Liveness probe (plain 200 OK).                |

### `/admin/status` fields

- `status`, `service`, `version`, `listen_port`.
- `layers`, `providers`, `queue_depth`, `cache_entries`.
- `analytics`: snapshot (tokens, ratios, requests).
- `analytics_queue`: capacity, depth, enqueued_total, dropped_total (T42).
- `recent_requests`: last 20 `RequestMetrics`.
- `layer0`: per-filter runtime observability: attempts, matches, misses,
  panics, bytes in/out, bytes saved, hit rate, and average runtime.
- `read_cache`, `checkpoints`, `tool_archive`: subsystem stats.
- `provider_health`: per-provider health.
- `prompt_cache.breakpoints_injected_total` (T45).
- `pipeline`: array of `PhaseSnapshot {name, count, p50_ms, p95_ms,
  avg_ms, max_ms, sample_size}` for `l1`, `l2`, `upstream`,
  `total` (T58).
- `anthropic_version`: whitelist + unknown-behavior + count (T62).
- `bypass`: current master-bypass state (T67).

### Structured logging

`internal/slogutil` ships `JSON` logger with rotation (10 MB × 5).
`SLIMFERENCE_LOG_LEVEL=debug|info|warn|error`. Format-switchable via
`--log-format text|json` in headless mode.

### Decision chain (debug)

`internal/debug/decisions.go` records per-request layer breakdowns
into a ring buffer (default 100) + optional JSONL log. Inspect via:

```
slimference debug last            # newest entry (--json for machine)
slimference debug tail 30         # 30 newest rows
slimference debug summary week    # aggregate SubLayerBreakdown
slimference debug replay file.jsonl
slimference debug paths           # where everything lives
slimference debug bundle          # bounded content-free diagnostics export
```

`slimference debug bundle` is the field-session handoff command. After running
real Slimference sessions, it writes a timestamped directory under
`~/.slimference/exports/` with manifest, path snapshot, admin state, today's
savings summary, decision/flight tails, filter-run token summaries, and daemon
log tails. It intentionally excludes raw prompts, raw tool outputs, raw WSS
frames, auth material, and capture archives; filter command/project strings are
hashed. Use `--out DIR`, `--flight-limit N`, `--filter-limit N`, and
`--log-lines N` to keep a bundle scoped for later analysis.

### Pipeline histograms (T58)

15 ns / observation on an M1. 200-sample rolling ring per phase;
percentiles on demand.

---

## 13. TUI

`internal/tui` is a BubbleTea UI with a seven-item home menu as the default
view: Launch Codex CLI, Launch Codex App, Activity, Savings, Status, Logs, and
Setup. There are no top tabs/reiter on the product surface. `↑/↓` selects,
Enter opens, and subviews return with `b`/`esc`; Activity, Savings, Status,
Logs, and Setup also return with Enter where no primary action is selected.
Apps and advanced daemon repair stay behind Setup instead of being promoted as
daily-use navigation.
The home view is strictly menu-only. Setup warnings, install/repair state,
diagnostics commands, current-session savings, traffic logs, provider maps,
checkpoint/tool-archive internals, cache parser details, and transport proof
vocabulary belong to Savings, Status, Logs, or Setup, not the first screen.
Daemon PID/port/liveness is not rendered in the global header; it belongs to
Status. Status is a daily operator check with four card families only: Daemon,
Install, Using Now, and Health. It does not show Normal Codex, advanced route,
provider-chip, lab, or transport vocabulary during normal scoped operation.
Activity is the live route-confidence view. Its first card shows scoped live
instances: active `slimference codex run` CLI process count, active
process-local Desktop app-server count, and any running Codex.app process that
is direct/unknown rather than counted as Slimference. Recent routed Slimference
requests remain in a separate card from daemon flight telemetry. When a routed
Codex WSS session maps to Codex's local thread store (`~/.codex/state_5.sqlite`),
Activity shows the real surface (Codex CLI or Codex App), thread title, cwd,
model, route state, and savings.
Raw provider IDs, internal route modes, backend paths, direct Codex windows, and
old hook-turn diagnostics are intentionally hidden there. Logs owns diagnostics
export, a compact route summary, and recent daemon events.

Launch Codex CLI opens the proven scoped wrapper path with
`transport=auto`; the TUI detects Ghostty vs Apple Terminal and opens the new
Slimference Codex CLI tab in the same terminal app, rooted at the TUI's current
working directory. The launched CLI drops stale Codex runtime/session env while
preserving config-bearing env such as `CODEX_HOME`, clears the visible raw shell
command, prints a short `[SF] Codex CLI started with Slimference` preamble, and
keeps the tab/window title prefixed with `[SF] ` while the proxied process is
active.
Launch Codex App launches the process-local
`--transport=app-server` Desktop path, whose hidden shim rewrites the
`thread/start` `modelProvider` so the Desktop conversation rides the same
`websocket_phasef` savings route as the CLI (verified 2026-05-22 via the daemon
decisions log; the Desktop app-server holds loopback sockets to `:8990` with no
direct `chatgpt.com` socket). Capability gating from `codex desktop status` still
exists, but note the gate currently reads the sampled WSS delta counters, which
lag and under-report; the reliable green signal is the decisions-log
`route_mode=websocket_phasef`. Desktop has no current external Slimference
indicator; use Activity, Status, and the shim flight log to inspect scoped
Desktop traffic. Desktop launch is intentionally cwd-agnostic: the TUI's current
directory only matters for Launch Codex CLI. Codex Desktop can switch between
projects and run multiple threads inside the app; Slimference attributes routed
traffic by Codex thread/session id and enriches it from Codex's local thread
store instead of assuming the TUI cwd. The historical in-composer `Slimference`
provider chip is kept only for older Codex Desktop builds that still render the
process-local provider config; current Desktop builds may not show it.
Historical proxy/CA failures remain diagnostic proof state. Normal
Finder/Spotlight Codex.app launches remain direct.
Setup owns one product-level install/repair surface for Codex CLI and Desktop
together. It renders a stable four-row checklist: Slimference install, Codex
hook, CLI savings route, and Autostart daemon. Per-app rows are route
policy/capability state, not separate install states, and are opened from Setup
with `a`. Advanced shared-route, global transparent routing, and asset
uninstall controls are CLI-only and stay out of the Setup surface. Old macOS
`U`/`UE` or `dyld_start` Slimference processes are shown as reboot-only stale
processes when detected; the current healthy daemon PID remains the actionable
state.

The home menu does not label readiness inline. WSS, transport, route, recert
attempt id, started/finished/last-success/retry times, last error, bounded
recert log path, daemon state, logs, and diagnostics bundle handoff are Status
or Setup details. Measured savings live under Savings and come from
`/admin/state.savings.product` plus the per-session accounting surface. Raw
parser matrices, policy internals, provider maps, traffic rates, mechanism
debug counters, and archive/checkpoint details stay out of the home view. The
TUI caches product status in the model and refreshes it on ticks/events instead
of fetching during render; host-budget attention slows the next tick from
500 ms to 2 s. Product-signal selection is handled by the pure
`PresentProductStatus` presenter before Bubble Tea styling, so savings/safety
projection is unit-testable without starting the TUI and debug-only WSS
internals cannot drift into user-facing detail views unnoticed.

### Keybindings

Auto-generated in `docs/tui-keybindings.md` from
`internal/tui/keys.go` (T64). Drift-check test fails if they diverge.

| Category    | Keys        | Action                         |
|-------------|-------------|--------------------------------|
| Navigation  | `↑/↓/j/k`   | move up / down                 |
| Navigation  | `enter`     | open selected home item; back from Savings/Status |
| Navigation  | `b` / `esc` | back to home                   |
| Views       | `s`         | savings view                   |
| Views       | `d`         | status view                    |
| Views       | `i`         | setup view                     |
| Setup       | `1`-`4`     | jump to setup step             |
| Setup       | `a`         | app routing view; Codex CLI/Desktop toggles; Claude row parked until explicit Claude hosts opt-in |
| Setup       | `p` / `o`   | start/stop daemon; restart/repair daemon |
| Actions     | `f`         | flush caches                   |
| Actions     | `y`         | export diagnostics             |
| Actions     | `ctrl+s`    | save preferences               |
| Actions     | `q`/`ctrl+c`| quit                           |

### Bypass badge

When bypass is on through CLI/admin controls, the header renders `⚠ BYPASS` so
it is visible from every view. The TUI `b` key is reserved for Back.

### Remote mode

`newRemoteProxyAdapter` (in `cmd/slimference/remote_proxy.go`) talks
to a running daemon via the admin API rather than driving a local
`Proxy` instance. Used when you run `slimference` against a daemon
started by `service install`.

The Savings view is product accounting, not parser telemetry. It renders total
input saved, estimated original vs sent tokens, tracked output tokens,
per-session recent/active savings rows, cache contribution, deterministic
evidence decision aggregates, and safety state.
Codex WSS and HTTP session rows are enriched from the same Codex thread store
as Activity, so Desktop and CLI threads show user-facing title/path/client
labels when Codex has persisted them. Raw flight IDs stay transport-precise
(`codex-wss:<thread>` or `codex-http:<thread>`), then normalize to the same
thread ID for metadata lookup. Raw `codex_chatgpt` provider names are never used
as Desktop/CLI proof by themselves.
Codex passthrough flights, including Desktop sideband endpoints and empty
responses payloads, are also recorded after successful upstream handoff as
content-free zero-savings rows with the same thread/client attribution and a
precise bypass reason. Generic OpenAI passthrough is not recorded there to avoid
turning unrelated API traffic into noise.
Parser matrices, checkpoints, archive internals, quality canaries, and raw
debug counters remain available through CLI/admin diagnostics, not the daily
TUI.

For measured conversation accounting, use `slimference savings <period>`. `period`
is `live`, `today`, `week`, `month`, or `all`. `live` is the current-daemon
proof window: it starts at the running daemon's `started_at` timestamp and uses
decision-log rows only, so stale anonymous rows from older binaries cannot make
the current attribution/cache health look worse than it is. If no daemon is
running, `live` falls back to the last 30 minutes. When
the decision log is configured, the report prints aggregate `Decision layer net`
and top Codex sessions with compact `layers=` fields. Per-session rows include
`display_name`, `project_path`, and `client_family` when Codex thread metadata
can be resolved from WSS, HTTP Codex thread metadata, strong Codex session
headers, or an unambiguous local Codex thread DB match. Strong thread identities
are read from top-level, `metadata`, `client_metadata`, nested
`x-codex-turn-metadata`, `x-codex-thread-id`, `x-codex-conversation-id`, or
`x-codex-session-id`, but only thread/conversation/session keys count; `user_id`
is deliberately ignored because it can merge parallel sessions from the same
account. HTTP rows use `codex-http:<thread>`, WSS rows use
`codex-wss:<thread>`, and local report-only resolutions use
`codex-local:<thread>`. WSS keeps its historical `prompt_cache_key` fallback
only when stronger metadata is absent. Codex HTTP `client_family` is captured
from the same metadata sources or User-Agent fallback; `codex/...` User-Agents
classify as Codex CLI. If Codex HTTP does not carry any strong thread metadata,
the anonymous fallback hashes Responses API `input` user text instead of
collapsing those rows into the empty bucket. During Savings reporting,
`fh:*` fallback rows may be resolved to `codex-local:<thread>` only when the
local `~/.codex/state_5.sqlite` thread table gives exactly one match by
first-user-message hash or by time/model/client-family. Anonymous fallback rows
such as `no-session:proxy` resolve only when exactly one local Codex thread
activity envelope (`created_at` through `updated_at`) encloses the token-bearing
request window; zero-token tunnel and ping rows do not widen that window.
Ambiguous parallel candidates remain anonymous and keep attribution status at
`attention`. Rows
also include
`layer0_net_tokens`, `layer1_net_tokens`,
`layer2_net_tokens`, `layer3_net_tokens`, `output_reduce_tokens`, and
`tool_prune_tokens`. These fields are measured-only: mechanism accounting is used
when present, request-stage token counters are used only as fallback, and missing
counters stay zero.
Codex attribution health is reported as
`decision_codex_requests`, `decision_codex_attributed_requests`,
`decision_codex_unattributed_requests`,
`decision_codex_unattributed_reasons`, and
`decision_codex_attribution_rate`. A Codex request counts as attributed only when
the row carries a strong `codex-http:<thread>`/`codex-wss:<thread>` session ID or
a report-time `codex-local:<thread>` resolution that passed the ambiguity guard.
Anonymous historical fallback buckets remain visible instead of being silently
merged into real sessions. The reason map separates lookup errors, missing local
thread candidates, ambiguous thread candidates, and missing thread identity
cases so unresolved history is inspectable without guessing. Content-free Codex
sideband endpoints such as
`/backend-api/codex/models` remain in total decision accounting but do not count
as unattributed conversation sessions. `decision_codex_attribution_status` is
`ok` when all conversation-bearing Codex rows are attributed and
`attention` when any such row remains anonymous.
Provider-cache accounting is deliberately separate from local input deletion:
`decision_cache_read_tokens`, `decision_cache_create_tokens`,
`decision_cache_net_tokens`, `decision_cache_hit_requests`,
`decision_cache_hit_rate`, and `decision_cache_negative_net_requests` show
whether cache steering helped or harmed. A cache-create-only request therefore
shows negative cache net instead of being hidden behind gross token savings.
Estimated saved cost uses cache-read discount equivalent minus cache-create
tokens, clamped at zero, so provider-cache warmup cannot overstate savings.
`decision_cache_status` is `ok` for positive cache reuse, `warming` for
create-only activity, `attention` for negative net cache impact, and `none` when
the decision log has no cache activity.

---

## 14. Configuration Reference

Order of precedence (highest wins):

1. CLI flag (`--config <path>`) — T46.
2. `SLIMFERENCE_CONFIG` env var.
3. `$XDG_CONFIG_HOME/slimference/config.toml`.
4. `~/.slimference/config.toml` (legacy — supported with deprecation
   warn).
5. Built-in defaults.

`slimference doctor` reports the resolved path + source so operators
can tell at a glance which file was read. `ResolveConfigPath(opts)`
surfaces the same info programmatically via `LoadInfo`.

### Top-level blocks

```toml
[proxy]
listen_address = "127.0.0.1"
listen_port    = 8990
ipv6           = false
anthropic_versions         = ["2023-06-01"]   # T62 whitelist
anthropic_unknown_behavior = "conservative"   # conservative|passthrough|full

[upstream.anthropic]     base_url = "https://api.anthropic.com"
[upstream.openai]        base_url = "https://api.openai.com"
[upstream.codex_chatgpt] base_url = "https://chatgpt.com"           # T66

[compression]
layer1_enabled                       = true
layer2_enabled                       = true
sliding_window                       = 5
min_messages_for_compression         = 8
structure_min_tokens                 = 500
dedup_similarity_threshold           = 0.85               # scalar fallback

  [compression.tuning]
  overflow_sliding_window = 2
  overflow_target_ratio = 0.10
  loop_detection    = false                  # T37
  structure_preview = true                   # T76 archive-backed default
  structure_in_window = false
  structure_in_window_min_tokens = 1500
  incremental_staircase = [ ... ]            # T27
  dedup_staircase = [                        # T53
    { msg_count_le = 10,       threshold = 0.88 },
    { msg_count_le = 20,       threshold = 0.85 },
    { msg_count_le = 40,       threshold = 0.82 },
    { msg_count_le = 1000000,  threshold = 0.78 },
  ]

    [compression.tuning.tool_compressor]     # T61
    aggressive_after_multiplier = 2
    git_moderate_diff_limit     = 60
    test_max_failure_lines      = 40


[cache]
response_cache_max_entries = 100
response_cache_ttl_seconds = 300

[security]
mode = "redact"              # off | warn | redact | block
  [security.allowlist]
  patterns = []

[analytics]
log_dir = "~/.slimference/analytics"

[filter]
passthrough_max_chars = 2000
filter_db             = ""
tee_dir               = ""

[hooks]
slimference_command = "slimference"
exclude_commands    = []
codex_posttool_timeout_seconds = 4
codex_posttool_min_tokens      = 800

[debug]
level           = "info"
format          = "jsonl"
max_entries     = 100
decisions_log   = ""
```

### Environment variable overrides

`config.go::applyEnvOverrides` handles:

```
SLIMFERENCE_LISTEN_ADDRESS, SLIMFERENCE_LISTEN_PORT,
SLIMFERENCE_UPSTREAM_{ANTHROPIC,OPENAI,CODEX_CHATGPT}_BASE_URL,
SLIMFERENCE_COMPRESSION_SLIDING_WINDOW,
SLIMFERENCE_SECRETS_MODE, SLIMFERENCE_LOGGING_LEVEL,
SLIMFERENCE_HOOK_SLIMFERENCE_COMMAND,
SLIMFERENCE_CODEX_POSTTOOL_TIMEOUT_SECONDS,
SLIMFERENCE_CODEX_POSTTOOL_MIN_TOKENS,
SLIMFERENCE_DEBUG_{DECISIONS_LOG,LEVEL,FORMAT,MAX_ENTRIES},
SLIMFERENCE_FILTER_{PASSTHROUGH_MAX_CHARS,DB,TEE_DIR}
```

Plus the runtime toggles: `SLIMFERENCE_HEADLESS=1`,
`SLIMFERENCE_CONFIG=<path>`.

---

## 15. CLI Reference

```
slimference                         Start TUI (requires TTY)
slimference --no-tui                Headless foreground proxy
slimference <subcommand> [flags]
slimference help [subcommand]
```

### Subcommands

| Verb          | Purpose                                                                |
|---------------|------------------------------------------------------------------------|
| `integrate`   | Legacy/advanced config-patch mode; Codex-only while Claude is parked.  |
| `bypass`      | on, off, status — master bypass via admin API.                         |
| `service`     | install, uninstall, start, stop, restart, status, logs; start/restart wait for daemon status (launchd). |
| `daemon`      | Run as long-lived daemon (invoked by launchd; users prefer `--no-tui`).|
| `proxy`       | Transparent CA/daemon/System-HTTPS-Proxy lifecycle plus Codex env helpers. |
| `codex`       | Scoped Codex run, enable/disable/status, WSS certify, Desktop status/launch. |
| `doctor`      | Full diagnostic sweep + integration checks.                            |
| `filter`      | Layer-0 filter wrapper: `slimference filter -- <cmd>`.                 |
| `rewrite`     | Rewrite captured output (used by PreToolUse hook).                     |
| `posttool`    | Codex PostToolUse entry point (stdin JSON).                            |
| `codexhook`   | Codex lifecycle hook entry point for session, permission, prompt, stop. |
| `readhook`    | Codex Read-hook entry point: `slimference readhook codex`.             |
| `expand`      | Retrieve archived tool result by id (T40).                             |
| `expand-body` | Retrieve one Go function/method body from an archived AST read (T125). |
| `checkpoint`  | Smart-compaction checkpoint tools: list, show, restore (T39).          |
| `hook`        | install, remove, verify, status, check-upstream (manual hook mgmt).    |
| `gain`        | Report Layer-0, by-command/by-parser, prompt-cache, output, or proxy-flight telemetry.|
| `stats`       | Analytics snapshots (today/week/month/prompt-cache).                   |
| `savings`     | Unified savings view per period (`live`/today/week/month/all) with decision-log layer/conversation breakdown; --json / --csv (T80/T293/T343).|
| `compress-preview` | Dry-run the L1 pipeline against a body; --diff / --json (T82).    |
| `watch`       | Live ticker against /admin/status; Ctrl-C to stop (T79).               |
| `filter --stream` | Streaming-aware Layer-0 wrapper for `tail -f` style inputs (T94).  |
| `debug`       | paths, last, summary, tail, replay, flight last/tail/replay/export.    |
| `config`      | init, show.                                                            |
| `test`        | anthropic, openai, intercept.                                          |
| `completion`  | Emit bash completion.                                                  |
| `trust`       | Trust-model tools for project-local filters.                           |
| `version`     | Print version.                                                         |

### Flight recorder

`slimference debug flight` reads the same normalized flight records that the
proxy and TUI use. A flight record is generated from each persisted
`RequestSummary` and records route/source, host/path/provider, layer list,
client family, estimated input before/after, provider-reported input/cache/output usage,
output-reduce metadata, `previous_response_id` state, errors, privacy state,
content-free debug facts, and proxy overhead. WSS debug facts include bounded
shape counters such as previous-response presence, tool-result counts,
source-tool-result counts, Layer-0 tokens saved, and bypass reason; they do not
store raw prompt, tool output, code, or auth material. `last`, `tail`, and
`replay` support `--json`; `export` writes JSONL by default and CSV with `--csv`
or an `.csv` target path.

The recorder is privacy-first: before a request summary is retained or flushed
to `[debug].decisions_log`, bearer auth, API-key/token/password/cookie
assignments, `sk-*` keys, user-home paths, and temp paths are redacted. Raw
request/response bodies are not captured by the flight recorder.

The TUI Logs view renders a compact `ROUTES` block sourced from the same
records: routed request count, saved/cache/output token totals, fallback count,
safety-block count, slowest request, and recent client/session rows. It does
not show raw route modes, backend paths, layer plans, or hook-turn internals on
the daily surface. Use `slimference debug flight --json` when the raw
diagnostic record is needed.

### WebSocket inspection

Transparent WebSocket transport is fail-open. `internal/wscompact` preserves
raw frames for unknown, binary, malformed, unsupported-extension, and
schema-drift cases. For negotiated `permessage-deflate` with a supported
profile, `internal/proxy/wsmitm` inflates complete text messages, runs the
Phase F adapter on the JSON envelope, and re-encodes with RSV1 only when a
mutation actually happens. Unmodified compressed messages are forwarded
byte-equal after their plaintext has advanced the destination-side rolling
dictionary, so later context-takeover mutations remain decodable. Reassembled
compressed payloads and inflated plaintext payloads are size-bounded; hitting
either cap fails open to byte-equal forwarding and disables compressed mutation
for that direction without parser degradation. For known
Codex WSS conversation envelopes, `PhaseFDispatcher` attaches a Phase F
adapter:

- client-to-server request payloads run stale-read aging, obsolete-read prune,
  stop-sequence guards, proxy Layer 0 captured-output compaction, and be-terse
  when existing config/cohort gates allow; stateful Codex WSS request bodies
  carrying tool output full-pass unless the lab/proof mutation switch is enabled
- server-to-client output item frames teach the adapter session-local tool-call
  metadata, so lab/proof client-to-server `function_call_output` mutation can
  preserve tool identity even when Codex splits the request state across WSS
  messages
- request-body summaries record repeated resolved read/tool keys as a re-read
  canary, so drift analysis can see context-recall pressure without logging raw
  tool output
- tool-output request bodies in stateful Codex WSS sessions full-pass before
  request mutation; edit/re-read observation still runs first
- server-to-client `error`, `response.failed`, and `response.incomplete` frames
  are forwarded byte-equal and recorded as content-free upstream-error
  summaries for diagnostics; after such an error the current adapter full-passes
  subsequent request bodies until reconnect
- server-to-client text deltas run repdet
- terminal response payloads stay byte-equal on WSS to avoid double-counting
  streaming repdet savings or corrupting final code/patch text
- WSS streamcut is intentionally disabled until T236 proves a terminal-safe
  Codex WSS early-cut sequence. HTTP/SSE streamcut is unchanged.

WSS frame-shape inspection is content-free. `wscompact.FrameSummary` records
route, direction, opcode, payload size, JSON top-level shape, top-level field
names, top-level field types, protocol message type, and a stable shape hash
derived from those schema facts. `wscompact.ShapeRegistry` stores counts plus
mutation eligibility and fallback behavior for each observed shape. Arbitrary
JSON and non-Codex routes are inspect-only and do not mark WSS mutation
shape-known for planner gating. Only registered Phase-F-compatible
request/response shapes on the Codex responses route can become
mutation-capable, and even those remain subject to route certification and live
corpus confidence. Unknown or inspect-only shapes stay byte-equal bridge
candidates.

`/_slimference/admin/state` `.wss` reports whether the engine is active,
whether frames are only forwarded byte-equal, whether parser degradation
occurred, how many compressed messages were inspected/mutated/bypassed,
Phase-F request/response event counts, and how many frames were re-encoded
after mutation.

Scoped Codex WSS has an additional pre-`net/http` frontdoor on the same
loopback listener (`:8990`). It intercepts only
`GET /backend-api/codex/responses` WebSocket upgrades whose offered
subprotocol list includes `responses_websockets`. The captured Upgrade
header is forwarded upstream with its original order, casing, subprotocol
text, and unknown headers intact; only Host and absolute request-targets are
normalised for the real upstream. Everything else is replayed into the normal
HTTP server unchanged. This restores the old transparent dispatcher's
raw-header property without reintroducing global `chatgpt.com` routing.

Codex Desktop app-server shim launch reuses the same loopback listener without
arming global transparent mode. Codex.app honors `CODEX_CLI_PATH` when it
starts its Rust `codex app-server`; the launcher points that variable at the
Slimference binary only for the spawned Codex.app process. The hidden
`slimference app-server` shim validates its scoped env, removes its own shim
variables, and runs the real Codex binary as `codex app-server` with a
process-local provider block
(`model_providers.slimference-codex.base_url=http://127.0.0.1:8990/backend-api/codex`,
`requires_openai_auth=true`, `wire_api=responses`). The launcher sets
`supports_websockets=true` only when the current local Phase-F proof is fresh;
when WSS proof is stale it sets `supports_websockets=false` so the spawned
Desktop app-server uses the HTTP Responses savings path instead of a
byte-equal WSS bridge.

Implemented in `cmd/slimference/codex_desktop_app_server_shim.go`. The shim is a
thin JSON-RPC mediator (not a bare exec): it spawns the real Codex app-server,
passes unrelated frames straight through, and inspects only scoped routing and
display-signal seams in the newline-delimited Desktop protocol. Client->server
`thread/start` requests
with a default provider (`null` or absent) are rewritten to
`slimference-codex`; explicit providers and realtime/voice threads via
`config["features.realtime_conversation"]` pass through byte-identically.
Server->client responses shaped as `result.config` are augmented with
`config.model_provider`/`config.modelProvider=slimference-codex` and matching
`model_providers`/`modelProviders` process-local provider entries so older
Codex Desktop builds can render the visible `Slimference` provider signal
across snake_case and camelCase Desktop config shapes. The current local Codex
Desktop bundle (`26.602.40724`) inspected on 2026-06-07
does not expose a stable process-local text-chip contract through app-server
response data. Slimference therefore treats `model/list` as read-only and never
mutates model IDs, display names, selected model values, default flags, or
service-tier metadata to fake a chip. Desktop has no external indicator in
current builds; Desktop savings and correctness are verified through TUI
Activity/Status, `codex desktop status`,
`~/.slimference/logs/desktop-shim.jsonl`, and daemon decision events.
The earlier floating overlay experiment was removed from the product path.
Non-JSON, non-config responses, error responses, malformed config shapes,
model-list responses, and unrelated notifications pass through byte-identically.
The shim writes a minimal flight log to `~/.slimference/logs/desktop-shim.jsonl`
with event names only, never payloads or secrets.

Discovery and proof (2026-05-22): a loopback tee proxy captured the real frames,
and the daemon decisions log (`SLIMFERENCE_DEBUG_DECISIONS_LOG`) recorded both the
CLI and the Desktop app-server (driven with the full Electron feature-flag
`config`) as `route_mode=websocket_phasef` on `/backend-api/codex/responses`. The
Desktop and CLI WSS frames are byte-identical `permessage-deflate`. So the
Desktop conversation rides the same Phase-F route as the certified CLI. Current
product default keeps risky stateful WSS tool-output request bodies byte-equal,
but proof-fresh WSS can compact state-safe status output after the tool call is
known. Broader token savings on those shapes remain lab/proof opt-in until live
certification proves the current Codex WSS contract accepts them without 400s.
Earlier "zero-byte /
`byte_bridge_only`" readings were sampled-counter artifacts plus trivial test
prompts with nothing to mutate (the same caveat as the CLI smoke). Normal Desktop
remains direct and no-drawback; Browser ChatGPT, ChatGPT.app, computer-use,
voice, and Claude Code are untouched.

Desktop savings proof (2026-05-29): after Codex CLI drifted to 0.135.0, the
official scoped recert path restored `auto=wss_phasef`. A real Codex.app
app-server-shim proof then launched PID 77770 through `slimference codex desktop
prove --manual`; the user prompted three separate `cat` reads of a 76540-byte
target and Codex returned `DESKTOP_T247_0135_DONE`. After quitting Codex.app to
flush the WSS session, `slimference codex desktop prove --finish --json` returned
`desktop_app_server_phasef_proven`, `desktop_savings=true`,
`frames_reencoded=3`, `compressed_messages_mutated=3`, `phasef_mutations=3`,
`phasef_bridged=4`, `compressed_messages_inspected=294`, and zero
parse/degrade/compression errors. This is the current Desktop savings gate:
route-ready still means launch-eligible only; `desktop_app_server_phasef_proven`
is the measured Desktop savings proof.

The older `--transport=proxy --with-ca-env` branch remains an advanced
diagnostic path for future Codex builds, but it is not the preferred Desktop
product route. Its 2026-05-22 proof reached CONNECT and removed Chromium's
direct-socket bypass, yet still produced zero application bytes because the
Desktop client did not accept the local CA/root-store path.
`slimference codex desktop status` reports daemon reachability, WSS counters,
whether a live Desktop app-server proof has been observed, and any legacy
proxy failure state without letting historical daemon-wide counters become a
Desktop savings claim.

WSS auto-promotion is local-proof gated. `slimference codex certify wss`
reads `/admin/state`, refuses to write when WSS parse failures, degraded
sessions, compression errors, byte-bridge-only state, or missing mutation are
present, and writes `~/.slimference/codex-wss-cert.json` only with
`frames_reencoded>0`, `compressed_messages_mutated>0`, and daemon reachability.
`--transport=auto` consumes that proof through `internal/codexroute` using the
savings-first safe ladder `wss_phasef -> http -> direct`. Version drift now
sets `needs_recert=true`; daemon startup, scoped auto transport, TUI
startup/status refresh, and TUI repair can start the shared recert path after
the daemon listener is reachable. If a clean byte-equal WSS bridge proof exists,
status reports it for diagnostics and explicit bridge runs, but auto keeps the
HTTP savings fallback until Phase-F recertifies.

`slimference codex recertify wss` is the shared repair core for CLI, background
auto-recert, and TUI Setup. It creates a temporary repo, runs real Codex CLI
turns through scoped WSS, evaluates only the `/_slimference/admin/state`
`.wss` delta window,
and writes either the Phase-F cert or the lower-risk
`~/.slimference/codex-wss-bridge.json` proof. It persists bounded repair state in
`~/.slimference/codex-wss-recert.json` and a 2 MiB rotating log at
`~/.slimference/logs/codex-wss-recert.log`. Bridge mode bypasses Phase-F
handlers and streamcut; it is compatibility mode, not a savings claim.
`/admin/state`, `slimference codex status`, and the TUI expose the latest recert
status, attempt id, start/finish/last-success/retry timestamps, last error, and
log path. Background auto-recert is still guarded by lock/backoff and never
keeps mutating after a version drift without fresh proof.

### Compression planner

`internal/planner` is the deterministic safety governor for cross-layer
coordination. It turns request facts (provider/model/route, input/output token
size, content classes, live-corpus confidence, manual disables, recent-edit
state, provider cache support, output-reduce/tool-prune cooldown, and WebSocket
shape confidence) into per-layer decisions for L0, L1, L2, Layer 3
output/tool controls, and WebSocket transport. The package is pure: same facts produce
the same `CompressionPlan`, every decision carries action, reason, expected
saving, risk, and confidence, and operator-disabled layers stay disabled.
The proxy derives recent-edit state from the current request plus file-backed
hook turn state, so read-only follow-up requests can still preserve recently
edited files. Live-corpus confidence defaults to `unknown`, can be asserted via
`[compression.tuning] planner_live_corpus_confidence`, or derived from
`planner_live_corpus_metadata_path` metadata. WebSocket shape confidence is fed
by the inspect-only `wscompact.ShapeRegistry`; it records observed JSON frame
shapes without changing bytes, and it exposes mutation confidence only for
registered Phase-F-compatible shapes rather than arbitrary JSON envelopes.
Layer 3 cooldown is sourced from the T141 output-reduce tracker and the T151
tool-prune session bucket; the planner marks it as a `cheap_only`
`quality_cooldown_soften_layer3` decision because the runtime softens Layer 3
rather than blindly continuing aggressive behavior. Output-reduce task-shape
selection now bypasses unproven detail-sensitive shapes instead of merely
capping them to `standard`: code edits, new-file generation, debugging, reviews,
tool-result reasoning, command-output relay, final summaries, read-only
analysis, deep explanations, and planning. Those shapes need complete evidence
or exact workflow content more than maximal terse output. The planner mirrors
the runtime output-reduce guard for its own summaries: exact replies,
command-output relay, repair follow-ups, unproven detail shapes, and low-ROI
direct-answer tasks bypass Layer 3 in the plan. Tool-schema
pruning runs only after strict schema extraction: if any `tools[]` entry cannot
be named for the provider shape, the request keeps the full schema instead of
partially pruning a mixed/unknown tool surface.

Product status separates provider-cache savings from local input/output-wire
savings. `/admin/state.savings.product` carries provider-cache read/create tokens
from analytics, billable Layer-0 input savings, output-wire savings, cache hit
counts, analytics proof-loss counters, and safety/host-budget state as distinct
fields so the TUI does not present a mixed headline number. Proof-critical
analytics loss is surfaced as product safety pressure rather than hidden as a
debug-only queue statistic. Host-budget demotion is wired into the Codex Layer-0
policy: if the latest product host-budget snapshot is exceeded, WSS/HTTP Codex
tool-output reducers full-pass until the process is back inside budget.
The reducer hot path reads this as an atomic state bit, avoiding fresh RSS/state
directory scans per frame. Oversized state trees that cannot be fully measured
within the bounded scan limit are treated as budget pressure. Phase-F request
metadata for session id, previous-response id, and model is derived from the raw
request map already parsed for message extraction, so normal WSS request handling
does not repeatedly unmarshal the same frame just to fill planner and recovery
fields.

The proxy hot path attaches this plan to `debug.RequestSummary` and normalized
`flight` records for upstream, local cache, transparent CONNECT, and direct
WebSocket routes. Planner summaries include content-free `content_classes`
labels such as `websocket`, `tool_output`, `source_file`, `json`, and
`repeated_tool_output`. Codex WSS Phase-F records both the upgrade-level route
record and per client request-body planner summaries, so decisions logs can show
real message counts, token deltas, previous-response state, output-reduce
reason, and cache candidates without logging frame payloads. For
HTTP compression requests, the same request-local plan now also controls the
first behavior gates: L0 proxy compaction skips planner `bypass`, and L1 skips
planner `bypass` and uses cheap-only mode for planner `cheap_only`. Layer-local
fallbacks remain active; the planner is an early governor, not the only safety
mechanism.

`slimference plan inspect` dry-runs the same planner without sending upstream
traffic. It accepts provider/model/route/token/cache/WebSocket facts, can
estimate input tokens from a request file or stdin, and prints either a compact
human table or JSON. This is the fixture-facing entry point for comparing
planned versus actual outcomes before any planner decision becomes behavior
controlling.

`scripts/benchmarks benchmark-corpus` replays recorded `plan` objects from
request summaries and compares them with observed layer execution. The report
counts requests with plans, decisions, expected planner savings, expected-active
versus observed-active actions, missed active actions, bypass/tunnel actions
that still saw activity, and safety-blocked requests. Category metadata can set
planner thresholds so future default-on changes have measurable evidence.
It also emits an observed layer-combination matrix keyed by stable labels
(`L0`, `L1`, `L2`, `L3`, `WS`, or `none`) with request count, saved
tokens, output tokens, and errors. This is factual corpus accounting, not a
simulated alternate-run replay. Category metadata can additionally declare
`scenario_validators` (`tool_heavy`, `cache_reuse`, `output_reduce`,
`output_reduce_ab`, `planner_alignment`, `websocket`, `low_error`,
`host_budget_ok`, `layer_combo_diversity`)
so a category fails unless the intended optimization behavior is actually
present in the captured request summaries; unknown validator names fail closed.
Categories can set `current_product_path=false` to remain visible and
category-testable as historical diagnostics while being excluded from
promotion/maxx client and workload counts.

`scripts/benchmarks benchmark-corpus --promotion-check` is the stricter
release/default-on gate. It ignores synthetic categories and fails closed unless
the corpus contains at least five `codex_cli` sessions, five `codex_desktop`
sessions, and real `live_operator` coverage for `repeat_read`, `ranged_read`,
`search_loop`, `git_status`, `test_failure`, `apply_patch_edit_read`,
`large_tool_output`, and `long_workday`. Every real category must also declare
`client_family`, `workload_class`, explicit zero error budget, explicit
re-read-canary budget, explicit latency budget, and a positive savings floor.
This keeps unit tests and synthetic fixtures useful while preventing a default
promotion from vague or one-sided evidence.
`wss-proof-export-corpus` appends deduplicated content-free proof rows to
existing categories and recalculates category gates from the combined records,
so new weaker rows cannot replace stronger existing proof.

`go run ./scripts/verify -mode release-proof-plan` prints the deterministic
operator ceremony for a release/default-on decision. The runbook starts from a
clean CI and synthetic-corpus baseline, opens a `workday-savings` window, lists
the scoped CLI and Desktop product launch paths, expands every required
live-corpus workload for both `codex_cli` and `codex_desktop`, then finishes
with `wss-proof-matrix --require-live-token-delta` and
`benchmark-corpus --promotion-check`. The strict matrix mode requires real
admin-state `live_delta` rows; replay bytes remain visible but cannot stand in
for product token savings. The command is content-free and plan-only: it does
not start capture, read payloads, or create fixtures. This keeps proof
collection manual, reviewable, and reproducible.
Unattended CLI capture collection uses
`go run ./scripts/utils codex-capture-run`, which owns the daemon foreground
process, sets `SLIMFERENCE_WSS_AB_CAPTURE`, waits for `/health`, runs scoped
Codex, records before/after admin-state deltas, replays with fail-on-lost
semantics, and appends an optional `wss-proof-matrix` row. The matrix row stores
live `billable_input_tokens_saved`, provider-cache read/create token deltas, and
safety counters (`parse_failures`, `degraded_sessions`, `compression_errors`,
`analytics_proof_events_dropped`) next to replay bytes. Release proof treats live
billable input-token savings and provider-cache read tokens as separate product
signals; replay bytes are retained only as a model-facing regression/safety
proxy. Any proof-critical analytics loss fails the release proof gate; low-value
analytics drops remain visible but are not product-proof blockers. The runner supports
`--codex-timeout` for bounded proof runs, `--exit-marker` /
`--exit-marker-count` for unattended shutdown, and `--quiet-codex-output` for
machine-readable runs without Codex TUI noise. Passing
`--resource-profile-proof <bundle-dir>` turns the same managed run into the
automated CLI host-resource proof: the runner defaults `--capture` to
`<bundle-dir>/frames.jsonl` and `--matrix-row` to
`<bundle-dir>/matrix.jsonl`, writes aggregate admin snapshots before/after,
`ps` snapshots before/after, a macOS `sample` file, and
`workday-finish.json`, then appends the content-free matrix row. This is the
preferred release-proof path for CLI workloads because it avoids detached
background daemons that do not inherit the capture environment reliably.
Expected-reducer validation is evidence-first: when a focused run misses an
expected reducer, the tool appends the matrix row and then exits non-zero. This
keeps negative live evidence such as missing hits or host-budget attention
auditable while still preventing a failed focused proof from passing.
Interactive Desktop proofs use `go run ./scripts/utils wss-proof-live-row`
after the operator-driven Codex.app prompts finish. The tool reads the current
content-free admin state/status snapshots, enforces the requested reducer
signals such as `tool_prune`, `tool_prune_tokens_saved`, or `host_budget_ok`,
and appends a matrix row without reading raw WSS frame payloads. This closes
Desktop cases where `codex-capture-run` cannot own the app process but the proof
still needs reducer-specific live counters before export into
`tests/fixtures/live_corpus`.
Focused `wss-proof-matrix` runs with `--required-workload` evaluate only rows
matching the requested workload classes. When `--expected-reducer` is also
passed, those command-line reducer expectations are authoritative for the
focused proof, so older exploratory rows in the same matrix cannot pollute a
single-mechanism closeout. Unfocused release-proof mode still validates every
row exactly as recorded.
`go run ./scripts/verify -mode host-resource-plan -client codex_cli|codex_desktop`
prints the T272 resource/profile ceremony for the only remaining host-budget
proof class. For CLI, the plan now prints the single automated
`codex-capture-run --resource-profile-proof` command. For Desktop, where the
app prompts remain operator-driven, the plan still prints the manual
admin-state snapshots before and after the workload via `aggregate-savings
--json`, `ps` RSS/CPU rows, `workday-savings finish --json`, a macOS `sample`
CPU profile for the Slimference process, and a WSS matrix row requiring
`host_budget_ok` plus a positive live economic token signal. Local
billable-input deletion, provider-cache read tokens, and output-side evidence
stay separate in the proof row. It deliberately does not enable a pprof HTTP
listener or open a new runtime surface; profiling stays operator-triggered and
file-based.
`go run ./scripts/utils wss-proof-clean-matrix ~/.slimference/captures
<clean-release-matrix.jsonl> --json` writes the explicit release-claim matrix.
It reads proof rows only, never raw WSS frames, and skips historical diagnostic
rows, host-budget issue rows, expected-zero rows with local savings, safety
issue rows, and rows without an economic signal. It may normalize stale
expected-reducer labels only when the same row has current live reducer
evidence, so a release report cannot pass by aggregate count alone.
`go run ./scripts/utils release-proof-report <clean-release-matrix.jsonl>
--resource-profile-proof <codex-cli-resource-proof-bundle-dir>
--resource-profile-proof <codex-desktop-resource-proof-bundle-dir>` produces the final content-free release proof
summary. It reads proof-matrix rows only and never raw WSS frames. The report
keeps local billable-input token deletion, request-side bytes, output-wire
bytes, provider-cache read/create tokens, tool-prune schema tokens,
output-reduce input overhead, output-reduce observed provider-output tokens,
output-reduce net-observed diagnostics, host-budget rows, and safety rows as
separate fields. Output-reduce net-observed is deliberately not a
counterfactual savings percentage: a focused output-reduce proof must show
guarded injection, observed output-token accounting, bounded input overhead,
host-budget OK, and zero safety errors, while a concrete output-token savings
claim still requires a matching no-directive A/B baseline. It fails closed
without validated CLI and Desktop resource/profile proof bundles
containing `admin-before.json`, `admin-after.json`, `ps-before.txt`,
`ps-after.txt`, `workday-finish.json`, `slimference.sample.txt`, and
`matrix.jsonl`; the JSON files must prove host-budget OK and zero WSS
parse/degrade/compression deltas, and each local `matrix.jsonl` must contain a
positive `host_resource_long_workday` row with `host_budget_ok` for the matching
client. Rows with their own expected reducers must satisfy those reducer
expectations inside the bundle; a positive provider-cache or local-savings
delta cannot mask a missed mechanism-specific proof.
This keeps a green host-budget snapshot from being mistaken for final resource
certification.
The 2026-06-06 release-proof refresh passed this strict path without enabling
the advanced shared Codex route. `benchmark-corpus --check`,
`benchmark-corpus --promotion-check`, and `benchmark-corpus --maxx-check` all
passed on `tests/fixtures/live_corpus`; the promotion/maxx gates saw 54 real
sessions split across `codex_cli=37` and `codex_desktop=17`. The local proof
inventory found 89 rows, 24 matrix files, all maxx workload classes complete,
and `safety_issue_rows=0`. The clean matrix step wrote 70 release rows from 89
local proof rows. The final release report against
`host-resource-codex_cli-auto-20260604T212018Z` and
`host-resource-codex_desktop-20260604T212111Z` returned
`gate_passed=true`, `resource_profile_proof_ok=true`,
`local_billable_input_tokens_saved=330518`,
`provider_cache_read_tokens=430720`, `tool_prune_tokens_saved=26`,
`output_reduce_injected_turns=2`, `host_budget_issue_rows=0`,
`proof_event_loss_rows=0`, and `safety_issue_rows=0`. These numbers are a
current release-corpus proof for the still-enabled mechanisms, not a universal
average savings percentage. The WSS output-reduce directive rows in that older
bundle are historical after T330; Codex WSS runtime now records
`codex_wss_directive_disabled` instead of injecting model-facing output-reduce
instructions.
`go run ./scripts/utils wss-output-reduce-ab-report <matrix.jsonl>
--min-net-tokens=1 --json` is the content-free output-reduce counterfactual
gate. It pairs matrix rows by `ab_pair_id` and `ab_variant` (`baseline` or
`directive`), requires the same client and workload class, requires provider
output-token observations in both rows, requires guarded output-reduce injection
only in the directive row, subtracts directive input overhead, and fails on
safety errors, output-reduce downgrades, host-budget violations, non-positive
output-token reduction, net tokens below the configured floor, or an injected
directive row that has no positive `output_reduce_input_overhead_tokens`.
`codex-capture-run` and `wss-proof-live-row` can stamp those A/B fields into
historical matrix rows; the report still reads only content-free proof
counters, never raw prompts, model text, or tool output. These A/B rows no
longer promote Codex WSS output-reduce into the product path.
The first focused CLI direct-answer/status A/B passed on 2026-06-05 after
fixing proof accounting to record general provider output tokens from WSS usage
frames and model-facing directive overhead instead of JSON re-marshal byte
churn: baseline `987` provider output tokens, directive `768`, directive input
overhead `23`, output saved `219`, net saved `196`, `22.19%` output-token
reduction, `lost=0`, host budget `ok`, and zero WSS safety errors. That is a
real positive pair for the tested workload, not a universal output-reduce
percentage. The content-free pair is committed as
`tests/fixtures/live_corpus/cli_output_reduce_ab_direct_answer/output_reduce_ab_report.json`;
`benchmark-corpus --maxx-check` no longer requires WSS output-reduce workloads
after T330 because Codex WSS model-facing directive injection is disabled. The
category-level output-reduce validators remain available for historical and
non-WSS diagnostics, but they are not current Codex WSS product proof. The first clean
Desktop direct-long A/B pair on 2026-06-05 proved route, guarded injection,
host-budget OK, `lost=0`, and zero WSS safety errors, but it was net-negative
(`245` baseline output tokens, `566` directive output tokens, `23` directive
overhead tokens, `-344` net tokens saved), so Desktop output-reduce is not part
of any broad savings claim from that evidence.
An autonomous CLI explanation-shape A/B did not generalize the win: after
compacting the standard preservation directive from `111` to `46` overhead
tokens, the pair still failed net-positive (`baseline=222`, `directive=248`,
`net=-72`). Explanation/deep-analysis and other detail-sensitive shapes now
bypass output-reduce injection by default until future paired A/B evidence proves
positive net savings without repair/re-ask signals; the current positive product
evidence is the direct-answer/status shape.
Historical host-budget attention rows or superseded expected-zero anomalies stay
visible in the report and now fail the release gate by row id. Current readiness
must be proven from a clean matrix or a focused release bundle instead of
letting stale diagnostic rows hide behind aggregate counts.
The WSS dispatcher includes active in-flight Phase-F sessions in `/admin/state`
snapshots, so capture deltas can see `frames_reencoded`,
`compressed_messages_mutated`, and `phasef_mutations` before the WebSocket
session fully tears down. Completed sessions are then folded into the same
monotonic dispatcher counters without double-counting.

Layer-0 mechanism cost is exposed as debug/audit telemetry, not product UI
noise. `/admin/state.savings.proxy_layer0_latency` reports rolling p50/p95/max
and average duration by route for `total`, `read_delta`, `structured_filter`,
`repeated_output`, and `chunk_dedup`. This lets release proof compare savings
against local host cost without logging payloads or charging the hot path with a
new per-frame disk probe. The hot path also uses the total Layer-0 duration as a
bounded runtime safety signal: repeated frames over the 25 ms budget demote
managed Codex reducers to full-pass until fast frames recover the gate.
Exact o200k token counts are reserved for real before/after savings guards and
positive savings claims. WSS no-op planner rows and output-reduce size gating
use cheap byte/4 estimates because those paths do not claim local billable
savings. Output-reduce proof gates use exact provider-reported output-token
usage when available and separately count injected instruction overhead. This
avoids loading the heavy BPE encoder for no-mutation frames while keeping every
reported token saving exact.

### Global flags

```
--no-tui / --headless   Run proxy foreground, no BubbleTea.
--port <n>              Override listen port.
--no-layer1/2/3         Disable Layer N.
--sliding-window <n>    Override L1 sliding window.
--log-level <lvl>       debug | info | warn | error.
--config <path>         Override config file path (T46).
-h / --help             Show help.
-V / --version          Print version.
```

### Exit codes (headless)

| Code | Meaning                                |
|------|----------------------------------------|
| 0    | Clean shutdown.                        |
| 1    | Boot or config error.                  |
| 2    | Bad flags / non-TTY without `--no-tui`.|
| 6    | Shutdown timeout (T60).                |

---

## 16. Installation

### From GitHub releases

```bash
curl -fsSL https://raw.githubusercontent.com/Christopher-Schulze/Slimference/main/install.sh | bash
~/.local/bin/slimference install
~/.local/bin/slimference status --preflight
```

The raw GitHub installer resolves the latest release, downloads the matching
macOS archive for the current architecture, installs only the local binary, and
leaves scoped Codex setup to the explicit `slimference install` or TUI Setup
step.

### From source (macOS M-series, recommended)

```bash
go run ./scripts/build --install
slimference doctor
slimference install
slimference status
```

`--install` writes the new binary to a same-directory temporary file and then
atomically renames it over `~/.local/bin/slimference`, so new processes never
observe a partially copied executable.

### From a release archive

```bash
curl -fsSL <url>/slimference_<version>_darwin_arm64.tar.gz | tar -xz -C /tmp
cd /tmp/slimference_<version>_darwin_arm64
./install.sh
```

## 17. Build and Release

Slimference v0.6.0 is a **macOS-first** product. The primary target is
Apple silicon (`darwin/arm64`); Intel macOS (`darwin/amd64`) is available as
an explicit release target. Linux, Windows, and container images are not part
of the public support surface.

### Default build (primary target only)

```bash
go run ./scripts/release --version v0.6.0
```

Produces:

```
dist/slimference_0.6.0_darwin_arm64/slimference
dist/slimference_0.6.0_darwin_arm64.tar.gz
dist/SHA256SUMS
```

### Public macOS set

```bash
go run ./scripts/release --version v0.6.0 --targets=darwin/arm64,darwin/amd64
```

Adds the Intel macOS archive next to the default Apple-silicon archive.

### Hand-picked macOS subset

```bash
go run ./scripts/release --version v0.6.0 \
    --targets=darwin/arm64
```

### `ldflags` injection

Both `main.version` (for backward compat) and
`github.com/Christopher-Schulze/Slimference/internal/buildinfo.Version` (the
canonical source read by `--version` and `doctor`) are set. Without
the buildinfo injection, `--version` would print the compile-time
default from `version.go` and ignore the tag.

### Reproducibility

- `-trimpath` strips absolute paths.
- `-s -w` strips debug sections.
- `go run ./scripts/build` is the canonical local build helper and
  always emits the single Slimference binary with those flags. It does
  not split the product into multiple runtime binaries.
- `go run ./scripts/build --install` installs through temp-file plus atomic
  rename instead of truncating the existing executable in place.
- `CGO_ENABLED=0` — Slimference has no C dependencies (SQLite via
  `modernc.org/sqlite` is pure Go).

### Release checklist

Before cutting artifacts, bump the version, update release notes if needed, run
`go run ./scripts/ci`, build with `go run ./scripts/release --version <tag>`,
verify SHA256 output, and smoke-test the produced binary with `--version`,
`doctor`, and `status --preflight`.

---

## 18. Testing Strategy

- **Unit tests**: `*_test.go` alongside every file. Target: high,
  behavior-significant coverage of production code (internal/ + cmd/).
- **Integration**: `tests/integration/` with `//go:build integration`
  tag; covers the full pipeline against a stub upstream.
- **TypeScript supplemental**: `tests/ts/` with `bun:test` for schema
  + CLI contract checks.
- **Race detector**: `go test -race ./...` green; required gate.
- **Coverage gate**: `scripts/coverage` fails CI below the threshold and runs
  package plus intra-package coverage serially to keep proxy shutdown/resource
  tests deterministic.
- **Benchmark harness**: `scripts/benchmarks` runs the canonical
  micro-benchmarks under `go test -bench`.

### Coverage headline

Current formal release gate: `go run ./scripts/ci` runs
`go vet ./...` plus `go run ./scripts/coverage -min=95.0`. `go vet ./...`
is the default static-analysis gate for all module packages, including
`scripts/` and `docs/`, because Slimference's repo tooling carries release
proof logic. No optional `staticcheck` or `golangci-lint` dependency is
required for normal CI. The coverage step is an aggregate gate for the
configured Go coverage profile. Individual package lines can report less than
the aggregate threshold while the total gate remains green; do not describe
that as a release failure unless `scripts/ci` itself exits non-zero. The hard
rule is behavior-significant testing: new or changed complex logic, product
paths, safety branches, routing/fallback decisions, and regression risks need
real failable tests. The intent is to keep meaningful product/safety paths
covered without spending engineering time on artificial coverage-chasing tests,
unreachable OS-dependent cleanup branches, or always-green assertions.

### Benchmarks

`internal/compression/bench_test.go`:
- `BenchmarkCompress_{small,medium,large,code}`: full L1 pipeline.
- `BenchmarkStripANSI`, `BenchmarkStripComments`,
  `BenchmarkExtractStructure`: per-sub-layer hot paths.

`internal/filter/bench_test.go`: filter hot paths.

`internal/proxy/layer0_bench_test.go`: Codex/WSS Layer-0 hot paths for
large git status compaction and repeated read-delta. T272 profiling found
repeated exact o200k tokenization as the dominant pre-cache cost for 64 KB
repeat-read frames; the bounded token-count cache keeps that path in the
sub-millisecond to low-millisecond range on Apple M1 benchmark runs. The
2026-06-04 full benchmark surface measured WSS repeated git-status at about
788 us/op and WSS repeated read 64 KB at about 814 us/op after the latest
safety hardening.

`internal/readcache/bench_test.go`: full-file and ranged read repeat-cache
hot paths, including archive-backed unchanged decisions. The same 2026-06-04
run measured full-repeat 64 KB at about 446 us/op and ranged-repeat 16 KB at
about 167 us/op.

`internal/chunkdedup/bench_test.go`: FastCDC chunking and partial-overlap
chunk-reference encoding. The same 2026-06-04 run measured FastCDC chunking
256 KB at about 211 us/op and partial-overlap 64 KB store/encode at about
2.03 ms/op.

`internal/contentarchive/bench_test.go`: archive write and archive expansion
for 64 KB-class payloads. Archive reads stay cheap for recovery, while 64 KB
archive writes measured about 29.4 ms/op on the full 3 s run; archive writes
therefore remain a bounded recovery path, not a blind default mutation tax.

`internal/planner/bench_test.go`: runtime planner decision overhead for a
large Codex WSS tool-output shape. The 2026-06-04 run measured that historical
lab/proof large-tool decision at about 267 ns/op.

Race coverage is part of the host-resource closeout evidence. The focused
savings/safety race pass covers `filter`, `readcache`, `chunkdedup`,
`toolprune`, `outputreduce`, `proxy/wsmitm`,
`quality`, and `hostmetrics`, and the full repository gate
`go test -race ./...` is green after the Codex recert helper-process timeout was widened for
race instrumentation. Full live CLI/Desktop resource/profile bundles are now
the release proof: the final CLI and Desktop bundles pass `release-proof-report`
with host-budget OK, zero WSS safety deltas, positive economic token evidence,
and the required local matrix rows.

`internal/analytics/phase_hist_test.go::BenchmarkPhaseHistogram_Record`:
phase recorder overhead (~15 ns/op on M1).

---

## 19. Package Map

```
cmd/slimference/              Entry point + every CLI subcommand.
  main.go                     Flag dispatch, subcommand router.
  codex_cmd.go                Codex traffic CLI core: run, enable, disable, status, certify.
  codex_desktop_proof_cmd.go  Codex Desktop app-server proof/status flow.
  tui_proxy_adapter.go        In-process proxy adapter for the Bubble Tea TUI.
  help.go + help_test.go      --help content; golden-file drift check (T64).
  headless.go                 --no-tui runner with signal traps (T44).
  integrate_cmd.go            integrate install|remove|status|emergency-off (T65).
  bypass_cmd.go               bypass on|off|status via admin API (T67).
  remote_proxy.go             TUI adapter talking to a remote daemon.

internal/proxy/               HTTP server + request pipeline.
  proxy.go                    New(), ServeHTTP(), toggles, admin router.
  handler.go                  Hot path, upstream relay, overflow recovery, compression orchestration.
  handler_workers.go          Compression worker, analytics worker, health JSON, periodic flush.
  handler_shutdown.go         Graceful shutdown, drain timeout, shutdown pprof dump.
  handler_accessors.go        TUI/admin accessors, cache flush, tool-prune helper extraction.
  provider.go                 detectProviderWithUA + request/response reconstruction.
  admin.go                    AdminStatus snapshot + /admin/* handlers.
  version_negotiation.go      Anthropic version whitelist (T62).

internal/compression/         Layer 1 sub-layers + Layer 1 pipeline.
  layer1.go                   Compress() orchestrator, dedup staircase (T53).
  prompt_cache.go             Breakpoint injection (T45).
  tool_compressor.go          Tuning knobs + filter set (T61).
  preview.go                  Structure-aware preview (T38).
  loop_detect.go              Loop-nudge Jaccard detector (T37).


internal/caching/             Layer 2 response cache + file watcher.

internal/analytics/           Rolling snapshots + phase histograms (T58).

internal/integrate/           Auto-integration (T65).
  integrate.go                Marker-fence block primitives.
  shellrc.go                  rc-file detection + write.
  codex_toml.go               config.toml fence writer with scope safety.
  detect.go                   Per-client + daemon detectors.
  install.go                  Install / Remove / DiffPreview.

internal/daemon/              launchd plumbing (macOS).
  daemon.go                   InstallLaunchd + plist + FormatStatus (T68).

internal/hooks/               Codex hook installers plus parked Claude hook code.
internal/filter/              Layer-0 pipeline + parser reducers + SQLite.
internal/security/            Secrets detector + per-session suspend (T59).
internal/tui/                 BubbleTea UI + keybinding registry (T64).
internal/readcache/           Read-hook delta cache (T37).
internal/toolarchive/         Large tool-result archive + expand (T40).
internal/checkpoints/         Smart-compaction checkpoints (T39).
internal/config/              Load / ResolveConfigPath / LoadOptions (T46).
internal/tokens/              tiktoken wrapper + per-provider calibration.
internal/debug/               Decision-chain recorder + replay.
internal/sessions/            Session logs, response-state, and file-backed T138 hook turn-state.
internal/resilience/          Retry + backoff + rate limiter.
internal/slogutil/            Rotating JSON log handler.
internal/buildinfo/           Build-time Version + Commit (ldflags-set).
internal/types/               Shared types (Provider, Message, ContentBlock).
internal/util/                Generic helpers.

scripts/release/              macOS release tarballs + SHA256 (T47).
scripts/benchmarks/           Benchmark runner.
scripts/coverage/             Coverage gate.
scripts/utils/                Offline session/decision/filter/proof reports and
                              local generated-artifact hygiene guard.

docs/
  documentation.md            This file.
  install.md                  Install/uninstall SSOT.
  spec.md                     Current technical target specification.
  layer0-exit-codes.md        Layer-0 exit-code matrix (T63).
  live-corpus-policy.md       Release-proof/live-corpus rules.
  benchmarks.md               Reproducible benchmark and corpus reports.
```

For the detailed dependency graph see `docs/map.md`.

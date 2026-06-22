# Slimference Specification v3

Last updated: 2026-06-19

This is the normative implementation target for Slimference. `docs/documentation.md`
is the explanatory reference; this file defines the compact product and technical
contract that code, tests, defaults, and current docs must satisfy.

## 1. Product Contract

Slimference reduces token usage for Codex-first coding workflows without product
drawdown. A drawdown is any runtime degradation in model intelligence, context
memory, recency, workflow reliability, hallucination risk, recovery, UX, or
normal Codex behavior. Development effort, captures, benchmarks, proof work, and
implementation complexity are not product drawdowns.

Default-on savings mechanisms must be deterministic, recoverable, fail-open, or
validated by live replay. When a mechanism cannot establish model-quality safety,
it must be removed from the product path.

New product mechanisms must be designed for default-on or automatic safe
enablement. New permanently default-off, manually promoted, or experimental
mechanisms are out of scope unless an explicit project override classifies them
as isolated legacy/lab/proof/operator work.

The owner local-savings target is **maximum practical `S_local`** (as high as
engineering and protocol physics allow) on longer eligible Codex sessions
without counting provider-cache discount. The previous floor of >=48% has been
met and exceeded; the goal is now to push `S_local` as high as possible.
Provider-cache economics, output-wire savings, and combined billable savings
remain valuable but cannot substitute for local input reduction.

Missing production-readiness evidence does not mean a candidate has zero
savings. Every candidate must be estimated before implementation with affected
route/workload, expected `S_local` impact, provider-cache impact, output impact,
confidence, and remaining readiness gaps. Candidate value guides prioritization;
product activation still requires the complete mitigated design to satisfy the
drawdown contract on the exact route/request class where it runs.

Guards are surgical product-safety mechanisms, not broad savings brakes. A guard
may reduce savings only for the exact route, request shape, command/output class,
state lineage, cache-prefix scope, or capability surface needed to prevent a
real product drawdown or upstream error. If the same safety can be achieved by a
narrower predicate, byte-equal observation path, recovery path, readiness latch,
state detach, metadata-consistent mutation, or scoped demotion, the broader
guard is a local-savings regression.

Savings work is priority-ordered by expected local impact. Engineering effort
must focus first on structural double-digit `S_local` candidates such as
command-output-first Codex interception, T354/Class-B/server-state continuation,
Desktop/Class-B unlocks, and high-frequency search/captured-output paths. Small
parser or guard-polish work is acceptable when cheap, when it unblocks a larger
lane, or when live owner input is the only blocker for the higher-leverage lane.
Small policy-safe wins should compound, but they are bundled into efficient
structural work until all materially sensible savings lanes are harvested or
proven blocked. Micro-optimization is a later product phase: after structural
savings are exhausted, Slimference continues down the optimization stack and
compounds smaller measured net-positive wins without relaxing the drawdown
contract.

## 2. Active Product Layers

| Layer | Name | Purpose | Safety contract |
| --- | --- | --- | --- |
| 0 | Pre-entry / Codex tool-output reducers | Shrink tool outputs before or as they enter model-visible context | Parser/reducer guards, archive recovery where needed, fail-open |
| 1 | Deterministic compression | Remove deterministic waste from safe prefix/tool content | Shorter-than-original guard, safety tiers, archive-backed recovery for lossy transforms |
| 2 | Response/provider cache leverage | Avoid repeat work and account provider-cache economics | Canonical keys, stochastic/stateful bypass, dependency invalidation, negative-net visibility |
| 3 | Output/tool-surface reduction | Reduce completion, repeated response, stale-read, and tool-definition overhead | Exact-reply/repair guards, provider-shape validation, auto-demotion |

Slimference must not ship model-facing semantic context replacement: no
external summarizer, local LLM summarizer, OCRL full-history replacement,
context-ledger insertion, summary cache application, or background summary worker.

## 3. Layer 0

Layer 0 includes CLI filters, Codex hook reducers, WSS Phase-F reducers,
readcache, full-file and ranged-read deltas, exact repeated-output dedup,
repo-safe search grouping/delta, git/build/test/log compactors, TOML/catalog
filters, and recoverable chunk dedup.

Requirements:

- command-output-first Codex interception is a required product lane: when a
  scoped Codex hook, launcher shim, app-server boundary, PTY wrapper, command
  runner proxy, MCP/tool facade, or subprocess wrapper can compact stdout/stderr
  before large tool output becomes durable WSS history, that path takes
  priority over equivalent downstream cleanup;
- command-output-first compaction must preserve command identity, cwd, args,
  exit code, stdout/stderr ordering and distinction, failure/warning/source/
  diagnostic/path/line/count/artifact facts, and byte-equal fail-open behavior;
- command-output-first compactors must provide exact raw-output recovery through
  archive, tee, sideband, or rerun, and retry/rerun cost is counted as negative
  savings;
- first reads and first post-edit reads preserve full relevant context;
- repeated/ranged reads may compact only with stable session/path/range identity;
- archive-backed references require exact local recovery;
- search grouping must preserve representative evidence and avoid cross-repo
  false hits;
- changed repeated searches must show added/removed evidence or full-pass;
- HTTP must not emit archive-backed chunk references unless explicitly proven
  safe for that route;
- Codex WSS read-delta/status-output compaction is allowed only when exact or
  state-safe and the WSS proof is fresh; broader tool-output request mutation
  remains default-off and requires explicit lab/proof opt-in;
- WSS recoverable chunk dedup requires route/workload/proof policy allow plus
  the same state-safety gate or explicit Codex WSS tool-output mutation opt-in;
- Codex WSS search/path-list/tool-output reduction remains risk-gated and
  full-passes when first-pass evidence, source attribution, or upstream session
  state could become ambiguous; exact parser-proven path-list classes such as
  `rg --files`, `fd`/`fdfind`, and conservative bounded `find` may compact
  only after both command shape and payload pass strict path-list parsers;
  metadata-less WSS outputs may compact as neutral plain path lists only when a
  payload-only parser rejects search hits, source, diagnostics, prose, shell
  metacharacters, and oversized listings, and the compacted text must not claim
  a specific originating command;
- sharper Codex WSS search-output caps may become runtime-active only through a
  a versioned final release proof artifact whose nested focused search-cap
  proof `capture_reports` themselves pass their row gates and prove CLI plus
  Desktop, two positive search-loop rows, retained evidence, replay safety,
  `captured_output` delta tool-output mutation proof for the selected cap, live
  mutated search-output downstream-state proof with a clean following turn, and
  before/after Codex route hygiene with no persistent shared route or legacy
  base-url keys; the final report must preserve `proof_schema_version`, both
  status snapshot paths, `required_reducer_hits.captured_output`,
  `downstream_state_proof`, and a named selected cap; aggregate proof counters,
  stale unversioned or pre-v2 final reports, and raw cap values are not product
  config knobs;
- every reducer records enough local telemetry to prove hit, miss, block, and
  fail-open reasons without logging raw private payloads.

## 4. Layer 1

Layer 1 is a local deterministic compressor over safe old prefix/tool content.
It may use ANSI stripping, JSON compaction, comment stripping, exact and near
dedup, structure extraction, deltas, prompt-cache breakpoints, tool-aware
compaction, success short-circuiting, image replacement where safe, repeated
collapse, graph pruning, and pre-filter markers.

Requirements:

- never run semantic paraphrase;
- preserve current/recent user task state;
- keep configured sliding-window exchanges verbatim except safe in-window
  tool-output compactors;
- apply shorter-than-original and schema-validity checks before forwarding;
- archive originals before any recoverable lossy transform;
- expose sub-layer decisions and savings for proof and debugging.

## 5. Layer 2

Layer 2 is a local response cache plus provider-cache leverage/accounting
surface. Generic OpenAI API prompt-cache steering is allowed through privacy-safe
stable-prefix hash keys. CodexChatGPT backend prompt-cache mutation is blocked
until live request acceptance is proven.

Requirements:

- key by provider, method, route, policy partition, canonical forwarded body,
  and cache-relevant headers;
- bypass stochastic, streaming, stateful, tool-calling, previous-response,
  conversation/thread, and non-deterministic request shapes;
- invalidate file-dependent entries only when watches are actually armed;
- keep provider-cache read/create/cached-token accounting separate from local
  reducer savings;
- for generic OpenAI API traffic, steer prompt-cache routing only with
  deterministic stable-prefix-hash keys, preserve caller-owned cache fields,
  never include raw prompt text/path/session data in generated keys, and
  downgrade fail-open with per-model cooldown if upstream rejects the fields;
- do not inject OpenAI prompt-cache keys into CodexChatGPT backend routes until
  live request acceptance is proven;
- server-state reuse (`previous_response_id`) is default-on with fail-open
  (4xx rejection → full body resend); live proof of net-positive `S_local`
  on a real long session is the remaining activation gate.

## 6. Layer 3

Layer 3 reduces output, repeated response, stale-read, obsolete-read, and
tool-surface overhead. Active mechanisms include output-reduce policy, stop
sequences, streamcut, repetition detection, stale-read aging, obsolete-read
pruning, conservative concise-chat hints, and optional tool-schema pruning.

Requirements:

- output directives must be idempotent and provider-shape valid;
- exact-answer and repair-followup turns skip directive injection;
- stop-sequence injection and HTTP/SSE streamcut may apply only to
  direct-answer and explanation task shapes; exact replies, code edits,
  read-only analysis, reviews, debugging, planning, final summaries, and
  unknown shapes must pass through;
- repetition detection may rewrite response text only from tool-result-derived
  source indexes, never from arbitrary user prompt text or pasted/code-fence
  text;
- concise-chat hints may apply only to direct-answer and explanation turns, must
  full-pass code/docs/JSON/log/diff/repair/review/planning/tool-output turns,
  and must respect a low-ROI input-token guard;
- on HTTP routes, existing output-reduce profiles keep priority for requests at
  or above the generic output-reduce minimum; concise-chat fills only the
  guarded chat-size gap;
- on Codex WSS routes, frames carrying a prompt-cache prefix remain byte-equal;
- aggressive profiles require measured positive net savings and low failure
  deltas;
- default-on Layer 3 must be deterministic, shape-bounded, auto-demoted, and
  invisible to model reasoning unless proof shows no repair/re-ask or quality
  regression;
- Codex WSS model-facing output-reduce directive injection is experimental
  non-product lab/proof code and stays disabled in the product path unless a
  future proof gate demonstrates positive net savings without repair/re-ask
  regressions;
- tool-schema pruning must preserve selected/used tools, reattach safely on
  miss, and retry when upstream/tool behavior proves pruning unsafe;
- output-wire savings, directive overhead, provider-cache economics, and local
  input savings are reported separately.

## 7. Codex Routing

Default product routing is scoped Codex:

- `slimference install` installs launchd service material and Codex hooks, but
  does not arm global hosts, pfctl, system proxy, persistent base-url env vars,
  or Claude Code hooks;
- scoped Codex launchers may introduce command-output-first interception only
  inside the launched process tree or explicit local tool boundary; normal Codex,
  browser ChatGPT, ordinary ChatGPT.app, shell configuration, system proxy, and
  unrelated apps remain untouched;
- the TUI Launch view and `slimference codex run -- <prompt>` affect only the
  spawned Codex CLI process and fail open to direct Codex if the daemon route is
  unavailable;
- `slimference codex run --transport=auto -- <prompt>` uses the savings-first
  ladder `wss_phasef -> http -> direct`; a clean `wss_bridge` proof remains
  visible for diagnostics and explicit bridge runs, but auto does not prefer a
  byte-equal bridge over HTTP savings;
- the daemon checks Codex WSS proof drift after startup, and TUI startup/status
  refreshes read the Codex route state; either path may launch background
  recertification through the same lock/backoff-gated recert path;
- `slimference codex desktop prove` and
  `slimference codex launch-desktop --transport=app-server` affect only the
  launched Codex.app process tree through the process-local app-server shim and
  fail closed while Codex.app is already running unless `--replace-existing` is
  explicitly requested; Desktop status must not apply an old green proof to a
  different currently running Codex.app process;
- the Desktop app-server shim sets provider WebSocket support from the same
  savings-first auto decision as the CLI: fresh `wss_phasef` enables WSS,
  stale or missing Phase-F uses HTTP Responses savings;
- Desktop route/savings truth comes from TUI Activity/Status, daemon decisions,
  app-server shim flight logs, and Desktop proof status; Activity separates live
  scoped instance counts from recent routed requests, and current Codex Desktop
  builds do not expose a stable external Slimference text-chip contract;
- `slimference enable` / `slimference disable` are the advanced shared Codex
  route and write/remove only marker-owned `slimference-codex` provider config;
- Browser ChatGPT and ordinary ChatGPT.app launches remain direct;
- Claude Code is parked and not modified by install or TUI flows;
- global transparent mode is lab-only and requires explicit lab/global flags;
- default install may create local CA material for isolated legacy/lab and
  diagnostic paths, but it must not trust that CA in Keychain or arm hosts,
  pfctl, or system proxy settings without explicit lab commands.

Codex WSS Phase-F must degrade to byte-equal forwarding on unknown frames,
parser drift, policy block, missing proof, host-resource pressure, or recovery
failure.

## 8. Configuration

Fresh configs expose active product surfaces only:

- `[proxy]` loopback listener, Codex direct-WSS policy, optional server state,
  and `[proxy.openai_prompt_cache]`;
- `[transparent]` defaults to no global arming; `scoped_desktop_proxy=true` is
  process-local launcher support, not system proxy or hosts routing;
- `[compression] layer1_enabled=true`, `layer2_enabled=true`, sliding window,
  structure languages, and deterministic thresholds;
- `[compression.output_reduce]` for output policy, concise-chat hints, stop
  sequences, streamcut, repetition detection, stale/obsolete read pruning,
  Codex savings policy, chunk-dedup proof controls, and the proof-latched
  `codex_search_cap_proof_path` search-cap promotion input pointing at the
  versioned final release proof artifact;
- `[compression.tuning]` for overflow, in-window compaction, structure preview,
  coordinator, optional tool-prune, streaming, planner, and tuning knobs;
- `[filter] passthrough_max_chars=2000` by default;
- `[cache]`, `[usage]`, `[secrets]`, `[analytics]`, `[logging]`, `[hooks]`, and
  `[debug]` as local runtime surfaces;
- legacy `layer3_enabled` may be read as `layer2_enabled` only for old config
  compatibility and must not revive removed semantic Layer 2 behavior;
- no `min_tokens_for_layer2`;
- no removed external model compression section;
- no `[compression.summary]`;
- no `[compression.ocrl]`.

Legacy config files may contain removed fields. The loader must ignore or strip
them without enabling any removed product path.

## 9. Observability

Observability must distinguish:

- local billable-input savings;
- provider-cache read/create/cached-token economics;
- output-wire savings and directive overhead;
- tool-prune schema-token savings;
- route/workload/proof status;
- fail-open and auto-demotion reasons;
- host-resource status;
- guard potential: tokens blocked by each guard, exact guard reason, candidate
  mechanism, request shape, route, command/output class, and the smallest
  recorded readiness gap for recovering those savings.

The normal TUI must show product signals, not internal parser matrices. Debug
surfaces may expose detailed counters.

## 10. Proof Gates

Required local gates:

- `go test ./...`
- `go run ./scripts/ci`
- focused package tests for touched mechanisms
- corpus gates for live proof when claims rely on live evidence

`go run ./scripts/ci` is the final local truth gate: gofmt check, `go vet`,
`go build`, `go test`, 95.0% aggregate coverage, Codex smoke gate, live corpus
gate, and leaf audit.

Savings claims must be scoped to the evidence that proves them. Synthetic smoke
fixtures prove report executability, not production median savings.

## 11. Removed Semantic Summary Invariant

The following must stay absent from product code, configs, tests, fixtures, and
current docs:

- side-channel summarization providers;
- local LLM summarization;
- summary cache application;
- background summary queue/workers;
- OCRL/context-ledger packages or config;
- model-facing context replacement by capsules, summaries, or paraphrases;
- benchmark or live-corpus promotion gates that require removed semantic summaries
  mechanisms.

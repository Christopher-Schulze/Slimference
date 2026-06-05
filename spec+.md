# Slimference Specification v3

Last updated: 2026-06-05

This is the current technical target for Slimference after Layer 2 removal.
Historical v1/v2 text that described MiniMax, OCRL, or context-ledger insertion
is superseded.

## 1. Product Contract

Slimference reduces token usage for Codex-first coding workflows without product
drawdown. A drawdown is any runtime degradation in model intelligence, context
memory, recency, workflow reliability, hallucination risk, recovery, UX, or
normal Codex behavior. Development effort, captures, benchmarks, proof work, and
implementation complexity are not product drawdowns.

Default-on savings mechanisms must be deterministic, recoverable, fail-open, or
proven by live replay. When a mechanism cannot prove model-quality safety, it
must be removed from the product path.

## 2. Active Product Layers

| Layer | Name | Purpose | Safety contract |
| --- | --- | --- | --- |
| 0 | Pre-entry / Codex tool-output reducers | Shrink tool outputs before or as they enter model-visible context | Parser/reducer guards, archive recovery where needed, fail-open |
| 1 | Deterministic compression | Remove deterministic waste from safe prefix/tool content | Shorter-than-original guard, safety tiers, archive-backed recovery for lossy transforms |
| 2 | Response/provider cache leverage | Avoid repeat work and account provider-cache economics | Canonical keys, stochastic/stateful bypass, dependency invalidation |
| 4 | Output/tool-surface reduction | Reduce completion and tool-definition overhead | Exact-reply/repair guards, provider-shape validation, auto-demotion |

The old semantic summary path is retired. Slimference must not ship MiniMax, external summarization,
local LLM summarization, OCRL full-history replacement, context-ledger insertion,
summary cache apply, or background summary workers.

## 3. Layer 0

Layer 0 includes CLI filters, Codex hook reducers, WSS Phase-F reducers,
readcache, ranged-read deltas, exact repeated-output dedup, repo-safe search
grouping/delta, git/test/log compactors, and recoverable chunk dedup.

Requirements:

- first reads and first post-edit reads preserve full relevant context;
- repeated/ranged reads may compact only with stable session/path/range identity;
- archive-backed references require exact local recovery;
- search grouping must preserve representative evidence and avoid cross-repo
  false hits;
- changed repeated searches must show added/removed evidence or full-pass;
- HTTP must not emit archive-backed chunk references unless explicitly proven
  safe for that route;
- WSS recoverable chunk dedup requires route/workload/proof policy allow;
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

Layer 2 is a local response cache plus provider-cache accounting surface.

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
  live request acceptance is proven.

## 6. Layer 4

Layer 4 reduces output and tool-surface overhead.

Requirements:

- output directives must be idempotent and provider-shape valid;
- exact-answer and repair-followup turns skip directive injection;
- aggressive profiles require measured positive net savings and low failure
  deltas;
- tool-schema pruning must preserve selected/used tools, reattach safely on
  miss, and retry when upstream/tool behavior proves pruning unsafe;
- output-wire savings, directive overhead, provider-cache economics, and local
  input savings are reported separately.

## 7. Codex Routing

Default product routing is scoped Codex:

- `slimference codex run -- ...` affects only the spawned CLI process;
- `slimference codex launch-desktop ...` affects only the launched Codex app
  process;
- Browser ChatGPT and ordinary ChatGPT.app launches remain direct;
- global transparent mode is lab-only and requires explicit global flags.

Codex WSS Phase-F must degrade to byte-equal forwarding on unknown frames,
parser drift, policy block, missing proof, host-resource pressure, or recovery
failure.

## 8. Configuration

Fresh configs expose active product surfaces only:

- `[compression] layer1_enabled`, `layer2_enabled`, sliding window, structure
  and deterministic tuning;
- Layer 0, readcache, chunk dedup, output reduce, tool prune, cache, and provider
  settings;
- no `layer2_enabled`;
- no `min_tokens_for_layer2`;
- no `[compression.minimax]`;
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
- host-resource status.

The normal TUI must show product signals, not internal parser matrices. Debug
surfaces may expose detailed counters.

## 10. Proof Gates

Required local gates:

- `go test ./...`
- `go run ./scripts/ci`
- focused package tests for touched mechanisms
- corpus gates for live proof when claims rely on live evidence

Savings claims must be scoped to the evidence that proves them. Synthetic smoke
fixtures prove report executability, not production median savings.

## 11. Removed Semantic Summary Invariant

The following must stay absent from product code, configs, tests, fixtures, and
current docs:

- MiniMax or any side-channel summarization provider;
- local LLM summarization;
- summary cache application;
- background summary queue/workers;
- OCRL/context-ledger packages or config;
- model-facing context replacement by capsules, summaries, or paraphrases;
- benchmark or live-corpus promotion gates that require removed semantic summaries
  mechanisms.

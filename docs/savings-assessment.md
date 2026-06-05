# Slimference - Savings Assessment

Date: 2026-06-05
Scope: current product architecture after Layer 2 removal
Method: repository evidence, checked-in fixtures, and current product safety policy

## Executive Summary

Slimference now has four active product layers:

- Layer 0: pre-entry / Codex tool-output reduction
- Layer 1: deterministic proxy-side compression
- Layer 2: response and provider-cache leverage
- Layer 4: output and tool-surface reduction

The old semantic summary path is retired. Slimference does not use MiniMax, another external model, a
local LLM, OCRL, or context-ledger insertion as a product savings path. That
choice is intentional: replacing old context with any summary can remove details
and create model-quality drawdown. The remaining product stack saves tokens by
removing repeated, noisy, structurally redundant, or recoverably archived tool
output, not by asking the model to reason from a lossy memory replacement.

## Current Product Layers

| Layer | Role | Deterministic | Product drawdown profile | Main savings source |
| --- | --- | --- | --- | --- |
| Layer 0 | Pre-entry and WSS/Codex tool-output reducers | Yes | Fail-open and proof-gated for recoverable refs | read/ranged-read/search/git/test/log/repeated/chunk outputs |
| Layer 1 | Deterministic compression of safe conversation/tool prefix content | Yes | Safe tiers only in default product path; archive-backed where needed | ANSI/JSON/dedup/delta/structure/repeated collapse |
| Layer 2 | Response cache and provider-cache steering/accounting | Yes | No model-content loss: local replay is fail-closed, provider steering does not rewrite prompt content | repeated effective requests and reusable stable prefixes |
| Layer 4 | Output discipline and tool-schema pruning | Rule-based deterministic | Safe profile only for default product path | shorter assistant output and smaller tool surface |

## Realistic Savings Range

The current stack is workload-dependent. The strongest savings appear when the
agent repeats expensive tool surfaces: reads, searches, git status/diff, tests,
logs, and long command outputs.

| Workload | Realistic product savings |
| --- | --- |
| Tool-heavy Codex coding with repeated reads/searches/tests | 30-60% |
| Very repeat-heavy sessions with proven readcache/chunk/cache hits | 60-80%+ |
| Mixed coding and chat with moderate tools | 15-40% |
| Linear greenfield/chat with little repeated tool output | 5-20% |

These ranges must not be marketed as universal. They are valid as engineering
expectations, not as release claims, until backed by the live-corpus gates.

## Proven vs Unproven

What is proven by the repository:

- Layer 0 and Layer 1 reducers are deterministic and covered by focused tests.
- WSS/Codex reducer paths have checked-in proof fixtures for repeated reads,
  ranged reads, search loops, git status, large tool output, output-reduce, and
  related live-corpus categories.
- The benchmark and release-proof scripts fail closed on missing metadata,
  missed validators, safety counters, and weak promotion evidence.
- Go source no longer contains the retired Layer 2 implementation.

What still requires live/product proof:

- Final median savings across a broad live Codex Desktop and CLI workday corpus.
- Host resource proof on target machines under real traffic.
- Provider-cache economics over long sessions.
- The exact contribution split between Layer 0, Layer 1, Layer 2, and Layer 4 on
  representative real work.

## Where Savings Come From

### Layer 0

Layer 0 is the largest safe lever because it prevents redundant tool output from
entering model-visible history at all. Repeat reads, ranged reads, search
grouping/delta, exact repeated output dedup, and recoverable chunk dedup are the
economic center of the product.

### Layer 1

Layer 1 is the broad base layer. It removes deterministic waste such as ANSI
noise, JSON whitespace, repeated blocks, old structural detail, duplicate tool
results, and stable path repetition. It is not allowed to replace old context
with a semantic paraphrase.

### Layer 2

Layer 2 pays when effective requests or stable prefixes repeat. It is highly
valuable when it hits and close to irrelevant when traffic is unique. Claims
must therefore be reported separately from local reducer savings. OpenAI
prompt-cache steering now defaults to model-bound stable-prefix keys for generic
OpenAI API traffic: the proxy hashes only stable prefix shape, never raw prompt
text, and does not inject keys into CodexChatGPT backend routes without live
acceptance proof. This can improve provider-side cache hit probability and
latency/cost when many turns share long static instructions, tools, or history;
it cannot create savings on unique one-shot prompts.

### Layer 4

Layer 4 trims output and tool-definition overhead. Its default-safe value is
smaller than Layer 0, but it is cheap, local, and useful on tool-heavy sessions.
Aggressive output shaping remains proof-gated because model behavior must not
be degraded.

## Final Judgment

The product direction is correct after Layer 2 removal. The max-savings path
without model-quality drawdown is not summarization. It is aggressive,
deterministic, recoverable handling of tool output plus cache leverage.

Current honest position:

- implementation direction: strong
- product drawdown posture: strong for active default-safe layers
- savings potential: high on real coding workflows, low on non-repetitive chat
- final release proof: still depends on clean live-corpus, resource, and cache
  evidence

The next work should stay focused on measured Layer 0/WSS, Layer 1 safety tiers,
Layer 2 cache proof, Layer 4 output/tool-surface proof, and release-corpus
coverage. Do not resurrect a model-facing Layer 2 summary path.

# Slimference - Savings Assessment

Date: 2026-06-09
Scope: v0.6.0 product architecture after Layer 2 removal
Method: repository evidence, checked-in fixtures, current local release proof,
and product safety policy

## Executive Summary

Slimference now has four active product layers:

- Layer 0: pre-entry / Codex tool-output reduction
- Layer 1: deterministic proxy-side compression
- Layer 2: response and provider-cache leverage
- Layer 3: output and tool-surface reduction

The old semantic summary path is retired. Slimference does not use another
external model, a local LLM, OCRL, or context-ledger insertion as a product
savings path. That choice is intentional: replacing old context with any
summary can remove details and create model-quality drawdown. The remaining
product stack saves tokens by removing repeated, noisy, structurally redundant,
or recoverably archived tool output, not by asking the model to reason from a
lossy memory replacement.

## Current Product Layers

| Layer | Role | Deterministic | Product drawdown profile | Main savings source |
| --- | --- | --- | --- | --- |
| Layer 0 | Pre-entry and WSS/Codex tool-output reducers | Yes | Fail-open and proof-gated for recoverable refs | read/ranged-read/search/git/test/log/repeated/chunk outputs |
| Layer 1 | Deterministic compression of safe conversation/tool prefix content | Yes | Safe tiers only in default product path; archive-backed where needed | ANSI/JSON/dedup/delta/structure/repeated collapse |
| Layer 2 | Response cache and provider-cache steering/accounting | Yes | No model-content loss: local replay is fail-closed, provider steering does not rewrite prompt content | repeated effective requests and reusable stable prefixes |
| Layer 3 | Output discipline and tool-schema pruning | Rule-based deterministic | Safe profile only for default product path | shorter assistant output and smaller tool surface |

## Realistic Per-Session Savings Range

The current stack is workload-dependent. The strongest savings appear when the
agent repeats expensive tool surfaces: reads, searches, git status/diff, tests,
logs, and long command outputs.

| Routed Codex session shape | Realistic product savings | Strong-session upside |
| --- | ---: | ---: |
| Normal tool-heavy coding with repeated reads/searches/tests | 25-50% | 50-60% |
| Long refactor/debug loops | 35-65% | 65-75% |
| Search/read/log-heavy loops | 45-70% | 70%+ bursts |
| Mixed coding and chat with moderate tools | 15-40% | 40-50% |
| Linear greenfield/chat with little repeated tool output | 0-15% | 20% |

These ranges must not be marketed as universal. The current local release proof
backs concrete corpus/resource claims, not a universal average across every
future user workload.

## Proven vs Unproven

What is proven by the repository:

- Layer 0 and Layer 1 reducers are deterministic and covered by focused tests.
- WSS/Codex reducer paths have checked-in proof fixtures for repeated reads,
  ranged reads, search loops, git status, large tool output, output-reduce, and
  related live-corpus categories.
- The benchmark and release-proof scripts fail closed on missing metadata,
  missed validators, safety counters, and weak promotion evidence.
- Go source no longer contains the retired Layer 2 implementation.
- The 2026-06-09 v0.6.0 refresh passed the checked-in live-corpus normal,
  promotion, and maxx gates: 55 requests, 51 real sessions, `codex_cli=34`,
  `codex_desktop=17`.
- The 2026-06-06 strict content-free release proof passed over 70
  clean release matrix rows with CLI and Desktop resource bundles:
  `gate_passed=true`, `resource_profile_proof_ok=true`,
  `local_billable_input_tokens_saved=330518`,
  `provider_cache_read_tokens=430720`, `tool_prune_tokens_saved=26`,
  `host_budget_issue_rows=0`, `proof_event_loss_rows=0`,
  `safety_issue_rows=0`, and all maxx workload classes complete.

What still requires live/product proof:

- Final median savings across many independent real user workdays. The current
  corpus proves mechanism breadth and concrete local economic deltas, not a
  market-wide average.
- Host resource proof on additional target machines and OS/hardware profiles.
- Provider-cache economics across more long-session shapes.
- The exact contribution split between Layer 0, Layer 1, Layer 2, and Layer 3
  for each workload family outside the captured release corpus.

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

### Layer 3

Layer 3 trims output and tool-definition overhead. Its default-safe value is
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
- current release proof: passed locally on the checked-in live corpus plus
  clean CLI/Desktop resource bundles
- remaining caution: do not generalize the 330,518-token release-matrix result
  into a universal percentage

Future work should only broaden measured corpus coverage, hardware/resource
coverage, and per-workload layer attribution. Do not resurrect a model-facing
Layer 2 summary path.

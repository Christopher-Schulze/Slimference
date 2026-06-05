# Output Reduce

Slimference's output-reduce layer reduces expensive completion tokens by
injecting short provider-specific output rules into the outbound system prompt.
It never edits provider responses after generation.

## Runtime Behaviour

- Enabled by default under `[compression.output_reduce]`.
- Skipped for small requests below `min_input_tokens` because the instruction
  overhead would dominate likely savings.
- Skipped for constrained exact-answer turns (`reply exactly`, `answer
  exactly`, `respond exactly`, `say exactly`, `output exactly`) because the
  user already supplied a stronger output contract and adding another directive
  is pure input overhead.
- Idempotent via `signature_marker`; if the marker is already present in the
  request body, Slimference does not inject again.
- Runs after input compression and tool pruning, before Layer 3 cache lookup
  and before the upstream request. Cache keys therefore include the injected
  prompt when injection is active.
- Provider profiles:
  - `off`: disables injection without turning the feature off globally.
  - `mild`: shortest low-risk discipline.
  - `standard`: direct answers, no recap, patch-oriented edits.
  - `aggressive`: stronger anti-filler rules for measured high-savings paths.
  - `codex_aggressive`: Codex-specific terse workflow rules.
  - `custom`: uses `custom_directive_path`.
  - `auto`: selects `standard` for generic providers and
    `codex_aggressive` for Codex traffic.
  - legacy aliases `anthropic`, `openai`, `codex`, and `noop` remain accepted
    for older configs.
- T141/T148 classifies each request shape (`direct_answer`, `code_edit`,
  `debugging`, `planning`, `review`, `tool_result_reasoning`,
  `new_file_generation`, `exact_reply`, `repair_followup`) and appends
  shape-specific guardrails so review and debugging tasks do not lose exact
  findings, paths, commands, or errors. Shape detection reads only
  task/content-bearing fields rather than tool-schema descriptions, so a tool
  description containing "create file" does not misclassify an unrelated
  direct-answer turn.
- T148 repair-followup turns (`you skipped`, `explain more`, `what did you do`,
  malformed patch/application failure wording, and German equivalents) are
  skipped with `repair_followup_low_roi`: if the user is asking for missing
  detail, another brevity directive is negative ROI.
- Codex Responses requests use Responses-compatible injected content blocks
  (`type: "input_text"`). Slimference must never inject generic
  `type: "text"` blocks into `input[].content[]`, because current Codex
  backends reject that shape.
- Auto-tune can downgrade underperforming provider/model/task-shape buckets
  from aggressive -> standard -> mild -> off based on observed overhead,
  upstream failures, and T148 one-shot repair/user-reask signals from the next
  turn in the same session.

## Config

```toml
[compression.output_reduce]
enabled = true
profile = "auto"
custom_directive_path = ""
signature_marker = "#slimference-output-rules"
max_added_bytes = 1400
min_input_tokens = 400
auto_disable_threshold = 30
auto_tune_enabled = true
auto_tune_min_samples = 30
min_net_savings_pct = 15
max_failure_rate_delta = 0.05
cooldown_turns = 50
```

`custom_directive_path` can point at a local text file. If the file does not
include the configured marker, Slimference prepends it automatically so future
turns remain idempotent.

## CLI

```bash
slimference output-reduce status
slimference output-reduce enable
slimference output-reduce disable
slimference gain --output [today|week|month|all] [--json|--csv]
```

The command edits the resolved config file using the same config update path as
active layer toggles. `gain --output` reads persisted analytics JSONL and reports
applied/skipped requests, directive input overhead, observed output tokens, and
profile/task-shape/reason breakdowns.

## Measurement

The admin status payload exposes:

- `output_reduce.enabled`
- `output_reduce.profile`
- `output_reduce.injected_turns`
- `output_reduce.skipped_turns`
- `output_reduce.input_overhead_tokens`
- `output_reduce.output_tokens_observed`
- `output_reduce.avg_output_tokens`
- `output_reduce.last_reason`
- `output_reduce.last_added_tokens`
- `output_reduce.auto_tune_enabled`
- `output_reduce.downgrades`

These are observability counters, not a false "proven saving" claim. Real
provider-side saving still depends on model compliance and must be judged on
live sessions or a real corpus. `gain --output` intentionally reports only
observable telemetry until that baseline exists.

## Limits

- Output-reduce does not mutate responses post-hoc.
- It does not guarantee a fixed percentage saving.
- The `min_input_tokens` gate is mandatory for sane economics: short turns often
  cannot recover the directive overhead.

## Output-Token Reduction Sprint (T165 / T166 / T167)

Three additional deterministic mechanisms run alongside the system-prompt
profile above. They target output tokens directly rather than via instruction
shaping.

### T165 - Stop-Sequence Injection

`internal/outstop/` curates a versioned list of trailing-commentary openers
(`"\nLet me know"`, `"\nHope this"`, `"\nIs there anything"`, …). The proxy
injects the top four phrases into every Anthropic (`stop_sequences`) and OpenAI
/ Codex (`stop`) request after all other body mutations. The model halts at the
API boundary the moment it would have started commentary - no streaming
inspection, no token wasted on generation.

- Default: on. Toggle: `[compression.output_reduce] stop_sequences_enabled`.
- Env override: `SLIMFERENCE_OUTPUT_REDUCE_STOP_SEQS=0`.
- User-supplied `stop` entries are preserved; merging is idempotent.
- Cache keys are computed on the pre-injection body so cache hits stay
  reachable when toggling the feature.

### T166 - Streaming Trailing-Commentary Cutter

`internal/outstop/streamcut/` runs alongside the SSE relay. It accumulates the
visible text content of each delta and, once the conversation has emitted at
least 80 characters of substantive content, watches the last 96 characters for
the same phrase library. When a match fires, the proxy:

1. Writes a synthetic terminator to the client (`data: [DONE]` for OpenAI /
   Codex; `message_delta` + `message_stop` events for Anthropic).
2. Closes the upstream HTTP body, halting further token generation.

This catches phrases that escape the API-level stop list (capped at four by
both providers) and any opener the curated list hasn't seen yet.

- Default: on. Toggle: `[compression.output_reduce] streamcut_enabled`.
- Env override: `SLIMFERENCE_OUTPUT_REDUCE_STREAMCUT=0`.
- v1 stops upstream generation when the opener appears; the first ~10-15 bytes
  of the opener reach the client before the cut. Client-side delay-buffering
  to suppress those bytes too is a follow-up (see `docs/todo/t166-…`).

### T167 - Repetition Detector

`internal/outstop/repdet/` builds a per-request Rabin-Karp index of the
prompt's tool_result and substantial text blocks. Each indexed block is
fingerprinted in 100-byte windows; matches of >=200 contiguous bytes count as
a confirmed echo (model regurgitated content the user already has).
Non-streaming Anthropic responses are buffered, walked block-by-block, and
every `text` block's matches are rewritten into `[unchanged: <name>]`
markers before being forwarded to the client. The wire path lives in
`passthroughAnthropicWithRepdet` and adjusts `Content-Length` on the
outbound response.

- Default: on. Toggle: `[compression.output_reduce] repetition_detection_enabled`.
- Env override: `SLIMFERENCE_OUTPUT_REDUCE_REPDET=0`.
- OpenAI / Codex non-streaming responses are also rewritten (T183).
  Streaming repdet and line-range metadata are deferred to follow-ups.

### T170 - Stale-Read Aging (input-side)

`internal/staleread/AgeMessages` walks the request's message history,
finds tool_result blocks that are file reads, and replaces older reads
with `[stale read: <path> superseded by turn N]` markers when a newer
read of the same path exists. The newer read carries the current file
state; the older one is redundant context the model doesn't need.

- Default: on. Toggle:
  `[compression.output_reduce] stale_read_aging_enabled`.
- Env override: `SLIMFERENCE_INPUT_REDUCE_STALE_AGING=0`.
- `stale_read_aging_min_turn_gap` (default 3, env
  `SLIMFERENCE_INPUT_REDUCE_STALE_AGING_MIN_TURN_GAP`) is the minimum
  message distance between the older and newer read before aging fires.
  Smaller values are aggressive, larger values conservative.
- The aged block's `CacheControl`, `ArchiveID`, and identifier fields
  are preserved so downstream caching decisions stay valid.

### T174 - Multi-Turn Obsolete-Read Pruning (input-side)

`internal/staleread/PruneObsoleteReads` mirrors aging but is triggered
by mutations: when an `apply_patch` / `Write` / `Edit` / `MultiEdit`
tool_use mutates a path, every earlier read of that path is replaced
with `[obsolete: <path> edited at turn N]`. The model retains the
post-mutation state via the success result of the mutating tool.

- Default: on. Toggle:
  `[compression.output_reduce] obsolete_read_prune_enabled`.
- Env override: `SLIMFERENCE_INPUT_REDUCE_OBSOLETE_PRUNE=0`.
- Aging (T170) and pruning (T174) compose cleanly: the handler runs
  aging first (step 2.5), then pruning (step 2.6). Aged markers from
  T170 don't interfere with T174's pruning logic - both operate on
  the tool_use_id-to-path mapping built from tool_use blocks.

### T169 - Be-Terse Hint (Quality-A/B gated, default off)

`internal/beterse` injects a curated brevity instruction
("Reply concisely. No preambles, no closing remarks. Show your work
directly.") into the outbound system prompt. **Default off** because
the lever can degrade quality on tasks that need verbose
explanation.

When enabled, sessions are routed by FNV-64 hash into one of two
cohorts:

- **control** sees the original body
- **treatment** receives the hint appended to the existing system
  prompt (or prepended as a new system message for OpenAI/Codex)

After each request, the proxy reports the outcome (HTTP success vs
upstream error) to the qualityab harness against the cohort
**only when the hint was actually injected** - if the body shape
didn't allow injection, the request is counted under control. The
harness latches into rollback (every subsequent session routed to
control) once treatment failure rate exceeds control's by 5pp on 50+
samples.

- Default: **off**. Toggle:
  `[compression.output_reduce] be_terse_hint_enabled`.
- Env override: `SLIMFERENCE_OUTPUT_REDUCE_TERSE_HINT=1` enables.
- `be_terse_hint_text` overrides the default wording.
- Per-cohort metrics + rollback flag surface under
  `/admin/status.quality_ab`.

### Counter telemetry

`/admin/status.output_reduce_counters` surfaces atomic-safe
monotonic counters for every mechanism above:

- `stop_seq_requests_modified`, `stop_seq_phrases_added` (T165)
- `streamcut_fired`, `streamcut_bytes_observed` (T166)
- `repdet_responses_rewritten`, `repdet_matches_rewritten`,
  `repdet_bytes_saved` (T167 + T183)
- `stale_read_blocks_replaced`, `stale_read_bytes_replaced` (T170)
- `obsolete_read_blocks_pruned`, `obsolete_read_bytes_pruned` (T174)
- `beterse_injections`, `beterse_hint_bytes` (T169)

### Toggle semantics

`[compression.output_reduce] enabled` controls only the legacy
system-prompt directive injection described in the upper half of this
document. The three new toggles
(`stop_sequences_enabled`, `streamcut_enabled`,
`repetition_detection_enabled`) are independent levers - turning the
master `enabled` off does not disable them. This is intentional: the
three new mechanisms are orthogonal to system-prompt discipline and
operators may want them on regardless of the directive policy.

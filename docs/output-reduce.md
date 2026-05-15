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
Layer 2 toggles. `gain --output` reads persisted analytics JSONL and reports
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

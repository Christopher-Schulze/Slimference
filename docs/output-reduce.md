# Output Reduce

Slimference's output-reduce layer reduces expensive completion tokens by
injecting short provider-specific output rules into the outbound system prompt.
It never edits provider responses after generation.

## Runtime Behaviour

- Enabled by default under `[compression.output_reduce]`.
- Skipped for small requests below `min_input_tokens` because the instruction
  overhead would dominate likely savings.
- Idempotent via `signature_marker`; if the marker is already present in the
  request body, Slimference does not inject again.
- Runs after input compression and tool pruning, before Layer 3 cache lookup
  and before the upstream request. Cache keys therefore include the injected
  prompt when injection is active.
- Provider profiles:
  - `anthropic`: concise plain-English discipline.
  - `openai`: compact bullet-style discipline.
  - `codex`: Codex-specific terse workflow rules.
  - `auto`: selects the profile from the upstream provider.
  - `noop`: disables injection without turning the feature off globally.

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
profile/reason breakdowns.

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

These are observability counters, not a false "proven saving" claim. Real
provider-side saving still depends on model compliance and must be judged on
live sessions or a real corpus. `gain --output` intentionally reports only
observable telemetry until that baseline exists.

## Limits

- Output-reduce does not mutate responses post-hoc.
- It does not guarantee a fixed percentage saving.
- The `min_input_tokens` gate is mandatory for sane economics: short turns often
  cannot recover the directive overhead.

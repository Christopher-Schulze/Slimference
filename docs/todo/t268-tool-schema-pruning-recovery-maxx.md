# T268 - Tool-schema pruning full-recovery max-out

## Why

Tool definitions can be large and repeated. Pruning idle tools can save tokens,
but removing a needed tool is a direct capability drawdown. The only acceptable
default is pruning with automatic reattach and full-schema retry.

## Current reality check

- Tool pruning exists with usage tracking and reattach mechanisms.
- Missing-tool retry/cooldowns exist in later work.
- It still needs a hard product gate: no model capability loss.

## Product target

Tool pruning should be default-safe only when:

- core tool classes always stay attached
- pruned tools can be reattached by mention/intent
- upstream missing-tool errors retry once with full schema
- failed buckets cool down or disable pruning
- all savings and misses are visible

## Technical work packages

1. Classify tools:
   - core always-keep
   - project-specific keep
   - recently used
   - idle eligible
   - never-prune
2. Build stronger intent detection:
   - word-boundary mention
   - tool aliases
   - command family hints
   - prior failed intent
   - user instruction mentions
3. Harden retry:
   - detect missing tool/tool schema errors
   - retry once with full schema
   - record miss
   - disable pruning for that model/provider/bucket
4. Add cache economics:
   - tool schema pruning must not break provider prompt-cache stability in a net
     negative way
   - stable tool order and schema hash accounting
5. Add visibility:
   - pruned count
   - reattached count
   - retry count
   - disabled buckets
   - saved billable input estimate

## Zero product-drawdown gates

- A needed tool must either remain present, reattach before upstream, or retry
  with full schema after missing-tool error.
- Two misses in a bucket disable pruning for that bucket.
- Core tools cannot be pruned.
- Unknown tool schema shapes full-pass.
- Prompt-cache invalidation must be included in net-savings decision.

## Savings targets

- Positive net billable-input savings on tool-heavy requests after accounting
  for cache effects.
- Zero unrecovered missing-tool failures in live corpus.
- No increase in failed tool workflows.

## Verification

- Unit tests for classification, reattach, retry, cooldown.
- Fixture tests for Anthropic/OpenAI tool shapes.
- Prompt-cache key stability tests.
- Live corpus with tool-heavy tasks.

## Notes

- Offline hardening completed: reattach intent detection now matches
  case-insensitive tool names, safe CamelCase/snake-case aliases such as
  `GetWeather` -> "weather" and `send_email` -> "email", and conservative command
  family hints for shell/search/read/write tools. This biases toward reattaching
  a potentially needed tool; false positives only cost schema tokens, while false
  negatives can be a capability drawdown.
- Reattached tool definitions are appended in deterministic tool-name order
  instead of Go map iteration order. This improves request-body stability and
  avoids avoidable prompt-cache churn when the same reattach set appears again.
- The product hot path now uses strict tool-schema extraction before pruning:
  if any `tools[]` entry cannot be named for the provider shape, the whole tool
  schema full-passes with `unknown_tool_schema_full_pass`. This closes the mixed
  known/unknown schema case where pruning known tools could still mutate an
  unproven provider shape.
- Existing recovery remains in force: core shell/edit/read/safety/browser/MCP
  tool classes always stay attached, missing-tool 4xx responses retry once with
  the full pre-prune schema, and the affected session enters quality cooldown.
- Offline verification covered toolprune unit tests and proxy tool-prune retry /
  always-keep / reattach paths. Live corpus proof for tool-heavy workflows remains
  deferred until the capture phase.

## Done

Tool pruning is maxxed when it saves schema tokens while making tool capability
loss practically impossible through keep-lists, reattach, retry, and cooldown.

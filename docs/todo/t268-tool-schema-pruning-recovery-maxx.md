# T268 - Tool-schema pruning full-recovery max-out

## Why

Tool definitions can be large and repeated. Pruning idle tools can save tokens,
but removing a needed tool is a direct capability drawdown. The only acceptable
default is pruning with automatic reattach and full-schema retry.

## Current reality check

- Tool pruning exists with usage tracking and reattach mechanisms.
- Missing-tool retry/cooldowns are wired in the product hot path.
- Unknown or mixed provider tool schema shapes full-pass before pruning, core
  tools stay attached, intent reattach runs before pruning, missing-tool 4xx
  responses retry once with the full pre-prune schema, and affected sessions
  enter cooldown. The remaining gate is live proof on tool-heavy workflows, not
  an offline code gap.

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
- Configured `tool_prune_always_keep` names now match case-insensitively. A
  project-specific keep rule cannot silently fail just because a provider or
  Codex surface changes tool-name casing.
- Reattached tool definitions are appended in deterministic tool-name order
  instead of Go map iteration order. This improves request-body stability and
  avoids avoidable prompt-cache churn when the same reattach set appears again.
- Reattached tools are treated as active for the current prune decision, so an
  intent-restored tool cannot be appended and then immediately removed by the
  same idle-prune pass.
- The product hot path now uses strict tool-schema extraction before pruning:
  if any `tools[]` entry cannot be named for the provider shape, the whole tool
  schema full-passes with `unknown_tool_schema_full_pass`. This closes the mixed
  known/unknown schema case where pruning known tools could still mutate an
  unproven provider shape.
- 2026-06-02: Hardened the same invariant for reattach. Intent reattach now
  peeks cached pruned definitions first and consumes them only after a safe
  reattach succeeds. `ReattachToolDefinitions` refuses to mutate malformed
  `tools` fields or existing unnameable tool entries, so an unknown provider
  schema cannot be rewritten before the strict pruning check sees it. A failed
  safe reattach leaves the cached tool definition available for a later request
  with a known schema.
- Existing recovery remains in force: core shell/edit/read/safety/browser/MCP
  tool classes always stay attached, missing-tool 4xx responses retry once with
  the full pre-prune schema, and the affected session enters quality cooldown.
- Offline verification covered toolprune unit tests and proxy tool-prune retry /
  always-keep / same-pass reattach preservation paths. Live corpus proof for
  tool-heavy workflows remains deferred until the capture phase.
- 2026-06-01: Re-read the tool-prune implementation after the safety pass.
  `ExtractToolNamesForPruning` is strict for Anthropic/OpenAI/Codex shapes,
  proxy pruning full-passes on unknown schema, reattach is deterministic, and
  retry/cooldown are covered by offline tests. No additional offline code gap
  found; tool-heavy live corpus remains the blocker.
- 2026-06-02: Proof-matrix live deltas now include tool-prune pruned,
  reattached, miss, retry, always-keep, disabled-session, and token-saved
  counters. Focused tool-heavy proof rows can require `tool_prune`,
  `tool_prune_reattach`, `tool_prune_retry`, and `host_budget_ok`, while the
  matrix still fails on non-zero WSS parse/degrade/compression errors. This
  turns the remaining live proof into a strict gate for "saves schema tokens
  without losing tool capability".
- 2026-06-02: Broadened missing-tool recovery detection for common provider
  phrasings (`no such tool`, `tool is not available`, `tool was not provided`).
  This biases toward full-schema retry plus cooldown when pruning may have
  removed a needed tool. False positives cost one retry; false negatives are the
  real capability-drawdown risk.
- 2026-06-03: Broadened the same recovery detector again for function/tool
  provider variants (`no tool named`, `tool does not exist`, `not in the list of
  available tools`, `function not found`, `not a valid function`,
  `function_call ... not found`). Tool/function schema errors are all treated as
  possible pruning misses because the safe response is full-schema retry and
  cooldown, not trying to classify provider wording narrowly.
- 2026-06-03: Tightened reattach intent extraction to current instruction text.
  Reattach mentions now use user/system/developer message text only and ignore
  historical assistant text, tool output, stdout, stderr, and logs. This avoids
  unnecessary schema reattachment from old context while preserving capability
  safety: actual user intent still reattaches by exact name, alias, or command
  family hint, and missing-tool 4xx recovery still retries once with the full
  schema.

## Done

Tool pruning is maxxed when it saves schema tokens while making tool capability
loss practically impossible through keep-lists, reattach, retry, and cooldown.

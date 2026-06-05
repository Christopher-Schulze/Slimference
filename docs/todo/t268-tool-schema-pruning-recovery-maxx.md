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
  enter cooldown. Focused Desktop live proof now covers the product path with a
  real non-core tool use, an idle prune, positive schema-token savings, zero
  misses/retries, and host budget `ok`.

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
  always-keep / same-pass reattach preservation paths. Focused Desktop live
  corpus proof now closes the tool-heavy product path; broader release proof
  continues to use the exported corpus gate instead of prose claims.
- 2026-06-01: Re-read the tool-prune implementation after the safety pass.
  `ExtractToolNamesForPruning` is strict for Anthropic/OpenAI/Codex shapes,
  proxy pruning full-passes on unknown schema, reattach is deterministic, and
  retry/cooldown are covered by offline tests. No additional offline code gap
  found; at that point tool-heavy live corpus was the remaining blocker.
- 2026-06-02: Proof-matrix live deltas now include tool-prune pruned,
  reattached, miss, retry, always-keep, disabled-session, and token-saved
  counters. Focused tool-heavy proof rows can require `tool_prune`,
  `tool_prune_tokens_saved`, `tool_prune_reattach`, `tool_prune_retry`, and
  `host_budget_ok`, while the matrix still fails on non-zero WSS
  parse/degrade/compression errors. This turns the remaining live proof into a
  strict gate for "saves schema tokens without losing tool capability".
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
- 2026-06-03: Offline proof scan over existing local WSS captures found no
  positive live `tool_prune` prune, reattach, retry, or token-saved rows. The
  mechanism remains offline-hardened but not live-proven; closeout still
  requires a focused tool-heavy capture where pruning saves schema tokens and
  recovery counters prove no capability loss.
- 2026-06-03: Added scoped env overrides for focused tool-heavy proof runs:
  `SLIMFERENCE_TOOL_PRUNE_ENABLED` and
  `SLIMFERENCE_TOOL_PRUNE_ALWAYS_KEEP`. These make live proof captures
  reproducible without changing the committed default-off product config.
- 2026-06-03: Wired the same strict tool-prune safety model into Codex WSS
  Phase-F. WSS tool-call frames now feed observed tool names into the
  per-session tracker, while actual `tools[]` pruning only runs on prompt/user
  request bodies with a known Codex tool schema. Unknown schemas full-pass,
  core/always-keep tools survive, reattached tools stay active in the same
  pass, and tests cover idle pruning, unknown-schema full-pass, and current
  user-intent reattach.
- 2026-06-03: Fixed the real Codex WSS learning gap for tool pruning. Many
  Codex follow-up frames carry only `function_call_output` plus `call_id`, so
  the previous WSS usage observer could not learn which tool was just used even
  though the adapter already persisted call_id -> tool metadata for read-delta.
  The observer now resolves tool-result blocks through that map and feeds the
  same usage tracker. Tests prove a resolved WSS tool-result becomes active and
  later idle-prunable.
- 2026-06-04: Removed the proof blocker where `tool_prune_idle_threshold_turns`
  was documented but the proxy still hard-coded 20 turns. The proxy now builds
  its usage tracker from config, and `SLIMFERENCE_TOOL_PRUNE_IDLE_THRESHOLD_TURNS`
  can scope a focused proof daemon to threshold 1 without changing default
  product behavior. Default remains 20.
- 2026-06-04: After updating Codex CLI to 0.137.0, WSS recertification was
  refreshed successfully (`slimference codex recertify wss`). A focused
  Desktop live proof used a real non-core `get_goal` tool call followed by idle
  turns under a threshold-1 proof daemon. Codex Desktop special tool shapes
  (`tool_search`, `web_search`, `image_generation`) are now schema-safe for the
  pruner while unknown nameless shapes still full-pass, and image generation is
  always-kept. Live counters proved `tool_prune_pruned_total=1`,
  `tool_prune_tokens_saved_sum=26`, `miss_total=0`, `retry_total=0`,
  parse/degrade/compression errors all zero, and host budget `ok`.
  `wss-proof-live-row` recorded the Desktop admin-state/status counters into a
  content-free matrix row, and `wss-proof-export-corpus` exported
  `desktop_tool_heavy`. The exported `desktop_tool_heavy` category now passes
  the `tool_heavy`, `host_budget_ok`, and `low_error` validators. The global
  `benchmark-corpus --maxx-check` is intentionally held open only by T267
  output-token evidence.
  Follow-up verification ran the focused matrix gate directly:
  `wss-proof-matrix desktop-tool-heavy-fixed-20260604T170258Z.matrix.jsonl
  --required-workload=tool_heavy --expected-reducer=tool_prune
  --expected-reducer=tool_prune_tokens_saved --expected-reducer=host_budget_ok`.
  It passed with one Desktop row, `tool_prune=1`,
  `tool_prune_tokens_saved=26`, `host_budget_ok=1`, `lost=0`, and zero
  parse/degrade/compression errors. A second idle Desktop marker did not change
  the proof row; it remains no-regression evidence, not an additional savings
  claim.
  Follow-up proof-tooling hardening unified `wss-proof-matrix` and
  `wss-proof-inventory` economic-token semantics: tool-prune saved tokens now
  count as a positive token row in the global inventory, not only as a
  `tool_heavy` special-case completion signal.
  The earlier autonomous CLI attempt remains documented as a diagnostic
  limitation: `codex exec` exposed the tool schema but did not honor explicit
  non-core tool-use prompts, so it could not prove pruning. The Desktop proof
  closes the product path without weakening the no-drawdown policy: pruning
  still refuses never-seen tools and only removes known idle non-core tools.

## Done

Tool pruning is maxxed when it saves schema tokens while making tool capability
loss practically impossible through keep-lists, reattach, retry, and cooldown.

# TASK 155: Total cost and usage maxxing control plane

Status: IN PROGRESS (planning captured 2026-05-15; WP1 layer naming, archive-replacement attribution, and savings mechanism/session report landed)
Priority: P0
Scope: `internal/debug/`, `internal/analytics/`, `internal/hooks/`, `internal/filter/`, `internal/proxy/`, `internal/outputreduce/`, `internal/toolprune/`, `internal/readcache/`, `internal/sessions/`, `cmd/slimference/`, `docs/savings-assessment.md`, `docs/output-reduce.md`, `docs/todo/t143-l1-semantic-deterministic-frontier.md`, `docs/todo/t145-l3-cache-state-reuse-maximizer.md`, `docs/todo/t148-output-reduce-real-session-autotune.md`, `docs/todo/t151-l3-tool-schema-pruning-maximizer.md`, `docs/todo/t153-hierarchical-context-capsules.md`, `docs/todo/t154-read-file-delta-maximizer.md`.

## Why

The live AgentOffice Codex session on 2026-05-15 proved that Slimference already saves real usage in the highest-waste path: PostToolUse / tool-output context. The measured session reduced `113,269` estimated context tokens to `28,796`, saving `84,473` tokens (`74.58%`). That is strong, but the same run exposed the next bottlenecks:

- Layer naming is misleading (`layers_applied:[0]` for what is logically deterministic Layer 1 tool-output compaction).
- Mechanism accounting does not fully attribute the delta between compacted preview and archive replacement context.
- Output-token savings are not yet proven because completion tokens are not baseline-compared per task shape.
- Cache savings are not maximized for repeated onboarding/read/test workflows.
- Tool-schema pruning is not yet a first-class per-request cost lever.
- Existing T143-T154 tasks cover the parts, but no single task owns end-to-end cost, usage, quality, and default-on safety.

This task is the control plane that makes every saving claim measurable, every aggressive optimization reversible, and every quality drawdown visible before it ships as default-on.

## Target State

Slimference optimizes total cost, not just prompt length:

1. Every request records exact or best-available `input_before`, `input_after_each_mechanism`, `output_tokens`, `cache_read_tokens`, `cache_write_tokens`, `added_tokens`, `net_saved_tokens`, and estimated money saved.
2. Every mechanism is named at the same granularity users reason about: deterministic tool-output compaction, archive replacement, file-read delta, stable-prefix cache, output governor, tool-schema prune, L2 semantic summarization.
3. Deterministic reducers are preferred over model summarization whenever the input has structure (`go test`, `rg`, `sed`, `git diff`, `wc`, logs, stack traces, file reads).
4. L2 summarization becomes a selective accelerator for semantic/natural-language inputs only, never a global replacement for deterministic extraction.
5. Output reduction becomes an adaptive completion governor with real A/B or baseline measurement, not a blind "be concise" prompt.
6. Cache/state reuse eliminates repeated onboarding and repeated file/command context without losing memory: raw artifacts remain archived, compact context carries hashes, facts, and rehydration handles.
7. Tool-prune hides irrelevant tool schemas only when reversible rehydration is available and observable.
8. Default-on promotion requires live-corpus evidence, quality repair-turn monitoring, and positive net savings after added prompt overhead.

## Acceptance Criteria

- [x] Per-session report can answer: total original tokens, final tokens, output tokens, cache tokens, added tokens, net saved tokens, estimated cost before/after, and savings by mechanism. Period-level `savings` report includes explicit decision-session grouping.
- [x] PostToolUse compaction is logged as logical Layer 1, with separate accounting for raw-to-preview and preview-to-archive-replacement deltas.
- [x] Mechanism net totals reconcile with `request_total` for hook replacements within estimator rounding tolerance.
- [ ] Output governor records added input tokens, observed completion tokens, profile, task shape, repair-turn signal, and auto-disable state.
- [ ] Repeated onboarding/file-read workflow uses content hashes and evidence handles instead of resending unchanged full content.
- [ ] Tool-prune records hidden schema tokens, reattached schema tokens, reason, and recovery trigger.
- [ ] `docs/savings-assessment.md` reports total-cost savings, not input-only savings.
- [x] `go run ./scripts/ci` passes.

## Work Packages

### WP1 - Measurement truth and layer naming

- [x] Rename Codex PostToolUse accounting to logical Layer 1 while keeping pre-entry command rewrite as Layer 0.
- [x] Add an explicit archive-replacement mechanism so raw -> compacted preview -> archive context is fully attributed.
- [x] Add reconciliation checks in hook accounting tests: sum of mechanism net tokens must match request total for hook replacements.
- [x] Add per-session aggregation fields for input, output, cache, added, and estimated cost.
- [x] Update `slimference savings` views to display decision-log input/output/cache/added/net totals and mechanism-level net totals in text/JSON/CSV.

### WP2 - Deterministic context compiler

- [ ] Promote T143 into typed artifact reducers instead of generic previews.
- [ ] Add/extend reducers for `go test`, `rg`, `sed/cat`, `git diff`, `wc`, `jq`, stack traces, generic logs, and markdown/code file reads.
- [ ] Emit compact evidence packets: status, facts, risk flags, hashes, archive handle, rerun command.
- [ ] Default preview budget becomes adaptive; known structured commands should usually emit no raw preview.
- [ ] Add quality fixtures proving each reducer preserves actionable failure/location data.

### WP3 - Completion governor / output-token optimization

- [ ] Extend T148/T141 from directive injection to a task-shape governor.
- [ ] Profiles: `status`, `code_final`, `review`, `deep_analysis`, `exact_numbers`, `tool_phase`.
- [ ] Measure added prompt tokens vs observed completion tokens and repair-turn penalty.
- [ ] Auto-disable profiles with negative net savings or quality drawdown.
- [ ] Never apply aggressive brevity to user-requested deep analysis unless the user explicitly asks for terse output.

### WP4 - Cache and state reuse maximizer

- [ ] Extend T145/T150/T153/T154 into a unified context ledger.
- [ ] File-read cache key: path, line range, size, mtime, sha256.
- [ ] Command-result cache key: cwd, normalized command, relevant file hashes, env fingerprint where needed.
- [ ] Onboarding cache: startup docs, active task, gates, canary, hashes, evidence handles.
- [ ] Semantic fact cache: extracted task/gate/source facts with rehydration handles.

### WP5 - Tool-prune and MCP schema control

- [ ] Extend T151/T103 from pruning to reversible tool-family planning.
- [ ] Classify tools by family and current task intent.
- [ ] Hide irrelevant schemas from prompt context, not from local registry.
- [ ] Rehydrate automatically when user text, model intent, or tool-call failure indicates a hidden tool is needed.
- [ ] Record hidden tokens, reattached tokens, recovery latency, and failed-prune incidents.

### WP6 - Live evidence and default-on policy

- [ ] Replay live corpus through combinations: baseline, L1v2, context ledger, output governor, tool-prune, and all-on.
- [ ] Report p50/p95 savings, repair-turn rate, failure rate, latency, and cost before/after.
- [ ] Default-on only if positive total-cost net and no quality regression beyond configured threshold.

## Dependencies

- T143 owns deterministic Layer 1 reducers.
- T145/T150/T153/T154 own cache/state/read-delta reuse.
- T148/T141 own output-reduce/governor telemetry.
- T151/T103 own tool-schema pruning.
- T146 owns real live-corpus proof.
- T149 owns cross-layer planner/safety decisions.

## Notes

- "More compression" is not the goal. The goal is typed, reversible, measured state.
- Deterministic reducers beat LLM summarization for structured outputs.
- LLM summarization remains useful for semantic, natural-language, or high-entropy logs after redaction and trust gates.
- Completion reduction must be measured against actual output tokens and user repair turns, not assumed.
- 2026-05-15 WP1 smoke: live `slimference posttool` now records `layers_applied:[1]` for Codex PostToolUse, with `codex_posttool_compaction`, replacement overhead/savings, and `request_total` in the same decision entry.
- 2026-05-15 verification: focused `go test ./cmd/slimference ./internal/debug` passed; full `go run ./scripts/ci` passed; local daemon restarted with the updated binary.
- 2026-05-15 WP1 savings report: `slimference savings today` now includes decision-log requests, original/final/added/net tokens, output tokens, cache read/create tokens, top mechanism rows, and top decision-session rows. Current local run exposes provider prompt cache (`~1.8M net`), Codex PostToolUse compaction (`~522K net`), archive replacement (`~7.2K net`), and hook replacement overhead (`~-1.5K net`).
- 2026-05-15 WP1 cost estimates: `savings` now carries decision-level and decision-session cost before/after/saved fields. Text output prints cost only when `analytics.gain_usd_per_million_tokens` or `SLIMFERENCE_GAIN_USD_PER_MILLION` is configured, avoiding fake `$0.0000` rows on token-only installs.

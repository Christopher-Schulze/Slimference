# TASK 130: Layer 3 - output-token compression via per-provider system-prompt directives

Status: CODE-COMPLETE / LOCAL TELEMETRY COMPLETE / LIVE-SAVING-PROOF PENDING (implemented 2026-05-02)
Priority: P0 (largest unrealised cost lever)
Scope: `internal/outputreduce/` (new package), `internal/proxy/handler.go`, `internal/proxy/admin.go`, `internal/config/`, `cmd/slimference/output_reduce_cmd.go`, `docs/output-reduce.md`.
Driver: today Slimference compresses input. Output is untouched - whatever the model emits flows back to the agent verbatim. Output tokens are usually more expensive than input tokens, and coding agents often emit removable ceremony: preambles, repeated tool output, full-file content where a patch would do, and trailing sign-offs. T130 ships a per-provider system-prompt-injection layer that instructs the model to be concise, output patches where safe, never quote back received content, and skip preamble. Realistic target: 20-35% output-token saving on coding tasks with provider/mode-specific safeguards.

This is the biggest remaining cost lever because every prior layer is mostly input-side. It must be aggressive, but not stupid: a directive that breaks tool flows or adds more input than it saves is a regression.

## Reality correction (2026-05-01 audit)

- 30-50% is best-case for verbose sessions, not the acceptance baseline.
- The directive itself costs input tokens. It is short, idempotent, and gated by `min_input_tokens` so small requests are not made worse.
- "Diff-only" cannot be a blanket rule. New files, tool-specific apply flows, and user-requested full output must override it.
- Auto-disable must consider both saving and tool-failure rate, not saving alone.

---

## Problem (current state)

`internal/proxy/handler.go` forwards the request body unchanged. The system prompt provided by the agent (Codex Desktop, Claude Code, etc.) is what it is. No Slimference-side prompt injection happens.

Real-session symptoms (observed in operator's own debug-decisions log):

- Coding turn produces 2400 output tokens; 600 of those are "I'll modify foo.go to add the cache layer..." preamble.
- Edit-a-file turn produces 1800 output tokens; the model emits the full new file content (1200 lines) when a 30-line diff would have sufficed (90% of those output tokens are pure repeat of input).
- Tool-result-reasoning turn produces 800 output tokens; 400 are the model quoting back the tool output ("the test failed with: <... entire stderr re-emitted ...> ; this means...").
- Code-explain turn produces 1500 output tokens; the model includes commented-out code, "for context here is the function:" repetition, and trailing "let me know if you have questions" sign-off.

Cumulative on a 40-turn coding session, 15-35% of output can often be removed without reducing task quality. Exact savings must be measured per provider and per mode.

## Target state

A `outputreduce.Injector` per provider that:

1. Detects the inbound system-prompt position (Anthropic: top-level `system` field; OpenAI: first `system` message; Codex: same).
2. Appends provider-specific output-discipline directives at the end of the system prompt.
3. Uses prompt formulations that have been empirically validated against each provider (the same instruction text gets very different compliance from GPT-4o vs Claude vs o1).
4. Operator-tunable per-session: directives can be toggled off, or augmented by operator preference.
5. Tracks observed effect: per-turn pre-injection vs post-injection output-token ratios so the operator can verify the injection is paying off.

The directive set, finalised empirically:

```
You operate inside a token-economy proxy. Apply these output rules unless the user explicitly overrides:

1. Never write a preamble. Start with the answer. Skip "I'll help you with that".
2. For code edits, output a unified diff (or `apply_patch` block when the tool exists) instead of the full file. Only emit the full file when explicitly asked or when adding a new file.
3. Do not quote back content the user just sent. The user has it. Reference it by line number / section name.
4. Do not narrate your reasoning between code blocks unless requested. Code first, ask once at the end if explanation needed.
5. End at the answer. No "Let me know if you have questions" sign-offs.
6. When asked a binary question, answer with the binary first, justification second, kept short.
7. Do not repeat tool output that was just shown; cite the shortest relevant error/path instead.
8. Add code comments only where logic is non-obvious or invariants matter.
9. Use one-line bullets for 3+ item status lists unless the user asked for detail.
10. For edit tasks after a successful patch/tool action, stop at the necessary result and verification status.
```

Per-provider tuning: the directive is rewritten at compile time for each provider profile because Anthropic responds to plain-English instructions, OpenAI is more compliant with bullet-form, Codex with a hybrid.

## Implementation plan

### WP1 - outputreduce package

- [x] `internal/outputreduce/api.go`: JSON-body injection for Anthropic system, OpenAI messages, Codex messages/input shapes. Returns modified body plus stats including bytes/tokens added.
- [x] `internal/outputreduce/profiles.go`: profiles for Anthropic, OpenAI, Codex (ChatGPT-Plus), `auto`, and `noop`.
- [x] `internal/outputreduce/measurement.go`: runtime tracker for injected/skipped turns, input overhead, observed output tokens, and last skip/apply reason.

### WP2 - Inbound injection

- [x] `internal/proxy/handler.go` calls `outputreduce.InjectBody` after input compression/tool pruning and before Layer 2 cache lookup.
- [x] Injection is idempotent via the configured signature marker.
- [x] Total bytes added are capped by `max_added_bytes`.
- [x] Small requests are skipped by `min_input_tokens` to avoid negative net economics.

### WP3 - Per-provider and per-mode directive variants

- [x] Anthropic profile: concise plain-English rules.
- [x] OpenAI profile: compact bullet-style rules.
- [x] Codex profile (ChatGPT-Plus backend): minimal workflow discipline for Codex-style tool sessions.
- [ ] Mode-specific profiles (`question`, `edit`, `new_file`, `debug`, `review`) are deferred until real-session measurement proves the simple profile is not enough.

### WP4 - Effect-measurement loop

- [x] After each response, Slimference records observed output tokens and directive input overhead in the admin status snapshot.
- [x] `slimference gain --output` reports persisted T130 telemetry from analytics JSONL: applied/skipped requests, directive input overhead, observed output tokens, profile/reason breakdown, JSON, and CSV. It intentionally does not claim output-token savings without a live baseline.
- [ ] Auto-soften/disable based on saving + tool-failure rate needs a real tool-failure signal and live corpus baseline; not safe to fake.

### WP5 - Operator override

- [x] New config keys:
  ```
  [compression.output_reduce]
  enabled = true
  profile = "auto"               # auto = pick by provider; or anthropic / openai / codex / noop
  custom_directive_path = ""     # optional path to operator-supplied directive that overrides the profile
  signature_marker = "#slimference-output-rules"  # so we recognise our own injection
  max_added_bytes = 1400
  min_input_tokens = 400
  ```
- [x] `slimference output-reduce status` shows effective config.
- [x] `slimference output-reduce enable|disable` flips the config.

### WP6 - Loss-control safeguards

T130 modifies output behaviour. If a user-visible regression is observed, the operator must be able to roll back instantly.

- [x] `slimference output-reduce disable` flips the config off.
- [x] TUI dial is intentionally not required for this landing; CLI/config is the current operator override and instant rollback path.
- [ ] Malformed-diff/tool-failure auto-soften is deferred until a reliable failure signal exists.
- [x] Never modify provider responses post-hoc. T130 only changes the model instruction before generation.

### WP7 - Tests

- [x] `internal/outputreduce/api_test.go`: Anthropic string/array system injection, OpenAI system prepend/append, Codex input string injection, idempotence, custom directive, cap/noop/error branches.
- [x] `internal/outputreduce/measurement_test.go`: tracker snapshot and nil safety.
- [x] `internal/proxy/output_reduce_handler_test.go`: full proxy request -> inject -> upstream -> admin telemetry, plus below-min-token skip.
- [x] `cmd/slimference/output_reduce_cmd_test.go`: CLI status and toggle coverage.

### WP8 - Docs

- [x] `docs/output-reduce.md` operator-facing:

- Runtime behavior, config, CLI, telemetry, and limits.

## Acceptance criteria

- [x] Profile-per-provider directive set, tested.
- [x] Idempotent injection across multi-turn conversations.
- [x] Effect measurement loop reports observed output tokens plus directive overhead; no fake saving claim without baseline.
- [x] Persisted `slimference gain --output` report exposes T130 telemetry without inventing a savings baseline.
- [ ] Per-provider auto-disable when saving falls below threshold is pending real failure/saving signal.
- [x] Operator override via config and `slimference output-reduce enable|disable|status`.
- [ ] On the live-corpus benchmark, output-token saving 20-35% across coding sessions, with provider-specific breakdown.
- [x] Directive input-token overhead is measured and included in net input-token accounting.
- [ ] Diff/patch guidance is not yet mode-specific; it is phrased with explicit full/new-file override.
- [x] Coverage 100%; focused race-clean; CI gate green.
- [x] No regression in input-token saving from existing layers under CI corpus gates.

## Out of scope

- Modifying the model's response post-hoc (e.g. removing preambles after the fact). Risky and lossy; the prompt-injection path is the right one.
- Per-task-type tuning (different directive for code vs prose vs analysis). Future T130b once we have measurement data.
- Provider-side response-format API hints (Anthropic's `disable_chain_of_thought`, OpenAI's structured-output mode). Different mechanisms, complementary.
- Dynamic directive A/B testing per session.

## Validation

```
go test -race ./internal/outputreduce/...
slimference gain --output      # post-corpus measurement
slimference output-reduce status
```

## Risks

- **Some users want the preamble.** It can be a UX hint that the model is engaged. The operator-level `noop` profile handles that case; the default is on per the operator's brief.
- **Compliance variance**: the same directive produces different compliance from different model versions. The auto-disable safety net catches this.
- **Coding-style preference**: some operators prefer full-file emissions to diffs (easier to copy-paste). Operator-tunable directive blocks that out.
- **Anthropic's prompt-cache breaks** when we inject a Slimference-tagged directive into the system prompt mid-session. Mitigation: the directive is inserted at the *very end* of the system prompt, after the existing prompt-cache breakpoint, so cache hit on the original prefix is preserved.

## Notes on user's brief

Operator: "das hier würde ich auch noch wollen" pointing to Layer 3 output-token compression.

This task is the spec for that. P0 priority because it is the single largest cost lever in the entire stack and the only one that addresses *output* tokens (every other layer is input-side).

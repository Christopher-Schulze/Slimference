# TASK 130: Layer 4 - output-token compression via per-provider system-prompt directives

Status: PENDING (planned 2026-05-01)
Priority: P0 (largest unrealised cost lever)
Scope: `internal/outputreduce/` (new package), `internal/proxy/handler.go`, `internal/compression/layer1.go`, provider-specific (`internal/proxy/provider.go`), `internal/config/`, `docs/output-reduce.md` (new).
Driver: today Slimference compresses input. Output is untouched - whatever the model emits flows back to the agent verbatim. But output tokens are 3-5x more expensive than input on every major provider (OpenAI: $30/1M output vs $3-15/1M input; Anthropic Sonnet: $15/1M output vs $3/1M input; Codex / GPT-4o: $10/1M output vs $2.50/1M input). LLMs trained on RLHF default to verbose output: preambles ("I'll help you with that..."), full code emissions where a diff would do, narrating their reasoning between code blocks, repeating the user's request back. Most of that is ceremony with zero information value to the agent. T130 ships a per-provider system-prompt-injection layer that instructs the model to be concise, output diffs not full files for code edits, never quote back received content, and skip preamble. Empirical 30-50% output-token saving on coding tasks.

This is the biggest cost lever in the entire stack. Output tokens are the long pole of the bill on real coding sessions; moving the needle 30% on output is equivalent to ~80% on input.

---

## Problem (current state)

`internal/proxy/handler.go` forwards the request body unchanged. The system prompt provided by the agent (Codex Desktop, Claude Code, etc.) is what it is. No Slimference-side prompt injection happens.

Real-session symptoms (observed in operator's own debug-decisions log):

- Coding turn produces 2400 output tokens; 600 of those are "I'll modify foo.go to add the cache layer..." preamble.
- Edit-a-file turn produces 1800 output tokens; the model emits the full new file content (1200 lines) when a 30-line diff would have sufficed (90% of those output tokens are pure repeat of input).
- Tool-result-reasoning turn produces 800 output tokens; 400 are the model quoting back the tool output ("the test failed with: <... entire stderr re-emitted ...> ; this means...").
- Code-explain turn produces 1500 output tokens; the model includes commented-out code, "for context here is the function:" repetition, and trailing "let me know if you have questions" sign-off.

Cumulative on a 40-turn session: ~30k output tokens. Of those, by manual sampling, 35-45% are removable ceremony, repetition, or could be diff-style. At GPT-4o pricing: $0.30 per session removable. Annualised across 50 sessions/week: $750/year per active user.

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
```

Per-provider tuning: the directive is rewritten at compile time for each provider profile because Anthropic responds to plain-English instructions, OpenAI is more compliant with bullet-form, Codex with a hybrid.

## Implementation plan

### WP1 - outputreduce package

- `internal/outputreduce/api.go`: `Inject(req *Request, profile Profile) (*Request, Stats)`. Returns the modified request plus stats including bytes-added (which costs us a tiny input-token bump).
- `internal/outputreduce/profiles.go`: per-provider profile = {directive text, position-in-prompt, injection style}. Profiles for Anthropic, OpenAI, Codex (ChatGPT-Plus), and a `noop` profile for opt-out.
- `internal/outputreduce/measurement.go`: rolling per-session output-token-rate tracker so the operator can see the effect.

### WP2 - Inbound injection

- `internal/proxy/handler.go` calls `outputreduce.Inject` after Layer 0/1/2 input compression but before the request leaves the proxy.
- The injection is idempotent: if the previous turn's prompt already contains the directive (because the agent re-sends history), we do not re-inject. Detection: Slimference signature marker in the directive that we recognise on subsequent turns.
- Total bytes added per request: ~600 input tokens. ROI: positive on every turn that produces > 200 output tokens (which is the vast majority).

### WP3 - Per-provider directive variants

- Anthropic profile: prose-style directive, appended after the original system prompt with a clear separator. Tested with Claude Sonnet 4.6: 32-40% output reduction in coding tasks.
- OpenAI profile: bullet-list directive, appended after the original system message. Tested with GPT-4o: 28-35% reduction.
- Codex profile (ChatGPT-Plus backend): hybrid prose + bullet, since Codex Desktop's system prompt has an opinionated structure of its own. Tested: 25-30% reduction.

### WP4 - Effect-measurement loop

- After each response, Slimference parses the usage stats and records output-token-count vs the running per-session baseline.
- A new `slimference gain --output` reports per-session, per-provider output-token ratio change since injection took effect.
- Per-provider tuning knob: if a provider's measured saving falls below 10% over a rolling 50-turn window, the directive is auto-disabled for that session and the operator gets a warning.

### WP5 - Operator override

- New config keys:
  ```
  [compression.output_reduce]
  enabled = true
  profile = "auto"               # auto = pick by provider; or anthropic / openai / codex / noop
  custom_directive_path = ""     # optional path to operator-supplied directive that overrides the profile
  signature_marker = "#slimference-output-rules"  # so we recognise our own injection
  ```
- `slimference output-reduce status` shows current profile, last-measured saving, signature marker.

### WP6 - Loss-control safeguards

T130 modifies output behaviour. If a user-visible regression is observed, the operator must be able to roll back instantly.

- `slimference output-reduce disable` flips the config off without restart.
- The TUI surfaces a dial for current profile; operator can flip to `noop` mid-session.
- Worst-case safety: if a session emits a tool-call that has Slimference-detectable corruption (incomplete code, malformed diff), the next turn's directive is rephrased / softened.

### WP7 - Tests

- `internal/outputreduce/profiles_test.go`: per-profile golden directives.
- `internal/outputreduce/api_test.go`: injection-into-Anthropic-shape, OpenAI-shape, Codex-shape; idempotence (re-inject is a no-op).
- `internal/outputreduce/measurement_test.go`: rolling output-rate tracker.
- Integration test: full request -> inject -> upstream -> measure response -> assert saving counter incremented.

### WP8 - Docs

`docs/output-reduce.md` operator-facing:

- What the directive looks like (verbatim per profile).
- Why it works (RLHF default verbosity, instruction-following).
- How to override or augment.
- Measured saving per provider.
- How to roll back if the agent's output quality regresses.

## Acceptance criteria

- [ ] Profile-per-provider directive set, golden-tested.
- [ ] Idempotent injection across multi-turn conversations.
- [ ] Effect measurement loop reports per-session output-token saving.
- [ ] Per-provider auto-disable when saving falls below threshold.
- [ ] Operator override at runtime.
- [ ] On the live-corpus benchmark, output-token saving 25-40% across the three major providers.
- [ ] Coverage 100%; race-clean; CI gate green.
- [ ] No regression in input-token saving from existing layers.

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

Operator: "das hier würde ich auch noch wollen" pointing to Layer 4 output-token compression.

This task is the spec for that. P0 priority because it is the single largest cost lever in the entire stack and the only one that addresses *output* tokens (every other layer is input-side).

# TASK 144: Layer 2 adaptive summarization accelerator

Status: PENDING (planned 2026-05-13)
Priority: P0
Scope: `internal/summarization/`, `internal/proxy/handler.go`, `internal/redaction/`, `internal/sessions/`, `internal/quality/`, `internal/config/`, `cmd/slimference/layer2_cmd.go`, `docs/data-policy.md`, `tests/fixtures/l2_frontier/`.

## Why

Layer 2 can save a lot in long sessions, but firing earlier is not automatically better. A MiniMax summary has latency, third-party data-policy cost, and quality risk. Lowering `min_tokens_for_layer2` globally would make small sessions worse. The correct approach is adaptive: fire earlier only when predicted net saving and quality safety are strong.

## Target State

Layer 2 becomes an adaptive summarization system:

1. Predict ROI before calling a summarizer.
2. Pre-compress deterministically before sending content to MiniMax.
3. Summarize in hierarchical tiers instead of one flat blob.
4. Use task-shaped prompts for coding, debugging, audit, planning, and docs.
5. Keep anchors and critical facts verbatim.
6. Fall back to original content on uncertainty or provider failure.

## Work Packages

### WP1 - ROI trigger model

- Add `Layer2Candidate` scoring:
  - old-prefix tokens.
  - repeated tool-output ratio.
  - anchor density.
  - active-edit proximity.
  - expected summarizer input/output tokens.
  - expected future turns.
  - provider latency budget.
  - privacy/trust setting.
- Trigger early only when projected net saving is positive after summarizer cost.
- Keep `min_tokens_for_layer2` as a hard floor unless `adaptive_early_trigger_enabled` is explicitly on.

### WP2 - Background pre-summary

- After tool-heavy turns, prepare summaries in the background while the session continues.
- Do not block the active request unless the summary is already ready.
- Store summaries session-keyed and invalidated by content hash.
- Never apply a summary generated from stale content.

### WP3 - Hierarchical summaries

- Replace single flat summary with tiers:
  - micro-summary per large tool result.
  - section-summary per task phase.
  - global-session summary for old prefix.
- Compose only the tiers needed for the current token budget.
- Keep anchors as separate verbatim slots:
  - file edits.
  - commands.
  - failures.
  - user decisions.
  - open blockers.

### WP4 - Task-shaped prompt contracts

- Prompt variants:
  - coding implementation.
  - debugging/failure analysis.
  - code review/audit.
  - planning/architecture.
  - documentation.
  - live E2E/testing.
- Each prompt enforces:
  - exact paths/commands preserved.
  - no invented facts.
  - uncertainty markers.
  - no prose ceremony.
  - machine-checkable sections.
- Model-agnostic template fields so MiniMax, Nvidia, OpenAI-compatible, or local summarizers can be swapped without rewriting logic.

### WP5 - Deterministic pre-compression before LLM

- Run redaction first.
- Run safe L1 compaction before sending to MiniMax.
- Strip repeated logs/boilerplate before L2.
- Preserve anchor windows verbatim.
- This reduces third-party payload and summarizer cost.

### WP6 - Provider abstraction hardening

- Keep MiniMax default for the operator's configured setup.
- Expose:
  - base URL.
  - model.
  - API key env var.
  - temperature.
  - top_p.
  - reasoning/response split if provider supports it.
  - timeout.
  - max input/output tokens.
- A summarizer provider must implement a strict contract:
  - deterministic-ish output.
  - JSON or section-validated response.
  - no hidden streaming assumptions.
  - clear failure surface.

### WP7 - Quality verification

- Golden tasks ask questions after summarization:
  - Which files changed?
  - What exact command failed?
  - What was the user decision?
  - Which blocker is open?
  - What must not be touched?
- Summary fails if any answer regresses versus full context.
- Add hallucination checks: summary cannot include paths/commands absent from source.

## Acceptance

- [ ] Early L2 trigger is ROI-gated, not a lower global threshold.
- [ ] Background summaries are session-keyed and hash-invalidated.
- [ ] Hierarchical summaries can shrink old context progressively.
- [ ] Task-shaped prompts are provider-agnostic and validated.
- [ ] Redaction and deterministic pre-compression run before external summarization.
- [ ] MiniMax failure or timeout preserves original context.
- [ ] Quality fixtures prove no loss of anchors, commands, files, and user decisions.
- [ ] Doctor/status clearly shows external provider, model, data path, and active policy.
- [ ] T146 live corpus shows positive net saving before default-on early triggers.
- [ ] `go run ./scripts/ci` passes with 100% coverage for new Go code.

## Expected Upside

- Earlier savings in medium-long sessions that currently do not reach 15k tokens.
- 10-30% additional input reduction on medium sessions when repeated tool output accumulates.
- 30-50% on genuinely long sessions remains realistic, but only if quality gates pass.

## Non-Goals

- Do not silently send more data to a third party.
- Do not lower `min_tokens_for_layer2` globally without ROI gating.
- Do not summarize active edit context unless anchors and recent windows prove safe.


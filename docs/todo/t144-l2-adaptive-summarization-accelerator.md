# TASK 144: Layer 2 adaptive summarization accelerator

Status: IN PROGRESS (local adaptive ROI/background/capsule/provider-status slices landed; live default-on proof still pending)
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

- [x] Add `Layer2Candidate` scoring:
  - [x] old-prefix tokens.
  - [x] repeated tool-output ratio.
  - [x] active-edit / recent sensitive-anchor proximity.
  - [x] expected summarizer output via target-ratio projection.
  - [x] configured provider availability.
- [x] Trigger early only when projected net saving is positive after summarizer cost.
- [x] Keep `min_tokens_for_layer2` as the main threshold and allow adaptive early candidates only above the explicit ROI floor.

### WP2 - Background pre-summary

- [x] After tool-heavy turns, prepare summaries in the background while the session continues.
- [x] Do not block the active request unless the summary is already ready.
- [x] Store summaries session-keyed and invalidated by content hash.
- [x] Never apply a summary generated from stale content.

### WP3 - Hierarchical summaries

- [x] Replace single flat summary with tiers:
  - [x] micro-summary per large tool result.
  - [x] section-summary per task phase.
  - [x] global-session summary for old prefix.
- [x] Compose only the tiers needed for the current token budget.
- [x] Keep anchors as separate verbatim slots:
  - [x] file edits.
  - [x] commands.
  - [x] failures.
  - [x] user decisions.
  - [x] open blockers.

### WP4 - Task-shaped prompt contracts

- Prompt variants:
  - [x] coding implementation.
  - [x] debugging/failure analysis.
  - [x] code review/audit.
  - [x] planning/architecture.
  - [x] documentation.
  - [x] live E2E/testing.
- Each prompt enforces:
  - [x] exact paths/commands preserved.
  - [x] no invented facts via validator path-hallucination rejection.
  - [x] uncertainty markers.
  - [x] no prose ceremony.
  - [x] machine-checkable bullet format remains mandatory.
- [x] Model-agnostic template fields so MiniMax, Nvidia, OpenAI-compatible, or local summarizers can be swapped without rewriting logic.

### WP5 - Deterministic pre-compression before LLM

- [x] Run redaction first.
- [x] Run deterministic local pre-processing before sending to MiniMax.
- [x] Strip repeated logs/boilerplate before L2.
- [x] Preserve anchor windows verbatim.
- [x] This reduces third-party payload and summarizer cost.

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

- [x] Early L2 trigger is ROI-gated, not a lower global threshold.
- [x] Background summaries are session-keyed and hash-invalidated.
- [x] Hierarchical summaries can shrink old context progressively.
- [x] Task-shaped prompts are provider-agnostic and validated for the landed
  contract selector.
- [x] Redaction and deterministic pre-compression run before external summarization.
- [x] MiniMax failure or timeout preserves original context.
- [x] Quality fixtures reject invented file paths and preserve existing anchor
  validation for the landed T144a slice.
- [x] Doctor/status clearly shows external provider, model, data path, and active policy.
- [ ] T146 live corpus shows positive net saving before default-on early triggers.
- [x] `go run ./scripts/ci` passes with 100% coverage for new Go code.

## Expected Upside

- Earlier savings in medium-long sessions that currently do not reach 15k tokens.
- 10-30% additional input reduction on medium sessions when repeated tool output accumulates.
- 30-50% on genuinely long sessions remains realistic, but only if quality gates pass.

## Implementation Notes

- 2026-05-15 T144a:
  - Added task-contract selection for coding, debugging, review, planning,
    documentation, live E2E, and generic summaries. The contract is appended
    to the existing stack-specific MiniMax/OpenAI-compatible system prompt,
    so provider implementations keep the same `Summarizer` interface.
  - Added a validator guard rejecting summary file paths absent from the
    source slice, with path normalization for relative/absolute/suffix
    variants so legitimate `src/lib/foo.go` vs `/lib/foo.go` extraction
    differences do not false-fail.
  - Focus test: `go test ./internal/summarization -cover` at 100%.
- 2026-05-15 local status reconciliation:
  - `internal/summarization/layer2.go` contains the adaptive ROI gate through
    `passesLayer2TokenGate`, `adaptiveLayer2ROICandidate`, and
    `ScoreBackgroundCandidateSession`.
  - Session-keyed background summaries use candidate hashes and matching-prefix
    apply checks before replacing old context.
  - `internal/summarization/capsules.go` provides micro/phase/session capsule
    tiers, and apply-time anchor validation falls back to the original message
    slice on uncertainty.
  - Outbound redaction runs before formatting; `preprocessInput` then performs
    deterministic local cleanup before any provider call.
  - `doctor` and the TUI expose Layer 2 provider/data-path policy, including
    MiniMax external trust class and outbound redaction status.

## Non-Goals

- Do not silently send more data to a third party.
- Do not lower `min_tokens_for_layer2` globally without ROI gating.
- Do not summarize active edit context unless anchors and recent windows prove safe.

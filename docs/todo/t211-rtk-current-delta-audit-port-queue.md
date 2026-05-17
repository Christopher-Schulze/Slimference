# TASK 211: RTK current delta audit + port queue

Status: DONE (2026-05-17)
Priority: P1 before any "RTK parity" claim
Scope: `research/rtk-ai/rtk`, `internal/filter`, `internal/hooks`, `cmd/slimference filter|rewrite`, docs that mention RTK

## Why

RTK's public claim of 60-90 percent command-output reduction and up to roughly 3x longer Claude sessions is plausible because it sits at the pre-entry shell/tool-output layer. Slimference now embeds the RTK-style TOML catalog and has many more Go compactors, but the current repository still needs a fresh, exact comparison against the embedded RTK research snapshot. Older RTK audit docs reference `rtk-master/` and are stale relative to the current `research/rtk-ai/rtk` tree.

The goal is not to copy RTK wholesale. The goal is to prove, command by command, whether Slimference is already equal/better, whether RTK has a stronger equivalent-layer trick, or whether the difference is only adoption/surface rather than compression quality.

## Target State

- One current audit table maps every RTK command/filter/rewrite capability to the Slimference equivalent.
- Every gap is classified as:
  - `already-better`: Slimference has stricter parser, broader shape support, or better fail-open behavior.
  - `parity`: equivalent effect and coverage.
  - `port-now`: RTK has a clear higher-savings or higher-adoption mechanism with low drawdown.
  - `port-later`: useful but not Codex-first or not measurable yet.
  - `not-needed`: RTK feature is outside Slimference's target surface.
- The audit distinguishes raw filter effectiveness from adoption effectiveness. RTK's Claude Bash hook rewrite can outperform a better parser if it is applied earlier and more consistently.
- `docs/rtk-audit.md` and any RTK references are marked current or explicitly historical.
- No code is deleted or ported in this task unless the audit finds a tiny obvious doc-only correction.

## Maximum-Possible Check

This task must answer the exact question: "Does RTK have anything more efficient, more effective, or more leverageful than Slimference in the equivalent layer?"

Required dimensions:

- Command coverage: RTK `src/cmds`, `src/filters`, TOML rules, and rewrite registry.
- Rewrite coverage: compound commands, pipes, env prefixes, absolute command paths, redirections, explicit opt-out.
- File/read modes: minimal, aggressive, signature-only, structure-only, comment stripping, read-cache/delta overlap.
- Cloud/system commands: AWS, kubectl, helm, docker, systemctl, journalctl, df/du, security/network outputs.
- Test/build/lint parsers: compare RTK's heuristic summaries against Slimference Tier-1 JSON parsers and Go compactors.
- Observability: RTK savings database vs Slimference filter.db, admin/gain/savings surfaces.
- Fail-open: exit-code preservation, raw recovery, timeout, parser panic recovery.
- Claude adoption: RTK `updatedInput` PreToolUse rewrite vs Slimference Claude hook rewrite.
- Codex adoption: Codex cannot rely on `updatedInput`; compare proxy-side Layer-0 and WSS mutation instead.

## Acceptance

- A generated or manually verified matrix covers every RTK command/filter surface in the current research snapshot.
- The matrix cites exact files and line ranges or exact command names for every non-trivial conclusion.
- Any `port-now` item gets its own child task or is folded into T212/T213/T214 with a concrete sub-task.
- Any stale RTK doc claim is corrected or marked historical.
- The audit is read-only with respect to `research/rtk-ai/rtk`.

## Sub-Tasks

- [x] Inventory current RTK commands, filters, rewrite registry, and hook behavior.
- [x] Inventory current Slimference Layer-0 built-ins, TOML catalog, rewrite engine, and proxy-side Layer-0.
- [x] Produce a current capability comparison table in `docs/rtk-audit.md`.
- [x] Identify RTK advantages caused by better compression logic: none in the copied TOML catalog; remaining differences are command-specific Rust implementations vs Slimference generic Go dispatchers.
- [x] Identify RTK advantages caused by better adoption / earlier hook placement: Claude transparent Bash rewrite and transparent prefixes.
- [x] Split findings into `port-now`, `port-later`, and `not-needed`.
- [x] Update stale RTK docs so future agents do not use old `rtk-master/` conclusions as current truth.
- [x] Run targeted tests for touched docs/tests.

## Verification

- `go test ./internal/filter ./internal/hooks ./cmd/slimference -run 'TestRewrite|TestFilter|TestClaude|TestCodex' -count=1 -timeout 120s`
- `go test ./docs -count=1`
- Optional audit helper if useful: a Go script under `scripts/utils/` that emits the comparison table.

## Notes

Do not edit `research/rtk-ai/rtk`. It is reference material only.

Outcome: Slimference is stronger for Codex because it has proxy/WSS mutation plus Layer-0 parser parity. RTK's still-useful deltas are Claude wrapper ergonomics, configurable transparent prefixes, and optional future Claude non-Bash hooks. T212/T214 already absorbed the high-value default-off Claude/wrapper items; transparent prefixes and Claude Grep/Glob/LS remain future Claude-only work after real payload capture.

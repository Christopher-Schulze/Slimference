# T347 Headroom deterministic no-drawdown polish

Status: completed

## Why

Headroom contains several useful deterministic parser, evidence, and cache-proof ideas, but its model compression, CCR retrieval, and memory-injection paths do not satisfy Slimference's no-drawdown product rule. This task ports only the small, safe pieces that improve parser breadth, evidence preservation, and product truth without asking the model to reconstruct missing context.

## Acceptance

- Search output grouping parses Windows paths, dashed path/line separators, and ordinary `file:line:content` without changing context-mode full-pass guards.
- Capped search output keeps first/last evidence and promotes high-signal matches/files before plain middle rows.
- Evidence signals use one deterministic keyword registry for errors, warnings, importance, and security; `token` is not a security keyword.
- Extended error words such as abort, timeout, rejected, critical, crash, and exception drive diagnostic priority.
- Cache-impact reporting remains explicit enough to see preserved cache, cache risk, and negative net behavior.
- Documentation states that Headroom-derived changes are deterministic only; no model compression, CCR retrieval, or memory injection is product-enabled.
- Focused tests plus final `go run ./scripts/ci` pass, then the installed `slimference` binary is rebuilt from the current tree.

## Sub-Tasks

- [x] Audit Headroom for no-drawdown candidates and reject model/CCR/memory paths.
- [x] Harden deterministic search parser, scoring, and evidence selection.
- [x] Add deterministic keyword registry and evidence tests.
- [x] Verify cache reporting visibility and add missing proof tests.
- [x] Update documentation and close task docs.
- [x] Run gates, rebuild installed binary, and commit locally.

## Notes

- Current accepted candidates: search parser robustness, first/last plus top-score selection, deterministic keyword registry, extended error/warning/importance/security signals, and cache-impact reporting clarity.
- Current rejected candidates: Kompress/local model compression, lossy text/code summaries, CCR retrieve-on-demand, model-facing memory injection, and learn loops as default product logic.
- Implemented parser support for Windows drive paths and dashed `file-line-content` rows while preserving context/json/list/count/null full-pass guards.
- Implemented capped search selection as first/last plus high-signal match/file promotion before plain middle rows.
- Evidence now has deterministic keyword signals for errors, warnings, importance, and security; `token` is intentionally not a security keyword.
- Savings CLI and TUI evidence summaries now aggregate cache impact so cache preservation, warmup, and negative-net cases are visible.
- Existing Python traceback compression already covered chained exceptions and blank-line handling; the log priority set was extended with `rejected`.
- Proof: `go test ./cmd/slimference ./internal/tui ./internal/debug ./internal/filter ./internal/evidence` passed.
- Proof: `go run ./scripts/ci` passed all 8 steps with total coverage 95.4%.

## Deviations

- None.

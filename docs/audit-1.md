# Audit 1 - Production Readiness Baseline

Date: 2026-04-17
Scope: entire Slimference repository except `rtk-master/`
Intent: establish a hard baseline for remediation and later comparison

---

## Mission

This audit treats the existing specification and documentation as the target
contract. The goal is not to lower the documentation to the implementation.
The goal is to raise the implementation until the documented guarantees can be
proven by code, tests, and live verification.

---

## Live Verification Snapshot

The following checks were executed during the audit:

- `go test ./...`
- `go test -race ./...`
- `go test -cover ./cmd/... ./internal/...`
- `go run ./scripts/coverage -min=100`
- `go run ./scripts/coverage -- -min=100`
- `go run ./scripts/ci`
- `bun test`
- `codex --version`
- `claude --version`

Observed baseline:

- Go test suite: green
- Race detector: green
- TypeScript test suite: green on the latest rerun
- Total Go coverage on `cmd/...` + `internal/...`: `97.3%`
- `scripts/coverage -min=100`: fails correctly
- `scripts/coverage -- -min=100`: passes incorrectly
- `scripts/ci`: passes while real coverage is below target

CLI versions used for compatibility spot checks:

- Codex CLI: `0.121.0`
- Claude Code: `2.1.110`

---

## What Is Strong

- The Layer 1 compressor architecture is substantial, modular, and already
  close to production shape.
- The proxy core is structurally sound and survives the race detector.
- The repository has unusually high test density for an early-stage proxy.
- The project already has the right decomposition for a serious hardening pass:
  hooks, filter pipeline, proxy hot path, summarization, cache, analytics, TUI.

---

## Findings

### Critical

#### A1 - Zero-downside is not actually guaranteed in the proxy hot path

- Severity: critical
- Area: `internal/proxy/handler.go`
- Evidence: request body is rebuilt before the negative-savings revert, so the
  revert updates metrics and state but may still send the already-built
  compressed body upstream.
- Why this matters: this directly violates the core product promise that the
  proxy never makes the request worse.
- Target fix: move the guard before body reconstruction or rebuild the body
  after reverting.
- Tracking plan: `docs/todo/t13-zero-downside-and-cache-correctness.md`

#### A2 - Response cache keying is not trustworthy enough for production use

- Severity: critical
- Area: `internal/caching/response_cache.go`, `internal/types/types.go`
- Evidence: the cache key is based on role + text-only content + model, while
  non-text blocks and other effective request inputs are omitted.
- Why this matters: different requests can alias to the same cache entry.
- Target fix: build a canonical request fingerprint from the full normalized
  effective request, not text-only slices.
- Tracking plan: `docs/todo/t13-zero-downside-and-cache-correctness.md`

#### A3 - Hook contracts for Claude Code and Codex are not proven-correct

- Severity: critical
- Area: `internal/hooks/claude.go`, `internal/hooks/codex.go`, `internal/hooks/verify.go`
- Evidence:
  - Claude hook generation is not yet aligned with the modern structured hook
    response contract and destroys existing `PreToolUse` user config on
    install/remove.
  - Codex `PostToolUse` currently routes `tool_response` into `slimference filter`,
    but `filter` executes a command instead of filtering a finished output blob.
  - `hook verify` treats Codex as best-effort and does not fail hard on missing
    or broken Codex integration.
- Why this matters: the main adoption layer for both supported CLIs is not yet
  strict enough to support the current product claims.
- Target fix: rebuild both hook paths around the real contracts, make config
  merging non-destructive, and make verification authoritative.
- Tracking plan: `docs/todo/t12-hook-contract-hardening.md`

### High

#### A4 - Coverage proof and CI gating are not aligned with the documented target

- Severity: high
- Area: `scripts/ci`, `scripts/coverage`, coverage-proof workflow
- Evidence: the CI runner passes `-- -min=100`, so the minimum is not parsed and
  the gate can succeed even when the repository is below 100%.
- Why this matters: the repo currently cannot prove the strongest testing claim
  in the docs.
- Target fix: repair the gate, add a proof-oriented verification flow, and
  increase package coverage until the target is real.
- Tracking plan: `docs/todo/t16-proof-gates-and-release-readiness.md`

#### A5 - Layer 2 is opportunistic, not truly strict

- Severity: high
- Area: `internal/summarization/*`, `internal/proxy/handler.go`
- Evidence:
  - Layer 2 is async and cache-first, so many requests run without fresh
    summarization.
  - Unconfigured or failed MiniMax calls fall through without a hard policy.
  - Background summarization still uses `context.Background()` in key call
    paths.
- Why this matters: the repo does not yet offer a clean answer to the tension
  between "zero downside" and "MiniMax as strongly forced as possible".
- Target fix: define explicit operating modes, tighten validation, and wire
  cancellation through every summarization path.
- Tracking plan: `docs/todo/t14-layer2-strictness-and-cancellation.md`

#### A6 - The daemon/service path is not safe enough for production

- Severity: high
- Area: `internal/daemon/daemon.go`
- Evidence:
  - launchd plist generation writes `MINIMAX_API_KEY` in plaintext
  - install/uninstall comments overstate what the code actually does
  - service lifecycle helpers are partly placeholder behavior
- Why this matters: this is not acceptable for a production-grade local service.
- Target fix: remove plaintext secret persistence, implement real launchctl
  lifecycle handling, and add serious tests.
- Tracking plan: `docs/todo/t15-daemon-service-productionization.md`

### Medium

#### A7 - The Layer 2 validator is weaker than the product story suggests

- Severity: medium
- Area: `internal/summarization/validator.go`
- Evidence: function-name preservation is derived from fenced code blocks only,
  while the summarization input format often does not preserve code as fenced
  blocks.
- Why this matters: the current validator can approve summaries without truly
  proving preservation of code-significant identifiers.
- Target fix: validate against structured message content instead of markdown
  fences alone.
- Tracking plan: `docs/todo/t14-layer2-strictness-and-cancellation.md`

#### A8 - Documentation, changelog, and todo history currently overstate proof

- Severity: medium
- Area: repository docs
- Evidence: multiple files state full parity or 100% coverage while the live
  verification baseline does not yet prove those claims.
- Why this matters: it weakens release confidence and makes regression review
  harder.
- Target fix: keep the target level unchanged, but add an explicit remediation
  and proof program so the repo can climb to that documented level with hard
  evidence.
- Tracking plan: `docs/gap-analysis.md`, `docs/todo/t11-audit-remediation-program.md`

---

## Production Blockers

The following must be fixed before the repository should be considered
production-ready:

1. Zero-downside guarantee must be mechanically true in the hot path.
2. Response cache correctness must be formally safe enough to trust.
3. Claude Code and Codex hook adoption must be contract-correct and verifiable.
4. The proof stack must be real: CI gate, coverage target, release checks.
5. Layer 2 behavior must have an explicit strictness model and full cancellation.
6. The daemon/service path must stop persisting secrets in plaintext.

---

## Remediation Linkage

- Gap matrix: `docs/gap-analysis.md`
- Program driver: `docs/todo/t11-audit-remediation-program.md`
- Hook hardening: `docs/todo/t12-hook-contract-hardening.md`
- Zero-downside and cache correctness: `docs/todo/t13-zero-downside-and-cache-correctness.md`
- Layer 2 strictness and cancellation: `docs/todo/t14-layer2-strictness-and-cancellation.md`
- Daemon productionization: `docs/todo/t15-daemon-service-productionization.md`
- Proof gates and release readiness: `docs/todo/t16-proof-gates-and-release-readiness.md`

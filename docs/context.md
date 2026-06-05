# Slimference - Current Context

Date: 2026-06-05

This file is a short orientation note. The old v2.0 remediation worklog has
been superseded by the current Layer 2 removal pass.

## Current Product Shape

- Layer 0: pre-entry / Codex WSS tool-output reduction.
- Layer 1: deterministic compression.
- Layer 3: response and provider-cache leverage.
- Layer 4: output and tool-surface reduction.
- Layer 2: removed. No MiniMax, no external summarizer, no local summarizer, no
  OCRL/context-ledger insertion, no Layer 2 CLI/config surface.

## Current Proof Rule

`go run ./scripts/ci` is the final local truth gate. `go test ./...` and focused
package tests are useful intermediate gates, but they are not the final release
verdict.

## Current Docs

- `docs/documentation.md`: architecture and usage.
- `docs/savings-assessment.md`: honest active-layer savings reality.
- `docs/live-corpus-policy.md`: release proof and live-corpus rules.
- `docs/map.md`: current package map.
- `docs/todo.md`: active work queue.

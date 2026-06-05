# OCRL - Retired

OCRL, the Old Context Recovery Ledger, is retired from the Slimference product
and codebase.

## Current Status

- No model-facing OCRL replacement exists.
- No OCRL shadow/proof route exists in product code.
- No `[compression.ocrl]` config surface exists.
- No `slimference layer2 status` command exists.
- No `internal/contextledger` or `internal/proxy/ocrl_*` package exists.
- No live-corpus category is required for OCRL promotion.

## Reason

The idea was deterministic and archive-backed, but the product requirement is
stricter: Slimference must not hide old context behind a capsule or replacement
unless it can prove the model will never need the hidden detail. That proof is
not generally available for Codex/GPT workflows. The safe savings direction is
therefore Layer 0/WSS tool-output reduction, Layer 1 deterministic compression,
Layer 2 cache leverage, and Layer 4 output/tool-surface reduction.

This file remains only to make old links resolve to the current decision.

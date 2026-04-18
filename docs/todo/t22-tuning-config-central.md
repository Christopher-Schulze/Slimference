# T22 - Central `[compression.tuning]` Config Block

Status: open
Priority: medium
Scope: internal/config, internal/compression, internal/summarization,
       internal/proxy/handler.go, spec+.md

---

## Problem

Numerical thresholds are scattered across the codebase. Examples:

- MinHash Jaccard `0.85` (compression dedup) vs `0.70` (summarization fuzzy
  dedup) - two philosophies, no central rationale.
- MiniMax input token cap around 120 000 - hardcoded in layer2.go.
- Overflow recover `SlidingWindow = 2` and `TargetRatio = 0.10` - inlined in
  handler.go.
- Token estimation: `len(s)/4` in proxy paths, word-based in summarization.
- L2 incremental-summary overlap threshold 0.70 - inlined.
- L2 min-messages-for-compression `20` - in defaults but referenced inline.

Consequence: tuning requires code edits, model/provider changes have friction,
and two places can drift against each other.

---

## Desired End State

A single `[compression.tuning]` TOML block holds every numerical knob that is
not a hard safety limit. Defaults live in `internal/config/defaults.go`.
Every use-site reads from config. `spec+.md` documents each knob: name,
effect, default, range, when to touch.

---

## Work Packages

### WP1 - Inventory and classify

Produce a table in `docs/tuning-inventory.md`:

| Name | Current literal | Location | Default | Range | Purpose |

Cover at minimum:

- dedup.jaccard_threshold
- summary.fuzzy_dedup_jaccard
- summary.input_token_cap
- overflow.sliding_window
- overflow.target_ratio
- incremental.overlap_threshold
- l2.min_messages_for_compression
- priority.staircase_ratios (links to T26)

### WP2 - Config schema

- Add `[compression.tuning]` section in `Config` struct and `defaults.go`.
- Validate ranges in `config.Load`.
- Back-compat: missing keys use defaults.

### WP3 - Replace literals

- Every inline literal becomes a config read.
- No function signatures change externally.
- Prefer passing `*config.Config` deeper where a reasonable boundary exists;
  otherwise keep a package-level `var` initialized on Proxy construction.

### WP4 - Docs and tests

- Document each knob in `spec+.md` §3 Compression Tuning.
- Regression: existing tests must all pass with defaults unchanged.
- New test: override each knob via TOML, verify end-to-end effect.

---

## Subtasks

- [ ] Write `docs/tuning-inventory.md`.
- [ ] Extend `Config` + `defaults.go` + validation.
- [ ] Replace literals with config reads.
- [ ] Add override tests per knob.
- [ ] Update `spec+.md` compression tuning section.

## Acceptance Criteria

- No magic number stays inline for the listed knobs.
- A single TOML edit changes the behaviour end-to-end.
- Default behaviour unchanged; full test suite stays green at 100 %.

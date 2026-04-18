# T36 - Layer 2 Operating Modes (strict / balanced / fast)

Status: open
Priority: high
Scope: internal/summarization, internal/config, internal/proxy,
       spec+.md §6, docs/documentation.md

---

## Problem

`docs/gap-analysis.md` identified the tension between three goals:

1. Zero downside.
2. Maximum MiniMax aggressiveness (strongest compression).
3. Zero perceived latency.

The codebase currently supports these as **independent flags** (`strict`,
various ratios, async queue thresholds). There is no explicit precedence
rule when they conflict, and no single switch to express user intent. In
practice that means tuning drifts: enabling "strict" does not also tighten
the other knobs consistently.

---

## Desired End State

Three named operating modes, selected via a single config field or runtime
toggle, each a coherent package:

- **strict**: validator enforces tight acceptance, target ratio low (e.g.
  0.15), retries with emphasis, aggressive window, sync where safe.
  Priority: correctness > aggressiveness > latency.
- **balanced** (default): current best-effort behaviour tightened into one
  named bundle. Priority: correctness > latency > aggressiveness.
- **fast**: async only, shorter queue timeouts, conservative target ratio,
  skip MiniMax entirely on small windows. Priority: latency > correctness
  > aggressiveness.

Precedence rules are written down in `spec+.md` §6.x and enforced in one
place (`applyOperatingMode(cfg, mode)` in `internal/summarization`).

Mode is set via:

- TOML: `[compression.summary] mode = "balanced"`.
- Env override: `SLIMFERENCE_L2_MODE=strict`.
- Runtime toggle in the TUI.

---

## Work Packages

### WP1 - Mode definition

- New `type OperatingMode string` with constants.
- `applyOperatingMode(cfg *Config, mode OperatingMode)` mutates the summary
  config to a self-consistent profile.
- Unit test each profile.

### WP2 - Wire the mode end-to-end

- Config load honors `mode`.
- Env override at startup.
- TUI exposes mode switch, persisted via T31.

### WP3 - Precedence documentation

- `spec+.md` §6 gains a table: per mode, what wins on conflict between
  correctness / aggressiveness / latency.
- `docs/documentation.md` explains which mode to pick.

### WP4 - Tests

- Each mode: verify resulting config fields match the profile.
- End-to-end: strict mode retries more, fast mode skips L2 on small windows,
  balanced matches current default.
- No regression on existing summarization tests when balanced is default.

### WP5 - Compatibility

- Existing explicit knobs (target_ratio, strict flag) continue to work as
  overrides *after* the mode is applied. Order: mode -> explicit override.
- Document the override order.

---

## Subtasks

- [ ] Define modes and `applyOperatingMode`.
- [ ] Wire through config / env / TUI.
- [ ] Write precedence table in spec and docs.
- [ ] Per-mode and override-order tests.
- [ ] Migrate existing tests to the named default (balanced).

## Acceptance Criteria

- A single config field selects a coherent operating profile.
- Documented precedence rules resolve conflicts deterministically.
- Explicit overrides still work and are documented.
- Coverage stays at 100 %.

# TASK 232: Non-product surface governance

Status: PARTIAL - CLI/help/docs/TUI lab fencing landed; legacy retirement refs pending
Priority: P1 alongside T227
Scope: App-server debug, global transparent MITM, legacy proxy/env/integrate surfaces

## Why

The project accumulated several viable but overlapping surfaces:

- scoped Codex provider route;
- Codex hooks;
- global transparent MITM;
- legacy proxy/env/config-patch commands;
- experimental Codex app-server/debug surfaces.

Only the first two belong to the normal Codex product path. The rest should not
be deleted yet, but they must be fenced so future agents do not accidentally
promote them again.

## Target State

Product surfaces:

1. Scoped Codex provider route.
2. Codex hooks as signal/local-output layer.

Lab/debug surfaces:

- global transparent MITM with CA/hosts/pfctl;
- Codex app-server/remote-control inspection;
- legacy proxy/env/integrate commands;
- tshark/indist probe.

Rules:

- Lab/debug surfaces are documented as non-product.
- They are not shown as normal TUI setup actions.
- They are not triggered by `install` or normal `enable`.
- They remain testable and reversible.

## Acceptance

- `docs/install.md` and help text use one vocabulary:
  product, fallback, lab, legacy.
- Global MITM is not extended except for safety/compatibility fixes.
- Codex app-server is not introduced as a primary integration route unless
  T225 proves Desktop needs it and no better scoped route exists.
- Legacy proxy/env/integrate paths keep tests but are not default examples.
- `research/` remains gitignored; no local RTK snapshot is required for builds.
- Future task additions must state whether they affect product or lab/debug
  surfaces.

## Sub-Tasks

- [x] Audit help text and docs for mixed language around product/lab/legacy.
- [x] Add a small governance table to `docs/install.md`.
- [x] Update TUI wording so global lab actions are visually separate.
- [x] Add tests that normal install/enable do not call lab/global paths.
- [ ] Add doc references to T210 for eventual legacy retirement.
- [ ] Add app-server note: debug/diagnostic only unless Desktop proof says
  otherwise.
- [ ] Keep global MITM code healthy but stop feature expansion there unless a
  task explicitly says "lab".

## Pre-Live Implementation Notes

- Normal `slimference enable|disable` no longer touches
  `transparent.sni_peek_mode`.
- Global SNI-peek mode is behind `slimference lab enable|disable`.
- `docs/install.md` now has an explicit surface-governance table.
- Existing `cert-trust`, `root-arm`, and `root-disarm` commands stay available
  for compatibility, but product docs and help promote `slimference lab ...`.
- TUI Setup renders global lab separately from Codex Mode and no longer uses
  the top-level scoped `enable` alias for lab arming.

## Benefits

- Prevents surface creep.
- Makes the architecture easier for humans and agents to understand.
- Keeps useful lab tools available without compromising the scoped product.

## Drawdowns and Guards

- Too much hiding can make debugging harder. Guard: advanced docs and commands
  stay available.
- Lab code can rot. Guard: keep smoke tests and reversibility checks, but do not
  expand features without explicit lab scope.

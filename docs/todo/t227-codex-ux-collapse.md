# TASK 227: Codex UX collapse

Status: PARTIAL - CLI/TUI collapse implemented; Desktop proof and live warnings pending
Priority: P1 after T226/T225 proof gates
Scope: User-facing command model and TUI wording

## Why

The product decision is now simple: Slimference is Codex-first. Claude Code code
stays in the repository but remains parked, default-off, and not installed or
routed. Global transparent MITM remains lab-only. The user should not have to
think in terms of old Phase G/H surfaces.

The final UX should say:

- `slimference enable` means enable scoped Codex route.
- `slimference disable` means disable scoped Codex route.
- `slimference status` reports Codex route, daemon, WSS health, and lab state.
- global CA/hosts/pfctl operations are explicit lab commands, not the normal
  product path.

Internally hooks and provider routing remain separate mechanisms. Externally
they should feel like one Codex mode.

## Target State

Top-level commands:

| Command | Final behavior |
|---------|----------------|
| `slimference install` | Install daemon + Codex hooks/notice only; no Claude, no hosts/pfctl. |
| `slimference enable` | Enable scoped Codex CLI/App route in auto transport mode. |
| `slimference enable --transport=wss` | Force scoped Codex WSS route. |
| `slimference disable` | Disable scoped Codex route. |
| `slimference status` | Show Codex route, WSS proof, daemon, hooks, Desktop observation, and lab state. |
| `slimference uninstall` | Remove installed Slimference-owned Codex artifacts and daemon state. |
| `slimference lab ...` or explicit old commands | Global CA/hosts/pfctl certification path only. |

`slimference codex ...` remains as an expert namespace and backwards-compatible
spelling, but the top-level UX should be enough for normal use.

## Acceptance

- Top-level `enable/disable/status` operate on scoped Codex route by default.
- Global transparent enable/disable semantics are moved behind an explicit lab
  namespace or still require unmistakable lab flags.
- TUI Setup has one primary control: `Codex Mode: off|http|wss|auto`.
- TUI separates `Codex CLI one-shot`, `Codex shared route`, and `Lab global MITM`
  without exposing legacy proxy/env/config-patch paths as normal choices.
- Help text does not make users choose between Phase H/G internals.
- Claude Code is visible only as parked/future/off, never as an install action.
- Existing scripts/tests that require legacy commands still work or get clear
  deprecation wording.

## Sub-Tasks

- [x] Define final command aliases and compatibility rules for top-level
  `enable`, `disable`, and `status`.
- [x] Decide exact lab namespace: `slimference lab cert-trust|root-arm|root-disarm|enable|disable`.
- [x] Update CLI help text to make scoped Codex the default product path.
- [x] Update TUI Setup view to show `Codex Mode` as the main switch and
  visually separate global lab controls.
- [x] Add transport display:
  `auto`, `wss`, `http`, `direct/off`. Editing remains CLI/TUI-route toggle
  only until WSS is live-certified.
- [ ] Add status warnings for:
  route enabled but daemon down, WSS uncertified, Desktop not observed,
  lab hosts active, and Claude unexpectedly active.
- [x] Add regression tests that bare `root-arm` is not part of normal enable.
- [x] Update `docs/install.md`, `docs/integration.md`, and help meta-tests.

## Pre-Live Implementation Notes

- `slimference enable` now delegates to `codex enable`.
- `slimference disable` now delegates to `codex disable`.
- The former global SNI-peek config switch is available through
  `slimference lab enable|disable`.
- Existing global commands `cert-trust`, `root-arm`, and `root-disarm` remain
  for compatibility, but help and docs promote the lab namespace.
- TUI global-lab actions now call `runLabEnableCmd` / `runLabDisableCmd`,
  not the top-level scoped Codex aliases.

## Benefits

- Large UX reduction: one product switch instead of multiple historical
  surfaces.
- Reduces future agent confusion: Slimference product mode equals Codex route.
- No runtime drawback because the internal hook/provider split remains intact.

## Drawdowns and Guards

- Risk: existing users expect `enable` to mean global SNI mode. Guard: explicit
  migration message and lab namespace.
- Risk: hiding internals can obscure recovery. Guard: `status --preflight` and
  TUI advanced panel still show daemon/listener/hosts/pfctl truth.

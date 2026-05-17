# TASK 221: Scoped Codex WSS Phase-F mode

Status: PLANNED
Priority: P0 after T220/T209 CLI smoke
Scope: Codex CLI first; Codex Desktop same target after T224/T225 proof; no global hosts/pfctl

## Why

T220 made the product path safe by moving Codex traffic away from global
`/etc/hosts` and into a scoped local provider route. That protects Browser
ChatGPT and ChatGPT.app, but the first implementation sets
`supports_websockets=false` for stability. This means Codex uses HTTP
Responses through the normal proxy path, not the native Codex WSS transport.

The maximum target is to recover the old transparent path's WSS benefits
without its machine-wide blast radius:

- Codex-only routing.
- WSS-first transport after live proof, because WSS is the native Codex
  conversation shape and the richest surface for frame-level savings.
- Real Codex WSS request/response frames.
- Existing Phase-F WSS adapter savings.
- Browser ChatGPT, ChatGPT.app, Claude Code, and generic OpenAI clients direct.
- Fail-open to byte-equal frame bridge on schema drift.
- HTTP and direct modes remain explicit fallbacks, not the desired final
  high-performance path.

## Target Transport Policy

`slimference codex run` should converge to:

| Mode | Purpose | Expected final status |
|------|---------|-----------------------|
| `--transport=auto` | Default after proof. Try WSS when preflight says WSS is safe; fall back to HTTP scoped; fall back to direct only when daemon is unreachable before launch. | Product default |
| `--transport=wss` | Force scoped WSS for certification, debugging, and maximum savings. | Advanced/power mode until T224 passes |
| `--transport=http` | Stable scoped HTTP Responses route from T220. | Fallback and regression baseline |
| `--direct` | Launch native Codex without Slimference. | Emergency / comparison baseline |

The same policy should be available to `slimference codex enable` only after
the shared config path and Desktop behavior are proven:

| Command | WSS goal |
|---------|----------|
| `codex run` | WSS-first for CLI once live-certified. |
| `codex enable` | WSS-first for CLI/App only after Desktop/provider reload proof. |
| TUI `[r]` | Toggle the shared scoped route; show transport state clearly. |

## Acceptance

- `slimference codex run` gains an explicit WSS mode, for example
  `--transport=wss` or an equivalent future flag.
- `--transport=auto` exists and is documented as the intended final default
  after T224 proof. Until then, default may remain HTTP to avoid premature
  breakage.
- `slimference codex enable` can optionally write
  `supports_websockets=true` only when the operator requests WSS mode.
- Default remains stable HTTP until scoped WSS passes live proof; after proof,
  auto mode should prefer WSS.
- Direct local Codex WSS upgrades route through `wsmitm.Session` and
  `wsPhaseFAdapter`, not only the byte tunnel.
- Known Codex WSS request frames apply the same Phase-F input pipeline as
  T208: stale-read aging, obsolete-read prune, Layer-0 tool output compaction,
  stop-sequence injection, and be-terse where gates allow.
- Known Codex WSS response frames apply streamcut and repdet where the existing
  adapter supports it.
- Unknown/binary/control/malformed frames remain byte-equal fail-open.
- `/admin/state.wss` distinguishes tunnel-only, mutated, degraded, and parse
  failure sessions for scoped WSS, not only global transparent WSS.
- Tests prove HTTP mode still works and WSS mode never touches
  `/etc/hosts`, pfctl, Keychain CA, Browser ChatGPT, ChatGPT.app, or Claude
  Code.
- Forced WSS mode has a clear error when WSS preflight fails; auto mode falls
  back without killing Codex.

## Sub-Tasks

- [ ] Verify how Codex CLI constructs WSS URLs when a custom provider has
  `base_url=http://127.0.0.1:8990/backend-api/codex` and
  `supports_websockets=true`.
- [ ] Add `--transport=auto|wss|http` parsing for `slimference codex run`.
- [ ] Add a scoped route option for WSS without prematurely changing the
  default HTTP route.
- [ ] Define the promotion gate for making `auto` prefer WSS by default:
  local tests + T224 live capture + no degraded sessions on smoke.
- [ ] Reuse the T208 `wsPhaseFAdapter` in direct local WSS upgrades.
- [ ] Add preflight for WSS readiness: daemon reachable, route enabled, WSS
  handler registered, upstream dial profile available.
- [ ] Add a scoped WSS route mode to debug/flight records.
- [ ] Add `/admin/state.wss` counters or labels that prove scoped WSS mutation.
- [ ] Add tests for WSS mutation, byte-equal fallback, degraded sessions, and
  HTTP-mode regression.
- [ ] Add docs/install wording: WSS mode is advanced until live-certified.
- [ ] Run T209/T224 live smoke from a non-Codex shell before promoting WSS.

## Notes

Critical drawbacks of WSS mode:

- Longer-lived sessions mean daemon death can break the active WSS turn. The
  process-level fallback of `codex run` only happens before launch; mid-session
  transport loss cannot be invisible.
- WSS schema drift can only fall back to byte-equal bridge; savings disappear
  until the parser is updated.
- Native Codex may rely on subtle WebSocket subprotocol/header behavior. T222
  must preserve raw upgrade bytes before this can be called close to native.

Why WSS is still the right high-end target:

- WSS is the closest match to current Codex conversation traffic.
- WSS lets Slimference mutate at message/frame boundaries rather than forcing
  Codex into a lower-fidelity HTTP fallback.
- WSS can preserve streaming semantics and future Codex conversation features
  better than forcing `supports_websockets=false`.
- WSS makes T208's frame adapter economically valuable on real traffic.

Benefit compared with T220 HTTP mode:

- Higher compatibility with native Codex 0.130+ transport.
- Restores T208 WSS Phase-F savings on real Codex WSS traffic.
- Avoids global `chatgpt.com` capture.

Benefit compared with old global transparent path:

- Same class of WSS frame mutation, but scoped to Codex CLI/App provider route
  instead of machine-wide hosts/pfctl.

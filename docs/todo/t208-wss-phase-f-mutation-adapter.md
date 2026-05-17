# TASK 208: WSS Phase F mutation adapter

Status: DONE (2026-05-17)
Priority: P0 after Codex-only wiring
Scope: `internal/proxy/wsmitm*`, `internal/proxy/wsmitm_dispatcher.go`, Phase F handler helpers, tests with Codex WSS envelope fixtures

## Why

The transparent SNI path can route and bridge Codex WSS traffic, but the current wsmitm frame handler is still byte-equal for MITM conversations. That is correct for safety but it means real Phase F savings on Codex conversation frames are not active yet.

## Acceptance

- Real or fixture-backed Codex WSS envelopes are parsed into typed request / response payloads.
- Client-to-server request payloads can run through the existing input Phase F pipeline without duplicating handler logic.
- Server-to-client deltas and completion payloads can run through streamcut / repdet / output-reduce logic.
- Unknown frame kind, schema drift, parse error, binary frame, or non-Codex path degrades to byte-equal bridge with telemetry.
- Tests prove both mutation and fail-open behavior. A broken mutator must make a test fail.
- `/admin/state` exposes enough counters to distinguish byte-bridge, degraded bridge, and mutated WSS frames.

## Sub-Tasks

- [x] Capture or reconstruct a minimal Codex WSS envelope corpus.
- [x] Define typed envelope adapters at the wsmitm boundary.
- [x] Extract shared Phase F helper functions from the HTTP-shaped handler path.
- [x] Wire `wsmitm.Session.FrameHandler` to those helpers.
- [x] Add mutation, no-op, degraded, binary, and schema-drift tests.
- [x] Add admin telemetry assertions.
- [ ] Run live certification after T209 allows arming outside active Codex.

## Verification

- `go test ./internal/proxy/wsmitm ./internal/proxy -run 'TestEnvelope|TestSession|TestMITM|TestWSPhaseF|TestDispatcher' -count=1 -timeout 120s`
- `go test ./internal/control ./internal/proxy ./cmd/slimference -run 'TestAdminState|TestBuild|TestWSPhaseF|TestMITM|TestSNIPeek|TestStartSNI|TestPhaseG' -count=1 -timeout 180s`
- `go test ./... -count=1 -timeout 300s`
- `go run ./scripts/ci` — passes all 8 steps, including formal coverage gate (`100.0%` statements).
- `go test ./internal/proxy ./internal/summarization ./internal/filter ./internal/transparent ./internal/control/apps ./internal/install/installsteps ./internal/tui -race -count=1 -timeout 300s`

## Notes

Implemented path:

- `wsmitm.Envelope` now preserves unknown JSON fields and can safely re-marshal mutated frames without dropping future Codex schema fields.
- `PhaseFDispatcher` now wires `wsmitm.Session` handlers to a WSS Phase F adapter when a live `Proxy` is attached.
- Client-to-server request frames support `body`, `request`, and top-level Codex request shapes. They run stale-read aging, obsolete-read prune, stop-sequence injection, and be-terse when the existing config/cohort gates allow it.
- Server-to-client text deltas run streamcut and repdet; terminal `response` payloads run the existing OpenAI/Codex response-body repdet helper.
- Unknown frames, absent request bodies, missing proxy wiring, malformed JSON, binary frames, and parser degradation remain byte-equal fail-open.
- `/admin/state.wss` now exposes engine state, bridge counts, parse failures, degraded sessions, forwarded frames, and re-encoded frames so operators can distinguish byte-bridge from active mutation.

Deferred deliberately: T209 live certification with real Codex CLI traffic, because arming hosts/pfctl/CA from inside the active Codex session can cut off the session.

# TASK 194: Codex Desktop App sideband bypass certification

Status: PLANNING 2026-05-16
Priority: P0 (correctness gate - we must not break voice, computer-use, etc.)
Scope: `internal/proxy/sniroute/` (T189) tests, new
       `tests/integration/codex_desktop_sideband_e2e.go`, capture corpus
       under `research/codex_desktop_capture/`

## Why

The Codex Desktop App (2026 build) emits many endpoint families under
`https://chatgpt.com/backend-api/codex/*` plus other host families. Only
ONE of them - the conversation `/responses` endpoint - is compression-
eligible. All others must pass through byte-equal.

The user requirement is explicit:

> "voice und browser use und computeruse traffic bleibt ja untouched in
>  der codex app korrekt?"

T189 has the routing table. T194 is the **certification** that the routing
table is correct against the real Codex Desktop App, not just against our
mental model.

## Sideband endpoints to certify

Captured / source-verified inventory of the Codex Desktop App's non-
conversation traffic (Codex 2026 builds):

### Under chatgpt.com/backend-api/

| Path                                              | Purpose                          | Decision      |
|---------------------------------------------------|----------------------------------|---------------|
| `/codex/responses` (POST or WSS)                  | Conversation                     | MITM          |
| `/codex/responses/compact`                        | Conversation compaction sideband | passthrough   |
| `/codex/realtime/calls`                           | Voice call setup                 | passthrough   |
| `/codex/realtime/calls/<id>`                      | Voice call mgmt                  | passthrough   |
| `/codex/realtime/calls/<id>/transcript`           | Microphone transcript stream     | passthrough   |
| `/codex/images/generations` (POST)                | Image gen (gpt-image-1.5)        | passthrough   |
| `/codex/images/edits`                             | Image edits                      | passthrough   |
| `/codex/memories/trace_summarize`                 | Memory subsystem                 | passthrough   |
| `/codex/memories/*`                               | Memory subsystem                 | passthrough   |
| `/codex/plugins`                                  | Plugin manifest                  | passthrough   |
| `/codex/plugins/install`                          | Plugin install                   | passthrough   |
| `/codex/plugins/<id>`                             | Plugin detail                    | passthrough   |
| `/codex/models`                                   | Model listing (GET)              | passthrough   |
| `/codex/skills/*`                                 | Skills loader                    | passthrough   |
| `/codex/analytics-events/events`                  | Telemetry                        | passthrough   |
| `/codex/audio/*`                                  | (any non-realtime audio)         | passthrough   |
| `/wham/remote/control/server` (WSS)               | Remote-control server            | passthrough   |
| `/wham/*` (other)                                 | Remote-control                   | passthrough   |
| `/api/auth/session`                               | ChatGPT web auth                 | passthrough   |
| `/api/*` (browser web UI APIs)                    | ChatGPT web                      | passthrough   |
| `/feedback/*`                                     | Feedback channel                 | passthrough   |

### Other domains (Codex Desktop may reach)

| Domain                | Purpose                              | Decision      |
|-----------------------|--------------------------------------|---------------|
| `auth.openai.com`     | OAuth flow                           | passthrough   |
| `cdn.oaistatic.com`   | Static assets (icons, fonts)         | passthrough   |
| `images.openai.com`   | Generated images, screenshots        | passthrough   |
| `videos.openai.com`   | Generated videos                     | passthrough   |
| `api.openai.com`      | API key flow (if user has one)       | MITM (T189)   |
| Any other             | Out-of-scope                         | passthrough   |

### WebRTC / UDP (off the :443 path entirely)

Voice / microphone (live realtime calls) flows over UDP/SRTP after the
WebRTC handshake. Never hits our :443 listener. Documented for
completeness; out of scope for routing.

## Test plan

### Layer 1: routing-table unit tests (already in T189 sub-tasks)

Cover every row of the table with synthetic SNI/path/UA inputs. Assert
correct decision.

### Layer 2: capture-corpus replay tests

For each sideband endpoint, capture a real request from the Codex
Desktop App (operator task with our CA + a development build of the
proxy in observe-only mode). Persist the request shape under
`research/codex_desktop_capture/<endpoint>.json`.

Replay each capture through the production SNI router with mock
upstream. Assert:
1. Decision was `passthrough_tls`.
2. The bytes forwarded to upstream are byte-equal to the captured input
   (modulo header re-serialization order tolerances we explicitly
   permit).
3. The bytes returned to the client are byte-equal to the upstream
   response (we don't touch passthrough responses).
4. No Phase F counter increments.

### Layer 3: live verification on a real Mac

Operator-driven smoke test:

1. Install Slimference via TUI.
2. Open Codex Desktop App, start a conversation - verify MITM counter
   increments.
3. In the same Codex Desktop App: dictate via microphone - voice
   transcription works, MITM counter does NOT increment for the
   realtime path, passthrough counter does.
4. In the same Codex Desktop App: generate an image - works, passthrough
   counter increments, MITM does not.
5. In Codex Desktop App: install a plugin - works, passthrough.
6. In Codex Desktop App: trigger a computer-use action - the
   `computer_call` items appear in the conversation MITM (because
   they live in the `/responses` body), but screenshots in
   `computer_call_output` are passed through verbatim inside the MITM
   path (the JSON content blocks survive).
7. Open `https://chatgpt.com` in Safari (system browser) - works,
   transparent passthrough.

Live verification is a manual checklist run before each release that
touches the SNI router.

### Layer 4: runtime safety guard

If at any time a `passthrough_tls`-classified request lands on the MITM
path due to a routing bug, the engine must:

1. Detect via a sanity check: the request body must parse as a Responses-
   API conversation envelope. If not, immediately downgrade to passthrough.
2. Increment `wsmitm_misroute_count` counter (T188 specifies this).
3. Log a structured warning (level=warn) with the path + UA.

This is a belt-and-braces fail-open guard. Code under
`internal/proxy/wsmitm/session.go` + parallel HTTP path.

## Sub-Tasks

- [ ] Capture corpus for every endpoint listed.
- [ ] Layer-1 unit tests already in T189; cross-link.
- [ ] Layer-2 replay test scaffolding.
- [ ] Layer-3 manual checklist in `docs/release/codex-desktop-checklist.md`.
- [ ] Layer-4 runtime guard implementation.
- [ ] CI: run layer-1 + layer-2 on every PR touching router or
      wire-protocol code.

## Acceptance

- Every endpoint in the inventory above has a captured corpus + replay
  test passing.
- Manual checklist passes on a fresh Mac with a real Codex Desktop App
  install.
- The runtime guard fires on a synthetically misrouted request (test
  case explicitly designed to violate the table, verify the guard
  downgrades cleanly).

## Notes

- The inventory will need to be refreshed as Codex Desktop App
  versions ship new endpoints. Capture corpus is checked into the repo
  with version metadata so regressions are caught.
- We do NOT capture sensitive user content - only the URL + headers +
  empty body or sanitised body. The capture script enforces this.

## Deviations

(none yet)

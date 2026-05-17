# TASK 190: Indistinguishability live audit for ChatGPT-Sub conversations

Status: PLANNING 2026-05-16
Priority: P0 (load-bearing constraint per user requirement)
Scope: `internal/tlsdial/` (refresh profile catalog), `internal/proxy/wsmitm/`
       (outbound side), new `scripts/utils/indist_probe/`, `internal/tlsproof/`
       (extend), recorded-traffic corpus under `research/indist/`

## Why

The user requirement is verbatim:

> "alles muss für openai wenn die traffic und requests von mir bekommen
> normal ausssehen ununterscheidbar von normalem traffic."

If OpenAI can fingerprint our outbound traffic and decide it's not coming
from a real Codex CLI / Desktop App, two bad things happen:
- they can rate-limit / block us;
- they could ban the user's ChatGPT subscription as "automated abuse".

This task delivers a **provable** indistinguishability gate: we measure
ours-vs-Codex traffic, find every divergence, fix it, then re-measure
until the diff is zero (modulo content payload that we deliberately
mutate per Phase F).

## Threat model

OpenAI/Cloudflare can fingerprint at multiple layers:

| Layer       | Fingerprint    | Our risk                                                |
|-------------|----------------|---------------------------------------------------------|
| TCP         | TCP options    | Low - kernel-level, hard for us to differ on same OS    |
| TLS         | JA3 / JA4 / JA4_RAW | High - rustls vs golang crypto/tls differ visibly    |
| HTTP/2      | SETTINGS frame, HEADERS pseudo-header order | High - rustls h2 vs net/http differ |
| HTTP/1.1    | Header order, casing | Medium - net/http canonicalizes headers          |
| WebSocket   | Sec-WebSocket-Extensions list, ordering, Key entropy | High |
| TLS session | TLS-resume, SCT, EMS support     | Medium                              |
| Timing      | Time-to-first-byte, frame inter-arrival times | Medium - we add latency  |
| Body        | JSON whitespace, key order, encoding | Medium - encoding/json differs     |

Out of scope (kernel-level, can't fix from userspace): TCP MSS, congestion
control variant, IP TTL.

## What we already have

- `internal/tlsdial/` - uTLS-based ClientHello mimicry with a profile
  catalog (Chrome 133, etc.).
- `internal/tlsproof/` - records and verifies our ClientHello matches a
  reference. T123 implemented this; T139 added the provider-edge proof.
- uTLS Profile Catalog under `internal/tlsdial/profiles/`.

What's missing:
- A profile for Codex 0.130 specifically (rustls-based, different from
  Chrome/Firefox).
- HTTP/2 SETTINGS frame fingerprint matching.
- WebSocket-layer fingerprint (extension list ordering, subprotocol
  negotiation, Sec-WebSocket-Key entropy).
- A live-comparison harness: capture real Codex traffic, capture ours,
  produce a diff report.

## Target state

### Part A: Capture-and-diff harness

`scripts/utils/indist_probe/` (Go program):

1. **capture-codex**: instructs the operator to run a no-op Codex command
   (`codex exec "echo hi"`) with our proxy disabled. The operator's
   network adapter is sniffed via tcpdump + a one-time mitm-CA in trust
   to decrypt. Records:
   - TLS ClientHello bytes
   - TLS extensions list + values
   - ALPN preferences
   - HTTP/2 SETTINGS frame
   - HEADERS pseudo-header order
   - WebSocket Upgrade headers + order
   - First-frame timing relative to TCP open

2. **capture-ours**: same Codex command with our proxy enabled. Same
   capture. Produces the same data structure.

3. **diff**: side-by-side compare. Output a report:
   ```
   TLS ClientHello: MATCH (cipher_order, extension_list_order, GREASE
                          tokens position, SNI value placement)
   HTTP/2 SETTINGS: MATCH (HEADER_TABLE_SIZE=65536, INITIAL_WINDOW_SIZE=...)
   HEADERS pseudo order: MATCH (:method, :scheme, :authority, :path)
   WebSocket Upgrade:
     - Sec-WebSocket-Extensions: DIFF
       Codex:  "permessage-deflate; client_max_window_bits"
       Ours:   "permessage-deflate"
     - Header order: MATCH
   First-frame timing:
     Codex: 12 ms
     Ours:  47 ms (Δ +35 ms - investigate)
   ```

4. **fix-loop**: iterate until diff is empty. Lock the result into a
   golden file under `research/indist/<codex-version>-<our-version>/`.

### Part B: Per-version uTLS profile

- Add a `codex_cli_rs_0_130` profile to `internal/tlsdial/profiles/`.
- Profile content sourced from a real ClientHello captured from a fresh
  Codex 0.130 install on macOS 14/15. Captured via wireshark with
  SSLKEYLOGFILE if needed.
- Profile auto-selected when our outbound destination is chatgpt.com
  AND the original UA was a Codex CLI variant.
- Similar profile for `codex_desktop_app_<ver>` once we capture it.

### Part C: HTTP/2 SETTINGS frame alignment

Go's `golang.org/x/net/http2` ships SETTINGS values different from
rustls' defaults (rustls sends `HEADER_TABLE_SIZE=4096` by default,
golang sends `4096` too but the SETTINGS frame ORDER differs).

We need an outbound HTTP/2 stack we can tune. Either:
- Replace `net/http` Transport with a custom `http2.Transport` and
  patch `Settings(...)` to match Codex byte-for-byte.
- Or, more robust: use a uTLS-aware HTTP/2 client. We have one in
  `internal/tlsdial/` as a wrapper.

### Part D: WebSocket Upgrade-header fidelity

The WS upgrade is HTTP/1.1 over TLS. Our outbound client sends:
- `Sec-WebSocket-Key` = base64(16 random bytes). MUST be 16 bytes,
  cryptographically random (RFC 6455 §4.1).
- `Sec-WebSocket-Extensions` MUST be Codex's exact extension list,
  in Codex's exact order. Captured from Part A.
- `User-Agent` MUST be forwarded VERBATIM from the client to upstream.
- `Authorization`, `OAI-Product-SKU`, `x-codex-*` headers VERBATIM.
- Header ordering: HTTP/1.1 spec allows arbitrary order; Codex sends
  in a specific order; we match.

### Part E: Body re-marshalling fidelity

When we mutate the request body (add `stop_sequences`, inject be-terse,
rewrite history), the JSON we send upstream MUST:
- Use the same key ordering as Codex's serializer for unchanged fields.
- Use the same whitespace (none) and number formatting.
- Add only the fields we deliberately add; everything else byte-equal.

Implementation: use `encoding/json` with `OrderedMap` for our mutations,
preserving the input field order. For fields we add, append at end (Codex
serializer is field-declaration-order; appending matches when the field
isn't in Codex's struct).

Validate with a byte-diff on round-trip-unchanged bodies.

### Part F: Timing fidelity

Slimference adds:
- TLS handshake to client (we serve the cert) ~ 1-2 ms
- Compression pipeline ~ 0.5-2 ms (Phase F)
- Outbound TLS handshake to chatgpt.com (we reuse connections) ~ 0.5 ms
  amortised, ~ 50-100 ms on first dial
- First frame to upstream

Budget target: p50 added latency ≤ 5 ms, p95 ≤ 25 ms (Phase G epic
constraint). Validate with the indist harness.

## Sub-Tasks

- [ ] Build `scripts/utils/indist_probe/capture.go` (tcpdump wrapper).
- [ ] Capture Codex 0.130 baseline on macOS (operator task).
- [ ] Capture Codex Desktop App baseline (operator task).
- [ ] Add `codex_cli_rs_0_130` and `codex_desktop_app_<ver>` profiles to
      `internal/tlsdial/profiles/`.
- [ ] Wire the profile selector in `tlsdial.DialContext` to pick the
      Codex profile when destination is chatgpt.com + UA indicates Codex.
- [ ] HTTP/2 SETTINGS frame audit: custom Transport with rustls-like
      defaults if needed.
- [ ] WebSocket Upgrade-header fidelity: pass-through client headers
      verbatim; emit extensions in Codex's order.
- [ ] Body re-marshalling: use OrderedMap-preserving JSON encoder.
- [ ] Timing budget benchmark: latency probe.
- [ ] Lock golden capture under `research/indist/codex-0.130/`.
- [ ] CI verification: every release re-runs the diff against the
      golden and fails on drift.

## Acceptance

- `slimference proxy verify --indist` runs against the recorded golden
  and reports `OK` or a precise diff.
- A trained-eye check (or an OpenAI server-side check) cannot
  distinguish our outbound from a direct Codex outbound on any captured
  layer. (We can verify this ourselves with the diff harness; we cannot
  certify what OpenAI's actual fingerprinter does, but we make ourselves
  identical to a passing baseline.)
- Latency budget held: p50 ≤ 5 ms added, p95 ≤ 25 ms.

## Notes

- This is the riskiest task in Phase G. Indistinguishability is
  asymptotic - we can only prove "no observable diff in our capture";
  OpenAI might still fingerprint dimensions we don't see.
- If we drift over time (rustls library updates, Codex version bumps),
  the CI diff catches it before users see it. The golden file is the
  load-bearing artifact.
- We will NOT spoof a User-Agent we don't have permission to. We
  forward whatever Codex sent, unchanged.

## Deviations

(none yet)

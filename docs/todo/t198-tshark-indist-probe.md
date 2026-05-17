# TASK 198: tshark indist-probe operator tool

Status: TODO (planning 2026-05-16)
Priority: P1 (operator audit tool; runs on demand, not at runtime)
Scope: new `scripts/utils/indist_probe/main.go`, consumes
       `internal/indist/`

## Why

T190 (`internal/indist/`) ships the diff engine + Capture data model.
What it needs is an operator-side instrument that **extracts a Capture
from a real network trace**. tshark is the right tool because:

- already installed (`/opt/homebrew/bin/tshark`, v4.6.5)
- runs unprivileged with `-i lo0` for local loopback or with
  `-i en0` if the operator has sudo
- has structured JSON output (`-T json`) so we don't parse pcap
  binary directly

**This is not a runtime component.** tshark does NOT run in the
daemon, it does NOT capture every Codex turn, it does NOT live in
production traffic flow. It exists to answer one question:

> "Did the latest Codex / rustls / Slimference change my TLS / HTTP/2 /
>  WebSocket fingerprint?"

When to run it:

- **Once at first install**: capture Codex 0.130 baseline before
  enabling MITM. Lock the resulting JSON as the indist golden file
  under `research/indist/codex-0.130/`.
- **Once per Codex release**: confirm Codex 0.131 didn't drift the
  fingerprint.
- **Once per Slimference release** that touches `internal/tlsdial`
  or `internal/tlsca`: confirm our outbound mimics Codex's wire byte-
  equal.
- **Rare, on demand**: when the operator suspects fingerprinting (rate
  limits, blocks, slowdowns).

## Target state

`slimference indist-probe` subcommand or standalone `scripts/utils/
indist_probe/` binary with two modes:

### Mode 1: capture-baseline

```
$ slimference indist-probe capture --label codex_baseline \
      --interface en0 --duration 30s \
      --command 'codex exec --skip-git-repo-check "say only OK"' \
      --output research/indist/codex-0.130/baseline.json
```

Workflow:
1. Launch tshark in the background with `-T json -i <iface>
   -f 'tcp port 443 and host chatgpt.com'`.
2. Run the operator-supplied command in foreground.
3. Wait for the command to exit, plus a 2-second grace window for
   trailing SSE.
4. Stop tshark.
5. Parse the tshark JSON, build one `indist.Capture` per TLS
   connection seen, deduplicate by JA3 fingerprint.
6. Write the picked Capture (the most-frequent fingerprint for the
   target host) as JSON to the output path.

### Mode 2: diff

```
$ slimference indist-probe diff \
      --baseline research/indist/codex-0.130/baseline.json \
      --ours research/indist/codex-0.130/ours.json
```

Loads both Captures, runs `indist.Diff()`, prints a one-line summary
plus the per-field drift table on stderr; exits non-zero on drift.
CI integrates this for release-gate verification.

### Mode 3: lock-golden

```
$ slimference indist-probe lock --capture baseline.json \
      --label codex_cli_rs_0_130
```

Moves the capture to `research/indist/codex-0.130/golden.json`,
records the fingerprint hash in `research/indist/golden_index.json`
so the diff command can find the golden by label.

## Implementation

```go
package main

import (
    "github.com/slimference/slimference/internal/indist"
)

// pseudo
func capture(args captureArgs) (*indist.Capture, error) {
    // 1. start tshark
    tsharkCmd := exec.Command("tshark", "-T", "json",
        "-i", args.iface, "-f", filter)
    stdout, _ := tsharkCmd.StdoutPipe()
    tsharkCmd.Start()
    // 2. run user command
    userCmd := exec.Command("sh", "-c", args.command)
    userCmd.Run()
    // 3. grace window then stop tshark
    time.Sleep(args.grace)
    tsharkCmd.Process.Signal(syscall.SIGTERM)
    // 4. parse tshark JSON, build Captures
    captures := parseTSharkJSON(stdout)
    // 5. pick the one we want
    return pickByHost(captures, args.target), nil
}
```

tshark JSON contains TLS ClientHello bytes verbatim in the
`tls.handshake.extension.data` field; we re-parse those bytes
client-side to extract the cipher list / extension list / curve
list. The host's SNI is in `tls.handshake.extensions_server_name`.
ALPN is in `tls.handshake.extensions_alpn_str`.

For HTTP/2 SETTINGS frames we need `http2.settings.parameter`
records following the handshake.

For WebSocket Upgrade headers we need the HTTP/1.1 plaintext on
the unencrypted half - but Codex talks TLS, so we only see
`tls.app_data` opaque blobs. **The WS extension / subprotocol fields
of the indist.Capture are not fillable from a passive tshark
capture without TLS keys.**

That's actually fine for the use case:
- TLS-layer fields (JA3 / JA4 / ALPN / GREASE / cipher list /
  extension list / curve list) are fillable from a passive trace.
- HTTP/2 SETTINGS, header order, WS Upgrade headers require TLS
  decryption.

For Phase G v1 we accept the TLS-layer subset as the baseline.
HTTP/2 + WS fields stay zero in the captured JSON; `indist.Diff`
treats matching-zero as match. Operators wanting full coverage
must wire `SSLKEYLOGFILE` on the client (some rustls builds honor
it) or use a privileged-capture path - both follow-ups.

## Acceptance

- Running `slimference indist-probe capture` with a synthetic
  Codex-like command produces a valid `indist.Capture` JSON.
- The JSON round-trips through `indist.Diff(self, self) == OK`.
- Running against a real Codex 0.130 session on macOS produces a
  Capture with non-empty JA3, ALPN={"h2","http/1.1"}, SNI
  "chatgpt.com", cipher list, extension list, curve list. Verified
  by the operator (manual checklist).
- `slimference indist-probe diff baseline ours` reports zero drifts
  when both files are byte-equal copies and one drift per modified
  field otherwise.
- The binary depends only on tshark + Go stdlib + indist package -
  no new third-party deps.
- ≥ 90% coverage on the JSON-parsing logic via fixtures.

## Sub-Tasks

- [ ] Skeleton `scripts/utils/indist_probe/main.go` with cobra-
      lite flag parsing (use stdlib `flag`, not external).
- [ ] tshark wrapper: spawn, capture stdout, stop on SIGTERM,
      handle exit codes.
- [ ] tshark JSON parser: extract TLS ClientHello fields.
- [ ] Build `indist.Capture` from extracted fields; populate JA3
      / JA4 strings via existing fingerprint utilities (or compute
      inline from cipher/extension/curve lists).
- [ ] `lock` sub-mode that updates `golden_index.json` with the new
      capture's SHA.
- [ ] `diff` sub-mode that loads two JSONs and runs
      `indist.Diff()`, prints report, exits non-zero on drift.
- [ ] Fixture: a sample tshark JSON capture committed under
      `research/indist/fixtures/sample-clienthello.json` for unit
      tests.
- [ ] Operator-runbook section in `docs/operations.md`: "How to
      refresh the indist golden file".

## Notes

- tshark is GPL-licensed. We only invoke it - no static linking.
- Capture files may contain other-domain TLS metadata (any HTTPS
  traffic to chatgpt.com via the system browser). The capture
  filter (`host chatgpt.com`) limits noise but does not guarantee
  privacy. Operators are advised to run on a quiet network.
- The tshark JSON output is voluminous; we filter to TLS handshake
  records before parsing to keep memory bounded.

## Deviations

(none yet)

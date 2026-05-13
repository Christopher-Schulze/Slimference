# TASK 129: Layer 2 default-ON re-flip (reverse T121)

Status: CODE-COMPLETE / FIRST-RUN ACK + PROVIDER-TUNABLE L2 GREEN (2026-05-13)
Priority: P2
Scope: `internal/config/defaults.go`, `internal/types/provider_caps.go`, `cmd/slimference/doctor.go`, first-run acknowledgement state, `docs/data-policy.md`. Requires T132 race-clean first.
Driver: T121 set `[compression] layer2_enabled = false` as the default after the audit found Layer 2 was sending unredacted conversation prefixes to MiniMax (a third-party / external provider). The audit fix landed as T109 (outbound redaction) and T110/T111 (cache + anchor correctness). Operator (the user driving this Slimference instance) has a MiniMax subscription, accepts the third-party trust label, and wants the saving back. T129 reverses the default flip for fresh installs, keeps every safety rail (T109 redaction default-on, T121 trust-label warning visible), adds an explicit first-run acknowledgement, and updates doctor output so the operator and any future user sees what the policy is.

This is not "silent default-on". Default-on is allowed only after T132 is race-clean and only with a loud first-run/doctor policy surface. Existing explicit opt-out configs remain off.

## Why not silent default-on?

Layer 2 sends conversation content to MiniMax after redaction. Redaction removes secrets/auth/path risk, but it does not make code, comments, filenames, and task context local-only. For the operator's local install that trade-off is accepted. For a fresh user, silent enablement would be a hidden third-party data path. The correct shape is: enabled by default for fresh config, but first run must say exactly what happens and how to disable it.

---

## Problem (current state)

After T121 the relevant defaults are:

```
[compression]
layer2_enabled = false     # T121 default
[compression.summary]
outbound_redaction = "default"   # T109 default
provider_trust = "external_third_party"  # MiniMax
```

T129 intentionally reverses this for fresh configs. The user is the operator, has the MiniMax subscription, accepts the external-provider trade-off, and wants L2 default-on for new installs. This is allowed only with the loud doctor warning, first-run acknowledgement marker, and `slimference layer2 disable` off-switch described below.

## Target state

```
[compression]
layer2_enabled = true      # T129 reversal, post-T109 + T110/T111 + T132 safety
[compression.summary]
outbound_redaction = "default"   # unchanged
provider_trust = "external_third_party"
```

Everything else stays. Doctor output continues to show the trust-label warning so a future operator who reads through it can see "L2 is enabled and ships data to MiniMax (PRC-hosted)". `slimference layer2 disable` remains the off-switch. The first-run acknowledgement is recorded locally so the warning is explicit without becoming a prompt loop.

## Implementation plan

### WP1 - Config flip

Completed:

- `internal/config/defaults.go`: `Compression.Layer2Enabled` changed from `false` to `true`.
- `DefaultTOML()`: `layer2_enabled = true`.
- Migration safety: existing configs with explicit `layer2_enabled = false` keep false because TOML decoding overlays that value onto the default config.

### WP0 - Race-clean prerequisite

- Complete T132 before this task.
- `go test -race ./...` must pass after the default flip.
- Any Layer 2 telemetry counters touched by T129 must be race-safe.

### WP2 - Doctor warning rewording

Completed. `slimference doctor` now emits a WARN-level line for L2 provider trust instead of treating the accepted external provider as a hard FAIL. If L2 is enabled and trust = external_third_party, the line reads:
  `[WARN ] L2 enabled - outbound to MiniMax (external_third_party). Redaction: default. See docs/data-policy.md`
- The WARN level is intentional. It is not a FAIL because the operator opted in (default ON or via subcommand); but it is loud enough to remind every doctor run that data is leaving the machine.

### WP2b - First-run acknowledgement

- Completed. On first interactive TUI start with L2 default-enabled and no acknowledgement marker, print a blocking terminal warning:
  `Layer 2 is enabled by default and sends redacted conversation content to MiniMax (external third party / PRC-hosted). Press Enter to acknowledge, or run slimference layer2 disable.`
- Non-interactive daemon/proxy startup does not hang; it logs a WARN and continues.
- `slimference layer2 acknowledge` records the acknowledgement explicitly.
- `slimference layer2 status` shows whether the acknowledgement marker is recorded.
- The acknowledgement is stored under `~/.slimference/policy/layer2-default-on-ack.json` with version and timestamp.

### WP3 - data-policy.md update

- Completed. `docs/data-policy.md` updates the "Default state" section to reflect L2 being on, with a stronger explainer for first-time readers:
  - "Layer 2 is on by default. With redaction enabled (default) the outbound data has secrets stripped, paths normalised, auth headers dropped, and JSON credential keys redacted. The conversation content itself (code, comments, file references) does leave your machine. To disable: `slimference layer2 disable`."

### WP4 - Acceptance test

- Existing T121 default test now asserts default true.
- Explicit-false config test asserts `layer2_enabled = false` remains disabled.
- Doctor warning tests continue to cover upstream override and external third-party fallback.

### WP5 - Communication

- `docs/transparent-mode.md` mentions L2 default-on so the operator sees both surfaces in one read.
- `slimference layer2 status` output reflects new default.

## Acceptance criteria

- [x] Fresh config: Layer 2 on by default.
- [x] Explicit `layer2_enabled = false` configs continue to disable.
- [x] T132 known race fixed before the default flip.
- [x] Doctor surfaces the WARN-level outbound-data line on every run with L2 enabled.
- [x] First-run acknowledgement exists for interactive startup; non-interactive startup logs/statuses the policy warning without hanging.
- [x] `docs/data-policy.md` reflects new default state.
- [x] Existing T121 tests adjusted; explicit-false test added.
- [x] CI gate green; race-clean after full Phase R batch.

## Out of scope

- Deep provider migration UI. The runtime path is now provider-tunable through `[compression.minimax]` and `SLIMFERENCE_MINIMAX_*` overrides, but T129 does not add a separate provider registry UI.
- Local-LLM summariser path (rejected by operator brief: "ich habe keine ressourcen auf meinem mac übrig").
- Re-doing the T109 redaction. T129 trusts T109 and ships on top of it.
- Re-doing T110 cache or T111 anchor logic. T129 inherits both.

## Validation

```
go test -race ./internal/config/... ./internal/summarization/... ./cmd/slimference/...
slimference doctor   # expect WARN-level L2 outbound line on default-config install
slimference layer2 status   # expect "Layer 2 is enabled (default)"
```

## Notes on user's brief

Operator: "default on bitte!"

Done. No further policy bits required; T109 + T110 + T111 + T121's redaction + trust-label safeguards remain active and visible.

2026-05-13 stabilisation:

- `[compression.minimax]` remains the historical section name, but the client is now documented and tested as an OpenAI-compatible `/chat/completions` summarizer endpoint.
- Env overrides now support direct `SLIMFERENCE_MINIMAX_API_KEY`, `SLIMFERENCE_MINIMAX_API_KEY_ENV`, `SLIMFERENCE_MINIMAX_BASE_URL`, `SLIMFERENCE_MINIMAX_MODEL`, sampling/timeouts/rate-limit/capability flags, trust class, prompt override path, and deterministic-mode policy.
- MiniMax M2.x `reasoning_split` is default-on to keep thinking content out of `message.content`; set `SLIMFERENCE_MINIMAX_ENABLE_REASONING_SPLIT=false` for non-MiniMax compatible providers that reject the extension.

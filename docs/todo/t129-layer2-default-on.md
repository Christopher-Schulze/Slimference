# TASK 129: Layer 2 default-ON re-flip (reverse T121)

Status: PENDING (planned 2026-05-01)
Priority: P2
Scope: `internal/config/defaults.go`, `internal/types/provider_caps.go`, `cmd/slimference/doctor.go`, `docs/data-policy.md`.
Driver: T121 set `[compression] layer2_enabled = false` as the default after the audit found Layer 2 was sending unredacted conversation prefixes to MiniMax (a third-party / external provider). The audit fix landed as T109 (outbound redaction) and T110/T111 (cache + anchor correctness). Operator (the user driving this Slimference instance) has a MiniMax subscription, accepts the third-party trust label, and wants the saving back. T129 reverses the default flip, keeps every safety rail (T109 redaction default-on, T121 trust-label warning visible), and updates the doctor output so the operator and any future user sees what the policy is.

This task is policy + config only. No new code; reverse a default, update one warning, refresh one doc.

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

A fresh install runs without L2; `slimference layer2 enable --acknowledge-data-policy` is the explicit-opt-in path. The user is the operator, has acknowledged, has the MiniMax subscription, wants L2 default-on for new installs.

## Target state

```
[compression]
layer2_enabled = true      # T129 reversal, post-T109 + T127 + T128 safety
[compression.summary]
outbound_redaction = "default"   # unchanged
provider_trust = "external_third_party"
```

Everything else stays. Doctor output continues to show the trust-label warning so a future operator who reads through it can see "L2 is enabled and ships data to MiniMax (PRC-hosted)". `slimference layer2 disable` remains the off-switch.

## Implementation plan

### WP1 - Config flip

- `internal/config/defaults.go`: change `Compression.Layer2Enabled` from `false` to `true`.
- Migration safety: existing configs with the explicit `layer2_enabled = false` keep the false (the operator was specific). Configs without the key (missing field, fresh install) inherit the new true.

### WP2 - Doctor warning rewording

- Today `slimference doctor` says "L2 disabled (no outbound data)" or "L2 enabled - outbound to MiniMax".
- After T129: if L2 enabled and trust = external_third_party, the line reads:
  `[WARN ] L2 enabled - outbound to MiniMax (external_third_party). Redaction: default. See docs/data-policy.md`
- The WARN level is intentional. It is not a FAIL because the operator opted in (default ON or via subcommand); but it is loud enough to remind every doctor run that data is leaving the machine.

### WP3 - data-policy.md update

- `docs/data-policy.md` updates the "Default state" section to reflect L2 being on, with a stronger explainer for first-time readers:
  - "Layer 2 is on by default. With redaction enabled (default) the outbound data has secrets stripped, paths normalised, auth headers dropped, and JSON credential keys redacted. The conversation content itself (code, comments, file references) does leave your machine. To disable: `slimference layer2 disable`."

### WP4 - Acceptance test

- Existing T121 tests assert default is false. Update them to assert default is true.
- New test: fresh-config flow (no `compression.layer2_enabled` key set) yields enabled=true.
- New test: explicit-false config yields enabled=false (back-compat for operators who set false explicitly).

### WP5 - Communication

- `docs/transparent-mode.md` mentions L2 default-on so the operator sees both surfaces in one read.
- `slimference layer2 status` output reflects new default.

## Acceptance criteria

- [ ] Fresh config: Layer 2 on by default.
- [ ] Explicit `layer2_enabled = false` configs continue to disable.
- [ ] Doctor surfaces the WARN-level outbound-data line on every run with L2 enabled.
- [ ] `docs/data-policy.md` reflects new default state.
- [ ] Existing T121 tests adjusted; new tests added; coverage 100%.
- [ ] CI gate green; race-clean.

## Out of scope

- Switching providers (replacing MiniMax with an alternative). Operator already has the MiniMax subscription; T129 stays compatible.
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

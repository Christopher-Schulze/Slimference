# T345: Healthy daemon stale-process status guard

## Why

`slimference status` can briefly see the current daemon PID in a macOS process
state that looks like a stale `U` process. Reporting the healthy current daemon
as an "old stuck" process is a false status signal and makes the product look
less trustworthy.

## Acceptance

- `slimference status` still reports real old stuck Slimference processes.
- The currently running healthy daemon PID is excluded from stale-process
  notices.
- Codex CLI processes that mention `slimference-codex` only in provider args
  are not classified as Slimference processes.
- The fix changes status reporting only; daemon lifecycle, routing, cache,
  payloads, and provider traffic are unchanged.
- Tests cover the PID exclusion and existing status rendering.

## Changes

- Added `staleSlimferenceProcessNoticeIgnoringPID`.
- Tightened stale-process matching to Slimference executables, not arbitrary
  command lines containing the word `slimference`.
- `renderStatus` excludes the current healthy daemon PID from stale-process
  notices.
- Added regression coverage for ignoring only the current daemon PID.

## Verification

- Focused gate passed:
  - `go test ./cmd/slimference ./internal/codexthreads ./internal/proxy -run 'TestStaleSlimferenceProcessNoticeIgnoringPID|TestParseStaleSlimferenceProcesses|TestRenderStatusHuman|TestRenderStatusIncludesStaleProcessNotice|TestSavingsHashFallbackMatchesProxyHashWithoutTrimming|TestSavingsResolvesHashFallbackToLocalCodexThread|TestSavingsKeepsAmbiguousHashFallbackUnattributed|TestComputeSavingsLiveUsesCurrentDaemonWindow|TestExtractSessionIDCodexHTTPUsesStrongThreadHeaders|TestExtractClientFamilyCodexHTTPFallbacks|TestLookupWindowCurrentCodexSchema' -count=1`
- Package gate passed:
  - `go test ./cmd/slimference ./internal/proxy ./internal/codexthreads ./internal/analytics -count=1`
- Full CI:
  - `go run ./scripts/ci` passed all 8 steps; total coverage `95.3%`
- Installed proof:
  - `go run ./scripts/build --install --restart`
  - `slimference status` reports daemon running without a stale-process line
    for the current healthy PID.
